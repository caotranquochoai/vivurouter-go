package dashboard

import (
	"testing"
	"time"

	"github.com/local/vivurouter-go/internal/store"
)

func TestBuildUsageSeries24hBuckets(t *testing.T) {
	now := time.Date(2026, 6, 10, 15, 35, 0, 0, time.UTC)
	logs := []store.RequestLog{
		{Timestamp: time.Date(2026, 6, 10, 14, 5, 0, 0, time.UTC), Status: "200", PromptTokens: 100, CompletionTokens: 25, TotalTokens: 125, CostUSD: 0.01},
		{Timestamp: time.Date(2026, 6, 10, 14, 45, 0, 0, time.UTC), Status: "500", PromptTokens: 20, CompletionTokens: 5, TotalTokens: 25, CostUSD: 0.02, EstimatedTokens: true},
		{Timestamp: time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC), Status: "200", TotalTokens: 999, CostUSD: 9},
	}

	series := buildUsageSeries(logs, "24h", now)
	if series.Range != "24h" || series.Bucket != "hour" || len(series.Buckets) != 24 {
		t.Fatalf("unexpected series metadata: %+v", series)
	}
	if series.Totals.Requests != 2 || series.Totals.TotalTokens != 150 || series.Totals.Estimated != 1 {
		t.Fatalf("unexpected totals: %+v", series.Totals)
	}

	bucketIndex := -1
	for i, bucket := range series.Buckets {
		if bucket.Start.Equal(time.Date(2026, 6, 10, 14, 0, 0, 0, time.UTC)) {
			bucketIndex = i
			break
		}
	}
	if bucketIndex < 0 {
		t.Fatal("14:00 bucket not found")
	}
	bucket := series.Buckets[bucketIndex]
	if bucket.Requests != 2 || bucket.Errors != 1 || bucket.TotalTokens != 150 || bucket.CostUSD != 0.03 {
		t.Fatalf("unexpected 14:00 bucket: %+v", bucket)
	}
}

func TestBuildUsageSeriesDefaultsTo24h(t *testing.T) {
	now := time.Date(2026, 6, 10, 15, 0, 0, 0, time.UTC)
	series := buildUsageSeries(nil, "bogus", now)
	if series.Range != "24h" || len(series.Buckets) != 24 {
		t.Fatalf("unexpected default range: %+v", series)
	}
}

func TestBuildBudgetStatus(t *testing.T) {
	now := time.Date(2026, 6, 10, 15, 0, 0, 0, time.UTC)
	settings := store.Settings{DailyBudgetUSD: 1, MonthlyBudgetUSD: 10, BudgetAlertPct: 80}
	logs := []store.RequestLog{
		{Timestamp: time.Date(2026, 6, 10, 1, 0, 0, 0, time.UTC), CostUSD: 0.85},
		{Timestamp: time.Date(2026, 6, 9, 1, 0, 0, 0, time.UTC), CostUSD: 2.00},
		{Timestamp: time.Date(2026, 5, 31, 23, 59, 0, 0, time.UTC), CostUSD: 99},
	}

	status := buildBudgetStatus(logs, settings, now)
	if !status.HasBudget || !status.Alert {
		t.Fatalf("expected active alert budget: %+v", status)
	}
	if status.DailySpentUSD != 0.85 || status.DailyLevel != "warn" {
		t.Fatalf("unexpected daily budget: %+v", status)
	}
	if status.MonthlySpentUSD != 2.85 || status.MonthlyLevel != "" {
		t.Fatalf("unexpected monthly budget: %+v", status)
	}
}

func TestBudgetLevelOver(t *testing.T) {
	level, pct := budgetLevel(12, 10, 80)
	if level != "over" || pct != 120 {
		t.Fatalf("unexpected budget level: %q %.2f", level, pct)
	}
}
