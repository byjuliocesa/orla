// Package flow reads Orla flows: Claude Code skills that describe, step by step, how a kind of
// task is done. A flow is
// an ordinary skill folder; its SKILL.md says it is a flow in its metadata and lists the steps
// as "## " headings, so Claude Code follows it like any skill and Orla can draw it.
//
//	---
//	name: release
//	description: Use when asked to cut a release...
//	metadata:
//	  orla: flow
//	  title: Release
//	---
//
//	## Tag
//	uses: changelog
//	signal: Bash(git tag*)
//	branch: build if the checks fail
//	file: steps/tag.md
//
//	Tag the version...
package flow

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

// Flow is one flow skill, read from its folder.
type Flow struct {
	// Name is the skill's name, the one Claude Code knows it by.
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
	// Scope is user (~/.claude/skills) or project (<cwd>/.claude/skills).
	Scope string `json:"scope"`
	Dir   string `json:"dir"`
	Path  string `json:"path"`
	// Intro is what comes before the first step: rules for the whole flow.
	Intro    string    `json:"intro,omitempty"`
	Steps    []Step    `json:"steps"`
	Files    []string  `json:"files"`
	Modified time.Time `json:"modified"`
}

// Step is one "## " section of the flow.
type Step struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Uses are other skills the step follows, such as a PR pattern shared by several flows.
	Uses []string `json:"uses"`
	// Signals are the tool calls that show the step is being done, such as Bash(gh pr create*)
	// or Skill(changelog); Orla lights the step when one is seen, at no cost in tokens.
	Signals []string `json:"signals"`
	// Branches lead elsewhere than the next step, under a condition.
	Branches []Branch `json:"branches"`
	// Next is where the step goes when it is done, when that is not the step written below it:
	// another step's id, or "end". Steps only reached by a branch say it, so they stand apart.
	Next string `json:"next,omitempty"`
	// File holds the step's longer instructions, read by Claude only when it gets there.
	File     string `json:"file,omitempty"`
	FileBody string `json:"file_body,omitempty"`
	Body     string `json:"body"`
}

// Branch is a jump to another step, such as back to the changes when the pipeline fails.
type Branch struct {
	To   string `json:"to"`
	When string `json:"when,omitempty"`
}

const maxFile = 64 << 10

// List reads every flow Claude Code would see from cwd: the owner's, then the project's.
// Folders that are not flows, or that do not read, are skipped.
func List(home, cwd string) []Flow {
	var out []Flow
	seen := map[string]bool{}
	for _, root := range roots(home, cwd) {
		entries, _ := os.ReadDir(root.dir)
		for _, e := range entries {
			if !e.IsDir() && e.Type()&os.ModeSymlink == 0 {
				continue
			}
			f, ok := Read(filepath.Join(root.dir, e.Name(), "SKILL.md"))
			if !ok || seen[f.Name] {
				continue
			}
			seen[f.Name] = true
			f.Scope = root.scope
			out = append(out, f)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return strings.ToLower(out[i].Title) < strings.ToLower(out[j].Title) })
	return out
}

// Off adds to off every flow the session has not taken, so a session only ever follows its own.
func Off(home, cwd string, taken, off []string) []string {
	keep := map[string]bool{}
	for _, n := range taken {
		keep[n] = true
	}
	out := append([]string(nil), off...)
	has := map[string]bool{}
	for _, n := range off {
		has[n] = true
	}
	for _, f := range List(home, cwd) {
		if !keep[f.Name] && !has[f.Name] {
			out = append(out, f.Name)
		}
	}
	return out
}

type root struct{ dir, scope string }

func roots(home, cwd string) []root {
	rs := []root{{filepath.Join(home, ".claude", "skills"), "user"}}
	if cwd != "" && filepath.Clean(cwd) != filepath.Clean(home) {
		rs = append(rs, root{filepath.Join(cwd, ".claude", "skills"), "project"})
	}
	return rs
}

// Read reads one SKILL.md; ok is false when it does not read or is not a flow.
func Read(path string) (Flow, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Flow{}, false
	}
	info, _ := os.Stat(path)
	front, body := split(string(b))
	if front["metadata.orla"] != "flow" {
		return Flow{}, false
	}
	dir := filepath.Dir(path)
	f := Flow{
		Name:        first(front["name"], filepath.Base(dir)),
		Description: front["description"],
		Dir:         dir,
		Path:        path,
	}
	if info != nil {
		f.Modified = info.ModTime()
	}
	f.Title = first(front["metadata.title"], f.Name)
	f.Intro, f.Steps = steps(body)
	for i := range f.Steps {
		s := &f.Steps[i]
		if s.File != "" {
			if p, ok := inside(dir, s.File); ok {
				if fb, err := os.ReadFile(p); err == nil && len(fb) <= maxFile {
					s.FileBody = string(fb)
				}
			}
		}
		s.Signals = commandSignals(s.Signals, s.Body+"\n"+s.FileBody)
	}
	f.Files = files(dir)
	return f, true
}

// Front reads a SKILL.md-like file's front matter, nested keys as "parent.key", and its body.
func Front(text string) (map[string]string, string) { return split(text) }

// split parses the front matter: top-level "key: value", folded or literal blocks ("> " / "|"),
// and one level of nesting, which comes out as "parent.key".
func split(text string) (map[string]string, string) {
	out := map[string]string{}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return out, text
	}
	end := strings.Index(text[4:], "\n---")
	if end < 0 {
		return out, text
	}
	head, body := text[4:4+end], text[4+end+4:]
	body = strings.TrimPrefix(body, "\n")
	parent, block, blockKey := "", false, ""
	var buf []string
	flush := func() {
		if block && blockKey != "" {
			out[blockKey] = strings.TrimSpace(strings.Join(buf, " "))
		}
		block, blockKey, buf = false, "", nil
	}
	for _, line := range strings.Split(head, "\n") {
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		trim := strings.TrimSpace(line)
		if block {
			if indent > 0 || trim == "" {
				buf = append(buf, trim)
				continue
			}
			flush()
		}
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		k, v, ok := strings.Cut(trim, ":")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		key := k
		if indent > 0 && parent != "" {
			key = parent + "." + k
		} else {
			parent = ""
			if v == "" {
				parent = k
				continue
			}
		}
		if v == ">" || v == "|" || v == ">-" || v == "|-" {
			block, blockKey = true, key
			continue
		}
		out[key] = unquote(v)
	}
	flush()
	return out, body
}

var (
	heading = regexp.MustCompile(`^##\s+(.+?)\s*#*\s*$`)
	number  = regexp.MustCompile(`^\d+[.)]\s+`)
	field   = regexp.MustCompile(`^(?i)(uses|usa|signal|signals|sinal|sinais|branch|desvio|file|arquivo|id|next|proximo|próximo)\s*:\s*(.+)$`)
)

// steps cuts the body at its "## " headings. The lines right under a heading that read
// "key: value" describe the step; the rest is what Claude reads to do it.
func steps(body string) (string, []Step) {
	var intro []string
	var out []Step
	var cur *Step
	var text []string
	inFields, fence := false, false
	done := func() {
		if cur != nil {
			cur.Body = strings.TrimSpace(strings.Join(text, "\n"))
			out = append(out, *cur)
		}
		text = nil
	}
	sc := bufio.NewScanner(strings.NewReader(body))
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fence = !fence
		}
		if m := heading.FindStringSubmatch(line); m != nil && !fence {
			done()
			title := number.ReplaceAllString(m[1], "")
			cur = &Step{Title: title, ID: Slug(title), Uses: []string{}, Signals: []string{}, Branches: []Branch{}}
			inFields = true
			continue
		}
		if cur == nil {
			if !strings.HasPrefix(line, "# ") {
				intro = append(intro, line)
			}
			continue
		}
		if inFields {
			if strings.TrimSpace(line) == "" && len(text) == 0 {
				continue
			}
			if m := field.FindStringSubmatch(strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "-*"))); m != nil {
				addField(cur, strings.ToLower(m[1]), strings.TrimSpace(m[2]))
				continue
			}
			inFields = false
		}
		text = append(text, line)
	}
	done()
	// A branch may name its step by title; it is kept by id.
	ids := map[string]string{}
	for _, s := range out {
		ids[s.ID] = s.ID
		ids[Slug(s.Title)] = s.ID
	}
	for i := range out {
		for j := range out[i].Branches {
			if id, ok := ids[Slug(out[i].Branches[j].To)]; ok {
				out[i].Branches[j].To = id
			}
		}
		if id, ok := ids[out[i].Next]; ok {
			out[i].Next = id
		}
	}
	return strings.TrimSpace(strings.Join(intro, "\n")), out
}

var when = regexp.MustCompile(`(?i)\s+(if|when|se|quando)\s+`)

func addField(s *Step, k, v string) {
	list := func() []string {
		var out []string
		for _, p := range strings.Split(v, ",") {
			if p = strings.Trim(strings.TrimSpace(p), "`"); p != "" {
				out = append(out, p)
			}
		}
		return out
	}
	switch k {
	case "uses", "usa":
		s.Uses = append(s.Uses, list()...)
	case "signal", "signals", "sinal", "sinais":
		s.Signals = append(s.Signals, list()...)
	case "branch", "desvio":
		to, cond := v, ""
		if loc := when.FindStringIndex(v); loc != nil {
			to, cond = v[:loc[0]], strings.TrimSpace(v[loc[0]:])
		}
		s.Branches = append(s.Branches, Branch{To: strings.Trim(strings.TrimSpace(to), "`"), When: cond})
	case "file", "arquivo":
		s.File = strings.Trim(v, "`")
	case "id":
		s.ID = Slug(v)
	case "next", "proximo", "próximo":
		s.Next = Slug(strings.Trim(v, "`"))
		if s.Next == "fim" || s.Next == "end" {
			s.Next = "end"
		}
	}
}

// Slug is a step's id: lower case, plain letters and digits joined by dashes.
func Slug(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		if plain, ok := accents[r]; ok {
			r = plain
		}
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			dash = false
		default:
			if !dash && b.Len() > 0 {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

var accents = func() map[rune]rune {
	m := map[rune]rune{}
	for plain, marked := range map[rune]string{'a': "áàâãäå", 'e': "éèêë", 'i': "íìîï", 'o': "óòôõö", 'u': "úùûü", 'c': "ç", 'n': "ñ"} {
		for _, r := range marked {
			m[r] = plain
		}
	}
	return m
}()

// inside resolves rel under dir, refusing anything that leaves it.
func inside(dir, rel string) (string, bool) {
	p := filepath.Clean(filepath.Join(dir, rel))
	r, err := filepath.Rel(dir, p)
	if err != nil || r == ".." || strings.HasPrefix(r, "../") {
		return "", false
	}
	return p, true
}

// files lists what the flow's folder holds besides SKILL.md: steps, scripts, templates.
func files(dir string) []string {
	out := []string{}
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || len(out) >= 200 {
			return filepath.SkipDir
		}
		if d.IsDir() {
			if p != dir && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		if rel != "SKILL.md" {
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

// ArchivedStep is a step taken out of a flow by /archiveflow, kept to be put back.
type ArchivedStep struct {
	Flow string `json:"flow"`
	Step Step   `json:"step"`
	Path string `json:"path"`
}

// Archive is where /archiveflow keeps what it takes out: whole flows, and single steps.
func Archive(home string) string { return filepath.Join(home, ".claude", "orla-archive") }

// Archived reads the archived flows, and the steps archived from any flow.
func Archived(home string) ([]Flow, []ArchivedStep) {
	flows := []Flow{}
	root := filepath.Join(Archive(home), "flows")
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		if f, ok := Read(filepath.Join(root, e.Name(), "SKILL.md")); ok {
			f.Scope = "archived"
			flows = append(flows, f)
		}
	}
	sort.SliceStable(flows, func(i, j int) bool { return strings.ToLower(flows[i].Title) < strings.ToLower(flows[j].Title) })
	kept := []ArchivedStep{}
	root = filepath.Join(Archive(home), "steps")
	owners, _ := os.ReadDir(root)
	for _, o := range owners {
		files, _ := os.ReadDir(filepath.Join(root, o.Name()))
		for _, f := range files {
			p := filepath.Join(root, o.Name(), f.Name())
			b, err := os.ReadFile(p)
			if err != nil || len(b) > maxFile {
				continue
			}
			if _, list := steps(string(b)); len(list) > 0 {
				kept = append(kept, ArchivedStep{Flow: o.Name(), Step: list[0], Path: p})
			}
		}
	}
	return flows, kept
}

var (
	codeSpan = regexp.MustCompile("`([^`\n]+)`")
	command  = regexp.MustCompile(`^[a-z][a-z0-9._-]*(\s|$)`)
)

// Tools whose first word says little: the second names what is done (git status, gh pr).
var twoWords = map[string]bool{"git": true, "gh": true, "glab": true, "npm": true, "pnpm": true, "yarn": true, "go": true, "cargo": true, "docker": true, "kubectl": true, "make": false}

// commandSignals adds, to the signals a step states, the commands its text names in backticks:
// a step that says "Run `git log --oneline -5`" is seen when git log runs, with nothing more to
// write. A step that runs nothing is seen by its "▶ step" line instead.
func commandSignals(signals []string, text string) []string {
	have := map[string]bool{}
	for _, s := range signals {
		have[s] = true
	}
	for _, m := range codeSpan.FindAllStringSubmatch(text, -1) {
		cmd := strings.TrimSpace(m[1])
		for _, sep := range []string{"|", "&&", ";", "||"} {
			cmd, _, _ = strings.Cut(cmd, sep)
		}
		cmd = strings.TrimSpace(cmd)
		if !command.MatchString(cmd) {
			continue
		}
		words := strings.Fields(cmd)
		prefix := words[0]
		if twoWords[prefix] && len(words) > 1 && !strings.HasPrefix(words[1], "-") {
			prefix += " " + words[1]
		}
		covered := false
		for s := range have {
			if strings.HasPrefix(s, "Bash("+prefix) || strings.HasPrefix("Bash("+prefix, strings.TrimSuffix(strings.TrimSuffix(s, ")"), "*")) && strings.HasPrefix(s, "Bash(") {
				covered = true
				break
			}
		}
		if covered {
			continue
		}
		for _, s := range []string{"Bash(" + prefix + ")", "Bash(" + prefix + " *)"} {
			have[s] = true
			signals = append(signals, s)
		}
	}
	return signals
}

// unquote drops quotes that wrap a whole value, and only those: a command such as
// echo "done" keeps its own.
func unquote(v string) string {
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		return v[1 : len(v)-1]
	}
	return v
}
