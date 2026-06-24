package provider

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func antigravityOpenAIResponse(model string, payload any) map[string]any {
	root, _ := payload.(map[string]any)
	response := root
	if nested, ok := root["response"].(map[string]any); ok {
		response = nested
	}
	content, finish := antigravityChoiceContent(response)
	usage := antigravityUsage(response)
	return map[string]any{
		"id":      antigravityResponseID(response),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []any{map[string]any{
			"index":         0,
			"message":       map[string]any{"role": "assistant", "content": content},
			"finish_reason": finish,
		}},
		"usage": usage,
	}
}

func antigravityChoiceContent(response map[string]any) (string, string) {
	candidates, _ := response["candidates"].([]any)
	if len(candidates) == 0 {
		return "", "stop"
	}
	candidate, _ := candidates[0].(map[string]any)
	finish := antigravityFinishReason(asString(candidate["finishReason"]))
	contentMap, _ := candidate["content"].(map[string]any)
	parts, _ := contentMap["parts"].([]any)
	texts := []string{}
	for _, item := range parts {
		part, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if text := asString(part["text"]); text != "" {
			texts = append(texts, text)
		}
	}
	return strings.Join(texts, ""), finish
}

func antigravityUsage(response map[string]any) map[string]any {
	usageMeta, _ := response["usageMetadata"].(map[string]any)
	prompt := int(numericValue(firstPresent(usageMeta, "promptTokenCount", "prompt_tokens")))
	completion := int(numericValue(firstPresent(usageMeta, "candidatesTokenCount", "completion_tokens")))
	total := int(numericValue(firstPresent(usageMeta, "totalTokenCount", "total_tokens")))
	if total == 0 {
		total = prompt + completion
	}
	return map[string]any{"prompt_tokens": prompt, "completion_tokens": completion, "total_tokens": total}
}

func firstPresent(m map[string]any, keys ...string) any {
	for _, key := range keys {
		if m != nil && m[key] != nil {
			return m[key]
		}
	}
	return nil
}

func antigravityResponseID(response map[string]any) string {
	if id := strings.TrimSpace(asString(response["responseId"])); id != "" {
		return id
	}
	return fmt.Sprintf("chatcmpl-ag-%d", time.Now().UnixNano())
}

func antigravityFinishReason(reason string) string {
	switch strings.ToUpper(strings.TrimSpace(reason)) {
	case "MAX_TOKENS":
		return "length"
	case "SAFETY":
		return "content_filter"
	default:
		return "stop"
	}
}

func antigravityRewriteJSONResponse(resp *http.Response, model string) (*http.Response, error) {
	defer resp.Body.Close()
	var payload any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	converted := antigravityOpenAIResponse(model, payload)
	raw, err := json.Marshal(converted)
	if err != nil {
		return nil, err
	}
	out := cloneHTTPResponse(resp, raw, "application/json")
	return out, nil
}

func antigravityRewriteSSEResponse(resp *http.Response, model string) (*http.Response, error) {
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	converted, err := antigravitySSEToOpenAI(raw, model)
	if err != nil {
		return nil, err
	}
	return cloneHTTPResponse(resp, converted, "text/event-stream"), nil
}

func cloneHTTPResponse(resp *http.Response, raw []byte, contentType string) *http.Response {
	out := new(http.Response)
	*out = *resp
	out.Body = io.NopCloser(bytes.NewReader(raw))
	out.ContentLength = int64(len(raw))
	out.Header = resp.Header.Clone()
	out.Header.Set("Content-Type", contentType)
	out.Header.Set("Content-Length", fmt.Sprintf("%d", len(raw)))
	return out
}

func antigravitySSEToOpenAI(raw []byte, model string) ([]byte, error) {
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var out bytes.Buffer
	id := fmt.Sprintf("chatcmpl-ag-%d", time.Now().UnixNano())
	created := time.Now().Unix()
	finished := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var payload any
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			continue
		}
		root, _ := payload.(map[string]any)
		response := root
		if nested, ok := root["response"].(map[string]any); ok {
			response = nested
		}
		content, finish := antigravityChoiceContent(response)
		if content != "" {
			chunk := map[string]any{"id": id, "object": "chat.completion.chunk", "created": created, "model": model, "choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": content}}}}
			writeSSEJSON(&out, chunk)
		}
		if hasAntigravityFinish(response) {
			finished = true
			chunk := map[string]any{"id": id, "object": "chat.completion.chunk", "created": created, "model": model, "choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": finish}}}
			writeSSEJSON(&out, chunk)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if !finished {
		chunk := map[string]any{"id": id, "object": "chat.completion.chunk", "created": created, "model": model, "choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}}}
		writeSSEJSON(&out, chunk)
	}
	out.WriteString("data: [DONE]\n\n")
	return out.Bytes(), nil
}

func hasAntigravityFinish(response map[string]any) bool {
	candidates, _ := response["candidates"].([]any)
	if len(candidates) == 0 {
		return false
	}
	candidate, _ := candidates[0].(map[string]any)
	return strings.TrimSpace(asString(candidate["finishReason"])) != ""
}

func writeSSEJSON(out *bytes.Buffer, payload map[string]any) {
	raw, _ := json.Marshal(payload)
	out.WriteString("data: ")
	out.Write(raw)
	out.WriteString("\n\n")
}
