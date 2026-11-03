package git

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// File statuses.
const (
	Added     = "added"
	Modified  = "modified"
	Deleted   = "deleted"
	Renamed   = "renamed"
	Untracked = "untracked"
)

// File is one file that differs from the base.
type File struct {
	Path string `json:"path"`
	// From is the old path of a rename.
	From    string `json:"from,omitempty"`
	Status  string `json:"status"`
	Added   int    `json:"added"`
	Removed int    `json:"removed"`
	Binary  bool   `json:"binary,omitempty"`
	// Uncommitted: part of the change is not in a commit yet, so it can be discarded.
	Uncommitted bool `json:"uncommitted"`
}

// MaxFiles bounds a list of changes. A new folder git does not ignore, such as an
// environment someone forgot to ignore, can hold tens of thousands of files.
const MaxFiles = 2000

// Changes lists what differs between base and the work tree at root: commits since base,
// staged and unstaged edits, and new files git does not ignore. An empty base means the last
// commit. more counts the files left out past MaxFiles.
func Changes(ctx context.Context, root, base string) (files []File, more int, err error) {
	vsHead := base == ""
	if base, err = resolveBase(ctx, root, base); err != nil {
		return nil, 0, err
	}

	byPath := map[string]*File{}
	status, err := run(ctx, root, "diff", "--name-status", "-z", "-M", base, "--")
	if err != nil {
		return nil, 0, err
	}
	for _, f := range parseNameStatus(status) {
		byPath[f.Path] = &f
	}
	counts, err := run(ctx, root, "diff", "--numstat", "-z", "-M", base, "--")
	if err != nil {
		return nil, 0, err
	}
	parseNumstat(counts, byPath)

	untracked, err := run(ctx, root, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, 0, err
	}
	for _, p := range splitZ(untracked) {
		f := File{Path: p, Status: Untracked, Uncommitted: true}
		f.Added, f.Binary = countLines(filepath.Join(root, p))
		byPath[p] = &f
	}

	if vsHead {
		for _, f := range byPath {
			f.Uncommitted = true
		}
	} else {
		dirty, err := dirtyPaths(ctx, root)
		if err != nil {
			return nil, 0, err
		}
		for _, f := range byPath {
			f.Uncommitted = f.Uncommitted || dirty[f.Path] || (f.From != "" && dirty[f.From])
		}
	}

	files = make([]File, 0, len(byPath))
	for _, f := range byPath {
		files = append(files, *f)
	}
	slices.SortFunc(files, func(a, b File) int { return strings.Compare(a.Path, b.Path) })
	if len(files) > MaxFiles {
		more = len(files) - MaxFiles
		files = files[:MaxFiles]
	}
	return files, more, nil
}

func resolveBase(ctx context.Context, root, base string) (string, error) {
	if base != "" {
		return base, nil
	}
	if _, err := run(ctx, root, "rev-parse", "-q", "--verify", "HEAD^{commit}"); err != nil {
		return emptyTree(ctx, root)
	}
	return "HEAD", nil
}

// parseNameStatus reads `git diff --name-status -z -M`: a status, then one path, or two for a
// rename or copy.
func parseNameStatus(out string) []File {
	var files []File
	fields := splitZ(out)
	for i := 0; i+1 < len(fields); i++ {
		code := fields[i]
		if code == "" {
			break
		}
		f := File{Path: fields[i+1]}
		i++
		switch code[0] {
		case 'R', 'C':
			if i+1 >= len(fields) {
				return files
			}
			f.From, f.Path = f.Path, fields[i+1]
			i++
			f.Status = Renamed
			if code[0] == 'C' {
				f.From, f.Status = "", Added
			}
		case 'A':
			f.Status = Added
		case 'D':
			f.Status = Deleted
		default: // M, T (type change), U (unmerged)
			f.Status = Modified
		}
		files = append(files, f)
	}
	return files
}

// parseNumstat reads `git diff --numstat -z -M`: "added\tremoved\tpath", or for a rename
// "added\tremoved\t" followed by the old and new paths. Binary files count "-".
func parseNumstat(out string, byPath map[string]*File) {
	fields := splitZ(out)
	for i := 0; i < len(fields); i++ {
		parts := strings.SplitN(fields[i], "\t", 3)
		if len(parts) != 3 {
			continue
		}
		path := parts[2]
		if path == "" && i+2 < len(fields) {
			path = fields[i+2]
			i += 2
		}
		f := byPath[path]
		if f == nil {
			continue
		}
		if parts[0] == "-" {
			f.Binary = true
			continue
		}
		f.Added, _ = strconv.Atoi(parts[0])
		f.Removed, _ = strconv.Atoi(parts[1])
	}
}

// countLines counts the lines of a new file, as a diff would add them, and says whether it
// looks binary: git's own test, a NUL in the first 8000 bytes.
func countLines(path string) (lines int, binary bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, false
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 64<<10)
	head, _ := r.Peek(8000)
	if bytes.IndexByte(head, 0) >= 0 {
		return 0, true
	}
	buf := make([]byte, 64<<10)
	var last byte
	for {
		n, err := r.Read(buf)
		if n > 0 {
			lines += bytes.Count(buf[:n], []byte{'\n'})
			last = buf[n-1]
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return lines, false
		}
	}
	if last != 0 && last != '\n' {
		lines++
	}
	return lines, false
}

// dirtyPaths are the paths whose work tree or index differs from the last commit.
func dirtyPaths(ctx context.Context, root string) (map[string]bool, error) {
	entries, err := status(ctx, root)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(entries))
	for _, e := range entries {
		out[e.path] = true
		if e.from != "" {
			out[e.from] = true
		}
	}
	return out, nil
}

type statusEntry struct {
	code, path, from string
}

// status reads `git status --porcelain -z`, whose paths are relative to the root of the work
// tree wherever it runs.
func status(ctx context.Context, dir string) ([]statusEntry, error) {
	out, err := run(ctx, dir, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return nil, err
	}
	fields := splitZ(out)
	entries := make([]statusEntry, 0, len(fields))
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if len(f) < 4 {
			continue
		}
		e := statusEntry{code: f[:2], path: f[3:]}
		// A rename or copy is followed by its source path.
		if (e.code[0] == 'R' || e.code[0] == 'C') && i+1 < len(fields) {
			e.from = fields[i+1]
			i++
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// Uncommitted counts the files that differ from the last commit in the work tree at root.
func Uncommitted(ctx context.Context, root string) (int, error) {
	entries, err := status(ctx, root)
	return len(entries), err
}

// Patch is the unified diff of one changed file against base, cut at limit bytes.
func Patch(ctx context.Context, root, base string, f File, limit int) (patch string, truncated bool, err error) {
	if f.Status == Untracked {
		out, truncated, err := output(ctx, root, limit, "diff", "--no-index", "--no-ext-diff", "--no-textconv",
			"--src-prefix=a/", "--dst-prefix=b/", "--", "/dev/null", f.Path)
		// --no-index exits 1 when the files differ, which is the point.
		if exitCode(err) == 1 {
			err = nil
		}
		return string(out), truncated, err
	}
	if base, err = resolveBase(ctx, root, base); err != nil {
		return "", false, err
	}
	args := []string{"diff", "--no-ext-diff", "--no-textconv", "-M", "--src-prefix=a/", "--dst-prefix=b/", base, "--", f.Path}
	if f.From != "" {
		args = append(args, f.From)
	}
	out, truncated, err := output(ctx, root, limit, args...)
	return string(out), truncated, err
}

// Discard sets a file back to its last commit, or removes it when it is new, and keeps what was
// there in a stash under label, so a mistaken discard is undone with `git stash pop`.
func Discard(ctx context.Context, root string, f File, label string) error {
	args := []string{"stash", "push", "--include-untracked", "--message", label, "--", f.Path}
	if f.From != "" {
		args = append(args, f.From)
	}
	_, err := run(ctx, root, args...)
	return err
}

// ErrNothingToCommit: the work tree matches the last commit.
var ErrNothingToCommit = errors.New("nothing to commit")

// Commit records every change in the work tree at root, new files included, and returns the new
// commit. The repository's hooks run as they would for the owner.
func Commit(ctx context.Context, root, message string) (string, error) {
	if n, err := Uncommitted(ctx, root); err != nil {
		return "", err
	} else if n == 0 {
		return "", ErrNothingToCommit
	}
	if _, err := run(ctx, root, "add", "--all"); err != nil {
		return "", err
	}
	if _, err := run(ctx, root, "commit", "--quiet", "--message", message); err != nil {
		return "", err
	}
	out, err := run(ctx, root, "rev-parse", "HEAD")
	return strings.TrimSpace(out), err
}
