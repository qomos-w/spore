package config

import (
	"github.com/qomos-w/spore/diagnostics"
)

// Diagnostic is a structured configuration diagnostic for LLM/tooling consumption.
type Diagnostic struct {
	Code        string
	Category    string // "correct" or "validate"
	Severity    string // "error", "warning", "info"
	Message     string
	Hint        string
	Line        int
	Col         int
	Field       string
	Expected    string
	Actual      string
	Suggestions []string
}

func (d Diagnostic) String() string {
	s := d.Severity + ": " + d.Message
	if d.Code != "" {
		s += " [" + d.Code + "]"
	}
	if d.Hint != "" {
		s += " — " + d.Hint
	}
	return s
}

// Error implements error so a Diagnostic value can be handed directly to
// diagnostics.FromError (and any other error-based API). It renders the
// human-facing message only; code, category, severity and hint travel as
// structured fields rather than being re-encoded into the message.
func (d Diagnostic) Error() string {
	if d.Message != "" {
		return d.Message
	}
	return d.String()
}

// The methods below satisfy the (unexported) interfaces that
// diagnostics.FromError probes, so a config diagnostic is recognized and
// classified by the shared diagnostics envelope instead of falling through
// as an opaque error. They were pinned against diagnostics/from_error.go:
// coder, categorizer, spanner, pather, expecteder, actualer and hinter.
//
// These are additive: the existing fields keep their meaning and the
// diagnostics.Descriptor has no severity dimension, so config severity stays
// on the source Diagnostic (see DiagnosticSeverity).

// DiagnosticCode returns the config diagnostic code, surfaced by FromError.
func (d Diagnostic) DiagnosticCode() string { return d.Code }

// DiagnosticCategory classifies the diagnostic in the diagnostics package's
// category space. Config diagnostics are registered in the schema layer, and
// the code registry is authoritative; CategorySchema is the documented
// fallback for an unregistered or empty code.
func (d Diagnostic) DiagnosticCategory() diagnostics.Category {
	return categoryForCode(d.Code)
}

// DiagnosticSeverity reports the config-side severity ("error", "warning",
// "info"). diagnostics.Descriptor carries no severity dimension, so severity
// is preserved on the originating Diagnostic rather than folded into the
// envelope; this accessor makes it reachable from callers that hold the
// error returned to FromError.
func (d Diagnostic) DiagnosticSeverity() string { return d.Severity }

// DiagnosticSpan maps the diagnostic's Line/Col onto a diagnostics source
// span. A zero position yields the zero span (omitted from the envelope).
func (d Diagnostic) DiagnosticSpan() diagnostics.Span {
	if d.Line == 0 && d.Col == 0 {
		return diagnostics.Span{}
	}
	start := diagnostics.Position{Line: d.Line, Column: d.Col}
	return diagnostics.Span{Start: start, End: start}
}

// DiagnosticPath reports the provenance path for config-originated
// diagnostics, mirroring the path convention used by other layers
// (e.g. binding/invocation/stage).
func (d Diagnostic) DiagnosticPath() string { return "config" }

// DiagnosticExpected exposes the expected type/value for guided repair.
func (d Diagnostic) DiagnosticExpected() string { return d.Expected }

// DiagnosticActual exposes the actual type/value for guided repair.
func (d Diagnostic) DiagnosticActual() string { return d.Actual }

// DiagnosticHint exposes the repair hint, falling back to the hint recorded
// in the code registry so FromError can surface guidance even when the field
// is unset.
func (d Diagnostic) DiagnosticHint() string {
	if d.Hint != "" {
		return d.Hint
	}
	return hintForCode(d.Code)
}

// Compile-time guarantees that Diagnostic interoperates with the diagnostics
// package: it is an error and it satisfies every shape FromError probes.
var (
	_ error                                                  = Diagnostic{}
	_ interface{ DiagnosticCode() string }                   = Diagnostic{}
	_ interface{ DiagnosticCategory() diagnostics.Category } = Diagnostic{}
	_ interface{ DiagnosticSpan() diagnostics.Span }         = Diagnostic{}
	_ interface{ DiagnosticPath() string }                   = Diagnostic{}
	_ interface{ DiagnosticExpected() string }               = Diagnostic{}
	_ interface{ DiagnosticActual() string }                 = Diagnostic{}
	_ interface{ DiagnosticHint() string }                   = Diagnostic{}
)

// configDiagnosticCodes is this package's diagnostic-code table; init()
// registers it in order. Kept package-local so config stays autonomous.
var configDiagnosticCodes = []diagnostics.CodeInfo{
	{Code: "config_parse_error", Category: diagnostics.CategorySchema, Description: "config parse error", Hint: "check syntax near the reported line"},
	{Code: "config_int_overflow", Category: diagnostics.CategorySchema, Description: "integer literal out of range for int64", Hint: "use a value within int64 range (-9223372036854775808..9223372036854775807), or write it as a float"},
	{Code: "config_float_overflow", Category: diagnostics.CategorySchema, Description: "numeric literal out of range for float64", Hint: "reduce the magnitude of the literal"},
	{Code: "config_markdown_block", Category: diagnostics.CategorySchema, Description: "config wrapped in markdown code block", Hint: "auto-stripped markdown fencing"},
	{Code: "config_json_quotes", Category: diagnostics.CategorySchema, Description: "JSON-style quoted map keys", Hint: "auto-removed quotes from map keys; use bare identifiers"},
	{Code: "config_json_trailing_comma", Category: diagnostics.CategorySchema, Description: "JSON-style trailing comma before } or ]", Hint: "auto-removed trailing commas"},
	{Code: "config_equals_colon", Category: diagnostics.CategorySchema, Description: "key = value instead of key: value", Hint: "auto-normalized '=' to ':'"},
	{Code: "config_json_brackets", Category: diagnostics.CategorySchema, Description: "JSON-style outer braces", Hint: "auto-stripped outer { } braces"},
	{Code: "config_unknown_field", Category: diagnostics.CategorySchema, Description: "config key not found in target struct", Hint: "remove the unknown key or check spelling"},
	{Code: "config_missing_field", Category: diagnostics.CategorySchema, Description: "required struct field missing from config", Hint: "add the missing key to config"},
	{Code: "config_type_mismatch", Category: diagnostics.CategorySchema, Description: "config value type does not match struct field type", Hint: "use the expected type shown in the diagnostic"},
	{Code: "config_format_rejected", Category: diagnostics.CategorySchema, Description: "input format not recognized as config or JSON", Hint: "use Spore config, JSON, or key=value syntax"},
	{Code: "config_too_many_corrections", Category: diagnostics.CategorySchema, Description: "too many corrections applied, input may be too far from valid syntax", Hint: "rewrite the config in Spore config or JSON syntax and retry"},
	// Pipeline diagnostic codes
	{Code: "pipeline_duplicate_step", Category: diagnostics.CategorySchema, Description: "duplicate step name within a pipeline", Hint: "rename the step or remove the duplicate"},
	{Code: "pipeline_missing_invoke", Category: diagnostics.CategorySchema, Description: "pipeline step is missing required invoke field", Hint: "add invoke: \"<schemaID>:<callableName>\" to the step"},
	{Code: "pipeline_invalid_invoke_ref", Category: diagnostics.CategorySchema, Description: "pipeline step invoke ref has invalid format", Hint: "use format \"<schemaID>:<callableName>\" or \"tool:<toolName>\""},
	{Code: "pipeline_unknown_dependency", Category: diagnostics.CategorySchema, Description: "pipeline step depends on a non-existent step", Hint: "ensure the referenced step exists in the pipeline"},
	{Code: "pipeline_dependency_cycle", Category: diagnostics.CategorySchema, Description: "pipeline contains a circular dependency", Hint: "remove circular dependencies so steps form a DAG"},
	{Code: "pipeline_invalid_timeout", Category: diagnostics.CategorySchema, Description: "pipeline step timeout is not a valid duration", Hint: "use a valid Go duration string like \"10m\", \"30s\", \"1h30m\""},
	// Pipeline registry-aware diagnostic codes
	{Code: "pipeline_unknown_schema", Category: diagnostics.CategorySchema, Description: "pipeline step references a schema that does not exist in the registry", Hint: "ensure the capability schema is registered"},
	{Code: "pipeline_unknown_callable", Category: diagnostics.CategorySchema, Description: "pipeline step references a callable that does not exist in the schema", Hint: "ensure the callable is declared in the capability schema"},
	{Code: "pipeline_input_mismatch", Category: diagnostics.CategorySchema, Description: "pipeline step input keys do not match callable parameters", Hint: "check input keys against the callable's parameter schema"},
}

func init() {
	for _, info := range configDiagnosticCodes {
		diagnostics.RegisterCode(info)
	}
}
