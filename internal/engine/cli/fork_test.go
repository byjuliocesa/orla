package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestForkPointIsTheLastMessageOfThatAnswer(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".claude", "projects", "-home-ana")
	os.MkdirAll(dir, 0o700)
	lines := []string{
		`{"type":"user","uuid":"u1","message":{"content":"first"}}`,
		`{"type":"assistant","uuid":"a1","message":{"content":[{"type":"text"}]}}`,
		`{"type":"assistant","uuid":"a2","message":{"content":[{"type":"tool_use"}]}}`,
		`{"type":"user","uuid":"t1","message":{"content":[{"type":"tool_result"}]}}`,
		`{"type":"assistant","uuid":"side","isSidechain":true,"message":{"content":[]}}`,
		`{"type":"assistant","uuid":"a3","message":{"content":[{"type":"text"}]}}`,
		`{"type":"user","uuid":"m","isMeta":true,"message":{"content":"caveat"}}`,
		`{"type":"user","uuid":"u2","message":{"content":[{"type":"text"},{"type":"image"}]}}`,
		`{"type":"assistant","uuid":"b1","message":{"content":[{"type":"text"}]}}`,
	}
	os.WriteFile(filepath.Join(dir, "conv.jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o600)

	for nth, want := range map[int]string{1: "a3", 2: "b1"} {
		if got, err := ForkPoint("conv", nth); err != nil || got != want {
			t.Errorf("ForkPoint(%d) = %q, %v; want %q", nth, got, err, want)
		}
	}
	if _, err := ForkPoint("conv", 3); err != ErrNoForkPoint {
		t.Errorf("past the end: %v", err)
	}
	if _, err := ForkPoint("missing", 1); err != ErrNoForkPoint {
		t.Errorf("no transcript: %v", err)
	}
}
