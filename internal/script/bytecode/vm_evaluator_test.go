package bytecode

import (
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/internal/script/frontend"
)

func TestVMEvaluator_StreamSessionStateAfterFinal(t *testing.T) {
	prog, err := frontend.ParseModuleForTest("stream fun emit(): int { yield 1 return 2 }")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	eval := NewVMEvaluator()
	if err := eval.CompileProgram(prog); err != nil {
		t.Fatalf("CompileProgram: %v", err)
	}

	if _, err := eval.Evaluate("emit", binding.InvocationStageNext, nil); err != nil {
		t.Fatalf("next: %v", err)
	}
	key := streamSessionKey("emit", nil)
	session, ok := eval.sessions[key]
	if !ok {
		t.Fatalf("expected session key %q after next, got keys %+v", key, eval.sessions)
	}
	if session.exhausted {
		t.Fatal("session should not be exhausted after next")
	}

	if _, err := eval.Evaluate("emit", binding.InvocationStageFinal, nil); err != nil {
		t.Fatalf("final: %v", err)
	}
	session, ok = eval.sessions[key]
	if !ok {
		t.Fatalf("expected session key %q after final", key)
	}
	if !session.exhausted {
		t.Fatalf("expected session exhausted after final, got %+v", session)
	}

	_, err = eval.Evaluate("emit", binding.InvocationStageNext, nil)
	if err == nil {
		t.Fatal("expected next after exhausted session to fail")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "stream_exhausted" {
		t.Fatalf("expected stream_exhausted diagnostic, got %v", err)
	}
}
