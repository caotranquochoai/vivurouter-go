package gateway

import "github.com/local/vivurouter-go/internal/store"

func apiKeyUsageDelta(usage usageInfo) store.APIKeyUsageDelta {
	return store.APIKeyUsageDelta{
		Requests: 1,
		Tokens:   usage.TotalTokens,
		CostUSD:  usage.CostUSD,
	}
}
