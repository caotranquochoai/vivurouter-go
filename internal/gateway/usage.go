package gateway

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/local/vivurouter-go/internal/store"
)

var runtimeSettingsProvider func() (store.Settings, error)

func SetRuntimeSettingsProvider(fn func() (store.Settings, error)) {
	runtimeSettingsProvider = fn
}

// usageInfo is the gateway-local normalized usage record before persistence.
type usageInfo struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	CachedTokens     int
	ReasoningTokens  int
	Estimated        bool
	CostUSD          float64
}

func (u usageInfo) hasTokens() bool {
	return u.PromptTokens > 0 || u.CompletionTokens > 0 || u.TotalTokens > 0 || u.CachedTokens > 0 || u.ReasoningTokens > 0
}

func ensureStreamUsageOptions(body map[string]any) map[string]any {
	out := cloneMap(body)
	options := cloneMap(asMap(out["stream_options"]))
	options["include_usage"] = true
	out["stream_options"] = options
	return out
}

func openAIUsageChunk(id string, created int64, model string, usage usageInfo) map[string]any {
	return map[string]any{
		"id":      id,
		"object":  "chat.completion.chunk",
		"created": created,
		"model":   model,
		"choices": []any{},
		"usage":   usageToOpenAIMap(usage),
	}
}

func usageToOpenAIMap(usage usageInfo) map[string]any {
	promptDetails := map[string]any{}
	if usage.CachedTokens > 0 {
		promptDetails["cached_tokens"] = usage.CachedTokens
	}
	completionDetails := map[string]any{}
	if usage.ReasoningTokens > 0 {
		completionDetails["reasoning_tokens"] = usage.ReasoningTokens
	}
	out := map[string]any{
		"prompt_tokens":     usage.PromptTokens,
		"completion_tokens": usage.CompletionTokens,
		"total_tokens":      usage.TotalTokens,
	}
	if len(promptDetails) > 0 {
		out["prompt_tokens_details"] = promptDetails
	}
	if len(completionDetails) > 0 {
		out["completion_tokens_details"] = completionDetails
	}
	if usage.Estimated {
		out["estimated"] = true
	}
	return out
}

type PublicUsageInfo struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	CachedTokens     int
	ReasoningTokens  int
	Estimated        bool
	CostUSD          float64
}

func (u usageInfo) withCost(provider store.Provider, model string) usageInfo {
	if !u.hasTokens() {
		return u
	}
	u.CostUSD = calculateUsageCost(provider, model, u)
	return u
}

func (u usageInfo) ensureEstimated(requestBody map[string]any, outputChars int) usageInfo {
	if u.hasTokens() {
		return u
	}
	return estimateUsage(requestBody, outputChars)
}

func AnalyzeUsage(provider store.Provider, model string, requestBody map[string]any, rawResponse []byte, outputChars int) PublicUsageInfo {
	usage, ok := extractUsageFromJSON(rawResponse)
	if !ok || !usage.hasTokens() {
		if outputChars <= 0 {
			outputChars = estimateOutputCharsFromJSON(rawResponse)
		}
		usage = estimateUsage(requestBody, outputChars)
	}
	usage = usage.withCost(provider, model)
	return PublicUsageInfo{
		PromptTokens:     usage.PromptTokens,
		CompletionTokens: usage.CompletionTokens,
		TotalTokens:      usage.TotalTokens,
		CachedTokens:     usage.CachedTokens,
		ReasoningTokens:  usage.ReasoningTokens,
		Estimated:        usage.Estimated,
		CostUSD:          usage.CostUSD,
	}
}

func passthroughJSONWithUsage(w http.ResponseWriter, resp *http.Response, requestBody map[string]any) (usageInfo, error) {
	copyHeaders(w.Header(), resp.Header)
	raw, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return usageInfo{}, readErr
	}

	usage, ok := extractUsageFromJSON(raw)
	if !ok || !usage.hasTokens() {
		usage = estimateUsage(requestBody, estimateOutputCharsFromJSON(raw))
		raw = ensureUsageInJSON(raw, usage)
	}

	w.WriteHeader(resp.StatusCode)
	if len(raw) > 0 {
		if _, err := w.Write(raw); err != nil {
			return usageInfo{}, err
		}
	}
	return usage, nil
}

func ensureUsageInJSON(raw []byte, usage usageInfo) []byte {
	if !usage.hasTokens() || len(bytes.TrimSpace(raw)) == 0 {
		return raw
	}
	var payload map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil || len(payload) == 0 {
		return raw
	}
	if existing := asMap(payload["usage"]); len(existing) > 0 {
		return raw
	}
	payload["usage"] = usageToOpenAIMap(usage)
	updated, err := json.Marshal(payload)
	if err != nil {
		return raw
	}
	return updated
}

func extractUsageFromJSON(raw []byte) (usageInfo, bool) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return usageInfo{}, false
	}
	var payload map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return usageInfo{}, false
	}
	return extractUsageFromPayload(payload)
}

func extractUsageFromPayload(payload map[string]any) (usageInfo, bool) {
	if len(payload) == 0 {
		return usageInfo{}, false
	}

	if usage := asMap(payload["usage"]); len(usage) > 0 {
		if out, ok := usageFromMap(usage); ok {
			return out, true
		}
	}

	if response := asMap(payload["response"]); len(response) > 0 {
		if usage := asMap(response["usage"]); len(usage) > 0 {
			if out, ok := usageFromMap(usage); ok {
				return out, true
			}
		}
	}

	if usageMeta := asMap(payload["usageMetadata"]); len(usageMeta) > 0 {
		return usageFromGeminiMap(usageMeta)
	}
	if response := asMap(payload["response"]); len(response) > 0 {
		if usageMeta := asMap(response["usageMetadata"]); len(usageMeta) > 0 {
			return usageFromGeminiMap(usageMeta)
		}
	}

	if boolFromAny(payload["done"]) && intFromAny(payload["prompt_eval_count"]) > 0 {
		prompt := intFromAny(payload["prompt_eval_count"])
		completion := intFromAny(payload["eval_count"])
		return normalizeUsage(prompt, completion, prompt+completion, 0, 0, false), true
	}

	return usageInfo{}, false
}

func usageFromMap(usage map[string]any) (usageInfo, bool) {
	prompt := firstPositiveInt(usage, "prompt_tokens", "input_tokens", "inputTokens")
	completion := firstPositiveInt(usage, "completion_tokens", "output_tokens", "completionTokens", "outputTokens")
	total := firstPositiveInt(usage, "total_tokens", "totalTokens")
	cached := firstPositiveInt(usage, "cached_tokens", "cached_input_tokens", "cache_read_input_tokens", "cache_read_tokens", "cacheReadInputTokens", "prompt_cache_hit_tokens")
	reasoning := firstPositiveInt(usage, "reasoning_tokens")

	if details := asMap(usage["prompt_tokens_details"]); len(details) > 0 {
		cached = maxInt(cached, firstPositiveInt(details, "cached_tokens", "cached_input_tokens", "cache_read_input_tokens", "cache_read_tokens", "cacheReadInputTokens"))
	}
	if details := asMap(usage["input_tokens_details"]); len(details) > 0 {
		cached = maxInt(cached, firstPositiveInt(details, "cached_tokens", "cached_input_tokens", "cache_read_input_tokens", "cache_read_tokens", "cacheReadInputTokens"))
	}
	if details := asMap(usage["completion_tokens_details"]); len(details) > 0 {
		reasoning = maxInt(reasoning, firstPositiveInt(details, "reasoning_tokens"))
	}
	if details := asMap(usage["output_tokens_details"]); len(details) > 0 {
		reasoning = maxInt(reasoning, firstPositiveInt(details, "reasoning_tokens"))
	}

	out := normalizeUsage(prompt, completion, total, cached, reasoning, boolFromAny(usage["estimated"]))
	return out, out.hasTokens()
}

func usageFromGeminiMap(usage map[string]any) (usageInfo, bool) {
	prompt := firstPositiveInt(usage, "promptTokenCount", "prompt_tokens")
	completion := firstPositiveInt(usage, "candidatesTokenCount", "completion_tokens")
	total := firstPositiveInt(usage, "totalTokenCount", "total_tokens")
	cached := firstPositiveInt(usage, "cachedContentTokenCount", "cached_tokens")
	reasoning := firstPositiveInt(usage, "thoughtsTokenCount", "reasoning_tokens")
	out := normalizeUsage(prompt, completion, total, cached, reasoning, boolFromAny(usage["estimated"]))
	return out, out.hasTokens()
}

func normalizeUsage(prompt, completion, total, cached, reasoning int, estimated bool) usageInfo {
	if total <= 0 {
		total = prompt + completion
	}
	if prompt < 0 {
		prompt = 0
	}
	if completion < 0 {
		completion = 0
	}
	if cached < 0 {
		cached = 0
	}
	if reasoning < 0 {
		reasoning = 0
	}
	return usageInfo{
		PromptTokens:     prompt,
		CompletionTokens: completion,
		TotalTokens:      total,
		CachedTokens:     cached,
		ReasoningTokens:  reasoning,
		Estimated:        estimated,
	}
}

func estimateUsage(body map[string]any, outputChars int) usageInfo {
	inputTokens := estimateInputTokens(body)
	outputTokens := estimateOutputTokens(outputChars)
	if inputTokens == 0 && len(body) > 0 {
		inputTokens = 1
	}
	return normalizeUsage(inputTokens, outputTokens, inputTokens+outputTokens, 0, 0, true)
}

func estimateInputTokens(body map[string]any) int {
	if len(body) == 0 {
		return 0
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return 0
	}
	return estimateOutputTokens(len(raw))
}

func estimateOutputTokens(chars int) int {
	if chars <= 0 {
		return 0
	}
	return int(math.Ceil(float64(chars) / 4.0))
}

func estimateOutputCharsFromJSON(raw []byte) int {
	var payload map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return len(raw)
	}
	chars := outputCharsFromPayload(payload, "")
	if chars > 0 {
		return chars
	}
	return len(raw)
}

func outputCharsFromPayload(payload map[string]any, eventType string) int {
	if len(payload) == 0 {
		return 0
	}
	if delta := asStringLocal(payload["delta"]); delta != "" {
		return len(delta)
	}
	if response := asMap(payload["response"]); len(response) > 0 {
		if outputText := asStringLocal(response["output_text"]); outputText != "" {
			return len(outputText)
		}
		if items := anySlice(response["output"]); len(items) > 0 {
			return outputCharsFromItems(items)
		}
	}
	if outputText := asStringLocal(payload["output_text"]); outputText != "" {
		return len(outputText)
	}
	if choices := anySlice(payload["choices"]); len(choices) > 0 {
		chars := 0
		for _, rawChoice := range choices {
			choice := asMap(rawChoice)
			if delta := asMap(choice["delta"]); len(delta) > 0 {
				chars += len(asStringLocal(delta["content"]))
				chars += len(asStringLocal(delta["reasoning_content"]))
				for _, rawTool := range anySlice(delta["tool_calls"]) {
					tool := asMap(rawTool)
					fn := asMap(tool["function"])
					chars += len(asStringLocal(fn["arguments"]))
				}
			}
			if message := asMap(choice["message"]); len(message) > 0 {
				chars += len(asStringLocal(message["content"]))
				chars += len(asStringLocal(message["reasoning_content"]))
				for _, rawTool := range anySlice(message["tool_calls"]) {
					tool := asMap(rawTool)
					fn := asMap(tool["function"])
					chars += len(asStringLocal(fn["arguments"]))
				}
			}
		}
		return chars
	}
	if items := anySlice(payload["output"]); len(items) > 0 {
		return outputCharsFromItems(items)
	}
	return 0
}

func outputCharsFromItems(items []any) int {
	chars := 0
	for _, rawItem := range items {
		item := asMap(rawItem)
		chars += len(asStringLocal(item["text"]))
		chars += len(asStringLocal(item["arguments"]))
		chars += len(asStringLocal(item["input"]))
		for _, rawContent := range anySlice(item["content"]) {
			content := asMap(rawContent)
			chars += len(asStringLocal(content["text"]))
		}
	}
	return chars
}

type usagePricing struct {
	Input       float64
	Output      float64
	CachedInput float64
	Reasoning   float64
}

var defaultUsagePricing = map[string]usagePricing{
	"gpt-4o-mini":  {Input: 0.15, Output: 0.60, CachedInput: 0.075, Reasoning: 0.60},
	"gpt-4.1":      {Input: 2.00, Output: 8.00, CachedInput: 0.50, Reasoning: 8.00},
	"gpt-4.1-mini": {Input: 0.40, Output: 1.60, CachedInput: 0.10, Reasoning: 1.60},
	"gpt-4.1-nano": {Input: 0.10, Output: 0.40, CachedInput: 0.025, Reasoning: 0.40},
	"gpt-5-codex":  {Input: 1.25, Output: 10.00, CachedInput: 0.125, Reasoning: 10.00},
	"cx/gpt-5.5":   {Input: 1.25, Output: 10.00, CachedInput: 0.125, Reasoning: 10.00},
	"cx/gpt-5.4":   {Input: 1.25, Output: 10.00, CachedInput: 0.125, Reasoning: 10.00},
	"cx/gpt-5.3":   {Input: 1.25, Output: 10.00, CachedInput: 0.125, Reasoning: 10.00},
}

func calculateUsageCost(provider store.Provider, model string, usage usageInfo) float64 {
	pricing := pricingFor(provider, model)
	if pricing.Input == 0 && pricing.Output == 0 && pricing.CachedInput == 0 && pricing.Reasoning == 0 {
		return 0
	}
	if pricing.CachedInput == 0 {
		pricing.CachedInput = pricing.Input
	}
	if pricing.Reasoning == 0 {
		pricing.Reasoning = pricing.Output
	}

	cached := minInt(usage.CachedTokens, usage.PromptTokens)
	nonCachedInput := maxInt(0, usage.PromptTokens-cached)
	cost := float64(nonCachedInput)*(pricing.Input/1_000_000) + float64(cached)*(pricing.CachedInput/1_000_000)
	cost += float64(usage.CompletionTokens) * (pricing.Output / 1_000_000)
	cost += float64(usage.ReasoningTokens) * (pricing.Reasoning / 1_000_000)
	return cost
}

func pricingFor(provider store.Provider, model string) usagePricing {
	if runtimeSettingsProvider != nil {
		if settings, err := runtimeSettingsProvider(); err == nil {
			if pricing, ok := pricingFromSettings(settings, provider, model); ok {
				return pricing
			}
		}
	}
	if envPricing, ok := pricingFromEnv(); ok {
		return envPricing
	}
	for _, modelKey := range pricingModelKeys(model) {
		if pricing, ok := defaultUsagePricing[modelKey]; ok {
			return pricing
		}
	}
	if provider.Type == store.ProviderCodex {
		return defaultUsagePricing["cx/gpt-5.5"]
	}
	return usagePricing{}
}

func pricingFromSettings(settings store.Settings, provider store.Provider, model string) (usagePricing, bool) {
	modelKeys := pricingModelKeys(model)
	for _, rule := range settings.ModelPrices {
		providerID := strings.TrimSpace(rule.ProviderID)
		if providerID != "" && providerID != provider.ID {
			continue
		}
		ruleModel := strings.ToLower(strings.TrimSpace(rule.Model))
		if ruleModel != "" && !containsString(modelKeys, ruleModel) {
			continue
		}
		return usagePricing{Input: rule.InputPer1M, Output: rule.OutputPer1M, CachedInput: rule.CachedInputPer1M, Reasoning: rule.ReasoningPer1M}, true
	}
	return usagePricing{}, false
}

func pricingModelKeys(model string) []string {
	model = strings.ToLower(strings.TrimSpace(model))
	keys := []string{}
	if model != "" {
		keys = append(keys, model)
	}
	if idx := strings.LastIndex(model, "/"); idx >= 0 && idx < len(model)-1 {
		keys = append(keys, model[idx+1:])
	}
	return keys
}

func containsString(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

func pricingFromEnv() (usagePricing, bool) {
	input, inputOK := envFloat("USAGE_PRICE_INPUT_PER_1M")
	output, outputOK := envFloat("USAGE_PRICE_OUTPUT_PER_1M")
	cached, cachedOK := envFloat("USAGE_PRICE_CACHED_INPUT_PER_1M")
	reasoning, reasoningOK := envFloat("USAGE_PRICE_REASONING_PER_1M")
	if !inputOK && !outputOK && !cachedOK && !reasoningOK {
		return usagePricing{}, false
	}
	return usagePricing{Input: input, Output: output, CachedInput: cached, Reasoning: reasoning}, true
}

func envFloat(key string) (float64, bool) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return 0, false
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || parsed < 0 {
		return 0, false
	}
	return parsed, true
}

func firstPositiveInt(m map[string]any, keys ...string) int {
	for _, key := range keys {
		if value := intFromAny(m[key]); value > 0 {
			return value
		}
	}
	return 0
}

func intFromAny(value any) int {
	switch v := value.(type) {
	case nil:
		return 0
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return int(i)
		}
		if f, err := v.Float64(); err == nil {
			return int(f)
		}
	case string:
		if i, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return i
		}
	}
	return 0
}

func boolFromAny(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		parsed, _ := strconv.ParseBool(strings.TrimSpace(v))
		return parsed
	default:
		return false
	}
}

func anySlice(value any) []any {
	if s, ok := value.([]any); ok {
		return s
	}
	return nil
}

func asStringLocal(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
