package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Julio0Cesar/orla/internal/engine/cli"
	"github.com/Julio0Cesar/orla/internal/events"
)

// gitRepo is a repository on main with one commit, with the owner's git config kept out.
func gitRepo(t *testing.T) string {
	t.Helper()
	empty := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", empty)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, k := range []string{"GIT_AUTHOR", "GIT_COMMITTER"} {
		t.Setenv(k+"_NAME", "Ana Example")
		t.Setenv(k+"_EMAIL", "ana@example.com")
	}
	dir := filepath.Join(t.TempDir(), "shop")
	if err := os.MkdirAll(filepath.Join(dir, "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "api", "limit.go"), []byte("package api\n\nconst limit = 10\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"add", "."}, {"commit", "-q", "-m", "start"}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func decodeInto(t *testing.T, res *http.Response, want int, v any) {
	t.Helper()
	if res.StatusCode != want {
		var e struct{ Error string }
		_ = json.NewDecoder(res.Body).Decode(&e)
		t.Fatalf("%s %s = %d (%s), want %d", res.Request.Method, res.Request.URL.Path, res.StatusCode, e.Error, want)
	}
	if v != nil {
		if err := json.NewDecoder(res.Body).Decode(v); err != nil {
			t.Fatal(err)
		}
	}
}

// From a worktree to a merge, through the API: the review sees what the agent did against the
// branch it started from, and nothing is lost on the way out.
func TestWorktreeReviewThroughTheAPI(t *testing.T) {
	srv := newServer(t)
	repo := gitRepo(t)

	var info struct {
		Git  bool
		Repo struct{ Root, Branch string }
	}
	decodeInto(t, request(t, srv, "GET", "/v1/git?path="+url.QueryEscape(repo), testToken, nil), 200, &info)
	if !info.Git || info.Repo.Branch != "main" {
		t.Fatalf("git info = %+v", info)
	}

	// Made from a folder inside the repository, the session starts in the same folder.
	var made struct {
		Worktree struct{ Path, Branch, Base string }
		Cwd      string
	}
	decodeInto(t, request(t, srv, "POST", "/v1/worktrees", testToken,
		map[string]string{"from": filepath.Join(repo, "api")}), 201, &made)
	if !strings.HasPrefix(made.Worktree.Branch, "orla/") || made.Worktree.Base != "main" ||
		made.Cwd != filepath.Join(made.Worktree.Path, "api") {
		t.Fatalf("worktree = %+v", made)
	}
	var sess struct {
		ID       string
		Worktree *struct{ Branch string }
	}
	decodeInto(t, request(t, srv, "POST", "/v1/sessions", testToken, map[string]string{"cwd": made.Cwd}), 201, &sess)
	if sess.Worktree == nil || sess.Worktree.Branch != made.Worktree.Branch {
		t.Fatalf("session = %+v", sess)
	}
	base := "/v1/sessions/" + sess.ID

	wt := made.Worktree.Path
	if err := os.WriteFile(filepath.Join(wt, "api", "limit.go"), []byte("package api\n\nconst limit = 50\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	type changes struct {
		Git   bool
		Files []struct {
			Path, Status string
			Uncommitted  bool
		}
		Ahead  int
		Behind int
	}
	var c changes
	decodeInto(t, request(t, srv, "GET", base+"/changes", testToken, nil), 200, &c)
	if !c.Git || len(c.Files) != 1 || c.Files[0].Path != "api/limit.go" || !c.Files[0].Uncommitted {
		t.Fatalf("changes = %+v", c)
	}
	var diff struct{ Patch string }
	decodeInto(t, request(t, srv, "GET", base+"/diff?path=api/limit.go", testToken, nil), 200, &diff)
	if !strings.Contains(diff.Patch, "+const limit = 50") {
		t.Fatalf("patch = %q", diff.Patch)
	}
	// Only changed files are read.
	decodeInto(t, request(t, srv, "GET", base+"/diff?path="+url.QueryEscape("../../../../etc/passwd"), testToken, nil), 404, nil)

	decodeInto(t, request(t, srv, "POST", base+"/commit", testToken, map[string]string{"message": "Raise the limit"}), 200, nil)
	decodeInto(t, request(t, srv, "POST", base+"/commit", testToken, map[string]string{"message": "Again"}), 409, nil)
	decodeInto(t, request(t, srv, "GET", base+"/changes", testToken, nil), 200, &c)
	if len(c.Files) != 1 || c.Files[0].Uncommitted || c.Ahead != 1 {
		t.Fatalf("after commit = %+v", c)
	}
	decodeInto(t, request(t, srv, "POST", base+"/discard", testToken, map[string]string{"path": "api/limit.go"}), 409, nil)

	var preview struct {
		Preview struct {
			Ahead int
			Clean bool
			Into  string
		}
		Uncommitted int
	}
	if err := os.WriteFile(filepath.Join(wt, "draft.md"), []byte("not yet\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	decodeInto(t, request(t, srv, "GET", base+"/merge", testToken, nil), 200, &preview)
	if !preview.Preview.Clean || preview.Preview.Ahead != 1 || preview.Preview.Into != repo || preview.Uncommitted != 1 {
		t.Fatalf("preview = %+v", preview)
	}
	decodeInto(t, request(t, srv, "POST", base+"/merge", testToken, nil), 200, &preview)
	if preview.Preview.Ahead != 0 {
		t.Fatalf("after merge = %+v", preview)
	}
	if b, _ := os.ReadFile(filepath.Join(repo, "api", "limit.go")); !strings.Contains(string(b), "50") {
		t.Fatalf("main after merge = %q", b)
	}

	var removed struct {
		Removal struct {
			Stash      string
			BranchKept bool `json:"branch_kept"`
		}
	}
	decodeInto(t, request(t, srv, "DELETE", base+"/worktree", testToken, nil), 200, &removed)
	if removed.Removal.Stash == "" || removed.Removal.BranchKept {
		t.Fatalf("removal = %+v", removed)
	}
	decodeInto(t, request(t, srv, "GET", base+"/changes", testToken, nil), 200, &c)
	if c.Git {
		t.Fatalf("changes after removal = %+v", c)
	}
	// The session remembers what the removal kept.
	var after struct {
		Worktree struct {
			Removed    *time.Time
			Stash      string
			BranchKept bool `json:"branch_kept"`
		}
	}
	var list struct{ Sessions []json.RawMessage }
	decodeInto(t, request(t, srv, "GET", "/v1/sessions", testToken, nil), 200, &list)
	if len(list.Sessions) != 1 || json.Unmarshal(list.Sessions[0], &after) != nil ||
		after.Worktree.Removed == nil || after.Worktree.Stash != removed.Removal.Stash || after.Worktree.BranchKept {
		t.Fatalf("session after removal = %s", list.Sessions)
	}
	decodeInto(t, request(t, srv, "DELETE", base+"/worktree", testToken, nil), 409, nil)
}

// What a run writes reaches the client as files.changed, before the run's end.
func TestRunReportsTheFilesItChanged(t *testing.T) {
	repo := gitRepo(t)
	fixture, _ := filepath.Abs("../engine/cli/testdata/claude-tool-run.jsonl")
	script := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho draft > notes.md\ncat "+fixture+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	srv, _ := newServerWith(t, cli.Claude{Path: script})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var sess struct{ ID string }
	decodeInto(t, request(t, srv, "POST", "/v1/sessions", testToken, map[string]string{"cwd": repo}), 201, &sess)
	body := streamEvents(t, ctx, srv, sess.ID, "")
	defer body.Close()
	decodeInto(t, request(t, srv, "POST", "/v1/sessions/"+sess.ID+"/runs", testToken, map[string]string{"prompt": "write notes"}), 202, nil)

	got := readSSE(t, body, events.RunFinished)
	for _, e := range got {
		if e.Type != events.FilesChanged {
			continue
		}
		var d struct{ Paths []string }
		_ = json.Unmarshal(e.Data, &d)
		if len(d.Paths) == 1 && d.Paths[0] == "notes.md" {
			return
		}
		t.Fatalf("files.changed = %s", e.Data)
	}
	t.Fatalf("no files.changed before run.finished in %d events", len(got))
}
