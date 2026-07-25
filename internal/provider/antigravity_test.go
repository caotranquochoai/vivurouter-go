package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/local/vivurouter-go/internal/store"
)

func TestAntigravityRequestTransformsChat(t *testing.T) {
	body := map[string]any{
		"messages": []any{
			map[string]any{"role": "system", "content": "be helpful"},
			map[string]any{"role": "user", "content": "hello"},
		},
		"max_tokens":  20000.0,
		"temperature": 0.5,
	}
	out := antigravityRequest("gemini-3-flash-agent", body, "session-1")
	if out["model"] != "gemini-3-flash-agent" || out["userAgent"] != "antigravity" {
		t.Fatalf("unexpected root = %#v", out)
	}
	req := out["request"].(map[string]any)
	system := req["systemInstruction"].(map[string]any)["parts"].([]any)[0].(map[string]any)["text"]
	if system != "be helpful" {
		t.Fatalf("system = %q", system)
	}
	contents := req["contents"].([]any)
	if len(contents) != 1 || contents[0].(map[string]any)["role"] != "user" {
		t.Fatalf("contents = %#v", contents)
	}
	config := req["generationConfig"].(map[string]any)
	if config["maxOutputTokens"] != float64(maxAntigravityOutputTokens) || config["temperature"] != 0.5 {
		t.Fatalf("config = %#v", config)
	}
}

func TestAntigravityResponseTransformsJSON(t *testing.T) {
	payload := map[string]any{"response": map[string]any{
		"responseId":    "r1",
		"candidates":    []any{map[string]any{"content": map[string]any{"parts": []any{map[string]any{"text": "hi"}}}, "finishReason": "STOP"}},
		"usageMetadata": map[string]any{"promptTokenCount": 1.0, "candidatesTokenCount": 2.0, "totalTokenCount": 3.0},
	}}
	out := antigravityOpenAIResponse("m", payload)
	choices := out["choices"].([]any)
	msg := choices[0].(map[string]any)["message"].(map[string]any)
	if msg["content"] != "hi" {
		t.Fatalf("response = %#v", out)
	}
	usage := out["usage"].(map[string]any)
	if usage["total_tokens"] != 3 {
		t.Fatalf("usage = %#v", usage)
	}
}

func TestAntigravitySSEStreamsOpenAIChunks(t *testing.T) {
	raw := []byte(`data: {"response":{"candidates":[{"content":{"parts":[{"text":"h"}]}}]}}

data: {"response":{"candidates":[{"content":{"parts":[{"text":"hi"}]}}]}}

data: {"response":{"candidates":[{"content":{"parts":[]},"finishReason":"STOP"}]}}

`)
	var out strings.Builder
	if err := streamAntigravitySSE(&out, strings.NewReader(string(raw)), "m"); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if strings.Count(text, `"content":"h"`) != 1 || strings.Count(text, `"content":"i"`) != 1 || !strings.Contains(text, "data: [DONE]") {
		t.Fatalf("sse = %s", text)
	}
}

func TestAntigravityExecutorStreamsBeforeUpstreamCompletes(t *testing.T) {
	firstEventWritten := make(chan struct{})
	allowFinish := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hello\"}]}}]}}\n\n")
		w.(http.Flusher).Flush()
		close(firstEventWritten)
		<-allowFinish
		_, _ = io.WriteString(w, "data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[]},\"finishReason\":\"STOP\"}]}}\n\n")
	}))
	defer server.Close()

	executor := &AntigravityExecutor{Client: server.Client()}
	resultCh := make(chan *ExecuteResult, 1)
	errCh := make(chan error, 1)
	go func() {
		result, err := executor.ExecuteChat(context.Background(), store.Provider{ID: "ag", Type: store.ProviderAntigravity, BaseURL: server.URL, AccessToken: "access"}, "m", map[string]any{"stream": true, "messages": []any{map[string]any{"role": "user", "content": "hi"}}})
		resultCh <- result
		errCh <- err
	}()
	select {
	case <-firstEventWritten:
	case <-time.After(time.Second):
		t.Fatal("upstream did not start")
	}
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
		result := <-resultCh
		buf := make([]byte, 256)
		n, readErr := result.Response.Body.Read(buf)
		if readErr != nil && readErr != io.EOF {
			t.Fatal(readErr)
		}
		if !strings.Contains(string(buf[:n]), `"content":"hello"`) {
			t.Fatalf("first chunk = %s", string(buf[:n]))
		}
		close(allowFinish)
		_ = result.Response.Body.Close()
	case <-time.After(250 * time.Millisecond):
		close(allowFinish)
		t.Fatal("executor buffered the stream until upstream completion")
	}
}

func TestAntigravityToolHistoryUsesFunctionResponse(t *testing.T) {
	contents := antigravityContents([]any{
		map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"id": "call-1", "function": map[string]any{"name": "lookup", "arguments": `{}`}}}},
		map[string]any{"role": "tool", "tool_call_id": "call-1", "content": `{"answer":"ok"}`},
	})
	if len(contents) != 2 {
		t.Fatalf("contents = %#v", contents)
	}
	parts := contents[1].(map[string]any)["parts"].([]any)
	response := parts[0].(map[string]any)["functionResponse"].(map[string]any)
	if response["name"] != "lookup" || response["id"] != "call-1" {
		t.Fatalf("function response = %#v", response)
	}
}

func TestAntigravityResponseIncludesToolCalls(t *testing.T) {
	payload := map[string]any{
		"response": map[string]any{
			"candidates": []any{map[string]any{
				"content": map[string]any{"parts": []any{map[string]any{
					"functionCall": map[string]any{"id": "call-1", "name": "lookup", "args": map[string]any{"q": "x"}},
				}}},
				"finishReason": "STOP",
			}},
		},
	}
	out := antigravityOpenAIResponse("m", payload)
	message := out["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)
	calls := message["tool_calls"].([]any)
	if len(calls) != 1 || calls[0].(map[string]any)["id"] != "call-1" {
		t.Fatalf("tool calls = %#v", message)
	}
}

func TestAntigravityExecutorHeadersAndURL(t *testing.T) {
	var gotPath, gotAuth, gotSource, gotSession, gotUA string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.String()
		gotAuth = r.Header.Get("Authorization")
		gotSource = r.Header.Get(antigravityRequestSourceHeader)
		gotSession = r.Header.Get(antigravityMachineSession)
		gotUA = r.Header.Get("User-Agent")
		_ = json.NewEncoder(w).Encode(map[string]any{"response": map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"parts": []any{map[string]any{"text": "ok"}}}, "finishReason": "STOP"}}}})
	}))
	defer server.Close()

	executor := &AntigravityExecutor{Client: server.Client()}
	provider := store.Provider{ID: "antigravity", Type: store.ProviderAntigravity, BaseURL: server.URL, AccessToken: "access"}
	result, err := executor.ExecuteChat(context.Background(), provider, "gemini-3-flash-agent", map[string]any{"messages": []any{map[string]any{"role": "user", "content": "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	result.Response.Body.Close()
	if gotPath != "/v1internal:generateContent" || gotAuth != "Bearer access" || gotSource != antigravityRequestSource || gotSession == "" || !strings.HasPrefix(gotUA, "antigravity/1.107.0 ") {
		t.Fatalf("path=%q auth=%q source=%q session=%q ua=%q", gotPath, gotAuth, gotSource, gotSession, gotUA)
	}
}

type antigravityMemoryStore struct {
	provider store.Provider
	accounts map[string]store.ProviderAccount
}

func (s *antigravityMemoryStore) GetSettings() (store.Settings, error) { return store.Settings{}, nil }
func (s *antigravityMemoryStore) SaveSettings(store.Settings) error    { return nil }
func (s *antigravityMemoryStore) RecordAPIKeyUsage(string, store.APIKeyUsageDelta) error {
	return nil
}
func (s *antigravityMemoryStore) ListProviders() ([]store.Provider, error) {
	return []store.Provider{s.provider}, nil
}
func (s *antigravityMemoryStore) GetProvider(string) (store.Provider, bool, error) {
	return s.provider, true, nil
}
func (s *antigravityMemoryStore) UpsertProvider(p store.Provider) error { s.provider = p; return nil }
func (s *antigravityMemoryStore) DeleteProvider(string) error           { return nil }
func (s *antigravityMemoryStore) ListProviderAccounts(providerID string) ([]store.ProviderAccount, error) {
	out := []store.ProviderAccount{}
	for _, account := range s.accounts {
		if account.ProviderID == providerID {
			out = append(out, account)
		}
	}
	return out, nil
}
func (s *antigravityMemoryStore) GetProviderAccount(id string) (store.ProviderAccount, bool, error) {
	account, found := s.accounts[id]
	return account, found, nil
}
func (s *antigravityMemoryStore) UpsertProviderAccount(account store.ProviderAccount) error {
	if s.accounts == nil {
		s.accounts = map[string]store.ProviderAccount{}
	}
	s.accounts[account.ID] = account
	return nil
}
func (s *antigravityMemoryStore) DeleteProviderAccount(string) error { return nil }
func (s *antigravityMemoryStore) RecordProviderAccountOutcome(string, store.ProviderAccountOutcome) error {
	return nil
}
func (s *antigravityMemoryStore) AddRequestLog(store.RequestLog) error              { return nil }
func (s *antigravityMemoryStore) RecentRequestLogs(int) ([]store.RequestLog, error) { return nil, nil }
func (s *antigravityMemoryStore) GetRequestDebugPayload(string) (*store.RequestLogDebugPayload, bool, error) {
	return nil, false, nil
}
func (s *antigravityMemoryStore) DeleteRequestDebugPayloads() (int, error) { return 0, nil }
func (s *antigravityMemoryStore) ResetAllData() error                      { return nil }
func (s *antigravityMemoryStore) ListProxies() ([]store.Proxy, error)      { return nil, nil }
func (s *antigravityMemoryStore) GetProxy(string) (store.Proxy, bool, error) {
	return store.Proxy{}, false, nil
}
func (s *antigravityMemoryStore) UpsertProxy(store.Proxy) error { return nil }
func (s *antigravityMemoryStore) DeleteProxy(string) error      { return nil }
func (s *antigravityMemoryStore) ListInvoices() ([]store.Invoice, error) {
	return nil, nil
}
func (s *antigravityMemoryStore) GetInvoice(string) (store.Invoice, bool, error) {
	return store.Invoice{}, false, nil
}
func (s *antigravityMemoryStore) UpsertInvoice(store.Invoice) error { return nil }
func (s *antigravityMemoryStore) DeleteInvoice(string) error        { return nil }

func TestAntigravityRetriesServiceUnavailable(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"response": map[string]any{
				"candidates": []any{map[string]any{
					"content":      map[string]any{"parts": []any{map[string]any{"text": "ok"}}},
					"finishReason": "STOP",
				}},
			},
		})
	}))
	defer server.Close()

	executor := &AntigravityExecutor{Client: server.Client()}
	result, err := executor.ExecuteChat(context.Background(), store.Provider{ID: "ag", Type: store.ProviderAntigravity, BaseURL: server.URL, AccessToken: "access"}, "m", map[string]any{"messages": []any{map[string]any{"role": "user", "content": "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Response.Body.Close()
	if calls != 2 || result.Response.StatusCode != http.StatusOK {
		t.Fatalf("calls=%d status=%d", calls, result.Response.StatusCode)
	}
}
func TestAntigravityRefreshesOnUnauthorized(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "new-access"})
			return
		}
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Header.Get("Authorization") != "Bearer new-access" {
			t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"response": map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"parts": []any{map[string]any{"text": "ok"}}}, "finishReason": "STOP"}}}})
	}))
	defer server.Close()

	st := &antigravityMemoryStore{}
	provider := store.Provider{ID: "antigravity", Type: store.ProviderAntigravity, BaseURL: server.URL, AccessToken: "old", RefreshToken: "refresh"}
	executor := &AntigravityExecutor{Client: server.Client(), Store: st, TokenURL: server.URL + "/token"}
	result, err := executor.ExecuteChat(context.Background(), provider, "gemini-3-flash-agent", map[string]any{"messages": []any{map[string]any{"role": "user", "content": "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	result.Response.Body.Close()
	if st.provider.AccessToken != "new-access" || calls != 2 {
		t.Fatalf("stored=%q calls=%d", st.provider.AccessToken, calls)
	}
}

func TestParseAntigravityQuotaPayload(t *testing.T) {
	report := ParseAntigravityQuotaPayload(map[string]any{
		"tierId": "pro",
		"models": map[string]any{
			"gemini-3-flash-agent": map[string]any{"displayName": "Gemini", "quotaInfo": map[string]any{"remainingFraction": 0.75, "resetTime": "2026-06-18T12:00:00Z"}},
		},
	})
	if report.Plan != "pro" || len(report.Models) != 1 || report.Models[0] != "gemini-3-flash-agent" {
		t.Fatalf("report = %#v", report)
	}
	if len(report.Quotas) != 1 || report.Quotas[0].Name != "Gemini" || report.Quotas[0].Remaining != 75 || report.Quotas[0].Used != 25 || report.Quotas[0].ResetAt != "2026-06-18T12:00:00Z" {
		t.Fatalf("quotas = %#v", report.Quotas)
	}
}

func TestParseAntigravityQuotaPayloadFiltersInternalModels(t *testing.T) {
	report := ParseAntigravityQuotaPayload(map[string]any{"models": map[string]any{
		"MODEL_PLACEHOLDER_M19": map[string]any{"quotaInfo": map[string]any{"remainingFraction": 1.0}},
		"MODEL_CHAT_20706":      map[string]any{"quotaInfo": map[string]any{"remainingFraction": 1.0}},
		"internal-model":        map[string]any{"displayName": "Internal", "isInternal": true, "quotaInfo": map[string]any{"remainingFraction": 1.0}},
		"MODEL_CHAT_NAMED":      map[string]any{"displayName": "Gemini 3.1 Flash Lite", "quotaInfo": map[string]any{"remainingFraction": 0.5}},
		"gemini-valid":          map[string]any{"displayName": "Gemini Valid", "quotaInfo": map[string]any{"remainingFraction": 0.25}},
	}})
	if len(report.Models) != 2 || len(report.Quotas) != 2 {
		t.Fatalf("report = %#v", report)
	}
	if report.Quotas[0].Name != "Gemini 3.1 Flash Lite" || report.Quotas[1].Name != "Gemini Valid" {
		t.Fatalf("quotas = %#v", report.Quotas)
	}
}

func TestParseAntigravityResetTime(t *testing.T) {
	cases := map[string]struct {
		value any
		want  string
	}{
		"rfc3339":      {"2026-07-22T10:20:30Z", "2026-07-22T10:20:30Z"},
		"unix seconds": {1784715630, "2026-07-22T10:20:30Z"},
		"unix millis":  {int64(1784715630000), "2026-07-22T10:20:30Z"},
		"invalid":      {"2026-not-a-date", ""},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			if got := parseAntigravityResetTime(test.value); got != test.want {
				t.Fatalf("got %q want %q", got, test.want)
			}
		})
	}
}

func TestParseAntigravityQuotaPayloadRejectsUnknownAndClampsFractions(t *testing.T) {
	report := ParseAntigravityQuotaPayload(map[string]any{"models": map[string]any{
		"missing": map[string]any{"quotaInfo": map[string]any{"resetTime": "2026-01-01T00:00:00Z"}},
		"empty":   map[string]any{},
		"high":    map[string]any{"quotaInfo": map[string]any{"remainingFraction": 2.0}},
		"zero":    map[string]any{"quotaInfo": map[string]any{"remainingFraction": 0.0}},
	}})
	if len(report.Models) != 4 || len(report.Quotas) != 2 {
		t.Fatalf("report = %#v", report)
	}
	if report.Quotas[0].Key != "high" || report.Quotas[0].Remaining != 100 || report.Quotas[1].Key != "zero" || report.Quotas[1].Remaining != 0 {
		t.Fatalf("quotas = %#v", report.Quotas)
	}
}

func TestAntigravityRefreshTokenForAccountIsScoped(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "new-account-token"})
	}))
	defer server.Close()
	st := &antigravityMemoryStore{
		provider: store.Provider{ID: "ag", Type: store.ProviderAntigravity, AccessToken: "provider-token", RefreshToken: "provider-refresh"},
		accounts: map[string]store.ProviderAccount{
			"a": {ID: "a", ProviderID: "ag", AccessToken: "old-account-token", RefreshToken: "account-refresh"},
			"b": {ID: "b", ProviderID: "ag", AccessToken: "other-token", RefreshToken: "other-refresh"},
		},
	}
	executor := &AntigravityExecutor{Client: server.Client(), Store: st, TokenURL: server.URL}
	target := st.provider
	target.AccessToken = st.accounts["a"].AccessToken
	target.RefreshToken = st.accounts["a"].RefreshToken
	if _, err := executor.RefreshAntigravityTokenForAccount(context.Background(), target, "a"); err != nil {
		t.Fatal(err)
	}
	if st.provider.AccessToken != "provider-token" || st.accounts["a"].AccessToken != "new-account-token" || st.accounts["a"].RefreshToken != "account-refresh" || st.accounts["b"].AccessToken != "other-token" {
		t.Fatalf("provider=%#v accounts=%#v", st.provider, st.accounts)
	}
}

func TestAntigravityFetchQuota(t *testing.T) {
	var gotQuotaPath, gotQuotaAuth, gotQuotaUA, gotQuotaSource, gotQuotaClientName, gotQuotaClientVersion string
	var gotLoadUA, gotLoadSource string
	var gotQuotaBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1internal:loadCodeAssist":
			gotLoadUA = r.Header.Get("User-Agent")
			gotLoadSource = r.Header.Get(antigravityRequestSourceHeader)
			_ = json.NewEncoder(w).Encode(map[string]any{"cloudaicompanionProject": map[string]any{"id": "project-1"}, "currentTier": map[string]any{"name": "Pro"}})
		case "/v1internal:fetchAvailableModels":
			gotQuotaPath = r.URL.Path
			gotQuotaAuth = r.Header.Get("Authorization")
			gotQuotaUA = r.Header.Get("User-Agent")
			gotQuotaSource = r.Header.Get(antigravityRequestSourceHeader)
			gotQuotaClientName = r.Header.Get("X-Client-Name")
			gotQuotaClientVersion = r.Header.Get("X-Client-Version")
			_ = json.NewDecoder(r.Body).Decode(&gotQuotaBody)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"models": map[string]any{"gemini-3-flash-agent": map[string]any{"quotaInfo": map[string]any{"remainingFraction": 0.5}}},
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	t.Setenv("ANTIGRAVITY_LOAD_PROJECT_URL", server.URL+"/v1internal:loadCodeAssist")
	t.Setenv("ANTIGRAVITY_QUOTA_URL", server.URL+"/v1internal:fetchAvailableModels")
	executor := &AntigravityExecutor{Client: server.Client()}
	report, err := executor.FetchQuota(context.Background(), store.Provider{ID: "ag", Type: store.ProviderAntigravity, AccessToken: "access"})
	if err != nil {
		t.Fatal(err)
	}
	if gotQuotaPath != "/v1internal:fetchAvailableModels" || gotQuotaAuth != "Bearer access" || gotQuotaUA != antigravityIDEUserAgent || gotQuotaSource != "" || gotQuotaClientName != "antigravity" || gotQuotaClientVersion != antigravityIDEVersion {
		t.Fatalf("path=%q auth=%q ua=%q source=%q client=%q version=%q", gotQuotaPath, gotQuotaAuth, gotQuotaUA, gotQuotaSource, gotQuotaClientName, gotQuotaClientVersion)
	}
	if gotLoadUA != antigravityIDEUserAgent || gotLoadSource != "" {
		t.Fatalf("load ua=%q source=%q", gotLoadUA, gotLoadSource)
	}
	if gotQuotaBody["project"] != "project-1" {
		t.Fatalf("quota body = %#v", gotQuotaBody)
	}
	if report.ProviderID != "ag" || report.Plan != "Pro" || len(report.Models) != 1 || len(report.Quotas) != 1 {
		t.Fatalf("report = %#v", report)
	}
}
