package gateway

import (
	"context"
	"errors"
	"fmt"
	"io"
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
		settings,
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

func TestExecuteCandidateKeepsContextUntilResponseBodyCloses(t *testing.T) {
	h, _ := newTestHandler(t)
	ctxSeen := make(chan context.Context, 1)
	body := io.NopCloser(strings.NewReader("data: first\n\ndata: [DONE]\n\n"))
	result, err, decision := h.executeCandidate(context.Background(), resolvedModel{}, func(ctx context.Context, _ resolvedModel) (*provider.ExecuteResult, error) {
		ctxSeen <- ctx
		return &provider.ExecuteResult{Response: &http.Response{StatusCode: http.StatusOK, Body: body, Header: make(http.Header)}}, nil
	})
	if err != nil || decision.Class != "" {
		t.Fatalf("err=%v decision=%+v", err, decision)
	}
	attemptCtx := <-ctxSeen
	if attemptCtx.Err() != nil {
		t.Fatal("attempt context was cancelled before response body consumption")
	}
	if _, err := io.ReadAll(result.Response.Body); err != nil {
		t.Fatal(err)
	}
	if attemptCtx.Err() != nil {
		t.Fatal("attempt context was cancelled before response body close")
	}
	if err := result.Response.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if attemptCtx.Err() == nil {
		t.Fatal("attempt context was not cancelled when response body closed")
	}
}
func TestExecuteCandidateAcceptedBodyOutlivesAttemptTimeout(t *testing.T) {
	oldTimeout := perProviderAttemptTimeout
	perProviderAttemptTimeout = 20 * time.Millisecond
	t.Cleanup(func() { perProviderAttemptTimeout = oldTimeout })

	h, _ := newTestHandler(t)
	ctxSeen := make(chan context.Context, 1)
	result, err, decision := h.executeCandidate(context.Background(), resolvedModel{}, func(ctx context.Context, _ resolvedModel) (*provider.ExecuteResult, error) {
		ctxSeen <- ctx
		return &provider.ExecuteResult{Response: &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("complete body")),
			Header:     make(http.Header),
		}}, nil
	})
	if err != nil || decision.Class != "" {
		t.Fatalf("err=%v decision=%+v", err, decision)
	}
	attemptCtx := <-ctxSeen
	time.Sleep(3 * perProviderAttemptTimeout)
	if attemptCtx.Err() != nil {
		t.Fatalf("accepted body inherited provider-attempt timeout: %v", context.Cause(attemptCtx))
	}
	body, err := io.ReadAll(result.Response.Body)
	if err != nil || string(body) != "complete body" {
		t.Fatalf("body=%q err=%v", body, err)
	}
	if err := result.Response.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(context.Cause(attemptCtx), context.Canceled) {
		t.Fatalf("close cause=%v, want context canceled", context.Cause(attemptCtx))
	}
}

func TestExecuteCandidateAcceptedBodyStillHonorsParentCancellation(t *testing.T) {
	h, _ := newTestHandler(t)
	parent, cancelParent := context.WithCancel(context.Background())
	ctxSeen := make(chan context.Context, 1)
	result, err, _ := h.executeCandidate(parent, resolvedModel{}, func(ctx context.Context, _ resolvedModel) (*provider.ExecuteResult, error) {
		ctxSeen <- ctx
		return &provider.ExecuteResult{Response: &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("body")), Header: make(http.Header)}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	attemptCtx := <-ctxSeen
	cancelParent()
	select {
	case <-attemptCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("accepted body did not inherit parent cancellation")
	}
	if err := result.Response.Body.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCancelOnCloseCancelsOnce(t *testing.T) {
	calls := 0
	body := &cancelOnClose{ReadCloser: io.NopCloser(strings.NewReader("body")), cancel: func() { calls++ }}
	if err := body.Close(); err != nil {
		t.Fatal(err)
	}
	if err := body.Close(); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("cancel calls=%d, want 1", calls)
	}
}

func TestDefaultPerProviderAttemptTimeout(t *testing.T) {
	if perProviderAttemptTimeout != 120*time.Second {
		t.Fatalf("per-provider attempt timeout = %v, want 120s", perProviderAttemptTimeout)
	}
}

func TestChatFallsBackOnPerProviderTimeout(t *testing.T) {
	oldTimeout := perProviderAttemptTimeout
	perProviderAttemptTimeout = 20 * time.Millisecond
	t.Cleanup(func() { perProviderAttemptTimeout = oldTimeout })

	var firstHits, secondHits int
	h, _ := newTestHandler(t)
	now := time.Now().UTC()
	settings := store.Settings{DefaultProvider: "p1"}
	providers := []store.Provider{
		{ID: "p1", Type: store.ProviderOpenAICompatible, Enabled: true, BaseURL: "http://slow.invalid", APIKey: "k", Models: []string{"m"}},
		{ID: "p2", Type: store.ProviderOpenAICompatible, Enabled: true, BaseURL: "http://ok.invalid", APIKey: "k", Models: []string{"m"}},
	}
	candidates := resolveCandidates("p1/m", settings, providers)
	if len(candidates) != 2 {
		t.Fatalf("candidates = %d, want 2", len(candidates))
	}

	rec := httptest.NewRecorder()
	r := chatRequest(t, "p1/m")
	h.runWithFallback(rec, r, now, "/v1/chat/completions", false, candidates,
		func(ctx context.Context, cand resolvedModel) (*provider.ExecuteResult, error) {
			if cand.Provider.ID == "p1" {
				firstHits++
				<-ctx.Done()
				return nil, ctx.Err()
			}
			secondHits++
			return &provider.ExecuteResult{
				Response: &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{},
					Body:       io.NopCloser(strings.NewReader(`{"id":"ok","object":"chat.completion","choices":[]}`)),
				},
			}, nil
		},
		func(w http.ResponseWriter, r *http.Request, result *provider.ExecuteResult, cand resolvedModel) (usageInfo, error) {
			passthroughResponse(w, result.Response)
			return usageInfo{}, nil
		},
		store.APIKeyPolicy{},
		func(_ resolvedModel, _ *provider.ExecuteResult) map[string]any { return map[string]any{"model": "m"} },
		map[string]any{"model": "p1/m"},
		upstreamOptimizationMeta{},
		0,
		promptRouterDecision{},
		settings,
	)

	if firstHits != 1 || secondHits != 1 {
		t.Fatalf("hits first=%d second=%d, want 1/1", firstHits, secondHits)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestFallbackDoesNotRetryInvalidRequest(t *testing.T) {
	var firstHits, secondHits int
	h, _ := newTestHandler(t)
	h.SetRequestDeadlinePolicy(time.Second, 10*time.Millisecond)
	candidates := []resolvedModel{
		{Provider: store.Provider{ID: "p1"}, Model: "m"},
		{Provider: store.Provider{ID: "p2"}, Model: "m"},
	}
	rec := httptest.NewRecorder()
	r := chatRequest(t, "m")
	h.runWithFallback(rec, r, time.Now(), "/v1/chat/completions", false, candidates,
		func(ctx context.Context, cand resolvedModel) (*provider.ExecuteResult, error) {
			if cand.Provider.ID == "p1" {
				firstHits++
				return &provider.ExecuteResult{Response: fakeUpstream(http.StatusBadRequest, "application/json", `{"error":"bad request"}`, nil)}, nil
			}
			secondHits++
			return &provider.ExecuteResult{Response: fakeUpstream(http.StatusOK, "application/json", `{}`, nil)}, nil
		},
		func(w http.ResponseWriter, r *http.Request, result *provider.ExecuteResult, cand resolvedModel) (usageInfo, error) {
			return usageInfo{}, nil
		},
		store.APIKeyPolicy{}, func(resolvedModel, *provider.ExecuteResult) map[string]any { return map[string]any{"model": "m"} }, map[string]any{}, upstreamOptimizationMeta{}, 0, promptRouterDecision{}, store.Settings{},
	)
	if firstHits != 1 || secondHits != 0 {
		t.Fatalf("hits first=%d second=%d, want 1/0", firstHits, secondHits)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestFallbackHonorsTotalRequestDeadline(t *testing.T) {
	oldAttemptTimeout := perProviderAttemptTimeout
	perProviderAttemptTimeout = time.Second
	t.Cleanup(func() { perProviderAttemptTimeout = oldAttemptTimeout })

	var secondHits int
	h, _ := newTestHandler(t)
	h.SetRequestDeadlinePolicy(30*time.Millisecond, 10*time.Millisecond)
	candidates := []resolvedModel{
		{Provider: store.Provider{ID: "p1"}, Model: "m"},
		{Provider: store.Provider{ID: "p2"}, Model: "m"},
	}
	rec := httptest.NewRecorder()
	started := time.Now()
	h.runWithFallback(rec, chatRequest(t, "m"), started, "/v1/chat/completions", false, candidates,
		func(ctx context.Context, cand resolvedModel) (*provider.ExecuteResult, error) {
			if cand.Provider.ID == "p2" {
				secondHits++
			}
			<-ctx.Done()
			return nil, ctx.Err()
		},
		func(w http.ResponseWriter, r *http.Request, result *provider.ExecuteResult, cand resolvedModel) (usageInfo, error) {
			return usageInfo{}, nil
		},
		store.APIKeyPolicy{}, func(resolvedModel, *provider.ExecuteResult) map[string]any { return map[string]any{"model": "m"} }, map[string]any{}, upstreamOptimizationMeta{}, 0, promptRouterDecision{}, store.Settings{},
	)
	if elapsed := time.Since(started); elapsed > 150*time.Millisecond {
		t.Fatalf("fallback exceeded total deadline: %v", elapsed)
	}
	if secondHits != 0 {
		t.Fatalf("second candidate must not start after budget exhaustion, hits=%d", secondHits)
	}
	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want 504", rec.Code)
	}
}

func TestClassifyResponse(t *testing.T) {
	if got := classifyResponse(http.StatusTooManyRequests, http.Header{}); got.Class != FailureRateLimited || !got.Fallback {
		t.Fatalf("429 decision = %+v", got)
	}
	if got := classifyResponse(http.StatusBadRequest, http.Header{}); got.Class != FailureInvalidRequest || got.Fallback {
		t.Fatalf("400 decision = %+v", got)
	}
	if got := classifyResponse(http.StatusServiceUnavailable, http.Header{}); got.Class != FailureUpstreamServer || !got.RetrySame {
		t.Fatalf("503 decision = %+v", got)
	}
}
func TestExpandAccountCandidatesUsesCodexAccounts(t *testing.T) {
	st, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	codex := store.Provider{ID: "codex", Type: store.ProviderCodex, Enabled: true, APIKey: "legacy", Models: []string{"cx/gpt-5.5"}}
	if err := st.UpsertProvider(codex); err != nil {
		t.Fatalf("save provider: %v", err)
	}
	for _, account := range []store.ProviderAccount{
		{ID: "codex-a", ProviderID: codex.ID, Name: "Primary", AuthType: "oauth", AccessToken: "access-a", RefreshToken: "refresh-a", Enabled: true, Priority: 1},
		{ID: "codex-disabled", ProviderID: codex.ID, Name: "Disabled", AuthType: "oauth", AccessToken: "access-disabled", Enabled: false, Priority: 2},
		{ID: "codex-cooldown", ProviderID: codex.ID, Name: "Cooldown", AuthType: "oauth", AccessToken: "access-cooldown", Enabled: true, Priority: 3, CooldownUntil: time.Now().UTC().Add(time.Minute)},
	} {
		if err := st.UpsertProviderAccount(account); err != nil {
			t.Fatalf("save account: %v", err)
		}
	}
	h := NewHandler(st, provider.NewExecutors(), observe.New())
	out := h.expandAccountCandidates(context.Background(), []resolvedModel{{Provider: codex, Model: "cx/gpt-5.5", IsCodex: true}}, time.Now().UTC())
	if len(out) != 1 {
		t.Fatalf("candidate count = %d, want 1: %+v", len(out), out)
	}
	if out[0].AccountID != "codex-a" || out[0].Provider.AccessToken != "access-a" || out[0].Provider.RefreshToken != "refresh-a" || out[0].Provider.APIKey != "" {
		t.Fatalf("Codex account candidate was not applied: %+v", out[0])
	}
}

func TestExpandAccountCandidatesSkipsCodexAccountAtQuotaLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		used := 20
		if r.Header.Get("Authorization") == "Bearer access-a" {
			used = 70
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(fmt.Sprintf(`{"rate_limit":{"primary_window":{"used_percent":%d},"secondary_window":{"used_percent":10}}}`, used)))
	}))
	defer server.Close()
	t.Setenv("CODEX_USAGE_URL", server.URL)

	st, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	codex := store.Provider{ID: "codex", Type: store.ProviderCodex, Enabled: true, Models: []string{"cx/gpt-5.5"}}
	for _, account := range []store.ProviderAccount{
		{ID: "codex-a", ProviderID: codex.ID, Name: "A", AuthType: "oauth", AccessToken: "access-a", Enabled: true, Priority: 1, QuotaLimitPercent: 70},
		{ID: "codex-b", ProviderID: codex.ID, Name: "B", AuthType: "oauth", AccessToken: "access-b", Enabled: true, Priority: 2, QuotaLimitPercent: 70},
	} {
		if err := st.UpsertProviderAccount(account); err != nil {
			t.Fatal(err)
		}
	}
	executors := provider.NewExecutors()
	executors.Codex.Client = server.Client()
	h := NewHandler(st, executors, observe.New())
	out := h.expandAccountCandidates(context.Background(), []resolvedModel{{Provider: codex, Model: "cx/gpt-5.5", IsCodex: true}}, time.Now().UTC())
	if len(out) != 1 || out[0].AccountID != "codex-b" {
		t.Fatalf("candidates=%+v, want only codex-b", out)
	}
}

func TestAccountCooldownBackoff(t *testing.T) {
	cases := []struct {
		streak int
		want   time.Duration
	}{
		{1, 15 * time.Second},
		{2, 30 * time.Second},
		{3, 60 * time.Second},
		{4, 120 * time.Second},
		{10, 120 * time.Second},
	}
	for _, tc := range cases {
		if got := accountCooldown(tc.streak, failureDecision{}, time.Now()); got != tc.want {
			t.Fatalf("streak %d cooldown=%s want %s", tc.streak, got, tc.want)
		}
	}
	if got := accountCooldown(1, failureDecision{RetryAfter: 45 * time.Second}, time.Now()); got != 45*time.Second {
		t.Fatalf("retry-after cooldown=%s want 45s", got)
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
