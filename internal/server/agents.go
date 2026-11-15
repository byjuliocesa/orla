package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"

	"github.com/byjuliocesa/orla/internal/engine/cli"
)

// Agent is a coding agent Orla can run, and how to set it up on this machine.
type Agent struct {
	ID, Name string
	Status   func(ctx context.Context) cli.Status
	// Install runs to completion with no input.
	Install []string
	// Login prints a page to open and then waits for the code that page gives. It is asked each
	// time: after an install the executable is somewhere new.
	Login func() []string
}

type agentView struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	cli.Status
}

func (s *Server) agent(w http.ResponseWriter, r *http.Request) (Agent, bool) {
	for _, a := range s.agents {
		if a.ID == r.PathValue("id") {
			return a, true
		}
	}
	writeError(w, http.StatusNotFound, "no agent "+r.PathValue("id"))
	return Agent{}, false
}

func (s *Server) listAgents(w http.ResponseWriter, r *http.Request) {
	out := make([]agentView, len(s.agents))
	var wg sync.WaitGroup
	for i, a := range s.agents {
		out[i] = agentView{ID: a.ID, Name: a.Name}
		wg.Add(1)
		go func() {
			defer wg.Done()
			out[i].Status = a.Status(r.Context())
		}()
	}
	wg.Wait()
	writeJSON(w, http.StatusOK, map[string]any{"agents": out})
}

func (s *Server) installAgent(w http.ResponseWriter, r *http.Request) {
	a, ok := s.agent(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, a.Install[0], a.Install[1:]...)
	cmd.Dir, _ = os.UserHomeDir()
	out, err := cmd.CombinedOutput()
	if err != nil {
		writeError(w, http.StatusBadGateway, "The install failed: "+lastLine(out, err))
		return
	}
	writeJSON(w, http.StatusOK, agentView{ID: a.ID, Name: a.Name, Status: a.Status(r.Context())})
}

// signIn is a login under way: the agent's CLI, waiting in a terminal for its code.
type signIn struct {
	cmd  *exec.Cmd
	tty  *os.File
	mu   sync.Mutex
	out  bytes.Buffer
	done chan struct{}
}

func (l *signIn) text() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return ansi.ReplaceAllString(l.out.String(), "")
}

func (l *signIn) stop() {
	_ = l.cmd.Process.Kill()
	<-l.done
}

var (
	ansi   = regexp.MustCompile(`\x1b\][^\x07]*\x07|\x1b\[[0-9;?]*[A-Za-z]`)
	pageRe = regexp.MustCompile(`https://\S+`)
)

// startLogin runs the agent's login and answers the page it asks the owner to open.
func (s *Server) startLogin(w http.ResponseWriter, r *http.Request) {
	a, ok := s.agent(w, r)
	if !ok {
		return
	}
	argv := a.Login()
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir, _ = os.UserHomeDir()
	// Wide, so the page's address is never wrapped onto two lines.
	tty, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: 4000, Rows: 40})
	if err != nil {
		writeError(w, http.StatusBadGateway, "Orla could not start the sign-in: "+err.Error())
		return
	}
	l := &signIn{cmd: cmd, tty: tty, done: make(chan struct{})}
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := tty.Read(buf)
			l.mu.Lock()
			l.out.Write(buf[:n])
			l.mu.Unlock()
			if err != nil {
				break
			}
		}
		_ = cmd.Wait()
		tty.Close()
		close(l.done)
	}()

	s.loginMu.Lock()
	if old := s.logins[a.ID]; old != nil {
		old.stop()
	}
	s.logins[a.ID] = l
	s.loginMu.Unlock()

	deadline := time.After(20 * time.Second)
	for {
		if page := pageRe.FindString(l.text()); page != "" {
			writeJSON(w, http.StatusOK, map[string]string{"page": page})
			return
		}
		select {
		case <-l.done:
			writeError(w, http.StatusBadGateway, "The sign-in ended early: "+lastLine([]byte(l.text()), nil))
			return
		case <-deadline:
			l.stop()
			writeError(w, http.StatusGatewayTimeout, "The sign-in gave no page to open.")
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// finishLogin gives the login the code its page showed, and answers the agent as it is then.
func (s *Server) finishLogin(w http.ResponseWriter, r *http.Request) {
	a, ok := s.agent(w, r)
	if !ok {
		return
	}
	var body struct {
		Code string `json:"code"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body) != nil || strings.TrimSpace(body.Code) == "" {
		writeError(w, http.StatusBadRequest, "Paste the code from the sign-in page.")
		return
	}
	s.loginMu.Lock()
	l := s.logins[a.ID]
	s.loginMu.Unlock()
	if l == nil {
		writeError(w, http.StatusConflict, "The sign-in expired. Start it again.")
		return
	}
	before := len(l.text())
	if _, err := l.tty.Write([]byte(strings.TrimSpace(body.Code) + "\r")); err != nil {
		writeError(w, http.StatusConflict, "The sign-in expired. Start it again.")
		return
	}
	select {
	case <-l.done:
	case <-time.After(60 * time.Second):
	case <-r.Context().Done():
		return
	}
	st := a.Status(r.Context())
	if !st.LoggedIn {
		said := l.text()
		if len(said) > before {
			said = said[before:]
		}
		if l.isDone() {
			s.dropLogin(a.ID, l)
		}
		writeError(w, http.StatusUnprocessableEntity, lastLine([]byte(said), errors.New("the code was not accepted")))
		return
	}
	s.dropLogin(a.ID, l)
	writeJSON(w, http.StatusOK, agentView{ID: a.ID, Name: a.Name, Status: st})
}

func (l *signIn) isDone() bool {
	select {
	case <-l.done:
		return true
	default:
		return false
	}
}

func (s *Server) cancelLogin(w http.ResponseWriter, r *http.Request) {
	a, ok := s.agent(w, r)
	if !ok {
		return
	}
	s.loginMu.Lock()
	l := s.logins[a.ID]
	s.loginMu.Unlock()
	if l != nil {
		s.dropLogin(a.ID, l)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) dropLogin(id string, l *signIn) {
	s.loginMu.Lock()
	if s.logins[id] == l {
		delete(s.logins, id)
	}
	s.loginMu.Unlock()
	l.stop()
}

// lastLine is the last thing a command said, plain, for an error message.
func lastLine(out []byte, err error) string {
	lines := strings.Split(strings.TrimSpace(ansi.ReplaceAllString(string(out), "")), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(strings.Trim(lines[i], "\r>")); l != "" && !strings.HasPrefix(l, "Paste code") {
			return l
		}
	}
	if err != nil {
		return err.Error()
	}
	return "no output"
}
