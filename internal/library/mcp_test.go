package library

import (
	"context"
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

	err := a.SaveMCP(ctx, MCPInput{Name: "vx-bot", Type: "http", URL: "https://work.example/v1/mcp", Headers: map[string]string{"Authorization": "Bearer s3cret"}})
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
	if !strings.Contains(string(got), `add-json -s user vx-bot {"headers":{"Authorization":"Bearer s3cret"},"type":"http","url":"https://work.example/v1/mcp"}`) {
		t.Fatalf("claude was told:\n%s", got)
	}

	// An empty value keeps the saved one.
	if err := a.SaveMCP(ctx, MCPInput{Name: "vx-bot", Type: "http", URL: "https://work.example/v2/mcp", Headers: map[string]string{"Authorization": ""}}); err != nil {
		t.Fatal(err)
	}
	sec, ok, err := a.openSecrets()
	if err != nil || !ok || sec.Servers["vx-bot"].Headers["Authorization"] != "Bearer s3cret" {
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

	if err := a.RemoveMCP(ctx, "vx-bot"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(a.mcpPath(mcpSecrets)); !os.IsNotExist(err) {
		t.Fatal("secrets.age outlived its last server")
	}
}
