package flow

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

const erp = `---
name: fluxo-erp-bugfix
description: >
  Use when asked to fix a bug
  from an ERP task.
metadata:
  orla: flow
  title: ERP Bugfix
---

# ERP Bugfix

Say "▶ <step>" when a step begins.

## 1. Board
signal: mcp__jira__get_issue

Take the card.

## 2. Alterações
Make the change.

` + "```" + `
## not a step, inside code
` + "```" + `

## 3. PR
next: end
uses: pr-padrao-erp, commit-padrao
signal: Bash(gh pr create*)
branch: Alterações se a revisão pedir mudanças
file: steps/pr.md

Open the PR.
`

func write(t *testing.T, path, body string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadAFlow(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".claude", "skills", "erp")
	write(t, filepath.Join(dir, "SKILL.md"), erp)
	write(t, filepath.Join(dir, "steps", "pr.md"), "Title: [ERP-123] ...")
	write(t, filepath.Join(home, ".claude", "skills", "plain", "SKILL.md"), "---\nname: plain\ndescription: x\n---\nbody")

	fs := List(home, "")
	if len(fs) != 1 {
		t.Fatalf("flows = %d, want only the one marked as a flow", len(fs))
	}
	f := fs[0]
	if f.Name != "fluxo-erp-bugfix" || f.Title != "ERP Bugfix" || f.Description != "Use when asked to fix a bug from an ERP task." || f.Scope != "user" {
		t.Fatalf("head = %q %q %q %q", f.Name, f.Title, f.Description, f.Scope)
	}
	if f.Intro != `Say "▶ <step>" when a step begins.` {
		t.Fatalf("intro = %q", f.Intro)
	}
	ids := []string{}
	for _, s := range f.Steps {
		ids = append(ids, s.ID)
	}
	if !slices.Equal(ids, []string{"board", "alteracoes", "pr"}) {
		t.Fatalf("steps = %v", ids)
	}
	pr := f.Steps[2]
	if !slices.Equal(pr.Uses, []string{"pr-padrao-erp", "commit-padrao"}) || !slices.Equal(pr.Signals, []string{"Bash(gh pr create*)"}) {
		t.Fatalf("pr uses %v signals %v", pr.Uses, pr.Signals)
	}
	if len(pr.Branches) != 1 || pr.Branches[0].To != "alteracoes" || pr.Branches[0].When != "se a revisão pedir mudanças" {
		t.Fatalf("branches = %+v", pr.Branches)
	}
	if pr.Next != "end" || f.Steps[0].Next != "" {
		t.Fatalf("next = %q, %q", pr.Next, f.Steps[0].Next)
	}
	if pr.FileBody != "Title: [ERP-123] ..." || pr.Body != "Open the PR." {
		t.Fatalf("pr file %q body %q", pr.FileBody, pr.Body)
	}
	if f.Steps[1].Body == "" || !slices.Contains(f.Files, "steps/pr.md") {
		t.Fatalf("code inside a step stays its text: %q; files %v", f.Steps[1].Body, f.Files)
	}
}

func TestOffLeavesOutFlowsNotTaken(t *testing.T) {
	home := t.TempDir()
	write(t, filepath.Join(home, ".claude", "skills", "a", "SKILL.md"), "---\nname: a\nmetadata:\n  orla: flow\n---\n## x\n")
	write(t, filepath.Join(home, ".claude", "skills", "b", "SKILL.md"), "---\nname: b\nmetadata:\n  orla: flow\n---\n## x\n")
	got := Off(home, "", []string{"a"}, []string{"impeccable"})
	if !slices.Equal(got, []string{"impeccable", "b"}) {
		t.Fatalf("off = %v", got)
	}
}

func TestAStepFileCannotLeaveItsFolder(t *testing.T) {
	if _, ok := inside("/x/flow", "../../etc/passwd"); ok {
		t.Fatal("a step file outside the flow was read")
	}
}

func TestInstallLeavesTheOwnersSkillAlone(t *testing.T) {
	home := t.TempDir()
	if err := Install(home); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".claude", "skills", "newflow", "SKILL.md")
	if Kind(path) != "system" {
		t.Fatalf("installed kind = %q", Kind(path))
	}
	write(t, path, "---\nname: newflow\n---\nmine")
	Install(home)
	if b, _ := os.ReadFile(path); string(b) != "---\nname: newflow\n---\nmine" {
		t.Fatalf("the owner's skill was replaced: %q", b)
	}
	if len(List(home, "")) != 0 {
		t.Fatal("/newflow is not a flow")
	}
	for _, name := range []string{"editflow", "deleteflow", "archiveflow"} {
		if Kind(filepath.Join(home, ".claude", "skills", name, "SKILL.md")) != "system" {
			t.Fatalf("%s was not installed", name)
		}
	}
}

func TestArchivedFlowsAndSteps(t *testing.T) {
	home := t.TempDir()
	write(t, filepath.Join(Archive(home), "flows", "old", "SKILL.md"), "---\nname: old\nmetadata:\n  orla: flow\n  title: Old\n---\n## A\n## B\n")
	write(t, filepath.Join(Archive(home), "steps", "release", "tag.md"), "<!-- after: build -->\n## Tag\nsignal: Bash(git tag*)\n\nTag it.\n")
	flows, steps := Archived(home)
	if len(flows) != 1 || flows[0].Title != "Old" || len(flows[0].Steps) != 2 || flows[0].Scope != "archived" {
		t.Fatalf("flows = %+v", flows)
	}
	if len(steps) != 1 || steps[0].Flow != "release" || steps[0].Step.Title != "Tag" || steps[0].Step.Body != "Tag it." {
		t.Fatalf("steps = %+v", steps)
	}
}

func TestCommandsInAStepAreItsSignals(t *testing.T) {
	got := commandSignals([]string{"Bash(git status*)"}, "Run `git status --short | wc -l`, then `git log --oneline -5` and `ls`. The `README` is not a command.")
	want := []string{"Bash(git status*)", "Bash(git log)", "Bash(git log *)", "Bash(ls)", "Bash(ls *)"}
	if !slices.Equal(got, want) {
		t.Fatalf("signals = %v, want %v", got, want)
	}
}
