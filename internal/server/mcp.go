package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/byjuliocesa/orla/internal/library"
)

// listMCP answers the library's MCP servers, and the ones Claude has outside it.
func (s *Server) listMCP(w http.ResponseWriter, r *http.Request) {
	if s.shelf == nil {
		writeError(w, http.StatusNotFound, "This Orla keeps no library.")
		return
	}
	list, err := s.shelf.MCP(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// saveMCP adds or changes a server; a header or variable sent empty keeps its saved value.
func (s *Server) saveMCP(w http.ResponseWriter, r *http.Request) {
	if s.shelf == nil {
		writeError(w, http.StatusNotFound, "This Orla keeps no library.")
		return
	}
	var in library.MCPInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "Send the server as JSON.")
		return
	}
	in.Name = r.PathValue("name")
	if err := s.shelf.SaveMCP(r.Context(), in); err != nil {
		writeError(w, mcpStatus(err), err.Error())
		return
	}
	s.listMCP(w, r)
}

// adoptMCP brings one of Claude's own servers into the library.
func (s *Server) adoptMCP(w http.ResponseWriter, r *http.Request) {
	if s.shelf == nil {
		writeError(w, http.StatusNotFound, "This Orla keeps no library.")
		return
	}
	if err := s.shelf.AdoptMCP(r.Context(), r.PathValue("name")); err != nil {
		writeError(w, mcpStatus(err), err.Error())
		return
	}
	s.listMCP(w, r)
}

func (s *Server) removeMCP(w http.ResponseWriter, r *http.Request) {
	if s.shelf == nil {
		writeError(w, http.StatusNotFound, "This Orla keeps no library.")
		return
	}
	if err := s.shelf.RemoveMCP(r.Context(), r.PathValue("name")); err != nil {
		writeError(w, mcpStatus(err), err.Error())
		return
	}
	s.listMCP(w, r)
}

// applyMCPLater sets up the servers once the library's key changed: a machine given the key
// can read the values now, and is listed to receive them.
func (s *Server) applyMCPLater() {
	go func() {
		if err := s.shelf.ApplyMCP(context.Background()); err != nil {
			s.log.Warn("library mcp", "err", err)
		}
	}()
}

func mcpStatus(err error) int {
	if errors.Is(err, library.ErrLocked) {
		return http.StatusConflict
	}
	return http.StatusUnprocessableEntity
}
