package frontend

import (
	"fmt"
	"sync"
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/diagnostics"
	"github.com/qomos-w/spore/schema"
)

type stubRuntimeBackend struct {
	evaluate func(callable string, stage binding.InvocationStage, args []any) (any, error)
	reset    func(callable string, args []any)
}

func (b stubRuntimeBackend) Evaluate(callable string, stage binding.InvocationStage, args []any) (any, error) {
	if b.evaluate == nil {
		return nil, fmt.Errorf("missing evaluate stub")
	}
	return b.evaluate(callable, stage, args)
}

func (b stubRuntimeBackend) Reset(callable string, args []any) {
	if b.reset != nil {
		b.reset(callable, args)
	}
}

type compileOnlyBackend struct{}

func (compileOnlyBackend) CompileProgram(prog *Program) error { return nil }

func TestNewScriptCallableAdapter_RejectsInvalidDesc(t *testing.T) {
	_, err := NewScriptCallableAdapter(schema.CallableDesc{Name: ""}, nil)
	if err == nil {
		t.Fatal("expected error for invalid descriptor")
	}
}

func TestScriptCallableAdapter_WrongStageDiagnosticShapeIsStable(t *testing.T) {
	desc := schema.CallableDesc{
		Name:       "mine",
		Mode:       schema.CallableModeUnary,
		Parameters: []schema.ParameterDesc{{Name: "x", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}}},
		Returns:    []schema.TypeDesc{{Kind: schema.TypeKindScalar, Name: "string"}},
	}
	backend := stubRuntimeBackend{evaluate: func(callable string, stage binding.InvocationStage, args []any) (any, error) {
		return "ok", nil
	}}
	adapter, err := NewScriptCallableAdapter(desc, backend)
	if err != nil {
		t.Fatalf("NewScriptCallableAdapter: %v", err)
	}

	_, err = adapter.Invoke(binding.InvocationRequest{
		Callable: "mine",
		Stage:    binding.InvocationStageNext,
		Args:     []any{int32(1)},
	})
	if err == nil {
		t.Fatal("expected wrong stage error")
	}

	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "invalid_invocation_stage" {
		t.Fatalf("expected invalid_invocation_stage code, got %v", err)
	}
	pather, ok := err.(interface{ DiagnosticPath() string })
	if !ok || pather.DiagnosticPath() == "" {
		t.Fatalf("expected non-empty DiagnosticPath, got %v", err)
	}
	category, ok := err.(interface{ DiagnosticCategory() diagnostics.Category })
	if !ok || category.DiagnosticCategory() == "" {
		t.Fatalf("expected non-empty DiagnosticCategory, got %v", err)
	}
}

func TestScriptCallableAdapter_WrongArgCountHasStructuredDiagnostic(t *testing.T) {
	desc := schema.CallableDesc{
		Name:       "mine",
		Mode:       schema.CallableModeUnary,
		Parameters: []schema.ParameterDesc{{Name: "x", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}}},
		Returns:    []schema.TypeDesc{{Kind: schema.TypeKindScalar, Name: "string"}},
	}
	backend := stubRuntimeBackend{evaluate: func(callable string, stage binding.InvocationStage, args []any) (any, error) {
		return "ok", nil
	}}
	adapter, err := NewScriptCallableAdapter(desc, backend)
	if err != nil {
		t.Fatalf("NewScriptCallableAdapter: %v", err)
	}

	_, err = adapter.Invoke(binding.InvocationRequest{
		Callable: "mine",
		Stage:    binding.InvocationStageUnary,
		Args:     []any{},
	})
	if err == nil {
		t.Fatal("expected wrong arg count error")
	}

	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() == "" {
		t.Fatalf("expected non-empty DiagnosticCode, got %v", err)
	}
	pather, ok := err.(interface{ DiagnosticPath() string })
	if !ok || pather.DiagnosticPath() == "" {
		t.Fatalf("expected non-empty DiagnosticPath, got %v", err)
	}
}

func TestScriptCallableAdapter_EvalErrorEnvelopeIsLLMFriendly(t *testing.T) {
	desc := schema.CallableDesc{
		Name:       "fail_callable",
		Mode:       schema.CallableModeUnary,
		Parameters: []schema.ParameterDesc{{Name: "x", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}}},
		Returns:    []schema.TypeDesc{{Kind: schema.TypeKindScalar, Name: "int"}},
	}
	evalErr := &sourceEvalError{
		code:     diagNoEvaluator,
		callable: "fail_callable",
	}
	backend := stubRuntimeBackend{evaluate: func(callable string, stage binding.InvocationStage, args []any) (any, error) {
		return nil, evalErr
	}}

	adapter, err := NewScriptCallableAdapter(desc, backend)
	if err != nil {
		t.Fatalf("NewScriptCallableAdapter: %v", err)
	}

	outcome, err := adapter.Invoke(binding.InvocationRequest{
		Callable: "fail_callable",
		Stage:    binding.InvocationStageUnary,
		Args:     []any{int32(1)},
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultError {
		t.Fatalf("expected error result, got %s", outcome.Result.Kind)
	}
	if outcome.Result.Error == nil {
		t.Fatal("expected InvocationErrorDesc")
	}
	if outcome.Result.Error.DiagnosticCode == "" {
		t.Fatal("expected non-empty DiagnosticCode")
	}
	if outcome.Result.Error.Category == "" {
		t.Fatal("expected non-empty Category")
	}
	if outcome.Result.Error.Callable != "fail_callable" {
		t.Fatalf("expected callable fail_callable, got %q", outcome.Result.Error.Callable)
	}
	if outcome.Result.Error.Message == "" {
		t.Fatal("expected non-empty Message")
	}
	if len(outcome.Result.Error.Stack) == 0 {
		t.Fatal("expected non-empty Stack")
	}
}

func TestScriptCallableAdapter_Conformance(t *testing.T) {
	desc := schema.CallableDesc{
		Name:       "conform",
		Mode:       schema.CallableModeUnary,
		Parameters: []schema.ParameterDesc{{Name: "arg0", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}}},
		Returns:    []schema.TypeDesc{{Kind: schema.TypeKindScalar, Name: "string"}},
	}
	backend := stubRuntimeBackend{evaluate: func(callable string, stage binding.InvocationStage, args []any) (any, error) {
		return "ok", nil
	}}
	adapter, err := NewScriptCallableAdapter(desc, backend)
	if err != nil {
		t.Fatalf("NewScriptCallableAdapter: %v", err)
	}
	executableAdapterConformance(t, adapter)
}

func TestScriptCallableAdapter_DiagnosticCodeOnEvalError(t *testing.T) {
	desc := schema.CallableDesc{
		Name:       "fail_callable",
		Mode:       schema.CallableModeUnary,
		Parameters: []schema.ParameterDesc{{Name: "x", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}}},
		Returns:    []schema.TypeDesc{{Kind: schema.TypeKindScalar, Name: "int"}},
	}
	evalErr := &sourceEvalError{
		code:     diagNoEvaluator,
		callable: "fail_callable",
	}
	backend := stubRuntimeBackend{evaluate: func(callable string, stage binding.InvocationStage, args []any) (any, error) {
		return nil, evalErr
	}}

	adapter, err := NewScriptCallableAdapter(desc, backend)
	if err != nil {
		t.Fatalf("NewScriptCallableAdapter: %v", err)
	}

	outcome, err := adapter.Invoke(binding.InvocationRequest{
		Callable: "fail_callable",
		Stage:    binding.InvocationStageUnary,
		Args:     []any{int32(1)},
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultError {
		t.Fatalf("expected error result, got %s", outcome.Result.Kind)
	}
	if outcome.Result.Error == nil {
		t.Fatal("expected InvocationErrorDesc")
	}
	if outcome.Result.Error.DiagnosticCode != "no_evaluator" {
		t.Fatalf("expected diagnostic code no_evaluator, got %q", outcome.Result.Error.DiagnosticCode)
	}
	if outcome.Result.Error.Category != string(diagnostics.CategoryHost) {
		t.Fatalf("expected host category, got %q", outcome.Result.Error.Category)
	}
	if len(outcome.Result.Error.Stack) == 0 || outcome.Result.Error.Stack[0].Callable != "fail_callable" {
		t.Fatalf("expected stack with fail_callable, got %+v", outcome.Result.Error.Stack)
	}
	if outcome.Result.Error.Callable != "fail_callable" {
		t.Fatalf("expected callable fail_callable, got %q", outcome.Result.Error.Callable)
	}
}

func TestScriptCallableAdapter_NilRuntimeBackendProducesNoEvaluatorDiagnostic(t *testing.T) {
	desc := schema.CallableDesc{
		Name:       "stub",
		Mode:       schema.CallableModeUnary,
		Parameters: []schema.ParameterDesc{{Name: "x", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}}},
		Returns:    []schema.TypeDesc{{Kind: schema.TypeKindScalar, Name: "string"}},
	}
	adapter, err := NewScriptCallableAdapter(desc, nil)
	if err != nil {
		t.Fatalf("NewScriptCallableAdapter with nil runtime backend: %v", err)
	}

	outcome, err := adapter.Invoke(binding.InvocationRequest{
		Callable: "stub",
		Stage:    binding.InvocationStageUnary,
		Args:     []any{int32(1)},
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultError {
		t.Fatalf("expected error result, got %s", outcome.Result.Kind)
	}
	if outcome.Result.Error == nil {
		t.Fatal("expected InvocationErrorDesc")
	}
	if outcome.Result.Error.DiagnosticCode != "no_evaluator" {
		t.Fatalf("expected no_evaluator diagnostic code, got %q", outcome.Result.Error.DiagnosticCode)
	}
	if outcome.Result.Error.Callable != "stub" {
		t.Fatalf("expected callable stub, got %q", outcome.Result.Error.Callable)
	}
}

func TestRuntimeBackendFromVMLowering_ReturnsNilForCompileOnlyBackend(t *testing.T) {
	if runtime := runtimeBackendFromVMLowering(compileOnlyBackend{}); runtime != nil {
		t.Fatalf("expected nil runtime backend for compile-only backend, got %T", runtime)
	}
}

func TestScriptCallableAdapter_NilReceiverSafety(t *testing.T) {
	var adapter *ScriptCallableAdapter

	desc := adapter.Callable()
	if desc.Name != "" {
		t.Fatalf("expected empty descriptor from nil adapter, got %q", desc.Name)
	}

	_, err := adapter.Invoke(binding.InvocationRequest{
		Callable: "any",
		Stage:    binding.InvocationStageUnary,
		Args:     []any{1},
	})
	if err == nil {
		t.Fatal("expected error from nil adapter Invoke")
	}
}

func TestScriptCallableAdapter_WrongCallableNameRejected(t *testing.T) {
	desc := schema.CallableDesc{
		Name:       "mine",
		Mode:       schema.CallableModeUnary,
		Parameters: []schema.ParameterDesc{{Name: "x", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}}},
		Returns:    []schema.TypeDesc{{Kind: schema.TypeKindScalar, Name: "string"}},
	}
	backend := stubRuntimeBackend{evaluate: func(callable string, stage binding.InvocationStage, args []any) (any, error) {
		return "ok", nil
	}}
	adapter, err := NewScriptCallableAdapter(desc, backend)
	if err != nil {
		t.Fatalf("NewScriptCallableAdapter: %v", err)
	}

	_, err = adapter.Invoke(binding.InvocationRequest{
		Callable: "other",
		Stage:    binding.InvocationStageUnary,
		Args:     []any{int32(1)},
	})
	if err == nil {
		t.Fatal("expected error for wrong callable name")
	}
}

func TestScriptCallableAdapter_WrongStageRejected(t *testing.T) {
	desc := schema.CallableDesc{
		Name:       "mine",
		Mode:       schema.CallableModeUnary,
		Parameters: []schema.ParameterDesc{{Name: "x", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}}},
		Returns:    []schema.TypeDesc{{Kind: schema.TypeKindScalar, Name: "string"}},
	}
	backend := stubRuntimeBackend{evaluate: func(callable string, stage binding.InvocationStage, args []any) (any, error) {
		return "ok", nil
	}}
	adapter, err := NewScriptCallableAdapter(desc, backend)
	if err != nil {
		t.Fatalf("NewScriptCallableAdapter: %v", err)
	}

	_, err = adapter.Invoke(binding.InvocationRequest{
		Callable: "mine",
		Stage:    binding.InvocationStageNext,
		Args:     []any{int32(1)},
	})
	if err == nil {
		t.Fatal("expected error for wrong stage on unary callable")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "invalid_invocation_stage" {
		t.Fatalf("expected invalid_invocation_stage code, got %v", err)
	}
	pather, ok := err.(interface{ DiagnosticPath() string })
	if !ok || pather.DiagnosticPath() != "binding/invocation/stage" {
		t.Fatalf("expected binding/invocation/stage path, got %v", err)
	}
}

func TestScriptCallableAdapter_WrongArgCountRejected(t *testing.T) {
	desc := schema.CallableDesc{
		Name:       "mine",
		Mode:       schema.CallableModeUnary,
		Parameters: []schema.ParameterDesc{{Name: "x", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}}},
		Returns:    []schema.TypeDesc{{Kind: schema.TypeKindScalar, Name: "string"}},
	}
	backend := stubRuntimeBackend{evaluate: func(callable string, stage binding.InvocationStage, args []any) (any, error) {
		return "ok", nil
	}}
	adapter, err := NewScriptCallableAdapter(desc, backend)
	if err != nil {
		t.Fatalf("NewScriptCallableAdapter: %v", err)
	}

	_, err = adapter.Invoke(binding.InvocationRequest{
		Callable: "mine",
		Stage:    binding.InvocationStageUnary,
		Args:     []any{},
	})
	if err == nil {
		t.Fatal("expected error for wrong arg count")
	}
}

func TestScriptCallableAdapter_StreamingDispatchesByStage(t *testing.T) {
	desc, err := schema.NewStreamingCallableDesc(
		"stream",
		nil,
		&schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"},
		&schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"},
		false,
	)
	if err != nil {
		t.Fatalf("NewStreamingCallableDesc: %v", err)
	}
	resetCalls := 0
	backend := stubRuntimeBackend{evaluate: func(callable string, stage binding.InvocationStage, args []any) (any, error) {
		if stage == binding.InvocationStageNext {
			return "partial", nil
		}
		if stage == binding.InvocationStageFinal {
			return 42, nil
		}
		return nil, fmt.Errorf("unexpected stage %s", stage)
	}, reset: func(callable string, args []any) {
		resetCalls++
	}}
	adapter, err := NewScriptCallableAdapter(desc, backend)
	if err != nil {
		t.Fatalf("NewScriptCallableAdapter: %v", err)
	}

	next, err := adapter.Invoke(binding.InvocationRequest{Callable: "stream", Stage: binding.InvocationStageNext})
	if err != nil {
		t.Fatalf("Invoke next: %v", err)
	}
	if next.Payload == nil || next.Payload.Value != "partial" {
		t.Fatalf("expected partial next payload, got %+v", next.Payload)
	}

	final, err := adapter.Invoke(binding.InvocationRequest{Callable: "stream", Stage: binding.InvocationStageFinal})
	if err != nil {
		t.Fatalf("Invoke final: %v", err)
	}
	if final.Payload == nil || final.Payload.Value != 42 {
		t.Fatalf("expected final payload 42, got %+v", final.Payload)
	}
	if resetCalls != 0 {
		t.Fatalf("final must preserve exhausted stream state; expected no reset after final, got %d", resetCalls)
	}
}

func TestScriptCallableAdapter_ConcurrentInvoke_NoDataRace(t *testing.T) {
	desc := schema.CallableDesc{
		Name:       "concurrent",
		Mode:       schema.CallableModeUnary,
		Parameters: []schema.ParameterDesc{{Name: "x", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}}},
		Returns:    []schema.TypeDesc{{Kind: schema.TypeKindScalar, Name: "int"}},
	}
	backend := stubRuntimeBackend{evaluate: func(callable string, stage binding.InvocationStage, args []any) (any, error) {
		return args[0], nil
	}}
	adapter, err := NewScriptCallableAdapter(desc, backend)
	if err != nil {
		t.Fatalf("NewScriptCallableAdapter: %v", err)
	}

	const goroutines = 10
	const opsPer = 20
	errCh := make(chan error, goroutines*opsPer)
	var wg sync.WaitGroup

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(seq int) {
			defer wg.Done()
			for j := 0; j < opsPer; j++ {
				outcome, err := adapter.Invoke(binding.InvocationRequest{
					Callable: "concurrent",
					Stage:    binding.InvocationStageUnary,
					Args:     []any{int32(seq*100 + j)},
				})
				if err != nil {
					errCh <- fmt.Errorf("concurrent invoke seq=%d j=%d: %w", seq, j, err)
					continue
				}
				if outcome.Result.Kind != binding.InvocationResultValue {
					errCh <- fmt.Errorf("seq=%d j=%d: expected value result, got %s", seq, j, outcome.Result.Kind)
				}
			}
		}(i)
	}

	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("concurrent operation error: %v", err)
	}
}

func TestScriptCallableAdapter_DescriptorDeepClone(t *testing.T) {
	desc := schema.CallableDesc{
		Name:       "clone_test",
		Mode:       schema.CallableModeUnary,
		Parameters: []schema.ParameterDesc{{Name: "x", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}}},
		Returns:    []schema.TypeDesc{{Kind: schema.TypeKindScalar, Name: "string"}},
	}
	backend := stubRuntimeBackend{evaluate: func(callable string, stage binding.InvocationStage, args []any) (any, error) {
		return "ok", nil
	}}
	adapter, err := NewScriptCallableAdapter(desc, backend)
	if err != nil {
		t.Fatalf("NewScriptCallableAdapter: %v", err)
	}

	// Mutate the original descriptor after adapter creation
	desc.Name = "mutated"

	// Adapter should still return the original name
	returned := adapter.Callable()
	if returned.Name != "clone_test" {
		t.Fatalf("expected clone_test, got %q", returned.Name)
	}

	// Mutate the returned descriptor
	returned.Name = "also_mutated"
	returned2 := adapter.Callable()
	if returned2.Name != "clone_test" {
		t.Fatalf("expected clone_test after mutating returned descriptor, got %q", returned2.Name)
	}
}
