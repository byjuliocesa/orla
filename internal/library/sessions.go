package library

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

// Sessions holds the conversations the owner saved, one folder each:
//
//	sessions/<key>/session.json      what it is and where it worked
//	sessions/<key>/transcript.jsonl  the agent's own transcript
//
// Saving is the owner's choice per session: a transcript holds what tools read, which may be
// secret, so nothing is saved on its own.
const Sessions = "sessions"

// SavedSession describes one saved conversation.
type SavedSession struct {
	// Key names its folder. It stays the same when the session is saved again, here or on the
	// machine that opened it, so one conversation keeps one entry.
	Key string `json:"key"`
	// Native is the agent's id for the conversation as last saved.
	Native string `json:"native"`
	Agent  string `json:"agent"`
	Title  string `json:"title"`
	// Cwd is the folder it worked in: the repository, when it was in a worktree.
	Cwd string `json:"cwd"`
	// Repo is the repository's page, which tells the folder apart on another machine.
	Repo   string    `json:"repo,omitempty"`
	Branch string    `json:"branch,omitempty"`
	Host   string    `json:"host"`
	Saved  time.Time `json:"saved"`
	// Here is the session on this machine that holds it, filled in by the server.
	Here string `json:"here,omitempty"`
}

var keyRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,80}$`)

// SaveSession copies a transcript into the library under s.Key, replacing what was there.
func SaveSession(root string, s SavedSession, transcript string) error {
	if !keyRe.MatchString(s.Key) || !keyRe.MatchString(s.Native) {
		return fmt.Errorf("%q is not a conversation id", s.Key)
	}
	dir := filepath.Join(root, Sessions, s.Key)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := copyFile(transcript, filepath.Join(dir, "transcript.jsonl")); err != nil {
		return err
	}
	s.Here = ""
	b, _ := json.MarshalIndent(s, "", "  ")
	return os.WriteFile(filepath.Join(dir, "session.json"), append(b, '\n'), 0o644)
}

// SavedSessions lists what the library holds, the latest saved first.
func SavedSessions(root string) []SavedSession {
	out := []SavedSession{}
	items, _ := os.ReadDir(filepath.Join(root, Sessions))
	for _, it := range items {
		if !it.IsDir() || !keyRe.MatchString(it.Name()) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, Sessions, it.Name(), "session.json"))
		if err != nil {
			continue
		}
		var s SavedSession
		if json.Unmarshal(b, &s) != nil || !keyRe.MatchString(s.Native) {
			continue
		}
		s.Key = it.Name()
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Saved.After(out[j].Saved) })
	return out
}

// Saved returns the saved session of that key.
func Saved(root, key string) (SavedSession, bool) {
	for _, s := range SavedSessions(root) {
		if s.Key == key {
			return s, true
		}
	}
	return SavedSession{}, false
}

// Place puts a saved transcript where Claude Code looks for conversations of cwd, every line's
// cwd set to it, so the agent resumes it in that folder on this machine. It answers the
// transcript's path.
func Place(root, home, key, cwd string) (string, error) {
	s, ok := Saved(root, key)
	if !ok {
		return "", fmt.Errorf("the library has no session %q", key)
	}
	if fi, err := os.Stat(cwd); err != nil || !fi.IsDir() || !filepath.IsAbs(cwd) {
		return "", fmt.Errorf("there is no folder %s on this machine", cwd)
	}
	dir := filepath.Join(home, ".claude", "projects", ProjectDir(cwd))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	dst := filepath.Join(dir, s.Native+".jsonl")
	if _, err := os.Stat(dst); err == nil {
		return dst, nil // already here, as the agent may have grown it since
	}
	in, err := os.Open(filepath.Join(root, Sessions, key, "transcript.jsonl"))
	if err != nil {
		return "", err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	w := bufio.NewWriter(out)
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
	newCwd, _ := json.Marshal(cwd)
	for sc.Scan() {
		line := sc.Bytes()
		var m map[string]json.RawMessage
		if json.Unmarshal(line, &m) == nil {
			if _, has := m["cwd"]; has {
				m["cwd"] = newCwd
				if b, err := json.Marshal(m); err == nil {
					line = b
				}
			}
		}
		w.Write(line)
		w.WriteByte('\n')
	}
	err = errors.Join(sc.Err(), w.Flush(), out.Close())
	if err != nil {
		os.Remove(tmp)
		return "", err
	}
	return dst, os.Rename(tmp, dst)
}

// ProjectDir is the folder name Claude Code keeps a working folder's conversations under:
// the path with every character but letters and digits made a dash.
func ProjectDir(cwd string) string {
	return regexp.MustCompile(`[^A-Za-z0-9]`).ReplaceAllString(cwd, "-")
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}
