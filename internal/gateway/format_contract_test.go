package gateway

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// This file locks the observable request/response format contract of the
// gateway. It is intentionally written BEFORE the performance optimizations so
// any change that alters the shape of what clients receive is caught. It only
// asserts existing behavior; it does not introduce new format requirements.

// fakeUpstream builds an *http.Response as if returned by an upstream provider.
func fakeUpstream(status int, contentType string, body string, extraHeaders map[string]string) *http.Response {
	header := http.Header{}
	if contentType != "" {
		header.Set("Content-Type", contentType)
	}
	for k, v := range extraHeaders {
		header.Set(k, v)
	}
	return &http.Response{
		StatusCode: status,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestPassthroughJSONForwardsUsageUntouched(t *testing.T) {
	upstreamBody := `{"id":"chatcmpl-1","object":"chat.completion","choices":[{"message":{"role":"assistant","content":"hi"}}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`
	resp := fakeUpstream(http.StatusOK, "application/json", upstreamBody, nil)
	rec := httptest.NewRecorder()

	usage, err := passthroughJSONWithUsage(rec, resp, map[string]any{"model": "x"})
	if err != nil {
		t.Fatalf("passthroughJSONWithUsage: %v", err)
	}
	if usage.PromptTokens != 10 || usage.CompletionTokens != 5 || usage.TotalTokens != 15 {
		t.Fatalf("usage not parsed from upstream: %+v", usage)
	}
	if usage.Estimated {
		t.Fatalf("usage must not be flagged estimated when upstream reports it")
	}
	// Body must be forwarded byte-for-byte when usage is already present.
	if got := rec.Body.String(); got != upstreamBody {
		t.Fatalf("body altered.\n got: %s\nwant: %s", got, upstreamBody)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestPassthroughJSONInjectsUsageWhenMissing(t *testing.T) {
	upstreamBody := `{"id":"chatcmpl-1","object":"chat.completion","choices":[{"message":{"role":"assistant","content":"hello world"}}]}`
	resp := fakeUpstream(http.StatusOK, "application/json", upstreamBody, nil)
	rec := httptest.NewRecorder()

	usage, err := passthroughJSONWithUsage(rec, resp, map[string]any{"model": "x", "messages": []any{}})
	if err != nil {
		t.Fatalf("passthroughJSONWithUsage: %v", err)
	}
	if !usage.Estimated {
		t.Fatalf("usage should be estimated when upstream omits it: %+v", usage)
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("response not valid JSON: %v", err)
	}
	if _, ok := out["usage"].(map[string]any); !ok {
		t.Fatalf("usage block must be injected when upstream omits it: %s", rec.Body.String())
	}
	// Original fields must be preserved.
	if out["id"] != "chatcmpl-1" || out["object"] != "chat.completion" {
		t.Fatalf("original fields lost after usage injection: %s", rec.Body.String())
	}
}

func TestCopyHeadersDropsLengthAndEncoding(t *testing.T) {
	src := http.Header{}
	src.Set("Content-Length", "1234")
	src.Set("Content-Encoding", "gzip")
	src.Set("X-Request-Id", "abc-123")
	src.Add("X-Multi", "a")
	src.Add("X-Multi", "b")
	dst := http.Header{}

	copyHeaders(dst, src)

	if dst.Get("Content-Length") != "" {
		t.Fatalf("Content-Length must be dropped (body may be rewritten)")
	}
	if dst.Get("Content-Encoding") != "" {
		t.Fatalf("Content-Encoding must be dropped (body is decoded)")
	}
	if dst.Get("X-Request-Id") != "abc-123" {
		t.Fatalf("passthrough header lost")
	}
	if got := dst.Values("X-Multi"); len(got) != 2 {
		t.Fatalf("multi-value header not preserved: %v", got)
	}
}

func TestStreamPassthroughForwardsBytesAndSetsSSEHeaders(t *testing.T) {
	sse := "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n" +
		"data: {\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2,\"total_tokens\":5}}\n\n" +
		"data: [DONE]\n\n"
	resp := fakeUpstream(http.StatusOK, "text/event-stream", sse, nil)
	rec := httptest.NewRecorder()

	usage, err := streamPassthrough(t.Context(), rec, resp, map[string]any{"model": "x"})
	if err != nil {
		t.Fatalf("streamPassthrough: %v", err)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("SSE content-type not set: %q", ct)
	}
	if rec.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("Cache-Control not set to no-cache")
	}
	// Every upstream byte must be forwarded verbatim.
	if got := rec.Body.String(); got != sse {
		t.Fatalf("SSE bytes altered.\n got: %q\nwant: %q", got, sse)
	}
	if usage.PromptTokens != 3 || usage.CompletionTokens != 2 {
		t.Fatalf("usage not read from final SSE event: %+v", usage)
	}
}

func TestStreamResponsesToChatEmitsOpenAIChunks(t *testing.T) {
	// Minimal Responses-API SSE: one text delta then completed.
	sse := "event: response.output_text.delta\n" +
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"Hello\"}\n\n" +
		"event: response.completed\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":4,\"output_tokens\":1,\"total_tokens\":5}}}\n\n"
	resp := fakeUpstream(http.StatusOK, "text/event-stream", sse, nil)
	rec := httptest.NewRecorder()

	usage, err := streamResponsesToChat(t.Context(), rec, resp, "gpt-x", map[string]any{"model": "gpt-x"})
	if err != nil {
		t.Fatalf("streamResponsesToChat: %v", err)
	}

	body := rec.Body.String()
	// Must emit chat.completion.chunk frames and terminate with [DONE].
	if !strings.Contains(body, `"object":"chat.completion.chunk"`) {
		t.Fatalf("did not emit chat.completion.chunk frames:\n%s", body)
	}
	if !strings.Contains(body, "data: [DONE]") {
		t.Fatalf("stream did not terminate with [DONE]:\n%s", body)
	}
	if !strings.Contains(body, `"content":"Hello"`) {
		t.Fatalf("text delta not translated into content:\n%s", body)
	}
	// Parse the first data frame to confirm required chunk fields exist.
	scanner := bufio.NewScanner(strings.NewReader(body))
	firstChunk := ""
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") && !strings.Contains(line, "[DONE]") {
			firstChunk = strings.TrimPrefix(line, "data: ")
			break
		}
	}
	if firstChunk == "" {
		t.Fatalf("no data frame found")
	}
	var chunk map[string]any
	if err := json.Unmarshal([]byte(firstChunk), &chunk); err != nil {
		t.Fatalf("chunk not valid JSON: %v (%s)", err, firstChunk)
	}
	for _, field := range []string{"id", "object", "created", "model", "choices"} {
		if _, ok := chunk[field]; !ok {
			t.Fatalf("chunk missing required field %q: %s", field, firstChunk)
		}
	}
	if usage.PromptTokens != 4 || usage.CompletionTokens != 1 {
		t.Fatalf("usage not extracted from response.completed: %+v", usage)
	}
}

func TestWriteErrorFormat(t *testing.T) {
	rec := httptest.NewRecorder()
	writeError(rec, http.StatusBadRequest, "boom")

	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("error content-type = %q, want application/json", ct)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var out struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("error body not valid JSON: %v", err)
	}
	if out.Error.Message != "boom" || out.Error.Type != "gateway_error" {
		t.Fatalf("unexpected error body: %s", rec.Body.String())
	}
}

func TestReadJSONBodyPreservesIntegerTokens(t *testing.T) {
	// UseNumber() must keep large integers exact (not float64 with precision loss).
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"x","max_tokens":123456789}`))
	body, err := readJSONBody(req)
	if err != nil {
		t.Fatalf("readJSONBody: %v", err)
	}
	num, ok := body["max_tokens"].(json.Number)
	if !ok {
		t.Fatalf("max_tokens should decode as json.Number, got %T", body["max_tokens"])
	}
	if num.String() != "123456789" {
		t.Fatalf("integer mangled: %s", num.String())
	}
}

func TestAnthropicJSONPassthroughSetsJSONContentType(t *testing.T) {
	// A chat-shaped upstream body converted to Anthropic must be valid JSON with usage read.
	upstreamBody := `{"id":"chatcmpl-1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":2,"total_tokens":9}}`
	resp := fakeUpstream(http.StatusOK, "application/json", upstreamBody, nil)
	rec := httptest.NewRecorder()

	usage, err := passthroughAnthropicJSONWithUsage(rec, resp, map[string]any{"model": "claude-x"}, "claude-x")
	if err != nil {
		t.Fatalf("passthroughAnthropicJSONWithUsage: %v", err)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q, want application/json", ct)
	}
	if usage.PromptTokens != 7 || usage.CompletionTokens != 2 {
		t.Fatalf("usage not parsed: %+v", usage)
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("converted body not valid JSON: %v (%s)", err, rec.Body.String())
	}
}
