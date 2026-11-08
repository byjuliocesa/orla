package server

import (
	"context"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// FileHit is a file or folder an @ in the prompt can point at.
type FileHit struct {
	// Path is what goes after the @: relative to the session's folder, or ~/... and /... as typed.
	Path string `json:"path"`
	Dir  bool   `json:"dir,omitempty"`
}

const maxHits = 40

// skipDirs are never walked: they are big and never what the owner points at.
var skipDirs = map[string]bool{".git": true, "node_modules": true, ".next": true, "dist": true, "out": true, "target": true, ".cache": true, "vendor": true, "__pycache__": true, ".venv": true}

// files finds what an @ can point at. ?q= starting with ~ or / browses folders from there;
// anything else searches the session's folder (?cwd=, the home directory when empty), its
// files tracked by git when it is a repository.
func (s *Server) files(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	cwd := r.URL.Query().Get("cwd")
	home, _ := os.UserHomeDir()
	if cwd == "" || !filepath.IsAbs(cwd) {
		cwd = home
	}
	var hits []FileHit
	if strings.HasPrefix(q, "~") || strings.HasPrefix(q, "/") {
		hits = browse(q, home)
	} else {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		hits = search(ctx, cwd, q)
	}
	if hits == nil {
		hits = []FileHit{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": hits})
}

// browse lists a folder's entries that start with what follows its last slash, folders first.
func browse(q, home string) []FileHit {
	shown := q
	if strings.HasPrefix(q, "~") && !strings.HasPrefix(q, "~/") {
		shown = "~/"
	}
	dirPart, prefix := shown[:strings.LastIndex(shown, "/")+1], shown[strings.LastIndex(shown, "/")+1:]
	real := dirPart
	if strings.HasPrefix(real, "~/") {
		real = filepath.Join(home, real[2:])
	}
	entries, err := os.ReadDir(real)
	if err != nil {
		return nil
	}
	low := strings.ToLower(prefix)
	var hits []FileHit
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") && !strings.HasPrefix(prefix, ".") {
			continue
		}
		if !strings.HasPrefix(strings.ToLower(name), low) {
			continue
		}
		dir := e.IsDir()
		if e.Type()&fs.ModeSymlink != 0 {
			if st, err := os.Stat(filepath.Join(real, name)); err == nil {
				dir = st.IsDir()
			}
		}
		p := dirPart + name
		if dir {
			p += "/"
		}
		hits = append(hits, FileHit{Path: p, Dir: dir})
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Dir != hits[j].Dir {
			return hits[i].Dir
		}
		return strings.ToLower(hits[i].Path) < strings.ToLower(hits[j].Path)
	})
	if len(hits) > maxHits {
		hits = hits[:maxHits]
	}
	return hits
}

// search ranks the folder's files and folders against q, as a fuzzy match on the path.
func search(ctx context.Context, cwd, q string) []FileHit {
	paths := listFiles(ctx, cwd)
	dirs := map[string]bool{}
	for _, p := range paths {
		for d := filepath.Dir(p); d != "." && d != "/" && !dirs[d]; d = filepath.Dir(d) {
			dirs[d] = true
		}
	}
	type scored struct {
		hit   FileHit
		score int
	}
	var all []scored
	low := strings.ToLower(q)
	add := func(p string, dir bool) {
		sc, ok := fuzzy(strings.ToLower(p), low)
		if !ok {
			return
		}
		if dir {
			p += "/"
		}
		all = append(all, scored{FileHit{Path: p, Dir: dir}, sc})
	}
	for d := range dirs {
		add(d, true)
	}
	for _, p := range paths {
		add(p, false)
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].score != all[j].score {
			return all[i].score > all[j].score
		}
		return len(all[i].hit.Path) < len(all[j].hit.Path)
	})
	var hits []FileHit
	for i := 0; i < len(all) && i < maxHits; i++ {
		hits = append(hits, all[i].hit)
	}
	return hits
}

// listFiles is the folder's files: what git tracks or would (not ignored), else a short walk.
func listFiles(ctx context.Context, cwd string) []string {
	cmd := exec.CommandContext(ctx, "git", "-C", cwd, "ls-files", "--cached", "--others", "--exclude-standard")
	if out, err := cmd.Output(); err == nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		if len(lines) > 20000 {
			lines = lines[:20000]
		}
		return lines
	}
	var paths []string
	_ = filepath.WalkDir(cwd, func(p string, d fs.DirEntry, err error) error {
		if err != nil || ctx.Err() != nil || len(paths) >= 5000 {
			return filepath.SkipDir
		}
		rel, _ := filepath.Rel(cwd, p)
		if d.IsDir() {
			if rel != "." && (skipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".") || strings.Count(rel, string(filepath.Separator)) >= 4) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasPrefix(d.Name(), ".") {
			paths = append(paths, rel)
		}
		return nil
	})
	return paths
}

// fuzzy matches q's characters in order inside p. Matches in the file's own name, at the start
// of a word, and in a row score higher; an empty q matches everything a little.
func fuzzy(p, q string) (int, bool) {
	if q == "" {
		return 1, true
	}
	base := strings.LastIndex(p, "/") + 1
	score, pi, run := 0, 0, 0
	for _, c := range q {
		i := strings.IndexRune(p[pi:], c)
		if i < 0 {
			return 0, false
		}
		at := pi + i
		s := 1
		if i == 0 {
			run++
			s += run * 2
		} else {
			run = 0
		}
		if at >= base {
			s += 3
		}
		if at == 0 || strings.ContainsRune("/._-", rune(p[at-1])) {
			s += 4
		}
		score += s
		pi = at + 1
	}
	if strings.Contains(p[base:], q) {
		score += 20
	}
	// All of q found, in order, inside the file's own name: what was most likely meant.
	if _, ok := inOrder(p[base:], q); ok {
		score += 30
	}
	return score, true
}

func inOrder(s, q string) (int, bool) {
	i := 0
	for _, c := range q {
		j := strings.IndexRune(s[i:], c)
		if j < 0 {
			return 0, false
		}
		i += j + 1
	}
	return i, true
}
