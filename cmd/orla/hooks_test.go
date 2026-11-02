package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	if n := strings.Count(string(b), cmd[1:len(cmd)-6]); n != 8 {
		t.Fatalf("orla hook appears %d times, want once per event (8):\n%s", n, b)
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
