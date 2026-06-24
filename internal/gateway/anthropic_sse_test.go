package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStreamResponsesToAnthropicEmitsToolUse(t *testing.T) {
	body := strings.Join([]string{
		"event: response.output_item.added\n" + `data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","call_id":"call_read","name":"Read"}}` + "\n",
		"event: response.function_call_arguments.delta\n" + `data: {"type":"response.function_call_arguments.delta","output_index":0,"delta":"{\"file_path\":\"README.md\",\"limit\":\"5000\",\"offset\":\"-1\",\"pages\":\"1-2\"}"}` + "\n",
		"event: response.completed\n" + `data: {"type":"response.completed","response":{"usage":{"input_tokens":42,"output_tokens":7}}}` + "\n",
		"data: [DONE]\n",
	}, "\n")
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: anthropicSSENopCloser{strings.NewReader(body)}}
	rr := httptest.NewRecorder()
	usage, err := streamResponsesToAnthropic(context.Background(), rr, resp, "cx/gpt-5.5", map[string]any{"input": "hello"})
	if err != nil {
		t.Fatalf("stream responses: %v", err)
	}
	out := rr.Body.String()
	if !strings.Contains(out, `"type":"tool_use"`) || !strings.Contains(out, `"name":"Read"`) || !strings.Contains(out, `"stop_reason":"tool_use"`) {
		t.Fatalf("tool_use stream not emitted correctly:\n%s", out)
	}
	if !strings.Contains(out, `\"limit\":2000`) || !strings.Contains(out, `\"offset\":0`) || strings.Contains(out, `\"pages\"`) {
		t.Fatalf("Read args were not sanitized:\n%s", out)
	}
	if usage.PromptTokens != 42 || usage.CompletionTokens != 7 {
		t.Fatalf("usage = %+v", usage)
	}
}

type anthropicSSENopCloser struct{ *strings.Reader }

func (c anthropicSSENopCloser) Close() error { return nil }
