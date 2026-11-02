// Package server is the only way into Orla from outside: the HTTP API and its WebSocket
// streams. Handlers translate HTTP to calls on the core packages and hold no state of their own.
package server

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/Julio0Cesar/orla/internal/auth"
	"github.com/Julio0Cesar/orla/internal/terminal"
)

type Server struct {
	log       *slog.Logger
	terminals *terminal.Manager
}

// New returns the root handler with every route registered.
func New(log *slog.Logger, token string, terminals *terminal.Manager) http.Handler {
	s := &Server{log: log, terminals: terminals}

	api := http.NewServeMux()
	api.HandleFunc("GET /v1/terminals", s.listTerminals)
	api.HandleFunc("POST /v1/terminals", s.openTerminal)
	api.HandleFunc("DELETE /v1/terminals/{id}", s.closeTerminal)
	api.HandleFunc("GET /v1/terminals/{id}/stream", s.streamTerminal)

	mux := http.NewServeMux()
	// Unauthenticated on purpose: it says only that the process is up, for health checks and
	// process supervisors.
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.Handle("/v1/", auth.Require(token, api))
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
