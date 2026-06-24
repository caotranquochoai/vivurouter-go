package gateway

import (
	"net/http/httptest"
	"testing"
)

func TestApplyClaudePromptCacheKey(t *testing.T) {
	req := httptest.NewRequest("POST", "/v1/messages", nil)
	req.Header.Set("x-claude-code-session-id", "session/abc 123!?-keep")
	body := map[string]any{}
	applyClaudePromptCacheKey(req, body)
	if body["prompt_cache_key"] != "claude-code:sessionabc123-keep" {
		t.Fatalf("prompt_cache_key = %#v", body["prompt_cache_key"])
	}
}

func TestApplyClaudePromptCacheKeyKeepsExisting(t *testing.T) {
	req := httptest.NewRequest("POST", "/v1/messages", nil)
	req.Header.Set("x-claude-code-session-id", "new-session")
	body := map[string]any{"prompt_cache_key": "existing"}
	applyClaudePromptCacheKey(req, body)
	if body["prompt_cache_key"] != "existing" {
		t.Fatalf("prompt_cache_key overwritten: %#v", body["prompt_cache_key"])
	}
}
