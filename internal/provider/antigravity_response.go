package provider

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

func antigravityOpenAIResponse(model string, payload any) map[string]any {
	root, _ := payload.(map[string]any)
	response := antigravityResponse(root)
	content, reasoning, toolCalls, finish := antigravityChoice(response)
	message := map[string]any{"role": "assistant", "content": content}
	if reasoning != "" {
		message["reasoning_content"] = reasoning
	}
	if len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls
	}
	return map[string]any{
		"id":      antigravityResponseID(response),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": finish}},
		"usage":   antigravityUsage(response),
	}
}

func antigravityResponse(root map[string]any) map[string]any {
	if nested, ok := root["response"].(map[string]any); ok {
		return nested
	}
	return root
}

func antigravityChoice(response map[string]any) (string, string, []any, string) {
	candidates, _ := response["candidates"].([]any)
	if len(candidates) == 0 {
		return "", "", nil, "stop"
	}
	candidate, _ := candidates[0].(map[string]any)
	finish := antigravityFinishReason(asString(candidate["finishReason"]))
	contentMap, _ := candidate["content"].(map[string]any)
	parts, _ := contentMap["parts"].([]any)
	texts := []string{}
	reasoning := []string{}
	toolCalls := []any{}
	for index, item := range parts {
		part, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if text := asString(part["text"]); text != "" {
			if thought, _ := part["thought"].(bool); thought {
				reasoning = append(reasoning, text)
			} else {
				texts = append(texts, text)
			}
		}
		if call, ok := part["functionCall"].(map[string]any); ok {
			name := asString(call["name"])
			if name == "" {
				continue
			}
			arguments, _ := json.Marshal(call["args"])
			if len(arguments) == 0 || string(arguments) == "null" {
				arguments = []byte("{}")
			}
			id := asString(call["id"])
			if id == "" {
				id = fmt.Sprintf("call_ag_%d", index)
			}
			toolCalls = append(toolCalls, map[string]any{"id": id, "type": "function", "function": map[string]any{"name": name, "arguments": string(arguments)}})
		}
	}
	return strings.Join(texts, ""), strings.Join(reasoning, ""), toolCalls, finish
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
	return cloneHTTPResponse(resp, raw, "application/json"), nil
}

// antigravityRewriteSSEResponse translates upstream events while they arrive.
// The returned pipe deliberately preserves backpressure from the downstream
// client instead of buffering an entire model response in memory.
func antigravityRewriteSSEResponse(resp *http.Response, model string) (*http.Response, error) {
	out := new(http.Response)
	*out = *resp
	out.Header = resp.Header.Clone()
	out.Header.Set("Content-Type", "text/event-stream")
	out.Header.Del("Content-Length")
	out.ContentLength = -1
	reader, writer := io.Pipe()
	out.Body = reader
	go func() {
		defer resp.Body.Close()
		defer writer.Close()
		if err := streamAntigravitySSE(writer, resp.Body, model); err != nil {
			_ = writer.CloseWithError(err)
		}
	}()
	return out, nil
}

type antigravityStreamState struct {
	id            string
	created       int64
	text          string
	reasoning     string
	toolCallsSent bool
	finished      bool
	mu            sync.Mutex
}

func streamAntigravitySSE(out io.Writer, input io.Reader, model string) error {
	state := &antigravityStreamState{id: fmt.Sprintf("chatcmpl-ag-%d", time.Now().UnixNano()), created: time.Now().Unix()}
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var dataLines []string
	process := func() error {
		if len(dataLines) == 0 {
			return nil
		}
		data := strings.Join(dataLines, "\n")
		dataLines = nil
		if strings.TrimSpace(data) == "[DONE]" {
			return state.finish(out, model, "stop")
		}
		var root map[string]any
		if err := json.Unmarshal([]byte(data), &root); err != nil {
			return fmt.Errorf("decode Antigravity SSE event: %w", err)
		}
		response := antigravityResponse(root)
		content, reasoning, calls, finish := antigravityChoice(response)
		if err := state.emitNewText(out, model, content, false); err != nil {
			return err
		}
		if err := state.emitNewText(out, model, reasoning, true); err != nil {
			return err
		}
		if len(calls) > 0 && !state.toolCallsSent {
			state.toolCallsSent = true
			if err := writeAntigravityChunk(out, state, model, map[string]any{"tool_calls": calls}, nil); err != nil {
				return err
			}
		}
		if hasAntigravityFinish(response) {
			return state.finish(out, model, finish)
		}
		return nil
	}
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if line == "" {
			if err := process(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read Antigravity SSE: %w", err)
	}
	if err := process(); err != nil {
		return err
	}
	return state.finish(out, model, "stop")
}

func (s *antigravityStreamState) emitNewText(out io.Writer, model, value string, reasoning bool) error {
	if value == "" {
		return nil
	}
	s.mu.Lock()
	previous := s.text
	key := "content"
	if reasoning {
		previous = s.reasoning
		key = "reasoning_content"
	}
	delta := value
	if strings.HasPrefix(value, previous) {
		delta = strings.TrimPrefix(value, previous)
	}
	if reasoning {
		s.reasoning = value
	} else {
		s.text = value
	}
	s.mu.Unlock()
	if delta == "" {
		return nil
	}
	return writeAntigravityChunk(out, s, model, map[string]any{key: delta}, nil)
}

func (s *antigravityStreamState) finish(out io.Writer, model, reason string) error {
	s.mu.Lock()
	if s.finished {
		s.mu.Unlock()
		return nil
	}
	s.finished = true
	s.mu.Unlock()
	if err := writeAntigravityChunk(out, s, model, map[string]any{}, reason); err != nil {
		return err
	}
	_, err := io.WriteString(out, "data: [DONE]\n\n")
	return err
}

func writeAntigravityChunk(out io.Writer, state *antigravityStreamState, model string, delta map[string]any, finish any) error {
	payload := map[string]any{"id": state.id, "object": "chat.completion.chunk", "created": state.created, "model": model, "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "data: %s\n\n", raw)
	return err
}

func hasAntigravityFinish(response map[string]any) bool {
	candidates, _ := response["candidates"].([]any)
	if len(candidates) == 0 {
		return false
	}
	candidate, _ := candidates[0].(map[string]any)
	return strings.TrimSpace(asString(candidate["finishReason"])) != ""
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
