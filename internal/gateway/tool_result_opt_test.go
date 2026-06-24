package gateway

import (
	"context"
	"strings"
	"testing"

	"github.com/local/vivurouter-go/internal/store"
	"github.com/local/vivurouter-go/internal/tokenopt"
)

func TestMaybeOptimizeAnthropicToolResultsOffByDefault(t *testing.T) {
	body := map[string]any{"messages": []any{map[string]any{"content": []any{map[string]any{"type": "tool_result", "content": strings.Repeat("noise\n", 3000)}}}}}
	out := maybeOptimizeAnthropicToolResults(context.Background(), body, store.Settings{})
	messages := out["messages"].([]any)
	message := messages[0].(map[string]any)
	content := message["content"].([]any)
	block := content[0].(map[string]any)
	if block["content"].(string) != strings.Repeat("noise\n", 3000) {
		t.Fatalf("expected original tool result when env disabled")
	}
}

func TestMaybeOptimizeAnthropicToolResultsCompactsWhenEnabled(t *testing.T) {
	longToolResult := strings.Repeat("noise line\n", 900) + "ERROR build failed\n" + strings.Repeat("tail noise\n", 900)
	body := map[string]any{"messages": []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "content": longToolResult}}}}}

	out := maybeOptimizeAnthropicToolResults(context.Background(), body, store.Settings{TokenOptimizeToolResults: true, TokenOptimizeMinChars: 1000, TokenOptimizeMaxChars: 2200})
	messages := out["messages"].([]any)
	message := messages[0].(map[string]any)
	content := message["content"].([]any)
	block := content[0].(map[string]any)
	text := block["content"].(string)
	if !strings.Contains(text, "VivuRouter token-optimized tool_result") {
		t.Fatalf("expected optimization marker in tool result")
	}
	if !strings.Contains(text, "ERROR build failed") {
		t.Fatalf("expected important error line to be preserved")
	}
	if block["vivurouter_token_optimized"] != true {
		t.Fatalf("expected optimization metadata")
	}

	originalMessages := body["messages"].([]any)
	originalMessage := originalMessages[0].(map[string]any)
	originalContent := originalMessage["content"].([]any)
	originalBlock := originalContent[0].(map[string]any)
	if originalBlock["content"].(string) != longToolResult {
		t.Fatalf("original body was mutated")
	}
}

func TestMaybeOptimizeAnthropicToolResultsSkipsNonToolContent(t *testing.T) {
	body := map[string]any{"messages": []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": strings.Repeat("user prompt\n", 2000)}}}}}
	out := maybeOptimizeAnthropicToolResults(context.Background(), body, store.Settings{TokenOptimizeToolResults: true, TokenOptimizeMinChars: 1000, TokenOptimizeMaxChars: 2200})
	messages := out["messages"].([]any)
	message := messages[0].(map[string]any)
	content := message["content"].([]any)
	block := content[0].(map[string]any)
	if _, ok := block["vivurouter_token_optimized"]; ok {
		t.Fatalf("non-tool text block should not be optimized")
	}
	if strings.Contains(block["text"].(string), "VivuRouter token-optimized") {
		t.Fatalf("normal text should not be optimized unless advanced text scope is enabled")
	}
}

func TestMaybeOptimizeAnthropicAdvancedScopes(t *testing.T) {
	longSystem := strings.Repeat("system guardrail line\n", 900) + "ERROR preserve system\n" + strings.Repeat("system tail\n", 900)
	longText := strings.Repeat("user context line\n", 900) + "ERROR preserve user\n" + strings.Repeat("user tail\n", 900)
	longSchemaDescription := strings.Repeat("schema description line\n", 900) + "ERROR preserve schema\n" + strings.Repeat("schema tail\n", 900)
	longArg := strings.Repeat("argument line\n", 900) + "ERROR preserve arg\n" + strings.Repeat("argument tail\n", 900)
	body := map[string]any{
		"system":   longSystem,
		"tools":    []any{map[string]any{"name": "lookup", "description": longSchemaDescription, "input_schema": map[string]any{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string", "description": longSchemaDescription}}}}},
		"messages": []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": longText}, map[string]any{"type": "tool_use", "id": "toolu_1", "name": "lookup", "input": map[string]any{"query": longArg}}}}},
	}
	out := maybeOptimizeAnthropicToolResults(context.Background(), body, store.Settings{TokenOptimizeSystem: true, TokenOptimizeText: true, TokenOptimizeToolSchemas: true, TokenOptimizeToolCalls: true, TokenOptimizeMinChars: 1000, TokenOptimizeMaxChars: 2200})
	if !strings.Contains(out["system"].(string), "VivuRouter token-optimized system") {
		t.Fatalf("expected system prompt optimization")
	}
	tools := out["tools"].([]any)
	tool := tools[0].(map[string]any)
	if !strings.Contains(tool["description"].(string), "VivuRouter token-optimized tool_schema") {
		t.Fatalf("expected tool description optimization")
	}
	schema := tool["input_schema"].(map[string]any)
	props := schema["properties"].(map[string]any)
	query := props["query"].(map[string]any)
	if !strings.Contains(query["description"].(string), "VivuRouter token-optimized structured_value") {
		t.Fatalf("expected nested schema description optimization")
	}
	messages := out["messages"].([]any)
	content := messages[0].(map[string]any)["content"].([]any)
	text := content[0].(map[string]any)
	if !strings.Contains(text["text"].(string), "VivuRouter token-optimized user") {
		t.Fatalf("expected user text optimization")
	}
	toolUse := content[1].(map[string]any)
	input := toolUse["input"].(map[string]any)
	if input["query"].(string) != longArg {
		t.Fatalf("tool call input must be preserved exactly")
	}
}

func TestMaybeOptimizeChatCompletionsToolMessage(t *testing.T) {
	longToolResult := strings.Repeat("chat tool noise\n", 900) + "ERROR chat tool failed\n" + strings.Repeat("tail\n", 900)
	body := map[string]any{"messages": []any{map[string]any{"role": "tool", "tool_call_id": "call_1", "content": longToolResult}}}
	out := maybeOptimizeChatCompletions(context.Background(), body, store.Settings{TokenOptimizeToolResults: true, TokenOptimizeMinChars: 1000, TokenOptimizeMaxChars: 2200})
	message := out["messages"].([]any)[0].(map[string]any)
	text := message["content"].(string)
	if !strings.Contains(text, "VivuRouter token-optimized tool_result") || !strings.Contains(text, "ERROR chat tool failed") {
		t.Fatalf("expected chat tool message to be optimized, got %q", text[:minInt(len(text), 120)])
	}
	if message["vivurouter_token_optimized"] != true {
		t.Fatalf("expected optimization metadata on chat tool message")
	}
}

func TestMaybeOptimizeChatCompletionsAdvancedScopes(t *testing.T) {
	longSystem := strings.Repeat("system line\n", 900) + "ERROR system\n" + strings.Repeat("tail\n", 900)
	longSchema := strings.Repeat("schema line\n", 900) + "ERROR schema\n" + strings.Repeat("tail\n", 900)
	longArgs := strings.Repeat("arg line\n", 900) + "ERROR args\n" + strings.Repeat("tail\n", 900)
	body := map[string]any{
		"tools": []any{map[string]any{"type": "function", "function": map[string]any{"name": "lookup", "description": longSchema, "parameters": map[string]any{"type": "object", "properties": map[string]any{"q": map[string]any{"type": "string", "description": longSchema}}}}}},
		"messages": []any{
			map[string]any{"role": "system", "content": longSystem},
			map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"id": "call_1", "type": "function", "function": map[string]any{"name": "lookup", "arguments": longArgs}}}},
		},
	}
	out := maybeOptimizeChatCompletions(context.Background(), body, store.Settings{TokenOptimizeSystem: true, TokenOptimizeToolSchemas: true, TokenOptimizeToolCalls: true, TokenOptimizeMinChars: 1000, TokenOptimizeMaxChars: 2200})
	messages := out["messages"].([]any)
	if !strings.Contains(messages[0].(map[string]any)["content"].(string), "VivuRouter token-optimized system") {
		t.Fatalf("expected chat system optimization")
	}
	fn := out["tools"].([]any)[0].(map[string]any)["function"].(map[string]any)
	if !strings.Contains(fn["description"].(string), "VivuRouter token-optimized tool_schema") {
		t.Fatalf("expected OpenAI tool description optimization")
	}
	props := fn["parameters"].(map[string]any)["properties"].(map[string]any)
	q := props["q"].(map[string]any)
	if !strings.Contains(q["description"].(string), "VivuRouter token-optimized structured_value") {
		t.Fatalf("expected OpenAI tool schema nested optimization")
	}
	callFn := messages[1].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)
	if callFn["arguments"].(string) != longArgs {
		t.Fatalf("OpenAI tool call arguments must be preserved exactly")
	}
}

func TestTokenOptimizeToolCallsPreservesReadOffsets(t *testing.T) {
	hugeOffset := float64(1101164347491414)
	anthropicBody := map[string]any{"messages": []any{map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "tool_use", "name": "Read", "input": map[string]any{"file_path": "main.tsx", "offset": hugeOffset, "limit": float64(90)}}}}}}
	anthropicOut := maybeOptimizeAnthropicToolResults(context.Background(), anthropicBody, store.Settings{TokenOptimizeToolCalls: true, TokenOptimizeMinChars: 1, TokenOptimizeMaxChars: 2000})
	anthropicInput := anthropicOut["messages"].([]any)[0].(map[string]any)["content"].([]any)[0].(map[string]any)["input"].(map[string]any)
	if anthropicInput["offset"].(float64) != hugeOffset {
		t.Fatalf("Anthropic Read offset changed: %v", anthropicInput["offset"])
	}

	args := `{"file_path":"main.tsx","offset":1101164347491414,"limit":90}`
	chatBody := map[string]any{"messages": []any{map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"type": "function", "function": map[string]any{"name": "Read", "arguments": args}}}}}}
	chatOut := maybeOptimizeChatCompletions(context.Background(), chatBody, store.Settings{TokenOptimizeToolCalls: true, TokenOptimizeMinChars: 1, TokenOptimizeMaxChars: 2000})
	chatArgs := chatOut["messages"].([]any)[0].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)["arguments"].(string)
	if chatArgs != args {
		t.Fatalf("OpenAI Read arguments changed: %q", chatArgs)
	}
}

func TestMaybeOptimizeAnthropicToolResultsUsesRTKWhenEnabled(t *testing.T) {
	old := compactToolResultWithRTK
	t.Cleanup(func() { compactToolResultWithRTK = old })
	called := false
	compactToolResultWithRTK = func(ctx context.Context, input string, opts tokenopt.Options, settings store.Settings) tokenopt.Result {
		called = true
		if !settings.RTKEnabled {
			t.Fatalf("expected RTK setting to be enabled")
		}
		return tokenopt.ResultFromCompactText(input, "rtk compacted ERROR preserved", "rtk test")
	}

	longToolResult := strings.Repeat("noise line\n", 900) + "ERROR build failed\n" + strings.Repeat("tail noise\n", 900)
	body := map[string]any{"messages": []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "content": longToolResult}}}}}
	out := maybeOptimizeAnthropicToolResults(context.Background(), body, store.Settings{TokenOptimizeToolResults: true, RTKEnabled: true, TokenOptimizeMinChars: 1000, TokenOptimizeMaxChars: 2200})
	if !called {
		t.Fatalf("expected RTK compactor to be called")
	}
	messages := out["messages"].([]any)
	message := messages[0].(map[string]any)
	content := message["content"].([]any)
	block := content[0].(map[string]any)
	if !strings.Contains(block["content"].(string), "rtk compacted ERROR preserved") {
		t.Fatalf("expected RTK compacted output")
	}
}
