package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/Julio0Cesar/orla/internal/auth"
	"github.com/Julio0Cesar/orla/internal/engine/cli"
	"github.com/Julio0Cesar/orla/internal/mcp"
	"github.com/Julio0Cesar/orla/internal/session"
)

// runMCP is the MCP endpoint of one headless run. Its only tool answers the CLI's permission
// prompts by asking the owner. It is authenticated by the run's own secret, so a run can only
// ask for itself, and only while it is in progress.
func (s *Server) runMCP(w http.ResponseWriter, r *http.Request) {
	run := r.PathValue("run")
	want, ok := s.sessions.RunAccess(run)
	got, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || !auth.Equal(got, want) {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	srv := &mcp.Server{Name: "orla", Version: "0.1", Tools: []mcp.Tool{{
		Name:        cli.ApprovalTool,
		Description: "Asks the owner of this Orla installation whether a tool may run.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tool_name":   map[string]any{"type": "string"},
				"input":       map[string]any{"type": "object"},
				"tool_use_id": map[string]any{"type": "string"},
			},
			"required": []string{"tool_name", "input"},
		},
		Call: func(ctx context.Context, args json.RawMessage) (string, bool) {
			var in struct {
				ToolName  string          `json:"tool_name"`
				Input     json.RawMessage `json:"input"`
				ToolUseID string          `json:"tool_use_id"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "invalid arguments: " + err.Error(), true
			}
			d := s.sessions.RequestApproval(ctx, run, in.ToolName, in.Input, in.ToolUseID)
			// The answer format Claude Code expects from a permission prompt tool.
			answer := map[string]any{"behavior": "deny", "message": cli.DenialMessage(d.Message)}
			if d.Allow {
				answer = map[string]any{"behavior": "allow", "updatedInput": in.Input}
			}
			b, _ := json.Marshal(answer)
			return string(b), false
		},
	}}}
	srv.ServeHTTP(w, r)
}

func (s *Server) resolveApproval(w http.ResponseWriter, r *http.Request) {
	var d session.Decision
	if !decode(w, r, &d) {
		return
	}
	if d.By == "" {
		d.By = "api"
	}
	if len(d.By) > 32 || len(d.Message) > 2000 {
		writeError(w, http.StatusBadRequest, "by or message too long")
		return
	}
	err := s.sessions.Resolve(r.Context(), r.PathValue("id"), r.PathValue("approval"), d)
	if errors.Is(err, session.ErrNoApproval) {
		writeError(w, http.StatusNotFound, "no such pending approval: it was answered, expired or never existed")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
