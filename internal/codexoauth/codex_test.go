package codexoauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/local/vivurouter-go/internal/store"
)

func TestBuildAuthURLMatchesCodexCLIShape(t *testing.T) {
	authURL := BuildAuthURL(RedirectURI, "state-123", "challenge-456")
	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("parse auth url: %v", err)
	}
	if parsed.Scheme+"://"+parsed.Host+parsed.Path != AuthorizeURL {
		t.Fatalf("authorize base = %s", parsed.Scheme+"://"+parsed.Host+parsed.Path)
	}
	q := parsed.Query()
	want := map[string]string{
		"response_type":              "code",
		"client_id":                  ClientID,
		"redirect_uri":               RedirectURI,
		"scope":                      Scope,
		"code_challenge":             "challenge-456",
		"code_challenge_method":      "S256",
		"id_token_add_organizations": "true",
		"codex_cli_simplified_flow":  "true",
		"originator":                 "codex_cli_rs",
		"state":                      "state-123",
	}
	for key, expected := range want {
		if got := q.Get(key); got != expected {
			t.Fatalf("%s = %q, want %q", key, got, expected)
		}
	}
	if strings.Contains(authURL, "+") {
		t.Fatalf("auth URL should encode spaces as %%20, got %s", authURL)
	}
}

func TestGeneratePKCE(t *testing.T) {
	verifier, challenge, state, err := GeneratePKCE()
	if err != nil {
		t.Fatalf("generate pkce: %v", err)
	}
	if verifier == "" || challenge == "" || state == "" {
		t.Fatalf("empty pkce values verifier=%q challenge=%q state=%q", verifier, challenge, state)
	}
	if strings.ContainsAny(verifier+challenge+state, "+/=") {
		t.Fatalf("pkce values should be raw URL-safe base64")
	}
}

func TestExchangeTokenPostsCodexForm(t *testing.T) {
	var seenForm url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		if got := r.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
			t.Fatalf("content-type = %q", got)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Fatalf("accept = %q", got)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		seenForm = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(tokenResponse{AccessToken: "access-token", RefreshToken: "refresh-token", IDToken: "id-token", ExpiresIn: 3600, Scope: Scope})
	}))
	defer server.Close()

	manager := &Manager{client: server.Client(), tokenURL: server.URL}
	tokens, err := manager.exchangeToken(context.Background(), "code-abc", "verifier-xyz", "")
	if err != nil {
		t.Fatalf("exchange token: %v", err)
	}
	if tokens.AccessToken != "access-token" || tokens.RefreshToken != "refresh-token" || tokens.IDToken != "id-token" {
		t.Fatalf("unexpected tokens: %+v", tokens)
	}

	want := map[string]string{"grant_type": "authorization_code", "client_id": ClientID, "code": "code-abc", "redirect_uri": RedirectURI, "code_verifier": "verifier-xyz"}
	for key, expected := range want {
		if got := seenForm.Get(key); got != expected {
			t.Fatalf("form %s = %q, want %q", key, got, expected)
		}
	}
}

func TestStartRejectsAccountFromAnotherProvider(t *testing.T) {
	st, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	if err := st.UpsertProvider(store.Provider{ID: "codex-a", Type: store.ProviderCodex, Enabled: true}); err != nil {
		t.Fatalf("save provider: %v", err)
	}
	if err := st.UpsertProvider(store.Provider{ID: "codex-b", Type: store.ProviderCodex, Enabled: true}); err != nil {
		t.Fatalf("save provider: %v", err)
	}
	if err := st.UpsertProviderAccount(store.ProviderAccount{ID: "account-b", ProviderID: "codex-b", Name: "Account B", Enabled: true}); err != nil {
		t.Fatalf("save account: %v", err)
	}
	if _, err := NewManager(st).Start("codex-a", "account-b", ""); err == nil {
		t.Fatal("expected cross-provider account to be rejected")
	}
}

func TestSaveTokensTargetsProviderAccount(t *testing.T) {
	st, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	provider := store.Provider{ID: "codex", Type: store.ProviderCodex, Enabled: true}
	if err := st.UpsertProvider(provider); err != nil {
		t.Fatalf("save provider: %v", err)
	}
	account := store.ProviderAccount{ID: "account-a", ProviderID: provider.ID, Name: "Account A", AuthType: "oauth", AccessToken: "old-access", RefreshToken: "old-refresh", Enabled: true}
	if err := st.UpsertProviderAccount(account); err != nil {
		t.Fatalf("save account: %v", err)
	}
	manager := NewManager(st)
	if err := manager.saveTokens(provider.ID, account.ID, "http://proxy.example:8080", tokenResponse{AccessToken: "new-access", RefreshToken: "new-refresh"}); err != nil {
		t.Fatalf("save account tokens: %v", err)
	}
	got, found, err := st.GetProviderAccount(account.ID)
	if err != nil || !found {
		t.Fatalf("get account found=%v err=%v", found, err)
	}
	if got.AccessToken != "new-access" || got.RefreshToken != "new-refresh" || got.ProxyURL != "http://proxy.example:8080" || !got.Enabled {
		t.Fatalf("account was not updated: %+v", got)
	}
	storedProvider, found, err := st.GetProvider(provider.ID)
	if err != nil || !found {
		t.Fatalf("get provider found=%v err=%v", found, err)
	}
	if storedProvider.AccessToken != "" || storedProvider.RefreshToken != "" {
		t.Fatalf("account OAuth must not change provider fallback: %+v", storedProvider)
	}
}
