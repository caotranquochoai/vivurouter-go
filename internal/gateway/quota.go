package gateway

import "github.com/local/vivurouter-go/internal/store"

func applyAPIKeyUsage(settings store.Settings, apiKeyID string, usage usageInfo) store.Settings {
	if apiKeyID == "" || apiKeyID == "local" {
		return settings
	}
	for i := range settings.APIKeys {
		if settings.APIKeys[i].ID != apiKeyID {
			continue
		}
		settings.APIKeys[i].UsedRequests++
		settings.APIKeys[i].UsedTokens += usage.TotalTokens
		settings.APIKeys[i].UsedCostUSD += usage.CostUSD
		break
	}
	return settings
}
