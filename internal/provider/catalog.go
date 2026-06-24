package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/local/vivurouter-go/internal/store"
)

const defaultCodexModelsURL = "https://chatgpt.com/backend-api/codex/models?client_version=1.0.0"

func (e *Executors) FetchModels(ctx context.Context, provider store.Provider) ([]ModelInfo, error) {
	switch provider.Type {
	case store.ProviderMimoFree:
		return []ModelInfo{{ID: "mimo-auto", Name: "MiMo Auto"}}, nil
	case store.ProviderOpenCodeFree:
		return e.OpenCodeFree.FetchModels(ctx, provider)
	case store.ProviderAntigravity:
		return antigravityModelCatalog(), nil
	case store.ProviderCodex:
		return e.Codex.FetchModels(ctx, provider)
	default:
		return e.OpenAI.FetchModels(ctx, provider)
	}
}

// ModelInfo is a small normalized model catalog entry returned to the dashboard.
type ModelInfo struct {
	ID    string `json:"id"`
	Name  string `json:"name,omitempty"`
	Type  string `json:"type,omitempty"`
	RawID string `json:"raw_id,omitempty"`
}

// FetchModels queries an OpenAI-compatible provider's /models endpoint.
func (e *OpenAIExecutor) FetchModels(ctx context.Context, provider store.Provider) ([]ModelInfo, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(provider.BaseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("provider %s has no base URL", provider.ID)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	if token := providerBearerToken(provider); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client, err := clientForProvider(e.Client, provider)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("models endpoint returned HTTP %d", resp.StatusCode)
	}

	var payload any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	return parseOpenAIStyleModels(payload), nil
}

// FetchModels queries the Codex account live model catalog used by VivuRouter.
func (e *CodexExecutor) FetchModels(ctx context.Context, provider store.Provider) ([]ModelInfo, error) {
	token := providerBearerToken(provider)
	if token == "" {
		return nil, fmt.Errorf("provider %s has no Codex access token", provider.ID)
	}

	url := strings.TrimSpace(os.Getenv("CODEX_MODELS_URL"))
	if url == "" {
		url = defaultCodexModelsURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	client, err := clientForProvider(e.Client, provider)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Codex models endpoint returned HTTP %d", resp.StatusCode)
	}

	var payload any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	models := parseOpenAIStyleModels(payload)
	for i := range models {
		models[i].RawID = models[i].ID
		models[i].ID = NormalizeCodexPublicModelID(models[i].ID)
	}
	return dedupeModels(models), nil
}

// NormalizeCodexPublicModelID exposes Codex models with the same cx/ prefix used by VivuRouter.
func NormalizeCodexPublicModelID(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return ""
	}
	model = strings.TrimPrefix(model, "models/")
	if strings.Contains(model, "/") {
		return model
	}
	return "cx/" + model
}

func parseOpenAIStyleModels(payload any) []ModelInfo {
	items := modelItems(payload)
	models := make([]ModelInfo, 0, len(items))
	for _, item := range items {
		model := modelInfoFromAny(item)
		if model.ID == "" {
			continue
		}
		models = append(models, model)
	}
	return dedupeModels(models)
}

func modelItems(payload any) []any {
	switch value := payload.(type) {
	case []any:
		return value
	case map[string]any:
		for _, key := range []string{"data", "models", "results"} {
			if items, ok := value[key].([]any); ok {
				return items
			}
		}
		if modelsMap, ok := value["models"].(map[string]any); ok {
			items := make([]any, 0, len(modelsMap))
			for id, raw := range modelsMap {
				if m, ok := raw.(map[string]any); ok {
					m["id"] = id
					items = append(items, m)
				}
			}
			return items
		}
	}
	return nil
}

func modelInfoFromAny(item any) ModelInfo {
	if s, ok := item.(string); ok {
		id := strings.TrimSpace(s)
		return ModelInfo{ID: id, Name: id}
	}
	m, ok := item.(map[string]any)
	if !ok {
		return ModelInfo{}
	}
	id := firstModelString(m, "id", "slug", "model", "name")
	id = strings.TrimPrefix(strings.TrimSpace(id), "models/")
	name := firstModelString(m, "display_name", "displayName", "label", "name")
	if strings.TrimSpace(name) == "" {
		name = id
	}
	return ModelInfo{ID: id, Name: name, Type: firstModelString(m, "type", "kind")}
}

func dedupeModels(models []ModelInfo) []ModelInfo {
	seen := map[string]bool{}
	out := make([]ModelInfo, 0, len(models))
	for _, model := range models {
		model.ID = strings.TrimSpace(model.ID)
		if model.ID == "" || seen[model.ID] {
			continue
		}
		seen[model.ID] = true
		out = append(out, model)
	}
	return out
}

func firstModelString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(asString(m[key])); value != "" {
			return value
		}
	}
	return ""
}

func providerBearerToken(provider store.Provider) string {
	if strings.TrimSpace(provider.AccessToken) != "" {
		return strings.TrimSpace(provider.AccessToken)
	}
	if strings.TrimSpace(provider.APIKey) != "" {
		return strings.TrimSpace(provider.APIKey)
	}
	return ""
}
