package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/byjuliocesa/orla/internal/events"
)

func parseFixture(t *testing.T, name string) ([]Out, Outcome) {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	p := newClaudeParser()
	var out []Out
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		out = append(out, p.Line(sc.Bytes())...)
	}
	return out, p.outcome
}

func data(t *testing.T, o Out) map[string]any {
	t.Helper()
	b, _ := json.Marshal(o.Data)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m
}

func TestClaudeStreamBecomesEvents(t *testing.T) {
	out, outcome := parseFixture(t, "claude-tool-run.jsonl")

	var types []string
	for _, o := range out {
		types = append(types, o.Type)
	}
	want := []string{
		events.MessageAssistant, events.Usage, // msg_A, first block, usage counted here
		events.ToolCall,         // msg_A again: same usage, not counted twice
		events.Usage,            // rate limits
		events.ToolResult,       // ok
		events.ToolResult,       // failed, from a subagent
		events.MessageAssistant, // msg_B
		events.Usage,            // msg_B usage
		events.Usage,            // run total
	}
	if strings.Join(types, " ") != strings.Join(want, " ") {
		t.Fatalf("event types\n got: %v\nwant: %v", types, want)
	}

	if d := data(t, out[2]); d["name"] != "Bash" || d["call_id"] != "toolu_1" {
		t.Errorf("tool call = %v", d)
	}
	if d := data(t, out[3]); d["scope"] != "account" {
		t.Errorf("limits = %v", d)
	}
	if d := data(t, out[4]); d["ok"] != true || d["output"] != "probe-ok" {
		t.Errorf("tool result = %v", d)
	}
	if d := data(t, out[5]); d["ok"] != false || d["output"] != "permission denied" || d["parent_call"] != "toolu_task" {
		t.Errorf("failed subagent tool result = %v", d)
	}
	// The fixture resumes a conversation: total_cost_usd (0.0071) includes earlier runs, the
	// run's own cost is the sum over the models it used.
	if d := data(t, out[8]); d["scope"] != "run" || d["list_cost_usd"].(float64)-0.0029 > 1e-9 ||
		d["list_cost_usd"].(float64)-0.0029 < -1e-9 || d["cache_read_tokens"] != 1900.0 {
		t.Errorf("run usage = %v", d)
	}

	if outcome.Status != "done" || outcome.Summary != "done" ||
		outcome.NativeSession != "11111111-2222-4333-8444-555555555555" {
		t.Errorf("outcome = %+v", outcome)
	}
}

func TestToolOutputIsTruncatedOnACharacterBoundary(t *testing.T) {
	s, cut := truncate(strings.Repeat("a", maxToolOutput-1)+"é", maxToolOutput)
	if !cut || !strings.HasSuffix(s, "a") || len(s) != maxToolOutput-1 {
		t.Fatalf("cut=%v len=%d tail=%q", cut, len(s), s[len(s)-2:])
	}
}

// fakeClaude writes an executable standing in for the claude CLI.
func fakeClaude(t *testing.T, script string) Claude {
	t.Helper()
	path := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	return Claude{Path: path}
}

func collect(c Claude, ctx context.Context, spec Spec) ([]Out, Outcome, []string) {
	var out []Out
	var natives []string
	outcome := c.Run(ctx, spec, func(o Out) { out = append(out, o) }, func(n string) { natives = append(natives, n) })
	return out, outcome, natives
}

func TestRunPassesResumeAndReportsSessionEarly(t *testing.T) {
	fixture, _ := filepath.Abs(filepath.Join("testdata", "claude-tool-run.jsonl"))
	argsFile := filepath.Join(t.TempDir(), "args")
	c := fakeClaude(t, `printf '%s\n' "$@" > `+argsFile+`; cat `+fixture+"\n")

	_, outcome, natives := collect(c, context.Background(), Spec{
		Cwd: t.TempDir(), Prompt: "do it", Resume: "prev-session", PermissionMode: "acceptEdits",
	})
	if outcome.Status != "done" {
		t.Fatalf("outcome = %+v", outcome)
	}
	if len(natives) != 1 || natives[0] != "11111111-2222-4333-8444-555555555555" {
		t.Errorf("native sessions reported = %v", natives)
	}
	args, _ := os.ReadFile(argsFile)
	for _, want := range []string{"-p\ndo it\n", "--resume\nprev-session\n", "--permission-mode\nacceptEdits\n", "stream-json"} {
		if !strings.Contains(string(args), want) {
			t.Errorf("args miss %q:\n%s", want, args)
		}
	}
}

// When the CLI cannot run (not logged in, unknown flag), the owner must see its own words.
func TestRunWithoutResultFailsWithTheCLIsError(t *testing.T) {
	c := fakeClaude(t, "echo 'Invalid API key. Please run /login' >&2; exit 1\n")
	out, outcome, _ := collect(c, context.Background(), Spec{Cwd: t.TempDir(), Prompt: "x"})

	if outcome.Status != "failed" || !strings.Contains(outcome.Summary, "Please run /login") {
		t.Fatalf("outcome = %+v", outcome)
	}
	if len(out) != 1 || out[0].Type != events.Error {
		t.Fatalf("events = %+v", out)
	}
}

func TestMissingExecutableFails(t *testing.T) {
	c := Claude{Path: filepath.Join(t.TempDir(), "no-such-claude")}
	_, outcome, _ := collect(c, context.Background(), Spec{Cwd: t.TempDir(), Prompt: "x"})
	if outcome.Status != "failed" || !strings.Contains(outcome.Summary, "could not start") {
		t.Fatalf("outcome = %+v", outcome)
	}
}

// Cancelling must stop the CLI and the tools it started, not just stop listening to them.
func TestCancelStopsTheCLIAndItsChildren(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "child-survived")
	c := fakeClaude(t, `echo '{"type":"system","subtype":"init","session_id":"s1"}'
(sleep 2; touch `+marker+`) &
sleep 30
`)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(300*time.Millisecond, cancel)

	start := time.Now()
	_, outcome, natives := collect(c, ctx, Spec{Cwd: t.TempDir(), Prompt: "x"})
	if outcome.Status != "cancelled" {
		t.Fatalf("outcome = %+v", outcome)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("cancel took %s", time.Since(start))
	}
	if len(natives) != 1 {
		t.Errorf("session id from before the cancel was lost: %v", natives)
	}

	time.Sleep(2500 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a process the CLI started kept running after cancel")
	}
}
