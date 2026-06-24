package gateway

import (
	"testing"

	"github.com/local/vivurouter-go/internal/store"
)

func makeProviders() []store.Provider {
	return []store.Provider{
		{ID: "openai", Type: store.ProviderOpenAICompatible, Enabled: true, Models: []string{"gpt-4.1"}},
		{ID: "openai-2", Type: store.ProviderOpenAICompatible, Enabled: true, Models: []string{"gpt-4.1"}},
		{ID: "openai-off", Type: store.ProviderOpenAICompatible, Enabled: false, Models: []string{"gpt-4.1"}},
		{ID: "codex", Type: store.ProviderCodex, Enabled: true, Models: []string{"gpt-5-codex"}},
		{ID: "codex-2", Type: store.ProviderCodex, Enabled: true, Models: []string{"gpt-5-codex"}},
	}
}

func makeProvidersWithNamespacedCodexModel() []store.Provider {
	providers := makeProviders()
	providers[3].Models = []string{"cx/gpt-5.5"}
	return providers
}

func defaultSettings() store.Settings {
	return store.Settings{DefaultProvider: "openai", DefaultCodexID: "codex"}
}

func TestResolveCandidatesExplicitProviderWithFallback(t *testing.T) {
	cands := resolveCandidates("openai/gpt-4.1", defaultSettings(), makeProviders())
	if len(cands) != 2 {
		t.Fatalf("len = %d, want 2", len(cands))
	}
	if cands[0].Provider.ID != "openai" {
		t.Fatalf("primary = %s, want openai", cands[0].Provider.ID)
	}
	if cands[1].Provider.ID != "openai-2" {
		t.Fatalf("fallback = %s, want openai-2", cands[1].Provider.ID)
	}
	for _, c := range cands {
		if c.Model != "gpt-4.1" {
			t.Fatalf("model = %s, want gpt-4.1", c.Model)
		}
	}
}

func TestResolveCandidatesSkipsDisabled(t *testing.T) {
	cands := resolveCandidates("openai/gpt-4.1", defaultSettings(), makeProviders())
	for _, c := range cands {
		if c.Provider.ID == "openai-off" {
			t.Fatal("disabled provider must not appear in candidates")
		}
	}
}

func TestResolveCandidatesCodex(t *testing.T) {
	cands := resolveCandidates("codex/gpt-5-codex", defaultSettings(), makeProviders())
	if len(cands) != 2 {
		t.Fatalf("len = %d, want 2", len(cands))
	}
	if !cands[0].IsCodex || !cands[1].IsCodex {
		t.Fatal("codex candidates must be flagged IsCodex")
	}
	if cands[0].Provider.ID != "codex" || cands[1].Provider.ID != "codex-2" {
		t.Fatalf("codex order = %s,%s want codex,codex-2", cands[0].Provider.ID, cands[1].Provider.ID)
	}
}

func TestResolveCandidatesBareModelUsesDefault(t *testing.T) {
	cands := resolveCandidates("gpt-4.1", defaultSettings(), makeProviders())
	if len(cands) == 0 {
		t.Fatal("bare model should resolve via default provider")
	}
	if cands[0].Provider.ID != "openai" {
		t.Fatalf("primary = %s, want openai", cands[0].Provider.ID)
	}
}

func TestResolveCandidatesNamespacedModelID(t *testing.T) {
	cands := resolveCandidates("cx/gpt-5.5", defaultSettings(), makeProvidersWithNamespacedCodexModel())
	if len(cands) == 0 {
		t.Fatal("expected namespaced model ID to resolve by exact provider model match")
	}
	if cands[0].Provider.ID != "codex" || cands[0].Model != "cx/gpt-5.5" || !cands[0].IsCodex {
		t.Fatalf("resolved = %+v, want codex cx/gpt-5.5", cands[0])
	}
}

func TestResolveCandidatesUnknownProvider(t *testing.T) {
	cands := resolveCandidates("nope/model", defaultSettings(), makeProviders())
	if len(cands) != 0 {
		t.Fatalf("len = %d, want 0 for unknown explicit provider", len(cands))
	}
}

func TestResolveModelBackwardCompatible(t *testing.T) {
	resolved, ok := resolveModel("openai/gpt-4.1", defaultSettings(), makeProviders())
	if !ok {
		t.Fatal("expected resolution")
	}
	if resolved.Provider.ID != "openai" || resolved.Model != "gpt-4.1" {
		t.Fatalf("resolved = %+v", resolved)
	}
}

func TestResolveComboCandidatesFallbackOrder(t *testing.T) {
	settings := defaultSettings()
	settings.Combos = []store.Combo{{Name: "fast", Models: []string{"openai/gpt-4.1", "codex/gpt-5-codex"}, Strategy: "fallback", StickyLimit: 1, Enabled: true}}
	combo, ok := findCombo("fast", settings)
	if !ok {
		t.Fatal("expected combo")
	}
	cands := resolveComboCandidates(combo, settings, makeProviders())
	if len(cands) < 3 {
		t.Fatalf("len = %d, want >= 3", len(cands))
	}
	if cands[0].Provider.ID != "openai" || cands[0].IsCodex {
		t.Fatalf("first candidate = %+v, want openai", cands[0])
	}
	foundCodex := false
	for _, cand := range cands {
		if cand.IsCodex {
			foundCodex = true
		}
	}
	if !foundCodex {
		t.Fatalf("expected at least one codex candidate: %+v", cands)
	}
}

func TestComboRoundRobinStickyLimit(t *testing.T) {
	tracker := &comboRotationTracker{byName: map[string]comboRotationEntry{}}
	combo := store.Combo{Name: "rr", Models: []string{"a", "b", "c"}, Strategy: "round-robin", StickyLimit: 2, Enabled: true}
	first := tracker.rotate(combo.Name, combo.Models, combo.StickyLimit)
	second := tracker.rotate(combo.Name, combo.Models, combo.StickyLimit)
	third := tracker.rotate(combo.Name, combo.Models, combo.StickyLimit)
	if first[0] != "a" || second[0] != "a" || third[0] != "b" {
		t.Fatalf("round robin starts = %s,%s,%s want a,a,b", first[0], second[0], third[0])
	}
}

func TestCodexCandidatesFallbackPath(t *testing.T) {
	settings := store.Settings{DefaultProvider: "codex", DefaultCodexID: "codex"}
	cands := codexCandidates("codex", settings, makeProviders())
	if len(cands) == 0 {
		t.Fatal("expected at least one codex candidate")
	}
	if cands[0].Provider.ID != "codex" {
		t.Fatalf("primary codex = %s, want codex", cands[0].Provider.ID)
	}
	if cands[0].Model != "gpt-5-codex" {
		t.Fatalf("codex model = %s, want gpt-5-codex", cands[0].Model)
	}
}
