package codexoauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/local/vivurouter-go/internal/store"
)

const (
	ClientID              = "app_EMoamEEZ73f0CkXaXp7hrann"
	AuthorizeURL          = "https://auth.openai.com/oauth/authorize"
	TokenURL              = "https://auth.openai.com/oauth/token"
	RedirectURI           = "http://localhost:1455/auth/callback"
	Scope                 = "openid profile email offline_access"
	codeChallengeMethod   = "S256"
	callbackListenAddress = "127.0.0.1:1455"
	callbackTimeout       = 5 * time.Minute
)

type Manager struct {
	store        store.Store
	client       *http.Client
	authorizeURL string
	tokenURL     string

	mu       sync.Mutex
	session  SessionStatus
	verifier string
	server   *http.Server
	timer    *time.Timer
}

type SessionStatus struct {
	ProviderID  string    `json:"provider_id"`
	State       string    `json:"state"`
	AuthURL     string    `json:"auth_url"`
	CallbackURL string    `json:"callback_url"`
	Status      string    `json:"status"`
	Error       string    `json:"error,omitempty"`
	StartedAt   time.Time `json:"started_at,omitempty"`
	CompletedAt time.Time `json:"completed_at,omitempty"`
	ProxyURL    string    `json:"proxy_url,omitempty"`
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
}

func NewManager(st store.Store) *Manager {
	return &Manager{
		store:        st,
		client:       &http.Client{Timeout: 30 * time.Second},
		authorizeURL: AuthorizeURL,
		tokenURL:     TokenURL,
	}
}

func (m *Manager) Start(providerID string, proxyURL string) (SessionStatus, error) {
	providerID = strings.TrimSpace(providerID)
	proxyURL = strings.TrimSpace(proxyURL)
	if providerID == "" || providerID == "codex" {
		providerID = m.nextCodexProviderID(providerID)
	}

	verifier, challenge, state, err := GeneratePKCE()
	if err != nil {
		return SessionStatus{}, err
	}

	status := SessionStatus{
		ProviderID:  providerID,
		State:       state,
		AuthURL:     BuildAuthURLWithBase(m.authorizeURL, RedirectURI, state, challenge),
		CallbackURL: RedirectURI,
		Status:      "pending",
		StartedAt:   time.Now().UTC(),
		ProxyURL:    proxyURL,
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopLocked()

	server := &http.Server{Handler: m.callbackHandler()}
	listener, err := net.Listen("tcp", callbackListenAddress)
	if err != nil {
		return SessionStatus{}, fmt.Errorf("listen Codex OAuth callback on %s: %w", callbackListenAddress, err)
	}

	m.session = status
	m.verifier = verifier
	m.server = server
	m.timer = time.AfterFunc(callbackTimeout, func() {
		m.markTimeout(state)
	})

	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			m.markError(state, err.Error())
		}
	}()

	return status, nil
}

func (m *Manager) Status() SessionStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.session
}

func (m *Manager) CompleteWithCallbackURL(ctx context.Context, callbackURL string) error {
	parsed, err := url.Parse(strings.TrimSpace(callbackURL))
	if err != nil {
		return fmt.Errorf("callback URL không hợp lệ: %w", err)
	}
	query := parsed.Query()
	state := query.Get("state")
	code := query.Get("code")
	if errorParam := query.Get("error"); errorParam != "" {
		message := query.Get("error_description")
		if message == "" {
			message = errorParam
		}
		return errors.New(message)
	}

	m.mu.Lock()
	session := m.session
	verifier := m.verifier
	m.mu.Unlock()

	if session.Status == "" || session.State != state {
		return errors.New("OAuth state không hợp lệ hoặc phiên đã hết hạn")
	}
	if strings.TrimSpace(code) == "" {
		return errors.New("Không nhận được authorization code trong callback URL")
	}
	tokens, err := m.exchangeToken(ctx, code, verifier, session.ProxyURL)
	if err != nil {
		m.finishError(state, err.Error())
		return err
	}
	if err := m.saveTokens(session.ProviderID, tokens); err != nil {
		m.finishError(state, err.Error())
		return err
	}
	m.finishSuccess(state)
	m.stopServerOnly()
	return nil
}

func (m *Manager) callbackHandler() http.Handler {
	mux := http.NewServeMux()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.handleCallback(w, r)
	})
	mux.Handle("/auth/callback", handler)
	mux.Handle("/callback", handler)
	return mux
}

func (m *Manager) handleCallback(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")
	errorParam := r.URL.Query().Get("error")

	m.mu.Lock()
	session := m.session
	verifier := m.verifier
	m.mu.Unlock()

	if session.Status == "" || session.State != state {
		m.writeResult(w, false, "OAuth state không hợp lệ hoặc phiên đã hết hạn")
		return
	}
	if errorParam != "" {
		message := r.URL.Query().Get("error_description")
		if message == "" {
			message = errorParam
		}
		m.finishError(state, message)
		m.writeResult(w, false, message)
		return
	}
	if strings.TrimSpace(code) == "" {
		m.finishError(state, "Không nhận được authorization code")
		m.writeResult(w, false, "Không nhận được authorization code")
		return
	}

	tokens, err := m.exchangeToken(r.Context(), code, verifier, session.ProxyURL)
	if err != nil {
		m.finishError(state, err.Error())
		m.writeResult(w, false, err.Error())
		return
	}
	if err := m.saveTokens(session.ProviderID, tokens); err != nil {
		m.finishError(state, err.Error())
		m.writeResult(w, false, err.Error())
		return
	}

	m.finishSuccess(state)
	m.writeResult(w, true, "Codex OAuth hoàn tất. Access/refresh token đã được lưu vào provider local.")
	go func() {
		time.Sleep(250 * time.Millisecond)
		m.stopServerOnly()
	}()
}

func (m *Manager) exchangeToken(ctx context.Context, code string, verifier string, proxyURL string) (tokenResponse, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", ClientID)
	form.Set("code", code)
	form.Set("redirect_uri", RedirectURI)
	form.Set("code_verifier", verifier)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return tokenResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	client, err := clientForProxy(m.client, proxyURL)
	if err != nil {
		return tokenResponse{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return tokenResponse{}, err
	}
	defer resp.Body.Close()

	var payload tokenResponse
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var body map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&body)
		return tokenResponse{}, fmt.Errorf("Codex token exchange failed: HTTP %d %s", resp.StatusCode, compactMap(body))
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return tokenResponse{}, err
	}
	if strings.TrimSpace(payload.AccessToken) == "" {
		return tokenResponse{}, errors.New("Codex token exchange did not return access_token")
	}
	return payload, nil
}

func (m *Manager) nextCodexProviderID(requested string) string {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		requested = "codex"
	}
	providers, err := m.store.ListProviders()
	if err != nil {
		return requested
	}
	used := map[string]bool{}
	for _, provider := range providers {
		used[provider.ID] = true
	}
	if !used[requested] {
		return requested
	}
	for i := 2; i < 1000; i++ {
		candidate := fmt.Sprintf("%s-%d", requested, i)
		if !used[candidate] {
			return candidate
		}
	}
	return requested + "-" + time.Now().UTC().Format("20060102150405")
}

func (m *Manager) saveTokens(providerID string, tokens tokenResponse) error {
	provider, found, err := m.store.GetProvider(providerID)
	if err != nil {
		return err
	}
	if !found {
		provider = store.Provider{
			ID:      providerID,
			Type:    store.ProviderCodex,
			Name:    "Codex Responses",
			BaseURL: "https://chatgpt.com/backend-api/codex/responses",
			Models:  []string{"cx/gpt-5.5", "cx/gpt-5.4", "cx/gpt-5.3-codex"},
		}
	}
	if strings.TrimSpace(m.session.ProxyURL) != "" {
		provider.ProxyURL = strings.TrimSpace(m.session.ProxyURL)
	}
	provider.Type = store.ProviderCodex
	if provider.Name == "" {
		provider.Name = "Codex Responses"
	}
	if strings.TrimSpace(provider.BaseURL) == "" {
		provider.BaseURL = "https://chatgpt.com/backend-api/codex/responses"
	}
	if len(provider.Models) == 0 {
		provider.Models = []string{"cx/gpt-5.5", "cx/gpt-5.4", "cx/gpt-5.3-codex"}
	}
	provider.AccessToken = tokens.AccessToken
	if tokens.RefreshToken != "" {
		provider.RefreshToken = tokens.RefreshToken
	}
	provider.Enabled = true
	now := time.Now().UTC()
	if provider.CreatedAt.IsZero() {
		provider.CreatedAt = now
	}
	provider.UpdatedAt = now
	return m.store.UpsertProvider(provider)
}

func (m *Manager) finishSuccess(state string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.session.State != state {
		return
	}
	m.session.Status = "done"
	m.session.AuthURL = ""
	m.session.CompletedAt = time.Now().UTC()
	m.session.Error = ""
	if m.timer != nil {
		m.timer.Stop()
		m.timer = nil
	}
}

func (m *Manager) finishError(state string, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.session.State != state {
		return
	}
	m.session.Status = "error"
	m.session.Error = message
	m.session.CompletedAt = time.Now().UTC()
	if m.timer != nil {
		m.timer.Stop()
		m.timer = nil
	}
}

func (m *Manager) markTimeout(state string) {
	m.finishError(state, "Codex OAuth timeout sau 5 phút")
	m.stopServerOnly()
}

func (m *Manager) markError(state string, message string) {
	m.finishError(state, message)
}

func (m *Manager) stopServerOnly() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.server != nil {
		_ = m.server.Close()
		m.server = nil
	}
	if m.timer != nil {
		m.timer.Stop()
		m.timer = nil
	}
}

func (m *Manager) stopLocked() {
	if m.timer != nil {
		m.timer.Stop()
		m.timer = nil
	}
	if m.server != nil {
		_ = m.server.Close()
		m.server = nil
	}
	m.verifier = ""
}

func (m *Manager) writeResult(w http.ResponseWriter, success bool, message string) {
	color := "#22c55e"
	title := "Codex OAuth thành công"
	if !success {
		color = "#ef4444"
		title = "Codex OAuth thất bại"
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprintf(w, `<!doctype html><html lang="vi"><head><meta charset="utf-8"><title>%s</title><style>body{font-family:system-ui,Segoe UI,Arial,sans-serif;background:#0b1020;color:#e8eefc;display:flex;align-items:center;justify-content:center;min-height:100vh;margin:0}.card{max-width:680px;background:#121a2f;border:1px solid #293653;border-radius:16px;padding:28px;box-shadow:0 20px 80px rgba(0,0,0,.25)}h1{color:%s}p{color:#b9c7e6}code{background:#080d19;border:1px solid #202b44;border-radius:8px;padding:4px 8px}</style></head><body><main class="card"><h1>%s</h1><p>%s</p><p>Quay lại dashboard: <code>http://127.0.0.1:20129/providers</code></p></main></body></html>`, html.EscapeString(title), color, html.EscapeString(title), html.EscapeString(message))
}

func GeneratePKCE() (verifier string, challenge string, state string, err error) {
	verifier, err = randomURLSafe(32)
	if err != nil {
		return "", "", "", err
	}
	state, err = randomURLSafe(32)
	if err != nil {
		return "", "", "", err
	}
	hash := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(hash[:])
	return verifier, challenge, state, nil
}

func BuildAuthURL(redirectURI string, state string, codeChallenge string) string {
	return BuildAuthURLWithBase(AuthorizeURL, redirectURI, state, codeChallenge)
}

func BuildAuthURLWithBase(authorizeURL string, redirectURI string, state string, codeChallenge string) string {
	params := [][2]string{
		{"response_type", "code"},
		{"client_id", ClientID},
		{"redirect_uri", redirectURI},
		{"scope", Scope},
		{"code_challenge", codeChallenge},
		{"code_challenge_method", codeChallengeMethod},
		{"id_token_add_organizations", "true"},
		{"codex_cli_simplified_flow", "true"},
		{"originator", "codex_cli_rs"},
		{"state", state},
	}
	parts := make([]string, 0, len(params))
	for _, item := range params {
		parts = append(parts, encodeURIComponent(item[0])+"="+encodeURIComponent(item[1]))
	}
	return authorizeURL + "?" + strings.Join(parts, "&")
}

func encodeURIComponent(value string) string {
	return strings.ReplaceAll(url.QueryEscape(value), "+", "%20")
}

func randomURLSafe(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func compactMap(m map[string]any) string {
	if len(m) == 0 {
		return ""
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	return string(raw)
}
