package dashboard

import (
	"testing"

	"github.com/local/vivurouter-go/internal/store"
)

func TestChatStudioTranslationsIncludeDisableFallback(t *testing.T) {
	for _, lang := range []string{"vi", "en"} {
		bundle := translationBundle(lang)
		if bundle["chat_studio.disable_fallback"] == "" || bundle["chat_studio.disable_fallback_hint"] == "" {
			t.Fatalf("missing disable-fallback translations for %s", lang)
		}
	}
}

func TestChatStudioModelsOnlyExposeEnabledConfiguredModels(t *testing.T) {
	providers := []store.Provider{
		{ID: "openai", Name: "OpenAI", Enabled: true, Models: []string{"gpt-test"}, APIKey: "secret"},
		{ID: "disabled", Enabled: false, Models: []string{"hidden"}},
	}
	settings := store.Settings{
		Combos:        []store.Combo{{Name: "fallback", Enabled: true}},
		PromptRouters: []store.PromptRouter{{Name: "router", Enabled: true}},
		Fusions:       []store.Fusion{{Name: "fusion", Enabled: true}},
	}
	models := chatStudioModels(providers, settings)
	if len(models) != 4 {
		t.Fatalf("model count = %d, want 4: %+v", len(models), models)
	}
	for _, model := range models {
		if model.Value == "hidden" || model.Value == "secret" {
			t.Fatalf("sensitive/disabled value exposed: %+v", model)
		}
	}
}
