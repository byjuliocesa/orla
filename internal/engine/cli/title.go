package cli

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"unicode/utf8"
)

// titleInstructions asks for what Claude Code itself shows as a session's name.
const titleInstructions = "You name coding sessions. Reply with only a title of 3 to 6 words for the session " +
	"that the user's first message starts, in the language of that message, in sentence case, " +
	"with no quotes, no markdown and no final period."

// Title asks the CLI's smallest model for a short name for a session that starts with prompt,
// the way Claude Code names its interactive sessions, which it does not do for headless runs.
// It runs with no tools, no settings (so no hooks) and nothing saved, in the temp directory.
func (c Claude) Title(ctx context.Context, prompt string) (string, error) {
	path := c.Path
	if path == "" {
		path = "claude"
	}
	if utf8.RuneCountInString(prompt) > 4000 {
		prompt = string([]rune(prompt)[:4000])
	}
	cmd := exec.CommandContext(ctx, path, "-p", "--model", "haiku", "--no-session-persistence",
		"--tools", "", "--setting-sources", "", "--output-format", "text",
		"--system-prompt", titleInstructions)
	// On stdin, so a prompt that starts with "-" is never read as a flag, and stays out of ps.
	cmd.Stdin = strings.NewReader(prompt)
	cmd.Dir = os.TempDir()
	cmd.Env = headlessEnv()
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return cleanTitle(string(out)), nil
}

// cleanTitle keeps the first line, without quotes or a final period, at 80 characters at most.
func cleanTitle(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	s = strings.Trim(strings.TrimSpace(s), `"'“”*#`)
	s = strings.TrimSuffix(strings.TrimSpace(s), ".")
	if utf8.RuneCountInString(s) > 80 {
		s = strings.TrimSpace(string([]rune(s)[:80]))
	}
	return s
}
