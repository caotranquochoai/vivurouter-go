package dashboard

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/local/vivurouter-go/internal/antigravityoauth"
	"github.com/local/vivurouter-go/internal/codexoauth"
	"github.com/local/vivurouter-go/internal/config"
	"github.com/local/vivurouter-go/internal/observe"
	"github.com/local/vivurouter-go/internal/provider"
	"github.com/local/vivurouter-go/internal/rtkbridge"
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

func (h *Handlers) Home(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, "/dashboard", http.StatusFound)
}

func (h *Handlers) Dashboard(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}
	h.render(w, r, "dashboard.html", "dashboard.title")
}

func (h *Handlers) ProvidersPage(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}
	if providerID := strings.TrimSpace(r.URL.Query().Get("provider")); providerID != "" && r.Method == http.MethodGet {
		http.Redirect(w, r, "/providers/"+url.PathEscape(providerID), http.StatusFound)
		return
	}
	if r.Method == http.MethodPost {
		switch strings.ToLower(strings.TrimSpace(r.FormValue("action"))) {
		case "delete":
			h.deleteProviderForm(w, r)
		case "toggle":
			h.toggleProviderForm(w, r)
		case "add-models":
			h.addProviderModelsForm(w, r)
		default:
			h.saveProviderForm(w, r)
		}
		return
	}
	h.render(w, r, "providers.html", "providers.title")
}

func (h *Handlers) ProxiesPage(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}
	if r.Method == http.MethodPost {
		switch strings.ToLower(strings.TrimSpace(r.FormValue("action"))) {
		case "delete-proxy":
			h.deleteProxyForm(w, r)
		case "bulk-proxies":
			h.bulkProxiesForm(w, r)
		default:
			h.saveProxyForm(w, r)
		}
		return
	}
	h.render(w, r, "proxies.html", "proxies.title")
}

func (h *Handlers) saveProxyForm(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form")
		return
	}
	id := strings.TrimSpace(r.FormValue("id"))
	proxy := store.Proxy{
		ID:      id,
		Name:    strings.TrimSpace(r.FormValue("name")),
		URL:     provider.NormalizeProxyURL(strings.TrimSpace(r.FormValue("proxy_url"))),
		Enabled: r.FormValue("enabled") == "on",
	}
	if id != "" {
		if current, found, err := h.store.GetProxy(id); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		} else if found {
			mergeProxySecrets(&proxy, current)
		}
	}
	if proxy.URL == "" {
		writeError(w, http.StatusBadRequest, "missing proxy URL")
		return
	}
	if _, err := provider.ParseProxyURL(proxy.URL); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.store.UpsertProxy(proxy); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	http.Redirect(w, r, "/proxies?saved=1", http.StatusFound)
}

func (h *Handlers) bulkProxiesForm(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form")
		return
	}
	lines := strings.Split(r.FormValue("bulk_proxies_raw"), "\n")
	added := 0
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		name, rawURL := bulkProxyLine(line)
		rawURL = provider.NormalizeProxyURL(rawURL)
		if rawURL == "" {
			continue
		}
		if _, err := provider.ParseProxyURL(rawURL); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("proxy line %d: %v", i+1, err))
			return
		}
		if name == "" {
			name = fmt.Sprintf("Proxy %d", i+1)
		}
		if err := h.store.UpsertProxy(store.Proxy{Name: name, URL: rawURL, Enabled: true}); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		added++
	}
	if added == 0 {
		writeError(w, http.StatusBadRequest, "no valid proxies")
		return
	}
	http.Redirect(w, r, "/proxies?saved=1", http.StatusFound)
}

func bulkProxyLine(line string) (string, string) {
	parts := strings.SplitN(line, "|", 2)
	if len(parts) == 2 {
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	return "", strings.TrimSpace(line)
}

func (h *Handlers) deleteProxyForm(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form")
		return
	}
	id := strings.TrimSpace(r.FormValue("id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing id")
		return
	}
	providers, err := h.store.ListProviders()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	usedBy := []string{}
	for _, p := range providers {
		if p.ProxyID == id {
			usedBy = append(usedBy, p.ID)
		}
	}
	if len(usedBy) > 0 {
		writeError(w, http.StatusBadRequest, "proxy is still used by providers: "+strings.Join(usedBy, ", "))
		return
	}
	if err := h.store.DeleteProxy(id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	http.Redirect(w, r, "/proxies?saved=1", http.StatusFound)
}

func (h *Handlers) ProviderDetailPage(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}
	providerID, ok := providerIDFromDetailPath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if providerID == "providers" {
		http.Redirect(w, r, "/providers?saved=1", http.StatusSeeOther)
		return
	}
	if r.Method == http.MethodPost {
		switch strings.ToLower(strings.TrimSpace(r.FormValue("action"))) {
		case "delete":
			h.deleteProviderForm(w, r)
		case "toggle":
			h.toggleProviderForm(w, r)
		case "add-models":
			h.addProviderModelsForm(w, r)
		case "add-key":
			h.addKeyForm(w, r)
		case "bulk-keys":
			h.bulkKeysForm(w, r)
		case "delete-key":
			h.deleteKeyForm(w, r)
		case "save-keys-config":
			h.saveKeysConfigForm(w, r)
		default:
			h.saveProviderForm(w, r)
		}
		return
	}
	if _, found, err := h.store.GetProvider(providerID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	} else if !found {
		http.NotFound(w, r)
		return
	}
	renderReq := r.Clone(r.Context())
	urlCopy := *r.URL
	query := urlCopy.Query()
	query.Set("provider", providerID)
	urlCopy.RawQuery = query.Encode()
	renderReq.URL = &urlCopy
	h.render(w, renderReq, "provider_detail.html", "providers.title")
}

func providerIDFromDetailPath(path string) (string, bool) {
	id := strings.TrimPrefix(path, "/providers/")
	id = strings.Trim(id, "/")
	if id == "" || strings.Contains(id, "/") {
		return "", false
	}
	decoded, err := url.PathUnescape(id)
	if err != nil || strings.TrimSpace(decoded) == "" {
		return "", false
	}
	return decoded, true
}

func providerFormRedirect(r *http.Request) string {
	if id, ok := providerIDFromDetailPath(r.URL.Path); ok {
		return "/providers/" + url.PathEscape(id) + "?saved=1"
	}
	return "/providers?saved=1"
}

func (h *Handlers) RequestsPage(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}
	h.render(w, r, "requests.html", "requests.title")
}

func (h *Handlers) PromptRoutersPage(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid form")
			return
		}
		settings, err := h.store.GetSettings()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		action := strings.ToLower(strings.TrimSpace(r.FormValue("action")))
		name := strings.TrimSpace(r.FormValue("name"))
		kept := []store.PromptRouter{}
		for _, existing := range settings.PromptRouters {
			if existing.Name != name {
				kept = append(kept, existing)
			}
		}
		if action != "delete" {
			router := store.PromptRouter{
				Name:                     name,
				Enabled:                  r.FormValue("enabled") == "on",
				ClassifierModel:          strings.TrimSpace(r.FormValue("classifier_model")),
				FallbackTarget:           strings.TrimSpace(r.FormValue("fallback_target")),
				FallbackRole:             strings.ToLower(strings.TrimSpace(r.FormValue("fallback_role"))),
				Description:              strings.TrimSpace(r.FormValue("description")),
				UseRawPrompt:             r.FormValue("use_raw_prompt") == "on",
				ClassifierPromptTemplate: strings.TrimSpace(r.FormValue("classifier_prompt_template")),
			}
			if router.FallbackTarget == "" {
				router.FallbackTarget = router.ClassifierModel
			}
			if router.FallbackRole == "" {
				router.FallbackRole = "planner"
			}
			roleCount, _ := strconv.Atoi(strings.TrimSpace(r.FormValue("role_count")))
			if roleCount < 5 {
				roleCount = 5
			}
			if roleCount > 100 {
				roleCount = 100
			}
			for i := 1; i <= roleCount; i++ {
				suffix := strconv.Itoa(i)
				role := strings.ToLower(strings.TrimSpace(r.FormValue("role_" + suffix)))
				complexity := strings.ToLower(strings.TrimSpace(r.FormValue("complexity_" + suffix)))
				risk := strings.ToLower(strings.TrimSpace(r.FormValue("risk_" + suffix)))
				target := strings.TrimSpace(r.FormValue("target_" + suffix))
				if role != "" && target == "" {
					target = router.FallbackTarget
				}
				if role == "" && target == "" {
					continue
				}
				router.Routes = append(router.Routes, store.PromptRoute{
					Role:              role,
					Complexity:        complexity,
					Risk:              risk,
					Target:            target,
					InjectInstruction: r.FormValue("inject_"+suffix) == "on",
					Instruction:       strings.TrimSpace(r.FormValue("instruction_" + suffix)),
				})
			}
			kept = append(kept, router)
		}
		settings.PromptRouters = store.NormalizePromptRouters(kept)
		if err := h.store.SaveSettings(settings); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		http.Redirect(w, r, "/prompt-routers?saved=1", http.StatusFound)
		return
	}
	h.render(w, r, "prompt_routers.html", "prompt_routers.title")
}

func (h *Handlers) PromptRoutersExportAPI(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	settings, err := h.store.GetSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	filename := "prompt-routers.json"
	var payload any = map[string]any{"prompt_routers": store.NormalizePromptRouters(settings.PromptRouters)}
	if name != "" {
		found := false
		for _, router := range settings.PromptRouters {
			if router.Name == name {
				filename = "prompt-router-" + safeDownloadName(router.Name) + ".json"
				payload = router
				found = true
				break
			}
		}
		if !found {
			writeError(w, http.StatusNotFound, "prompt router not found")
			return
		}
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename="+strconv.Quote(filename))
	_, _ = w.Write(append(data, '\n'))
}

func (h *Handlers) PromptRoutersImportAPI(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON payload")
		return
	}
	incoming, err := decodePromptRoutersImport(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	incoming = store.NormalizePromptRouters(incoming)
	if len(incoming) == 0 {
		writeError(w, http.StatusBadRequest, "no valid prompt routers found")
		return
	}
	settings, err := h.store.GetSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	byName := map[string]store.PromptRouter{}
	order := []string{}
	for _, router := range settings.PromptRouters {
		name := strings.TrimSpace(router.Name)
		if name == "" {
			continue
		}
		if _, exists := byName[name]; !exists {
			order = append(order, name)
		}
		byName[name] = router
	}
	for _, router := range incoming {
		name := strings.TrimSpace(router.Name)
		if name == "" {
			continue
		}
		if _, exists := byName[name]; !exists {
			order = append(order, name)
		}
		byName[name] = router
	}
	merged := make([]store.PromptRouter, 0, len(order))
	for _, name := range order {
		merged = append(merged, byName[name])
	}
	settings.PromptRouters = store.NormalizePromptRouters(merged)
	if err := h.store.SaveSettings(settings); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"imported": len(incoming), "total": len(settings.PromptRouters)})
}

func decodePromptRoutersImport(data []byte) ([]store.PromptRouter, error) {
	data = []byte(strings.TrimSpace(string(data)))
	if len(data) == 0 {
		return nil, fmt.Errorf("missing JSON payload")
	}
	var wrapped struct {
		PromptRouters []store.PromptRouter `json:"prompt_routers"`
	}
	if err := json.Unmarshal(data, &wrapped); err == nil && wrapped.PromptRouters != nil {
		return wrapped.PromptRouters, nil
	}
	var list []store.PromptRouter
	if err := json.Unmarshal(data, &list); err == nil {
		return list, nil
	}
	var single store.PromptRouter
	if err := json.Unmarshal(data, &single); err != nil {
		return nil, fmt.Errorf("invalid prompt router JSON: %w", err)
	}
	return []store.PromptRouter{single}, nil
}

func safeDownloadName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
			continue
		}
		if r == ' ' || r == '/' || r == '\\' || r == ':' {
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-._")
	if out == "" {
		return "router"
	}
	if len(out) > 80 {
		out = strings.Trim(out[:80], "-._")
	}
	if out == "" {
		return "router"
	}
	return out
}

func (h *Handlers) SettingsPage(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}
	if r.Method == http.MethodPost {
		h.saveSettingsForm(w, r)
		return
	}
	h.render(w, r, "settings.html", "settings.title")
}

func (h *Handlers) APIKeysPage(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}
	if r.Method == http.MethodPost {
		h.saveAPIKeysForm(w, r)
		return
	}
	h.render(w, r, "api_keys.html", "api_keys.title")
}

func (h *Handlers) PricingPage(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}
	if r.Method == http.MethodPost {
		h.savePricingForm(w, r)
		return
	}
	h.render(w, r, "pricing.html", "pricing.title")
}

func (h *Handlers) CombosPage(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}
	if r.Method == http.MethodPost {
		h.saveCombosForm(w, r)
		return
	}
	h.render(w, r, "combos.html", "combos.title")
}

func (h *Handlers) FusionsPage(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}
	if r.Method == http.MethodPost {
		h.saveFusionsForm(w, r)
		return
	}
	h.render(w, r, "fusions.html", "Fusion")
}

func (h *Handlers) AdminLoginPage(w http.ResponseWriter, r *http.Request) {
	settings, err := h.store.GetSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !settings.AdminSecurityEnabled || strings.TrimSpace(settings.AdminPasscode) == "" {
		http.Redirect(w, r, "/dashboard", http.StatusFound)
		return
	}
	if r.Method == http.MethodPost {
		if subtle.ConstantTimeCompare([]byte(r.FormValue("passcode")), []byte(settings.AdminPasscode)) == 1 {
			h.setAdminSession(w)
			http.Redirect(w, r, "/dashboard", http.StatusFound)
			return
		}
		h.renderLogin(w, "Mã bảo mật không đúng")
		return
	}
	h.renderLogin(w, "")
}

func (h *Handlers) AdminLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "admin_session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, "/admin/login", http.StatusFound)
}

func (h *Handlers) HealthAPI(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "time": time.Now().UTC(), "name": "vivurouter"})
}

func (h *Handlers) RTKStatusAPI(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	settings, err := h.store.GetSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if overrideEnabled := strings.TrimSpace(r.URL.Query().Get("rtk_enabled")); overrideEnabled != "" {
		settings.RTKEnabled = overrideEnabled == "1" || strings.EqualFold(overrideEnabled, "true") || strings.EqualFold(overrideEnabled, "on")
	}
	if _, ok := r.URL.Query()["rtk_path"]; ok {
		settings.RTKPath = strings.TrimSpace(r.URL.Query().Get("rtk_path"))
	}
	cfg := rtkbridge.ResolveConfig(settings)
	version := ""
	canRunNow := cfg.Detection.Found && cfg.Detection.CanRunNow
	message := cfg.Message
	if settings.RTKEnabled && canRunNow {
		if v, err := cfg.Runner.Version(r.Context()); err == nil {
			version = v
		} else {
			canRunNow = false
			message = "rtk found but version check failed: " + err.Error()
		}
	}
	if strings.TrimSpace(message) == "" {
		message = cfg.Detection.Message
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"token_optimize_tool_results": settings.TokenOptimizeToolResults,
		"native_optimizer_available":  true,
		"enabled":                     settings.RTKEnabled,
		"found":                       cfg.Detection.Found,
		"source":                      cfg.Detection.Source,
		"path":                        cfg.Detection.Path,
		"os":                          cfg.Detection.OS,
		"expected_binary_name":        cfg.Detection.Binary,
		"can_run_now":                 canRunNow,
		"version":                     version,
		"message":                     message,
	})
}

func (h *Handlers) BackupAPI(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}
	path, err := h.databasePath()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.Header().Set("Content-Disposition", "attachment; filename="+filepath.Base(path))
	http.ServeFile(w, r, path)
}

func (h *Handlers) RestoreAPI(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	file, _, err := r.FormFile("db_file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing db_file")
		return
	}
	defer file.Close()
	path, err := h.databasePath()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	backup := path + ".before-restore-" + time.Now().Format("20060102150405") + ".bak"
	_ = copyFile(path, backup)
	if err := writeUploadedFile(path, file); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "restored": filepath.Base(path), "backup": filepath.Base(backup), "message": "Đã khôi phục DB file. Hãy restart server để reload dữ liệu."})
}

func (h *Handlers) ResetDataAPI(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if r.FormValue("confirm") != "DELETE" {
		writeError(w, http.StatusBadRequest, "type DELETE to reset all data")
		return
	}
	if err := h.store.ResetAllData(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	http.Redirect(w, r, "/settings?reset=1", http.StatusSeeOther)
}

func (h *Handlers) RequestDebugAPI(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing request id")
		return
	}
	payload, ok, err := h.store.GetRequestDebugPayload(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok || payload == nil {
		writeError(w, http.StatusNotFound, "debug payload not found")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "debug": payload})
}

func (h *Handlers) ClearRequestDebugAPI(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	deleted, err := h.store.DeleteRequestDebugPayloads()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": deleted, "message": "Đã xóa raw debug payload đã lưu."})
}

func (h *Handlers) ConfigAPI(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdminAPI(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		settings, err := h.store.GetSettings()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, sanitizeSettings(settings))
	case http.MethodPut, http.MethodPost:
		current, err := h.store.GetSettings()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		var settings store.Settings
		if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		mergeSettingsSecrets(&settings, current)
		if err := h.store.SaveSettings(settings); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, sanitizeSettings(settings))
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handlers) ProvidersAPI(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdminAPI(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		providers, err := h.store.ListProviders()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, sanitizeProviders(providers))
	case http.MethodPost, http.MethodPut:
		var incoming store.Provider
		if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if existing, found, err := h.store.GetProvider(incoming.ID); err == nil && found {
			mergeProviderSecrets(&incoming, existing)
		}
		if err := h.store.UpsertProvider(incoming); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, sanitizeProvider(incoming))
	case http.MethodDelete:
		id := strings.TrimSpace(r.URL.Query().Get("id"))
		if id == "" {
			writeError(w, http.StatusBadRequest, "missing id")
			return
		}
		if err := h.store.DeleteProvider(id); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handlers) CombosAPI(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdminAPI(w, r) {
		return
	}
	settings, err := h.store.GetSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, settings.Combos)
	case http.MethodPost, http.MethodPut:
		var combo store.Combo
		if err := json.NewDecoder(r.Body).Decode(&combo); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		kept := []store.Combo{}
		for _, existing := range settings.Combos {
			if existing.Name != combo.Name {
				kept = append(kept, existing)
			}
		}
		settings.Combos = store.NormalizeCombos(append(kept, combo))
		if err := h.store.SaveSettings(settings); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, settings.Combos)
	case http.MethodDelete:
		name := strings.TrimSpace(r.URL.Query().Get("name"))
		if name == "" {
			writeError(w, http.StatusBadRequest, "missing name")
			return
		}
		kept := []store.Combo{}
		for _, combo := range settings.Combos {
			if combo.Name != name {
				kept = append(kept, combo)
			}
		}
		settings.Combos = kept
		if err := h.store.SaveSettings(settings); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handlers) RecentRequestsAPI(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdminAPI(w, r) {
		return
	}
	logs, err := h.store.RecentRequestLogs(100)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, logs)
}

func (h *Handlers) MetricsAPI(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdminAPI(w, r) {
		return
	}
	now := time.Now().UTC()
	writeJSON(w, http.StatusOK, map[string]any{
		"metrics":   h.observe.Metrics.Snapshot(now),
		"cooldowns": h.observe.Cooldowns.Snapshot(now),
	})
}

func (h *Handlers) UsageStatsAPI(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdminAPI(w, r) {
		return
	}
	logs, err := h.store.RecentRequestLogs(0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, summarizeUsage(logs))
}

func (h *Handlers) UsageRecentAPI(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdminAPI(w, r) {
		return
	}
	limit := 100
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 1000 {
			limit = parsed
		}
	}
	logs, err := h.store.RecentRequestLogs(limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, logs)
}

// UsageTimeseriesAPI returns bucketed usage plus budget status for the dashboard
// time-series chart. Query param `range` is one of today/24h/7d/30d.
func (h *Handlers) UsageTimeseriesAPI(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdminAPI(w, r) {
		return
	}
	logs, err := h.store.RecentRequestLogs(0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	settings, err := h.store.GetSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	now := time.Now().UTC()
	rangeKey := r.URL.Query().Get("range")
	series := buildUsageSeries(logs, rangeKey, now)
	filteredLogs := filterLogsForRange(logs, rangeKey, now)
	providers, _ := h.store.ListProviders()
	tableData := buildUsageTableData(filteredLogs, settings, providers, now)

	writeJSON(w, http.StatusOK, map[string]any{
		"series": series,
		"budget": buildBudgetStatus(logs, settings, now),
		"table":  tableData,
	})
}

func (h *Handlers) CodexOAuthStartAPI(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdminAPI(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	providerID := strings.TrimSpace(r.URL.Query().Get("provider_id"))
	if providerID == "" {
		_ = r.ParseForm()
		providerID = strings.TrimSpace(r.FormValue("provider_id"))
	}
	proxyURL := strings.TrimSpace(r.URL.Query().Get("proxy_url"))
	if proxyURL == "" {
		_ = r.ParseForm()
		proxyURL = strings.TrimSpace(r.FormValue("proxy_url"))
	}
	status, err := h.codexOAuth.Start(providerID, proxyURL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if strings.Contains(r.Header.Get("Accept"), "application/json") || r.URL.Query().Get("json") == "1" {
		writeJSON(w, http.StatusOK, status)
		return
	}
	http.Redirect(w, r, status.AuthURL, http.StatusFound)
}

func (h *Handlers) CodexOAuthCompleteAPI(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdminAPI(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var payload struct {
		CallbackURL string `json:"callback_url"`
	}
	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		_ = json.NewDecoder(r.Body).Decode(&payload)
	} else {
		_ = r.ParseForm()
		payload.CallbackURL = r.FormValue("callback_url")
	}
	if strings.TrimSpace(payload.CallbackURL) == "" {
		writeError(w, http.StatusBadRequest, "missing callback URL")
		return
	}
	if err := h.codexOAuth.CompleteWithCallbackURL(r.Context(), payload.CallbackURL); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": h.codexOAuth.Status()})
}

func (h *Handlers) CodexOAuthStatusAPI(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdminAPI(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, h.codexOAuth.Status())
}

func (h *Handlers) ProviderProxyTestAPI(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdminAPI(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var payload struct {
		ProxyURL string `json:"proxy_url"`
		ProxyID  string `json:"proxy_id"`
		Target   string `json:"target"`
	}
	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		_ = json.NewDecoder(r.Body).Decode(&payload)
	} else {
		_ = r.ParseForm()
		payload.ProxyURL = r.FormValue("proxy_url")
		payload.ProxyID = r.FormValue("proxy_id")
		payload.Target = r.FormValue("target")
	}
	payload.ProxyID = strings.TrimSpace(payload.ProxyID)
	if payload.ProxyID != "" {
		px, found, err := h.store.GetProxy(payload.ProxyID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !found {
			writeError(w, http.StatusBadRequest, "proxy ID không tồn tại")
			return
		}
		if !px.Enabled {
			writeError(w, http.StatusBadRequest, "proxy này đang bị tắt")
			return
		}
		payload.ProxyURL = px.URL
	}
	payload.ProxyURL = provider.NormalizeProxyURL(strings.TrimSpace(payload.ProxyURL))
	if payload.ProxyURL == "" {
		writeError(w, http.StatusBadRequest, "missing proxy URL")
		return
	}
	parsed, err := url.Parse(payload.ProxyURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		writeError(w, http.StatusBadRequest, "proxy URL không hợp lệ")
		return
	}
	target := strings.TrimSpace(payload.Target)
	if target == "" {
		target = "https://api.openai.com/v1/models"
	}
	if err := validateOutboundURL(target); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	client := &http.Client{
		Timeout: 12 * time.Second,
		Transport: &http.Transport{
			Proxy:                 http.ProxyURL(parsed),
			TLSHandshakeTimeout:   8 * time.Second,
			ResponseHeaderTimeout: 8 * time.Second,
		},
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, target, nil)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	started := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error(), "latency_ms": time.Since(started).Milliseconds()})
		return
	}
	defer resp.Body.Close()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": resp.StatusCode, "latency_ms": time.Since(started).Milliseconds(), "proxy": parsed.Redacted()})
}

func (h *Handlers) CooldownsAPI(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdminAPI(w, r) {
		return
	}
	writeJSON(w, http.StatusOK, h.observe.Cooldowns.Snapshot(time.Now().UTC()))
}

func requestPageParams(r *http.Request) (int, int) {
	limit := 25
	switch strings.TrimSpace(r.URL.Query().Get("limit")) {
	case "50":
		limit = 50
	case "100":
		limit = 100
	}
	page := 1
	if raw := strings.TrimSpace(r.URL.Query().Get("page")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			page = n
		}
	}
	return page, limit
}

func buildRequestLogViews(logs []store.RequestLog) []requestLogView {
	views := make([]requestLogView, 0, len(logs))
	for _, log := range logs {
		view := requestLogView{RequestLog: log}
		if trace := parseFusionTraceView(log); trace != nil && trace.HasDetails() {
			view.Fusion = trace
		}
		views = append(views, view)
	}
	return views
}

type fusionTracePayload struct {
	Experts     []fusionExpertTracePayload `json:"experts"`
	Synthesizer fusionStageTracePayload    `json:"synthesizer"`
	Reviewer    fusionStageTracePayload    `json:"reviewer"`
	Synthesis   string                     `json:"synthesis"`
	Final       string                     `json:"final"`
}

type fusionStageTracePayload struct {
	Name         string `json:"name"`
	Target       string `json:"target"`
	Success      bool   `json:"success"`
	Error        string `json:"error"`
	Content      string `json:"content"`
	DurationMS   int64  `json:"duration_ms"`
	PromptTokens int    `json:"prompt_tokens"`
	OutputTokens int    `json:"output_tokens"`
}

type fusionExpertTracePayload struct {
	ExpertName   string `json:"expert_name"`
	Target       string `json:"target"`
	Role         string `json:"role"`
	Success      bool   `json:"success"`
	Error        string `json:"error"`
	Content      string `json:"content"`
	DurationMS   int64  `json:"duration_ms"`
	PromptTokens int    `json:"prompt_tokens"`
	OutputTokens int    `json:"output_tokens"`
}

func parseFusionTraceView(log store.RequestLog) *fusionTraceView {
	if strings.TrimSpace(log.FusionTrace) == "" {
		return nil
	}
	var payload fusionTracePayload
	if err := json.Unmarshal([]byte(log.FusionTrace), &payload); err != nil {
		return nil
	}
	view := fusionTraceView{
		SynthesisPreview: truncate(payload.Synthesis, 900),
		FinalPreview:     truncate(payload.Final, 900),
	}
	if stage := fusionStageTraceViewFromPayload(payload.Synthesizer); stage != nil {
		view.Synthesizer = stage
	}
	if stage := fusionStageTraceViewFromPayload(payload.Reviewer); stage != nil {
		view.Reviewer = stage
	}
	for _, item := range payload.Experts {
		name := strings.TrimSpace(item.ExpertName)
		if name == "" {
			name = item.Target
		}
		view.Experts = append(view.Experts, fusionExpertTraceView{
			Name:           name,
			Target:         item.Target,
			Role:           item.Role,
			Success:        item.Success,
			Error:          truncate(item.Error, 500),
			ContentPreview: truncate(item.Content, 900),
			DurationMS:     item.DurationMS,
			PromptTokens:   item.PromptTokens,
			OutputTokens:   item.OutputTokens,
		})
	}
	return &view
}

func fusionStageTraceViewFromPayload(item fusionStageTracePayload) *fusionStageTraceView {
	if strings.TrimSpace(item.Target) == "" && item.DurationMS == 0 && !item.Success && strings.TrimSpace(item.Error) == "" && strings.TrimSpace(item.Content) == "" {
		return nil
	}
	return &fusionStageTraceView{
		Name:           item.Name,
		Target:         item.Target,
		Success:        item.Success,
		Error:          truncate(item.Error, 500),
		ContentPreview: truncate(item.Content, 900),
		DurationMS:     item.DurationMS,
		PromptTokens:   item.PromptTokens,
		OutputTokens:   item.OutputTokens,
	}
}

func paginateRequestLogs(logs []store.RequestLog, page int, limit int) []store.RequestLog {
	if limit <= 0 {
		limit = 25
	}
	if page <= 0 {
		page = 1
	}
	start := (page - 1) * limit
	if start >= len(logs) {
		return []store.RequestLog{}
	}
	end := start + limit
	if end > len(logs) {
		end = len(logs)
	}
	return logs[start:end]
}

func (h *Handlers) render(w http.ResponseWriter, r *http.Request, templateName string, title string) {
	settings, err := h.store.GetSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	providers, err := h.store.ListProviders()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	proxies, err := h.store.ListProxies()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	proxyCards := make([]proxyCard, len(proxies))
	proxyTotal, proxyEnabled, proxyDisabled := 0, 0, 0
	for i, px := range proxies {
		proxyTotal++
		if px.Enabled {
			proxyEnabled++
		} else {
			proxyDisabled++
		}
		useCount := 0
		for _, p := range providers {
			if p.ProxyID == px.ID {
				useCount++
			}
		}
		proxyCards[i] = proxyCard{
			ID:        px.ID,
			Name:      px.Name,
			URL:       px.URL,
			Redacted:  redactProxyURL(px.URL),
			Enabled:   px.Enabled,
			CreatedAt: px.CreatedAt.Format("2006-01-02 15:04:05"),
			UpdatedAt: px.UpdatedAt.Format("2006-01-02 15:04:05"),
			UseCount:  useCount,
		}
	}
	pSummary := proxySummary{
		Total:    proxyTotal,
		Enabled:  proxyEnabled,
		Disabled: proxyDisabled,
	}
	logs, _ := h.store.RecentRequestLogs(25)
	allLogs, _ := h.store.RecentRequestLogs(0)
	requestPage, requestLimit := requestPageParams(r)
	requestTotal := len(allLogs)
	requestShown := len(logs)
	requestHasPrev, requestHasNext := false, false
	requestPrevPage, requestNextPage := 1, 1
	if templateName == "requests.html" {
		logs = paginateRequestLogs(allLogs, requestPage, requestLimit)
		requestShown = len(logs)
		requestHasPrev = requestPage > 1
		requestHasNext = requestPage*requestLimit < requestTotal
		requestPrevPage = requestPage - 1
		if requestPrevPage < 1 {
			requestPrevPage = 1
		}
		requestNextPage = requestPage + 1
	}
	now := time.Now().UTC()
	providerUsagePeriod := providerUsagePeriodParam(r)
	providerUsageLogs := filterProviderUsageLogs(allLogs, providerUsagePeriod, now)
	providerUsageByProvider, providerUsageByModel := summarizeProviderUsage(providerUsageLogs)
	providerUsagePeriodLabel := providerUsagePeriodLabel(providerUsagePeriod)
	cooldowns := h.observe.Cooldowns.Snapshot(now)
	providerGroups, providerSummary := buildProviderGroups(providers, proxies, allLogs, cooldowns, settings, now, providerUsageByProvider, providerUsageByModel, providerUsagePeriodLabel)
	selectedProvider := selectedProviderCard(providerGroups, r.URL.Query().Get("provider"))
	metrics := h.observe.Metrics.Snapshot(now)
	lang := resolveLang(r)
	bundle := translationBundle(lang)
	if r.URL.Query().Get("lang") != "" {
		http.SetCookie(w, &http.Cookie{Name: "vivurouter_lang", Value: lang, Path: "/", MaxAge: 86400 * 365, SameSite: http.SameSiteLaxMode})
	}
	data := pageData{
		Title:               translate(bundle, title),
		Lang:                lang,
		T:                   bundle,
		Now:                 now,
		Config:              h.cfg,
		Settings:            settings,
		Providers:           providers,
		Proxies:             sanitizeProxies(proxies),
		ProxyCards:          proxyCards,
		ProxySummary:        pSummary,
		ProviderSummary:     providerSummary,
		ProviderGroups:      providerGroups,
		SelectedProvider:    selectedProvider,
		Requests:            logs,
		RequestViews:        buildRequestLogViews(logs),
		Usage:               summarizeUsage(allLogs),
		UsageTable:          buildUsageTableData(filterLogsForRange(allLogs, "24h", now), settings, providers, now),
		ProviderUsagePeriod: providerUsagePeriod,
		CodexOAuth:          h.codexOAuth.Status(),
		CodexQuota:          codexQuotaSeed{ProviderID: firstCodexProviderID(settings, providers)},
		Metrics:             metrics,
		Cooldowns:           cooldowns,
		Saved:               r.URL.Query().Get("saved") == "1",
		APIKeysText:         formatAPIKeyPolicies(settings.APIKeys),
		ModelPricesText:     formatModelPriceRules(settings.ModelPrices),
		PricingGroups:       groupModelPriceRules(settings.ModelPrices),
		CombosText:          formatCombos(settings.Combos),
		PromptRoutersText:   formatPromptRouters(settings.PromptRouters),
		FusionsText:         formatFusions(settings.Fusions),
		AvailableModels:     comboModelOptions(providers),
		RequestTotal:        requestTotal,
		RequestShown:        requestShown,
		RequestPage:         requestPage,
		RequestLimit:        requestLimit,
		RequestPrevPage:     requestPrevPage,
		RequestNextPage:     requestNextPage,
		RequestHasPrev:      requestHasPrev,
		RequestHasNext:      requestHasNext,
		UptimeLabel:         formatDuration(time.Duration(metrics.UptimeSeconds) * time.Second),
		CostNote:            buildCostNote(allLogs, settings),
		Budget:              buildBudgetStatus(allLogs, settings, now),
		UsageSeries:         buildUsageSeries(allLogs, "24h", now),
	}
	if editID := strings.TrimSpace(r.URL.Query().Get("edit")); editID != "" {
		if editID == "1" && selectedProvider != nil {
			editID = selectedProvider.ID
		}
		if p, found, _ := h.store.GetProvider(editID); found {
			data.EditProvider = &p
		}
	}
	if editProxyID := strings.TrimSpace(r.URL.Query().Get("edit_proxy")); editProxyID != "" {
		if p, found, _ := h.store.GetProxy(editProxyID); found {
			masked := sanitizeProxy(p)
			data.EditProxy = &masked
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.templates.ExecuteTemplate(w, templateName, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (h *Handlers) deleteProviderForm(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form")
		return
	}
	id := strings.TrimSpace(r.FormValue("id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing id")
		return
	}
	if err := h.store.DeleteProvider(id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	http.Redirect(w, r, "/providers?saved=1", http.StatusFound)
}

func (h *Handlers) toggleProviderForm(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form")
		return
	}
	id := strings.TrimSpace(r.FormValue("id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing id")
		return
	}
	provider, found, err := h.store.GetProvider(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "provider not found")
		return
	}
	provider.Enabled = r.FormValue("enabled") == "true" || r.FormValue("enabled") == "on"
	if err := h.store.UpsertProvider(provider); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	http.Redirect(w, r, providerFormRedirect(r), http.StatusSeeOther)
}

func (h *Handlers) addProviderModelsForm(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form")
		return
	}
	id := strings.TrimSpace(r.FormValue("id"))
	models := splitModels(r.FormValue("custom_models"))
	if id == "" || len(models) == 0 {
		writeError(w, http.StatusBadRequest, "missing provider id or models")
		return
	}
	provider, found, err := h.store.GetProvider(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "provider not found")
		return
	}
	provider.Models = store.NormalizeModels(append(provider.Models, models...))
	if err := h.store.UpsertProvider(provider); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	http.Redirect(w, r, providerFormRedirect(r), http.StatusSeeOther)
}

func (h *Handlers) saveProviderForm(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form")
		return
	}
	id := strings.TrimSpace(r.FormValue("id"))
	provider := store.Provider{
		ID:           id,
		Type:         strings.TrimSpace(r.FormValue("type")),
		Name:         strings.TrimSpace(r.FormValue("name")),
		BaseURL:      strings.TrimSpace(r.FormValue("base_url")),
		APIKey:       strings.TrimSpace(r.FormValue("api_key")),
		AccessToken:  strings.TrimSpace(r.FormValue("access_token")),
		RefreshToken: strings.TrimSpace(r.FormValue("refresh_token")),
		ProxyURL:     provider.NormalizeProxyURL(strings.TrimSpace(r.FormValue("proxy_url"))),
			ProxyID:      strings.TrimSpace(r.FormValue("proxy_id")),
		Enabled:      r.FormValue("enabled") == "on",
		Models:       splitModels(r.FormValue("models")),
		KeyStrategy:  strings.TrimSpace(r.FormValue("key_strategy")),
	}
	if limit := r.FormValue("sticky_limit"); limit != "" {
		if n, err := strconv.Atoi(limit); err == nil {
			provider.StickyLimit = n
		}
	}
	if existing, found, err := h.store.GetProvider(id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	} else if found {
		if provider.APIKey == "" {
			provider.APIKey = existing.APIKey
		}
		if provider.AccessToken == "" {
			provider.AccessToken = existing.AccessToken
		}
		if provider.RefreshToken == "" {
			provider.RefreshToken = existing.RefreshToken
		}
		provider.Keys = existing.Keys
		if provider.KeyStrategy == "" {
			provider.KeyStrategy = existing.KeyStrategy
		}
		if provider.StickyLimit <= 0 {
			provider.StickyLimit = existing.StickyLimit
		}
		mergeProviderSecrets(&provider, existing)
	}
	if provider.ProxyID != "" {
		px, found, err := h.store.GetProxy(provider.ProxyID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !found {
			writeError(w, http.StatusBadRequest, "proxy not found")
			return
		}
		if !px.Enabled {
			writeError(w, http.StatusBadRequest, "proxy is disabled")
			return
		}
	}
	if err := h.store.UpsertProvider(provider); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	http.Redirect(w, r, providerFormRedirect(r), http.StatusSeeOther)
}

func (h *Handlers) addKeyForm(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form")
		return
	}
	id := strings.TrimSpace(r.FormValue("id"))
	p, found, err := h.store.GetProvider(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "provider not found")
		return
	}

	keyID := strings.TrimSpace(r.FormValue("key_id"))
	keyVal := strings.TrimSpace(r.FormValue("key_value"))
	priority := 1
	if pri := r.FormValue("key_priority"); pri != "" {
		if n, err := strconv.Atoi(pri); err == nil && n > 0 {
			priority = n
		}
	}
	enabled := r.FormValue("key_enabled") == "on"

	if keyID != "" {
		// Edit mode
		updated := false
		for i := range p.Keys {
			if p.Keys[i].ID == keyID {
				p.Keys[i].Name = strings.TrimSpace(r.FormValue("key_name"))
				if p.Keys[i].Name == "" {
					p.Keys[i].Name = "Key " + p.Keys[i].ID
				}
				if keyVal != "" && !isMaskedSecret(keyVal) {
					p.Keys[i].Key = keyVal
				}
				p.Keys[i].Priority = priority
				p.Keys[i].Enabled = enabled
				updated = true
				break
			}
		}
		if !updated {
			writeError(w, http.StatusNotFound, "key not found")
			return
		}
	} else {
		// Add mode
		if keyVal == "" {
			writeError(w, http.StatusBadRequest, "key value is required")
			return
		}
		newKeyID := "key-" + randomHex(4)
		newKey := store.ProviderKey{
			ID:       newKeyID,
			Name:     strings.TrimSpace(r.FormValue("key_name")),
			Key:      keyVal,
			Enabled:  true,
			Priority: priority,
		}
		if newKey.Name == "" {
			newKey.Name = "Key " + newKey.ID
		}
		p.Keys = append(p.Keys, newKey)
	}

	if err := h.store.UpsertProvider(p); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Clear provider cooldown tracker
	if h.executors != nil && h.executors.KeyPool != nil {
		h.executors.KeyPool.ClearCooldowns(p.ID)
	}

	http.Redirect(w, r, "/providers/"+p.ID, http.StatusSeeOther)
}

func (h *Handlers) bulkKeysForm(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form")
		return
	}
	id := strings.TrimSpace(r.FormValue("id"))
	p, found, err := h.store.GetProvider(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "provider not found")
		return
	}

	raw := r.FormValue("bulk_keys_raw")
	lines := strings.Split(raw, "\n")
	basePriority := len(p.Keys) + 1

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		name := ""
		key := line
		if idx := strings.Index(line, "|"); idx != -1 {
			name = strings.TrimSpace(line[:idx])
			key = strings.TrimSpace(line[idx+1:])
		}
		if key == "" {
			continue
		}

		keyID := "key-" + randomHex(4)
		if name == "" {
			name = "Key " + keyID
		}
		newKey := store.ProviderKey{
			ID:       keyID,
			Name:     name,
			Key:      key,
			Enabled:  true,
			Priority: basePriority,
		}
		p.Keys = append(p.Keys, newKey)
		basePriority++
	}

	if err := h.store.UpsertProvider(p); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if h.executors != nil && h.executors.KeyPool != nil {
		h.executors.KeyPool.ClearCooldowns(p.ID)
	}

	http.Redirect(w, r, "/providers/"+p.ID, http.StatusSeeOther)
}

func (h *Handlers) deleteKeyForm(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form")
		return
	}
	id := strings.TrimSpace(r.FormValue("id"))
	p, found, err := h.store.GetProvider(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "provider not found")
		return
	}

	keyID := strings.TrimSpace(r.FormValue("key_id"))
	newKeys := []store.ProviderKey{}
	for _, k := range p.Keys {
		if k.ID != keyID {
			newKeys = append(newKeys, k)
		}
	}
	p.Keys = newKeys

	if err := h.store.UpsertProvider(p); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if h.executors != nil && h.executors.KeyPool != nil {
		h.executors.KeyPool.ClearCooldowns(p.ID)
	}

	http.Redirect(w, r, "/providers/"+p.ID, http.StatusSeeOther)
}

func (h *Handlers) saveKeysConfigForm(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form")
		return
	}
	id := strings.TrimSpace(r.FormValue("id"))
	p, found, err := h.store.GetProvider(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "provider not found")
		return
	}

	if delID := strings.TrimSpace(r.FormValue("delete_key_btn")); delID != "" {
		newKeys := []store.ProviderKey{}
		for _, k := range p.Keys {
			if k.ID != delID {
				newKeys = append(newKeys, k)
			}
		}
		p.Keys = newKeys
		if err := h.store.UpsertProvider(p); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if h.executors != nil && h.executors.KeyPool != nil {
			h.executors.KeyPool.ClearCooldowns(p.ID)
		}
		http.Redirect(w, r, "/providers/"+p.ID, http.StatusSeeOther)
		return
	}

	p.KeyStrategy = strings.TrimSpace(r.FormValue("key_strategy"))
	if limit := r.FormValue("sticky_limit"); limit != "" {
		if n, err := strconv.Atoi(limit); err == nil && n > 0 {
			p.StickyLimit = n
		}
	}

	if err := h.store.UpsertProvider(p); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if h.executors != nil && h.executors.KeyPool != nil {
		h.executors.KeyPool.ClearCooldowns(p.ID)
	}

	http.Redirect(w, r, "/providers/"+p.ID, http.StatusSeeOther)
}

func (h *Handlers) saveSettingsForm(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form")
		return
	}
	settings, err := h.store.GetSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	settings.LocalAPIKey = strings.TrimSpace(r.FormValue("local_api_key"))
	settings.DefaultProvider = strings.TrimSpace(r.FormValue("default_provider"))
	settings.DefaultCodexID = strings.TrimSpace(r.FormValue("default_codex_id"))
	settings.DashboardMessage = strings.TrimSpace(r.FormValue("dashboard_message"))
	settings.AdminSecurityEnabled = r.FormValue("admin_security_enabled") == "on"
	if passcode := strings.TrimSpace(r.FormValue("admin_passcode")); passcode != "" {
		settings.AdminPasscode = passcode
	}
	if !settings.AdminSecurityEnabled {
		settings.AdminPasscode = ""
	}
	settings.ObservabilityEnabled = r.FormValue("observability_enabled") == "on"
	settings.SaveRawPrompt = r.FormValue("save_raw_prompt") == "on"
	settings.SaveRawToolResult = r.FormValue("save_raw_tool_result") == "on"
	settings.MaskDebugSecrets = r.FormValue("mask_debug_secrets") == "on"
	settings.CompactDebugPayloads = r.FormValue("compact_debug_payloads") == "on"
	settings.PromptRouterCompressionMode = strings.TrimSpace(r.FormValue("prompt_router_compression_mode"))
	settings.PromptRouterCompressSystem = r.FormValue("prompt_router_compress_system") == "on"
	settings.PromptRouterCompressDeveloper = r.FormValue("prompt_router_compress_developer") == "on"
	settings.PromptRouterCompressMessages = r.FormValue("prompt_router_compress_messages") == "on"
	settings.PromptRouterCompressToolResults = r.FormValue("prompt_router_compress_tool_results") == "on"
	settings.PromptRouterCompressToolSchemas = r.FormValue("prompt_router_compress_tool_schemas") == "on"
	settings.PromptRouterCompressImages = r.FormValue("prompt_router_compress_images") == "on"
	settings.TokenOptimizeToolResults = r.FormValue("token_optimize_tool_results") == "on"
	settings.TokenOptimizeSystem = r.FormValue("token_optimize_system") == "on"
	settings.TokenOptimizeDeveloper = r.FormValue("token_optimize_developer") == "on"
	settings.TokenOptimizeText = r.FormValue("token_optimize_text") == "on"
	settings.TokenOptimizeToolSchemas = r.FormValue("token_optimize_tool_schemas") == "on"
	settings.TokenOptimizeToolCalls = r.FormValue("token_optimize_tool_calls") == "on"
	settings.RTKEnabled = r.FormValue("rtk_enabled") == "on"
	settings.RTKPath = strings.TrimSpace(r.FormValue("rtk_path"))
	if minChars := strings.TrimSpace(r.FormValue("token_optimize_min_chars")); minChars != "" {
		if n, err := strconv.Atoi(minChars); err == nil && n > 0 {
			settings.TokenOptimizeMinChars = n
		}
	}
	if maxChars := strings.TrimSpace(r.FormValue("token_optimize_max_chars")); maxChars != "" {
		if n, err := strconv.Atoi(maxChars); err == nil && n > 0 {
			settings.TokenOptimizeMaxChars = n
		}
	}
	if maxDebug := strings.TrimSpace(r.FormValue("max_debug_payload_bytes")); maxDebug != "" {
		if n, err := strconv.Atoi(maxDebug); err == nil && n > 0 {
			settings.MaxDebugPayloadBytes = n
		}
	}
	if keep := strings.TrimSpace(r.FormValue("keep_request_logs")); keep != "" {
		if n, err := strconv.Atoi(keep); err == nil && n > 0 {
			settings.KeepRequestLogs = n
		}
	}
	settings.DailyBudgetUSD = parseNonNegativeFloatLimit(r.FormValue("daily_budget_usd"))
	settings.MonthlyBudgetUSD = parseNonNegativeFloatLimit(r.FormValue("monthly_budget_usd"))
	if pct := strings.TrimSpace(r.FormValue("budget_alert_pct")); pct != "" {
		if n, err := strconv.Atoi(pct); err == nil {
			settings.BudgetAlertPct = n
		}
	}
	if settings.DefaultProvider == "" {
		settings.DefaultProvider = "openai"
	}
	if settings.DefaultCodexID == "" {
		settings.DefaultCodexID = "codex"
	}
	if err := h.store.SaveSettings(settings); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	http.Redirect(w, r, "/settings?saved=1", http.StatusFound)
}

func (h *Handlers) saveAPIKeysForm(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form")
		return
	}
	settings, err := h.store.GetSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	settings.RequireAPIKey = r.FormValue("require_api_key") == "on"
	settings.APIKeys = parseAPIKeyPolicies(r.FormValue("api_keys"))
	if err := h.store.SaveSettings(settings); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	http.Redirect(w, r, "/api-keys?saved=1", http.StatusFound)
}

func (h *Handlers) saveCombosForm(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form")
		return
	}
	settings, err := h.store.GetSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	settings.Combos = parseCombos(r.FormValue("combos"))
	if err := h.store.SaveSettings(settings); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	http.Redirect(w, r, "/combos?saved=1", http.StatusFound)
}

func (h *Handlers) saveFusionsForm(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form")
		return
	}
	settings, err := h.store.GetSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	settings.Fusions = parseFusions(r.FormValue("fusions"))
	if err := h.store.SaveSettings(settings); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	http.Redirect(w, r, "/fusions?saved=1", http.StatusFound)
}

func (h *Handlers) savePricingForm(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form")
		return
	}
	settings, err := h.store.GetSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	settings.ModelPrices = parseModelPriceRules(r.FormValue("model_prices"))
	if err := h.store.SaveSettings(settings); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	http.Redirect(w, r, "/pricing?saved=1", http.StatusFound)
}

func parseAPIKeyPolicies(raw string) []store.APIKeyPolicy {
	out := []store.APIKeyPolicy{}
	for _, line := range strings.Split(raw, "\n") {
		parts := strings.Split(line, "|")
		if len(parts) < 2 {
			continue
		}
		item := store.APIKeyPolicy{ID: strings.TrimSpace(parts[0]), Key: strings.TrimSpace(parts[1]), Enabled: true}
		if len(parts) > 6 {
			item.Enabled, _ = strconv.ParseBool(strings.TrimSpace(parts[6]))
		}
		if len(parts) > 2 {
			item.AllowedModels = splitModels(parts[2])
		}
		if len(parts) > 3 {
			item.MaxRequests = parseNonNegativeIntLimit(parts[3])
		}
		if len(parts) > 4 {
			item.MaxTokens = parseNonNegativeIntLimit(parts[4])
		}
		if len(parts) > 5 {
			item.MaxCostUSD = parseNonNegativeFloatLimit(parts[5])
		}
		out = append(out, item)
	}
	return store.NormalizeAPIKeyPolicies(out)
}

func parseModelPriceRules(raw string) []store.ModelPriceRule {
	out := []store.ModelPriceRule{}
	for _, line := range strings.Split(raw, "\n") {
		parts := strings.Split(line, "|")
		if len(parts) < 6 {
			continue
		}
		item := store.ModelPriceRule{ProviderID: strings.TrimSpace(parts[0]), Model: strings.TrimSpace(parts[1])}
		item.InputPer1M, _ = strconv.ParseFloat(strings.TrimSpace(parts[2]), 64)
		item.OutputPer1M, _ = strconv.ParseFloat(strings.TrimSpace(parts[3]), 64)
		item.CachedInputPer1M, _ = strconv.ParseFloat(strings.TrimSpace(parts[4]), 64)
		item.ReasoningPer1M, _ = strconv.ParseFloat(strings.TrimSpace(parts[5]), 64)
		if len(parts) > 6 {
			item.ContextLength, _ = strconv.Atoi(strings.TrimSpace(parts[6]))
		}
		if len(parts) > 7 {
			item.RPM, _ = strconv.Atoi(strings.TrimSpace(parts[7]))
		}
		if len(parts) > 8 {
			item.TPM, _ = strconv.Atoi(strings.TrimSpace(parts[8]))
		}
		if len(parts) > 9 {
			item.DailyRequests, _ = strconv.Atoi(strings.TrimSpace(parts[9]))
		}
		if len(parts) > 10 {
			item.DailyTokens, _ = strconv.Atoi(strings.TrimSpace(parts[10]))
		}
		out = append(out, item)
	}
	return store.NormalizeModelPriceRules(out)
}

func parseNonNegativeIntLimit(raw string) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

func parseNonNegativeFloatLimit(raw string) float64 {
	n, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

func formatAPIKeyPolicies(items []store.APIKeyPolicy) string {
	lines := []string{}
	for _, item := range items {
		lines = append(lines, strings.Join([]string{
			item.ID,
			item.Key,
			strings.Join(item.AllowedModels, ","),
			strconv.Itoa(item.MaxRequests),
			strconv.Itoa(item.MaxTokens),
			strconv.FormatFloat(item.MaxCostUSD, 'f', -1, 64),
			strconv.FormatBool(item.Enabled),
		}, "|"))
	}
	return strings.Join(lines, "\n")
}

func groupModelPriceRules(items []store.ModelPriceRule) []pricingRuleGroup {
	groups := []pricingRuleGroup{}
	index := map[string]int{}
	for _, item := range items {
		key := strings.Join([]string{
			strconv.FormatFloat(item.InputPer1M, 'f', -1, 64),
			strconv.FormatFloat(item.OutputPer1M, 'f', -1, 64),
			strconv.FormatFloat(item.CachedInputPer1M, 'f', -1, 64),
			strconv.FormatFloat(item.ReasoningPer1M, 'f', -1, 64),
			strconv.Itoa(item.ContextLength),
			strconv.Itoa(item.RPM),
			strconv.Itoa(item.TPM),
			strconv.Itoa(item.DailyRequests),
			strconv.Itoa(item.DailyTokens),
		}, "|")
		idx, ok := index[key]
		if !ok {
			idx = len(groups)
			index[key] = idx
			groups = append(groups, pricingRuleGroup{
				InputPer1M:       item.InputPer1M,
				OutputPer1M:      item.OutputPer1M,
				CachedInputPer1M: item.CachedInputPer1M,
				ReasoningPer1M:   item.ReasoningPer1M,
				ContextLength:    item.ContextLength,
				RPM:              item.RPM,
				TPM:              item.TPM,
				DailyRequests:    item.DailyRequests,
				DailyTokens:      item.DailyTokens,
			})
		}
		group := &groups[idx]
		if group.ProviderID == "" {
			group.ProviderID = item.ProviderID
		} else if !containsCSVValue(group.ProviderID, item.ProviderID) {
			group.ProviderID += ", " + item.ProviderID
		}
		if group.Model == "" {
			group.Model = item.Model
		} else if !containsCSVValue(group.Model, item.Model) {
			group.Model += ", " + item.Model
		}
		if group.PairsText != "" {
			group.PairsText += "\n"
		}
		group.PairsText += item.ProviderID + "|" + item.Model
		group.Count++
	}
	return groups
}

func containsCSVValue(csv string, value string) bool {
	for _, part := range strings.Split(csv, ",") {
		if strings.TrimSpace(part) == value {
			return true
		}
	}
	return false
}

func formatModelPriceRules(items []store.ModelPriceRule) string {
	lines := []string{}
	for _, item := range items {
		lines = append(lines, strings.Join([]string{
			item.ProviderID,
			item.Model,
			strconv.FormatFloat(item.InputPer1M, 'f', -1, 64),
			strconv.FormatFloat(item.OutputPer1M, 'f', -1, 64),
			strconv.FormatFloat(item.CachedInputPer1M, 'f', -1, 64),
			strconv.FormatFloat(item.ReasoningPer1M, 'f', -1, 64),
			strconv.Itoa(item.ContextLength),
			strconv.Itoa(item.RPM),
			strconv.Itoa(item.TPM),
			strconv.Itoa(item.DailyRequests),
			strconv.Itoa(item.DailyTokens),
		}, "|"))
	}
	return strings.Join(lines, "\n")
}

func formatPromptRouters(items []store.PromptRouter) string {
	raw, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return "[]"
	}
	return string(raw)
}

func parseFusions(raw string) []store.Fusion {
	items := []store.Fusion{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &items); err != nil {
		return nil
	}
	return store.NormalizeFusions(items)
}

func formatFusions(items []store.Fusion) string {
	raw, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return "[]"
	}
	return string(raw)
}

func parseCombos(raw string) []store.Combo {
	out := []store.Combo{}
	for _, line := range strings.Split(raw, "\n") {
		parts := strings.Split(line, "|")
		if len(parts) < 2 {
			continue
		}
		combo := store.Combo{Name: strings.TrimSpace(parts[0]), Models: splitModels(parts[1]), Enabled: true}
		if len(parts) > 2 {
			combo.Strategy = strings.TrimSpace(parts[2])
		}
		if len(parts) > 3 {
			combo.StickyLimit, _ = strconv.Atoi(strings.TrimSpace(parts[3]))
		}
		if len(parts) > 4 {
			combo.ContextLength, _ = strconv.Atoi(strings.TrimSpace(parts[4]))
		}
		if len(parts) > 5 {
			combo.Enabled, _ = strconv.ParseBool(strings.TrimSpace(parts[5]))
		}
		if len(parts) > 6 {
			combo.Description = strings.TrimSpace(parts[6])
		}
		out = append(out, combo)
	}
	return store.NormalizeCombos(out)
}

func formatCombos(items []store.Combo) string {
	lines := []string{}
	for _, item := range items {
		lines = append(lines, strings.Join([]string{
			item.Name,
			strings.Join(item.Models, ","),
			item.Strategy,
			strconv.Itoa(item.StickyLimit),
			strconv.Itoa(item.ContextLength),
			strconv.FormatBool(item.Enabled),
			item.Description,
		}, "|"))
	}
	return strings.Join(lines, "\n")
}

func comboModelOptions(providers []store.Provider) []comboModelOption {
	out := []comboModelOption{}
	seen := map[string]bool{}
	for _, provider := range providers {
		if !provider.Enabled {
			continue
		}
		providerLabel := strings.TrimSpace(provider.Name)
		if providerLabel == "" {
			providerLabel = provider.ID
		}
		for _, model := range provider.Models {
			model = strings.TrimSpace(model)
			if model == "" {
				continue
			}
			value := provider.ID + "/" + model
			if seen[value] {
				continue
			}
			seen[value] = true
			out = append(out, comboModelOption{ProviderID: provider.ID, Provider: providerLabel, Model: model, Value: value})
		}
	}
	return out
}

func buildProviderGroups(providers []store.Provider, proxies []store.Proxy, logs []store.RequestLog, cooldowns []observe.CooldownStatus, settings store.Settings, now time.Time, usageByProvider map[string]UsageCounter, usageByModel map[string]UsageCounter, usagePeriodLabel string) ([]providerGroup, providerSummary) {
	proxyByID := map[string]store.Proxy{}
	for _, proxy := range proxies {
		proxyByID[proxy.ID] = proxy
	}
	providerCreatedAt := map[string]time.Time{}
	for _, provider := range providers {
		providerCreatedAt[provider.ID] = provider.CreatedAt
	}
	logStats := map[string]providerLogStats{}
	for _, log := range logs {
		if createdAt := providerCreatedAt[log.ProviderID]; !createdAt.IsZero() && log.Timestamp.Before(createdAt) {
			continue
		}
		stats := logStats[log.ProviderID]
		stats.RequestCount++
		if isSuccessStatus(log.Status) {
			stats.SuccessCount++
		} else {
			stats.ErrorCount++
		}
		if stats.LastSeen.IsZero() || log.Timestamp.After(stats.LastSeen) {
			stats.LastSeen = log.Timestamp
			stats.LastStatus = log.Status
			stats.LastError = log.Error
		}
		logStats[log.ProviderID] = stats
	}

	cooldownByProvider := map[string]observe.CooldownStatus{}
	for _, cooldown := range cooldowns {
		cooldownByProvider[cooldown.ProviderID] = cooldown
	}

	groups := []providerGroup{
		{Key: "oauth", Title: "OAuth Providers", Subtitle: "Tài khoản đăng nhập qua OAuth/local token như Codex CLI."},
		{Key: "apikey", Title: "API Key Providers", Subtitle: "Provider OpenAI-compatible hoặc endpoint tùy chỉnh dùng API key."},
		{Key: "other", Title: "Other Providers", Subtitle: "Provider khác hoặc cấu hình thử nghiệm."},
	}
	summary := providerSummary{Total: len(providers)}
	for _, provider := range providers {
		card := buildProviderCard(provider, proxyByID, logStats[provider.ID], cooldownByProvider[provider.ID], settings, now, usageByProvider[provider.ID], modelUsageForProvider(provider.ID, provider.Models, usageByModel), usagePeriodLabel)
		if provider.Enabled {
			summary.Enabled++
		} else {
			summary.Disabled++
		}
		if card.HasCredential {
			summary.WithCredentials++
		}
		if card.HasCredential && (card.AuthLabel == "OAuth" || card.AuthLabel == "Bearer") {
			summary.OAuthConnected++
		}
		if card.Cooldown {
			summary.InCooldown++
		}
		switch providerGroupKey(provider) {
		case "oauth":
			groups[0].Cards = append(groups[0].Cards, card)
		case "apikey":
			groups[1].Cards = append(groups[1].Cards, card)
		default:
			groups[2].Cards = append(groups[2].Cards, card)
		}
	}
	out := []providerGroup{}
	for _, group := range groups {
		if group.Key == "oauth" || group.Key == "apikey" || len(group.Cards) > 0 {
			out = append(out, group)
		}
	}
	return out, summary
}

func firstProviderModels(models []providerModelUsage, limit int) []providerModelUsage {
	if limit <= 0 || len(models) <= limit {
		return append([]providerModelUsage(nil), models...)
	}
	return append([]providerModelUsage(nil), models[:limit]...)
}

func hiddenProviderModels(models []providerModelUsage, limit int) []providerModelUsage {
	if limit <= 0 || len(models) <= limit {
		return nil
	}
	return append([]providerModelUsage(nil), models[limit:]...)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func selectedProviderCard(groups []providerGroup, id string) *providerCard {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	for _, group := range groups {
		for _, card := range group.Cards {
			if card.ID == id {
				out := card
				return &out
			}
		}
	}
	return nil
}

type providerLogStats struct {
	RequestCount int
	SuccessCount int
	ErrorCount   int
	LastStatus   string
	LastError    string
	LastSeen     time.Time
}

func providerActiveKeyCount(provider store.Provider) int {
	cnt := 0
	for _, k := range provider.Keys {
		if k.Enabled && k.Key != "" {
			cnt++
		}
	}
	return cnt
}

func maskedProviderKeys(keys []store.ProviderKey) []store.ProviderKey {
	out := make([]store.ProviderKey, len(keys))
	for i, key := range keys {
		key.Key = maskSecret(key.Key)
		out[i] = key
	}
	return out
}

func buildProviderCard(provider store.Provider, proxyByID map[string]store.Proxy, stats providerLogStats, cooldown observe.CooldownStatus, settings store.Settings, now time.Time, usage UsageCounter, modelUsage []providerModelUsage, usagePeriodLabel string) providerCard {
	hasAPIKey := strings.TrimSpace(provider.APIKey) != "" || providerActiveKeyCount(provider) > 0
	hasAccessToken := strings.TrimSpace(provider.AccessToken) != ""
	hasRefreshToken := strings.TrimSpace(provider.RefreshToken) != ""
	hasCredential := hasAPIKey || hasAccessToken || hasRefreshToken
	proxyID := strings.TrimSpace(provider.ProxyID)
	proxyName := ""
	proxyEnabled := false
	if proxyID != "" {
		if px, ok := proxyByID[proxyID]; ok {
			proxyName = px.Name
			proxyEnabled = px.Enabled
		}
	}
	card := providerCard{
		ID:               provider.ID,
		Type:             provider.Type,
		Name:             provider.Name,
		BaseURL:          provider.BaseURL,
		IconText:         providerIconText(provider),
		ProxyURL:         provider.ProxyURL,
		ProxyID:          proxyID,
		ProxyName:        proxyName,
		ProxyEnabled:     proxyEnabled,
		ProxyLabel:       providerProxyLabel(provider.ProxyURL),
		ProxyClass:       providerProxyClass(provider.ProxyURL),
		AuthLabel:        providerAuthLabel(provider, hasAPIKey, hasAccessToken, hasRefreshToken),
		AuthClass:        providerAuthClass(provider, hasAPIKey, hasAccessToken, hasRefreshToken),
		Enabled:          provider.Enabled,
		IsDefault:        provider.ID == settings.DefaultProvider || provider.ID == settings.DefaultCodexID,
		HasCredential:    hasCredential,
		SecretLabel:      providerSecretLabel(hasAPIKey, hasAccessToken, hasRefreshToken),
		SecretClass:      providerSecretClass(hasCredential),
		SecretTitle:      providerSecretTitle(provider, hasAPIKey, hasAccessToken, hasRefreshToken),
		KeyCount:         len(provider.Keys),
		KeyStrategy:      provider.KeyStrategy,
		StickyLimit:      provider.StickyLimit,
		Keys:             maskedProviderKeys(provider.Keys),
		Models:           append([]string(nil), provider.Models...),
		VisibleModels:    firstProviderModels(modelUsage, 10),
		HiddenModels:     hiddenProviderModels(modelUsage, 10),
		HiddenModelCount: maxInt(len(modelUsage)-10, 0),
		ModelCount:       len(provider.Models),
		RequestCount:     stats.RequestCount,
		SuccessCount:     stats.SuccessCount,
		ErrorCount:       stats.ErrorCount,
		UsageTokens:      usage.TotalTokens,
		UsageRequests:    usage.Requests,
		UsagePeriodLabel: usagePeriodLabel,
		LastStatus:       stats.LastStatus,
		LastError:        stats.LastError,
		LastSeen:         formatRelativeTime(stats.LastSeen, now),
		CapabilityBadges: providerCapabilityBadges(provider, settings),
		StatusLabel:      "No credentials",
		StatusClass:      "warn",
	}
	if !provider.Enabled {
		card.StatusLabel = "Disabled"
		card.StatusClass = "muted"
	} else if !cooldown.Until.IsZero() {
		card.Cooldown = true
		card.CooldownRemaining = formatDuration(time.Duration(cooldown.RemainingMS) * time.Millisecond)
		card.CooldownReason = cooldown.Reason
		card.StatusLabel = "Cooldown"
		card.StatusClass = "warn"
	} else if stats.ErrorCount > 0 && stats.LastStatus != "" && !isSuccessStatus(stats.LastStatus) {
		card.StatusLabel = "Error " + stats.LastStatus
		card.StatusClass = "error"
	} else if hasCredential {
		card.StatusLabel = "Connected"
		card.StatusClass = "success"
	}
	if provider.ID == settings.DefaultProvider {
		card.DefaultLabel = "Default OpenAI"
		card.DefaultTitle = "Provider mặc định cho /v1/chat/completions"
	}
	if provider.ID == settings.DefaultCodexID {
		card.DefaultLabel = "Default Codex"
		card.DefaultTitle = "Provider mặc định cho /v1/responses và /codex/responses"
	}
	if card.IsDefault && stats.RequestCount >= 5 && stats.LastSeen.After(provider.UpdatedAt) && stats.ErrorCount*100/stats.RequestCount > 50 {
		card.DefaultWarning = "Provider mặc định đang lỗi trên 50% request gần đây — hãy kiểm tra quota, token hoặc chuyển default provider."
	}
	return card
}

func providerGroupKey(provider store.Provider) string {
	if provider.Type == store.ProviderCodex || provider.Type == store.ProviderAntigravity {
		return "oauth"
	}
	if provider.Type == store.ProviderOpenAICompatible {
		return "apikey"
	}
	return "other"
}

func providerCapabilityBadges(provider store.Provider, settings store.Settings) []providerCapabilityBadge {
	badges := []providerCapabilityBadge{}
	if provider.Type == store.ProviderCodex {
		badges = append(badges,
			providerCapabilityBadge{Label: "Codex", Class: "info", Title: "Codex/Responses-compatible provider"},
			providerCapabilityBadge{Label: "Responses", Class: "info", Title: "Supports Responses-style requests"},
			providerCapabilityBadge{Label: "OAuth", Class: "oauth", Title: "OAuth/local token provider"},
		)
	} else if provider.Type == store.ProviderAntigravity {
		badges = append(badges,
			providerCapabilityBadge{Label: "Antigravity", Class: "info", Title: "Google Antigravity / Cloud Code Assist provider"},
			providerCapabilityBadge{Label: "OAuth", Class: "oauth", Title: "Google OAuth access/refresh token provider"},
			providerCapabilityBadge{Label: "Risk", Class: "warn", Title: "Marked deprecated/risky in the 9router reference implementation"},
		)
	} else {
		badges = append(badges, providerCapabilityBadge{Label: "Chat", Class: "info", Title: "OpenAI-compatible chat provider"})
		if strings.Contains(strings.ToLower(provider.BaseURL), "responses") {
			badges = append(badges, providerCapabilityBadge{Label: "Responses", Class: "info", Title: "Endpoint hints at Responses compatibility"})
		}
	}
	if strings.TrimSpace(provider.APIKey) != "" {
		badges = append(badges, providerCapabilityBadge{Label: "API Key", Class: "apikey", Title: "Uses API key authentication"})
	}
	if strings.TrimSpace(provider.AccessToken) != "" || strings.TrimSpace(provider.RefreshToken) != "" {
		badges = append(badges, providerCapabilityBadge{Label: "Bearer", Class: "oauth", Title: "Uses bearer/access token authentication"})
	}
	if strings.TrimSpace(provider.ProxyURL) != "" {
		badges = append(badges, providerCapabilityBadge{Label: "Proxy", Class: "info", Title: "Provider traffic uses a configured proxy"})
	}
	if provider.ID == settings.DefaultProvider {
		badges = append(badges, providerCapabilityBadge{Label: "Default OpenAI", Class: "info", Title: "Default provider for OpenAI-compatible routes"})
	}
	if provider.ID == settings.DefaultCodexID {
		badges = append(badges, providerCapabilityBadge{Label: "Default Codex", Class: "info", Title: "Default provider for Codex/Responses routes"})
	}
	return badges
}

func providerAuthLabel(provider store.Provider, hasAPIKey bool, hasAccessToken bool, hasRefreshToken bool) string {
	if (provider.Type == store.ProviderCodex || provider.Type == store.ProviderAntigravity) && (hasAccessToken || hasRefreshToken) {
		return "OAuth"
	}
	if hasAPIKey {
		return "API Key"
	}
	if hasAccessToken || hasRefreshToken {
		return "Bearer"
	}
	if provider.Type == store.ProviderCodex || provider.Type == store.ProviderAntigravity {
		return "OAuth"
	}
	return "API Key"
}

func providerAuthClass(provider store.Provider, hasAPIKey bool, hasAccessToken bool, hasRefreshToken bool) string {
	if hasAPIKey || hasAccessToken || hasRefreshToken {
		if provider.Type == store.ProviderCodex || provider.Type == store.ProviderAntigravity {
			return "oauth"
		}
		return "apikey"
	}
	return "muted"
}

func providerProxyLabel(proxyURL string) string {
	if strings.TrimSpace(proxyURL) == "" {
		return "Direct IP"
	}
	return "Proxy"
}

func providerProxyClass(proxyURL string) string {
	if strings.TrimSpace(proxyURL) == "" {
		return "muted"
	}
	return "info"
}

func providerSecretLabel(hasAPIKey bool, hasAccessToken bool, hasRefreshToken bool) string {
	parts := []string{}
	if hasAPIKey {
		parts = append(parts, "API key")
	}
	if hasAccessToken {
		parts = append(parts, "access token")
	}
	if hasRefreshToken {
		parts = append(parts, "refresh token")
	}
	if len(parts) == 0 {
		return "No secret"
	}
	return strings.Join(parts, " + ")
}

func providerSecretTitle(provider store.Provider, hasAPIKey bool, hasAccessToken bool, hasRefreshToken bool) string {
	parts := []string{}
	if hasAPIKey {
		parts = append(parts, "api_key (đã lưu)")
	}
	if hasAccessToken {
		if provider.Type == store.ProviderCodex {
			parts = append(parts, "access_token (OAuth, đã lưu)")
		} else {
			parts = append(parts, "access_token (đã lưu)")
		}
	}
	if hasRefreshToken {
		parts = append(parts, "refresh_token (đã lưu)")
	}
	if len(parts) == 0 {
		return "Chưa lưu credential"
	}
	return strings.Join(parts, " + ")
}

func providerSecretClass(hasCredential bool) string {
	if hasCredential {
		return "success"
	}
	return "warn"
}

func providerIconText(provider store.Provider) string {
	value := provider.ID
	if provider.Type == store.ProviderCodex {
		return "CX"
	}
	if provider.Type == store.ProviderAntigravity {
		return "AG"
	}
	if strings.Contains(provider.Type, "openai") {
		return "OA"
	}
	if strings.TrimSpace(provider.Name) != "" {
		value = provider.Name
	}
	letters := []rune{}
	for _, part := range strings.FieldsFunc(value, func(r rune) bool { return r == '-' || r == '_' || r == ' ' || r == '/' }) {
		if part != "" {
			letters = append(letters, []rune(strings.ToUpper(part))[0])
		}
		if len(letters) == 2 {
			break
		}
	}
	if len(letters) == 0 {
		return "AI"
	}
	return string(letters)
}

func isSuccessStatus(status string) bool {
	status = strings.TrimSpace(status)
	if len(status) == 3 && strings.HasPrefix(status, "2") {
		return true
	}
	return strings.EqualFold(status, "OK") || strings.EqualFold(status, "SUCCESS")
}

func formatRelativeTimeFromNow(t time.Time) string {
	return formatRelativeTime(t, time.Now())
}

func formatRelativeTime(t time.Time, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	dur := now.Sub(t)
	if dur < 0 {
		dur = 0
	}
	if dur < time.Minute {
		seconds := int(dur.Seconds())
		if seconds < 1 {
			seconds = 1
		}
		return strconv.Itoa(seconds) + "s"
	}
	if dur < time.Hour {
		return strconv.Itoa(int(dur.Minutes())) + "p"
	}
	if dur < 24*time.Hour {
		return strconv.Itoa(int(dur.Hours())) + "h"
	}
	return strconv.Itoa(int(dur.Hours()/24)) + "d"
}

func formatDuration(d time.Duration) string {
	if d <= 0 {
		return "0s"
	}
	if d < time.Minute {
		return strconv.Itoa(int(d.Seconds())) + "s"
	}
	if d < time.Hour {
		return strconv.Itoa(int(d.Minutes())) + "m " + strconv.Itoa(int(d.Seconds())%60) + "s"
	}
	return strconv.Itoa(int(d.Hours())) + "h " + strconv.Itoa(int(d.Minutes())%60) + "m"
}

func splitModels(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == '\n' || r == '\r' || r == '\t' })
	models := []string{}
	seen := map[string]bool{}
	for _, part := range parts {
		model := strings.TrimSpace(part)
		if model == "" || seen[model] {
			continue
		}
		seen[model] = true
		models = append(models, model)
	}
	return models
}

func providerUsagePeriodParam(r *http.Request) string {
	switch strings.ToLower(strings.TrimSpace(r.URL.Query().Get("usage_period"))) {
	case "day", "24h", "today":
		return "day"
	case "week", "7d":
		return "week"
	case "all", "all_time", "all-time":
		return "all"
	default:
		return "day"
	}
}

func providerUsagePeriodLabel(period string) string {
	switch period {
	case "week":
		return "7d"
	case "all":
		return "all time"
	default:
		return "24h"
	}
}

func filterProviderUsageLogs(logs []store.RequestLog, period string, now time.Time) []store.RequestLog {
	if period == "all" {
		return logs
	}
	window := 24 * time.Hour
	if period == "week" {
		window = 7 * 24 * time.Hour
	}
	cutoff := now.Add(-window)
	out := make([]store.RequestLog, 0, len(logs))
	for _, log := range logs {
		if log.Timestamp.IsZero() || !log.Timestamp.Before(cutoff) {
			out = append(out, log)
		}
	}
	return out
}

func summarizeProviderUsage(logs []store.RequestLog) (map[string]UsageCounter, map[string]UsageCounter) {
	byProvider := map[string]UsageCounter{}
	byModel := map[string]UsageCounter{}
	for _, log := range logs {
		counter := counterFromLog(log)
		addUsageCounterToMap(byProvider, log.ProviderID, counter)
		addUsageCounterToMap(byModel, log.ProviderID+"/"+log.Model, counter)
	}
	return byProvider, byModel
}

func modelUsageForProvider(providerID string, models []string, usageByModel map[string]UsageCounter) []providerModelUsage {
	out := make([]providerModelUsage, 0, len(models))
	for idx, model := range models {
		usage := usageByModel[strings.TrimSpace(providerID)+"/"+strings.TrimSpace(model)]
		out = append(out, providerModelUsage{Name: model, Tokens: usage.TotalTokens, Requests: usage.Requests, HasUsage: usage.TotalTokens > 0 || usage.Requests > 0, UsageRank: idx + 1})
	}
	return out
}

func summarizeUsage(logs []store.RequestLog) usageSummary {
	summary := usageSummary{
		ByProvider: map[string]UsageCounter{},
		ByModel:    map[string]UsageCounter{},
		ByEndpoint: map[string]UsageCounter{},
		ByAPIKey:   map[string]UsageCounter{},
	}
	for _, log := range logs {
		counter := counterFromLog(log)
		addUsageCounter(&summary.UsageCounter, counter)
		addUsageCounterToMap(summary.ByProvider, log.ProviderID, counter)
		addUsageCounterToMap(summary.ByModel, log.ProviderID+"/"+log.Model, counter)
		addUsageCounterToMap(summary.ByEndpoint, log.Endpoint, counter)

		apiKeyKey := log.APIKeyID
		if apiKeyKey == "" {
			if log.APIKeyPrefix != "" || log.APIKeySuffix != "" {
				apiKeyKey = log.APIKeyPrefix + "..." + log.APIKeySuffix
			} else {
				apiKeyKey = "no_key"
			}
		}
		addUsageCounterToMap(summary.ByAPIKey, apiKeyKey, counter)
	}
	return summary
}

func counterFromLog(log store.RequestLog) UsageCounter {
	estimated := 0
	if log.EstimatedTokens {
		estimated = 1
	}
	totalTokens := log.TotalTokens
	if totalTokens <= 0 {
		totalTokens = log.PromptTokens + log.CompletionTokens
	}
	return UsageCounter{
		Requests:           1,
		PromptTokens:       log.PromptTokens,
		CompletionTokens:   log.CompletionTokens,
		TotalTokens:        totalTokens,
		CachedTokens:       log.CachedTokens,
		ReasoningTokens:    log.ReasoningTokens,
		UpstreamSaved:      log.UpstreamTokensSaved,
		DebugSaved:         log.EstimatedTokensSaved,
		OptimizeDurationMS: log.OptimizeDurationMS,
		ProviderDurationMS: log.ProviderDurationMS,
		DebugLogDurationMS: log.DebugLogDurationMS,
		Estimated:          estimated,
		CostUSD:            log.CostUSD,
	}
}

func addUsageCounterToMap(target map[string]UsageCounter, key string, value UsageCounter) {
	key = strings.TrimSpace(key)
	if key == "" || key == "/" {
		key = "unknown"
	}
	current := target[key]
	addUsageCounter(&current, value)
	target[key] = current
}

func addUsageCounter(target *UsageCounter, value UsageCounter) {
	target.Requests += value.Requests
	target.PromptTokens += value.PromptTokens
	target.CompletionTokens += value.CompletionTokens
	target.TotalTokens += value.TotalTokens
	target.CachedTokens += value.CachedTokens
	target.ReasoningTokens += value.ReasoningTokens
	target.UpstreamSaved += value.UpstreamSaved
	target.DebugSaved += value.DebugSaved
	target.OptimizeDurationMS += value.OptimizeDurationMS
	target.ProviderDurationMS += value.ProviderDurationMS
	target.DebugLogDurationMS += value.DebugLogDurationMS
	target.Estimated += value.Estimated
	target.CostUSD += value.CostUSD
}

func filterLogsForRange(logs []store.RequestLog, rangeKey string, now time.Time) []store.RequestLog {
	rangeKey = normalizeUsageRange(rangeKey)
	now = now.UTC()

	var start time.Time
	switch rangeKey {
	case "today":
		start = now.Truncate(24 * time.Hour)
	case "7d":
		start = now.Truncate(24 * time.Hour).AddDate(0, 0, -6)
	case "30d":
		start = now.Truncate(24 * time.Hour).AddDate(0, 0, -29)
	default: // 24h
		start = now.Add(-23 * time.Hour).Truncate(time.Hour)
	}

	out := make([]store.RequestLog, 0)
	for _, log := range logs {
		if !log.Timestamp.UTC().Before(start) {
			out = append(out, log)
		}
	}
	return out
}

type usageTableAggregate struct {
	Requests         int
	LastUsed         time.Time
	InputCost        float64
	OutputCost       float64
	TotalCost        float64
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

func buildUsageTableData(logs []store.RequestLog, settings store.Settings, providers []store.Provider, now time.Time) usageTableData {
	providerByID := map[string]store.Provider{}
	for _, p := range providers {
		providerByID[p.ID] = p
	}

	byModel := map[string]map[string]*usageTableAggregate{}
	byAccount := map[string]map[string]*usageTableAggregate{}
	byAPIKey := map[string]map[string]*usageTableAggregate{}
	byEndpoint := map[string]map[string]*usageTableAggregate{}

	getAggregate := func(target map[string]map[string]*usageTableAggregate, parent string, child string) *usageTableAggregate {
		if target[parent] == nil {
			target[parent] = map[string]*usageTableAggregate{}
		}
		if target[parent][child] == nil {
			target[parent][child] = &usageTableAggregate{}
		}
		return target[parent][child]
	}

	addLog := func(agg *usageTableAggregate, log store.RequestLog, inputCost float64, outputCost float64) {
		agg.Requests++
		if log.Timestamp.After(agg.LastUsed) {
			agg.LastUsed = log.Timestamp
		}
		agg.InputCost += inputCost
		agg.OutputCost += outputCost
		agg.TotalCost += log.CostUSD
		agg.PromptTokens += log.PromptTokens
		agg.CompletionTokens += log.CompletionTokens
		totalTokens := log.TotalTokens
		if totalTokens <= 0 {
			totalTokens = log.PromptTokens + log.CompletionTokens
		}
		agg.TotalTokens += totalTokens
	}

	for _, log := range logs {
		inputCost, outputCost := estimateLogInputOutputCost(log, settings, providerByID[log.ProviderID])

		modelName := strings.TrimSpace(log.Model)
		if modelName == "" {
			modelName = "unknown"
		}
		providerName := strings.TrimSpace(log.ProviderID)
		if providerName == "" {
			providerName = "—"
		}
		accountName := providerName
		apiKeyName := usageAPIKeyLabel(log)
		endpointName := strings.TrimSpace(log.Endpoint)
		if endpointName == "" {
			endpointName = "unknown"
		}

		addLog(getAggregate(byModel, modelName, providerName), log, inputCost, outputCost)
		addLog(getAggregate(byAccount, accountName, modelName), log, inputCost, outputCost)
		addLog(getAggregate(byAPIKey, apiKeyName, modelName), log, inputCost, outputCost)
		addLog(getAggregate(byEndpoint, endpointName, modelName), log, inputCost, outputCost)
	}

	return usageTableData{
		ByModel:    buildUsageTableRows(byModel, now, "model"),
		ByAccount:  buildUsageTableRows(byAccount, now, "account"),
		ByAPIKey:   buildUsageTableRows(byAPIKey, now, "api_key"),
		ByEndpoint: buildUsageTableRows(byEndpoint, now, "endpoint"),
	}
}

func buildUsageTableRows(groups map[string]map[string]*usageTableAggregate, now time.Time, mode string) []usageTableRow {
	rows := make([]usageTableRow, 0, len(groups))
	for parentName, childrenMap := range groups {
		parent := usageTableRow{Name: parentName, Provider: "—"}
		children := make([]usageTableRow, 0, len(childrenMap))
		var parentLastUsed time.Time
		for childName, agg := range childrenMap {
			child := usageTableRow{
				Name:             childName,
				Requests:         agg.Requests,
				InputCost:        agg.InputCost,
				OutputCost:       agg.OutputCost,
				TotalCost:        agg.TotalCost,
				PromptTokens:     agg.PromptTokens,
				CompletionTokens: agg.CompletionTokens,
				TotalTokens:      agg.TotalTokens,
			}
			switch mode {
			case "model":
				child.Provider = childName
			case "account":
				child.Provider = parentName
			default:
				child.Provider = "—"
			}
			if !agg.LastUsed.IsZero() {
				child.LastUsedSecsAgo = int(maxDuration(0, now.Sub(agg.LastUsed)).Seconds())
				child.LastUsedLabel = usageRelativeTimeLabel(now, agg.LastUsed)
			}
			children = append(children, child)
			if agg.LastUsed.After(parentLastUsed) {
				parentLastUsed = agg.LastUsed
			}
			parent.Requests += agg.Requests
			parent.InputCost += agg.InputCost
			parent.OutputCost += agg.OutputCost
			parent.TotalCost += agg.TotalCost
			parent.PromptTokens += agg.PromptTokens
			parent.CompletionTokens += agg.CompletionTokens
			parent.TotalTokens += agg.TotalTokens
		}
		sort.Slice(children, func(i, j int) bool {
			if children[i].TotalCost == children[j].TotalCost {
				if children[i].Requests == children[j].Requests {
					return children[i].Name < children[j].Name
				}
				return children[i].Requests > children[j].Requests
			}
			return children[i].TotalCost > children[j].TotalCost
		})
		if !parentLastUsed.IsZero() {
			parent.LastUsedSecsAgo = int(maxDuration(0, now.Sub(parentLastUsed)).Seconds())
			parent.LastUsedLabel = usageRelativeTimeLabel(now, parentLastUsed)
		}
		parent.Children = children
		rows = append(rows, parent)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].TotalCost == rows[j].TotalCost {
			if rows[i].Requests == rows[j].Requests {
				return rows[i].Name < rows[j].Name
			}
			return rows[i].Requests > rows[j].Requests
		}
		return rows[i].TotalCost > rows[j].TotalCost
	})
	return rows
}

func usageAPIKeyLabel(log store.RequestLog) string {
	if key := strings.TrimSpace(log.APIKeyID); key != "" {
		return key
	}
	masked := strings.TrimSpace(log.APIKeyMasked)
	if masked != "" {
		return masked
	}
	if strings.TrimSpace(log.APIKeyPrefix) != "" || strings.TrimSpace(log.APIKeySuffix) != "" {
		return strings.TrimSpace(log.APIKeyPrefix) + "..." + strings.TrimSpace(log.APIKeySuffix)
	}
	return "anonymous"
}

func usageRelativeTimeLabel(now time.Time, t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	d := maxDuration(0, now.Sub(t))
	if d < time.Minute {
		return "Just now"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}

func estimateLogInputOutputCost(log store.RequestLog, settings store.Settings, providerCfg store.Provider) (float64, float64) {
	if log.CostUSD <= 0 {
		return 0, 0
	}
	inputPrice, cachedPrice, outputPrice, reasoningPrice := usagePricingForLog(settings, providerCfg, log.Model)
	if cachedPrice <= 0 {
		cachedPrice = inputPrice
	}
	if reasoningPrice <= 0 {
		reasoningPrice = outputPrice
	}
	cached := log.CachedTokens
	if cached > log.PromptTokens {
		cached = log.PromptTokens
	}
	nonCachedInput := maxInt(0, log.PromptTokens-cached)
	inputWeight := float64(nonCachedInput)*(inputPrice/1_000_000) + float64(cached)*(cachedPrice/1_000_000)
	outputWeight := float64(log.CompletionTokens)*(outputPrice/1_000_000) + float64(log.ReasoningTokens)*(reasoningPrice/1_000_000)
	totalWeight := inputWeight + outputWeight
	if totalWeight <= 0 {
		return log.CostUSD, 0
	}
	inputCost := log.CostUSD * (inputWeight / totalWeight)
	outputCost := log.CostUSD - inputCost
	if outputCost < 0 {
		outputCost = 0
	}
	return inputCost, outputCost
}

func usagePricingForLog(settings store.Settings, providerCfg store.Provider, model string) (float64, float64, float64, float64) {
	providerID := strings.TrimSpace(providerCfg.ID)
	model = strings.TrimSpace(model)
	for _, rule := range settings.ModelPrices {
		if strings.TrimSpace(rule.ProviderID) == providerID && strings.TrimSpace(rule.Model) == model {
			return rule.InputPer1M, rule.CachedInputPer1M, rule.OutputPer1M, rule.ReasoningPer1M
		}
	}
	return 0, 0, 0, 0
}

func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}

func (h *Handlers) databasePath() (string, error) {
	if h.cfg.StoreBackend == "sqlite" {
		return filepath.Join(h.cfg.DataDir, "sample.sqlite"), nil
	}
	return filepath.Join(h.cfg.DataDir, "sample-db.json"), nil
}

func copyFile(src string, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func writeUploadedFile(path string, r io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	out, err := os.Create(path)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, io.LimitReader(r, 128<<20))
	return err
}

// adminSecurityActive reports whether passcode protection is configured.
func (h *Handlers) adminSecurityActive(settings store.Settings) bool {
	return settings.AdminSecurityEnabled && strings.TrimSpace(settings.AdminPasscode) != ""
}

// hasAdminSession verifies the signed session cookie without exposing the passcode.
func (h *Handlers) hasAdminSession(r *http.Request, settings store.Settings) bool {
	cookie, err := r.Cookie("admin_session")
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(h.adminSessionValue(settings))) == 1
}

// requireAdmin guards HTML pages; it redirects to the login page when unauthorized.
func (h *Handlers) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	settings, err := h.store.GetSettings()
	if err != nil || !h.adminSecurityActive(settings) {
		return true
	}
	if h.hasAdminSession(r, settings) {
		return true
	}
	http.Redirect(w, r, "/admin/login", http.StatusFound)
	return false
}

// requireAdminAPI guards JSON/management endpoints; it returns 401 JSON instead of
// redirecting so browser fetch()/CLI callers get a clear error. When passcode
// protection is not configured it still requires a loopback client unless the
// gateway API key requirement is the active control, mirroring requireAdmin's
// "open until configured" behavior for local-only use.
func (h *Handlers) requireAdminAPI(w http.ResponseWriter, r *http.Request) bool {
	settings, err := h.store.GetSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return false
	}
	if !h.adminSecurityActive(settings) {
		// No passcode configured: only allow same-machine callers so a remote
		// attacker cannot read/modify secrets on an unconfigured deployment.
		if !isLoopbackRequest(r) {
			writeError(w, http.StatusUnauthorized, "admin security is not configured; enable it in settings to manage this instance remotely")
			return false
		}
		return true
	}
	if h.hasAdminSession(r, settings) {
		return true
	}
	writeError(w, http.StatusUnauthorized, "admin authentication required")
	return false
}

// adminSessionValue derives an opaque, signed session token from the passcode.
// It never embeds the passcode itself, so a leaked cookie does not reveal it.
func (h *Handlers) adminSessionValue(settings store.Settings) string {
	mac := hmac.New(sha256.New, []byte(h.sessionSigningKey(settings)))
	mac.Write([]byte("admin-session-v1|" + settings.AdminPasscode))
	return "v1." + hex.EncodeToString(mac.Sum(nil))
}

// sessionSigningKey is a per-instance secret so session tokens cannot be forged
// from the passcode alone by an outside party.
func (h *Handlers) sessionSigningKey(settings store.Settings) string {
	return "vivurouter|" + settings.AdminPasscode + "|" + settings.LocalAPIKey
}

func (h *Handlers) setAdminSession(w http.ResponseWriter) {
	settings, _ := h.store.GetSettings()
	http.SetCookie(w, &http.Cookie{Name: "admin_session", Value: h.adminSessionValue(settings), Path: "/", MaxAge: 86400 * 7, HttpOnly: true, SameSite: http.SameSiteLaxMode})
}

// isLoopbackRequest reports whether the request originates from the local machine.
func isLoopbackRequest(r *http.Request) bool {
	host := r.RemoteAddr
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if host == "" {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return strings.EqualFold(host, "localhost")
}

func (h *Handlers) renderLogin(w http.ResponseWriter, errText string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	bundle := translationBundle("vi")
	_ = h.templates.ExecuteTemplate(w, "login.html", map[string]any{"Title": translate(bundle, "login.title"), "Lang": "vi", "T": bundle, "Error": errText})
}

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(buf)
}

func buildCostNote(logs []store.RequestLog, settings store.Settings) string {
	if len(logs) == 0 {
		return "chưa có request"
	}
	withTokens := 0
	unpriced := 0
	for _, log := range logs {
		if log.TotalTokens <= 0 {
			continue
		}
		withTokens++
		if log.CostUSD == 0 && !hasPricingForLog(log, settings) {
			unpriced++
		}
	}
	if withTokens == 0 {
		return "chưa có token để tính cost"
	}
	if unpriced > 0 {
		return strconv.Itoa(unpriced) + "/" + strconv.Itoa(withTokens) + " request có token nhưng chưa có pricing rule"
	}
	return "custom pricing aware"
}

func hasPricingForLog(log store.RequestLog, settings store.Settings) bool {
	model := strings.ToLower(strings.TrimSpace(log.Model))
	providerID := strings.TrimSpace(log.ProviderID)
	for _, rule := range settings.ModelPrices {
		if strings.TrimSpace(rule.ProviderID) != "" && strings.TrimSpace(rule.ProviderID) != providerID {
			continue
		}
		ruleModel := strings.ToLower(strings.TrimSpace(rule.Model))
		if ruleModel == "" || ruleModel == model || strings.TrimPrefix(model, providerID+"/") == ruleModel {
			return true
		}
	}
	return false
}

func templateSeq(start, end int) []int {
	if end < start {
		return nil
	}
	out := make([]int, 0, end-start+1)
	for i := start; i <= end; i++ {
		out = append(out, i)
	}
	return out
}

func templateJSON(value any) template.JS {
	raw, err := json.Marshal(value)
	if err != nil {
		return template.JS("{}")
	}
	return template.JS(raw)
}

func translate(bundle map[string]string, key string) string {
	if value, ok := bundle[key]; ok {
		return value
	}
	if value, ok := translations["vi"][key]; ok {
		return value
	}
	return key
}

func formatCost(cost float64) string {
	return strconv.FormatFloat(cost, 'f', 6, 64)
}

func formatTokens(tokens any) string {
	value, ok := numberToInt64(tokens)
	if !ok {
		return "0"
	}
	return formatIntWithCommas(value)
}

func formatTokensShort(tokens any) string {
	value, ok := numberToInt64(tokens)
	if !ok {
		return "0"
	}
	abs := value
	if abs < 0 {
		abs = -abs
	}
	if abs < 10_000_000 {
		return formatIntWithCommas(value)
	}
	floatValue := float64(value)
	unit := "M"
	divisor := 1_000_000.0
	if abs >= 1_000_000_000 {
		unit = "B"
		divisor = 1_000_000_000.0
	}
	formatted := strconv.FormatFloat(floatValue/divisor, 'f', 1, 64)
	formatted = strings.TrimSuffix(strings.TrimSuffix(formatted, "0"), ".")
	return formatted + unit
}

func numberToInt64(value any) (int64, bool) {
	switch v := value.(type) {
	case int:
		return int64(v), true
	case int8:
		return int64(v), true
	case int16:
		return int64(v), true
	case int32:
		return int64(v), true
	case int64:
		return v, true
	case uint:
		return int64(v), true
	case uint8:
		return int64(v), true
	case uint16:
		return int64(v), true
	case uint32:
		return int64(v), true
	case uint64:
		if v > uint64(^uint(0)>>1) {
			return 0, false
		}
		return int64(v), true
	default:
		return 0, false
	}
}

func formatIntWithCommas(value int64) string {
	sign := ""
	if value < 0 {
		sign = "-"
		value = -value
	}
	raw := strconv.FormatInt(value, 10)
	if len(raw) <= 3 {
		return sign + raw
	}
	groups := []string{}
	for len(raw) > 3 {
		groups = append([]string{raw[len(raw)-3:]}, groups...)
		raw = raw[:len(raw)-3]
	}
	if raw != "" {
		groups = append([]string{raw}, groups...)
	}
	return sign + strings.Join(groups, ",")
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]any{"message": message}})
}
