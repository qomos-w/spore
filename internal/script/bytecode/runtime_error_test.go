package bytecode

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/qomos-w/spore/diagnostics"
)

func TestRuntimeError_DiagnosticStackPrefersExplicitStack(t *testing.T) {
	err := &RuntimeError{Code: "division_by_zero", Stack: nil, Callable: "div"}
	stack := err.DiagnosticStack()
	if len(stack) != 1 || stack[0].Callable != "div" {
		t.Fatalf("expected fallback callable stack, got %+v", stack)
	}
}

func TestRuntimeError_FallbackStackAlwaysCarriesCallable(t *testing.T) {
	err := &RuntimeError{Code: "division_by_zero", Callable: "div"}
	stack := err.DiagnosticStack()
	if len(stack) == 0 {
		t.Fatal("expected non-empty diagnostic stack")
	}
	if stack[0].Callable != "div" {
		t.Fatalf("expected callable div in fallback stack, got %+v", stack)
	}
}

func TestRuntimeError_DiagnosticCodeOrCodeIsStable(t *testing.T) {
	err := &RuntimeError{Code: "division_by_zero", Callable: "div"}

	if coder, ok := any(err).(interface{ DiagnosticCode() string }); ok {
		if coder.DiagnosticCode() != "division_by_zero" {
			t.Fatalf("expected DiagnosticCode division_by_zero, got %q", coder.DiagnosticCode())
		}
		return
	}

	if err.Code != "division_by_zero" {
		t.Fatalf("expected Code division_by_zero, got %q", err.Code)
	}
}

func TestRuntimeError_DiagnosticExpectedActual(t *testing.T) {
	var nilErr *RuntimeError
	if nilErr.DiagnosticExpected() != "" {
		t.Fatalf("expected empty Expected for nil RuntimeError, got %q", nilErr.DiagnosticExpected())
	}
	if nilErr.DiagnosticActual() != "" {
		t.Fatalf("expected empty Actual for nil RuntimeError, got %q", nilErr.DiagnosticActual())
	}

	err := &RuntimeError{Code: "type_cast_failed", Callable: "as", Expected: "Player", Actual: "Enemy", Message: "cast failed"}
	if err.DiagnosticExpected() != "Player" {
		t.Fatalf("expected Expected 'Player', got %q", err.DiagnosticExpected())
	}
	if err.DiagnosticActual() != "Enemy" {
		t.Fatalf("expected Actual 'Enemy', got %q", err.DiagnosticActual())
	}
}

func TestRuntimeError_FromErrorRoundTripExpectedActual(t *testing.T) {
	err := &RuntimeError{Code: "type_cast_failed", Callable: "as", Expected: "int", Actual: "string", Message: "cast failed", Path: "bytecode/op"}
	diag := diagnostics.FromError(err, diagnostics.Descriptor{})
	if diag.Expected != "int" {
		t.Fatalf("expected FromError Expected 'int', got %q", diag.Expected)
	}
	if diag.Actual != "string" {
		t.Fatalf("expected FromError Actual 'string', got %q", diag.Actual)
	}
	if diag.Code != "type_cast_failed" {
		t.Fatalf("expected FromError code preserved, got %q", diag.Code)
	}
	if diag.Path != "bytecode/op" {
		t.Fatalf("expected FromError path preserved, got %q", diag.Path)
	}

	// Verify JSON serialization includes Expected/Actual.
	payload, marshalErr := diag.MarshalJSON()
	if marshalErr != nil {
		t.Fatalf("MarshalJSON: %v", marshalErr)
	}
	text := string(payload)
	if !strings.Contains(text, `"expected":"int"`) {
		t.Fatalf("expected expected in JSON, got %s", text)
	}
	if !strings.Contains(text, `"actual":"string"`) {
		t.Fatalf("expected actual in JSON, got %s", text)
	}
}

func TestRuntimeError_AllDiagnosticCodesRegistered(t *testing.T) {
	type entry struct {
		code     string
		category diagnostics.Category
	}
	entries := []entry{
		{"type_cast_failed", diagnostics.CategoryRuntime},
		{"stream_exhausted", diagnostics.CategoryStream},
		{"division_by_zero", diagnostics.CategoryRuntime},
		{"modulo_by_zero", diagnostics.CategoryRuntime},
		{"stack_overflow", diagnostics.CategoryRuntime},
		{"native_call_failed", diagnostics.CategoryHost},
		{"local_index_out_of_bounds", diagnostics.CategoryRuntime},
		{"local_store_out_of_bounds", diagnostics.CategoryRuntime},
		{"global_index_out_of_bounds", diagnostics.CategoryRuntime},
		{"undefined_function", diagnostics.CategoryRuntime},
		{"undefined_native_callable", diagnostics.CategoryRuntime},
		{"class_not_found", diagnostics.CategoryRuntime},
		{"invalid_map_key_type", diagnostics.CategoryRuntime},
		{"map_key_not_found", diagnostics.CategoryRuntime},
		{"invalid_array_index_type", diagnostics.CategoryRuntime},
		{"array_index_out_of_range", diagnostics.CategoryRuntime},
		{"struct_not_found", diagnostics.CategoryRuntime},
		{"unknown_opcode", diagnostics.CategoryRuntime},
		{"unsupported_vm_argument_type", diagnostics.CategoryRuntime},
	}
	for _, e := range entries {
		info, ok := diagnostics.LookupCode(e.code)
		if !ok {
			t.Fatalf("expected runtime diagnostic code %q to be registered", e.code)
		}
		if info.Category != e.category {
			t.Fatalf("code %q registered under category %q, want %q", e.code, info.Category, e.category)
		}
		if info.Hint == "" {
			t.Fatalf("code %q registered without a hint", e.code)
		}
		if info.Description == "" {
			t.Fatalf("code %q registered without a description", e.code)
		}
	}
}

func TestRuntimeError_AllRuntimeErrorCodesAreRegistered(t *testing.T) {
	files := []string{"interpreter.go", "vm_evaluator.go"}
	re := regexp.MustCompile(`RuntimeError\{[^}]*Code:\s*"([a-z_]+)"`)
	seen := map[string]struct{}{}
	for _, name := range files {
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, m := range re.FindAllSubmatch(source, -1) {
			seen[string(m[1])] = struct{}{}
		}
	}
	if len(seen) == 0 {
		t.Fatal("expected to find at least one RuntimeError{Code: ...} literal")
	}
	for code := range seen {
		if _, ok := diagnostics.LookupCode(code); !ok {
			t.Errorf("runtime diagnostic code %q is emitted but not registered with diagnostics package", code)
		}
	}
}
