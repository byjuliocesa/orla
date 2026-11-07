package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestHandlerServesTheExport(t *testing.T) {
	files := fstest.MapFS{
		"index.html":           {Data: []byte("<p>home")},
		"login/index.html":     {Data: []byte("<p>login")},
		"404.html":             {Data: []byte("<p>lost")},
		"assets/index-1a2b.js": {Data: []byte("js")},
	}
	h := Handler(files)
	get := func(method, path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(method, path, nil))
		return w
	}

	if w := get("GET", "/"); w.Code != 200 || !strings.Contains(w.Body.String(), "home") ||
		!strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Fatalf("/ = %d %q %v", w.Code, w.Body, w.Header())
	}
	if w := get("GET", "/login/"); w.Code != 200 || !strings.Contains(w.Body.String(), "login") {
		t.Fatalf("/login/ = %d %q", w.Code, w.Body)
	}
	if w := get("GET", "/assets/index-1a2b.js"); !strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("hashed asset cache = %q", w.Header().Get("Cache-Control"))
	}
	if w := get("GET", "/nope"); w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "lost") {
		t.Fatalf("/nope = %d %q", w.Code, w.Body)
	}
	if w := get("GET", "/../../etc/passwd"); w.Code != http.StatusNotFound {
		t.Fatalf("path escape = %d", w.Code)
	}
	if w := get("POST", "/"); w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST / = %d", w.Code)
	}
}
