package binding_test

import (
	"context"
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/diagnostics"
)

type diagnosticCapabilityError struct{}

func (diagnosticCapabilityError) Error() string          { return "boom" }
func (diagnosticCapabilityError) DiagnosticCode() string { return "host_failed" }
func (diagnosticCapabilityError) DiagnosticCategory() diagnostics.Category {
	return diagnostics.CategoryHost
}
func (diagnosticCapabilityError) DiagnosticPath() string { return "host/capability/execute" }
func (diagnosticCapabilityError) DiagnosticStack() []diagnostics.Frame {
	return []diagnostics.Frame{{Callable: "host.execute", Stage: "unary"}}
}

func TestCapabilityExecutableAdapterInvokesFlattenedCallable(t *testing.T) {
	builder := binding.NewCapability("tool", "service")
	if err := builder.AddFunction("execute", capabilityEchoContext); err != nil {
		t.Fatalf("AddFunction: %v", err)
	}
	cap, err := builder.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	adapter, err := binding.NewCapabilityExecutableAdapter("tool", cap.Callables["execute"])
	if err != nil {
		t.Fatalf("NewCapabilityExecutableAdapter: %v", err)
	}
	if adapter.Callable().Name != "tool.execute" {
		t.Fatalf("expected tool.execute callable, got %s", adapter.Callable().Name)
	}
	outcome, err := adapter.Invoke(binding.InvocationRequest{
		Callable: "tool.execute",
		Stage:    binding.InvocationStageUnary,
		Args:     []any{map[string]any{"Text": "hello"}},
		Context:  context.WithValue(context.Background(), "prefix", "ctx:"),
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Result.Callable != "tool.execute" {
		t.Fatalf("expected result callable tool.execute, got %s", outcome.Result.Callable)
	}
	out, ok := outcome.Payload.Value.(capabilityOutput)
	if !ok {
		t.Fatalf("expected capabilityOutput payload, got %T", outcome.Payload.Value)
	}
	if out.Text != "ctx:hello" {
		t.Fatalf("expected ctx:hello, got %q", out.Text)
	}
}

func TestCapabilityExecutableAdapterProjectsHostError(t *testing.T) {
	builder := binding.NewCapability("tool", "service")
	if err := builder.AddFunction("execute", func(in capabilityInput) (capabilityOutput, error) {
		return capabilityOutput{}, diagnosticCapabilityError{}
	}); err != nil {
		t.Fatalf("AddFunction: %v", err)
	}
	cap, err := builder.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	adapter, err := binding.NewCapabilityExecutableAdapter("tool", cap.Callables["execute"])
	if err != nil {
		t.Fatalf("NewCapabilityExecutableAdapter: %v", err)
	}
	outcome, err := adapter.Invoke(binding.InvocationRequest{
		Callable: "tool.execute",
		Stage:    binding.InvocationStageUnary,
		Args:     []any{capabilityInput{Text: "hello"}},
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultError {
		t.Fatalf("expected error result, got %s", outcome.Result.Kind)
	}
	if outcome.Result.Error == nil || outcome.Result.Error.Message != "boom" {
		t.Fatalf("expected projected boom error, got %#v", outcome.Result.Error)
	}
	if outcome.Result.Error.DiagnosticCode != "host_failed" {
		t.Fatalf("expected host_failed diagnostic code, got %q", outcome.Result.Error.DiagnosticCode)
	}
	if outcome.Result.Error.Category != string(diagnostics.CategoryHost) {
		t.Fatalf("expected host category, got %q", outcome.Result.Error.Category)
	}
	if outcome.Result.Error.Path != "host/capability/execute" {
		t.Fatalf("expected host path, got %q", outcome.Result.Error.Path)
	}
	if len(outcome.Result.Error.Stack) != 1 || outcome.Result.Error.Stack[0].Callable != "host.execute" {
		t.Fatalf("expected host stack, got %#v", outcome.Result.Error.Stack)
	}
	if outcome.Payload != nil {
		t.Fatalf("expected nil payload for error result, got %#v", outcome.Payload)
	}
}

func TestCapabilityExecutableAdapterRejectsWrongRequest(t *testing.T) {
	builder := binding.NewCapability("tool", "service")
	if err := builder.AddFunction("execute", capabilityEcho); err != nil {
		t.Fatalf("AddFunction: %v", err)
	}
	cap, err := builder.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	adapter, err := binding.NewCapabilityExecutableAdapter("tool", cap.Callables["execute"])
	if err != nil {
		t.Fatalf("NewCapabilityExecutableAdapter: %v", err)
	}
	if _, err := adapter.Invoke(binding.InvocationRequest{Callable: "other.execute", Stage: binding.InvocationStageUnary, Args: []any{capabilityInput{}}}); err == nil {
		t.Fatal("expected wrong callable error")
	}
	if _, err := adapter.Invoke(binding.InvocationRequest{Callable: "tool.execute", Stage: binding.InvocationStageNext, Args: []any{capabilityInput{}}}); err == nil {
		t.Fatal("expected wrong stage error")
	}
	if _, err := adapter.Invoke(binding.InvocationRequest{Callable: "tool.execute", Stage: binding.InvocationStageUnary, Args: nil}); err == nil {
		t.Fatal("expected wrong arg count error")
	}
}

func TestCapabilityExecutableAdapter_FreeFunctionMultiParam(t *testing.T) {
	builder := binding.NewCapability("math", "module")
	if err := builder.AddFreeFunction("max", func(a, b int) int {
		if a > b {
			return a
		}
		return b
	}); err != nil {
		t.Fatalf("AddFreeFunction: %v", err)
	}
	cap, err := builder.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	adapter, err := binding.NewCapabilityExecutableAdapter("math", cap.Callables["max"])
	if err != nil {
		t.Fatalf("NewCapabilityExecutableAdapter: %v", err)
	}
	if adapter.Callable().Name != "math.max" {
		t.Fatalf("expected math.max callable, got %s", adapter.Callable().Name)
	}
	outcome, err := adapter.Invoke(binding.InvocationRequest{
		Callable: "math.max",
		Stage:    binding.InvocationStageUnary,
		Args:     []any{3, 7},
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload.Value != 7 {
		t.Fatalf("expected 7, got %v", outcome.Payload.Value)
	}
}

func TestCapabilityExecutableAdapter_FreeFunctionSingleParam(t *testing.T) {
	builder := binding.NewCapability("math", "module")
	if err := builder.AddFreeFunction("abs", func(x float64) float64 {
		if x < 0 {
			return -x
		}
		return x
	}); err != nil {
		t.Fatalf("AddFreeFunction: %v", err)
	}
	cap, err := builder.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	adapter, err := binding.NewCapabilityExecutableAdapter("math", cap.Callables["abs"])
	if err != nil {
		t.Fatalf("NewCapabilityExecutableAdapter: %v", err)
	}
	outcome, err := adapter.Invoke(binding.InvocationRequest{
		Callable: "math.abs",
		Stage:    binding.InvocationStageUnary,
		Args:     []any{-2.5},
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload.Value != 2.5 {
		t.Fatalf("expected 2.5, got %v", outcome.Payload.Value)
	}
}
