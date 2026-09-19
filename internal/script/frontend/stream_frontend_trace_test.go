package frontend_test

import (
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/internal/script/bytecode"
	"github.com/qomos-w/spore/internal/script/frontend"
)

func TestVMFrontend_StreamContractMatrix(t *testing.T) {
	f := newVMFrontend(t)
	if err := f.LoadSource(`stream fun emit(): int { yield 4 return 5 }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	t.Run("invalid_unary_stage", func(t *testing.T) {
		_, err := f.Invoke("emit", nil)
		if err == nil {
			t.Fatal("expected invalid unary stage error")
		}
		coder, ok := err.(interface{ DiagnosticCode() string })
		if !ok || coder.DiagnosticCode() != "invalid_invocation_stage" {
			t.Fatalf("expected invalid_invocation_stage, got %v", err)
		}
	})

	t.Run("final_before_next_returns_final_payload", func(t *testing.T) {
		outcome, err := f.InvokeStage("emit", binding.InvocationStageFinal, nil)
		if err != nil {
			t.Fatalf("final before next: %v", err)
		}
		if outcome.Result.Kind != binding.InvocationResultValue {
			t.Fatalf("expected value result, got %s", outcome.Result.Kind)
		}
		if outcome.Payload == nil || outcome.Payload.Value != 5 {
			t.Fatalf("expected final payload 5, got %+v", outcome.Payload)
		}
	})

	f = newVMFrontend(t)
	if err := f.LoadSource(`stream fun emit(): int { yield 4 return 5 }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	t.Run("next_after_final", func(t *testing.T) {
		first, err := f.InvokeStage("emit", binding.InvocationStageNext, nil)
		if err != nil {
			t.Fatalf("first next: %v", err)
		}
		if first.Payload == nil || first.Payload.Value != 4 {
			t.Fatalf("expected first yield payload 4, got %+v", first.Payload)
		}
		final, err := f.InvokeStage("emit", binding.InvocationStageFinal, nil)
		if err != nil {
			t.Fatalf("final: %v", err)
		}
		if final.Payload == nil || final.Payload.Value != 5 {
			t.Fatalf("expected final payload 5, got %+v", final.Payload)
		}
		outcome, err := f.InvokeStage("emit", binding.InvocationStageNext, nil)
		if err != nil {
			t.Fatalf("next after final: %v", err)
		}
		if outcome.Result.Kind != binding.InvocationResultError {
			t.Fatalf("expected exhausted error result, got %s", outcome.Result.Kind)
		}
		if outcome.Result.Error == nil || outcome.Result.Error.DiagnosticCode != "stream_exhausted" {
			t.Fatalf("expected stream_exhausted, got %+v", outcome.Result.Error)
		}
	})

	f = newVMFrontend(t)
	if err := f.LoadSource(`stream fun emit(): int { yield 4 return 5 }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	t.Run("repeated_final", func(t *testing.T) {
		_, err := f.InvokeStage("emit", binding.InvocationStageNext, nil)
		if err != nil {
			t.Fatalf("first next: %v", err)
		}
		_, err = f.InvokeStage("emit", binding.InvocationStageFinal, nil)
		if err != nil {
			t.Fatalf("first final: %v", err)
		}
		outcome, err := f.InvokeStage("emit", binding.InvocationStageFinal, nil)
		if err != nil {
			t.Fatalf("second final: %v", err)
		}
		if outcome.Result.Kind != binding.InvocationResultError {
			t.Fatalf("expected exhausted error result, got %s", outcome.Result.Kind)
		}
		if outcome.Result.Error == nil || outcome.Result.Error.DiagnosticCode != "stream_exhausted" {
			t.Fatalf("expected stream_exhausted, got %+v", outcome.Result.Error)
		}
	})
}

func TestFrontendStreamPipeline_NextAfterFinalExhaustedState(t *testing.T) {
	sb := binding.NewScriptBinding()
	f, err := frontend.New(sb)
	if err != nil {
		t.Fatalf("frontend.New: %v", err)
	}
	vmEval := bytecode.NewVMEvaluator()
	f.SetVMCompileHook(vmEval)

	if err := f.LoadSource("stream fun emit(): int { yield 1 return 2 }"); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	first, err := f.InvokeStage("emit", binding.InvocationStageNext, nil)
	if err != nil {
		t.Fatalf("first next: %v", err)
	}
	if first.Payload == nil || first.Payload.Value != 1 {
		t.Fatalf("expected first next payload 1, got %+v", first.Payload)
	}

	final, err := f.InvokeStage("emit", binding.InvocationStageFinal, nil)
	if err != nil {
		t.Fatalf("final: %v", err)
	}
	if final.Payload == nil || final.Payload.Value != 2 {
		t.Fatalf("expected final payload 2, got %+v", final.Payload)
	}

	key := "emit|"
	session, ok := vmEval.SessionsForTest()[key]
	if !ok {
		t.Fatalf("expected evaluator session %q after final", key)
	}
	if !session.ExhaustedForTest() {
		t.Fatalf("expected evaluator session exhausted after final, got %+v", session)
	}

	outcome, err := f.InvokeStage("emit", binding.InvocationStageNext, nil)
	if err != nil {
		t.Fatalf("next after final: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultError {
		t.Fatalf("expected exhausted error result, got %s", outcome.Result.Kind)
	}
	if outcome.Result.Error == nil || outcome.Result.Error.DiagnosticCode != "stream_exhausted" {
		t.Fatalf("expected stream_exhausted envelope, got %+v", outcome.Result.Error)
	}
}

func TestVMFrontend_StreamScenarioBusinessProgression(t *testing.T) {
	f := newVMFrontend(t)
	source := `
stream fun process(): int {
  var total: int = 2
  yield total
  total = total * 3
  yield total
  return total + 1
}`
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	first, err := f.InvokeStage("process", binding.InvocationStageNext, nil)
	if err != nil {
		t.Fatalf("InvokeStage next #1: %v", err)
	}
	if first.Payload == nil || first.Payload.Value != 2 {
		t.Fatalf("expected first yield payload 2, got %+v", first.Payload)
	}

	second, err := f.InvokeStage("process", binding.InvocationStageNext, nil)
	if err != nil {
		t.Fatalf("InvokeStage next #2: %v", err)
	}
	if second.Payload == nil || second.Payload.Value != 6 {
		t.Fatalf("expected second yield payload 6, got %+v", second.Payload)
	}

	final, err := f.InvokeStage("process", binding.InvocationStageFinal, nil)
	if err != nil {
		t.Fatalf("InvokeStage final: %v", err)
	}
	if final.Payload == nil || final.Payload.Value != 7 {
		t.Fatalf("expected final payload 7, got %+v", final.Payload)
	}
}
