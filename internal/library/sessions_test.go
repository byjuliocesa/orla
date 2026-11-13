package library

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSavedSessionOpensInAnotherFolder(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	src := filepath.Join(t.TempDir(), "abc.jsonl")
	lines := `{"type":"user","cwd":"/home/a/proj","message":"hi"}` + "\n" + `{"type":"summary"}` + "\n"
	if err := os.WriteFile(src, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	s := SavedSession{Key: "abc", Native: "abc", Title: "Fix login", Cwd: "/home/a/proj", Saved: time.Now()}
	if err := SaveSession(root, s, src); err != nil {
		t.Fatal(err)
	}
	if got := SavedSessions(root); len(got) != 1 || got[0].Title != "Fix login" {
		t.Fatalf("saved: %+v", got)
	}
	cwd := t.TempDir()
	path, err := Place(root, home, "abc", cwd)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".claude", "projects", ProjectDir(cwd), "abc.jsonl"); path != want {
		t.Fatalf("placed at %s, want %s", path, want)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), `"cwd":"`+cwd+`"`) || strings.Contains(string(b), "/home/a/proj") || !strings.Contains(string(b), `{"type":"summary"}`) {
		t.Fatalf("transcript: %s", b)
	}
	if _, err := Place(root, home, "abc", filepath.Join(cwd, "missing")); err == nil {
		t.Fatal("a missing folder was accepted")
	}
	if err := SaveSession(root, SavedSession{Key: "../x", Native: "abc"}, src); err == nil {
		t.Fatal("a key with a path was accepted")
	}
}

func TestProjectDir(t *testing.T) {
	if got := ProjectDir("/home/julio/Documentos/Github/orla"); got != "-home-julio-Documentos-Github-orla" {
		t.Fatal(got)
	}
}
