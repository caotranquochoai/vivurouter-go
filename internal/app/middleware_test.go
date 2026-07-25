package app

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/local/vivurouter-go/internal/config"
)

func TestCSRFBlocksCrossOriginPost(t *testing.T) {
	handler := csrfMiddleware(config.Config{}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodPost, "http://router.local/api/config", nil)
	req.Host = "router.local"
	req.Header.Set("Origin", "http://evil.example")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for cross-origin POST, got %d", rec.Code)
	}
}

func TestCSRFAllowsSameOriginPost(t *testing.T) {
	handler := csrfMiddleware(config.Config{}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodPost, "http://router.local/api/config", nil)
	req.Host = "router.local"
	req.Header.Set("Origin", "http://router.local")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for same-origin POST, got %d", rec.Code)
	}
}

func TestCSRFExemptsGateway(t *testing.T) {
	handler := csrfMiddleware(config.Config{}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodPost, "http://router.local/v1/chat/completions", nil)
	req.Host = "router.local"
	req.Header.Set("Origin", "http://some-cli-tool.example")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("gateway should be exempt from CSRF origin check, got %d", rec.Code)
	}
}

func TestCSRFAllowsGetWithoutOrigin(t *testing.T) {
	handler := csrfMiddleware(config.Config{}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "http://router.local/dashboard", nil)
	req.Host = "router.local"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET should pass, got %d", rec.Code)
	}
}

func TestCORSRequiresExplicitGatewayOrigin(t *testing.T) {
	handler := corsMiddleware(config.Config{}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	gw := httptest.NewRequest(http.MethodGet, "http://router.local/v1/models", nil)
	gw.Header.Set("Origin", "https://client.example")
	gwRec := httptest.NewRecorder()
	handler.ServeHTTP(gwRec, gw)
	if got := gwRec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("gateway must not grant wildcard/default CORS, got %q", got)
	}

	api := httptest.NewRequest(http.MethodGet, "http://router.local/api/config", nil)
	api.Header.Set("Origin", "https://client.example")
	apiRec := httptest.NewRecorder()
	handler.ServeHTTP(apiRec, api)
	if got := apiRec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("api must not advertise cross-origin access, got %q", got)
	}
}

func TestCORSAllowsConfiguredGatewayOrigin(t *testing.T) {
	cfg := config.Config{GatewayCORSOrigins: []string{"https://client.example"}}
	handler := corsMiddleware(cfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodOptions, "http://router.local/v1/chat/completions", nil)
	req.Header.Set("Origin", "https://client.example")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://client.example" {
		t.Fatalf("configured origin not granted, got %q", got)
	}
}

func TestCSRFTrustsForwardedHostOnlyFromTrustedProxy(t *testing.T) {
	cfg := config.Config{TrustedProxyCIDRs: []string{"127.0.0.0/8"}}
	req := httptest.NewRequest(http.MethodPost, "http://router.local/api/config", nil)
	req.Host = "router.local"
	req.RemoteAddr = "198.51.100.1:1234"
	req.Header.Set("Origin", "https://trusted.example")
	req.Header.Set("X-Forwarded-Host", "trusted.example")
	if sameOriginRequest(cfg, req) {
		t.Fatal("untrusted client must not control X-Forwarded-Host")
	}
	req.RemoteAddr = "127.0.0.1:1234"
	if !sameOriginRequest(cfg, req) {
		t.Fatal("configured proxy should be able to provide X-Forwarded-Host")
	}
}
