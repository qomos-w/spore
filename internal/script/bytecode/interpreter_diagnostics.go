package bytecode

import (
	"github.com/qomos-w/spore/invoke"

	"github.com/qomos-w/spore/diagnostics"
	"github.com/qomos-w/spore/internal/script/vm"
)

func (interp *Interpreter) logicalStack(current diagnostics.Frame) []diagnostics.Frame {
	stack := make([]diagnostics.Frame, 0, len(interp.callFrames)+1)
	stack = append(stack, current)
	for i := len(interp.callFrames) - 1; i >= 0; i-- {
		frame := interp.callFrames[i]
		if frame.function == "" {
			continue
		}
		stack = append(stack, diagnostics.Frame{Callable: frame.function, Stage: string(invoke.InvocationStageUnary)})
	}
	return stack
}

func (interp *Interpreter) lineForIP() int {
	if interp == nil || interp.chunk == nil {
		return 0
	}
	idx := interp.ip - 1
	if idx < 0 || idx >= len(interp.chunk.lines) {
		return 0
	}
	return interp.chunk.lines[idx]
}

// restoreFrame pops the call frame and restores execution context.
func (interp *Interpreter) restoreFrame() {
	interp.sp = interp.stackBase
	interp.locals = interp.locals[:interp.localsBase]

	if len(interp.callFrames) > 0 {
		frame := interp.callFrames[len(interp.callFrames)-1]
		interp.callFrames = interp.callFrames[:len(interp.callFrames)-1]
		interp.function = frame.function
		interp.chunk = frame.chunk
		interp.ip = frame.ip
		interp.stackBase = frame.stackBase
		interp.localsBase = frame.localsBase
		interp.localsCount = frame.localsCount
	}
}

// enterCatch dispatches a RuntimeError to the innermost active try/catch
// handler, if any was pushed by the current run (above handlerBase in the
// same chunk). On success it unwinds the frame to the state captured at
// opPushHandler time, pushes the error value (as a map with the error
// attributes) for the catch variable binding, and returns true. Errors that
// bypass dispatchInstrError entirely (budget/policy checks) are never caught
// because they never reach this function.
func (interp *Interpreter) enterCatch(handlerBase int, fnChunk *chunk, err error) bool {
	rtErr, ok := err.(*RuntimeError)
	if !ok {
		return false
	}
	if len(interp.handlerStack) <= handlerBase {
		return false
	}
	h := interp.handlerStack[len(interp.handlerStack)-1]
	if h.chunk != fnChunk {
		return false
	}
	interp.handlerStack = interp.handlerStack[:len(interp.handlerStack)-1]
	interp.chunk = h.chunk
	interp.function = h.function
	interp.stackBase = h.stackBase
	interp.localsBase = h.localsBase
	interp.localsCount = h.localsCount
	if len(interp.locals) > h.localsLen {
		interp.locals = interp.locals[:h.localsLen]
	}
	if len(interp.callFrames) > h.frameDepth {
		interp.callFrames = interp.callFrames[:h.frameDepth]
	}
	interp.sp = h.sp
	interp.push(interp.errorValue(rtErr))
	interp.ip = h.catchIP
	return true
}

// errorValue materializes a RuntimeError as a map<string, any> value that can
// be bound to a catch variable. Field access (e.code, e.message, ...) and
// subscript access (e["code"]) both work on the result.
func (interp *Interpreter) errorValue(err *RuntimeError) vm.Value {
	// Many error sites predate the Callable field; fall back to the deepest
	// known logical stack frame, then to the frame dispatching the catch.
	callable := err.Callable
	if callable == "" && len(err.Stack) > 0 {
		callable = err.Stack[0].Callable
	}
	if callable == "" {
		callable = interp.function
	}
	handle := interp.vm_.NewMap(vm.TypeString, vm.TypeInvalid, 16)
	val := vm.EncodeHandle(handle)
	// MapSet below may allocate and trigger GC; keep the handle rooted.
	release := interp.vm_.AddTemporaryRoot(val)
	defer release()
	interp.vm_.MapSet(handle, interp.vm_.EncodeString("code"), interp.vm_.EncodeString(err.Code))
	interp.vm_.MapSet(handle, interp.vm_.EncodeString("category"), interp.vm_.EncodeString(string(err.Category)))
	interp.vm_.MapSet(handle, interp.vm_.EncodeString("message"), interp.vm_.EncodeString(err.Message))
	interp.vm_.MapSet(handle, interp.vm_.EncodeString("callable"), interp.vm_.EncodeString(callable))
	interp.vm_.MapSet(handle, interp.vm_.EncodeString("line"), vm.EncodeInt(int32(err.Line)))
	interp.vm_.MapSet(handle, interp.vm_.EncodeString("column"), vm.EncodeInt(int32(err.Column)))
	interp.vm_.MapSet(handle, interp.vm_.EncodeString("path"), interp.vm_.EncodeString(err.Path))
	interp.vm_.MapSet(handle, interp.vm_.EncodeString("expected"), interp.vm_.EncodeString(err.Expected))
	interp.vm_.MapSet(handle, interp.vm_.EncodeString("actual"), interp.vm_.EncodeString(err.Actual))
	interp.vm_.MapSet(handle, interp.vm_.EncodeString("target"), interp.vm_.EncodeString(err.Target))
	return val
}
