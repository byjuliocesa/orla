package library

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Manager keeps the library folder in step with its remote: what changes here is committed and
// pushed, what changed elsewhere is pulled, and the links follow both.
type Manager struct {
	// Root is the library folder.
	Root string
	// Home is the owner's home folder, where the agents look for skills.
	Home string
	// Settings is the file that keeps the owner's choices.
	Settings string
	// Keys holds this machine's own SSH key for the library and the hosts it has met. Empty
	// leaves SSH to the machine's own setup alone.
	Keys string
	Log  *slog.Logger

	op sync.Mutex // one git operation at a time
	mu sync.Mutex // guards st, which Status reads while an operation runs
	st state
}

type state struct {
	syncing   bool
	err       string
	conflicts []string
	last      time.Time
}

// Status is the library as the owner sees it.
type Status struct {
	Dir string `json:"dir"`
	// Remote is the repository it syncs with, without any password in it; empty when none.
	Remote string `json:"remote"`
	Branch string `json:"branch,omitempty"`
	Auto   bool   `json:"auto"`
	// State is off (no remote), synced, pending, syncing, error or conflict.
	State string `json:"state"`
	Error string `json:"error,omitempty"`
	// Conflicts are the files the remote changed too, when State is conflict.
	Conflicts []string   `json:"conflicts,omitempty"`
	LastSync  *time.Time `json:"last_sync,omitempty"`
	// Pending counts the changes not on the remote yet: files changed here and commits not pushed.
	Pending int      `json:"pending"`
	Entries []Entry  `json:"entries"`
	Outside []Entry  `json:"outside"`
	Agents  []agentV `json:"agents"`
	// Sessions are the conversations saved in the library.
	Sessions []SavedSession `json:"sessions"`
}

type agentV struct {
	Agent
	Present bool `json:"present"`
}

// ErrNoRemote is returned by Sync when the library has no remote.
var ErrNoRemote = errors.New("the library is not connected to a repository")

// Status reads the library; it links new entries as it goes.
func (m *Manager) Status(ctx context.Context) (Status, error) {
	s := Status{Dir: m.Root, Auto: readSettings(m.Settings).Auto, State: "off", Entries: []Entry{}, Outside: []Entry{}}
	for _, a := range Agents {
		s.Agents = append(s.Agents, agentV{a, Present(m.Home, a)})
	}
	if err := os.MkdirAll(m.Root, 0o755); err != nil {
		return s, err
	}
	entries, err := Entries(m.Root, m.Home)
	if err != nil {
		return s, err
	}
	if entries != nil {
		s.Entries = entries
	}
	if o := Outside(m.Root, m.Home); o != nil {
		s.Outside = o
	}
	s.Sessions = SavedSessions(m.Root)

	m.mu.Lock()
	st := m.st
	m.mu.Unlock()
	if st.syncing {
		// git holds its lock; asking it now would wait or fail.
		s.State = "syncing"
		return s, nil
	}
	if remote, err := m.git(ctx, "remote", "get-url", "origin"); err == nil {
		s.Remote = redact(remote)
	}
	s.Branch, _ = m.git(ctx, "rev-parse", "--abbrev-ref", "HEAD")
	if s.Remote == "" {
		return s, nil
	}
	s.Pending = m.pending(ctx, s.Branch)
	if !st.last.IsZero() {
		t := st.last
		s.LastSync = &t
	}
	switch {
	case len(st.conflicts) > 0:
		s.State, s.Error, s.Conflicts = "conflict", st.err, st.conflicts
	case st.err != "":
		s.State, s.Error = "error", st.err
	case s.Pending > 0:
		s.State = "pending"
	default:
		s.State = "synced"
	}
	return s, nil
}

// pending counts changed files plus commits the remote lacks.
func (m *Manager) pending(ctx context.Context, branch string) int {
	n := 0
	if out, err := m.git(ctx, "status", "--porcelain"); err == nil && out != "" {
		n += len(strings.Split(out, "\n"))
	}
	if out, err := m.git(ctx, "rev-list", "--count", "origin/"+branch+"..HEAD"); err == nil {
		var c int
		fmt.Sscan(out, &c)
		n += c
	} else if out, err := m.git(ctx, "rev-list", "--count", "HEAD"); err == nil {
		// Never pushed: every commit is pending.
		var c int
		fmt.Sscan(out, &c)
		n += c
	}
	return n
}

// SetAuto turns syncing on its own on or off.
func (m *Manager) SetAuto(on bool) error {
	s := readSettings(m.Settings)
	s.Auto = on
	return writeSettings(m.Settings, s)
}

// Connect ties the library to a remote. An empty remote receives what the library has; a remote
// with history brings it here. Either way this machine's git credentials are the ones used.
func (m *Manager) Connect(ctx context.Context, remote string) error {
	remote = strings.TrimSpace(remote)
	if remote == "" || strings.HasPrefix(remote, "-") || strings.ContainsAny(remote, " \t\r\n") {
		return fmt.Errorf("that is not a repository address")
	}
	m.op.Lock()
	defer m.op.Unlock()

	refs, err := m.gitIn(ctx, "", "ls-remote", "--", remote)
	if err != nil {
		return fmt.Errorf("could not reach %s with this machine's git credentials: %w", redact(remote), err)
	}
	if err := os.MkdirAll(m.Root, 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(m.Root, ".git")); errors.Is(err, os.ErrNotExist) {
		if empty(m.Root) && refs != "" {
			// A fresh machine joining a library that exists: clone into the empty folder.
			if _, err := m.gitIn(ctx, m.Root, "clone", "--", remote, "."); err != nil {
				return err
			}
			m.done(nil)
			_, err = Entries(m.Root, m.Home)
			return err
		}
		if _, err := m.git(ctx, "init", "-b", "main"); err != nil {
			return err
		}
		if refs == "" {
			// Only a new repository gets the layout: one with history brings its own.
			if err := starter(m.Root); err != nil {
				return err
			}
		}
	}
	if _, err := m.git(ctx, "remote", "get-url", "origin"); err == nil {
		_, err = m.git(ctx, "remote", "set-url", "origin", remote)
		if err != nil {
			return err
		}
	} else if _, err := m.git(ctx, "remote", "add", "origin", remote); err != nil {
		return err
	}
	return m.syncLocked(ctx, true)
}

// Disconnect stops syncing; the library and its links stay as they are.
func (m *Manager) Disconnect(ctx context.Context) error {
	m.op.Lock()
	defer m.op.Unlock()
	if _, err := m.git(ctx, "remote", "remove", "origin"); err != nil {
		return err
	}
	m.mu.Lock()
	m.st = state{}
	m.mu.Unlock()
	return nil
}

// Sync commits what changed here, takes what changed on the remote and pushes the result. When
// both sides changed the same lines it stops before touching anything, and reports the files.
func (m *Manager) Sync(ctx context.Context) error {
	m.op.Lock()
	defer m.op.Unlock()
	return m.syncLocked(ctx, false)
}

func (m *Manager) syncLocked(ctx context.Context, first bool) (err error) {
	if _, e := m.git(ctx, "remote", "get-url", "origin"); e != nil {
		return ErrNoRemote
	}
	m.setSyncing(true)
	defer func() { m.setSyncing(false); m.done(err) }()

	if err := m.commit(ctx); err != nil {
		return err
	}
	branch, err := m.git(ctx, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return err
	}
	if _, err := m.git(ctx, "fetch", "origin"); err != nil {
		return err
	}
	if _, err := m.git(ctx, "rev-parse", "--verify", "-q", "origin/"+branch); err == nil {
		args := []string{"merge", "--no-edit", "origin/" + branch}
		if first {
			// The library here and the remote may have started apart.
			args = append(args, "--allow-unrelated-histories")
		}
		if _, err := m.git(ctx, args...); err != nil {
			files, _ := m.git(ctx, "diff", "--name-only", "--diff-filter=U")
			_, _ = m.git(ctx, "merge", "--abort")
			if files != "" {
				return &ConflictError{Files: strings.Split(files, "\n")}
			}
			return err
		}
	}
	if _, err := m.git(ctx, "push", "-u", "origin", "HEAD"); err != nil {
		return err
	}
	_, err = Entries(m.Root, m.Home)
	return err
}

// ConflictError lists the files both sides changed.
type ConflictError struct{ Files []string }

func (e *ConflictError) Error() string {
	return "this machine and the repository both changed " + strings.Join(e.Files, ", ")
}

// commit records every change in the library, named by the entries it touches.
func (m *Manager) commit(ctx context.Context) error {
	if _, err := m.git(ctx, "add", "-A"); err != nil {
		return err
	}
	out, err := m.git(ctx, "diff", "--cached", "--name-only")
	if err != nil || out == "" {
		return err
	}
	seen := map[string]bool{}
	var names []string
	for _, p := range strings.Split(out, "\n") {
		parts := strings.SplitN(p, "/", 3)
		n := parts[0]
		if len(parts) > 1 {
			n = parts[1]
		}
		if !seen[n] {
			seen[n] = true
			names = append(names, n)
		}
	}
	sort.Strings(names)
	msg := "Update " + strings.Join(names, ", ")
	if len(names) > 4 {
		msg = fmt.Sprintf("Update %s and %d more", strings.Join(names[:3], ", "), len(names)-3)
	}
	host, _ := os.Hostname()
	if host != "" {
		msg += "\n\nFrom " + host + "."
	}
	_, err = m.git(ctx, "commit", "-q", "-m", msg)
	return err
}

// Run syncs on its own while auto is on: soon after a change settles, and every few minutes to
// take what other machines pushed. It stops with ctx.
func (m *Manager) Run(ctx context.Context) {
	tick := time.NewTicker(20 * time.Second)
	defer tick.Stop()
	var dirtySince time.Time
	lastPull := time.Time{}
	for {
		if readSettings(m.Settings).Auto && m.connected(ctx) && !m.stuck() {
			dirty := m.dirty(ctx)
			switch {
			case dirty && dirtySince.IsZero():
				dirtySince = time.Now() // let a burst of edits settle first
			case dirty && time.Since(dirtySince) >= 15*time.Second, !dirty && time.Since(lastPull) >= 5*time.Minute:
				if err := m.Sync(ctx); err != nil && m.Log != nil {
					m.Log.Warn("library sync", "err", err)
				}
				dirtySince, lastPull = time.Time{}, time.Now()
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func (m *Manager) connected(ctx context.Context) bool {
	_, err := m.git(ctx, "remote", "get-url", "origin")
	return err == nil
}

func (m *Manager) dirty(ctx context.Context) bool {
	out, err := m.git(ctx, "status", "--porcelain")
	return err == nil && out != ""
}

// stuck is true after a conflict: syncing again would fail the same way until the owner acts.
func (m *Manager) stuck() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.st.conflicts) > 0
}

func (m *Manager) setSyncing(on bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.st.syncing = on
}

func (m *Manager) done(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.st.err, m.st.conflicts = "", nil
	var c *ConflictError
	switch {
	case errors.As(err, &c):
		m.st.err, m.st.conflicts = c.Error(), c.Files
	case err != nil:
		m.st.err = err.Error()
	default:
		m.st.last = time.Now()
	}
}

func (m *Manager) git(ctx context.Context, args ...string) (string, error) {
	return m.gitIn(ctx, m.Root, args...)
}

// gitIn runs git without ever asking for a password: a remote this machine cannot reach fails
// at once with git's own reason, instead of waiting on a prompt nobody sees.
func (m *Manager) gitIn(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		// No ext:: or fd:: transports: an address must not run a command.
		"GIT_ALLOW_PROTOCOL=file:git:http:https:ssh",
		"GCM_INTERACTIVE=never",
	)
	if os.Getenv("GIT_SSH_COMMAND") == "" {
		cmd.Env = append(cmd.Env, "GIT_SSH_COMMAND="+m.sshCommand())
	}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		msg := explain(errb.String(), err)
		if refused(errb.String()) {
			return "", &AccessError{msg}
		}
		return "", errors.New(msg)
	}
	return strings.TrimSpace(out.String()), nil
}

// sshCommand is how git reaches an SSH remote: never asking, trying this machine's library key
// beside its usual ones, and trusting a host the first time it is met (as a person answering
// yes would), while refusing one whose key has changed since.
func (m *Manager) sshCommand() string {
	c := "ssh -o BatchMode=yes -o ConnectTimeout=15"
	if m.Keys == "" {
		return c
	}
	// Hosts met are written to the first file; the machine's own list is read too.
	known := filepath.Join(m.Keys, "known_hosts")
	if home, _ := os.UserHomeDir(); home != "" {
		known += " " + filepath.Join(home, ".ssh", "known_hosts")
	}
	c += " -o StrictHostKeyChecking=accept-new -o " + shellQuote("UserKnownHostsFile="+known)
	if _, err := os.Stat(m.keyPath()); err == nil {
		c += " -i " + shellQuote(m.keyPath())
	}
	return c
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// AccessError is a remote that answered but did not let this machine in: a key or a credential
// fixes it, where a wrong address or a network failure does not.
type AccessError struct{ msg string }

func (e *AccessError) Error() string { return e.msg }

// refused tells a refusal from other failures. A host that hides private repositories (GitHub
// does) answers "not found" to a machine without access, so that counts as one too.
func refused(stderr string) bool {
	for _, s := range []string{"Permission denied", "Authentication failed", "could not read Username", "Host key verification failed", "Repository not found", "does not appear to be a git repository"} {
		if strings.Contains(stderr, s) {
			return true
		}
	}
	return false
}

// explain turns git's stderr into one sentence the owner can act on.
func explain(stderr string, err error) string {
	s := strings.TrimSpace(stderr)
	switch {
	case strings.Contains(s, "Host key verification failed"), strings.Contains(s, "REMOTE HOST IDENTIFICATION HAS CHANGED"):
		return "the repository's host did not prove who it is to this machine"
	case strings.Contains(s, "Please tell me who you are"), strings.Contains(s, "user.email"):
		return "git on this machine has no author yet: set user.name and user.email"
	case strings.Contains(s, "Permission denied"), strings.Contains(s, "Authentication failed"), strings.Contains(s, "could not read Username"):
		return "this machine has no access yet: add its key to the repository, with write access"
	case strings.Contains(s, "not found"), strings.Contains(s, "does not appear to be a git repository"):
		return "no repository answers at that address, or this machine cannot see it"
	case strings.Contains(s, "Could not resolve host"):
		return "the repository's host cannot be reached from this machine"
	case strings.Contains(s, "rejected"):
		return "the repository refused the push; sync again to take its changes first"
	}
	// Otherwise git's first line of substance: its closing advice ("make sure you have the
	// correct access rights and the repository exists") says nothing of what went wrong.
	for _, line := range strings.Split(s, "\n") {
		l := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(line, "fatal: "), "error: "))
		if l == "" || strings.HasPrefix(l, "Could not read from remote") || strings.HasPrefix(l, "Please make sure") || strings.HasPrefix(l, "and the repository exists") {
			continue
		}
		return l
	}
	return err.Error()
}

// redact drops a password or token from an https address.
func redact(remote string) string {
	u, err := url.Parse(remote)
	if err != nil || u.User == nil || u.Scheme == "" {
		return remote
	}
	u.User = nil
	return u.String()
}

func empty(dir string) bool {
	items, err := os.ReadDir(dir)
	return err == nil && len(items) == 0
}

// starter lays out a new library so a clone shows what goes where.
func starter(root string) error {
	for _, d := range append([]string{Shared, Automations, Sessions}, agentNames()...) {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			return err
		}
		keep := filepath.Join(root, d, ".gitkeep")
		if _, err := os.Stat(keep); errors.Is(err, os.ErrNotExist) {
			if err := os.WriteFile(keep, nil, 0o644); err != nil {
				return err
			}
		}
	}
	readme := filepath.Join(root, "README.md")
	if _, err := os.Stat(readme); err == nil {
		return nil
	}
	return os.WriteFile(readme, []byte(`# Orla library

Skills, flows and automations that travel with Orla to every machine.

- shared/: skills every agent loads (Claude Code, Codex…)
- claude/, codex/: skills only that agent loads
- automations/: Orla's automations
- sessions/: conversations saved from Orla, to open on another machine

Connect this repository in Orla, under Library > Sync, on each machine.
`), 0o644)
}

func agentNames() []string {
	var out []string
	for _, a := range Agents {
		out = append(out, a.Name)
	}
	return out
}
