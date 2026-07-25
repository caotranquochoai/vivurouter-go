package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/local/vivurouter-go/internal/store"
)

type codexRefreshStore struct {
	provider store.Provider
	account  store.ProviderAccount
}

func (s *codexRefreshStore) GetSettings() (store.Settings, error) {
	return store.DefaultSettings(), nil
}
func (s *codexRefreshStore) SaveSettings(store.Settings) error { return nil }
func (s *codexRefreshStore) RecordAPIKeyUsage(string, store.APIKeyUsageDelta) error {
	return nil
}
func (s *codexRefreshStore) ListProviders() ([]store.Provider, error) {
	return []store.Provider{s.provider}, nil
}
func (s *codexRefreshStore) GetProvider(id string) (store.Provider, bool, error) {
	return s.provider, s.provider.ID == id, nil
}
func (s *codexRefreshStore) UpsertProvider(provider store.Provider) error {
	s.provider = provider
	return nil
}
func (s *codexRefreshStore) DeleteProvider(string) error { return nil }
func (s *codexRefreshStore) ListProviderAccounts(string) ([]store.ProviderAccount, error) {
	return nil, nil
}
func (s *codexRefreshStore) GetProviderAccount(id string) (store.ProviderAccount, bool, error) {
	return s.account, s.account.ID == id, nil
}
func (s *codexRefreshStore) UpsertProviderAccount(account store.ProviderAccount) error {
	s.account = account
	return nil
}
func (s *codexRefreshStore) DeleteProviderAccount(string) error { return nil }
func (s *codexRefreshStore) RecordProviderAccountOutcome(string, store.ProviderAccountOutcome) error {
	return nil
}
func (s *codexRefreshStore) AddRequestLog(store.RequestLog) error              { return nil }
func (s *codexRefreshStore) RecentRequestLogs(int) ([]store.RequestLog, error) { return nil, nil }
func (s *codexRefreshStore) GetRequestDebugPayload(string) (*store.RequestLogDebugPayload, bool, error) {
	return nil, false, nil
}
func (s *codexRefreshStore) DeleteRequestDebugPayloads() (int, error) { return 0, nil }
func (s *codexRefreshStore) ResetAllData() error                      { return nil }
func (s *codexRefreshStore) ListProxies() ([]store.Proxy, error)      { return nil, nil }
func (s *codexRefreshStore) GetProxy(string) (store.Proxy, bool, error) {
	return store.Proxy{}, false, nil
}
func (s *codexRefreshStore) UpsertProxy(store.Proxy) error          { return nil }
func (s *codexRefreshStore) DeleteProxy(string) error               { return nil }
func (s *codexRefreshStore) ListInvoices() ([]store.Invoice, error) { return nil, nil }
func (s *codexRefreshStore) GetInvoice(string) (store.Invoice, bool, error) {
	return store.Invoice{}, false, nil
}
func (s *codexRefreshStore) UpsertInvoice(store.Invoice) error { return nil }
func (s *codexRefreshStore) DeleteInvoice(string) error        { return nil }

func TestNormalizeCodexBodyPortsOriginalVivuRouterRules(t *testing.T) {
	provider := store.Provider{ID: "codex", Type: store.ProviderCodex}
	body := map[string]any{
		"input": []map[string]any{
			{"type": "message", "role": "system", "content": []map[string]any{{"type": "input_text", "text": "system"}}},
			{"type": "message", "role": "user", "id": "msg_server", "content": []map[string]any{{"type": "input_text", "text": "hello"}}},
			{"type": "item_reference", "id": "resp_old"},
		},
		"tools":       []map[string]any{{"type": "function", "function": map[string]any{"name": "Read", "description": "Read file", "parameters": map[string]any{"type": "object"}}}},
		"tool_choice": map[string]any{"type": "function", "name": "Missing"}, "reasoning": map[string]any{"effort": "low"}, "metadata": map[string]any{"drop": true},
	}
	out := normalizeCodexBody(provider, "cx/gpt-5.5", body)
	input := out["input"].([]map[string]any)
	if len(input) != 2 || input[0]["role"] != "developer" {
		t.Fatalf("input normalization failed: %+v", input)
	}
	if _, ok := input[1]["id"]; ok {
		t.Fatalf("server-generated id was not stripped: %+v", input[1])
	}
	tools := out["tools"].([]map[string]any)
	if len(tools) != 1 || tools[0]["name"] != "Read" || tools[0]["function"] != nil {
		t.Fatalf("tools were not flattened: %+v", tools)
	}
	if _, ok := out["tool_choice"]; ok {
		t.Fatalf("invalid tool_choice was not dropped: %+v", out["tool_choice"])
	}
	if _, ok := out["metadata"]; ok {
		t.Fatalf("unsupported metadata was not stripped")
	}
	if include, ok := out["include"].([]string); !ok || len(include) != 1 || include[0] != "reasoning.encrypted_content" {
		t.Fatalf("reasoning include missing: %+v", out["include"])
	}
}

func TestCodexExecutorRefreshesTokenOnAuthFailure(t *testing.T) {
	server, refreshCalled, upstreamTokens := newCodexRefreshServer(t)
	defer server.Close()
	st := &codexRefreshStore{}
	executor := &CodexExecutor{Client: server.Client(), Store: st, TokenURL: server.URL + "/token"}
	provider := store.Provider{ID: "codex", Type: store.ProviderCodex, BaseURL: server.URL + "/responses", AccessToken: "access-old", RefreshToken: "refresh-old", Enabled: true, Models: []string{"cx/gpt-5.5"}}
	result, err := executor.ExecuteResponses(context.Background(), provider, "cx/gpt-5.5", map[string]any{"input": "hello"})
	if err != nil {
		t.Fatalf("execute responses: %v", err)
	}
	defer result.Response.Body.Close()
	if result.Response.StatusCode != http.StatusOK || !*refreshCalled {
		t.Fatalf("status=%d refresh=%v", result.Response.StatusCode, *refreshCalled)
	}
	if got := *upstreamTokens; len(got) != 2 || got[0] != "access-old" || got[1] != "access-new" {
		t.Fatalf("upstream tokens = %#v", got)
	}
	if st.provider.AccessToken != "access-new" || st.provider.RefreshToken != "refresh-new" {
		t.Fatalf("stored provider tokens were not updated: %+v", st.provider)
	}
}

func TestCodexExecutorRefreshesTargetAccountOnly(t *testing.T) {
	server, _, _ := newCodexRefreshServer(t)
	defer server.Close()
	st := &codexRefreshStore{
		provider: store.Provider{ID: "codex", Type: store.ProviderCodex, AccessToken: "fallback-access", RefreshToken: "fallback-refresh"},
		account:  store.ProviderAccount{ID: "account-a", ProviderID: "codex", AccessToken: "access-old", RefreshToken: "refresh-old", Enabled: true},
	}
	executor := &CodexExecutor{Client: server.Client(), Store: st, TokenURL: server.URL + "/token"}
	effective := st.provider
	effective.BaseURL = server.URL + "/responses"
	effective.AccessToken, effective.RefreshToken = st.account.AccessToken, st.account.RefreshToken
	result, err := executor.ExecuteResponsesForAccount(context.Background(), effective, "cx/gpt-5.5", map[string]any{"input": "hello"}, st.account.ID)
	if err != nil {
		t.Fatalf("execute responses: %v", err)
	}
	defer result.Response.Body.Close()
	if st.account.AccessToken != "access-new" || st.account.RefreshToken != "refresh-new" {
		t.Fatalf("stored account tokens were not updated: %+v", st.account)
	}
	if st.provider.AccessToken != "fallback-access" || st.provider.RefreshToken != "fallback-refresh" {
		t.Fatalf("provider fallback was overwritten: %+v", st.provider)
	}
}

func newCodexRefreshServer(t *testing.T) (*httptest.Server, *bool, *[]string) {
	t.Helper()
	refreshCalled := false
	upstreamTokens := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/responses":
			upstreamTokens = append(upstreamTokens, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
			if len(upstreamTokens) == 1 {
				http.Error(w, "expired", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("event: response.completed\ndata: {}\n\nevent: done\ndata: [DONE]\n\n"))
		case "/token":
			refreshCalled = true
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse refresh form: %v", err)
			}
			if got := r.PostForm.Get("grant_type"); got != "refresh_token" {
				t.Fatalf("grant_type = %q", got)
			}
			if got := r.PostForm.Get("refresh_token"); got != "refresh-old" {
				t.Fatalf("refresh_token = %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "access-new", "refresh_token": "refresh-new", "expires_in": 3600})
		default:
			http.NotFound(w, r)
		}
	}))
	return server, &refreshCalled, &upstreamTokens
}
