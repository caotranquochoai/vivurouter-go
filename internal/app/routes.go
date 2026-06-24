package app

import (
	"net"
	"net/http"
	"net/http/pprof"
	"path/filepath"

	"github.com/local/vivurouter-go/internal/config"
	"github.com/local/vivurouter-go/internal/dashboard"
	"github.com/local/vivurouter-go/internal/gateway"
)

func registerRoutes(mux *http.ServeMux, cfg config.Config, dash *dashboard.Handlers, gw *gateway.Handler) {
	staticDir := filepath.Join(cfg.AssetsDir, "static")
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(staticDir))))

	mux.HandleFunc("/", dash.Home)
	mux.HandleFunc("/admin/login", dash.AdminLoginPage)
	mux.HandleFunc("/admin/logout", dash.AdminLogout)
	mux.HandleFunc("/dashboard", dash.Dashboard)
	mux.HandleFunc("/providers", dash.ProvidersPage)
	mux.HandleFunc("/proxies", dash.ProxiesPage)
	mux.HandleFunc("/providers/", dash.ProviderDetailPage)
	mux.HandleFunc("/requests", dash.RequestsPage)
	mux.HandleFunc("/log", redirectToRequests)
	mux.HandleFunc("/logs", redirectToRequests)
	mux.HandleFunc("/api-keys", dash.APIKeysPage)
	mux.HandleFunc("/pricing", dash.PricingPage)
	mux.HandleFunc("/combos", dash.CombosPage)
	mux.HandleFunc("/prompt-routers", dash.PromptRoutersPage)
	mux.HandleFunc("/prompt_routers", dash.PromptRoutersPage)
	mux.HandleFunc("/settings", dash.SettingsPage)

	mux.HandleFunc("/api/health", dash.HealthAPI)
	mux.HandleFunc("/api/config", dash.ConfigAPI)
	mux.HandleFunc("/api/providers", dash.ProvidersAPI)
	mux.HandleFunc("/api/combos", dash.CombosAPI)
	mux.HandleFunc("/api/prompt-routers/export", dash.PromptRoutersExportAPI)
	mux.HandleFunc("/api/prompt-routers/import", dash.PromptRoutersImportAPI)
	mux.HandleFunc("/api/requests/recent", dash.RecentRequestsAPI)
	mux.HandleFunc("/api/metrics", dash.MetricsAPI)
	mux.HandleFunc("/api/usage/stats", dash.UsageStatsAPI)
	mux.HandleFunc("/api/usage/recent", dash.UsageRecentAPI)
	mux.HandleFunc("/api/usage/timeseries", dash.UsageTimeseriesAPI)
	mux.HandleFunc("/api/codex/oauth/start", dash.CodexOAuthStartAPI)
	mux.HandleFunc("/api/codex/oauth/complete", dash.CodexOAuthCompleteAPI)
	mux.HandleFunc("/api/codex/oauth/status", dash.CodexOAuthStatusAPI)
	mux.HandleFunc("/api/antigravity/oauth/start", dash.AntigravityOAuthStartAPI)
	mux.HandleFunc("/api/antigravity/oauth/complete", dash.AntigravityOAuthCompleteAPI)
	mux.HandleFunc("/api/antigravity/oauth/status", dash.AntigravityOAuthStatusAPI)
	mux.HandleFunc("/api/providers/proxy-test", dash.ProviderProxyTestAPI)
	mux.HandleFunc("/api/providers/models", dash.ProviderModelsAPI)
	mux.HandleFunc("/api/providers/test-model", dash.ProviderModelTestAPI)
	mux.HandleFunc("/api/codex/quota", dash.CodexQuotaAPI)
	mux.HandleFunc("/api/antigravity/quota", dash.AntigravityQuotaAPI)
	mux.HandleFunc("/api/cooldowns", dash.CooldownsAPI)
	mux.HandleFunc("/api/admin/backup", dash.BackupAPI)
	mux.HandleFunc("/api/admin/restore", dash.RestoreAPI)
	mux.HandleFunc("/api/admin/reset-data", dash.ResetDataAPI)
	mux.HandleFunc("/api/admin/rtk/status", dash.RTKStatusAPI)
	mux.HandleFunc("/api/admin/request-debug", dash.RequestDebugAPI)
	mux.HandleFunc("/api/admin/request-debug/clear", dash.ClearRequestDebugAPI)

	mux.HandleFunc("/v1/models", gw.Models)
	mux.HandleFunc("/v1/messages", gw.Messages)
	mux.HandleFunc("/v1/chat/completions", gw.ChatCompletions)
	mux.HandleFunc("/v1/responses", gw.Responses)
	mux.HandleFunc("/codex/responses", gw.Responses)
	mux.HandleFunc("/codex/v1/responses", gw.Responses)

	if cfg.Debug {
		registerPprof(mux)
	}
}

func redirectToRequests(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/requests", http.StatusFound)
}

// registerPprof exposes the standard runtime profiling endpoints when DEBUG is
// on. Access is restricted to loopback clients so profiling/heap data and the
// process cmdline are never reachable from the network.
func registerPprof(mux *http.ServeMux) {
	mux.HandleFunc("/debug/pprof/", loopbackOnly(pprof.Index))
	mux.HandleFunc("/debug/pprof/cmdline", loopbackOnly(pprof.Cmdline))
	mux.HandleFunc("/debug/pprof/profile", loopbackOnly(pprof.Profile))
	mux.HandleFunc("/debug/pprof/symbol", loopbackOnly(pprof.Symbol))
	mux.HandleFunc("/debug/pprof/trace", loopbackOnly(pprof.Trace))
}

// loopbackOnly wraps a handler so it only responds to requests from the local
// machine.
func loopbackOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		host := r.RemoteAddr
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			http.NotFound(w, r)
			return
		}
		next(w, r)
	}
}
