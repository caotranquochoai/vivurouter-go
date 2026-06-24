package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/local/vivurouter-go/internal/provider"
	"github.com/local/vivurouter-go/internal/store"
	"github.com/local/vivurouter-go/internal/translator"
)

const (
	defaultFusionExpertPrompt = `You are one expert in a multi-model Fusion panel.

Your role: {{role}}

Analyze the user's request independently. Focus on correctness, overlooked constraints, risks, and actionable details.
Return your best answer with clear reasoning.`

	defaultFusionSynthesisPrompt = `You are the secretary of a multi-model Fusion panel.

You will receive the original user request and several expert responses.
Produce a structured synthesis with:
1. Consensus
2. Disagreements
3. Partial coverage
4. Unique insights
5. Blind spots
6. Draft final answer

Do not invent facts not supported by the original request or expert responses.`

	defaultFusionReviewerPrompt = `You are the final manager/reviewer.

You will receive the original user request and a synthesis report.
Resolve contradictions, remove unsupported claims, improve clarity, and produce the final answer for the user.
Return only the final user-facing answer.`
)

type fusionExpertResult struct {
	ExpertName   string `json:"expert_name"`
	Target       string `json:"target"`
	Role         string `json:"role,omitempty"`
	Success      bool   `json:"success"`
	Error        string `json:"error,omitempty"`
	Content      string `json:"content,omitempty"`
	DurationMS   int64  `json:"duration_ms"`
	PromptTokens int    `json:"prompt_tokens,omitempty"`
	OutputTokens int    `json:"output_tokens,omitempty"`
}

type fusionStageResult struct {
	Name         string `json:"name"`
	Target       string `json:"target"`
	Success      bool   `json:"success"`
	Error        string `json:"error,omitempty"`
	Content      string `json:"content,omitempty"`
	DurationMS   int64  `json:"duration_ms"`
	PromptTokens int    `json:"prompt_tokens,omitempty"`
	OutputTokens int    `json:"output_tokens,omitempty"`
}

type fusionRunResult struct {
	Content        string
	Usage          usageInfo
	Trace          string
	ExpertCount    int
	SuccessCount   int
	SynthTarget    string
	ReviewerTarget string
	UsedReviewer   bool
	DurationMS     int64
}

func findFusion(model string, settings store.Settings) (store.Fusion, bool) {
	model = strings.TrimPrefix(strings.TrimSpace(model), "fusion:")
	if model == "" || strings.Contains(model, "/") {
		return store.Fusion{}, false
	}
	for _, fusion := range settings.Fusions {
		if fusion.Enabled && fusion.Name == model && len(fusion.Experts) > 0 {
			return fusion, true
		}
	}
	return store.Fusion{}, false
}

func fusionMetadata(fusion store.Fusion) map[string]any {
	return map[string]any{"id": fusion.Name, "object": "model", "created": time.Now().Unix(), "owned_by": "vivurouter", "type": "fusion"}
}

func (h *Handler) handleFusionChat(w http.ResponseWriter, r *http.Request, started time.Time, body map[string]any, settings store.Settings, providers []store.Provider, fusion store.Fusion, apiKey store.APIKeyPolicy) {
	stream := bodyStreamRequested(body)
	if stream {
		body = cloneMap(body)
		body["stream"] = false
	}
	result, err := h.runFusion(r.Context(), fusion, body, settings, providers)
	status := http.StatusOK
	statusText := "200"
	errText := ""
	if err != nil {
		status = http.StatusBadGateway
		statusText = "FAILED"
		errText = err.Error()
		writeError(w, status, errText)
	} else if stream {
		if writeErr := writeFusionStreamResponse(w, fusion.Name, result.Content, result.Usage); writeErr != nil && errText == "" {
			errText = writeErr.Error()
		}
	} else {
		writeJSON(w, status, fusionChatResponse(fusion.Name, result.Content, result.Usage))
	}
	log := store.RequestLog{
		Timestamp:               time.Now().UTC(),
		Endpoint:                r.URL.Path,
		ProviderID:              "fusion",
		Model:                   fusion.Name,
		Status:                  statusText,
		DurationMS:              time.Since(started).Milliseconds(),
		Stream:                  stream,
		PromptTokens:            result.Usage.PromptTokens,
		CompletionTokens:        result.Usage.CompletionTokens,
		TotalTokens:             result.Usage.TotalTokens,
		CachedTokens:            result.Usage.CachedTokens,
		ReasoningTokens:         result.Usage.ReasoningTokens,
		EstimatedTokens:         result.Usage.Estimated,
		CostUSD:                 result.Usage.CostUSD,
		APIKeyID:                apiKey.ID,
		Error:                   errText,
		FusionName:              fusion.Name,
		FusionMode:              fusion.Mode,
		FusionExpertCount:       result.ExpertCount,
		FusionSuccessfulExperts: result.SuccessCount,
		FusionSynthesizerTarget: result.SynthTarget,
		FusionReviewerTarget:    result.ReviewerTarget,
		FusionDurationMS:        result.DurationMS,
		FusionUsedReviewer:      result.UsedReviewer,
		FusionError:             errText,
		FusionTrace:             result.Trace,
	}
	_ = h.store.AddRequestLog(log)
	if apiKey.ID != "" && apiKey.ID != "local" {
		_ = h.store.SaveSettings(applyAPIKeyUsage(settings, apiKey.ID, result.Usage))
	}
}

func (h *Handler) runFusion(ctx context.Context, fusion store.Fusion, body map[string]any, settings store.Settings, providers []store.Provider) (fusionRunResult, error) {
	started := time.Now()
	ctx, cancel := context.WithTimeout(ctx, time.Duration(fusion.TimeoutMS)*time.Millisecond)
	defer cancel()
	experts := enabledFusionExperts(fusion.Experts)
	if len(experts) == 0 {
		return fusionRunResult{}, fmt.Errorf("fusion %s has no enabled experts", fusion.Name)
	}
	var results []fusionExpertResult
	if fusion.Mode == store.FusionModeSequential {
		for _, expert := range experts {
			results = append(results, h.runFusionExpert(ctx, fusion, expert, body, settings, providers))
		}
	} else {
		results = make([]fusionExpertResult, len(experts))
		var wg sync.WaitGroup
		for i, expert := range experts {
			wg.Add(1)
			go func(i int, expert store.FusionExpert) {
				defer wg.Done()
				results[i] = h.runFusionExpert(ctx, fusion, expert, body, settings, providers)
			}(i, expert)
		}
		wg.Wait()
	}
	success := 0
	usage := usageInfo{}
	for _, res := range results {
		if res.Success {
			success++
		}
		usage.PromptTokens += res.PromptTokens
		usage.CompletionTokens += res.OutputTokens
		usage.TotalTokens += res.PromptTokens + res.OutputTokens
	}
	minSuccess := fusion.MinSuccessfulExperts
	if minSuccess <= 0 {
		minSuccess = len(experts)
	}
	if success < minSuccess {
		trace := encodeFusionTrace(results, fusionStageResult{}, fusionStageResult{}, "", "")
		return fusionRunResult{Trace: trace, ExpertCount: len(experts), SuccessCount: success, DurationMS: time.Since(started).Milliseconds()}, fmt.Errorf("fusion %s only had %d/%d successful experts", fusion.Name, success, minSuccess)
	}
	synthStarted := time.Now()
	synthesis, synthUsage, err := h.runFusionStage(ctx, fusion.SynthesizerTarget, buildFusionSynthesisBody(fusion, body, results), settings, providers)
	synthStage := fusionStageResult{Name: "synthesizer", Target: fusion.SynthesizerTarget, Success: err == nil, Content: synthesis, DurationMS: time.Since(synthStarted).Milliseconds(), PromptTokens: synthUsage.PromptTokens, OutputTokens: synthUsage.CompletionTokens}
	usage = addUsage(usage, synthUsage)
	if err != nil {
		synthStage.Error = err.Error()
		trace := encodeFusionTrace(results, synthStage, fusionStageResult{}, "", "")
		return fusionRunResult{Usage: usage, Trace: trace, ExpertCount: len(experts), SuccessCount: success, SynthTarget: fusion.SynthesizerTarget, DurationMS: time.Since(started).Milliseconds()}, err
	}
	final := synthesis
	usedReviewer := false
	reviewStage := fusionStageResult{Name: "reviewer", Target: fusion.ReviewerTarget}
	if fusion.RequireReviewer && strings.TrimSpace(fusion.ReviewerTarget) != "" {
		reviewStarted := time.Now()
		review, reviewUsage, reviewErr := h.runFusionStage(ctx, fusion.ReviewerTarget, buildFusionReviewerBody(fusion, body, synthesis), settings, providers)
		reviewStage = fusionStageResult{Name: "reviewer", Target: fusion.ReviewerTarget, Success: reviewErr == nil, Content: review, DurationMS: time.Since(reviewStarted).Milliseconds(), PromptTokens: reviewUsage.PromptTokens, OutputTokens: reviewUsage.CompletionTokens}
		usage = addUsage(usage, reviewUsage)
		if reviewErr == nil && strings.TrimSpace(review) != "" {
			final = review
			usedReviewer = true
		} else if reviewErr != nil {
			reviewStage.Error = reviewErr.Error()
		}
	}
	trace := encodeFusionTrace(results, synthStage, reviewStage, synthesis, final)
	usage.Estimated = true
	return fusionRunResult{Content: final, Usage: usage, Trace: trace, ExpertCount: len(experts), SuccessCount: success, SynthTarget: fusion.SynthesizerTarget, ReviewerTarget: fusion.ReviewerTarget, UsedReviewer: usedReviewer, DurationMS: time.Since(started).Milliseconds()}, nil
}

func (h *Handler) runFusionExpert(ctx context.Context, fusion store.Fusion, expert store.FusionExpert, body map[string]any, settings store.Settings, providers []store.Provider) fusionExpertResult {
	started := time.Now()
	stageBody := buildFusionExpertBody(fusion, expert, body)
	content, usage, err := h.runFusionStage(ctx, expert.Target, stageBody, settings, providers)
	res := fusionExpertResult{ExpertName: expert.Name, Target: expert.Target, Role: expert.Role, Success: err == nil, Content: content, DurationMS: time.Since(started).Milliseconds(), PromptTokens: usage.PromptTokens, OutputTokens: usage.CompletionTokens}
	if err != nil {
		res.Error = err.Error()
	}
	return res
}

func (h *Handler) runFusionStage(ctx context.Context, target string, body map[string]any, settings store.Settings, providers []store.Provider) (string, usageInfo, error) {
	candidates := resolveRoutableTarget(target, settings, providers)
	if len(candidates) == 0 {
		return "", usageInfo{}, fmt.Errorf("no provider for fusion target %s", target)
	}
	cand := candidates[0]
	requestBody := setModel(body, cand.Model)
	requestBody["stream"] = false
	var result *provider.ExecuteResult
	var err error
	if cand.IsCodex {
		responsesBody := translator.ChatToResponses(cand.Model, requestBody)
		requestBody = responsesBody
		result, err = h.executors.Codex.ExecuteResponses(ctx, cand.Provider, cand.Model, responsesBody)
	} else {
		result, err = h.executors.ExecuteChat(ctx, cand.Provider, cand.Model, requestBody)
	}
	if err != nil {
		return "", usageInfo{}.ensureEstimated(requestBody, 0).withCost(cand.Provider, cand.Model), err
	}
	defer result.Response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(result.Response.Body, 4*1024*1024))
	content, extracted := extractAssistantTextStrict(raw)
	outputChars := estimateOutputCharsFromJSON(raw)
	if extracted {
		outputChars = len(content)
	}
	usage, ok := extractUsageFromJSON(raw)
	if !ok || !usage.hasTokens() {
		usage = usageInfo{}.ensureEstimated(requestBody, outputChars)
	}
	usage = usage.withCost(cand.Provider, cand.Model)
	if err != nil {
		return "", usage, err
	}
	if result.Response.StatusCode < 200 || result.Response.StatusCode >= 300 {
		return "", usage, fmt.Errorf("fusion target %s status %d", target, result.Response.StatusCode)
	}
	content = strings.TrimSpace(content)
	if !extracted || content == "" {
		return "", usage, fmt.Errorf("fusion target %s returned no assistant text", target)
	}
	return content, usage, nil
}

func enabledFusionExperts(items []store.FusionExpert) []store.FusionExpert {
	out := []store.FusionExpert{}
	for _, item := range items {
		if item.Enabled {
			out = append(out, item)
		}
	}
	return out
}

func buildFusionExpertBody(fusion store.Fusion, expert store.FusionExpert, original map[string]any) map[string]any {
	body := cloneMap(original)
	body["stream"] = false
	if fusion.MaxOutputTokens > 0 {
		body["max_tokens"] = fusion.MaxOutputTokens
	}
	prompt := expert.PromptTemplate
	if strings.TrimSpace(prompt) == "" {
		prompt = defaultFusionExpertPrompt
	}
	prompt = strings.ReplaceAll(prompt, "{{role}}", expert.Role)
	prependSystemMessage(body, prompt)
	return body
}

func buildFusionSynthesisBody(fusion store.Fusion, original map[string]any, results []fusionExpertResult) map[string]any {
	body := cloneMap(original)
	body["stream"] = false
	body["temperature"] = 0
	if fusion.MaxOutputTokens > 0 {
		body["max_tokens"] = fusion.MaxOutputTokens
	}
	prompt := fusion.SynthesisPromptTemplate
	if strings.TrimSpace(prompt) == "" {
		prompt = defaultFusionSynthesisPrompt
	}
	prependSystemMessage(body, prompt+"\n\n"+formatFusionExpertResults(results))
	return body
}

func buildFusionReviewerBody(fusion store.Fusion, original map[string]any, synthesis string) map[string]any {
	body := cloneMap(original)
	body["stream"] = false
	body["temperature"] = 0
	if fusion.MaxOutputTokens > 0 {
		body["max_tokens"] = fusion.MaxOutputTokens
	}
	prompt := fusion.ReviewerPromptTemplate
	if strings.TrimSpace(prompt) == "" {
		prompt = defaultFusionReviewerPrompt
	}
	prependSystemMessage(body, prompt+"\n\nSynthesis report:\n"+synthesis)
	return body
}

func prependSystemMessage(body map[string]any, content string) {
	messages, _ := body["messages"].([]any)
	if typed, ok := body["messages"].([]map[string]any); ok && messages == nil {
		messages = make([]any, len(typed))
		for i := range typed {
			messages[i] = typed[i]
		}
	}
	body["messages"] = append([]any{map[string]any{"role": "system", "content": content}}, messages...)
}

func formatFusionExpertResults(results []fusionExpertResult) string {
	var b strings.Builder
	b.WriteString("Expert responses:\n")
	for _, res := range results {
		b.WriteString("\n---\n")
		b.WriteString("Expert: " + res.ExpertName + "\n")
		b.WriteString("Target: " + res.Target + "\n")
		if res.Role != "" {
			b.WriteString("Role: " + res.Role + "\n")
		}
		if !res.Success {
			b.WriteString("Error: " + res.Error + "\n")
			continue
		}
		b.WriteString(res.Content + "\n")
	}
	return b.String()
}

func fusionChatResponse(model string, content string, usage usageInfo) map[string]any {
	now := time.Now().Unix()
	return map[string]any{
		"id":      fmt.Sprintf("fusion-%d", now),
		"object":  "chat.completion",
		"created": now,
		"model":   model,
		"choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": content}, "finish_reason": "stop"}},
		"usage":   usageToOpenAIMap(usage),
	}
}

func writeFusionStreamResponse(w http.ResponseWriter, model string, content string, usage usageInfo) error {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	id := fmt.Sprintf("fusion-%d", nowUnixMillis())
	created := nowUnix()
	emit := func(chunk map[string]any) error {
		raw, err := json.Marshal(chunk)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", raw); err != nil {
			return err
		}
		if flusher != nil {
			flusher.Flush()
		}
		return nil
	}
	if err := emit(chatChunk(id, created, model, map[string]any{"role": "assistant"}, nil)); err != nil {
		return err
	}
	if strings.TrimSpace(content) != "" {
		if err := emit(chatChunk(id, created, model, map[string]any{"content": content}, nil)); err != nil {
			return err
		}
	}
	if err := emit(chatChunk(id, created, model, map[string]any{}, "stop")); err != nil {
		return err
	}
	if err := emit(openAIUsageChunk(id, created, model, usage)); err != nil {
		return err
	}
	_, err := io.WriteString(w, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
	return err
}

func addUsage(a, b usageInfo) usageInfo {
	a.PromptTokens += b.PromptTokens
	a.CompletionTokens += b.CompletionTokens
	a.TotalTokens += b.TotalTokens
	a.CachedTokens += b.CachedTokens
	a.ReasoningTokens += b.ReasoningTokens
	a.CostUSD += b.CostUSD
	a.Estimated = a.Estimated || b.Estimated
	return a
}

func encodeFusionTrace(results []fusionExpertResult, synthesizer fusionStageResult, reviewer fusionStageResult, synthesis string, final string) string {
	payload := map[string]any{"experts": results}
	if synthesizer.Target != "" || synthesizer.DurationMS > 0 || synthesizer.Success || synthesizer.Error != "" {
		payload["synthesizer"] = synthesizer
	}
	if reviewer.Target != "" || reviewer.DurationMS > 0 || reviewer.Success || reviewer.Error != "" {
		payload["reviewer"] = reviewer
	}
	if synthesis != "" {
		payload["synthesis"] = synthesis
	}
	if final != "" {
		payload["final"] = final
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return string(raw)
}
