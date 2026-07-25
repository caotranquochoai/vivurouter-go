package dashboard

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/local/vivurouter-go/internal/store"
)

func requestPageParams(r *http.Request) (int, int) {
	limit := 25
	switch strings.TrimSpace(r.URL.Query().Get("limit")) {
	case "50":
		limit = 50
	case "100":
		limit = 100
	}
	page := 1
	if raw := strings.TrimSpace(r.URL.Query().Get("page")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			page = n
		}
	}
	return page, limit
}

func buildRequestLogViews(logs []store.RequestLog) []requestLogView {
	views := make([]requestLogView, 0, len(logs))
	for _, log := range logs {
		view := requestLogView{RequestLog: log}
		if trace := parseFusionTraceView(log); trace != nil && trace.HasDetails() {
			view.Fusion = trace
		}
		views = append(views, view)
	}
	return views
}

type fusionTracePayload struct {
	Experts     []fusionExpertTracePayload `json:"experts"`
	Synthesizer fusionStageTracePayload    `json:"synthesizer"`
	Reviewer    fusionStageTracePayload    `json:"reviewer"`
	Synthesis   string                     `json:"synthesis"`
	Final       string                     `json:"final"`
}

type fusionStageTracePayload struct {
	Name         string `json:"name"`
	Target       string `json:"target"`
	Success      bool   `json:"success"`
	Error        string `json:"error"`
	Content      string `json:"content"`
	DurationMS   int64  `json:"duration_ms"`
	PromptTokens int    `json:"prompt_tokens"`
	OutputTokens int    `json:"output_tokens"`
}

type fusionExpertTracePayload struct {
	ExpertName   string `json:"expert_name"`
	Target       string `json:"target"`
	Role         string `json:"role"`
	Success      bool   `json:"success"`
	Error        string `json:"error"`
	Content      string `json:"content"`
	DurationMS   int64  `json:"duration_ms"`
	PromptTokens int    `json:"prompt_tokens"`
	OutputTokens int    `json:"output_tokens"`
}

func parseFusionTraceView(log store.RequestLog) *fusionTraceView {
	if strings.TrimSpace(log.FusionTrace) == "" {
		return nil
	}
	var payload fusionTracePayload
	if err := json.Unmarshal([]byte(log.FusionTrace), &payload); err != nil {
		return nil
	}
	view := fusionTraceView{
		SynthesisPreview: truncate(payload.Synthesis, 900),
		FinalPreview:     truncate(payload.Final, 900),
	}
	if stage := fusionStageTraceViewFromPayload(payload.Synthesizer); stage != nil {
		view.Synthesizer = stage
	}
	if stage := fusionStageTraceViewFromPayload(payload.Reviewer); stage != nil {
		view.Reviewer = stage
	}
	for _, item := range payload.Experts {
		name := strings.TrimSpace(item.ExpertName)
		if name == "" {
			name = item.Target
		}
		view.Experts = append(view.Experts, fusionExpertTraceView{
			Name:           name,
			Target:         item.Target,
			Role:           item.Role,
			Success:        item.Success,
			Error:          truncate(item.Error, 500),
			ContentPreview: truncate(item.Content, 900),
			DurationMS:     item.DurationMS,
			PromptTokens:   item.PromptTokens,
			OutputTokens:   item.OutputTokens,
		})
	}
	return &view
}

func fusionStageTraceViewFromPayload(item fusionStageTracePayload) *fusionStageTraceView {
	if strings.TrimSpace(item.Target) == "" && item.DurationMS == 0 && !item.Success && strings.TrimSpace(item.Error) == "" && strings.TrimSpace(item.Content) == "" {
		return nil
	}
	return &fusionStageTraceView{
		Name:           item.Name,
		Target:         item.Target,
		Success:        item.Success,
		Error:          truncate(item.Error, 500),
		ContentPreview: truncate(item.Content, 900),
		DurationMS:     item.DurationMS,
		PromptTokens:   item.PromptTokens,
		OutputTokens:   item.OutputTokens,
	}
}

func paginateRequestLogs(logs []store.RequestLog, page int, limit int) []store.RequestLog {
	if limit <= 0 {
		limit = 25
	}
	if page <= 0 {
		page = 1
	}
	start := (page - 1) * limit
	if start >= len(logs) {
		return []store.RequestLog{}
	}
	end := start + limit
	if end > len(logs) {
		end = len(logs)
	}
	return logs[start:end]
}
