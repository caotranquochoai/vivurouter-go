package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/local/vivurouter-go/internal/app"
	"github.com/local/vivurouter-go/internal/config"
	"github.com/local/vivurouter-go/internal/store"
)

func main() {
	cfg := config.Load()

	st, closeStore, err := openStore(cfg)
	if err != nil {
		log.Fatalf("init store: %v", err)
	}
	defer closeStore()

	warnIfInsecure(cfg, st)

	server, err := app.NewServer(cfg, st)
	if err != nil {
		log.Fatalf("init server: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serverErr := make(chan error, 1)
	go func() {
		log.Printf("VivuRouter listening on http://%s (store=%s)", cfg.Addr(), cfg.StoreBackend)
		serverErr <- server.ListenAndServe()
	}()

	select {
	case err := <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server stopped: %v", err)
		}
	case <-ctx.Done():
		log.Printf("shutdown signal received, draining up to %s", cfg.ShutdownTimeout)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("graceful shutdown failed: %v", err)
			_ = server.Close()
		}
		log.Printf("server stopped cleanly")
	}
}

// warnIfInsecure prints a loud warning when the server is exposed beyond
// localhost without either the gateway API key requirement or admin passcode
// protection enabled, since that leaves management endpoints open.
func warnIfInsecure(cfg config.Config, st store.Store) {
	host := strings.TrimSpace(cfg.Host)
	loopbackBind := host == "" || host == "127.0.0.1" || host == "::1" || strings.EqualFold(host, "localhost")
	if loopbackBind {
		return
	}
	settings, err := st.GetSettings()
	adminProtected := err == nil && settings.AdminSecurityEnabled && strings.TrimSpace(settings.AdminPasscode) != ""
	gatewayProtected := cfg.RequireAPIKey || (err == nil && settings.RequireAPIKey)
	if !adminProtected {
		log.Printf("SECURITY WARNING: bound to %s but admin passcode protection is OFF — dashboard and /api/* management endpoints are exposed. Enable admin security in settings.", cfg.Addr())
	}
	if !gatewayProtected {
		log.Printf("SECURITY WARNING: bound to %s but REQUIRE_API_KEY is OFF — the /v1 gateway is an open proxy to your provider credentials.", cfg.Addr())
	}
}

// openStore selects the persistence backend and returns a close function.
func openStore(cfg config.Config) (store.Store, func(), error) {
	switch strings.ToLower(strings.TrimSpace(cfg.StoreBackend)) {
	case "sqlite":
		st, err := store.NewSQLiteStore(cfg.DataDir)
		if err != nil {
			return nil, func() {}, err
		}
		return st, func() { _ = st.Close() }, nil
	default:
		st, err := store.NewFileStore(cfg.DataDir)
		if err != nil {
			return nil, func() {}, err
		}
		return st, func() {}, nil
	}
}
