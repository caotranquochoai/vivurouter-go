package gateway

import (
	"strings"
	"testing"

	"github.com/local/vivurouter-go/internal/store"
)

func TestBuildDebugPayloadStoresCompactToolResult(t *testing.T) {
	body := map[string]any{
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "tool_result", "content": strings.Repeat("noise line\n", 900) + "ERROR failed to build\n" + strings.Repeat("more noise\n", 900)},
				},
			},
		},
	}
	payload := buildDebugPayload(store.Settings{SaveRawToolResult: true, MaskDebugSecrets: true, CompactDebugPayloads: true, MaxDebugPayloadBytes: 24 * 1024}, body)
	if payload == nil {
		t.Fatal("expected debug payload")
	}
	if !payload.CompactToolApplied {
		t.Fatalf("expected compact tool result to be applied: %+v", payload)
	}
	if payload.EstimatedToolTokensSaved <= 0 {
		t.Fatalf("expected positive token savings")
	}
	if !strings.Contains(payload.CompactToolResult, "ERROR failed to build") {
		t.Fatalf("expected important error line in compact tool result")
	}
}

func TestBuildDebugPayloadStoresCompactPrompt(t *testing.T) {
	body := map[string]any{"messages": []any{map[string]any{"role": "user", "content": strings.Repeat("x", 9000)}}}
	payload := buildDebugPayload(store.Settings{SaveRawPrompt: true, MaskDebugSecrets: true, CompactDebugPayloads: true, MaxDebugPayloadBytes: 24 * 1024}, body)
	if payload == nil {
		t.Fatal("expected debug payload")
	}
	if !payload.CompactPromptApplied {
		t.Fatalf("expected compact prompt to be applied: %+v", payload)
	}
	if payload.EstimatedPromptTokensSaved <= 0 {
		t.Fatalf("expected positive prompt token savings")
	}
}
