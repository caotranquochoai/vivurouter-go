package dashboard

import (
	"testing"

	"github.com/local/vivurouter-go/internal/store"
)

func TestParseFusionTraceView(t *testing.T) {
	log := store.RequestLog{FusionTrace: `{"experts":[{"expert_name":"Coder","target":"p/model","role":"code","success":true,"content":"answer","duration_ms":123,"prompt_tokens":10,"output_tokens":20}],"synthesizer":{"name":"synthesizer","target":"p/synth","success":true,"content":"summary","duration_ms":456,"prompt_tokens":30,"output_tokens":40},"reviewer":{"name":"reviewer","target":"p/review","success":false,"error":"bad","duration_ms":789,"prompt_tokens":50,"output_tokens":0},"synthesis":"summary","final":"final answer"}`}
	view := parseFusionTraceView(log)
	if view == nil || !view.HasDetails() {
		t.Fatal("expected parsed fusion trace")
	}
	if len(view.Experts) != 1 {
		t.Fatalf("experts = %d, want 1", len(view.Experts))
	}
	expert := view.Experts[0]
	if expert.Name != "Coder" || expert.Target != "p/model" || !expert.Success || expert.DurationMS != 123 || expert.PromptTokens != 10 || expert.OutputTokens != 20 {
		t.Fatalf("unexpected expert: %+v", expert)
	}
	if view.SynthesisPreview != "summary" || view.FinalPreview != "final answer" {
		t.Fatalf("unexpected previews: %+v", view)
	}
	if view.Synthesizer == nil || view.Synthesizer.Target != "p/synth" || view.Synthesizer.PromptTokens != 30 || view.Synthesizer.OutputTokens != 40 {
		t.Fatalf("unexpected synthesizer: %+v", view.Synthesizer)
	}
	if view.Reviewer == nil || view.Reviewer.Target != "p/review" || view.Reviewer.Success || view.Reviewer.Error != "bad" {
		t.Fatalf("unexpected reviewer: %+v", view.Reviewer)
	}
}

func TestParseFusionTraceViewInvalid(t *testing.T) {
	if got := parseFusionTraceView(store.RequestLog{FusionTrace: `{bad json`}); got != nil {
		t.Fatalf("invalid trace parsed as %+v", got)
	}
}
