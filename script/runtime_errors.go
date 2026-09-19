package script

import (
	"errors"
	"fmt"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/diagnostics"
)

// This file holds the compile-time and runtime error types, their predicates,
// and the wrapping helpers shared across the package.

// CompileError marks a compile/load/link failure surfaced by Runtime. The
// embedded Diagnostic preserves structured context (Code, Category, Path,
// Span) so hosts can branch on stable identifiers and surface precise
// locations rather than parsing opaque text.
type CompileError struct {
	Err        error
	Diagnostic diagnostics.Descriptor
}

func (e *CompileError) Error() string {
	if e == nil || e.Err == nil {
		return "compile error"
	}
	return e.Err.Error()
}

// Unwrap returns the wrapped underlying error so errors.Is and errors.As can
// reach sentinels and structured types beneath the public CompileError shell.
func (e *CompileError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// RuntimeError marks an invocation/runtime failure surfaced by Runtime. The
// embedded Diagnostic preserves structured context (Code, Category, Stack,
// Cause, etc.) so hosts can branch on stable identifiers, walk the script
// stack, and surface precise repair information without parsing text.
type RuntimeError struct {
	Err        error
	Diagnostic diagnostics.Descriptor
}

func (e *RuntimeError) Error() string {
	if e == nil || e.Err == nil {
		return "runtime error"
	}
	return e.Err.Error()
}

// Unwrap returns the wrapped underlying error so errors.Is and errors.As can
// reach sentinels and structured types beneath the public RuntimeError shell.
func (e *RuntimeError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// IsCompileError reports whether err is a Runtime compile/load/link error.
func IsCompileError(err error) bool {
	_, ok := err.(*CompileError)
	return ok
}

// IsRuntimeError reports whether err is a Runtime invocation/runtime error.
func IsRuntimeError(err error) bool {
	_, ok := err.(*RuntimeError)
	return ok
}

func wrapCompileError(err error) error {
	if err == nil {
		return nil
	}
	return &CompileError{Err: err, Diagnostic: diagnostics.FromError(err, diagnostics.Descriptor{})}
}

func wrapRuntimeError(err error) error {
	if err == nil {
		return nil
	}
	return &RuntimeError{Err: err, Diagnostic: diagnostics.FromError(err, diagnostics.Descriptor{})}
}

func runtimeErrorFromOutcome(outcome binding.InvocationOutcome) *RuntimeError {
	if outcome.Result.Error == nil {
		return &RuntimeError{Err: fmt.Errorf("runtime call failed")}
	}
	diag := diagnostics.Descriptor{
		Code:     outcome.Result.Error.DiagnosticCode,
		Category: diagnostics.Category(outcome.Result.Error.Category),
		Message:  outcome.Result.Error.Message,
		Span:     outcome.Result.Error.Span,
		Path:     outcome.Result.Error.Path,
		Identity: outcome.Result.Error.Identity,
		Stack:    append([]diagnostics.Frame(nil), outcome.Result.Error.Stack...),
		Cause:    diagnostics.ClonePtr(outcome.Result.Error.Cause),
	}
	diag = diagnostics.Normalize(diag)
	msg := diag.Message
	if msg == "" {
		msg = "runtime call failed"
	}
	return &RuntimeError{Err: errors.New(msg), Diagnostic: diag}
}
