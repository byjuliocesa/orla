package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strconv"
	"time"

	"github.com/Julio0Cesar/orla/internal/engine/cli"
	"github.com/Julio0Cesar/orla/internal/session"
	"github.com/Julio0Cesar/orla/internal/store"
)

// heartbeat keeps idle event streams alive through proxies that close quiet connections.
const heartbeat = 25 * time.Second

type sessionView struct {
	store.Session
	// ActiveRun is the run in progress, if any.
	ActiveRun string `json:"active_run,omitempty"`
	// LastSeq is the seq of the latest event, to stream only what comes next.
	LastSeq int64 `json:"last_seq"`
}

func (s *Server) view(ctx context.Context, x store.Session) sessionView {
	run, _ := s.sessions.Busy(x.ID)
	seq, _ := s.store.LastSeq(ctx, x.ID)
	return sessionView{Session: x, ActiveRun: run, LastSeq: seq}
}

func (s *Server) listSessions(w http.ResponseWriter, r *http.Request) {
	all, err := s.store.Sessions(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]sessionView, 0, len(all))
	for _, x := range all {
		out = append(out, s.view(r.Context(), x))
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": out})
}

func (s *Server) createSession(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Agent string `json:"agent"`
		Cwd   string `json:"cwd"`
	}
	if !decode(w, r, &body) {
		return
	}
	if body.Agent == "" {
		body.Agent = "claude"
	}
	if info, err := os.Stat(body.Cwd); body.Cwd == "" || err != nil || !info.IsDir() {
		writeError(w, http.StatusBadRequest, "cwd must be an existing directory")
		return
	}
	x, err := s.sessions.Create(r.Context(), body.Agent, body.Cwd)
	if errors.Is(err, session.ErrUnknownAgent) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, s.view(r.Context(), x))
}

func (s *Server) getSession(w http.ResponseWriter, r *http.Request) {
	x, ok := s.lookup(w, r)
	if !ok {
		return
	}
	runs, err := s.store.Runs(r.Context(), x.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"session": s.view(r.Context(), x), "runs": runs})
}

func (s *Server) startRun(w http.ResponseWriter, r *http.Request) {
	x, ok := s.lookup(w, r)
	if !ok {
		return
	}
	var body struct {
		Prompt         string `json:"prompt"`
		PermissionMode string `json:"permission_mode"`
		Model          string `json:"model"`
	}
	if !decode(w, r, &body) {
		return
	}
	if body.Prompt == "" {
		writeError(w, http.StatusBadRequest, "prompt is required")
		return
	}
	if body.PermissionMode != "" && !slices.Contains(cli.PermissionModes, body.PermissionMode) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("permission_mode must be one of %v", cli.PermissionModes))
		return
	}

	run, err := s.sessions.Start(r.Context(), x.ID, session.RunOptions{
		Prompt: body.Prompt, PermissionMode: body.PermissionMode, Model: body.Model,
	})
	if errors.Is(err, session.ErrBusy) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, run)
}

func (s *Server) cancelRun(w http.ResponseWriter, r *http.Request) {
	x, ok := s.lookup(w, r)
	if !ok {
		return
	}
	if err := s.sessions.Cancel(x.ID); errors.Is(err, session.ErrNotRunning) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// streamEvents sends a session's events as Server-Sent Events: stored ones after the given seq,
// then live ones. The seq is the SSE id, so a browser EventSource resumes by itself through
// Last-Event-ID; other clients pass ?after=. A stream that ends without the client leaving
// means it fell behind; reconnecting from the last id loses nothing.
func (s *Server) streamEvents(w http.ResponseWriter, r *http.Request) {
	x, ok := s.lookup(w, r)
	if !ok {
		return
	}
	after := r.URL.Query().Get("after")
	if last := r.Header.Get("Last-Event-ID"); last != "" {
		after = last
	}
	var from int64
	if after != "" {
		var err error
		if from, err = strconv.ParseInt(after, 10, 64); err != nil || from < 0 {
			writeError(w, http.StatusBadRequest, "after must be a non-negative seq")
			return
		}
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	// Tells nginx not to buffer the stream, which would hold events back.
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ctx := r.Context()
	ch := s.bus.Subscribe(ctx, x.ID, from)
	tick := time.NewTicker(heartbeat)
	defer tick.Stop()
	for {
		select {
		case e, ok := <-ch:
			if !ok {
				return
			}
			b, _ := json.Marshal(e)
			if _, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", e.Seq, e.Type, b); err != nil {
				return
			}
			flusher.Flush()
		case <-tick.C:
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-ctx.Done():
			return
		}
	}
}

func (s *Server) lookup(w http.ResponseWriter, r *http.Request) (store.Session, bool) {
	x, err := s.store.Session(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no such session")
		return x, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return x, false
	}
	return x, true
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return false
	}
	return true
}
