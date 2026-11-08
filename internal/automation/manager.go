package automation

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/byjuliocesa/orla/internal/childenv"
	"github.com/byjuliocesa/orla/internal/events"
)

// Status is how an automation stands where it runs.
type Status struct {
	Name    string `json:"name"`
	Session string `json:"session,omitempty"`
	// State is running (its command runs now), waiting (for its next time or event),
	// stopped, or failed (it gave up after failing again and again).
	State   string     `json:"state"`
	Since   time.Time  `json:"since"`
	LastRun *time.Time `json:"last_run,omitempty"`
	LastEnd *time.Time `json:"last_end,omitempty"`
	// Exit is the last run's exit code.
	Exit  *int   `json:"exit,omitempty"`
	Runs  int    `json:"runs"`
	Error string `json:"error,omitempty"`
}

// Line is one line an automation printed.
type Line struct {
	N    int64     `json:"n"`
	Time time.Time `json:"time"`
	// Stream is out, err, or orla for what Orla says about the run.
	Stream string `json:"stream"`
	Text   string `json:"text"`
}

const keep = 1000

type instance struct {
	mu     sync.Mutex
	status Status
	lines  []Line
	n      int64
	cancel context.CancelFunc
	done   chan struct{}
	file   *os.File
}

// Manager starts, watches and stops automations. One instance runs per automation and
// session ("" for global).
type Manager struct {
	// Env is what every run gets besides the system's: how to reach Orla.
	URL, Token string
	Bus        *events.Bus
	Logs       string
	Log        *slog.Logger

	mu   sync.Mutex
	runs map[string]*instance
}

func key(name, session string) string { return name + "\x00" + session }

// Start runs the automation where asked, replacing an instance already there. cwd is the
// session's folder, for a session automation.
func (m *Manager) Start(a Automation, session, cwd string) error {
	if a.Problem != "" {
		return errors.New(a.Problem)
	}
	if a.Scope == "session" && session == "" {
		return errors.New("a session automation runs with a session")
	}
	if a.Scope == "global" {
		session = ""
	}
	m.Stop(a.Name, session)
	ctx, cancel := context.WithCancel(context.Background())
	in := &instance{cancel: cancel, done: make(chan struct{})}
	in.status = Status{Name: a.Name, Session: session, State: "waiting", Since: time.Now()}
	if m.Logs != "" {
		_ = os.MkdirAll(m.Logs, 0o700)
		path := m.logPath(a.Name, session)
		// What it printed before Orla restarted is still worth seeing.
		in.lines, in.n = tail(path, 200)
		in.file, _ = os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	}
	m.mu.Lock()
	if m.runs == nil {
		m.runs = map[string]*instance{}
	}
	m.runs[key(a.Name, session)] = in
	m.mu.Unlock()
	go func() {
		defer close(in.done)
		defer func() {
			if in.file != nil {
				in.file.Close()
			}
		}()
		m.loop(ctx, in, a, session, cwd)
	}()
	return nil
}

// Stop ends the automation there, its running command included, and waits for it.
func (m *Manager) Stop(name, session string) bool {
	m.mu.Lock()
	in := m.runs[key(name, session)]
	delete(m.runs, key(name, session))
	m.mu.Unlock()
	if in == nil {
		return false
	}
	in.cancel()
	<-in.done
	return true
}

// StopSession ends every automation running with a session.
func (m *Manager) StopSession(session string) {
	for _, st := range m.Statuses() {
		if st.Session == session && session != "" {
			m.Stop(st.Name, session)
		}
	}
}

// Close stops everything.
func (m *Manager) Close() {
	for _, st := range m.Statuses() {
		m.Stop(st.Name, st.Session)
	}
}

// Statuses says how every running instance stands.
func (m *Manager) Statuses() []Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Status{}
	for _, in := range m.runs {
		in.mu.Lock()
		out = append(out, in.status)
		in.mu.Unlock()
	}
	return out
}

// Lines returns what the instance printed after line n.
func (m *Manager) Lines(name, session string, after int64) []Line {
	m.mu.Lock()
	in := m.runs[key(name, session)]
	m.mu.Unlock()
	if in == nil {
		// Off: what it printed when it last ran, from its file.
		out := []Line{}
		if m.Logs != "" {
			lines, _ := tail(m.logPath(name, session), keep)
			for _, l := range lines {
				if l.N > after {
					out = append(out, l)
				}
			}
		}
		return out
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	out := []Line{}
	for _, l := range in.lines {
		if l.N > after {
			out = append(out, l)
		}
	}
	return out
}

func (in *instance) say(stream, text string) {
	in.mu.Lock()
	defer in.mu.Unlock()
	in.n++
	l := Line{N: in.n, Time: time.Now(), Stream: stream, Text: text}
	in.lines = append(in.lines, l)
	if len(in.lines) > keep {
		in.lines = in.lines[len(in.lines)-keep:]
	}
	if in.file != nil {
		_, _ = in.file.WriteString(l.Time.Format(time.RFC3339) + " " + stream + " " + text + "\n")
	}
}

func (in *instance) set(f func(s *Status)) {
	in.mu.Lock()
	f(&in.status)
	in.mu.Unlock()
}

// loop runs the automation by its trigger until it is stopped.
func (m *Manager) loop(ctx context.Context, in *instance, a Automation, session, cwd string) {
	defer in.set(func(s *Status) {
		if s.State != "failed" {
			s.State, s.Since = "stopped", time.Now()
		}
	})
	switch a.Trigger.Kind {
	case "always":
		var fails []time.Time
		for wait := time.Second; ; {
			code, err := m.run(ctx, in, a, session, cwd, "")
			if ctx.Err() != nil {
				return
			}
			if a.Restart == "never" || (code == 0 && err == nil && a.Restart != "always") {
				in.say("orla", "ended; it is not restarted")
				return
			}
			now := time.Now()
			fails = append(fails, now)
			for len(fails) > 0 && now.Sub(fails[0]) > 2*time.Minute {
				fails = fails[1:]
			}
			if len(fails) >= 5 {
				in.say("orla", "failed 5 times in 2 minutes; gave up")
				in.set(func(s *Status) { s.State, s.Since, s.Error = "failed", time.Now(), "failed 5 times in 2 minutes" })
				return
			}
			in.say("orla", "restarting in "+wait.String())
			in.set(func(s *Status) { s.State, s.Since = "waiting", time.Now() })
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
			wait = min(wait*2, time.Minute)
		}
	case "every":
		t := time.NewTicker(a.Trigger.Every)
		defer t.Stop()
		for {
			_, _ = m.run(ctx, in, a, session, cwd, "")
			if ctx.Err() != nil {
				return
			}
			in.set(func(s *Status) { s.State, s.Since = "waiting", time.Now() })
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	case "on":
		if m.Bus == nil {
			return
		}
		ch := m.Bus.SubscribeAll(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-ch:
				if !ok {
					return
				}
				if !a.Trigger.Matches(ev.Type) || (session != "" && ev.Session != session) {
					continue
				}
				b, _ := json.Marshal(ev)
				_, _ = m.run(ctx, in, a, session, cwd, string(b))
				if ctx.Err() != nil {
					return
				}
				in.set(func(s *Status) { s.State, s.Since = "waiting", time.Now() })
			}
		}
	}
}

// run runs the command once, its output into the log, and says how it ended.
func (m *Manager) run(ctx context.Context, in *instance, a Automation, session, cwd, event string) (int, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", a.Run)
	cmd.Dir = a.Dir
	cmd.Env = append(childenv.Environ(),
		"ORLA_URL="+m.URL, "ORLA_TOKEN="+m.Token, "ORLA_AUTOMATION="+a.Name,
		"ORLA_SESSION="+session, "ORLA_CWD="+cwd, "ORLA_EVENT="+event, "PYTHONUNBUFFERED=1")
	// Its own process group, so stopping it ends what it started too.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM) }
	cmd.WaitDelay = 5 * time.Second
	out, _ := cmd.StdoutPipe()
	errs, _ := cmd.StderrPipe()
	now := time.Now()
	in.set(func(s *Status) {
		s.State, s.Since, s.LastRun, s.Error = "running", now, &now, ""
		s.Runs++
	})
	if event != "" {
		var ev struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal([]byte(event), &ev)
		in.say("orla", "run for "+ev.Type)
	} else {
		in.say("orla", "run: "+a.Run)
	}
	if err := cmd.Start(); err != nil {
		in.say("orla", "could not start: "+err.Error())
		in.set(func(s *Status) { s.Error = err.Error() })
		return -1, err
	}
	var wg sync.WaitGroup
	pipe := func(r io.Reader, stream string) {
		defer wg.Done()
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
		for sc.Scan() {
			in.say(stream, strings.TrimRight(sc.Text(), "\r"))
		}
	}
	wg.Add(2)
	go pipe(out, "out")
	go pipe(errs, "err")
	wg.Wait()
	err := cmd.Wait()
	code := cmd.ProcessState.ExitCode()
	end := time.Now()
	in.set(func(s *Status) { s.Exit, s.LastEnd = &code, &end })
	if ctx.Err() != nil {
		in.say("orla", "stopped")
		return code, ctx.Err()
	}
	if err != nil {
		in.say("orla", "ended with exit code "+itoa(code))
		in.set(func(s *Status) { s.Error = "exit code " + itoa(code) })
	} else {
		in.say("orla", "ended")
	}
	if m.Log != nil && err != nil {
		m.Log.Warn("automation run failed", "automation", a.Name, "session", session, "err", err)
	}
	return code, err
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// tail reads the last lines of a log file, as Lines numbered from 1.
func tail(path string, n int) ([]Line, int64) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, 0
	}
	rows := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(rows) > n {
		rows = rows[len(rows)-n:]
	}
	var out []Line
	for _, r := range rows {
		at, rest, ok := strings.Cut(r, " ")
		if !ok {
			continue
		}
		stream, text, _ := strings.Cut(rest, " ")
		t, err := time.Parse(time.RFC3339, at)
		if err != nil {
			continue
		}
		out = append(out, Line{N: int64(len(out) + 1), Time: t, Stream: stream, Text: text})
	}
	return out, int64(len(out))
}

func (m *Manager) logPath(name, session string) string {
	if session != "" {
		name += "." + session
	}
	return filepath.Join(m.Logs, name+".log")
}
