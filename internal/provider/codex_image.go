package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/local/vivurouter-go/internal/store"
)

var codexImageModels = map[string]bool{
	"gpt-5.5-image": true,
	"gpt-5.4-image": true,
	"gpt-5.3-image": true,
}

// ExecuteImage generates an image through the Codex Responses image_generation
// tool. It performs one logical dispatch; auth refresh remains the only retry.
func (e *CodexExecutor) ExecuteImage(ctx context.Context, p store.Provider, accountID, model, prompt string, references []string, options map[string]string) (*ExecuteResult, error) {
	model = strings.TrimSpace(model)
	if !codexImageModels[model] {
		return nil, fmt.Errorf("unsupported Codex image model")
	}
	baseModel := strings.TrimSuffix(model, "-image")
	content := make([]map[string]any, 0, len(references)*3+1)
	for i, ref := range references {
		if strings.TrimSpace(ref) == "" {
			continue
		}
		content = append(content,
			map[string]any{"type": "input_text", "text": fmt.Sprintf("<image name=image%d>", i+1)},
			map[string]any{"type": "input_image", "image_url": ref, "detail": "high"},
			map[string]any{"type": "input_text", "text": "</image>"},
		)
	}
	content = append(content, map[string]any{"type": "input_text", "text": prompt})
	tool := map[string]any{"type": "image_generation", "output_format": valueOr(options["output_format"], "png")}
	for _, key := range []string{"size", "quality", "background"} {
		if value := strings.TrimSpace(options[key]); value != "" && value != "auto" {
			tool[key] = value
		}
	}
	body := map[string]any{
		"model": baseModel, "instructions": "", "input": []map[string]any{{"type": "message", "role": "user", "content": content}},
		"tools": []map[string]any{tool}, "tool_choice": "auto", "parallel_tool_calls": false, "stream": true, "store": false,
	}
	result, err := e.ExecuteResponsesForAccount(ctx, p, baseModel, body, accountID)
	if err != nil || result == nil || result.Response == nil || result.Response.StatusCode < 200 || result.Response.StatusCode >= 300 {
		return result, err
	}
	defer result.Response.Body.Close()
	image, err := readCodexImageSSE(result.Response.Body)
	if err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(map[string]any{"created": time.Now().Unix(), "data": []map[string]any{{"b64_json": image}}})
	result.Response = &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewReader(payload))}
	return result, nil
}

func readCodexImageSSE(body io.Reader) (string, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64*1024), 32*1024*1024)
	var event, data, image string
	flush := func() {
		if event == "response.output_item.done" && data != "" {
			var payload struct {
				Item struct {
					Type   string `json:"type"`
					Result string `json:"result"`
				} `json:"item"`
			}
			if json.Unmarshal([]byte(data), &payload) == nil && payload.Item.Type == "image_generation_call" && payload.Item.Result != "" {
				image = payload.Item.Result
			}
		}
		event, data = "", ""
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, "event:") {
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		}
		if strings.HasPrefix(line, "data:") {
			data += strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		}
	}
	flush()
	if err := scanner.Err(); err != nil {
		return "", err
	}
	if image == "" {
		return "", fmt.Errorf("Codex did not return an image; a ChatGPT Plus/Pro entitlement may be required")
	}
	return image, nil
}

func valueOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
