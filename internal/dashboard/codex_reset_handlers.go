package dashboard

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/local/vivurouter-go/internal/provider"
	"github.com/local/vivurouter-go/internal/store"
)

type codexResetTarget struct {
	provider  store.Provider
	accountID string
}

// CodexResetCreditsAPI reads reset-credit expiry details or consumes one credit.
func (h *Handlers) CodexResetCreditsAPI(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdminAPI(w, r) {
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	target, status, err := h.resolveCodexResetTarget(r)
	if err != nil {
		writeError(w, status, err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	target, err = h.refreshCodexResetTarget(ctx, target, false)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "Codex credential refresh failed: "+err.Error())
		return
	}

	if r.Method == http.MethodGet {
		report, err := h.executors.Codex.FetchResetCredits(ctx, target.provider)
		if isCodexAuthError(err) && target.provider.RefreshToken != "" {
			target, err = h.refreshCodexResetTarget(ctx, target, true)
			if err == nil {
				report, err = h.executors.Codex.FetchResetCredits(ctx, target.provider)
			}
		}
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		response := map[string]any{
			"provider_id":     target.provider.ID,
			"available_count": report.AvailableCount,
			"credits":         report.Credits,
			"fetched_at":      report.FetchedAt,
		}
		if target.accountID != "" {
			response["account_id"] = target.accountID
		}
		writeJSON(w, http.StatusOK, response)
		return
	}

	redeemRequestID, err := newRedeemRequestID()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create redeem request id")
		return
	}
	result, err := h.executors.Codex.ConsumeResetCredit(ctx, target.provider, redeemRequestID)
	if err == nil && (result.Status == http.StatusUnauthorized || result.Status == http.StatusForbidden) && target.provider.RefreshToken != "" {
		target, err = h.refreshCodexResetTarget(ctx, target, true)
		if err == nil {
			result, err = h.executors.Codex.ConsumeResetCredit(ctx, target.provider, redeemRequestID)
		}
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if result.OK {
		writeJSON(w, http.StatusOK, map[string]any{
			"code":              result.Code,
			"reset":             true,
			"windows_reset":     result.WindowsReset,
			"redeem_request_id": redeemRequestID,
		})
		return
	}
	if result.NoCredit {
		writeJSON(w, http.StatusConflict, map[string]any{
			"code":          "no_credit",
			"reset":         false,
			"windows_reset": result.WindowsReset,
			"message":       "No Codex reset credits available.",
		})
		return
	}
	status = http.StatusBadGateway
	if result.Status >= 400 && result.Status < 500 {
		status = result.Status
	}
	writeJSON(w, status, map[string]any{
		"code":          fallbackCodexCode(result.Code),
		"reset":         false,
		"windows_reset": result.WindowsReset,
		"message":       fallbackCodexMessage(result.Message),
	})
}

func (h *Handlers) resolveCodexResetTarget(r *http.Request) (codexResetTarget, int, error) {
	providerID := strings.TrimSpace(r.URL.Query().Get("provider_id"))
	accountID := strings.TrimSpace(r.URL.Query().Get("account_id"))
	if providerID == "" {
		return codexResetTarget{}, http.StatusBadRequest, errors.New("missing provider_id")
	}
	configured, found, err := h.store.GetProvider(providerID)
	if err != nil {
		return codexResetTarget{}, http.StatusInternalServerError, err
	}
	if !found {
		return codexResetTarget{}, http.StatusNotFound, errors.New("provider not found")
	}
	if configured.Type != store.ProviderCodex {
		return codexResetTarget{}, http.StatusBadRequest, errors.New("provider is not a Codex provider")
	}
	if accountID == "" {
		if strings.TrimSpace(configured.AccessToken) == "" && strings.TrimSpace(configured.APIKey) == "" {
			return codexResetTarget{}, http.StatusBadRequest, errors.New("Codex provider has no access token")
		}
		return codexResetTarget{provider: configured}, http.StatusOK, nil
	}
	account, found, err := h.store.GetProviderAccount(accountID)
	if err != nil {
		return codexResetTarget{}, http.StatusInternalServerError, err
	}
	if !found || account.ProviderID != providerID {
		return codexResetTarget{}, http.StatusNotFound, errors.New("account not found")
	}
	if !account.Enabled {
		return codexResetTarget{}, http.StatusBadRequest, errors.New("account is disabled")
	}
	configured.APIKey = account.APIKey
	configured.AccessToken = account.AccessToken
	configured.RefreshToken = account.RefreshToken
	if account.ProxyID != "" || account.ProxyURL != "" {
		configured.ProxyID = account.ProxyID
		configured.ProxyURL = account.ProxyURL
	}
	if strings.TrimSpace(configured.AccessToken) == "" && strings.TrimSpace(configured.APIKey) == "" {
		return codexResetTarget{}, http.StatusBadRequest, errors.New("Codex account has no access token")
	}
	return codexResetTarget{provider: configured, accountID: accountID}, http.StatusOK, nil
}

func (h *Handlers) refreshCodexResetTarget(ctx context.Context, target codexResetTarget, force bool) (codexResetTarget, error) {
	if strings.TrimSpace(target.provider.RefreshToken) == "" {
		return target, nil
	}
	// A normal preflight refresh matches 9Router. force is retained to make the
	// single auth-failure retry explicit at call sites.
	_ = force
	refreshed, err := h.executors.Codex.RefreshCodexTokenForAccount(ctx, target.provider, target.accountID)
	if err != nil {
		return target, err
	}
	target.provider = refreshed
	return target, nil
}

func isCodexAuthError(err error) bool {
	var upstream *provider.CodexUpstreamError
	return errors.As(err, &upstream) && (upstream.Status == http.StatusUnauthorized || upstream.Status == http.StatusForbidden)
}

func newRedeemRequestID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	hexID := hex.EncodeToString(raw[:])
	return fmt.Sprintf("%s-%s-%s-%s-%s", hexID[:8], hexID[8:12], hexID[12:16], hexID[16:20], hexID[20:]), nil
}

func fallbackCodexCode(code string) string {
	if strings.TrimSpace(code) == "" {
		return "unknown_response"
	}
	return code
}

func fallbackCodexMessage(message string) string {
	if strings.TrimSpace(message) == "" {
		return "Codex reset credit consume returned an unexpected response."
	}
	return message
}
