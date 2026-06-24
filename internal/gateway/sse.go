package gateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func isSSEResponse(resp *http.Response) bool {
	contentType := resp.Header.Get("Content-Type")
	return strings.Contains(contentType, "text/event-stream")
}

func streamPassthrough(ctx context.Context, w http.ResponseWriter, resp *http.Response, requestBody map[string]any) (usageInfo, error) {
	copyHeaders(w.Header(), resp.Header)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(resp.StatusCode)
	flusher, _ := w.(http.Flusher)
	reader := bufio.NewReader(resp.Body)
	var eventName string
	var dataLines []string
	usage := usageInfo{}
	outputChars := 0

	processEvent := func(eventType string, data string) {
		if strings.TrimSpace(data) == "" || strings.TrimSpace(data) == "[DONE]" {
			return
		}
		var payload map[string]any
		decoder := json.NewDecoder(strings.NewReader(data))
		decoder.UseNumber()
		if err := decoder.Decode(&payload); err != nil {
			outputChars += len(data)
			return
		}
		if eventType == "" {
			if t, ok := payload["type"].(string); ok {
				eventType = t
			}
		}
		if extracted, ok := extractUsageFromPayload(payload); ok {
			usage = extracted
		}
		outputChars += outputCharsFromPayload(payload, eventType)
	}

	flushRaw := func(raw []byte) error {
		if len(raw) == 0 {
			return nil
		}
		if _, writeErr := w.Write(raw); writeErr != nil {
			return writeErr
		}
		if flusher != nil {
			flusher.Flush()
		}
		return nil
	}

	for {
		select {
		case <-ctx.Done():
			return usage, ctx.Err()
		default:
		}
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			if writeErr := flushRaw(line); writeErr != nil {
				return usage, writeErr
			}
			trimmed := strings.TrimRight(string(line), "\r\n")
			if trimmed == "" {
				processEvent(eventName, strings.Join(dataLines, "\n"))
				eventName = ""
				dataLines = nil
			} else if strings.HasPrefix(trimmed, "event:") {
				eventName = strings.TrimSpace(strings.TrimPrefix(trimmed, "event:"))
			} else if strings.HasPrefix(trimmed, "data:") {
				dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(trimmed, "data:")))
			}
		}
		if err != nil {
			if err == io.EOF {
				if len(dataLines) > 0 {
					processEvent(eventName, strings.Join(dataLines, "\n"))
				}
				if !usage.hasTokens() {
					usage = estimateUsage(requestBody, outputChars)
				}
				return usage, nil
			}
			return usage, err
		}
	}
}

func streamResponsesToChat(ctx context.Context, w http.ResponseWriter, resp *http.Response, model string, requestBody map[string]any) (usageInfo, error) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)

	reader := bufio.NewReader(resp.Body)
	var eventName string
	var dataLines []string
	chatID := fmt.Sprintf("chatcmpl-%d", nowUnixMillis())
	created := nowUnix()
	toolCallIndex := 0
	currentToolCallID := ""
	usage := usageInfo{}
	outputChars := 0

	emit := func(chunk map[string]any) error {
		raw, err := json.Marshal(chunk)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", raw); err != nil {
			return err
		}
		if flusher != nil {
			flusher.Flush()
		}
		return nil
	}

	finalSent := false
	final := func(finishReason string) error {
		if finalSent {
			return nil
		}
		finalSent = true
		if !usage.hasTokens() {
			usage = estimateUsage(requestBody, outputChars)
		}
		chunk := map[string]any{
			"id":      chatID,
			"object":  "chat.completion.chunk",
			"created": created,
			"model":   model,
			"choices": []map[string]any{{"index": 0, "delta": map[string]any{}, "finish_reason": finishReason}},
		}
		if err := emit(chunk); err != nil {
			return err
		}
		if err := emit(openAIUsageChunk(chatID, created, model, usage)); err != nil {
			return err
		}
		_, err := io.WriteString(w, "data: [DONE]\n\n")
		if flusher != nil {
			flusher.Flush()
		}
		return err
	}

	process := func(eventType string, data string) (bool, error) {
		if strings.TrimSpace(data) == "[DONE]" {
			return true, final("stop")
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			return false, nil
		}
		if eventType == "" {
			if t, ok := payload["type"].(string); ok {
				eventType = t
			}
		}
		if eventType == "response.output_text.delta" {
			delta, _ := payload["delta"].(string)
			if delta == "" {
				return false, nil
			}
			outputChars += len(delta)
			return false, emit(chatChunk(chatID, created, model, map[string]any{"content": delta}, nil))
		}
		if eventType == "response.reasoning_summary_text.delta" {
			delta, _ := payload["delta"].(string)
			if delta == "" {
				return false, nil
			}
			outputChars += len(delta)
			return false, emit(chatChunk(chatID, created, model, map[string]any{"reasoning_content": delta}, nil))
		}
		if eventType == "response.output_item.added" {
			item := asMap(payload["item"])
			if itemType, _ := item["type"].(string); itemType == "function_call" || itemType == "custom_tool_call" {
				currentToolCallID, _ = item["call_id"].(string)
				if currentToolCallID == "" {
					currentToolCallID = fmt.Sprintf("call_%d", nowUnixMillis())
				}
				name, _ := item["name"].(string)
				delta := map[string]any{"tool_calls": []map[string]any{{"index": toolCallIndex, "id": currentToolCallID, "type": "function", "function": map[string]any{"name": name, "arguments": ""}}}}
				return false, emit(chatChunk(chatID, created, model, delta, nil))
			}
		}
		if eventType == "response.function_call_arguments.delta" || eventType == "response.custom_tool_call_input.delta" {
			deltaText, _ := payload["delta"].(string)
			if deltaText == "" {
				return false, nil
			}
			outputChars += len(deltaText)
			delta := map[string]any{"tool_calls": []map[string]any{{"index": toolCallIndex, "function": map[string]any{"arguments": deltaText}}}}
			return false, emit(chatChunk(chatID, created, model, delta, nil))
		}
		if eventType == "response.output_item.done" {
			item := asMap(payload["item"])
			if itemType, _ := item["type"].(string); itemType == "function_call" || itemType == "custom_tool_call" {
				toolCallIndex++
				currentToolCallID = ""
			}
		}
		if extracted, ok := extractUsageFromPayload(payload); ok {
			usage = extracted
		}
		if eventType == "response.completed" || eventType == "response.done" {
			finish := "stop"
			if toolCallIndex > 0 || currentToolCallID != "" {
				finish = "tool_calls"
			}
			return true, final(finish)
		}
		if eventType == "error" || eventType == "response.failed" {
			delta := map[string]any{"content": "[Error] " + compactJSON(payload)}
			if err := emit(chatChunk(chatID, created, model, delta, nil)); err != nil {
				return true, err
			}
			return true, final("stop")
		}
		return false, nil
	}

	for {
		select {
		case <-ctx.Done():
			return usage, ctx.Err()
		default:
		}
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			trimmed := strings.TrimRight(string(line), "\r\n")
			if trimmed == "" {
				done, procErr := process(eventName, strings.Join(dataLines, "\n"))
				eventName = ""
				dataLines = nil
				if procErr != nil || done {
					if !usage.hasTokens() {
						usage = estimateUsage(requestBody, outputChars)
					}
					return usage, procErr
				}
			} else if strings.HasPrefix(trimmed, "event:") {
				eventName = strings.TrimSpace(strings.TrimPrefix(trimmed, "event:"))
			} else if strings.HasPrefix(trimmed, "data:") {
				dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(trimmed, "data:")))
			}
		}
		if err != nil {
			if err == io.EOF {
				if len(dataLines) > 0 {
					_, procErr := process(eventName, strings.Join(dataLines, "\n"))
					if procErr != nil {
						return usage, procErr
					}
				}
				if !usage.hasTokens() {
					usage = estimateUsage(requestBody, outputChars)
				}
				return usage, final("stop")
			}
			return usage, err
		}
	}
}

func chatChunk(id string, created int64, model string, delta map[string]any, finish any) map[string]any {
	return map[string]any{
		"id":      id,
		"object":  "chat.completion.chunk",
		"created": created,
		"model":   model,
		"choices": []map[string]any{{"index": 0, "delta": delta, "finish_reason": finish}},
	}
}

func copyHeaders(dst http.Header, src http.Header) {
	for key, values := range src {
		if strings.EqualFold(key, "Content-Length") || strings.EqualFold(key, "Content-Encoding") {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func compactJSON(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return string(raw)
	}
	return buf.String()
}
