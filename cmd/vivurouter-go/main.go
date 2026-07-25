package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
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

	if settings, err := st.GetSettings(); err != nil {
		log.Fatalf("load settings: %v", err)
	} else {
		if strings.TrimSpace(settings.BindHost) != "" {
			if settings.BindHost != "127.0.0.1" && settings.BindHost != "0.0.0.0" {
				log.Fatalf("invalid persisted bind host %q", settings.BindHost)
			}
			cfg.Host = settings.BindHost
		}
		if strings.TrimSpace(settings.BindPort) != "" {
			port, err := strconv.Atoi(settings.BindPort)
			if err != nil || port < 1 || port > 65535 {
				log.Fatalf("invalid persisted bind port %q", settings.BindPort)
			}
			cfg.Port = strconv.Itoa(port)
		}
	}

	if err := validateStartupSecurity(cfg, st); err != nil {
		log.Fatalf("invalid deployment security: %v", err)
	}

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

// validateStartupSecurity makes network exposure an explicit operator decision.
// Local loopback operation remains backward-compatible; private and public
// modes require both gateway and dashboard protection unless the narrowly
// scoped migration override is deliberately enabled.
func validateStartupSecurity(cfg config.Config, st store.Store) error {
	if err := cfg.ValidateDeploymentMode(); err != nil {
		return err
	}
	if cfg.AllowInsecureNonLoopback && !cfg.IsLoopbackBind() {
		log.Printf("SECURITY WARNING: ALLOW_INSECURE_NON_LOOPBACK=true bypasses deployment protection for %s", cfg.Addr())
		return nil
	}
	mode := cfg.EffectiveDeploymentMode()
	if mode == config.DeploymentLocal {
		return nil
	}
	settings, err := st.GetSettings()
	if err != nil {
		return fmt.Errorf("load settings for deployment validation: %w", err)
	}
	adminProtected := settings.AdminSecurityEnabled && strings.TrimSpace(settings.AdminPasscode) != ""
	gatewayProtected := cfg.RequireAPIKey || settings.RequireAPIKey
	if !adminProtected {
		return fmt.Errorf("DEPLOYMENT_MODE=%s requires admin security with a non-empty passcode", mode)
	}
	if !gatewayProtected {
		return fmt.Errorf("DEPLOYMENT_MODE=%s requires REQUIRE_API_KEY=true or persisted gateway API-key enforcement", mode)
	}
	if mode == config.DeploymentPublic && len(cfg.GatewayCORSOrigins) == 0 {
		return fmt.Errorf("DEPLOYMENT_MODE=public requires GATEWAY_CORS_ORIGINS")
	}
	return nil
}

// openStore selects the persistence backend and returns a close function. The
// backend is wrapped in an AsyncLogStore so request-log writes happen off the
// gateway's response path; the close function flushes any buffered logs (and
// closes the underlying store) during graceful shutdown so nothing is lost.
func openStore(cfg config.Config) (store.Store, func(), error) {
	switch strings.ToLower(strings.TrimSpace(cfg.StoreBackend)) {
	case "sqlite":
		st, err := store.NewSQLiteStore(cfg.DataDir)
		if err != nil {
			return nil, func() {}, err
		}
		async := store.NewAsyncLogStore(st, 0)
		return async, func() { _ = async.Close() }, nil
	default:
		st, err := store.NewFileStore(cfg.DataDir)
		if err != nil {
			return nil, func() {}, err
		}
		async := store.NewAsyncLogStore(st, 0)
		return async, func() { async.Flush() }, nil
	}
}
