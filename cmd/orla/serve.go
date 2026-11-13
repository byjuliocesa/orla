package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/byjuliocesa/orla/internal/auth"
	"github.com/byjuliocesa/orla/internal/automation"
	"github.com/byjuliocesa/orla/internal/engine/cli"
	"github.com/byjuliocesa/orla/internal/events"
	"github.com/byjuliocesa/orla/internal/flow"
	"github.com/byjuliocesa/orla/internal/library"
	"github.com/byjuliocesa/orla/internal/server"
	"github.com/byjuliocesa/orla/internal/session"
	"github.com/byjuliocesa/orla/internal/store"
	"github.com/byjuliocesa/orla/internal/terminal"
	"github.com/byjuliocesa/orla/internal/webui"
)

func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	// Loopback by default: the API opens shells on this machine, so reaching it from outside
	// must be a deliberate choice.
	addr := fs.String("addr", defaultAddr, "address to listen on")
	dataDir := fs.String("data", defaultDataDir(), "directory for the database and token")
	claudePath := fs.String("claude", "claude", "the Claude Code executable")
	uiDev := fs.String("ui-dev", "", "serve the web client from this Vite dev server (development)")
	_ = fs.Parse(args)

	var ui http.Handler
	switch {
	case *uiDev != "":
		target, err := url.Parse(*uiDev)
		if err != nil || target.Host == "" {
			return fmt.Errorf("-ui-dev: %q is not a URL", *uiDev)
		}
		ui = webui.DevProxy(target)
	case webui.Files() != nil:
		ui = webui.Handler(webui.Files())
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if host, _, err := net.SplitHostPort(*addr); err == nil {
		if ip := net.ParseIP(host); host == "" || (ip != nil && !ip.IsLoopback()) {
			log.Warn("listening beyond loopback: anyone who reaches this address and has the token gets a shell",
				"addr", *addr)
		}
	}

	if err := os.MkdirAll(*dataDir, 0o700); err != nil {
		return err
	}
	token, err := auth.LoadOrCreateToken(tokenPath(*dataDir))
	if err != nil {
		return fmt.Errorf("token: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := store.Open(ctx, filepath.Join(*dataDir, "orla.db"))
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer db.Close()

	bus := events.NewBus(db)
	claude := cli.Claude{Path: *claudePath}
	sessions := session.NewService(log, db, bus, map[string]session.Runner{
		"claude": claude,
	})
	sessions.ApprovalsAt("http://" + *addr + "/mcp/runs/")
	if err := sessions.Recover(ctx); err != nil {
		return fmt.Errorf("recovering runs: %w", err)
	}
	terminals := terminal.NewManager()
	if home, err := os.UserHomeDir(); err == nil {
		if err := flow.Install(home); err != nil {
			log.Warn("installing /newflow", "err", err)
		}
	}

	// Automations reach Orla over its own API, with its token.
	// The library: the owner's skills, flows and automations, kept in a repository of their own.
	home, _ := os.UserHomeDir()
	shelf := &library.Manager{Root: filepath.Join(*dataDir, "library"), Home: home, Settings: filepath.Join(*dataDir, "library.json"), Log: log}
	go shelf.Run(ctx)

	automations := &automation.Manager{URL: "http://" + *addr, Token: token, Bus: bus, Logs: filepath.Join(*dataDir, "automations"), Log: log}
	defer automations.Close()

	// Every request derives from this, so ending it on shutdown ends the event streams, which
	// otherwise stay open until the client leaves: Shutdown does not cancel requests.
	requests, endRequests := context.WithCancel(context.Background())
	defer endRequests()
	srv := &http.Server{
		Addr:        *addr,
		BaseContext: func(net.Listener) context.Context { return requests },
		Handler: server.New(server.Deps{
			Log: log, Token: token, Terminals: terminals, Store: db, Bus: bus, Sessions: sessions,
			HookURL: "http://" + *addr + "/hooks/claude", UI: ui, Titler: claude.Title, Contexter: claude.Context, Library: claude.Library,
			Worktrees:   filepath.Join(*dataDir, "worktrees"),
			Uploads:     filepath.Join(*dataDir, "uploads"),
			Automations: automations,
			Shelf:       shelf,
		}),
		ReadHeaderTimeout: 10 * time.Second,
	}

	failed := make(chan error, 1)
	go func() {
		// What was on when Orla stopped runs again, once the API it talks to is up.
		time.Sleep(500 * time.Millisecond)
		server.ResumeAutomations(context.Background(), db, automations, log)
	}()
	go func() {
		log.Info("listening", "addr", *addr, "data", *dataDir, "token_file", tokenPath(*dataDir))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			failed <- err
		}
	}()

	select {
	case err := <-failed:
		return err
	case <-ctx.Done():
	}

	log.Info("shutting down")
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Runs record how they ended before the streams that report them close.
	sessions.Close()
	automations.Close()
	terminals.CloseAll()
	endRequests()
	if err := srv.Shutdown(shutdown); err != nil {
		return srv.Close()
	}
	return nil
}
