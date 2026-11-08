package server

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFilesSearchAndBrowse(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	proj := filepath.Join(home, "proj")
	for _, f := range []string{"web/components/SessionView.tsx", "web/components/Sidebar.tsx", "internal/server/files.go", "node_modules/x/index.js", "README.md"} {
		os.MkdirAll(filepath.Join(proj, filepath.Dir(f)), 0o755)
		os.WriteFile(filepath.Join(proj, f), nil, 0o644)
	}
	os.MkdirAll(filepath.Join(proj, "internal/session"), 0o755)
	os.WriteFile(filepath.Join(proj, "internal/session/service.go"), nil, 0o644)
	hits := search(context.Background(), proj, "sessview")
	if len(hits) == 0 || hits[0].Path != "web/components/SessionView.tsx" {
		t.Fatalf("search = %+v", hits)
	}
	for _, h := range search(context.Background(), proj, "") {
		if filepath.Base(filepath.Dir(h.Path)) == "x" {
			t.Fatalf("walked into node_modules: %+v", h)
		}
	}
	b := browse("~/pr", home)
	if len(b) != 1 || b[0].Path != "~/proj/" || !b[0].Dir {
		t.Fatalf("browse = %+v", b)
	}
	b = browse("~/proj/", home)
	if len(b) < 3 || !b[0].Dir || b[len(b)-1].Path != "~/proj/README.md" {
		t.Fatalf("browse dir = %+v", b)
	}
}
