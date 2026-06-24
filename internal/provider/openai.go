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

// OpenAIExecutor handles OpenAI-compatible chat/completions upstreams.
type OpenAIExecutor struct {
	Client  *http.Client
	KeyPool *KeyPool
}

func (e *OpenAIExecutor) ExecuteChat(ctx context.Context, provider store.Provider, model string, body map[string]any) (*ExecuteResult, error) {
	transformed := cloneBody(body)
	transformed["model"] = model
	url := strings.TrimRight(provider.BaseURL, "/") + "/chat/completions"

	raw, err := json.Marshal(transformed)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if bodyStream(transformed) {
		req.Header.Set("Accept", "text/event-stream")
	}

	usedKeyID := ""
	if e.KeyPool != nil {
		if key := e.KeyPool.SelectKey(provider); key != nil {
			req.Header.Set("Authorization", "Bearer "+key.Key)
			usedKeyID = key.ID
		}
	}
	if usedKeyID == "" {
		// Fall back to legacy single-key fields.
		if provider.APIKey != "" {
			req.Header.Set("Authorization", "Bearer "+provider.APIKey)
			usedKeyID = "legacy"
		} else if provider.AccessToken != "" {
			req.Header.Set("Authorization", "Bearer "+provider.AccessToken)
			usedKeyID = "legacy"
		} else {
			return nil, fmt.Errorf("provider %s has no API key or access token", provider.ID)
		}
	}

	client, err := clientForProvider(e.Client, provider)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	return &ExecuteResult{Response: resp, URL: url, TransformedBody: transformed, UsedKeyID: usedKeyID}, nil
}

func cloneBody(body map[string]any) map[string]any {
	out := make(map[string]any, len(body))
	for key, value := range body {
		out[key] = value
	}
	return out
}

func bodyStream(body map[string]any) bool {
	value, _ := body["stream"].(bool)
	return value
}
