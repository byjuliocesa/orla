// Package server is the only way into Orla from outside: the HTTP API and its WebSocket
// streams. Handlers translate HTTP to calls on the core packages and hold no state of their own.
package server

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/Julio0Cesar/orla/internal/auth"
	"github.com/Julio0Cesar/orla/internal/events"
	"github.com/Julio0Cesar/orla/internal/session"
	"github.com/Julio0Cesar/orla/internal/store"
	"github.com/Julio0Cesar/orla/internal/terminal"
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
}

type Server struct {
	log       *slog.Logger
	terminals *terminal.Manager
	store     *store.Store
	bus       *events.Bus
	sessions  *session.Service
	hookURL   string
}

// New returns the root handler with every route registered.
func New(d Deps) http.Handler {
	s := &Server{
		log: d.Log, terminals: d.Terminals, store: d.Store, bus: d.Bus, sessions: d.Sessions,
		hookURL: d.HookURL,
	}

	api := http.NewServeMux()
	api.HandleFunc("GET /v1/terminals", s.listTerminals)
	api.HandleFunc("POST /v1/terminals", s.openTerminal)
	api.HandleFunc("DELETE /v1/terminals/{id}", s.closeTerminal)
	api.HandleFunc("GET /v1/terminals/{id}/stream", s.streamTerminal)

	api.HandleFunc("GET /v1/sessions", s.listSessions)
	api.HandleFunc("POST /v1/sessions", s.createSession)
	api.HandleFunc("GET /v1/sessions/{id}", s.getSession)
	api.HandleFunc("POST /v1/sessions/{id}/runs", s.startRun)
	api.HandleFunc("POST /v1/sessions/{id}/cancel", s.cancelRun)
	api.HandleFunc("GET /v1/sessions/{id}/events", s.streamEvents)

	mux := http.NewServeMux()
	// Unauthenticated on purpose: it says only that the process is up, for health checks and
	// process supervisors.
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.Handle("/v1/", auth.Require(d.Token, api))
	mux.HandleFunc("POST /hooks/claude", s.claudeHook)
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
