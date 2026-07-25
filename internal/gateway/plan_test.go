package gateway

import (
	"testing"

	"github.com/local/vivurouter-go/internal/store"
)

func TestPlanCandidatesRejectsUnsupportedProtocol(t *testing.T) {
	settings := store.Settings{}
	candidates := []resolvedModel{{Provider: store.Provider{ID: "openai"}, Model: "m"}}
	plan := planCandidates("m", RequestRequirements{Protocol: ProtocolResponses}, candidates, settings)
	if len(plan.Candidates) != 0 || len(plan.Skipped) != 1 || plan.Skipped[0].Reason != "protocol_unsupported" {
		t.Fatalf("unexpected plan: %+v", plan)
	}
}

func TestPlanCandidatesRejectsContextExceedingCandidate(t *testing.T) {
	settings := store.Settings{ModelPrices: []store.ModelPriceRule{{ProviderID: "p1", Model: "m", ContextLength: 10}}}
	candidates := []resolvedModel{{Provider: store.Provider{ID: "p1"}, Model: "m"}}
	plan := planCandidates("m", RequestRequirements{Protocol: ProtocolOpenAI, EstimatedContext: 13}, candidates, settings)
	if len(plan.Candidates) != 0 || len(plan.Skipped) != 1 || plan.Skipped[0].Reason != "context_limit" {
		t.Fatalf("unexpected plan: %+v", plan)
	}
}

func TestPlanCandidatesAllowsUnknownModelBeyondDisplayDefault(t *testing.T) {
	candidates := []resolvedModel{{Provider: store.Provider{ID: "custom"}, Model: "custom/unknown-model"}}
	plan := planCandidates("custom/unknown-model", RequestRequirements{Protocol: ProtocolOpenAI, EstimatedContext: defaultContextLength + 1}, candidates, store.Settings{})
	if len(plan.Candidates) != 1 || len(plan.Skipped) != 0 {
		t.Fatalf("unknown model should remain permissive: %+v", plan)
	}
	if plan.Candidates[0].Capability.MaxContext != 0 {
		t.Fatalf("unknown model hard context limit = %d, want 0", plan.Candidates[0].Capability.MaxContext)
	}
}

func TestPlanCandidatesRejectsKnownModelBeyondContextLimit(t *testing.T) {
	candidates := []resolvedModel{{Provider: store.Provider{ID: "openai"}, Model: "gpt-4o"}}
	plan := planCandidates("gpt-4o", RequestRequirements{Protocol: ProtocolOpenAI, EstimatedContext: 160001}, candidates, store.Settings{})
	if len(plan.Candidates) != 0 || len(plan.Skipped) != 1 || plan.Skipped[0].Reason != "context_limit" {
		t.Fatalf("unexpected plan: %+v", plan)
	}
}

func TestExtractRequirementsDetectsToolsVisionAndJSON(t *testing.T) {
	body := map[string]any{
		"stream":          true,
		"tools":           []any{map[string]any{"type": "function"}},
		"response_format": map[string]any{"type": "json_schema"},
		"messages":        []any{map[string]any{"content": []any{map[string]any{"type": "image_url", "image_url": "https://example.com/image.png"}}}},
	}
	req := extractRequirements(ProtocolOpenAI, body)
	if !req.Stream || !req.NeedsTools || !req.NeedsVision || !req.NeedsJSONMode {
		t.Fatalf("requirements not detected: %+v", req)
	}
}
