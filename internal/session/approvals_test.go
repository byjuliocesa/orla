package session

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/byjuliocesa/orla/internal/engine/cli"
	"github.com/byjuliocesa/orla/internal/events"
	"github.com/byjuliocesa/orla/internal/store"
)

// askingRunner stands in for a CLI that needs permission once: it asks, reports the answer
// and finishes.
type askingRunner struct {
	svc       *Service
	decisions chan Decision
}

func (a *askingRunner) Run(ctx context.Context, spec cli.Spec, emit func(cli.Out), native func(string)) cli.Outcome {
	d := a.svc.RequestApproval(ctx, spec.Run, "Bash", json.RawMessage(`{"command":"rm -rf build"}`), "toolu_1")
	a.decisions <- d
	return cli.Outcome{Status: store.RunDone}
}

func askingSetup(t *testing.T) (fixture, *askingRunner, store.Session) {
	t.Helper()
	runner := &askingRunner{decisions: make(chan Decision, 1)}
	f := setup(t, runner)
	runner.svc = f.svc
	f.svc.ApprovalsAt("http://test/mcp/runs/")
	sess, _ := f.svc.Create(context.Background(), "claude", t.TempDir())
	return f, runner, sess
}

// nextRequest waits for the session's next approval request and returns its data.
func nextRequest(t *testing.T, f fixture, session string) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for e := range f.bus.Subscribe(ctx, session, 0) {
		if e.Type == events.ApprovalRequested {
			var d map[string]any
			_ = json.Unmarshal(e.Data, &d)
			return d
		}
	}
	t.Fatal("no approval requested")
	return nil
}

func nextApproval(t *testing.T, f fixture, session string) string {
	t.Helper()
	return nextRequest(t, f, session)["approval_id"].(string)
}

func resolvedEvents(t *testing.T, f fixture, session string) []map[string]any {
	t.Helper()
	all, _ := f.store.After(context.Background(), session, 0, 1000)
	var out []map[string]any
	for _, e := range all {
		if e.Type == events.ApprovalResolved {
			var d map[string]any
			_ = json.Unmarshal(e.Data, &d)
			out = append(out, d)
		}
	}
	return out
}

func TestOwnerAnswerReachesTheCLI(t *testing.T) {
	f, runner, sess := askingSetup(t)
	if _, err := f.svc.Start(context.Background(), sess.ID, RunOptions{Prompt: "clean"}); err != nil {
		t.Fatal(err)
	}
	approval := nextApproval(t, f, sess.ID)

	// An answer for another session must not count.
	if err := f.svc.Resolve(context.Background(), "ses_other", approval, Decision{Allow: true}); err != ErrNoApproval {
		t.Fatalf("resolve from another session = %v", err)
	}
	if err := f.svc.Resolve(context.Background(), sess.ID, approval, Decision{Allow: true, By: "cli"}); err != nil {
		t.Fatal(err)
	}
	if d := <-runner.decisions; !d.Allow {
		t.Fatalf("CLI got %+v", d)
	}
	waitFinished(t, f.bus, sess.ID, 1)
	if r := resolvedEvents(t, f, sess.ID); len(r) != 1 || r[0]["allowed"] != true || r[0]["by"] != "cli" {
		t.Fatalf("resolved events = %v", r)
	}
}

// Two devices answering at once: one answer wins, the other is refused, the CLI gets one.
func TestConcurrentAnswersOnlyOneCounts(t *testing.T) {
	f, runner, sess := askingSetup(t)
	_, _ = f.svc.Start(context.Background(), sess.ID, RunOptions{Prompt: "clean"})
	approval := nextApproval(t, f, sess.ID)

	var ok atomic.Int32
	var wg sync.WaitGroup
	for i := range 10 {
		wg.Go(func() {
			if f.svc.Resolve(context.Background(), sess.ID, approval, Decision{Allow: i%2 == 0}) == nil {
				ok.Add(1)
			}
		})
	}
	wg.Wait()
	if ok.Load() != 1 {
		t.Fatalf("%d answers accepted, want 1", ok.Load())
	}
	<-runner.decisions
	waitFinished(t, f.bus, sess.ID, 1)
	if r := resolvedEvents(t, f, sess.ID); len(r) != 1 {
		t.Fatalf("resolved events = %v", r)
	}
}

func TestUnansweredRequestIsDenied(t *testing.T) {
	f, runner, sess := askingSetup(t)
	f.svc.approvalWait = 200 * time.Millisecond
	_, _ = f.svc.Start(context.Background(), sess.ID, RunOptions{Prompt: "clean"})

	select {
	case d := <-runner.decisions:
		if d.Allow || d.By != "timeout" {
			t.Fatalf("CLI got %+v", d)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("request never expired")
	}
	waitFinished(t, f.bus, sess.ID, 1)
	if r := resolvedEvents(t, f, sess.ID); len(r) != 1 || r[0]["by"] != "timeout" {
		t.Fatalf("resolved events = %v", r)
	}
}

func TestCancellingTheRunDeniesItsRequest(t *testing.T) {
	f, runner, sess := askingSetup(t)
	_, _ = f.svc.Start(context.Background(), sess.ID, RunOptions{Prompt: "clean"})
	approval := nextApproval(t, f, sess.ID)

	if err := f.svc.Cancel(sess.ID); err != nil {
		t.Fatal(err)
	}
	if d := <-runner.decisions; d.Allow {
		t.Fatalf("CLI got %+v after cancel", d)
	}
	if err := f.svc.Resolve(context.Background(), sess.ID, approval, Decision{Allow: true}); err != ErrNoApproval {
		t.Fatalf("answer after cancel = %v", err)
	}
}

// The "by" field comes from the client; whatever it says, an answer must reach the CLI.
func TestAnswerReachesTheCLIWhateverItSaysAboutItself(t *testing.T) {
	f, runner, sess := askingSetup(t)
	_, _ = f.svc.Start(context.Background(), sess.ID, RunOptions{Prompt: "clean"})
	approval := nextApproval(t, f, sess.ID)
	_ = f.svc.Resolve(context.Background(), sess.ID, approval, Decision{Allow: true, By: "cancel"})
	select {
	case d := <-runner.decisions:
		if !d.Allow {
			t.Fatalf("CLI got %+v", d)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the run hung after an answer claiming to be a cancel")
	}
}

func TestRequestFromAFinishedRunIsDenied(t *testing.T) {
	f, _, _ := askingSetup(t)
	if d := f.svc.RequestApproval(context.Background(), "run_gone", "Bash", nil, ""); d.Allow {
		t.Fatal("a run that is not in progress got permission")
	}
}
