// Package webui serves the web client: the static export of the Next.js project in web/,
// embedded in the binary when it is built with the webui tag (see the Makefile).
package webui

import (
	"io/fs"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"strings"
)

// Handler serves the client's files. Unknown paths get its 404 page.
func Handler(files fs.FS) http.Handler {
	fileServer := http.FileServerFS(files)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		securityHeaders(w)
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" {
			name = "."
		}
		if !exists(files, name) {
			notFound(w, files)
			return
		}
		if strings.HasPrefix(name, "_next/static/") {
			// Named by content hash: a new build gets new names.
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		fileServer.ServeHTTP(w, r)
	})
}

// DevProxy sends every request outside the API to a Next.js dev server, so the client is
// developed same-origin with the API, cookie and streams included. Development only: the dev
// server runs code it reloads, so it gets no security headers.
func DevProxy(target *url.URL) http.Handler {
	return httputil.NewSingleHostReverseProxy(target)
}

// securityHeaders: the export inlines its bootstrap scripts, hence 'unsafe-inline'; everything
// else, fonts included, comes from Orla itself. The one outside call is the login's read of the
// repository's star count from GitHub's public API.
func securityHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; "+
		"style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; "+
		"connect-src 'self' https://api.github.com; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Frame-Options", "DENY")
}

func exists(files fs.FS, name string) bool {
	info, err := fs.Stat(files, name)
	if err != nil {
		return false
	}
	if info.IsDir() {
		_, err = fs.Stat(files, path.Join(name, "index.html"))
		return err == nil
	}
	return true
}

func notFound(w http.ResponseWriter, files fs.FS) {
	page, err := fs.ReadFile(files, "404.html")
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write(page)
}
