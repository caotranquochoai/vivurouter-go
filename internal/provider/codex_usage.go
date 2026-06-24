package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/local/vivurouter-go/internal/store"
)

const defaultCodexUsageURL = "https://chatgpt.com/backend-api/wham/usage"

// CodexQuotaReport is the normalized quota view used by the dashboard.
type CodexQuotaReport struct {
	ProviderID         string       `json:"provider_id"`
	Plan               string       `json:"plan"`
	LimitReached       bool         `json:"limit_reached"`
	ReviewLimitReached bool         `json:"review_limit_reached"`
	Quotas             []CodexQuota `json:"quotas"`
	Message            string       `json:"message,omitempty"`
	FetchedAt          time.Time    `json:"fetched_at"`
}

// CodexQuota describes one normalized quota bucket, expressed as percentages.
type CodexQuota struct {
	Key       string  `json:"key"`
	Name      string  `json:"name"`
	Used      float64 `json:"used"`
	Total     float64 `json:"total"`
	Remaining float64 `json:"remaining"`
	ResetAt   string  `json:"reset_at,omitempty"`
	Unlimited bool    `json:"unlimited"`
}

// FetchQuota reads Codex quota windows from the same ChatGPT backend used by VivuRouter.
func (e *CodexExecutor) FetchQuota(ctx context.Context, provider store.Provider) (CodexQuotaReport, error) {
	token := providerBearerToken(provider)
	if token == "" {
		return CodexQuotaReport{}, fmt.Errorf("provider %s has no Codex access token", provider.ID)
	}

	url := strings.TrimSpace(os.Getenv("CODEX_USAGE_URL"))
	if url == "" {
		url = defaultCodexUsageURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return CodexQuotaReport{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	client, err := clientForProvider(e.Client, provider)
	if err != nil {
		return CodexQuotaReport{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return CodexQuotaReport{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return CodexQuotaReport{
			ProviderID: provider.ID,
			Message:    fmt.Sprintf("Codex connected. Usage API temporarily unavailable (%d).", resp.StatusCode),
			FetchedAt:  time.Now().UTC(),
		}, nil
	}

	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return CodexQuotaReport{}, err
	}
	report := ParseCodexQuotaPayload(payload)
	report.ProviderID = provider.ID
	report.FetchedAt = time.Now().UTC()
	return report, nil
}

// ParseCodexQuotaPayload normalizes the known Codex quota response shapes.
func ParseCodexQuotaPayload(data map[string]any) CodexQuotaReport {
	normalRateLimit := firstObject(data["rate_limit"], data["rate_limits"])
	if normalRateLimit == nil {
		if byLimitID := objectValue(data["rate_limits_by_limit_id"]); byLimitID != nil {
			normalRateLimit = firstObject(byLimitID["codex"], byLimitID["chat"], byLimitID["default"])
		}
	}
	reviewRateLimit := codexReviewRateLimit(data)

	report := CodexQuotaReport{
		Plan:               firstString(data, "plan_type", "plan"),
		LimitReached:       limitReached(normalRateLimit),
		ReviewLimitReached: limitReached(reviewRateLimit),
	}
	if report.Plan == "" {
		if summary := objectValue(data["summary"]); summary != nil {
			report.Plan = firstString(summary, "plan", "plan_type")
		}
	}
	if report.Plan == "" {
		report.Plan = "unknown"
	}

	report.Quotas = append(report.Quotas, quotaWindows("", normalRateLimit)...)
	report.Quotas = append(report.Quotas, quotaWindows("review", reviewRateLimit)...)
	return report
}

func quotaWindows(prefix string, snapshot map[string]any) []CodexQuota {
	body := codexRateLimitBody(snapshot)
	if body == nil {
		return nil
	}
	out := []CodexQuota{}
	primary := firstObject(body["primary_window"], body["primary"], snapshot["primary_window"], snapshot["primary"])
	secondary := firstObject(body["secondary_window"], body["secondary"], snapshot["secondary_window"], snapshot["secondary"])
	if primary != nil {
		key := "session"
		name := "Session"
		if prefix != "" {
			key = prefix + "_session"
			name = "Review session"
		}
		out = append(out, formatCodexQuota(key, name, primary))
	}
	if secondary != nil {
		key := "weekly"
		name := "Weekly"
		if prefix != "" {
			key = prefix + "_weekly"
			name = "Review weekly"
		}
		out = append(out, formatCodexQuota(key, name, secondary))
	}
	return out
}

func codexRateLimitBody(snapshot map[string]any) map[string]any {
	if snapshot == nil {
		return nil
	}
	if rateLimit := objectValue(snapshot["rate_limit"]); rateLimit != nil {
		return rateLimit
	}
	return snapshot
}

func codexReviewRateLimit(data map[string]any) map[string]any {
	if review := firstObject(data["code_review_rate_limit"], data["review_rate_limit"]); review != nil {
		return review
	}
	if byLimitID := objectValue(data["rate_limits_by_limit_id"]); byLimitID != nil {
		if review := firstObject(byLimitID["code_review"], byLimitID["codex_review"], byLimitID["review"]); review != nil {
			return review
		}
	}
	if additional, ok := data["additional_rate_limits"].([]any); ok {
		for _, item := range additional {
			entry := objectValue(item)
			if entry == nil {
				continue
			}
			id := strings.ToLower(firstString(entry, "limit_name", "metered_feature", "id", "name"))
			if id == "code_review" || id == "codex_review" || id == "review" || strings.Contains(id, "review") {
				return entry
			}
		}
	}
	return nil
}

func formatCodexQuota(key string, name string, window map[string]any) CodexQuota {
	used, ok := finiteValue(window["used_percent"], window["percent_used"], window["usage_percent"], window["usedPercentage"])
	remaining, hasRemaining := finiteValue(window["remaining"], window["remaining_percent"], window["remaining_percentage"], window["remainingPercentage"])
	if !ok && hasRemaining {
		used = 100 - remaining
		ok = true
	}
	if !ok {
		used = 0
	}
	used = math.Max(0, math.Min(100, used))
	remaining = math.Max(0, 100-used)
	return CodexQuota{
		Key:       key,
		Name:      name,
		Used:      used,
		Total:     100,
		Remaining: remaining,
		ResetAt:   parseResetTimeAny(firstNonNil(window["reset_at"], window["resets_at"], window["resetAt"], window["reset_time"])),
		Unlimited: false,
	}
}

func limitReached(snapshot map[string]any) bool {
	body := codexRateLimitBody(snapshot)
	if body == nil {
		return false
	}
	return boolValue(body["limit_reached"]) || boolValue(body["limitReached"])
}

func objectValue(value any) map[string]any {
	m, _ := value.(map[string]any)
	return m
}

func firstObject(values ...any) map[string]any {
	for _, value := range values {
		if object := objectValue(value); object != nil {
			return object
		}
	}
	return nil
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func firstString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(asString(m[key])); value != "" {
			return value
		}
	}
	return ""
}

func finiteValue(values ...any) (float64, bool) {
	for _, value := range values {
		switch v := value.(type) {
		case float64:
			if isFinite(v) {
				return v, true
			}
		case int:
			return float64(v), true
		case json.Number:
			if parsed, err := v.Float64(); err == nil && isFinite(parsed) {
				return parsed, true
			}
		case string:
			var parsed float64
			if _, err := fmt.Sscanf(strings.TrimSpace(v), "%f", &parsed); err == nil && isFinite(parsed) {
				return parsed, true
			}
		}
	}
	return 0, false
}

func isFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func boolValue(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(v, "true") || v == "1"
	default:
		return false
	}
}

func parseResetTimeAny(value any) string {
	if value == nil {
		return ""
	}
	switch v := value.(type) {
	case float64:
		if v <= 0 {
			return ""
		}
		if v < 1e12 {
			v *= 1000
		}
		return time.UnixMilli(int64(v)).UTC().Format(time.RFC3339)
	case json.Number:
		if parsed, err := v.Float64(); err == nil {
			return parseResetTimeAny(parsed)
		}
	case string:
		value := strings.TrimSpace(v)
		if value == "" {
			return ""
		}
		if number, ok := finiteValue(value); ok {
			return parseResetTimeAny(number)
		}
		if t, err := time.Parse(time.RFC3339, value); err == nil {
			return t.UTC().Format(time.RFC3339)
		}
		if t, err := time.Parse(time.RFC3339Nano, value); err == nil {
			return t.UTC().Format(time.RFC3339)
		}
		return value
	}
	return ""
}
