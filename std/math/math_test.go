package math_test

import (
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/internal/script/bytecode"
	"github.com/qomos-w/spore/internal/script/frontend"
	"github.com/qomos-w/spore/std/math"
)

func TestMathModule_RegistersAllFunctions(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := math.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	functions := []string{
		"math.abs", "math.floor", "math.ceil", "math.round",
		"math.sqrt", "math.max", "math.min", "math.pow",
	}
	for _, name := range functions {
		_, ok := sb.Executors.Lookup(name)
		if !ok {
			t.Fatalf("expected %s to be registered", name)
		}
	}
}

func TestMathModule_Abs(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := math.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	outcome, err := sb.Invoke(binding.InvocationRequest{Callable: "math.abs", Stage: binding.InvocationStageUnary, Args: []any{-3.14}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 3.14 {
		t.Fatalf("expected 3.14, got %+v", outcome.Payload)
	}
}

func TestMathModule_Max(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := math.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	outcome, err := sb.Invoke(binding.InvocationRequest{Callable: "math.max", Stage: binding.InvocationStageUnary, Args: []any{2.0, 5.0}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 5.0 {
		t.Fatalf("expected 5.0, got %+v", outcome.Payload)
	}
}

func TestMathModule_Pow(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := math.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	outcome, err := sb.Invoke(binding.InvocationRequest{Callable: "math.pow", Stage: binding.InvocationStageUnary, Args: []any{2.0, 3.0}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 8.0 {
		t.Fatalf("expected 8.0, got %+v", outcome.Payload)
	}
}

func TestMathModule_VMEndToEnd(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := math.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	f, err := frontend.New(sb)
	if err != nil {
		t.Fatalf("frontend.New: %v", err)
	}
	vmEval := bytecode.NewVMEvaluator()
	vmEval.SetNativeBinding(sb)
	f.SetVMCompileHook(vmEval)
	f.SetModuleResolver(frontend.MapModuleResolver{})

	if err := f.LoadSource(`import abs from "math"
import max from "math"
import pow from "math"
fun calc(): double { return abs(pow(max(-2.0, -5.0), 2.0)) }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("calc", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 4.0 {
		t.Fatalf("expected 4.0, got %+v", outcome.Payload)
	}
}
