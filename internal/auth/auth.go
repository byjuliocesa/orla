// Package auth decides who may use the API. Every route except /health goes through it.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// LoadOrCreateToken returns the owner token stored at path, creating it on first run. The file is
// readable only by its owner: the token opens a shell on this machine.
func LoadOrCreateToken(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err == nil {
		token := strings.TrimSpace(string(b))
		if token == "" {
			return "", fmt.Errorf("%s is empty; delete it to create a new token", path)
		}
		return token, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := "orla_" + base64.RawURLEncoding.EncodeToString(raw)

	// O_EXCL: two processes starting at once must not each write a different token.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return LoadOrCreateToken(path)
	}
	if err != nil {
		return "", err
	}
	if _, err := f.WriteString(token + "\n"); err != nil {
		f.Close()
		return "", err
	}
	return token, f.Close()
}

// Require lets a request through only with "Authorization: Bearer <token>".
//
// The token is never read from the query string, where it would end up in logs and browser
// history. Browsers cannot set headers on a WebSocket, so the web client will need a cookie;
// that comes with it.
func Require(token string, next http.Handler) http.Handler {
	want := []byte("Bearer " + token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := []byte(r.Header.Get("Authorization"))
		if subtle.ConstantTimeCompare(got, want) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="orla"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
