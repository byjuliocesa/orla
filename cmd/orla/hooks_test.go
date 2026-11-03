package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/byjuliocesa/orla/internal/engine/cli"
)

const ownSettings = `{
  "permissions": {"defaultMode": "manual"},
  "hooks": {
    "Stop": [{"hooks": [{"type": "command", "command": "notify-send done"}]}]
  },
  "model": "sonnet"
}
`

// The settings file is the owner's: install and uninstall must touch nothing but Orla's hooks.
func TestHooksInstallKeepsTheOwnersSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(ownSettings), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := `"/opt/orla/bin/orla" hook`

	for range 2 { // twice: install is idempotent
		if err := editSettings(path, cmd, true); err != nil {
			t.Fatal(err)
		}
	}
	var s map[string]any
	b, _ := os.ReadFile(path)
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	if s["model"] != "sonnet" || s["permissions"].(map[string]any)["defaultMode"] != "manual" {
		t.Fatalf("other settings changed: %s", b)
	}
	if n, want := strings.Count(string(b), cmd[1:len(cmd)-6]), len(cli.HookEvents); n != want {
		t.Fatalf("orla hook appears %d times, want once per event (%d):\n%s", n, want, b)
	}
	if !strings.Contains(string(b), "notify-send done") {
		t.Fatal("the owner's own Stop hook was dropped")
	}
	if backup, err := os.ReadFile(path + ".orla-backup"); err != nil || len(backup) == 0 {
		t.Fatalf("no backup: %v", err)
	}

	// Uninstall from a binary that has moved still finds them.
	if err := editSettings(path, `"/elsewhere/orla" hook`, false); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(path)
	var back, orig map[string]any
	_ = json.Unmarshal(b, &back)
	_ = json.Unmarshal([]byte(ownSettings), &orig)
	gotJSON, _ := json.Marshal(back)
	wantJSON, _ := json.Marshal(orig)
	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("after uninstall\n got: %s\nwant: %s", gotJSON, wantJSON)
	}
}

func TestHooksRefuseABrokenSettingsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	_ = os.WriteFile(path, []byte("{ not json"), 0o600)
	if err := editSettings(path, `"/x/orla" hook`, true); err == nil {
		t.Fatal("edited a file it could not parse")
	}
	if b, _ := os.ReadFile(path); string(b) != "{ not json" {
		t.Fatal("broken file was overwritten")
	}
}

func TestOnlyOrlaHooksAreRecognized(t *testing.T) {
	for cmd, want := range map[string]bool{
		`"/usr/local/bin/orla" hook`: true,
		`/home/me/orla hook`:         true,
		`notify-send hook`:           false,
		`"/usr/bin/gorla" hook`:      false,
		`"/usr/bin/orla" hooks`:      false,
	} {
		if isOrlaHook(cmd) != want {
			t.Errorf("isOrlaHook(%s) = %v", cmd, !want)
		}
	}
}

// Hook stdout reaches Claude Code: UserPromptSubmit adds it to the prompt, PermissionRequest
// reads it as the answer. Only a permission answer may be printed.
func TestHookPrintsOnlyPermissionAnswers(t *testing.T) {
	answer := `{"hookSpecificOutput":{"hookEventName":"PermissionRequest","decision":{"behavior":"allow"}}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer hook-token" || r.Header.Get("X-Orla-Terminal") != "term_1" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		b, _ := io.ReadAll(r.Body)
		switch {
		case strings.Contains(string(b), "withdrawn"):
			w.WriteHeader(http.StatusNoContent)
		case strings.Contains(string(b), "broken"):
			_, _ = io.WriteString(w, "{not json")
		default:
			_, _ = io.WriteString(w, answer) // whatever the event: the hook decides what to print
		}
	}))
	defer srv.Close()
	inTerminal := map[string]string{"ORLA_TERMINAL": "term_1", "ORLA_HOOK_URL": srv.URL, "ORLA_HOOK_TOKEN": "hook-token"}

	cases := []struct {
		name, payload string
		env           map[string]string
		want          string
	}{
		{"permission answered", `{"hook_event_name":"PermissionRequest"}`, inTerminal, answer},
		{"permission left to the terminal", `{"hook_event_name":"PermissionRequest","x":"withdrawn"}`, inTerminal, ""},
		{"broken answer", `{"hook_event_name":"PermissionRequest","x":"broken"}`, inTerminal, ""},
		{"prompt", `{"hook_event_name":"UserPromptSubmit","prompt":"hi"}`, inTerminal, ""},
		{"outside a terminal", `{"hook_event_name":"PermissionRequest"}`, map[string]string{}, ""},
		{"server gone", `{"hook_event_name":"PermissionRequest"}`, map[string]string{
			"ORLA_TERMINAL": "term_1", "ORLA_HOOK_URL": "http://127.0.0.1:1/hooks/claude", "ORLA_HOOK_TOKEN": "hook-token",
		}, ""},
	}
	for _, c := range cases {
		var out bytes.Buffer
		sendHook(strings.NewReader(c.payload), &out, func(k string) string { return c.env[k] })
		if out.String() != c.want {
			t.Errorf("%s: printed %q, want %q", c.name, out.String(), c.want)
		}
	}
}
