package diagnostics

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestClonePtr_NilReturnsNil(t *testing.T) {
	if got := ClonePtr(nil); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestClonePtr_DeepCopies(t *testing.T) {
	orig := &Descriptor{Category: CategoryRuntime, Message: "boom"}
	cloned := ClonePtr(orig)
	if cloned == nil || cloned == orig {
		t.Fatal("expected distinct clone")
	}
	if cloned.Message != "boom" {
		t.Fatalf("expected message preserved, got %q", cloned.Message)
	}
}

func TestCloneDeepCopiesCauseAndStack(t *testing.T) {
	orig := Descriptor{
		Category: CategoryRuntime,
		Code:     "division_by_zero",
		Stack:    []Frame{{Callable: "inner"}},
		Cause:    &Descriptor{Category: CategoryHost, Message: "boom"},
	}
	cloned := Clone(orig)
	cloned.Stack[0].Callable = "mutated"
	cloned.Cause.Message = "changed"
	if orig.Stack[0].Callable != "inner" {
		t.Fatalf("expected original stack untouched, got %+v", orig.Stack)
	}
	if orig.Cause.Message != "boom" {
		t.Fatalf("expected original cause untouched, got %+v", orig.Cause)
	}
}

func TestNormalizeDefaultsCategory(t *testing.T) {
	normalized := Normalize(Descriptor{Code: "x", Message: "y"})
	if normalized.Category != CategoryRuntime {
		t.Fatalf("expected runtime default category, got %q", normalized.Category)
	}
}

type codedTestError struct{}

func (codedTestError) Error() string { return "coded" }
func (codedTestError) DiagnosticCode() string { return "coded_error" }
func (codedTestError) DiagnosticCategory() Category { return CategoryContract }
func (codedTestError) DiagnosticPath() string { return "binding/test" }
func (codedTestError) DiagnosticStack() []Frame { return []Frame{{Callable: "test"}} }
func (codedTestError) DiagnosticCause() *Descriptor { return &Descriptor{Category: CategoryHost, Message: "cause"} }

func TestFromErrorExtractsDiagnosticCapabilities(t *testing.T) {
	diag := FromError(codedTestError{}, Descriptor{Message: "fallback"})
	if diag.Code != "coded_error" || diag.Category != CategoryContract || diag.Path != "binding/test" {
		t.Fatalf("expected extracted code/category/path, got %+v", diag)
	}
	if diag.Message != "fallback" {
		t.Fatalf("fallback message should be preserved when present, got %q", diag.Message)
	}
	if len(diag.Stack) != 1 || diag.Stack[0].Callable != "test" {
		t.Fatalf("expected extracted stack, got %+v", diag.Stack)
	}
	if diag.Cause == nil || diag.Cause.Message != "cause" {
		t.Fatalf("expected extracted cause, got %+v", diag.Cause)
	}
}

func TestFromErrorUsesFallbackForPlainError(t *testing.T) {
	diag := FromError(errors.New("plain"), Descriptor{Callable: "fallback"})
	if diag.Message != "plain" || diag.Callable != "fallback" || diag.Category != CategoryRuntime {
		t.Fatalf("expected fallback plus plain message, got %+v", diag)
	}
}

func TestDescriptorMarshalJSON_StableShape(t *testing.T) {
	payload, err := json.Marshal(Descriptor{
		Category: CategoryRuntime,
		Code:     "division_by_zero",
		Message:  "boom",
		Callable: "calc",
		Stage:    "unary",
		Path:     "script/body",
		Identity: "entity:1",
		Span: Span{Start: Position{Line: 3, Column: 4}, End: Position{Line: 3, Column: 8}},
		Stack: []Frame{{Callable: "calc", Stage: "unary", Span: Span{Start: Position{Line: 3, Column: 4}}}},
		Cause: &Descriptor{Category: CategoryHost, Code: "no_evaluator", Message: "inner"},
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	text := string(payload)
	for _, needle := range []string{"\"category\":\"runtime\"", "\"code\":\"division_by_zero\"", "\"callable\":\"calc\"", "\"path\":\"script/body\"", "\"identity\":\"entity:1\"", "\"cause\":"} {
		if !strings.Contains(text, needle) {
			t.Fatalf("expected %s in %s", needle, text)
		}
	}
}

func TestDescriptorMarshalJSON_OmitsEmptyFields(t *testing.T) {
	payload, err := json.Marshal(Descriptor{Message: "boom"})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	text := string(payload)
	if strings.Contains(text, "\"path\"") || strings.Contains(text, "\"identity\"") || strings.Contains(text, "\"stack\"") {
		t.Fatalf("expected empty fields omitted, got %s", text)
	}
}

func TestMultiErrorBundleAndJSON(t *testing.T) {
	err := NewMultiError([]error{codedTestError{}, errors.New("plain")})
	multi, ok := err.(*MultiError)
	if !ok {
		t.Fatalf("expected *MultiError, got %T", err)
	}
	bundle := multi.Envelope()
	if len(bundle) != 2 || bundle[0]["code"] != "coded_error" {
		t.Fatalf("unexpected bundle: %+v", bundle)
	}
	payload, marshalErr := json.Marshal(multi)
	if marshalErr != nil {
		t.Fatalf("MarshalJSON: %v", marshalErr)
	}
	text := string(payload)
	if !strings.Contains(text, "\"code\":\"coded_error\"") || !strings.Contains(text, "\"message\":\"plain\"") {
		t.Fatalf("unexpected multi-error JSON: %s", text)
	}
}

func TestMultiErrorUsesConfiguredFallback(t *testing.T) {
	err := NewMultiErrorWithFallback([]error{errors.New("plain1"), errors.New("plain2")}, Descriptor{Category: CategorySchema, Path: "frontend/schema"})
	multi, ok := err.(*MultiError)
	if !ok {
		t.Fatalf("expected *MultiError, got %T", err)
	}
	if multi.DiagnosticCategory() != CategorySchema || multi.DiagnosticPath() != "frontend/schema" {
		t.Fatalf("expected configured category/path, got %q %q", multi.DiagnosticCategory(), multi.DiagnosticPath())
	}
	bundle := multi.Envelope()
	if bundle[0]["category"] != "schema" || bundle[0]["path"] != "frontend/schema" {
		t.Fatalf("expected fallback in bundle, got %+v", bundle)
	}
}

func TestMultiError_Error(t *testing.T) {
	e := NewMultiError([]error{errors.New("first"), errors.New("second")})
	multi, ok := e.(*MultiError)
	if !ok {
		t.Fatalf("expected *MultiError, got %T", e)
	}
	got := multi.Error()
	if got == "" {
		t.Fatal("expected non-empty error string")
	}
	if !strings.Contains(got, "first") || !strings.Contains(got, "second") {
		t.Fatalf("expected error to contain both messages, got %q", got)
	}

	var nilMulti *MultiError
	if nilMulti.Error() != "" {
		t.Fatalf("expected empty string for nil MultiError, got %q", nilMulti.Error())
	}

	empty := NewMultiError([]error{})
	if empty != nil {
		t.Fatal("expected nil for empty error slice")
	}

	single := NewMultiError([]error{errors.New("only")})
	if single.Error() != "only" {
		t.Fatalf("expected single error passthrough, got %q", single.Error())
	}
}

func TestMultiError_Unwrap(t *testing.T) {
	e1 := errors.New("a")
	e2 := errors.New("b")
	multi := NewMultiError([]error{e1, e2}).(*MultiError)
	unwrapped := multi.Unwrap()
	if len(unwrapped) != 2 {
		t.Fatalf("expected 2 errors, got %d", len(unwrapped))
	}
	if unwrapped[0] != e1 || unwrapped[1] != e2 {
		t.Fatal("expected original errors in order")
	}

	var nilMulti *MultiError
	if nilMulti.Unwrap() != nil {
		t.Fatal("expected nil for nil MultiError.Unwrap")
	}
}

func TestMultiError_DiagnosticCode(t *testing.T) {
	multi := NewMultiError([]error{errors.New("a"), errors.New("b")}).(*MultiError)
	if multi.DiagnosticCode() != "multiple_diagnostics" {
		t.Fatalf("expected code multiple_diagnostics, got %q", multi.DiagnosticCode())
	}
}

func TestMultiError_NilReceiverDiagnosticCategoryAndPath(t *testing.T) {
	var nilMulti *MultiError
	if nilMulti.DiagnosticCategory() != CategoryLoad {
		t.Fatalf("expected default category, got %q", nilMulti.DiagnosticCategory())
	}
	if nilMulti.DiagnosticPath() != "frontend/diagnostics" {
		t.Fatalf("expected default path, got %q", nilMulti.DiagnosticPath())
	}
}

func TestMultiError_DiagnosticsSkipsNilErrors(t *testing.T) {
	multi := NewMultiError([]error{errors.New("a"), nil, errors.New("b")}).(*MultiError)
	diags := multi.Diagnostics()
	if len(diags) != 2 {
		t.Fatalf("expected 2 diagnostics, got %d", len(diags))
	}
}

func TestMultiError_DiagnosticCause(t *testing.T) {
	var nilMulti *MultiError
	if nilMulti.DiagnosticCause() != nil {
		t.Fatal("expected nil cause for nil MultiError")
	}

	emptyMulti := &MultiError{errors: []error{}}
	if emptyMulti.DiagnosticCause() != nil {
		t.Fatal("expected nil cause for empty MultiError")
	}

	multi := NewMultiError([]error{errors.New("first"), errors.New("second")}).(*MultiError)
	cause := multi.DiagnosticCause()
	if cause == nil {
		t.Fatal("expected non-nil cause")
	}
	// Cause chain is built in reverse order
	if cause.Message != "first" {
		t.Fatalf("expected head cause message 'first', got %q", cause.Message)
	}
	if cause.Cause == nil {
		t.Fatal("expected nested cause")
	}
	if cause.Cause.Message != "second" {
		t.Fatalf("expected nested cause message 'second', got %q", cause.Cause.Message)
	}
}

func TestDescriptorEnvelope_StableShape(t *testing.T) {
	env := (Descriptor{
		Category: CategoryRuntime,
		Code:     "division_by_zero",
		Message:  "boom",
		Callable: "calc",
		Stage:    "unary",
		Path:     "script/body",
		Identity: "entity:1",
		Span:     Span{Start: Position{Line: 3, Column: 4}},
		Stack:    []Frame{{Callable: "calc", Stage: "unary"}},
		Cause:    &Descriptor{Category: CategoryHost, Code: "no_evaluator", Message: "inner"},
	}).Envelope()
	if env["category"] != "runtime" || env["code"] != "division_by_zero" || env["path"] != "script/body" {
		t.Fatalf("unexpected envelope root: %+v", env)
	}
	stack, ok := env["stack"].([]map[string]any)
	if !ok || len(stack) != 1 || stack[0]["callable"] != "calc" {
		t.Fatalf("unexpected envelope stack: %+v", env["stack"])
	}
	cause, ok := env["cause"].(map[string]any)
	if !ok || cause["code"] != "no_evaluator" {
		t.Fatalf("unexpected envelope cause: %+v", env["cause"])
	}
}
