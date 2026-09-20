package bytecode

import (
	"fmt"

	"github.com/qomos-w/spore/diagnostics"
	"github.com/qomos-w/spore/internal/script/vm"
)

// This file is the single place where the two failure tracks described in
// doc.go meet. Every top-level entry point of the package installs a deferred
// closure that calls recover() directly (Go only lets the deferred function
// itself recover) and hands the recovered value to bytecodePanicError.

// vmInternalPanicCode is the stable diagnostic code reported when a panic that
// carries no structured RuntimeError escapes the VM. Hosts branch on it to
// distinguish "the engine violated an internal invariant" from every
// script-repairable runtime error; see the registration in runtime_error.go.
const vmInternalPanicCode = "vm_internal_panic"

// bytecodePanicError turns a recovered panic value into the error the bytecode
// boundary must return, and drops the interpreter's per-run state so the
// evaluator stays usable for later calls.
//
// A *RuntimeError panic value is returned as-is: it is the documented way for
// a body invoked through a value-only vm seam (see doc.go) to report a
// structured, already-classified failure. Any other value — a plain string
// from vm.Panic, the vm package's own *runtimeError, or anything unexpected —
// is reported as vm_internal_panic, with the recovered value preserved in the
// message so the invariant violation stays diagnosable from the host side.
func bytecodePanicError(interp *Interpreter, callable, path string, recovered any) error {
	if interp != nil {
		interp.resetExecutionState()
	}
	if rtErr, ok := recovered.(*RuntimeError); ok && rtErr != nil {
		if rtErr.Callable == "" {
			rtErr.Callable = callable
		}
		if rtErr.Path == "" {
			rtErr.Path = path
		}
		return rtErr
	}
	return &RuntimeError{
		Code:     vmInternalPanicCode,
		Category: diagnostics.CategoryRuntime,
		Callable: callable,
		Path:     path,
		Message:  recoveredPanicMessage(recovered),
	}
}

// recoveredPanicMessage renders a recovered panic value without losing the
// original text: hosts and bug reports need the invariant message, not a
// generic sentence.
func recoveredPanicMessage(recovered any) string {
	switch value := recovered.(type) {
	case nil:
		return "internal VM panic"
	case error:
		return value.Error()
	case string:
		return value
	default:
		return fmt.Sprintf("%v", value)
	}
}

// resetExecutionState discards every per-run interpreter field, so a panic
// recovered at the boundary cannot leave a half-unwound frame behind on the
// interpreter that the evaluator shares across calls.
//
// Durable state is deliberately preserved: module globals, capture cells and
// the closure table belong to the compiled program (and outlive one
// invocation), while the operand stack, locals, call frames, handler/defer
// stacks and the current frame cursor all belong to the aborted run.
func (interp *Interpreter) resetExecutionState() {
	if interp == nil {
		return
	}
	interp.sp = 0
	interp.locals = interp.locals[:0]
	interp.callFrames = interp.callFrames[:0]
	interp.handlerStack = interp.handlerStack[:0]
	interp.deferStack = interp.deferStack[:0]
	interp.chunk = nil
	interp.ip = 0
	interp.function = ""
	interp.stackBase = 0
	interp.localsBase = 0
	interp.localsCount = 0
}

// discardStreamSession drops the streaming session for (callable, args). A
// panic recovered mid-stream must not leave a resumable cursor pointing into
// an aborted run, so the next CallNext/CallFinal starts a fresh session.
func (e *VMEvaluator) discardStreamSession(callable string, args []vm.Value) {
	if e == nil || e.sessions == nil {
		return
	}
	delete(e.sessions, streamSessionKey(callable, args))
}
