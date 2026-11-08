package session

import (
	"os"
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
