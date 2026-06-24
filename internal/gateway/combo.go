package gateway

import (
	"strings"
	"sync"

	"github.com/local/vivurouter-go/internal/store"
)

type comboRotationEntry struct {
	Index               int
	ConsecutiveUseCount int
}

type comboRotationTracker struct {
	mu     sync.Mutex
	byName map[string]comboRotationEntry
}

var comboRotations = &comboRotationTracker{byName: map[string]comboRotationEntry{}}

func findCombo(model string, settings store.Settings) (store.Combo, bool) {
	model = strings.TrimSpace(model)
	if model == "" || strings.Contains(model, "/") {
		return store.Combo{}, false
	}
	for _, combo := range settings.Combos {
		if combo.Enabled && combo.Name == model && len(combo.Models) > 0 {
			return combo, true
		}
	}
	return store.Combo{}, false
}

func resolveComboCandidates(combo store.Combo, settings store.Settings, providers []store.Provider) []resolvedModel {
	models := orderedComboModels(combo)
	out := []resolvedModel{}
	seen := map[string]bool{}
	for _, model := range models {
		if model == combo.Name {
			continue
		}
		for _, cand := range resolveCandidates(model, settings, providers) {
			key := cand.Provider.ID + "\x00" + cand.Model
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, cand)
		}
	}
	return out
}

func orderedComboModels(combo store.Combo) []string {
	models := append([]string(nil), combo.Models...)
	if len(models) <= 1 || combo.Strategy != "round-robin" {
		return models
	}
	return comboRotations.rotate(combo.Name, models, combo.StickyLimit)
}

func (t *comboRotationTracker) rotate(name string, models []string, stickyLimit int) []string {
	if stickyLimit <= 0 {
		stickyLimit = 1
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	state := t.byName[name]
	idx := state.Index % len(models)
	rotated := append([]string(nil), models[idx:]...)
	rotated = append(rotated, models[:idx]...)
	state.ConsecutiveUseCount++
	if state.ConsecutiveUseCount >= stickyLimit {
		state.Index = (idx + 1) % len(models)
		state.ConsecutiveUseCount = 0
	} else {
		state.Index = idx
	}
	t.byName[name] = state
	return rotated
}

func comboContextLength(combo store.Combo, settings store.Settings) int {
	if combo.ContextLength > 0 {
		return combo.ContextLength
	}
	min := 0
	for _, model := range combo.Models {
		length := contextLengthForModel("", model, settings)
		if length > 0 && (min == 0 || length < min) {
			min = length
		}
	}
	if min > 0 {
		return min
	}
	return defaultContextLength
}

func comboMetadata(combo store.Combo, settings store.Settings) map[string]any {
	contextLength := comboContextLength(combo, settings)
	return map[string]any{
		"id":                 combo.Name,
		"object":             "model",
		"owned_by":           "combo",
		"type":               "combo",
		"strategy":           combo.Strategy,
		"models":             combo.Models,
		"context_length":     contextLength,
		"max_context_length": contextLength,
		"max_input_tokens":   contextLength,
		"max_tokens":         contextLength,
	}
}

func promptRouterMetadata(router store.PromptRouter) map[string]any {
	roles := []string{}
	for _, route := range router.Routes {
		roles = append(roles, route.Role)
	}
	return map[string]any{
		"id":               router.Name,
		"object":           "model",
		"owned_by":         "prompt-router",
		"type":             "prompt_router",
		"classifier_model": router.ClassifierModel,
		"fallback_target":  router.FallbackTarget,
		"roles":            roles,
	}
}
