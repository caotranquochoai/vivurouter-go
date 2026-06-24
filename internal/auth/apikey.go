package auth

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/local/vivurouter-go/internal/store"
)

// ExtractAPIKey accepts both OpenAI and Anthropic style headers.
func ExtractAPIKey(r *http.Request) string {
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
	}
	return strings.TrimSpace(r.Header.Get("x-api-key"))
}

// CheckAPIKey enforces the local API key only when settings require it.
func CheckAPIKey(settings store.Settings, r *http.Request) bool {
	_, ok := ResolveAPIKey(settings, r)
	return ok
}

func ResolveAPIKey(settings store.Settings, r *http.Request) (store.APIKeyPolicy, bool) {
	if !settings.RequireAPIKey {
		return store.APIKeyPolicy{}, true
	}
	key := ExtractAPIKey(r)
	if key == "" {
		return store.APIKeyPolicy{}, false
	}
	for _, item := range settings.APIKeys {
		if !item.Enabled || item.Key == "" {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(key), []byte(item.Key)) == 1 {
			return item, true
		}
	}
	if settings.LocalAPIKey != "" && subtle.ConstantTimeCompare([]byte(key), []byte(settings.LocalAPIKey)) == 1 {
		return store.APIKeyPolicy{ID: "local", Key: settings.LocalAPIKey, Enabled: true}, true
	}
	return store.APIKeyPolicy{}, false
}

func KeyAllowsModel(policy store.APIKeyPolicy, model string) bool {
	if len(policy.AllowedModels) == 0 {
		return true
	}
	model = strings.TrimSpace(model)
	for _, allowed := range policy.AllowedModels {
		allowed = strings.TrimSpace(allowed)
		if allowed == "" || allowed == "*" || allowed == model {
			return true
		}
		if strings.HasSuffix(allowed, "/*") && strings.HasPrefix(model, strings.TrimSuffix(allowed, "*")) {
			return true
		}
	}
	return false
}

func KeyWithinQuota(policy store.APIKeyPolicy) bool {
	if !policy.Enabled && policy.ID != "local" {
		return false
	}
	if policy.MaxRequests > 0 && policy.UsedRequests >= policy.MaxRequests {
		return false
	}
	if policy.MaxTokens > 0 && policy.UsedTokens >= policy.MaxTokens {
		return false
	}
	if policy.MaxCostUSD > 0 && policy.UsedCostUSD >= policy.MaxCostUSD {
		return false
	}
	return true
}
