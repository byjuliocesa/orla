package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Julio0Cesar/orla/internal/engine/cli"
	"github.com/Julio0Cesar/orla/internal/events"
	"github.com/Julio0Cesar/orla/internal/store"
)

func hookIn(t *testing.T, f fixture, terminal string, h cli.Hook) {
	t.Helper()
	if h.SessionID == "" {
		h.SessionID = "native-a"
	}
	if h.Cwd == "" {
		h.Cwd = "/work"
	}
	if err := f.svc.HandleClaudeHook(context.Background(), terminal, h); err != nil {
		t.Fatal(err)
	}
}

func storedTypes(t *testing.T, f fixture, session string) []string {
	t.Helper()
	all, err := f.store.After(context.Background(), session, 0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	var types []string
	for _, e := range all {
		types = append(types, e.Type)
	}
	return types
}

func onlySession(t *testing.T, f fixture) store.Session {
	t.Helper()
	all, _ := f.store.Sessions(context.Background())
	if len(all) != 1 {
		t.Fatalf("sessions = %+v", all)
	}
	return all[0]
}

func TestHooksBecomeARun(t *testing.T) {
	f := setup(t, &fakeRunner{})
	hookIn(t, f, "term_1", cli.Hook{Event: cli.HookSessionStart, Source: "startup"})
	hookIn(t, f, "term_1", cli.Hook{Event: cli.HookUserPromptSubmit, PromptID: "p1", Prompt: "list files"})
	hookIn(t, f, "term_1", cli.Hook{Event: cli.HookPreToolUse, PromptID: "p1", ToolName: "Bash", ToolUseID: "t1",
		ToolInput: json.RawMessage(`{"command":"ls"}`)})
	hookIn(t, f, "term_1", cli.Hook{Event: cli.HookPostToolUse, PromptID: "p1", ToolName: "Bash", ToolUseID: "t1",
		ToolResponse: json.RawMessage(`{"stdout":"a.txt","stderr":""}`)})
	hookIn(t, f, "term_1", cli.Hook{Event: cli.HookPostToolUseFailure, PromptID: "p1", ToolName: "Bash",
		ToolUseID: "t2", Error: "Exit code 1"})
	hookIn(t, f, "term_1", cli.Hook{Event: cli.HookStop, PromptID: "p1", LastMessage: "there is a.txt"})
	// A repeated Stop must not finish the run twice.
	hookIn(t, f, "term_1", cli.Hook{Event: cli.HookStop, PromptID: "p1", LastMessage: "there is a.txt"})

	sess := onlySession(t, f)
	waitFinished(t, f.bus, sess.ID, 1)
	time.Sleep(100 * time.Millisecond) // the idle status follows run.finished
	if sess.Terminal != "term_1" || sess.NativeSession != "native-a" {
		t.Fatalf("session = %+v", sess)
	}
	want := []string{
		events.Status, events.RunStarted, events.Status, events.ToolCall, events.ToolResult,
		events.ToolResult, events.MessageAssistant, events.RunFinished, events.Status,
	}
	got := storedTypes(t, f, sess.ID)
	if len(got) != len(want) {
		t.Fatalf("events\n got: %v\nwant: %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("events\n got: %v\nwant: %v", got, want)
		}
	}
	runs, _ := f.store.Runs(context.Background(), sess.ID)
	if len(runs) != 1 || runs[0].Status != store.RunDone || runs[0].Prompt != "list files" {
		t.Fatalf("runs = %+v", runs)
	}
}

// Esc in Claude Code interrupts a prompt without a Stop; the next prompt closes it.
func TestNewPromptClosesAnInterruptedOne(t *testing.T) {
	f := setup(t, &fakeRunner{})
	hookIn(t, f, "term_1", cli.Hook{Event: cli.HookUserPromptSubmit, PromptID: "p1", Prompt: "one"})
	hookIn(t, f, "term_1", cli.Hook{Event: cli.HookUserPromptSubmit, PromptID: "p2", Prompt: "two"})

	runs, _ := f.store.Runs(context.Background(), onlySession(t, f).ID)
	if len(runs) != 2 || runs[0].Status != store.RunCancelled || runs[1].Status != store.RunRunning {
		t.Fatalf("runs = %+v", runs)
	}
}

func TestClosedTerminalEndsItsRuns(t *testing.T) {
	f := setup(t, &fakeRunner{})
	hookIn(t, f, "term_1", cli.Hook{Event: cli.HookUserPromptSubmit, PromptID: "p1", Prompt: "one"})
	hookIn(t, f, "term_2", cli.Hook{Event: cli.HookUserPromptSubmit, SessionID: "native-b", PromptID: "q1", Prompt: "other"})

	if err := f.svc.TerminalClosed(context.Background(), "term_1"); err != nil {
		t.Fatal(err)
	}
	all, _ := f.store.Sessions(context.Background())
	for _, sess := range all {
		runs, _ := f.store.Runs(context.Background(), sess.ID)
		want := store.RunRunning
		if sess.Terminal == "term_1" {
			want = store.RunCancelled
		}
		if runs[0].Status != want {
			t.Errorf("%s run = %s, want %s", sess.Terminal, runs[0].Status, want)
		}
	}
}

// A conversation being typed in a terminal cannot be resumed headless at the same time: two
// writers would fork it.
func TestHeadlessRunWaitsForInteractiveOne(t *testing.T) {
	f := setup(t, &fakeRunner{})
	hookIn(t, f, "term_1", cli.Hook{Event: cli.HookUserPromptSubmit, PromptID: "p1", Prompt: "one"})
	sess := onlySession(t, f)

	if _, err := f.svc.Start(context.Background(), sess.ID, RunOptions{Prompt: "x"}); err != ErrBusy {
		t.Fatalf("start during interactive run = %v, want ErrBusy", err)
	}
	hookIn(t, f, "term_1", cli.Hook{Event: cli.HookStop, PromptID: "p1"})
	waitFinished(t, f.bus, sess.ID, 1)
	if _, err := f.svc.Start(context.Background(), sess.ID, RunOptions{Prompt: "x"}); err != nil {
		t.Fatalf("start after interactive run = %v", err)
	}
}

// Typing the next prompt while the last response is still reaching the transcript must not turn
// a finished prompt into a cancelled one.
func TestPromptRightAfterStopKeepsItDone(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	transcript := filepath.Join(home, ".claude", "projects", "-w", "c.jsonl")
	_ = os.MkdirAll(filepath.Dir(transcript), 0o700)
	// The final response is not in yet, so the run waits for it.
	_ = os.WriteFile(transcript, []byte(`{"type":"user","promptId":"p1","message":{"content":"one"}}`+"\n"), 0o600)

	f := setup(t, &fakeRunner{})
	hookIn(t, f, "term_1", cli.Hook{Event: cli.HookUserPromptSubmit, PromptID: "p1", Prompt: "one"})
	hookIn(t, f, "term_1", cli.Hook{Event: cli.HookStop, PromptID: "p1", TranscriptPath: transcript, LastMessage: "reply"})
	hookIn(t, f, "term_1", cli.Hook{Event: cli.HookUserPromptSubmit, PromptID: "p2", Prompt: "two"})

	sess := onlySession(t, f)
	waitFinished(t, f.bus, sess.ID, 1)
	runs, _ := f.store.Runs(context.Background(), sess.ID)
	if runs[0].Status != store.RunDone || runs[1].Status != store.RunRunning {
		t.Fatalf("runs = %s, %s; want done, running", runs[0].Status, runs[1].Status)
	}
}
