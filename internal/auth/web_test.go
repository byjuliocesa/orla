package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Julio0Cesar/orla/internal/store"
)

const owner = "orla_test-owner-token"

type memLogins struct {
	mu   sync.Mutex
	made map[string]time.Time
}

func (m *memLogins) CreateWebLogin(_ context.Context, hash, _ string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.made[hash] = at
	return nil
}

func (m *memLogins) WebLoginCreated(_ context.Context, hash string) (time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	at, ok := m.made[hash]
	if !ok {
		return time.Time{}, store.ErrNotFound
	}
	return at, nil
}

func (m *memLogins) DeleteWebLogin(_ context.Context, hash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.made, hash)
	return nil
}

func (m *memLogins) DeleteWebLoginsBefore(_ context.Context, cutoff time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for h, at := range m.made {
		if at.Before(cutoff) {
			delete(m.made, h)
		}
	}
	return nil
}

func gateServer(t *testing.T) (*httptest.Server, *memLogins) {
	t.Helper()
	logins := &memLogins{made: map[string]time.Time{}}
	g := Gate{Token: owner, Logins: logins}
	mux := http.NewServeMux()
	mux.Handle("/v1/", g.Require(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot) // reached the API
	})))
	mux.HandleFunc("POST /auth/login", g.Login)
	mux.HandleFunc("POST /auth/logout", g.Logout)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, logins
}

func send(t *testing.T, srv *httptest.Server, method, path, body string, header http.Header) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
	for k, v := range header {
		req.Header[k] = v
	}
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	return res
}

func login(t *testing.T, srv *httptest.Server) *http.Cookie {
	t.Helper()
	res := send(t, srv, "POST", "/auth/login", `{"token":"`+owner+`"}`, http.Header{"Origin": {srv.URL}})
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("login = %d", res.StatusCode)
	}
	for _, c := range res.Cookies() {
		if c.Name == CookieName {
			return c
		}
	}
	t.Fatal("login set no cookie")
	return nil
}

func TestBrowserLogin(t *testing.T) {
	srv, _ := gateServer(t)
	origin := http.Header{"Origin": {srv.URL}}

	if res := send(t, srv, "POST", "/auth/login", `{"token":"orla_wrong"}`, origin); res.StatusCode != http.StatusUnauthorized || len(res.Cookies()) != 0 {
		t.Fatalf("wrong token = %d, cookies %v", res.StatusCode, res.Cookies())
	}
	if res := send(t, srv, "POST", "/auth/login", `{"token":"`+owner+`"}`, http.Header{"Origin": {"https://evil.example"}}); res.StatusCode != http.StatusForbidden {
		t.Fatalf("login from another site = %d", res.StatusCode)
	}

	c := login(t, srv)
	if !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Value == owner || strings.Contains(c.Value, owner) {
		t.Fatalf("cookie = %+v", c)
	}
	withCookie := func(h http.Header) http.Header {
		if h == nil {
			h = http.Header{}
		}
		h.Set("Cookie", c.Name+"="+c.Value)
		return h
	}
	if res := send(t, srv, "GET", "/v1/sessions", "", withCookie(nil)); res.StatusCode != http.StatusTeapot {
		t.Fatalf("GET with cookie = %d", res.StatusCode)
	}
	if res := send(t, srv, "POST", "/v1/sessions", "{}", withCookie(http.Header{"Origin": {srv.URL}})); res.StatusCode != http.StatusTeapot {
		t.Fatalf("same-origin POST with cookie = %d", res.StatusCode)
	}
	// What a forged form or script on another site would send.
	if res := send(t, srv, "POST", "/v1/sessions", "{}", withCookie(http.Header{"Origin": {"https://evil.example"}})); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("cross-origin POST with cookie = %d", res.StatusCode)
	}
	if res := send(t, srv, "POST", "/v1/sessions", "{}", withCookie(nil)); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("POST with cookie and no origin = %d", res.StatusCode)
	}
	// A wrong token is not rescued by a valid cookie.
	if res := send(t, srv, "GET", "/v1/sessions", "", withCookie(http.Header{"Authorization": {"Bearer orla_wrong"}})); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong bearer with cookie = %d", res.StatusCode)
	}
	if res := send(t, srv, "GET", "/v1/sessions", "", http.Header{"Authorization": {"Bearer " + owner}}); res.StatusCode != http.StatusTeapot {
		t.Fatalf("bearer = %d", res.StatusCode)
	}

	// After logout the same cookie, replayed, opens nothing.
	if res := send(t, srv, "POST", "/auth/logout", "", withCookie(http.Header{"Origin": {srv.URL}})); res.StatusCode != http.StatusNoContent {
		t.Fatalf("logout = %d", res.StatusCode)
	}
	if res := send(t, srv, "GET", "/v1/sessions", "", withCookie(nil)); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("cookie after logout = %d", res.StatusCode)
	}
}

func TestOldLoginExpires(t *testing.T) {
	srv, logins := gateServer(t)
	c := login(t, srv)
	logins.mu.Lock()
	for h := range logins.made {
		logins.made[h] = time.Now().Add(-LoginTTL - time.Hour)
	}
	logins.mu.Unlock()
	if res := send(t, srv, "GET", "/v1/sessions", "", http.Header{"Cookie": {c.Name + "=" + c.Value}}); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expired login = %d", res.StatusCode)
	}
}
