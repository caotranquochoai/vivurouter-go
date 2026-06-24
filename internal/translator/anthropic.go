package translator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// AnthropicMessagesToChat converts an Anthropic Messages API body to an
// OpenAI-compatible Chat Completions body. It intentionally keeps the output as
// map[string]any so provider-specific fields can still pass through where safe.
func AnthropicMessagesToChat(body map[string]any, model string) map[string]any {
	out := map[string]any{"model": model}

	messages := []map[string]any{}
	if system := anthropicContentToString(body["system"]); strings.TrimSpace(system) != "" {
		messages = append(messages, map[string]any{"role": "system", "content": system})
	}

	for _, raw := range asSlice(body["messages"]) {
		msg := asMap(raw)
		role := asString(msg["role"])
		content := msg["content"]
		switch role {
		case "assistant":
			chatMsg := map[string]any{"role": "assistant"}
			parts := []map[string]any{}
			toolCalls := []map[string]any{}
			for _, block := range anthropicBlocks(content) {
				switch asString(block["type"]) {
				case "text":
					if text := asString(block["text"]); text != "" {
						parts = append(parts, map[string]any{"type": "text", "text": text})
					}
				case "tool_use":
					args := compactJSONString(block["input"])
					toolCalls = append(toolCalls, map[string]any{
						"id":   clampCallID(asString(block["id"])),
						"type": "function",
						"function": map[string]any{
							"name":      asString(block["name"]),
							"arguments": args,
						},
					})
				default:
					if encoded := compactJSONString(block); encoded != "" {
						parts = append(parts, map[string]any{"type": "text", "text": encoded})
					}
				}
			}
			chatMsg["content"] = openAIContent(parts)
			if len(toolCalls) > 0 {
				chatMsg["tool_calls"] = toolCalls
			}
			messages = append(messages, chatMsg)
		case "user":
			userParts := []map[string]any{}
			for _, block := range anthropicBlocks(content) {
				switch asString(block["type"]) {
				case "text":
					if text := asString(block["text"]); text != "" {
						userParts = append(userParts, map[string]any{"type": "text", "text": text})
					}
				case "image":
					if part := anthropicImageToOpenAI(block); len(part) > 0 {
						userParts = append(userParts, part)
					}
				case "tool_result":
					messages = append(messages, map[string]any{
						"role":         "tool",
						"tool_call_id": clampCallID(asString(block["tool_use_id"])),
						"content":      anthropicContentToString(block["content"]),
					})
				default:
					if encoded := compactJSONString(block); encoded != "" {
						userParts = append(userParts, map[string]any{"type": "text", "text": encoded})
					}
				}
			}
			if len(userParts) > 0 || !hasOnlyToolResults(content) {
				messages = append(messages, map[string]any{"role": "user", "content": openAIContent(userParts)})
			}
		default:
			if role != "" {
				messages = append(messages, map[string]any{"role": role, "content": anthropicContentToString(content)})
			}
		}
	}
	messages = fixMissingToolResponses(messages)
	out["messages"] = messages

	copyIfPresent(out, body, "temperature")
	copyIfPresent(out, body, "top_p")
	copyIfPresent(out, body, "stream")
	copyIfPresent(out, body, "metadata")
	if maxTokens, ok := body["max_tokens"]; ok {
		out["max_tokens"] = maxTokens
	}
	if stop := body["stop_sequences"]; stop != nil {
		out["stop"] = stop
	}
	if tools := anthropicToolsToOpenAI(asSlice(body["tools"])); len(tools) > 0 {
		out["tools"] = tools
	}
	if toolChoice := anthropicToolChoiceToOpenAI(body["tool_choice"]); toolChoice != nil {
		out["tool_choice"] = toolChoice
	}
	return out
}

// AnthropicMessagesToResponses converts an Anthropic Messages API body directly
// to OpenAI Responses API shape for Codex-like upstreams. This avoids the
// Anthropic -> Chat -> Responses double conversion so Claude Code history keeps
// a more stable cacheable prefix.
func AnthropicMessagesToResponses(body map[string]any, model string) map[string]any {
	out := map[string]any{
		"model":        model,
		"input":        []map[string]any{},
		"stream":       true,
		"store":        false,
		"instructions": "",
	}
	input := []map[string]any{}
	if system := anthropicContentToString(body["system"]); strings.TrimSpace(system) != "" {
		input = append(input, map[string]any{"type": "message", "role": "developer", "content": []map[string]any{{"type": "input_text", "text": system}}})
	}
	for _, raw := range asSlice(body["messages"]) {
		msg := asMap(raw)
		role := asString(msg["role"])
		switch role {
		case "user":
			userContent := []map[string]any{}
			for _, block := range anthropicBlocks(msg["content"]) {
				switch asString(block["type"]) {
				case "text":
					if text := asString(block["text"]); text != "" {
						userContent = append(userContent, map[string]any{"type": "input_text", "text": text})
					}
				case "image":
					if part := anthropicImageToResponses(block); len(part) > 0 {
						userContent = append(userContent, part)
					}
				case "tool_result":
					input = append(input, map[string]any{"type": "function_call_output", "call_id": clampCallID(asString(block["tool_use_id"])), "output": anthropicContentToString(block["content"])})
				default:
					if encoded := compactJSONString(block); encoded != "" {
						userContent = append(userContent, map[string]any{"type": "input_text", "text": encoded})
					}
				}
			}
			if len(userContent) > 0 {
				input = append(input, map[string]any{"type": "message", "role": "user", "content": userContent})
			}
		case "assistant":
			assistantContent := []map[string]any{}
			for _, block := range anthropicBlocks(msg["content"]) {
				switch asString(block["type"]) {
				case "text":
					if text := asString(block["text"]); text != "" {
						assistantContent = append(assistantContent, map[string]any{"type": "output_text", "text": text})
					}
				case "tool_use":
					if len(assistantContent) > 0 {
						input = append(input, map[string]any{"type": "message", "role": "assistant", "content": assistantContent})
						assistantContent = nil
					}
					input = append(input, map[string]any{"type": "function_call", "call_id": clampCallID(asString(block["id"])), "name": asString(block["name"]), "arguments": compactJSONString(block["input"])})
				default:
					if encoded := compactJSONString(block); encoded != "" {
						assistantContent = append(assistantContent, map[string]any{"type": "output_text", "text": encoded})
					}
				}
			}
			if len(assistantContent) > 0 {
				input = append(input, map[string]any{"type": "message", "role": "assistant", "content": assistantContent})
			}
		}
	}
	out["input"] = input
	if tools := anthropicToolsToResponses(asSlice(body["tools"])); len(tools) > 0 {
		out["tools"] = tools
	}
	if toolChoice := anthropicToolChoiceToResponses(body["tool_choice"]); toolChoice != nil {
		out["tool_choice"] = toolChoice
	}
	copyIfPresent(out, body, "metadata")
	copyIfPresent(out, body, "service_tier")
	copyIfPresent(out, body, "reasoning")
	return out
}

// response to Anthropic Messages API response shape.
func ChatJSONToAnthropic(raw []byte, fallbackModel string) ([]byte, error) {
	var payload map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return nil, err
	}
	choices := asSlice(payload["choices"])
	message := map[string]any{}
	finishReason := "stop"
	if len(choices) > 0 {
		choice := asMap(choices[0])
		message = asMap(choice["message"])
		finishReason = asString(choice["finish_reason"])
	}
	model := asString(payload["model"])
	if model == "" {
		model = fallbackModel
	}
	content := []map[string]any{}
	if text := asString(message["content"]); text != "" {
		content = append(content, map[string]any{"type": "text", "text": text})
	}
	for _, rawTool := range asSlice(message["tool_calls"]) {
		tool := asMap(rawTool)
		fn := asMap(tool["function"])
		content = append(content, map[string]any{
			"type":  "tool_use",
			"id":    clampCallID(asString(tool["id"])),
			"name":  asString(fn["name"]),
			"input": parseJSONOrString(asString(fn["arguments"])),
		})
	}
	if len(content) == 0 {
		content = append(content, map[string]any{"type": "text", "text": ""})
	}
	out := map[string]any{
		"id":            firstString(payload, "id", fmt.Sprintf("msg_%d", 0)),
		"type":          "message",
		"role":          "assistant",
		"model":         model,
		"content":       content,
		"stop_reason":   anthropicStopReason(finishReason),
		"stop_sequence": nil,
		"usage":         chatUsageToAnthropic(asMap(payload["usage"])),
	}
	return json.Marshal(out)
}

func fixMissingToolResponses(messages []map[string]any) []map[string]any {
	for i := 0; i < len(messages); i++ {
		msg := messages[i]
		if asString(msg["role"]) != "assistant" {
			continue
		}
		toolCalls := mapSlice(msg["tool_calls"])
		if len(toolCalls) == 0 {
			continue
		}
		toolIDs := []string{}
		for _, tool := range toolCalls {
			id := asString(tool["id"])
			if id != "" {
				toolIDs = append(toolIDs, id)
			}
		}
		responded := map[string]bool{}
		insertAt := i + 1
		for j := i + 1; j < len(messages); j++ {
			next := messages[j]
			if asString(next["role"]) != "tool" {
				break
			}
			if id := asString(next["tool_call_id"]); id != "" {
				responded[id] = true
			}
			insertAt = j + 1
		}
		missing := []map[string]any{}
		for _, id := range toolIDs {
			if !responded[id] {
				missing = append(missing, map[string]any{"role": "tool", "tool_call_id": id, "content": "[No response received]"})
			}
		}
		if len(missing) == 0 {
			continue
		}
		messages = append(messages[:insertAt], append(missing, messages[insertAt:]...)...)
		i = insertAt + len(missing) - 1
	}
	return messages
}

func mapSlice(value any) []map[string]any {
	switch v := value.(type) {
	case []map[string]any:
		return v
	case []any:
		out := []map[string]any{}
		for _, raw := range v {
			if m := asMap(raw); len(m) > 0 {
				out = append(out, m)
			}
		}
		return out
	default:
		return nil
	}
}

func anthropicBlocks(content any) []map[string]any {
	if s, ok := content.(string); ok {
		return []map[string]any{{"type": "text", "text": s}}
	}
	out := []map[string]any{}
	for _, raw := range asSlice(content) {
		block := asMap(raw)
		if len(block) > 0 {
			out = append(out, block)
		}
	}
	return out
}

func anthropicContentToString(content any) string {
	if s, ok := content.(string); ok {
		return s
	}
	parts := []string{}
	for _, block := range anthropicBlocks(content) {
		switch asString(block["type"]) {
		case "text":
			parts = append(parts, asString(block["text"]))
		default:
			parts = append(parts, compactJSONString(block))
		}
	}
	return strings.Join(parts, "\n")
}

func openAIContent(parts []map[string]any) any {
	if len(parts) == 0 {
		return ""
	}
	allText := true
	texts := []string{}
	for _, part := range parts {
		if asString(part["type"]) != "text" {
			allText = false
			break
		}
		texts = append(texts, asString(part["text"]))
	}
	if allText {
		return strings.Join(texts, "\n")
	}
	return parts
}

func anthropicImageToOpenAI(block map[string]any) map[string]any {
	source := asMap(block["source"])
	if asString(source["type"]) == "base64" {
		mediaType := asString(source["media_type"])
		data := asString(source["data"])
		if mediaType != "" && data != "" {
			return map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:" + mediaType + ";base64," + data}}
		}
	}
	if url := asString(source["url"]); url != "" {
		return map[string]any{"type": "image_url", "image_url": map[string]any{"url": url}}
	}
	return nil
}

func anthropicImageToResponses(block map[string]any) map[string]any {
	source := asMap(block["source"])
	if asString(source["type"]) == "base64" {
		mediaType := asString(source["media_type"])
		data := asString(source["data"])
		if mediaType != "" && data != "" {
			return map[string]any{"type": "input_image", "image_url": "data:" + mediaType + ";base64," + data, "detail": "auto"}
		}
	}
	if url := asString(source["url"]); url != "" {
		return map[string]any{"type": "input_image", "image_url": url, "detail": "auto"}
	}
	return nil
}

func anthropicToolsToResponses(tools []any) []map[string]any {
	out := []map[string]any{}
	for _, raw := range tools {
		tool := asMap(raw)
		name := asString(tool["name"])
		if name == "" {
			continue
		}
		params := tool["input_schema"]
		if params == nil {
			params = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		entry := map[string]any{"type": "function", "name": name, "parameters": params}
		if description := asString(tool["description"]); description != "" {
			entry["description"] = description
		}
		out = append(out, entry)
	}
	return out
}

func anthropicToolChoiceToResponses(value any) any {
	choice := asMap(value)
	switch asString(choice["type"]) {
	case "auto":
		return "auto"
	case "any":
		return "required"
	case "tool":
		name := asString(choice["name"])
		if name == "" {
			return nil
		}
		return map[string]any{"type": "function", "name": name}
	default:
		return nil
	}
}

func anthropicToolsToOpenAI(tools []any) []map[string]any {
	out := []map[string]any{}
	for _, raw := range tools {
		tool := asMap(raw)
		name := asString(tool["name"])
		if name == "" {
			continue
		}
		params := tool["input_schema"]
		if params == nil {
			params = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		out = append(out, map[string]any{"type": "function", "function": map[string]any{"name": name, "description": asString(tool["description"]), "parameters": params}})
	}
	return out
}

func anthropicToolChoiceToOpenAI(value any) any {
	choice := asMap(value)
	switch asString(choice["type"]) {
	case "auto":
		return "auto"
	case "any":
		return "required"
	case "tool":
		name := asString(choice["name"])
		if name == "" {
			return nil
		}
		return map[string]any{"type": "function", "function": map[string]any{"name": name}}
	default:
		return nil
	}
}

func hasOnlyToolResults(content any) bool {
	blocks := anthropicBlocks(content)
	if len(blocks) == 0 {
		return false
	}
	for _, block := range blocks {
		if asString(block["type"]) != "tool_result" {
			return false
		}
	}
	return true
}

func chatUsageToAnthropic(usage map[string]any) map[string]any {
	return map[string]any{
		"input_tokens":  intAny(usage["prompt_tokens"]),
		"output_tokens": intAny(usage["completion_tokens"]),
	}
}

func anthropicStopReason(reason string) string {
	switch reason {
	case "length":
		return "max_tokens"
	case "tool_calls":
		return "tool_use"
	case "content_filter":
		return "stop_sequence"
	default:
		return "end_turn"
	}
}

func copyIfPresent(dst map[string]any, src map[string]any, key string) {
	if value, ok := src[key]; ok {
		dst[key] = value
	}
}

func compactJSONString(value any) string {
	if value == nil {
		return "{}"
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return string(raw)
	}
	return buf.String()
}

func parseJSONOrString(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return map[string]any{}
	}
	var parsed any
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.UseNumber()
	if err := decoder.Decode(&parsed); err == nil {
		return parsed
	}
	return value
}

func firstString(m map[string]any, key string, fallback string) string {
	if value := asString(m[key]); value != "" {
		return value
	}
	return fallback
}

func intAny(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return int(i)
		}
		if f, err := v.Float64(); err == nil {
			return int(f)
		}
	}
	return 0
}
