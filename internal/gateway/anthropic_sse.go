package gateway

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/local/vivurouter-go/internal/translator"
)

func passthroughAnthropicJSONWithUsage(w http.ResponseWriter, resp *http.Response, requestBody map[string]any, model string) (usageInfo, error) {
	copyHeaders(w.Header(), resp.Header)
	raw, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return usageInfo{}, readErr
	}
	usage, ok := extractUsageFromJSON(raw)
	if !ok || !usage.hasTokens() {
		usage = estimateUsage(requestBody, estimateOutputCharsFromJSON(raw))
	}
	converted, err := translatorChatJSONToAnthropic(raw, model)
	if err != nil {
		return usageInfo{}, err
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, err = w.Write(converted)
	return usage, err
}

func streamChatToAnthropic(ctx context.Context, w http.ResponseWriter, resp *http.Response, model string, requestBody map[string]any) (usageInfo, error) {
	return streamOpenAIOrResponsesToAnthropic(ctx, w, resp, model, requestBody, false)
}

func streamResponsesToAnthropic(ctx context.Context, w http.ResponseWriter, resp *http.Response, model string, requestBody map[string]any) (usageInfo, error) {
	return streamOpenAIOrResponsesToAnthropic(ctx, w, resp, model, requestBody, true)
}

func streamOpenAIOrResponsesToAnthropic(ctx context.Context, w http.ResponseWriter, resp *http.Response, model string, requestBody map[string]any, responsesEvents bool) (usageInfo, error) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	reader := bufio.NewReader(resp.Body)

	msgID := fmt.Sprintf("msg_%d", nowUnixMillis())
	usage := usageInfo{}
	outputChars := 0
	contentStarted := false
	finalSent := false
	toolBlockIndex := 0
	startedToolBlocks := []int{}
	responseToolBlocks := map[int]int{}
	responseToolArgBuffers := map[int]string{}
	responseToolNames := map[int]string{}
	hadToolUse := false
	textBlockIndex := 0
	var eventName string
	var dataLines []string

	emit := func(event string, payload map[string]any) error {
		raw, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, raw); err != nil {
			return err
		}
		if flusher != nil {
			flusher.Flush()
		}
		return nil
	}

	startMessage := func() error {
		return emit("message_start", map[string]any{"type": "message_start", "message": map[string]any{"id": msgID, "type": "message", "role": "assistant", "model": model, "content": []any{}, "stop_reason": nil, "stop_sequence": nil, "usage": map[string]any{"input_tokens": 0, "output_tokens": 0}}})
	}
	startText := func() error {
		if contentStarted {
			return nil
		}
		contentStarted = true
		return emit("content_block_start", map[string]any{"type": "content_block_start", "index": textBlockIndex, "content_block": map[string]any{"type": "text", "text": ""}})
	}
	final := func(reason string) error {
		if finalSent {
			return nil
		}
		finalSent = true
		if !usage.hasTokens() {
			usage = estimateUsage(requestBody, outputChars)
		}
		if contentStarted {
			if err := emit("content_block_stop", map[string]any{"type": "content_block_stop", "index": textBlockIndex}); err != nil {
				return err
			}
		}
		for responseIdx, blockIdx := range responseToolBlocks {
			if args := responseToolArgBuffers[responseIdx]; args != "" {
				if err := emit("content_block_delta", map[string]any{"type": "content_block_delta", "index": blockIdx, "delta": map[string]any{"type": "input_json_delta", "partial_json": sanitizeToolArgsLocal(responseToolNames[responseIdx], args)}}); err != nil {
					return err
				}
				responseToolArgBuffers[responseIdx] = ""
			}
		}
		for _, idx := range startedToolBlocks {
			if err := emit("content_block_stop", map[string]any{"type": "content_block_stop", "index": idx}); err != nil {
				return err
			}
		}
		if hadToolUse && reason == "stop" {
			reason = "tool_calls"
		}
		if err := emit("message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": anthropicStopReasonLocal(reason), "stop_sequence": nil}, "usage": map[string]any{"input_tokens": usage.PromptTokens, "output_tokens": usage.CompletionTokens}}); err != nil {
			return err
		}
		return emit("message_stop", map[string]any{"type": "message_stop"})
	}

	if err := startMessage(); err != nil {
		return usage, err
	}

	processOpenAI := func(data string) (bool, error) {
		if strings.TrimSpace(data) == "[DONE]" {
			return true, final("stop")
		}
		var payload map[string]any
		decoder := json.NewDecoder(strings.NewReader(data))
		decoder.UseNumber()
		if err := decoder.Decode(&payload); err != nil {
			return false, nil
		}
		if extracted, ok := extractUsageFromPayload(payload); ok {
			usage = extracted
		}
		choices := anySlice(payload["choices"])
		if len(choices) == 0 {
			return false, nil
		}
		choice := asMap(choices[0])
		delta := asMap(choice["delta"])
		if content := asStringLocal(delta["content"]); content != "" {
			outputChars += len(content)
			if err := startText(); err != nil {
				return true, err
			}
			return false, emit("content_block_delta", map[string]any{"type": "content_block_delta", "index": textBlockIndex, "delta": map[string]any{"type": "text_delta", "text": content}})
		}
		for _, rawTool := range anySlice(delta["tool_calls"]) {
			tool := asMap(rawTool)
			fn := asMap(tool["function"])
			if name := asStringLocal(fn["name"]); name != "" {
				if contentStarted {
					if err := emit("content_block_stop", map[string]any{"type": "content_block_stop", "index": textBlockIndex}); err != nil {
						return true, err
					}
					contentStarted = false
				}
				idx := toolBlockIndex + 1
				toolBlockIndex++
				startedToolBlocks = append(startedToolBlocks, idx)
				id := asStringLocal(tool["id"])
				if id == "" {
					id = fmt.Sprintf("toolu_%d", nowUnixMillis())
				}
				if err := emit("content_block_start", map[string]any{"type": "content_block_start", "index": idx, "content_block": map[string]any{"type": "tool_use", "id": id, "name": name, "input": map[string]any{}}}); err != nil {
					return true, err
				}
			}
			if args := asStringLocal(fn["arguments"]); args != "" {
				outputChars += len(args)
				idx := toolBlockIndex
				if idx == 0 {
					idx = 1
				}
				if err := emit("content_block_delta", map[string]any{"type": "content_block_delta", "index": idx, "delta": map[string]any{"type": "input_json_delta", "partial_json": args}}); err != nil {
					return true, err
				}
			}
		}
		if finish := asStringLocal(choice["finish_reason"]); finish != "" {
			return true, final(finish)
		}
		return false, nil
	}

	processResponses := func(eventType string, data string) (bool, error) {
		if strings.TrimSpace(data) == "[DONE]" {
			return true, final("stop")
		}
		var payload map[string]any
		decoder := json.NewDecoder(strings.NewReader(data))
		decoder.UseNumber()
		if err := decoder.Decode(&payload); err != nil {
			return false, nil
		}
		if eventType == "" {
			eventType = asStringLocal(payload["type"])
		}
		if extracted, ok := extractUsageFromPayload(payload); ok {
			usage = extracted
		}
		if eventType == "response.output_text.delta" || eventType == "response.reasoning_summary_text.delta" {
			delta := asStringLocal(payload["delta"])
			if delta == "" {
				return false, nil
			}
			outputChars += len(delta)
			if err := startText(); err != nil {
				return true, err
			}
			return false, emit("content_block_delta", map[string]any{"type": "content_block_delta", "index": textBlockIndex, "delta": map[string]any{"type": "text_delta", "text": delta}})
		}
		if eventType == "response.output_item.added" {
			item := asMap(payload["item"])
			itemType := asStringLocal(item["type"])
			if itemType == "function_call" || itemType == "custom_tool_call" {
				if contentStarted {
					if err := emit("content_block_stop", map[string]any{"type": "content_block_stop", "index": textBlockIndex}); err != nil {
						return true, err
					}
					contentStarted = false
				}
				responseIdx := intFromAnyDefault(payload["output_index"], len(responseToolBlocks))
				idx := toolBlockIndex + 1
				toolBlockIndex++
				startedToolBlocks = append(startedToolBlocks, idx)
				responseToolBlocks[responseIdx] = idx
				id := asStringLocal(item["call_id"])
				if id == "" {
					id = asStringLocal(item["id"])
				}
				if id == "" {
					id = fmt.Sprintf("toolu_%d", nowUnixMillis())
				}
				name := asStringLocal(item["name"])
				responseToolNames[responseIdx] = name
				hadToolUse = true
				if err := emit("content_block_start", map[string]any{"type": "content_block_start", "index": idx, "content_block": map[string]any{"type": "tool_use", "id": id, "name": name, "input": map[string]any{}}}); err != nil {
					return true, err
				}
			}
			return false, nil
		}
		if eventType == "response.function_call_arguments.delta" || eventType == "response.custom_tool_call_input.delta" {
			delta := asStringLocal(payload["delta"])
			if delta == "" {
				return false, nil
			}
			responseIdx := intFromAnyDefault(payload["output_index"], 0)
			responseToolArgBuffers[responseIdx] += delta
			outputChars += len(delta)
			return false, nil
		}
		if eventType == "response.output_item.done" {
			item := asMap(payload["item"])
			itemType := asStringLocal(item["type"])
			if itemType == "function_call" || itemType == "custom_tool_call" {
				responseIdx := intFromAnyDefault(payload["output_index"], 0)
				if args := asStringLocal(item["arguments"]); args != "" && responseToolArgBuffers[responseIdx] == "" {
					responseToolArgBuffers[responseIdx] = args
				}
				if name := asStringLocal(item["name"]); name != "" {
					responseToolNames[responseIdx] = name
				}
			}
			return false, nil
		}
		if eventType == "response.completed" || eventType == "response.done" {
			return true, final("stop")
		}
		if eventType == "error" || eventType == "response.failed" {
			if err := startText(); err != nil {
				return true, err
			}
			if err := emit("content_block_delta", map[string]any{"type": "content_block_delta", "index": textBlockIndex, "delta": map[string]any{"type": "text_delta", "text": "[Error] " + compactJSON(payload)}}); err != nil {
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
				data := strings.Join(dataLines, "\n")
				done := false
				var procErr error
				if responsesEvents {
					done, procErr = processResponses(eventName, data)
				} else {
					done, procErr = processOpenAI(data)
				}
				eventName = ""
				dataLines = nil
				if procErr != nil || done {
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
					if responsesEvents {
						_, _ = processResponses(eventName, strings.Join(dataLines, "\n"))
					} else {
						_, _ = processOpenAI(strings.Join(dataLines, "\n"))
					}
				}
				return usage, final("stop")
			}
			return usage, err
		}
	}
}

func intFromAnyDefault(value any, fallback int) int {
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
	}
	return fallback
}

func sanitizeToolArgsLocal(toolName string, argsJSON string) string {
	var args map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return argsJSON
	}
	if toolName == "Read" {
		sanitizeReadArgsLocal(args)
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return argsJSON
	}
	return string(raw)
}

func sanitizeReadArgsLocal(args map[string]any) {
	if limit, ok := numericStringToInt(args["limit"]); ok {
		args["limit"] = limit
	}
	if offset, ok := numericStringToInt(args["offset"]); ok {
		args["offset"] = offset
	}
	if limit, ok := intValue(args["limit"]); ok {
		if limit > 2000 {
			args["limit"] = 2000
		} else if limit < 1 {
			delete(args, "limit")
		}
	}
	if offset, ok := intValue(args["offset"]); ok && offset < 0 {
		args["offset"] = 0
	}
	pages, hasPages := args["pages"].(string)
	filePath, _ := args["file_path"].(string)
	if hasPages && (!strings.HasSuffix(strings.ToLower(filePath), ".pdf") || !validPDFPagesArg(pages)) {
		delete(args, "pages")
	}
}

func numericStringToInt(value any) (int, bool) {
	s, ok := value.(string)
	if !ok || s == "" {
		return 0, false
	}
	for i, r := range s {
		if r == '-' && i == 0 {
			continue
		}
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		return 0, false
	}
	return n, true
}

func intValue(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return int(i), true
		}
	}
	return 0, false
}

func validPDFPagesArg(value string) bool {
	if value == "" {
		return false
	}
	dashSeen := false
	for i, r := range value {
		if r == '-' {
			if dashSeen || i == 0 || i == len(value)-1 {
				return false
			}
			dashSeen = true
			continue
		}
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func anthropicStopReasonLocal(reason string) string {
	switch reason {
	case "length":
		return "max_tokens"
	case "tool_calls":
		return "tool_use"
	default:
		return "end_turn"
	}
}

func translatorChatJSONToAnthropic(raw []byte, model string) ([]byte, error) {
	return translator.ChatJSONToAnthropic(raw, model)
}
