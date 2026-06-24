package provider

import (
	"encoding/json"
	"fmt"
	"strings"
)

const maxAntigravityOutputTokens = 16384

func antigravityRequest(model string, body map[string]any, sessionID string) map[string]any {
	request := map[string]any{
		"contents":  antigravityContents(body["messages"]),
		"sessionId": sessionID,
	}
	if system := antigravitySystemInstruction(body["messages"]); system != "" {
		request["systemInstruction"] = map[string]any{"parts": []any{map[string]any{"text": system}}}
	}
	if config := antigravityGenerationConfig(body); len(config) > 0 {
		request["generationConfig"] = config
	}
	if tools := antigravityTools(body["tools"]); len(tools) > 0 {
		request["tools"] = tools
		request["toolConfig"] = map[string]any{"functionCallingConfig": map[string]any{"mode": "VALIDATED"}}
	}
	return map[string]any{
		"project":     strings.TrimSpace(asString(body["project"])),
		"model":       model,
		"userAgent":   "antigravity",
		"requestType": "agent",
		"requestId":   "agent-" + sessionID,
		"request":     request,
	}
}

func antigravityContents(value any) []any {
	messages := normalizeChatMessages(value)
	contents := make([]any, 0, len(messages))
	for _, msg := range messages {
		role := strings.TrimSpace(asString(msg["role"]))
		if role == "system" {
			continue
		}
		agRole := role
		if role == "assistant" {
			agRole = "model"
		}
		if agRole == "" || agRole == "tool" {
			agRole = "user"
		}
		parts := antigravityPartsFromContent(msg["content"])
		if len(parts) == 0 {
			if calls := antigravityFunctionCalls(msg["tool_calls"]); len(calls) > 0 {
				parts = calls
			}
		}
		if len(parts) == 0 {
			continue
		}
		contents = append(contents, map[string]any{"role": agRole, "parts": parts})
	}
	return contents
}

func antigravitySystemInstruction(value any) string {
	messages := normalizeChatMessages(value)
	parts := []string{}
	for _, msg := range messages {
		if strings.TrimSpace(asString(msg["role"])) == "system" {
			text := strings.TrimSpace(chatContentText(msg["content"]))
			if text != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, "\n\n")
}

func normalizeChatMessages(value any) []map[string]any {
	switch messages := value.(type) {
	case []map[string]any:
		return messages
	case []any:
		out := make([]map[string]any, 0, len(messages))
		for _, item := range messages {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	case []map[string]string:
		out := make([]map[string]any, 0, len(messages))
		for _, item := range messages {
			m := make(map[string]any, len(item))
			for k, v := range item {
				m[k] = v
			}
			out = append(out, m)
		}
		return out
	default:
		return nil
	}
}

func antigravityPartsFromContent(value any) []any {
	text := chatContentText(value)
	if strings.TrimSpace(text) == "" {
		return nil
	}
	return []any{map[string]any{"text": text}}
}

func chatContentText(value any) string {
	switch content := value.(type) {
	case string:
		return content
	case []any:
		parts := []string{}
		for _, item := range content {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if text := asString(m["text"]); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "")
	default:
		return asString(value)
	}
}

func antigravityGenerationConfig(body map[string]any) map[string]any {
	config := map[string]any{}
	if maxTokens := numericValue(body["max_tokens"]); maxTokens > 0 {
		if maxTokens > maxAntigravityOutputTokens {
			maxTokens = maxAntigravityOutputTokens
		}
		config["maxOutputTokens"] = maxTokens
	}
	if temperature, ok := optionalNumeric(body["temperature"]); ok {
		config["temperature"] = temperature
	}
	if topP, ok := optionalNumeric(body["top_p"]); ok {
		config["topP"] = topP
	}
	return config
}

func optionalNumeric(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

func numericValue(value any) float64 {
	f, ok := optionalNumeric(value)
	if !ok {
		return 0
	}
	return f
}

func antigravityTools(value any) []any {
	items, ok := value.([]any)
	if !ok || len(items) == 0 {
		return nil
	}
	declarations := []any{}
	for _, item := range items {
		tool, ok := item.(map[string]any)
		if !ok {
			continue
		}
		fn, ok := tool["function"].(map[string]any)
		if !ok {
			continue
		}
		name := sanitizeAntigravityFunctionName(asString(fn["name"]))
		if name == "" {
			continue
		}
		decl := map[string]any{"name": name}
		if desc := asString(fn["description"]); desc != "" {
			decl["description"] = desc
		}
		if params, ok := fn["parameters"].(map[string]any); ok {
			decl["parameters"] = normalizeAntigravitySchema(params)
		} else {
			decl["parameters"] = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		declarations = append(declarations, decl)
	}
	if len(declarations) == 0 {
		return nil
	}
	return []any{map[string]any{"functionDeclarations": declarations}}
}

func antigravityFunctionCalls(value any) []any {
	calls, ok := value.([]any)
	if !ok {
		return nil
	}
	parts := []any{}
	for _, item := range calls {
		call, ok := item.(map[string]any)
		if !ok {
			continue
		}
		fn, ok := call["function"].(map[string]any)
		if !ok {
			continue
		}
		args := map[string]any{}
		if raw := asString(fn["arguments"]); raw != "" {
			_ = json.Unmarshal([]byte(raw), &args)
		}
		parts = append(parts, map[string]any{"functionCall": map[string]any{"name": sanitizeAntigravityFunctionName(asString(fn["name"])), "args": args}})
	}
	return parts
}

func sanitizeAntigravityFunctionName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "_unknown"
	}
	var b strings.Builder
	for _, r := range name {
		valid := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '.' || r == ':' || r == '-'
		if valid {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
		if b.Len() >= 64 {
			break
		}
	}
	out := b.String()
	if out == "" {
		return "_unknown"
	}
	first := out[0]
	if !((first >= 'a' && first <= 'z') || (first >= 'A' && first <= 'Z') || first == '_') {
		out = "_" + out
		if len(out) > 64 {
			out = out[:64]
		}
	}
	return out
}

func normalizeAntigravitySchema(schema map[string]any) map[string]any {
	out := make(map[string]any, len(schema))
	for k, v := range schema {
		if k == "enumDescriptions" {
			continue
		}
		if k == "type" {
			out[k] = strings.ToLower(asString(v))
			continue
		}
		if child, ok := v.(map[string]any); ok {
			out[k] = normalizeAntigravitySchema(child)
			continue
		}
		if list, ok := v.([]any); ok {
			converted := make([]any, 0, len(list))
			for _, item := range list {
				if child, ok := item.(map[string]any); ok {
					converted = append(converted, normalizeAntigravitySchema(child))
				} else {
					converted = append(converted, item)
				}
			}
			out[k] = converted
			continue
		}
		out[k] = v
	}
	if _, ok := out["type"]; !ok {
		out["type"] = "object"
	}
	return out
}

func antigravityModelCatalog() []ModelInfo {
	return []ModelInfo{
		{ID: "gemini-3-flash-agent", Name: "Gemini 3.5 Flash (High)"},
		{ID: "gemini-3.5-flash-low", Name: "Gemini 3.5 Flash (Medium)"},
		{ID: "gemini-3.5-flash-extra-low", Name: "Gemini 3.5 Flash (Low)"},
		{ID: "gemini-pro-agent", Name: "Gemini 3.1 Pro (High)"},
		{ID: "gemini-3.1-pro-low", Name: "Gemini 3.1 Pro (Low)"},
		{ID: "claude-sonnet-4-6", Name: "Claude Sonnet 4.6 (Thinking)"},
		{ID: "claude-opus-4-6-thinking", Name: "Claude Opus 4.6 (Thinking)"},
		{ID: "gpt-oss-120b-medium", Name: "GPT-OSS 120B (Medium)"},
		{ID: "gemini-3-flash", Name: "Gemini 3 Flash"},
	}
}

func antigravityDefaultModels() []string {
	models := antigravityModelCatalog()
	out := make([]string, 0, len(models))
	for _, model := range models {
		out = append(out, model.ID)
	}
	return out
}

func antigravityFormatError(format string, args ...any) error {
	return fmt.Errorf("Antigravity: "+format, args...)
}
