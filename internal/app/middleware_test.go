package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCSRFBlocksCrossOriginPost(t *testing.T) {
	handler := csrfMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	handler := csrfMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	handler := csrfMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	handler := csrfMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

func TestCORSWildcardOnlyForGateway(t *testing.T) {
	handler := corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	gw := httptest.NewRequest(http.MethodGet, "http://router.local/v1/models", nil)
	gwRec := httptest.NewRecorder()
	handler.ServeHTTP(gwRec, gw)
	if got := gwRec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("gateway should allow wildcard CORS, got %q", got)
	}

	api := httptest.NewRequest(http.MethodGet, "http://router.local/api/config", nil)
	apiRec := httptest.NewRecorder()
	handler.ServeHTTP(apiRec, api)
	if got := apiRec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("api must not advertise cross-origin access, got %q", got)
	}
}
