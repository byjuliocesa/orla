package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/byjuliocesa/orla/internal/engine/cli"
	"github.com/byjuliocesa/orla/internal/git"
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
	// A saved session is here when a session on this machine holds its conversation.
	if len(st.Sessions) > 0 {
		local, _ := s.store.Sessions(r.Context())
		for i, saved := range st.Sessions {
			for _, x := range local {
				if x.NativeSession == saved.Native || x.ForkOf == saved.Native || x.ForkOf == saved.Key {
					st.Sessions[i].Here = x.ID
					break
				}
			}
		}
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
		// Key and Cwd open a saved session here, in that folder.
		Key string `json:"key"`
		Cwd string `json:"cwd"`
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
	case "open":
		s.openSaved(w, r, body.Key, body.Cwd)
		return
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

// openSaved makes a session on this machine from one saved in the library, working in cwd.
func (s *Server) openSaved(w http.ResponseWriter, r *http.Request, key, cwd string) {
	saved, ok := library.Saved(s.shelf.Root, key)
	if !ok {
		writeError(w, http.StatusNotFound, "The library has no such session.")
		return
	}
	if _, err := library.Place(s.shelf.Root, s.shelf.Home, key, filepath.Clean(cwd)); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	x, err := s.sessions.Import(r.Context(), saved.Native, saved.Title)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.announce(r.Context(), x, x.Title)
	writeJSON(w, http.StatusCreated, s.view(r.Context(), x))
}

// saveSession copies a session's conversation into the library, for another machine to open.
func (s *Server) saveSession(w http.ResponseWriter, r *http.Request) {
	x, ok := s.lookup(w, r)
	if !ok {
		return
	}
	if s.shelf == nil {
		writeError(w, http.StatusNotFound, "This Orla keeps no library.")
		return
	}
	native := x.NativeSession
	if native == "" {
		native = x.ForkOf // opened from the library, or forked, and not run since
	}
	if native == "" {
		writeError(w, http.StatusConflict, "This session has no conversation yet.")
		return
	}
	transcript := cli.Transcript(native)
	if transcript == "" {
		writeError(w, http.StatusConflict, "Claude Code keeps no transcript of this conversation.")
		return
	}
	// One conversation keeps one entry: a session opened from the library saves back into it.
	key := native
	for _, saved := range library.SavedSessions(s.shelf.Root) {
		if saved.Native == native || (x.ForkOf != "" && (saved.Native == x.ForkOf || saved.Key == x.ForkOf)) {
			key = saved.Key
			break
		}
	}
	host, _ := os.Hostname()
	saved := library.SavedSession{Key: key, Native: native, Agent: x.Agent, Title: x.Title, Cwd: x.Cwd, Host: host, Saved: time.Now().UTC()}
	if repo, err := git.Open(r.Context(), x.Cwd); err == nil {
		saved.Cwd, saved.Repo, saved.Branch = repo.Main, repo.Web, repo.Branch
	}
	if saved.Title == "" {
		saved.Title = filepath.Base(saved.Cwd)
	}
	if err := library.SaveSession(s.shelf.Root, saved, transcript); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, saved)
}
