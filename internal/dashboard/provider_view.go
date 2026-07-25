package dashboard

import (
	"strings"
	"time"

	"github.com/local/vivurouter-go/internal/observe"
	"github.com/local/vivurouter-go/internal/store"
)

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
		card := buildProviderCard(provider, proxyByID, logStats[provider.ID], cooldownByProvider[provider.ID], settings, now, usageByProvider[provider.ID], modelUsageForProvider(provider.ID, provider.Models, usageByModel), modelUsageForProvider(provider.ID, provider.HiddenModels, usageByModel), usagePeriodLabel)
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

func buildProviderCard(provider store.Provider, proxyByID map[string]store.Proxy, stats providerLogStats, cooldown observe.CooldownStatus, settings store.Settings, now time.Time, usage UsageCounter, modelUsage []providerModelUsage, hiddenModelUsage []providerModelUsage, usagePeriodLabel string) providerCard {
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
		HiddenModelNames: append([]string(nil), provider.HiddenModels...),
		VisibleModels:    firstProviderModels(modelUsage, 10),
		ExtraModels:      hiddenProviderModels(modelUsage, 10),
		HiddenModels:     hiddenModelUsage,
		ExtraModelCount:  maxInt(len(modelUsage)-10, 0),
		HiddenModelCount: len(provider.HiddenModels),
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
