package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/Julio0Cesar/orla/internal/terminal"
)

const testToken = "orla_test-token-not-a-secret"

func newServer(t *testing.T) *httptest.Server {
	t.Helper()
	terminals := terminal.NewManager()
	srv := httptest.NewServer(New(slog.New(slog.NewTextHandler(io.Discard, nil)), testToken, terminals))
	t.Cleanup(func() {
		terminals.CloseAll()
		srv.Close()
	})
	return srv
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
