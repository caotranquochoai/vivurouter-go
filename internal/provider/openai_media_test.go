package provider

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/local/vivurouter-go/internal/store"
)

func TestExecuteMediaUsesPathContentTypeAndCredential(t *testing.T) {
	var gotPath, gotType, gotAuth, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotType = r.Header.Get("Content-Type")
		gotAuth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		w.Header().Set("Content-Type", "audio/mpeg")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	executor := &OpenAIExecutor{Client: server.Client(), KeyPool: NewKeyPool()}
	result, err := executor.ExecuteMedia(context.Background(), store.Provider{
		ID:      "openai",
		BaseURL: server.URL + "/v1/chat/completions",
		APIKey:  "test-key",
	}, MediaRequest{Path: "/audio/speech", ContentType: "application/json", Body: strings.NewReader(`{"model":"tts"}`)})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Response.Body.Close()
	if gotPath != "/v1/audio/speech" || gotType != "application/json" || gotAuth != "Bearer test-key" || gotBody != `{"model":"tts"}` {
		t.Fatalf("unexpected request path=%q type=%q auth=%q body=%q", gotPath, gotType, gotAuth, gotBody)
	}
}

func TestExecuteMediaDispatchesExactlyOnce(t *testing.T) {
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	executor := &OpenAIExecutor{Client: server.Client(), KeyPool: NewKeyPool()}
	result, err := executor.ExecuteMedia(context.Background(), store.Provider{ID: "p", BaseURL: server.URL + "/v1", APIKey: "key"}, MediaRequest{Path: "/images/generations", ContentType: "application/json", Body: strings.NewReader(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	result.Response.Body.Close()
	if hits != 1 {
		t.Fatalf("upstream hits = %d, want 1", hits)
	}
}
