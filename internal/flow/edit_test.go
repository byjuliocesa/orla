package flow

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const numberedFlow = `---
name: rel
metadata:
  orla: flow
---

Intro.

## 1. Build
signal: Bash(make*)

Build it.

## 2. Tag
branch: Build if the checks fail

Tag it.

## 3. Publish
branch: Tag if the push is refused

Push it.
`

func setup(t *testing.T) (home string, path string) {
	home = t.TempDir()
	path = filepath.Join(home, ".claude", "skills", "rel", "SKILL.md")
	write(t, path, numberedFlow)
	return home, path
}

func ids(t *testing.T, home string) []string {
	t.Helper()
	fs := List(home, "")
	if len(fs) != 1 {
		t.Fatalf("flows = %d", len(fs))
	}
	var out []string
	for _, s := range fs[0].Steps {
		out = append(out, s.ID)
	}
	return out
}

func TestArchiveAndRestoreAStep(t *testing.T) {
	home, path := setup(t)
	if err := ArchiveStep(home, "", "rel", "tag"); err != nil {
		t.Fatal(err)
	}
	if got := ids(t, home); !slices.Equal(got, []string{"build", "publish"}) {
		t.Fatalf("after archiving = %v", got)
	}
	b, _ := os.ReadFile(path)
	if strings.Contains(string(b), "Tag if the push") || !strings.Contains(string(b), "## 2. Publish") {
		t.Fatalf("the branch to it stays, or the steps were not renumbered:\n%s", b)
	}
	_, archived := Archived(home)
	if len(archived) != 1 || archived[0].Step.Title != "Tag" {
		t.Fatalf("archived = %+v", archived)
	}
	if err := RestoreStep(home, "", "rel", "tag"); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(path)
	if string(b) != numberedFlow {
		t.Fatalf("restored flow differs:\n%s", b)
	}
	if _, archived := Archived(home); len(archived) != 0 {
		t.Fatal("the archived step stays after restoring")
	}
}

func TestDeleteAStep(t *testing.T) {
	home, path := setup(t)
	if err := DeleteStep(home, "", "rel", "build"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if strings.Contains(string(b), "Build") || !strings.Contains(string(b), "## 1. Tag") {
		t.Fatalf("after deleting:\n%s", b)
	}
	if err := DeleteStep(home, "", "rel", "nothing"); err == nil {
		t.Fatal("deleting a missing step went through")
	}
}

func TestArchiveRestoreAndDeleteAFlow(t *testing.T) {
	home, _ := setup(t)
	if err := ArchiveFlow(home, "", "rel"); err != nil {
		t.Fatal(err)
	}
	if len(List(home, "")) != 0 {
		t.Fatal("the archived flow is still followed")
	}
	if flows, _ := Archived(home); len(flows) != 1 {
		t.Fatalf("archived flows = %d", len(flows))
	}
	if err := RestoreFlow(home, "rel"); err != nil {
		t.Fatal(err)
	}
	if len(ids(t, home)) != 3 {
		t.Fatal("the restored flow lost steps")
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills", "rel", origin)); err == nil {
		t.Fatal("the origin note came back with the flow")
	}
	if err := DeleteFlow(home, "", "rel"); err != nil {
		t.Fatal(err)
	}
	if len(List(home, "")) != 0 {
		t.Fatal("the deleted flow is still there")
	}
}

func TestDuplicateAFlow(t *testing.T) {
	home, _ := setup(t)
	write(t, filepath.Join(home, ".claude", "skills", "rel", "scripts", "go.sh"), "echo go")
	name, err := DuplicateFlow(home, "", "rel")
	if err != nil || name != "rel-copy" {
		t.Fatalf("duplicate = %q, %v", name, err)
	}
	again, _ := DuplicateFlow(home, "", "rel")
	if again != "rel-copy-2" {
		t.Fatalf("second copy = %q", again)
	}
	fs := List(home, "")
	if len(fs) != 3 {
		t.Fatalf("flows = %d", len(fs))
	}
	var copy Flow
	for _, f := range fs {
		if f.Name == "rel-copy" {
			copy = f
		}
	}
	if copy.Title != "rel (copy)" || len(copy.Steps) != 3 || !slices.Contains(copy.Files, "scripts/go.sh") {
		t.Fatalf("copy = %q, %d steps, files %v", copy.Title, len(copy.Steps), copy.Files)
	}
}
