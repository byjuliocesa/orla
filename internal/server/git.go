package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Julio0Cesar/orla/internal/git"
	"github.com/Julio0Cesar/orla/internal/store"
)

// maxPatch bounds one file's diff sent to a client. Past it the client says the diff is too
// large and the owner reads it with git.
const maxPatch = 1 << 20

// gitInfo says whether a folder is in a git repository, and which, for the new session form.
func (s *Server) gitInfo(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("path")
	if !filepath.IsAbs(dir) {
		writeError(w, http.StatusBadRequest, "path must be absolute")
		return
	}
	repo, err := git.Open(r.Context(), dir)
	if errors.Is(err, git.ErrNotRepo) {
		writeJSON(w, http.StatusOK, map[string]any{"git": false})
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"git": true, "repo": repo})
}

// createWorktree checks out a new branch, made from what a folder has checked out, in a folder
// of its own under Orla's data directory, for an agent to work in apart from everyone else.
func (s *Server) createWorktree(w http.ResponseWriter, r *http.Request) {
	var body struct {
		From   string `json:"from"`
		Branch string `json:"branch"`
	}
	if !decode(w, r, &body) {
		return
	}
	if !filepath.IsAbs(body.From) {
		writeError(w, http.StatusBadRequest, "from must be an absolute path")
		return
	}
	if s.worktrees == "" {
		writeError(w, http.StatusInternalServerError, "this Orla has no folder for worktrees")
		return
	}
	ctx := r.Context()
	repo, err := git.Open(ctx, body.From)
	if errors.Is(err, git.ErrNotRepo) {
		writeError(w, http.StatusBadRequest, body.From+" is not in a git repository")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := os.MkdirAll(s.worktrees, 0o700); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	named := body.Branch != ""
	for range 5 {
		branch := body.Branch
		if !named {
			branch = git.RandomBranch()
		}
		slug := strings.ReplaceAll(strings.TrimPrefix(branch, "orla/"), "/", "-")
		path := filepath.Join(s.worktrees, filepath.Base(repo.Main)+"-"+slug)
		if _, err := os.Stat(path); err == nil {
			if named {
				writeError(w, http.StatusConflict, path+" already exists")
				return
			}
			continue
		}
		made, err := git.AddWorktree(ctx, body.From, path, branch)
		if !named && err != nil && strings.Contains(err.Error(), "already exists") {
			continue
		}
		switch {
		case errors.Is(err, git.ErrBadBranch):
			writeError(w, http.StatusBadRequest, err.Error())
			return
		case err != nil:
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		// Stored as git reports it, so it matches the root git gives for folders inside it.
		if real, err := filepath.EvalSymlinks(made.Path); err == nil {
			made.Path = real
		}
		wt := store.Worktree{
			Path: made.Path, Repo: made.Repo, Branch: made.Branch, Base: made.Base, Created: time.Now().UTC(),
		}
		if err := s.store.CreateWorktree(ctx, wt); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		// A folder inside the repository maps to the same folder inside the worktree.
		cwd := wt.Path
		if rel, err := filepath.Rel(repo.Root, body.From); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			cwd = filepath.Join(wt.Path, rel)
		}
		writeJSON(w, http.StatusCreated, map[string]any{"worktree": wt, "cwd": cwd})
		return
	}
	writeError(w, http.StatusConflict, "could not find a free branch name; name one")
}

// review is what a session's changes are compared against.
type review struct {
	repo git.Repo
	// base is the commit compared against: where a worktree's branch left its base, or empty
	// for the last commit.
	base string
	wt   *store.Worktree
}

func (s *Server) reviewOf(ctx context.Context, x store.Session) (review, error) {
	repo, err := git.Open(ctx, x.Cwd)
	if err != nil {
		return review{}, err
	}
	rv := review{repo: repo}
	wt, err := s.store.WorktreeFor(ctx, x.Cwd)
	if err == nil && wt.Removed == nil && wt.Path == repo.Root {
		if base, err := git.MergeBase(ctx, repo.Root, wt.Base, "HEAD"); err == nil {
			rv.base, rv.wt = base, &wt
		}
	}
	return rv, nil
}

// reviewFor answers for a session outside a repository, or whose folder is gone, and reports
// whether there is anything to review.
func (s *Server) reviewFor(w http.ResponseWriter, r *http.Request, x store.Session) (review, bool) {
	rv, err := s.reviewOf(r.Context(), x)
	if err == nil {
		return rv, true
	}
	if _, statErr := os.Stat(x.Cwd); statErr != nil {
		writeError(w, http.StatusConflict, "the session's folder is gone: "+x.Cwd)
	} else if errors.Is(err, git.ErrNotRepo) {
		writeError(w, http.StatusConflict, x.Cwd+" is not in a git repository")
	} else {
		writeError(w, http.StatusInternalServerError, err.Error())
	}
	return rv, false
}

// sessionChanges lists what a session's agent changed: against the branch its worktree came
// from, or else against the last commit.
func (s *Server) sessionChanges(w http.ResponseWriter, r *http.Request) {
	x, ok := s.lookup(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	rv, err := s.reviewOf(ctx, x)
	if err != nil {
		_, statErr := os.Stat(x.Cwd)
		if statErr != nil || errors.Is(err, git.ErrNotRepo) {
			writeJSON(w, http.StatusOK, map[string]any{"git": false, "folder_gone": statErr != nil})
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	files, more, err := git.Changes(ctx, rv.repo.Root, rv.base)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := map[string]any{"git": true, "repo": rv.repo, "files": files, "more": more}
	if rv.wt != nil {
		ahead, behind, err := git.Counts(ctx, rv.repo.Root, rv.wt.Base, "HEAD")
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		out["worktree"], out["base_commit"], out["ahead"], out["behind"] = rv.wt, rv.base, ahead, behind
	}
	writeJSON(w, http.StatusOK, out)
}

// changed finds one of a session's changed files. Only those are diffed or discarded, so a
// path from a client never reaches anything else.
func (s *Server) changed(w http.ResponseWriter, r *http.Request, rv review, path string) (git.File, bool) {
	files, _, err := git.Changes(r.Context(), rv.repo.Root, rv.base)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return git.File{}, false
	}
	for _, f := range files {
		if f.Path == path {
			return f, true
		}
	}
	writeError(w, http.StatusNotFound, "no change to "+path)
	return git.File{}, false
}

func (s *Server) sessionDiff(w http.ResponseWriter, r *http.Request) {
	x, ok := s.lookup(w, r)
	if !ok {
		return
	}
	rv, ok := s.reviewFor(w, r, x)
	if !ok {
		return
	}
	f, ok := s.changed(w, r, rv, r.URL.Query().Get("path"))
	if !ok {
		return
	}
	patch, truncated, err := git.Patch(r.Context(), rv.repo.Root, rv.base, f, maxPatch)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"file": f, "patch": patch, "truncated": truncated})
}

func (s *Server) discardChange(w http.ResponseWriter, r *http.Request) {
	x, ok := s.lookup(w, r)
	if !ok {
		return
	}
	var body struct {
		Path string `json:"path"`
	}
	if !decode(w, r, &body) {
		return
	}
	rv, ok := s.reviewFor(w, r, x)
	if !ok {
		return
	}
	f, ok := s.changed(w, r, rv, body.Path)
	if !ok {
		return
	}
	if !f.Uncommitted {
		writeError(w, http.StatusConflict, f.Path+" is committed; only uncommitted changes can be discarded")
		return
	}
	label := fmt.Sprintf("orla: discarded %s in %s", f.Path, x.ID)
	if err := git.Discard(r.Context(), rv.repo.Root, f, label); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"stash": label})
}

func (s *Server) commitChanges(w http.ResponseWriter, r *http.Request) {
	x, ok := s.lookup(w, r)
	if !ok {
		return
	}
	var body struct {
		Message string `json:"message"`
	}
	if !decode(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.Message) == "" {
		writeError(w, http.StatusBadRequest, "message is required")
		return
	}
	rv, ok := s.reviewFor(w, r, x)
	if !ok {
		return
	}
	commit, err := git.Commit(r.Context(), rv.repo.Root, strings.TrimSpace(body.Message))
	if err != nil {
		// Nothing to commit, a hook that refused, or no identity configured: git says which.
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"commit": commit})
}

// worktreeReview is the review of a session in a worktree Orla made; anything else answers
// that there is nothing to merge.
func (s *Server) worktreeReview(w http.ResponseWriter, r *http.Request) (review, bool) {
	x, ok := s.lookup(w, r)
	if !ok {
		return review{}, false
	}
	rv, ok := s.reviewFor(w, r, x)
	if ok && rv.wt == nil {
		writeError(w, http.StatusConflict, "this session is not in a worktree Orla made")
		return rv, false
	}
	return rv, ok
}

func (s *Server) mergePreview(ctx context.Context, rv review) (map[string]any, error) {
	p, err := git.MergePreview(ctx, rv.wt.Repo, rv.wt.Base, rv.wt.Branch)
	if err != nil {
		return nil, err
	}
	uncommitted, err := git.Uncommitted(ctx, rv.repo.Root)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"preview": p, "base": rv.wt.Base, "branch": rv.wt.Branch,
		// A merge takes commits only; these files stay behind until committed.
		"uncommitted": uncommitted,
	}, nil
}

func (s *Server) previewMerge(w http.ResponseWriter, r *http.Request) {
	rv, ok := s.worktreeReview(w, r)
	if !ok {
		return
	}
	out, err := s.mergePreview(r.Context(), rv)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) mergeWorktree(w http.ResponseWriter, r *http.Request) {
	rv, ok := s.worktreeReview(w, r)
	if !ok {
		return
	}
	err := git.Merge(r.Context(), rv.wt.Repo, rv.wt.Base, rv.wt.Branch)
	if errors.Is(err, git.ErrBaseNotCheckedOut) {
		writeError(w, http.StatusConflict, rv.wt.Base+" is not checked out anywhere; check it out to merge into it")
		return
	}
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	out, err := s.mergePreview(r.Context(), rv)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// removeWorktree deletes a session's worktree folder. Uncommitted work goes to a stash and an
// unmerged branch stays, so nothing is lost.
func (s *Server) removeWorktree(w http.ResponseWriter, r *http.Request) {
	x, ok := s.lookup(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	// Not through reviewOf: a folder deleted by hand is still a worktree to clean up.
	wt, err := s.store.WorktreeFor(ctx, x.Cwd)
	if errors.Is(err, store.ErrNotFound) || (err == nil && wt.Removed != nil) {
		writeError(w, http.StatusConflict, "this session is not in a worktree Orla made")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, busy := s.sessions.Busy(x.ID); busy {
		writeError(w, http.StatusConflict, "a run is in progress in this worktree; cancel it or wait for it")
		return
	}
	if running, err := s.store.RunningRunsOf(ctx, x.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	} else if len(running) > 0 {
		writeError(w, http.StatusConflict, "Claude Code is working in this worktree's terminal; wait for it")
		return
	}
	removal, err := git.RemoveWorktree(ctx, git.Worktree{Path: wt.Path, Repo: wt.Repo, Branch: wt.Branch, Base: wt.Base})
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if err := s.store.WorktreeRemoved(ctx, wt.Path, time.Now(), removal.Stash, removal.BranchKept); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"removal": removal, "branch": wt.Branch})
}
