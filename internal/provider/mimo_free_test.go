package provider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/local/vivurouter-go/internal/store"
)

func TestMimoInjectSystemMarker(t *testing.T) {
	body := map[string]any{"messages": []any{map[string]any{"role": "user", "content": "hi"}}}
	out := injectMimoSystemMarker(body)
	messages := out["messages"].([]any)
	if len(messages) != 2 {
		t.Fatalf("messages = %d", len(messages))
	}
	first := messages[0].(map[string]any)
	if first["role"] != "system" || !strings.Contains(first["content"].(string), mimoSystemMarker) {
		t.Fatalf("first message = %#v", first)
	}

	out = injectMimoSystemMarker(out)
	if got := len(out["messages"].([]any)); got != 2 {
		t.Fatalf("marker injected twice, len = %d", got)
	}
}

func TestMimoInjectSystemMarkerNormalizesDashboardMessages(t *testing.T) {
	body := map[string]any{"messages": []map[string]string{{"role": "user", "content": "hi"}}}
	out := injectMimoSystemMarker(body)
	messages := out["messages"].([]any)
	if len(messages) != 2 {
		t.Fatalf("messages = %d", len(messages))
	}
	first := messages[0].(map[string]any)
	if first["role"] != "system" || !strings.Contains(first["content"].(string), mimoSystemMarker) {
		t.Fatalf("first message = %#v", first)
	}
	second := messages[1].(map[string]any)
	if second["role"] != "user" || second["content"] != "hi" {
		t.Fatalf("second message = %#v", second)
	}
}

func TestParseMimoJWTExp(t *testing.T) {
	now := time.Unix(100, 0)
	exp := now.Add(time.Hour).Unix()
	payload, _ := json.Marshal(map[string]int64{"exp": exp})
	jwt := "x." + base64.RawURLEncoding.EncodeToString(payload) + ".y"
	if got := parseMimoJWTExp(jwt, now); !got.Equal(time.Unix(exp, 0)) {
		t.Fatalf("exp = %s", got)
	}
	if got := parseMimoJWTExp("bad", now); !got.Equal(now.Add(mimoJWTFallbackTTL)) {
		t.Fatalf("fallback = %s", got)
	}
}

func TestMimoBootstrapCachesJWTAndSendsHeaders(t *testing.T) {
	jwt := testJWT(time.Now().Add(time.Hour))
	bootstrapCalls := 0
	var gotAuth, gotSource, gotSession, gotAccept string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bootstrap":
			bootstrapCalls++
			_, _ = fmt.Fprintf(w, `{"jwt":%q}`, jwt)
		case "/chat":
			gotAuth = r.Header.Get("Authorization")
			gotSource = r.Header.Get("X-Mimo-Source")
			gotSession = r.Header.Get("x-session-affinity")
			gotAccept = r.Header.Get("Accept")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
		default:
			t.Fatalf("path = %s", r.URL.Path)
		}
	}))
	defer server.Close()

	executor := &MimoFreeExecutor{Client: server.Client(), BootstrapURL: server.URL + "/bootstrap", SessionID: "ses_test"}
	provider := store.Provider{ID: "mimo-free", Type: store.ProviderMimoFree, BaseURL: server.URL + "/chat"}
	for i := 0; i < 2; i++ {
		result, err := executor.ExecuteChat(context.Background(), provider, "mimo-auto", map[string]any{"stream": true, "messages": []any{map[string]any{"role": "user", "content": "hi"}}})
		if err != nil {
			t.Fatalf("ExecuteChat %d: %v", i, err)
		}
		result.Response.Body.Close()
	}
	if bootstrapCalls != 1 {
		t.Fatalf("bootstrapCalls = %d", bootstrapCalls)
	}
	if gotAuth != "Bearer "+jwt {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	if gotSource != mimoSourceHeader || gotSession != "ses_test" || gotAccept != "text/event-stream" {
		t.Fatalf("headers source=%q session=%q accept=%q", gotSource, gotSession, gotAccept)
	}
}

func TestMimoRetriesOnAuthFailure(t *testing.T) {
	bootstrapCalls := 0
	chatCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bootstrap":
			bootstrapCalls++
			_, _ = fmt.Fprintf(w, `{"jwt":%q}`, testJWT(time.Now().Add(time.Hour)))
		case "/chat":
			chatCalls++
			if chatCalls == 1 {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
		default:
			t.Fatalf("path = %s", r.URL.Path)
		}
	}))
	defer server.Close()

	executor := &MimoFreeExecutor{Client: server.Client(), BootstrapURL: server.URL + "/bootstrap", SessionID: "ses_test"}
	provider := store.Provider{ID: "mimo-free", Type: store.ProviderMimoFree, BaseURL: server.URL + "/chat"}
	result, err := executor.ExecuteChat(context.Background(), provider, "mimo-auto", map[string]any{"messages": []any{map[string]any{"role": "user", "content": "hi"}}})
	if err != nil {
		t.Fatalf("ExecuteChat: %v", err)
	}
	result.Response.Body.Close()
	if bootstrapCalls != 2 || chatCalls != 2 {
		t.Fatalf("bootstrapCalls=%d chatCalls=%d", bootstrapCalls, chatCalls)
	}
}

func testJWT(exp time.Time) string {
	payload, _ := json.Marshal(map[string]int64{"exp": exp.Unix()})
	return "x." + base64.RawURLEncoding.EncodeToString(payload) + ".y"
}
