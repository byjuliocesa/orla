package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/byjuliocesa/orla/internal/events"
)

// renderer prints a run's events as a readable timeline.
type renderer struct {
	color  bool
	limits map[string]struct{ Used float64 }
	total  []string
}

func newRenderer(color bool) *renderer { return &renderer{color: color} }

func (r *renderer) paint(code, s string) string {
	if !r.color {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func (r *renderer) dim(s string) string { return r.paint("2", s) }

func (r *renderer) render(e events.Event) {
	var d map[string]json.RawMessage
	_ = json.Unmarshal(e.Data, &d)
	str := func(k string) string {
		var s string
		_ = json.Unmarshal(d[k], &s)
		return s
	}
	num := func(k string) float64 {
		var f float64
		_ = json.Unmarshal(d[k], &f)
		return f
	}
	indent := ""
	if _, ok := d["parent_call"]; ok {
		indent = "    " // a subagent's step
	}

	switch e.Type {
	case events.RunStarted:
		where := "  in " + str("cwd")
		if t := str("terminal"); t != "" {
			where += " · typed in " + t
		}
		prompt := str("prompt")
		if prompt == "" {
			prompt = "(prompt not seen)"
		}
		fmt.Println(r.paint("1", "▶ "+oneLine(prompt, 100)) + r.dim(where))
	case events.MessageAssistant:
		fmt.Println(indent + strings.ReplaceAll(strings.TrimSpace(str("text")), "\n", "\n"+indent))
	case events.ToolCall:
		fmt.Println(indent + r.paint("36", "→ "+str("name")) + " " + r.dim(toolSummary(d["input"])))
	case events.ToolResult:
		var ok bool
		_ = json.Unmarshal(d["ok"], &ok)
		mark := r.paint("32", "✓")
		if !ok {
			mark = r.paint("31", "✗")
		}
		out := str("output")
		lines := strings.Count(strings.TrimRight(out, "\n"), "\n") + 1
		detail := oneLine(out, 90)
		if lines > 1 {
			detail += fmt.Sprintf(" (+%d lines)", lines-1)
		}
		fmt.Println(indent + "  " + mark + " " + r.dim(detail))
	case events.Usage:
		switch str("scope") {
		case "account":
			_ = json.Unmarshal(d["limits"], &r.limits)
		case "run":
			r.total = []string{plural(int(num("turns")), "turn")}
			// Interactive runs are measured from the transcript, which has no duration or cost.
			if ms := num("duration_ms"); ms > 0 {
				r.total = append(r.total, (time.Duration(ms) * time.Millisecond).Round(100*time.Millisecond).String())
			}
			r.total = append(r.total, fmt.Sprintf("tokens in %s · out %s · cache read %s · cache write %s",
				count(num("input_tokens")), count(num("output_tokens")),
				count(num("cache_read_tokens")), count(num("cache_write_tokens"))))
			if _, ok := d["list_cost_usd"]; ok {
				r.total = append(r.total, fmt.Sprintf("≈$%.4f at API prices", num("list_cost_usd")))
			}
		}
	case events.ApprovalRequested:
		fmt.Println(indent + r.paint("33", "? permission for "+str("tool")) + " " + r.dim(toolSummary(d["input"])))
		where := "  id " + str("approval_id") + " · session " + e.Session
		if t := str("terminal"); t != "" {
			where += " · also asked in " + t
		}
		fmt.Println(indent + r.dim(where))
	case events.ApprovalResolved:
		var allowed, withdrawn bool
		_ = json.Unmarshal(d["allowed"], &allowed)
		_ = json.Unmarshal(d["withdrawn"], &withdrawn)
		if withdrawn {
			// Nobody decided through Orla; the terminal still has the prompt.
			fmt.Println(indent + "  " + r.dim("withdrawn · "+oneLine(str("message"), 80)))
			break
		}
		verdict := r.paint("31", "denied")
		if allowed {
			verdict = r.paint("32", "allowed")
		}
		note := ""
		if m := str("message"); m != "" && !allowed {
			note = " · " + oneLine(m, 80)
		}
		fmt.Println(indent + "  " + verdict + r.dim(" by "+str("by")+note))
	case events.Status:
		switch str("state") {
		case "waiting":
			fmt.Println(indent + r.paint("33", "⏸ waiting for you") + r.dim(" "+oneLine(str("message"), 100)))
		case "ended":
			fmt.Println(r.dim("· session ended"))
		}
	case events.Error:
		fmt.Println(indent + r.paint("31", "! "+str("message")))
	case events.RunFinished:
		status := str("status")
		color := "32"
		if status != "done" {
			color = "31"
		}
		parts := append([]string{r.paint(color, "■ "+status)}, r.total...)
		r.total = nil
		var windows []string
		for name, w := range r.limits {
			windows = append(windows, fmt.Sprintf("%s %.0f%%", strings.ReplaceAll(name, "_", " "), w.Used*100))
		}
		sort.Strings(windows)
		if len(windows) > 0 {
			parts = append(parts, "plan used: "+strings.Join(windows, ", "))
		}
		if len(parts) > 1 {
			fmt.Println(parts[0] + r.dim(" · "+strings.Join(parts[1:], " · ")))
		} else {
			fmt.Println(parts[0])
		}
		if status != "done" && str("summary") != "" {
			fmt.Println(r.dim("  " + oneLine(str("summary"), 200)))
		}
	}
}

// toolSummary picks the input field that says the most about a tool call.
func toolSummary(raw json.RawMessage) string {
	var in map[string]any
	if json.Unmarshal(raw, &in) != nil {
		return ""
	}
	for _, k := range []string{"command", "file_path", "path", "pattern", "url", "query", "description", "prompt"} {
		if v, ok := in[k].(string); ok && v != "" {
			return oneLine(v, 100)
		}
	}
	return oneLine(string(raw), 100)
}

func oneLine(s string, max int) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i] + " …"
	}
	if r := []rune(s); len(r) > max {
		s = string(r[:max]) + "…"
	}
	return s
}

func count(n float64) string {
	switch {
	case n >= 1e6:
		return fmt.Sprintf("%.1fM", n/1e6)
	case n >= 1e3:
		return fmt.Sprintf("%.1fk", n/1e3)
	}
	return fmt.Sprintf("%.0f", n)
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}
