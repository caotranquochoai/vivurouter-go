package app

import (
	"log"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/local/vivurouter-go/internal/config"
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

// corsMiddleware only grants cross-origin access to explicitly configured
// origins on gateway endpoints. A valid API key is not a reason to let arbitrary
// browser origins invoke a gateway with the user's credentials.
func corsMiddleware(cfg config.Config, next http.Handler) http.Handler {
	allowedOrigins := make(map[string]struct{}, len(cfg.GatewayCORSOrigins))
	for _, origin := range cfg.GatewayCORSOrigins {
		if origin = normalizeOrigin(origin); origin != "" {
			allowedOrigins[origin] = struct{}{}
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isPublicGatewayPath(r.URL.Path) {
			if r.Method == http.MethodOptions {
				// Same-origin preflight: do not advertise cross-origin access.
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
			return
		}

		origin := normalizeOrigin(r.Header.Get("Origin"))
		_, originAllowed := allowedOrigins[origin]
		if originAllowed {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization,Content-Type,x-api-key")
		}
		if r.Method == http.MethodOptions {
			if !originAllowed {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func normalizeOrigin(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.Path != "" && parsed.Path != "/" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return ""
	}
	return strings.ToLower(parsed.Scheme) + "://" + strings.ToLower(parsed.Host)
}

func isPublicGatewayPath(path string) bool {
	return strings.HasPrefix(path, "/v1/") || strings.HasPrefix(path, "/codex/")
}

// csrfMiddleware blocks cross-site state-changing requests to the dashboard and
// management API. Combined with the SameSite=Lax session cookie this defends
// admin forms against CSRF without per-form tokens. The public /v1 gateway is
// exempt because it is authenticated by API key and called cross-origin by CLIs.
func csrfMiddleware(cfg config.Config, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isStateChangingMethod(r.Method) && !isPublicGatewayPath(r.URL.Path) {
			if !sameOriginRequest(cfg, r) {
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
// host the request was sent to. Forwarded host headers are honored only from a
// configured reverse-proxy peer. Missing both headers falls back to the
// SameSite=Lax cookie protection and is allowed so non-browser clients work.
func sameOriginRequest(cfg config.Config, r *http.Request) bool {
	expected := r.Host
	if fwd := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); fwd != "" && isTrustedProxy(cfg, r.RemoteAddr) {
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

func isTrustedProxy(cfg config.Config, remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip, err := netip.ParseAddr(strings.TrimSpace(host))
	if err != nil {
		return false
	}
	for _, rawCIDR := range cfg.TrustedProxyCIDRs {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(rawCIDR))
		if err == nil && prefix.Contains(ip) {
			return true
		}
	}
	return false
}
