package bytecode

import (
	"strings"
	"testing"

	"github.com/qomos-w/spore/diagnostics"
	"github.com/qomos-w/spore/internal/script/frontend"
	"github.com/qomos-w/spore/internal/script/vm"
	"github.com/qomos-w/spore/invoke"
)

// These tests pin the boundary conversion documented in doc.go: the two
// failure tracks meet in bytecodePanicError, and every top-level entry point
// installs it, so no caller of this package has to recover.

func TestBytecodePanicError_WrapsUnstructuredPanicValues(t *testing.T) {
	cases := []struct {
		name      string
		recovered any
		wantPart  string
	}{
		{name: "string", recovered: "stack underflow", wantPart: "stack underflow"},
		{name: "error", recovered: errStub("out of memory"), wantPart: "out of memory"},
		{name: "other", recovered: 42, wantPart: "42"},
		{name: "nil", recovered: nil, wantPart: "internal VM panic"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := bytecodePanicError(nil, "someCallable", "vm/evaluate", tc.recovered)
			rtErr, ok := err.(*RuntimeError)
			if !ok {
				t.Fatalf("got %T, want *RuntimeError", err)
			}
			if rtErr.Code != vmInternalPanicCode {
				t.Fatalf("code = %q, want %q", rtErr.Code, vmInternalPanicCode)
			}
			if rtErr.Category != diagnostics.CategoryRuntime {
				t.Fatalf("category = %q, want runtime", rtErr.Category)
			}
			if rtErr.Callable != "someCallable" || rtErr.Path != "vm/evaluate" {
				t.Fatalf("context lost: callable=%q path=%q", rtErr.Callable, rtErr.Path)
			}
			if !strings.Contains(rtErr.Message, tc.wantPart) {
				t.Fatalf("message %q should contain %q", rtErr.Message, tc.wantPart)
			}
			if rtErr.DiagnosticCode() != vmInternalPanicCode {
				t.Fatalf("DiagnosticCode() = %q, want %q", rtErr.DiagnosticCode(), vmInternalPanicCode)
			}
		})
	}
}

func TestBytecodePanicError_PassesStructuredRuntimeErrorThrough(t *testing.T) {
	original := &RuntimeError{Code: "division_by_zero", Path: "vm/arithmetic/div", Message: "division by zero"}
	err := bytecodePanicError(nil, "caller", "vm/evaluate", original)
	rtErr, ok := err.(*RuntimeError)
	if !ok {
		t.Fatalf("got %T, want *RuntimeError", err)
	}
	if rtErr != original {
		t.Fatal("the structured error pointer should survive the round trip unchanged")
	}
	if rtErr.Code != "division_by_zero" || rtErr.Message != "division by zero" {
		t.Fatalf("structured identity lost: %+v", rtErr)
	}
	// Missing context is filled in; existing context is not overwritten.
	if rtErr.Callable != "caller" {
		t.Fatalf("callable = %q, want caller", rtErr.Callable)
	}
	if rtErr.Path != "vm/arithmetic/div" {
		t.Fatalf("path = %q, want the original vm/arithmetic/div", rtErr.Path)
	}
}

func TestBytecodePanicError_ResetsInterpreterRunState(t *testing.T) {
	interp := newInterpreter(vm.NewVM(4096, 256))
	interp.sp = 3
	interp.locals = append(interp.locals, 1, 2)
	interp.callFrames = append(interp.callFrames, callFrame{function: "frame"})
	interp.handlerStack = append(interp.handlerStack, vmHandler{})
	interp.deferStack = append(interp.deferStack, deferEntry{})
	interp.chunk = &chunk{sourceName: "c"}
	interp.function = "c"
	interp.stackBase, interp.localsBase, interp.localsCount = 1, 1, 1

	bytecodePanicError(interp, "caller", "vm/evaluate", "boom")

	if interp.sp != 0 || len(interp.locals) != 0 || len(interp.callFrames) != 0 ||
		len(interp.handlerStack) != 0 || len(interp.deferStack) != 0 {
		t.Fatalf("run state not cleared: sp=%d locals=%d frames=%d handlers=%d defers=%d",
			interp.sp, len(interp.locals), len(interp.callFrames), len(interp.handlerStack), len(interp.deferStack))
	}
	if interp.chunk != nil || interp.function != "" || interp.stackBase != 0 || interp.localsBase != 0 || interp.localsCount != 0 {
		t.Fatalf("frame cursor not reset: chunk=%v function=%q bases=%d/%d count=%d",
			interp.chunk, interp.function, interp.stackBase, interp.localsBase, interp.localsCount)
	}
}

// TestVMEvaluator_OOMConvertedToStructuredErrorAndEvaluationContinues checks
// the two halves of the boundary contract in one place: an exhausted heap
// (Track 1) reaches the caller as a vm_internal_panic RuntimeError, and the
// evaluator — which shares one interpreter across calls — keeps working
// afterwards, because the recovery drops the aborted run's state instead of
// leaving a half-unwound frame behind.
func TestVMEvaluator_OOMConvertedToStructuredErrorAndEvaluationContinues(t *testing.T) {
	eval := NewVMEvaluatorWith(65536, 256)
	prog, err := frontend.ParseModuleForTest(`fun count(v: any): int {
		var m: map<string, any> = (v as map<string, any>)
		var ids: array<any> = (m["ids"] as array<any>)
		return len(ids)
	}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := eval.CompileProgram(prog); err != nil {
		t.Fatalf("compile: %v", err)
	}

	_, err = eval.Evaluate("count", invoke.InvocationStageUnary, []any{buildLargeNestedBatchEnvelope(4000)})
	if err == nil {
		t.Fatal("expected the small budget to fail materialising the nested envelope")
	}
	rtErr, ok := err.(*RuntimeError)
	if !ok {
		t.Fatalf("got %T (%v), want *RuntimeError", err, err)
	}
	if rtErr.Code != vmInternalPanicCode {
		t.Fatalf("code = %q, want %q", rtErr.Code, vmInternalPanicCode)
	}
	if !strings.Contains(strings.ToLower(rtErr.Message), "out of memory") {
		t.Fatalf("message %q should keep the VM panic text", rtErr.Message)
	}

	result, err := eval.Evaluate("count", invoke.InvocationStageUnary, []any{map[string]any{"ids": []any{"a", "b"}}})
	if err != nil {
		t.Fatalf("evaluator unusable after a recovered panic: %v", err)
	}
	if result.(int) != 2 {
		t.Fatalf("result = %v, want 2", result)
	}
}

// errStub is a minimal non-RuntimeError error, used to prove the recovery
// renders arbitrary error values instead of requiring a known type.
type errStub string

func (e errStub) Error() string { return string(e) }
