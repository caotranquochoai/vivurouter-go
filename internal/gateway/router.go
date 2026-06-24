package gateway

import (
	"strings"

	"github.com/local/vivurouter-go/internal/store"
)

type resolvedModel struct {
	Provider store.Provider
	Model    string
	IsCodex  bool
}

func resolveModel(model string, settings store.Settings, providers []store.Provider) (resolvedModel, bool) {
	candidates := resolveCandidates(model, settings, providers)
	if len(candidates) == 0 {
		return resolvedModel{}, false
	}
	return candidates[0], true
}

// resolveCandidates returns the ordered providers able to serve a model. The
// first entry preserves the original single-provider behavior; any remaining
// entries are same-type fallbacks used when the primary is unavailable.
func resolveCandidates(model string, settings store.Settings, providers []store.Provider) []resolvedModel {
	model = strings.TrimSpace(model)
	if model == "" {
		return nil
	}

	// Some upstream model IDs are already namespace-like, for example
	// "cx/gpt-5.5". Prefer exact configured model matches before treating the
	// first slash segment as a local provider prefix.
	if primary := findExactModelMatch(model, providers); primary != nil {
		out := []resolvedModel{*primary}
		out = append(out, fallbackCandidates(*primary, model, providers)...)
		return out
	}

	providerID, modelName := splitModel(model)
	explicitProvider := providerID != ""
	if providerID == "" {
		providerID = settings.DefaultProvider
	}

	var primary *resolvedModel
	for _, candidate := range providers {
		if !candidate.Enabled {
			continue
		}
		if candidate.ID == providerID || candidate.Type == providerID {
			name := modelName
			if name == "" && len(candidate.Models) > 0 {
				name = candidate.Models[0]
			}
			if name == "" {
				name = model
			}
			match := resolvedModel{Provider: candidate, Model: name, IsCodex: candidate.Type == store.ProviderCodex}
			primary = &match
			break
		}
	}

	// Fallback by model prefix when no provider prefix was supplied.
	if primary == nil && !explicitProvider {
		for _, candidate := range providers {
			if candidate.Enabled && candidate.Type == store.ProviderOpenAICompatible {
				name := modelName
				if name == "" {
					name = model
				}
				match := resolvedModel{Provider: candidate, Model: name}
				primary = &match
				break
			}
		}
	}

	if primary == nil {
		return nil
	}

	out := []resolvedModel{*primary}
	out = append(out, fallbackCandidates(*primary, modelName, providers)...)
	return out
}

func findExactModelMatch(model string, providers []store.Provider) *resolvedModel {
	for _, candidate := range providers {
		if !candidate.Enabled {
			continue
		}
		for _, configured := range candidate.Models {
			if configured == model {
				match := resolvedModel{Provider: candidate, Model: configured, IsCodex: candidate.Type == store.ProviderCodex}
				return &match
			}
		}
	}
	return nil
}

// fallbackCandidates lists other enabled providers of the same type that can
// serve the requested model name, excluding the primary.
func fallbackCandidates(primary resolvedModel, requestedModel string, providers []store.Provider) []resolvedModel {
	out := []resolvedModel{}
	for _, candidate := range providers {
		if !candidate.Enabled || candidate.ID == primary.Provider.ID {
			continue
		}
		if candidate.Type != primary.Provider.Type {
			continue
		}
		name := pickFallbackModel(candidate, requestedModel)
		if name == "" {
			continue
		}
		out = append(out, resolvedModel{Provider: candidate, Model: name, IsCodex: candidate.Type == store.ProviderCodex})
	}
	return out
}

// pickFallbackModel chooses which model a fallback provider should run. If an
// explicit model name was requested it is reused only when the provider lists
// it (or lists nothing); otherwise the provider's first model is used.
func pickFallbackModel(candidate store.Provider, requestedModel string) string {
	if requestedModel == "" {
		if len(candidate.Models) > 0 {
			return candidate.Models[0]
		}
		return ""
	}
	if len(candidate.Models) == 0 {
		return requestedModel
	}
	for _, m := range candidate.Models {
		if m == requestedModel {
			return requestedModel
		}
	}
	return candidate.Models[0]
}

func splitModel(model string) (provider string, name string) {
	model = strings.TrimSpace(model)
	if model == "" {
		return "", ""
	}
	idx := strings.Index(model, "/")
	if idx < 0 {
		return "", model
	}
	return model[:idx], model[idx+1:]
}
