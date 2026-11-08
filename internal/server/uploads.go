package server

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

// maxUpload bounds one attached file. Videos are the large case.
const maxUpload = 512 << 20

// Upload is a file the owner attached, kept on the server where the agent can read it.
type Upload struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
	Size int64  `json:"size"`
	// Path is where the agent reads it.
	Path string `json:"path"`
}

var uploadID = regexp.MustCompile(`^upl_[0-9a-f]{24}$`)

// upload stores one file from a multipart "file" field, each in a folder of its own named by
// a random id, under its own name made safe.
func (s *Server) upload(w http.ResponseWriter, r *http.Request) {
	if s.uploads == "" {
		writeError(w, http.StatusInternalServerError, "this Orla has no folder for uploads")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload+1<<20)
	file, head, err := r.FormFile("file")
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, "the file is larger than 512 MB")
			return
		}
		writeError(w, http.StatusBadRequest, "send the file as the multipart field \"file\"")
		return
	}
	defer file.Close()

	var b [12]byte
	_, _ = rand.Read(b[:])
	id := "upl_" + hex.EncodeToString(b[:])
	dir := filepath.Join(s.uploads, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	name := safeName(head.Filename)
	path := filepath.Join(dir, name)
	out, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	size, err := io.Copy(out, file)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.RemoveAll(dir)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	typ := head.Header.Get("Content-Type")
	if t := mime.TypeByExtension(filepath.Ext(name)); typ == "" || typ == "application/octet-stream" {
		typ = t
	}
	writeJSON(w, http.StatusCreated, Upload{ID: id, Name: name, Type: typ, Size: size, Path: path})
}

// getUpload serves an attached file back, for previews.
func (s *Server) getUpload(w http.ResponseWriter, r *http.Request) {
	path, ok := s.uploadPath(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "no such upload")
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// Shown as an image or offered as a download, never run as a page of Orla's origin.
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'; img-src 'self'; media-src 'self'")
	http.ServeFile(w, r, path)
}

// uploadPath finds the one file of an upload, given its id.
func (s *Server) uploadPath(id string) (string, bool) {
	if s.uploads == "" || !uploadID.MatchString(id) {
		return "", false
	}
	dir := filepath.Join(s.uploads, id)
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || !entries[0].Type().IsRegular() {
		return "", false
	}
	return filepath.Join(dir, entries[0].Name()), true
}

// safeName keeps a file's own name where it can, without separators or a leading dot.
func safeName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == '/' {
			return '_'
		}
		return r
	}, name)
	name = strings.TrimLeft(strings.TrimSpace(name), ".")
	for utf8.RuneCountInString(name) > 120 {
		ext := filepath.Ext(name)
		r := []rune(strings.TrimSuffix(name, ext))
		name = string(r[:len(r)-1]) + ext
	}
	if name == "" {
		name = "file"
	}
	return name
}
