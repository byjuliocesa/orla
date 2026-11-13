package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"github.com/byjuliocesa/orla/internal/automation"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/byjuliocesa/orla/internal/engine/cli"
	"github.com/byjuliocesa/orla/internal/events"
	"github.com/byjuliocesa/orla/internal/session"
	"github.com/byjuliocesa/orla/internal/store"
	"github.com/byjuliocesa/orla/internal/terminal"
)

const testToken = "orla_test-token-not-a-secret"

// fakeClaude stands in for the claude CLI: it prints a recorded stream-json run.
func fakeClaude(t *testing.T) cli.Claude {
	t.Helper()
	fixture, _ := filepath.Abs("../engine/cli/testdata/claude-tool-run.jsonl")
	path := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(path, []byte("#!/bin/sh\ncat "+fixture+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return cli.Claude{Path: path}
}

func newServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv, _ := newServerWith(t, fakeClaude(t))
	return srv
}

func newServerWith(t *testing.T, claude cli.Claude) (*httptest.Server, *session.Service) {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "orla.db"))
	if err != nil {
		t.Fatal(err)
	}
	bus := events.NewBus(st)
	sessions := session.NewService(log, st, bus, map[string]session.Runner{"claude": claude})
	terminals := terminal.NewManager()
	automations := &automation.Manager{Bus: bus, Token: testToken}
	srv := httptest.NewServer(New(Deps{
		Log: log, Token: testToken, Terminals: terminals, Store: st, Bus: bus, Sessions: sessions,
		Worktrees: filepath.Join(t.TempDir(), "worktrees"), Uploads: filepath.Join(t.TempDir(), "uploads"),
		Automations: automations,
	}))
	t.Cleanup(func() {
		automations.Close()
		srv.CloseClientConnections()
		srv.Close()
		terminals.CloseAll()
		sessions.Close()
		st.Close()
	})
	sessions.ApprovalsAt(srv.URL + "/mcp/runs/")
	return srv, sessions
}

func request(t *testing.T, srv *httptest.Server, method, path, token string, body any) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, srv.URL+path, reader)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { res.Body.Close() })
	return res
}

func TestEveryRouteButHealthNeedsTheToken(t *testing.T) {
	srv := newServer(t)

	if res := request(t, srv, "GET", "/health", "", nil); res.StatusCode != http.StatusOK {
		t.Fatalf("health = %d", res.StatusCode)
	}

	routes := []struct{ method, path string }{
		{"GET", "/v1/terminals"},
		{"POST", "/v1/terminals"},
		{"DELETE", "/v1/terminals/term_x"},
		{"GET", "/v1/terminals/term_x/stream"},
		{"GET", "/v1/library"},
		{"PATCH", "/v1/library"},
		{"POST", "/v1/library/sync"},
		{"GET", "/v1/sessions"},
		{"POST", "/v1/sessions"},
		{"GET", "/v1/sessions/ses_x"},
		{"PATCH", "/v1/sessions/ses_x"},
		{"DELETE", "/v1/sessions/ses_x"},
		{"POST", "/v1/sessions/ses_x/runs"},
		{"POST", "/v1/sessions/ses_x/cancel"},
		{"GET", "/v1/sessions/ses_x/events"},
		{"POST", "/v1/sessions/ses_x/approvals/apr_x"},
		{"GET", "/v1/events"},
		{"GET", "/v1/approvals"},
		{"GET", "/v1/git?path=/tmp"},
		{"POST", "/v1/worktrees"},
		{"GET", "/v1/sessions/ses_x/changes"},
		{"GET", "/v1/sessions/ses_x/diff?path=a"},
		{"POST", "/v1/sessions/ses_x/discard"},
		{"POST", "/v1/sessions/ses_x/commit"},
		{"GET", "/v1/sessions/ses_x/merge"},
		{"POST", "/v1/sessions/ses_x/merge"},
		{"DELETE", "/v1/sessions/ses_x/worktree"},
		{"GET", "/v1/anything"},
	}
	for _, token := range []string{"", "wrong", testToken + "x"} {
		for _, r := range routes {
			if res := request(t, srv, r.method, r.path, token, nil); res.StatusCode != http.StatusUnauthorized {
				t.Errorf("%s %s with token %q = %d, want 401", r.method, r.path, token, res.StatusCode)
			}
		}
	}

	// The terminal must not have been opened by the refused POSTs.
	var list struct{ Terminals []terminal.Info }
	_ = json.NewDecoder(request(t, srv, "GET", "/v1/terminals", testToken, nil).Body).Decode(&list)
	if len(list.Terminals) != 0 {
		t.Fatalf("refused requests opened terminals: %+v", list.Terminals)
	}
}

func dial(t *testing.T, ctx context.Context, srv *httptest.Server, id string, header http.Header) (*websocket.Conn, error) {
	t.Helper()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/v1/terminals/" + id + "/stream"
	c, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: header})
	return c, err
}

func readUntil(t *testing.T, ctx context.Context, c *websocket.Conn, want string) {
	t.Helper()
	var got strings.Builder
	for !strings.Contains(got.String(), want) {
		typ, data, err := c.Read(ctx)
		if err != nil {
			t.Fatalf("waiting for %q: %v; got %q", want, err, got.String())
		}
		if typ == websocket.MessageBinary {
			got.Write(data)
		}
	}
}

// A browser on another site must not be able to open a shell, even if it could send the token.
func TestStreamRefusesCrossOriginBrowsers(t *testing.T) {
	srv := newServer(t)
	var info terminal.Info
	_ = json.NewDecoder(request(t, srv, "POST", "/v1/terminals", testToken,
		terminal.Spec{Argv: []string{"sh"}}).Body).Decode(&info)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := dial(t, ctx, srv, info.ID, http.Header{
		"Authorization": {"Bearer " + testToken},
		"Origin":        {"https://evil.example"},
	})
	if err == nil {
		t.Fatal("cross-origin stream was accepted")
	}
}

func TestTerminalOutlivesItsStream(t *testing.T) {
	srv := newServer(t)
	auth := http.Header{"Authorization": {"Bearer " + testToken}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	res := request(t, srv, "POST", "/v1/terminals", testToken,
		terminal.Spec{Argv: []string{"sh"}, Cwd: t.TempDir()})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("open = %d", res.StatusCode)
	}
	var info terminal.Info
	_ = json.NewDecoder(res.Body).Decode(&info)

	c, err := dial(t, ctx, srv, info.ID, auth)
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Write(ctx, websocket.MessageBinary, []byte("echo before-$((1+1))\n"))
	readUntil(t, ctx, c, "before-2")
	c.Close(websocket.StatusNormalClosure, "")

	// Leaving the stream detaches; the shell keeps running and keeps its output.
	c, err = dial(t, ctx, srv, info.ID, auth)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	readUntil(t, ctx, c, "before-2")

	_ = c.Write(ctx, websocket.MessageBinary, []byte("exit 5\n"))
	for {
		typ, data, err := c.Read(ctx)
		if err != nil {
			t.Fatalf("stream ended without an exit message: %v", err)
		}
		if typ == websocket.MessageText {
			if string(data) != `{"type":"exit","code":5}` {
				t.Fatalf("exit message = %s", data)
			}
			return
		}
	}
}

// readSSE reads events from an SSE response until one of type until arrives.
func readSSE(t *testing.T, body io.Reader, until string) []events.Event {
	t.Helper()
	var got []events.Event
	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var e events.Event
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &e); err != nil {
			t.Fatal(err)
		}
		got = append(got, e)
		if e.Type == until {
			return got
		}
	}
	t.Fatalf("stream ended before %s; got %d events", until, len(got))
	return nil
}

func streamEvents(t *testing.T, ctx context.Context, srv *httptest.Server, session, lastID string) io.ReadCloser {
	t.Helper()
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/v1/sessions/"+session+"/events", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	if lastID != "" {
		req.Header.Set("Last-Event-ID", lastID)
	}
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("events stream = %d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}
	return res.Body
}

// A run's steps reach the client as they happen, and a client that reconnects with the last
// id it saw gets only what came after.
func TestRunEventsStreamAndResume(t *testing.T) {
	srv := newServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var sess struct{ ID string }
	res := request(t, srv, "POST", "/v1/sessions", testToken, map[string]string{"agent": "claude", "cwd": t.TempDir()})
	_ = json.NewDecoder(res.Body).Decode(&sess)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create session = %d", res.StatusCode)
	}

	body := streamEvents(t, ctx, srv, sess.ID, "")
	if res := request(t, srv, "POST", "/v1/sessions/"+sess.ID+"/runs", testToken,
		map[string]string{"prompt": "run it"}); res.StatusCode != http.StatusAccepted {
		t.Fatalf("start run = %d", res.StatusCode)
	}
	first := readSSE(t, body, events.RunFinished)
	body.Close()

	types := map[string]int{}
	for i, e := range first {
		types[e.Type]++
		if e.Seq != int64(i+1) {
			t.Fatalf("event %d has seq %d", i, e.Seq)
		}
	}
	if first[0].Type != events.RunStarted || types[events.ToolCall] != 1 || types[events.ToolResult] != 2 {
		t.Fatalf("event types = %v", types)
	}
	if first[len(first)-1].Source.NativeSession == "" {
		t.Fatal("events do not carry the CLI session id")
	}

	last := first[len(first)-1].Seq
	if res := request(t, srv, "POST", "/v1/sessions/"+sess.ID+"/runs", testToken,
		map[string]string{"prompt": "again"}); res.StatusCode != http.StatusAccepted {
		t.Fatalf("second run = %d", res.StatusCode)
	}
	body = streamEvents(t, ctx, srv, sess.ID, strconv.FormatInt(last, 10))
	defer body.Close()
	second := readSSE(t, body, events.RunFinished)
	if second[0].Seq != last+1 || second[0].Type != events.RunStarted {
		t.Fatalf("resumed stream starts at seq %d (%s), want %d", second[0].Seq, second[0].Type, last+1)
	}
}

func TestRunRequestsAreValidated(t *testing.T) {
	srv := newServer(t)
	if res := request(t, srv, "POST", "/v1/sessions", testToken,
		map[string]string{"cwd": "/no/such/dir"}); res.StatusCode != http.StatusBadRequest {
		t.Errorf("missing cwd = %d", res.StatusCode)
	}
	if res := request(t, srv, "POST", "/v1/sessions", testToken,
		map[string]string{"agent": "nope", "cwd": t.TempDir()}); res.StatusCode != http.StatusBadRequest {
		t.Errorf("unknown agent = %d", res.StatusCode)
	}
	if res := request(t, srv, "POST", "/v1/sessions/ses_missing/runs", testToken,
		map[string]string{"prompt": "x"}); res.StatusCode != http.StatusNotFound {
		t.Errorf("missing session = %d", res.StatusCode)
	}

}

func openTerminal(t *testing.T, srv *httptest.Server, argv []string) terminal.Info {
	t.Helper()
	var info terminal.Info
	res := request(t, srv, "POST", "/v1/terminals", testToken, terminal.Spec{Argv: argv})
	_ = json.NewDecoder(res.Body).Decode(&info)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("open terminal = %d", res.StatusCode)
	}
	return info
}

func postHook(t *testing.T, srv *httptest.Server, terminal, token string) int {
	t.Helper()
	req, _ := http.NewRequest("POST", srv.URL+"/hooks/claude",
		strings.NewReader(`{"hook_event_name":"SessionStart","session_id":"n1","cwd":"/w"}`))
	req.Header.Set("X-Orla-Terminal", terminal)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	return res.StatusCode
}

// What runs in a terminal gets a token that reports hooks for that terminal and does nothing
// else; the owner token never reaches it.
func TestTerminalEnvironmentHoldsOnlyItsHookToken(t *testing.T) {
	srv := newServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	a := openTerminal(t, srv, []string{"sh"})
	b := openTerminal(t, srv, []string{"sh"})
	c, err := dial(t, ctx, srv, a.ID, http.Header{"Authorization": {"Bearer " + testToken}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	_ = c.Write(ctx, websocket.MessageBinary, []byte("env; echo env-$((1+1))\n"))
	var env strings.Builder
	for !strings.Contains(env.String(), "env-2") {
		_, data, err := c.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		env.Write(data)
	}
	out := env.String()
	if strings.Contains(out, testToken) {
		t.Fatal("the owner token is in the terminal's environment")
	}
	var hookToken string
	for _, line := range strings.Split(out, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "ORLA_HOOK_TOKEN="); ok {
			hookToken = v
		}
	}
	if hookToken == "" || !strings.Contains(out, "ORLA_TERMINAL="+a.ID) {
		t.Fatalf("terminal env lacks the hook settings:\n%s", out)
	}

	if code := postHook(t, srv, a.ID, hookToken); code != http.StatusNoContent {
		t.Fatalf("hook with its own token = %d", code)
	}
	if code := postHook(t, srv, b.ID, hookToken); code != http.StatusUnauthorized {
		t.Fatalf("hook token of one terminal accepted for another = %d", code)
	}
	if code := postHook(t, srv, "term_missing", hookToken); code != http.StatusUnauthorized {
		t.Fatalf("hook for unknown terminal = %d", code)
	}
	// It must not open the API either.
	if res := request(t, srv, "GET", "/v1/sessions", hookToken, nil); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("hook token opened the API: %d", res.StatusCode)
	}
	if code := postHook(t, srv, a.ID, testToken); code != http.StatusUnauthorized {
		t.Fatalf("owner token accepted as a hook token = %d", code)
	}
}

func TestSessionsAreRenamedPinnedArchivedAndRemoved(t *testing.T) {
	srv := newServer(t)
	var x struct {
		ID       string
		Title    string
		Pinned   bool
		Archived *time.Time
	}
	_ = json.NewDecoder(request(t, srv, "POST", "/v1/sessions", testToken,
		map[string]string{"cwd": t.TempDir()}).Body).Decode(&x)

	res := request(t, srv, "PATCH", "/v1/sessions/"+x.ID, testToken,
		map[string]any{"title": "  Login fix ", "pinned": true, "archived": true})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("patch = %d", res.StatusCode)
	}
	_ = json.NewDecoder(res.Body).Decode(&x)
	if x.Title != "Login fix" || !x.Pinned || x.Archived == nil {
		t.Fatalf("after patch: %+v", x)
	}

	// A field left out stays; archived false brings the session back.
	x.Archived = nil
	_ = json.NewDecoder(request(t, srv, "PATCH", "/v1/sessions/"+x.ID, testToken,
		map[string]any{"archived": false}).Body).Decode(&x)
	if x.Title != "Login fix" || !x.Pinned || x.Archived != nil {
		t.Fatalf("after unarchive: %+v", x)
	}

	if res := request(t, srv, "PATCH", "/v1/sessions/"+x.ID, testToken,
		map[string]any{"title": strings.Repeat("a", 121)}); res.StatusCode != http.StatusBadRequest {
		t.Errorf("long title = %d", res.StatusCode)
	}

	if res := request(t, srv, "DELETE", "/v1/sessions/"+x.ID, testToken, nil); res.StatusCode != http.StatusNoContent {
		t.Fatalf("delete = %d", res.StatusCode)
	}
	if res := request(t, srv, "GET", "/v1/sessions/"+x.ID, testToken, nil); res.StatusCode != http.StatusNotFound {
		t.Fatalf("get after delete = %d", res.StatusCode)
	}
}
