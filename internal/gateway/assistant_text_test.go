package gateway

import "testing"

func TestExtractAssistantTextStrictResponsesOutputItems(t *testing.T) {
	raw := []byte(`{"id":"resp","output":[{"type":"message","content":[{"type":"output_text","text":"final answer"}]}],"metadata":{"large":"ignored"}}`)
	text, ok := extractAssistantTextStrict(raw)
	if !ok {
		t.Fatal("assistant text not extracted")
	}
	if text != "final answer" {
		t.Fatalf("text = %q", text)
	}
}

func TestExtractAssistantTextStrictGemini(t *testing.T) {
	raw := []byte(`{"candidates":[{"content":{"parts":[{"text":"gemini answer"}]}}]}`)
	text, ok := extractAssistantTextStrict(raw)
	if !ok || text != "gemini answer" {
		t.Fatalf("text = %q ok=%v", text, ok)
	}
}

func TestExtractAssistantTextStrictTopLevelContentArray(t *testing.T) {
	raw := []byte(`{"content":[{"type":"text","text":"array answer"}]}`)
	text, ok := extractAssistantTextStrict(raw)
	if !ok || text != "array answer" {
		t.Fatalf("text = %q ok=%v", text, ok)
	}
}

func TestExtractAssistantTextStrictNestedResultMessage(t *testing.T) {
	raw := []byte(`{"result":{"message":{"content":[{"text":"nested answer"}]}}}`)
	text, ok := extractAssistantTextStrict(raw)
	if !ok || text != "nested answer" {
		t.Fatalf("text = %q ok=%v", text, ok)
	}
}

func TestExtractAssistantTextStrictSSEChatChunks(t *testing.T) {
	raw := []byte("data: {\"choices\":[{\"delta\":{\"content\":\"hello \"}}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"world\"}}]}\n\ndata: [DONE]\n\n")
	text, ok := extractAssistantTextStrict(raw)
	if !ok || text != "hello world" {
		t.Fatalf("text = %q ok=%v", text, ok)
	}
}

func TestExtractAssistantTextStrictSSEResponsesDelta(t *testing.T) {
	raw := []byte("event: response.output_text.delta\ndata: {\"delta\":\"part one\"}\n\nevent: response.output_text.delta\ndata: {\"delta\":\" part two\"}\n\n")
	text, ok := extractAssistantTextStrict(raw)
	if !ok || text != "part one part two" {
		t.Fatalf("text = %q ok=%v", text, ok)
	}
}

func TestExtractAssistantTextStrictNoRawJSONFallback(t *testing.T) {
	raw := []byte(`{"id":"resp","object":"response","metadata":{"large":"ignored"}}`)
	text, ok := extractAssistantTextStrict(raw)
	if ok || text != "" {
		t.Fatalf("text = %q ok=%v, want no extraction", text, ok)
	}
	if fallback := extractAssistantText(raw); fallback == "" {
		t.Fatal("legacy extractor should still fallback for non-Fusion callers")
	}
}
