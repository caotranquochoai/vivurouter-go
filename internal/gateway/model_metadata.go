package gateway

import (
	"strings"

	"github.com/local/vivurouter-go/internal/store"
)

const defaultContextLength = 128000

var knownContextLengths = map[string]int{
	"gpt-4o-mini":      128000,
	"gpt-4o":           128000,
	"gpt-4.1":          1047576,
	"gpt-4.1-mini":     1047576,
	"gpt-4.1-nano":     1047576,
	"gpt-5":            400000,
	"gpt-5-mini":       400000,
	"gpt-5-nano":       400000,
	"gpt-5-codex":      400000,
	"gpt-5.5":          400000,
	"gpt-5.4":          400000,
	"gpt-5.3-codex":    400000,
	"cx/gpt-5.5":       400000,
	"cx/gpt-5.4":       400000,
	"cx/gpt-5.3-codex": 400000,
}

func modelMetadata(providerID string, model string, settings store.Settings) map[string]any {
	contextLength := contextLengthForModel(providerID, model, settings)
	return map[string]any{
		"id":                 model,
		"object":             "model",
		"owned_by":           providerID,
		"context_length":     contextLength,
		"max_context_length": contextLength,
		"max_input_tokens":   contextLength,
		"max_tokens":         contextLength,
	}
}

func contextLengthForModel(providerID string, model string, settings store.Settings) int {
	model = strings.TrimSpace(model)
	if contextLength := contextLengthFromSettings(providerID, model, settings); contextLength > 0 {
		return contextLength
	}
	candidates := []string{model}
	if strings.Contains(model, "/") {
		parts := strings.Split(model, "/")
		candidates = append(candidates, parts[len(parts)-1])
	}
	if strings.TrimSpace(providerID) != "" {
		candidates = append(candidates, strings.TrimPrefix(model, providerID+"/"))
	}
	for _, candidate := range candidates {
		candidate = strings.ToLower(strings.TrimSpace(candidate))
		if value, ok := knownContextLengths[candidate]; ok {
			return value
		}
	}
	return defaultContextLength
}

func contextLengthFromSettings(providerID string, model string, settings store.Settings) int {
	providerID = strings.TrimSpace(providerID)
	modelKeys := metadataModelKeys(model)
	for _, rule := range settings.ModelPrices {
		if rule.ContextLength <= 0 {
			continue
		}
		ruleProviderID := strings.TrimSpace(rule.ProviderID)
		if ruleProviderID != "" && ruleProviderID != providerID {
			continue
		}
		ruleModel := strings.ToLower(strings.TrimSpace(rule.Model))
		if ruleModel == "" || containsMetadataKey(modelKeys, ruleModel) {
			return rule.ContextLength
		}
	}
	return 0
}

func metadataModelKeys(model string) []string {
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

func containsMetadataKey(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}
