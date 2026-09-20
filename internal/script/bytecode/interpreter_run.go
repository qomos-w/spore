package bytecode

import (
	"context"
	"fmt"
	"github.com/qomos-w/spore/invoke"

	"github.com/qomos-w/spore/diagnostics"
	"github.com/qomos-w/spore/internal/script/vm"
)

// Execute runs the main chunk from start to halt.
func (interp *Interpreter) Execute(mainChunk *chunk, functions map[string]*chunk) (vm.Value, error) {
	return interp.executeWithContext(context.Background(), invoke.ExecutionBudget{}, mainChunk, functions)
}

func (interp *Interpreter) executeWithContext(ctx context.Context, budget invoke.ExecutionBudget, mainChunk *chunk, functions map[string]*chunk) (value vm.Value, err error) {
	// Module-init / top-level statements are a bytecode entry point of their
	// own (they do not run through runUntilBoundary), so they need the same
	// recovery boundary as the evaluator methods; see doc.go.
	defer func() {
		if r := recover(); r != nil {
			err = bytecodePanicError(interp, "", "vm/program", r)
			value = vm.EncodeInt(0)
		}
	}()
	interp.ctx = ctx
	interp.budget = budget
	interp.execution = &invoke.ExecutionState{}
	interp.chunk = mainChunk
	interp.functions = functions
	interp.ip = 0

	for interp.ip < len(interp.chunk.code) {
		if interp.execution != nil {
			interp.execution.Instructions++
			if err := invoke.CheckExecution(interp.ctx, interp.budget, interp.execution); err != nil {
				return vm.EncodeInt(0), err
			}
		}
		inst := interp.chunk.code[interp.ip]
		interp.ip++

		result, shouldReturn, err := interp.executeInstruction(inst)
		if err != nil {
			return vm.EncodeInt(0), err
		}
		if shouldReturn {
			return result, nil
		}
	}

	if interp.sp > 0 {
		return interp.pop(), nil
	}
	return vm.EncodeInt(0), nil
}

func (interp *Interpreter) ExecuteUntilYield(fnChunk *chunk, args []vm.Value) (vm.Value, int, []vm.Value, []vm.Value, error) {
	return interp.runUntilBoundary(fnChunk, 0, nil, nil, args, true, true)
}

func (interp *Interpreter) ExecuteUntilYieldContext(ctx context.Context, budget invoke.ExecutionBudget, fnChunk *chunk, args []vm.Value) (vm.Value, int, []vm.Value, []vm.Value, error) {
	interp.ctx = ctx
	interp.budget = budget
	interp.execution = &invoke.ExecutionState{}
	return interp.runUntilBoundary(fnChunk, 0, nil, nil, args, true, true)
}

func (interp *Interpreter) ResumeFunction(fnChunk *chunk, ip int, stack []vm.Value, locals []vm.Value) (vm.Value, error) {
	result, _, _, _, err := interp.runUntilBoundary(fnChunk, ip, stack, locals, nil, true, false)
	return result, err
}

func (interp *Interpreter) ResumeUntilFinal(fnChunk *chunk, ip int, stack []vm.Value, locals []vm.Value) (vm.Value, error) {
	result, _, _, _, err := interp.runUntilBoundary(fnChunk, ip, stack, locals, nil, false, false)
	return result, err
}

func (interp *Interpreter) runUntilBoundary(fnChunk *chunk, ip int, stack []vm.Value, locals []vm.Value, args []vm.Value, stopOnYield bool, cloneOnReturn bool) (vm.Value, int, []vm.Value, []vm.Value, error) {
	if len(interp.callFrames) >= maxCallDepth {
		return vm.EncodeInt(0), 0, nil, nil, &RuntimeError{Code: "stack_overflow", Category: diagnostics.CategoryRuntime, Callable: interp.function, Line: interp.lineForIP(), Path: "vm/call/stack", Message: fmt.Sprintf("stack overflow: maximum call depth (%d) exceeded", maxCallDepth)}
	}
	if interp.chunk != nil {
		interp.callFrames = append(interp.callFrames, callFrame{
			chunk:       interp.chunk,
			ip:          interp.ip,
			stackBase:   interp.stackBase,
			localsBase:  interp.localsBase,
			localsCount: interp.localsCount,
			function:    interp.function,
		})
	}
	interp.chunk = fnChunk
	interp.function = fnChunk.sourceName
	interp.ip = ip
	interp.stackBase = interp.sp
	interp.localsBase = len(interp.locals)
	interp.localsCount = fnChunk.LocalCount
	if len(stack) > 0 {
		for _, val := range stack {
			interp.push(val)
		}
	}
	if len(locals) > 0 {
		interp.locals = append(interp.locals, locals...)
	} else {
		for _, arg := range args {
			interp.locals = append(interp.locals, arg)
		}
		if fnChunk.LocalCount > len(args) {
			for i := 0; i < fnChunk.LocalCount-len(args); i++ {
				interp.locals = append(interp.locals, vm.EncodeInt(0))
			}
		}
	}

	// Per-run bases for the error-handler and defer stacks. Handlers/defers
	// pushed by nested runs sit above these bases and are truncated when this
	// run exits, so abandoned nested state never leaks into later runs.
	handlerBase := len(interp.handlerStack)
	deferBase := len(interp.deferStack)
	defer func() {
		if len(interp.handlerStack) > handlerBase {
			interp.handlerStack = interp.handlerStack[:handlerBase]
		}
		if len(interp.deferStack) > deferBase {
			interp.deferStack = interp.deferStack[:deferBase]
		}
	}()

	// Snapshot of this run's frame context, used to re-enter the frame when
	// unwinding into a catch block or running defers after an error surfaced
	// from a nested call (which leaves interpreter state pointing into the
	// callee).
	runStackBase := interp.stackBase
	runLocalsBase := interp.localsBase
	runLocalsLen := len(interp.locals)
	runFrameDepth := len(interp.callFrames)
	runFunction := interp.function
	restoreRunCtx := func() {
		interp.chunk = fnChunk
		interp.function = runFunction
		interp.stackBase = runStackBase
		interp.localsBase = runLocalsBase
		interp.localsCount = fnChunk.LocalCount
		if len(interp.locals) > runLocalsLen {
			interp.locals = interp.locals[:runLocalsLen]
		}
		if len(interp.callFrames) > runFrameDepth {
			interp.callFrames = interp.callFrames[:runFrameDepth]
		}
	}

	// Defer-processing state for this run.
	var deferErr error        // pending error while defers run (nil for normal returns)
	var inDefers bool         // true while executing this run's defer bodies
	var pendingReturnVal bool // true when the operand stack still holds the return value

	// startDefers redirects execution into the innermost registered defer
	// body; returns false when there is nothing to run. Call sites must set
	// interp.sp beforehand: for return exits the return value stays on the
	// operand stack (GC-rooted) while defers run.
	startDefers := func() bool {
		if len(interp.deferStack) <= deferBase {
			return false
		}
		inDefers = true
		interp.handlerStack = interp.handlerStack[:handlerBase]
		restoreRunCtx()
		interp.ip = interp.deferStack[len(interp.deferStack)-1].startIP
		return true
	}

	// dispatchInstrError routes an instruction error through the active
	// try/catch handler (fast and slow paths alike), then pending defers.
	// It returns nil when execution continues at a redirected position
	// (catch block or defer body), otherwise the error the caller must
	// propagate after the frame has been restored.
	dispatchInstrError := func(err error) error {
		if interp.enterCatch(handlerBase, fnChunk, err) {
			return nil
		}
		if !inDefers {
			deferErr = err
			interp.sp = runStackBase
			if startDefers() {
				return nil
			}
			deferErr = nil
		}
		frame := diagnostics.Frame{Callable: interp.function, Stage: string(invoke.InvocationStageUnary), Span: diagnostics.Span{Start: diagnostics.Position{Line: interp.lineForIP(), Column: 0}, End: diagnostics.Position{Line: interp.lineForIP(), Column: 0}}}
		if rtErr, ok := err.(*RuntimeError); ok {
			if len(rtErr.Stack) == 0 {
				rtErr.Stack = interp.logicalStack(frame)
			}
		}
		interp.restoreFrame()
		return err
	}

	for interp.ip < len(fnChunk.code) {
		if interp.execution != nil {
			interp.execution.Instructions++
			if err := invoke.CheckExecution(interp.ctx, interp.budget, interp.execution); err != nil {
				interp.restoreFrame()
				return vm.EncodeInt(0), 0, nil, nil, err
			}
		}
		inst := fnChunk.code[interp.ip]
		interp.ip++

		// Fast path: inline hot opcodes to avoid executeInstruction call overhead.
		// Cases are ordered by observed frequency in tight loops (e.g. Sum) so the
		// most common matches are reached with the fewest branches.
		// Matched cases use `continue` to skip the slow path; unmatched cases fall
		// through to the generic dispatch below.
		switch inst.op {
		case opLoadLocal:
			index := interp.localsBase + int(inst.operand)
			if index >= len(interp.locals) {
				break // bounds check failed; fall through to slow path
			}
			interp.push(interp.locals[index])
			continue
		case opAddInt:
			b := interp.stack[interp.sp-1]
			a := interp.stack[interp.sp-2]
			interp.stack[interp.sp-2] = vm.EncodeInt(vm.DecodeInt(a) + vm.DecodeInt(b))
			interp.sp--
			continue
		case opAddString:
			b := interp.stack[interp.sp-1]
			a := interp.stack[interp.sp-2]
			interp.stack[interp.sp-2] = interp.vm_.ConcatStrings(a, b)
			interp.sp--
			continue
		case opConcatLocalConstString:
			localIdx, constIdx := unpackLocalPair(inst.operand)
			localIndex := interp.localsBase + int(localIdx)
			if localIndex < 0 || localIndex >= len(interp.locals) {
				break
			}
			s := interp.locals[localIndex]
			lit := interp.vm_.EncodeString(fnChunk.constants[constIdx].(string))
			interp.locals[localIndex] = interp.vm_.ConcatStrings(s, lit)
			continue
		case opAdd:
			b := interp.stack[interp.sp-1]
			a := interp.stack[interp.sp-2]
			if interp.vm_.IsStringValue(a) || interp.vm_.IsStringValue(b) {
				interp.stack[interp.sp-2] = interp.vm_.ConcatStrings(a, b)
			} else {
				interp.stack[interp.sp-2] = arithBinOp(interp.vm_, a, b,
					func(a, b float64) float64 { return a + b },
					func(a, b int64) int64 { return a + b },
					func(a, b uint64) uint64 { return a + b })
			}
			interp.sp--
			continue
		case opStoreLocalPop:
			index := interp.localsBase + int(inst.operand)
			if index < 0 || index >= len(interp.locals) {
				break // bounds check failed; fall through to slow path
			}
			interp.locals[index] = interp.stack[interp.sp-1]
			interp.sp--
			continue
		case opJump:
			interp.ip = int(inst.operand)
			continue
		case opJumpIfFalse:
			interp.sp--
			if !vm.DecodeBool(interp.stack[interp.sp]) {
				interp.ip = int(inst.operand)
			}
			continue
		case opPushInt:
			val := toInt32(fnChunk.constants[inst.operand])
			interp.push(vm.EncodeInt(val))
			continue
		case opPushString:
			val := fnChunk.constants[inst.operand].(string)
			interp.push(interp.vm_.EncodeString(val))
			continue
		case opMapGetString:
			key := interp.vm_.EncodeString(fnChunk.constants[inst.operand].(string))
			mapVal := interp.stack[interp.sp-1]
			interp.sp--
			value, ok := interp.vm_.MapGet(vm.DecodeHandle(mapVal), key)
			if !ok {
				if err := dispatchInstrError(&RuntimeError{Code: "map_key_not_found", Category: diagnostics.CategoryRuntime, Callable: interp.function, Line: interp.lineForIP(), Path: "vm/map/key", Message: fmt.Sprintf("map key not found: %q", fnChunk.constants[inst.operand].(string))}); err != nil {
					return vm.EncodeInt(0), 0, nil, nil, err
				}
				continue
			}
			interp.push(value)
			continue
		case opMapSetString:
			key := interp.vm_.EncodeString(fnChunk.constants[inst.operand].(string))
			val := interp.stack[interp.sp-1]
			mapVal := interp.stack[interp.sp-2]
			interp.sp -= 2
			interp.vm_.MapSet(vm.DecodeHandle(mapVal), key, val)
			interp.push(val)
			continue
		case opArrayGetInt:
			arr := interp.stack[interp.sp-1]
			interp.sp--
			idx := int(inst.operand)
			if idx >= interp.vm_.ArrayLength(vm.DecodeHandle(arr)) {
				if err := dispatchInstrError(&RuntimeError{Code: "array_index_out_of_range", Category: diagnostics.CategoryRuntime, Callable: interp.function, Line: interp.lineForIP(), Path: "vm/array/index", Message: fmt.Sprintf("array index out of range: index=%d length=%d", idx, interp.vm_.ArrayLength(vm.DecodeHandle(arr)))}); err != nil {
					return vm.EncodeInt(0), 0, nil, nil, err
				}
				continue
			}
			interp.push(interp.vm_.GetArrayElement(vm.DecodeHandle(arr), idx))
			continue
		case opArraySetInt:
			val := interp.stack[interp.sp-1]
			arr := interp.stack[interp.sp-2]
			interp.sp -= 2
			idx := int(inst.operand)
			arrHandle := vm.DecodeHandle(arr)
			if idx >= interp.vm_.ArrayLength(arrHandle) {
				if err := dispatchInstrError(&RuntimeError{Code: "array_index_out_of_range", Category: diagnostics.CategoryRuntime, Callable: interp.function, Line: interp.lineForIP(), Path: "vm/array/index", Message: fmt.Sprintf("array index out of range: index=%d length=%d", idx, interp.vm_.ArrayLength(arrHandle))}); err != nil {
					return vm.EncodeInt(0), 0, nil, nil, err
				}
				continue
			}
			interp.vm_.SetArrayElement(arrHandle, idx, val)
			interp.push(val)
			continue
		case opArrayPushLocalConstString:
			localIdx, constIdx := unpackLocalPair(inst.operand)
			localIndex := interp.localsBase + int(localIdx)
			if localIndex < 0 || localIndex >= len(interp.locals) {
				break
			}
			arr := interp.locals[localIndex]
			lit := interp.vm_.EncodeString(fnChunk.constants[constIdx].(string))
			interp.vm_.ArrayPush(vm.DecodeHandle(arr), lit)
			continue
		case opArrayPush:
			val := interp.pop()
			arr := interp.peek()
			interp.vm_.ArrayPush(vm.DecodeHandle(arr), val)
			continue
		case opLtInt:
			b := interp.stack[interp.sp-1]
			a := interp.stack[interp.sp-2]
			interp.stack[interp.sp-2] = vm.EncodeBool(vm.DecodeInt(a) < vm.DecodeInt(b))
			interp.sp--
			continue
		case opPop:
			interp.sp--
			continue
		case opSubInt:
			b := interp.stack[interp.sp-1]
			a := interp.stack[interp.sp-2]
			interp.stack[interp.sp-2] = vm.EncodeInt(vm.DecodeInt(a) - vm.DecodeInt(b))
			interp.sp--
			continue
		case opLeInt:
			b := interp.stack[interp.sp-1]
			a := interp.stack[interp.sp-2]
			interp.stack[interp.sp-2] = vm.EncodeBool(vm.DecodeInt(a) <= vm.DecodeInt(b))
			interp.sp--
			continue
		case opEqInt:
			b := interp.stack[interp.sp-1]
			a := interp.stack[interp.sp-2]
			interp.stack[interp.sp-2] = vm.EncodeBool(vm.DecodeInt(a) == vm.DecodeInt(b))
			interp.sp--
			continue
		case opJumpIfTrue:
			interp.sp--
			if vm.DecodeBool(interp.stack[interp.sp]) {
				interp.ip = int(inst.operand)
			}
			continue
		case opJumpLocalLtInt:
			left, right, target := unpackLocalLocalTarget(inst.operand)
			leftIndex := interp.localsBase + left
			rightIndex := interp.localsBase + right
			if leftIndex < 0 || leftIndex >= len(interp.locals) || rightIndex < 0 || rightIndex >= len(interp.locals) {
				break
			}
			if vm.DecodeInt(interp.locals[leftIndex]) < vm.DecodeInt(interp.locals[rightIndex]) {
				interp.ip = target
			}
			continue
		case opCallDirect:
			argCount := int(inst.operand >> 16)
			funcID := int(inst.operand & 0xFFFF)
			calleeChunk := fnChunk.getFunctionRef(funcID)
			if calleeChunk == nil {
				break
			}
			var args []vm.Value
			if argCount <= len(interp.callArgs) {
				args = interp.callArgs[:argCount]
			} else {
				args = make([]vm.Value, argCount)
			}
			for i := argCount - 1; i >= 0; i-- {
				args[i] = interp.pop()
			}
			result, _, _, _, err := interp.runUntilBoundary(calleeChunk, 0, nil, nil, args, false, false)
			if err != nil {
				if interp.enterCatch(handlerBase, fnChunk, err) {
					continue
				}
				if !inDefers {
					deferErr = err
					interp.sp = runStackBase
					if startDefers() {
						continue
					}
					deferErr = nil
				}
				return vm.EncodeInt(0), 0, nil, nil, err
			}
			interp.push(result)
			continue
		case opCall:
			argCount := int(inst.operand >> 16)
			constIndex := int(inst.operand & 0xFFFF)
			funcName := fnChunk.constants[constIndex].(string)
			if calleeChunk, ok := interp.functions[funcName]; ok {
				var args []vm.Value
				if argCount <= len(interp.callArgs) {
					args = interp.callArgs[:argCount]
				} else {
					args = make([]vm.Value, argCount)
				}
				for i := argCount - 1; i >= 0; i-- {
					args[i] = interp.pop()
				}
				result, _, _, _, err := interp.runUntilBoundary(calleeChunk, 0, nil, nil, args, false, false)
				if err != nil {
					if interp.enterCatch(handlerBase, fnChunk, err) {
						continue
					}
					if !inDefers {
						deferErr = err
						interp.sp = runStackBase
						if startDefers() {
							continue
						}
						deferErr = nil
					}
					return vm.EncodeInt(0), 0, nil, nil, err
				}
				interp.push(result)
				continue
			}
			// Not a known bytecode function — fall through to slow path
			// so native/closure dispatch is handled normally.
		case opStoreLocal:
			index := interp.localsBase + int(inst.operand)
			if index < 0 || index >= len(interp.locals) {
				break // bounds check failed; fall through to slow path
			}
			interp.locals[index] = interp.stack[interp.sp-1]
			continue
		case opStoreGlobalPop:
			interp.globals[inst.operand] = interp.stack[interp.sp-1]
			interp.sp--
			continue
		case opIncLocal:
			index := interp.localsBase + int(inst.operand)
			if index < 0 || index >= len(interp.locals) {
				break
			}
			interp.locals[index] = vm.EncodeInt(vm.DecodeInt(interp.locals[index]) + 1)
			continue
		case opDecLocal:
			index := interp.localsBase + int(inst.operand)
			if index < 0 || index >= len(interp.locals) {
				break
			}
			interp.locals[index] = vm.EncodeInt(vm.DecodeInt(interp.locals[index]) - 1)
			continue
		case opAddLocalInt:
			dst, src := unpackLocalPair(inst.operand)
			dstIndex := interp.localsBase + dst
			srcIndex := interp.localsBase + src
			if dstIndex < 0 || dstIndex >= len(interp.locals) || srcIndex < 0 || srcIndex >= len(interp.locals) {
				break
			}
			interp.locals[dstIndex] = vm.EncodeInt(vm.DecodeInt(interp.locals[dstIndex]) + vm.DecodeInt(interp.locals[srcIndex]))
			continue
		case opReturn:
			if stopOnYield {
				interp.sp--
				continue
			}
			if startDefers() {
				// Park the return value in the frame's base stack slot
				// (GC-rooted) while the deferred bodies run; defer bodies may
				// leave their own stack residue, so sp is not reliable until
				// opEndDefer completes the exit.
				interp.stack[interp.stackBase] = interp.stack[interp.sp-1]
				interp.sp = interp.stackBase + 1
				pendingReturnVal = true
				deferErr = nil
				continue
			}
			interp.sp--
			result := interp.stack[interp.sp]
			if !cloneOnReturn {
				interp.restoreFrame()
				return result, 0, nil, nil, nil
			}
			nextIP := interp.ip
			stackSnapshot := cloneValues(interp.stack[interp.stackBase:interp.sp])
			localsSnapshot := cloneValues(interp.locals[interp.localsBase:])
			interp.restoreFrame()
			return result, nextIP, stackSnapshot, localsSnapshot, nil
		case opReturnVoid:
			if stopOnYield {
				continue
			}
			if startDefers() {
				interp.sp = interp.stackBase
				pendingReturnVal = false
				deferErr = nil
				continue
			}
			if !cloneOnReturn {
				interp.restoreFrame()
				return vm.EncodeInt(0), 0, nil, nil, nil
			}
			nextIP := interp.ip
			stackSnapshot := cloneValues(interp.stack[interp.stackBase:interp.sp])
			localsSnapshot := cloneValues(interp.locals[interp.localsBase:])
			interp.restoreFrame()
			return vm.EncodeInt(0), nextIP, stackSnapshot, localsSnapshot, nil
		case opEndDefer:
			// Completed one deferred body. Continue with the next registered
			// defer (innermost first), or finish the pending function exit.
			if len(interp.deferStack) > deferBase {
				interp.deferStack = interp.deferStack[:len(interp.deferStack)-1]
				if len(interp.deferStack) > deferBase {
					interp.ip = interp.deferStack[len(interp.deferStack)-1].startIP
					continue
				}
			}
			inDefers = false
			err := deferErr
			deferErr = nil
			if err != nil {
				interp.restoreFrame()
				return vm.EncodeInt(0), 0, nil, nil, err
			}
			var result vm.Value
			if pendingReturnVal {
				result = interp.stack[interp.stackBase]
			} else {
				result = vm.EncodeInt(0)
			}
			interp.sp = interp.stackBase
			pendingReturnVal = false
			if !cloneOnReturn {
				interp.restoreFrame()
				return result, 0, nil, nil, nil
			}
			nextIP := interp.ip
			stackSnapshot := cloneValues(interp.stack[interp.stackBase:interp.sp])
			localsSnapshot := cloneValues(interp.locals[interp.localsBase:])
			interp.restoreFrame()
			return result, nextIP, stackSnapshot, localsSnapshot, nil
		}

		// Slow path: generic dispatch for all other opcodes.
		result, shouldReturn, err := interp.executeInstruction(inst)
		if err != nil {
			if err := dispatchInstrError(err); err != nil {
				return vm.EncodeInt(0), 0, nil, nil, err
			}
			continue
		}
		if shouldReturn {
			if stopOnYield && (inst.op == opReturn || inst.op == opReturnVoid) {
				continue
			}
			if !stopOnYield && (inst.op == opYield || inst.op == opYieldVoid) {
				continue
			}
			if inst.op == opReturn || inst.op == opReturnVoid {
				if startDefers() {
					if inst.op == opReturn {
						// Park the return value in the frame's base slot.
						interp.stack[interp.stackBase] = result
						interp.sp = interp.stackBase + 1
						pendingReturnVal = true
					} else {
						interp.sp = interp.stackBase
						pendingReturnVal = false
					}
					deferErr = nil
					continue
				}
			}
			if !cloneOnReturn {
				interp.restoreFrame()
				return result, 0, nil, nil, nil
			}
			nextIP := interp.ip
			stackSnapshot := cloneValues(interp.stack[interp.stackBase:interp.sp])
			localsSnapshot := cloneValues(interp.locals[interp.localsBase:])
			interp.restoreFrame()
			return result, nextIP, stackSnapshot, localsSnapshot, nil
		}
	}
	// NOTE: falling off the end without a return is impossible for
	// compiler-generated chunks (a trailing opReturnVoid is always emitted),
	// so registered defers are always driven by the return path above.
	interp.restoreFrame()
	return vm.EncodeInt(0), len(fnChunk.code), nil, nil, nil
}

func cloneValues(values []vm.Value) []vm.Value {
	if len(values) == 0 {
		return nil
	}
	cloned := make([]vm.Value, len(values))
	copy(cloned, values)
	return cloned
}

// ExecuteFunction runs a specific function chunk with the given arguments for the requested stage.
func (interp *Interpreter) ExecuteFunction(fnChunk *chunk, stage invoke.InvocationStage, args []vm.Value) (vm.Value, error) {
	result, _, _, _, err := interp.runUntilBoundary(fnChunk, 0, nil, nil, args, stage == invoke.InvocationStageNext, false)
	return result, err
}
