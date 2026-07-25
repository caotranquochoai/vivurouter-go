package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/local/vivurouter-go/internal/provider"
	"github.com/local/vivurouter-go/internal/store"
)

func TestProviderAccountAntigravityQuotaRefreshIsScoped(t *testing.T) {
	quotaCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "new-account-token"})
		case "/load":
			if r.Header.Get("Authorization") == "Bearer old-account-token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"cloudaicompanionProject": map[string]any{"id": "project-1"}})
		case "/quota":
			quotaCalls++
			if r.Header.Get("Authorization") != "Bearer new-account-token" {
				t.Fatalf("quota authorization = %q", r.Header.Get("Authorization"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"models": map[string]any{
				"gemini-3-flash-agent": map[string]any{"quotaInfo": map[string]any{"remainingFraction": 0.5}},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	t.Setenv("ANTIGRAVITY_LOAD_PROJECT_URL", upstream.URL+"/load")
	t.Setenv("ANTIGRAVITY_QUOTA_URL", upstream.URL+"/quota")

	st, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertProvider(store.Provider{ID: "ag", Type: store.ProviderAntigravity, AccessToken: "provider-token", RefreshToken: "provider-refresh", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertProviderAccount(store.ProviderAccount{ID: "account-a", ProviderID: "ag", Name: "A", AccessToken: "old-account-token", RefreshToken: "account-refresh", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	executors := provider.NewExecutorsWithStore(st)
	executors.Antigravity.Client = upstream.Client()
	executors.Antigravity.TokenURL = upstream.URL + "/token"
	h := &Handlers{store: st, executors: executors}

	req := httptest.NewRequest(http.MethodGet, "/api/provider-accounts/quota?provider_id=ag&account_id=account-a", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	recorder := httptest.NewRecorder()
	h.ProviderAccountQuotaAPI(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	account, _, _ := st.GetProviderAccount("account-a")
	storedProvider, _, _ := st.GetProvider("ag")
	if account.AccessToken != "new-account-token" || storedProvider.AccessToken != "provider-token" || quotaCalls != 1 {
		t.Fatalf("account=%#v provider=%#v quotaCalls=%d", account, storedProvider, quotaCalls)
	}
}
