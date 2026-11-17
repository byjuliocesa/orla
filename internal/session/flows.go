package session

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/byjuliocesa/orla/internal/flow"
	"github.com/byjuliocesa/orla/internal/store"
)

// Off is every skill a session's conversation leaves out: the ones turned off by the owner,
// and every flow the session does not follow.
func Off(sess store.Session) []string {
	home, _ := os.UserHomeDir()
	return flow.Off(home, sess.Cwd, sess.Flows, sess.SkillsOff)
}

// flowRefs explains Orla's short references to flows, added to a run only when its prompt has one.
const flowRefs = "In the user's messages, @flow:<name> names an Orla flow: the skill <name>, whose SKILL.md is in ~/.claude/skills/ or .claude/skills/. @flow:<name>#<step> names one of its \"## \" steps, by the step's title in lower case with dashes."

// System is what a run adds to Claude Code's system prompt for the prompt it sends.
func System(prompt string) string {
	if strings.Contains(prompt, "@flow:") {
		return flowRefs
	}
	return ""
}

// Always is the part of the instructions that carries the skills a session keeps loaded: each
// one's SKILL.md, whole, so the agent works by it from the first message without deciding to
// open it. A skill whose file is not found (a plugin's) is named, to be opened with the Skill
// tool.
func Always(home, cwd string, names []string) string {
	var b strings.Builder
	for _, n := range names {
		var body, dir string
		for _, d := range []string{filepath.Join(cwd, ".claude", "skills", n), filepath.Join(home, ".claude", "skills", n)} {
			if raw, err := os.ReadFile(filepath.Join(d, "SKILL.md")); err == nil {
				body, dir = string(raw), d
				break
			}
		}
		if body == "" {
			fmt.Fprintf(&b, "\nThe owner keeps the skill %q loaded in this session: open it with the Skill tool before acting, if this conversation has not yet.\n", n)
			continue
		}
		fmt.Fprintf(&b, "\n<skill name=%q dir=%q>\nThe owner keeps this skill loaded in this session: work by it. Files it names are under its dir.\n\n%s\n</skill>\n", n, dir, strings.TrimSpace(body))
	}
	return b.String()
}

func userHome() string {
	home, _ := os.UserHomeDir()
	return home
}
