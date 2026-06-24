package dashboard

import (
	"strings"
	"time"

	"github.com/local/vivurouter-go/internal/store"
)

// UsageBucket is one point in a usage time series.
type UsageBucket struct {
	Start            time.Time `json:"start"`
	Label            string    `json:"label"`
	Requests         int       `json:"requests"`
	Errors           int       `json:"errors"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	TotalTokens      int       `json:"total_tokens"`
	CostUSD          float64   `json:"cost_usd"`
}

// UsageSeries is a bucketed view of request logs over a time range.
type UsageSeries struct {
	Range   string        `json:"range"`
	Bucket  string        `json:"bucket"` // "hour" or "day"
	Buckets []UsageBucket `json:"buckets"`
	Totals  UsageCounter  `json:"totals"`
}

// usageRanges lists the time ranges the dashboard chart supports.
var usageRanges = map[string]bool{"today": true, "24h": true, "7d": true, "30d": true}

func normalizeUsageRange(rangeKey string) string {
	rangeKey = strings.ToLower(strings.TrimSpace(rangeKey))
	if usageRanges[rangeKey] {
		return rangeKey
	}
	return "24h"
}

// buildUsageSeries buckets request logs into a fixed number of equal time
// buckets ending at now. Hourly buckets are used for "today"/"24h" and daily
// buckets for "7d"/"30d". now and log timestamps are compared as absolute
// instants, so callers may pass either UTC or local time.
func buildUsageSeries(logs []store.RequestLog, rangeKey string, now time.Time) UsageSeries {
	rangeKey = normalizeUsageRange(rangeKey)
	now = now.UTC()

	var start time.Time
	var bucketDur time.Duration
	var count int
	bucketKind := "hour"

	switch rangeKey {
	case "today":
		bucketDur = time.Hour
		count = 24
		start = now.Truncate(24 * time.Hour)
	case "7d":
		bucketDur = 24 * time.Hour
		count = 7
		bucketKind = "day"
		start = now.Truncate(24*time.Hour).AddDate(0, 0, -(count - 1))
	case "30d":
		bucketDur = 24 * time.Hour
		count = 30
		bucketKind = "day"
		start = now.Truncate(24*time.Hour).AddDate(0, 0, -(count - 1))
	default: // 24h
		bucketDur = time.Hour
		count = 24
		start = now.Add(-time.Duration(count-1) * time.Hour).Truncate(time.Hour)
	}

	series := UsageSeries{Range: rangeKey, Bucket: bucketKind, Buckets: make([]UsageBucket, count)}
	for i := 0; i < count; i++ {
		bucketStart := start.Add(time.Duration(i) * bucketDur)
		series.Buckets[i] = UsageBucket{Start: bucketStart, Label: bucketLabel(bucketStart, bucketKind)}
	}

	for _, log := range logs {
		ts := log.Timestamp.UTC()
		if ts.Before(start) {
			continue
		}
		idx := int(ts.Sub(start) / bucketDur)
		if idx < 0 || idx >= count {
			continue
		}
		b := &series.Buckets[idx]
		b.Requests++
		if !isSuccessStatus(log.Status) {
			b.Errors++
		}
		b.PromptTokens += log.PromptTokens
		b.CompletionTokens += log.CompletionTokens
		b.TotalTokens += log.TotalTokens
		b.CostUSD += log.CostUSD

		series.Totals.Requests++
		series.Totals.PromptTokens += log.PromptTokens
		series.Totals.CompletionTokens += log.CompletionTokens
		series.Totals.TotalTokens += log.TotalTokens
		series.Totals.CachedTokens += log.CachedTokens
		series.Totals.ReasoningTokens += log.ReasoningTokens
		series.Totals.CostUSD += log.CostUSD
		if log.EstimatedTokens {
			series.Totals.Estimated++
		}
	}
	return series
}

func bucketLabel(start time.Time, kind string) string {
	if kind == "day" {
		return start.Format("01-02")
	}
	return start.Format("15:04")
}

// BudgetStatus summarizes spend against the configured daily/monthly budgets.
type BudgetStatus struct {
	HasBudget        bool    `json:"has_budget"`
	AlertThreshold   int     `json:"alert_threshold"`
	Alert            bool    `json:"alert"`
	DailyBudgetUSD   float64 `json:"daily_budget_usd"`
	DailySpentUSD    float64 `json:"daily_spent_usd"`
	DailyPct         float64 `json:"daily_pct"`
	DailyLevel       string  `json:"daily_level"`
	MonthlyBudgetUSD float64 `json:"monthly_budget_usd"`
	MonthlySpentUSD  float64 `json:"monthly_spent_usd"`
	MonthlyPct       float64 `json:"monthly_pct"`
	MonthlyLevel     string  `json:"monthly_level"`
}

// buildBudgetStatus sums cost since the start of the current UTC day and month
// and compares it against the configured budgets. A zero budget means the
// matching window is not tracked.
func buildBudgetStatus(logs []store.RequestLog, settings store.Settings, now time.Time) BudgetStatus {
	now = now.UTC()
	dayStart := now.Truncate(24 * time.Hour)
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	status := BudgetStatus{
		AlertThreshold:   settings.BudgetAlertPct,
		DailyBudgetUSD:   settings.DailyBudgetUSD,
		MonthlyBudgetUSD: settings.MonthlyBudgetUSD,
	}
	if status.AlertThreshold <= 0 {
		status.AlertThreshold = 80
	}

	for _, log := range logs {
		ts := log.Timestamp.UTC()
		if !ts.Before(dayStart) {
			status.DailySpentUSD += log.CostUSD
		}
		if !ts.Before(monthStart) {
			status.MonthlySpentUSD += log.CostUSD
		}
	}

	status.DailyLevel, status.DailyPct = budgetLevel(status.DailySpentUSD, status.DailyBudgetUSD, status.AlertThreshold)
	status.MonthlyLevel, status.MonthlyPct = budgetLevel(status.MonthlySpentUSD, status.MonthlyBudgetUSD, status.AlertThreshold)
	status.HasBudget = status.DailyBudgetUSD > 0 || status.MonthlyBudgetUSD > 0
	status.Alert = status.DailyLevel == "warn" || status.DailyLevel == "over" ||
		status.MonthlyLevel == "warn" || status.MonthlyLevel == "over"
	return status
}

// budgetLevel returns the alert level ("", "warn", "over") and used percentage
// for a spend against a budget. An unset budget (<=0) yields an empty level.
func budgetLevel(spent, budget float64, threshold int) (string, float64) {
	if budget <= 0 {
		return "", 0
	}
	pct := spent / budget * 100
	level := ""
	switch {
	case pct >= 100:
		level = "over"
	case pct >= float64(threshold):
		level = "warn"
	}
	return level, pct
}
