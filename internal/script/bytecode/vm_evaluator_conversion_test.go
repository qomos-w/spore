package bytecode

import (
	"strings"
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/internal/script/frontend"
)

type unsupportedArg struct{}

func TestVMEvaluator_PreservesInt64AndFloat64Arguments(t *testing.T) {
	evaluator := NewVMEvaluator()
	source := `
	fun idLong(x: long): long { return x }
	fun idDouble(x: double): double { return x }
	`
	prog, err := frontend.ParseModuleForTest(source)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if err := evaluator.CompileProgram(prog); err != nil {
		t.Fatalf("compile error: %v", err)
	}

	longResult, err := evaluator.Evaluate("idLong", binding.InvocationStageUnary, []any{int64(1 << 40)})
	if err != nil {
		t.Fatalf("evaluate long: %v", err)
	}
	if got, ok := longResult.(int64); !ok || got != int64(1<<40) {
		t.Fatalf("expected int64 %d, got %#v", int64(1<<40), longResult)
	}

	doubleResult, err := evaluator.Evaluate("idDouble", binding.InvocationStageUnary, []any{123.5})
	if err != nil {
		t.Fatalf("evaluate double: %v", err)
	}
	if got, ok := doubleResult.(float64); !ok || got != 123.5 {
		t.Fatalf("expected float64 123.5, got %#v", doubleResult)
	}
}

func TestVMEvaluator_UnsupportedArgumentReturnsDiagnostic(t *testing.T) {
	evaluator := NewVMEvaluator()
	source := `fun id(x: int): int { return x }`
	prog, err := frontend.ParseModuleForTest(source)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if err := evaluator.CompileProgram(prog); err != nil {
		t.Fatalf("compile error: %v", err)
	}

	_, err = evaluator.Evaluate("id", binding.InvocationStageUnary, []any{unsupportedArg{}})
	if err == nil {
		t.Fatal("expected unsupported argument error")
	}
	rtErr, ok := err.(*RuntimeError)
	if !ok {
		t.Fatalf("expected RuntimeError, got %T", err)
	}
	if rtErr.Code != "unsupported_vm_argument_type" {
		t.Fatalf("expected diagnostic code unsupported_vm_argument_type, got %q", rtErr.Code)
	}
	if !strings.HasPrefix(rtErr.Path, "vm/evaluator/argument/0") {
		t.Fatalf("expected diagnostic path prefix vm/evaluator/argument/0, got %q", rtErr.Path)
	}
	if !strings.Contains(rtErr.Message, "unsupportedArg") {
		t.Fatalf("expected error message to mention unsupportedArg, got %q", rtErr.Message)
	}
}
