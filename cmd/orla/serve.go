package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Julio0Cesar/orla/internal/auth"
	"github.com/Julio0Cesar/orla/internal/engine/cli"
	"github.com/Julio0Cesar/orla/internal/events"
	"github.com/Julio0Cesar/orla/internal/server"
	"github.com/Julio0Cesar/orla/internal/session"
	"github.com/Julio0Cesar/orla/internal/store"
	"github.com/Julio0Cesar/orla/internal/terminal"
)

func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	// Loopback by default: the API opens shells on this machine, so reaching it from outside
	// must be a deliberate choice.
	addr := fs.String("addr", defaultAddr, "address to listen on")
	dataDir := fs.String("data", defaultDataDir(), "directory for the database and token")
	claudePath := fs.String("claude", "claude", "the Claude Code executable")
	_ = fs.Parse(args)

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
	sessions := session.NewService(log, db, bus, map[string]session.Runner{
		"claude": cli.Claude{Path: *claudePath},
	})
	sessions.ApprovalsAt("http://" + *addr + "/mcp/runs/")
	if err := sessions.Recover(ctx); err != nil {
		return fmt.Errorf("recovering runs: %w", err)
	}
	terminals := terminal.NewManager()

	// Every request derives from this, so ending it on shutdown ends the event streams, which
	// otherwise stay open until the client leaves: Shutdown does not cancel requests.
	requests, endRequests := context.WithCancel(context.Background())
	defer endRequests()
	srv := &http.Server{
		Addr:        *addr,
		BaseContext: func(net.Listener) context.Context { return requests },
		Handler: server.New(server.Deps{
			Log: log, Token: token, Terminals: terminals, Store: db, Bus: bus, Sessions: sessions,
			HookURL: "http://" + *addr + "/hooks/claude",
		}),
		ReadHeaderTimeout: 10 * time.Second,
	}

	failed := make(chan error, 1)
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
	terminals.CloseAll()
	endRequests()
	if err := srv.Shutdown(shutdown); err != nil {
		return srv.Close()
	}
	return nil
}
