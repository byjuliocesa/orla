package library

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// Environments are named sets of variables, each linked to the repositories that use it. Like the
// MCP servers, the library keeps their names and keys in the clear and their values sealed:
//
//	env/environments.json  each environment: its keys and the repositories it is linked to
//	mcp/secrets.age        the values, beside the MCP servers' own
//
// When a session runs in a checkout of a linked repository, Orla writes the file there (.env, or
// the path the link names), so the app finds its variables as it always has.
const (
	EnvDir   = "env"
	envsFile = "environments.json"
	// envMark opens every file Orla writes, so it never overwrites one the owner made.
	envMark = "# Written by Orla from the library's environments. Edit them there; changes here are overwritten."
)

// Environment is one named set of variables.
type Environment struct {
	Name string `json:"name"`
	// Keys name the variables; their values are sealed.
	Keys  []string  `json:"keys"`
	Links []EnvLink `json:"links"`
}

// EnvLink puts an environment in a repository's checkouts.
type EnvLink struct {
	// Repo is the repository as host/owner/name, the same on every machine.
	Repo string `json:"repo"`
	// Path is the file, from the checkout's root: .env unless it says otherwise.
	Path string `json:"path"`
}

// EnvInput is an environment to save. Values holds every variable it keeps: one sent empty keeps
// the value saved before, and one left out is removed.
type EnvInput struct {
	Values map[string]string `json:"values"`
	Links  []EnvLink         `json:"links"`
}

// EnvList is what the Environments tab shows.
type EnvList struct {
	Environments []Environment `json:"environments"`
	// Repos are the repositories of this machine's sessions, to link without typing.
	Repos  []string `json:"repos"`
	Locked bool     `json:"locked"`
}

var (
	envName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)
	envKey  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

func (m *Manager) envPath() string { return filepath.Join(m.Root, EnvDir, envsFile) }

func (m *Manager) readEnvs() ([]Environment, error) {
	b, err := os.ReadFile(m.envPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var f struct {
		Environments []Environment `json:"environments"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Join(EnvDir, envsFile), err)
	}
	return f.Environments, nil
}

func (m *Manager) writeEnvs(list []Environment) error {
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	b, _ := json.MarshalIndent(struct {
		Environments []Environment `json:"environments"`
	}{list}, "", "  ")
	return writeFile(m.envPath(), append(b, '\n'), 0o644)
}

// Envs lists the environments. repos are the folders sessions work in, offered for linking.
func (m *Manager) Envs(ctx context.Context, dirs []string) (EnvList, error) {
	m.mcpMu.Lock()
	defer m.mcpMu.Unlock()
	out := EnvList{Environments: []Environment{}, Repos: []string{}}
	list, err := m.readEnvs()
	if err != nil {
		return out, err
	}
	if list != nil {
		out.Environments = list
	}
	_, ok, err := m.openSecrets()
	if err != nil {
		return out, err
	}
	out.Locked = !ok
	for _, d := range dirs {
		if r := repoOf(ctx, d); r != "" && !slices.Contains(out.Repos, r) {
			out.Repos = append(out.Repos, r)
		}
	}
	sort.Strings(out.Repos)
	return out, nil
}

// SaveEnv adds an environment or changes one.
func (m *Manager) SaveEnv(ctx context.Context, name string, in EnvInput) error {
	if !envName.MatchString(name) {
		return errors.New("a name uses letters, digits, ., - and _ only, up to 64")
	}
	for k := range in.Values {
		if !envKey.MatchString(k) {
			return fmt.Errorf("%q is not a variable name: letters, digits and _, not starting with a digit", k)
		}
	}
	links := []EnvLink{}
	for _, l := range in.Links {
		repo := normalizeRepo(l.Repo)
		if repo == "" {
			return fmt.Errorf("%q is not a repository; use host/owner/name, like github.com/you/app", l.Repo)
		}
		p := filepath.ToSlash(filepath.Clean(strings.TrimSpace(l.Path)))
		if l.Path == "" {
			p = ".env"
		}
		if filepath.IsAbs(p) || p == "." || strings.HasPrefix(p, "../") || p == ".." || strings.HasPrefix(p, ".git/") {
			return fmt.Errorf("%q must be a file inside the repository", l.Path)
		}
		if !slices.Contains(links, EnvLink{repo, p}) {
			links = append(links, EnvLink{repo, p})
		}
	}
	m.mcpMu.Lock()
	defer m.mcpMu.Unlock()
	if m.own() == "" {
		return errors.New("the library has no key yet; set one in the library's sync menu")
	}
	if err := m.enroll(); err != nil {
		return err
	}
	sec, ok, err := m.openSecrets()
	if err != nil {
		return err
	}
	if !ok {
		return ErrLocked
	}
	vals := merge(sec.Envs[name], in.Values)
	if len(vals) == 0 {
		delete(sec.Envs, name)
	} else {
		sec.Envs[name] = vals
	}
	list, err := m.readEnvs()
	if err != nil {
		return err
	}
	list = slices.DeleteFunc(list, func(e Environment) bool { return e.Name == name })
	list = append(list, Environment{Name: name, Keys: keys(vals), Links: links})
	if err := m.writeEnvs(list); err != nil {
		return err
	}
	return m.seal(sec)
}

// RemoveEnv deletes an environment. The files it wrote stay until their checkout's next run.
func (m *Manager) RemoveEnv(name string) error {
	m.mcpMu.Lock()
	defer m.mcpMu.Unlock()
	list, err := m.readEnvs()
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(list, func(e Environment) bool { return e.Name == name }) {
		return fmt.Errorf("the library has no environment named %s", name)
	}
	sec, ok, err := m.openSecrets()
	if err != nil {
		return err
	}
	if _, has := sec.Envs[name]; has || !ok {
		if !ok {
			return ErrLocked
		}
		delete(sec.Envs, name)
		if err := m.seal(sec); err != nil {
			return err
		}
	}
	return m.writeEnvs(slices.DeleteFunc(list, func(e Environment) bool { return e.Name == name }))
}

// WriteEnvs writes the environments linked to dir's repository into dir, a checkout or a
// worktree of it. Several environments for one file merge; where they share a key, the one
// linked to fewer repositories, the more specific, wins. A file Orla did not write is left
// alone, and every file it writes is kept out of git.
func (m *Manager) WriteEnvs(ctx context.Context, dir string) error {
	repo := repoOf(ctx, dir)
	if repo == "" {
		return nil
	}
	m.mcpMu.Lock()
	defer m.mcpMu.Unlock()
	list, err := m.readEnvs()
	if err != nil || len(list) == 0 {
		return err
	}
	sec, ok, err := m.openSecrets()
	if err != nil || !ok {
		return err
	}
	byPath := map[string][]Environment{}
	for _, e := range list {
		for _, l := range e.Links {
			if l.Repo == repo {
				byPath[l.Path] = append(byPath[l.Path], e)
			}
		}
	}
	root, err := gitOut(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return err
	}
	var errs []error
	for path, envs := range byPath {
		// The more repositories an environment serves, the more general: it goes first.
		sort.SliceStable(envs, func(i, j int) bool { return len(envs[i].Links) > len(envs[j].Links) })
		vals := map[string]string{}
		var from []string
		for _, e := range envs {
			for k, v := range sec.Envs[e.Name] {
				vals[k] = v
			}
			from = append(from, e.Name)
		}
		file := filepath.Join(root, filepath.FromSlash(path))
		if b, err := os.ReadFile(file); err == nil && !bytes.HasPrefix(b, []byte(envMark)) {
			errs = append(errs, fmt.Errorf("%s exists and was not written by Orla; left alone", file))
			continue
		}
		var buf strings.Builder
		buf.WriteString(envMark + "\n# From: " + strings.Join(from, ", ") + "\n")
		for _, k := range keys(vals) {
			buf.WriteString(k + "=" + quoteEnv(vals[k]) + "\n")
		}
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			errs = append(errs, err)
			continue
		}
		if err := os.WriteFile(file, []byte(buf.String()), 0o600); err != nil {
			errs = append(errs, err)
			continue
		}
		if err := ignore(ctx, root, path); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// ignore keeps a written file out of git, in this clone's own exclude list: the repository's
// .gitignore is the team's, and stays as it is.
func ignore(ctx context.Context, root, path string) error {
	if _, err := gitOut(ctx, root, "check-ignore", "-q", path); err == nil {
		return nil
	}
	exclude, err := gitOut(ctx, root, "rev-parse", "--git-path", "info/exclude")
	if err != nil {
		return err
	}
	if !filepath.IsAbs(exclude) {
		exclude = filepath.Join(root, exclude)
	}
	if err := os.MkdirAll(filepath.Dir(exclude), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(exclude, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString("/" + path + "\n")
	return err
}

// quoteEnv writes a value the way dotenv readers take it back: bare when it is plain, in double
// quotes otherwise.
func quoteEnv(v string) string {
	if v != "" && !strings.ContainsAny(v, " \t\n\"'#$\\`=") {
		return v
	}
	return strconv.Quote(v)
}

// repoOf names dir's repository by its origin, as host/owner/name; empty when it has none.
func repoOf(ctx context.Context, dir string) string {
	u, err := gitOut(ctx, dir, "remote", "get-url", "origin")
	if err != nil {
		return ""
	}
	return normalizeRepo(u)
}

// normalizeRepo turns any form of a repository's address into host/owner/name.
func normalizeRepo(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(strings.TrimSuffix(s, "/"), ".git")
	for _, p := range []string{"https://", "http://", "ssh://", "git://"} {
		s = strings.TrimPrefix(s, p)
	}
	if at := strings.Index(s, "@"); at >= 0 && at < strings.IndexAny(s+"/", ":/") {
		s = s[at+1:]
	}
	// scp form, host:owner/name; a port in an ssh:// address goes too.
	if i := strings.Index(s, ":"); i >= 0 {
		rest := s[i+1:]
		if j := strings.Index(rest, "/"); j >= 0 && isPort(rest[:j]) {
			rest = rest[j+1:]
		}
		s = s[:i] + "/" + rest
	}
	parts := strings.Split(s, "/")
	if len(parts) < 3 || slices.Contains(parts, "") {
		return ""
	}
	parts[0] = strings.ToLower(parts[0])
	// ssh.github.com and ssh.bitbucket.org are the same hosts as github.com and bitbucket.org,
	// reached over SSH on another name.
	for _, p := range []string{"ssh.", "altssh."} {
		if h := strings.TrimPrefix(parts[0], p); h != parts[0] && strings.Contains(h, ".") {
			parts[0] = h
		}
	}
	return strings.Join(parts, "/")
}

func isPort(s string) bool {
	_, err := strconv.Atoi(s)
	return err == nil
}

func gitOut(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}
