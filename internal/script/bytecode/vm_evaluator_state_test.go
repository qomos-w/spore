package bytecode

import (
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/internal/script/frontend"
)

func TestVMEvaluator_StreamStateAcrossNextFinalNext(t *testing.T) {
	prog, err := frontend.ParseModuleForTest("stream fun emit(): int { yield 1 return 2 }")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	eval := NewVMEvaluator()
	if err := eval.CompileProgram(prog); err != nil {
		t.Fatalf("CompileProgram: %v", err)
	}

	key := streamSessionKey("emit", nil)
	if _, ok := eval.sessions[key]; ok {
		t.Fatal("expected no session before next")
	}

	if _, err := eval.Evaluate("emit", binding.InvocationStageNext, nil); err != nil {
		t.Fatalf("next: %v", err)
	}
	state, ok := eval.sessions[key]
	if !ok {
		t.Fatal("expected session after next")
	}
	if state.exhausted {
		t.Fatal("expected non-exhausted session after next")
	}

	if _, err := eval.Evaluate("emit", binding.InvocationStageFinal, nil); err != nil {
		t.Fatalf("final: %v", err)
	}
	state, ok = eval.sessions[key]
	if !ok {
		t.Fatal("expected session after final")
	}
	if !state.exhausted {
		t.Fatal("expected exhausted session after final")
	}

	_, err = eval.Evaluate("emit", binding.InvocationStageNext, nil)
	if err == nil {
		t.Fatal("expected stream_exhausted on next after final")
	}
}
