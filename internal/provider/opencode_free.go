package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/local/vivurouter-go/internal/store"
)

const (
	defaultOpenCodeFreeBaseURL   = "https://opencode.ai"
	openCodeFreeAuthorization    = "Bearer public"
	openCodeFreeClientHeaderName = "x-opencode-client"
	openCodeFreeClientHeader     = "desktop"
)

// OpenCodeFreeExecutor handles OpenCode's no-auth public free endpoint.
type OpenCodeFreeExecutor struct {
	Client *http.Client
}

func (e *OpenCodeFreeExecutor) ExecuteChat(ctx context.Context, provider store.Provider, model string, body map[string]any) (*ExecuteResult, error) {
	transformed := cloneBody(body)
	transformed["model"] = model

	baseURL := openCodeFreeBaseURL(provider)
	url := strings.TrimRight(baseURL, "/") + "/zen/v1/chat/completions"
	raw, err := json.Marshal(transformed)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	setOpenCodeFreeHeaders(req)

	client, err := clientForProvider(e.Client, provider)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	return &ExecuteResult{Response: resp, URL: url, TransformedBody: transformed}, nil
}

func (e *OpenCodeFreeExecutor) FetchModels(ctx context.Context, provider store.Provider) ([]ModelInfo, error) {
	baseURL := openCodeFreeBaseURL(provider)
	url := strings.TrimRight(baseURL, "/") + "/zen/v1/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	setOpenCodeFreeHeaders(req)
	req.Header.Set("Accept", "application/json")

	client, err := clientForProvider(e.Client, provider)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("OpenCode Free models endpoint returned HTTP %d", resp.StatusCode)
	}

	var payload any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	return parseOpenAIStyleModels(payload), nil
}

func openCodeFreeBaseURL(provider store.Provider) string {
	if baseURL := strings.TrimSpace(provider.BaseURL); baseURL != "" {
		return baseURL
	}
	return defaultOpenCodeFreeBaseURL
}

func setOpenCodeFreeHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", openCodeFreeAuthorization)
	req.Header.Set(openCodeFreeClientHeaderName, openCodeFreeClientHeader)
	req.Header.Set("Accept", "text/event-stream")
}
