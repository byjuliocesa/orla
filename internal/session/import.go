package session

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/byjuliocesa/orla/internal/engine/cli"
	"github.com/byjuliocesa/orla/internal/events"
	"github.com/byjuliocesa/orla/internal/id"
	"github.com/byjuliocesa/orla/internal/store"
)

// ErrNoTranscript: Claude Code keeps no conversation by that id on this machine.
var ErrNoTranscript = errors.New("Claude Code has no conversation with that id here")

// What Claude Code adds to a prompt for itself, not what the person wrote.
var harnessNotes = regexp.MustCompile(`(?s)<(system-reminder|command-name|command-message|command-args|local-command-stdout|local-command-caveat|task-notification)>.*?</(system-reminder|command-name|command-message|command-args|local-command-stdout|local-command-caveat|task-notification)>`)

// systemNote marks a whole message as Claude Code's, such as a background task's end.
const systemNote = "[SYSTEM NOTIFICATION"

type transcriptLine struct {
	Type        string    `json:"type"`
	IsSidechain bool      `json:"isSidechain"`
	IsMeta      bool      `json:"isMeta"`
	Cwd         string    `json:"cwd"`
	Timestamp   time.Time `json:"timestamp"`
	Message     struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
	// A message sent while Claude Code worked is kept as a queued_command attachment.
	Attachment struct {
		Type   string          `json:"type"`
		Prompt json.RawMessage `json:"prompt"`
	} `json:"attachment"`
}

// midTurn is how Claude Code hands over a message the person sent while it worked: as text
// beside a tool's result. What follows the marker, up to Claude Code's own note, is theirs.
const midTurn = "The user sent a new message while you were working:"

func midTurnText(content json.RawMessage) string {
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(content, &blocks) != nil {
		return ""
	}
	for _, b := range blocks {
		if b.Type != "text" {
			continue
		}
		i := strings.Index(b.Text, midTurn)
		if i < 0 {
			continue
		}
		t := b.Text[i+len(midTurn):]
		if j := strings.Index(t, "\n\nThis is how Claude Code surfaces"); j >= 0 {
			t = t[:j]
		}
		return strings.TrimSpace(harnessNotes.ReplaceAllString(t, ""))
	}
	return ""
}

// promptText is what the person wrote in a user message, or "" when the message is not a
// prompt (a tool result, or only Claude Code's own notes).
func promptText(content json.RawMessage) string {
	var s string
	if json.Unmarshal(content, &s) == nil {
		if strings.Contains(s, systemNote) {
			return ""
		}
		return strings.TrimSpace(harnessNotes.ReplaceAllString(s, ""))
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(content, &blocks) != nil {
		return ""
	}
	var parts []string
	images := 0
	for _, b := range blocks {
		switch b.Type {
		case "tool_result":
			return ""
		case "text":
			if strings.Contains(b.Text, systemNote) {
				return ""
			}
			if t := strings.TrimSpace(harnessNotes.ReplaceAllString(b.Text, "")); t != "" {
				parts = append(parts, t)
			}
		case "image":
			images++
		}
	}
	text := strings.Join(parts, "\n\n")
	if images > 0 && text != "" {
		text += "\n\n" + strings.Repeat("[image] ", images)
	}
	return strings.TrimSpace(text)
}

// Import brings a conversation Claude Code holds in its transcript into Orla as a session: each
// prompt becomes a run with what Claude Code said and did, at the times it happened. The
// session goes on as a fork, so the conversation, which may still be open in a terminal, is
// never written to by two CLIs at once.
func (s *Service) Import(ctx context.Context, native, title string) (store.Session, error) {
	path := cli.Transcript(native)
	if path == "" {
		return store.Session{}, ErrNoTranscript
	}
	f, err := os.Open(path)
	if err != nil {
		return store.Session{}, err
	}
	defer f.Close()

	sess := store.Session{ID: id.New("ses"), Agent: "claude", Created: time.Now().UTC(), Title: title, ForkOf: native}
	parser := cli.NewLineParser()
	var run *store.Run
	var last time.Time
	publish := func(typ string, data any, at time.Time) {
		raw, _ := json.Marshal(data)
		if _, err := s.bus.Publish(ctx, events.Event{
			Session: sess.ID, Run: run.ID, Time: at,
			Source: events.Source{Engine: events.EngineCLI, Agent: sess.Agent, NativeSession: native},
			Type:   typ, Data: raw,
		}); err != nil {
			s.log.Error("importing an event", "session", sess.ID, "err", err)
		}
	}
	finish := func() {
		if run == nil {
			return
		}
		publish(events.RunFinished, map[string]any{"status": store.RunDone}, last)
		if err := s.store.FinishRun(ctx, run.ID, store.RunDone, last); err != nil {
			s.log.Error("importing a run", "run", run.ID, "err", err)
		}
		run = nil
	}

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
	created := false
	for sc.Scan() {
		var l transcriptLine
		if json.Unmarshal(sc.Bytes(), &l) != nil || l.IsSidechain {
			continue
		}
		queued := l.Type == "attachment" && l.Attachment.Type == "queued_command"
		if l.Type != "user" && l.Type != "assistant" && !queued {
			continue
		}
		if !created {
			if l.Cwd == "" {
				continue
			}
			sess.Cwd = l.Cwd
			if err := s.store.CreateSession(ctx, sess); err != nil {
				return store.Session{}, err
			}
			created = true
		}
		at := l.Timestamp
		if at.IsZero() {
			at = last
		}
		if queued {
			if text := promptText(l.Attachment.Prompt); text != "" && created {
				finish()
				run = &store.Run{ID: id.New("run"), Session: sess.ID, Prompt: text, Status: store.RunRunning, Started: at}
				if err := s.store.CreateRun(ctx, *run); err != nil {
					return store.Session{}, err
				}
				publish(events.RunStarted, map[string]any{"prompt": text, "cwd": sess.Cwd, "resumes": native}, at)
				last = at
			}
			continue
		}
		if l.Type == "user" && !l.IsMeta {
			if text := promptText(l.Message.Content); text != "" {
				finish()
				run = &store.Run{ID: id.New("run"), Session: sess.ID, Prompt: text, Status: store.RunRunning, Started: at}
				if err := s.store.CreateRun(ctx, *run); err != nil {
					return store.Session{}, err
				}
				publish(events.RunStarted, map[string]any{"prompt": text, "cwd": sess.Cwd, "resumes": native}, at)
				last = at
				continue
			}
		}
		if run == nil {
			continue
		}
		for _, out := range parser.Line(sc.Bytes()) {
			publish(out.Type, out.Data, at)
		}
		last = at
		// A message sent mid-turn comes with a tool's result: it starts a run of its own there,
		// so it reads where it was sent.
		if l.Type == "user" {
			if text := midTurnText(l.Message.Content); text != "" {
				finish()
				run = &store.Run{ID: id.New("run"), Session: sess.ID, Prompt: text, Status: store.RunRunning, Started: at}
				if err := s.store.CreateRun(ctx, *run); err != nil {
					return store.Session{}, err
				}
				publish(events.RunStarted, map[string]any{"prompt": text, "cwd": sess.Cwd, "resumes": native}, at)
			}
		}
	}
	if err := sc.Err(); err != nil {
		return store.Session{}, err
	}
	if !created {
		return store.Session{}, ErrNoTranscript
	}
	finish()
	return sess, nil
}
