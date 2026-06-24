package app

import (
	"net/http"
	"time"

	"github.com/local/vivurouter-go/internal/antigravityoauth"
	"github.com/local/vivurouter-go/internal/codexoauth"
	"github.com/local/vivurouter-go/internal/config"
	"github.com/local/vivurouter-go/internal/dashboard"
	"github.com/local/vivurouter-go/internal/gateway"
	"github.com/local/vivurouter-go/internal/observe"
	"github.com/local/vivurouter-go/internal/provider"
	"github.com/local/vivurouter-go/internal/store"
)

// NewServer wires the fullstack Go sample into one HTTP server.
func NewServer(cfg config.Config, st store.Store) (*http.Server, error) {
	obs := observe.New()
	executors := provider.NewExecutorsWithStore(st)
	gateway.SetRuntimeSettingsProvider(st.GetSettings)
	gatewayHandler := gateway.NewHandler(st, executors, obs)
	codexOAuth := codexoauth.NewManager(st)
	antigravityOAuth := antigravityoauth.NewManager(st)
	dashboardHandler, err := dashboard.NewHandlers(cfg, st, obs, codexOAuth, antigravityOAuth, executors)
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	registerRoutes(mux, cfg, dashboardHandler, gatewayHandler)

	// WriteTimeout is intentionally unset so SSE streams are not cut off.
	return &http.Server{
		Addr:              cfg.Addr(),
		Handler:           recoveryMiddleware(metricsMiddleware(obs, loggingMiddleware(corsMiddleware(csrfMiddleware(mux))))),
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       120 * time.Second,
	}, nil
}
