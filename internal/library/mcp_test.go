package library

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeClaude records each `claude mcp` call in a file and answers the script's path.
func fakeClaude(t *testing.T) (func() string, string) {
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	exe := filepath.Join(dir, "claude")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + log + "\n"
	if err := os.WriteFile(exe, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return func() string { return exe }, log
}

func keyed(t *testing.T, root string) *Manager {
	t.Helper()
	data := t.TempDir()
	m := &Manager{Root: root, Home: t.TempDir(), Settings: filepath.Join(data, "library.json"), Keys: filepath.Join(data, "ssh")}
	if _, err := m.GenerateKey(context.Background()); err != nil {
		t.Skip("ssh-keygen:", err)
	}
	return m
}

func TestMCPSecretsTravelEncrypted(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	a := keyed(t, root)
	claude, calls := fakeClaude(t)
	a.Claude = claude

	err := a.SaveMCP(ctx, MCPInput{Name: "tracker", Type: "http", URL: "https://work.example/v1/mcp", Headers: map[string]string{"Authorization": "Bearer s3cret"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{mcpServers, mcpSecrets, mcpMachines} {
		b, err := os.ReadFile(a.mcpPath(f))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "s3cret") {
			t.Fatalf("%s holds the token in the clear", f)
		}
	}
	got, _ := os.ReadFile(calls)
	if !strings.Contains(string(got), `add-json -s user tracker {"headers":{"Authorization":"Bearer s3cret"},"type":"http","url":"https://work.example/v1/mcp"}`) {
		t.Fatalf("claude was told:\n%s", got)
	}

	// An empty value keeps the saved one.
	if err := a.SaveMCP(ctx, MCPInput{Name: "tracker", Type: "http", URL: "https://work.example/v2/mcp", Headers: map[string]string{"Authorization": ""}}); err != nil {
		t.Fatal(err)
	}
	sec, ok, err := a.openSecrets()
	if err != nil || !ok || sec.Servers["tracker"].Headers["Authorization"] != "Bearer s3cret" {
		t.Fatalf("kept value: %+v %v %v", sec, ok, err)
	}

	// A second machine with its own key cannot read the values until the first opens them to it.
	b := keyed(t, root)
	if err := b.enroll(); err != nil {
		t.Fatal(err)
	}
	if list, _ := b.MCP(ctx); !list.Locked || list.Servers[0].State != "locked" {
		t.Fatalf("second machine before: %+v", list)
	}
	if err := a.ApplyMCP(ctx); err != nil {
		t.Fatal(err)
	}
	if list, _ := b.MCP(ctx); list.Locked {
		t.Fatalf("second machine after: %+v", list)
	}

	if err := a.RemoveMCP(ctx, "tracker"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(a.mcpPath(mcpSecrets)); !os.IsNotExist(err) {
		t.Fatal("secrets.age outlived its last server")
	}
}

func TestEnrollListsEachMachineOnce(t *testing.T) {
	m := keyed(t, t.TempDir())
	for range 3 {
		if err := m.enroll(); err != nil {
			t.Fatal(err)
		}
	}
	// A file already holding the key twice is set straight.
	b, _ := os.ReadFile(m.mcpPath(mcpMachines))
	_ = os.WriteFile(m.mcpPath(mcpMachines), append(b, b...), 0o644)
	if err := m.enroll(); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(m.mcpPath(mcpMachines))
	if n := strings.Count(string(b), "\n"); n != 1 {
		t.Fatalf("machines has %d lines:\n%s", n, b)
	}
}

func TestProbeCountsTools(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer k" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var m struct {
			ID     any    `json:"id"`
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&m)
		w.Header().Set("Mcp-Session-Id", "s1")
		switch m.Method {
		case "initialize":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n\n")
		case "tools/list":
			if r.Header.Get("Mcp-Session-Id") != "s1" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"a"},{"name":"b"}]}}`)
		default:
			w.WriteHeader(http.StatusAccepted)
		}
	}))
	defer srv.Close()
	n, err := probe(context.Background(), srv.URL, map[string]string{"Authorization": "Bearer k"})
	if err != nil || n != 2 {
		t.Fatalf("probe = %d, %v", n, err)
	}
	if _, err := probe(context.Background(), srv.URL, nil); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("without the key: %v", err)
	}
}
