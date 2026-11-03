package git

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"strings"
)

var (
	ErrBadBranch = errors.New("not a valid branch name")
	// ErrBaseNotCheckedOut: a merge runs in a work tree that has the base branch checked out.
	ErrBaseNotCheckedOut = errors.New("the base branch is not checked out in any work tree")
)

// Worktree is a work tree Orla made for an agent, on a branch of its own.
type Worktree struct {
	Path string `json:"path"`
	// Repo is the repository's main work tree.
	Repo   string `json:"repo"`
	Branch string `json:"branch"`
	// Base is the branch, or the commit when HEAD was detached, the worktree started from and
	// merges back into.
	Base string `json:"base"`
}

// AddWorktree checks out a new branch, made from what dir has checked out, in a new work tree
// at path.
func AddWorktree(ctx context.Context, dir, path, branch string) (Worktree, error) {
	repo, err := Open(ctx, dir)
	if err != nil {
		return Worktree{}, err
	}
	if repo.Head == "" {
		return Worktree{}, ErrNoCommits
	}
	if _, err := run(ctx, repo.Root, "check-ref-format", "--branch", branch); err != nil {
		return Worktree{}, fmt.Errorf("%w: %q", ErrBadBranch, branch)
	}
	base := repo.Branch
	if base == "" {
		base = repo.Head
	}
	if _, err := run(ctx, repo.Root, "worktree", "add", "--quiet", "-b", branch, path, "HEAD"); err != nil {
		return Worktree{}, err
	}
	return Worktree{Path: path, Repo: repo.Main, Branch: branch, Base: base}, nil
}

// Removal says what removing a worktree kept.
type Removal struct {
	// Stash names the stash that holds the changes that were not committed, if there were any.
	Stash string `json:"stash,omitempty"`
	// BranchKept: the branch has commits its base lacks, so it stays.
	BranchKept bool `json:"branch_kept"`
}

// RemoveWorktree deletes a worktree's folder. Nothing is lost: uncommitted changes go to a
// stash, and the branch stays while it has commits its base lacks.
func RemoveWorktree(ctx context.Context, w Worktree) (Removal, error) {
	var out Removal
	if _, err := os.Stat(w.Path); err == nil {
		if n, err := Uncommitted(ctx, w.Path); err != nil {
			return out, err
		} else if n > 0 {
			label := "orla: uncommitted work of " + w.Branch + " when its worktree was removed"
			if _, err := run(ctx, w.Path, "stash", "push", "--include-untracked", "--message", label); err != nil {
				return out, err
			}
			out.Stash = label
		}
		if _, err := run(ctx, w.Repo, "worktree", "remove", "--force", w.Path); err != nil {
			return out, err
		}
	} else if _, err := run(ctx, w.Repo, "worktree", "prune"); err != nil {
		return out, err
	}

	ahead, _, err := Counts(ctx, w.Repo, w.Base, w.Branch)
	if err != nil || ahead > 0 {
		// A branch that cannot be compared is kept too: deleting is the one thing not undone.
		out.BranchKept = true
		return out, nil
	}
	_, err = run(ctx, w.Repo, "branch", "--delete", "--force", w.Branch)
	return out, err
}

// Preview is what merging a branch into its base would do.
type Preview struct {
	// Ahead counts the branch's commits that the base lacks, which is what a merge brings.
	Ahead int `json:"ahead"`
	// Behind counts the base's commits since the branch left it.
	Behind int `json:"behind"`
	// Clean: the merge needs no hand to resolve anything.
	Clean     bool     `json:"clean"`
	Conflicts []string `json:"conflicts,omitempty"`
	// Into is the work tree that has the base checked out, where the merge runs. Empty when
	// none has.
	Into string `json:"into,omitempty"`
}

// MergePreview works out a merge of branch into base without touching any work tree, with
// `git merge-tree`.
func MergePreview(ctx context.Context, repo, base, branch string) (Preview, error) {
	var p Preview
	var err error
	if p.Ahead, p.Behind, err = Counts(ctx, repo, base, branch); err != nil {
		return p, err
	}
	if p.Into, err = checkoutOf(ctx, repo, base); err != nil {
		return p, err
	}
	if p.Ahead == 0 {
		p.Clean = true
		return p, nil
	}
	out, err := run(ctx, repo, "merge-tree", "--write-tree", "--name-only", "--no-messages", base, branch)
	if err == nil {
		p.Clean = true
		return p, nil
	}
	if exitCode(err) != 1 {
		return p, err
	}
	// Exit 1 is a conflict: the written tree's id, then one conflicted path per line.
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for _, l := range lines[1:] {
		if l != "" {
			p.Conflicts = append(p.Conflicts, l)
		}
	}
	return p, nil
}

// Merge merges branch into base, in the work tree that has base checked out. A merge that
// stops on a conflict is aborted, so that work tree is left as it was.
func Merge(ctx context.Context, repo, base, branch string) error {
	into, err := checkoutOf(ctx, repo, base)
	if err != nil {
		return err
	}
	if into == "" {
		return ErrBaseNotCheckedOut
	}
	_, err = run(ctx, into, "merge", "--no-edit", branch)
	if err != nil {
		if _, verr := run(ctx, into, "rev-parse", "-q", "--verify", "MERGE_HEAD"); verr == nil {
			_, _ = run(ctx, into, "merge", "--abort")
		}
	}
	return err
}

// checkoutOf finds the work tree that has branch checked out.
func checkoutOf(ctx context.Context, repo, branch string) (string, error) {
	out, err := run(ctx, repo, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return "", err
	}
	var path string
	for _, f := range splitZ(out) {
		switch {
		case strings.HasPrefix(f, "worktree "):
			path = strings.TrimPrefix(f, "worktree ")
		case f == "branch refs/heads/"+branch:
			return path, nil
		}
	}
	return "", nil
}

var (
	adjectives = []string{
		"amber", "brisk", "calm", "clear", "copper", "coral", "dawn", "deep", "dry", "early",
		"fair", "fern", "frost", "gentle", "golden", "green", "grey", "high", "iron", "jade",
		"keen", "late", "lime", "low", "mild", "misty", "north", "pale", "quiet", "rapid",
		"red", "salt", "sandy", "sharp", "silver", "slate", "south", "steady", "still", "swift",
		"tidal", "warm", "west", "wild", "windy",
	}
	nouns = []string{
		"anchor", "bay", "beacon", "breaker", "cape", "cliff", "cove", "current", "dune", "ebb",
		"estuary", "ferry", "fjord", "gull", "harbor", "heron", "inlet", "isle", "jetty", "kelp",
		"lagoon", "lighthouse", "marsh", "mooring", "otter", "pebble", "pier", "puffin", "quay",
		"reef", "sail", "sandbar", "seal", "shell", "shoal", "shore", "skiff", "spit", "strait",
		"surf", "swell", "tern", "tide", "wave", "wharf",
	}
)

// RandomBranch names a branch for a worktree nobody named, such as orla/quiet-harbor.
func RandomBranch() string {
	return "orla/" + adjectives[rand.IntN(len(adjectives))] + "-" + nouns[rand.IntN(len(nouns))]
}
