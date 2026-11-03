package store

import (
	"context"
	"database/sql"
	"time"
)

// Worktree is a work tree Orla made, on a branch of its own. See git.Worktree.
type Worktree struct {
	Path    string     `json:"path"`
	Repo    string     `json:"repo"`
	Branch  string     `json:"branch"`
	Base    string     `json:"base"`
	Created time.Time  `json:"created"`
	Removed *time.Time `json:"removed,omitempty"`
	// Stash names the stash that holds what was uncommitted when it was removed.
	Stash string `json:"stash,omitempty"`
	// BranchKept: on removal the branch stayed, because its base lacked its commits.
	BranchKept bool `json:"branch_kept,omitempty"`
}

func (s *Store) CreateWorktree(ctx context.Context, w Worktree) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO worktrees (path, repo, branch, base, created) VALUES (?, ?, ?, ?, ?)`,
		w.Path, w.Repo, w.Branch, w.Base, formatTime(w.Created))
	return err
}

// WorktreeFor finds the worktree that holds dir, which may be a folder inside it.
func (s *Store) WorktreeFor(ctx context.Context, dir string) (Worktree, error) {
	var w Worktree
	var created string
	var removed sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT path, repo, branch, base, created, removed, stash, branch_kept FROM worktrees
		 WHERE path = ?1 OR substr(?1, 1, length(path) + 1) = path || '/'
		 ORDER BY length(path) DESC LIMIT 1`, dir).
		Scan(&w.Path, &w.Repo, &w.Branch, &w.Base, &created, &removed, &w.Stash, &w.BranchKept)
	if err == sql.ErrNoRows {
		return w, ErrNotFound
	}
	if err != nil {
		return w, err
	}
	w.Created, _ = time.Parse(time.RFC3339Nano, created)
	if removed.Valid {
		t, _ := time.Parse(time.RFC3339Nano, removed.String)
		w.Removed = &t
	}
	return w, nil
}

// WorktreeRemoved records a removal and what it kept.
func (s *Store) WorktreeRemoved(ctx context.Context, path string, at time.Time, stash string, branchKept bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE worktrees SET removed = ?, stash = ?, branch_kept = ? WHERE path = ?`,
		formatTime(at), stash, branchKept, path)
	return err
}
