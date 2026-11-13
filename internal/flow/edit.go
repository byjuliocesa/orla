package flow

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// The owner archives, restores and deletes flows and steps from Orla's buttons; these do it
// on the files directly, the way /archiveflow and /deleteflow describe.

var ErrNotFound = errors.New("not found")

func find(home, cwd, name string) (Flow, error) {
	for _, f := range List(home, cwd) {
		if f.Name == name {
			return f, nil
		}
	}
	return Flow{}, fmt.Errorf("flow %q: %w", name, ErrNotFound)
}

const origin = ".orla-origin"

// ArchiveFlow moves a flow out of Claude Code's reach, into the archive, remembering where from.
func ArchiveFlow(home, cwd, name string) error {
	f, err := find(home, cwd, name)
	if err != nil {
		return err
	}
	dst := filepath.Join(Archive(home), "flows", filepath.Base(f.Dir))
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("an archived flow is already at %s", dst)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	src, err := Unlink(f.Dir)
	if err != nil {
		return err
	}
	if err := os.Rename(src, dst); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dst, origin), []byte(src), 0o644)
}

// RestoreFlow puts an archived flow back where it was.
func RestoreFlow(home, name string) error {
	flows, _ := Archived(home)
	for _, f := range flows {
		if f.Name != name {
			continue
		}
		dst := filepath.Join(home, ".claude", "skills", filepath.Base(f.Dir))
		if b, err := os.ReadFile(filepath.Join(f.Dir, origin)); err == nil && filepath.IsAbs(strings.TrimSpace(string(b))) {
			dst = strings.TrimSpace(string(b))
		}
		if _, err := os.Stat(dst); err == nil {
			return fmt.Errorf("something is already at %s", dst)
		}
		_ = os.Remove(filepath.Join(f.Dir, origin))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.Rename(f.Dir, dst)
	}
	return fmt.Errorf("archived flow %q: %w", name, ErrNotFound)
}

// DeleteFlow removes a flow's folder for good.
func DeleteFlow(home, cwd, name string) error {
	f, err := find(home, cwd, name)
	if err != nil {
		return err
	}
	if Kind(f.Path) != "flow" {
		return fmt.Errorf("%s is not a flow", f.Dir)
	}
	dir, err := Unlink(f.Dir)
	if err != nil {
		return err
	}
	return os.RemoveAll(dir)
}

// Unlink returns the folder a skill entry stands for: the entry itself, or, when it is a link
// such as the library makes, the folder it points to, with the link removed. Moving or deleting
// only the link would leave the folder behind, and the library would link it again.
func Unlink(dir string) (string, error) {
	fi, err := os.Lstat(dir)
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		return dir, err
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}
	return real, os.Remove(dir)
}

// doc is a SKILL.md cut at its steps: what comes before them, then each step's lines.
type doc struct {
	head  []string
	steps []section
}

type section struct {
	id    string
	lines []string
	// fields counts the "key: value" lines right under the heading, blank lines among them.
	fields int
}

func parse(text string) doc {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	var d doc
	// The front matter is never cut.
	start := 0
	if len(lines) > 0 && lines[0] == "---" {
		for i := 1; i < len(lines); i++ {
			if lines[i] == "---" {
				start = i + 1
				break
			}
		}
	}
	d.head = append(d.head, lines[:start]...)
	fence := false
	var cur *section
	for _, line := range lines[start:] {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fence = !fence
		}
		if m := heading.FindStringSubmatch(line); m != nil && !fence {
			if cur != nil {
				d.steps = append(d.steps, *cur)
			}
			cur = &section{id: Slug(number.ReplaceAllString(m[1], "")), lines: []string{line}}
			continue
		}
		if cur == nil {
			d.head = append(d.head, line)
			continue
		}
		if len(cur.lines) == cur.fields+1 {
			t := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "-*"))
			if fm := field.FindStringSubmatch(t); fm != nil {
				if strings.EqualFold(fm[1], "id") {
					cur.id = Slug(fm[2])
				}
				cur.fields++
			} else if t == "" && cur.fields == 0 {
				// a blank line straight under the heading is not yet the text
				cur.fields++
			}
		}
		cur.lines = append(cur.lines, line)
	}
	if cur != nil {
		d.steps = append(d.steps, *cur)
	}
	return d
}

func (d doc) text() string {
	var out []string
	out = append(out, d.head...)
	for _, s := range d.steps {
		out = append(out, s.lines...)
	}
	return strings.Join(out, "\n")
}

func (d doc) index(id string) int {
	for i, s := range d.steps {
		if s.id == id {
			return i
		}
	}
	return -1
}

// branchesTo takes out of every other step the branch lines that lead to id, and says which.
func (d *doc) branchesTo(id string) []string {
	ids := map[string]string{}
	for _, s := range d.steps {
		ids[s.id] = s.id
		ids[Slug(number.ReplaceAllString(heading.FindStringSubmatch(s.lines[0])[1], ""))] = s.id
	}
	var gone []string
	for i := range d.steps {
		s := &d.steps[i]
		if s.id == id {
			continue
		}
		var keep []string
		for j, line := range s.lines {
			if j >= 1 && j <= s.fields {
				t := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "-*"))
				if fm := field.FindStringSubmatch(t); fm != nil && (strings.EqualFold(fm[1], "branch") || strings.EqualFold(fm[1], "desvio")) {
					to := fm[2]
					if loc := when.FindStringIndex(to); loc != nil {
						to = to[:loc[0]]
					}
					target := Slug(strings.Trim(strings.TrimSpace(to), "`"))
					if target == id || ids[target] == id {
						gone = append(gone, s.id+": "+line)
						s.fields--
						continue
					}
				}
			}
			keep = append(keep, line)
		}
		s.lines = keep
	}
	return gone
}

var numbered = regexp.MustCompile(`^(##\s+)\d+([.)]\s+)(.*)$`)

// renumber keeps "## 1. Title" headings counting from one, when the flow numbers them.
func (d *doc) renumber() {
	for i := range d.steps {
		if m := numbered.FindStringSubmatch(d.steps[i].lines[0]); m != nil {
			d.steps[i].lines[0] = m[1] + strconv.Itoa(i+1) + m[2] + m[3]
		}
	}
}

func rewrite(f Flow, change func(d *doc) error) error {
	b, err := os.ReadFile(f.Path)
	if err != nil {
		return err
	}
	d := parse(string(b))
	if err := change(&d); err != nil {
		return err
	}
	d.renumber()
	return os.WriteFile(f.Path, []byte(d.text()), 0o644)
}

func stepFile(home, flow, id string) string {
	return filepath.Join(Archive(home), "steps", flow, id+".md")
}

// ArchiveStep cuts a step out of its flow and keeps it, with where it stood and the branches
// that led to it, so RestoreStep puts all of it back.
func ArchiveStep(home, cwd, name, id string) error {
	f, err := find(home, cwd, name)
	if err != nil {
		return err
	}
	return rewrite(f, func(d *doc) error {
		i := d.index(id)
		if i < 0 {
			return fmt.Errorf("step %q of %s: %w", id, name, ErrNotFound)
		}
		after := "<!-- first -->"
		if i > 0 {
			after = "<!-- after: " + d.steps[i-1].id + " -->"
		}
		s := d.steps[i]
		gone := d.branchesTo(id)
		d.steps = append(d.steps[:i], d.steps[i+1:]...)
		kept := []string{after}
		for _, b := range gone {
			kept = append(kept, "<!-- branch from "+b+" -->")
		}
		kept = append(kept, strings.TrimRight(strings.Join(s.lines, "\n"), "\n"), "")
		p := stepFile(home, name, id)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		return os.WriteFile(p, []byte(strings.Join(kept, "\n")), 0o644)
	})
}

var (
	afterNote  = regexp.MustCompile(`^<!-- after: (\S+) -->$`)
	branchNote = regexp.MustCompile(`^<!-- branch from ([^:]+): (.*) -->$`)
)

// RestoreStep puts an archived step back after the step it followed, with its branches.
func RestoreStep(home, cwd, name, id string) error {
	f, err := find(home, cwd, name)
	if err != nil {
		return err
	}
	p := stepFile(home, name, id)
	b, err := os.ReadFile(p)
	if err != nil {
		return fmt.Errorf("archived step %q of %s: %w", id, name, ErrNotFound)
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	after, first := "", false
	branches := map[string][]string{}
	var body []string
	for _, l := range lines {
		switch {
		case l == "<!-- first -->":
			first = true
		case afterNote.MatchString(l):
			after = afterNote.FindStringSubmatch(l)[1]
		case branchNote.MatchString(l):
			m := branchNote.FindStringSubmatch(l)
			branches[m[1]] = append(branches[m[1]], m[2])
		default:
			body = append(body, l)
		}
	}
	if err := rewrite(f, func(d *doc) error {
		s := parse(strings.Join(body, "\n") + "\n")
		if len(s.steps) == 0 {
			return fmt.Errorf("the archived step %s has no heading", p)
		}
		at := len(d.steps)
		if first {
			at = 0
		} else if i := d.index(after); i >= 0 {
			at = i + 1
		}
		// Steps end with a blank line before the next heading.
		if at > 0 {
			prev := &d.steps[at-1]
			if prev.lines[len(prev.lines)-1] != "" {
				prev.lines = append(prev.lines, "")
			}
		}
		d.steps = append(d.steps[:at], append([]section{s.steps[0]}, d.steps[at:]...)...)
		for from, ls := range branches {
			if i := d.index(from); i >= 0 {
				st := &d.steps[i]
				st.lines = append(st.lines[:1], append(ls, st.lines[1:]...)...)
				st.fields += len(ls)
			}
		}
		return nil
	}); err != nil {
		return err
	}
	return os.Remove(p)
}

// DeleteStep removes a step from its flow for good, and the branches that led to it.
func DeleteStep(home, cwd, name, id string) error {
	f, err := find(home, cwd, name)
	if err != nil {
		return err
	}
	return rewrite(f, func(d *doc) error {
		i := d.index(id)
		if i < 0 {
			return fmt.Errorf("step %q of %s: %w", id, name, ErrNotFound)
		}
		d.branchesTo(id)
		d.steps = append(d.steps[:i], d.steps[i+1:]...)
		return nil
	})
}

var (
	nameLine  = regexp.MustCompile(`(?m)^name:.*$`)
	titleLine = regexp.MustCompile(`(?m)^(\s+title:).*$`)
)

// DuplicateFlow copies a flow's folder beside it, as "<name>-copy" titled "<title> (copy)", and
// says the copy's name. Steps, scripts and step files come along.
func DuplicateFlow(home, cwd, name string) (string, error) {
	f, err := find(home, cwd, name)
	if err != nil {
		return "", err
	}
	parent := filepath.Dir(f.Dir)
	base := filepath.Base(f.Dir) + "-copy"
	newName := f.Name + "-copy"
	dst := filepath.Join(parent, base)
	taken := map[string]bool{}
	for _, x := range List(home, cwd) {
		taken[x.Name] = true
	}
	for n := 2; ; n++ {
		if _, err := os.Stat(dst); os.IsNotExist(err) && !taken[newName] {
			break
		}
		base = fmt.Sprintf("%s-copy-%d", filepath.Base(f.Dir), n)
		newName = fmt.Sprintf("%s-copy-%d", f.Name, n)
		dst = filepath.Join(parent, base)
	}
	if err := copyDir(f.Dir, dst); err != nil {
		_ = os.RemoveAll(dst)
		return "", err
	}
	path := filepath.Join(dst, "SKILL.md")
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	text := string(b)
	// Only the front matter is renamed; the steps stay word for word.
	end := strings.Index(text[4:], "\n---") + 4
	if !strings.HasPrefix(text, "---\n") || end < 4 {
		return "", fmt.Errorf("%s has no front matter", path)
	}
	head := nameLine.ReplaceAllString(text[:end], "name: "+newName)
	title := f.Title + " (copy)"
	if titleLine.MatchString(head) {
		head = titleLine.ReplaceAllString(head, "${1} "+title)
	} else {
		head = strings.Replace(head, "  orla: flow", "  orla: flow\n  title: "+title, 1)
	}
	if !nameLine.MatchString(head) {
		head = "---\nname: " + newName + head[3:]
	}
	return newName, os.WriteFile(path, []byte(head+text[end:]), 0o644)
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		to := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(to, 0o755)
		}
		if d.Name() == origin {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(to, b, info.Mode().Perm())
	})
}
