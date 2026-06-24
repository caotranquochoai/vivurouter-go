package provider

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/user"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/local/vivurouter-go/internal/store"
)

const (
	defaultMimoBootstrapURL = "https://api.xiaomimimo.com/api/free-ai/bootstrap"
	defaultMimoChatURL      = "https://api.xiaomimimo.com/api/free-ai/openai/chat"
	mimoSystemMarker        = "You are MiMoCode, an interactive CLI tool that helps users with software engineering tasks."
	mimoSourceHeader        = "mimocode-cli-free"
	mimoSessionPrefix       = "ses_"
	mimoSessionIDLength     = 24
	mimoJWTFallbackTTL      = 3000 * time.Second
	mimoJWTExpiryBuffer     = 5 * time.Minute
	mimoSessionChars        = "abcdefghijklmnopqrstuvwxyz0123456789"
)

// MimoFreeExecutor handles MiMo Code Free's no-user-auth endpoint.
type MimoFreeExecutor struct {
	Client       *http.Client
	BootstrapURL string

	mu           sync.Mutex
	cachedJWT    string
	jwtExpiresAt time.Time
	fingerprint  string
	SessionID    string
}

func (e *MimoFreeExecutor) ExecuteChat(ctx context.Context, provider store.Provider, model string, body map[string]any) (*ExecuteResult, error) {
	transformed := injectMimoSystemMarker(cloneBody(body))
	transformed["model"] = model
	raw, err := json.Marshal(transformed)
	if err != nil {
		return nil, err
	}

	jwt, err := e.bootstrapJWT(ctx, provider)
	if err != nil {
		return nil, err
	}
	resp, url, err := e.doChat(ctx, provider, raw, jwt, bodyStream(transformed))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		resp.Body.Close()
		e.resetJWTCache()
		jwt, err = e.bootstrapJWT(ctx, provider)
		if err != nil {
			return nil, err
		}
		resp, url, err = e.doChat(ctx, provider, raw, jwt, bodyStream(transformed))
		if err != nil {
			return nil, err
		}
	}
	return &ExecuteResult{Response: resp, URL: url, TransformedBody: transformed}, nil
}

func (e *MimoFreeExecutor) doChat(ctx context.Context, provider store.Provider, raw []byte, jwt string, stream bool) (*http.Response, string, error) {
	url := mimoChatURL(provider)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return nil, url, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Mimo-Source", mimoSourceHeader)
	req.Header.Set("x-session-affinity", e.sessionID())
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+jwt)

	client, err := clientForProvider(e.Client, provider)
	if err != nil {
		return nil, url, err
	}
	resp, err := client.Do(req)
	return resp, url, err
}

func (e *MimoFreeExecutor) bootstrapJWT(ctx context.Context, provider store.Provider) (string, error) {
	e.mu.Lock()
	if e.cachedJWT != "" && time.Now().Before(e.jwtExpiresAt.Add(-mimoJWTExpiryBuffer)) {
		jwt := e.cachedJWT
		e.mu.Unlock()
		return jwt, nil
	}
	e.mu.Unlock()

	body, err := json.Marshal(map[string]string{"client": e.deviceFingerprint()})
	if err != nil {
		return "", err
	}
	url := e.bootstrapURL()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	client, err := clientForProvider(e.Client, provider)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("MiMo bootstrap failed: HTTP %d", resp.StatusCode)
	}
	var payload struct {
		JWT string `json:"jwt"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	if strings.TrimSpace(payload.JWT) == "" {
		return "", fmt.Errorf("MiMo bootstrap returned no JWT")
	}

	e.mu.Lock()
	e.cachedJWT = payload.JWT
	e.jwtExpiresAt = parseMimoJWTExp(payload.JWT, time.Now())
	e.mu.Unlock()
	return payload.JWT, nil
}

func (e *MimoFreeExecutor) resetJWTCache() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cachedJWT = ""
	e.jwtExpiresAt = time.Time{}
}

func (e *MimoFreeExecutor) bootstrapURL() string {
	if url := strings.TrimSpace(e.BootstrapURL); url != "" {
		return url
	}
	return defaultMimoBootstrapURL
}

func (e *MimoFreeExecutor) sessionID() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.SessionID == "" {
		e.SessionID = generateMimoSessionID()
	}
	return e.SessionID
}

func (e *MimoFreeExecutor) deviceFingerprint() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.fingerprint == "" {
		e.fingerprint = generateMimoFingerprint()
	}
	return e.fingerprint
}

func mimoChatURL(provider store.Provider) string {
	if url := strings.TrimSpace(provider.BaseURL); url != "" {
		return url
	}
	return defaultMimoChatURL
}

func injectMimoSystemMarker(body map[string]any) map[string]any {
	messages := normalizeMimoMessages(body["messages"])
	if messages == nil {
		return body
	}
	for _, item := range messages {
		m, ok := item.(map[string]any)
		if !ok || strings.TrimSpace(asString(m["role"])) != "system" {
			continue
		}
		if strings.Contains(asString(m["content"]), mimoSystemMarker) {
			body["messages"] = messages
			return body
		}
	}
	withMarker := make([]any, 0, len(messages)+1)
	withMarker = append(withMarker, map[string]any{"role": "system", "content": mimoSystemMarker})
	withMarker = append(withMarker, messages...)
	body["messages"] = withMarker
	return body
}

func normalizeMimoMessages(value any) []any {
	switch messages := value.(type) {
	case []any:
		return messages
	case []map[string]any:
		out := make([]any, 0, len(messages))
		for _, msg := range messages {
			out = append(out, msg)
		}
		return out
	case []map[string]string:
		out := make([]any, 0, len(messages))
		for _, msg := range messages {
			converted := make(map[string]any, len(msg))
			for k, v := range msg {
				converted[k] = v
			}
			out = append(out, converted)
		}
		return out
	default:
		return nil
	}
}

func generateMimoFingerprint() string {
	hostname, _ := os.Hostname()
	username := "unknown-user"
	if current, err := user.Current(); err == nil && strings.TrimSpace(current.Username) != "" {
		username = current.Username
	}
	seed := strings.Join([]string{hostname, runtime.GOOS, runtime.GOARCH, username}, "|")
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])
}

func generateMimoSessionID() string {
	buf := make([]byte, mimoSessionIDLength)
	if _, err := rand.Read(buf); err != nil {
		now := time.Now().UnixNano()
		for i := range buf {
			buf[i] = byte(now >> (uint(i%8) * 8))
		}
	}
	var b strings.Builder
	b.WriteString(mimoSessionPrefix)
	for _, v := range buf {
		b.WriteByte(mimoSessionChars[int(v)%len(mimoSessionChars)])
	}
	return b.String()
}

func parseMimoJWTExp(jwt string, now time.Time) time.Time {
	parts := strings.Split(jwt, ".")
	if len(parts) < 2 {
		return now.Add(mimoJWTFallbackTTL)
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return now.Add(mimoJWTFallbackTTL)
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Exp <= 0 {
		return now.Add(mimoJWTFallbackTTL)
	}
	return time.Unix(claims.Exp, 0)
}
