package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

const transcript = `{"type":"user","promptId":"p1","message":{"role":"user","content":"first"}}
{"type":"assistant","message":{"id":"m1","model":"claude-haiku-5-5","usage":{"input_tokens":2,"output_tokens":10,"cache_read_input_tokens":100,"cache_creation_input_tokens":50}}}
{"type":"user","promptId":"p2","message":{"role":"user","content":"second"}}
{"type":"attachment","attachment":{}}
{"type":"assistant","message":{"id":"m2","model":"claude-haiku-5-5","usage":{"input_tokens":3,"output_tokens":20,"cache_read_input_tokens":200,"cache_creation_input_tokens":5}}}
{"type":"assistant","message":{"id":"m2","model":"claude-haiku-5-5","usage":{"input_tokens":3,"output_tokens":20,"cache_read_input_tokens":200,"cache_creation_input_tokens":5}}}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t"}]}}
{"type":"user","promptId":"p2","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t2"}]}}
{"type":"assistant","message":{"id":"m3","model":"claude-sonnet-5-5","content":[{"type":"text","text":"all done"}],"usage":{"input_tokens":1,"output_tokens":5,"cache_read_input_tokens":300,"cache_creation_input_tokens":0}}}
`

func writeTranscript(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	dir := filepath.Join(home, ".claude", "projects", "-work")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "conv.jsonl")
	if err := os.WriteFile(path, []byte(transcript), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestTranscriptUsageCountsOnlyThatPrompt(t *testing.T) {
	path := writeTranscript(t)
	u, complete, err := TranscriptUsage(path, "p2", "all done")
	if err != nil || !complete {
		t.Fatalf("err %v, complete %v", err, complete)
	}
	// m2 counted once although written twice; tool results carry the same prompt id, so m3
	// counts too.
	if u["input_tokens"] != int64(4) || u["output_tokens"] != int64(25) ||
		u["cache_read_tokens"] != int64(500) || u["turns"] != 2 {
		t.Fatalf("usage = %v", u)
	}
	// Stop arrives before the last response is written: not complete yet.
	if _, complete, _ := TranscriptUsage(path, "p2", "a reply still being written"); complete {
		t.Fatal("complete before the final response is in the transcript")
	}
	if _, _, err := TranscriptUsage(path, "p-unknown", ""); err != ErrNoTranscript {
		t.Fatalf("unknown prompt: %v", err)
	}
}

// The path comes from a hook payload; Orla must not read files outside Claude's transcripts.
func TestTranscriptUsageRefusesOtherFiles(t *testing.T) {
	writeTranscript(t)
	outside := filepath.Join(t.TempDir(), "secrets.jsonl")
	_ = os.WriteFile(outside, []byte(transcript), 0o600)
	for _, p := range []string{outside, "/etc/passwd", "relative.jsonl",
		filepath.Join(os.Getenv("HOME"), ".claude", "projects", "..", "..", "x.jsonl")} {
		if _, _, err := TranscriptUsage(p, "p1", ""); err != ErrNoTranscript {
			t.Errorf("read %s: %v", p, err)
		}
	}
}

func TestToolOutputFlattensResponses(t *testing.T) {
	for raw, want := range map[string]string{
		`{"stdout":"out","stderr":"warn","interrupted":false}`: "out\nwarn",
		`"plain"`: "plain",
		`[{"type":"text","text":"a"},{"type":"image"}]`: "a\n[image]",
		`{"filePath":"/x"}`:                             `{"filePath":"/x"}`,
	} {
		if got, _ := ToolOutput(json.RawMessage(raw)); got != want {
			t.Errorf("%s -> %q, want %q", raw, got, want)
		}
	}
}
