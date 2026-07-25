package provider

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/local/vivurouter-go/internal/store"
)

// MediaRequest describes one OpenAI-compatible media operation. Body ownership
// remains with the caller; ExecuteMedia performs exactly one upstream dispatch.
type MediaRequest struct {
	Path        string
	ContentType string
	Body        io.Reader
}

// ExecuteMedia sends a billable media request exactly once. Callers must not
// retry ambiguous transport failures because the upstream may have accepted it.
func (e *OpenAIExecutor) ExecuteMedia(ctx context.Context, p store.Provider, media MediaRequest) (*ExecuteResult, error) {
	path := strings.TrimSpace(media.Path)
	if path == "" || !strings.HasPrefix(path, "/") || strings.Contains(path, "..") {
		return nil, fmt.Errorf("invalid media endpoint path")
	}
	url := mediaEndpointURL(p.BaseURL, path)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, media.Body)
	if err != nil {
		return nil, err
	}
	if media.ContentType != "" {
		req.Header.Set("Content-Type", media.ContentType)
	}
	usedKeyID, err := e.authorize(req, p)
	if err != nil {
		return nil, err
	}
	client, err := clientForProvider(e.Client, p)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	return &ExecuteResult{Response: resp, URL: url, UsedKeyID: usedKeyID}, nil
}

func mediaEndpointURL(baseURL, path string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	for _, suffix := range []string{"/chat/completions", "/responses"} {
		base = strings.TrimSuffix(base, suffix)
	}
	return base + path
}
