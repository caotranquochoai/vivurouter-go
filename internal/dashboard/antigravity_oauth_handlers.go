package dashboard

import (
	"encoding/json"
	"net/http"
	"strings"
)

func (h *Handlers) AntigravityOAuthStartAPI(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdminAPI(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	providerID := strings.TrimSpace(r.URL.Query().Get("provider_id"))
	if providerID == "" {
		_ = r.ParseForm()
		providerID = strings.TrimSpace(r.FormValue("provider_id"))
	}
	proxyURL := strings.TrimSpace(r.URL.Query().Get("proxy_url"))
	if proxyURL == "" {
		_ = r.ParseForm()
		proxyURL = strings.TrimSpace(r.FormValue("proxy_url"))
	}
	status, err := h.antigravityOAuth.Start(providerID, proxyURL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if strings.Contains(r.Header.Get("Accept"), "application/json") || r.URL.Query().Get("json") == "1" {
		writeJSON(w, http.StatusOK, status)
		return
	}
	http.Redirect(w, r, status.AuthURL, http.StatusFound)
}

func (h *Handlers) AntigravityOAuthCompleteAPI(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdminAPI(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var payload struct {
		CallbackURL string `json:"callback_url"`
	}
	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		_ = json.NewDecoder(r.Body).Decode(&payload)
	} else {
		_ = r.ParseForm()
		payload.CallbackURL = r.FormValue("callback_url")
	}
	if strings.TrimSpace(payload.CallbackURL) == "" {
		writeError(w, http.StatusBadRequest, "missing callback URL")
		return
	}
	if err := h.antigravityOAuth.CompleteWithCallbackURL(r.Context(), payload.CallbackURL); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": h.antigravityOAuth.Status()})
}

func (h *Handlers) AntigravityOAuthStatusAPI(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdminAPI(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, h.antigravityOAuth.Status())
}
