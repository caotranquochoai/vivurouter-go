package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/local/vivurouter-go/internal/observe"
	"github.com/local/vivurouter-go/internal/provider"
	"github.com/local/vivurouter-go/internal/store"
)

func newTestHandler(t *testing.T) (*Handler, *observe.State) {
	t.Helper()
	st, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	obs := observe.New()
	return NewHandler(st, provider.NewExecutors(), obs), obs
}

func chatRequest(t *testing.T, model string) *http.Request {
	t.Helper()
	body := `{"model":"` + model + `","stream":false,"messages":[{"role":"user","content":"hi"}]}`
	return httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
}

func TestChatFallsBackOnServerError(t *testing.T) {
	var firstHits, secondHits int
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		firstHits++
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"down"}`))
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondHits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"ok","object":"chat.completion","choices":[]}`))
	}))
	defer second.Close()

	h, obs := newTestHandler(t)
	now := time.Now().UTC()
	settings := store.Settings{DefaultProvider: "p1"}
	providers := []store.Provider{
		{ID: "p1", Type: store.ProviderOpenAICompatible, Enabled: true, BaseURL: first.URL, APIKey: "k", Models: []string{"m"}},
		{ID: "p2", Type: store.ProviderOpenAICompatible, Enabled: true, BaseURL: second.URL, APIKey: "k", Models: []string{"m"}},
	}
	candidates := resolveCandidates("p1/m", settings, providers)
	if len(candidates) != 2 {
		t.Fatalf("candidates = %d, want 2", len(candidates))
	}

	rec := httptest.NewRecorder()
	r := chatRequest(t, "p1/m")
	h.runWithFallback(rec, r, now, "/v1/chat/completions", false, candidates,
		func(ctx context.Context, cand resolvedModel) (*provider.ExecuteResult, error) {
			return h.executors.OpenAI.ExecuteChat(ctx, cand.Provider, cand.Model, map[string]any{"model": cand.Model})
		},
		func(w http.ResponseWriter, r *http.Request, result *provider.ExecuteResult, cand resolvedModel) (usageInfo, error) {
			passthroughResponse(w, result.Response)
			return usageInfo{}, nil
		},
		store.APIKeyPolicy{},
		func(_ resolvedModel, result *provider.ExecuteResult) map[string]any {
			if result != nil && result.TransformedBody != nil {
				return result.TransformedBody
			}
			return map[string]any{"model": "m", "messages": []any{map[string]any{"role": "user", "content": "hello"}}}
		},
		map[string]any{"model": "p1/m"},
		upstreamOptimizationMeta{},
		0,
		promptRouterDecision{},
	)

	if firstHits != 1 {
		t.Fatalf("first upstream hits = %d, want 1", firstHits)
	}
	if secondHits != 1 {
		t.Fatalf("second upstream hits = %d, want 1", secondHits)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if obs.Cooldowns.Available("p1", now) {
		t.Fatal("p1 should be in cooldown after 503")
	}
	if !obs.Cooldowns.Available("p2", now) {
		t.Fatal("p2 should remain available")
	}
}

func TestParseRetryAfterSeconds(t *testing.T) {
	if got := parseRetryAfter("30"); got != 30*time.Second {
		t.Fatalf("parseRetryAfter(30) = %v, want 30s", got)
	}
	if got := parseRetryAfter(""); got != 0 {
		t.Fatalf("parseRetryAfter empty = %v, want 0", got)
	}
	if got := parseRetryAfter("-5"); got != 0 {
		t.Fatalf("parseRetryAfter(-5) = %v, want 0", got)
	}
}

func TestCooldownForStatus(t *testing.T) {
	h := http.Header{}
	if d := cooldownForStatus(http.StatusInternalServerError, h); d != serverCooldown {
		t.Fatalf("5xx cooldown = %v, want %v", d, serverCooldown)
	}
	if d := cooldownForStatus(http.StatusTooManyRequests, h); d != rateLimitFloor {
		t.Fatalf("429 default cooldown = %v, want %v", d, rateLimitFloor)
	}
	h.Set("Retry-After", "45")
	if d := cooldownForStatus(http.StatusTooManyRequests, h); d != 45*time.Second {
		t.Fatalf("429 retry-after cooldown = %v, want 45s", d)
	}
}
