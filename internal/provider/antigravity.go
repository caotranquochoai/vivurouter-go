package provider

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"time"

	"github.com/local/vivurouter-go/internal/store"
)

const (
	defaultAntigravityBaseURL      = "https://daily-cloudcode-pa.googleapis.com"
	defaultAntigravityTokenURL     = "https://oauth2.googleapis.com/token"
	antigravityClientID            = "1071006060591-tmhssin2h21lcre235vtolojh4g403ep.apps.googleusercontent.com"
	antigravityClientSecret        = "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"
	antigravityRequestSourceHeader = "x-request-source"
	antigravityRequestSource       = "local"
	antigravityMachineSession      = "X-Machine-Session-Id"
)

// AntigravityExecutor handles Google Antigravity / Cloud Code Assist chat calls.
type AntigravityExecutor struct {
	Client   *http.Client
	Store    store.Store
	TokenURL string
}

func (e *AntigravityExecutor) ExecuteChat(ctx context.Context, provider store.Provider, model string, body map[string]any) (*ExecuteResult, error) {
	sessionID := antigravitySessionID(body)
	transformed := antigravityRequest(model, body, sessionID)
	result, err := e.executeChatOnce(ctx, provider, model, transformed, bodyStream(body), sessionID)
	if err != nil || result.Response == nil || result.Response.StatusCode != http.StatusUnauthorized || strings.TrimSpace(provider.RefreshToken) == "" || e.Store == nil {
		return result, err
	}
	_ = result.Response.Body.Close()
	refreshed, refreshErr := e.RefreshAntigravityToken(ctx, provider)
	if refreshErr != nil {
		return nil, refreshErr
	}
	return e.executeChatOnce(ctx, refreshed, model, transformed, bodyStream(body), sessionID)
}

func (e *AntigravityExecutor) executeChatOnce(ctx context.Context, provider store.Provider, model string, transformed map[string]any, stream bool, sessionID string) (*ExecuteResult, error) {
	if strings.TrimSpace(provider.AccessToken) == "" {
		return nil, fmt.Errorf("provider %s has no Antigravity access token", provider.ID)
	}
	raw, err := json.Marshal(transformed)
	if err != nil {
		return nil, err
	}
	url := antigravityURL(provider, stream)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+provider.AccessToken)
	req.Header.Set("User-Agent", antigravityUserAgent())
	req.Header.Set(antigravityRequestSourceHeader, antigravityRequestSource)
	req.Header.Set(antigravityMachineSession, sessionID)
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}

	client, err := clientForProvider(e.Client, provider)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &ExecuteResult{Response: resp, URL: url, TransformedBody: transformed}, nil
	}
	if stream {
		resp, err = antigravityRewriteSSEResponse(resp, model)
	} else {
		resp, err = antigravityRewriteJSONResponse(resp, model)
	}
	if err != nil {
		return nil, err
	}
	return &ExecuteResult{Response: resp, URL: url, TransformedBody: transformed}, nil
}

func (e *AntigravityExecutor) RefreshAntigravityToken(ctx context.Context, provider store.Provider) (store.Provider, error) {
	if strings.TrimSpace(provider.RefreshToken) == "" {
		return provider, fmt.Errorf("provider %s has no Antigravity refresh token", provider.ID)
	}
	tokenURL := strings.TrimSpace(e.TokenURL)
	if tokenURL == "" {
		tokenURL = defaultAntigravityTokenURL
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", provider.RefreshToken)
	form.Set("client_id", antigravityClientID)
	form.Set("client_secret", antigravityClientSecret)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return provider, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	client, err := clientForProvider(e.Client, provider)
	if err != nil {
		return provider, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return provider, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return provider, fmt.Errorf("Antigravity token refresh failed: HTTP %d", resp.StatusCode)
	}
	var payload struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return provider, err
	}
	if strings.TrimSpace(payload.AccessToken) == "" {
		return provider, fmt.Errorf("Antigravity token refresh did not return access_token")
	}
	provider.AccessToken = payload.AccessToken
	if strings.TrimSpace(payload.RefreshToken) != "" {
		provider.RefreshToken = payload.RefreshToken
	}
	if e.Store != nil {
		if err := e.Store.UpsertProvider(provider); err != nil {
			return provider, err
		}
	}
	return provider, nil
}

func antigravityURL(provider store.Provider, stream bool) string {
	baseURL := strings.TrimRight(strings.TrimSpace(provider.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultAntigravityBaseURL
	}
	if stream {
		return baseURL + "/v1internal:streamGenerateContent?alt=sse"
	}
	return baseURL + "/v1internal:generateContent"
}

func antigravityUserAgent() string {
	return "antigravity/1.107.0 " + runtime.GOOS + "/" + runtime.GOARCH
}

func antigravitySessionID(body map[string]any) string {
	if id := strings.TrimSpace(asString(body["session_id"])); id != "" {
		return id
	}
	if id := strings.TrimSpace(asString(body["sessionId"])); id != "" {
		return id
	}
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("ag-%d", time.Now().UnixNano())
	}
	return "ag-" + hex.EncodeToString(buf)
}
