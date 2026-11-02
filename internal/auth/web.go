package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"time"
)

// CookieName is the cookie that holds a browser login.
const CookieName = "orla_login"

// LoginTTL is how long a browser login lasts before the token is asked for again.
const LoginTTL = 30 * 24 * time.Hour

// Logins keeps browser logins by the SHA-256 of their secret.
type Logins interface {
	CreateWebLogin(ctx context.Context, hash, agent string, at time.Time) error
	WebLoginCreated(ctx context.Context, hash string) (time.Time, error)
	DeleteWebLogin(ctx context.Context, hash string) error
	DeleteWebLoginsBefore(ctx context.Context, cutoff time.Time) error
}

// Gate decides who may use the API: a client holding the owner token, or a browser that logged
// in with it. Browsers need the cookie because neither EventSource nor WebSocket can send an
// Authorization header.
type Gate struct {
	Token  string
	Logins Logins
}

// Require lets a request through with "Authorization: Bearer <token>" or a login cookie.
//
// The token is never read from the query string, where it would end up in logs and browser
// history. A cookie request that changes something must come from Orla's own pages: the cookie
// is SameSite=Strict, and the origin check also covers browsers and proxies that get that
// wrong. WebSocket upgrades are GETs; the WebSocket library refuses cross-origin ones itself.
func (g Gate) Require(next http.Handler) http.Handler {
	want := []byte("Bearer " + g.Token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h := r.Header.Get("Authorization"); h != "" {
			// Explicit credentials decide alone: a wrong token is not rescued by a cookie.
			if subtle.ConstantTimeCompare([]byte(h), want) == 1 {
				next.ServeHTTP(w, r)
				return
			}
		} else if g.loggedIn(r) && (safeMethod(r.Method) || SameOrigin(r)) {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("WWW-Authenticate", `Bearer realm="orla"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
}

func (g Gate) loggedIn(r *http.Request) bool {
	c, err := r.Cookie(CookieName)
	if err != nil || c.Value == "" {
		return false
	}
	created, err := g.Logins.WebLoginCreated(r.Context(), hashSecret(c.Value))
	return err == nil && time.Since(created) < LoginTTL
}

// Login exchanges the owner token for a login cookie: POST {"token": "..."}.
func (g Gate) Login(w http.ResponseWriter, r *http.Request) {
	if !SameOrigin(r) {
		http.Error(w, "cross-origin login refused", http.StatusForbidden)
		return
	}
	var in struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&in); err != nil || !Equal(in.Token, g.Token) {
		http.Error(w, "wrong token", http.StatusUnauthorized)
		return
	}
	now := time.Now()
	_ = g.Logins.DeleteWebLoginsBefore(r.Context(), now.Add(-LoginTTL))
	secret := NewSecret()
	if err := g.Logins.CreateWebLogin(r.Context(), hashSecret(secret), r.UserAgent(), now); err != nil {
		http.Error(w, "could not record the login", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, g.cookie(r, secret, int(LoginTTL/time.Second)))
	w.WriteHeader(http.StatusNoContent)
}

// Logout ends the browser's login, so its cookie stops working even where a copy survives.
func (g Gate) Logout(w http.ResponseWriter, r *http.Request) {
	if !SameOrigin(r) {
		http.Error(w, "cross-origin logout refused", http.StatusForbidden)
		return
	}
	if c, err := r.Cookie(CookieName); err == nil && c.Value != "" {
		_ = g.Logins.DeleteWebLogin(r.Context(), hashSecret(c.Value))
	}
	http.SetCookie(w, g.cookie(r, "", -1))
	w.WriteHeader(http.StatusNoContent)
}

func (g Gate) cookie(r *http.Request, value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name: CookieName, Value: value, Path: "/", MaxAge: maxAge,
		HttpOnly: true, SameSite: http.SameSiteStrictMode,
		// Behind a TLS proxy the request reaches Orla in plain HTTP; the proxy says so.
		Secure: r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
	}
}

// SameOrigin reports whether a browser request comes from a page served by this host. A proxy
// in front of Orla must pass the original Host header for this to hold.
func SameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return r.Header.Get("Sec-Fetch-Site") == "same-origin"
	}
	u, err := url.Parse(origin)
	return err == nil && u.Host != "" && u.Host == r.Host
}

func safeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}

func hashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}
