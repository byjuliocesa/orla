package server

import (
	"encoding/json"
	"io"
	"net/http"
	"regexp"
)

var prefKey = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// maxPref bounds one preference: an arrangement of a few hundred names, not a store of files.
const maxPref = 256 << 10

// getPref answers {"value": ...}, null when the owner never set it.
func (s *Server) getPref(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if !prefKey.MatchString(key) {
		writeError(w, http.StatusBadRequest, "That is not a preference name.")
		return
	}
	v, err := s.store.Pref(r.Context(), key)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if v == nil {
		v = json.RawMessage("null")
	}
	writeJSON(w, http.StatusOK, map[string]any{"value": v})
}

// putPref keeps the body, any JSON value, under the key.
func (s *Server) putPref(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if !prefKey.MatchString(key) {
		writeError(w, http.StatusBadRequest, "That is not a preference name.")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxPref+1))
	if err != nil || len(body) > maxPref {
		writeError(w, http.StatusRequestEntityTooLarge, "That preference is too large to keep.")
		return
	}
	if !json.Valid(body) {
		writeError(w, http.StatusBadRequest, "A preference is kept as JSON.")
		return
	}
	if err := s.store.SetPref(r.Context(), key, body); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"value": json.RawMessage(body)})
}
