package bytecode

import (
	"context"
	"fmt"
	"math"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/diagnostics"
	"github.com/qomos-w/spore/internal/script/vm"
)

// maxCallDepth limits recursive function calls.
const maxCallDepth = 1000

type nativeValueResolver interface {
	ResolveImportedNativeValue(slot int) (vm.Value, error)
}

type nativeInvoker interface {
	InvokeVMNative(ctx context.Context, callable string, args []vm.Value) (vm.Value, bool, error)
}

// callFrame tracks one activation record on the call stack.
type callFrame struct {
	chunk       *chunk
	ip          int
	stackBase   int
	localsBase  int
	localsCount int
	function    string
}

// closureInstance is one runtime closure: a compiled lambda chunk bound to
// the capture cells it closed over at creation time.
type closureInstance struct {
	chunk    *chunk
	captures []vm.Value // capture-cell values, in the order the lambda body expects
}

// vmHandler is one active try/catch error handler on the handler stack. It
// captures the frame state at opPushHandler time so a RuntimeError raised
// anywhere inside the protected body (including nested calls) can unwind
// back to the catch block with a consistent frame.
type vmHandler struct {
	catchIP     int
	sp          int
	stackBase   int
	localsBase  int
	localsLen   int
	localsCount int
	frameDepth  int
	chunk       *chunk
	function    string
}

// deferEntry is one registered defer body on the defer stack. startIP points
// at the first instruction of the deferred block.
type deferEntry struct {
	startIP int
}

// Interpreter executes compiled bytecode chunks on a VM.
type Interpreter struct {
	vm_            *vm.VM
	ctx            context.Context
	budget         binding.ExecutionBudget
	execution      *binding.ExecutionState
	rootProviderID int
	chunk          *chunk
	functions      map[string]*chunk
	ip             int
	stack          []vm.Value
	sp             int
	locals         []vm.Value
	globals        []vm.Value
	nativeValues   []vm.Value
	nativeResolver nativeValueResolver
	callFrames     []callFrame
	stackBase      int
	localsBase     int
	localsCount    int
	function       string
	nativeInvoker  nativeInvoker
	callArgs       [8]vm.Value
	cells          []vm.Value        // capture cells (index = cell id, value = current content)
	closures       []closureInstance // closure table (index = closure id)
	handlerStack   []vmHandler       // active try/catch handlers (innermost last)
	deferStack     []deferEntry      // registered defer bodies (execution order: innermost first)
}

// newInterpreter creates an interpreter bound to the given VM.
func newInterpreter(v *vm.VM) *Interpreter {
	interp := &Interpreter{
		vm_:        v,
		stack:      make([]vm.Value, 1024),
		locals:     make([]vm.Value, 0, 256),
		globals:    make([]vm.Value, 256),
		callFrames: make([]callFrame, 0, 64),
	}
	interp.rootProviderID = v.AddRootProvider(func(visit func(vm.Value)) {
		for i := 0; i < interp.sp; i++ {
			visit(interp.stack[i])
		}
		for _, val := range interp.locals {
			visit(val)
		}
		for _, val := range interp.globals {
			visit(val)
		}
		for _, val := range interp.nativeValues {
			visit(val)
		}
		// Capture cells are interpreter-owned; their contents must stay
		// reachable even when no live local slot references them anymore.
		for _, val := range interp.cells {
			visit(val)
		}
	})
	return interp
}

// newCell allocates a capture cell holding val and returns its cell value.
func (interp *Interpreter) newCell(val vm.Value) vm.Value {
	interp.cells = append(interp.cells, val)
	return vm.EncodeCellIndex(uint32(len(interp.cells) - 1))
}

// cellValue returns the current content of the cell referenced by cell.
func (interp *Interpreter) cellValue(cell vm.Value) (vm.Value, bool) {
	if !vm.IsCell(cell) {
		return vm.EncodeInt(0), false
	}
	idx := int(vm.DecodeCellIndex(cell))
	if idx < 0 || idx >= len(interp.cells) {
		return vm.EncodeInt(0), false
	}
	return interp.cells[idx], true
}

// setCellValue stores val into the cell referenced by cell.
func (interp *Interpreter) setCellValue(cell vm.Value, val vm.Value) bool {
	if !vm.IsCell(cell) {
		return false
	}
	idx := int(vm.DecodeCellIndex(cell))
	if idx < 0 || idx >= len(interp.cells) {
		return false
	}
	interp.cells[idx] = val
	return true
}

// newClosure allocates a closure binding fnChunk to the given capture cells
// and returns the first-class closure value.
func (interp *Interpreter) newClosure(fnChunk *chunk, captures []vm.Value) vm.Value {
	interp.closures = append(interp.closures, closureInstance{chunk: fnChunk, captures: captures})
	return vm.EncodeClosureIndex(uint32(len(interp.closures) - 1))
}

func (interp *Interpreter) setNativeValues(values []vm.Value) {
	if interp == nil {
		return
	}
	interp.nativeValues = values
}

func (interp *Interpreter) setNativeValueResolver(resolver nativeValueResolver) {
	if interp == nil {
		return
	}
	interp.nativeResolver = resolver
}

// Execute runs the main chunk from start to halt.
func (interp *Interpreter) Execute(mainChunk *chunk, functions map[string]*chunk) (vm.Value, error) {
	return interp.executeWithContext(context.Background(), binding.ExecutionBudget{}, mainChunk, functions)
}

func (interp *Interpreter) executeWithContext(ctx context.Context, budget binding.ExecutionBudget, mainChunk *chunk, functions map[string]*chunk) (vm.Value, error) {
	interp.ctx = ctx
	interp.budget = budget
	interp.execution = &binding.ExecutionState{}
	interp.chunk = mainChunk
	interp.functions = functions
	interp.ip = 0

	for interp.ip < len(interp.chunk.code) {
		if interp.execution != nil {
			interp.execution.Instructions++
			if err := binding.CheckExecution(interp.ctx, interp.budget, interp.execution); err != nil {
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

func (interp *Interpreter) ExecuteUntilYieldContext(ctx context.Context, budget binding.ExecutionBudget, fnChunk *chunk, args []vm.Value) (vm.Value, int, []vm.Value, []vm.Value, error) {
	interp.ctx = ctx
	interp.budget = budget
	interp.execution = &binding.ExecutionState{}
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
		frame := diagnostics.Frame{Callable: interp.function, Stage: string(binding.InvocationStageUnary), Span: diagnostics.Span{Start: diagnostics.Position{Line: interp.lineForIP(), Column: 0}, End: diagnostics.Position{Line: interp.lineForIP(), Column: 0}}}
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
			if err := binding.CheckExecution(interp.ctx, interp.budget, interp.execution); err != nil {
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
func (interp *Interpreter) ExecuteFunction(fnChunk *chunk, stage binding.InvocationStage, args []vm.Value) (vm.Value, error) {
	result, _, _, _, err := interp.runUntilBoundary(fnChunk, 0, nil, nil, args, stage == binding.InvocationStageNext, false)
	return result, err
}

func (interp *Interpreter) logicalStack(current diagnostics.Frame) []diagnostics.Frame {
	stack := make([]diagnostics.Frame, 0, len(interp.callFrames)+1)
	stack = append(stack, current)
	for i := len(interp.callFrames) - 1; i >= 0; i-- {
		frame := interp.callFrames[i]
		if frame.function == "" {
			continue
		}
		stack = append(stack, diagnostics.Frame{Callable: frame.function, Stage: string(binding.InvocationStageUnary)})
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

func (interp *Interpreter) invokeNative(callable string, args []vm.Value) (vm.Value, bool, error) {
	if interp.execution != nil {
		interp.execution.HostCalls++
		if err := binding.CheckExecution(interp.ctx, interp.budget, interp.execution); err != nil {
			return vm.EncodeInt(0), false, err
		}
	}
	if result, ok, err := interp.invokeNativeFastPath(callable, args); ok || err != nil {
		return result, ok, err
	}
	if interp.nativeInvoker == nil {
		return vm.EncodeInt(0), false, nil
	}
	releaseArgRoots := interp.vm_.AddTemporaryRoot(args...)
	defer releaseArgRoots()
	result, ok, err := interp.nativeInvoker.InvokeVMNative(context.Background(), callable, args)
	if err != nil {
		return vm.EncodeInt(0), ok, wrapNativeInvocationError(callable, interp.function, interp.lineForIP(), err)
	}
	return result, ok, nil
}

func (interp *Interpreter) invokeNativeFastPath(callable string, args []vm.Value) (vm.Value, bool, error) {
	if callable != "strings.join" || len(args) != 2 {
		return vm.EncodeInt(0), false, nil
	}
	result, ok := interp.vm_.JoinStringArray(args[0], args[1])
	if !ok {
		return vm.EncodeInt(0), false, nil
	}
	return result, true, nil
}

func (interp *Interpreter) invokeNativeMember(receiver vm.Value, methodName string, args []vm.Value) (vm.Value, bool, error) {
	if !interp.vm_.IsStringValue(receiver) {
		return vm.EncodeInt(0), false, nil
	}
	prefix := interp.vm_.DecodeString(receiver)
	if prefix == "" {
		return vm.EncodeInt(0), false, nil
	}
	return interp.invokeNative(prefix+"."+methodName, args)
}

func wrapNativeInvocationError(callable, current string, line int, err error) error {
	if rtErr, ok := err.(*RuntimeError); ok {
		if rtErr.Callable == "" {
			rtErr.Callable = current
		}
		if rtErr.Line == 0 {
			rtErr.Line = line
		}
		return rtErr
	}
	diag := diagnostics.FromError(err, diagnostics.Descriptor{
		Category: diagnostics.CategoryHost,
		Code:     "native_call_failed",
		Path:     "vm/call/native",
		Message:  fmt.Sprintf("native callable %s failed: %v", callable, err),
	})
	return &RuntimeError{
		Code:     diag.Code,
		Category: diag.Category,
		Callable: current,
		Line:     line,
		Path:     diag.Path,
		Message:  diag.Message,
		Stack:    diag.Stack,
		Cause:    diag.Cause,
	}
}

// executeInstruction dispatches a single bytecode instruction.
// Returns (result, shouldReturn, error).
func (interp *Interpreter) executeInstruction(inst instruction) (vm.Value, bool, error) {
	chunk := interp.chunk

	switch inst.op {

	// --- Stack ---
	case opPushInt:
		val := toInt32(chunk.constants[inst.operand])
		interp.push(vm.EncodeInt(val))

	case opPushFloat:
		val := toFloat32(chunk.constants[inst.operand])
		interp.push(vm.EncodeFloat(val))

	case opPushLong:
		val := toInt64(chunk.constants[inst.operand])
		interp.push(vm.EncodeLong(val, interp.vm_))

	case opPushULong:
		val := toUInt64(chunk.constants[inst.operand])
		interp.push(vm.EncodeULong(val, interp.vm_))

	case opPushDouble:
		val := toFloat64(chunk.constants[inst.operand])
		interp.push(vm.EncodeDouble(val, interp.vm_))

	case opPushString:
		val := chunk.constants[inst.operand].(string)
		interp.push(interp.vm_.EncodeString(val))

	case opPushConstValue:
		interp.push(chunk.constants[inst.operand].(vm.Value))

	case opLoadNativeValue:
		index := int(inst.operand)
		if index < 0 {
			return vm.EncodeInt(0), false, &RuntimeError{Code: "native_value_index_out_of_bounds", Category: diagnostics.CategoryRuntime, Callable: interp.function, Line: interp.lineForIP(), Path: "vm/native-value/load", Message: fmt.Sprintf("native value index out of bounds: %d", index)}
		}
		if interp.nativeResolver != nil {
			value, err := interp.nativeResolver.ResolveImportedNativeValue(index)
			if err != nil {
				return vm.EncodeInt(0), false, err
			}
			interp.push(value)
			break
		}
		if index >= len(interp.nativeValues) {
			return vm.EncodeInt(0), false, &RuntimeError{Code: "native_value_index_out_of_bounds", Category: diagnostics.CategoryRuntime, Callable: interp.function, Line: interp.lineForIP(), Path: "vm/native-value/load", Message: fmt.Sprintf("native value index out of bounds: %d", index)}
		}
		interp.push(interp.nativeValues[index])

	case opPushTrue:
		interp.push(vm.EncodeBool(true))

	case opPushFalse:
		interp.push(vm.EncodeBool(false))

	case opPushNull:
		interp.push(vm.EncodeHandle(vm.InvalidHandle))

	case opPop:
		interp.pop()

	case opDup:
		interp.push(interp.peek())

	// --- Locals ---
	case opLoadLocal:
		index := interp.localsBase + int(inst.operand)
		if index >= len(interp.locals) {
			return vm.EncodeInt(0), false, &RuntimeError{Code: "local_index_out_of_bounds", Category: diagnostics.CategoryRuntime, Callable: interp.function, Line: interp.lineForIP(), Path: "vm/local/load", Message: fmt.Sprintf("local variable index out of bounds: %d", index)}
		}
		interp.push(interp.locals[index])

	case opStoreLocal:
		val := interp.peek()
		index := interp.localsBase + int(inst.operand)
		if index < 0 || index >= len(interp.locals) {
			return vm.EncodeInt(0), false, &RuntimeError{Code: "local_store_out_of_bounds", Category: diagnostics.CategoryRuntime, Callable: interp.function, Line: interp.lineForIP(), Path: "vm/local/store", Message: fmt.Sprintf("local variable store index out of bounds: %d (locals count: %d)", index, len(interp.locals))}
		}
		interp.locals[index] = val

	case opStoreLocalPop:
		val := interp.peek()
		index := interp.localsBase + int(inst.operand)
		if index < 0 || index >= len(interp.locals) {
			return vm.EncodeInt(0), false, &RuntimeError{Code: "local_store_out_of_bounds", Category: diagnostics.CategoryRuntime, Callable: interp.function, Line: interp.lineForIP(), Path: "vm/local/store", Message: fmt.Sprintf("local variable store index out of bounds: %d (locals count: %d)", index, len(interp.locals))}
		}
		interp.locals[index] = val
		interp.sp--

	case opIncLocal:
		index := interp.localsBase + int(inst.operand)
		if index < 0 || index >= len(interp.locals) {
			return vm.EncodeInt(0), false, &RuntimeError{Code: "local_index_out_of_bounds", Category: diagnostics.CategoryRuntime, Callable: interp.function, Line: interp.lineForIP(), Path: "vm/local/inc", Message: fmt.Sprintf("local variable index out of bounds: %d", index)}
		}
		interp.locals[index] = vm.EncodeInt(vm.DecodeInt(interp.locals[index]) + 1)

	case opDecLocal:
		index := interp.localsBase + int(inst.operand)
		if index < 0 || index >= len(interp.locals) {
			return vm.EncodeInt(0), false, &RuntimeError{Code: "local_index_out_of_bounds", Category: diagnostics.CategoryRuntime, Callable: interp.function, Line: interp.lineForIP(), Path: "vm/local/dec", Message: fmt.Sprintf("local variable index out of bounds: %d", index)}
		}
		interp.locals[index] = vm.EncodeInt(vm.DecodeInt(interp.locals[index]) - 1)

	case opAddLocalInt:
		dst, src := unpackLocalPair(inst.operand)
		dstIndex := interp.localsBase + dst
		srcIndex := interp.localsBase + src
		if dstIndex < 0 || dstIndex >= len(interp.locals) || srcIndex < 0 || srcIndex >= len(interp.locals) {
			return vm.EncodeInt(0), false, &RuntimeError{Code: "local_index_out_of_bounds", Category: diagnostics.CategoryRuntime, Callable: interp.function, Line: interp.lineForIP(), Path: "vm/local/add", Message: fmt.Sprintf("local add index out of bounds: dst=%d src=%d (locals count: %d)", dstIndex, srcIndex, len(interp.locals))}
		}
		interp.locals[dstIndex] = vm.EncodeInt(vm.DecodeInt(interp.locals[dstIndex]) + vm.DecodeInt(interp.locals[srcIndex]))

	// --- Globals ---
	case opLoadGlobal:
		index := int(inst.operand)
		if index < 0 || index >= len(interp.globals) {
			return vm.EncodeInt(0), false, &RuntimeError{Code: "global_index_out_of_bounds", Category: diagnostics.CategoryRuntime, Callable: interp.function, Line: interp.lineForIP(), Path: "vm/global/load", Message: fmt.Sprintf("global variable index out of bounds: %d (globals count: %d)", index, len(interp.globals))}
		}
		interp.push(interp.globals[index])

	case opStoreGlobal:
		interp.globals[inst.operand] = interp.peek()

	case opStoreGlobalPop:
		interp.globals[inst.operand] = interp.peek()
		interp.sp--

	// --- Arithmetic ---
	case opAdd:
		b, a := interp.pop(), interp.pop()
		if interp.vm_.IsStringValue(a) || interp.vm_.IsStringValue(b) {
			interp.push(interp.vm_.ConcatStrings(a, b))
		} else {
			interp.push(arithBinOp(interp.vm_, a, b,
				func(a, b float64) float64 { return a + b },
				func(a, b int64) int64 { return a + b },
				func(a, b uint64) uint64 { return a + b }))
		}

	case opSub:
		b, a := interp.pop(), interp.pop()
		interp.push(arithBinOp(interp.vm_, a, b,
			func(a, b float64) float64 { return a - b },
			func(a, b int64) int64 { return a - b },
			func(a, b uint64) uint64 { return a - b }))

	case opMul:
		b, a := interp.pop(), interp.pop()
		interp.push(arithBinOp(interp.vm_, a, b,
			func(a, b float64) float64 { return a * b },
			func(a, b int64) int64 { return a * b },
			func(a, b uint64) uint64 { return a * b }))

	case opDiv:
		b, a := interp.pop(), interp.pop()
		if isZero(interp.vm_, b) {
			return vm.EncodeInt(0), false, &RuntimeError{Code: "division_by_zero", Category: diagnostics.CategoryRuntime, Line: interp.lineForIP(), Path: "vm/arithmetic/div", Message: "division by zero"}
		}
		interp.push(arithBinOp(interp.vm_, a, b,
			func(a, b float64) float64 { return a / b },
			func(a, b int64) int64 { return a / b },
			func(a, b uint64) uint64 { return a / b }))

	case opMod:
		b, a := interp.pop(), interp.pop()
		if isZero(interp.vm_, b) {
			return vm.EncodeInt(0), false, &RuntimeError{Code: "modulo_by_zero", Category: diagnostics.CategoryRuntime, Line: interp.lineForIP(), Path: "vm/arithmetic/mod", Message: "modulo by zero"}
		}
		interp.push(arithBinOp(interp.vm_, a, b,
			func(a, b float64) float64 { return 0 },
			func(a, b int64) int64 { return a % b },
			func(a, b uint64) uint64 { return a % b }))

	// --- Typed arithmetic (compiler-emitted when both operands' static
	// types are known to match) ---
	case opAddInt:
		b, a := interp.pop(), interp.pop()
		interp.push(vm.EncodeInt(vm.DecodeInt(a) + vm.DecodeInt(b)))

	case opSubInt:
		b, a := interp.pop(), interp.pop()
		interp.push(vm.EncodeInt(vm.DecodeInt(a) - vm.DecodeInt(b)))

	case opMulInt:
		b, a := interp.pop(), interp.pop()
		interp.push(vm.EncodeInt(vm.DecodeInt(a) * vm.DecodeInt(b)))

	case opDivInt:
		b, a := interp.pop(), interp.pop()
		bi := vm.DecodeInt(b)
		if bi == 0 {
			return vm.EncodeInt(0), false, &RuntimeError{Code: "division_by_zero", Category: diagnostics.CategoryRuntime, Line: interp.lineForIP(), Path: "vm/arithmetic/div", Message: "division by zero"}
		}
		interp.push(vm.EncodeInt(vm.DecodeInt(a) / bi))

	case opModInt:
		b, a := interp.pop(), interp.pop()
		bi := vm.DecodeInt(b)
		if bi == 0 {
			return vm.EncodeInt(0), false, &RuntimeError{Code: "modulo_by_zero", Category: diagnostics.CategoryRuntime, Line: interp.lineForIP(), Path: "vm/arithmetic/mod", Message: "modulo by zero"}
		}
		interp.push(vm.EncodeInt(vm.DecodeInt(a) % bi))

	case opAddDouble:
		b, a := interp.pop(), interp.pop()
		interp.push(vm.EncodeDouble(interp.vm_.DecodeDouble(a)+interp.vm_.DecodeDouble(b), interp.vm_))

	case opSubDouble:
		b, a := interp.pop(), interp.pop()
		interp.push(vm.EncodeDouble(interp.vm_.DecodeDouble(a)-interp.vm_.DecodeDouble(b), interp.vm_))

	case opMulDouble:
		b, a := interp.pop(), interp.pop()
		interp.push(vm.EncodeDouble(interp.vm_.DecodeDouble(a)*interp.vm_.DecodeDouble(b), interp.vm_))

	case opDivDouble:
		b, a := interp.pop(), interp.pop()
		bf := interp.vm_.DecodeDouble(b)
		if bf == 0 {
			return vm.EncodeInt(0), false, &RuntimeError{Code: "division_by_zero", Category: diagnostics.CategoryRuntime, Line: interp.lineForIP(), Path: "vm/arithmetic/div", Message: "division by zero"}
		}
		interp.push(vm.EncodeDouble(interp.vm_.DecodeDouble(a)/bf, interp.vm_))

	case opAddLong:
		b, a := interp.pop(), interp.pop()
		interp.push(vm.EncodeLong(interp.vm_.DecodeLong(a)+interp.vm_.DecodeLong(b), interp.vm_))

	case opSubLong:
		b, a := interp.pop(), interp.pop()
		interp.push(vm.EncodeLong(interp.vm_.DecodeLong(a)-interp.vm_.DecodeLong(b), interp.vm_))

	case opMulLong:
		b, a := interp.pop(), interp.pop()
		interp.push(vm.EncodeLong(interp.vm_.DecodeLong(a)*interp.vm_.DecodeLong(b), interp.vm_))

	case opDivLong:
		b, a := interp.pop(), interp.pop()
		bi := interp.vm_.DecodeLong(b)
		if bi == 0 {
			return vm.EncodeInt(0), false, &RuntimeError{Code: "division_by_zero", Category: diagnostics.CategoryRuntime, Line: interp.lineForIP(), Path: "vm/arithmetic/div", Message: "division by zero"}
		}
		interp.push(vm.EncodeLong(interp.vm_.DecodeLong(a)/bi, interp.vm_))

	case opModLong:
		b, a := interp.pop(), interp.pop()
		bi := interp.vm_.DecodeLong(b)
		if bi == 0 {
			return vm.EncodeInt(0), false, &RuntimeError{Code: "modulo_by_zero", Category: diagnostics.CategoryRuntime, Line: interp.lineForIP(), Path: "vm/arithmetic/mod", Message: "modulo by zero"}
		}
		interp.push(vm.EncodeLong(interp.vm_.DecodeLong(a)%bi, interp.vm_))

	case opAddULong:
		b, a := interp.pop(), interp.pop()
		interp.push(vm.EncodeULong(interp.vm_.DecodeULong(a)+interp.vm_.DecodeULong(b), interp.vm_))

	case opSubULong:
		b, a := interp.pop(), interp.pop()
		interp.push(vm.EncodeULong(interp.vm_.DecodeULong(a)-interp.vm_.DecodeULong(b), interp.vm_))

	case opMulULong:
		b, a := interp.pop(), interp.pop()
		interp.push(vm.EncodeULong(interp.vm_.DecodeULong(a)*interp.vm_.DecodeULong(b), interp.vm_))

	case opDivULong:
		b, a := interp.pop(), interp.pop()
		bu := interp.vm_.DecodeULong(b)
		if bu == 0 {
			return vm.EncodeInt(0), false, &RuntimeError{Code: "division_by_zero", Category: diagnostics.CategoryRuntime, Line: interp.lineForIP(), Path: "vm/arithmetic/div", Message: "division by zero"}
		}
		interp.push(vm.EncodeULong(interp.vm_.DecodeULong(a)/bu, interp.vm_))

	case opModULong:
		b, a := interp.pop(), interp.pop()
		bu := interp.vm_.DecodeULong(b)
		if bu == 0 {
			return vm.EncodeInt(0), false, &RuntimeError{Code: "modulo_by_zero", Category: diagnostics.CategoryRuntime, Line: interp.lineForIP(), Path: "vm/arithmetic/mod", Message: "modulo by zero"}
		}
		interp.push(vm.EncodeULong(interp.vm_.DecodeULong(a)%bu, interp.vm_))

	case opAddFloat:
		b, a := interp.pop(), interp.pop()
		interp.push(vm.EncodeFloat(vm.DecodeFloat(a) + vm.DecodeFloat(b)))

	case opSubFloat:
		b, a := interp.pop(), interp.pop()
		interp.push(vm.EncodeFloat(vm.DecodeFloat(a) - vm.DecodeFloat(b)))

	case opMulFloat:
		b, a := interp.pop(), interp.pop()
		interp.push(vm.EncodeFloat(vm.DecodeFloat(a) * vm.DecodeFloat(b)))

	case opDivFloat:
		b, a := interp.pop(), interp.pop()
		bf := vm.DecodeFloat(b)
		if bf == 0 {
			return vm.EncodeInt(0), false, &RuntimeError{Code: "division_by_zero", Category: diagnostics.CategoryRuntime, Line: interp.lineForIP(), Path: "vm/arithmetic/div", Message: "division by zero"}
		}
		interp.push(vm.EncodeFloat(vm.DecodeFloat(a) / bf))

	case opAddString:
		b, a := interp.pop(), interp.pop()
		interp.push(interp.vm_.ConcatStrings(a, b))

	case opConcatLocalConstString:
		localIdx, constIdx := unpackLocalPair(inst.operand)
		localIndex := interp.localsBase + int(localIdx)
		if localIndex < 0 || localIndex >= len(interp.locals) {
			return vm.EncodeInt(0), false, &RuntimeError{Code: "local_index_out_of_bounds", Category: diagnostics.CategoryRuntime, Callable: interp.function, Line: interp.lineForIP(), Path: "vm/local/concat", Message: fmt.Sprintf("local variable index out of bounds: %d", localIndex)}
		}
		s := interp.locals[localIndex]
		lit := interp.vm_.EncodeString(chunk.constants[constIdx].(string))
		interp.locals[localIndex] = interp.vm_.ConcatStrings(s, lit)

	case opNeg:
		a := interp.pop()
		if vm.IsDouble(a) {
			interp.push(vm.EncodeDouble(-interp.vm_.DecodeDouble(a), interp.vm_))
		} else if vm.IsLong(a) {
			interp.push(vm.EncodeLong(-interp.vm_.DecodeLong(a), interp.vm_))
		} else if vm.IsULong(a) {
			interp.push(vm.EncodeLong(-int64(interp.vm_.DecodeULong(a)), interp.vm_))
		} else if vm.IsFloat(a) {
			interp.push(vm.EncodeFloat(-vm.DecodeFloat(a)))
		} else {
			interp.push(vm.EncodeInt(-vm.DecodeInt(a)))
		}

	// --- Comparison ---
	case opEq:
		b, a := interp.pop(), interp.pop()
		interp.push(vm.EncodeBool(valuesEqual(interp.vm_, a, b)))

	case opNe:
		b, a := interp.pop(), interp.pop()
		interp.push(vm.EncodeBool(!valuesEqual(interp.vm_, a, b)))

	case opLt:
		b, a := interp.pop(), interp.pop()
		interp.push(vm.EncodeBool(compareValues(interp.vm_, a, b) < 0))

	case opLe:
		b, a := interp.pop(), interp.pop()
		interp.push(vm.EncodeBool(compareValues(interp.vm_, a, b) <= 0))

	case opGt:
		b, a := interp.pop(), interp.pop()
		interp.push(vm.EncodeBool(compareValues(interp.vm_, a, b) > 0))

	case opGe:
		b, a := interp.pop(), interp.pop()
		interp.push(vm.EncodeBool(compareValues(interp.vm_, a, b) >= 0))

	// --- Typed comparison (compiler-emitted when both operands' static
	// types are known to match) ---
	case opEqInt:
		b, a := interp.pop(), interp.pop()
		interp.push(vm.EncodeBool(vm.DecodeInt(a) == vm.DecodeInt(b)))

	case opNeInt:
		b, a := interp.pop(), interp.pop()
		interp.push(vm.EncodeBool(vm.DecodeInt(a) != vm.DecodeInt(b)))

	case opLtInt:
		b, a := interp.pop(), interp.pop()
		interp.push(vm.EncodeBool(vm.DecodeInt(a) < vm.DecodeInt(b)))

	case opLeInt:
		b, a := interp.pop(), interp.pop()
		interp.push(vm.EncodeBool(vm.DecodeInt(a) <= vm.DecodeInt(b)))

	case opGtInt:
		b, a := interp.pop(), interp.pop()
		interp.push(vm.EncodeBool(vm.DecodeInt(a) > vm.DecodeInt(b)))

	case opGeInt:
		b, a := interp.pop(), interp.pop()
		interp.push(vm.EncodeBool(vm.DecodeInt(a) >= vm.DecodeInt(b)))

	case opEqDouble:
		b, a := interp.pop(), interp.pop()
		interp.push(vm.EncodeBool(interp.vm_.DecodeDouble(a) == interp.vm_.DecodeDouble(b)))

	case opNeDouble:
		b, a := interp.pop(), interp.pop()
		interp.push(vm.EncodeBool(interp.vm_.DecodeDouble(a) != interp.vm_.DecodeDouble(b)))

	case opLtDouble:
		b, a := interp.pop(), interp.pop()
		interp.push(vm.EncodeBool(interp.vm_.DecodeDouble(a) < interp.vm_.DecodeDouble(b)))

	case opLeDouble:
		b, a := interp.pop(), interp.pop()
		interp.push(vm.EncodeBool(interp.vm_.DecodeDouble(a) <= interp.vm_.DecodeDouble(b)))

	case opGtDouble:
		b, a := interp.pop(), interp.pop()
		interp.push(vm.EncodeBool(interp.vm_.DecodeDouble(a) > interp.vm_.DecodeDouble(b)))

	case opGeDouble:
		b, a := interp.pop(), interp.pop()
		interp.push(vm.EncodeBool(interp.vm_.DecodeDouble(a) >= interp.vm_.DecodeDouble(b)))

	case opEqLong:
		b, a := interp.pop(), interp.pop()
		interp.push(vm.EncodeBool(interp.vm_.DecodeLong(a) == interp.vm_.DecodeLong(b)))

	case opNeLong:
		b, a := interp.pop(), interp.pop()
		interp.push(vm.EncodeBool(interp.vm_.DecodeLong(a) != interp.vm_.DecodeLong(b)))

	case opLtLong:
		b, a := interp.pop(), interp.pop()
		interp.push(vm.EncodeBool(interp.vm_.DecodeLong(a) < interp.vm_.DecodeLong(b)))

	case opLeLong:
		b, a := interp.pop(), interp.pop()
		interp.push(vm.EncodeBool(interp.vm_.DecodeLong(a) <= interp.vm_.DecodeLong(b)))

	case opGtLong:
		b, a := interp.pop(), interp.pop()
		interp.push(vm.EncodeBool(interp.vm_.DecodeLong(a) > interp.vm_.DecodeLong(b)))

	case opGeLong:
		b, a := interp.pop(), interp.pop()
		interp.push(vm.EncodeBool(interp.vm_.DecodeLong(a) >= interp.vm_.DecodeLong(b)))

	case opEqULong:
		b, a := interp.pop(), interp.pop()
		interp.push(vm.EncodeBool(interp.vm_.DecodeULong(a) == interp.vm_.DecodeULong(b)))

	case opNeULong:
		b, a := interp.pop(), interp.pop()
		interp.push(vm.EncodeBool(interp.vm_.DecodeULong(a) != interp.vm_.DecodeULong(b)))

	case opLtULong:
		b, a := interp.pop(), interp.pop()
		interp.push(vm.EncodeBool(interp.vm_.DecodeULong(a) < interp.vm_.DecodeULong(b)))

	case opLeULong:
		b, a := interp.pop(), interp.pop()
		interp.push(vm.EncodeBool(interp.vm_.DecodeULong(a) <= interp.vm_.DecodeULong(b)))

	case opGtULong:
		b, a := interp.pop(), interp.pop()
		interp.push(vm.EncodeBool(interp.vm_.DecodeULong(a) > interp.vm_.DecodeULong(b)))

	case opGeULong:
		b, a := interp.pop(), interp.pop()
		interp.push(vm.EncodeBool(interp.vm_.DecodeULong(a) >= interp.vm_.DecodeULong(b)))

	case opEqFloat:
		b, a := interp.pop(), interp.pop()
		interp.push(vm.EncodeBool(vm.DecodeFloat(a) == vm.DecodeFloat(b)))

	case opNeFloat:
		b, a := interp.pop(), interp.pop()
		interp.push(vm.EncodeBool(vm.DecodeFloat(a) != vm.DecodeFloat(b)))

	case opLtFloat:
		b, a := interp.pop(), interp.pop()
		interp.push(vm.EncodeBool(vm.DecodeFloat(a) < vm.DecodeFloat(b)))

	case opLeFloat:
		b, a := interp.pop(), interp.pop()
		interp.push(vm.EncodeBool(vm.DecodeFloat(a) <= vm.DecodeFloat(b)))

	case opGtFloat:
		b, a := interp.pop(), interp.pop()
		interp.push(vm.EncodeBool(vm.DecodeFloat(a) > vm.DecodeFloat(b)))

	case opGeFloat:
		b, a := interp.pop(), interp.pop()
		interp.push(vm.EncodeBool(vm.DecodeFloat(a) >= vm.DecodeFloat(b)))

	// --- Logic ---
	case opAnd:
		b, a := interp.pop(), interp.pop()
		interp.push(vm.EncodeBool(vm.DecodeBool(a) && vm.DecodeBool(b)))

	case opOr:
		b, a := interp.pop(), interp.pop()
		interp.push(vm.EncodeBool(vm.DecodeBool(a) || vm.DecodeBool(b)))

	case opNot:
		a := interp.pop()
		interp.push(vm.EncodeBool(!vm.DecodeBool(a)))

	// --- Jump ---
	case opJump:
		interp.ip = int(inst.operand)

	case opJumpIfFalse:
		cond := interp.pop()
		if !vm.DecodeBool(cond) {
			interp.ip = int(inst.operand)
		}

	case opJumpIfTrue:
		cond := interp.pop()
		if vm.DecodeBool(cond) {
			interp.ip = int(inst.operand)
		}

	// Null-aware branches: the tested value stays on the stack so the jump
	// target can consume it as the expression result (null short-circuit
	// for `?.`, value retention for `??`).
	case opJumpIfNull:
		if vm.IsNull(interp.peek()) {
			interp.ip = int(inst.operand)
		}

	case opJumpIfNotNull:
		if !vm.IsNull(interp.peek()) {
			interp.ip = int(inst.operand)
		}

	case opJumpLocalLtInt:
		left, right, target := unpackLocalLocalTarget(inst.operand)
		leftIndex := interp.localsBase + left
		rightIndex := interp.localsBase + right
		if leftIndex < 0 || leftIndex >= len(interp.locals) || rightIndex < 0 || rightIndex >= len(interp.locals) {
			return vm.EncodeInt(0), false, &RuntimeError{Code: "local_index_out_of_bounds", Category: diagnostics.CategoryRuntime, Callable: interp.function, Line: interp.lineForIP(), Path: "vm/local/branch", Message: fmt.Sprintf("local branch index out of bounds: left=%d right=%d (locals count: %d)", leftIndex, rightIndex, len(interp.locals))}
		}
		if vm.DecodeInt(interp.locals[leftIndex]) < vm.DecodeInt(interp.locals[rightIndex]) {
			interp.ip = target
		}

	// --- Call ---
	case opCallDirect:
		argCount := int(inst.operand >> 16)
		funcID := int(inst.operand & 0xFFFF)
		calleeChunk := chunk.getFunctionRef(funcID)
		if calleeChunk == nil {
			return vm.EncodeInt(0), false, &RuntimeError{Code: "undefined_function", Category: diagnostics.CategoryRuntime, Callable: interp.function, Line: interp.lineForIP(), Path: "vm/call/function", Message: fmt.Sprintf("undefined direct function id: %d", funcID)}
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
			return vm.EncodeInt(0), false, err
		}
		interp.push(result)

	case opCall:
		argCount := int(inst.operand >> 16)
		constIndex := int(inst.operand & 0xFFFF)
		funcName := chunk.constants[constIndex].(string)

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
				return vm.EncodeInt(0), false, err
			}
			interp.push(result)
			break
		}

		args := make([]vm.Value, argCount)
		for i := argCount - 1; i >= 0; i-- {
			args[i] = interp.pop()
		}

		fn := interp.vm_.FuncReg().GetFunction(funcName)
		if fn == nil {
			result, ok, err := interp.invokeNative(funcName, args)
			if err != nil {
				return vm.EncodeInt(0), false, err
			}
			if !ok {
				return vm.EncodeInt(0), false, &RuntimeError{Code: "undefined_function", Category: diagnostics.CategoryRuntime, Callable: interp.function, Line: interp.lineForIP(), Path: "vm/call/function", Message: fmt.Sprintf("undefined function: %s", funcName)}
			}
			interp.push(result)
			break
		}
		interp.push(fn.ExecuteBody(interp.vm_, args))

	case opCallValue:
		argCount := int(inst.operand)
		calleeVal := interp.pop()

		args := make([]vm.Value, argCount)
		for i := argCount - 1; i >= 0; i-- {
			args[i] = interp.pop()
		}

		// Closure call: bind captured cells after the declared arguments.
		if vm.IsClosure(calleeVal) {
			closureIdx := int(vm.DecodeClosureIndex(calleeVal))
			if closureIdx < 0 || closureIdx >= len(interp.closures) {
				return vm.EncodeInt(0), false, &RuntimeError{Code: "undefined_closure", Category: diagnostics.CategoryRuntime, Callable: interp.function, Line: interp.lineForIP(), Path: "vm/call/closure", Message: fmt.Sprintf("invalid closure reference: %d", closureIdx)}
			}
			clo := interp.closures[closureIdx]
			fullArgs := args
			if len(clo.captures) > 0 {
				fullArgs = make([]vm.Value, 0, len(args)+len(clo.captures))
				fullArgs = append(fullArgs, args...)
				fullArgs = append(fullArgs, clo.captures...)
			}
			result, _, _, _, err := interp.runUntilBoundary(clo.chunk, 0, nil, nil, fullArgs, false, false)
			if err != nil {
				return vm.EncodeInt(0), false, err
			}
			interp.push(result)
			break
		}

		funcName := interp.vm_.DecodeString(calleeVal)
		fn := interp.vm_.FuncReg().GetFunction(funcName)
		if fn == nil {
			result, ok, err := interp.invokeNative(funcName, args)
			if err != nil {
				return vm.EncodeInt(0), false, err
			}
			if !ok {
				return vm.EncodeInt(0), false, &RuntimeError{Code: "undefined_function", Category: diagnostics.CategoryRuntime, Callable: interp.function, Line: interp.lineForIP(), Path: "vm/call/function", Message: fmt.Sprintf("undefined function: %s", funcName)}
			}
			interp.push(result)
			break
		}
		interp.push(fn.ExecuteBody(interp.vm_, args))

	// --- Closures ---

	case opMakeCell:
		val := interp.pop()
		interp.push(interp.newCell(val))

	case opLoadCell:
		index := interp.localsBase + int(inst.operand)
		if index < 0 || index >= len(interp.locals) {
			return vm.EncodeInt(0), false, &RuntimeError{Code: "local_index_out_of_bounds", Category: diagnostics.CategoryRuntime, Callable: interp.function, Line: interp.lineForIP(), Path: "vm/local/cell", Message: fmt.Sprintf("cell local index out of bounds: %d (locals count: %d)", index, len(interp.locals))}
		}
		content, ok := interp.cellValue(interp.locals[index])
		if !ok {
			return vm.EncodeInt(0), false, &RuntimeError{Code: "invalid_capture_cell", Category: diagnostics.CategoryRuntime, Callable: interp.function, Line: interp.lineForIP(), Path: "vm/local/cell", Message: fmt.Sprintf("local slot %d does not hold a capture cell", index)}
		}
		interp.push(content)

	case opStoreCell:
		index := interp.localsBase + int(inst.operand)
		if index < 0 || index >= len(interp.locals) {
			return vm.EncodeInt(0), false, &RuntimeError{Code: "local_index_out_of_bounds", Category: diagnostics.CategoryRuntime, Callable: interp.function, Line: interp.lineForIP(), Path: "vm/local/cell", Message: fmt.Sprintf("cell local index out of bounds: %d (locals count: %d)", index, len(interp.locals))}
		}
		val := interp.stack[interp.sp-1]
		if !interp.setCellValue(interp.locals[index], val) {
			return vm.EncodeInt(0), false, &RuntimeError{Code: "invalid_capture_cell", Category: diagnostics.CategoryRuntime, Callable: interp.function, Line: interp.lineForIP(), Path: "vm/local/cell", Message: fmt.Sprintf("local slot %d does not hold a capture cell", index)}
		}

	case opStoreCellPop:
		index := interp.localsBase + int(inst.operand)
		if index < 0 || index >= len(interp.locals) {
			return vm.EncodeInt(0), false, &RuntimeError{Code: "local_index_out_of_bounds", Category: diagnostics.CategoryRuntime, Callable: interp.function, Line: interp.lineForIP(), Path: "vm/local/cell", Message: fmt.Sprintf("cell local index out of bounds: %d (locals count: %d)", index, len(interp.locals))}
		}
		interp.sp--
		val := interp.stack[interp.sp]
		if !interp.setCellValue(interp.locals[index], val) {
			return vm.EncodeInt(0), false, &RuntimeError{Code: "invalid_capture_cell", Category: diagnostics.CategoryRuntime, Callable: interp.function, Line: interp.lineForIP(), Path: "vm/local/cell", Message: fmt.Sprintf("local slot %d does not hold a capture cell", index)}
		}

	case opMakeClosure:
		funcID := int(inst.operand & 0xFFFF)
		captureCount := int(inst.operand >> 16)
		calleeChunk := chunk.getFunctionRef(funcID)
		if calleeChunk == nil {
			return vm.EncodeInt(0), false, &RuntimeError{Code: "undefined_function", Category: diagnostics.CategoryRuntime, Callable: interp.function, Line: interp.lineForIP(), Path: "vm/call/closure", Message: fmt.Sprintf("undefined lambda function id: %d", funcID)}
		}
		var captures []vm.Value
		if captureCount > 0 {
			captures = make([]vm.Value, captureCount)
			for i := captureCount - 1; i >= 0; i-- {
				captures[i] = interp.pop()
			}
			for _, cell := range captures {
				if !vm.IsCell(cell) {
					return vm.EncodeInt(0), false, &RuntimeError{Code: "invalid_capture_cell", Category: diagnostics.CategoryRuntime, Callable: interp.function, Line: interp.lineForIP(), Path: "vm/call/closure", Message: "lambda capture operand is not a capture cell"}
				}
			}
		}
		interp.push(interp.newClosure(calleeChunk, captures))

	case opPushHandler:
		// Record a try/catch handler with the frame state at push time.
		interp.handlerStack = append(interp.handlerStack, vmHandler{
			catchIP:     int(inst.operand),
			sp:          interp.sp,
			stackBase:   interp.stackBase,
			localsBase:  interp.localsBase,
			localsLen:   len(interp.locals),
			localsCount: interp.localsCount,
			frameDepth:  len(interp.callFrames),
			chunk:       interp.chunk,
			function:    interp.function,
		})

	case opPopHandler:
		if len(interp.handlerStack) > 0 {
			interp.handlerStack = interp.handlerStack[:len(interp.handlerStack)-1]
		}

	case opPushDefer:
		// Register a deferred body; executed at function exit (innermost
		// registration first) by runUntilBoundary's defer processing.
		interp.deferStack = append(interp.deferStack, deferEntry{startIP: int(inst.operand)})

	case opEndDefer:
		// Normally handled by the fast path in runUntilBoundary; reaching the
		// slow path means no defer processing owns this entry — ignore it.

	case opEnumValue:
		ec, ok := chunk.constants[inst.operand].(enumConst)
		if !ok {
			return vm.EncodeInt(0), false, &RuntimeError{Code: "invalid_constant", Category: diagnostics.CategoryRuntime, Callable: interp.function, Line: interp.lineForIP(), Path: "vm/enum/const", Message: fmt.Sprintf("enum constant operand %d has wrong type", inst.operand)}
		}
		ed := interp.vm_.EnumReg().GetEnum(ec.enum)
		if ed == nil {
			return vm.EncodeInt(0), false, &RuntimeError{Code: "enum_not_registered", Category: diagnostics.CategoryRuntime, Callable: interp.function, Line: interp.lineForIP(), Path: "vm/enum/registry", Message: fmt.Sprintf("enum %s is not registered", ec.enum)}
		}
		interp.push(vm.EncodeEnumValue(ed.ID(), ec.value))

	case opCallMethod:
		argCount := int(inst.operand >> 16)
		methodIndex := int(inst.operand & 0xFFFF)
		methodName := chunk.constants[methodIndex].(string)

		args := make([]vm.Value, argCount)
		for i := argCount - 1; i >= 0; i-- {
			args[i] = interp.pop()
		}

		receiver := interp.pop()
		if interp.vm_.IsStringValue(receiver) {
			result, ok, err := interp.invokeNativeMember(receiver, methodName, args)
			if err != nil {
				return vm.EncodeInt(0), false, err
			}
			if !ok {
				return vm.EncodeInt(0), false, &RuntimeError{Code: "undefined_native_callable", Category: diagnostics.CategoryRuntime, Callable: interp.function, Line: interp.lineForIP(), Path: "vm/call/native", Message: fmt.Sprintf("undefined native callable: %s.%s", interp.vm_.DecodeString(receiver), methodName)}
			}
			interp.push(result)
			break
		}
		if result, ok, err := interp.invokeNativeMember(receiver, methodName, args); err != nil {
			return vm.EncodeInt(0), false, err
		} else if ok {
			interp.push(result)
			break
		}
		objHandle := vm.DecodeHandle(receiver)
		result := interp.vm_.CallMethod(objHandle, methodName, args)
		interp.push(result)

	case opCallSuperMethod:
		argCount := int(inst.operand >> 16)
		methodIndex := int(inst.operand & 0xFFFF)
		methodName := chunk.constants[methodIndex].(string)

		args := make([]vm.Value, argCount)
		for i := argCount - 1; i >= 0; i-- {
			args[i] = interp.pop()
		}

		receiver := interp.pop()
		objHandle := vm.DecodeHandle(receiver)
		result := interp.vm_.CallSuperMethod(objHandle, methodName, args)
		interp.push(result)

	case opReturn:
		return interp.pop(), true, nil

	case opReturnVoid:
		return vm.EncodeInt(0), true, nil

	case opYield:
		return interp.pop(), true, nil

	case opYieldVoid:
		return vm.EncodeInt(0), true, nil

	// --- Object ---
	case opNewObject:
		className := chunk.constants[inst.operand].(string)
		cls := interp.vm_.ClassReg().GetClassByName(className)
		if cls == nil {
			return vm.EncodeInt(0), false, &RuntimeError{Code: "class_not_found", Category: diagnostics.CategoryRuntime, Callable: interp.function, Line: interp.lineForIP(), Path: "vm/object/class", Message: fmt.Sprintf("class not found: %s", className)}
		}
		objHandle := interp.vm_.CreateObject(cls.ID())
		interp.push(vm.EncodeHandle(objHandle))

	case opGetField:
		fieldName := chunk.constants[inst.operand].(string)
		objVal := interp.pop()
		if vm.IsHandle(objVal) {
			objHandle := vm.DecodeHandle(objVal)
			if interp.vm_.IsMap(objHandle) {
				// Map field access (e.g. error attributes on a catch value):
				// look up the field name as a string key.
				value, ok := interp.vm_.MapGet(objHandle, interp.vm_.EncodeString(fieldName))
				if !ok {
					return vm.EncodeInt(0), false, &RuntimeError{Code: "map_key_not_found", Category: diagnostics.CategoryRuntime, Callable: interp.function, Line: interp.lineForIP(), Path: "vm/map/key", Message: fmt.Sprintf("map key not found: %q", fieldName)}
				}
				interp.push(value)
				break
			}
			fieldVal := interp.vm_.GetField(objHandle, fieldName)
			interp.push(fieldVal)
			break
		}
		fieldVal := interp.vm_.GetField(vm.DecodeHandle(objVal), fieldName)
		interp.push(fieldVal)

	case opSetField:
		fieldName := chunk.constants[inst.operand].(string)
		val := interp.pop()
		objVal := interp.pop()
		objHandle := vm.DecodeHandle(objVal)
		interp.vm_.SetField(objHandle, fieldName, val)
		interp.push(val)

	case opIs:
		typeName := chunk.constants[inst.operand].(string)
		objVal := interp.pop()
		var isType bool
		if vm.IsHandle(objVal) {
			objHandle := vm.DecodeHandle(objVal)
			switch typeName {
			case "string":
				isType = interp.vm_.IsStringValue(objVal)
			case "bytes":
				isType = interp.vm_.IsBytesValue(objVal)
			case "array":
				isType = interp.vm_.IsArray(objHandle)
			case "map":
				isType = interp.vm_.IsMap(objHandle)
			default:
				isType = interp.vm_.IsInstanceOf(objHandle, typeName)
			}
		} else {
			switch typeName {
			case "int", "int32":
				isType = vm.IsInt(objVal) || vm.IsLong(objVal) || vm.IsEnum(objVal)
			case "int64", "long":
				isType = vm.IsLong(objVal)
			case "float", "float32":
				isType = vm.IsFloat(objVal) || vm.IsDouble(objVal)
			case "double", "float64":
				isType = vm.IsDouble(objVal)
			case "string":
				isType = vm.IsString(objVal)
			case "bytes":
				isType = vm.IsBytes(objVal)
			case "bool":
				isType = vm.IsBool(objVal)
			default:
				// Enum types: the value must be tagged with the same enum ID.
				if ed := interp.vm_.EnumReg().GetEnum(typeName); ed != nil {
					isType = vm.IsEnum(objVal) && vm.DecodeEnumID(objVal) == ed.ID()
				}
			}
		}
		interp.push(vm.EncodeBool(isType))

	case opAs:
		typeName := chunk.constants[inst.operand].(string)
		val := interp.peek()
		if vm.IsHandle(val) {
			// Object path: check class/interface hierarchy or container types.
			objHandle := vm.DecodeHandle(val)
			var isTargetType bool
			switch typeName {
			case "string":
				isTargetType = interp.vm_.IsStringValue(val)
			case "bytes":
				isTargetType = interp.vm_.IsBytesValue(val)
			case "array":
				isTargetType = interp.vm_.IsArray(objHandle)
			case "map":
				isTargetType = interp.vm_.IsMap(objHandle)
			default:
				isTargetType = interp.vm_.IsInstanceOf(objHandle, typeName)
			}
			if !isTargetType {
				// Struct cast from map: if the target is a registered struct
				// and the value is a map, extract fields by name.
				if sd := interp.vm_.StructReg().GetStruct(typeName); sd != nil && interp.vm_.IsMap(objHandle) {
					handle, err := castMapToStruct(interp.vm_, objHandle, typeName)
					if err != nil {
						return vm.EncodeInt(0), false, &RuntimeError{
							Code:    "type_cast_failed",
							Path:    "vm/type/cast",
							Target:  typeName,
							Message: fmt.Sprintf("type cast failed: %v", err),
						}
					}
					interp.pop()
					interp.push(vm.EncodeHandle(handle))
				} else {
					actualType := interp.vm_.GetObjectTypeName(objHandle)
					return vm.EncodeInt(0), false, &RuntimeError{
						Code:     "type_cast_failed",
						Path:     "vm/type/cast",
						Target:   typeName,
						Message:  fmt.Sprintf("type cast failed: not an instance of %s", typeName),
						Expected: typeName,
						Actual:   actualType,
					}
				}
			}
		} else {
			// Scalar path: check value kind.
			if ed := interp.vm_.EnumReg().GetEnum(typeName); ed != nil {
				// Enum target: a value already tagged with this enum passes
				// through; a plain int is accepted only if it is a member of
				// the enum's closed set and is re-tagged.
				if vm.IsEnum(val) {
					if vm.DecodeEnumID(val) != ed.ID() {
						return vm.EncodeInt(0), false, &RuntimeError{
							Code:    "type_cast_failed",
							Path:    "vm/type/cast",
							Target:  typeName,
							Message: fmt.Sprintf("type cast failed: value is not %s", typeName),
						}
					}
				} else if vm.IsInt(val) || vm.IsLong(val) {
					iv, fits := int32FromIntegral(interp.vm_, val)
					if !fits || !ed.HasValue(iv) {
						return vm.EncodeInt(0), false, &RuntimeError{
							Code:    "type_cast_failed",
							Path:    "vm/type/cast",
							Target:  typeName,
							Message: fmt.Sprintf("type cast failed: value is not a member of enum %s", typeName),
						}
					}
					interp.pop()
					interp.push(vm.EncodeEnumValue(ed.ID(), iv))
				} else {
					return vm.EncodeInt(0), false, &RuntimeError{
						Code:    "type_cast_failed",
						Path:    "vm/type/cast",
						Target:  typeName,
						Message: fmt.Sprintf("type cast failed: value is not %s", typeName),
					}
				}
			} else if vm.IsEnum(val) && (typeName == "int" || typeName == "int32") {
				// Enum source, int target: untag to the underlying value.
				interp.pop()
				interp.push(vm.EncodeInt(vm.DecodeEnumValue(val)))
			} else {
				var ok bool
				switch typeName {
				case "int", "int32":
					ok = vm.IsInt(val) || vm.IsLong(val)
				case "int64", "long":
					ok = vm.IsLong(val)
				case "float", "float32":
					ok = vm.IsFloat(val) || vm.IsDouble(val)
				case "double", "float64":
					ok = vm.IsDouble(val)
				case "string":
					ok = vm.IsString(val)
				case "bytes":
					ok = vm.IsBytes(val)
				case "bool":
					ok = vm.IsBool(val)
				default:
					ok = false
				}
				if !ok {
					return vm.EncodeInt(0), false, &RuntimeError{
						Code:    "type_cast_failed",
						Path:    "vm/type/cast",
						Target:  typeName,
						Message: fmt.Sprintf("type cast failed: value is not %s", typeName),
					}
				}
			}
		}
		// Cast succeeds; leave value on stack.

	// --- Array ---
	case opNewArray:
		size := int(inst.operand)
		arr := interp.vm_.NewArray(vm.TypeInvalid, size)
		// Pop elements from stack (top = last element) and store in reverse order.
		for i := size - 1; i >= 0; i-- {
			elem := interp.pop()
			interp.vm_.SetArrayElement(arr, i, elem)
		}
		interp.push(vm.EncodeHandle(arr))

	case opGetElement:
		index := interp.pop()
		container := interp.pop()
		handle := vm.DecodeHandle(container)
		if interp.vm_.IsMap(handle) {
			if !interp.vm_.IsStringValue(index) {
				return vm.EncodeInt(0), false, &RuntimeError{Code: "invalid_map_key_type", Category: diagnostics.CategoryRuntime, Callable: interp.function, Line: interp.lineForIP(), Path: "vm/map/key_type", Message: fmt.Sprintf("map key must be string, got value tag %d", index)}
			}
			mapVal, ok := interp.vm_.MapGet(handle, index)
			if !ok {
				return vm.EncodeInt(0), false, &RuntimeError{Code: "map_key_not_found", Category: diagnostics.CategoryRuntime, Callable: interp.function, Line: interp.lineForIP(), Path: "vm/map/key", Message: fmt.Sprintf("map key not found: %q", interp.vm_.DecodeString(index))}
			}
			interp.push(mapVal)
		} else {
			if !vm.IsInt(index) {
				return vm.EncodeInt(0), false, &RuntimeError{Code: "invalid_array_index_type", Category: diagnostics.CategoryRuntime, Callable: interp.function, Line: interp.lineForIP(), Path: "vm/array/index_type", Message: fmt.Sprintf("array index must be int, got value tag %d", index)}
			}
			idx := vm.DecodeInt(index)
			length := interp.vm_.ArrayLength(handle)
			if idx < 0 || int(idx) >= length {
				return vm.EncodeInt(0), false, &RuntimeError{Code: "array_index_out_of_range", Category: diagnostics.CategoryRuntime, Callable: interp.function, Line: interp.lineForIP(), Path: "vm/array/index", Message: fmt.Sprintf("array index out of range: index=%d length=%d", idx, length)}
			}
			element := interp.vm_.GetArrayElement(handle, int(idx))
			interp.push(element)
		}

	case opArrayGetInt:
		idx := int(inst.operand)
		arr := interp.pop()
		arrHandle := vm.DecodeHandle(arr)
		length := interp.vm_.ArrayLength(arrHandle)
		if idx >= length {
			return vm.EncodeInt(0), false, &RuntimeError{Code: "array_index_out_of_range", Category: diagnostics.CategoryRuntime, Callable: interp.function, Line: interp.lineForIP(), Path: "vm/array/index", Message: fmt.Sprintf("array index out of range: index=%d length=%d", idx, length)}
		}
		interp.push(interp.vm_.GetArrayElement(arrHandle, idx))

	case opArraySetInt:
		idx := int(inst.operand)
		val := interp.pop()
		arr := interp.pop()
		arrHandle := vm.DecodeHandle(arr)
		length := interp.vm_.ArrayLength(arrHandle)
		if idx >= length {
			return vm.EncodeInt(0), false, &RuntimeError{Code: "array_index_out_of_range", Category: diagnostics.CategoryRuntime, Callable: interp.function, Line: interp.lineForIP(), Path: "vm/array/index", Message: fmt.Sprintf("array index out of range: index=%d length=%d", idx, length)}
		}
		interp.vm_.SetArrayElement(arrHandle, idx, val)
		interp.push(val)

	case opSetElement:
		val := interp.pop()
		index := interp.pop()
		container := interp.pop()
		handle := vm.DecodeHandle(container)
		if interp.vm_.IsMap(handle) {
			if !interp.vm_.IsStringValue(index) {
				return vm.EncodeInt(0), false, &RuntimeError{Code: "invalid_map_key_type", Category: diagnostics.CategoryRuntime, Callable: interp.function, Line: interp.lineForIP(), Path: "vm/map/key_type", Message: fmt.Sprintf("map key must be string, got value tag %d", index)}
			}
			interp.vm_.MapSet(handle, index, val)
		} else {
			if !vm.IsInt(index) {
				return vm.EncodeInt(0), false, &RuntimeError{Code: "invalid_array_index_type", Category: diagnostics.CategoryRuntime, Callable: interp.function, Line: interp.lineForIP(), Path: "vm/array/index_type", Message: fmt.Sprintf("array index must be int, got value tag %d", index)}
			}
			idx := vm.DecodeInt(index)
			length := interp.vm_.ArrayLength(handle)
			if idx < 0 || int(idx) >= length {
				return vm.EncodeInt(0), false, &RuntimeError{Code: "array_index_out_of_range", Category: diagnostics.CategoryRuntime, Callable: interp.function, Line: interp.lineForIP(), Path: "vm/array/index", Message: fmt.Sprintf("array index out of range: index=%d length=%d", idx, length)}
			}
			interp.vm_.SetArrayElement(handle, int(idx), val)
		}
		interp.push(val)

	case opArrayPushLocalConstString:
		localIdx, constIdx := unpackLocalPair(inst.operand)
		localIndex := interp.localsBase + int(localIdx)
		if localIndex < 0 || localIndex >= len(interp.locals) {
			return vm.EncodeInt(0), false, &RuntimeError{Code: "local_index_out_of_bounds", Category: diagnostics.CategoryRuntime, Callable: interp.function, Line: interp.lineForIP(), Path: "vm/local/array_push", Message: fmt.Sprintf("local variable index out of bounds: %d", localIndex)}
		}
		arr := interp.locals[localIndex]
		lit := interp.vm_.EncodeString(chunk.constants[constIdx].(string))
		interp.vm_.ArrayPush(vm.DecodeHandle(arr), lit)

	case opArrayPush:
		val := interp.pop()
		arr := interp.peek()
		arrHandle := vm.DecodeHandle(arr)
		interp.vm_.ArrayPush(arrHandle, val)

	case opArrayLen:
		arr := interp.pop()
		arrHandle := vm.DecodeHandle(arr)
		length := interp.vm_.ArrayLength(arrHandle)
		interp.push(vm.EncodeInt(int32(length)))

	case opIterLen:
		// Runtime-dispatched length for for-in loops: array, map, or string.
		coll := interp.pop()
		if interp.vm_.IsStringValue(coll) {
			length, ok := interp.vm_.StringLength(coll)
			if !ok {
				return vm.EncodeInt(0), false, &RuntimeError{Code: "invalid_iterable_type", Category: diagnostics.CategoryRuntime, Callable: interp.function, Line: interp.lineForIP(), Path: "vm/iter/len", Message: "for-in iterable must be array, map, or string"}
			}
			interp.push(vm.EncodeInt(int32(length)))
		} else {
			collHandle := vm.DecodeHandle(coll)
			if interp.vm_.IsMap(collHandle) {
				interp.push(vm.EncodeInt(int32(interp.vm_.MapSize(collHandle))))
			} else {
				interp.push(vm.EncodeInt(int32(interp.vm_.ArrayLength(collHandle))))
			}
		}

	case opIterItem:
		// Runtime-dispatched item access for for-in loops. For arrays this
		// yields the element at the index, for maps the key at the index
		// (insertion order), and for strings the byte at the index.
		idxVal := interp.pop()
		coll := interp.pop()
		if !vm.IsInt(idxVal) {
			return vm.EncodeInt(0), false, &RuntimeError{Code: "invalid_array_index_type", Category: diagnostics.CategoryRuntime, Callable: interp.function, Line: interp.lineForIP(), Path: "vm/array/index_type", Message: fmt.Sprintf("array index must be int, got value tag %d", idxVal)}
		}
		idx := int(vm.DecodeInt(idxVal))
		if interp.vm_.IsStringValue(coll) {
			ch, ok := interp.vm_.StringByteAt(coll, idx)
			if !ok {
				length, _ := interp.vm_.StringLength(coll)
				return vm.EncodeInt(0), false, &RuntimeError{Code: "array_index_out_of_range", Category: diagnostics.CategoryRuntime, Callable: interp.function, Line: interp.lineForIP(), Path: "vm/string/index", Message: fmt.Sprintf("string index out of range: index=%d length=%d", idx, length)}
			}
			interp.push(ch)
		} else {
			collHandle := vm.DecodeHandle(coll)
			if interp.vm_.IsMap(collHandle) {
				key, ok := interp.vm_.MapKeyAt(collHandle, idx)
				if !ok {
					size := interp.vm_.MapSize(collHandle)
					return vm.EncodeInt(0), false, &RuntimeError{Code: "array_index_out_of_range", Category: diagnostics.CategoryRuntime, Callable: interp.function, Line: interp.lineForIP(), Path: "vm/map/index", Message: fmt.Sprintf("map index out of range: index=%d size=%d", idx, size)}
				}
				interp.push(key)
			} else {
				length := interp.vm_.ArrayLength(collHandle)
				if idx < 0 || idx >= length {
					return vm.EncodeInt(0), false, &RuntimeError{Code: "array_index_out_of_range", Category: diagnostics.CategoryRuntime, Callable: interp.function, Line: interp.lineForIP(), Path: "vm/array/index", Message: fmt.Sprintf("array index out of range: index=%d length=%d", idx, length)}
				}
				interp.push(interp.vm_.GetArrayElement(collHandle, idx))
			}
		}

	// --- Map ---
	case opNewMap:
		mapHandle := interp.vm_.NewMap(vm.TypeInvalid, vm.TypeInvalid, 16)
		interp.push(vm.EncodeHandle(mapHandle))

	case opMapGet:
		key := interp.pop()
		mapVal := interp.pop()
		mapHandle := vm.DecodeHandle(mapVal)
		value, ok := interp.vm_.MapGet(mapHandle, key)
		if !ok {
			value = vm.EncodeInt(0)
		}
		interp.push(value)

	case opMapGetString:
		keyText := chunk.constants[inst.operand].(string)
		key := interp.vm_.EncodeString(keyText)
		mapVal := interp.pop()
		mapHandle := vm.DecodeHandle(mapVal)
		value, ok := interp.vm_.MapGet(mapHandle, key)
		if !ok {
			return vm.EncodeInt(0), false, &RuntimeError{Code: "map_key_not_found", Category: diagnostics.CategoryRuntime, Callable: interp.function, Line: interp.lineForIP(), Path: "vm/map/key", Message: fmt.Sprintf("map key not found: %q", keyText)}
		}
		interp.push(value)

	case opMapSetString:
		keyText := chunk.constants[inst.operand].(string)
		key := interp.vm_.EncodeString(keyText)
		val := interp.pop()
		mapVal := interp.pop()
		mapHandle := vm.DecodeHandle(mapVal)
		interp.vm_.MapSet(mapHandle, key, val)
		interp.push(val)

	case opMapSet:
		val := interp.pop()
		key := interp.pop()
		mapVal := interp.pop()
		mapHandle := vm.DecodeHandle(mapVal)
		interp.vm_.MapSet(mapHandle, key, val)

	case opMapDelete:
		key := interp.pop()
		mapVal := interp.pop()
		mapHandle := vm.DecodeHandle(mapVal)
		interp.vm_.MapDelete(mapHandle, key)

	case opMapLen:
		mapVal := interp.pop()
		mapHandle := vm.DecodeHandle(mapVal)
		length := interp.vm_.MapSize(mapHandle)
		interp.push(vm.EncodeInt(int32(length)))

	case opNewStructInstance:
		structName := chunk.constants[inst.operand].(string)
		structDef := interp.vm_.StructReg().GetStruct(structName)
		if structDef == nil {
			return vm.EncodeInt(0), false, &RuntimeError{Code: "struct_not_found", Category: diagnostics.CategoryRuntime, Callable: interp.function, Line: interp.lineForIP(), Path: "vm/object/struct", Message: fmt.Sprintf("struct not found: %s", structName)}
		}
		fieldCount := len(structDef.Fields())
		// Pop field values from stack (top = last field).
		vals := make([]vm.Value, fieldCount)
		for i := fieldCount - 1; i >= 0; i-- {
			vals[i] = interp.pop()
		}
		handle := interp.vm_.NewStructInstance(structName, vals)
		interp.push(vm.EncodeHandle(handle))

	// --- Debug ---
	case opSetLine:
		// No-op for now; line tracking is informational.

	// --- Halt ---
	case opHalt:
		if interp.sp > 0 {
			return interp.pop(), true, nil
		}
		return vm.EncodeInt(0), true, nil

	default:
		return vm.EncodeInt(0), false, &RuntimeError{Code: "unknown_opcode", Category: diagnostics.CategoryRuntime, Callable: interp.function, Line: interp.lineForIP(), Path: "vm/opcode", Message: fmt.Sprintf("unknown opcode: %d", inst.op)}
	}

	return vm.EncodeInt(0), false, nil
}

// castMapToStruct recursively converts a map handle to a struct instance.
// It handles nested structs by inspecting each field's TypeID.
func castMapToStruct(v_ *vm.VM, mapHandle vm.Handle, structName string) (vm.Handle, error) {
	sd := v_.StructReg().GetStruct(structName)
	if sd == nil {
		return vm.InvalidHandle, fmt.Errorf("struct %s not found", structName)
	}
	fields := sd.Fields()
	fieldValues := make([]vm.Value, len(fields))
	for i, f := range fields {
		keyVal := v_.EncodeString(f.Name())
		mapVal, ok := v_.MapGet(mapHandle, keyVal)
		if !ok {
			return vm.InvalidHandle, fmt.Errorf("map missing field %q for struct %s", f.Name(), structName)
		}
		// Recursive conversion for nested struct fields.
		if vm.IsHandle(mapVal) {
			childHandle := vm.DecodeHandle(mapVal)
			if v_.IsMap(childHandle) {
				tid := f.TypeID()
				category := (uint64(tid) >> 60) & 0xF
				if category == 3 { // struct category
					sid := uint32(tid & 0xFFFFFFFFFFFFFFF)
					childSd := v_.StructReg().GetStructByID(sid)
					if childSd != nil {
						casted, err := castMapToStruct(v_, childHandle, childSd.Name())
						if err != nil {
							return vm.InvalidHandle, err
						}
						mapVal = vm.EncodeHandle(casted)
					}
				}
			}
		}
		fieldValues[i] = mapVal
	}
	return v_.NewStructInstance(structName, fieldValues), nil
}

// --- Stack helpers ---

func (interp *Interpreter) push(val vm.Value) {
	if interp.sp >= len(interp.stack) {
		interp.stack = append(interp.stack, make([]vm.Value, 256)...)
	}
	interp.stack[interp.sp] = val
	interp.sp++
}

func (interp *Interpreter) pop() vm.Value {
	if interp.sp == 0 {
		panic("stack underflow")
	}
	interp.sp--
	return interp.stack[interp.sp]
}

func (interp *Interpreter) peek() vm.Value {
	if interp.sp == 0 {
		panic("stack underflow")
	}
	return interp.stack[interp.sp-1]
}

// --- Value helpers ---

func toFloat(v vm.Value) float32 {
	if vm.IsFloat(v) {
		return vm.DecodeFloat(v)
	}
	return float32(vm.DecodeInt(v))
}

func toInt32(x any) int32 {
	switch v := x.(type) {
	case int:
		return int32(v)
	case int32:
		return v
	case int64:
		return int32(v)
	default:
		return 0
	}
}

func toFloat32(x any) float32 {
	switch v := x.(type) {
	case float32:
		return v
	case float64:
		return float32(v)
	case int:
		return float32(v)
	case int32:
		return float32(v)
	default:
		return 0
	}
}

func toInt64(x any) int64 {
	switch v := x.(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case int32:
		return int64(v)
	default:
		return 0
	}
}

func toUInt64(x any) uint64 {
	switch v := x.(type) {
	case uint64:
		return v
	case int64:
		return uint64(v)
	case int:
		return uint64(v)
	case int32:
		return uint64(v)
	default:
		return 0
	}
}

func toFloat64(x any) float64 {
	switch v := x.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int32:
		return float64(v)
	default:
		return 0
	}
}

// arithBinOp performs a binary arithmetic operation, dispatching to the
// correct domain (double > long > ulong > float > int) based on operand types.
func arithBinOp(v *vm.VM, a, b vm.Value,
	doubleFn func(float64, float64) float64,
	longFn func(int64, int64) int64,
	ulongFn func(uint64, uint64) uint64,
) vm.Value {
	// double wins over everything
	if vm.IsDouble(a) || vm.IsDouble(b) {
		aVal := toNumericFloat64(v, a)
		bVal := toNumericFloat64(v, b)
		return vm.EncodeDouble(doubleFn(aVal, bVal), v)
	}
	// long wins over ulong/int
	if vm.IsLong(a) || vm.IsLong(b) {
		aVal := toNumericInt64(v, a)
		bVal := toNumericInt64(v, b)
		return vm.EncodeLong(longFn(aVal, bVal), v)
	}
	// ulong wins over int
	if vm.IsULong(a) || vm.IsULong(b) {
		aVal := toNumericUInt64(v, a)
		bVal := toNumericUInt64(v, b)
		return vm.EncodeULong(ulongFn(aVal, bVal), v)
	}
	// float wins over int
	if vm.IsFloat(a) || vm.IsFloat(b) {
		aVal := toFloat(a)
		bVal := toFloat(b)
		return vm.EncodeFloat(float32(doubleFn(float64(aVal), float64(bVal))))
	}
	// int + int
	return vm.EncodeInt(int32(longFn(int64(vm.DecodeInt(a)), int64(vm.DecodeInt(b)))))
}

// toNumericFloat64 converts any numeric value to float64.
func toNumericFloat64(v *vm.VM, val vm.Value) float64 {
	if vm.IsDouble(val) {
		return v.DecodeDouble(val)
	}
	if vm.IsLong(val) {
		return float64(v.DecodeLong(val))
	}
	if vm.IsULong(val) {
		return float64(v.DecodeULong(val))
	}
	if vm.IsFloat(val) {
		return float64(vm.DecodeFloat(val))
	}
	return float64(vm.DecodeInt(val))
}

// toNumericInt64 converts any numeric value to int64.
func toNumericInt64(v *vm.VM, val vm.Value) int64 {
	if vm.IsLong(val) {
		return v.DecodeLong(val)
	}
	if vm.IsULong(val) {
		return int64(v.DecodeULong(val))
	}
	if vm.IsDouble(val) {
		return int64(v.DecodeDouble(val))
	}
	if vm.IsFloat(val) {
		return int64(vm.DecodeFloat(val))
	}
	return int64(vm.DecodeInt(val))
}

// toNumericUInt64 converts any numeric value to uint64.
func toNumericUInt64(v *vm.VM, val vm.Value) uint64 {
	if vm.IsULong(val) {
		return v.DecodeULong(val)
	}
	if vm.IsLong(val) {
		return uint64(v.DecodeLong(val))
	}
	if vm.IsDouble(val) {
		return uint64(v.DecodeDouble(val))
	}
	if vm.IsFloat(val) {
		return uint64(vm.DecodeFloat(val))
	}
	return uint64(vm.DecodeInt(val))
}

// isZero checks if a numeric value is zero (for div/mod safety).
func isZero(v *vm.VM, val vm.Value) bool {
	if vm.IsDouble(val) {
		return v.DecodeDouble(val) == 0
	}
	if vm.IsLong(val) {
		return v.DecodeLong(val) == 0
	}
	if vm.IsULong(val) {
		return v.DecodeULong(val) == 0
	}
	if vm.IsFloat(val) {
		return vm.DecodeFloat(val) == 0
	}
	return vm.DecodeInt(val) == 0
}

// compareValues compares two numeric values, returning -1, 0, or 1.
func compareValues(v *vm.VM, a, b vm.Value) int {
	// double wins
	if vm.IsDouble(a) || vm.IsDouble(b) {
		aVal := toNumericFloat64(v, a)
		bVal := toNumericFloat64(v, b)
		if aVal < bVal {
			return -1
		}
		if aVal > bVal {
			return 1
		}
		return 0
	}
	// long
	if vm.IsLong(a) || vm.IsLong(b) {
		aVal := toNumericInt64(v, a)
		bVal := toNumericInt64(v, b)
		if aVal < bVal {
			return -1
		}
		if aVal > bVal {
			return 1
		}
		return 0
	}
	// ulong
	if vm.IsULong(a) || vm.IsULong(b) {
		aVal := toNumericUInt64(v, a)
		bVal := toNumericUInt64(v, b)
		if aVal < bVal {
			return -1
		}
		if aVal > bVal {
			return 1
		}
		return 0
	}
	// float
	if vm.IsFloat(a) || vm.IsFloat(b) {
		aVal := toFloat(a)
		bVal := toFloat(b)
		if aVal < bVal {
			return -1
		}
		if aVal > bVal {
			return 1
		}
		return 0
	}
	// int
	aVal := vm.DecodeInt(a)
	bVal := vm.DecodeInt(b)
	if aVal < bVal {
		return -1
	}
	if aVal > bVal {
		return 1
	}
	return 0
}

func valuesEqual(v *vm.VM, a, b vm.Value) bool {
	if vm.IsNull(a) && vm.IsNull(b) {
		return true
	}
	if v.IsStringValue(a) || v.IsStringValue(b) {
		return v.DecodeString(a) == v.DecodeString(b)
	}
	// Check wide numeric types first
	if vm.IsDouble(a) || vm.IsDouble(b) {
		return toNumericFloat64(v, a) == toNumericFloat64(v, b)
	}
	if vm.IsLong(a) || vm.IsLong(b) {
		return toNumericInt64(v, a) == toNumericInt64(v, b)
	}
	if vm.IsULong(a) || vm.IsULong(b) {
		return toNumericUInt64(v, a) == toNumericUInt64(v, b)
	}
	if vm.IsFloat(a) || vm.IsFloat(b) {
		return toFloat(a) == toFloat(b)
	}
	if vm.IsBool(a) && vm.IsBool(b) {
		return vm.DecodeBool(a) == vm.DecodeBool(b)
	}
	// Enum values: same-tag raw equality (enum ID + member value). An enum
	// value never equals a plain int — the tag must match too.
	return a == b
}

// int32FromIntegral converts an int- or long-tagged value to int32, reporting
// whether the value fits.
func int32FromIntegral(v *vm.VM, val vm.Value) (int32, bool) {
	if vm.IsInt(val) {
		return vm.DecodeInt(val), true
	}
	if vm.IsLong(val) {
		i := v.DecodeLong(val)
		if i < math.MinInt32 || i > math.MaxInt32 {
			return 0, false
		}
		return int32(i), true
	}
	return 0, false
}
