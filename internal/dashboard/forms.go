package dashboard

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/local/vivurouter-go/internal/provider"
	"github.com/local/vivurouter-go/internal/store"
)

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
	bindHost := strings.TrimSpace(r.FormValue("bind_host"))
	if bindHost != "127.0.0.1" && bindHost != "0.0.0.0" {
		writeError(w, http.StatusBadRequest, "bind host must be 127.0.0.1 or 0.0.0.0")
		return
	}
	bindPort, err := strconv.Atoi(strings.TrimSpace(r.FormValue("bind_port")))
	if err != nil || bindPort < 1 || bindPort > 65535 {
		writeError(w, http.StatusBadRequest, "bind port must be between 1 and 65535")
		return
	}
	settings.BindHost = bindHost
	settings.BindPort = strconv.Itoa(bindPort)
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
	settings.SaveRawResponse = r.FormValue("save_raw_response") == "on"
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

func (h *Handlers) saveGuardrailsForm(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form")
		return
	}
	settings, err := h.store.GetSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	items := parseGuardrails(r.FormValue("guardrails"))
	if items == nil && strings.TrimSpace(r.FormValue("guardrails")) != "[]" {
		writeError(w, http.StatusBadRequest, "invalid guardrails JSON")
		return
	}
	settings.Guardrails = items
	if err := h.store.SaveSettings(settings); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	http.Redirect(w, r, "/guardrails?saved=1", http.StatusFound)
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
