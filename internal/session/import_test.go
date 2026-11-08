package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/byjuliocesa/orla/internal/events"
)

func TestImportReadsATranscriptIntoRuns(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".claude", "projects", "-work")
	os.MkdirAll(dir, 0o700)
	lines := []string{
		`{"type":"mode","mode":"normal"}`,
		`{"type":"user","cwd":"/work","timestamp":"2026-11-07T20:00:00Z","message":{"content":"<system-reminder>ignore me</system-reminder>da pull"}}`,
		`{"type":"assistant","cwd":"/work","timestamp":"2026-11-07T20:00:05Z","message":{"id":"m1","content":[{"type":"text","text":"Feito."},{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"git pull"}}]}}`,
		`{"type":"user","cwd":"/work","timestamp":"2026-11-07T20:00:06Z","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}}`,
		`{"type":"attachment","cwd":"/work","timestamp":"2026-11-07T20:00:06Z","attachment":{"type":"queued_command","prompt":"[SYSTEM NOTIFICATION - NOT USER INPUT]\n<task-notification><task-id>b1</task-id></task-notification>"}}`,
		`{"type":"assistant","isSidechain":true,"cwd":"/work","message":{"content":[{"type":"text","text":"subagent"}]}}`,
		`{"type":"user","cwd":"/work","timestamp":"2026-11-07T20:00:07Z","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"},{"type":"text","text":"The user sent a new message while you were working:\nmuda a cor\n\nThis is how Claude Code surfaces messages the user sends mid-turn."}]}}`,
		`{"type":"attachment","cwd":"/work","timestamp":"2026-11-07T20:00:08Z","attachment":{"type":"queued_command","prompt":[{"type":"image"},{"type":"text","text":"e o botão"}]}}`,
		`{"type":"assistant","cwd":"/work","timestamp":"2026-11-07T20:00:09Z","message":{"id":"m3","content":[{"type":"text","text":"Ajustado."}]}}`,
		`{"type":"user","cwd":"/work","timestamp":"2026-11-07T20:10:00Z","message":{"content":[{"type":"text","text":"agora roda"},{"type":"image"}]}}`,
		`{"type":"assistant","cwd":"/work","timestamp":"2026-11-07T20:10:03Z","message":{"id":"m2","content":[{"type":"text","text":"Rodando."}]}}`,
	}
	os.WriteFile(filepath.Join(dir, "conv-1.jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o600)

	f := setup(t, &fakeRunner{})
	ctx := context.Background()
	sess, err := f.svc.Import(ctx, "conv-1", "Imported")
	if err != nil {
		t.Fatal(err)
	}
	if sess.Cwd != "/work" || sess.ForkOf != "conv-1" || sess.Title != "Imported" {
		t.Fatalf("session = %+v", sess)
	}
	runs, _ := f.store.Runs(ctx, sess.ID)
	if len(runs) != 4 || runs[0].Prompt != "da pull" || runs[1].Prompt != "muda a cor" || runs[2].Prompt != "e o botão\n\n[image]" || runs[3].Prompt != "agora roda\n\n[image]" || runs[0].Status != "done" {
		t.Fatalf("runs = %+v", runs)
	}
	evs, _ := f.store.After(ctx, sess.ID, 0, 1000)
	var kinds []string
	for _, e := range evs {
		kinds = append(kinds, e.Type)
		if strings.Contains(string(e.Data), "subagent") {
			t.Error("a sidechain message was imported")
		}
	}
	want := []string{events.RunStarted, events.MessageAssistant, events.ToolCall, events.ToolResult, events.RunFinished, events.RunStarted, events.MessageAssistant, events.RunFinished}
	got := strings.Join(kinds, " ")
	for _, k := range want {
		if !strings.Contains(got, k) {
			t.Errorf("events %q lack %s", got, k)
		}
	}
	if evs[0].Time.Format("15:04") != "20:00" {
		t.Errorf("first event at %v, want the transcript's time", evs[0].Time)
	}
	if _, err := f.svc.Import(ctx, "missing", ""); err != ErrNoTranscript {
		t.Errorf("missing transcript: %v", err)
	}
}

func TestFlowReferencesAreExplainedOnlyWhenUsed(t *testing.T) {
	if System("fix it") != "" {
		t.Fatal("a prompt without a flow reference got a system note")
	}
	if !strings.Contains(System("change @flow:git-sync#pull please"), "@flow:<name>#<step>") {
		t.Fatal("a flow reference went unexplained")
	}
}
