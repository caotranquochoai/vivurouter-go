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

func TestAntigravitySSETransformsToOpenAI(t *testing.T) {
	raw := []byte(`data: {"response":{"candidates":[{"content":{"parts":[{"text":"hi"}]}}]}}

data: {"response":{"candidates":[{"content":{"parts":[]},"finishReason":"STOP"}]}}

`)
	out, err := antigravitySSEToOpenAI(raw, "m")
	if err != nil {
		t.Fatal(err)
	}
	text := string(out)
	if !strings.Contains(text, `"content":"hi"`) || !strings.Contains(text, "data: [DONE]") {
		t.Fatalf("sse = %s", text)
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

type antigravityMemoryStore struct{ provider store.Provider }

func (s *antigravityMemoryStore) GetSettings() (store.Settings, error) { return store.Settings{}, nil }
func (s *antigravityMemoryStore) SaveSettings(store.Settings) error    { return nil }
func (s *antigravityMemoryStore) ListProviders() ([]store.Provider, error) {
	return []store.Provider{s.provider}, nil
}
func (s *antigravityMemoryStore) GetProvider(string) (store.Provider, bool, error) {
	return s.provider, true, nil
}
func (s *antigravityMemoryStore) UpsertProvider(p store.Provider) error             { s.provider = p; return nil }
func (s *antigravityMemoryStore) DeleteProvider(string) error                       { return nil }
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
	if len(report.Quotas) != 1 || report.Quotas[0].Remaining != 75 || report.Quotas[0].Used != 25 {
		t.Fatalf("quotas = %#v", report.Quotas)
	}
}

func TestAntigravityFetchQuota(t *testing.T) {
	var gotQuotaPath, gotQuotaAuth, gotQuotaUA, gotQuotaSource string
	var gotQuotaBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1internal:loadCodeAssist":
			_ = json.NewEncoder(w).Encode(map[string]any{"cloudaicompanionProject": "project-1", "currentTier": map[string]any{"name": "Pro"}})
		case "/v1internal:fetchAvailableModels":
			gotQuotaPath = r.URL.Path
			gotQuotaAuth = r.Header.Get("Authorization")
			gotQuotaUA = r.Header.Get("User-Agent")
			gotQuotaSource = r.Header.Get(antigravityRequestSourceHeader)
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
	if gotQuotaPath != "/v1internal:fetchAvailableModels" || gotQuotaAuth != "Bearer access" || !strings.HasPrefix(gotQuotaUA, "antigravity/1.107.0 ") || gotQuotaSource != antigravityRequestSource {
		t.Fatalf("path=%q auth=%q ua=%q source=%q", gotQuotaPath, gotQuotaAuth, gotQuotaUA, gotQuotaSource)
	}
	if gotQuotaBody["project"] != "project-1" {
		t.Fatalf("quota body = %#v", gotQuotaBody)
	}
	if report.ProviderID != "ag" || report.Plan != "Pro" || len(report.Models) != 1 || len(report.Quotas) != 1 {
		t.Fatalf("report = %#v", report)
	}
}
