// Package server is the only way into Orla from outside: the HTTP API and its WebSocket
// streams. Handlers translate HTTP to calls on the core packages and hold no state of their own.
package server

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/byjuliocesa/orla/internal/auth"
	"github.com/byjuliocesa/orla/internal/events"
	"github.com/byjuliocesa/orla/internal/session"
	"github.com/byjuliocesa/orla/internal/store"
	"github.com/byjuliocesa/orla/internal/terminal"
)

// Deps are the core services the API exposes.
type Deps struct {
	Log       *slog.Logger
	Token     string
	Terminals *terminal.Manager
	Store     *store.Store
	Bus       *events.Bus
	Sessions  *session.Service
	// HookURL is where CLIs in Orla terminals send their hook events.
	HookURL string
	// Worktrees is the folder that holds the worktrees Orla makes.
	Worktrees string
	// UI serves the web client at every path outside the API. Nil serves a note saying the
	// binary was built without it.
	UI http.Handler
}

type Server struct {
	log       *slog.Logger
	terminals *terminal.Manager
	store     *store.Store
	bus       *events.Bus
	sessions  *session.Service
	hookURL   string
	worktrees string
}

// New returns the root handler with every route registered.
func New(d Deps) http.Handler {
	s := &Server{
		log: d.Log, terminals: d.Terminals, store: d.Store, bus: d.Bus, sessions: d.Sessions,
		hookURL: d.HookURL, worktrees: d.Worktrees,
	}

	api := http.NewServeMux()
	api.HandleFunc("GET /v1/terminals", s.listTerminals)
	api.HandleFunc("POST /v1/terminals", s.openTerminal)
	api.HandleFunc("DELETE /v1/terminals/{id}", s.closeTerminal)
	api.HandleFunc("GET /v1/terminals/{id}/stream", s.streamTerminal)

	api.HandleFunc("GET /v1/sessions", s.listSessions)
	api.HandleFunc("POST /v1/sessions", s.createSession)
	api.HandleFunc("GET /v1/sessions/{id}", s.getSession)
	api.HandleFunc("PATCH /v1/sessions/{id}", s.updateSession)
	api.HandleFunc("DELETE /v1/sessions/{id}", s.deleteSession)
	api.HandleFunc("POST /v1/sessions/{id}/runs", s.startRun)
	api.HandleFunc("POST /v1/sessions/{id}/cancel", s.cancelRun)
	api.HandleFunc("GET /v1/sessions/{id}/events", s.streamEvents)
	api.HandleFunc("POST /v1/sessions/{id}/approvals/{approval}", s.resolveApproval)
	api.HandleFunc("GET /v1/events", s.streamAllEvents)
	api.HandleFunc("GET /v1/approvals", s.listApprovals)

	api.HandleFunc("GET /v1/git", s.gitInfo)
	api.HandleFunc("POST /v1/worktrees", s.createWorktree)
	api.HandleFunc("GET /v1/sessions/{id}/changes", s.sessionChanges)
	api.HandleFunc("GET /v1/sessions/{id}/diff", s.sessionDiff)
	api.HandleFunc("POST /v1/sessions/{id}/discard", s.discardChange)
	api.HandleFunc("POST /v1/sessions/{id}/commit", s.commitChanges)
	api.HandleFunc("GET /v1/sessions/{id}/merge", s.previewMerge)
	api.HandleFunc("POST /v1/sessions/{id}/merge", s.mergeWorktree)
	api.HandleFunc("DELETE /v1/sessions/{id}/worktree", s.removeWorktree)

	mux := http.NewServeMux()
	// Unauthenticated on purpose: it says only that the process is up, for health checks and
	// process supervisors.
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	gate := auth.Gate{Token: d.Token, Logins: d.Store}
	mux.Handle("/v1/", gate.Require(api))
	mux.HandleFunc("POST /auth/login", gate.Login)
	mux.HandleFunc("POST /auth/logout", gate.Logout)
	mux.HandleFunc("POST /hooks/claude", s.claudeHook)
	mux.HandleFunc("/mcp/runs/{run}", s.runMCP)
	ui := d.UI
	if ui == nil {
		ui = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "This Orla was built without its web client. Build it with `make`.", http.StatusNotFound)
		})
	}
	// "/" and not "GET /": a method on the catch-all would conflict with "/v1/".
	mux.Handle("/", ui)
	return mux
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
