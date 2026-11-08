package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/coder/websocket"

	"github.com/byjuliocesa/orla/internal/engine/cli"
	"github.com/byjuliocesa/orla/internal/events"
	"github.com/byjuliocesa/orla/internal/session"
	"github.com/byjuliocesa/orla/internal/store"
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
	if x.SkillsOff == nil {
		x.SkillsOff = []string{} // a list, never null, for clients
	}
	if x.Flows == nil {
		x.Flows = []string{}
	}
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
	// No folder: the session starts in the home directory, and the agent makes its own from there.
	if body.Cwd == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "no folder given and no home directory to start in")
			return
		}
		body.Cwd = home
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
	// remote: the claude.ai page of the conversation, when Remote Control was turned on for it.
	writeJSON(w, http.StatusOK, map[string]any{"session": s.view(r.Context(), x), "runs": runs, "remote": cli.Bridge(x.NativeSession)})
}

// updateSession changes what the owner sets on a session; fields left out stay as they are.
func (s *Server) updateSession(w http.ResponseWriter, r *http.Request) {
	x, ok := s.lookup(w, r)
	if !ok {
		return
	}
	var body struct {
		Title    *string `json:"title"`
		Pinned   *bool   `json:"pinned"`
		Archived *bool   `json:"archived"`
		// SkillsOff replaces the skills this session leaves out.
		SkillsOff *[]string `json:"skills_off"`
		// Flows replaces the flows this session follows.
		Flows *[]string `json:"flows"`
	}
	if !decode(w, r, &body) {
		return
	}
	ctx := r.Context()
	var err error
	if body.Title != nil {
		title := strings.TrimSpace(*body.Title)
		if utf8.RuneCountInString(title) > 120 {
			writeError(w, http.StatusBadRequest, "title is longer than 120 characters")
			return
		}
		err = errors.Join(err, s.store.SetSessionTitle(ctx, x.ID, title))
	}
	if body.Pinned != nil {
		err = errors.Join(err, s.store.SetSessionPinned(ctx, x.ID, *body.Pinned))
	}
	if body.Archived != nil {
		var at *time.Time
		if *body.Archived {
			now := time.Now()
			at = &now
		}
		err = errors.Join(err, s.store.SetSessionArchived(ctx, x.ID, at))
	}
	if body.Flows != nil {
		err = errors.Join(err, s.store.SetSessionFlows(ctx, x.ID, *body.Flows))
	}
	if body.SkillsOff != nil || body.Flows != nil {
		if body.SkillsOff != nil {
			err = errors.Join(err, s.store.SetSessionSkillsOff(ctx, x.ID, *body.SkillsOff))
		}
		// What fills the context changes with them.
		s.ctxMu.Lock()
		delete(s.contexts, x.ID)
		s.ctxMu.Unlock()
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if x, err = s.store.Session(ctx, x.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.announce(ctx, x, x.Title)
	writeJSON(w, http.StatusOK, s.view(ctx, x))
}

// importSession brings a conversation from Claude Code's transcripts into Orla, to go on with
// it here as a fork.
func (s *Server) importSession(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Native string `json:"native_session"`
		Title  string `json:"title"`
	}
	if !decode(w, r, &body) {
		return
	}
	if body.Native == "" || strings.ContainsAny(body.Native, "/\\*?[") {
		writeError(w, http.StatusBadRequest, "native_session is required")
		return
	}
	x, err := s.sessions.Import(r.Context(), body.Native, strings.TrimSpace(body.Title))
	if errors.Is(err, session.ErrNoTranscript) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.announce(r.Context(), x, x.Title)
	writeJSON(w, http.StatusCreated, s.view(r.Context(), x))
}

// forkSession makes a session that goes on from this one's conversation as it is now.
func (s *Server) forkSession(w http.ResponseWriter, r *http.Request) {
	x, ok := s.lookup(w, r)
	if !ok {
		return
	}
	var body struct {
		// Run is the run whose answer the fork goes on from; empty goes on from the end.
		Run string `json:"run"`
	}
	if r.ContentLength > 0 && !decode(w, r, &body) {
		return
	}
	at := ""
	runs, err := s.store.Runs(r.Context(), x.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if body.Run == "" {
		for _, run := range runs {
			if run.ParentRun == "" {
				body.Run = run.ID // the latest: the fork goes on from the end
			}
		}
	} else {
		// The run's place among the session's own runs, and whether it is the latest.
		nth, total := countPrompts(runs, body.Run), countPrompts(runs, "")
		last := nth == total
		if nth == 0 {
			writeError(w, http.StatusNotFound, "no such run in this session")
			return
		}
		if !last {
			if at, err = cli.ForkPoint(x.NativeSession, nth); err != nil {
				writeError(w, http.StatusConflict, err.Error())
				return
			}
		}
	}
	title := x.Title
	if title != "" {
		title = strings.TrimSpace(string([]rune(title)[:min(utf8.RuneCountInString(title), 110)])) + " · fork"
	}
	f, err := s.sessions.Fork(r.Context(), x, title, at, body.Run)
	if errors.Is(err, session.ErrNothingToFork) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.announce(r.Context(), f, f.Title)
	writeJSON(w, http.StatusCreated, s.view(r.Context(), f))
}

// countPrompts is the place, from 1, of a run among a session's own runs (not its subagents'),
// 0 if it is not one of them. An empty id counts them all.
func countPrompts(runs []store.Run, id string) int {
	n := 0
	for _, r := range runs {
		if r.ParentRun != "" {
			continue
		}
		n++
		if r.ID == id {
			return n
		}
	}
	if id == "" {
		return n
	}
	return 0
}

// limits is the account's plan windows as last reported by any run, and when.
func (s *Server) limits(w http.ResponseWriter, r *http.Request) {
	data, at, ok, err := s.store.LatestLimits(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"limits": nil})
		return
	}
	var d struct {
		Limits json.RawMessage `json:"limits"`
	}
	_ = json.Unmarshal(data, &d)
	writeJSON(w, http.StatusOK, map[string]any{"limits": d.Limits, "at": at})
}

// deleteSession forgets a session that is at rest. A session still working, waiting on the owner
// or holding a worktree is refused, so removing it never loses work.
func (s *Server) deleteSession(w http.ResponseWriter, r *http.Request) {
	x, ok := s.lookup(w, r)
	if !ok {
		return
	}
	v := s.view(r.Context(), x)
	switch {
	case v.State == "working" || v.State == "waiting":
		writeError(w, http.StatusConflict, "the session is still working; stop it first")
		return
	case v.Worktree != nil && v.Worktree.Removed == nil:
		writeError(w, http.StatusConflict, "the session has a worktree; remove it from the Changes tab first")
		return
	}
	// Its automations end with it.
	if s.automations != nil {
		s.automations.StopSession(x.ID)
	}
	_ = s.store.DropAutomations(r.Context(), "", x.ID)
	if err := s.store.DeleteSession(r.Context(), x.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
		Effort         string `json:"effort"`
		// Ultracode asks Claude Code for its multi-agent mode, through the keyword it reads.
		Ultracode bool `json:"ultracode"`
		// Attachments are upload ids; the agent gets their paths and may read their folders.
		Attachments []string `json:"attachments"`
	}
	if !decode(w, r, &body) {
		return
	}
	if body.Prompt == "" {
		writeError(w, http.StatusBadRequest, "prompt is required")
		return
	}

	prompt := body.Prompt
	var dirs []string
	if len(body.Attachments) > 0 {
		var list strings.Builder
		for _, id := range body.Attachments {
			path, ok := s.uploadPath(id)
			if !ok {
				writeError(w, http.StatusBadRequest, "attachment "+id+" is not on the server")
				return
			}
			list.WriteString("\n- " + path)
			dirs = append(dirs, filepath.Dir(path))
		}
		prompt += "\n\nAttached files:" + list.String()
	}
	if body.Ultracode {
		prompt += "\n\nultracode"
	}
	run, err := s.sessions.Start(r.Context(), x.ID, session.RunOptions{
		Prompt: prompt, PermissionMode: body.PermissionMode, Model: body.Model, Effort: body.Effort, AddDirs: dirs,
	})
	if errors.Is(err, session.ErrBusy) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// A session's first prompt names it, as Claude Code names its own, unless the owner did.
	if x.Title == "" && x.NativeSession == "" && s.titler != nil {
		go s.nameSession(x.ID, body.Prompt)
	}
	writeJSON(w, http.StatusAccepted, run)
}

// nameSession asks for a title and sets it, if the session still has none by then.
func (s *Server) nameSession(id, prompt string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	title, err := s.titler(ctx, prompt)
	if err != nil || title == "" {
		s.log.Warn("naming a session", "session", id, "err", err)
		return
	}
	x, err := s.store.Session(ctx, id)
	if err != nil || x.Title != "" {
		return
	}
	if err := s.store.SetSessionTitle(ctx, id, title); err != nil {
		s.log.Warn("naming a session", "session", id, "err", err)
		return
	}
	s.announce(ctx, x, title)
}

// announce tells every client that a session's own settings changed, so lists reload.
func (s *Server) announce(ctx context.Context, x store.Session, title string) {
	data, _ := json.Marshal(map[string]string{"title": title})
	if _, err := s.bus.Publish(ctx, events.Event{
		Session: x.ID, Source: events.Source{Agent: x.Agent}, Type: events.SessionUpdated, Data: data,
	}); err != nil {
		s.log.Warn("announcing a session change", "session", x.ID, "err", err)
	}
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

	writeStream(w, r, s.bus.Subscribe(r.Context(), x.ID, from), true)
}

// sessionContext is Claude Code's own account of what fills the session's context window. It
// is asked once per state of the session: the next event makes it stale.
func (s *Server) sessionContext(w http.ResponseWriter, r *http.Request) {
	x, ok := s.lookup(w, r)
	if !ok {
		return
	}
	native := x.NativeSession
	if native == "" {
		native = x.ForkOf
	}
	if native == "" || s.contexter == nil {
		writeJSON(w, http.StatusOK, map[string]any{"context": nil})
		return
	}
	seq, _ := s.store.LastSeq(r.Context(), x.ID)
	s.ctxMu.Lock()
	c, hit := s.contexts[x.ID]
	s.ctxMu.Unlock()
	if hit && c.seq == seq {
		writeJSON(w, http.StatusOK, map[string]any{"context": c.usage})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	u, err := s.contexter(ctx, native, x.Cwd, cli.SkillSettings(sessionOff(x)))
	if err != nil {
		writeError(w, http.StatusBadGateway, "Claude Code did not report the context: "+err.Error())
		return
	}
	s.ctxMu.Lock()
	s.contexts[x.ID] = cachedContext{seq: seq, usage: u}
	s.ctxMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"context": u})
}

// history is every event of a session so far, in one response: the client draws the whole
// conversation at once, then follows it live from the last seq.
func (s *Server) history(w http.ResponseWriter, r *http.Request) {
	x, ok := s.lookup(w, r)
	if !ok {
		return
	}
	evs, err := s.store.After(r.Context(), x.ID, 0, 200_000)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": evs})
}

// streamAllEvents sends the events of every session as they happen, without replay and without
// ids: seqs count per session, so there is nothing to resume from. A client reads the current
// state from the sessions list after it connects, and applies what follows.
func (s *Server) streamAllEvents(w http.ResponseWriter, r *http.Request) {
	writeStream(w, r, s.bus.SubscribeAll(r.Context()), false)
}

func (s *Server) listApprovals(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"approvals": s.sessions.PendingApprovals()})
}

// writeStream sends events over a WebSocket when the client asks for one, and as SSE
// otherwise. Browsers use the WebSocket: an SSE response holds one of the six HTTP/1.1
// connections a browser allows per site, and two streams in each of a few tabs used them all,
// so a reloaded tab waited in line for its own page. WebSockets have a pool of their own.
func writeStream(w http.ResponseWriter, r *http.Request, ch <-chan events.Event, withID bool) {
	if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		writeWS(w, r, ch)
		return
	}
	writeSSE(w, r, ch, withID)
}

// writeWS sends each event as one JSON text message until the client leaves or the channel
// closes. The client sends nothing; a ping keeps proxies from closing a quiet stream.
func writeWS(w http.ResponseWriter, r *http.Request, ch <-chan events.Event) {
	// Accept refuses cross-origin browser requests, as for terminals.
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer c.CloseNow()
	ctx := c.CloseRead(r.Context())
	tick := time.NewTicker(heartbeat)
	defer tick.Stop()
	for {
		select {
		case e, ok := <-ch:
			if !ok {
				c.Close(websocket.StatusTryAgainLater, "fell behind; reconnect")
				return
			}
			b, _ := json.Marshal(e)
			wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := c.Write(wctx, websocket.MessageText, b)
			cancel()
			if err != nil {
				return
			}
		case <-tick.C:
			pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := c.Ping(pctx)
			cancel()
			if err != nil {
				return
			}
		case <-ctx.Done():
			return
		}
	}
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

// sessionOff is what a session's conversation leaves out, as its runs are started with.
func sessionOff(x store.Session) []string { return session.Off(x) }
