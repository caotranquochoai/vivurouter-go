package dashboard

import (
	"html/template"
	"path/filepath"
	"time"

	"github.com/local/vivurouter-go/internal/antigravityoauth"
	"github.com/local/vivurouter-go/internal/codexoauth"
	"github.com/local/vivurouter-go/internal/config"
	"github.com/local/vivurouter-go/internal/observe"
	"github.com/local/vivurouter-go/internal/provider"
	"github.com/local/vivurouter-go/internal/store"
)

// Handlers serves server-rendered dashboard pages and small management APIs.
type Handlers struct {
	cfg              config.Config
	store            store.Store
	observe          *observe.State
	codexOAuth       *codexoauth.Manager
	antigravityOAuth *antigravityoauth.Manager
	executors        *provider.Executors
	templates        *template.Template
}

func NewHandlers(cfg config.Config, st store.Store, obs *observe.State, codexOAuth *codexoauth.Manager, antigravityOAuth *antigravityoauth.Manager, executors *provider.Executors) (*Handlers, error) {
	pattern := filepath.Join(cfg.AssetsDir, "templates", "*.html")
	funcMap := template.FuncMap{
		"fmtCost":        formatCost,
		"fmtTokens":      formatTokens,
		"fmtTokensShort": formatTokensShort,
		"relativeTime":   formatRelativeTimeFromNow,
		"fmtDuration":    formatDuration,
		"fmtDurationMS":  func(ms int64) string { return formatDuration(time.Duration(ms) * time.Millisecond) },
		"fmtDate":        formatDateForInput,
		"tr":             translate,
		"json":           templateJSON,
		"seq":            templateSeq,
	}
	tpl, err := template.New("").Funcs(funcMap).ParseGlob(pattern)
	if err != nil {
		return nil, err
	}
	return &Handlers{cfg: cfg, store: st, observe: obs, codexOAuth: codexOAuth, antigravityOAuth: antigravityOAuth, executors: executors, templates: tpl}, nil
}
