package antigravityoauth

import (
	"context"
	"crypto/rand"
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
	ClientID              = "1071006060591-tmhssin2h21lcre235vtolojh4g403ep.apps.googleusercontent.com"
	ClientSecret          = "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"
	AuthorizeURL          = "https://accounts.google.com/o/oauth2/v2/auth"
	TokenURL              = "https://oauth2.googleapis.com/token"
	UserInfoURL           = "https://www.googleapis.com/oauth2/v1/userinfo?alt=json"
	RedirectURI           = "http://localhost:1456/auth/callback"
	callbackListenAddress = "127.0.0.1:1456"
	callbackTimeout       = 5 * time.Minute
	Scope                 = "https://www.googleapis.com/auth/cloud-platform https://www.googleapis.com/auth/userinfo.email https://www.googleapis.com/auth/userinfo.profile https://www.googleapis.com/auth/cclog https://www.googleapis.com/auth/experimentsandconfigs"
)

type Manager struct {
	store        store.Store
	client       *http.Client
	authorizeURL string
	tokenURL     string
	userInfoURL  string

	mu      sync.Mutex
	session SessionStatus
	server  *http.Server
	timer   *time.Timer
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
	Email       string    `json:"email,omitempty"`
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
	TokenType    string `json:"token_type"`
}

func NewManager(st store.Store) *Manager {
	return &Manager{store: st, client: &http.Client{Timeout: 30 * time.Second}, authorizeURL: AuthorizeURL, tokenURL: TokenURL, userInfoURL: UserInfoURL}
}

func (m *Manager) Start(providerID string, proxyURL string) (SessionStatus, error) {
	providerID = strings.TrimSpace(providerID)
	if providerID == "" || providerID == "antigravity" {
		providerID = m.nextProviderID(providerID)
	}
	state, err := randomURLSafe(32)
	if err != nil {
		return SessionStatus{}, err
	}
	status := SessionStatus{ProviderID: providerID, State: state, AuthURL: BuildAuthURLWithBase(m.authorizeURL, RedirectURI, state), CallbackURL: RedirectURI, Status: "pending", StartedAt: time.Now().UTC(), ProxyURL: strings.TrimSpace(proxyURL)}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopLocked()
	server := &http.Server{Handler: m.callbackHandler()}
	listener, err := net.Listen("tcp", callbackListenAddress)
	if err != nil {
		return SessionStatus{}, fmt.Errorf("listen Antigravity OAuth callback on %s: %w", callbackListenAddress, err)
	}
	m.session = status
	m.server = server
	m.timer = time.AfterFunc(callbackTimeout, func() { m.markTimeout(state) })
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			m.markError(state, err.Error())
		}
	}()
	return status, nil
}

func (m *Manager) Status() SessionStatus { m.mu.Lock(); defer m.mu.Unlock(); return m.session }

func (m *Manager) CompleteWithCallbackURL(ctx context.Context, callbackURL string) error {
	parsed, err := url.Parse(strings.TrimSpace(callbackURL))
	if err != nil {
		return fmt.Errorf("callback URL không hợp lệ: %w", err)
	}
	return m.complete(ctx, parsed.Query().Get("state"), parsed.Query().Get("code"), parsed.Query().Get("error"), parsed.Query().Get("error_description"))
}

func (m *Manager) callbackHandler() http.Handler {
	mux := http.NewServeMux()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := m.complete(r.Context(), r.URL.Query().Get("state"), r.URL.Query().Get("code"), r.URL.Query().Get("error"), r.URL.Query().Get("error_description"))
		if err != nil {
			m.writeResult(w, false, err.Error())
			return
		}
		m.writeResult(w, true, "Antigravity OAuth hoàn tất. Access/refresh token đã được lưu vào provider local.")
		go func() { time.Sleep(250 * time.Millisecond); m.stopServerOnly() }()
	})
	mux.Handle("/auth/callback", handler)
	mux.Handle("/callback", handler)
	return mux
}

func (m *Manager) complete(ctx context.Context, state string, code string, errorParam string, errorDescription string) error {
	m.mu.Lock()
	session := m.session
	m.mu.Unlock()
	if session.Status == "" || session.State != state {
		return errors.New("OAuth state không hợp lệ hoặc phiên đã hết hạn")
	}
	if errorParam != "" {
		if errorDescription != "" {
			errorParam = errorDescription
		}
		m.finishError(state, errorParam)
		return errors.New(errorParam)
	}
	if strings.TrimSpace(code) == "" {
		err := errors.New("Không nhận được authorization code")
		m.finishError(state, err.Error())
		return err
	}
	tokens, err := m.exchangeToken(ctx, code, session.ProxyURL)
	if err != nil {
		m.finishError(state, err.Error())
		return err
	}
	email := m.fetchEmail(ctx, tokens.AccessToken, session.ProxyURL)
	if err := m.saveTokens(session.ProviderID, tokens, email); err != nil {
		m.finishError(state, err.Error())
		return err
	}
	m.finishSuccess(state, email)
	return nil
}

func (m *Manager) exchangeToken(ctx context.Context, code string, proxyURL string) (tokenResponse, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", ClientID)
	form.Set("client_secret", ClientSecret)
	form.Set("code", code)
	form.Set("redirect_uri", RedirectURI)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return tokenResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := m.client.Do(req)
	if err != nil {
		return tokenResponse{}, err
	}
	defer resp.Body.Close()
	var payload tokenResponse
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return tokenResponse{}, fmt.Errorf("Antigravity token exchange failed: HTTP %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return tokenResponse{}, err
	}
	if strings.TrimSpace(payload.AccessToken) == "" {
		return tokenResponse{}, errors.New("Antigravity token exchange did not return access_token")
	}
	return payload, nil
}

func (m *Manager) fetchEmail(ctx context.Context, accessToken string, proxyURL string) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.userInfoURL, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	resp, err := m.client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var payload struct {
		Email string `json:"email"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&payload)
	return strings.TrimSpace(payload.Email)
}

func (m *Manager) saveTokens(providerID string, tokens tokenResponse, email string) error {
	provider, found, err := m.store.GetProvider(providerID)
	if err != nil {
		return err
	}
	if !found {
		provider = store.Provider{ID: providerID, Type: store.ProviderAntigravity, Name: "Antigravity", BaseURL: "https://daily-cloudcode-pa.googleapis.com", Models: []string{"gemini-3-flash-agent", "gemini-3.5-flash-low", "gemini-3.5-flash-extra-low", "gemini-pro-agent", "gemini-3.1-pro-low", "claude-sonnet-4-6", "claude-opus-4-6-thinking", "gpt-oss-120b-medium", "gemini-3-flash"}}
	}
	if strings.TrimSpace(m.session.ProxyURL) != "" {
		provider.ProxyURL = strings.TrimSpace(m.session.ProxyURL)
	}
	provider.Type = store.ProviderAntigravity
	if provider.Name == "" {
		provider.Name = "Antigravity"
	}
	if provider.BaseURL == "" {
		provider.BaseURL = "https://daily-cloudcode-pa.googleapis.com"
	}
	if len(provider.Models) == 0 {
		provider.Models = []string{"gemini-3-flash-agent", "gemini-3.5-flash-low", "gemini-3.5-flash-extra-low", "gemini-pro-agent", "gemini-3.1-pro-low", "claude-sonnet-4-6", "claude-opus-4-6-thinking", "gpt-oss-120b-medium", "gemini-3-flash"}
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

func (m *Manager) nextProviderID(requested string) string {
	if requested == "" {
		requested = "antigravity"
	}
	providers, err := m.store.ListProviders()
	if err != nil {
		return requested
	}
	used := map[string]bool{}
	for _, p := range providers {
		used[p.ID] = true
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

func BuildAuthURLWithBase(authorizeURL string, redirectURI string, state string) string {
	params := url.Values{}
	params.Set("client_id", ClientID)
	params.Set("response_type", "code")
	params.Set("redirect_uri", redirectURI)
	params.Set("scope", Scope)
	params.Set("state", state)
	params.Set("access_type", "offline")
	params.Set("prompt", "consent")
	return authorizeURL + "?" + params.Encode()
}

func (m *Manager) finishSuccess(state string, email string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.session.State != state {
		return
	}
	m.session.Status = "done"
	m.session.AuthURL = ""
	m.session.Email = email
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
	m.finishError(state, "Antigravity OAuth timeout sau 5 phút")
	m.stopServerOnly()
}
func (m *Manager) markError(state string, message string) { m.finishError(state, message) }
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
}

func (m *Manager) writeResult(w http.ResponseWriter, success bool, message string) {
	color := "#22c55e"
	title := "Antigravity OAuth thành công"
	if !success {
		color = "#ef4444"
		title = "Antigravity OAuth thất bại"
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprintf(w, `<!doctype html><html lang="vi"><head><meta charset="utf-8"><title>%s</title><style>body{font-family:system-ui,Segoe UI,Arial,sans-serif;background:#0b1020;color:#e8eefc;display:flex;align-items:center;justify-content:center;min-height:100vh;margin:0}.card{max-width:680px;background:#121a2f;border:1px solid #293653;border-radius:16px;padding:28px;box-shadow:0 20px 80px rgba(0,0,0,.25)}h1{color:%s}p{color:#b9c7e6}code{background:#080d19;border:1px solid #202b44;border-radius:8px;padding:4px 8px}</style></head><body><main class="card"><h1>%s</h1><p>%s</p><p>Quay lại dashboard: <code>http://127.0.0.1:20129/providers</code></p></main></body></html>`, html.EscapeString(title), color, html.EscapeString(title), html.EscapeString(message))
}

func randomURLSafe(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
