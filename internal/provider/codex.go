package provider

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/local/vivurouter-go/internal/store"
)

const defaultCodexInstructions = "You are Codex, an AI coding assistant. Be concise, precise, and helpful."

var codexAllowList = map[string]bool{
	"model": true, "input": true, "instructions": true, "tools": true, "tool_choice": true,
	"stream": true, "store": true, "reasoning": true, "service_tier": true, "include": true,
	"prompt_cache_key": true, "client_metadata": true,
}

// CodexExecutor handles ChatGPT Codex Responses API upstreams.
type CodexExecutor struct {
	Client   *http.Client
	Store    store.Store
	TokenURL string
}

func (e *CodexExecutor) ExecuteResponses(ctx context.Context, provider store.Provider, model string, body map[string]any) (*ExecuteResult, error) {
	transformed := normalizeCodexBody(provider, model, body)
	result, err := e.executeResponsesOnce(ctx, provider, transformed)
	if err != nil || result.Response == nil || !isCodexAuthFailure(result.Response.StatusCode) || strings.TrimSpace(provider.RefreshToken) == "" || e.Store == nil {
		return result, err
	}

	_ = result.Response.Body.Close()
	refreshed, refreshErr := e.RefreshCodexToken(ctx, provider)
	if refreshErr != nil {
		return nil, refreshErr
	}
	return e.executeResponsesOnce(ctx, refreshed, transformed)
}

func (e *CodexExecutor) executeResponsesOnce(ctx context.Context, provider store.Provider, transformed map[string]any) (*ExecuteResult, error) {
	url := provider.BaseURL
	if strings.TrimSpace(url) == "" {
		url = "https://chatgpt.com/backend-api/codex/responses"
	}

	raw, err := json.Marshal(transformed)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("originator", "codex_cli_rs")
	req.Header.Set("User-Agent", "codex_cli_rs/0.136.0 vivurouter-go")
	req.Header.Set("session_id", resolveSessionID(provider, transformed))
	if provider.AccessToken != "" {
		req.Header.Set("Authorization", "Bearer "+provider.AccessToken)
	} else if provider.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+provider.APIKey)
	} else {
		return nil, fmt.Errorf("provider %s has no Codex access token", provider.ID)
	}

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

func (e *CodexExecutor) RefreshCodexToken(ctx context.Context, provider store.Provider) (store.Provider, error) {
	if strings.TrimSpace(provider.RefreshToken) == "" {
		return provider, fmt.Errorf("provider %s has no Codex refresh token", provider.ID)
	}
	tokenURL := strings.TrimSpace(e.TokenURL)
	if tokenURL == "" {
		tokenURL = "https://auth.openai.com/oauth/token"
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("client_id", "app_EMoamEEZ73f0CkXaXp7hrann")
	form.Set("refresh_token", provider.RefreshToken)
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
	var payload struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return provider, fmt.Errorf("Codex token refresh failed: HTTP %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return provider, err
	}
	if strings.TrimSpace(payload.AccessToken) == "" {
		return provider, fmt.Errorf("Codex token refresh did not return access_token")
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

func isCodexAuthFailure(statusCode int) bool {
	return statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden
}

func normalizeCodexBody(provider store.Provider, model string, body map[string]any) map[string]any {
	out := cloneBody(body)
	out["model"] = NormalizeCodexUpstreamModelID(provider, model)
	out["stream"] = true
	out["store"] = false
	if input := normalizeResponsesInput(out["input"]); len(input) > 0 {
		out["input"] = input
	} else {
		out["input"] = []map[string]any{{"type": "message", "role": "user", "content": []map[string]any{{"type": "input_text", "text": "..."}}}}
	}
	convertCodexSystemToDeveloper(out)
	stripCodexStoredItemReferences(out)
	normalizeCodexTools(out)
	if out["prompt_cache_key"] == nil || strings.TrimSpace(asString(out["prompt_cache_key"])) == "" {
		out["prompt_cache_key"] = resolveSessionID(provider, out)
	}
	if strings.TrimSpace(asString(out["instructions"])) == "" {
		out["instructions"] = defaultCodexInstructions
	}
	if out["reasoning"] == nil {
		out["reasoning"] = map[string]any{"effort": "low", "summary": "auto"}
	} else if reasoning := asMapLocal(out["reasoning"]); len(reasoning) > 0 && strings.TrimSpace(asString(reasoning["summary"])) == "" {
		reasoning["summary"] = "auto"
		out["reasoning"] = reasoning
	}
	if reasoning := asMapLocal(out["reasoning"]); len(reasoning) > 0 && strings.TrimSpace(asString(reasoning["effort"])) != "" && asString(reasoning["effort"]) != "none" {
		out["include"] = []string{"reasoning.encrypted_content"}
	}
	stripCodexUnsupported(out)
	return out
}

func normalizeResponsesInput(input any) []map[string]any {
	switch value := input.(type) {
	case string:
		if strings.TrimSpace(value) == "" {
			return nil
		}
		return []map[string]any{{"type": "message", "role": "user", "content": []map[string]any{{"type": "input_text", "text": value}}}}
	case []any:
		out := make([]map[string]any, 0, len(value))
		for _, item := range value {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	case []map[string]any:
		return value
	default:
		return nil
	}
}

func convertCodexSystemToDeveloper(body map[string]any) {
	input, ok := body["input"].([]map[string]any)
	if !ok {
		return
	}
	for _, item := range input {
		if asString(item["role"]) == "system" && (item["type"] == nil || asString(item["type"]) == "message") {
			item["role"] = "developer"
		}
	}
}

func stripCodexStoredItemReferences(body map[string]any) {
	input, ok := body["input"].([]map[string]any)
	if !ok {
		return
	}
	out := make([]map[string]any, 0, len(input))
	for _, item := range input {
		id := asString(item["id"])
		if asString(item["type"]) == "item_reference" {
			continue
		}
		if id != "" && isCodexServerID(id) {
			delete(item, "id")
		}
		out = append(out, item)
	}
	body["input"] = out
}

func isCodexServerID(id string) bool {
	return strings.HasPrefix(id, "rs_") || strings.HasPrefix(id, "fc_") || strings.HasPrefix(id, "resp_") || strings.HasPrefix(id, "msg_")
}

func normalizeCodexTools(body map[string]any) {
	tools, ok := body["tools"].([]map[string]any)
	if !ok {
		if rawTools, ok := body["tools"].([]any); ok {
			tools = make([]map[string]any, 0, len(rawTools))
			for _, raw := range rawTools {
				if m, ok := raw.(map[string]any); ok {
					tools = append(tools, m)
				}
			}
		} else {
			return
		}
	}
	validNames := map[string]bool{}
	out := []map[string]any{}
	for _, tool := range tools {
		toolType := asString(tool["type"])
		if toolType != "function" {
			if toolType == "namespace" || isCodexHostedToolType(toolType) {
				out = append(out, tool)
			}
			continue
		}
		fn := asMapLocal(tool["function"])
		name := strings.TrimSpace(asString(tool["name"]))
		if name == "" {
			name = strings.TrimSpace(asString(fn["name"]))
		}
		if name == "" {
			continue
		}
		description := asString(tool["description"])
		if description == "" {
			description = asString(fn["description"])
		}
		params := tool["parameters"]
		if params == nil {
			params = fn["parameters"]
		}
		if params == nil {
			params = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		if len(name) > 128 {
			name = name[:128]
		}
		flat := map[string]any{"type": "function", "name": name, "parameters": params}
		if description != "" {
			flat["description"] = description
		}
		validNames[name] = true
		out = append(out, flat)
	}
	if len(out) == 0 {
		delete(body, "tools")
		delete(body, "tool_choice")
		return
	}
	body["tools"] = out
	choice := asMapLocal(body["tool_choice"])
	if len(choice) > 0 && asString(choice["type"]) == "function" && !validNames[asString(choice["name"])] {
		delete(body, "tool_choice")
	}
}

func isCodexHostedToolType(toolType string) bool {
	switch toolType {
	case "image_generation", "web_search", "web_search_preview", "file_search", "computer", "computer_use_preview", "code_interpreter", "mcp", "local_shell":
		return true
	default:
		return false
	}
}

func asMapLocal(value any) map[string]any {
	if m, ok := value.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func stripCodexUnsupported(body map[string]any) {
	delete(body, "temperature")
	delete(body, "top_p")
	delete(body, "frequency_penalty")
	delete(body, "presence_penalty")
	delete(body, "logprobs")
	delete(body, "top_logprobs")
	delete(body, "n")
	delete(body, "seed")
	delete(body, "max_tokens")
	delete(body, "max_completion_tokens")
	delete(body, "max_output_tokens")
	delete(body, "user")
	delete(body, "metadata")
	delete(body, "stream_options")
	delete(body, "previous_response_id")
	for key := range body {
		if !codexAllowList[key] {
			delete(body, key)
		}
	}
}

// NormalizeCodexUpstreamModelID converts local VivuRouter-style Codex IDs such as
// cx/gpt-5.5 back to the model ID expected by the ChatGPT Codex backend.
func NormalizeCodexUpstreamModelID(provider store.Provider, model string) string {
	model = strings.TrimSpace(model)
	for _, prefix := range []string{provider.ID + "/", "cx/", "codex/"} {
		if strings.HasPrefix(model, prefix) {
			model = strings.TrimPrefix(model, prefix)
			break
		}
	}
	if strings.HasSuffix(model, "-review") {
		model = strings.TrimSuffix(model, "-review")
	}
	return model
}

func resolveSessionID(provider store.Provider, body map[string]any) string {
	for _, key := range []string{"prompt_cache_key", "session_id", "conversation_id"} {
		if value := strings.TrimSpace(asString(body[key])); value != "" {
			return value
		}
	}
	seed := provider.ID + ":" + os.Getenv("COMPUTERNAME") + ":" + os.Getenv("HOSTNAME")
	if seed == provider.ID+":"+":" {
		seed = provider.ID + ":" + time.Now().UTC().Format("20060102")
	}
	hash := sha256.Sum256([]byte(seed))
	return "sess_" + hex.EncodeToString(hash[:])[:16]
}

func asString(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}
