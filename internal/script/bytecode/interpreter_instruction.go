package bytecode

import (
	"fmt"

	"github.com/qomos-w/spore/diagnostics"
	"github.com/qomos-w/spore/internal/script/vm"
)

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
