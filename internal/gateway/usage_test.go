package gateway

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/local/vivurouter-go/internal/store"
)

func TestExtractUsageFromOpenAIJSON(t *testing.T) {
	raw := []byte(`{
		"id":"chatcmpl-test",
		"usage":{
			"prompt_tokens":100,
			"completion_tokens":25,
			"total_tokens":125,
			"prompt_tokens_details":{"cached_tokens":40},
			"completion_tokens_details":{"reasoning_tokens":7}
		}
	}`)
	usage, ok := extractUsageFromJSON(raw)
	if !ok {
		t.Fatal("usage not extracted")
	}
	if usage.PromptTokens != 100 || usage.CompletionTokens != 25 || usage.TotalTokens != 125 {
		t.Fatalf("unexpected usage: %+v", usage)
	}
	if usage.CachedTokens != 40 || usage.ReasoningTokens != 7 {
		t.Fatalf("unexpected detail usage: %+v", usage)
	}
}

func TestExtractUsageFromResponsesPayload(t *testing.T) {
	payload := map[string]any{
		"type": "response.completed",
		"response": map[string]any{
			"usage": map[string]any{
				"input_tokens":  50,
				"output_tokens": 10,
				"input_tokens_details": map[string]any{
					"cached_tokens": 15,
				},
				"output_tokens_details": map[string]any{
					"reasoning_tokens": 3,
				},
			},
		},
	}
	usage, ok := extractUsageFromPayload(payload)
	if !ok {
		t.Fatal("usage not extracted")
	}
	if usage.PromptTokens != 50 || usage.CompletionTokens != 10 || usage.TotalTokens != 60 {
		t.Fatalf("unexpected usage: %+v", usage)
	}
	if usage.CachedTokens != 15 || usage.ReasoningTokens != 3 {
		t.Fatalf("unexpected detail usage: %+v", usage)
	}
}

func TestExtractUsageFromResponsesPayloadCacheVariants(t *testing.T) {
	payload := map[string]any{
		"response": map[string]any{
			"usage": map[string]any{
				"input_tokens":  100,
				"output_tokens": 10,
				"input_tokens_details": map[string]any{
					"cache_read_input_tokens": 60,
				},
			},
		},
	}
	usage, ok := extractUsageFromPayload(payload)
	if !ok || usage.CachedTokens != 60 {
		t.Fatalf("usage = %+v ok=%v", usage, ok)
	}
}

func TestEstimateUsage(t *testing.T) {
	usage := estimateUsage(map[string]any{"messages": []any{map[string]any{"role": "user", "content": "hello world"}}}, 40)
	if !usage.Estimated {
		t.Fatal("expected estimated usage")
	}
	if usage.PromptTokens <= 0 || usage.CompletionTokens != 10 || usage.TotalTokens != usage.PromptTokens+usage.CompletionTokens {
		t.Fatalf("unexpected estimated usage: %+v", usage)
	}
}

func TestEnsureStreamUsageOptions(t *testing.T) {
	body := map[string]any{"stream": true, "stream_options": map[string]any{"foo": "bar"}}
	updated := ensureStreamUsageOptions(body)
	options := asMap(updated["stream_options"])
	if options["include_usage"] != true || options["foo"] != "bar" {
		t.Fatalf("unexpected stream options: %+v", options)
	}
	if asMap(body["stream_options"])["include_usage"] == true {
		t.Fatal("original body was mutated")
	}
}

func TestEnsureUsageInJSONAddsEstimatedUsage(t *testing.T) {
	raw := []byte(`{"id":"chatcmpl-test","choices":[{"message":{"content":"hello"}}]}`)
	usage := usageInfo{PromptTokens: 10, CompletionTokens: 2, TotalTokens: 12, Estimated: true}
	updated := ensureUsageInJSON(raw, usage)
	var payload map[string]any
	if err := json.Unmarshal(updated, &payload); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	extracted, ok := extractUsageFromPayload(payload)
	if !ok {
		t.Fatal("usage not injected")
	}
	if extracted.PromptTokens != 10 || extracted.CompletionTokens != 2 || extracted.TotalTokens != 12 || !extracted.Estimated {
		t.Fatalf("unexpected usage: %+v", extracted)
	}
}

func TestOpenAIUsageChunkShape(t *testing.T) {
	chunk := openAIUsageChunk("chatcmpl-test", 123, "gpt-test", usageInfo{PromptTokens: 3, CompletionTokens: 4, TotalTokens: 7})
	choices, ok := chunk["choices"].([]any)
	if !ok || len(choices) != 0 {
		t.Fatalf("usage chunk choices = %#v, want empty array", chunk["choices"])
	}
	usage := asMap(chunk["usage"])
	if intFromAny(usage["prompt_tokens"]) != 3 || intFromAny(usage["completion_tokens"]) != 4 || intFromAny(usage["total_tokens"]) != 7 {
		t.Fatalf("unexpected usage chunk: %+v", chunk)
	}
}

func TestCalculateUsageCost(t *testing.T) {
	provider := store.Provider{ID: "openai", Type: store.ProviderOpenAICompatible}
	usage := usageInfo{PromptTokens: 1_000_000, CompletionTokens: 500_000, CachedTokens: 100_000, ReasoningTokens: 10_000}
	cost := calculateUsageCost(provider, "gpt-4o-mini", usage)
	// (900k * 0.15 + 100k * 0.075 + 500k * 0.60 + 10k * 0.60) / 1M
	want := 0.4485
	if math.Abs(cost-want) > 0.000001 {
		t.Fatalf("cost = %.8f, want %.8f", cost, want)
	}
}
