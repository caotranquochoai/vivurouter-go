package gateway

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/local/vivurouter-go/internal/store"
	"github.com/local/vivurouter-go/internal/tokenopt"
)

var debugSecretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(authorization\s*[:=]\s*bearer\s+)[^\s\"']+`),
	regexp.MustCompile(`(?i)((?:api[_-]?key|access[_-]?token|refresh[_-]?token|client[_-]?secret|password|passcode)\s*[\"']?\s*[:=]\s*[\"']?)[^\"'\s,}]+`),
	regexp.MustCompile(`(?i)\b(sk-(?:proj-|ant-|local-)?)[A-Za-z0-9_\-]{10,}\b`),
	regexp.MustCompile(`\b(ghp_|github_pat_)[A-Za-z0-9_\-]{12,}\b`),
}

func maskedAPIKeyParts(key string) (string, string, string) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", "", ""
	}
	prefix := key
	if idx := strings.Index(key, "-"); idx > 0 {
		if next := strings.Index(key[idx+1:], "-"); next >= 0 {
			prefix = key[:idx+1+next]
		} else {
			prefix = key[:idx]
		}
	}
	if len(prefix) > 16 {
		prefix = prefix[:16]
	}
	suffixLen := 6
	if len(key) < suffixLen {
		suffixLen = len(key)
	}
	suffix := key[len(key)-suffixLen:]
	return prefix, suffix, prefix + "-***-***" + suffix
}

func buildDebugPayload(settings store.Settings, body map[string]any) *store.RequestLogDebugPayload {
	if body == nil || (!settings.SaveRawPrompt && !settings.SaveRawToolResult) {
		return nil
	}
	limit := settings.MaxDebugPayloadBytes
	if limit <= 0 {
		limit = 128 * 1024
	}
	// Always redact secrets from stored debug payloads. The payload is persisted
	// and served back via the debug API, so leaking auth headers/keys here would
	// be a durable disclosure regardless of the operator's display preference.
	payload := &store.RequestLogDebugPayload{Redacted: true}
	if settings.SaveRawPrompt {
		raw := redactDebugSecrets(compactDebugJSON(body))
		payload.RawPromptBytes = len(raw)
		payload.RawPrompt, payload.RawPromptTruncated = truncateDebugString(raw, limit)
		if settings.CompactDebugPayloads {
			applyCompactDebugPrompt(payload, raw, limit)
		}
	}
	if settings.SaveRawToolResult {
		raw := redactDebugSecrets(strings.Join(extractToolResultTexts(body), "\n\n--- tool result ---\n\n"))
		payload.RawToolResultBytes = len(raw)
		payload.RawToolResult, payload.RawToolTruncated = truncateDebugString(raw, limit)
		if settings.CompactDebugPayloads {
			applyCompactDebugToolResult(payload, raw, limit)
		}
	}
	if payload.RawPrompt == "" && payload.RawToolResult == "" {
		return nil
	}
	return payload
}

func applyCompactDebugPrompt(payload *store.RequestLogDebugPayload, raw string, limit int) {
	res := tokenopt.CompactJSON(raw, tokenopt.Options{MinChars: 4096, MaxChars: limit / 2, PreserveErrors: true})
	if !res.Applied {
		return
	}
	payload.CompactPromptApplied = true
	payload.CompactPromptBytes = len(res.Text)
	payload.EstimatedPromptTokensSaved = res.EstimatedSavedTokens
	payload.CompactPrompt, _ = truncateDebugString(res.Text, limit)
}

func applyCompactDebugToolResult(payload *store.RequestLogDebugPayload, raw string, limit int) {
	if strings.TrimSpace(raw) == "" {
		return
	}
	res := tokenopt.CompactToolResult(raw, tokenopt.Options{MinChars: 4096, MaxChars: limit / 2, PreserveErrors: true})
	if !res.Applied {
		return
	}
	payload.CompactToolApplied = true
	payload.CompactToolResultBytes = len(res.Text)
	payload.EstimatedToolTokensSaved = res.EstimatedSavedTokens
	payload.CompactToolResult, _ = truncateDebugString(res.Text, limit)
}

func compactDebugJSON(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(raw)
}

func truncateDebugString(value string, limit int) (string, bool) {
	if limit <= 0 || len(value) <= limit {
		return value, false
	}
	return value[:limit] + "\n...[truncated]", true
}

func redactDebugSecrets(value string) string {
	out := value
	for _, pattern := range debugSecretPatterns {
		out = pattern.ReplaceAllString(out, `${1}***REDACTED***`)
	}
	return out
}

func extractToolResultTexts(body map[string]any) []string {
	out := []string{}
	walkDebugValue(body, func(m map[string]any) {
		switch strings.TrimSpace(debugString(m["type"])) {
		case "tool_result", "function_call_output":
			if content := m["content"]; content != nil {
				out = append(out, debugValueToString(content))
			} else if output := m["output"]; output != nil {
				out = append(out, debugValueToString(output))
			}
		}
	})
	return out
}

func walkDebugValue(value any, visit func(map[string]any)) {
	switch v := value.(type) {
	case map[string]any:
		visit(v)
		for _, child := range v {
			walkDebugValue(child, visit)
		}
	case []any:
		for _, child := range v {
			walkDebugValue(child, visit)
		}
	case []map[string]any:
		for _, child := range v {
			walkDebugValue(child, visit)
		}
	}
}

func debugValueToString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	default:
		return compactDebugJSON(v)
	}
}

func debugString(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}
