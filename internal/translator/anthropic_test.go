package translator

import (
	"encoding/json"
	"testing"
)

func TestAnthropicMessagesToChatTextAndSystem(t *testing.T) {
	body := map[string]any{
		"model":      "claude-test",
		"system":     "be helpful",
		"max_tokens": json.Number("128"),
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
	}
	chat := AnthropicMessagesToChat(body, "openai/gpt-test")
	if chat["model"] != "openai/gpt-test" || chat["max_tokens"] == nil {
		t.Fatalf("unexpected chat body: %+v", chat)
	}
	messages := chat["messages"].([]map[string]any)
	if len(messages) != 2 || messages[0]["role"] != "system" || messages[0]["content"] != "be helpful" {
		t.Fatalf("unexpected messages: %+v", messages)
	}
	if messages[1]["role"] != "user" || messages[1]["content"] != "hello" {
		t.Fatalf("unexpected user message: %+v", messages[1])
	}
}

func TestAnthropicMessagesToChatTools(t *testing.T) {
	body := map[string]any{
		"messages": []any{
			map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "tool_use", "id": "toolu_1", "name": "lookup", "input": map[string]any{"q": "x"}}}},
			map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": "toolu_1", "content": "ok"}}},
		},
		"tools": []any{map[string]any{"name": "lookup", "description": "Lookup", "input_schema": map[string]any{"type": "object"}}},
	}
	chat := AnthropicMessagesToChat(body, "gpt-test")
	messages := chat["messages"].([]map[string]any)
	if len(messages) != 2 {
		t.Fatalf("unexpected message count: %+v", messages)
	}
	calls := messages[0]["tool_calls"].([]map[string]any)
	if calls[0]["id"] != "toolu_1" || asMap(calls[0]["function"])["name"] != "lookup" {
		t.Fatalf("unexpected tool call: %+v", calls[0])
	}
	if messages[1]["role"] != "tool" || messages[1]["tool_call_id"] != "toolu_1" || messages[1]["content"] != "ok" {
		t.Fatalf("unexpected tool result: %+v", messages[1])
	}
	tools := chat["tools"].([]map[string]any)
	if len(tools) != 1 || asMap(tools[0]["function"])["name"] != "lookup" {
		t.Fatalf("unexpected tools: %+v", tools)
	}
}

func TestAnthropicMessagesToChatPreservesLongClaudeCodeContext(t *testing.T) {
	body := map[string]any{
		"system": []any{
			map[string]any{"type": "text", "text": "You are Claude Code.", "cache_control": map[string]any{"type": "ephemeral"}},
			map[string]any{"type": "text", "text": "Large project context here."},
		},
		"messages": []any{
			map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "text", "text": "Earlier user turn"},
				map[string]any{"type": "text", "text": "Selected file context with many tokens"},
			}},
			map[string]any{"role": "assistant", "content": []any{
				map[string]any{"type": "text", "text": "I will inspect it."},
				map[string]any{"type": "tool_use", "id": "toolu_read", "name": "Read", "input": map[string]any{"file_path": "README.md"}},
			}},
			map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "tool_result", "tool_use_id": "toolu_read", "content": []any{map[string]any{"type": "text", "text": "README contents"}}},
				map[string]any{"type": "text", "text": "Continue previous task"},
			}},
		},
	}
	chat := AnthropicMessagesToChat(body, "gpt-test")
	messages := chat["messages"].([]map[string]any)
	if len(messages) != 5 {
		t.Fatalf("message count = %d, want 5: %+v", len(messages), messages)
	}
	if messages[0]["role"] != "system" || messages[0]["content"] != "You are Claude Code.\nLarge project context here." {
		t.Fatalf("system context not preserved: %+v", messages[0])
	}
	if messages[1]["role"] != "user" || messages[1]["content"] != "Earlier user turn\nSelected file context with many tokens" {
		t.Fatalf("multi text user context not preserved: %+v", messages[1])
	}
	if messages[2]["role"] != "assistant" || messages[2]["content"] != "I will inspect it." || len(messages[2]["tool_calls"].([]map[string]any)) != 1 {
		t.Fatalf("assistant tool_use not preserved: %+v", messages[2])
	}
	if messages[3]["role"] != "tool" || messages[3]["content"] != "README contents" {
		t.Fatalf("tool result not preserved/order wrong: %+v", messages[3])
	}
	if messages[4]["role"] != "user" || messages[4]["content"] != "Continue previous task" {
		t.Fatalf("post tool user text not preserved: %+v", messages[4])
	}
}

func TestAnthropicMessagesToResponsesPreservesContextForCodex(t *testing.T) {
	body := map[string]any{
		"system": []any{
			map[string]any{"type": "text", "text": "You are Claude Code."},
			map[string]any{"type": "text", "text": "Large project context here."},
		},
		"messages": []any{
			map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "text", "text": "Earlier user turn"},
				map[string]any{"type": "text", "text": "Selected file context with many tokens"},
			}},
			map[string]any{"role": "assistant", "content": []any{
				map[string]any{"type": "text", "text": "I will inspect it."},
				map[string]any{"type": "tool_use", "id": "toolu_read", "name": "Read", "input": map[string]any{"file_path": "README.md"}},
			}},
			map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "tool_result", "tool_use_id": "toolu_read", "content": []any{map[string]any{"type": "text", "text": "README contents"}}},
				map[string]any{"type": "text", "text": "Continue previous task"},
			}},
		},
	}
	chat := AnthropicMessagesToChat(body, "gpt-test")
	responses := ChatToResponses("gpt-test", chat)
	input := responses["input"].([]map[string]any)
	if len(input) != 6 {
		t.Fatalf("responses input count = %d, want 6: %+v", len(input), input)
	}
	systemContent := input[0]["content"].([]map[string]any)
	if input[0]["role"] != "developer" || systemContent[0]["text"] != "You are Claude Code.\nLarge project context here." {
		t.Fatalf("system context not preserved as developer input: %+v", input[0])
	}
	firstContent := input[1]["content"].([]map[string]any)
	if input[1]["role"] != "user" || firstContent[0]["text"] != "Earlier user turn\nSelected file context with many tokens" {
		t.Fatalf("user context not preserved for responses: %+v", input[1])
	}
	if input[2]["role"] != "assistant" || input[2]["content"].([]map[string]any)[0]["text"] != "I will inspect it." {
		t.Fatalf("assistant text not preserved for responses: %+v", input[2])
	}
	if input[3]["type"] != "function_call" || input[3]["call_id"] != "toolu_read" {
		t.Fatalf("tool call not preserved for responses: %+v", input[3])
	}
	if input[4]["type"] != "function_call_output" || input[4]["output"] != "README contents" {
		t.Fatalf("tool result not preserved for responses: %+v", input[4])
	}
	if input[5]["role"] != "user" || input[5]["content"].([]map[string]any)[0]["text"] != "Continue previous task" {
		t.Fatalf("post-tool user text not preserved for responses: %+v", input[5])
	}
}

func TestAnthropicMessagesToChatAddsMissingToolResponse(t *testing.T) {
	body := map[string]any{"messages": []any{
		map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "tool_use", "id": "toolu_missing", "name": "Read", "input": map[string]any{"file_path": "x"}}}},
		map[string]any{"role": "user", "content": "next turn"},
	}}
	chat := AnthropicMessagesToChat(body, "gpt-test")
	messages := chat["messages"].([]map[string]any)
	if len(messages) != 3 || messages[1]["role"] != "tool" || messages[1]["tool_call_id"] != "toolu_missing" {
		t.Fatalf("missing tool response was not inserted: %+v", messages)
	}
}

func TestAnthropicMessagesToResponsesDirect(t *testing.T) {
	body := map[string]any{
		"system": "You are Claude Code.",
		"messages": []any{
			map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": "Inspect repo"}}},
			map[string]any{"role": "assistant", "content": []any{
				map[string]any{"type": "text", "text": "I will read."},
				map[string]any{"type": "tool_use", "id": "toolu_1", "name": "Read", "input": map[string]any{"file_path": "README.md"}},
			}},
			map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": "toolu_1", "content": "ok"}}},
		},
		"tools": []any{map[string]any{"name": "Read", "description": "Read file", "input_schema": map[string]any{"type": "object"}}},
	}
	responses := AnthropicMessagesToResponses(body, "cx/gpt-5.5")
	input := responses["input"].([]map[string]any)
	if len(input) != 5 {
		t.Fatalf("input count = %d: %+v", len(input), input)
	}
	if input[0]["role"] != "developer" || input[0]["type"] != "message" {
		t.Fatalf("system was not developer input: %+v", input[0])
	}
	if input[1]["role"] != "user" || input[2]["role"] != "assistant" {
		t.Fatalf("message roles not preserved: %+v", input)
	}
	if input[3]["type"] != "function_call" || input[3]["call_id"] != "toolu_1" || input[3]["name"] != "Read" {
		t.Fatalf("tool_use not converted directly: %+v", input[3])
	}
	if input[4]["type"] != "function_call_output" || input[4]["call_id"] != "toolu_1" || input[4]["output"] != "ok" {
		t.Fatalf("tool_result not converted directly: %+v", input[4])
	}
	tools := responses["tools"].([]map[string]any)
	if len(tools) != 1 || tools[0]["type"] != "function" || tools[0]["name"] != "Read" {
		t.Fatalf("tools not converted directly: %+v", tools)
	}
}

func TestChatJSONToAnthropic(t *testing.T) {
	raw := []byte(`{
		"id":"chatcmpl-test",
		"model":"gpt-test",
		"choices":[{"finish_reason":"tool_calls","message":{"content":"hello","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"q\":\"x\"}"}}]}}],
		"usage":{"prompt_tokens":10,"completion_tokens":3,"total_tokens":13}
	}`)
	converted, err := ChatJSONToAnthropic(raw, "fallback")
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(converted, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["type"] != "message" || payload["role"] != "assistant" || payload["stop_reason"] != "tool_use" {
		t.Fatalf("unexpected anthropic payload: %+v", payload)
	}
	content := payload["content"].([]any)
	if len(content) != 2 || asMap(content[1])["type"] != "tool_use" {
		t.Fatalf("unexpected content: %+v", content)
	}
	usage := asMap(payload["usage"])
	if intAny(usage["input_tokens"]) != 10 || intAny(usage["output_tokens"]) != 3 {
		t.Fatalf("unexpected usage: %+v", usage)
	}
}
