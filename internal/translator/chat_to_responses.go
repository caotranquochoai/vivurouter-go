package translator

import (
	"encoding/json"
	"strings"
)

// ChatToResponses converts a minimal OpenAI Chat Completions body to OpenAI Responses API shape.
func ChatToResponses(model string, chat map[string]any) map[string]any {
	out := map[string]any{
		"model":  model,
		"input":  []map[string]any{},
		"stream": true,
		"store":  false,
	}

	input := []map[string]any{}
	messages := asSlice(chat["messages"])
	for _, raw := range messages {
		msg := asMap(raw)
		role := asString(msg["role"])
		if role == "system" {
			content := chatContentToResponses("user", msg["content"])
			if len(content) > 0 {
				input = append(input, map[string]any{"type": "message", "role": "developer", "content": content})
			}
			continue
		}
		if role == "user" || role == "assistant" {
			content := chatContentToResponses(role, msg["content"])
			if len(content) > 0 {
				input = append(input, map[string]any{"type": "message", "role": role, "content": content})
			}
		}
		if role == "assistant" {
			for _, rawTool := range asSlice(msg["tool_calls"]) {
				tool := asMap(rawTool)
				fn := asMap(tool["function"])
				input = append(input, map[string]any{
					"type":      "function_call",
					"call_id":   clampCallID(asString(tool["id"])),
					"name":      asString(fn["name"]),
					"arguments": asString(fn["arguments"]),
				})
			}
		}
		if role == "tool" {
			input = append(input, map[string]any{
				"type":    "function_call_output",
				"call_id": clampCallID(asString(msg["tool_call_id"])),
				"output":  contentToString(msg["content"]),
			})
		}
	}
	out["instructions"] = ""
	out["input"] = input

	if tools := asSlice(chat["tools"]); len(tools) > 0 {
		out["tools"] = chatToolsToResponses(tools)
	}
	if value, ok := chat["reasoning_effort"]; ok {
		out["reasoning"] = map[string]any{"effort": value, "summary": "auto"}
	}
	return out
}

func chatContentToResponses(role string, content any) []map[string]any {
	textType := "input_text"
	if role == "assistant" {
		textType = "output_text"
	}
	if s, ok := content.(string); ok {
		if strings.TrimSpace(s) == "" {
			return nil
		}
		return []map[string]any{{"type": textType, "text": s}}
	}
	out := []map[string]any{}
	for _, raw := range asSlice(content) {
		part := asMap(raw)
		switch asString(part["type"]) {
		case "text":
			out = append(out, map[string]any{"type": textType, "text": asString(part["text"])})
		case "image_url":
			imageURL := part["image_url"]
			url := ""
			detail := "auto"
			if m := asMap(imageURL); len(m) > 0 {
				url = asString(m["url"])
				if d := asString(m["detail"]); d != "" {
					detail = d
				}
			} else {
				url = asString(imageURL)
			}
			out = append(out, map[string]any{"type": "input_image", "image_url": url, "detail": detail})
		case "input_image":
			out = append(out, part)
		default:
			out = append(out, map[string]any{"type": textType, "text": contentToString(part)})
		}
	}
	return out
}

func chatToolsToResponses(tools []any) []map[string]any {
	out := []map[string]any{}
	for _, raw := range tools {
		tool := asMap(raw)
		if asString(tool["type"]) != "function" {
			continue
		}
		fn := asMap(tool["function"])
		name := asString(fn["name"])
		if name == "" {
			continue
		}
		params := fn["parameters"]
		if params == nil {
			params = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		out = append(out, map[string]any{"type": "function", "name": name, "description": asString(fn["description"]), "parameters": params})
	}
	return out
}

func clampCallID(id string) string {
	if len(id) > 64 {
		return id[:64]
	}
	if id == "" {
		return "call_unknown"
	}
	return id
}

func contentToString(content any) string {
	if s, ok := content.(string); ok {
		return s
	}
	raw, err := json.Marshal(content)
	if err != nil {
		return ""
	}
	return string(raw)
}

func asSlice(v any) []any {
	switch s := v.(type) {
	case []any:
		return s
	case []map[string]any:
		out := make([]any, 0, len(s))
		for _, item := range s {
			out = append(out, item)
		}
		return out
	default:
		return nil
	}
}

func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
