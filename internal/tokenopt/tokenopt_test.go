package tokenopt

import (
	"strings"
	"testing"
)

func TestEstimateTokens(t *testing.T) {
	if got := EstimateTokens("abcd"); got != 1 {
		t.Fatalf("EstimateTokens(abcd) = %d, want 1", got)
	}
	if got := EstimateTokens("abcde"); got != 2 {
		t.Fatalf("EstimateTokens(abcde) = %d, want 2", got)
	}
}

func TestCompactTextKeepsImportantLines(t *testing.T) {
	input := strings.Repeat("noise line\n", 900) + "ERROR failed to build package\n" + strings.Repeat("more noise\n", 900)
	res := CompactText(input, Options{MinChars: 1000, MaxChars: 2500, PreserveErrors: true})
	if !res.Applied {
		t.Fatalf("expected compaction to apply: %+v", res)
	}
	if !strings.Contains(res.Text, "ERROR failed to build package") {
		t.Fatalf("important line missing from compacted text")
	}
	if res.EstimatedSavedTokens <= 0 {
		t.Fatalf("expected positive estimated savings")
	}
}

func TestCompactLogDeduplicates(t *testing.T) {
	input := strings.Repeat("2026-06-12T00:00:00Z INFO worker completed task id=123\n", 500) + strings.Repeat("2026-06-12T00:00:01Z ERROR worker failed task id=124\n", 10)
	res := CompactLog(input, Options{MinChars: 1000, MaxChars: 3000, PreserveErrors: true})
	if !res.Applied {
		t.Fatalf("expected log compaction to apply: %+v", res)
	}
	if !strings.Contains(res.Text, "x500") {
		t.Fatalf("expected repeated pattern count in output: %s", res.Text)
	}
	if !strings.Contains(strings.ToLower(res.Text), "error") {
		t.Fatalf("expected error lines to be preserved")
	}
}

func TestCompactJSONTruncatesLongValues(t *testing.T) {
	input := `{"items":[{"name":"one","payload":"` + strings.Repeat("x", 5000) + `"},{"name":"two","payload":"` + strings.Repeat("y", 5000) + `"}]}`
	res := CompactJSON(input, Options{MinChars: 1000, MaxChars: 2500})
	if !res.Applied {
		t.Fatalf("expected json compaction to apply: %+v", res)
	}
	if !strings.Contains(res.Text, "token-optimized JSON") {
		t.Fatalf("expected JSON marker")
	}
	if strings.Contains(res.Text, strings.Repeat("x", 1000)) {
		t.Fatalf("long value was not truncated")
	}
}

func TestCompactToolResultRoutesJSON(t *testing.T) {
	input := `{"payload":"` + strings.Repeat("x", 8000) + `"}`
	res := CompactToolResult(input, Options{MinChars: 1000, MaxChars: 2000})
	if !res.Applied || res.Reason != "json compacted" {
		t.Fatalf("expected json compact result, got %+v", res)
	}
}
