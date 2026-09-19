package diagnostics

import (
	"encoding/json"
	"strings"
	"testing"
)

// mockExpectedActualError implements the full diagnostic interface including
// Expected/Actual for testing FromError extraction.
type mockExpectedActualError struct {
	code     string
	category Category
	message  string
	path     string
	expected string
	actual   string
	hint     string
	span     Span
	stack    []Frame
	cause    *Descriptor
}

func (e mockExpectedActualError) Error() string               { return e.message }
func (e mockExpectedActualError) DiagnosticCode() string       { return e.code }
func (e mockExpectedActualError) DiagnosticCategory() Category { return e.category }
func (e mockExpectedActualError) DiagnosticPath() string       { return e.path }
func (e mockExpectedActualError) DiagnosticExpected() string   { return e.expected }
func (e mockExpectedActualError) DiagnosticActual() string     { return e.actual }
func (e mockExpectedActualError) DiagnosticHint() string       { return e.hint }
func (e mockExpectedActualError) DiagnosticSpan() Span         { return e.span }
func (e mockExpectedActualError) DiagnosticStack() []Frame     { return append([]Frame(nil), e.stack...) }
func (e mockExpectedActualError) DiagnosticCause() *Descriptor {
	if e.cause == nil {
		return nil
	}
	cloned := *e.cause
	return &cloned
}

func TestDescriptorEnvelope_IncludesExpectedActual(t *testing.T) {
	d := Descriptor{
		Category: CategorySchema,
		Code:     "type_mismatch",
		Message:  "types do not match",
		Expected: "int",
		Actual:   "string",
		Hint:     "change the value type to int",
	}
	env := d.Envelope()
	if env["expected"] != "int" {
		t.Fatalf("expected 'int' in envelope, got %v", env["expected"])
	}
	if env["actual"] != "string" {
		t.Fatalf("expected 'string' in envelope, got %v", env["actual"])
	}
	if env["hint"] != "change the value type to int" {
		t.Fatalf("expected hint in envelope, got %v", env["hint"])
	}
}

func TestDescriptorEnvelope_OmitsEmptyExpectedActual(t *testing.T) {
	d := Descriptor{Category: CategoryRuntime, Code: "boom", Message: "error"}
	env := d.Envelope()
	if _, ok := env["expected"]; ok {
		t.Fatal("expected empty 'expected' to be omitted from envelope")
	}
	if _, ok := env["actual"]; ok {
		t.Fatal("expected empty 'actual' to be omitted from envelope")
	}
	if _, ok := env["hint"]; ok {
		t.Fatal("expected empty 'hint' to be omitted from envelope")
	}
}

func TestDescriptorMarshalJSON_IncludesExpectedActual(t *testing.T) {
	d := Descriptor{
		Category: CategorySchema,
		Code:     "type_mismatch",
		Message:  "types do not match",
		Expected: "array<int>",
		Actual:   "array<string>",
		Hint:     "convert array<string> to array<int>",
	}
	payload, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	// Verify by round-tripping through unmarshal to avoid HTML-escape matching issues.
	var roundTrip jsonDescriptor
	if unmarshalErr := json.Unmarshal(payload, &roundTrip); unmarshalErr != nil {
		t.Fatalf("Unmarshal: %v", unmarshalErr)
	}
	if roundTrip.Expected != "array<int>" {
		t.Fatalf("expected Expected 'array<int>', got %q", roundTrip.Expected)
	}
	if roundTrip.Actual != "array<string>" {
		t.Fatalf("expected Actual 'array<string>', got %q", roundTrip.Actual)
	}
	if roundTrip.Hint != "convert array<string> to array<int>" {
		t.Fatalf("expected Hint preserved, got %q", roundTrip.Hint)
	}
}

func TestDescriptorMarshalJSON_OmitsEmptyExpectedActual(t *testing.T) {
	d := Descriptor{Category: CategoryRuntime, Code: "boom", Message: "error"}
	payload, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	text := string(payload)
	if strings.Contains(text, `"expected"`) {
		t.Fatalf("expected empty expected omitted, got %s", text)
	}
	if strings.Contains(text, `"actual"`) {
		t.Fatalf("expected empty actual omitted, got %s", text)
	}
	if strings.Contains(text, `"hint"`) {
		t.Fatalf("expected empty hint omitted, got %s", text)
	}
}

func TestFromError_ExtractsExpectedActual(t *testing.T) {
	err := mockExpectedActualError{
		code:     "invalid_argument_type",
		category: CategoryContract,
		message:  "arg invalid",
		path:     "binding/args/type",
		expected: "int",
		actual:   "string",
		hint:     "convert the argument to int",
		span:     Span{Start: Position{Line: 3, Column: 4}},
		stack:    []Frame{{Callable: "add", Stage: "unary"}},
	}
	diag := FromError(err, Descriptor{})
	if diag.Code != "invalid_argument_type" {
		t.Fatalf("expected code invalid_argument_type, got %q", diag.Code)
	}
	if diag.Expected != "int" {
		t.Fatalf("expected Expected 'int', got %q", diag.Expected)
	}
	if diag.Actual != "string" {
		t.Fatalf("expected Actual 'string', got %q", diag.Actual)
	}
	if diag.Hint != "convert the argument to int" {
		t.Fatalf("expected Hint preserved, got %q", diag.Hint)
	}
	if diag.Category != CategoryContract {
		t.Fatalf("expected category contract, got %q", diag.Category)
	}
	if diag.Path != "binding/args/type" {
		t.Fatalf("expected path binding/args/type, got %q", diag.Path)
	}
	if len(diag.Stack) != 1 || diag.Stack[0].Callable != "add" {
		t.Fatalf("expected stack preserved, got %+v", diag.Stack)
	}
}

func TestFromError_FallbackPreservesExpectedActual(t *testing.T) {
	// When the error does NOT implement expecteder/actualer, fallback descriptor's
	// Expected/Actual should be preserved.
	fallback := Descriptor{
		Code:     "fallback_code",
		Message:  "fallback msg",
		Expected: "fallback_expected",
		Actual:   "fallback_actual",
	}
	plainErr := &plainError{msg: "plain"}
	diag := FromError(plainErr, fallback)
	if diag.Expected != "fallback_expected" {
		t.Fatalf("expected fallback Expected preserved, got %q", diag.Expected)
	}
	if diag.Actual != "fallback_actual" {
		t.Fatalf("expected fallback Actual preserved, got %q", diag.Actual)
	}
}

func TestFromError_ErrorOverridesFallbackExpectedActual(t *testing.T) {
	// When error implements expecteder/actualer, those values override fallback.
	fallback := Descriptor{
		Code:     "fallback_code",
		Message:  "fallback msg",
		Expected: "fallback_expected",
		Actual:   "fallback_actual",
	}
	err := mockExpectedActualError{
		code:     "override",
		message:  "override msg",
		expected: "err_expected",
		actual:   "err_actual",
	}
	diag := FromError(err, fallback)
	if diag.Expected != "err_expected" {
		t.Fatalf("expected error Expected to override fallback, got %q", diag.Expected)
	}
	if diag.Actual != "err_actual" {
		t.Fatalf("expected error Actual to override fallback, got %q", diag.Actual)
	}
}

func TestDescriptorEnvelope_NestedCauseWithExpectedActual(t *testing.T) {
	d := Descriptor{
		Category: CategorySchema,
		Code:     "outer",
		Message:  "outer error",
		Expected: "struct",
		Actual:   "class",
		Cause: &Descriptor{
			Category: CategorySchema,
			Code:     "inner",
			Message:  "inner error",
			Expected: "int",
			Actual:   "string",
		},
	}
	env := d.Envelope()
	if env["expected"] != "struct" {
		t.Fatalf("expected outer expected, got %v", env["expected"])
	}
	cause, ok := env["cause"].(map[string]any)
	if !ok {
		t.Fatalf("expected cause map, got %T", env["cause"])
	}
	if cause["expected"] != "int" {
		t.Fatalf("expected inner expected, got %v", cause["expected"])
	}
	if cause["actual"] != "string" {
		t.Fatalf("expected inner actual, got %v", cause["actual"])
	}
}

func TestDescriptorMarshalJSON_NestedCauseWithExpectedActual(t *testing.T) {
	d := Descriptor{
		Category: CategorySchema,
		Code:     "outer",
		Message:  "outer error",
		Expected: "struct",
		Actual:   "class",
		Cause: &Descriptor{
			Category: CategorySchema,
			Code:     "inner",
			Message:  "inner error",
			Expected: "int",
			Actual:   "string",
		},
	}
	payload, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	text := string(payload)
	if !strings.Contains(text, `"expected":"struct"`) {
		t.Fatalf("expected outer expected in JSON, got %s", text)
	}
	if !strings.Contains(text, `"expected":"int"`) {
		t.Fatalf("expected inner expected in JSON, got %s", text)
	}
	if !strings.Contains(text, `"actual":"string"`) {
		t.Fatalf("expected inner actual in JSON, got %s", text)
	}
}

type plainError struct{ msg string }

func (e *plainError) Error() string { return e.msg }
