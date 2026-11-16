package library

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeRepo(t *testing.T) {
	for in, want := range map[string]string{
		"git@bitbucket.org:vxcaselabs/app.git":      "bitbucket.org/vxcaselabs/app",
		"https://user@bitbucket.org/vxcaselabs/app": "bitbucket.org/vxcaselabs/app",
		"ssh://git@GitHub.com:22/you/app.git":       "github.com/you/app",
		"github.com/you/app":                        "github.com/you/app",
		"app":                                       "",
	} {
		if got := normalizeRepo(in); got != want {
			t.Errorf("normalizeRepo(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWriteEnvsMergesAndLeavesOwnFilesAlone(t *testing.T) {
	ctx := context.Background()
	m := keyed(t, t.TempDir())
	repo := t.TempDir()
	gitT(t, repo, "init", "-q")
	gitT(t, repo, "remote", "add", "origin", "git@github.com:you/app.git")

	// A shared environment, and one only this repository uses, which wins where they overlap.
	if err := m.SaveEnv(ctx, "shared", EnvInput{Values: map[string]string{"API": "https://api", "MODE": "dev"}, Links: []EnvLink{{"github.com/you/app", ""}, {"github.com/you/other", ""}}}); err != nil {
		t.Fatal(err)
	}
	if err := m.SaveEnv(ctx, "app", EnvInput{Values: map[string]string{"MODE": "local", "SECRET": "a b"}, Links: []EnvLink{{"https://github.com/you/app", ".env"}}}); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{filepath.Join(m.Root, EnvDir, envsFile), m.mcpPath(mcpSecrets)} {
		if b, _ := os.ReadFile(f); strings.Contains(string(b), "https://api") {
			t.Fatalf("%s holds a value in the clear", f)
		}
	}
	if err := m.WriteEnvs(ctx, repo); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(repo, ".env"))
	for _, want := range []string{"API=https://api\n", "MODE=local\n", `SECRET="a b"` + "\n"} {
		if !strings.Contains(string(b), want) {
			t.Fatalf(".env lacks %q:\n%s", want, b)
		}
	}
	if _, err := gitOut(ctx, repo, "check-ignore", "-q", ".env"); err != nil {
		t.Fatal(".env is not kept out of git")
	}

	// An empty value keeps the saved one; a key left out goes.
	if err := m.SaveEnv(ctx, "app", EnvInput{Values: map[string]string{"MODE": ""}, Links: []EnvLink{{"github.com/you/app", ".env"}}}); err != nil {
		t.Fatal(err)
	}
	sec, _, _ := m.openSecrets()
	if v := sec.Envs["app"]; v["MODE"] != "local" || v["SECRET"] != "" {
		t.Fatalf("app = %v", v)
	}

	// A .env the owner made is never overwritten.
	other := t.TempDir()
	gitT(t, other, "init", "-q")
	gitT(t, other, "remote", "add", "origin", "https://github.com/you/other")
	_ = os.WriteFile(filepath.Join(other, ".env"), []byte("MINE=1\n"), 0o644)
	if err := m.WriteEnvs(ctx, other); err == nil {
		t.Fatal("overwrote the owner's .env without a word")
	}
	if b, _ := os.ReadFile(filepath.Join(other, ".env")); string(b) != "MINE=1\n" {
		t.Fatalf("the owner's .env changed: %s", b)
	}
}

func TestWriteEnvsAroundReachesReposBelow(t *testing.T) {
	ctx := context.Background()
	m := keyed(t, t.TempDir())
	home := t.TempDir()
	repo := filepath.Join(home, "repos", "app")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	gitT(t, repo, "init", "-q")
	gitT(t, repo, "remote", "add", "origin", "git@github.com:you/app.git")
	if err := m.SaveEnv(ctx, "app", EnvInput{Values: map[string]string{"A": "1"}, Links: []EnvLink{{"github.com/you/app", ".env"}}}); err != nil {
		t.Fatal(err)
	}
	if err := m.WriteEnvsAround(ctx, home); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(repo, ".env")); !strings.Contains(string(b), "A=1") {
		t.Fatalf("the repository two levels down got no .env: %q", b)
	}
}
