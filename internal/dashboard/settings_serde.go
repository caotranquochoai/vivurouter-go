package dashboard

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/local/vivurouter-go/internal/store"
)

func decodeGuardrailsImport(data []byte) ([]store.Guardrail, error) {
	data = []byte(strings.TrimSpace(string(data)))
	if len(data) == 0 {
		return nil, fmt.Errorf("missing JSON payload")
	}
	var wrapped struct {
		Guardrails []store.Guardrail `json:"guardrails"`
	}
	if err := json.Unmarshal(data, &wrapped); err == nil && wrapped.Guardrails != nil {
		return wrapped.Guardrails, nil
	}
	var list []store.Guardrail
	if err := json.Unmarshal(data, &list); err == nil {
		return list, nil
	}
	var single store.Guardrail
	if err := json.Unmarshal(data, &single); err != nil {
		return nil, fmt.Errorf("invalid guardrail JSON: %w", err)
	}
	return []store.Guardrail{single}, nil
}

func decodePromptRoutersImport(data []byte) ([]store.PromptRouter, error) {
	data = []byte(strings.TrimSpace(string(data)))
	if len(data) == 0 {
		return nil, fmt.Errorf("missing JSON payload")
	}
	var wrapped struct {
		PromptRouters []store.PromptRouter `json:"prompt_routers"`
	}
	if err := json.Unmarshal(data, &wrapped); err == nil && wrapped.PromptRouters != nil {
		return wrapped.PromptRouters, nil
	}
	var list []store.PromptRouter
	if err := json.Unmarshal(data, &list); err == nil {
		return list, nil
	}
	var single store.PromptRouter
	if err := json.Unmarshal(data, &single); err != nil {
		return nil, fmt.Errorf("invalid prompt router JSON: %w", err)
	}
	return []store.PromptRouter{single}, nil
}

func safeDownloadName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
			continue
		}
		if r == ' ' || r == '/' || r == '\\' || r == ':' {
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-._")
	if out == "" {
		return "router"
	}
	if len(out) > 80 {
		out = strings.Trim(out[:80], "-._")
	}
	if out == "" {
		return "router"
	}
	return out
}

func parseAPIKeyPolicies(raw string) []store.APIKeyPolicy {
	out := []store.APIKeyPolicy{}
	for _, line := range strings.Split(raw, "\n") {
		parts := strings.Split(line, "|")
		if len(parts) < 2 {
			continue
		}
		item := store.APIKeyPolicy{ID: strings.TrimSpace(parts[0]), Key: strings.TrimSpace(parts[1]), Enabled: true}
		if len(parts) > 6 {
			item.Enabled, _ = strconv.ParseBool(strings.TrimSpace(parts[6]))
		}
		if len(parts) > 2 {
			item.AllowedModels = splitModels(parts[2])
		}
		if len(parts) > 3 {
			item.MaxRequests = parseNonNegativeIntLimit(parts[3])
		}
		if len(parts) > 4 {
			item.MaxTokens = parseNonNegativeIntLimit(parts[4])
		}
		if len(parts) > 5 {
			item.MaxCostUSD = parseNonNegativeFloatLimit(parts[5])
		}
		if len(parts) > 7 {
			item.MaxRPM = parseNonNegativeIntLimit(parts[7])
		}
		if len(parts) > 8 {
			item.MaxConcurrent = parseNonNegativeIntLimit(parts[8])
		}
		out = append(out, item)
	}
	return store.NormalizeAPIKeyPolicies(out)
}

func parseModelPriceRules(raw string) []store.ModelPriceRule {
	out := []store.ModelPriceRule{}
	for _, line := range strings.Split(raw, "\n") {
		parts := strings.Split(line, "|")
		if len(parts) < 6 {
			continue
		}
		item := store.ModelPriceRule{ProviderID: strings.TrimSpace(parts[0]), Model: strings.TrimSpace(parts[1])}
		item.InputPer1M, _ = strconv.ParseFloat(strings.TrimSpace(parts[2]), 64)
		item.OutputPer1M, _ = strconv.ParseFloat(strings.TrimSpace(parts[3]), 64)
		item.CachedInputPer1M, _ = strconv.ParseFloat(strings.TrimSpace(parts[4]), 64)
		item.ReasoningPer1M, _ = strconv.ParseFloat(strings.TrimSpace(parts[5]), 64)
		if len(parts) > 6 {
			item.ContextLength, _ = strconv.Atoi(strings.TrimSpace(parts[6]))
		}
		if len(parts) > 7 {
			item.RPM, _ = strconv.Atoi(strings.TrimSpace(parts[7]))
		}
		if len(parts) > 8 {
			item.TPM, _ = strconv.Atoi(strings.TrimSpace(parts[8]))
		}
		if len(parts) > 9 {
			item.DailyRequests, _ = strconv.Atoi(strings.TrimSpace(parts[9]))
		}
		if len(parts) > 10 {
			item.DailyTokens, _ = strconv.Atoi(strings.TrimSpace(parts[10]))
		}
		out = append(out, item)
	}
	return store.NormalizeModelPriceRules(out)
}

func parseNonNegativeIntLimit(raw string) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

func parseNonNegativeFloatLimit(raw string) float64 {
	n, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

func formatAPIKeyPolicies(items []store.APIKeyPolicy) string {
	lines := []string{}
	for _, item := range items {
		lines = append(lines, strings.Join([]string{
			item.ID,
			item.Key,
			strings.Join(item.AllowedModels, ","),
			strconv.Itoa(item.MaxRequests),
			strconv.Itoa(item.MaxTokens),
			strconv.FormatFloat(item.MaxCostUSD, 'f', -1, 64),
			strconv.FormatBool(item.Enabled),
			strconv.Itoa(item.MaxRPM),
			strconv.Itoa(item.MaxConcurrent),
		}, "|"))
	}
	return strings.Join(lines, "\n")
}

func groupModelPriceRules(items []store.ModelPriceRule) []pricingRuleGroup {
	groups := []pricingRuleGroup{}
	index := map[string]int{}
	for _, item := range items {
		key := strings.Join([]string{
			strconv.FormatFloat(item.InputPer1M, 'f', -1, 64),
			strconv.FormatFloat(item.OutputPer1M, 'f', -1, 64),
			strconv.FormatFloat(item.CachedInputPer1M, 'f', -1, 64),
			strconv.FormatFloat(item.ReasoningPer1M, 'f', -1, 64),
			strconv.Itoa(item.ContextLength),
			strconv.Itoa(item.RPM),
			strconv.Itoa(item.TPM),
			strconv.Itoa(item.DailyRequests),
			strconv.Itoa(item.DailyTokens),
		}, "|")
		idx, ok := index[key]
		if !ok {
			idx = len(groups)
			index[key] = idx
			groups = append(groups, pricingRuleGroup{
				InputPer1M:       item.InputPer1M,
				OutputPer1M:      item.OutputPer1M,
				CachedInputPer1M: item.CachedInputPer1M,
				ReasoningPer1M:   item.ReasoningPer1M,
				ContextLength:    item.ContextLength,
				RPM:              item.RPM,
				TPM:              item.TPM,
				DailyRequests:    item.DailyRequests,
				DailyTokens:      item.DailyTokens,
			})
		}
		group := &groups[idx]
		if group.ProviderID == "" {
			group.ProviderID = item.ProviderID
		} else if !containsCSVValue(group.ProviderID, item.ProviderID) {
			group.ProviderID += ", " + item.ProviderID
		}
		if group.Model == "" {
			group.Model = item.Model
		} else if !containsCSVValue(group.Model, item.Model) {
			group.Model += ", " + item.Model
		}
		if group.PairsText != "" {
			group.PairsText += "\n"
		}
		group.PairsText += item.ProviderID + "|" + item.Model
		group.Count++
	}
	return groups
}

func containsCSVValue(csv string, value string) bool {
	for _, part := range strings.Split(csv, ",") {
		if strings.TrimSpace(part) == value {
			return true
		}
	}
	return false
}

func formatModelPriceRules(items []store.ModelPriceRule) string {
	lines := []string{}
	for _, item := range items {
		lines = append(lines, strings.Join([]string{
			item.ProviderID,
			item.Model,
			strconv.FormatFloat(item.InputPer1M, 'f', -1, 64),
			strconv.FormatFloat(item.OutputPer1M, 'f', -1, 64),
			strconv.FormatFloat(item.CachedInputPer1M, 'f', -1, 64),
			strconv.FormatFloat(item.ReasoningPer1M, 'f', -1, 64),
			strconv.Itoa(item.ContextLength),
			strconv.Itoa(item.RPM),
			strconv.Itoa(item.TPM),
			strconv.Itoa(item.DailyRequests),
			strconv.Itoa(item.DailyTokens),
		}, "|"))
	}
	return strings.Join(lines, "\n")
}

func formatPromptRouters(items []store.PromptRouter) string {
	raw, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return "[]"
	}
	return string(raw)
}

func parseGuardrails(raw string) []store.Guardrail {
	items := []store.Guardrail{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &items); err != nil {
		return nil
	}
	return store.NormalizeGuardrails(items)
}

func formatGuardrails(items []store.Guardrail) string {
	raw, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return "[]"
	}
	return string(raw)
}

func parseFusions(raw string) []store.Fusion {
	items := []store.Fusion{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &items); err != nil {
		return nil
	}
	return store.NormalizeFusions(items)
}

func formatFusions(items []store.Fusion) string {
	raw, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return "[]"
	}
	return string(raw)
}

func parseCombos(raw string) []store.Combo {
	out := []store.Combo{}
	for _, line := range strings.Split(raw, "\n") {
		parts := strings.Split(line, "|")
		if len(parts) < 2 {
			continue
		}
		combo := store.Combo{Name: strings.TrimSpace(parts[0]), Models: splitModels(parts[1]), Enabled: true}
		if len(parts) > 2 {
			combo.Strategy = strings.TrimSpace(parts[2])
		}
		if len(parts) > 3 {
			combo.StickyLimit, _ = strconv.Atoi(strings.TrimSpace(parts[3]))
		}
		if len(parts) > 4 {
			combo.ContextLength, _ = strconv.Atoi(strings.TrimSpace(parts[4]))
		}
		if len(parts) > 5 {
			combo.Enabled, _ = strconv.ParseBool(strings.TrimSpace(parts[5]))
		}
		if len(parts) > 6 {
			combo.Description = strings.TrimSpace(parts[6])
		}
		out = append(out, combo)
	}
	return store.NormalizeCombos(out)
}

func formatCombos(items []store.Combo) string {
	lines := []string{}
	for _, item := range items {
		lines = append(lines, strings.Join([]string{
			item.Name,
			strings.Join(item.Models, ","),
			item.Strategy,
			strconv.Itoa(item.StickyLimit),
			strconv.Itoa(item.ContextLength),
			strconv.FormatBool(item.Enabled),
			item.Description,
		}, "|"))
	}
	return strings.Join(lines, "\n")
}

func chatStudioModels(providers []store.Provider, settings store.Settings) []chatStudioModel {
	models := []chatStudioModel{}
	seen := map[string]bool{}
	add := func(value, label, group string) {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			return
		}
		seen[value] = true
		models = append(models, chatStudioModel{Value: value, Label: label, Group: group})
	}
	for _, provider := range providers {
		if !provider.Enabled {
			continue
		}
		name := strings.TrimSpace(provider.Name)
		if name == "" {
			name = provider.ID
		}
		for _, model := range provider.Models {
			model = strings.TrimSpace(model)
			if model != "" {
				add(provider.ID+"/"+model, model, name)
			}
		}
	}
	for _, combo := range settings.Combos {
		if combo.Enabled {
			add(combo.Name, combo.Name, "Combos")
		}
	}
	for _, router := range settings.PromptRouters {
		if router.Enabled {
			add(router.Name, router.Name, "Prompt Routers")
		}
	}
	for _, fusion := range settings.Fusions {
		if fusion.Enabled {
			add(fusion.Name, fusion.Name, "Fusions")
		}
	}
	for _, guardrail := range settings.Guardrails {
		if guardrail.Enabled {
			add(guardrail.Name, guardrail.Name, "Guardrails")
		}
	}
	return models
}

func comboModelOptions(providers []store.Provider) []comboModelOption {
	out := []comboModelOption{}
	seen := map[string]bool{}
	for _, provider := range providers {
		if !provider.Enabled {
			continue
		}
		providerLabel := strings.TrimSpace(provider.Name)
		if providerLabel == "" {
			providerLabel = provider.ID
		}
		for _, model := range provider.Models {
			model = strings.TrimSpace(model)
			if model == "" {
				continue
			}
			value := provider.ID + "/" + model
			if seen[value] {
				continue
			}
			seen[value] = true
			out = append(out, comboModelOption{ProviderID: provider.ID, Provider: providerLabel, Model: model, Value: value})
		}
	}
	return out
}
