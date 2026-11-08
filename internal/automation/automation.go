// Package automation runs the owner's automations: scripts Orla starts, watches and logs, so
// the work they do (watching, validating, approving by a rule) costs no model tokens. Each one
// is a folder with an AUTOMATION.md that says what to run and when:
//
//	---
//	name: approve-design
//	description: Approves Claude Design prototype reviews in this session.
//	run: python3 approve.py
//	scope: session          # session (runs with a session, knows it) or global
//	trigger: always         # always | every 5m | on approval.requested
//	restart: on-failure     # on-failure | never
//	metadata:
//	  title: Approve design
//	---
//
// The scripts beside it are its own. The AI writes and changes automations; the owner turns
// them on and off.
package automation

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/byjuliocesa/orla/internal/flow"
)

// Automation is one automation, read from its folder.
type Automation struct {
	Name        string   `json:"name"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Run         string   `json:"run"`
	Scope       string   `json:"scope"`
	Trigger     Trigger  `json:"trigger"`
	Restart     string   `json:"restart"`
	Dir         string   `json:"dir"`
	Path        string   `json:"path"`
	Body        string   `json:"body,omitempty"`
	Files       []string `json:"files"`
	// Problem says why it cannot run as written, if it cannot.
	Problem  string    `json:"problem,omitempty"`
	Modified time.Time `json:"modified"`
}

// Trigger is when an automation runs.
type Trigger struct {
	// Kind is always, every or on.
	Kind  string        `json:"kind"`
	Every time.Duration `json:"every,omitempty"`
	// On is the event type that runs it, such as approval.requested; a trailing * matches a
	// family (run.*).
	On string `json:"on,omitempty"`
	// Text is the trigger as written.
	Text string `json:"text"`
}

// Root is where automations live: beside the flows, for the AI to write and read.
func Root(home string) string { return filepath.Join(home, ".claude", "orla-automations") }

// List reads every automation; folders that do not read are skipped.
func List(home string) []Automation {
	var out []Automation
	entries, _ := os.ReadDir(Root(home))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if a, ok := Read(filepath.Join(Root(home), e.Name(), "AUTOMATION.md")); ok {
			out = append(out, a)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return strings.ToLower(out[i].Title) < strings.ToLower(out[j].Title) })
	return out
}

// Find returns the automation of that name.
func Find(home, name string) (Automation, bool) {
	for _, a := range List(home) {
		if a.Name == name {
			return a, true
		}
	}
	return Automation{}, false
}

// Read reads one AUTOMATION.md; ok is false when there is no such file.
func Read(path string) (Automation, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Automation{}, false
	}
	front, body := flow.Front(string(b))
	dir := filepath.Dir(path)
	a := Automation{
		Name:        first(front["name"], filepath.Base(dir)),
		Description: front["description"],
		Run:         front["run"],
		Scope:       first(bare(front["scope"]), "session"),
		Restart:     first(bare(front["restart"]), "on-failure"),
		Dir:         dir,
		Path:        path,
		Body:        strings.TrimSpace(body),
	}
	a.Title = first(front["metadata.title"], front["title"], a.Name)
	if info, err := os.Stat(path); err == nil {
		a.Modified = info.ModTime()
	}
	t, err := ParseTrigger(first(bare(front["trigger"]), "always"))
	a.Trigger = t
	switch {
	case err != nil:
		a.Problem = err.Error()
	case strings.TrimSpace(a.Run) == "":
		a.Problem = "it says nothing to run (run:)"
	case a.Scope != "session" && a.Scope != "global":
		a.Problem = fmt.Sprintf("scope %q is neither session nor global", a.Scope)
	}
	a.Files = files(dir)
	return a, true
}

// ParseTrigger reads "always", "every 5m" or "on approval.requested".
func ParseTrigger(text string) (Trigger, error) {
	t := Trigger{Text: strings.TrimSpace(text)}
	f := strings.Fields(strings.ToLower(t.Text))
	switch {
	case len(f) == 1 && f[0] == "always":
		t.Kind = "always"
	case len(f) == 2 && f[0] == "every":
		d, err := time.ParseDuration(f[1])
		if err != nil || d < 10*time.Second {
			return t, fmt.Errorf("trigger %q: every takes a duration of 10s or more, such as 5m", t.Text)
		}
		t.Kind, t.Every = "every", d
	case len(f) == 2 && f[0] == "on":
		t.Kind, t.On = "on", f[1]
	default:
		return t, fmt.Errorf("trigger %q is none of always, every <duration>, on <event>", t.Text)
	}
	return t, nil
}

// Matches says whether an event type sets off an "on" trigger.
func (t Trigger) Matches(event string) bool {
	if t.Kind != "on" {
		return false
	}
	if p, ok := strings.CutSuffix(t.On, "*"); ok {
		return strings.HasPrefix(event, p)
	}
	return event == t.On
}

func files(dir string) []string {
	out := []string{}
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || len(out) >= 100 {
			return filepath.SkipDir
		}
		if d.IsDir() {
			if p != dir && (strings.HasPrefix(d.Name(), ".") || d.Name() == "__pycache__" || d.Name() == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if rel, _ := filepath.Rel(dir, p); rel != "AUTOMATION.md" {
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	return out
}

func first(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// bare drops a trailing "# note" from a front matter value.
func bare(v string) string {
	if i := strings.Index(v, " #"); i >= 0 {
		v = v[:i]
	}
	return strings.TrimSpace(v)
}
