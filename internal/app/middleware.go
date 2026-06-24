package app

import (
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/local/vivurouter-go/internal/observe"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// Flush forwards to the underlying writer so SSE streaming keeps working
// when wrapped by the metrics/logging recorder.
func (r *statusRecorder) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func metricsMiddleware(obs *observe.State, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		done := obs.Metrics.BeginRequest()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		done(recorder.status)
	})
}

func recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				log.Printf("panic %s %s: %v", r.Method, r.URL.Path, recovered)
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, recorder.status, time.Since(started).Round(time.Millisecond))
	})
}

// corsMiddleware enables permissive CORS only for the OpenAI-compatible gateway
// endpoints, which are authenticated by API key and meant to be called from
// arbitrary clients. Dashboard pages and /api/* management endpoints are
// same-origin only: exposing them with `Access-Control-Allow-Origin: *` would
// let any website read an operator's secrets via the browser.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isPublicGatewayPath(r.URL.Path) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization,Content-Type,x-api-key")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		} else if r.Method == http.MethodOptions {
			// Same-origin preflight: do not advertise cross-origin access.
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isPublicGatewayPath(path string) bool {
	return strings.HasPrefix(path, "/v1/") || strings.HasPrefix(path, "/codex/")
}

// csrfMiddleware blocks cross-site state-changing requests to the dashboard and
// management API. Combined with the SameSite=Lax session cookie this defends
// admin forms against CSRF without per-form tokens. The public /v1 gateway is
// exempt because it is authenticated by API key and called cross-origin by CLIs.
func csrfMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isStateChangingMethod(r.Method) && !isPublicGatewayPath(r.URL.Path) {
			if !sameOriginRequest(r) {
				http.Error(w, "cross-origin request blocked", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func isStateChangingMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch:
		return true
	default:
		return false
	}
}

// sameOriginRequest verifies the Origin (or Referer) header host matches the
// host the request was sent to. Missing both headers falls back to the
// SameSite=Lax cookie protection and is allowed so non-browser clients work.
func sameOriginRequest(r *http.Request) bool {
	expected := r.Host
	if fwd := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); fwd != "" {
		expected = fwd
	}
	for _, header := range []string{"Origin", "Referer"} {
		value := strings.TrimSpace(r.Header.Get(header))
		if value == "" {
			continue
		}
		parsed, err := url.Parse(value)
		if err != nil || parsed.Host == "" {
			return false
		}
		return strings.EqualFold(parsed.Host, expected)
	}
	return true
}
