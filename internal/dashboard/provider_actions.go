package dashboard

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/local/vivurouter-go/internal/gateway"
	upstream "github.com/local/vivurouter-go/internal/provider"
	"github.com/local/vivurouter-go/internal/store"
)

func (h *Handlers) logModelTest(provider store.Provider, model string, status int, started time.Time, usage gatewayUsageShim, errText string) {
	settings, err := h.store.GetSettings()
	if err == nil && !settings.ObservabilityEnabled {
		return
	}
	_ = h.store.AddRequestLog(store.RequestLog{
		Timestamp:        time.Now().UTC(),
		Endpoint:         "/api/providers/test-model",
		ProviderID:       provider.ID,
		Model:            model,
		Status:           fmt.Sprintf("%d", status),
		DurationMS:       time.Since(started).Milliseconds(),
		Stream:           provider.Type == store.ProviderCodex,
		PromptTokens:     usage.PromptTokens,
		CompletionTokens: usage.CompletionTokens,
		TotalTokens:      usage.TotalTokens,
		EstimatedTokens:  usage.Estimated,
		CostUSD:          usage.CostUSD,
		Error:            errText,
	})
}

type gatewayUsageShim struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	Estimated        bool
	CostUSD          float64
}

func usageShimFromPublic(usage gateway.PublicUsageInfo) gatewayUsageShim {
	return gatewayUsageShim{
		PromptTokens:     usage.PromptTokens,
		CompletionTokens: usage.CompletionTokens,
		TotalTokens:      usage.TotalTokens,
		Estimated:        usage.Estimated,
		CostUSD:          usage.CostUSD,
	}
}

type providerActionInput struct {
	ProviderID string `json:"provider_id"`
	Model      string `json:"model"`
	Kind       string `json:"kind"`
	Apply      bool   `json:"apply"`
}

type modelTestResult struct {
	OK         bool      `json:"ok"`
	ProviderID string    `json:"provider_id"`
	Model      string    `json:"model"`
	Status     int       `json:"status,omitempty"`
	LatencyMS  int64     `json:"latency_ms"`
	Error      string    `json:"error,omitempty"`
	TestedAt   time.Time `json:"tested_at"`
}

func (h *Handlers) ProviderModelsAPI(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdminAPI(w, r) {
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	input := readProviderActionInput(r)
	if input.ProviderID == "" {
		writeError(w, http.StatusBadRequest, "missing provider_id")
		return
	}

	provider, found, err := h.store.GetProvider(input.ProviderID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "provider not found")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()

	models, err := h.fetchProviderModels(ctx, provider)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if input.Apply {
		provider.Models = modelIDs(models)
		if err := h.store.UpsertProvider(provider); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"provider_id": provider.ID,
		"models":      models,
		"count":       len(models),
		"saved":       input.Apply,
		"fetched_at":  time.Now().UTC(),
	})
}

func (h *Handlers) CodexQuotaAPI(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdminAPI(w, r) {
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	input := readProviderActionInput(r)
	if input.ProviderID == "" {
		settings, _ := h.store.GetSettings()
		providers, _ := h.store.ListProviders()
		input.ProviderID = firstCodexProviderID(settings, providers)
	}
	if input.ProviderID == "" {
		writeError(w, http.StatusBadRequest, "missing provider_id")
		return
	}
	provider, found, err := h.store.GetProvider(input.ProviderID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "provider not found")
		return
	}
	if provider.Type != store.ProviderCodex {
		writeError(w, http.StatusBadRequest, "provider is not a Codex provider")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	report, err := h.executors.Codex.FetchQuota(ctx, provider)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (h *Handlers) AntigravityQuotaAPI(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdminAPI(w, r) {
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	input := readProviderActionInput(r)
	if input.ProviderID == "" {
		providers, _ := h.store.ListProviders()
		for _, provider := range providers {
			if provider.Type == store.ProviderAntigravity {
				input.ProviderID = provider.ID
				break
			}
		}
	}
	if input.ProviderID == "" {
		writeError(w, http.StatusBadRequest, "missing provider_id")
		return
	}
	provider, found, err := h.store.GetProvider(input.ProviderID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "provider not found")
		return
	}
	if provider.Type != store.ProviderAntigravity {
		writeError(w, http.StatusBadRequest, "provider is not an Antigravity provider")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	report, err := h.executors.Antigravity.FetchQuota(ctx, provider)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (h *Handlers) ProviderModelTestAPI(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdminAPI(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	input := readProviderActionInput(r)
	if input.ProviderID == "" || input.Model == "" {
		writeError(w, http.StatusBadRequest, "missing provider_id or model")
		return
	}

	provider, found, err := h.store.GetProvider(input.ProviderID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "provider not found")
		return
	}

	const timeout = 30 * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	started := time.Now()
	var result modelTestResult
	if provider.Type == store.ProviderCodex {
		result = h.testCodexModel(ctx, provider, input.Model, started)
	} else {
		result = h.testOpenAIModel(ctx, provider, input.Model, started)
	}
	if ctx.Err() == context.DeadlineExceeded && result.Error == "" {
		result.Error = "upstream timeout after 30s"
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handlers) fetchProviderModels(ctx context.Context, provider store.Provider) ([]upstream.ModelInfo, error) {
	return h.executors.FetchModels(ctx, provider)
}

func (h *Handlers) testOpenAIModel(ctx context.Context, provider store.Provider, model string, started time.Time) modelTestResult {
	body := map[string]any{
		"model":      model,
		"max_tokens": 1,
		"stream":     false,
		"messages": []map[string]string{{
			"role":    "user",
			"content": "hi",
		}},
	}
	result, err := h.executors.ExecuteChat(ctx, provider, model, body)
	out := modelTestResult{ProviderID: provider.ID, Model: model, LatencyMS: time.Since(started).Milliseconds(), TestedAt: time.Now().UTC()}
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			out.Error = "upstream timeout after 30s"
		} else {
			out.Error = err.Error()
		}
		usage := usageShimFromPublic(gateway.AnalyzeUsage(provider, model, body, nil, 0))
		h.logModelTest(provider, model, 504, started, usage, out.Error)
		return out
	}
	defer result.Response.Body.Close()
	out.Status = result.Response.StatusCode
	raw, _ := io.ReadAll(io.LimitReader(result.Response.Body, 1<<20))
	out.LatencyMS = time.Since(started).Milliseconds()
	usage := usageShimFromPublic(gateway.AnalyzeUsage(provider, model, body, raw, 0))
	if result.Response.StatusCode < 200 || result.Response.StatusCode >= 300 {
		out.Error = responseErrorDetail(raw, result.Response.StatusCode)
		h.logModelTest(provider, model, out.Status, started, usage, out.Error)
		return out
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err == nil {
		if choices, ok := payload["choices"].([]any); ok && len(choices) > 0 {
			out.OK = true
			h.logModelTest(provider, model, out.Status, started, usage, "")
			return out
		}
		if payload["error"] != nil {
			out.Error = truncate(responseErrorDetail(raw, result.Response.StatusCode), 300)
			h.logModelTest(provider, model, out.Status, started, usage, out.Error)
			return out
		}
	}
	out.OK = true
	h.logModelTest(provider, model, out.Status, started, usage, "")
	return out
}

func (h *Handlers) testCodexModel(ctx context.Context, provider store.Provider, model string, started time.Time) modelTestResult {
	body := map[string]any{
		"model":        model,
		"input":        "hi",
		"instructions": "Reply with ok.",
		"reasoning":    map[string]any{"effort": "none", "summary": "auto"},
	}
	result, err := h.executors.Codex.ExecuteResponses(ctx, provider, model, body)
	out := modelTestResult{ProviderID: provider.ID, Model: model, LatencyMS: time.Since(started).Milliseconds(), TestedAt: time.Now().UTC()}
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			out.Error = "upstream timeout after 30s"
		} else {
			out.Error = err.Error()
		}
		usage := usageShimFromPublic(gateway.AnalyzeUsage(provider, model, body, nil, 0))
		h.logModelTest(provider, model, 504, started, usage, out.Error)
		return out
	}
	defer result.Response.Body.Close()
	out.Status = result.Response.StatusCode
	if result.Response.StatusCode < 200 || result.Response.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(result.Response.Body, 1<<20))
		out.LatencyMS = time.Since(started).Milliseconds()
		out.Error = responseErrorDetail(raw, result.Response.StatusCode)
		usage := usageShimFromPublic(gateway.AnalyzeUsage(provider, model, body, raw, 0))
		h.logModelTest(provider, model, out.Status, started, usage, out.Error)
		return out
	}
	ok, errText, rawStream, outputChars := readCodexTestStream(ctx, result.Response.Body)
	out.LatencyMS = time.Since(started).Milliseconds()
	out.OK = ok && errText == ""
	if errText == context.DeadlineExceeded.Error() {
		out.Error = "upstream timeout after 30s"
	} else {
		out.Error = errText
	}
	usage := usageShimFromPublic(gateway.AnalyzeUsage(provider, model, body, []byte(rawStream), outputChars))
	h.logModelTest(provider, model, out.Status, started, usage, out.Error)
	return out
}

func readCodexTestStream(ctx context.Context, body io.Reader) (bool, string, string, int) {
	reader := bufio.NewReader(body)
	var eventName string
	var dataLines []string
	var raw strings.Builder
	outputChars := 0
	seenAccepted := false
	for {
		select {
		case <-ctx.Done():
			return seenAccepted, ctx.Err().Error(), raw.String(), outputChars
		default:
		}
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			raw.Write(line)
			trimmed := strings.TrimRight(string(line), "\r\n")
			if trimmed == "" {
				data := strings.Join(dataLines, "\n")
				outputChars += codexEventOutputChars(data)
				done, streamErr := inspectCodexEvent(eventName, data)
				if streamErr != "" || done {
					return streamErr == "", streamErr, raw.String(), outputChars
				}
				if data != "" {
					seenAccepted = true
				}
				eventName = ""
				dataLines = nil
			} else if strings.HasPrefix(trimmed, "event:") {
				eventName = strings.TrimSpace(strings.TrimPrefix(trimmed, "event:"))
			} else if strings.HasPrefix(trimmed, "data:") {
				dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(trimmed, "data:")))
			}
		}
		if err != nil {
			if err == io.EOF {
				return seenAccepted, "", raw.String(), outputChars
			}
			return seenAccepted, err.Error(), raw.String(), outputChars
		}
	}
}

func codexEventOutputChars(data string) int {
	if data == "" || data == "[DONE]" {
		return 0
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(data), &payload); err != nil {
		return len(data)
	}
	chars := 0
	for _, key := range []string{"delta", "text", "output_text", "content"} {
		if value, _ := payload[key].(string); value != "" {
			chars += len(value)
		}
	}
	if response, ok := payload["response"].(map[string]any); ok {
		if value, _ := response["output_text"].(string); value != "" {
			chars += len(value)
		}
	}
	if chars == 0 {
		return len(data) / 8
	}
	return chars
}

func inspectCodexEvent(eventName string, data string) (bool, string) {
	if data == "" || data == "[DONE]" {
		return data == "[DONE]", ""
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(data), &payload); err != nil {
		return false, ""
	}
	if eventName == "" {
		eventName, _ = payload["type"].(string)
	}
	if eventName == "error" || eventName == "response.failed" {
		return true, truncate(errorMessageFromPayload(payload), 360)
	}
	if eventName == "response.completed" || eventName == "response.done" {
		return true, ""
	}
	return false, ""
}

func responseErrorDetail(raw []byte, status int) string {
	message := strings.TrimSpace(string(raw))
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err == nil {
		message = errorMessageFromPayload(payload)
	}
	if message == "" {
		message = http.StatusText(status)
	}
	return fmt.Sprintf("HTTP %d: %s", status, truncate(message, 360))
}

func errorMessageFromPayload(payload map[string]any) string {
	if detail, _ := payload["detail"].(string); detail != "" {
		return detail
	}
	if message, _ := payload["message"].(string); message != "" {
		return message
	}
	if errValue, ok := payload["error"].(string); ok && errValue != "" {
		return errValue
	}
	if errMap, ok := payload["error"].(map[string]any); ok {
		for _, key := range []string{"message", "detail", "code", "type"} {
			if value, _ := errMap[key].(string); value != "" {
				return value
			}
		}
	}
	raw, _ := json.Marshal(payload)
	return string(raw)
}

func readProviderActionInput(r *http.Request) providerActionInput {
	input := providerActionInput{
		ProviderID: strings.TrimSpace(firstNonEmpty(r.URL.Query().Get("provider_id"), r.URL.Query().Get("id"))),
		Model:      strings.TrimSpace(r.URL.Query().Get("model")),
		Kind:       strings.TrimSpace(r.URL.Query().Get("kind")),
		Apply:      isTruthy(r.URL.Query().Get("apply")),
	}
	if r.Method == http.MethodGet {
		return input
	}
	if strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		var body providerActionInput
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err == nil {
			if body.ProviderID != "" {
				input.ProviderID = strings.TrimSpace(body.ProviderID)
			}
			if body.Model != "" {
				input.Model = strings.TrimSpace(body.Model)
			}
			if body.Kind != "" {
				input.Kind = strings.TrimSpace(body.Kind)
			}
			if body.Apply {
				input.Apply = true
			}
		}
		return input
	}
	_ = r.ParseForm()
	if value := strings.TrimSpace(firstNonEmpty(r.FormValue("provider_id"), r.FormValue("id"))); value != "" {
		input.ProviderID = value
	}
	if value := strings.TrimSpace(r.FormValue("model")); value != "" {
		input.Model = value
	}
	if value := strings.TrimSpace(r.FormValue("kind")); value != "" {
		input.Kind = value
	}
	if isTruthy(r.FormValue("apply")) {
		input.Apply = true
	}
	return input
}

func modelIDs(models []upstream.ModelInfo) []string {
	ids := make([]string, 0, len(models))
	for _, model := range models {
		if strings.TrimSpace(model.ID) != "" {
			ids = append(ids, model.ID)
		}
	}
	return store.NormalizeModels(ids)
}

func firstCodexProviderID(settings store.Settings, providers []store.Provider) string {
	if strings.TrimSpace(settings.DefaultCodexID) != "" {
		return strings.TrimSpace(settings.DefaultCodexID)
	}
	for _, provider := range providers {
		if provider.Type == store.ProviderCodex {
			return provider.ID
		}
	}
	return "codex"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func isTruthy(value string) bool {
	value = strings.TrimSpace(strings.ToLower(value))
	return value == "1" || value == "true" || value == "on" || value == "yes"
}

func truncate(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "..."
}
