// Package server is the only way into Orla from outside: the HTTP API and its WebSocket
// streams. Handlers translate HTTP to calls on the core packages and hold no state of their own.
package server

import (
	"context"
	"encoding/json"
	"github.com/byjuliocesa/orla/internal/automation"
	"github.com/byjuliocesa/orla/internal/library"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/byjuliocesa/orla/internal/auth"
	"github.com/byjuliocesa/orla/internal/engine/cli"
	"github.com/byjuliocesa/orla/internal/events"
	"github.com/byjuliocesa/orla/internal/session"
	"github.com/byjuliocesa/orla/internal/store"
	"github.com/byjuliocesa/orla/internal/terminal"
)

// Deps are the core services the API exposes.
type Deps struct {
	Log       *slog.Logger
	Token     string
	Terminals *terminal.Manager
	Store     *store.Store
	Bus       *events.Bus
	Sessions  *session.Service
	// HookURL is where CLIs in Orla terminals send their hook events.
	HookURL string
	// Worktrees is the folder that holds the worktrees Orla makes.
	Worktrees string
	// Uploads is the folder that holds the files the owner attaches to prompts.
	Uploads string
	// Contexter reports what fills a conversation's context window; nil leaves it unknown.
	Contexter func(ctx context.Context, native, cwd, settings string) (cli.ContextUsage, error)
	// Library lists the skills Claude Code would load in a folder, with their cost.
	Library func(ctx context.Context, cwd string) ([]cli.ContextPart, error)
	// Automations runs the owner's automations; nil leaves them off.
	Automations *automation.Manager
	// Shelf keeps the owner's library in a git repository; nil leaves the library unsynced.
	Shelf *library.Manager
	// Titler names a session from its first prompt; nil leaves sessions unnamed.
	Titler func(ctx context.Context, prompt string) (string, error)
	// UI serves the web client at every path outside the API. Nil serves a note saying the
	// binary was built without it.
	UI http.Handler
}

type Server struct {
	automations *automation.Manager
	shelf       *library.Manager
	log         *slog.Logger
	terminals   *terminal.Manager
	store       *store.Store
	bus         *events.Bus
	sessions    *session.Service
	hookURL     string
	worktrees   string
	uploads     string
	titler      func(ctx context.Context, prompt string) (string, error)
	contexter   func(ctx context.Context, native, cwd, settings string) (cli.ContextUsage, error)
	library     func(ctx context.Context, cwd string) ([]cli.ContextPart, error)
	// libraries caches the skill library per folder for a few minutes.
	libMu     sync.Mutex
	libraries map[string]cachedLibrary
	// contexts caches /context per session, good until the session's next event.
	ctxMu    sync.Mutex
	contexts map[string]cachedContext
}

type cachedLibrary struct {
	at     time.Time
	skills []cli.ContextPart
	// refreshing is set while a fresh copy is on its way, so only one is asked for at a time.
	refreshing bool
}

type cachedContext struct {
	seq   int64
	usage cli.ContextUsage
}

// New returns the root handler with every route registered.
func New(d Deps) http.Handler {
	s := &Server{
		log: d.Log, terminals: d.Terminals, store: d.Store, bus: d.Bus, sessions: d.Sessions,
		hookURL: d.HookURL, worktrees: d.Worktrees, uploads: d.Uploads, titler: d.Titler, contexter: d.Contexter, contexts: map[string]cachedContext{}, library: d.Library, libraries: map[string]cachedLibrary{}, automations: d.Automations, shelf: d.Shelf,
	}

	api := http.NewServeMux()
	api.HandleFunc("GET /v1/terminals", s.listTerminals)
	api.HandleFunc("POST /v1/terminals", s.openTerminal)
	api.HandleFunc("DELETE /v1/terminals/{id}", s.closeTerminal)
	api.HandleFunc("GET /v1/terminals/{id}/stream", s.streamTerminal)

	api.HandleFunc("GET /v1/sessions", s.listSessions)
	api.HandleFunc("POST /v1/sessions", s.createSession)
	api.HandleFunc("POST /v1/sessions/import", s.importSession)
	api.HandleFunc("GET /v1/sessions/{id}", s.getSession)
	api.HandleFunc("PATCH /v1/sessions/{id}", s.updateSession)
	api.HandleFunc("DELETE /v1/sessions/{id}", s.deleteSession)
	api.HandleFunc("POST /v1/sessions/{id}/fork", s.forkSession)
	api.HandleFunc("POST /v1/sessions/{id}/save", s.saveSession)
	api.HandleFunc("GET /v1/limits", s.limits)
	api.HandleFunc("GET /v1/skills", s.skills)
	api.HandleFunc("GET /v1/automations", s.listAutomations)
	api.HandleFunc("POST /v1/automations/{name}/{action}", s.automationAction)
	api.HandleFunc("GET /v1/automations/{name}/logs", s.automationLogs)
	api.HandleFunc("DELETE /v1/automations/{name}", s.deleteAutomation)
	api.HandleFunc("GET /v1/flows", s.flows)
	api.HandleFunc("GET /v1/flows/archived", s.archivedFlows)
	api.HandleFunc("POST /v1/flows/{name}/{action}", s.flowAction)
	api.HandleFunc("DELETE /v1/flows/{name}", s.flowAction)
	api.HandleFunc("POST /v1/flows/{name}/steps/{step}/{action}", s.flowAction)
	api.HandleFunc("DELETE /v1/flows/{name}/steps/{step}", s.flowAction)
	api.HandleFunc("GET /v1/skill-doc", s.skillDoc)
	api.HandleFunc("GET /v1/library", s.libraryStatus)
	api.HandleFunc("GET /v1/library/key", s.libraryKey)
	api.HandleFunc("PATCH /v1/library", s.librarySettings)
	api.HandleFunc("POST /v1/library/{action}", s.libraryAction)
	api.HandleFunc("GET /v1/prefs/{key}", s.getPref)
	api.HandleFunc("PUT /v1/prefs/{key}", s.putPref)
	api.HandleFunc("GET /v1/files", s.files)
	api.HandleFunc("POST /v1/sessions/{id}/runs", s.startRun)
	api.HandleFunc("POST /v1/sessions/{id}/cancel", s.cancelRun)
	api.HandleFunc("GET /v1/sessions/{id}/events", s.streamEvents)
	api.HandleFunc("GET /v1/sessions/{id}/history", s.history)
	api.HandleFunc("GET /v1/sessions/{id}/context", s.sessionContext)
	api.HandleFunc("POST /v1/sessions/{id}/approvals/{approval}", s.resolveApproval)
	api.HandleFunc("GET /v1/events", s.streamAllEvents)
	api.HandleFunc("GET /v1/approvals", s.listApprovals)

	api.HandleFunc("GET /v1/git", s.gitInfo)
	api.HandleFunc("POST /v1/worktrees", s.createWorktree)
	api.HandleFunc("POST /v1/uploads", s.upload)
	api.HandleFunc("GET /v1/uploads/{id}", s.getUpload)
	api.HandleFunc("GET /v1/sessions/{id}/changes", s.sessionChanges)
	api.HandleFunc("GET /v1/sessions/{id}/diff", s.sessionDiff)
	api.HandleFunc("POST /v1/sessions/{id}/discard", s.discardChange)
	api.HandleFunc("POST /v1/sessions/{id}/commit", s.commitChanges)
	api.HandleFunc("GET /v1/sessions/{id}/merge", s.previewMerge)
	api.HandleFunc("POST /v1/sessions/{id}/merge", s.mergeWorktree)
	api.HandleFunc("DELETE /v1/sessions/{id}/worktree", s.removeWorktree)

	mux := http.NewServeMux()
	// Unauthenticated on purpose: it says only that the process is up, for health checks and
	// process supervisors.
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	gate := auth.Gate{Token: d.Token, Logins: d.Store}
	mux.Handle("/v1/", gate.Require(api))
	mux.HandleFunc("POST /auth/login", gate.Login)
	mux.HandleFunc("POST /auth/logout", gate.Logout)
	mux.HandleFunc("POST /hooks/claude", s.claudeHook)
	mux.HandleFunc("/mcp/runs/{run}", s.runMCP)
	ui := d.UI
	if ui == nil {
		ui = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "This Orla was built without its web client. Build it with `make`.", http.StatusNotFound)
		})
	}
	// "/" and not "GET /": a method on the catch-all would conflict with "/v1/".
	mux.Handle("/", ui)
	return mux
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
