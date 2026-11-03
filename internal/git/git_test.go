package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// isolate keeps the owner's git config, such as commit signing, out of the tests.
func isolate(t *testing.T) {
	t.Helper()
	empty := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", empty)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "Ana Example")
	t.Setenv("GIT_AUTHOR_EMAIL", "ana@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Ana Example")
	t.Setenv("GIT_COMMITTER_EMAIL", "ana@example.com")
}

func sh(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// newRepo is a repository on main with one commit.
func newRepo(t *testing.T) string {
	t.Helper()
	isolate(t)
	dir := filepath.Join(t.TempDir(), "shop")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	sh(t, dir, "init", "-q", "-b", "main")
	write(t, filepath.Join(dir, "limit.go"), "package shop\n\nconst limit = 10\n")
	write(t, filepath.Join(dir, "old.go"), "package shop\n\n// moved soon\nfunc old() {}\n")
	write(t, filepath.Join(dir, "gone.txt"), "one\ntwo\n")
	write(t, filepath.Join(dir, "ab.txt"), "ab\n")
	sh(t, dir, "add", ".")
	sh(t, dir, "commit", "-q", "-m", "start")
	return dir
}

func byPath(files []File) map[string]File {
	m := map[string]File{}
	for _, f := range files {
		m[f.Path] = f
	}
	return m
}

func TestChangesSeeEveryKindOfChange(t *testing.T) {
	ctx := context.Background()
	dir := newRepo(t)
	write(t, filepath.Join(dir, "limit.go"), "package shop\n\nconst limit = 20\nconst burst = 5\n")
	os.Remove(filepath.Join(dir, "gone.txt"))
	sh(t, dir, "mv", "old.go", "legacy.go")
	write(t, filepath.Join(dir, "notes", "plan.md"), "one\ntwo\nthree")
	write(t, filepath.Join(dir, "logo.png"), "\x89PNG\x00\x00binary")

	files, more, err := Changes(ctx, dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if more != 0 {
		t.Fatalf("more = %d", more)
	}
	got := byPath(files)
	want := map[string]File{
		"limit.go":      {Path: "limit.go", Status: Modified, Added: 2, Removed: 1, Uncommitted: true},
		"gone.txt":      {Path: "gone.txt", Status: Deleted, Removed: 2, Uncommitted: true},
		"legacy.go":     {Path: "legacy.go", From: "old.go", Status: Renamed, Uncommitted: true},
		"notes/plan.md": {Path: "notes/plan.md", Status: Untracked, Added: 3, Uncommitted: true},
		"logo.png":      {Path: "logo.png", Status: Untracked, Binary: true, Uncommitted: true},
	}
	if len(got) != len(want) {
		t.Fatalf("files = %+v", files)
	}
	for p, w := range want {
		if got[p] != w {
			t.Errorf("%s = %+v, want %+v", p, got[p], w)
		}
	}

	patch, truncated, err := Patch(ctx, dir, "", got["notes/plan.md"], 1<<20)
	if err != nil || truncated || !strings.Contains(patch, "+three") {
		t.Fatalf("new file patch = %q, %v, %v", patch, truncated, err)
	}
	patch, _, err = Patch(ctx, dir, "", got["limit.go"], 1<<20)
	if err != nil || !strings.Contains(patch, "-const limit = 10") || !strings.Contains(patch, "+const burst = 5") {
		t.Fatalf("patch = %q, %v", patch, err)
	}
	cut, truncated, err := Patch(ctx, dir, "", got["limit.go"], 40)
	if err != nil || !truncated || len(cut) != 40 {
		t.Fatalf("cut patch = %q, %v, %v", cut, truncated, err)
	}
}

func TestOutsideARepository(t *testing.T) {
	isolate(t)
	if _, err := Open(context.Background(), t.TempDir()); err != ErrNotRepo {
		t.Fatalf("err = %v, want ErrNotRepo", err)
	}
}

// A discard takes exactly the file named, keeps it in a stash, and treats the name literally.
func TestDiscardKeepsAStash(t *testing.T) {
	ctx := context.Background()
	dir := newRepo(t)
	write(t, filepath.Join(dir, "a*.txt"), "draft\n")
	write(t, filepath.Join(dir, "ab.txt"), "ab changed\n")
	write(t, filepath.Join(dir, "limit.go"), "package shop\n")

	if err := Discard(ctx, dir, File{Path: "a*.txt", Status: Untracked}, "orla: discarded a*.txt"); err != nil {
		t.Fatal(err)
	}
	if err := Discard(ctx, dir, File{Path: "limit.go", Status: Modified}, "orla: discarded limit.go"); err != nil {
		t.Fatal(err)
	}
	files, _, err := Changes(ctx, dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "ab.txt" {
		t.Fatalf("left = %+v, want only ab.txt", files)
	}
	stashes := sh(t, dir, "stash", "list")
	if !strings.Contains(stashes, "orla: discarded a*.txt") || !strings.Contains(stashes, "orla: discarded limit.go") {
		t.Fatalf("stash list = %q", stashes)
	}
	sh(t, dir, "stash", "pop", "-q", "stash@{1}")
	if b, _ := os.ReadFile(filepath.Join(dir, "a*.txt")); string(b) != "draft\n" {
		t.Fatalf("restored = %q", b)
	}
}

func TestWorktreeFromStartToMerge(t *testing.T) {
	ctx := context.Background()
	repo := newRepo(t)
	path := filepath.Join(t.TempDir(), "shop-quiet-harbor")
	w, err := AddWorktree(ctx, filepath.Join(repo), path, "orla/quiet-harbor")
	if err != nil {
		t.Fatal(err)
	}
	if w.Base != "main" || w.Repo != repo {
		t.Fatalf("worktree = %+v", w)
	}
	if _, err := AddWorktree(ctx, repo, path+"-2", "bad..name"); err == nil {
		t.Fatal("a bad branch name was accepted")
	}

	// One change committed, one not: the review shows both against where the branch started.
	write(t, filepath.Join(path, "limit.go"), "package shop\n\nconst limit = 99\n")
	if _, err := Commit(ctx, path, "Raise the limit"); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(path, "burst.go"), "package shop\n")
	r, err := Open(ctx, path)
	if err != nil || r.Branch != "orla/quiet-harbor" || r.Main != repo || r.Root != path {
		t.Fatalf("open = %+v, %v", r, err)
	}
	base, err := MergeBase(ctx, path, w.Base, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	files, _, err := Changes(ctx, path, base)
	if err != nil {
		t.Fatal(err)
	}
	got := byPath(files)
	if got["limit.go"].Uncommitted || !got["burst.go"].Uncommitted || len(got) != 2 {
		t.Fatalf("files = %+v", files)
	}

	p, err := MergePreview(ctx, repo, w.Base, w.Branch)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Clean || p.Ahead != 1 || p.Behind != 0 || p.Into != repo {
		t.Fatalf("preview = %+v", p)
	}
	if err := Merge(ctx, repo, w.Base, w.Branch); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(repo, "limit.go")); !strings.Contains(string(b), "99") {
		t.Fatalf("main after merge = %q", b)
	}

	// Merged, so the branch goes; burst.go was never committed, so it waits in a stash.
	removal, err := RemoveWorktree(ctx, w)
	if err != nil {
		t.Fatal(err)
	}
	if removal.BranchKept || removal.Stash == "" {
		t.Fatalf("removal = %+v", removal)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("worktree folder still there: %v", err)
	}
	if out := sh(t, repo, "branch", "--list", w.Branch); out != "" {
		t.Fatalf("branch left: %q", out)
	}
	if out := sh(t, repo, "stash", "list"); !strings.Contains(out, "orla/quiet-harbor") {
		t.Fatalf("stash list = %q", out)
	}
}

func TestMergePreviewFindsConflicts(t *testing.T) {
	ctx := context.Background()
	repo := newRepo(t)
	path := filepath.Join(t.TempDir(), "shop-red-reef")
	w, err := AddWorktree(ctx, repo, path, "orla/red-reef")
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(path, "limit.go"), "package shop\n\nconst limit = 30\n")
	if _, err := Commit(ctx, path, "Thirty"); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(repo, "limit.go"), "package shop\n\nconst limit = 40\n")
	sh(t, repo, "commit", "-q", "-am", "Forty")

	p, err := MergePreview(ctx, repo, w.Base, w.Branch)
	if err != nil {
		t.Fatal(err)
	}
	if p.Clean || !slices.Equal(p.Conflicts, []string{"limit.go"}) || p.Ahead != 1 || p.Behind != 1 {
		t.Fatalf("preview = %+v", p)
	}
	if err := Merge(ctx, repo, w.Base, w.Branch); err == nil {
		t.Fatal("a conflicting merge went through")
	}
	if out := sh(t, repo, "status", "--porcelain"); out != "" {
		t.Fatalf("main left mid-merge: %q", out)
	}

	removal, err := RemoveWorktree(ctx, w)
	if err != nil {
		t.Fatal(err)
	}
	if !removal.BranchKept || removal.Stash != "" {
		t.Fatalf("removal = %+v", removal)
	}
}

func TestWatcherReportsWhatChanged(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := newRepo(t)
	got := make(chan []string, 10)
	w, err := Watch(ctx, dir, time.Hour, func(p []string) { got <- p })
	if err != nil {
		t.Fatal(err)
	}
	go w.Run(ctx)

	next := func() []string {
		t.Helper()
		select {
		case p := <-got:
			return p
		case <-time.After(5 * time.Second):
			t.Fatal("no change reported")
			return nil
		}
	}
	write(t, filepath.Join(dir, "limit.go"), "package shop\n// edited\n")
	w.Nudge()
	if p := next(); !slices.Equal(p, []string{"limit.go"}) {
		t.Fatalf("paths = %v", p)
	}
	// Edited again: same status, new content.
	time.Sleep(10 * time.Millisecond)
	write(t, filepath.Join(dir, "limit.go"), "package shop\n// edited twice\n")
	write(t, filepath.Join(dir, "new.go"), "package shop\n")
	// Stop reports the last changes before it returns.
	w.Stop()
	select {
	case p := <-got:
		if !slices.Equal(p, []string{"limit.go", "new.go"}) {
			t.Fatalf("paths = %v", p)
		}
	default:
		t.Fatal("Stop returned before reporting the last changes")
	}
	if _, err := Watch(ctx, t.TempDir(), time.Hour, nil); err != ErrNotRepo {
		t.Fatalf("outside a repository: err = %v", err)
	}
}
