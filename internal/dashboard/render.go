package dashboard

import (
	"encoding/json"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/local/vivurouter-go/internal/store"
)

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
	invoices, err := h.store.ListInvoices()
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
		ActivePath:          r.URL.Path,
		T:                   bundle,
		Now:                 now,
		Config:              h.cfg,
		Settings:            settings,
		Providers:           providers,
		Proxies:             sanitizeProxies(proxies),
		Invoices:            invoices,
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
		GuardrailsText:      formatGuardrails(settings.Guardrails),
		AvailableModels:     comboModelOptions(providers),
		ChatModels:          chatStudioModels(providers, settings),
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
		APIKeySummary:       buildDashboardAPIKeySummary(settings.APIKeys),
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

func buildDashboardAPIKeySummary(keys []store.APIKeyPolicy) dashboardAPIKeySummary {
	summary := dashboardAPIKeySummary{Total: len(keys)}
	for _, key := range keys {
		if key.Enabled {
			summary.Active++
		}
		summary.Requests += key.UsedRequests
		summary.CostUSD += key.UsedCostUSD
		if key.MaxRequests > 0 {
			summary.HasRequestLimit = true
			summary.RequestLimit += key.MaxRequests
		}
		if key.MaxCostUSD > 0 {
			summary.HasCostLimit = true
			summary.CostLimitUSD += key.MaxCostUSD
		}
	}
	return summary
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
