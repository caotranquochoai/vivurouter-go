package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/local/vivurouter-go/internal/rtkbridge"
	"github.com/local/vivurouter-go/internal/store"
	"github.com/local/vivurouter-go/internal/tokenopt"
)

var compactToolResultWithRTK = func(ctx context.Context, input string, opts tokenopt.Options, settings store.Settings) tokenopt.Result {
	if !settings.RTKEnabled {
		return tokenopt.CompactToolResult(input, opts)
	}
	cfg := rtkbridge.ResolveConfig(settings)
	if !cfg.Enabled || !cfg.Detection.Found || !cfg.Detection.CanRunNow {
		return tokenopt.CompactToolResult(input, opts)
	}
	res, err := cfg.Runner.CompactToolResult(ctx, input, opts)
	if err != nil || !res.Applied {
		return tokenopt.CompactToolResult(input, opts)
	}
	return res
}

func maybeOptimizeAnthropicToolResults(ctx context.Context, body map[string]any, settings store.Settings) map[string]any {
	if !settings.TokenOptimizeToolResults && !hasAdvancedTokenOptimization(settings) {
		return body
	}
	optimized := deepCloneMap(body)
	if optimized == nil {
		return body
	}
	minChars := settings.TokenOptimizeMinChars
	if minChars <= 0 {
		minChars = 12000
	}
	maxChars := settings.TokenOptimizeMaxChars
	if maxChars <= 0 {
		maxChars = 12000
	}
	if maxChars < 2000 {
		maxChars = 2000
	}
	opts := tokenopt.Options{MinChars: minChars, MaxChars: maxChars, PreserveErrors: true}
	optimizeMessagesPayload(ctx, optimized, opts, settings)
	return optimized
}

func maybeOptimizeChatCompletions(ctx context.Context, body map[string]any, settings store.Settings) map[string]any {
	if !settings.TokenOptimizeToolResults && !hasAdvancedTokenOptimization(settings) {
		return body
	}
	optimized := deepCloneMap(body)
	if optimized == nil {
		return body
	}
	minChars := settings.TokenOptimizeMinChars
	if minChars <= 0 {
		minChars = 12000
	}
	maxChars := settings.TokenOptimizeMaxChars
	if maxChars <= 0 {
		maxChars = 12000
	}
	if maxChars < 2000 {
		maxChars = 2000
	}
	opts := tokenopt.Options{MinChars: minChars, MaxChars: maxChars, PreserveErrors: true}
	optimizeChatPayload(ctx, optimized, opts, settings)
	return optimized
}

func hasAdvancedTokenOptimization(settings store.Settings) bool {
	return settings.TokenOptimizeSystem || settings.TokenOptimizeDeveloper || settings.TokenOptimizeText || settings.TokenOptimizeToolSchemas || settings.TokenOptimizeToolCalls
}

func optimizeMessagesPayload(ctx context.Context, body map[string]any, opts tokenopt.Options, settings store.Settings) {
	if settings.TokenOptimizeSystem {
		optimizeSystemPrompt(body, opts)
	}
	if settings.TokenOptimizeToolSchemas {
		optimizeToolSchemas(body, opts)
	}
	messages, ok := body["messages"].([]any)
	if !ok {
		return
	}
	for _, msg := range messages {
		message, ok := msg.(map[string]any)
		if !ok {
			continue
		}
		role := strings.TrimSpace(debugString(message["role"]))
		if role == "developer" && settings.TokenOptimizeDeveloper {
			optimizeMessageContentStrings(message, "developer", opts)
		}
		content, ok := message["content"].([]any)
		if !ok {
			if role == "developer" || settings.TokenOptimizeText {
				optimizeStringField(message, "content", role, opts)
			}
			continue
		}
		for _, item := range content {
			block, ok := item.(map[string]any)
			if !ok {
				continue
			}
			typeName := strings.TrimSpace(debugString(block["type"]))
			switch typeName {
			case "tool_result":
				if settings.TokenOptimizeToolResults {
					optimizeToolResultBlock(ctx, block, opts, settings)
				}
			case "text":
				if settings.TokenOptimizeText || (role == "developer" && settings.TokenOptimizeDeveloper) {
					optimizeTextBlock(block, role, opts)
				}
			case "tool_use":
				// Tool-use input is executable JSON passed to tools. Never compact or
				// truncate it: changing fields such as file offsets, line numbers, paths,
				// or command arguments can make the model call tools with invalid values.
				// Keep TokenOptimizeToolCalls limited to schema/description savings.
				continue
			default:
				if role == "developer" && settings.TokenOptimizeDeveloper {
					optimizeTextBlock(block, role, opts)
				}
			}
		}
	}
}

func optimizeChatPayload(ctx context.Context, body map[string]any, opts tokenopt.Options, settings store.Settings) {
	if settings.TokenOptimizeToolSchemas {
		optimizeOpenAIToolSchemas(body, opts)
	}
	messages, ok := body["messages"].([]any)
	if !ok {
		return
	}
	for _, msg := range messages {
		message, ok := msg.(map[string]any)
		if !ok {
			continue
		}
		role := strings.TrimSpace(debugString(message["role"]))
		switch role {
		case "tool":
			if settings.TokenOptimizeToolResults {
				optimizeChatToolMessage(ctx, message, opts, settings)
			}
		case "system":
			if settings.TokenOptimizeSystem {
				optimizeOpenAIMessageContent(message, "system", opts)
			}
		case "developer":
			if settings.TokenOptimizeDeveloper {
				optimizeOpenAIMessageContent(message, "developer", opts)
			}
		default:
			if settings.TokenOptimizeText {
				optimizeOpenAIMessageContent(message, role, opts)
			}
		}
		// Tool-call arguments are executable JSON; do not optimize them. See the
		// Anthropic tool_use branch above for why offsets/paths/commands must be
		// preserved exactly.
	}
}

func optimizeOpenAIToolSchemas(body map[string]any, opts tokenopt.Options) {
	tools, ok := body["tools"].([]any)
	if !ok {
		return
	}
	for _, item := range tools {
		tool, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if fn, ok := tool["function"].(map[string]any); ok {
			optimizeStringField(fn, "description", "tool_schema", opts)
			if params, ok := fn["parameters"]; ok {
				fn["parameters"] = compactStructuredValue(params, opts)
			}
			continue
		}
		optimizeStringField(tool, "description", "tool_schema", opts)
	}
}

func optimizeOpenAIMessageContent(message map[string]any, kind string, opts tokenopt.Options) {
	switch content := message["content"].(type) {
	case string:
		message["content"] = compactStringWithMarker(content, kind, opts)
	case []any:
		for _, item := range content {
			block, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if strings.TrimSpace(debugString(block["type"])) == "text" {
				optimizeTextBlock(block, kind, opts)
			}
		}
	}
}

func optimizeChatToolMessage(ctx context.Context, message map[string]any, opts tokenopt.Options, settings store.Settings) {
	raw, ok := message["content"].(string)
	if !ok || strings.TrimSpace(raw) == "" {
		return
	}
	res := compactToolResultWithRTK(ctx, raw, opts, settings)
	if !res.Applied {
		return
	}
	message["content"] = tokenOptimizationMarker("tool_result", res) + "\n\n" + res.Text
	message["vivurouter_token_optimized"] = true
	message["vivurouter_original_chars"] = res.OriginalChars
	message["vivurouter_compact_chars"] = res.CompactChars
	message["vivurouter_estimated_tokens_saved"] = res.EstimatedSavedTokens
	message["vivurouter_optimizer_engine"] = optimizerEngineFromReason(res.Reason)
}

func optimizeOpenAIToolCalls(message map[string]any, opts tokenopt.Options) {
	calls, ok := message["tool_calls"].([]any)
	if !ok {
		return
	}
	for _, item := range calls {
		call, ok := item.(map[string]any)
		if !ok {
			continue
		}
		fn, ok := call["function"].(map[string]any)
		if !ok {
			continue
		}
		optimizeStringField(fn, "arguments", "tool_call_arguments", opts)
	}
}

func optimizeSystemPrompt(body map[string]any, opts tokenopt.Options) {
	switch system := body["system"].(type) {
	case string:
		body["system"] = compactStringWithMarker(system, "system", opts)
	case []any:
		for _, item := range system {
			if block, ok := item.(map[string]any); ok {
				optimizeTextBlock(block, "system", opts)
			}
		}
	}
}

func optimizeMessageContentStrings(message map[string]any, kind string, opts tokenopt.Options) {
	optimizeStringField(message, "content", kind, opts)
}

func optimizeToolSchemas(body map[string]any, opts tokenopt.Options) {
	tools, ok := body["tools"].([]any)
	if !ok {
		return
	}
	for _, item := range tools {
		tool, ok := item.(map[string]any)
		if !ok {
			continue
		}
		optimizeStringField(tool, "description", "tool_schema", opts)
		if schema, ok := tool["input_schema"]; ok {
			tool["input_schema"] = compactStructuredValue(schema, opts)
		}
	}
}

func optimizeToolCallBlock(block map[string]any, opts tokenopt.Options) {
	if input, ok := block["input"]; ok {
		block["input"] = compactStructuredValue(input, opts)
	}
}

func optimizeToolResultBlock(ctx context.Context, block map[string]any, opts tokenopt.Options, settings store.Settings) {
	raw, ok := block["content"].(string)
	if !ok || strings.TrimSpace(raw) == "" {
		return
	}
	res := compactToolResultWithRTK(ctx, raw, opts, settings)
	if !res.Applied {
		return
	}
	block["content"] = tokenOptimizationMarker("tool_result", res) + "\n\n" + res.Text
	block["vivurouter_token_optimized"] = true
	block["vivurouter_original_chars"] = res.OriginalChars
	block["vivurouter_compact_chars"] = res.CompactChars
	block["vivurouter_estimated_tokens_saved"] = res.EstimatedSavedTokens
	block["vivurouter_optimizer_engine"] = optimizerEngineFromReason(res.Reason)
}

func optimizeTextBlock(block map[string]any, kind string, opts tokenopt.Options) {
	optimizeStringField(block, "text", kind, opts)
}

func optimizeStringField(target map[string]any, field string, kind string, opts tokenopt.Options) {
	raw, ok := target[field].(string)
	if !ok || strings.TrimSpace(raw) == "" {
		return
	}
	target[field] = compactStringWithMarker(raw, kind, opts)
}

func compactStringWithMarker(raw string, kind string, opts tokenopt.Options) string {
	res := tokenopt.CompactToolResult(raw, opts)
	if !res.Applied {
		return raw
	}
	return tokenOptimizationMarker(kind, res) + "\n\n" + res.Text
}

func compactStructuredValue(value any, opts tokenopt.Options) any {
	raw, err := json.Marshal(value)
	if err != nil || len([]rune(string(raw))) < opts.MinChars {
		return value
	}
	return compactStructuredValueRecursive(value, 0, opts)
}

func compactStructuredValueRecursive(value any, depth int, opts tokenopt.Options) any {
	if depth >= 6 {
		return "…"
	}
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, child := range v {
			out[key] = compactStructuredValueRecursive(child, depth+1, opts)
		}
		return out
	case []any:
		limit := len(v)
		if limit > 50 {
			limit = 50
		}
		out := make([]any, 0, limit+1)
		for i := 0; i < limit; i++ {
			out = append(out, compactStructuredValueRecursive(v[i], depth+1, opts))
		}
		if len(v) > limit {
			out = append(out, fmt.Sprintf("… %d more items", len(v)-limit))
		}
		return out
	case string:
		if len([]rune(v)) < opts.MinChars {
			return v
		}
		res := tokenopt.CompactToolResult(v, tokenopt.Options{MinChars: opts.MinChars, MaxChars: opts.MaxChars, PreserveErrors: opts.PreserveErrors})
		if !res.Applied {
			return v
		}
		return tokenOptimizationMarker("structured_value", res) + "\n\n" + res.Text
	default:
		return v
	}
}

func tokenOptimizationMarker(kind string, res tokenopt.Result) string {
	return fmt.Sprintf("[VivuRouter token-optimized %s: original %d chars -> %d chars, estimated saved %d tokens]", kind, res.OriginalChars, res.CompactChars, res.EstimatedSavedTokens)
}

func deepCloneMap(input map[string]any) map[string]any {
	raw, err := json.Marshal(input)
	if err != nil {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}
