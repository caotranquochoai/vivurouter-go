package gateway

import (
	"testing"

	"github.com/local/vivurouter-go/internal/store"
)

func TestModelMetadataIncludesContextLength(t *testing.T) {
	meta := modelMetadata("codex", "codex/cx/gpt-5.5", store.Settings{})
	for _, key := range []string{"context_length", "max_context_length", "max_input_tokens", "max_tokens"} {
		if got := meta[key]; got != 400000 {
			t.Fatalf("%s = %v, want 400000", key, got)
		}
	}
}

func TestContextLengthForUnknownModelUsesSafeDefault(t *testing.T) {
	if got := contextLengthForModel("openai", "openai/unknown-model", store.Settings{}); got != defaultContextLength {
		t.Fatalf("context length = %d, want %d", got, defaultContextLength)
	}
	if got, ok := knownContextLengthForModel("openai", "openai/unknown-model", store.Settings{}); ok || got != 0 {
		t.Fatalf("known context length = %d, %t; want 0, false", got, ok)
	}
}

func TestKnownContextLengthForModelRecognizesCatalogAndOverride(t *testing.T) {
	if got, ok := knownContextLengthForModel("openai", "openai/gpt-4o", store.Settings{}); !ok || got != 128000 {
		t.Fatalf("catalog context length = %d, %t; want 128000, true", got, ok)
	}
	settings := store.Settings{ModelPrices: []store.ModelPriceRule{{ProviderID: "custom", Model: "unknown-model", ContextLength: 262144}}}
	if got, ok := knownContextLengthForModel("custom", "custom/unknown-model", settings); !ok || got != 262144 {
		t.Fatalf("override context length = %d, %t; want 262144, true", got, ok)
	}
}

func TestContextLengthForModelUsesPricingOverride(t *testing.T) {
	settings := store.Settings{ModelPrices: []store.ModelPriceRule{{ProviderID: "openai", Model: "custom-model", ContextLength: 262144}}}
	if got := contextLengthForModel("openai", "openai/custom-model", settings); got != 262144 {
		t.Fatalf("context length = %d, want 262144", got)
	}
}
