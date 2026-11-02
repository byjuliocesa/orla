package terminal

import (
	"errors"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"

	"github.com/Julio0Cesar/orla/internal/id"
)

// scrollbackLimit is how many bytes of recent output a terminal keeps for clients that attach
// later. Trimming can cut an escape sequence or a UTF-8 character in half at the very start of
// the replay; terminals recover from that within a line.
const scrollbackLimit = 256 << 10

// drainGrace is how long output may still be read after the process exits, so its last words
// reach the clients.
const drainGrace = 500 * time.Millisecond

// clientBuffer is how many output chunks a client may fall behind before it is detached.
const clientBuffer = 256

// Spec describes the process to run in a new terminal.
type Spec struct {
	Argv []string `json:"argv"`
	Cwd  string   `json:"cwd"`
	Cols uint16   `json:"cols"`
	Rows uint16   `json:"rows"`
}

// Info is what is reported about a terminal.
type Info struct {
	ID      string    `json:"id"`
	Argv    []string  `json:"argv"`
	Cwd     string    `json:"cwd"`
	Started time.Time `json:"started"`
	Clients int       `json:"clients"`
}

// Terminal is a process running on a PTY. It keeps running while no client is attached.
type Terminal struct {
	info Info
	pty  *os.File
	cmd  *exec.Cmd

	mu         sync.Mutex
	scrollback []byte
	clients    map[chan []byte]struct{}

	done     chan struct{}
	exitCode int
}

func start(spec Spec) (*Terminal, error) {
	if len(spec.Argv) == 0 {
		return nil, errors.New("terminal: argv is empty")
	}
	if spec.Cols == 0 || spec.Rows == 0 {
		spec.Cols, spec.Rows = 80, 24
	}

	cmd := exec.Command(spec.Argv[0], spec.Argv[1:]...)
	cmd.Dir = spec.Cwd
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor")

	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: spec.Cols, Rows: spec.Rows})
	if err != nil {
		return nil, err
	}

	t := &Terminal{
		info: Info{
			ID:      id.New("term"),
			Argv:    spec.Argv,
			Cwd:     spec.Cwd,
			Started: time.Now().UTC(),
		},
		pty:     f,
		cmd:     cmd,
		clients: map[chan []byte]struct{}{},
		done:    make(chan struct{}),
	}
	output := make(chan struct{})
	go t.pump(output)
	go t.wait(output)
	return t, nil
}

// pump copies PTY output to the scrollback and every attached client until reading fails,
// then closes output.
func (t *Terminal) pump(output chan<- struct{}) {
	defer close(output)
	buf := make([]byte, 32<<10)
	for {
		n, err := t.pty.Read(buf)
		if n > 0 {
			t.broadcast(buf[:n])
		}
		if err != nil {
			return
		}
	}
}

// wait ends the terminal when its process exits.
//
// Reading the PTY to its end is not a way to tell: the end comes only when every process holding
// the terminal has closed it, and one that called setsid, or a job that survived, may hold it
// for good. So the process's exit decides, and the PTY is closed after a short drain.
func (t *Terminal) wait(output <-chan struct{}) {
	code := 0
	if err := t.cmd.Wait(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			code = exit.ExitCode()
		} else {
			code = -1
		}
	}

	select {
	case <-output:
	case <-time.After(drainGrace):
	}
	t.pty.Close()
	<-output

	t.mu.Lock()
	t.exitCode = code
	for c := range t.clients {
		close(c)
	}
	t.clients = nil
	t.mu.Unlock()
	close(t.done)
}

func (t *Terminal) broadcast(p []byte) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.scrollback = append(t.scrollback, p...)
	if over := len(t.scrollback) - scrollbackLimit; over > 0 {
		t.scrollback = append(t.scrollback[:0:0], t.scrollback[over:]...)
	}

	for c := range t.clients {
		chunk := append([]byte(nil), p...)
		select {
		case c <- chunk:
		default:
			// A client that cannot keep up is detached rather than stalling the process: the
			// PTY is read only here, so a blocked send would freeze the program's output.
			delete(t.clients, c)
			close(c)
		}
	}
}

// Attach returns the recent output and a channel of everything after it, with nothing lost or
// repeated in between. The channel closes when the process exits or the client falls behind;
// detach stops delivery early.
func (t *Terminal) Attach() (scrollback []byte, output <-chan []byte, detach func()) {
	c := make(chan []byte, clientBuffer)

	t.mu.Lock()
	defer t.mu.Unlock()

	scrollback = append([]byte(nil), t.scrollback...)
	if t.clients == nil {
		close(c)
		return scrollback, c, func() {}
	}
	t.clients[c] = struct{}{}

	return scrollback, c, func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		if _, ok := t.clients[c]; ok {
			delete(t.clients, c)
			close(c)
		}
	}
}

func (t *Terminal) Write(p []byte) (int, error) { return t.pty.Write(p) }

func (t *Terminal) Resize(cols, rows uint16) error {
	if cols == 0 || rows == 0 {
		return errors.New("terminal: size must be positive")
	}
	return pty.Setsize(t.pty, &pty.Winsize{Cols: cols, Rows: rows})
}

// Done closes when the process has exited.
func (t *Terminal) Done() <-chan struct{} { return t.done }

// ExitCode is valid once Done is closed. -1 means the exit status could not be read.
func (t *Terminal) ExitCode() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.exitCode
}

func (t *Terminal) Info() Info {
	t.mu.Lock()
	defer t.mu.Unlock()
	info := t.info
	info.Clients = len(t.clients)
	return info
}

// kill hangs up every process the terminal started, as closing a real terminal does. Anything
// still alive after the grace period is killed.
func (t *Terminal) kill(grace time.Duration) {
	// pty.Start makes the child a session leader, so its pid is also the session id.
	sid := t.cmd.Process.Pid
	signalSession(sid, syscall.SIGHUP)
	select {
	case <-t.done:
	case <-time.After(grace):
		signalSession(sid, syscall.SIGKILL)
		<-t.done
	}
	// Background jobs can outlive the shell that started them and ignore SIGHUP.
	signalSession(sid, syscall.SIGKILL)
}
