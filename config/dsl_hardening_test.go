package config

import (
	"fmt"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/qomos-w/spore/diagnostics"
)

// syntaxDiags parses src, requires a failure, and returns every diagnostic the
// parse reported.
func syntaxDiags(t *testing.T, src string) []Diagnostic {
	t.Helper()
	cfg, err := Parse(src)
	if err == nil {
		t.Fatalf("Parse(%q) succeeded, want a syntax error", src)
	}
	if cfg != nil {
		t.Fatalf("Parse(%q) returned a partial config alongside the error: %+v", src, cfg)
	}
	return SyntaxDiagnostics(err)
}

// TestParse_ReportsEveryErrorInOnePass is the multi-error acceptance test: two
// independent defects in one source must both come back from a single Parse
// call, in source order, instead of the parser stopping at the first one.
func TestParse_ReportsEveryErrorInOnePass(t *testing.T) {
	src := "port 8080\nhost: \"ok\"\nlimit: @\nname: \"ok\"\n"

	_, err := Parse(src)
	if err == nil {
		t.Fatal("Parse returned no error for a source with two defects")
	}
	for _, want := range []string{"line 1", "line 3"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error text %q does not report %s", err.Error(), want)
		}
	}

	diags := SyntaxDiagnostics(err)
	if len(diags) != 2 {
		t.Fatalf("got %d diagnostics, want 2: %+v", len(diags), diags)
	}
	if diags[0].Line != 1 {
		t.Errorf("first diagnostic line = %d, want 1 (%+v)", diags[0].Line, diags[0])
	}
	if !strings.Contains(diags[0].Message, "expected ':' or '{'") {
		t.Errorf("first diagnostic message = %q, want a missing-':' report", diags[0].Message)
	}
	if diags[1].Line != 3 {
		t.Errorf("second diagnostic line = %d, want 3 (%+v)", diags[1].Line, diags[1])
	}
	if !strings.Contains(diags[1].Message, "unexpected character") {
		t.Errorf("second diagnostic message = %q, want the lexer's character report", diags[1].Message)
	}
	for _, d := range diags {
		if d.Code == "" || d.Severity == "" || d.Category == "" {
			t.Errorf("diagnostic is missing code/severity/category: %+v", d)
		}
	}
}

// TestParse_ReportsErrorsAfterADamagedBlock proves recovery actually resumes:
// statements that follow a broken construct are still parsed and checked, and a
// trailing closer left over from that construct is not reported twice.
func TestParse_ReportsErrorsAfterADamagedBlock(t *testing.T) {
	src := "db {\n    a { 1\n}\nhost: \"ok\"\n"

	diags := syntaxDiags(t, src)
	if len(diags) != 1 {
		t.Fatalf("got %d diagnostics, want exactly 1 (residue of the damaged block): %+v", len(diags), diags)
	}
	if diags[0].Line != 2 || !strings.Contains(diags[0].Message, "expected map key") {
		t.Errorf("diagnostic = %+v, want the broken map key on line 2", diags[0])
	}
}

// TestParse_IntOverflowIsReported is the overflow acceptance test: an integer
// literal outside int64 range must produce an explicit diagnostic instead of
// silently becoming 0.
func TestParse_IntOverflowIsReported(t *testing.T) {
	diags := syntaxDiags(t, "n: 99999999999999999999")
	if len(diags) != 1 {
		t.Fatalf("got %d diagnostics, want 1: %+v", len(diags), diags)
	}
	got := diags[0]
	if got.Code != CodeConfigIntOverflow {
		t.Errorf("code = %q, want %q", got.Code, CodeConfigIntOverflow)
	}
	if got.Line != 1 || got.Col != 4 {
		t.Errorf("position = line %d col %d, want line 1 col 4", got.Line, got.Col)
	}
	if got.Expected != "int64" {
		t.Errorf("expected = %q, want int64", got.Expected)
	}
	if got.Actual != "99999999999999999999" {
		t.Errorf("actual = %q, want the offending literal", got.Actual)
	}
	if got.Hint == "" {
		t.Error("overflow diagnostic carries no repair hint")
	}
	if !strings.Contains(got.Message, "out of range") {
		t.Errorf("message = %q, want an out-of-range report", got.Message)
	}
}

// TestLexer_NumberOverflowTokens pins the fix at the lexer boundary: the lexeme
// is well-formed, so the failure has to be carried out as an error token with a
// diagnostic rather than dropped on the floor as a zero value.
func TestLexer_NumberOverflowTokens(t *testing.T) {
	intTok := newLexer("99999999999999999999").nextToken()
	if intTok.typ != tokError {
		t.Fatalf("int overflow token type = %d, want tokError", intTok.typ)
	}
	if intTok.diag == nil || intTok.diag.Code != CodeConfigIntOverflow {
		t.Fatalf("int overflow token diagnostic = %+v, want code %q", intTok.diag, CodeConfigIntOverflow)
	}
	if intTok.intVal != 0 {
		t.Errorf("error token carries intVal %d; an out-of-range literal must not yield a value", intTok.intVal)
	}

	floatTok := newLexer(strings.Repeat("9", 400) + ".0").nextToken()
	if floatTok.typ != tokError {
		t.Fatalf("float overflow token type = %d, want tokError", floatTok.typ)
	}
	if floatTok.diag == nil || floatTok.diag.Code != CodeConfigFloatOverflow {
		t.Fatalf("float overflow token diagnostic = %+v, want code %q", floatTok.diag, CodeConfigFloatOverflow)
	}
}

// TestLexer_IntLiteralBoundaries keeps the in-range side of the fix honest:
// valid literals keep parsing with the exact value, including int64's maximum.
func TestLexer_IntLiteralBoundaries(t *testing.T) {
	cases := []struct {
		src  string
		want int64
	}{
		{"0", 0},
		{"42", 42},
		{"9223372036854775807", math.MaxInt64},
		{"00000000000000000042", 42},
	}
	for _, tc := range cases {
		tok := newLexer(tc.src).nextToken()
		if tok.typ != tokIntLit {
			t.Errorf("%q: token type = %d, want tokIntLit", tc.src, tok.typ)
			continue
		}
		if tok.intVal != tc.want {
			t.Errorf("%q: intVal = %d, want %d", tc.src, tok.intVal, tc.want)
		}
	}

	cfg, err := Parse("big: 9223372036854775807")
	if err != nil {
		t.Fatalf("Parse of int64 max failed: %v", err)
	}
	v, ok := cfg.Get("big")
	if !ok || v.IntVal != math.MaxInt64 {
		t.Fatalf("big = %+v (found %v), want %d", v, ok, int64(math.MaxInt64))
	}
}

// TestParse_OverflowAndSyntaxErrorBothReported mixes the two defect classes in
// one source: the overflow diagnostic keeps its own code while the unrelated
// syntax error is reported in the same pass.
func TestParse_OverflowAndSyntaxErrorBothReported(t *testing.T) {
	diags := syntaxDiags(t, "big: 99999999999999999999\nbroken 1\n")
	if len(diags) != 2 {
		t.Fatalf("got %d diagnostics, want 2: %+v", len(diags), diags)
	}
	if diags[0].Code != CodeConfigIntOverflow || diags[0].Line != 1 {
		t.Errorf("first diagnostic = %+v, want %s on line 1", diags[0], CodeConfigIntOverflow)
	}
	if diags[1].Code != CodeConfigParseError || diags[1].Line != 2 {
		t.Errorf("second diagnostic = %+v, want %s on line 2", diags[1], CodeConfigParseError)
	}
}

// TestSyntaxDiagnostics_SingleAndWrapped pins the accessor used by callers (and
// by ParseWithPipeline) to turn a parse failure into the Diagnostic shape: a
// single error and a wrapped error both yield the same one diagnostic.
func TestSyntaxDiagnostics_SingleAndWrapped(t *testing.T) {
	_, err := Parse("x 1")
	if err == nil {
		t.Fatal("expected a syntax error")
	}
	direct := SyntaxDiagnostics(err)
	if len(direct) != 1 {
		t.Fatalf("got %d diagnostics for a single error, want 1: %+v", len(direct), direct)
	}

	wrapped := fmt.Errorf("load config: %w", err)
	fromWrapped := SyntaxDiagnostics(wrapped)
	if len(fromWrapped) != 1 {
		t.Fatalf("got %d diagnostics from the wrapped error, want 1: %+v", len(fromWrapped), fromWrapped)
	}
	if fromWrapped[0].Code != direct[0].Code || fromWrapped[0].Message != direct[0].Message {
		t.Errorf("wrapped diagnostics %+v do not match direct diagnostics %+v", fromWrapped, direct)
	}

	if got := SyntaxDiagnostics(nil); got != nil {
		t.Errorf("SyntaxDiagnostics(nil) = %+v, want nil", got)
	}
}

// TestSyntaxError_ClassifiedByDiagnosticsFromError proves the new error type is
// recognized by the shared diagnostics envelope (same contract as Diagnostic),
// so a syntax error keeps its code, category, path and span for tooling.
func TestSyntaxError_ClassifiedByDiagnosticsFromError(t *testing.T) {
	_, err := Parse("n: 99999999999999999999")
	if err == nil {
		t.Fatal("expected an overflow error")
	}
	desc := diagnostics.FromError(err, diagnostics.Descriptor{})
	if desc.Code != CodeConfigIntOverflow {
		t.Errorf("code = %q, want %q", desc.Code, CodeConfigIntOverflow)
	}
	if desc.Category != diagnostics.CategorySchema {
		t.Errorf("category = %q, want %q", desc.Category, diagnostics.CategorySchema)
	}
	if desc.Path != "config" {
		t.Errorf("path = %q, want config", desc.Path)
	}
	if desc.Span.Start.Line != 1 || desc.Span.Start.Column != 4 {
		t.Errorf("span = %+v, want line 1 col 4", desc.Span)
	}
	if desc.Hint == "" {
		t.Error("envelope carries no hint")
	}

	// The aggregate keeps classifying as a parse failure at the schema layer.
	_, aggErr := Parse("port 8080\nlimit: @\n")
	aggDesc := diagnostics.FromError(aggErr, diagnostics.Descriptor{})
	if aggDesc.Code != CodeConfigParseError {
		t.Errorf("aggregate code = %q, want %q", aggDesc.Code, CodeConfigParseError)
	}
	if aggDesc.Category != diagnostics.CategorySchema {
		t.Errorf("aggregate category = %q, want %q", aggDesc.Category, diagnostics.CategorySchema)
	}
}

// TestParseWithPipeline_SurfacesSyntaxDiagnostics pins the integration: the
// LLM-facing pipeline entry point reports parse diagnostics in the same slice
// as correction and validation diagnostics.
func TestParseWithPipeline_SurfacesSyntaxDiagnostics(t *testing.T) {
	_, diags, err := ParseWithPipeline("port 8080\nlimit: @\n", nil)
	if err == nil {
		t.Fatal("expected a parse error")
	}
	if len(diags) != 2 {
		t.Fatalf("got %d diagnostics, want 2: %+v", len(diags), diags)
	}
	if _, ok := err.(*syntaxErrors); !ok {
		t.Fatalf("parse error = %T, want the *syntaxErrors aggregate", err)
	}
	for _, d := range diags {
		if d.Category != "parse" {
			t.Errorf("diagnostic %+v is not categorized as a parse diagnostic", d)
		}
	}
}

// TestParse_ReportsEveryStepFieldErrorInOnePass covers recovery inside a
// pipeline block: two damaged steps in one pipeline are both reported, each
// against its own line, and the block balance survives so the errors do not
// degrade into a cascade of follow-on reports.
func TestParse_ReportsEveryStepFieldErrorInOnePass(t *testing.T) {
	src := "pipeline p {\n" +
		"    step a {\n" +
		"        invok: \"x:y\"\n" +
		"    }\n" +
		"    step b {\n" +
		"        timeout: 30\n" +
		"    }\n" +
		"}\n"

	diags := syntaxDiags(t, src)
	if len(diags) != 2 {
		t.Fatalf("got %d diagnostics, want 2: %+v", len(diags), diags)
	}
	if diags[0].Line != 3 || !strings.Contains(diags[0].Message, "unknown step field") {
		t.Errorf("first diagnostic = %+v, want the unknown field on line 3", diags[0])
	}
	if diags[1].Line != 6 || !strings.Contains(diags[1].Message, "expected string for "+PipelineStepFieldTimeout) {
		t.Errorf("second diagnostic = %+v, want the bad value on line 6", diags[1])
	}
}

// TestParse_StrayCloserIsStillReported pins the boundary of the residue rule:
// a closing brace is only dropped when an error was already reported, so a
// stray closer on otherwise clean input stays an error.
func TestParse_StrayCloserIsStillReported(t *testing.T) {
	diags := syntaxDiags(t, "}\n")
	if len(diags) != 1 || diags[0].Line != 1 {
		t.Fatalf("got %+v, want a single error on line 1", diags)
	}
	if !strings.Contains(diags[0].Message, "expected identifier") {
		t.Errorf("message = %q, want an expected-identifier report", diags[0].Message)
	}
}

// TestParse_MalformedSourcesAlwaysReportAndNeverHang exercises the recovery
// paths on truncated and hostile inputs: every one of them must terminate, must
// not panic, and must leave at least one inspectable diagnostic behind.
func TestParse_MalformedSourcesAlwaysReportAndNeverHang(t *testing.T) {
	sources := []string{
		"}",
		"]",
		"@",
		"@@@",
		"x:",
		"x: {",
		"x: [",
		"x: [1, 2",
		"a {\n b {\n",
		"struct",
		"struct S {",
		"struct S { a: }",
		"pipeline",
		"pipeline p",
		"pipeline p {",
		"pipeline p {\n step a\n",
		"pipeline p {\n step a {\n",
		"x: @",
		"n: 99999999999999999999",
		strings.Repeat("x: 1\n", 50) + "broken 1\n",
	}
	for _, src := range sources {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Parse(%q) panicked: %v", src, r)
				}
			}()
			cfg, err := Parse(src)
			if err == nil {
				t.Fatalf("Parse(%q) reported no error", src)
			}
			if cfg != nil {
				t.Errorf("Parse(%q) returned a config alongside the error: %+v", src, cfg)
			}
			if len(SyntaxDiagnostics(err)) == 0 {
				t.Errorf("Parse(%q) produced no inspectable diagnostic", src)
			}
		}()
	}
}

// TestPipelineStepFields_SingleSourceTable pins the single-source invariant for
// the step DSL field names: every field in the table is accepted by the parser
// and lands on the AST, the parser's unknown-field diagnostic enumerates the
// table, and the validators quote the table's hints. A new field cannot be
// added to one side alone.
func TestPipelineStepFields_SingleSourceTable(t *testing.T) {
	samples := map[string]string{
		PipelineStepFieldInvoke:    `"x:y"`,
		PipelineStepFieldInput:     `{a: 1}`,
		PipelineStepFieldDependsOn: `["prev"]`,
		PipelineStepFieldTimeout:   `"30s"`,
		PipelineStepFieldWhen:      `prev.output`,
	}
	if len(samples) != len(pipelineStepFields) {
		t.Fatalf("test covers %d fields but the table declares %d; extend the sample map", len(samples), len(pipelineStepFields))
	}

	for _, spec := range pipelineStepFields {
		sample, ok := samples[spec.Name]
		if !ok {
			t.Fatalf("field %q is in the table but has no sample value in this test", spec.Name)
		}
		src := fmt.Sprintf("pipeline p {\n    step s {\n        %s: %s\n    }\n}\n", spec.Name, sample)
		cfg, err := Parse(src)
		if err != nil {
			t.Fatalf("parser rejects table field %q: %v", spec.Name, err)
		}
		if len(cfg.Pipelines) != 1 || len(cfg.Pipelines[0].Steps) != 1 {
			t.Fatalf("field %q: unexpected pipeline AST %+v", spec.Name, cfg.Pipelines)
		}
		step := cfg.Pipelines[0].Steps[0]
		switch spec.Name {
		case PipelineStepFieldInvoke:
			if step.Invoke != "x:y" {
				t.Errorf("invoke = %q, want x:y", step.Invoke)
			}
		case PipelineStepFieldInput:
			if step.Input.Kind != ValueMap {
				t.Errorf("input kind = %d, want a map value", step.Input.Kind)
			}
		case PipelineStepFieldDependsOn:
			if len(step.DependsOn) != 1 || step.DependsOn[0] != "prev" {
				t.Errorf("depends_on = %v, want [prev]", step.DependsOn)
			}
		case PipelineStepFieldTimeout:
			if step.Timeout != "30s" {
				t.Errorf("timeout = %q, want 30s", step.Timeout)
			}
		case PipelineStepFieldWhen:
			if step.When == nil || step.When.StepName != "prev" {
				t.Errorf("when = %+v, want the prev.output reference", step.When)
			}
		default:
			t.Fatalf("field %q reaches no AST assertion; wire it into this test and into parseStepFieldValue", spec.Name)
		}
	}

	// An unknown field is reported against the table, with a spelling repair.
	unknown := syntaxDiags(t, "pipeline p {\n    step s {\n        invok: \"x:y\"\n    }\n}\n")
	if len(unknown) != 1 {
		t.Fatalf("got %d diagnostics for the unknown field, want 1: %+v", len(unknown), unknown)
	}
	if !strings.Contains(unknown[0].Message, pipelineStepFieldList()) {
		t.Errorf("message %q does not enumerate the canonical fields %q", unknown[0].Message, pipelineStepFieldList())
	}
	if unknown[0].Actual != "invok" {
		t.Errorf("actual = %q, want invok", unknown[0].Actual)
	}
	if len(unknown[0].Suggestions) == 0 || unknown[0].Suggestions[0] != PipelineStepFieldInvoke {
		t.Errorf("suggestions = %v, want %s first", unknown[0].Suggestions, PipelineStepFieldInvoke)
	}

	// The validator's missing-field guidance comes from the same table.
	cfg, err := Parse("pipeline p {\n    step s {\n        input: {a: 1}\n    }\n}\n")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	found := false
	for _, d := range ValidatePipelines()(cfg, nil) {
		if d.Code != "pipeline_missing_invoke" {
			continue
		}
		found = true
		if d.Hint != pipelineStepFieldHint(PipelineStepFieldInvoke) {
			t.Errorf("missing-invoke hint = %q, want the table hint %q", d.Hint, pipelineStepFieldHint(PipelineStepFieldInvoke))
		}
		if !strings.Contains(d.Message, PipelineStepFieldInvoke) {
			t.Errorf("missing-invoke message %q does not name the field constant %q", d.Message, PipelineStepFieldInvoke)
		}
	}
	if !found {
		t.Fatal("expected a pipeline_missing_invoke diagnostic")
	}
}

// TestPipelineStepFieldNames_HaveOneDefinition closes the "hardcoded in two
// places" defect shut: the parser and the validators must read the step field
// names from the table, so the literals may only appear in the table's own
// file. Reintroducing a literal in either file fails here.
func TestPipelineStepFieldNames_HaveOneDefinition(t *testing.T) {
	for _, file := range []string{"parser.go", "orchestration_validator.go"} {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		source := string(data)
		for _, name := range pipelineStepFieldNames() {
			if strings.Contains(source, `"`+name+`"`) {
				t.Errorf("%s hardcodes the step field name %q; read it from pipelineStepFields instead", file, name)
			}
		}
	}
}
