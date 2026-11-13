package library

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitT(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func skill(t *testing.T, dir, name, desc string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: " + name + "\ndescription: " + desc + "\n---\n\nBody.\n"
	if err := os.WriteFile(filepath.Join(dir, name, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// machine is one computer running Orla: a home with Claude Code and Codex, and its library.
func machine(t *testing.T) *Manager {
	t.Helper()
	home := t.TempDir()
	for _, d := range []string{".claude/skills", ".codex"} {
		if err := os.MkdirAll(filepath.Join(home, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return &Manager{Root: filepath.Join(home, "lib"), Home: home, Settings: filepath.Join(home, "lib.json")}
}

func TestLibraryTravelsBetweenMachines(t *testing.T) {
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@t")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@t")
	ctx := context.Background()
	remote := filepath.Join(t.TempDir(), "remote.git")
	gitT(t, "", "init", "--bare", "-b", "main", remote)

	a := machine(t)
	skill(t, filepath.Join(a.Home, ".claude/skills"), "taste", "Design taste.")
	skill(t, filepath.Join(a.Home, ".claude/skills"), "only-claude", "Claude's own.")

	s, err := a.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if s.State != "off" || len(s.Outside) != 2 {
		t.Fatalf("before connecting: state %q, outside %+v", s.State, s.Outside)
	}
	if err := Adopt(a.Root, a.Home, "claude", "taste", Shared); err != nil {
		t.Fatal(err)
	}
	if err := Adopt(a.Root, a.Home, "claude", "only-claude", "claude"); err != nil {
		t.Fatal(err)
	}
	// Shared reaches both agents; an agent's own reaches only it.
	for _, p := range []string{".claude/skills/taste", ".codex/skills/taste", ".claude/skills/only-claude"} {
		if _, err := os.Stat(filepath.Join(a.Home, p, "SKILL.md")); err != nil {
			t.Fatalf("%s not linked: %v", p, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(a.Home, ".codex/skills/only-claude")); err == nil {
		t.Fatal("Claude's own skill reached Codex")
	}

	if err := a.Connect(ctx, remote); err != nil {
		t.Fatal(err)
	}
	if s, _ := a.Status(ctx); s.State != "synced" || s.Remote != remote {
		t.Fatalf("after connecting: %+v", s)
	}

	// A second machine joins with an empty library and gets everything, linked.
	b := machine(t)
	if err := b.Connect(ctx, remote); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(b.Home, ".codex/skills/taste/SKILL.md")); err != nil {
		t.Fatalf("taste did not reach the second machine: %v", err)
	}

	// An edit on b, through the agent's link, travels back to a; a deletion too.
	skill(t, filepath.Join(b.Home, ".claude/skills"), "taste", "Design taste, revised.")
	if err := os.RemoveAll(filepath.Join(b.Root, "claude", "only-claude")); err != nil {
		t.Fatal(err)
	}
	if err := b.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if err := a.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(a.Home, ".claude/skills/taste/SKILL.md"))
	if want := "Design taste, revised."; !strings.Contains(string(got), want) {
		t.Fatalf("a has %q", got)
	}
	if _, err := os.Lstat(filepath.Join(a.Home, ".claude/skills/only-claude")); err == nil {
		t.Fatal("the deleted skill's link stayed on a")
	}

	// Both change the same line: the sync stops and names the file, touching nothing.
	skill(t, filepath.Join(a.Root, Shared), "taste", "From a.")
	skill(t, filepath.Join(b.Root, Shared), "taste", "From b.")
	if err := a.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	err = b.Sync(ctx)
	var c *ConflictError
	if !errors.As(err, &c) || len(c.Files) != 1 || c.Files[0] != "shared/taste/SKILL.md" {
		t.Fatalf("conflict: %v", err)
	}
	if s, _ := b.Status(ctx); s.State != "conflict" {
		t.Fatalf("state after conflict: %q", s.State)
	}
	got, _ = os.ReadFile(filepath.Join(b.Root, Shared, "taste", "SKILL.md"))
	if !strings.Contains(string(got), "From b.") {
		t.Fatalf("b's file changed: %q", got)
	}
}

func TestConnectRefusesAddressesThatAreOptions(t *testing.T) {
	m := machine(t)
	for _, bad := range []string{"", "--upload-pack=touch /tmp/x", "ext::sh -c touch% /tmp/x", "a b"} {
		if err := m.Connect(context.Background(), bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}

func TestRedact(t *testing.T) {
	if got := redact("https://user:tok@github.com/o/r.git"); got != "https://github.com/o/r.git" {
		t.Fatal(got)
	}
	if got := redact("git@github.com:o/r.git"); got != "git@github.com:o/r.git" {
		t.Fatal(got)
	}
}

func TestMachineKeyIsMadeOnceAndUsedBySSH(t *testing.T) {
	m := machine(t)
	m.Keys = filepath.Join(m.Home, "keys")
	a, err := m.PublicKey(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	b, _ := m.PublicKey(context.Background())
	if !strings.HasPrefix(a, "ssh-ed25519 ") || a != b {
		t.Fatalf("keys %q then %q", a, b)
	}
	if c := m.sshCommand(); !strings.Contains(c, "-i '"+m.keyPath()+"'") || !strings.Contains(c, "StrictHostKeyChecking=accept-new") {
		t.Fatalf("ssh command: %s", c)
	}
}

func TestExplainSkipsGitsClosingAdvice(t *testing.T) {
	got := explain("Host key verification failed.\nfatal: Could not read from remote repository.\n\nPlease make sure you have the correct access rights\nand the repository exists.\n", errors.New("exit 128"))
	if got != "the repository's host did not prove who it is to this machine" {
		t.Fatal(got)
	}
	got = explain("fatal: something odd\nPlease make sure you have the correct access rights\nand the repository exists.", errors.New("x"))
	if got != "something odd" {
		t.Fatal(got)
	}
}
