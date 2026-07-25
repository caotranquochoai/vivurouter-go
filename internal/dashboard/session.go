package dashboard

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/local/vivurouter-go/internal/store"
)

// adminSecurityActive reports whether passcode protection is configured.
func (h *Handlers) adminSecurityActive(settings store.Settings) bool {
	return settings.AdminSecurityEnabled && strings.TrimSpace(settings.AdminPasscode) != ""
}

// hasAdminSession verifies the signed session cookie without exposing the passcode.
func (h *Handlers) hasAdminSession(r *http.Request, settings store.Settings) bool {
	cookie, err := r.Cookie("admin_session")
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(h.adminSessionValue(settings))) == 1
}

// requireAdmin guards HTML pages; it redirects to the login page when unauthorized.
func (h *Handlers) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	settings, err := h.store.GetSettings()
	if err != nil || !h.adminSecurityActive(settings) {
		return true
	}
	if h.hasAdminSession(r, settings) {
		return true
	}
	http.Redirect(w, r, "/admin/login", http.StatusFound)
	return false
}

// requireAdminAPI guards JSON/management endpoints; it returns 401 JSON instead of
// redirecting so browser fetch()/CLI callers get a clear error. When passcode
// protection is not configured it still requires a loopback client unless the
// gateway API key requirement is the active control, mirroring requireAdmin's
// "open until configured" behavior for local-only use.
// RequireAdminAPI protects dashboard-owned JSON endpoints.
func (h *Handlers) RequireAdminAPI(w http.ResponseWriter, r *http.Request) bool {
	return h.requireAdminAPI(w, r)
}

func (h *Handlers) requireAdminAPI(w http.ResponseWriter, r *http.Request) bool {
	settings, err := h.store.GetSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return false
	}
	if !h.adminSecurityActive(settings) {
		// No passcode configured: only allow same-machine callers so a remote
		// attacker cannot read/modify secrets on an unconfigured deployment.
		if !isLoopbackRequest(r) {
			writeError(w, http.StatusUnauthorized, "admin security is not configured; enable it in settings to manage this instance remotely")
			return false
		}
		return true
	}
	if h.hasAdminSession(r, settings) {
		return true
	}
	writeError(w, http.StatusUnauthorized, "admin authentication required")
	return false
}

// adminSessionValue derives an opaque, signed session token from the passcode.
// It never embeds the passcode itself, so a leaked cookie does not reveal it.
func (h *Handlers) adminSessionValue(settings store.Settings) string {
	mac := hmac.New(sha256.New, []byte(h.sessionSigningKey(settings)))
	mac.Write([]byte("admin-session-v1|" + settings.AdminPasscode))
	return "v1." + hex.EncodeToString(mac.Sum(nil))
}

// sessionSigningKey is a per-instance secret so session tokens cannot be forged
// from the passcode alone by an outside party.
func (h *Handlers) sessionSigningKey(settings store.Settings) string {
	return "vivurouter|" + settings.AdminPasscode + "|" + settings.LocalAPIKey
}

func (h *Handlers) setAdminSession(w http.ResponseWriter) {
	settings, _ := h.store.GetSettings()
	http.SetCookie(w, &http.Cookie{Name: "admin_session", Value: h.adminSessionValue(settings), Path: "/", MaxAge: 86400 * 7, HttpOnly: true, SameSite: http.SameSiteLaxMode})
}

// isLoopbackRequest reports whether the request originates from the local machine.
func isLoopbackRequest(r *http.Request) bool {
	host := r.RemoteAddr
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if host == "" {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return strings.EqualFold(host, "localhost")
}

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(buf)
}
