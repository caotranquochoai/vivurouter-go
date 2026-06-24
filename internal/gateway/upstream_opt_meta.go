package gateway

import (
	"sort"
	"strings"

	"github.com/local/vivurouter-go/internal/store"
)

type upstreamOptimizationMeta struct {
	TokensSaved int
	Engine      string
	Parts       int
}

func collectUpstreamOptimizationMeta(body map[string]any) upstreamOptimizationMeta {
	meta := upstreamOptimizationMeta{}
	engines := map[string]bool{}
	walkOptimizationMeta(body, func(m map[string]any) {
		if optimized, _ := m["vivurouter_token_optimized"].(bool); !optimized {
			return
		}
		meta.Parts++
		meta.TokensSaved += intFromOptimizationMeta(m["vivurouter_estimated_tokens_saved"])
		if engine := strings.TrimSpace(debugString(m["vivurouter_optimizer_engine"])); engine != "" {
			engines[engine] = true
		}
	})
	meta.Engine = joinEngineLabels(engines)
	return meta
}

func applyUpstreamOptimizationMeta(log *store.RequestLog, meta upstreamOptimizationMeta) {
	if log == nil {
		return
	}
	log.UpstreamTokensSaved = meta.TokensSaved
	log.UpstreamOptimizerEngine = meta.Engine
	log.UpstreamOptimizedParts = meta.Parts
}

func optimizerEngineFromReason(reason string) string {
	reason = strings.ToLower(strings.TrimSpace(reason))
	if strings.HasPrefix(reason, "rtk") {
		return "rtk"
	}
	return "native"
}

func walkOptimizationMeta(value any, visit func(map[string]any)) {
	switch v := value.(type) {
	case map[string]any:
		visit(v)
		for _, child := range v {
			walkOptimizationMeta(child, visit)
		}
	case []any:
		for _, child := range v {
			walkOptimizationMeta(child, visit)
		}
	}
}

func intFromOptimizationMeta(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}

func joinEngineLabels(engines map[string]bool) string {
	if len(engines) == 0 {
		return ""
	}
	labels := make([]string, 0, len(engines))
	for engine := range engines {
		labels = append(labels, engine)
	}
	sort.Strings(labels)
	return strings.Join(labels, "+")
}
