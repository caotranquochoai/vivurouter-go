package provider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/local/vivurouter-go/internal/store"
)

func TestParseCodexResetCreditsPayload(t *testing.T) {
	report := ParseCodexResetCreditsPayload(map[string]any{
		"available_count": float64(2),
		"credits": []any{
			map[string]any{"status": "redeemed", "granted_at": "bad-date", "expires_at": nil},
			map[string]any{"status": "available", "granted_at": "2026-06-18T00:25:18Z", "expires_at": "2026-07-18T00:25:18Z"},
		},
	})
	if report.AvailableCount != 2 || len(report.Credits) != 2 {
		t.Fatalf("unexpected report: %#v", report)
	}
	if report.Credits[0].Status != "available" || report.Credits[0].ExpiresAt != "2026-07-18T00:25:18Z" {
		t.Fatalf("expected available credit sorted first: %#v", report.Credits)
	}
	if report.Credits[1].GrantedAt != "" || report.Credits[1].ExpiresAt != "" {
		t.Fatalf("invalid dates must normalize to empty strings: %#v", report.Credits[1])
	}
}

func TestParseCodexQuotaPayloadIncludesResetCredits(t *testing.T) {
	report := ParseCodexQuotaPayload(map[string]any{
		"rate_limit_reset_credits": map[string]any{"available_count": float64(3)},
	})
	if report.ResetCredits.AvailableCount != 3 {
		t.Fatalf("available reset credits = %d, want 3", report.ResetCredits.AvailableCount)
	}
}

func TestCodexFetchResetCreditsHeadersAndNormalization(t *testing.T) {
	token := testCodexJWT(t, map[string]any{
		"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": "acct_123"},
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer "+token {
			t.Fatalf("unexpected request: %s %s", r.Method, r.Header.Get("Authorization"))
		}
		if r.Header.Get("ChatGPT-Account-ID") != "acct_123" || r.Header.Get("OpenAI-Beta") != "codex-1" || r.Header.Get("originator") != "codex_cli_rs" {
			t.Fatalf("missing Codex headers: %#v", r.Header)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"available_count": 1,
			"credits":         []map[string]any{{"status": "available", "granted_at": 1781742318, "expires_at": "2026-07-18T00:25:18Z"}},
		})
	}))
	defer server.Close()
	t.Setenv("CODEX_RESET_CREDITS_URL", server.URL)

	report, err := (&CodexExecutor{Client: server.Client()}).FetchResetCredits(context.Background(), store.Provider{ID: "codex", Type: store.ProviderCodex, AccessToken: token})
	if err != nil {
		t.Fatal(err)
	}
	if report.ProviderID != "codex" || report.AvailableCount != 1 || len(report.Credits) != 1 || report.Credits[0].GrantedAt == "" {
		t.Fatalf("unexpected report: %#v", report)
	}
}

func TestCodexConsumeResetCreditClassifiesResponses(t *testing.T) {
	var gotID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			t.Fatalf("unexpected request: %s %#v", r.Method, r.Header)
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotID = body["redeem_request_id"]
		_ = json.NewEncoder(w).Encode(map[string]any{"code": "reset", "windows_reset": 2})
	}))
	defer server.Close()
	t.Setenv("CODEX_RESET_CREDITS_CONSUME_URL", server.URL)

	result, err := (&CodexExecutor{Client: server.Client()}).ConsumeResetCredit(context.Background(), store.Provider{ID: "codex", AccessToken: "token"}, "redeem-123")
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK || result.NoCredit || result.WindowsReset != 2 || gotID != "redeem-123" {
		t.Fatalf("unexpected result: %#v id=%q", result, gotID)
	}
}

func TestCodexConsumeResetCreditNoCredit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"code": "no_credit", "windows_reset": 0})
	}))
	defer server.Close()
	t.Setenv("CODEX_RESET_CREDITS_CONSUME_URL", server.URL)
	result, err := (&CodexExecutor{Client: server.Client()}).ConsumeResetCredit(context.Background(), store.Provider{ID: "codex", AccessToken: "token"}, "redeem-123")
	if err != nil {
		t.Fatal(err)
	}
	if result.OK || !result.NoCredit {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestCodexFetchResetCreditsPreservesAuthStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{"message": "expired token"})
	}))
	defer server.Close()
	t.Setenv("CODEX_RESET_CREDITS_URL", server.URL)
	_, err := (&CodexExecutor{Client: server.Client()}).FetchResetCredits(context.Background(), store.Provider{ID: "codex", AccessToken: "secret-token"})
	upstream, ok := err.(*CodexUpstreamError)
	if !ok || upstream.Status != http.StatusUnauthorized || strings.Contains(upstream.Error(), "secret-token") {
		t.Fatalf("unexpected error: %#v", err)
	}
}

func testCodexJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	return header + "." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
}
