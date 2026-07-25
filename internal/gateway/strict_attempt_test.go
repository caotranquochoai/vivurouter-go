package gateway

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/local/vivurouter-go/internal/provider"
	"github.com/local/vivurouter-go/internal/store"
)

func TestConsumeStrictSingleAttemptDashboardOnly(t *testing.T) {
	for _, tc := range []struct {
		name      string
		dashboard bool
		want      bool
	}{
		{name: "dashboard", dashboard: true, want: true},
		{name: "public", dashboard: false, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := map[string]any{"model": "m", "vivurouter_disable_fallback": true}
			if got := consumeStrictSingleAttempt(body, tc.dashboard); got != tc.want {
				t.Fatalf("strict=%v want %v", got, tc.want)
			}
			if _, found := body["vivurouter_disable_fallback"]; found {
				t.Fatal("internal fallback field was not removed")
			}
		})
	}
}

func TestExecuteCandidateStrictModeDoesNotRetrySameCandidate(t *testing.T) {
	h, _ := newTestHandler(t)
	calls := 0
	ctx := withStrictSingleAttempt(context.Background(), true)
	_, err, decision := h.executeCandidate(ctx, resolvedModel{}, func(context.Context, resolvedModel) (*provider.ExecuteResult, error) {
		calls++
		return nil, io.ErrUnexpectedEOF
	})
	if err == nil || !decision.RetrySame {
		t.Fatalf("err=%v decision=%+v", err, decision)
	}
	if calls != 1 {
		t.Fatalf("calls=%d want 1", calls)
	}
}

func TestExecuteWithKeyRetryStrictModeUsesOneKey(t *testing.T) {
	h, _ := newTestHandler(t)
	cand := resolvedModel{Provider: store.Provider{
		ID:      "p",
		Enabled: true,
		Keys: []store.ProviderKey{
			{ID: "k1", Key: "secret-1", Enabled: true},
			{ID: "k2", Key: "secret-2", Enabled: true},
		},
	}}
	calls := 0
	result, _, err := h.executeWithKeyRetry(withStrictSingleAttempt(context.Background(), true), cand, func(context.Context) (*provider.ExecuteResult, error) {
		calls++
		return &provider.ExecuteResult{Response: &http.Response{
			StatusCode: http.StatusServiceUnavailable,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("down")),
		}, UsedKeyID: "k1"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || result.Response == nil || result.Response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("result=%+v", result)
	}
	if calls != 1 {
		t.Fatalf("calls=%d want 1", calls)
	}
	_ = result.Response.Body.Close()
}

func TestRunWithFallbackStrictModeUsesFirstCandidateOnly(t *testing.T) {
	h, _ := newTestHandler(t)
	calls := []string{}
	candidates := []resolvedModel{
		{Provider: store.Provider{ID: "p1"}, Model: "m1"},
		{Provider: store.Provider{ID: "p2"}, Model: "m2"},
	}
	rec := httptest.NewRecorder()
	r := chatRequest(t, "m1")
	r = r.WithContext(withStrictSingleAttempt(r.Context(), true))
	h.runWithFallback(rec, r, time.Now(), "/api/chat/completions", false, candidates,
		func(_ context.Context, cand resolvedModel) (*provider.ExecuteResult, error) {
			calls = append(calls, cand.Provider.ID)
			return &provider.ExecuteResult{Response: &http.Response{
				StatusCode: http.StatusServiceUnavailable,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("down")),
			}}, nil
		},
		func(http.ResponseWriter, *http.Request, *provider.ExecuteResult, resolvedModel) (usageInfo, error) {
			return usageInfo{}, nil
		},
		store.APIKeyPolicy{}, func(resolvedModel, *provider.ExecuteResult) map[string]any { return map[string]any{"model": "m1"} },
		map[string]any{"model": "m1"}, upstreamOptimizationMeta{}, 0, promptRouterDecision{}, store.Settings{},
	)
	if len(calls) != 1 || calls[0] != "p1" {
		t.Fatalf("calls=%v want [p1]", calls)
	}
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status=%d want 502", rec.Code)
	}
}
