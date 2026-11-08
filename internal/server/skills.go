package server

import (
	"bufio"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/byjuliocesa/orla/internal/flow"
)

// Skill is a Claude Code skill the owner can tie to a session's buttons, such as Create PR.
type Skill struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// Scope is where it comes from, as Claude Code says: user, project, plugin, built-in.
	Scope string `json:"scope"`
	// Tokens is what it takes in the context while it is on.
	Tokens int64 `json:"tokens"`
	// Flow is set for flow skills, which the Flow tab shows instead.
	Flow bool `json:"flow,omitempty"`
	// Orla is set for Orla's own commands, such as /newflow: typed, never arranged.
	Orla bool `json:"orla,omitempty"`
}

// skills lists every skill Claude Code would load for a session started in ?cwd=: the owner's,
// the project's, plugins' and the bundled ones, each with the tokens it takes in the context
// (from /context, which costs nothing) and, when its SKILL.md is at hand, its description.
func (s *Server) skills(w http.ResponseWriter, r *http.Request) {
	cwd := r.URL.Query().Get("cwd")
	home, _ := os.UserHomeDir()
	if !filepath.IsAbs(cwd) {
		cwd = home
	}
	descs := map[string]string{}
	orla := map[string]bool{} // Orla's own skills, such as /newflow, are not the owner's to arrange
	// Skills on disk, some of which /context leaves out (those only typed, never loaded).
	onDisk := map[string]string{}
	var order []string
	for i, dir := range []string{filepath.Join(home, ".claude", "skills"), filepath.Join(cwd, ".claude", "skills")} {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			p := filepath.Join(dir, e.Name(), "SKILL.md")
			name, desc := skillHead(p)
			if name != "" && desc != "" {
				descs[name] = desc
			}
			if name != "" && flow.Kind(p) == "system" {
				orla[name] = true
			}
			if _, err := os.Stat(p); err == nil && name != "" {
				if _, seen := onDisk[name]; !seen {
					order = append(order, name)
				}
				onDisk[name] = []string{"user", "project"}[i]
			}
		}
	}
	flows := map[string]bool{}
	for _, f := range flow.List(home, cwd) {
		flows[f.Name] = true
	}
	var out []Skill
	if s.library != nil {
		s.libMu.Lock()
		c, ok := s.libraries[cwd]
		stale := ok && time.Since(c.at) > 5*time.Minute && !c.refreshing
		if stale {
			// The list kept answers now; a fresh one replaces it in the background.
			c.refreshing = true
			s.libraries[cwd] = c
		}
		s.libMu.Unlock()
		if stale {
			go s.refreshLibrary(cwd)
		}
		if !ok || r.URL.Query().Get("fresh") != "" {
			ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
			defer cancel()
			parts, err := s.library(ctx, cwd)
			if err != nil {
				writeError(w, http.StatusBadGateway, "Claude Code did not list its skills: "+err.Error())
				return
			}
			c = cachedLibrary{at: time.Now(), skills: parts}
			s.libMu.Lock()
			s.libraries[cwd] = c
			s.libMu.Unlock()
		}
		listed := map[string]bool{}
		for _, p := range c.skills {
			listed[p.Name] = true
			out = append(out, Skill{Name: p.Name, Description: descs[p.Name], Scope: strings.ToLower(p.Detail), Tokens: p.Tokens, Flow: flows[p.Name], Orla: orla[p.Name]})
		}
		for _, name := range order {
			if !listed[name] {
				out = append(out, Skill{Name: name, Description: descs[name], Scope: onDisk[name], Flow: flows[name], Orla: orla[name]})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	if out == nil {
		out = []Skill{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"skills": out})
}

// refreshLibrary asks Claude Code for the folder's skills again and keeps the answer; on failure
// the old list stays and the next request tries again.
func (s *Server) refreshLibrary(cwd string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	parts, err := s.library(ctx, cwd)
	s.libMu.Lock()
	defer s.libMu.Unlock()
	c := s.libraries[cwd]
	c.refreshing = false
	if err == nil {
		c = cachedLibrary{at: time.Now(), skills: parts}
	}
	s.libraries[cwd] = c
}

// skillHead reads name and description from a SKILL.md's front matter. A skill with no name
// there is named after its folder; a missing file gives "".
func skillHead(path string) (name, desc string) {
	f, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for i := 0; sc.Scan() && i < 40; i++ {
		line := sc.Text()
		if i == 0 && line != "---" {
			break
		}
		if i > 0 && line == "---" {
			break
		}
		if k, v, ok := strings.Cut(line, ":"); ok {
			v = strings.Trim(strings.TrimSpace(v), `"'`)
			switch strings.TrimSpace(k) {
			case "name":
				name = v
			case "description":
				if len(v) > 220 {
					v = v[:217] + "…"
				}
				desc = v
			}
		}
	}
	if name == "" {
		name = filepath.Base(filepath.Dir(path))
	}
	return name, desc
}
