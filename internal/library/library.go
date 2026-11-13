// Package library keeps the owner's skills, flows and automations in one folder that is a git
// repository, so they travel to every machine that runs Orla. Each coding agent finds them where
// it already looks, through links:
//
//	library/
//	  shared/<name>/       every agent's skills folder (~/.claude/skills, ~/.codex/skills…)
//	  claude/<name>/       Claude Code's only
//	  codex/<name>/        Codex's only
//	  automations/<name>/  Orla's automations (~/.claude/orla-automations)
//
// Any git remote works: the library uses the credentials this machine's git already has (an SSH
// key, a credential helper, gh auth) and keeps none of its own.
package library

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Agent is a coding agent whose skills folder the library links into.
type Agent struct {
	Name  string `json:"name"`
	Title string `json:"title"`
	// Dir is its user skills folder, under the home folder.
	Dir string `json:"dir"`
}

// Agents are the agents the library knows. An agent is linked only once its own folder exists
// (~/.codex for Codex), so installing the library does not invent agents the machine lacks.
var Agents = []Agent{
	{Name: "claude", Title: "Claude Code", Dir: ".claude/skills"},
	{Name: "codex", Title: "Codex", Dir: ".codex/skills"},
}

const (
	// Shared holds what every agent loads.
	Shared = "shared"
	// Automations holds Orla's automations.
	Automations = "automations"
)

// Entry is one skill, flow or automation.
type Entry struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// Kind is skill, flow or automation.
	Kind string `json:"kind"`
	// Place is shared, an agent's name, or automations.
	Place string `json:"place"`
	// Linked lists the agents (or "orla", for automations) that load it now.
	Linked []string `json:"linked"`
	// Blocked lists where another folder of the same name keeps it from being linked.
	Blocked []string `json:"blocked,omitempty"`
}

func agentDir(home string, a Agent) string { return filepath.Join(home, filepath.FromSlash(a.Dir)) }

// agentHome is the folder whose presence says the agent is installed: ~/.codex for Codex.
func agentHome(home string, a Agent) string {
	return filepath.Join(home, strings.SplitN(filepath.ToSlash(a.Dir), "/", 2)[0])
}

func automationsDir(home string) string { return filepath.Join(home, ".claude", "orla-automations") }

// Present reports whether the agent is installed on this machine.
func Present(home string, a Agent) bool {
	_, err := os.Stat(agentHome(home, a))
	return err == nil
}

// places are the library's folders, with where each one is linked.
func places(root, home string) []struct {
	name    string
	targets map[string]string // who loads it → the folder its links go in
} {
	type place = struct {
		name    string
		targets map[string]string
	}
	var out []place
	shared := place{Shared, map[string]string{}}
	for _, a := range Agents {
		if Present(home, a) {
			shared.targets[a.Name] = agentDir(home, a)
			out = append(out, place{a.Name, map[string]string{a.Name: agentDir(home, a)}})
		} else {
			out = append(out, place{a.Name, map[string]string{}})
		}
	}
	out = append([]place{shared}, out...)
	return append(out, place{Automations, map[string]string{"orla": automationsDir(home)}})
}

// Entries reads every entry in the library and links what is not linked yet. A folder of the
// same name already in an agent's folder is left alone and reported as Blocked; a link that
// points into the library at something gone is removed.
func Entries(root, home string) ([]Entry, error) {
	var out []Entry
	for _, p := range places(root, home) {
		dir := filepath.Join(root, p.name)
		items, err := os.ReadDir(dir)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		for _, it := range items {
			if !it.IsDir() || strings.HasPrefix(it.Name(), ".") {
				continue
			}
			src := filepath.Join(dir, it.Name())
			e := describe(src, p.name)
			e.Linked = []string{}
			who := make([]string, 0, len(p.targets))
			for w := range p.targets {
				who = append(who, w)
			}
			sort.Strings(who)
			for _, w := range who {
				switch err := link(src, filepath.Join(p.targets[w], it.Name())); err {
				case nil:
					e.Linked = append(e.Linked, w)
				case errTaken:
					e.Blocked = append(e.Blocked, w)
				default:
					return nil, err
				}
			}
			out = append(out, e)
		}
	}
	prune(root, home)
	return out, nil
}

var errTaken = errors.New("taken")

// link makes dst a link to src, unless something else is already there.
func link(src, dst string) error {
	if cur, err := os.Readlink(dst); err == nil {
		if cur == src {
			return nil
		}
		return errTaken
	}
	if _, err := os.Lstat(dst); err == nil {
		return errTaken
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.Symlink(src, dst)
}

// prune removes links into the library whose folder is gone: deleted here, or by a pull.
func prune(root, home string) {
	dirs := []string{automationsDir(home)}
	for _, a := range Agents {
		dirs = append(dirs, agentDir(home, a))
	}
	for _, d := range dirs {
		items, _ := os.ReadDir(d)
		for _, it := range items {
			p := filepath.Join(d, it.Name())
			to, err := os.Readlink(p)
			if err != nil || !strings.HasPrefix(to, root+string(filepath.Separator)) {
				continue
			}
			if _, err := os.Stat(to); errors.Is(err, os.ErrNotExist) {
				_ = os.Remove(p)
			}
		}
	}
}

// Outside lists what agents load that is not in the library yet: real folders in their skills
// folders and in Orla's automations, which Adopt can move in. Orla's own commands, such as
// /newflow, which Orla installs itself, are left out.
func Outside(root, home string) []Entry {
	var out []Entry
	scan := func(dir, place string) {
		items, _ := os.ReadDir(dir)
		for _, it := range items {
			if !it.IsDir() || strings.HasPrefix(it.Name(), ".") {
				continue // links, the library's among them, and files
			}
			e := describe(filepath.Join(dir, it.Name()), place)
			if e.Kind == "system" {
				continue
			}
			out = append(out, e)
		}
	}
	for _, a := range Agents {
		scan(agentDir(home, a), a.Name)
	}
	scan(automationsDir(home), Automations)
	return out
}

// Adopt moves a folder an agent loads into the library, at place (shared or an agent's own,
// automations for automations), and links it back.
func Adopt(root, home, from, name, place string) error {
	if !clean(name) {
		return fmt.Errorf("%q is not a folder name", name)
	}
	var src string
	switch {
	case from == Automations:
		src = filepath.Join(automationsDir(home), name)
		place = Automations
	default:
		a, ok := agent(from)
		if !ok {
			return fmt.Errorf("no agent is called %q", from)
		}
		src = filepath.Join(agentDir(home, a), name)
		if place != Shared && place != a.Name {
			return fmt.Errorf("a skill of %s goes in shared or %s", a.Title, a.Name)
		}
	}
	fi, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return fmt.Errorf("%s is not a folder of its own", src)
	}
	dst := filepath.Join(root, place, name)
	if _, err := os.Lstat(dst); err == nil {
		return fmt.Errorf("the library already has %s in %s", name, place)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err != nil {
		return err
	}
	_, err = Entries(root, home)
	return err
}

// Move puts an entry in another place: from one agent's to shared, or back.
func Move(root, home, from, name, to string) error {
	if !clean(name) || from == Automations || to == Automations {
		return fmt.Errorf("%s cannot move there", name)
	}
	for _, p := range []string{from, to} {
		if _, ok := agent(p); !ok && p != Shared {
			return fmt.Errorf("the library has no %q", p)
		}
	}
	src, dst := filepath.Join(root, from, name), filepath.Join(root, to, name)
	if _, err := os.Lstat(dst); err == nil {
		return fmt.Errorf("the library already has %s in %s", name, to)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err != nil {
		return err
	}
	prune(root, home)
	_, err := Entries(root, home)
	return err
}

func agent(name string) (Agent, bool) {
	for _, a := range Agents {
		if a.Name == name {
			return a, true
		}
	}
	return Agent{}, false
}

func clean(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, `/\`) && !strings.HasPrefix(name, ".")
}

// describe reads a folder's SKILL.md or AUTOMATION.md head.
func describe(dir, place string) Entry {
	e := Entry{Name: filepath.Base(dir), Place: place, Kind: "skill"}
	file := "SKILL.md"
	if place == Automations {
		e.Kind, file = "automation", "AUTOMATION.md"
	}
	f, err := os.Open(filepath.Join(dir, file))
	if err != nil {
		return e
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for i := 0; sc.Scan() && i < 60; i++ {
		line := sc.Text()
		if (i == 0 && line != "---") || (i > 0 && line == "---") {
			break
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		switch strings.TrimSpace(k) {
		case "description":
			if r := []rune(v); len(r) > 200 {
				v = string(r[:197]) + "…"
			}
			e.Description = v
		case "orla":
			if v == "flow" || v == "system" {
				e.Kind = v
			}
		}
	}
	return e
}

// settings are what the owner chose, kept beside the library.
type settings struct {
	Auto bool `json:"auto"`
}

func readSettings(path string) settings {
	s := settings{Auto: true}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	return s
}

func writeSettings(path string, s settings) error {
	b, _ := json.Marshal(s)
	return os.WriteFile(path, b, 0o600)
}
