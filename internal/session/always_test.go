package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAlwaysCarriesTheSkillWhole(t *testing.T) {
	home, cwd := t.TempDir(), t.TempDir()
	dir := filepath.Join(home, ".claude", "skills", "work")
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: work\n---\n\nUse the bot.\n"), 0o644)
	got := Always(home, cwd, []string{"work", "plugin:thing"})
	if !strings.Contains(got, "Use the bot.") || !strings.Contains(got, dir) {
		t.Fatalf("the skill is not carried: %q", got)
	}
	if !strings.Contains(got, `"plugin:thing"`) {
		t.Fatalf("a skill without a file is not named: %q", got)
	}
	if Always(home, cwd, nil) != "" {
		t.Fatal("no skills should add nothing")
	}
}
