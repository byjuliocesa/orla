package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/Julio0Cesar/orla/internal/events"
	"github.com/Julio0Cesar/orla/internal/session"
	"github.com/Julio0Cesar/orla/internal/store"
)

// heartbeat keeps idle event streams alive through proxies that close quiet connections.
const heartbeat = 25 * time.Second

type sessionView struct {
	store.Session
	// ActiveRun is the headless run in progress, if any.
	ActiveRun string `json:"active_run,omitempty"`
	// LastSeq is the seq of the latest event, to stream only what comes next.
	LastSeq int64 `json:"last_seq"`
	// State is what the session is doing: working, waiting (for the owner), idle or ended.
	State string `json:"state"`
	// Pending counts the approval requests waiting for the owner.
	Pending int        `json:"pending"`
	Updated *time.Time `json:"updated,omitempty"`
	LastRun *store.Run `json:"last_run,omitempty"`
	// Worktree is set when the session works in a worktree Orla made.
	Worktree *store.Worktree `json:"worktree,omitempty"`
}

func (s *Server) view(ctx context.Context, x store.Session) sessionView {
	v := sessionView{Session: x}
	v.ActiveRun, _ = s.sessions.Busy(x.ID)
	a, _ := s.store.Activity(ctx, x.ID)
	v.LastSeq = a.LastSeq
	if !a.Updated.IsZero() {
		v.Updated = &a.Updated
	}
	if run, err := s.store.LatestRun(ctx, x.ID); err == nil {
		v.LastRun = &run
	}
	v.Pending = s.sessions.PendingCount(x.ID)
	if wt, err := s.store.WorktreeFor(ctx, x.Cwd); err == nil {
		v.Worktree = &wt
	}

	running := v.LastRun != nil && v.LastRun.Status == store.RunRunning
	switch {
	case v.Pending > 0 || (running && a.State == "waiting"):
		v.State = "waiting"
	case running:
		v.State = "working"
	case a.State == "ended":
		v.State = "ended"
	default:
		v.State = "idle"
	}
	return v
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

	writeSSE(w, r, s.bus.Subscribe(r.Context(), x.ID, from), true)
}

// streamAllEvents sends the events of every session as they happen, without replay and without
// ids: seqs count per session, so there is nothing to resume from. A client reads the current
// state from the sessions list after it connects, and applies what follows.
func (s *Server) streamAllEvents(w http.ResponseWriter, r *http.Request) {
	writeSSE(w, r, s.bus.SubscribeAll(r.Context()), false)
}

func (s *Server) listApprovals(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"approvals": s.sessions.PendingApprovals()})
}

// writeSSE streams events until the client leaves or the channel closes (a subscriber that fell
// behind; the client reconnects).
func writeSSE(w http.ResponseWriter, r *http.Request, ch <-chan events.Event, withID bool) {
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
	tick := time.NewTicker(heartbeat)
	defer tick.Stop()
	for {
		select {
		case e, ok := <-ch:
			if !ok {
				return
			}
			b, _ := json.Marshal(e)
			var err error
			if withID {
				_, err = fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", e.Seq, e.Type, b)
			} else {
				_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Type, b)
			}
			if err != nil {
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
