package cli

import (
	"encoding/json"
	"strings"

	"github.com/Julio0Cesar/orla/internal/events"
)

// maxToolOutput is how much of a tool's output an event carries. Timelines show a glimpse;
// the whole output stays in the CLI's own transcript.
const maxToolOutput = 8 << 10

// Out is one event an engine wants published. The session fills in the envelope.
type Out struct {
	Type string
	Data any
}

// Outcome is what a finished run reports.
type Outcome struct {
	// NativeSession is the CLI's session id, the handle to resume it on the next run.
	NativeSession string
	Status        string // done, failed or cancelled
	Summary       string
}

// claudeParser turns Claude Code's stream-json output (claude -p --output-format stream-json
// --verbose) into events, one line at a time.
type claudeParser struct {
	outcome Outcome
	// A response with several content blocks arrives as several "assistant" lines that repeat
	// the same message id and usage; usage is counted once per message.
	counted map[string]bool
}

func newClaudeParser() *claudeParser {
	return &claudeParser{counted: map[string]bool{}}
}

type claudeLine struct {
	Type            string          `json:"type"`
	Subtype         string          `json:"subtype"`
	SessionID       string          `json:"session_id"`
	Model           string          `json:"model"`
	ParentToolUseID *string         `json:"parent_tool_use_id"`
	Message         json.RawMessage `json:"message"`
	RateLimitInfo   *struct {
		Windows map[string]struct {
			Utilization float64 `json:"utilization"`
			ResetsAt    int64   `json:"resetsAt"`
		} `json:"unifiedWindows"`
	} `json:"rate_limit_info"`

	// result lines
	IsError           bool            `json:"is_error"`
	Result            string          `json:"result"`
	NumTurns          int             `json:"num_turns"`
	DurationMS        int64           `json:"duration_ms"`
	TotalCostUSD      float64         `json:"total_cost_usd"`
	Usage             *claudeUsage    `json:"usage"`
	PermissionDenials json.RawMessage `json:"permission_denials"`
	// Per model, what this run alone used. total_cost_usd covers the whole conversation since
	// its first run, so on a resumed session it is not this run's cost.
	ModelUsageSinceLastResult map[string]struct {
		CostUSD float64 `json:"costUSD"`
	} `json:"model_usage_since_last_result"`
}

type claudeMessage struct {
	ID      string          `json:"id"`
	Model   string          `json:"model"`
	Content json.RawMessage `json:"content"`
	Usage   *claudeUsage    `json:"usage"`
}

type claudeUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
}

type claudeBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

// Line parses one line of output. Lines it does not know, or cannot parse, yield nothing:
// the CLI adds message types between versions, and an unknown one must not end the run.
func (p *claudeParser) Line(line []byte) []Out {
	var l claudeLine
	if err := json.Unmarshal(line, &l); err != nil {
		return nil
	}
	if l.SessionID != "" {
		p.outcome.NativeSession = l.SessionID
	}

	switch l.Type {
	case "assistant":
		return p.assistant(l)
	case "user":
		return p.user(l)
	case "rate_limit_event":
		return p.limits(l)
	case "result":
		return p.result(l)
	}
	return nil
}

// parentCall is set on events from a subagent: the id of the tool call that started it.
func parentCall(l claudeLine) map[string]any {
	if l.ParentToolUseID == nil || *l.ParentToolUseID == "" {
		return map[string]any{}
	}
	return map[string]any{"parent_call": *l.ParentToolUseID}
}

func with(base map[string]any, kv ...any) map[string]any {
	out := map[string]any{}
	for k, v := range base {
		out[k] = v
	}
	for i := 0; i+1 < len(kv); i += 2 {
		out[kv[i].(string)] = kv[i+1]
	}
	return out
}

func (p *claudeParser) assistant(l claudeLine) []Out {
	var m claudeMessage
	if json.Unmarshal(l.Message, &m) != nil {
		return nil
	}
	var blocks []claudeBlock
	_ = json.Unmarshal(m.Content, &blocks)

	base := parentCall(l)
	var out []Out
	for _, b := range blocks {
		switch b.Type {
		case "text":
			if strings.TrimSpace(b.Text) != "" {
				out = append(out, Out{events.MessageAssistant, with(base, "text", b.Text, "final", true)})
			}
		case "tool_use":
			out = append(out, Out{events.ToolCall, with(base, "call_id", b.ID, "name", b.Name, "input", b.Input)})
		}
	}

	if m.Usage != nil && (m.ID == "" || !p.counted[m.ID]) {
		if m.ID != "" {
			p.counted[m.ID] = true
		}
		out = append(out, Out{events.Usage, with(base,
			"scope", "step",
			"model", m.Model,
			"input_tokens", m.Usage.InputTokens,
			"output_tokens", m.Usage.OutputTokens,
			"cache_read_tokens", m.Usage.CacheReadInputTokens,
			"cache_write_tokens", m.Usage.CacheCreationInputTokens,
		)})
	}
	return out
}

func (p *claudeParser) user(l claudeLine) []Out {
	var m claudeMessage
	if json.Unmarshal(l.Message, &m) != nil {
		return nil
	}
	var blocks []claudeBlock
	if json.Unmarshal(m.Content, &blocks) != nil {
		// A plain string is the prompt itself, which run.started already carries.
		return nil
	}

	base := parentCall(l)
	var out []Out
	for _, b := range blocks {
		if b.Type != "tool_result" {
			continue
		}
		text, truncated := truncate(toolText(b.Content), maxToolOutput)
		out = append(out, Out{events.ToolResult, with(base,
			"call_id", b.ToolUseID, "ok", !b.IsError, "output", text, "truncated", truncated)})
	}
	return out
}

// toolText flattens a tool result, which is either a string or a list of content blocks.
func toolText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []claudeBlock
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	var parts []string
	for _, b := range blocks {
		switch b.Type {
		case "text":
			parts = append(parts, b.Text)
		default:
			parts = append(parts, "["+b.Type+"]")
		}
	}
	return strings.Join(parts, "\n")
}

func truncate(s string, max int) (string, bool) {
	if len(s) <= max {
		return s, false
	}
	// Back up to a rune boundary so the cut never leaves half a character.
	cut := max
	for cut > 0 && s[cut]&0xC0 == 0x80 {
		cut--
	}
	return s[:cut], true
}

func (p *claudeParser) limits(l claudeLine) []Out {
	if l.RateLimitInfo == nil || len(l.RateLimitInfo.Windows) == 0 {
		return nil
	}
	windows := map[string]any{}
	for name, w := range l.RateLimitInfo.Windows {
		windows[name] = map[string]any{"used": w.Utilization, "resets_at": w.ResetsAt}
	}
	return []Out{{events.Usage, map[string]any{"scope": "account", "limits": windows}}}
}

func (p *claudeParser) result(l claudeLine) []Out {
	p.outcome.Summary = l.Result
	p.outcome.Status = "done"
	if l.IsError || l.Subtype != "success" {
		p.outcome.Status = "failed"
	}

	// What the run would have cost at API list prices. On a subscription nothing is charged per
	// token; it is still the honest measure of how much the run consumed.
	runCost := l.TotalCostUSD
	if l.ModelUsageSinceLastResult != nil {
		runCost = 0
		for _, m := range l.ModelUsageSinceLastResult {
			runCost += m.CostUSD
		}
	}
	data := map[string]any{
		"scope":                      "run",
		"turns":                      l.NumTurns,
		"duration_ms":                l.DurationMS,
		"list_cost_usd":              runCost,
		"conversation_list_cost_usd": l.TotalCostUSD,
	}
	if l.Usage != nil {
		data["input_tokens"] = l.Usage.InputTokens
		data["output_tokens"] = l.Usage.OutputTokens
		data["cache_read_tokens"] = l.Usage.CacheReadInputTokens
		data["cache_write_tokens"] = l.Usage.CacheCreationInputTokens
	}
	out := []Out{{events.Usage, data}}

	var denials []json.RawMessage
	if json.Unmarshal(l.PermissionDenials, &denials) == nil && len(denials) > 0 {
		out = append(out, Out{events.Error, map[string]any{
			"message":   "tools were refused for lack of permission",
			"denials":   denials,
			"retryable": true,
		}})
	}
	return out
}
