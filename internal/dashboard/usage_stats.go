package dashboard

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/local/vivurouter-go/internal/store"
)

func providerUsagePeriodParam(r *http.Request) string {
	switch strings.ToLower(strings.TrimSpace(r.URL.Query().Get("usage_period"))) {
	case "day", "24h", "today":
		return "day"
	case "week", "7d":
		return "week"
	case "all", "all_time", "all-time":
		return "all"
	default:
		return "day"
	}
}

func providerUsagePeriodLabel(period string) string {
	switch period {
	case "week":
		return "7d"
	case "all":
		return "all time"
	default:
		return "24h"
	}
}

func filterProviderUsageLogs(logs []store.RequestLog, period string, now time.Time) []store.RequestLog {
	if period == "all" {
		return logs
	}
	window := 24 * time.Hour
	if period == "week" {
		window = 7 * 24 * time.Hour
	}
	cutoff := now.Add(-window)
	out := make([]store.RequestLog, 0, len(logs))
	for _, log := range logs {
		if log.Timestamp.IsZero() || !log.Timestamp.Before(cutoff) {
			out = append(out, log)
		}
	}
	return out
}

func summarizeProviderUsage(logs []store.RequestLog) (map[string]UsageCounter, map[string]UsageCounter) {
	byProvider := map[string]UsageCounter{}
	byModel := map[string]UsageCounter{}
	for _, log := range logs {
		counter := counterFromLog(log)
		addUsageCounterToMap(byProvider, log.ProviderID, counter)
		addUsageCounterToMap(byModel, log.ProviderID+"/"+log.Model, counter)
	}
	return byProvider, byModel
}

func modelUsageForProvider(providerID string, models []string, usageByModel map[string]UsageCounter) []providerModelUsage {
	out := make([]providerModelUsage, 0, len(models))
	for idx, model := range models {
		usage := usageByModel[strings.TrimSpace(providerID)+"/"+strings.TrimSpace(model)]
		out = append(out, providerModelUsage{Name: model, Tokens: usage.TotalTokens, Requests: usage.Requests, HasUsage: usage.TotalTokens > 0 || usage.Requests > 0, UsageRank: idx + 1})
	}
	return out
}

func summarizeUsage(logs []store.RequestLog) usageSummary {
	summary := usageSummary{
		ByProvider: map[string]UsageCounter{},
		ByModel:    map[string]UsageCounter{},
		ByEndpoint: map[string]UsageCounter{},
		ByAPIKey:   map[string]UsageCounter{},
	}
	for _, log := range logs {
		counter := counterFromLog(log)
		addUsageCounter(&summary.UsageCounter, counter)
		addUsageCounterToMap(summary.ByProvider, log.ProviderID, counter)
		addUsageCounterToMap(summary.ByModel, log.ProviderID+"/"+log.Model, counter)
		addUsageCounterToMap(summary.ByEndpoint, log.Endpoint, counter)

		apiKeyKey := log.APIKeyID
		if apiKeyKey == "" {
			if log.APIKeyPrefix != "" || log.APIKeySuffix != "" {
				apiKeyKey = log.APIKeyPrefix + "..." + log.APIKeySuffix
			} else {
				apiKeyKey = "no_key"
			}
		}
		addUsageCounterToMap(summary.ByAPIKey, apiKeyKey, counter)
	}
	return summary
}

func counterFromLog(log store.RequestLog) UsageCounter {
	estimated := 0
	if log.EstimatedTokens {
		estimated = 1
	}
	totalTokens := log.TotalTokens
	if totalTokens <= 0 {
		totalTokens = log.PromptTokens + log.CompletionTokens
	}
	return UsageCounter{
		Requests:           1,
		PromptTokens:       log.PromptTokens,
		CompletionTokens:   log.CompletionTokens,
		TotalTokens:        totalTokens,
		CachedTokens:       log.CachedTokens,
		ReasoningTokens:    log.ReasoningTokens,
		UpstreamSaved:      log.UpstreamTokensSaved,
		DebugSaved:         log.EstimatedTokensSaved,
		OptimizeDurationMS: log.OptimizeDurationMS,
		ProviderDurationMS: log.ProviderDurationMS,
		DebugLogDurationMS: log.DebugLogDurationMS,
		Estimated:          estimated,
		CostUSD:            log.CostUSD,
	}
}

func addUsageCounterToMap(target map[string]UsageCounter, key string, value UsageCounter) {
	key = strings.TrimSpace(key)
	if key == "" || key == "/" {
		key = "unknown"
	}
	current := target[key]
	addUsageCounter(&current, value)
	target[key] = current
}

func addUsageCounter(target *UsageCounter, value UsageCounter) {
	target.Requests += value.Requests
	target.PromptTokens += value.PromptTokens
	target.CompletionTokens += value.CompletionTokens
	target.TotalTokens += value.TotalTokens
	target.CachedTokens += value.CachedTokens
	target.ReasoningTokens += value.ReasoningTokens
	target.UpstreamSaved += value.UpstreamSaved
	target.DebugSaved += value.DebugSaved
	target.OptimizeDurationMS += value.OptimizeDurationMS
	target.ProviderDurationMS += value.ProviderDurationMS
	target.DebugLogDurationMS += value.DebugLogDurationMS
	target.Estimated += value.Estimated
	target.CostUSD += value.CostUSD
}

func filterLogsForRange(logs []store.RequestLog, rangeKey string, now time.Time) []store.RequestLog {
	rangeKey = normalizeUsageRange(rangeKey)
	now = now.UTC()

	var start time.Time
	switch rangeKey {
	case "today":
		start = now.Truncate(24 * time.Hour)
	case "7d":
		start = now.Truncate(24*time.Hour).AddDate(0, 0, -6)
	case "30d":
		start = now.Truncate(24*time.Hour).AddDate(0, 0, -29)
	default: // 24h
		start = now.Add(-23 * time.Hour).Truncate(time.Hour)
	}

	out := make([]store.RequestLog, 0)
	for _, log := range logs {
		if !log.Timestamp.UTC().Before(start) {
			out = append(out, log)
		}
	}
	return out
}

type usageTableAggregate struct {
	Requests         int
	LastUsed         time.Time
	InputCost        float64
	OutputCost       float64
	TotalCost        float64
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

func buildUsageTableData(logs []store.RequestLog, settings store.Settings, providers []store.Provider, now time.Time) usageTableData {
	providerByID := map[string]store.Provider{}
	for _, p := range providers {
		providerByID[p.ID] = p
	}

	byModel := map[string]map[string]*usageTableAggregate{}
	byAccount := map[string]map[string]*usageTableAggregate{}
	byAPIKey := map[string]map[string]*usageTableAggregate{}
	byEndpoint := map[string]map[string]*usageTableAggregate{}

	getAggregate := func(target map[string]map[string]*usageTableAggregate, parent string, child string) *usageTableAggregate {
		if target[parent] == nil {
			target[parent] = map[string]*usageTableAggregate{}
		}
		if target[parent][child] == nil {
			target[parent][child] = &usageTableAggregate{}
		}
		return target[parent][child]
	}

	addLog := func(agg *usageTableAggregate, log store.RequestLog, inputCost float64, outputCost float64) {
		agg.Requests++
		if log.Timestamp.After(agg.LastUsed) {
			agg.LastUsed = log.Timestamp
		}
		agg.InputCost += inputCost
		agg.OutputCost += outputCost
		agg.TotalCost += log.CostUSD
		agg.PromptTokens += log.PromptTokens
		agg.CompletionTokens += log.CompletionTokens
		totalTokens := log.TotalTokens
		if totalTokens <= 0 {
			totalTokens = log.PromptTokens + log.CompletionTokens
		}
		agg.TotalTokens += totalTokens
	}

	for _, log := range logs {
		inputCost, outputCost := estimateLogInputOutputCost(log, settings, providerByID[log.ProviderID])

		modelName := strings.TrimSpace(log.Model)
		if modelName == "" {
			modelName = "unknown"
		}
		providerName := strings.TrimSpace(log.ProviderID)
		if providerName == "" {
			providerName = "—"
		}
		accountName := providerName
		apiKeyName := usageAPIKeyLabel(log)
		endpointName := strings.TrimSpace(log.Endpoint)
		if endpointName == "" {
			endpointName = "unknown"
		}

		addLog(getAggregate(byModel, modelName, providerName), log, inputCost, outputCost)
		addLog(getAggregate(byAccount, accountName, modelName), log, inputCost, outputCost)
		addLog(getAggregate(byAPIKey, apiKeyName, modelName), log, inputCost, outputCost)
		addLog(getAggregate(byEndpoint, endpointName, modelName), log, inputCost, outputCost)
	}

	return usageTableData{
		ByModel:    buildUsageTableRows(byModel, now, "model"),
		ByAccount:  buildUsageTableRows(byAccount, now, "account"),
		ByAPIKey:   buildUsageTableRows(byAPIKey, now, "api_key"),
		ByEndpoint: buildUsageTableRows(byEndpoint, now, "endpoint"),
	}
}

func buildUsageTableRows(groups map[string]map[string]*usageTableAggregate, now time.Time, mode string) []usageTableRow {
	rows := make([]usageTableRow, 0, len(groups))
	for parentName, childrenMap := range groups {
		parent := usageTableRow{Name: parentName, Provider: "—"}
		children := make([]usageTableRow, 0, len(childrenMap))
		var parentLastUsed time.Time
		for childName, agg := range childrenMap {
			child := usageTableRow{
				Name:             childName,
				Requests:         agg.Requests,
				InputCost:        agg.InputCost,
				OutputCost:       agg.OutputCost,
				TotalCost:        agg.TotalCost,
				PromptTokens:     agg.PromptTokens,
				CompletionTokens: agg.CompletionTokens,
				TotalTokens:      agg.TotalTokens,
			}
			switch mode {
			case "model":
				child.Provider = childName
			case "account":
				child.Provider = parentName
			default:
				child.Provider = "—"
			}
			if !agg.LastUsed.IsZero() {
				child.LastUsedSecsAgo = int(maxDuration(0, now.Sub(agg.LastUsed)).Seconds())
				child.LastUsedLabel = usageRelativeTimeLabel(now, agg.LastUsed)
			}
			children = append(children, child)
			if agg.LastUsed.After(parentLastUsed) {
				parentLastUsed = agg.LastUsed
			}
			parent.Requests += agg.Requests
			parent.InputCost += agg.InputCost
			parent.OutputCost += agg.OutputCost
			parent.TotalCost += agg.TotalCost
			parent.PromptTokens += agg.PromptTokens
			parent.CompletionTokens += agg.CompletionTokens
			parent.TotalTokens += agg.TotalTokens
		}
		sort.Slice(children, func(i, j int) bool {
			if children[i].TotalCost == children[j].TotalCost {
				if children[i].Requests == children[j].Requests {
					return children[i].Name < children[j].Name
				}
				return children[i].Requests > children[j].Requests
			}
			return children[i].TotalCost > children[j].TotalCost
		})
		if !parentLastUsed.IsZero() {
			parent.LastUsedSecsAgo = int(maxDuration(0, now.Sub(parentLastUsed)).Seconds())
			parent.LastUsedLabel = usageRelativeTimeLabel(now, parentLastUsed)
		}
		parent.Children = children
		rows = append(rows, parent)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].TotalCost == rows[j].TotalCost {
			if rows[i].Requests == rows[j].Requests {
				return rows[i].Name < rows[j].Name
			}
			return rows[i].Requests > rows[j].Requests
		}
		return rows[i].TotalCost > rows[j].TotalCost
	})
	return rows
}

func usageAPIKeyLabel(log store.RequestLog) string {
	if key := strings.TrimSpace(log.APIKeyID); key != "" {
		return key
	}
	masked := strings.TrimSpace(log.APIKeyMasked)
	if masked != "" {
		return masked
	}
	if strings.TrimSpace(log.APIKeyPrefix) != "" || strings.TrimSpace(log.APIKeySuffix) != "" {
		return strings.TrimSpace(log.APIKeyPrefix) + "..." + strings.TrimSpace(log.APIKeySuffix)
	}
	return "anonymous"
}

func usageRelativeTimeLabel(now time.Time, t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	d := maxDuration(0, now.Sub(t))
	if d < time.Minute {
		return "Just now"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}

func estimateLogInputOutputCost(log store.RequestLog, settings store.Settings, providerCfg store.Provider) (float64, float64) {
	if log.CostUSD <= 0 {
		return 0, 0
	}
	inputPrice, cachedPrice, outputPrice, reasoningPrice := usagePricingForLog(settings, providerCfg, log.Model)
	if cachedPrice <= 0 {
		cachedPrice = inputPrice
	}
	if reasoningPrice <= 0 {
		reasoningPrice = outputPrice
	}
	cached := log.CachedTokens
	if cached > log.PromptTokens {
		cached = log.PromptTokens
	}
	nonCachedInput := maxInt(0, log.PromptTokens-cached)
	inputWeight := float64(nonCachedInput)*(inputPrice/1_000_000) + float64(cached)*(cachedPrice/1_000_000)
	outputWeight := float64(log.CompletionTokens)*(outputPrice/1_000_000) + float64(log.ReasoningTokens)*(reasoningPrice/1_000_000)
	totalWeight := inputWeight + outputWeight
	if totalWeight <= 0 {
		return log.CostUSD, 0
	}
	inputCost := log.CostUSD * (inputWeight / totalWeight)
	outputCost := log.CostUSD - inputCost
	if outputCost < 0 {
		outputCost = 0
	}
	return inputCost, outputCost
}

func usagePricingForLog(settings store.Settings, providerCfg store.Provider, model string) (float64, float64, float64, float64) {
	providerID := strings.TrimSpace(providerCfg.ID)
	model = strings.TrimSpace(model)
	for _, rule := range settings.ModelPrices {
		if strings.TrimSpace(rule.ProviderID) == providerID && strings.TrimSpace(rule.Model) == model {
			return rule.InputPer1M, rule.CachedInputPer1M, rule.OutputPer1M, rule.ReasoningPer1M
		}
	}
	return 0, 0, 0, 0
}

func buildCostNote(logs []store.RequestLog, settings store.Settings) string {
	if len(logs) == 0 {
		return "chưa có request"
	}
	withTokens := 0
	unpriced := 0
	for _, log := range logs {
		if log.TotalTokens <= 0 {
			continue
		}
		withTokens++
		if log.CostUSD == 0 && !hasPricingForLog(log, settings) {
			unpriced++
		}
	}
	if withTokens == 0 {
		return "chưa có token để tính cost"
	}
	if unpriced > 0 {
		return strconv.Itoa(unpriced) + "/" + strconv.Itoa(withTokens) + " request có token nhưng chưa có pricing rule"
	}
	return "custom pricing aware"
}

func hasPricingForLog(log store.RequestLog, settings store.Settings) bool {
	model := strings.ToLower(strings.TrimSpace(log.Model))
	providerID := strings.TrimSpace(log.ProviderID)
	for _, rule := range settings.ModelPrices {
		if strings.TrimSpace(rule.ProviderID) != "" && strings.TrimSpace(rule.ProviderID) != providerID {
			continue
		}
		ruleModel := strings.ToLower(strings.TrimSpace(rule.Model))
		if ruleModel == "" || ruleModel == model || strings.TrimPrefix(model, providerID+"/") == ruleModel {
			return true
		}
	}
	return false
}
