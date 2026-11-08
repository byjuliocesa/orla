// Package git handles worktrees, status, live diffs and merges with a conflict preview.
//
// It runs the git executable rather than a library: the agents run the same git on the same
// repositories, so what Orla reports is what they see, including hooks and the owner's config.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

var (
	ErrNotRepo = errors.New("not a git repository")
	// ErrNoCommits: a branch or worktree needs a commit to start from.
	ErrNoCommits = errors.New("the repository has no commits yet")
)

// Error is a git command that failed, with what git said.
type Error struct {
	Args   []string
	Code   int
	Stderr string
}

func (e *Error) Error() string {
	msg := strings.TrimSpace(e.Stderr)
	if msg == "" {
		msg = fmt.Sprintf("exit status %d", e.Code)
	}
	return msg
}

func exitCode(err error) int {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return -1
}

// maxOutput bounds what one command may print into memory. A diff of a generated file can run
// to hundreds of megabytes.
const maxOutput = 16 << 20

// inherited git variables that would point every command at another repository.
var scrubbed = []string{"GIT_DIR=", "GIT_WORK_TREE=", "GIT_INDEX_FILE=", "GIT_COMMON_DIR=", "GIT_PREFIX="}

func command(ctx context.Context, dir string, args ...string) *exec.Cmd {
	// Pathspecs are literal: a file named "*.go" must never match every Go file, least of all
	// in a discard. Paths come out unquoted, and nothing is coloured.
	full := append([]string{"-C", dir, "--literal-pathspecs", "-c", "core.quotepath=off", "-c", "color.ui=false"}, args...)
	c := exec.CommandContext(ctx, "git", full...)
	env := make([]string, 0, len(os.Environ())+4)
	for _, kv := range os.Environ() {
		keep := true
		for _, p := range scrubbed {
			if strings.HasPrefix(kv, p) {
				keep = false
				break
			}
		}
		if keep {
			env = append(env, kv)
		}
	}
	c.Env = append(env,
		// Messages are parsed, and shown in the client's language, which is English.
		"LC_ALL=C",
		// Nobody can type a password or edit a message on the server.
		"GIT_TERMINAL_PROMPT=0", "GIT_EDITOR=true",
		// A status check must never take the index lock that an agent's own git command needs.
		"GIT_OPTIONAL_LOCKS=0",
	)
	return c
}

// output runs git and returns at most limit bytes of what it prints. Past the limit git is
// stopped and truncated is true.
func output(ctx context.Context, dir string, limit int, args ...string) (out []byte, truncated bool, err error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	c := command(ctx, dir, args...)
	var stderr bytes.Buffer
	c.Stderr = &stderr
	pipe, err := c.StdoutPipe()
	if err != nil {
		return nil, false, err
	}
	if err := c.Start(); err != nil {
		return nil, false, err
	}
	out, readErr := io.ReadAll(io.LimitReader(pipe, int64(limit)+1))
	if len(out) > limit {
		out, truncated = out[:limit], true
		cancel() // killed, so Wait does not hang on a pipe nobody reads
	}
	waitErr := c.Wait()
	if truncated {
		return out, true, nil
	}
	if readErr != nil {
		return nil, false, readErr
	}
	var exit *exec.ExitError
	if errors.As(waitErr, &exit) {
		return out, false, &Error{Args: args, Code: exit.ExitCode(), Stderr: stderr.String()}
	}
	return out, false, waitErr
}

func run(ctx context.Context, dir string, args ...string) (string, error) {
	out, truncated, err := output(ctx, dir, maxOutput, args...)
	if err == nil && truncated {
		err = fmt.Errorf("git %s: output over %d bytes", args[0], maxOutput)
	}
	return string(out), err
}

// Repo is the work tree that holds a folder.
type Repo struct {
	// Root is the top of the work tree.
	Root string `json:"root"`
	// Main is the repository's main work tree, which a linked worktree belongs to. It is Root
	// for the main one.
	Main string `json:"main"`
	// Branch is the branch checked out, empty when HEAD is detached.
	Branch string `json:"branch,omitempty"`
	// Head is the commit checked out, empty before the first commit.
	Head string `json:"head,omitempty"`
	// Web is the page of origin on its forge (GitHub, GitLab...), when origin is one.
	Web string `json:"web,omitempty"`
}

// webURL turns a remote's URL into its page in a browser: git@host:o/r.git and
// https://host/o/r.git both become https://host/o/r. Anything else gives "".
func webURL(remote string) string {
	r := strings.TrimSuffix(strings.TrimSpace(remote), ".git")
	switch {
	case strings.HasPrefix(r, "git@"):
		host, path, ok := strings.Cut(strings.TrimPrefix(r, "git@"), ":")
		if !ok {
			return ""
		}
		return "https://" + host + "/" + path
	case strings.HasPrefix(r, "ssh://git@"):
		return "https://" + strings.TrimPrefix(r, "ssh://git@")
	case strings.HasPrefix(r, "https://"):
		if at := strings.Index(r, "@"); at > 0 && at < strings.Index(r[8:]+"/", "/")+8 {
			r = "https://" + r[at+1:] // drop credentials
		}
		return r
	}
	return ""
}

// Open finds the work tree that holds dir.
func Open(ctx context.Context, dir string) (Repo, error) {
	out, err := run(ctx, dir, "rev-parse", "--path-format=absolute", "--show-toplevel", "--git-common-dir")
	if err != nil {
		var e *Error
		if errors.As(err, &e) && strings.Contains(e.Stderr, "not a git repository") {
			return Repo{}, ErrNotRepo
		}
		return Repo{}, err
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 2 {
		return Repo{}, fmt.Errorf("git rev-parse: unexpected output %q", out)
	}
	r := Repo{Root: lines[0], Main: lines[0]}
	if common := lines[1]; strings.HasSuffix(common, "/.git") {
		r.Main = strings.TrimSuffix(common, "/.git")
	}
	if b, err := run(ctx, dir, "symbolic-ref", "--short", "-q", "HEAD"); err == nil {
		r.Branch = strings.TrimSpace(b)
	}
	if h, err := run(ctx, dir, "rev-parse", "-q", "--verify", "HEAD^{commit}"); err == nil {
		r.Head = strings.TrimSpace(h)
	}
	if u, err := run(ctx, dir, "config", "--get", "remote.origin.url"); err == nil {
		r.Web = webURL(u)
	}
	return r, nil
}

// emptyTree is the tree with nothing in it, the base of a repository before its first commit.
// Asked of git, since it differs between SHA-1 and SHA-256 repositories.
func emptyTree(ctx context.Context, dir string) (string, error) {
	out, err := run(ctx, dir, "hash-object", "-t", "tree", "/dev/null")
	return strings.TrimSpace(out), err
}

// MergeBase is the commit where a and b last shared history.
func MergeBase(ctx context.Context, dir, a, b string) (string, error) {
	out, err := run(ctx, dir, "merge-base", a, b)
	return strings.TrimSpace(out), err
}

// Counts says how many commits b has that a lacks (ahead), and the other way round (behind).
func Counts(ctx context.Context, dir, a, b string) (ahead, behind int, err error) {
	out, err := run(ctx, dir, "rev-list", "--left-right", "--count", a+"..."+b)
	if err != nil {
		return 0, 0, err
	}
	if _, err := fmt.Sscanf(out, "%d\t%d", &behind, &ahead); err != nil {
		return 0, 0, fmt.Errorf("git rev-list: unexpected output %q", out)
	}
	return ahead, behind, nil
}

// splitZ splits NUL-terminated output.
func splitZ(out string) []string {
	out = strings.TrimSuffix(out, "\x00")
	if out == "" {
		return nil
	}
	return strings.Split(out, "\x00")
}
