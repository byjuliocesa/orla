// Package server is the only way into Orla from outside: the HTTP API and its WebSocket
// streams. Handlers translate HTTP to calls on the core packages and hold no state of their own.
package server

import (
	"log/slog"
	"net/http"
)

// New returns the root handler with every route registered.
func New(log *slog.Logger) http.Handler {
	mux := http.NewServeMux()

	// Unauthenticated on purpose: it says only that the process is up, for health checks and
	// process supervisors.
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}` + "\n"))
	})

	return mux
}
