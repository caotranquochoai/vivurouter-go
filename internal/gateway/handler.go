package gateway

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/local/vivurouter-go/internal/auth"
	"github.com/local/vivurouter-go/internal/observe"
	"github.com/local/vivurouter-go/internal/provider"
	"github.com/local/vivurouter-go/internal/store"
	"github.com/local/vivurouter-go/internal/translator"
)

// Handler serves OpenAI-compatible and Codex gateway endpoints.
type Handler struct {
	store     store.Store
	executors *provider.Executors
	observe   *observe.State
}

func NewHandler(st store.Store, executors *provider.Executors, obs *observe.State) *Handler {
	return &Handler{store: st, executors: executors, observe: obs}
}

func (h *Handler) Models(w http.ResponseWriter, r *http.Request) {
	if methodAllowed(w, r, http.MethodGet) {
		return
	}
	settings, err := h.store.GetSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !auth.CheckAPIKey(settings, r) {
		writeError(w, http.StatusUnauthorized, "invalid API key")
		return
	}
	providers, err := h.store.ListProviders()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	data := []map[string]any{}
	for _, p := range providers {
		if !p.Enabled {
			continue
		}
		for _, model := range p.Models {
			if strings.Contains(model, "/") {
				data = append(data, modelMetadata(p.ID, model, settings))
			}
			data = append(data, modelMetadata(p.ID, p.ID+"/"+model, settings))
			if p.ID == settings.DefaultProvider {
				data = append(data, modelMetadata(p.ID, model, settings))
			}
		}
	}
	for _, combo := range settings.Combos {
		if combo.Enabled {
			data = append(data, comboMetadata(combo, settings))
		}
	}
	for _, router := range settings.PromptRouters {
		if router.Enabled {
			data = append(data, promptRouterMetadata(router))
		}
	}
	for _, fusion := range settings.Fusions {
		if fusion.Enabled {
			data = append(data, fusionMetadata(fusion))
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": data})
}

func (h *Handler) ChatCompletions(w http.ResponseWriter, r *http.Request) {
	if methodAllowed(w, r, http.MethodPost) {
		return
	}
	started := time.Now()
	settings, providers, apiKey, ok := h.loadGatewayState(w, r)
	if !ok {
		return
	}
	body, err := readJSONBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	modelStr := getString(body, "model")
	if modelStr == "" {
		writeError(w, http.StatusBadRequest, "missing model")
		return
	}
	if !auth.KeyAllowsModel(apiKey, modelStr) {
		writeError(w, http.StatusForbidden, "API key is not allowed to use model "+modelStr)
		return
	}
	if !auth.KeyWithinQuota(apiKey) {
		writeError(w, http.StatusTooManyRequests, "API key quota exhausted")
		return
	}
	if fusion, ok := findFusion(modelStr, settings); ok {
		h.handleFusionChat(w, r, started, body, settings, providers, fusion, apiKey)
		return
	}
	routerDecision := promptRouterDecision{}
	routeBody := body
	candidates := []resolvedModel{}
	if promptRouter, ok := findPromptRouter(modelStr, settings); ok {
		routerDecision = h.classifyPromptRoute(r.Context(), body, settings, providers, promptRouter, apiKey)
		routeBody = applyPromptRouteInstruction(body, routerDecision.Route, false)
		candidates = resolveRoutableTarget(routerDecision.Target, settings, providers)
	} else {
		candidates = resolveCandidates(modelStr, settings, providers)
		if combo, ok := findCombo(modelStr, settings); ok {
			candidates = resolveComboCandidates(combo, settings, providers)
		}
	}
	if len(candidates) == 0 {
		writeError(w, http.StatusNotFound, "no enabled provider for model "+modelStr)
		return
	}

	optimizeStarted := time.Now()
	optimizedBody := maybeOptimizeChatCompletions(r.Context(), routeBody, settings)
	optimizeDurationMS := time.Since(optimizeStarted).Milliseconds()
	upstreamMeta := collectUpstreamOptimizationMeta(optimizedBody)
	stream := bodyStreamRequested(optimizedBody)
	attempt := func(ctx context.Context, cand resolvedModel) (*provider.ExecuteResult, error) {
		chatBody := setModel(optimizedBody, cand.Model)
		if stream {
			chatBody = ensureStreamUsageOptions(chatBody)
		}
		if cand.IsCodex {
			responsesBody := translator.ChatToResponses(cand.Model, chatBody)
			applyClaudePromptCacheKey(r, responsesBody)
			result, _, err := h.executeWithKeyRetry(ctx, cand, func(ctx context.Context) (*provider.ExecuteResult, error) {
				return h.executors.Codex.ExecuteResponses(ctx, cand.Provider, cand.Model, responsesBody)
			})
			return result, err
		}
		result, _, err := h.executeWithKeyRetry(ctx, cand, func(ctx context.Context) (*provider.ExecuteResult, error) {
			return h.executors.ExecuteChat(ctx, cand.Provider, cand.Model, chatBody)
		})
		if result == nil && err == nil {
			return nil, errAllKeysExhausted(cand)
		}
		return result, err
	}
	commit := func(w http.ResponseWriter, r *http.Request, result *provider.ExecuteResult, cand resolvedModel) (usageInfo, error) {
		resp := result.Response
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			passthroughResponse(w, resp)
			return usageInfo{}, nil
		}
		requestBody := result.TransformedBody
		if cand.IsCodex {
			usage, err := streamResponsesToChat(r.Context(), w, resp, cand.Model, requestBody)
			return usage.withCost(cand.Provider, cand.Model), err
		}
		if isSSEResponse(resp) || stream {
			usage, err := streamPassthrough(r.Context(), w, resp, requestBody)
			return usage.withCost(cand.Provider, cand.Model), err
		}
		usage, err := passthroughJSONWithUsage(w, resp, requestBody)
		return usage.withCost(cand.Provider, cand.Model), err
	}
	requestBodyForUsage := func(cand resolvedModel, result *provider.ExecuteResult) map[string]any {
		if result != nil && result.TransformedBody != nil {
			return result.TransformedBody
		}
		if cand.IsCodex {
			responsesBody := translator.ChatToResponses(cand.Model, optimizedBody)
			applyClaudePromptCacheKey(r, responsesBody)
			return responsesBody
		}
		requestBody := setModel(optimizedBody, cand.Model)
		if stream {
			requestBody = ensureStreamUsageOptions(requestBody)
		}
		return requestBody
	}
	h.runWithFallback(w, r, started, r.URL.Path, stream, candidates, attempt, commit, apiKey, requestBodyForUsage, body, upstreamMeta, optimizeDurationMS, routerDecision)
}

func (h *Handler) Messages(w http.ResponseWriter, r *http.Request) {
	if methodAllowed(w, r, http.MethodPost) {
		return
	}
	started := time.Now()
	settings, providers, apiKey, ok := h.loadGatewayState(w, r)
	if !ok {
		return
	}
	body, err := readJSONBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	modelStr := getString(body, "model")
	if modelStr == "" {
		writeError(w, http.StatusBadRequest, "missing model")
		return
	}
	if !auth.KeyAllowsModel(apiKey, modelStr) {
		writeError(w, http.StatusForbidden, "API key is not allowed to use model "+modelStr)
		return
	}
	if !auth.KeyWithinQuota(apiKey) {
		writeError(w, http.StatusTooManyRequests, "API key quota exhausted")
		return
	}
	routerDecision := promptRouterDecision{}
	routeBody := body
	candidates := []resolvedModel{}
	if promptRouter, ok := findPromptRouter(modelStr, settings); ok {
		routerDecision = h.classifyPromptRoute(r.Context(), body, settings, providers, promptRouter, apiKey)
		routeBody = applyPromptRouteInstruction(body, routerDecision.Route, true)
		candidates = resolveRoutableTarget(routerDecision.Target, settings, providers)
	} else {
		candidates = resolveCandidates(modelStr, settings, providers)
		if combo, ok := findCombo(modelStr, settings); ok {
			candidates = resolveComboCandidates(combo, settings, providers)
		}
	}
	if len(candidates) == 0 {
		writeError(w, http.StatusNotFound, "no enabled provider for model "+modelStr)
		return
	}

	optimizeStarted := time.Now()
	optimizedBody := maybeOptimizeAnthropicToolResults(r.Context(), routeBody, settings)
	optimizeDurationMS := time.Since(optimizeStarted).Milliseconds()
	upstreamMeta := collectUpstreamOptimizationMeta(optimizedBody)
	stream := bodyStreamRequested(optimizedBody)
	attempt := func(ctx context.Context, cand resolvedModel) (*provider.ExecuteResult, error) {
		chatBody := translator.AnthropicMessagesToChat(optimizedBody, cand.Model)
		if stream {
			chatBody = ensureStreamUsageOptions(chatBody)
		}
		if cand.IsCodex {
			responsesBody := translator.AnthropicMessagesToResponses(optimizedBody, cand.Model)
			applyClaudePromptCacheKey(r, responsesBody)
			result, _, err := h.executeWithKeyRetry(ctx, cand, func(ctx context.Context) (*provider.ExecuteResult, error) {
				return h.executors.Codex.ExecuteResponses(ctx, cand.Provider, cand.Model, responsesBody)
			})
			return result, err
		}
		result, _, err := h.executeWithKeyRetry(ctx, cand, func(ctx context.Context) (*provider.ExecuteResult, error) {
			return h.executors.ExecuteChat(ctx, cand.Provider, cand.Model, chatBody)
		})
		if result == nil && err == nil {
			return nil, errAllKeysExhausted(cand)
		}
		return result, err
	}
	commit := func(w http.ResponseWriter, r *http.Request, result *provider.ExecuteResult, cand resolvedModel) (usageInfo, error) {
		resp := result.Response
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			passthroughResponse(w, resp)
			return usageInfo{}, nil
		}
		requestBody := result.TransformedBody
		if cand.IsCodex {
			usage, err := streamResponsesToAnthropic(r.Context(), w, resp, cand.Model, requestBody)
			return usage.withCost(cand.Provider, cand.Model), err
		}
		if isSSEResponse(resp) || stream {
			usage, err := streamChatToAnthropic(r.Context(), w, resp, cand.Model, requestBody)
			return usage.withCost(cand.Provider, cand.Model), err
		}
		usage, err := passthroughAnthropicJSONWithUsage(w, resp, requestBody, cand.Model)
		return usage.withCost(cand.Provider, cand.Model), err
	}
	requestBodyForUsage := func(cand resolvedModel, result *provider.ExecuteResult) map[string]any {
		if result != nil && result.TransformedBody != nil {
			return result.TransformedBody
		}
		chatBody := translator.AnthropicMessagesToChat(optimizedBody, cand.Model)
		if stream {
			chatBody = ensureStreamUsageOptions(chatBody)
		}
		if cand.IsCodex {
			responsesBody := translator.AnthropicMessagesToResponses(optimizedBody, cand.Model)
			applyClaudePromptCacheKey(r, responsesBody)
			return responsesBody
		}
		return chatBody
	}
	h.runWithFallback(w, r, started, r.URL.Path, stream, candidates, attempt, commit, apiKey, requestBodyForUsage, body, upstreamMeta, optimizeDurationMS, routerDecision)
}

func (h *Handler) Responses(w http.ResponseWriter, r *http.Request) {
	if methodAllowed(w, r, http.MethodPost) {
		return
	}
	started := time.Now()
	settings, providers, apiKey, ok := h.loadGatewayState(w, r)
	if !ok {
		return
	}
	body, err := readJSONBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	modelStr := getString(body, "model")
	if modelStr == "" {
		modelStr = settings.DefaultCodexID
	}
	codexSettings := store.Settings{DefaultProvider: settings.DefaultCodexID, DefaultCodexID: settings.DefaultCodexID}
	if !auth.KeyAllowsModel(apiKey, modelStr) {
		writeError(w, http.StatusForbidden, "API key is not allowed to use model "+modelStr)
		return
	}
	if !auth.KeyWithinQuota(apiKey) {
		writeError(w, http.StatusTooManyRequests, "API key quota exhausted")
		return
	}
	candidates := codexCandidates(modelStr, codexSettings, providers)
	if len(candidates) == 0 {
		writeError(w, http.StatusNotFound, "no enabled Codex provider")
		return
	}

	attempt := func(ctx context.Context, cand resolvedModel) (*provider.ExecuteResult, error) {
		result, _, err := h.executeWithKeyRetry(ctx, cand, func(ctx context.Context) (*provider.ExecuteResult, error) {
			return h.executors.Codex.ExecuteResponses(ctx, cand.Provider, cand.Model, body)
		})
		if result == nil && err == nil {
			return nil, errAllKeysExhausted(cand)
		}
		return result, err
	}
	commit := func(w http.ResponseWriter, r *http.Request, result *provider.ExecuteResult, cand resolvedModel) (usageInfo, error) {
		resp := result.Response
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			passthroughResponse(w, resp)
			return usageInfo{}, nil
		}
		usage, err := streamPassthrough(r.Context(), w, resp, result.TransformedBody)
		return usage.withCost(cand.Provider, cand.Model), err
	}
	requestBodyForUsage := func(_ resolvedModel, result *provider.ExecuteResult) map[string]any {
		if result != nil && result.TransformedBody != nil {
			return result.TransformedBody
		}
		return body
	}
	h.runWithFallback(w, r, started, r.URL.Path, true, candidates, attempt, commit, apiKey, requestBodyForUsage, body, upstreamOptimizationMeta{}, 0, promptRouterDecision{})
}

func applyClaudePromptCacheKey(r *http.Request, body map[string]any) {
	if body == nil || strings.TrimSpace(getString(body, "prompt_cache_key")) != "" {
		return
	}
	for _, header := range []string{"x-claude-code-session-id", "X-Claude-Code-Session-Id", "anthropic-session-id"} {
		if value := sanitizePromptCacheKey(r.Header.Get(header)); value != "" {
			body["prompt_cache_key"] = "claude-code:" + value
			return
		}
	}
}

func sanitizePromptCacheKey(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' || r == ':' {
			b.WriteRune(r)
		}
		if b.Len() >= 180 {
			break
		}
	}
	return b.String()
}

// codexCandidates resolves Codex providers, keeping the legacy bare-ID and
// provider-ID matching while returning all enabled Codex providers as fallbacks.
func codexCandidates(modelStr string, settings store.Settings, providers []store.Provider) []resolvedModel {
	candidates := resolveCandidates(modelStr, settings, providers)
	filtered := candidates[:0]
	for _, c := range candidates {
		if c.IsCodex {
			// A bare provider ID (e.g. "codex") is not a model name; fall back
			// to the provider's first configured model.
			if c.Model == c.Provider.ID && len(c.Provider.Models) > 0 {
				c.Model = c.Provider.Models[0]
			}
			filtered = append(filtered, c)
		}
	}
	if len(filtered) > 0 {
		return filtered
	}

	// Explicit fallback: first enabled Codex provider, matching prior behavior.
	out := []resolvedModel{}
	for _, p := range providers {
		if !p.Enabled || p.Type != store.ProviderCodex {
			continue
		}
		model := modelStr
		if strings.Contains(modelStr, "/") {
			_, model = splitModel(modelStr)
		}
		if model == p.ID && len(p.Models) > 0 {
			model = p.Models[0]
		}
		if model == "" && len(p.Models) > 0 {
			model = p.Models[0]
		}
		out = append(out, resolvedModel{Provider: p, Model: model, IsCodex: true})
	}
	return out
}

func (h *Handler) loadGatewayState(w http.ResponseWriter, r *http.Request) (store.Settings, []store.Provider, store.APIKeyPolicy, bool) {
	settings, err := h.store.GetSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return store.Settings{}, nil, store.APIKeyPolicy{}, false
	}
	apiKey, keyOK := auth.ResolveAPIKey(settings, r)
	if !keyOK {
		writeError(w, http.StatusUnauthorized, "invalid API key")
		return store.Settings{}, nil, store.APIKeyPolicy{}, false
	}
	providers, err := h.store.ListProviders()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return store.Settings{}, nil, store.APIKeyPolicy{}, false
	}
	return settings, providers, apiKey, true
}

func (h *Handler) logRequest(endpoint string, resolved resolvedModel, stream bool, started time.Time, status string, errText string, usage usageInfo, apiKey store.APIKeyPolicy, debugBody map[string]any, upstreamMeta upstreamOptimizationMeta, optimizeDurationMS int64, providerDurationMS int64, routerDecision promptRouterDecision) {
	settings, settingsErr := h.store.GetSettings()
	if settingsErr != nil || settings.ObservabilityEnabled {
		debugStarted := time.Now()
		debugPayload := buildDebugPayload(settings, debugBody)
		debugLogDurationMS := time.Since(debugStarted).Milliseconds()
		promptSaved, toolSaved := 0, 0
		if debugPayload != nil {
			promptSaved = debugPayload.EstimatedPromptTokensSaved
			toolSaved = debugPayload.EstimatedToolTokensSaved
		}
		tokensSaved := promptSaved + toolSaved
		rawInputTokens := usage.PromptTokens
		if rawInputTokens > 0 && tokensSaved > 0 {
			rawInputTokens += tokensSaved
		}
		prefix, suffix, masked := maskedAPIKeyParts(apiKey.Key)
		_ = h.store.AddRequestLog(store.RequestLog{
			Timestamp:                  time.Now().UTC(),
			Endpoint:                   endpoint,
			ProviderID:                 resolved.Provider.ID,
			Model:                      resolved.Model,
			Status:                     status,
			DurationMS:                 time.Since(started).Milliseconds(),
			Stream:                     stream,
			PromptTokens:               usage.PromptTokens,
			CompletionTokens:           usage.CompletionTokens,
			TotalTokens:                usage.TotalTokens,
			CachedTokens:               usage.CachedTokens,
			ReasoningTokens:            usage.ReasoningTokens,
			OptimizeDurationMS:         optimizeDurationMS,
			ProviderDurationMS:         providerDurationMS,
			DebugLogDurationMS:         debugLogDurationMS,
			EstimatedTokens:            usage.Estimated,
			RawInputTokens:             rawInputTokens,
			UpstreamTokensSaved:        upstreamMeta.TokensSaved,
			UpstreamOptimizerEngine:    upstreamMeta.Engine,
			UpstreamOptimizedParts:     upstreamMeta.Parts,
			EstimatedTokensSaved:       tokensSaved,
			EstimatedPromptTokensSaved: promptSaved,
			EstimatedToolTokensSaved:   toolSaved,
			CostUSD:                    usage.CostUSD,
			APIKeyID:                   apiKey.ID,
			APIKeyMasked:               masked,
			APIKeyPrefix:               prefix,
			APIKeySuffix:               suffix,
			Error:                      errText,
			RouterName:                 routerDecision.Router.Name,
			RouterRole:                 routerDecision.Role,
			RouterComplexity:           routerDecision.Complexity,
			RouterRisk:                 routerDecision.Risk,
			RouterTarget:               routerDecision.Target,
			RouterClassifierModel:      routerDecision.ClassifierModel,
			RouterConfidence:           routerDecision.Confidence,
			RouterReason:               routerDecision.Reason,
			RouterDurationMS:           routerDecision.DurationMS,
			RouterUsedFallback:         routerDecision.UsedFallback,
			Debug:                      debugPayload,
		})
	}
	if apiKey.ID != "" && apiKey.ID != "local" {
		if settingsErr == nil {
			_ = h.store.SaveSettings(applyAPIKeyUsage(settings, apiKey.ID, usage))
		} else if settings, err := h.store.GetSettings(); err == nil {
			_ = h.store.SaveSettings(applyAPIKeyUsage(settings, apiKey.ID, usage))
		}
	}
}

func errString(err error) string {
	if err == nil || err == io.EOF {
		return ""
	}
	return err.Error()
}
