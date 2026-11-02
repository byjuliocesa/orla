// Package auth decides who may use the API. Every route except /health goes through it.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
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
	token := "orla_" + NewSecret()

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

// NewSecret returns 256 random bits, URL-safe.
func NewSecret() string {
	raw := make([]byte, 32)
	_, _ = rand.Read(raw) // never fails: crypto/rand panics rather than return an error
	return base64.RawURLEncoding.EncodeToString(raw)
}

// Equal compares secrets in constant time.
func Equal(got, want string) bool {
	return want != "" && subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}
