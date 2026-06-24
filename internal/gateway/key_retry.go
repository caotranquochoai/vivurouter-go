package gateway

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/local/vivurouter-go/internal/provider"
	"github.com/local/vivurouter-go/internal/store"
)

// keyRetryMaxBounds caps how many key-level retries we attempt within a single
// provider before giving up and letting provider-level fallback take over.
const keyRetryMaxBounds = 8

// executeWithKeyRetry runs an executor attempt for a provider that may have a
// pool of API keys. When the upstream returns an auth/rate-limit/server error
// and the provider has additional keys available, the used key is cooled down
// and the attempt is retried with another key.
//
// It returns the ExecuteResult (whose Response body is still open) and a flag
// indicating whether the failure (if any) is fallback-eligible at the provider
// level. Callers should still run their own provider-level fallback/cooldown.
func (h *Handler) executeWithKeyRetry(
	ctx context.Context,
	cand resolvedModel,
	run func(ctx context.Context) (*provider.ExecuteResult, error),
) (*provider.ExecuteResult, bool, error) {
	if h.executors.KeyPool == nil || !providerHasKeys(cand.Provider) {
		result, err := run(ctx)
		return result, err == nil, err
	}

	maxAttempts := keyRetryMaxBounds
	if n := len(cand.Provider.Keys); n > maxAttempts {
		maxAttempts = n
	}

	for attempt := 0; attempt < maxAttempts; attempt++ {
		result, err := run(ctx)
		if err != nil {
			// Network error: no key to blame; let provider-level fallback handle it.
			return result, false, err
		}
		resp := result.Response
		if resp == nil {
			return result, true, nil
		}

		if !isKeyRetryEligible(resp.StatusCode) {
			return result, true, nil
		}

		// The key used hit an auth/rate-limit/server error. Cool it down and retry
		// with another key from the pool, if any remain.
		if result.UsedKeyID != "" && result.UsedKeyID != "legacy" {
			h.executors.KeyPool.MarkUnavailable(cand.Provider.ID, result.UsedKeyID, cooldownForKeyStatus(resp.StatusCode, resp.Header))
		}
		resp.Body.Close()

		// Peek whether another key is still available; if not, fall back to provider level.
		if key := h.executors.KeyPool.SelectKey(cand.Provider); key == nil {
			return nil, true, nil
		}
		// Loop and retry with the next key.
	}
	// Exhausted key retries; signal fallback-eligible so the provider gets cooled down too.
	return nil, true, nil
}

func providerHasKeys(p store.Provider) bool {
	for _, k := range p.Keys {
		if k.Enabled && k.Key != "" {
			return true
		}
	}
	return false
}

func isKeyRetryEligible(status int) bool {
	return status == http.StatusUnauthorized || status == http.StatusForbidden ||
		status == http.StatusTooManyRequests || status >= 500
}

func cooldownForKeyStatus(status int, header http.Header) time.Duration {
	switch {
	case status == http.StatusTooManyRequests:
		if d := parseRetryAfter(header.Get("Retry-After")); d > 0 {
			return d
		}
		return rateLimitFloor
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		// Auth errors are unlikely to resolve quickly; cool longer.
		return 2 * time.Minute
	default:
		return serverCooldown
	}
}

func errAllKeysExhausted(cand resolvedModel) error {
	return fmt.Errorf("provider %s: all API keys exhausted or unavailable", cand.Provider.ID)
}
