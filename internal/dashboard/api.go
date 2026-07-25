package dashboard

import (
	"encoding/csv"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/local/vivurouter-go/internal/provider"
	"github.com/local/vivurouter-go/internal/rtkbridge"
	"github.com/local/vivurouter-go/internal/store"
)

func (h *Handlers) GuardrailsExportAPI(w http.ResponseWriter, r *http.Request) {
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
	data, err := json.MarshalIndent(map[string]any{"guardrails": store.NormalizeGuardrails(settings.Guardrails)}, "", "  ")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=guardrails.json")
	_, _ = w.Write(append(data, '\n'))
}

func (h *Handlers) GuardrailsImportAPI(w http.ResponseWriter, r *http.Request) {
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
	incoming, err := decodeGuardrailsImport(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	incoming = store.NormalizeGuardrails(incoming)
	if len(incoming) == 0 {
		writeError(w, http.StatusBadRequest, "no valid guardrails found")
		return
	}
	settings, err := h.store.GetSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	byName := map[string]store.Guardrail{}
	order := []string{}
	for _, item := range settings.Guardrails {
		if _, ok := byName[item.Name]; !ok {
			order = append(order, item.Name)
		}
		byName[item.Name] = item
	}
	for _, item := range incoming {
		if _, ok := byName[item.Name]; !ok {
			order = append(order, item.Name)
		}
		byName[item.Name] = item
	}
	merged := make([]store.Guardrail, 0, len(order))
	for _, name := range order {
		merged = append(merged, byName[name])
	}
	settings.Guardrails = store.NormalizeGuardrails(merged)
	if err := h.store.SaveSettings(settings); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"imported": len(incoming), "total": len(settings.Guardrails)})
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
	if r.FormValue("confirm") != "RESTORE" {
		writeError(w, http.StatusBadRequest, "type RESTORE to confirm")
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
	if confirm := r.FormValue("confirm"); confirm != "DELETE" && confirm != "DELETE ALL DATA" {
		writeError(w, http.StatusBadRequest, "type DELETE ALL DATA to reset all data")
		return
	}
	if path, err := h.databasePath(); err == nil {
		backup := path + ".before-reset-" + time.Now().Format("20060102150405") + ".bak"
		_ = copyFile(path, backup)
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

func (h *Handlers) ProviderExportAPI(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing provider id")
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
	payload := map[string]any{
		"kind":        "vivurouter.provider",
		"version":     1,
		"exported_at": time.Now().UTC(),
		"provider":    provider,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	filename := "provider-" + safeDownloadName(provider.ID) + ".json"
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename="+strconv.Quote(filename))
	_, _ = w.Write(append(data, '\n'))
}

func (h *Handlers) ProviderImportAPI(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var body []byte
	if strings.Contains(r.Header.Get("Content-Type"), "multipart/form-data") {
		file, _, err := r.FormFile("provider_file")
		if err != nil {
			writeError(w, http.StatusBadRequest, "missing provider_file")
			return
		}
		defer file.Close()
		body, err = io.ReadAll(io.LimitReader(file, 2<<20))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid provider file")
			return
		}
	} else {
		var err error
		body, err = io.ReadAll(http.MaxBytesReader(w, r.Body, 2<<20))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON payload")
			return
		}
	}
	provider, err := decodeProviderImport(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	provider = store.NormalizeProvider(provider)
	if provider.ID == "" {
		writeError(w, http.StatusBadRequest, "provider id is required")
		return
	}
	if current, found, err := h.store.GetProvider(provider.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	} else if found {
		mergeProviderSecrets(&provider, current)
		if provider.CreatedAt.IsZero() {
			provider.CreatedAt = current.CreatedAt
		}
	}
	if err := h.store.UpsertProvider(provider); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if strings.Contains(r.Header.Get("Accept"), "application/json") {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "provider_id": provider.ID})
		return
	}
	http.Redirect(w, r, "/providers/"+url.PathEscape(provider.ID)+"?saved=1", http.StatusSeeOther)
}

func decodeProviderImport(body []byte) (store.Provider, error) {
	var wrapped struct {
		Kind     string         `json:"kind"`
		Provider store.Provider `json:"provider"`
	}
	if err := json.Unmarshal(body, &wrapped); err == nil && wrapped.Provider.ID != "" {
		return wrapped.Provider, nil
	}
	var provider store.Provider
	if err := json.Unmarshal(body, &provider); err != nil {
		return store.Provider{}, err
	}
	return provider, nil
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

func (h *Handlers) ProviderAccountsAPI(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdminAPI(w, r) {
		return
	}
	providerID := strings.TrimSpace(r.URL.Query().Get("provider_id"))
	if providerID == "" {
		writeError(w, http.StatusBadRequest, "missing provider_id")
		return
	}
	if _, found, err := h.store.GetProvider(providerID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	} else if !found {
		writeError(w, http.StatusNotFound, "provider not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		accounts, err := h.store.ListProviderAccounts(providerID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		for i := range accounts {
			accounts[i] = sanitizeProviderAccount(accounts[i])
		}
		writeJSON(w, http.StatusOK, accounts)
	case http.MethodPost, http.MethodPut:
		var account store.ProviderAccount
		if err := json.NewDecoder(r.Body).Decode(&account); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if account.ProviderID != "" && account.ProviderID != providerID {
			writeError(w, http.StatusBadRequest, "provider_id cannot be changed")
			return
		}
		account.ProviderID = providerID
		providerConfig, _, _ := h.store.GetProvider(providerID)
		if math.IsNaN(account.QuotaLimitPercent) || math.IsInf(account.QuotaLimitPercent, 0) || account.QuotaLimitPercent < 0 || account.QuotaLimitPercent > 100 {
			writeError(w, http.StatusBadRequest, "quota_limit_percent must be between 0 and 100")
			return
		}
		if account.QuotaLimitPercent > 0 && providerConfig.Type != store.ProviderCodex {
			writeError(w, http.StatusBadRequest, "quota limits are only supported for Codex accounts")
			return
		}
		if account.ID != "" {
			if current, found, err := h.store.GetProviderAccount(account.ID); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			} else if found {
				if current.ProviderID != providerID {
					writeError(w, http.StatusBadRequest, "account belongs to another provider")
					return
				}
				mergeProviderAccountSecrets(&account, current)
			}
		}
		if account.ProxyID != "" {
			proxy, found, err := h.store.GetProxy(account.ProxyID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			if !found || !proxy.Enabled {
				writeError(w, http.StatusBadRequest, "proxy is missing or disabled")
				return
			}
		}
		account.ProxyURL = provider.NormalizeProxyURL(account.ProxyURL)
		if account.ProxyID == "" && account.ProxyURL != "" {
			if _, err := provider.ParseProxyURL(account.ProxyURL); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
		}
		if err := h.store.UpsertProviderAccount(account); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if account.ID == "" {
			writeJSON(w, http.StatusCreated, map[string]any{"ok": true})
			return
		}
		stored, _, _ := h.store.GetProviderAccount(account.ID)
		writeJSON(w, http.StatusOK, sanitizeProviderAccount(stored))
	case http.MethodDelete:
		id := strings.TrimSpace(r.URL.Query().Get("id"))
		if id == "" {
			writeError(w, http.StatusBadRequest, "missing id")
			return
		}
		account, found, err := h.store.GetProviderAccount(id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !found || account.ProviderID != providerID {
			writeError(w, http.StatusNotFound, "account not found")
			return
		}
		if err := h.store.DeleteProviderAccount(id); err != nil {
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

func (h *Handlers) InvoicesAPI(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := h.store.ListInvoices()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"invoices": items})
	case http.MethodPost:
		if err := r.ParseForm(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid form")
			return
		}
		action := strings.ToLower(strings.TrimSpace(r.FormValue("action")))
		id := strings.TrimSpace(r.FormValue("id"))
		if action == "delete" {
			if id == "" {
				writeError(w, http.StatusBadRequest, "missing invoice id")
				return
			}
			if err := h.store.DeleteInvoice(id); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			http.Redirect(w, r, "/invoices?saved=1", http.StatusSeeOther)
			return
		}
		invoice := store.Invoice{
			ID:         id,
			ProviderID: strings.TrimSpace(r.FormValue("provider_id")),
			Vendor:     strings.TrimSpace(r.FormValue("vendor")),
			InvoiceNo:  strings.TrimSpace(r.FormValue("invoice_no")),
			Status:     strings.TrimSpace(r.FormValue("status")),
			IssueDate:  parseDateInput(r.FormValue("issue_date")),
			DueDate:    parseDateInput(r.FormValue("due_date")),
			PaidDate:   parseDateInput(r.FormValue("paid_date")),
			Currency:   strings.TrimSpace(r.FormValue("currency")),
			Amount:     parseNonNegativeFloatLimit(r.FormValue("amount")),
			Tax:        parseNonNegativeFloatLimit(r.FormValue("tax")),
			Total:      parseNonNegativeFloatLimit(r.FormValue("total")),
			Note:       strings.TrimSpace(r.FormValue("note")),
			Attachment: strings.TrimSpace(r.FormValue("attachment")),
		}
		if invoice.Vendor == "" && invoice.ProviderID == "" {
			writeError(w, http.StatusBadRequest, "vendor or provider is required")
			return
		}
		if err := h.store.UpsertInvoice(invoice); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		http.Redirect(w, r, "/invoices?saved=1", http.StatusSeeOther)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handlers) InvoicesExportAPI(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}
	items, err := h.store.ListInvoices()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename="+strconv.Quote("vivurouter-invoices.csv"))
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"id", "provider_id", "vendor", "invoice_no", "status", "issue_date", "due_date", "paid_date", "currency", "amount", "tax", "total", "note", "attachment"})
	for _, inv := range items {
		_ = cw.Write([]string{inv.ID, inv.ProviderID, inv.Vendor, inv.InvoiceNo, inv.Status, formatDateForInput(inv.IssueDate), formatDateForInput(inv.DueDate), formatDateForInput(inv.PaidDate), inv.Currency, strconv.FormatFloat(inv.Amount, 'f', 2, 64), strconv.FormatFloat(inv.Tax, 'f', 2, 64), strconv.FormatFloat(inv.Total, 'f', 2, 64), inv.Note, inv.Attachment})
	}
	cw.Flush()
}

func parseDateInput(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	if t, err := time.Parse("2006-01-02", value); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return t
	}
	return time.Time{}
}

func formatDateForInput(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format("2006-01-02")
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
