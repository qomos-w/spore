package frontend

import (
	"strings"
	"testing"

	"github.com/qomos-w/spore/diagnostics"
)

func TestSchemaValidationError_DiagnosticExpectedActual(t *testing.T) {
	var nilErr *schemaValidationError
	if nilErr.DiagnosticExpected() != "" {
		t.Fatalf("expected empty Expected for nil error, got %q", nilErr.DiagnosticExpected())
	}
	if nilErr.DiagnosticActual() != "" {
		t.Fatalf("expected empty Actual for nil error, got %q", nilErr.DiagnosticActual())
	}

	e := newSchemaValidationErrorWithTypes("yield_type_mismatch", "types must match", diagnostics.Span{}, "frontend/type/inference", "int", "string")
	sve, ok := e.(*schemaValidationError)
	if !ok {
		t.Fatalf("expected *schemaValidationError, got %T", e)
	}
	if sve.DiagnosticExpected() != "int" {
		t.Fatalf("expected Expected 'int', got %q", sve.DiagnosticExpected())
	}
	if sve.DiagnosticActual() != "string" {
		t.Fatalf("expected Actual 'string', got %q", sve.DiagnosticActual())
	}
	if sve.DiagnosticCode() != "yield_type_mismatch" {
		t.Fatalf("expected code yield_type_mismatch, got %q", sve.DiagnosticCode())
	}
}

func TestSchemaValidationError_FromErrorRoundTrip(t *testing.T) {
	e := newSchemaValidationErrorWithTypes("array_literal_type_mismatch", "elements must share a type",
		diagnostics.Span{Start: diagnostics.Position{Line: 2, Column: 5}}, "frontend/type/inference", "int", "string")
	diag := diagnostics.FromError(e, diagnostics.Descriptor{})
	if diag.Expected != "int" {
		t.Fatalf("expected FromError Expected 'int', got %q", diag.Expected)
	}
	if diag.Actual != "string" {
		t.Fatalf("expected FromError Actual 'string', got %q", diag.Actual)
	}
	if diag.Code != "array_literal_type_mismatch" {
		t.Fatalf("expected code preserved, got %q", diag.Code)
	}
	if diag.Span.Start.Line != 2 || diag.Span.Start.Column != 5 {
		t.Fatalf("expected span preserved, got %+v", diag.Span)
	}

	// Verify Envelope round-trip.
	env := diag.Envelope()
	if env["expected"] != "int" {
		t.Fatalf("expected envelope expected 'int', got %v", env["expected"])
	}
	if env["actual"] != "string" {
		t.Fatalf("expected envelope actual 'string', got %v", env["actual"])
	}
}

func TestParseError_DiagnosticExpectedActual(t *testing.T) {
	e := parseError{line: 1, column: 5, msg: "unexpected token", code: diagParseError, path: "frontend/parse", expected: "identifier", actual: "}"}
	if e.DiagnosticExpected() != "identifier" {
		t.Fatalf("expected Expected 'identifier', got %q", e.DiagnosticExpected())
	}
	if e.DiagnosticActual() != "}" {
		t.Fatalf("expected Actual '}', got %q", e.DiagnosticActual())
	}
}

func TestParseError_FromErrorRoundTrip(t *testing.T) {
	e := parseError{line: 3, column: 8, msg: "unexpected token", code: diagParseError, path: "frontend/parse/token", expected: "type annotation", actual: ")"}
	diag := diagnostics.FromError(e, diagnostics.Descriptor{})
	if diag.Expected != "type annotation" {
		t.Fatalf("expected FromError Expected 'type annotation', got %q", diag.Expected)
	}
	if diag.Actual != ")" {
		t.Fatalf("expected FromError Actual ')', got %q", diag.Actual)
	}
	if diag.Code != "parse_error" {
		t.Fatalf("expected code parse_error, got %q", diag.Code)
	}
	if diag.Path != "frontend/parse/token" {
		t.Fatalf("expected path preserved, got %q", diag.Path)
	}

	payload, err := diag.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	text := string(payload)
	if !strings.Contains(text, `"expected":"type annotation"`) {
		t.Fatalf("expected expected in JSON, got %s", text)
	}
	if !strings.Contains(text, `"actual":")"`) {
		t.Fatalf("expected actual in JSON, got %s", text)
	}
}

func TestSourceEvalError_DiagnosticExpectedActual(t *testing.T) {
	var nilErr *sourceEvalError
	if nilErr.DiagnosticExpected() != "" {
		t.Fatalf("expected empty Expected for nil error, got %q", nilErr.DiagnosticExpected())
	}
	if nilErr.DiagnosticActual() != "" {
		t.Fatalf("expected empty Actual for nil error, got %q", nilErr.DiagnosticActual())
	}

	e := &sourceEvalError{
		code:     diagNoEvaluator,
		category: diagnostics.CategoryHost,
		callable: "test",
		expected: "int",
		actual:   "string",
	}
	if e.DiagnosticExpected() != "int" {
		t.Fatalf("expected Expected 'int', got %q", e.DiagnosticExpected())
	}
	if e.DiagnosticActual() != "string" {
		t.Fatalf("expected Actual 'string', got %q", e.DiagnosticActual())
	}
}

func TestSourceEvalError_FromErrorRoundTrip(t *testing.T) {
	e := &sourceEvalError{
		code:        diagNoEvaluator,
		category:    diagnostics.CategoryHost,
		callable:    "collect",
		stage:       string(diagnostics.CategoryHost),
		span:        diagnostics.Span{Start: diagnostics.Position{Line: 5, Column: 10}},
		baseMessage: "no evaluator",
		expected:    "evaluator",
		actual:      "nil",
	}
	diag := diagnostics.FromError(e, diagnostics.Descriptor{})
	if diag.Expected != "evaluator" {
		t.Fatalf("expected FromError Expected 'evaluator', got %q", diag.Expected)
	}
	if diag.Actual != "nil" {
		t.Fatalf("expected FromError Actual 'nil', got %q", diag.Actual)
	}
	// Note: sourceEvalError does not implement DiagnosticCallable(), so callable is not extracted.
	if diag.Span.Start.Line != 5 {
		t.Fatalf("expected span line 5, got %+v", diag.Span)
	}
}

func TestLoweringError_HasExpectedActualFields(t *testing.T) {
	// Verify that newSchemaValidationErrorWithTypes produces an error with Expected/Actual
	// that can be detected through the expecteder/actualer interfaces.
	e := newSchemaValidationErrorWithTypes("map_literal_type_mismatch", "key types must match",
		diagnostics.Span{}, "frontend/type/inference", "string", "int")

	exp, ok := e.(interface{ DiagnosticExpected() string })
	if !ok {
		t.Fatal("expected error to implement DiagnosticExpected interface")
	}
	if exp.DiagnosticExpected() != "string" {
		t.Fatalf("expected Expected 'string', got %q", exp.DiagnosticExpected())
	}

	act, ok := e.(interface{ DiagnosticActual() string })
	if !ok {
		t.Fatal("expected error to implement DiagnosticActual interface")
	}
	if act.DiagnosticActual() != "int" {
		t.Fatalf("expected Actual 'int', got %q", act.DiagnosticActual())
	}
}
