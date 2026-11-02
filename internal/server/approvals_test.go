package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Julio0Cesar/orla/internal/engine/cli"
	"github.com/Julio0Cesar/orla/internal/events"
	"github.com/Julio0Cesar/orla/internal/session"
)

// slowClaude stays in progress long enough to be asked over MCP, and records each run's
// arguments in the directory it returns.
func slowClaude(t *testing.T) (cli.Claude, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "claude")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + dir + "/args-$ORLA_RUN\nsleep 30\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return cli.Claude{Path: path}, dir
}

func startSlowRun(t *testing.T, srv *httptest.Server) (session, run string) {
	t.Helper()
	var sess struct{ ID string }
	_ = json.NewDecoder(request(t, srv, "POST", "/v1/sessions", testToken,
		map[string]string{"cwd": t.TempDir()}).Body).Decode(&sess)
	var r struct{ ID string }
	res := request(t, srv, "POST", "/v1/sessions/"+sess.ID+"/runs", testToken, map[string]string{"prompt": "x"})
	_ = json.NewDecoder(res.Body).Decode(&r)
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("start = %d", res.StatusCode)
	}
	return sess.ID, r.ID
}

func rpc(t *testing.T, ctx context.Context, srv *httptest.Server, run, secret, body string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequestWithContext(ctx, "POST", srv.URL+"/mcp/runs/"+run, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

func waitRunAccess(t *testing.T, sessions *session.Service, run string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if secret, ok := sessions.RunAccess(run); ok {
			return secret
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("run never got MCP access")
	return ""
}

// The MCP endpoint answers only the run it belongs to, with that run's secret, while it runs.
func TestRunMCPEndpointIsBoundToItsRun(t *testing.T) {
	claude, argsDir := slowClaude(t)
	srv, sessions := newServerWith(t, claude)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sessA, runA := startSlowRun(t, srv)
	_, runB := startSlowRun(t, srv)
	secretA := waitRunAccess(t, sessions, runA)
	waitRunAccess(t, sessions, runB)

	init := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25"}}`
	for _, c := range []struct {
		run, secret string
	}{{runA, ""}, {runA, "wrong"}, {runB, secretA}, {runA, testToken}} {
		if code, _ := rpc(t, ctx, srv, c.run, c.secret, init); code != http.StatusUnauthorized {
			t.Errorf("run %s with secret %q = %d, want 401", c.run, c.secret, code)
		}
	}

	// The CLI is told where to ask, with the secret, and which tool answers prompts.
	time.Sleep(200 * time.Millisecond)
	args, _ := os.ReadFile(filepath.Join(argsDir, "args-"+runA))
	if !strings.Contains(string(args), "/mcp/runs/"+runA) || !strings.Contains(string(args), secretA) ||
		!strings.Contains(string(args), "--permission-prompt-tool\nmcp__orla__approve") {
		t.Fatalf("CLI args miss the approval endpoint:\n%s", args)
	}

	code, res := rpc(t, ctx, srv, runA, secretA, init)
	if code != http.StatusOK || res["result"].(map[string]any)["protocolVersion"] != "2025-11-25" {
		t.Fatalf("initialize = %d %v", code, res)
	}
	if code, _ := rpc(t, ctx, srv, runA, secretA, `{"jsonrpc":"2.0","method":"notifications/initialized"}`); code != http.StatusAccepted {
		t.Fatalf("notification = %d", code)
	}
	_, res = rpc(t, ctx, srv, runA, secretA, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	if tools := res["result"].(map[string]any)["tools"].([]any); len(tools) != 1 ||
		tools[0].(map[string]any)["name"] != "approve" {
		t.Fatalf("tools = %v", tools)
	}

	// The CLI asks; the owner answers through the API; the CLI gets the answer it expects.
	answer := make(chan map[string]any, 1)
	go func() {
		_, res := rpc(t, ctx, srv, runA, secretA, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"approve","arguments":{"tool_name":"Bash","input":{"command":"rm -rf build"},"tool_use_id":"toolu_9"}}}`)
		answer <- res
	}()
	body := streamEvents(t, ctx, srv, sessA, "")
	defer body.Close()
	requested := readSSE(t, body, events.ApprovalRequested)
	var d struct {
		ApprovalID string `json:"approval_id"`
	}
	_ = json.Unmarshal(requested[len(requested)-1].Data, &d)
	if res := request(t, srv, "POST", "/v1/sessions/"+sessA+"/approvals/"+d.ApprovalID, testToken,
		map[string]any{"allow": true, "by": "test"}); res.StatusCode != http.StatusNoContent {
		t.Fatalf("resolve = %d", res.StatusCode)
	}
	res = <-answer
	text := res["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	var verdict map[string]any
	_ = json.Unmarshal([]byte(text), &verdict)
	if verdict["behavior"] != "allow" || verdict["updatedInput"].(map[string]any)["command"] != "rm -rf build" {
		t.Fatalf("permission answer = %s", text)
	}
	if res := request(t, srv, "POST", "/v1/sessions/"+sessA+"/approvals/"+d.ApprovalID, testToken,
		map[string]any{"allow": false}); res.StatusCode != http.StatusNotFound {
		t.Fatalf("second answer = %d, want 404", res.StatusCode)
	}

	// Once the run ends, its endpoint is closed.
	_ = request(t, srv, "POST", "/v1/sessions/"+sessA+"/cancel", testToken, nil)
	deadline := time.Now().Add(8 * time.Second)
	for {
		code, _ := rpc(t, ctx, srv, runA, secretA, init)
		if code == http.StatusUnauthorized {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("endpoint still open after the run ended")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// terminalHookToken opens a terminal and reads the hook token it was given, as the hook command
// in it would.
func terminalHookToken(t *testing.T, ctx context.Context, srv *httptest.Server) (termID, token string) {
	t.Helper()
	info := openTerminal(t, srv, []string{"sh", "-c", `echo "token=$ORLA_HOOK_TOKEN."; sleep 60`})
	c, err := dial(t, ctx, srv, info.ID, http.Header{"Authorization": {"Bearer " + testToken}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	var out strings.Builder
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		out.Write(data)
		if m := regexp.MustCompile(`token=(\S+)\.`).FindStringSubmatch(out.String()); m != nil {
			return info.ID, m[1]
		}
	}
}

func sendHook(ctx context.Context, srv *httptest.Server, termID, token, payload string) (*http.Response, error) {
	req, _ := http.NewRequestWithContext(ctx, "POST", srv.URL+"/hooks/claude", strings.NewReader(payload))
	req.Header.Set("X-Orla-Terminal", termID)
	req.Header.Set("Authorization", "Bearer "+token)
	return srv.Client().Do(req)
}

// A permission prompt of a CLI in a terminal is held open until the owner answers through the
// API, and the answer comes back in the form Claude Code reads from the hook.
func TestTerminalPromptWaitsForTheOwner(t *testing.T) {
	srv := newServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	termID, token := terminalHookToken(t, ctx, srv)

	const ask = `{"hook_event_name":"PermissionRequest","session_id":"n1","prompt_id":"p1","cwd":"/w",
		"tool_name":"Bash","tool_input":{"command":"make deploy"}}`
	type reply struct {
		code int
		body string
	}
	replies := make(chan reply, 1)
	go func() {
		res, err := sendHook(ctx, srv, termID, token, ask)
		if err != nil {
			replies <- reply{code: -1, body: err.Error()}
			return
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		replies <- reply{res.StatusCode, string(b)}
	}()

	var list struct{ Sessions []struct{ ID string } }
	for len(list.Sessions) == 0 && ctx.Err() == nil {
		time.Sleep(20 * time.Millisecond)
		_ = json.NewDecoder(request(t, srv, "GET", "/v1/sessions", testToken, nil).Body).Decode(&list)
	}
	if len(list.Sessions) == 0 {
		t.Fatal("the hook created no session")
	}
	sessions := list.Sessions
	body := streamEvents(t, ctx, srv, sessions[0].ID, "")
	defer body.Close()
	got := readSSE(t, body, events.ApprovalRequested)
	var req struct {
		ApprovalID string `json:"approval_id"`
		Terminal   string `json:"terminal"`
	}
	_ = json.Unmarshal(got[len(got)-1].Data, &req)
	if req.Terminal != termID {
		t.Fatalf("request names terminal %q, want %s", req.Terminal, termID)
	}

	res := request(t, srv, "POST", "/v1/sessions/"+sessions[0].ID+"/approvals/"+req.ApprovalID, testToken,
		map[string]any{"allow": false, "message": "not today", "by": "web"})
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("resolve = %d", res.StatusCode)
	}
	r := <-replies
	want := `{"hookSpecificOutput":{"decision":{"behavior":"deny","message":"The user denied this from Orla, their remote approval tool. Their note: not today"},"hookEventName":"PermissionRequest"}}`
	if r.code != http.StatusOK || strings.TrimSpace(r.body) != want {
		t.Fatalf("hook got %d %s", r.code, r.body)
	}

	// A prompt that ends before anyone answers gets no answer: the terminal decides.
	go func() {
		res, err := sendHook(ctx, srv, termID, token, ask)
		if err != nil {
			replies <- reply{code: -1, body: err.Error()}
			return
		}
		res.Body.Close()
		replies <- reply{code: res.StatusCode}
	}()
	readSSE(t, body, events.ApprovalRequested)
	res2, err := sendHook(ctx, srv, termID, token,
		`{"hook_event_name":"UserPromptSubmit","session_id":"n1","prompt_id":"p2","cwd":"/w","prompt":"other"}`)
	if err != nil {
		t.Fatal(err)
	}
	res2.Body.Close()
	if r := <-replies; r.code != http.StatusNoContent {
		t.Fatalf("hook of an ended prompt got %d %s", r.code, r.body)
	}
}
