package gateway

import (
	"testing"
	"time"

	"github.com/local/vivurouter-go/internal/store"
)

func TestAPIKeyLimiterRPM(t *testing.T) {
	limiter := newAPIKeyLimiter()
	policy := store.APIKeyPolicy{ID: "limited", MaxRPM: 2}
	now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)

	for i := 0; i < 2; i++ {
		release, retryAfter, code := limiter.acquire(policy, now)
		if code != "" || retryAfter != 0 {
			t.Fatalf("request %d unexpectedly rejected: code=%q retry=%s", i+1, code, retryAfter)
		}
		release()
	}
	if release, retryAfter, code := limiter.acquire(policy, now); release != nil || code != "api_key_rate_limited" || retryAfter <= 0 {
		t.Fatalf("rate limit = release:%v code:%q retry:%s", release != nil, code, retryAfter)
	}
	if release, retryAfter, code := limiter.acquire(policy, now.Add(time.Minute)); release == nil || code != "" || retryAfter != 0 {
		t.Fatalf("request after window reset = release:%v code:%q retry:%s", release != nil, code, retryAfter)
	} else {
		release()
	}
}

func TestAPIKeyLimiterConcurrentRequests(t *testing.T) {
	limiter := newAPIKeyLimiter()
	policy := store.APIKeyPolicy{ID: "limited", MaxConcurrent: 1}
	now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)

	release, retryAfter, code := limiter.acquire(policy, now)
	if release == nil || retryAfter != 0 || code != "" {
		t.Fatalf("first request rejected: code=%q retry=%s", code, retryAfter)
	}
	if second, retryAfter, code := limiter.acquire(policy, now); second != nil || retryAfter != 0 || code != "api_key_concurrency_limited" {
		t.Fatalf("concurrent request = release:%v code:%q retry:%s", second != nil, code, retryAfter)
	}
	release()
	if next, retryAfter, code := limiter.acquire(policy, now); next == nil || retryAfter != 0 || code != "" {
		t.Fatalf("request after release = release:%v code:%q retry:%s", next != nil, code, retryAfter)
	} else {
		next()
	}
}

func TestAPIKeyLimiterUnlimitedAndLocalBypass(t *testing.T) {
	limiter := newAPIKeyLimiter()
	now := time.Now()
	for _, policy := range []store.APIKeyPolicy{{ID: "unlimited"}, {ID: "local", MaxRPM: 1, MaxConcurrent: 1}} {
		for i := 0; i < 3; i++ {
			release, _, code := limiter.acquire(policy, now)
			if release == nil || code != "" {
				t.Fatalf("policy %+v request %d unexpectedly rejected: %q", policy, i+1, code)
			}
			release()
		}
	}
}
