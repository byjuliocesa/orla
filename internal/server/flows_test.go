package server

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/byjuliocesa/orla/internal/session"
	"github.com/byjuliocesa/orla/internal/store"
)

func TestFlowsAreListedAndTakenPerSession(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".claude", "skills", "erp")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: fluxo-erp\ndescription: bugfix\nmetadata:\n  orla: flow\n  title: ERP\n---\n## Board\n## PR\nuses: pr-padrao\n"), 0o644)
	pr := filepath.Join(home, ".claude", "skills", "pr")
	os.MkdirAll(pr, 0o755)
	os.WriteFile(filepath.Join(pr, "SKILL.md"), []byte("---\nname: pr-padrao\ndescription: the PR pattern\n---\nTitle: ..."), 0o644)

	srv := newServer(t)
	var list struct {
		Flows []struct {
			Name  string `json:"name"`
			Title string `json:"title"`
			Steps []struct {
				ID   string   `json:"id"`
				Uses []string `json:"uses"`
			} `json:"steps"`
		} `json:"flows"`
	}
	json.NewDecoder(request(t, srv, "GET", "/v1/flows?cwd="+url.QueryEscape(t.TempDir()), testToken, nil).Body).Decode(&list)
	if len(list.Flows) != 1 || list.Flows[0].Title != "ERP" || len(list.Flows[0].Steps) != 2 || list.Flows[0].Steps[1].Uses[0] != "pr-padrao" {
		t.Fatalf("flows = %+v", list)
	}

	var doc struct {
		Body string `json:"body"`
	}
	res := request(t, srv, "GET", "/v1/skill-doc?name=pr-padrao", testToken, nil)
	json.NewDecoder(res.Body).Decode(&doc)
	if res.StatusCode != http.StatusOK || doc.Body == "" {
		t.Fatalf("skill doc = %d %q", res.StatusCode, doc.Body)
	}
	if res := request(t, srv, "GET", "/v1/skill-doc?name=nothing", testToken, nil); res.StatusCode != http.StatusNotFound {
		t.Fatalf("missing skill doc = %d", res.StatusCode)
	}

	var s struct {
		ID    string   `json:"id"`
		Flows []string `json:"flows"`
	}
	json.NewDecoder(request(t, srv, "POST", "/v1/sessions", testToken, map[string]string{"cwd": t.TempDir()}).Body).Decode(&s)
	if s.Flows == nil || len(s.Flows) != 0 {
		t.Fatalf("a new session follows %v", s.Flows)
	}
	// A session that follows no flow leaves every flow out of its conversation.
	if off := session.Off(store.Session{Cwd: home}); !slices.Equal(off, []string{"fluxo-erp"}) {
		t.Fatalf("off without flows = %v", off)
	}
	res = request(t, srv, "PATCH", "/v1/sessions/"+s.ID, testToken, map[string]any{"flows": []string{"fluxo-erp"}})
	json.NewDecoder(res.Body).Decode(&s)
	if !slices.Equal(s.Flows, []string{"fluxo-erp"}) {
		t.Fatalf("after taking = %v", s.Flows)
	}
	if off := session.Off(store.Session{Cwd: home, Flows: s.Flows}); len(off) != 0 {
		t.Fatalf("off with the flow taken = %v", off)
	}
}

func TestFlowButtonsArchiveRestoreAndDelete(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".claude", "skills", "rel")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: rel\nmetadata:\n  orla: flow\n---\n## Build\n\n## Tag\n"), 0o644)
	srv := newServer(t)
	for _, c := range []struct {
		method, path string
		want         int
	}{
		{"POST", "/v1/flows/rel/steps/tag/archive", 200},
		{"POST", "/v1/flows/rel/steps/tag/restore", 200},
		{"POST", "/v1/flows/rel/steps/nothing/archive", 404},
		{"POST", "/v1/flows/rel/archive", 200},
		{"POST", "/v1/flows/rel/restore", 200},
		{"POST", "/v1/flows/rel/duplicate", 200},
		{"DELETE", "/v1/flows/rel/steps/build", 200},
		{"DELETE", "/v1/flows/rel", 200},
		{"DELETE", "/v1/flows/rel", 404},
	} {
		if res := request(t, srv, c.method, c.path, testToken, nil); res.StatusCode != c.want {
			t.Fatalf("%s %s = %d, want %d", c.method, c.path, res.StatusCode, c.want)
		}
	}
}
