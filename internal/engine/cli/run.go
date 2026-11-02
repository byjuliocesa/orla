package cli

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/Julio0Cesar/orla/internal/events"
)

// maxLine bounds one line of CLI output. A tool result can be large, and a line longer than
// this would end the run with an error rather than be silently cut.
const maxLine = 32 << 20

// stopGrace is how long a cancelled CLI gets to stop after SIGTERM before it is killed.
const stopGrace = 5 * time.Second

// runEnv marks every process of a run, so leftovers can be found after a crash.
const runEnv = "ORLA_RUN"

// Spec is one headless run of a CLI.
type Spec struct {
	// Run is Orla's run id, set in the CLI's environment as ORLA_RUN.
	Run    string
	Cwd    string
	Prompt string
	// Resume is the CLI's session id from an earlier run; empty starts a new conversation.
	Resume string
	// PermissionMode overrides the CLI's own default. Empty keeps the owner's setting in the
	// CLI, which may well be bypassPermissions.
	PermissionMode string
	Model          string
}

// Claude runs Claude Code headless. It uses whatever account the CLI is logged into; Orla
// never sees those credentials.
type Claude struct {
	// Path is the claude executable. Empty means "claude" on PATH.
	Path string
}

func (c Claude) args(spec Spec) []string {
	args := []string{"-p", spec.Prompt, "--output-format", "stream-json", "--verbose"}
	if spec.Resume != "" {
		args = append(args, "--resume", spec.Resume)
	}
	if spec.PermissionMode != "" {
		args = append(args, "--permission-mode", spec.PermissionMode)
	}
	if spec.Model != "" {
		args = append(args, "--model", spec.Model)
	}
	return args
}

// Run runs the CLI until it finishes or ctx ends, passing each event to emit as it happens.
// The CLI's session id reaches native as soon as the CLI reports it, so it is saved even if
// the run fails later.
func (c Claude) Run(ctx context.Context, spec Spec, emit func(Out), native func(string)) Outcome {
	path := c.Path
	if path == "" {
		path = "claude"
	}

	cmd := exec.Command(path, c.args(spec)...)
	cmd.Dir = spec.Cwd
	cmd.SysProcAttr = sysProcAttr()
	cmd.Env = append(headlessEnv(), runEnv+"="+spec.Run)
	var stderr bytes.Buffer
	cmd.Stderr = &limitedWriter{w: &stderr, n: 64 << 10}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return failed(emit, "", err.Error())
	}
	if err := cmd.Start(); err != nil {
		return failed(emit, "", "could not start "+path+": "+err.Error())
	}

	stopped := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
			select {
			case <-stopped:
			case <-time.After(stopGrace):
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			}
		case <-stopped:
		}
	}()

	parser := newClaudeParser()
	reader := bufio.NewReaderSize(stdout, 64<<10)
	var readErr error
	for {
		line, err := readLine(reader)
		if len(line) > 0 {
			before := parser.outcome.NativeSession
			for _, out := range parser.Line(line) {
				emit(out)
			}
			if s := parser.outcome.NativeSession; s != "" && s != before {
				native(s)
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				readErr = err
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			}
			break
		}
	}
	waitErr := cmd.Wait()
	close(stopped)

	outcome := parser.outcome
	switch {
	case ctx.Err() != nil:
		outcome.Status = "cancelled"
	case readErr != nil:
		return failed(emit, outcome.NativeSession, "reading CLI output: "+readErr.Error())
	case outcome.Status == "":
		// It ended without a result line: it crashed, or refused to start (not logged in,
		// bad flag). Its stderr says why.
		msg := strings.TrimSpace(stderr.String())
		if msg == "" && waitErr != nil {
			msg = waitErr.Error()
		}
		if msg == "" {
			msg = "the CLI ended without a result"
		}
		return failed(emit, outcome.NativeSession, msg)
	}
	return outcome
}

func failed(emit func(Out), native, message string) Outcome {
	emit(Out{Type: events.Error, Data: map[string]any{"message": message, "retryable": false}})
	return Outcome{NativeSession: native, Status: "failed", Summary: message}
}

func readLine(r *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		chunk, err := r.ReadSlice('\n')
		line = append(line, chunk...)
		if len(line) > maxLine {
			return nil, fmt.Errorf("a line exceeded %d bytes", maxLine)
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		return bytes.TrimSpace(line), err
	}
}

// limitedWriter keeps the first n bytes and drops the rest, so a CLI that floods stderr cannot
// grow memory without bound.
type limitedWriter struct {
	w io.Writer
	n int
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	if l.n > 0 {
		k := min(len(p), l.n)
		_, _ = l.w.Write(p[:k])
		l.n -= k
	}
	return len(p), nil
}

// TerminalEnv names the variables an Orla terminal sets for the hook command.
var TerminalEnv = []string{"ORLA_TERMINAL", "ORLA_HOOK_URL", "ORLA_HOOK_TOKEN"}

// headlessEnv is Orla's environment without the terminal variables, in case Orla itself runs
// in an Orla terminal: a headless run reports through its output, and its hooks must not be
// taken for the terminal's.
func headlessEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if !slices.Contains(TerminalEnv, name) {
			env = append(env, kv)
		}
	}
	return env
}
