package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/local/vivurouter-go/internal/provider"
	"github.com/local/vivurouter-go/internal/store"
)

func TestCodexRoutingQuotaUsageOnlyUsesSessionAndWeekly(t *testing.T) {
	report := provider.CodexQuotaReport{Quotas: []provider.CodexQuota{
		{Key: "session", Used: 42},
		{Key: "weekly", Used: 69.5},
		{Key: "review_weekly", Used: 99},
	}}
	used, valid := codexRoutingQuotaUsage(report)
	if !valid || used != 69.5 {
		t.Fatalf("used=%v valid=%v, want 69.5/true", used, valid)
	}
}

func TestCodexQuotaGateCachesAndAppliesThreshold(t *testing.T) {
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"rate_limit":{"primary_window":{"used_percent":70},"secondary_window":{"used_percent":20}}}`))
	}))
	defer server.Close()
	t.Setenv("CODEX_USAGE_URL", server.URL)

	gate := newCodexQuotaGate(&provider.CodexExecutor{Client: server.Client()})
	account := store.ProviderAccount{ID: "account-a", QuotaLimitPercent: 70}
	target := store.Provider{ID: "codex", Type: store.ProviderCodex, AccessToken: "token-a"}
	now := time.Now().UTC()
	if gate.allows(context.Background(), account, target, now) {
		t.Fatal("account at its threshold should be skipped")
	}
	if gate.allows(context.Background(), account, target, now.Add(time.Second)) {
		t.Fatal("cached account at its threshold should remain skipped")
	}
	if hits != 1 {
		t.Fatalf("quota hits=%d, want 1", hits)
	}
}

func TestCodexQuotaGateDisabledDoesNotFetch(t *testing.T) {
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	t.Setenv("CODEX_USAGE_URL", server.URL)

	gate := newCodexQuotaGate(&provider.CodexExecutor{Client: server.Client()})
	if !gate.allows(context.Background(), store.ProviderAccount{ID: "account-a"}, store.Provider{}, time.Now()) {
		t.Fatal("disabled quota limit should allow the account")
	}
	if hits != 0 {
		t.Fatalf("quota hits=%d, want 0", hits)
	}
}

func TestCodexQuotaGateFailsOpenWithoutSnapshot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	t.Setenv("CODEX_USAGE_URL", server.URL)

	gate := newCodexQuotaGate(&provider.CodexExecutor{Client: server.Client()})
	account := store.ProviderAccount{ID: "account-a", QuotaLimitPercent: 70}
	target := store.Provider{ID: "codex", AccessToken: "token-a"}
	if !gate.allows(context.Background(), account, target, time.Now()) {
		t.Fatal("quota fetch failure without a snapshot should fail open")
	}
}
