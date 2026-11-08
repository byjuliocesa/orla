package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// ContextUsage is what Claude Code's /context reports of a conversation: how full its window
// is and what fills it.
type ContextUsage struct {
	Model      string        `json:"model"`
	Used       int64         `json:"used"`
	Total      int64         `json:"total"`
	Categories []ContextPart `json:"categories"`
	MCPTools   []ContextPart `json:"mcp_tools,omitempty"`
	Skills     []ContextPart `json:"skills,omitempty"`
	Memory     []ContextPart `json:"memory,omitempty"`
}

// ContextPart is one row of /context's tables: a name, its tokens, and, for categories, the
// share of the window.
type ContextPart struct {
	Name    string  `json:"name"`
	Tokens  int64   `json:"tokens"`
	Percent float64 `json:"percent,omitempty"`
	// Detail is the table's other column: a tool's server, a skill's source, a file's type.
	Detail string `json:"detail,omitempty"`
}

// SkillSettings is the --settings value that leaves the named skills out of a conversation,
// through Claude Code's own skillOverrides; "" when none are.
func SkillSettings(off []string) string {
	if len(off) == 0 {
		return ""
	}
	o := map[string]string{}
	for _, n := range off {
		o[n] = "off"
	}
	b, _ := json.Marshal(map[string]any{"skillOverrides": o})
	return string(b)
}

// Context asks Claude Code for /context on a conversation. /context runs inside the CLI with
// no model call, so it costs nothing; the conversation is opened as a fork that is never
// saved, so it is not touched. settings is what the session's runs get (see SkillSettings).
func (c Claude) Context(ctx context.Context, native, cwd, settings string) (ContextUsage, error) {
	args := []string{"-p", "--resume", native, "--fork-session", "--no-session-persistence", "--output-format", "json"}
	if settings != "" {
		args = append(args, "--settings", settings)
	}
	return c.context(ctx, cwd, args)
}

// Library is every skill Claude Code would load in a folder, with what each costs: /context
// of a new conversation there, nothing turned off.
func (c Claude) Library(ctx context.Context, cwd string) ([]ContextPart, error) {
	u, err := c.context(ctx, cwd, []string{"-p", "--no-session-persistence", "--output-format", "json"})
	return u.Skills, err
}

func (c Claude) context(ctx context.Context, cwd string, args []string) (ContextUsage, error) {
	path := c.Path
	if path == "" {
		path = "claude"
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = cwd
	cmd.Env = headlessEnv()
	cmd.Stdin = strings.NewReader("/context")
	out, err := cmd.Output()
	if err != nil {
		return ContextUsage{}, err
	}
	var res struct {
		Result  string `json:"result"`
		IsError bool   `json:"is_error"`
	}
	if err := json.Unmarshal(out, &res); err != nil || res.IsError {
		return ContextUsage{}, errors.New("claude did not report its context")
	}
	return parseContext(res.Result), nil
}

var (
	modelLine  = regexp.MustCompile(`\*\*Model:\*\*\s*(\S+)`)
	tokensLine = regexp.MustCompile(`\*\*Tokens:\*\*\s*([\d.,]+[km]?)\s*/\s*([\d.,]+[km]?)`)
)

// parseContext reads /context's Markdown: the header lines and its tables, by section.
func parseContext(md string) ContextUsage {
	var u ContextUsage
	if m := modelLine.FindStringSubmatch(md); m != nil {
		u.Model = m[1]
	}
	if m := tokensLine.FindStringSubmatch(md); m != nil {
		u.Used, u.Total = tokenCount(m[1]), tokenCount(m[2])
	}
	section := ""
	for _, line := range strings.Split(md, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "### ") {
			section = strings.ToLower(strings.TrimPrefix(line, "### "))
			continue
		}
		if !strings.HasPrefix(line, "|") || strings.HasPrefix(line, "|--") || strings.HasPrefix(line, "| --") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		if len(cells) < 2 {
			continue
		}
		switch {
		case strings.Contains(section, "by category") && len(cells) >= 3 && cells[0] != "Category":
			pct, _ := strconv.ParseFloat(strings.TrimSuffix(cells[2], "%"), 64)
			u.Categories = append(u.Categories, ContextPart{Name: cells[0], Tokens: tokenCount(cells[1]), Percent: pct})
		case strings.HasPrefix(section, "mcp tools") && len(cells) >= 3 && cells[0] != "Tool":
			u.MCPTools = append(u.MCPTools, ContextPart{Name: cells[0], Detail: cells[1], Tokens: tokenCount(cells[2])})
		case strings.HasPrefix(section, "skills") && len(cells) >= 3 && cells[0] != "Skill":
			u.Skills = append(u.Skills, ContextPart{Name: cells[0], Detail: cells[1], Tokens: tokenCount(cells[2])})
		case strings.HasPrefix(section, "memory") && len(cells) >= 3 && cells[0] != "Type":
			u.Memory = append(u.Memory, ContextPart{Name: cells[1], Detail: cells[0], Tokens: tokenCount(cells[2])})
		}
	}
	return u
}

// tokenCount reads 774.9k, 1m, 661 or ~290 as a number of tokens.
func tokenCount(s string) int64 {
	s = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "~")))
	s = strings.ReplaceAll(s, ",", "")
	mult := 1.0
	switch {
	case strings.HasSuffix(s, "k"):
		mult, s = 1e3, strings.TrimSuffix(s, "k")
	case strings.HasSuffix(s, "m"):
		mult, s = 1e6, strings.TrimSuffix(s, "m")
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return int64(f*mult + 0.5)
}
