package gateway

import (
	"sync"
	"time"

	"github.com/local/vivurouter-go/internal/store"
)

const apiKeyRateWindow = time.Minute

type apiKeyLimiterEntry struct {
	windowStarted time.Time
	requests      int
	inFlight      int
}

type apiKeyLimiter struct {
	mu      sync.Mutex
	entries map[string]apiKeyLimiterEntry
}

func newAPIKeyLimiter() *apiKeyLimiter {
	return &apiKeyLimiter{entries: make(map[string]apiKeyLimiterEntry)}
}

// acquire admits a request and returns a release function. Limits are held for
// the complete gateway request, including streaming response delivery.
func (l *apiKeyLimiter) acquire(policy store.APIKeyPolicy, now time.Time) (func(), time.Duration, string) {
	if l == nil || policy.ID == "" || policy.ID == "local" || (policy.MaxRPM <= 0 && policy.MaxConcurrent <= 0) {
		return func() {}, 0, ""
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	entry := l.entries[policy.ID]
	if entry.windowStarted.IsZero() || now.Sub(entry.windowStarted) >= apiKeyRateWindow {
		entry.windowStarted = now
		entry.requests = 0
	}
	if policy.MaxRPM > 0 && entry.requests >= policy.MaxRPM {
		return nil, apiKeyRateWindow - now.Sub(entry.windowStarted), "api_key_rate_limited"
	}
	if policy.MaxConcurrent > 0 && entry.inFlight >= policy.MaxConcurrent {
		return nil, 0, "api_key_concurrency_limited"
	}
	entry.requests++
	entry.inFlight++
	l.entries[policy.ID] = entry

	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			defer l.mu.Unlock()
			entry, ok := l.entries[policy.ID]
			if !ok {
				return
			}
			if entry.inFlight > 0 {
				entry.inFlight--
			}
			l.entries[policy.ID] = entry
		})
	}, 0, ""
}
