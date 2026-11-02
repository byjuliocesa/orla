package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"

	"github.com/coder/websocket"

	"github.com/Julio0Cesar/orla/internal/terminal"
)

// maxInputMessage bounds one message from a client: a large paste fits, a runaway client
// does not.
const maxInputMessage = 1 << 20

func (s *Server) listTerminals(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"terminals": s.terminals.List()})
}

func (s *Server) openTerminal(w http.ResponseWriter, r *http.Request) {
	var spec terminal.Spec
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&spec); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if len(spec.Argv) == 0 {
		spec.Argv = []string{defaultShell()}
	}
	if spec.Cwd == "" {
		spec.Cwd, _ = os.UserHomeDir()
	}
	if info, err := os.Stat(spec.Cwd); err != nil || !info.IsDir() {
		writeError(w, http.StatusBadRequest, "cwd is not a directory: "+spec.Cwd)
		return
	}

	t, err := s.terminals.Open(spec)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.log.Info("terminal opened", "id", t.Info().ID, "argv", spec.Argv, "cwd", spec.Cwd)
	writeJSON(w, http.StatusCreated, t.Info())
}

func (s *Server) closeTerminal(w http.ResponseWriter, r *http.Request) {
	if !s.terminals.Close(r.PathValue("id")) {
		writeError(w, http.StatusNotFound, "no such terminal")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// control is a text message on a terminal stream. Binary messages carry terminal bytes: input
// from the client, output to it.
type control struct {
	Type string `json:"type"`
	// resize
	Cols uint16 `json:"cols,omitempty"`
	Rows uint16 `json:"rows,omitempty"`
	// exit
	Code *int `json:"code,omitempty"`
}

// streamTerminal attaches a client. Leaving the stream detaches it; the process keeps running.
func (s *Server) streamTerminal(w http.ResponseWriter, r *http.Request) {
	t, ok := s.terminals.Get(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "no such terminal")
		return
	}

	// Accept refuses cross-origin browser requests by default, so a page on another site cannot
	// open a shell with a cookie the browser sends along. Clients outside a browser send no Origin.
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer c.CloseNow()
	c.SetReadLimit(maxInputMessage)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	scrollback, output, detach := t.Attach()
	defer detach()

	go s.readInput(ctx, cancel, c, t)

	if len(scrollback) > 0 {
		if err := c.Write(ctx, websocket.MessageBinary, scrollback); err != nil {
			return
		}
	}
	for {
		select {
		case p, ok := <-output:
			if !ok {
				s.finishStream(ctx, c, t)
				return
			}
			if err := c.Write(ctx, websocket.MessageBinary, p); err != nil {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

// finishStream tells the client why output ended: the process exited, or the client fell
// behind and should attach again.
func (s *Server) finishStream(ctx context.Context, c *websocket.Conn, t *terminal.Terminal) {
	select {
	case <-t.Done():
		code := t.ExitCode()
		msg, _ := json.Marshal(control{Type: "exit", Code: &code})
		_ = c.Write(ctx, websocket.MessageText, msg)
		c.Close(websocket.StatusNormalClosure, "process exited")
	default:
		c.Close(websocket.StatusTryAgainLater, "client fell behind; attach again")
	}
}

func (s *Server) readInput(ctx context.Context, cancel context.CancelFunc, c *websocket.Conn, t *terminal.Terminal) {
	defer cancel()
	for {
		typ, data, err := c.Read(ctx)
		if err != nil {
			return
		}
		switch typ {
		case websocket.MessageBinary:
			if _, err := t.Write(data); err != nil {
				return
			}
		case websocket.MessageText:
			var msg control
			if err := json.Unmarshal(data, &msg); err != nil {
				continue
			}
			if msg.Type == "resize" {
				if err := t.Resize(msg.Cols, msg.Rows); err != nil && !errors.Is(err, os.ErrClosed) {
					s.log.Debug("resize", "err", err)
				}
			}
		}
	}
}

func defaultShell() string {
	if sh := os.Getenv("SHELL"); sh != "" {
		return sh
	}
	return "/bin/sh"
}
