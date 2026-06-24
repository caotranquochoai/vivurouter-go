package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteFusionStreamResponse(t *testing.T) {
	rec := httptest.NewRecorder()
	usage := usageInfo{PromptTokens: 3, CompletionTokens: 4, TotalTokens: 7, Estimated: true}
	if err := writeFusionStreamResponse(rec, "fusion-code", "hello from fusion", usage); err != nil {
		t.Fatalf("write fusion stream: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "text/event-stream") {
		t.Fatalf("content-type = %q, want event stream", got)
	}
	out := rec.Body.String()
	if !strings.Contains(out, "data: [DONE]\n\n") {
		t.Fatalf("stream missing DONE:\n%s", out)
	}
	chunks := parseSSEDataLines(t, out)
	if len(chunks) < 4 {
		t.Fatalf("chunks = %d, want at least 4:\n%s", len(chunks), out)
	}
	first := decodeChunk(t, chunks[0])
	choice := firstChoice(t, first)
	delta := asMap(choice["delta"])
	if delta["role"] != "assistant" {
		t.Fatalf("first delta role = %#v, want assistant", delta["role"])
	}
	second := decodeChunk(t, chunks[1])
	choice = firstChoice(t, second)
	delta = asMap(choice["delta"])
	if delta["content"] != "hello from fusion" {
		t.Fatalf("content delta = %#v", delta["content"])
	}
	usageChunk := decodeChunk(t, chunks[len(chunks)-2])
	usageMap := asMap(usageChunk["usage"])
	if usageMap["total_tokens"] != float64(7) || usageMap["estimated"] != true {
		t.Fatalf("usage chunk = %#v", usageMap)
	}
}

func parseSSEDataLines(t *testing.T, body string) []string {
	t.Helper()
	var chunks []string
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			chunks = append(chunks, data)
			continue
		}
		chunks = append(chunks, data)
	}
	return chunks
}

func decodeChunk(t *testing.T, data string) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal([]byte(data), &payload); err != nil {
		t.Fatalf("decode chunk %q: %v", data, err)
	}
	return payload
}

func firstChoice(t *testing.T, payload map[string]any) map[string]any {
	t.Helper()
	choices, ok := payload["choices"].([]any)
	if !ok || len(choices) == 0 {
		t.Fatalf("missing choices in %#v", payload)
	}
	return asMap(choices[0])
}
