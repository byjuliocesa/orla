package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// ErrNoForkPoint: the conversation has no answer to the given prompt in its transcript.
var ErrNoForkPoint = errors.New("that message is not in Claude Code's transcript")

// ForkPoint finds, in the transcript Claude Code keeps of a conversation, the last message of
// its answer to the nth prompt (from 1), so a fork can resume it from there with
// --resume-session-at. Prompts are the owner's own messages; tool results and messages of
// subagents do not count.
func ForkPoint(native string, nth int) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	files, _ := filepath.Glob(filepath.Join(home, ".claude", "projects", "*", native+".jsonl"))
	if len(files) == 0 {
		return "", ErrNoForkPoint
	}
	f, err := os.Open(files[0])
	if err != nil {
		return "", err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), maxLine)
	prompts, last := 0, ""
	for sc.Scan() {
		var l struct {
			Type        string `json:"type"`
			UUID        string `json:"uuid"`
			IsSidechain bool   `json:"isSidechain"`
			IsMeta      bool   `json:"isMeta"`
			Message     struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(sc.Bytes(), &l) != nil || l.IsSidechain {
			continue
		}
		switch {
		case l.Type == "user" && !l.IsMeta && isPrompt(l.Message.Content):
			prompts++
			if prompts == nth+1 {
				break
			}
			continue
		case l.Type == "assistant" && prompts == nth:
			last = l.UUID
			continue
		default:
			continue
		}
		break
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	if last == "" {
		return "", ErrNoForkPoint
	}
	return last, nil
}

// isPrompt says whether a user message's content is something the owner wrote: a string, or
// blocks with text and no tool result.
func isPrompt(content json.RawMessage) bool {
	var s string
	if json.Unmarshal(content, &s) == nil {
		return s != ""
	}
	var blocks []struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(content, &blocks) != nil {
		return false
	}
	text := false
	for _, b := range blocks {
		if b.Type == "tool_result" {
			return false
		}
		text = text || b.Type == "text"
	}
	return text
}

// Bridge is the claude.ai page of a conversation Remote Control was turned on for, read from
// the last bridge-session line of its transcript; "" when it never was.
func Bridge(native string) string {
	home, err := os.UserHomeDir()
	if err != nil || native == "" {
		return ""
	}
	files, _ := filepath.Glob(filepath.Join(home, ".claude", "projects", "*", native+".jsonl"))
	if len(files) == 0 {
		return ""
	}
	f, err := os.Open(files[0])
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), maxLine)
	id := ""
	for sc.Scan() {
		b := sc.Bytes()
		if !bytes.Contains(b, []byte(`"bridge-session"`)) {
			continue
		}
		var l struct {
			Type   string `json:"type"`
			Bridge string `json:"bridgeSessionId"`
		}
		if json.Unmarshal(b, &l) == nil && l.Type == "bridge-session" && l.Bridge != "" {
			id = l.Bridge
		}
	}
	if id == "" {
		return ""
	}
	return "https://claude.ai/code/session_" + strings.TrimPrefix(id, "cse_")
}

// Transcript is the file where Claude Code keeps a conversation, "" when it has none.
func Transcript(native string) string {
	home, err := os.UserHomeDir()
	if err != nil || native == "" {
		return ""
	}
	files, _ := filepath.Glob(filepath.Join(home, ".claude", "projects", "*", native+".jsonl"))
	if len(files) == 0 {
		return ""
	}
	return files[0]
}

// LineParser reads Claude Code's message lines, from a run's output or from its transcript,
// into events.
type LineParser interface {
	Line(line []byte) []Out
}

// NewLineParser parses lines as a run's output is parsed. Transcript lines carry the same
// messages, so a conversation held only in a transcript can be read into the same events.
func NewLineParser() LineParser { return newClaudeParser() }
