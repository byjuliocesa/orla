// Package childenv keeps another Claude Code session's markers out of the processes Orla starts.
//
// When Orla itself is started from inside a Claude Code session (or one of its terminals), it
// inherits variables that tell a claude it is that session's child. A claude that sees them stops
// saving its transcript, so it could not be resumed, forked or imported, and may take the parent's
// effort or bridge. Orla's own runs and terminals are sessions of their own: these go.
package childenv

import (
	"os"
	"strings"
)

// markers are the variables a Claude Code session sets for what it starts. Settings the owner
// chose (CLAUDE_CODE_USE_BEDROCK, CLAUDE_CODE_MAX_OUTPUT_TOKENS...) are not among them and stay.
var markers = map[string]bool{
	"CLAUDECODE":                    true,
	"CLAUDE_CODE_ENTRYPOINT":        true,
	"CLAUDE_CODE_SESSION_ID":        true,
	"CLAUDE_CODE_CHILD_SESSION":     true,
	"CLAUDE_CODE_SESSION_ATTENDED":  true,
	"CLAUDE_CODE_BRIDGE_SESSION_ID": true,
	"CLAUDE_CODE_MESSAGING_SOCKET":  true,
	"CLAUDE_CODE_MESSAGING_TOKEN":   true,
	"CLAUDE_CODE_EXECPATH":          true,
	"CLAUDE_PID":                    true,
	"CLAUDE_EFFORT":                 true,
}

// Clean is env without another session's markers.
func Clean(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if !markers[name] {
			out = append(out, kv)
		}
	}
	return out
}

// Environ is this process's environment, cleaned.
func Environ() []string { return Clean(os.Environ()) }
