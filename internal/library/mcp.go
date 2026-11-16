package library

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"filippo.io/age"
	"filippo.io/age/agessh"
	"filippo.io/age/armor"
)

// MCP servers travel in the library like skills, but their keys must not travel in the clear: a
// token committed to a repository sits in every clone and in its history for good. So:
//
//	mcp/servers.json  each server, naming its headers and variables but not their values
//	mcp/secrets.age   the values, encrypted with age to the SSH key of every machine in mcp/machines
//	mcp/machines      those SSH public keys, one per line
//
// The library's SSH key decrypts them, so a machine that has the key reads the keys too. Orla
// writes each server into Claude Code's user config (claude mcp add-json), where Claude finds it
// in Orla and in a terminal alike. Claude keeps the values there in the clear: the encryption
// covers the copy that travels, not the one in use.
const (
	MCPDir      = "mcp"
	mcpServers  = "servers.json"
	mcpSecrets  = "secrets.age"
	mcpMachines = "machines"
)

// MCPServer is one MCP server as the library keeps it.
type MCPServer struct {
	Name string `json:"name"`
	// Type is http, sse or stdio.
	Type    string   `json:"type"`
	URL     string   `json:"url,omitempty"`
	Command string   `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`
	// Headers and Env name the values kept encrypted, such as Authorization.
	Headers []string `json:"headers,omitempty"`
	Env     []string `json:"env,omitempty"`
}

// MCPInput is a server to save. An empty value in Headers or Env keeps the one saved before.
type MCPInput struct {
	Name    string            `json:"name"`
	Type    string            `json:"type"`
	URL     string            `json:"url,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// MCPView is a library server and how it stands on this machine.
type MCPView struct {
	MCPServer
	// State is ready (Claude has it), locked (this machine cannot read its values yet), taken
	// (Claude has another server by that name, set up outside Orla) or error.
	State string `json:"state"`
	Error string `json:"error,omitempty"`
}

// MCPList is what the MCP tab shows.
type MCPList struct {
	Servers []MCPView `json:"servers"`
	// Outside are the servers Claude Code has that the library does not: they can be brought in.
	Outside []MCPServer `json:"outside"`
	// Locked is true when the library has values this machine's key cannot read.
	Locked bool `json:"locked"`
}

type mcpValues struct {
	Headers map[string]string `json:"headers,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// sealed is what secrets.age holds. For lists the keys it was encrypted to, so a machine that
// can read it notices a new machine and encrypts it again for everyone.
type sealed struct {
	For     []string             `json:"for"`
	Servers map[string]mcpValues `json:"servers"`
}

var mcpName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

// ErrLocked is returned when the library's values cannot be read here.
var ErrLocked = errors.New("this machine cannot read the library's MCP keys yet; another machine that can will open them to it once it syncs")

func (m *Manager) mcpPath(f string) string { return filepath.Join(m.Root, MCPDir, f) }

func (m *Manager) readServers() ([]MCPServer, error) {
	b, err := os.ReadFile(m.mcpPath(mcpServers))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var f struct {
		Servers []MCPServer `json:"servers"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Join(MCPDir, mcpServers), err)
	}
	return f.Servers, nil
}

func (m *Manager) writeServers(list []MCPServer) error {
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	b, _ := json.MarshalIndent(struct {
		Servers []MCPServer `json:"servers"`
	}{list}, "", "  ")
	return writeFile(m.mcpPath(mcpServers), append(b, '\n'), 0o644)
}

func (m *Manager) readMachines() []string {
	b, _ := os.ReadFile(m.mcpPath(mcpMachines))
	var out []string
	for _, l := range strings.Split(string(b), "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") && !slices.Contains(out, l) {
			out = append(out, l)
		}
	}
	return out
}

// own is this machine's public key, without its comment; empty when the library has none.
func (m *Manager) own() string {
	k, err := m.PublicKey()
	if err != nil {
		return ""
	}
	f := strings.Fields(k.Public)
	if len(f) < 2 {
		return ""
	}
	return f[0] + " " + f[1]
}

// openSecrets reads secrets.age. ok is false when there are values this machine cannot read.
func (m *Manager) openSecrets() (s sealed, ok bool, err error) {
	s.Servers = map[string]mcpValues{}
	b, err := os.ReadFile(m.mcpPath(mcpSecrets))
	if errors.Is(err, os.ErrNotExist) {
		return s, true, nil
	}
	if err != nil {
		return s, false, err
	}
	if m.Keys == "" {
		return s, false, nil
	}
	pem, err := os.ReadFile(m.keyPath())
	if err != nil {
		return s, false, nil
	}
	id, err := agessh.ParseIdentity(pem)
	if err != nil {
		return s, false, fmt.Errorf("the library's key cannot decrypt: %w", err)
	}
	r, err := age.Decrypt(armor.NewReader(bytes.NewReader(b)), id)
	if err != nil {
		return s, false, nil
	}
	plain, err := io.ReadAll(r)
	if err != nil {
		return s, false, err
	}
	if err := json.Unmarshal(plain, &s); err != nil {
		return s, false, err
	}
	if s.Servers == nil {
		s.Servers = map[string]mcpValues{}
	}
	return s, true, nil
}

// seal encrypts the values to every machine.
func (m *Manager) seal(values map[string]mcpValues) error {
	machines := m.readMachines()
	if len(values) == 0 {
		err := os.Remove(m.mcpPath(mcpSecrets))
		if errors.Is(err, os.ErrNotExist) {
			err = nil
		}
		return err
	}
	var to []age.Recipient
	for _, k := range machines {
		r, err := agessh.ParseRecipient(k)
		if err != nil {
			return fmt.Errorf("%s: %w", filepath.Join(MCPDir, mcpMachines), err)
		}
		to = append(to, r)
	}
	if len(to) == 0 {
		return errors.New("the library has no key to encrypt with; set one in Library › Sync")
	}
	plain, _ := json.Marshal(sealed{For: machines, Servers: values})
	var out bytes.Buffer
	a := armor.NewWriter(&out)
	w, err := age.Encrypt(a, to...)
	if err != nil {
		return err
	}
	if _, err := w.Write(plain); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	if err := a.Close(); err != nil {
		return err
	}
	return writeFile(m.mcpPath(mcpSecrets), out.Bytes(), 0o644)
}

// enroll adds this machine's key to mcp/machines, so the values get encrypted to it too.
func (m *Manager) enroll() error {
	own := m.own()
	if own == "" || slices.Contains(m.readMachines(), own) {
		return nil
	}
	b, _ := os.ReadFile(m.mcpPath(mcpMachines))
	if len(b) > 0 && b[len(b)-1] != '\n' {
		b = append(b, '\n')
	}
	host, _ := os.Hostname()
	return writeFile(m.mcpPath(mcpMachines), append(b, []byte(own+" "+host+"\n")...), 0o644)
}

// MCP lists the library's servers and those Claude has outside it.
func (m *Manager) MCP(ctx context.Context) (MCPList, error) {
	m.mcpMu.Lock()
	defer m.mcpMu.Unlock()
	return m.mcpList()
}

func (m *Manager) mcpList() (MCPList, error) {
	out := MCPList{Servers: []MCPView{}, Outside: []MCPServer{}}
	list, err := m.readServers()
	if err != nil {
		return out, err
	}
	_, ok, err := m.openSecrets()
	if err != nil {
		return out, err
	}
	out.Locked = !ok
	claude := m.claudeServers()
	applied := m.readApplied()
	for _, s := range list {
		v := MCPView{MCPServer: s, State: "ready"}
		switch {
		case !ok && (len(s.Headers) > 0 || len(s.Env) > 0):
			v.State = "locked"
		case claude[s.Name] != nil && applied[s.Name] == "":
			v.State = "taken"
		case m.mcpErrs[s.Name] != "":
			v.State, v.Error = "error", m.mcpErrs[s.Name]
		}
		out.Servers = append(out.Servers, v)
	}
	names := make([]string, 0, len(claude))
	for n := range claude {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if _, mine := applied[n]; mine || slices.ContainsFunc(list, func(s MCPServer) bool { return s.Name == n }) {
			continue
		}
		s, _ := fromClaude(n, claude[n])
		out.Outside = append(out.Outside, s.server())
	}
	return out, nil
}

// SaveMCP adds a server to the library, or changes one, and sets it up in Claude.
func (m *Manager) SaveMCP(ctx context.Context, in MCPInput) error {
	if !mcpName.MatchString(in.Name) {
		return errors.New("a name uses letters, digits, - and _ only, up to 64")
	}
	switch in.Type {
	case "http", "sse":
		if !strings.HasPrefix(in.URL, "https://") && !strings.HasPrefix(in.URL, "http://") {
			return errors.New("the URL starts with https://")
		}
		in.Command, in.Args, in.Env = "", nil, nil
	case "stdio":
		if strings.TrimSpace(in.Command) == "" {
			return errors.New("a command is needed")
		}
		in.URL, in.Headers = "", nil
	default:
		return errors.New("the type is http, sse or stdio")
	}
	m.mcpMu.Lock()
	defer m.mcpMu.Unlock()
	if m.own() == "" {
		return errors.New("the library has no key yet; set one in Library › Sync")
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
	old := sec.Servers[in.Name]
	vals := mcpValues{Headers: merge(old.Headers, in.Headers), Env: merge(old.Env, in.Env)}
	if len(vals.Headers)+len(vals.Env) == 0 {
		delete(sec.Servers, in.Name)
	} else {
		sec.Servers[in.Name] = vals
	}
	list, err := m.readServers()
	if err != nil {
		return err
	}
	list = slices.DeleteFunc(list, func(s MCPServer) bool { return s.Name == in.Name })
	list = append(list, MCPServer{Name: in.Name, Type: in.Type, URL: in.URL, Command: in.Command, Args: in.Args, Headers: keys(vals.Headers), Env: keys(vals.Env)})
	if err := m.writeServers(list); err != nil {
		return err
	}
	if err := m.seal(sec.Servers); err != nil {
		return err
	}
	return m.applyMCP(ctx)
}

// AdoptMCP brings a server Claude has into the library, values and all.
func (m *Manager) AdoptMCP(ctx context.Context, name string) error {
	m.mcpMu.Lock()
	raw := m.claudeServers()[name]
	m.mcpMu.Unlock()
	if raw == nil {
		return fmt.Errorf("Claude has no server named %s", name)
	}
	in, err := fromClaude(name, raw)
	if err != nil {
		return err
	}
	m.mcpMu.Lock()
	applied := m.readApplied()
	applied[name] = "adopted" // it is Orla's to rewrite now
	err = m.writeApplied(applied)
	m.mcpMu.Unlock()
	if err != nil {
		return err
	}
	return m.SaveMCP(ctx, in)
}

// RemoveMCP takes a server out of the library, and out of Claude.
func (m *Manager) RemoveMCP(ctx context.Context, name string) error {
	m.mcpMu.Lock()
	defer m.mcpMu.Unlock()
	list, err := m.readServers()
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(list, func(s MCPServer) bool { return s.Name == name }) {
		return fmt.Errorf("the library has no server named %s", name)
	}
	sec, ok, err := m.openSecrets()
	if err != nil {
		return err
	}
	if _, has := sec.Servers[name]; has || !ok {
		if !ok {
			return ErrLocked
		}
		delete(sec.Servers, name)
		if err := m.seal(sec.Servers); err != nil {
			return err
		}
	}
	if err := m.writeServers(slices.DeleteFunc(list, func(s MCPServer) bool { return s.Name == name })); err != nil {
		return err
	}
	return m.applyMCP(ctx)
}

// ApplyMCP brings Claude's config in step with the library: it runs at start and after each
// sync, so what another machine changed arrives here.
func (m *Manager) ApplyMCP(ctx context.Context) error {
	m.mcpMu.Lock()
	defer m.mcpMu.Unlock()
	if err := m.enroll(); err != nil {
		return err
	}
	return m.applyMCP(ctx)
}

func (m *Manager) applyMCP(ctx context.Context) error {
	if m.Claude == nil {
		return nil
	}
	list, err := m.readServers()
	if err != nil {
		return err
	}
	sec, ok, err := m.openSecrets()
	if err != nil {
		return err
	}
	// A machine that can read the values opens them to the machines that joined since.
	if ok && len(sec.Servers) > 0 && !slices.Equal(sec.For, m.readMachines()) {
		if err := m.seal(sec.Servers); err != nil {
			return err
		}
	}
	claude := m.claudeServers()
	applied := m.readApplied()
	errs := map[string]string{}
	for _, s := range list {
		vals, has := sec.Servers[s.Name]
		if (len(s.Headers) > 0 || len(s.Env) > 0) && !has {
			continue // locked here, or the values went missing
		}
		if claude[s.Name] != nil && applied[s.Name] == "" {
			continue // someone else's server by that name; the owner decides
		}
		cfg := s.config(vals)
		b, _ := json.Marshal(cfg)
		sum := sha256.Sum256(b)
		h := hex.EncodeToString(sum[:])
		if applied[s.Name] == h && claude[s.Name] != nil {
			continue
		}
		if claude[s.Name] != nil {
			if err := m.claudeMCP(ctx, "remove", "-s", "user", s.Name); err != nil {
				errs[s.Name] = err.Error()
				continue
			}
		}
		if err := m.claudeMCP(ctx, "add-json", "-s", "user", s.Name, string(b)); err != nil {
			errs[s.Name] = err.Error()
			continue
		}
		applied[s.Name] = h
	}
	for n := range applied {
		if slices.ContainsFunc(list, func(s MCPServer) bool { return s.Name == n }) {
			continue
		}
		if claude[n] != nil {
			if err := m.claudeMCP(ctx, "remove", "-s", "user", n); err != nil {
				errs[n] = err.Error()
				continue
			}
		}
		delete(applied, n)
	}
	m.mcpErrs = errs
	return m.writeApplied(applied)
}

func (m *Manager) claudeMCP(ctx context.Context, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, m.Claude(), append([]string{"mcp"}, args...)...).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("claude mcp %s: %s", args[0], msg)
	}
	return nil
}

// claudeServers reads the servers in Claude Code's user config.
func (m *Manager) claudeServers() map[string]json.RawMessage {
	b, err := os.ReadFile(filepath.Join(m.Home, ".claude.json"))
	if err != nil {
		return nil
	}
	var f struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	_ = json.Unmarshal(b, &f)
	return f.MCPServers
}

// applied remembers which of Claude's servers Orla wrote, with a hash of what it wrote, so it
// changes or removes only its own.
func (m *Manager) appliedPath() string {
	return filepath.Join(filepath.Dir(m.Settings), "mcp-applied.json")
}

func (m *Manager) readApplied() map[string]string {
	out := map[string]string{}
	b, err := os.ReadFile(m.appliedPath())
	if err == nil {
		_ = json.Unmarshal(b, &out)
	}
	return out
}

func (m *Manager) writeApplied(a map[string]string) error {
	b, _ := json.MarshalIndent(a, "", "  ")
	return writeFile(m.appliedPath(), b, 0o600)
}

func (s MCPServer) config(v mcpValues) map[string]any {
	cfg := map[string]any{"type": s.Type}
	if s.Type == "stdio" {
		cfg["command"] = s.Command
		cfg["args"] = append([]string{}, s.Args...)
		if len(v.Env) > 0 {
			cfg["env"] = v.Env
		}
		return cfg
	}
	cfg["url"] = s.URL
	if len(v.Headers) > 0 {
		cfg["headers"] = v.Headers
	}
	return cfg
}

func (in MCPInput) server() MCPServer {
	return MCPServer{Name: in.Name, Type: in.Type, URL: in.URL, Command: in.Command, Args: in.Args, Headers: keys(in.Headers), Env: keys(in.Env)}
}

// fromClaude reads one entry of Claude's config.
func fromClaude(name string, raw json.RawMessage) (MCPInput, error) {
	var c struct {
		Type    string            `json:"type"`
		URL     string            `json:"url"`
		Command string            `json:"command"`
		Args    []string          `json:"args"`
		Headers map[string]string `json:"headers"`
		Env     map[string]string `json:"env"`
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		return MCPInput{}, fmt.Errorf("Claude's entry for %s does not read: %w", name, err)
	}
	if c.Type == "" {
		c.Type = "stdio"
		if c.URL != "" {
			c.Type = "http"
		}
	}
	return MCPInput{Name: name, Type: c.Type, URL: c.URL, Command: c.Command, Args: c.Args, Headers: c.Headers, Env: c.Env}, nil
}

func merge(old, in map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range in {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		if v == "" {
			if o, ok := old[k]; ok {
				out[k] = o
			}
			continue
		}
		out[k] = v
	}
	return out
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil
	}
	return out
}

// writeFile replaces a file whole, so a reader never sees half of it.
func writeFile(path string, b []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
