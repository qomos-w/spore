package config

import (
	"fmt"
	"strings"

	"github.com/qomos-w/spore/diagnostics"
)

// Syntax error codes. Every one of them is registered in init() (see
// diagnostic.go), so diagnostics.FromError classifies a syntax error by code
// and category exactly like a corrector or validator diagnostic.
const (
	// CodeConfigParseError marks a grammar error: a token that cannot appear
	// where it was found.
	CodeConfigParseError = "config_parse_error"
	// CodeConfigIntOverflow marks an integer literal that does not fit int64.
	// Before this code existed such a literal was silently parsed as 0.
	CodeConfigIntOverflow = "config_int_overflow"
	// CodeConfigFloatOverflow marks a numeric literal that does not fit float64.
	CodeConfigFloatOverflow = "config_float_overflow"
)

// SyntaxError is one structured syntax error produced by the lexer or the
// parser: what went wrong, where, and what repair the caller can apply.
//
// A single parse reports several of them (see SyntaxDiagnostics). The parser is
// deliberately not a full error-recovering parser: it collects every error it
// can report and stops at the first construct it cannot recover from.
type SyntaxError struct {
	Code        string // a CodeConfig* constant
	Message     string
	Hint        string
	Line        int
	Col         int
	Expected    string
	Actual      string
	Suggestions []string
}

// Error renders the position together with the message, so a wrapped error
// (`fmt.Errorf("load %s: %w", path, err)`) still tells the caller where to look.
func (e *SyntaxError) Error() string {
	if e == nil {
		return ""
	}
	if e.Line > 0 {
		return fmt.Sprintf("config parse error at line %d, col %d: %s", e.Line, e.Col, e.Message)
	}
	return "config parse error: " + e.Message
}

// Diagnostic converts the syntax error into the config Diagnostic shape used by
// correctors and validators, so a caller can handle syntax and validation
// problems uniformly.
func (e *SyntaxError) Diagnostic() Diagnostic {
	if e == nil {
		return Diagnostic{}
	}
	return Diagnostic{
		Code:        e.DiagnosticCode(),
		Category:    "parse",
		Severity:    "error",
		Message:     e.Message,
		Hint:        e.DiagnosticHint(),
		Line:        e.Line,
		Col:         e.Col,
		Expected:    e.Expected,
		Actual:      e.Actual,
		Suggestions: append([]string(nil), e.Suggestions...),
	}
}

// Diagnostics returns the syntax errors carried by err as config diagnostics.
// It is the error-side counterpart of Parse: one call turns a parse failure
// into the same []Diagnostic shape the pipeline returns for corrections and
// validation, including when the parse reported several errors at once.
func SyntaxDiagnostics(err error) []Diagnostic {
	if err == nil {
		return nil
	}
	var out []Diagnostic
	collectSyntaxDiagnostics(err, &out)
	if len(out) == 0 {
		// Not a structured parse failure (e.g. a file read error): report the
		// error as a single, position-less diagnostic rather than nothing.
		out = append(out, Diagnostic{
			Code:     CodeConfigParseError,
			Category: "parse",
			Severity: "error",
			Message:  err.Error(),
		})
	}
	return out
}

func collectSyntaxDiagnostics(err error, out *[]Diagnostic) {
	switch e := err.(type) {
	case nil:
		return
	case *SyntaxError:
		*out = append(*out, e.Diagnostic())
	case *syntaxErrors:
		for _, child := range e.errs {
			*out = append(*out, child.Diagnostic())
		}
	case interface{ Unwrap() []error }:
		for _, child := range e.Unwrap() {
			collectSyntaxDiagnostics(child, out)
		}
	case interface{ Unwrap() error }:
		collectSyntaxDiagnostics(e.Unwrap(), out)
	}
}

// newSyntaxErrors aggregates the errors reported by one parse pass. A single
// error is returned as-is, so the common case keeps the plain *SyntaxError
// shape; several errors are wrapped so SyntaxDiagnostics and errors.As can
// reach all of them.
func newSyntaxErrors(errs []*SyntaxError) error {
	switch len(errs) {
	case 0:
		return nil
	case 1:
		return errs[0]
	}
	return &syntaxErrors{errs: append([]*SyntaxError(nil), errs...)}
}

// syntaxErrors aggregates every syntax error reported by one parse pass.
type syntaxErrors struct {
	errs []*SyntaxError
}

// Error lists every reported error, in source order.
func (e *syntaxErrors) Error() string {
	if e == nil || len(e.errs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(e.errs))
	for _, err := range e.errs {
		parts = append(parts, err.Error())
	}
	return fmt.Sprintf("config parse failed with %d errors: %s", len(parts), strings.Join(parts, "; "))
}

// Unwrap exposes the individual errors to errors.Is/errors.As and to
// SyntaxDiagnostics.
func (e *syntaxErrors) Unwrap() []error {
	if e == nil {
		return nil
	}
	unwrapped := make([]error, 0, len(e.errs))
	for _, err := range e.errs {
		unwrapped = append(unwrapped, err)
	}
	return unwrapped
}

// SyntaxErrors returns the errors aggregated by this value.
func (e *syntaxErrors) SyntaxErrors() []*SyntaxError {
	if e == nil {
		return nil
	}
	return append([]*SyntaxError(nil), e.errs...)
}

// DiagnosticCode reports the aggregate as a parse failure; the individual
// errors keep their own, more specific codes.
func (e *syntaxErrors) DiagnosticCode() string { return CodeConfigParseError }

func (e *syntaxErrors) DiagnosticCategory() diagnostics.Category {
	return CategoryForCode(CodeConfigParseError)
}

func (e *syntaxErrors) DiagnosticPath() string { return "config" }

func (e *syntaxErrors) DiagnosticHint() string {
	return HintForCode(CodeConfigParseError)
}

// The methods below satisfy the interfaces diagnostics.FromError probes, so a
// syntax error is recognized and classified by the shared diagnostics envelope
// instead of falling through as an opaque error (same contract as Diagnostic).
func (e *SyntaxError) DiagnosticCode() string {
	if e == nil || e.Code == "" {
		return CodeConfigParseError
	}
	return e.Code
}

func (e *SyntaxError) DiagnosticCategory() diagnostics.Category {
	return CategoryForCode(e.DiagnosticCode())
}

func (e *SyntaxError) DiagnosticSpan() diagnostics.Span {
	if e == nil || (e.Line == 0 && e.Col == 0) {
		return diagnostics.Span{}
	}
	start := diagnostics.Position{Line: e.Line, Column: e.Col}
	return diagnostics.Span{Start: start, End: start}
}

func (e *SyntaxError) DiagnosticPath() string { return "config" }

func (e *SyntaxError) DiagnosticExpected() string {
	if e == nil {
		return ""
	}
	return e.Expected
}

func (e *SyntaxError) DiagnosticActual() string {
	if e == nil {
		return ""
	}
	return e.Actual
}

func (e *SyntaxError) DiagnosticHint() string {
	if e == nil {
		return ""
	}
	if e.Hint != "" {
		return e.Hint
	}
	return HintForCode(e.DiagnosticCode())
}

// CategoryForCode returns the diagnostics category a config code is registered
// under, falling back to the schema layer for an unregistered or empty code.
func CategoryForCode(code string) diagnostics.Category {
	if info, ok := diagnostics.LookupCode(code); ok && info.Category != "" {
		return info.Category
	}
	return diagnostics.CategorySchema
}

// HintForCode returns the repair hint registered for a config code.
func HintForCode(code string) string {
	if info, ok := diagnostics.LookupCode(code); ok {
		return info.Hint
	}
	return ""
}

// Compile-time guarantees that a syntax error interoperates with the
// diagnostics package, mirroring the guarantees asserted for Diagnostic.
var (
	_ error                                                  = (*SyntaxError)(nil)
	_ interface{ DiagnosticCode() string }                   = (*SyntaxError)(nil)
	_ interface{ DiagnosticCategory() diagnostics.Category } = (*SyntaxError)(nil)
	_ interface{ DiagnosticSpan() diagnostics.Span }         = (*SyntaxError)(nil)
	_ interface{ DiagnosticPath() string }                   = (*SyntaxError)(nil)
	_ interface{ DiagnosticExpected() string }               = (*SyntaxError)(nil)
	_ interface{ DiagnosticActual() string }                 = (*SyntaxError)(nil)
	_ interface{ DiagnosticHint() string }                   = (*SyntaxError)(nil)
	_ interface{ Unwrap() []error }                          = (*syntaxErrors)(nil)
	_ error                                                  = (*syntaxErrors)(nil)
)
