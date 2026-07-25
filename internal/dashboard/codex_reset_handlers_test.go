package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/local/vivurouter-go/internal/provider"
	"github.com/local/vivurouter-go/internal/store"
)

func TestCodexResetCreditsAPIGetAndNoCredit(t *testing.T) {
	var consumeID string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/credits":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"available_count": 1,
				"credits":         []map[string]any{{"status": "available", "expires_at": "2026-08-01T00:00:00Z"}},
			})
		case "/consume":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			consumeID = body["redeem_request_id"]
			_ = json.NewEncoder(w).Encode(map[string]any{"code": "no_credit", "windows_reset": 0})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	t.Setenv("CODEX_RESET_CREDITS_URL", upstream.URL+"/credits")
	t.Setenv("CODEX_RESET_CREDITS_CONSUME_URL", upstream.URL+"/consume")

	st, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertProvider(store.Provider{ID: "codex-test", Type: store.ProviderCodex, AccessToken: "secret-token", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	executors := provider.NewExecutorsWithStore(st)
	executors.Codex.Client = upstream.Client()
	h := &Handlers{store: st, executors: executors}

	get := httptest.NewRequest(http.MethodGet, "/api/codex/reset-credits?provider_id=codex-test", nil)
	get.RemoteAddr = "127.0.0.1:12345"
	getRecorder := httptest.NewRecorder()
	h.CodexResetCreditsAPI(getRecorder, get)
	if getRecorder.Code != http.StatusOK {
		t.Fatalf("GET status=%d body=%s", getRecorder.Code, getRecorder.Body.String())
	}
	if strings.Contains(getRecorder.Body.String(), "secret-token") {
		t.Fatal("response leaked access token")
	}
	var report struct {
		AvailableCount int `json:"available_count"`
	}
	if err := json.Unmarshal(getRecorder.Body.Bytes(), &report); err != nil || report.AvailableCount != 1 {
		t.Fatalf("unexpected GET response: %s err=%v", getRecorder.Body.String(), err)
	}

	post := httptest.NewRequest(http.MethodPost, "/api/codex/reset-credits?provider_id=codex-test", nil)
	post.RemoteAddr = "127.0.0.1:12345"
	postRecorder := httptest.NewRecorder()
	h.CodexResetCreditsAPI(postRecorder, post)
	if postRecorder.Code != http.StatusConflict {
		t.Fatalf("POST status=%d body=%s", postRecorder.Code, postRecorder.Body.String())
	}
	if consumeID == "" || !strings.Contains(consumeID, "-") {
		t.Fatalf("server did not generate a redeem request ID: %q", consumeID)
	}
}

func TestCodexResetTargetRejectsForeignAndDisabledAccounts(t *testing.T) {
	st, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []store.Provider{
		{ID: "codex-a", Type: store.ProviderCodex, AccessToken: "provider-a", Enabled: true},
		{ID: "codex-b", Type: store.ProviderCodex, AccessToken: "provider-b", Enabled: true},
	} {
		if err := st.UpsertProvider(item); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.UpsertProviderAccount(store.ProviderAccount{ID: "account-b", ProviderID: "codex-b", AccessToken: "account-token", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertProviderAccount(store.ProviderAccount{ID: "account-off", ProviderID: "codex-a", AccessToken: "account-token", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	h := &Handlers{store: st}

	foreign := httptest.NewRequest(http.MethodGet, "/?provider_id=codex-a&account_id=account-b", nil)
	if _, status, err := h.resolveCodexResetTarget(foreign); err == nil || status != http.StatusNotFound {
		t.Fatalf("foreign account status=%d err=%v", status, err)
	}
	disabled := httptest.NewRequest(http.MethodGet, "/?provider_id=codex-a&account_id=account-off", nil)
	if _, status, err := h.resolveCodexResetTarget(disabled); err == nil || status != http.StatusBadRequest {
		t.Fatalf("disabled account status=%d err=%v", status, err)
	}
}

func TestCodexResetCreditsAPIRequiresAdminAccess(t *testing.T) {
	st, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := &Handlers{store: st, executors: provider.NewExecutorsWithStore(st)}
	req := httptest.NewRequest(http.MethodGet, "/api/codex/reset-credits?provider_id=codex", nil)
	req.RemoteAddr = "203.0.113.10:12345"
	recorder := httptest.NewRecorder()
	h.CodexResetCreditsAPI(recorder, req)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
