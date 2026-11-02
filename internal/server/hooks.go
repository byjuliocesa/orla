package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Julio0Cesar/orla/internal/auth"
	"github.com/Julio0Cesar/orla/internal/engine/cli"
)

// maxHookBody bounds a hook payload. A tool response can be large; it is truncated later anyway.
const maxHookBody = 8 << 20

// claudeHook receives Claude Code's hook events from a CLI running in an Orla terminal. It is
// authenticated by that terminal's hook token, which grants this and nothing else.
func (s *Server) claudeHook(w http.ResponseWriter, r *http.Request) {
	termID := r.Header.Get("X-Orla-Terminal")
	t, ok := s.terminals.Get(termID)
	got, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || !auth.Equal(got, t.HookToken()) {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var h cli.Hook
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxHookBody)).Decode(&h); err != nil {
		writeError(w, http.StatusBadRequest, "invalid hook payload: "+err.Error())
		return
	}
	if err := s.sessions.HandleClaudeHook(r.Context(), termID, h); err != nil {
		s.log.Error("claude hook", "terminal", termID, "event", h.Event, "err", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
