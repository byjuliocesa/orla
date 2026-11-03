package session

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/byjuliocesa/orla/internal/engine/cli"
	"github.com/byjuliocesa/orla/internal/events"
	"github.com/byjuliocesa/orla/internal/store"
)

// fakeRunner stands in for a CLI: it reports a native session, emits one tool call and
// finishes, or waits for cancellation when block is set.
type fakeRunner struct {
	block   bool
	started chan struct{}
	mu      sync.Mutex
	specs   []cli.Spec
}

func (f *fakeRunner) Run(ctx context.Context, spec cli.Spec, emit func(cli.Out), native func(string)) cli.Outcome {
	f.mu.Lock()
	f.specs = append(f.specs, spec)
	f.mu.Unlock()

	native("native-1")
	emit(cli.Out{Type: events.ToolCall, Data: map[string]any{"name": "Bash"}})
	if f.started != nil {
		f.started <- struct{}{}
	}
	if f.block {
		<-ctx.Done()
		return cli.Outcome{NativeSession: "native-1", Status: store.RunCancelled}
	}
	return cli.Outcome{NativeSession: "native-1", Status: store.RunDone, Summary: "ok"}
}

type fixture struct {
	store *store.Store
	bus   *events.Bus
	svc   *Service
	path  string
}

func setup(t *testing.T, runner Runner) fixture {
	t.Helper()
	path := filepath.Join(t.TempDir(), "orla.db")
	return reopen(t, path, runner)
}

func reopen(t *testing.T, path string, runner Runner) fixture {
	t.Helper()
	st, err := store.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	bus := events.NewBus(st)
	svc := NewService(slog.New(slog.NewTextHandler(io.Discard, nil)), st, bus, map[string]Runner{"claude": runner})
	t.Cleanup(svc.Close)
	return fixture{store: st, bus: bus, svc: svc, path: path}
}

// waitFinished reads the session's events until a run.finished and returns them all.
func waitFinished(t *testing.T, bus *events.Bus, session string, runs int) []events.Event {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var got []events.Event
	for e := range bus.Subscribe(ctx, session, 0) {
		got = append(got, e)
		if e.Type == events.RunFinished {
			if runs--; runs == 0 {
				return got
			}
		}
	}
	t.Fatalf("no run.finished; got %d events", len(got))
	return nil
}

func status(t *testing.T, e events.Event) string {
	var d struct{ Status string }
	_ = json.Unmarshal(e.Data, &d)
	return d.Status
}

func TestSecondRunResumesTheCLIConversation(t *testing.T) {
	runner := &fakeRunner{}
	f := setup(t, runner)
	ctx := context.Background()
	sess, err := f.svc.Create(ctx, "claude", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	if _, err := f.svc.Start(ctx, sess.ID, RunOptions{Prompt: "one"}); err != nil {
		t.Fatal(err)
	}
	waitFinished(t, f.bus, sess.ID, 1)
	if _, err := f.svc.Start(ctx, sess.ID, RunOptions{Prompt: "two"}); err != nil {
		t.Fatal(err)
	}
	got := waitFinished(t, f.bus, sess.ID, 2)

	if runner.specs[0].Resume != "" || runner.specs[1].Resume != "native-1" {
		t.Fatalf("resume ids = %q, %q", runner.specs[0].Resume, runner.specs[1].Resume)
	}
	var types []string
	for _, e := range got {
		types = append(types, e.Type)
	}
	want := []string{events.RunStarted, events.ToolCall, events.RunFinished,
		events.RunStarted, events.ToolCall, events.RunFinished}
	if len(types) != len(want) {
		t.Fatalf("events = %v", types)
	}
	for i := range want {
		if types[i] != want[i] {
			t.Fatalf("events = %v", types)
		}
	}
	runs, _ := f.store.Runs(ctx, sess.ID)
	if len(runs) != 2 || runs[0].Status != store.RunDone || runs[1].Finished == nil {
		t.Fatalf("runs = %+v", runs)
	}
}

func TestOnlyOneRunAtATimePerSession(t *testing.T) {
	runner := &fakeRunner{block: true, started: make(chan struct{}, 1)}
	f := setup(t, runner)
	ctx := context.Background()
	sess, _ := f.svc.Create(ctx, "claude", t.TempDir())

	var ok, busy atomic.Int32
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			_, err := f.svc.Start(ctx, sess.ID, RunOptions{Prompt: "x"})
			switch err {
			case nil:
				ok.Add(1)
			case ErrBusy:
				busy.Add(1)
			default:
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if ok.Load() != 1 || busy.Load() != 7 {
		t.Fatalf("started %d, refused %d; want 1 and 7", ok.Load(), busy.Load())
	}

	<-runner.started
	if err := f.svc.Cancel(sess.ID); err != nil {
		t.Fatal(err)
	}
	got := waitFinished(t, f.bus, sess.ID, 1)
	if s := status(t, got[len(got)-1]); s != store.RunCancelled {
		t.Fatalf("finished as %q", s)
	}

	// Free again once the run has ended.
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, busy := f.svc.Busy(sess.ID); !busy {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("session still busy after its run finished")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Shutting down mid-run must still record how the run ended.
func TestShutdownRecordsTheRunAsCancelled(t *testing.T) {
	runner := &fakeRunner{block: true, started: make(chan struct{}, 1)}
	f := setup(t, runner)
	ctx := context.Background()
	sess, _ := f.svc.Create(ctx, "claude", t.TempDir())
	run, _ := f.svc.Start(ctx, sess.ID, RunOptions{Prompt: "x"})
	<-runner.started

	f.svc.Close()

	runs, _ := f.store.Runs(ctx, sess.ID)
	if runs[0].ID != run.ID || runs[0].Status != store.RunCancelled {
		t.Fatalf("run after shutdown = %+v", runs[0])
	}
	stored, _ := f.store.After(ctx, sess.ID, 0, 100)
	if last := stored[len(stored)-1]; last.Type != events.RunFinished {
		t.Fatalf("last stored event = %s", last.Type)
	}
}

// A run whose process died with the server is closed on the next start, with the event clients
// wait for.
func TestRecoverClosesRunsLeftByACrash(t *testing.T) {
	f := setup(t, &fakeRunner{})
	ctx := context.Background()
	sess, _ := f.svc.Create(ctx, "claude", t.TempDir())
	// What a crash leaves: a run marked running, and no process behind it.
	if err := f.store.CreateRun(ctx, store.Run{
		ID: "run_crashed", Session: sess.ID, Prompt: "x", Status: store.RunRunning, Started: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	g := reopen(t, f.path, &fakeRunner{})
	if err := g.svc.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	runs, _ := g.store.Runs(ctx, sess.ID)
	if runs[0].Status != store.RunFailed {
		t.Fatalf("crashed run = %+v", runs[0])
	}
	stored, _ := g.store.After(ctx, sess.ID, 0, 100)
	if len(stored) != 1 || stored[0].Type != events.RunFinished || stored[0].Run != "run_crashed" {
		t.Fatalf("events = %+v", stored)
	}
}

func TestUnknownAgentIsRefused(t *testing.T) {
	f := setup(t, &fakeRunner{})
	if _, err := f.svc.Create(context.Background(), "gpt-cli", t.TempDir()); err == nil {
		t.Fatal("created a session for an agent Orla cannot run")
	}
}
