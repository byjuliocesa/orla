package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// The library's SSH key lives in Keys: one Orla makes for this machine, or the owner's own, put
// in its place. Only the public half ever leaves the machine.
const (
	keyFile = "key"
	// ownMark says the key is the owner's, not one Orla made.
	ownMark = "key.own"
)

func (m *Manager) keyPath() string { return filepath.Join(m.Keys, keyFile) }

// Key is the library's SSH key as the owner sees it.
type Key struct {
	Public string `json:"key"`
	// Own is true when the owner put their own key in; false for the one Orla made.
	Own bool `json:"own"`
}

// PublicKey answers the library's key, making one the first time: the key the owner adds to
// the repository (on GitHub, as a deploy key with write access) to let this machine in.
func (m *Manager) PublicKey(ctx context.Context) (Key, error) {
	if m.Keys == "" {
		return Key{}, errors.New("this Orla keeps no key of its own")
	}
	_, own := os.Stat(filepath.Join(m.Keys, ownMark))
	if b, err := os.ReadFile(m.keyPath() + ".pub"); err == nil {
		return Key{Public: strings.TrimSpace(string(b)), Own: own == nil}, nil
	}
	if err := os.MkdirAll(m.Keys, 0o700); err != nil {
		return Key{}, err
	}
	host, _ := os.Hostname()
	cmd := exec.CommandContext(ctx, "ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", "orla@"+host, "-f", m.keyPath())
	if out, err := cmd.CombinedOutput(); err != nil {
		return Key{}, fmt.Errorf("ssh-keygen: %s", strings.TrimSpace(string(out)))
	}
	b, err := os.ReadFile(m.keyPath() + ".pub")
	return Key{Public: strings.TrimSpace(string(b))}, err
}

// SetKey puts the owner's own private key in place of the library's, after checking it is one:
// for someone who uses a single key everywhere. A key with a passphrase cannot be used, since
// syncing runs with nobody to type it.
func (m *Manager) SetKey(ctx context.Context, private string) (Key, error) {
	if m.Keys == "" {
		return Key{}, errors.New("this Orla keeps no key of its own")
	}
	private = strings.TrimSpace(strings.ReplaceAll(private, "\r\n", "\n")) + "\n"
	if !strings.Contains(private, "PRIVATE KEY-----") {
		return Key{}, errors.New("that is not a private key: paste the whole file, from its BEGIN line to its END line")
	}
	if err := os.MkdirAll(m.Keys, 0o700); err != nil {
		return Key{}, err
	}
	tmp := m.keyPath() + ".new"
	if err := os.WriteFile(tmp, []byte(private), 0o600); err != nil {
		return Key{}, err
	}
	defer os.Remove(tmp)
	// -P "" refuses a passphrase instead of asking for one.
	out, err := exec.CommandContext(ctx, "ssh-keygen", "-y", "-P", "", "-f", tmp).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "incorrect passphrase") {
			return Key{}, errors.New("that key has a passphrase; use one without, since syncing runs with nobody to type it")
		}
		return Key{}, errors.New("that key does not read: paste the whole file, from its BEGIN line to its END line")
	}
	pub := strings.TrimSpace(string(out))
	if err := os.WriteFile(m.keyPath()+".pub", []byte(pub+"\n"), 0o644); err != nil {
		return Key{}, err
	}
	if err := os.Rename(tmp, m.keyPath()); err != nil {
		return Key{}, err
	}
	if err := os.WriteFile(filepath.Join(m.Keys, ownMark), nil, 0o600); err != nil {
		return Key{}, err
	}
	return Key{Public: pub, Own: true}, nil
}

// RemoveKey deletes the library's key; the next one asked for is made anew.
func (m *Manager) RemoveKey() error {
	if m.Keys == "" {
		return nil
	}
	var errs []error
	for _, f := range []string{keyFile, keyFile + ".pub", ownMark} {
		if err := os.Remove(filepath.Join(m.Keys, f)); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
