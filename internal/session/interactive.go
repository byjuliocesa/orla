package session

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"time"

	"github.com/byjuliocesa/orla/internal/engine/cli"
	"github.com/byjuliocesa/orla/internal/events"
	"github.com/byjuliocesa/orla/internal/id"
	"github.com/byjuliocesa/orla/internal/store"
)

// Interactive sessions are Claude Code conversations typed into an Orla terminal. Orla does not
// drive them; it learns what happens from the CLI's hooks and records it as the same events a
// headless run produces. Each prompt becomes a run.

// HandleClaudeHook records one hook event from Claude Code running in terminal.
func (s *Service) HandleClaudeHook(ctx context.Context, terminal string, h cli.Hook) error {
	if h.SessionID == "" || h.Event == "" {
		return errors.New("hook without session_id or hook_event_name")
	}
	// Hooks of one conversation can arrive at once (parallel tool calls); runs and sessions are
	// created by the first that needs them, so the whole handling is serialized.
	s.hookMu.Lock()
	defer s.hookMu.Unlock()

	sess, err := s.interactiveSession(ctx, terminal, h)
	if err != nil {
		return err
	}

	switch h.Event {
	case cli.HookSessionStart:
		s.publish(ctx, sess, "", events.Status, map[string]any{"state": "idle", "source": h.Source})

	case cli.HookUserPromptSubmit:
		// A prompt sent while another is in progress means the earlier one was interrupted:
		// Claude Code sends no Stop for it.
		if err := s.finishRunning(ctx, sess, store.RunCancelled, "interrupted by a new prompt"); err != nil {
			return err
		}
		run, err := s.interactiveRun(ctx, sess, h)
		if err != nil {
			return err
		}
		s.publish(ctx, sess, run.ID, events.Status, map[string]any{"state": "working"})

	case cli.HookPreToolUse, cli.HookPostToolUse, cli.HookPostToolUseFailure:
		run, err := s.interactiveRun(ctx, sess, h)
		if err != nil {
			return err
		}
		if h.Event == cli.HookPreToolUse {
			s.calls[sess.ID] = append(s.calls[sess.ID], openCall{id: h.ToolUseID, name: h.ToolName, input: h.ToolInput})
		} else {
			s.closeCall(ctx, sess, h.ToolUseID)
		}
		for _, out := range cli.HookOuts(h) {
			s.publish(ctx, sess, run.ID, out.Type, out.Data)
		}

	case cli.HookNotification:
		run, _ := s.store.RunByNativePrompt(ctx, sess.ID, h.PromptID)
		s.publish(ctx, sess, run.ID, events.Status, map[string]any{"state": "waiting", "message": h.Message})

	case cli.HookStop:
		run, err := s.interactiveRun(ctx, sess, h)
		if err != nil {
			return err
		}
		if run.Status != store.RunRunning || s.stopping[run.ID] {
			return nil // already ended, by an interruption or a repeated Stop
		}
		if h.LastMessage != "" {
			s.publish(ctx, sess, run.ID, events.MessageAssistant, map[string]any{"text": h.LastMessage, "final": true})
		}
		// The transcript lags Stop by the last response, so its usage is read once that is in,
		// off the hook: Claude Code waits for the hook before it goes on.
		s.stopping[run.ID] = true
		s.wg.Go(func() { s.finishFromTranscript(context.WithoutCancel(ctx), sess, run.ID, h) })

	case cli.HookSessionEnd:
		if err := s.finishRunning(ctx, sess, store.RunCancelled, "the CLI session ended"); err != nil {
			return err
		}
		s.publish(ctx, sess, "", events.Status, map[string]any{"state": "ended", "reason": h.Reason})
	}
	return nil
}

// TerminalClosed ends the runs of a terminal that is gone. Its CLI went with it, without a
// SessionEnd if it was killed.
func (s *Service) TerminalClosed(ctx context.Context, terminal string) error {
	s.hookMu.Lock()
	defer s.hookMu.Unlock()
	sessions, err := s.store.SessionsOfTerminal(ctx, terminal)
	if err != nil {
		return err
	}
	for _, sess := range sessions {
		if err := s.finishRunning(ctx, sess, store.RunCancelled, "the terminal closed"); err != nil {
			return err
		}
	}
	return nil
}

// AskInteractive forwards a permission prompt of Claude Code in an Orla terminal to the owner.
// The terminal shows the same prompt meanwhile and the first answer counts: once it is answered
// at the keyboard, Claude Code ignores the hook. ok is false when there is no answer for the
// CLI: the prompt was answered in the terminal, its run ended or nobody answered in time.
func (s *Service) AskInteractive(ctx context.Context, terminal string, h cli.Hook) (d Decision, ok bool) {
	if h.SessionID == "" {
		return Decision{}, false
	}
	record := context.WithoutCancel(ctx)
	s.hookMu.Lock()
	sess, err := s.interactiveSession(ctx, terminal, h)
	var run store.Run
	if err == nil {
		run, err = s.interactiveRun(ctx, sess, h)
	}
	if err != nil || run.Status != store.RunRunning || s.stopping[run.ID] {
		s.hookMu.Unlock()
		if err != nil {
			s.log.Error("interactive approval", "terminal", terminal, "err", err)
		}
		return Decision{}, false
	}
	p := &pending{sess: sess, run: run.ID, callID: s.callFor(sess.ID, h), terminal: terminal, ended: make(chan ending, 1)}
	// Opened under hookMu, so the run cannot end between the check above and this.
	approvalID := s.openApproval(record, p, h.ToolName, h.ToolInput)
	s.hookMu.Unlock()

	var e ending
	select {
	case e = <-p.ended:
	case <-time.After(s.approvalWait):
		s.endApproval(record, approvalID, nil, withdrawal("timeout", "no answer in time; the terminal still asks"))
		e = <-p.ended // this ending, or one that came first
	case <-ctx.Done():
		// Claude Code stops the hook when the prompt is answered No or interrupted in the
		// terminal, and when it exits.
		s.endApproval(record, approvalID, nil, withdrawal("cancel", "Claude Code stopped asking"))
		e = <-p.ended
	}
	if e.fromOwner && !e.Allow && p.callID != "" {
		// Claude Code reports no result for a call a hook denied; the timeline gets the one a
		// headless run shows.
		s.publish(record, sess, run.ID, events.ToolResult, map[string]any{
			"call_id": p.callID, "ok": false, "output": cli.DenialMessage(e.Message),
		})
	}
	return e.Decision, e.fromOwner
}

// openCall is a tool call of an interactive run that has not reported its result.
type openCall struct {
	id    string
	name  string
	input json.RawMessage
}

// callFor finds the tool call a permission prompt is about. The prompt carries no call id, but
// it comes right after the call's PreToolUse, with the same tool and input.
func (s *Service) callFor(session string, h cli.Hook) string {
	var input any
	_ = json.Unmarshal(h.ToolInput, &input)
	calls := s.calls[session]
	for i := len(calls) - 1; i >= 0; i-- {
		var other any
		_ = json.Unmarshal(calls[i].input, &other)
		if calls[i].name == h.ToolName && reflect.DeepEqual(input, other) {
			return calls[i].id
		}
	}
	return ""
}

// closeCall forgets a call that reported its result. A result means the call ran, so a prompt
// still pending for it was answered yes in the terminal.
func (s *Service) closeCall(ctx context.Context, sess store.Session, callID string) {
	if callID == "" {
		return
	}
	s.calls[sess.ID] = slices.DeleteFunc(s.calls[sess.ID], func(c openCall) bool { return c.id == callID })
	ofCall := func(p *pending) bool { return p.sess.ID == sess.ID && p.callID == callID }
	s.endApprovalsWhere(ctx, ofCall, ending{Decision: Decision{Allow: true, By: "terminal"}})
}

// interactiveSession finds the Orla session of a CLI conversation, creating it on first sight.
// A conversation resumed in another terminal moves there.
func (s *Service) interactiveSession(ctx context.Context, terminal string, h cli.Hook) (store.Session, error) {
	sess, err := s.store.SessionByNative(ctx, "claude", h.SessionID)
	if err == nil {
		if sess.Terminal != terminal {
			if err := s.store.SetSessionTerminal(ctx, sess.ID, terminal); err != nil {
				return sess, err
			}
			sess.Terminal = terminal
		}
		return sess, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return sess, err
	}
	sess = store.Session{
		ID: id.New("ses"), Agent: "claude", Cwd: h.Cwd, NativeSession: h.SessionID,
		Terminal: terminal, Created: time.Now().UTC(),
	}
	return sess, s.store.CreateSession(ctx, sess)
}

// interactiveRun finds the run of the hook's prompt, creating it when the prompt is new. Hooks
// installed in the middle of a conversation see tool events before any prompt; those still get
// a run, with an unknown prompt.
func (s *Service) interactiveRun(ctx context.Context, sess store.Session, h cli.Hook) (store.Run, error) {
	run, err := s.store.RunByNativePrompt(ctx, sess.ID, h.PromptID)
	if err == nil || !errors.Is(err, store.ErrNotFound) {
		return run, err
	}
	run = store.Run{
		ID: id.New("run"), Session: sess.ID, Prompt: h.Prompt, Status: store.RunRunning,
		NativePrompt: h.PromptID, Started: time.Now().UTC(),
	}
	if err := s.store.CreateRun(ctx, run); err != nil {
		return run, err
	}
	s.publish(ctx, sess, run.ID, events.RunStarted, map[string]any{
		"prompt": h.Prompt, "cwd": h.Cwd, "permission_mode": h.PermissionMode,
		"interactive": true, "terminal": sess.Terminal,
	})
	return run, nil
}

// transcriptWait bounds how long a finished prompt waits for its last response to reach the
// transcript before it is closed without usage.
const transcriptWait = 3 * time.Second

func (s *Service) finishFromTranscript(ctx context.Context, sess store.Session, run string, h cli.Hook) {
	var usage map[string]any
	deadline := time.Now().Add(transcriptWait)
	for h.TranscriptPath != "" {
		u, complete, err := cli.TranscriptUsage(h.TranscriptPath, h.PromptID, h.LastMessage)
		if err == nil {
			usage = u
		}
		if complete || time.Now().After(deadline) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	s.hookMu.Lock()
	defer s.hookMu.Unlock()
	delete(s.stopping, run)
	if usage != nil {
		s.publish(ctx, sess, run, events.Usage, usage)
	}
	if err := s.finishRun(ctx, sess, run, store.RunDone, ""); err != nil {
		s.log.Error("finishing interactive run", "run", run, "err", err)
	}
	s.publish(ctx, sess, "", events.Status, map[string]any{"state": "idle"})
}

// finishRunning ends a session's runs still in progress. Runs past their Stop are left to finish
// as done.
func (s *Service) finishRunning(ctx context.Context, sess store.Session, status, summary string) error {
	runs, err := s.store.RunningRunsOf(ctx, sess.ID)
	if err != nil {
		return err
	}
	for _, r := range runs {
		if s.stopping[r.ID] {
			continue
		}
		if err := s.finishRun(ctx, sess, r.ID, status, summary); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) finishRun(ctx context.Context, sess store.Session, run, status, summary string) error {
	// Claude Code normally stops a pending hook before the prompt ends; any request left ends
	// with it.
	ofRun := func(p *pending) bool { return p.run == run }
	s.endApprovalsWhere(ctx, ofRun, withdrawal("run", "the prompt ended"))
	delete(s.calls, sess.ID)
	s.publish(ctx, sess, run, events.RunFinished, map[string]any{"status": status, "summary": summary})
	return s.store.FinishRun(ctx, run, status, time.Now())
}
