package server

import (
	"encoding/json"
	"net/http"

	"github.com/byjuliocesa/orla/internal/library"
)

// listEnvs answers the environments, and the repositories of this machine's sessions.
func (s *Server) listEnvs(w http.ResponseWriter, r *http.Request) {
	if s.shelf == nil {
		writeError(w, http.StatusNotFound, "This Orla keeps no library.")
		return
	}
	var dirs []string
	if list, err := s.store.Sessions(r.Context()); err == nil {
		for _, x := range list {
			dirs = append(dirs, x.Cwd)
		}
	}
	envs, err := s.shelf.Envs(r.Context(), dirs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, envs)
}

// saveEnv adds or changes an environment, then rewrites its files in the sessions' folders.
func (s *Server) saveEnv(w http.ResponseWriter, r *http.Request) {
	if s.shelf == nil {
		writeError(w, http.StatusNotFound, "This Orla keeps no library.")
		return
	}
	var in library.EnvInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "Send the environment as JSON.")
		return
	}
	if err := s.shelf.SaveEnv(r.Context(), r.PathValue("name"), in); err != nil {
		writeError(w, mcpStatus(err), err.Error())
		return
	}
	s.rewriteEnvs(r)
	s.listEnvs(w, r)
}

func (s *Server) removeEnv(w http.ResponseWriter, r *http.Request) {
	if s.shelf == nil {
		writeError(w, http.StatusNotFound, "This Orla keeps no library.")
		return
	}
	if err := s.shelf.RemoveEnv(r.PathValue("name")); err != nil {
		writeError(w, mcpStatus(err), err.Error())
		return
	}
	s.listEnvs(w, r)
}

// rewriteEnvs brings every session folder's environment files up to date with the library.
func (s *Server) rewriteEnvs(r *http.Request) {
	list, err := s.store.Sessions(r.Context())
	if err != nil {
		return
	}
	seen := map[string]bool{}
	for _, x := range list {
		if seen[x.Cwd] {
			continue
		}
		seen[x.Cwd] = true
		if err := s.shelf.WriteEnvs(r.Context(), x.Cwd); err != nil {
			s.log.Warn("library env", "cwd", x.Cwd, "err", err)
		}
	}
}
