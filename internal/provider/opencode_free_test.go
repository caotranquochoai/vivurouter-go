package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/local/vivurouter-go/internal/store"
)

func TestOpenCodeFreeExecuteChatUsesPublicHeaders(t *testing.T) {
	var gotAuth, gotClient, gotAccept string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotClient = r.Header.Get("x-opencode-client")
		gotAccept = r.Header.Get("Accept")
		if r.URL.Path != "/zen/v1/chat/completions" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer server.Close()

	executor := &OpenCodeFreeExecutor{Client: server.Client()}
	result, err := executor.ExecuteChat(context.Background(), store.Provider{ID: "opencode", Type: store.ProviderOpenCodeFree, BaseURL: server.URL}, "big-pickle", map[string]any{"messages": []any{}})
	if err != nil {
		t.Fatalf("ExecuteChat: %v", err)
	}
	defer result.Response.Body.Close()
	if gotAuth != openCodeFreeAuthorization {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	if gotClient != openCodeFreeClientHeader {
		t.Fatalf("x-opencode-client = %q", gotClient)
	}
	if gotAccept != "text/event-stream" {
		t.Fatalf("Accept = %q", gotAccept)
	}
	if gotBody["model"] != "big-pickle" {
		t.Fatalf("model = %v", gotBody["model"])
	}
}

func TestOpenCodeFreeFetchModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/zen/v1/models" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != openCodeFreeAuthorization {
			t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"big-pickle","name":"Big Pickle"}]}`))
	}))
	defer server.Close()

	executor := &OpenCodeFreeExecutor{Client: server.Client()}
	models, err := executor.FetchModels(context.Background(), store.Provider{ID: "opencode", Type: store.ProviderOpenCodeFree, BaseURL: server.URL})
	if err != nil {
		t.Fatalf("FetchModels: %v", err)
	}
	if len(models) != 1 || models[0].ID != "big-pickle" || models[0].Name != "Big Pickle" {
		t.Fatalf("models = %#v", models)
	}
}
