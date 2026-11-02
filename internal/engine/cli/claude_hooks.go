package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/Julio0Cesar/orla/internal/events"
)

// Hook is what Claude Code sends to a hook command on stdin. Fields not set by an event are
// empty.
type Hook struct {
	Event          string          `json:"hook_event_name"`
	SessionID      string          `json:"session_id"`
	PromptID       string          `json:"prompt_id"`
	Cwd            string          `json:"cwd"`
	TranscriptPath string          `json:"transcript_path"`
	PermissionMode string          `json:"permission_mode"`
	Prompt         string          `json:"prompt"`
	ToolName       string          `json:"tool_name"`
	ToolInput      json.RawMessage `json:"tool_input"`
	ToolUseID      string          `json:"tool_use_id"`
	ToolResponse   json.RawMessage `json:"tool_response"`
	Error          string          `json:"error"`
	Interrupted    bool            `json:"is_interrupt"`
	Message        string          `json:"message"`
	LastMessage    string          `json:"last_assistant_message"`
	Source         string          `json:"source"`
	Reason         string          `json:"reason"`
}

// Hook events Orla subscribes to.
const (
	HookSessionStart       = "SessionStart"
	HookUserPromptSubmit   = "UserPromptSubmit"
	HookPreToolUse         = "PreToolUse"
	HookPostToolUse        = "PostToolUse"
	HookPostToolUseFailure = "PostToolUseFailure"
	HookNotification       = "Notification"
	HookStop               = "Stop"
	HookSessionEnd         = "SessionEnd"
)

var HookEvents = []string{
	HookSessionStart, HookUserPromptSubmit, HookPreToolUse, HookPostToolUse,
	HookPostToolUseFailure, HookNotification, HookStop, HookSessionEnd,
}

// HookSettings is the Claude Code settings fragment that sends every hook event Orla uses to
// command.
func HookSettings(command string) map[string]any {
	hooks := map[string]any{}
	for _, event := range HookEvents {
		entry := map[string]any{"hooks": []any{map[string]any{"type": "command", "command": command}}}
		switch event {
		case HookPreToolUse, HookPostToolUse, HookPostToolUseFailure:
			entry["matcher"] = "*"
		}
		hooks[event] = []any{entry}
	}
	return map[string]any{"hooks": hooks}
}

// ToolOutput turns a tool response into the text a timeline shows. Bash answers with an object
// of stdout and stderr; other tools with text or arbitrary JSON.
func ToolOutput(raw json.RawMessage) (string, bool) {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return truncate(s, maxToolOutput)
	}
	var shell struct {
		Stdout *string `json:"stdout"`
		Stderr string  `json:"stderr"`
	}
	if json.Unmarshal(raw, &shell) == nil && shell.Stdout != nil {
		out := *shell.Stdout
		if shell.Stderr != "" {
			out = strings.TrimRight(out+"\n"+shell.Stderr, "\n")
		}
		return truncate(out, maxToolOutput)
	}
	if text := toolText(raw); text != "" {
		return truncate(text, maxToolOutput)
	}
	return truncate(string(raw), maxToolOutput)
}

// ErrNoTranscript means the usage of a prompt could not be read.
var ErrNoTranscript = errors.New("transcript unavailable")

// TranscriptUsage adds up what the model used to answer one prompt, from the conversation
// transcript Claude Code keeps: the assistant messages after the user message with that prompt
// id. Hooks carry no usage, so this is the only source for interactive runs.
//
// The transcript format is internal to Claude Code and changes between versions; anything
// unexpected yields ErrNoTranscript, and the run is reported without usage.
//
// Stop fires before Claude Code writes the last response to the transcript. final, the text of
// that response as the Stop hook gives it, tells whether the file has caught up: complete is
// false until an assistant text equal to it is in.
func TranscriptUsage(path, promptID, final string) (usage map[string]any, complete bool, err error) {
	if !isTranscriptPath(path) || promptID == "" {
		return nil, false, ErrNoTranscript
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, false, ErrNoTranscript
	}
	defer f.Close()

	var (
		inPrompt  bool
		found     bool
		finalSeen = strings.TrimSpace(final) == ""
		models    = map[string]bool{}
		seen      = map[string]bool{}
		total     claudeUsage
	)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), maxLine)
	for sc.Scan() {
		var entry struct {
			Type     string `json:"type"`
			PromptID string `json:"promptId"`
			Message  struct {
				ID      string          `json:"id"`
				Model   string          `json:"model"`
				Usage   *claudeUsage    `json:"usage"`
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(sc.Bytes(), &entry) != nil {
			continue
		}
		switch {
		case entry.Type == "user" && entry.PromptID != "":
			inPrompt = entry.PromptID == promptID
			found = found || inPrompt
		case entry.Type == "assistant" && inPrompt && entry.Message.Usage != nil:
			var blocks []claudeBlock
			_ = json.Unmarshal(entry.Message.Content, &blocks)
			for _, b := range blocks {
				if b.Type == "text" && strings.TrimSpace(b.Text) == strings.TrimSpace(final) {
					finalSeen = true
				}
			}
			// A response with several content blocks is written once per block.
			if entry.Message.ID != "" && seen[entry.Message.ID] {
				continue
			}
			seen[entry.Message.ID] = true
			models[entry.Message.Model] = true
			u := entry.Message.Usage
			total.InputTokens += u.InputTokens
			total.OutputTokens += u.OutputTokens
			total.CacheReadInputTokens += u.CacheReadInputTokens
			total.CacheCreationInputTokens += u.CacheCreationInputTokens
		}
	}
	if !found || sc.Err() != nil {
		return nil, false, ErrNoTranscript
	}
	var names []string
	for m := range models {
		names = append(names, m)
	}
	return map[string]any{
		"scope":              "run",
		"models":             names,
		"turns":              len(seen),
		"input_tokens":       total.InputTokens,
		"output_tokens":      total.OutputTokens,
		"cache_read_tokens":  total.CacheReadInputTokens,
		"cache_write_tokens": total.CacheCreationInputTokens,
	}, finalSeen, nil
}

// isTranscriptPath accepts only Claude Code transcripts: the path comes in a hook payload, and
// Orla must not be made to read any other file.
func isTranscriptPath(path string) bool {
	home, err := os.UserHomeDir()
	if err != nil || !filepath.IsAbs(path) || filepath.Ext(path) != ".jsonl" {
		return false
	}
	root := filepath.Join(home, ".claude", "projects") + string(filepath.Separator)
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		if strings.HasPrefix(filepath.Clean(path), filepath.Join(dir, "projects")+string(filepath.Separator)) {
			return true
		}
	}
	return strings.HasPrefix(filepath.Clean(path), root)
}

// HookOuts turns a hook into events, for the parts that need no state. Runs, statuses and usage
// are handled by the session, which knows which run is in progress.
func HookOuts(h Hook) []Out {
	switch h.Event {
	case HookPreToolUse:
		return []Out{{events.ToolCall, map[string]any{"call_id": h.ToolUseID, "name": h.ToolName, "input": h.ToolInput}}}
	case HookPostToolUse:
		out, cut := ToolOutput(h.ToolResponse)
		return []Out{{events.ToolResult, map[string]any{"call_id": h.ToolUseID, "ok": true, "output": out, "truncated": cut}}}
	case HookPostToolUseFailure:
		out, cut := truncate(h.Error, maxToolOutput)
		return []Out{{events.ToolResult, map[string]any{
			"call_id": h.ToolUseID, "ok": false, "output": out, "truncated": cut, "interrupted": h.Interrupted,
		}}}
	}
	return nil
}
