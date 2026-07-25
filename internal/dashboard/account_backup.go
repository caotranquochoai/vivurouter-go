package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/local/vivurouter-go/internal/backupcrypto"
	"github.com/local/vivurouter-go/internal/store"
)

const accountBackupKind = "vivurouter.provider-accounts"

type accountBackup struct {
	Kind       string                  `json:"kind"`
	Version    int                     `json:"version"`
	Security   accountBackupSecurity   `json:"security"`
	ExportedAt time.Time               `json:"exported_at"`
	Provider   accountBackupProvider   `json:"provider"`
	Accounts   []store.ProviderAccount `json:"accounts"`
	Proxies    []store.Proxy           `json:"proxies"`
}

type accountBackupSecurity struct {
	Encrypted bool   `json:"encrypted"`
	Warning   string `json:"warning,omitempty"`
}

type accountBackupProvider struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

type accountBackupRequest struct {
	ProviderID       string   `json:"provider_id"`
	AccountIDs       []string `json:"account_ids"`
	Scope            string   `json:"scope"`
	Format           string   `json:"format"`
	Passphrase       string   `json:"passphrase"`
	ConfirmPlaintext bool     `json:"confirm_plaintext"`
}

type accountBackupPreviewRequest struct {
	Bundle           json.RawMessage `json:"bundle"`
	Passphrase       string          `json:"passphrase"`
	ConfirmPlaintext bool            `json:"confirm_plaintext"`
}

type accountBackupImportRequest struct {
	Bundle           json.RawMessage `json:"bundle"`
	Passphrase       string          `json:"passphrase"`
	ConfirmPlaintext bool            `json:"confirm_plaintext"`
	ProviderID       string          `json:"provider_id"`
	Policy           string          `json:"policy"`
	Confirm          string          `json:"confirm"`
}

type accountBackupPreview struct {
	Kind       string `json:"kind"`
	Encrypted  bool   `json:"encrypted"`
	ProviderID string `json:"provider_id"`
	ProviderTy string `json:"provider_type"`
	Accounts   []struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		AuthType string `json:"auth_type"`
		Enabled  bool   `json:"enabled"`
		Priority int    `json:"priority"`
		HasProxy bool   `json:"has_proxy"`
	} `json:"accounts"`
	ProxyCount int `json:"proxy_count"`
}

func (h *Handlers) ProviderAccountsExportAPI(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdminAPI(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var input accountBackupRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid backup request")
		return
	}
	backup, err := h.resolveAccountBackup(input.ProviderID, input.AccountIDs, input.Scope)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	format := strings.ToLower(strings.TrimSpace(input.Format))
	var output []byte
	filename := "vivurouter-accounts-" + safeDownloadName(backup.Provider.ID)
	switch format {
	case "encrypted", "":
		plaintext, err := json.Marshal(backup)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "cannot prepare backup")
			return
		}
		envelope, err := backupcrypto.Encrypt(plaintext, input.Passphrase)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid encrypted backup request")
			return
		}
		output, err = backupcrypto.Marshal(envelope)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "cannot prepare backup")
			return
		}
		filename += ".vrbackup"
	case "json":
		if !input.ConfirmPlaintext {
			writeError(w, http.StatusBadRequest, "plaintext export requires explicit confirmation")
			return
		}
		backup.Security = accountBackupSecurity{Encrypted: false, Warning: "Contains plaintext credentials"}
		var err error
		output, err = json.Marshal(backup)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "cannot prepare backup")
			return
		}
		filename += "-UNENCRYPTED.json"
	default:
		writeError(w, http.StatusBadRequest, "unsupported backup format")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename="+fmt.Sprintf("%q", filename))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(output)
}

func (h *Handlers) resolveAccountBackup(providerID string, ids []string, scope string) (accountBackup, error) {
	provider, found, err := h.store.GetProvider(strings.TrimSpace(providerID))
	if err != nil || !found {
		return accountBackup{}, fmt.Errorf("provider not found")
	}
	all, err := h.store.ListProviderAccounts(provider.ID)
	if err != nil {
		return accountBackup{}, err
	}
	wanted := map[string]bool{}
	if strings.EqualFold(strings.TrimSpace(scope), "pool") {
		for _, a := range all {
			wanted[a.ID] = true
		}
	} else {
		for _, id := range ids {
			if id = strings.TrimSpace(id); id != "" {
				wanted[id] = true
			}
		}
	}
	if len(wanted) == 0 {
		return accountBackup{}, fmt.Errorf("select at least one account")
	}
	out := accountBackup{Kind: accountBackupKind, Version: 1, Security: accountBackupSecurity{Encrypted: true}, ExportedAt: time.Now().UTC(), Provider: accountBackupProvider{ID: provider.ID, Type: provider.Type}}
	proxyIDs := map[string]bool{}
	for _, a := range all {
		if wanted[a.ID] {
			out.Accounts = append(out.Accounts, a)
			if a.ProxyID != "" {
				proxyIDs[a.ProxyID] = true
			}
		}
	}
	if len(out.Accounts) != len(wanted) {
		return accountBackup{}, fmt.Errorf("account does not belong to provider")
	}
	for id := range proxyIDs {
		p, ok, err := h.store.GetProxy(id)
		if err != nil {
			return accountBackup{}, err
		}
		if !ok {
			return accountBackup{}, fmt.Errorf("referenced proxy not found")
		}
		out.Proxies = append(out.Proxies, p)
	}
	return out, nil
}

func (h *Handlers) ProviderAccountsImportPreviewAPI(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdminAPI(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	backup, err := decodeAccountBackup(w, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid backup or passphrase")
		return
	}
	writeJSON(w, http.StatusOK, redactedAccountBackupPreview(backup))
}

func (h *Handlers) ProviderAccountsImportAPI(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdminAPI(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var input accountBackupImportRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 5<<20)).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid import request")
		return
	}
	backup, err := decodeAccountBackupPayload(input.Bundle, input.Passphrase, input.ConfirmPlaintext)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid backup or passphrase")
		return
	}
	providerID := strings.TrimSpace(input.ProviderID)
	if providerID == "" {
		providerID = backup.Provider.ID
	}
	provider, found, err := h.store.GetProvider(providerID)
	if err != nil || !found || provider.Type != backup.Provider.Type {
		writeError(w, http.StatusBadRequest, "target provider is incompatible")
		return
	}
	policy := strings.ToLower(strings.TrimSpace(input.Policy))
	if policy == "" {
		policy = "skip"
	}
	if policy != "skip" && policy != "replace" {
		writeError(w, http.StatusBadRequest, "unsupported conflict policy")
		return
	}
	if policy == "replace" && input.Confirm != "REPLACE" {
		writeError(w, http.StatusBadRequest, "replace requires confirmation")
		return
	}
	imported, skipped := 0, 0
	for _, proxy := range backup.Proxies {
		if _, exists, _ := h.store.GetProxy(proxy.ID); exists && policy == "skip" {
			skipped++
			continue
		}
		if err := h.store.UpsertProxy(proxy); err != nil {
			writeError(w, http.StatusBadRequest, "backup proxy is invalid")
			return
		}
	}
	for _, account := range backup.Accounts {
		account.ProviderID = provider.ID
		if _, exists, _ := h.store.GetProviderAccount(account.ID); exists && policy == "skip" {
			skipped++
			continue
		}
		if err := h.store.UpsertProviderAccount(account); err != nil {
			writeError(w, http.StatusBadRequest, "backup account is invalid")
			return
		}
		imported++
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "imported": imported, "skipped": skipped})
}

func decodeAccountBackup(w http.ResponseWriter, r *http.Request) (accountBackup, error) {
	var input accountBackupPreviewRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 5<<20)).Decode(&input); err != nil {
		return accountBackup{}, err
	}
	return decodeAccountBackupPayload(input.Bundle, input.Passphrase, input.ConfirmPlaintext)
}
func decodeAccountBackupPayload(raw json.RawMessage, passphrase string, confirmPlaintext bool) (accountBackup, error) {
	var envelope backupcrypto.Envelope
	if json.Unmarshal(raw, &envelope) == nil && envelope.Kind == backupcrypto.Kind {
		plaintext, err := backupcrypto.Decrypt(envelope, passphrase)
		if err != nil {
			return accountBackup{}, err
		}
		raw = plaintext
	} else if !confirmPlaintext {
		return accountBackup{}, fmt.Errorf("plaintext import requires explicit confirmation")
	}
	var backup accountBackup
	if err := json.Unmarshal(raw, &backup); err != nil || backup.Kind != accountBackupKind || backup.Version != 1 || len(backup.Accounts) == 0 {
		return accountBackup{}, fmt.Errorf("invalid backup")
	}
	return backup, nil
}
func redactedAccountBackupPreview(backup accountBackup) accountBackupPreview {
	out := accountBackupPreview{Kind: backup.Kind, Encrypted: backup.Security.Encrypted, ProviderID: backup.Provider.ID, ProviderTy: backup.Provider.Type, ProxyCount: len(backup.Proxies)}
	for _, a := range backup.Accounts {
		out.Accounts = append(out.Accounts, struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			AuthType string `json:"auth_type"`
			Enabled  bool   `json:"enabled"`
			Priority int    `json:"priority"`
			HasProxy bool   `json:"has_proxy"`
		}{a.ID, a.Name, a.AuthType, a.Enabled, a.Priority, a.ProxyID != "" || a.ProxyURL != ""})
	}
	return out
}
