package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/byjuliocesa/orla/internal/library"
)

// libraryStatus answers what the library holds, what agents load from outside it, and how it
// stands with its repository.
func (s *Server) libraryStatus(w http.ResponseWriter, r *http.Request) {
	if s.shelf == nil {
		writeError(w, http.StatusNotFound, "This Orla keeps no library.")
		return
	}
	st, err := s.shelf.Status(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// librarySettings takes {"auto": bool}: whether the library syncs on its own.
func (s *Server) librarySettings(w http.ResponseWriter, r *http.Request) {
	if s.shelf == nil {
		writeError(w, http.StatusNotFound, "This Orla keeps no library.")
		return
	}
	var body struct {
		Auto *bool `json:"auto"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Auto == nil {
		writeError(w, http.StatusBadRequest, `Send {"auto": true} or {"auto": false}.`)
		return
	}
	if err := s.shelf.SetAuto(*body.Auto); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.libraryStatus(w, r)
}

// libraryAction runs connect {remote}, sync, disconnect, adopt {from, name, to} or
// move {from, name, to}, then answers the library as it stands.
func (s *Server) libraryAction(w http.ResponseWriter, r *http.Request) {
	if s.shelf == nil {
		writeError(w, http.StatusNotFound, "This Orla keeps no library.")
		return
	}
	var body struct {
		Remote string `json:"remote"`
		From   string `json:"from"`
		Name   string `json:"name"`
		To     string `json:"to"`
	}
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "The body is not JSON.")
			return
		}
	}
	m := s.shelf
	var err error
	switch r.PathValue("action") {
	case "connect":
		err = m.Connect(r.Context(), body.Remote)
	case "sync":
		err = m.Sync(r.Context())
	case "disconnect":
		err = m.Disconnect(r.Context())
	case "adopt":
		err = library.Adopt(m.Root, m.Home, body.From, body.Name, body.To)
	case "move":
		err = library.Move(m.Root, m.Home, body.From, body.Name, body.To)
	default:
		writeError(w, http.StatusNotFound, "The library does not do that.")
		return
	}
	var conflict *library.ConflictError
	switch {
	case errors.As(err, &conflict), errors.Is(err, library.ErrNoRemote):
		writeError(w, http.StatusConflict, err.Error())
		return
	case err != nil:
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	s.libraryStatus(w, r)
}
