package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// BenchmarkReadJSONBody measures request-body decode cost on the gateway's
// inbound hot path (every /v1 call parses the body with UseNumber()).
func BenchmarkReadJSONBody(b *testing.B) {
	payload := `{"model":"gpt-4o","stream":true,"max_tokens":4096,"temperature":0.7,` +
		`"messages":[{"role":"system","content":"You are a helpful assistant."},` +
		`{"role":"user","content":"Explain the theory of relativity in two paragraphs."}]}`
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(payload))
		if _, err := readJSONBody(req); err != nil {
			b.Fatalf("readJSONBody: %v", err)
		}
	}
}

// BenchmarkPassthroughJSONWithUsageHasUsage measures the common non-stream
// response path where the upstream already reports usage (body forwarded as-is
// after a usage extraction).
func BenchmarkPassthroughJSONWithUsageHasUsage(b *testing.B) {
	upstreamBody := `{"id":"chatcmpl-1","object":"chat.completion","choices":` +
		`[{"index":0,"message":{"role":"assistant","content":"The answer is 42."},"finish_reason":"stop"}],` +
		`"usage":{"prompt_tokens":1200,"completion_tokens":350,"total_tokens":1550}}`
	reqBody := map[string]any{"model": "gpt-4o"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(upstreamBody)),
		}
		rec := httptest.NewRecorder()
		if _, err := passthroughJSONWithUsage(rec, resp, reqBody); err != nil {
			b.Fatalf("passthroughJSONWithUsage: %v", err)
		}
	}
}
