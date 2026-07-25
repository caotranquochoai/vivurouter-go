package gateway

import (
	"strings"

	"github.com/local/vivurouter-go/internal/store"
)

// Protocol identifies the client-facing request contract while adapters retain
// their protocol-specific serializers and translators.
type Protocol string

const (
	ProtocolOpenAI    Protocol = "openai_chat"
	ProtocolAnthropic Protocol = "anthropic_messages"
	ProtocolResponses Protocol = "responses"
)

const contextEstimateTolerance = 1.25

// routing. It deliberately coexists with the original body map to avoid a
// risky full AST rewrite of tool and multimodal payloads.
type RequestRequirements struct {
	Protocol         Protocol
	Stream           bool
	NeedsTools       bool
	NeedsVision      bool
	NeedsJSONMode    bool
	EstimatedContext int
	RequestedOutput  int
}

// Capability describes the known limits of a provider candidate. Unknown
// metadata remains permissive for existing providers; explicit metadata is used
// only to reject candidates known to be incompatible.
type Capability struct {
	Streaming       bool
	ToolCalls       bool
	Vision          bool
	JSONMode        bool
	MaxContext      int
	MaxOutputTokens int
	Protocols       []Protocol
}

type CandidatePlan struct {
	Candidate  resolvedModel
	Capability Capability
}

type ExecutionPlan struct {
	Model        string
	Requirements RequestRequirements
	Candidates   []CandidatePlan
	Skipped      []CandidateSkip
}

type CandidateSkip struct {
	ProviderID string `json:"provider_id"`
	Model      string `json:"model"`
	Reason     string `json:"reason"`
}

func (p ExecutionPlan) resolvedCandidates() []resolvedModel {
	out := make([]resolvedModel, 0, len(p.Candidates))
	for _, candidate := range p.Candidates {
		out = append(out, candidate.Candidate)
	}
	return out
}

func extractRequirements(protocol Protocol, body map[string]any) RequestRequirements {
	req := RequestRequirements{
		Protocol:        protocol,
		Stream:          bodyStreamRequested(body),
		NeedsTools:      hasNonEmptyField(body, "tools") || hasNonEmptyField(body, "tool_choice"),
		NeedsVision:     bodyNeedsVision(body),
		NeedsJSONMode:   bodyNeedsJSONMode(body),
		RequestedOutput: intFromAny(body["max_tokens"]),
	}
	if req.RequestedOutput == 0 {
		req.RequestedOutput = intFromAny(body["max_output_tokens"])
	}
	req.EstimatedContext = estimateInputTokens(body)
	return req
}

func planCandidates(model string, req RequestRequirements, candidates []resolvedModel, settings store.Settings) ExecutionPlan {
	plan := ExecutionPlan{Model: model, Requirements: req}
	for _, cand := range candidates {
		capability := capabilityForCandidate(cand, settings)
		if reason := capabilityMismatch(req, capability); reason != "" {
			plan.Skipped = append(plan.Skipped, CandidateSkip{ProviderID: cand.Provider.ID, Model: cand.Model, Reason: reason})
			continue
		}
		plan.Candidates = append(plan.Candidates, CandidatePlan{Candidate: cand, Capability: capability})
	}
	return plan
}

func capabilityForCandidate(cand resolvedModel, settings store.Settings) Capability {
	capability := Capability{Streaming: true, ToolCalls: true, Vision: true, JSONMode: true}
	if cand.IsCodex {
		capability.Protocols = []Protocol{ProtocolOpenAI, ProtocolAnthropic, ProtocolResponses}
	} else {
		capability.Protocols = []Protocol{ProtocolOpenAI, ProtocolAnthropic}
	}
	if maxContext, ok := knownContextLengthForModel(cand.Provider.ID, cand.Model, settings); ok {
		capability.MaxContext = maxContext
	}
	return capability
}

func capabilityMismatch(req RequestRequirements, capability Capability) string {
	if !supportsProtocol(capability.Protocols, req.Protocol) {
		return "protocol_unsupported"
	}
	if req.Stream && !capability.Streaming {
		return "stream_unsupported"
	}
	if req.NeedsTools && !capability.ToolCalls {
		return "tools_unsupported"
	}
	if req.NeedsVision && !capability.Vision {
		return "vision_unsupported"
	}
	if req.NeedsJSONMode && !capability.JSONMode {
		return "json_mode_unsupported"
	}
	if capability.MaxContext > 0 && float64(req.EstimatedContext) > float64(capability.MaxContext)*contextEstimateTolerance {
		return "context_limit"
	}
	if capability.MaxOutputTokens > 0 && req.RequestedOutput > capability.MaxOutputTokens {
		return "output_limit"
	}
	return ""
}

func supportsProtocol(protocols []Protocol, want Protocol) bool {
	for _, protocol := range protocols {
		if protocol == want {
			return true
		}
	}
	return false
}

func hasNonEmptyField(body map[string]any, key string) bool {
	value, ok := body[key]
	if !ok || value == nil {
		return false
	}
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text) != "" && strings.ToLower(strings.TrimSpace(text)) != "none"
	}
	return len(anySlice(value)) > 0 || len(asMap(value)) > 0
}

func bodyNeedsJSONMode(body map[string]any) bool {
	format := asMap(body["response_format"])
	kind := strings.ToLower(getString(format, "type"))
	return kind == "json_object" || kind == "json_schema"
}

func bodyNeedsVision(body map[string]any) bool {
	found := false
	walkRequirementValue(body["messages"], func(item map[string]any) {
		typeName := strings.ToLower(getString(item, "type"))
		if strings.Contains(typeName, "image") || item["image_url"] != nil || item["source"] != nil {
			found = true
		}
	})
	return found
}

func walkRequirementValue(value any, visit func(map[string]any)) {
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			walkRequirementValue(item, visit)
		}
	case []map[string]any:
		for _, item := range typed {
			visit(item)
			for _, nested := range item {
				walkRequirementValue(nested, visit)
			}
		}
	case map[string]any:
		visit(typed)
		for _, nested := range typed {
			walkRequirementValue(nested, visit)
		}
	}
}
