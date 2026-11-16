package session

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStepShowsPicturesTheRunWrote(t *testing.T) {
	cwd, media := t.TempDir(), t.TempDir()
	s := &Service{log: slog.New(slog.NewTextHandler(io.Discard, nil)), media: media, scans: map[string]*scan{}}
	old := filepath.Join(cwd, "old.png")
	_ = os.WriteFile(old, []byte("x"), 0o644)
	_ = os.Chtimes(old, time.Now().Add(-time.Hour), time.Now().Add(-time.Hour))
	s.scans["run"] = &scan{since: time.Now().Add(-time.Minute), seen: map[string]bool{}}

	shots := filepath.Join(cwd, "shots")
	_ = os.MkdirAll(shots, 0o755)
	_ = os.WriteFile(filepath.Join(shots, "home.png"), []byte("png"), 0o644)
	_ = os.MkdirAll(filepath.Join(cwd, "node_modules", "x"), 0o755)
	_ = os.WriteFile(filepath.Join(cwd, "node_modules", "x", "icon.png"), []byte("png"), 0o644)

	out := s.withImages("run", cwd, map[string]any{"call_id": "c1", "ok": true})
	imgs, _ := out.(map[string]any)["images"].([]map[string]any)
	if len(imgs) != 1 || imgs[0]["name"] != "home.png" {
		t.Fatalf("images = %v", imgs)
	}
	if _, err := os.Stat(filepath.Join(media, imgs[0]["id"].(string), "home.png")); err != nil {
		t.Fatal("the picture was not kept:", err)
	}
	// Already shown: the next step does not show it again.
	out = s.withImages("run", cwd, map[string]any{"call_id": "c2", "ok": true})
	if _, has := out.(map[string]any)["images"]; has {
		t.Fatalf("shown twice: %v", out)
	}
}
