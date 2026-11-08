package automation

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/byjuliocesa/orla/internal/events"
)

func write(t *testing.T, home, name, front, script string) Automation {
	t.Helper()
	dir := filepath.Join(Root(home), name)
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "AUTOMATION.md"), []byte("---\nname: "+name+"\n"+front+"---\nWhat it does.\n"), 0o644)
	if script != "" {
		os.WriteFile(filepath.Join(dir, "run.sh"), []byte(script), 0o755)
	}
	a, ok := Find(home, name)
	if !ok {
		t.Fatalf("%s did not read", name)
	}
	return a
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatalf("never: %s", what)
}

func status(m *Manager, name, session string) Status {
	for _, s := range m.Statuses() {
		if s.Name == name && s.Session == session {
			return s
		}
	}
	return Status{}
}

func said(m *Manager, name, session, text string) bool {
	for _, l := range m.Lines(name, session, 0) {
		if strings.Contains(l.Text, text) {
			return true
		}
	}
	return false
}

func TestReadAnAutomation(t *testing.T) {
	home := t.TempDir()
	a := write(t, home, "watch", "run: python3 watch.py\nscope: global   # runs on its own\ntrigger: every 5m\nmetadata:\n  title: Watch\n", "")
	if a.Title != "Watch" || a.Scope != "global" || a.Trigger.Kind != "every" || a.Trigger.Every != 5*time.Minute || a.Problem != "" || a.Restart != "on-failure" {
		t.Fatalf("read %+v", a)
	}
	bad := write(t, home, "bad", "run: x\ntrigger: sometimes\n", "")
	if bad.Problem == "" {
		t.Fatal("a trigger that means nothing went unnoticed")
	}
	if !(Trigger{Kind: "on", On: "run.*"}).Matches("run.finished") || (Trigger{Kind: "on", On: "run.finished"}).Matches("run.started") {
		t.Fatal("event matching")
	}
}

func TestAlwaysRestartsOnFailureAndStops(t *testing.T) {
	home := t.TempDir()
	m := &Manager{URL: "http://orla", Token: "tok", Logs: t.TempDir()}
	defer m.Close()
	// Fails once (no marker yet), then stays up, printing what Orla gave it.
	a := write(t, home, "up", "run: sh run.sh\nscope: session\n", "if [ ! -f marker ]; then touch marker; echo first >&2; exit 3; fi\necho \"session=$ORLA_SESSION url=$ORLA_URL\"\nexec sleep 60\n")
	if err := m.Start(a, "", ""); err == nil {
		t.Fatal("a session automation started with no session")
	}
	if err := m.Start(a, "ses_1", "/tmp"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "it restarted and runs", func() bool {
		s := status(m, "up", "ses_1")
		return s.State == "running" && s.Runs == 2 && said(m, "up", "ses_1", "session=ses_1 url=http://orla")
	})
	if !said(m, "up", "ses_1", "first") || !said(m, "up", "ses_1", "exit code 3") {
		t.Fatalf("log = %+v", m.Lines("up", "ses_1", 0))
	}
	if !m.Stop("up", "ses_1") || len(m.Statuses()) != 0 {
		t.Fatal("did not stop")
	}
	if b, _ := os.ReadFile(filepath.Join(m.Logs, "up.ses_1.log")); !strings.Contains(string(b), "first") {
		t.Fatalf("log file = %q", b)
	}
	// Off, it still shows what it printed when it ran.
	if !said(m, "up", "ses_1", "first") {
		t.Fatal("an automation turned off lost its log")
	}
	// Started again (as after Orla restarts), it shows what it printed before.
	m.Start(a, "ses_1", "/tmp")
	if !said(m, "up", "ses_1", "first") {
		t.Fatalf("the earlier log is gone: %+v", m.Lines("up", "ses_1", 0))
	}
}

type memLog struct{ n int64 }

func (l *memLog) Append(context.Context, events.Event) (int64, error) { l.n++; return l.n, nil }
func (l *memLog) After(context.Context, string, int64, int) ([]events.Event, error) {
	return nil, nil
}

func TestOnRunsForItsEventInItsSession(t *testing.T) {
	home := t.TempDir()
	st := &memLog{}
	bus := events.NewBus(st)
	m := &Manager{Bus: bus}
	defer m.Close()
	a := write(t, home, "on", "run: echo \"got $ORLA_EVENT\"\ntrigger: on approval.requested\n", "")
	if err := m.Start(a, "ses_1", ""); err != nil {
		t.Fatal(err)
	}
	eventually(t, "it waits", func() bool { return status(m, "on", "ses_1").State == "waiting" })
	time.Sleep(50 * time.Millisecond)
	ctx := context.Background()
	bus.Publish(ctx, events.Event{Session: "ses_2", Type: "approval.requested"})
	bus.Publish(ctx, events.Event{Session: "ses_1", Type: "run.started"})
	bus.Publish(ctx, events.Event{Session: "ses_1", Type: "approval.requested"})
	defer func() {
		if t.Failed() {
			t.Logf("status %+v lines %+v", status(m, "on", "ses_1"), m.Lines("on", "ses_1", 0))
		}
	}()
	eventually(t, "it ran once, for its own session's event", func() bool {
		return status(m, "on", "ses_1").Runs == 1 && said(m, "on", "ses_1", `"type":"approval.requested"`)
	})
	time.Sleep(200 * time.Millisecond)
	if n := status(m, "on", "ses_1").Runs; n != 1 {
		t.Fatalf("runs = %d", n)
	}
}
