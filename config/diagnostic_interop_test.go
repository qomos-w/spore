package config_test

import (
	"testing"

	"github.com/qomos-w/spore/config"
	"github.com/qomos-w/spore/diagnostics"
)

// allConfigDiagnosticCodes mirrors the codes registered in config's init();
// it is the shared fixture for the interop tests below.
var allConfigDiagnosticCodes = []string{
	"config_parse_error",
	"config_int_overflow",
	"config_float_overflow",
	"config_markdown_block",
	"config_json_quotes",
	"config_json_trailing_comma",
	"config_equals_colon",
	"config_json_brackets",
	"config_unknown_field",
	"config_missing_field",
	"config_type_mismatch",
	"config_format_rejected",
	"config_too_many_corrections",
	"pipeline_duplicate_step",
	"pipeline_missing_invoke",
	"pipeline_invalid_invoke_ref",
	"pipeline_unknown_dependency",
	"pipeline_dependency_cycle",
	"pipeline_invalid_timeout",
	"pipeline_unknown_schema",
	"pipeline_unknown_callable",
	"pipeline_input_mismatch",
}

// TestConfigDiagnostic_ImplementsDiagnosticsInterfaces pins the shapes that
// diagnostics.FromError probes. If any of these regress, FromError silently
// degrades a config diagnostic into an opaque error.
func TestConfigDiagnostic_ImplementsDiagnosticsInterfaces(t *testing.T) {
	var d config.Diagnostic
	var _ error = d
	var _ interface{ DiagnosticCode() string } = d
	var _ interface{ DiagnosticCategory() diagnostics.Category } = d
	var _ interface{ DiagnosticSpan() diagnostics.Span } = d
	var _ interface{ DiagnosticPath() string } = d
	var _ interface{ DiagnosticExpected() string } = d
	var _ interface{ DiagnosticActual() string } = d
	var _ interface{ DiagnosticHint() string } = d

	if got := d.DiagnosticPath(); got != "config" {
		t.Fatalf("DiagnosticPath() = %q, want %q", got, "config")
	}
}

// TestConfigDiagnostic_CategoryMatchesRegistry proves the category FromError
// reports for a config diagnostic is the one registered for its code, so the
// two diagnostic systems agree instead of drifting.
func TestConfigDiagnostic_CategoryMatchesRegistry(t *testing.T) {
	for _, code := range allConfigDiagnosticCodes {
		info, ok := diagnostics.LookupCode(code)
		if !ok {
			t.Fatalf("code %q not registered", code)
		}
		d := config.Diagnostic{Code: code, Severity: "error", Message: "m"}
		if got, want := d.DiagnosticCategory(), info.Category; got != want {
			t.Fatalf("code %q: DiagnosticCategory() = %q, want registered %q", code, got, want)
		}
		if d.DiagnosticCategory() != diagnostics.CategorySchema {
			t.Fatalf("code %q: config diagnostics must classify at the schema layer", code)
		}
	}
	// Unregistered / empty code falls back to the documented schema default.
	if got := (config.Diagnostic{}).DiagnosticCategory(); got != diagnostics.CategorySchema {
		t.Fatalf("empty diagnostic DiagnosticCategory() = %q, want CategorySchema", got)
	}
}

// TestFromError_ClassifiesConfigDiagnostic is the end-to-end acceptance test:
// an error whose dynamic type is config.Diagnostic is recognized and classified
// by diagnostics.FromError.
func TestFromError_ClassifiesConfigDiagnostic(t *testing.T) {
	cases := []struct {
		name string
		diag config.Diagnostic
	}{
		{
			name: "validation error with expected/actual and position",
			diag: config.Diagnostic{
				Code:     "config_type_mismatch",
				Category: "validate",
				Severity: "error",
				Message:  `field "Port": expected int, got string`,
				Hint:     "change the value to type int",
				Line:     4,
				Col:      7,
				Field:    "Port",
				Expected: "int",
				Actual:   "string",
			},
		},
		{
			name: "unknown-field warning",
			diag: config.Diagnostic{
				Code:     "config_unknown_field",
				Category: "validate",
				Severity: "warning",
				Message:  `unknown config key "por"`,
				Hint:     "did you mean port?",
			},
		},
		{
			name: "auto-correction info",
			diag: config.Diagnostic{
				Code:     "config_markdown_block",
				Category: "correct",
				Severity: "info",
				Message:  "stripped markdown code block fencing",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var err error = tc.diag // must satisfy error
			got := diagnostics.FromError(err, diagnostics.Descriptor{})

			if got.Code != tc.diag.Code {
				t.Fatalf("code = %q, want %q", got.Code, tc.diag.Code)
			}
			if got.Category != diagnostics.CategorySchema {
				t.Fatalf("category = %q, want %q", got.Category, diagnostics.CategorySchema)
			}
			if got.Message != tc.diag.Message {
				t.Fatalf("message = %q, want %q", got.Message, tc.diag.Message)
			}
			if got.Path != "config" {
				t.Fatalf("path = %q, want %q", got.Path, "config")
			}
			if got.Expected != tc.diag.Expected {
				t.Fatalf("expected = %q, want %q", got.Expected, tc.diag.Expected)
			}
			if got.Actual != tc.diag.Actual {
				t.Fatalf("actual = %q, want %q", got.Actual, tc.diag.Actual)
			}
			if tc.diag.Hint != "" && got.Hint != tc.diag.Hint {
				t.Fatalf("hint = %q, want %q", got.Hint, tc.diag.Hint)
			}
			if tc.diag.Line != 0 && got.Span.Start.Line != tc.diag.Line {
				t.Fatalf("span start line = %d, want %d", got.Span.Start.Line, tc.diag.Line)
			}
			if tc.diag.Col != 0 && got.Span.Start.Column != tc.diag.Col {
				t.Fatalf("span start column = %d, want %d", got.Span.Start.Column, tc.diag.Col)
			}
			// Severity is preserved on the source diagnostic (the descriptor has
			// no severity slot) and remains reachable through the error value.
			if sev := tc.diag.DiagnosticSeverity(); sev != tc.diag.Severity {
				t.Fatalf("severity = %q, want %q", sev, tc.diag.Severity)
			}
		})
	}
}

// TestFromError_ConfigDiagnosticSeverityMapping documents the severity -> layer
// mapping: every config severity classifies at the schema layer, and the
// severity itself stays readable on the originating diagnostic.
func TestFromError_ConfigDiagnosticSeverityMapping(t *testing.T) {
	for _, severity := range []string{"error", "warning", "info"} {
		d := config.Diagnostic{
			Code:     "config_parse_error",
			Category: "validate",
			Severity: severity,
			Message:  "boom",
		}
		got := diagnostics.FromError(d, diagnostics.Descriptor{})
		if got.Category != diagnostics.CategorySchema {
			t.Fatalf("severity %q: category = %q, want %q", severity, got.Category, diagnostics.CategorySchema)
		}
		if d.DiagnosticSeverity() != severity {
			t.Fatalf("severity %q: DiagnosticSeverity() = %q", severity, d.DiagnosticSeverity())
		}
	}
}

// TestFromError_ConfigDiagnosticHintFallsBackToRegistry verifies FromError can
// surface a usable repair hint even when the diagnostic carries none itself.
func TestFromError_ConfigDiagnosticHintFallsBackToRegistry(t *testing.T) {
	got := diagnostics.FromError(config.Diagnostic{
		Code:     "config_parse_error",
		Severity: "error",
		Message:  "unexpected token",
	}, diagnostics.Descriptor{})

	info, ok := diagnostics.LookupCode("config_parse_error")
	if !ok {
		t.Fatal("config_parse_error not registered")
	}
	if info.Hint == "" {
		t.Fatal("config_parse_error has no registered hint")
	}
	if got.Hint != info.Hint {
		t.Fatalf("hint = %q, want registry hint %q", got.Hint, info.Hint)
	}
}

// TestFromError_ConfigDiagnosticFallbackMessageKept verifies an explicit
// fallback message wins, matching FromError semantics for any error.
func TestFromError_ConfigDiagnosticFallbackMessageKept(t *testing.T) {
	got := diagnostics.FromError(config.Diagnostic{
		Code:     "config_missing_field",
		Severity: "error",
		Message:  "missing required field \"Name\"",
	}, diagnostics.Descriptor{Message: "loading config"})

	if got.Message != "loading config" {
		t.Fatalf("message = %q, want fallback message preserved", got.Message)
	}
	if got.Code != "config_missing_field" {
		t.Fatalf("code = %q, want config_missing_field", got.Code)
	}
}

// TestConfigDiagnostic_ErrorReturnsMessage pins the error rendering used when a
// Diagnostic is passed as an error: the plain message, not the decorated
// String() form (which would duplicate code/hint into the envelope message).
func TestConfigDiagnostic_ErrorReturnsMessage(t *testing.T) {
	d := config.Diagnostic{Code: "config_parse_error", Severity: "error", Message: "unexpected token"}
	var err error = d
	if err.Error() != "unexpected token" {
		t.Fatalf("Error() = %q, want %q", err.Error(), "unexpected token")
	}
	// Fallback when Message is empty: still an error, uses the decorated form.
	empty := config.Diagnostic{Code: "config_parse_error", Severity: "error"}
	if empty.Error() == "" {
		t.Fatal("Error() must be non-empty")
	}
}
