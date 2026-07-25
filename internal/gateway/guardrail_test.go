package gateway

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/local/vivurouter-go/internal/store"
)

func TestApplyGuardrailPatchesPreservesStructuredFields(t *testing.T) {
	body := map[string]any{
		"model": "guarded",
		"messages": []any{
			map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": "verbose request"}, map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,abc"}}}},
			map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"id": "call-1", "function": map[string]any{"name": "lookup", "arguments": "{\"x\":1}"}}}},
			map[string]any{"role": "tool", "tool_call_id": "call-1", "content": "large tool result"},
		},
		"tools": []any{map[string]any{"type": "function", "function": map[string]any{"name": "lookup", "parameters": map[string]any{"type": "object"}}}},
	}
	before, _ := json.Marshal(body)
	segments := editableGuardrailSegments(body)
	patched, err := applyGuardrailPatches(body, guardrailPatchEnvelope{Version: 1, Patches: []guardrailPatch{{Locator: "messages/0/content/0/text", Replacement: "compact request"}}}, segments, 10, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if got := asStringLocal(asMap(anySlice(asMap(anySlice(patched["messages"])[0])["content"])[0])["text"]); got != "compact request" {
		t.Fatalf("replacement = %q", got)
	}
	afterOriginal, _ := json.Marshal(body)
	if string(before) != string(afterOriginal) {
		t.Fatal("original request was mutated")
	}
	if !reflect.DeepEqual(body["tools"], patched["tools"]) {
		t.Fatal("tool schema changed")
	}
	if !reflect.DeepEqual(anySlice(body["messages"])[1:], anySlice(patched["messages"])[1:]) {
		t.Fatal("tool call/result changed")
	}
}

func TestApplyGuardrailPatchesRejectsAtomically(t *testing.T) {
	body := map[string]any{"messages": []any{map[string]any{"role": "user", "content": "original"}}}
	segments := editableGuardrailSegments(body)
	_, err := applyGuardrailPatches(body, guardrailPatchEnvelope{Version: 1, Patches: []guardrailPatch{{Locator: "messages/0/content", Replacement: "changed"}, {Locator: "tools/0", Replacement: "bad"}}}, segments, 10, 1024)
	if err == nil {
		t.Fatal("expected invalid locator error")
	}
	if got := asStringLocal(asMap(anySlice(body["messages"])[0])["content"]); got != "original" {
		t.Fatalf("original mutated to %q", got)
	}
}

func TestGuardrailTargetAllowedRejectsVirtualTargets(t *testing.T) {
	settings := store.Settings{
		Combos:        []store.Combo{{Name: "combo", Enabled: true, Models: []string{"p/m"}}},
		PromptRouters: []store.PromptRouter{{Name: "router", Enabled: true, ClassifierModel: "p/m", Routes: []store.PromptRoute{{Role: "default", Target: "p/m"}}}},
		Fusions:       []store.Fusion{{Name: "fusion", Enabled: true, Experts: []store.FusionExpert{{Target: "p/m", Enabled: true}}}},
		Guardrails:    []store.Guardrail{{Name: "guard", Enabled: true, MainTarget: "p/m", ValidatorTarget: "p/m"}},
	}
	if !guardrailTargetAllowed("combo", settings) || !guardrailTargetAllowed("p/m", settings) {
		t.Fatal("concrete and combo targets should be allowed")
	}
	for _, target := range []string{"router", "fusion", "guard"} {
		if guardrailTargetAllowed(target, settings) {
			t.Fatalf("virtual target %q was allowed", target)
		}
	}
}
