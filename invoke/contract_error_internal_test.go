package invoke

import (
	"errors"
	"testing"

	"github.com/qomos-w/spore/diagnostics"
)

func TestContractError_Error(t *testing.T) {
	var nilErr *ContractError
	if nilErr.Error() != "" {
		t.Fatalf("expected empty string for nil ContractError, got %q", nilErr.Error())
	}

	e := newContractError("E001", "TestCallable", InvocationStageUnary, ".field", "something went wrong")
	if e.Error() != "something went wrong" {
		t.Fatalf("expected message, got %q", e.Error())
	}
}

func TestContractError_Unwrap(t *testing.T) {
	var nilErr *ContractError
	if nilErr.Unwrap() != nil {
		t.Fatal("expected nil for nil ContractError.Unwrap")
	}

	cause := errors.New("root cause")
	e := &ContractError{Code: "E002", Cause: cause, Message: "wrapped"}
	if e.Unwrap() != cause {
		t.Fatalf("expected cause %v, got %v", cause, e.Unwrap())
	}
}

func TestContractError_DiagnosticCode(t *testing.T) {
	var nilErr *ContractError
	if nilErr.DiagnosticCode() != "" {
		t.Fatalf("expected empty code for nil ContractError, got %q", nilErr.DiagnosticCode())
	}

	e := newContractError("E003", "TestCallable", InvocationStageUnary, "", "msg")
	if e.DiagnosticCode() != "E003" {
		t.Fatalf("expected code E003, got %q", e.DiagnosticCode())
	}
}

func TestContractError_DiagnosticCategory(t *testing.T) {
	e := newContractError("E004", "TestCallable", InvocationStageUnary, "", "msg")
	if e.DiagnosticCategory() != diagnostics.CategoryContract {
		t.Fatalf("expected contract category, got %q", e.DiagnosticCategory())
	}
}

func TestContractError_DiagnosticPath(t *testing.T) {
	var nilErr *ContractError
	if nilErr.DiagnosticPath() != "" {
		t.Fatalf("expected empty path for nil ContractError, got %q", nilErr.DiagnosticPath())
	}

	e := newContractError("E005", "TestCallable", InvocationStageUnary, ".items[2]", "msg")
	if e.DiagnosticPath() != ".items[2]" {
		t.Fatalf("expected path .items[2], got %q", e.DiagnosticPath())
	}
}

func TestContractError_DiagnosticStack(t *testing.T) {
	var nilErr *ContractError
	if nilErr.DiagnosticStack() != nil {
		t.Fatal("expected nil stack for nil ContractError")
	}

	e := newContractError("E006", "TestCallable", InvocationStageUnary, "", "msg")
	stack := e.DiagnosticStack()
	if len(stack) != 1 || stack[0].Callable != "TestCallable" || stack[0].Stage != string(InvocationStageUnary) {
		t.Fatalf("unexpected stack: %+v", stack)
	}

	// Empty callable should produce nil stack
	eNoCallable := newContractError("E007", "", InvocationStageUnary, "", "msg")
	if eNoCallable.DiagnosticStack() != nil {
		t.Fatalf("expected nil stack when callable is empty, got %+v", eNoCallable.DiagnosticStack())
	}
}

func TestContractError_DiagnosticCause(t *testing.T) {
	var nilErr *ContractError
	if nilErr.DiagnosticCause() != nil {
		t.Fatal("expected nil cause for nil ContractError")
	}

	e := newContractError("E008", "TestCallable", InvocationStageUnary, "", "msg")
	if e.DiagnosticCause() != nil {
		t.Fatal("expected nil cause when no underlying error")
	}

	cause := errors.New("underlying")
	eWithCause := &ContractError{Code: "E009", Callable: "TestCallable", Stage: InvocationStageUnary, Cause: cause, Message: "msg"}
	dc := eWithCause.DiagnosticCause()
	if dc == nil || dc.Category != diagnostics.CategoryContract || dc.Message != "underlying" {
		t.Fatalf("unexpected cause descriptor: %+v", dc)
	}
}

func TestContractError_DiagnosticExpectedActual(t *testing.T) {
	var nilErr *ContractError
	if nilErr.DiagnosticExpected() != "" {
		t.Fatalf("expected empty Expected for nil ContractError, got %q", nilErr.DiagnosticExpected())
	}
	if nilErr.DiagnosticActual() != "" {
		t.Fatalf("expected empty Actual for nil ContractError, got %q", nilErr.DiagnosticActual())
	}

	e := newContractErrorWithTypes(CodeInvalidInvocationStage, "TestCallable", InvocationStageUnary, "binding/stage", "unary", "next", "msg")
	if e.DiagnosticExpected() != "unary" {
		t.Fatalf("expected Expected 'unary', got %q", e.DiagnosticExpected())
	}
	if e.DiagnosticActual() != "next" {
		t.Fatalf("expected Actual 'next', got %q", e.DiagnosticActual())
	}
}

func TestContractError_FromErrorRoundTripExpectedActual(t *testing.T) {
	e := newContractErrorWithTypes(CodeInvalidArgumentType, "add", InvocationStageUnary, "binding/args/type", "int", "string", "arg invalid")
	diag := diagnostics.FromError(e, diagnostics.Descriptor{})
	if diag.Expected != "int" {
		t.Fatalf("expected FromError Expected 'int', got %q", diag.Expected)
	}
	if diag.Actual != "string" {
		t.Fatalf("expected FromError Actual 'string', got %q", diag.Actual)
	}
	if diag.Code != CodeInvalidArgumentType {
		t.Fatalf("expected FromError code preserved, got %q", diag.Code)
	}
	if diag.Hint == "" {
		t.Fatal("expected FromError hint from code registry")
	}

	// Verify Envelope includes Expected/Actual.
	env := diag.Envelope()
	if env["expected"] != "int" {
		t.Fatalf("expected envelope expected 'int', got %v", env["expected"])
	}
	if env["actual"] != "string" {
		t.Fatalf("expected envelope actual 'string', got %v", env["actual"])
	}
	if env["hint"] == "" {
		t.Fatal("expected envelope hint")
	}
}
