package terminal

import (
	"bytes"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func readUntil(t *testing.T, out <-chan []byte, want string) string {
	t.Helper()
	return readMatch(t, out, regexp.MustCompile(regexp.QuoteMeta(want)))[0]
}

// readMatch reads output until re matches it and returns the match and its groups.
func readMatch(t *testing.T, out <-chan []byte, re *regexp.Regexp) []string {
	t.Helper()
	var got bytes.Buffer
	timeout := time.After(5 * time.Second)
	for {
		if m := re.FindStringSubmatch(got.String()); m != nil {
			return m
		}
		select {
		case p, ok := <-out:
			if !ok {
				t.Fatalf("output closed before %s; got %q", re, got.String())
			}
			got.Write(p)
		case <-timeout:
			t.Fatalf("timed out waiting for %s; got %q", re, got.String())
		}
	}
}

// The point of keeping terminals on the server: work goes on while nobody is attached, and
// whoever attaches next sees what happened.
func TestOutputWhileDetachedIsReplayedOnAttach(t *testing.T) {
	m := NewManager()
	defer m.CloseAll()

	term, err := m.Open(Spec{Argv: []string{"sh"}, Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}

	_, out, detach := term.Attach()
	if _, err := term.Write([]byte("echo first-$((40+2))\n")); err != nil {
		t.Fatal(err)
	}
	readUntil(t, out, "first-42")
	detach()

	if _, err := term.Write([]byte("echo second-$((40+3))\n")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)

	scrollback, out, detach := term.Attach()
	defer detach()
	if !strings.Contains(string(scrollback), "second-43") {
		t.Fatalf("scrollback misses output produced while detached: %q", scrollback)
	}

	if _, err := term.Write([]byte("echo third-$((40+4))\n")); err != nil {
		t.Fatal(err)
	}
	readUntil(t, out, "third-44")
}

func TestExitReportsCodeAndLeavesManager(t *testing.T) {
	m := NewManager()
	term, err := m.Open(Spec{Argv: []string{"sh", "-c", "exit 7"}})
	if err != nil {
		t.Fatal(err)
	}

	select {
	case <-term.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("process did not exit")
	}
	if code := term.ExitCode(); code != 7 {
		t.Fatalf("exit code = %d, want 7", code)
	}

	deadline := time.Now().Add(2 * time.Second)
	for len(m.List()) > 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if len(m.List()) != 0 {
		t.Fatal("exited terminal still listed")
	}
}

// Closing a terminal must also end what the shell started, or processes leak on the server.
func TestCloseEndsChildProcesses(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("needs bash: plain sh does not turn on job control, which is the case under test")
	}
	m := NewManager()
	// An interactive bash runs each background job in its own process group, like the
	// owner's shell does.
	term, err := m.Open(Spec{Argv: []string{bash, "--norc", "--noprofile", "-i"}})
	if err != nil {
		t.Fatal(err)
	}
	_, out, detach := term.Attach()
	defer detach()

	// The arithmetic keeps the marker out of the terminal's echo of the command itself.
	if _, err := term.Write([]byte("sleep 300 & echo child-$((1+1))=$!.\n")); err != nil {
		t.Fatal(err)
	}
	pid, _ := strconv.Atoi(readMatch(t, out, regexp.MustCompile(`child-2=(\d+)\.`))[1])

	if !m.Close(term.Info().ID) {
		t.Fatal("close reported terminal missing")
	}

	deadline := time.Now().Add(5 * time.Second)
	for processAlive(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("child %d still running after close", pid)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// processAlive reports whether pid runs. A zombie has already exited and only waits to be
// reaped by its new parent, so it does not count.
func processAlive(pid int) bool {
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return false
	}
	end := strings.LastIndexByte(string(stat), ')')
	return end >= 0 && !strings.HasPrefix(strings.TrimSpace(string(stat[end+1:])), "Z")
}

// A process that detaches with setsid keeps the PTY open after the shell exits. The terminal
// must still end, or closing it would hang forever.
func TestExitIsNotHeldByDetachedProcess(t *testing.T) {
	m := NewManager()
	term, err := m.Open(Spec{Argv: []string{"sh", "-c", "setsid sleep 300 & echo held-$((1+1))=$!.; exit 3"}})
	if err != nil {
		t.Fatal(err)
	}
	scrollback, out, detach := term.Attach()
	defer detach()

	select {
	case <-term.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("terminal did not end while a detached process held its PTY")
	}
	if code := term.ExitCode(); code != 3 {
		t.Fatalf("exit code = %d, want 3", code)
	}

	got := string(scrollback)
	for p := range out {
		got += string(p)
	}
	match := regexp.MustCompile(`held-2=(\d+)\.`).FindStringSubmatch(got)
	if match == nil {
		t.Fatalf("last output lost: %q", got)
	}
	// setsid is the deliberate way to outlive the terminal, so the process is still running.
	if pid, _ := strconv.Atoi(match[1]); processAlive(pid) {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
}
