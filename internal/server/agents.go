package server

import (
	"context"
	"net/http"
	"sync"

	"github.com/byjuliocesa/orla/internal/engine/cli"
)

// Agent is a coding agent Orla can run, and how to set it up on this machine.
type Agent struct {
	ID, Name string
	Status   func(ctx context.Context) cli.Status
	// Install and Login are the commands, as argv, that a terminal runs to set the agent up.
	// Login is asked each time: after an install the executable is somewhere new.
	Install []string
	Login   func() []string
}

type agentView struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Install []string `json:"install"`
	Login   []string `json:"login"`
	cli.Status
}

func (s *Server) listAgents(w http.ResponseWriter, r *http.Request) {
	out := make([]agentView, len(s.agents))
	var wg sync.WaitGroup
	for i, a := range s.agents {
		out[i] = agentView{ID: a.ID, Name: a.Name, Install: a.Install, Login: a.Login()}
		wg.Add(1)
		go func() {
			defer wg.Done()
			out[i].Status = a.Status(r.Context())
		}()
	}
	wg.Wait()
	writeJSON(w, http.StatusOK, map[string]any{"agents": out})
}
