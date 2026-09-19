package bytecode

// opcode represents a single bytecode operation.
type opcode byte

const (
	// Stack
	opPushInt opcode = iota
	opPushFloat
	opPushLong
	opPushULong
	opPushDouble
	opPushString
	opPushConstValue
	opPushTrue
	opPushFalse
	opPushNull
	opPop
	opDup

	// Locals
	opLoadLocal
	opStoreLocal

	// Globals
	opLoadGlobal
	opStoreGlobal
	opLoadNativeValue

	// Arithmetic
	opAdd
	opSub
	opMul
	opDiv
	opMod
	opNeg

	// Type-specialized arithmetic (compiler emits when both operands are
	// statically known to share the named scalar type; skips the runtime
	// tag dispatch in the generic op).
	opAddInt
	opSubInt
	opMulInt
	opDivInt
	opModInt
	opAddDouble
	opSubDouble
	opMulDouble
	opDivDouble
	opAddLong
	opSubLong
	opMulLong
	opDivLong
	opModLong
	opAddULong
	opSubULong
	opMulULong
	opDivULong
	opModULong
	opAddFloat
	opSubFloat
	opMulFloat
	opDivFloat
	opAddString
	opConcatLocalConstString

	// Comparison
	opEq
	opNe
	opLt
	opLe
	opGt
	opGe

	// Type-specialized comparison.
	opEqInt
	opNeInt
	opLtInt
	opLeInt
	opGtInt
	opGeInt
	opEqDouble
	opNeDouble
	opLtDouble
	opLeDouble
	opGtDouble
	opGeDouble
	opEqLong
	opNeLong
	opLtLong
	opLeLong
	opGtLong
	opGeLong
	opEqULong
	opNeULong
	opLtULong
	opLeULong
	opGtULong
	opGeULong
	opEqFloat
	opNeFloat
	opLtFloat
	opLeFloat
	opGtFloat
	opGeFloat

	// Logic
	opAnd
	opOr
	opNot

	// Jump
	opJump
	opJumpIfFalse
	opJumpIfTrue
	// Null-aware branches for optional chaining (`?.`) and null
	// coalescing (`??`). Both keep the tested value on the stack: the
	// jumped-to code uses it directly as the expression result.
	opJumpIfNull
	opJumpIfNotNull

	// Call
	opCall
	opCallDirect
	opCallValue
	opReturn
	opReturnVoid
	opYield
	opYieldVoid

	// Object
	opNewObject
	opGetField
	opSetField
	opCallMethod
	opCallSuperMethod
	opIs
	opAs

	// Array
	opNewArray
	opGetElement
	opSetElement
	opArrayPush
	opArrayLen
	opArrayGetInt
	opArraySetInt
	opArrayPushLocalConstString

	// Map
	opNewMap
	opMapGet
	opMapSet
	opMapDelete
	opMapLen
	opMapGetString
	opMapSetString
	opNewStructInstance

	// Debug
	opSetLine

	// Halt
	opHalt

	// Fused store-and-pop: used by the compiler for top-level assignment
	// expressions where the produced value is immediately discarded.
	opStoreLocalPop
	opStoreGlobalPop

	// Local increment/decrement: fused load-add-store for the common
	// loop-update pattern (e.g. i = i + 1).
	opIncLocal
	opDecLocal
	opAddLocalInt
	opJumpLocalLtInt

	// Iteration: runtime-dispatched length and element access for
	// for-in loops over arrays, maps, and strings.
	opIterLen
	opIterItem

	// Closures: capture cells and closure creation/invocation.
	// opMakeCell pops a value, allocates a capture cell holding it, and
	// pushes the cell reference (used for locals captured by lambdas).
	opMakeCell
	// opLoadCell pushes the current content of the capture cell held in
	// the given local slot.
	opLoadCell
	// opStoreCell pops a value into the capture cell held in the given
	// local slot, leaving the stored value on the stack.
	opStoreCell
	// opStoreCellPop pops a value into the capture cell held in the given
	// local slot (fused store+pop for expression statements).
	opStoreCellPop
	// opMakeClosure pops `operand & 0xFFFF` cell references (in capture
	// order), binds them to the lambda chunk identified by the function
	// reference `operand >> 16`, and pushes the resulting closure value.
	opMakeClosure

	// Error handlers (try/catch): opPushHandler records a catch target (its
	// operand is the absolute ip of the catch block) together with the frame
	// state at push time; opPopHandler discards the innermost record.
	opPushHandler
	opPopHandler

	// Deferred execution: opPushDefer registers the block starting at the
	// operand ip to run when the enclosing function exits (returns or
	// unwinds); opEndDefer terminates one deferred block and continues with
	// the next registered defer or the pending exit.
	opPushDefer
	opEndDefer

	// opEnumValue pushes the enum member described by the constant-table
	// entry `operand` (an enumConst: enum name + member value), re-tagged
	// with the enum's runtime ID from the VM's enum registry.
	opEnumValue
)

// instruction represents a single bytecode instruction with an optional operand.
type instruction struct {
	op      opcode
	operand int32
}

// enumConst is a constant-table entry naming an enum member by enum name and
// underlying int value. The interpreter resolves the enum's runtime ID from
// the VM's enum registry at execution time (opEnumValue).
type enumConst struct {
	enum  string
	value int32
}

// chunk is a compiled bytecode unit (one per function or the main program).
type chunk struct {
	code       []instruction
	constants  []any
	lines      []int
	LocalCount int
	sourceName string
	funcIDs    map[string]int
	funcChunks []*chunk
}

func newChunk() *chunk {
	return &chunk{
		code:      make([]instruction, 0),
		constants: make([]any, 0),
		lines:     make([]int, 0),
	}
}

func (c *chunk) addInstruction(op opcode, operand int32, line int) int {
	c.code = append(c.code, instruction{op: op, operand: operand})
	c.lines = append(c.lines, line)
	return len(c.code) - 1
}

func (c *chunk) addConstant(value any) int {
	c.constants = append(c.constants, value)
	return len(c.constants) - 1
}

func (c *chunk) patchJump(offset int, target int) {
	c.code[offset].operand = int32(target)
}

func (c *chunk) size() int {
	return len(c.code)
}

func (c *chunk) addFunctionRef(name string, fn *chunk) int {
	if c.funcIDs == nil {
		c.funcIDs = make(map[string]int)
	}
	if idx, ok := c.funcIDs[name]; ok {
		return idx
	}
	idx := len(c.funcChunks)
	c.funcIDs[name] = idx
	c.funcChunks = append(c.funcChunks, fn)
	return idx
}

func (c *chunk) getFunctionRef(index int) *chunk {
	if index < 0 || index >= len(c.funcChunks) {
		return nil
	}
	return c.funcChunks[index]
}

// opcodeNames maps opcodes to human-readable names for debugging.
var opcodeNames = map[opcode]string{
	opPushInt:                   "PUSH_INT",
	opPushFloat:                 "PUSH_FLOAT",
	opPushLong:                  "PUSH_LONG",
	opPushULong:                 "PUSH_ULONG",
	opPushDouble:                "PUSH_DOUBLE",
	opPushString:                "PUSH_STRING",
	opPushConstValue:            "PUSH_CONST_VALUE",
	opPushTrue:                  "PUSH_TRUE",
	opPushFalse:                 "PUSH_FALSE",
	opPushNull:                  "PUSH_NULL",
	opPop:                       "POP",
	opDup:                       "DUP",
	opLoadLocal:                 "LOAD_LOCAL",
	opStoreLocal:                "STORE_LOCAL",
	opLoadGlobal:                "LOAD_GLOBAL",
	opStoreGlobal:               "STORE_GLOBAL",
	opLoadNativeValue:           "LOAD_NATIVE_VALUE",
	opAdd:                       "ADD",
	opSub:                       "SUB",
	opMul:                       "MUL",
	opDiv:                       "DIV",
	opMod:                       "MOD",
	opNeg:                       "NEG",
	opAddInt:                    "ADD_INT",
	opSubInt:                    "SUB_INT",
	opMulInt:                    "MUL_INT",
	opDivInt:                    "DIV_INT",
	opModInt:                    "MOD_INT",
	opAddDouble:                 "ADD_DOUBLE",
	opSubDouble:                 "SUB_DOUBLE",
	opMulDouble:                 "MUL_DOUBLE",
	opDivDouble:                 "DIV_DOUBLE",
	opAddLong:                   "ADD_LONG",
	opSubLong:                   "SUB_LONG",
	opMulLong:                   "MUL_LONG",
	opDivLong:                   "DIV_LONG",
	opModLong:                   "MOD_LONG",
	opAddULong:                  "ADD_ULONG",
	opSubULong:                  "SUB_ULONG",
	opMulULong:                  "MUL_ULONG",
	opDivULong:                  "DIV_ULONG",
	opModULong:                  "MOD_ULONG",
	opAddFloat:                  "ADD_FLOAT",
	opSubFloat:                  "SUB_FLOAT",
	opMulFloat:                  "MUL_FLOAT",
	opDivFloat:                  "DIV_FLOAT",
	opAddString:                 "ADD_STRING",
	opConcatLocalConstString:    "CONCAT_LOCAL_CONST_STRING",
	opEq:                        "EQ",
	opNe:                        "NE",
	opLt:                        "LT",
	opLe:                        "LE",
	opGt:                        "GT",
	opGe:                        "GE",
	opEqInt:                     "EQ_INT",
	opNeInt:                     "NE_INT",
	opLtInt:                     "LT_INT",
	opLeInt:                     "LE_INT",
	opGtInt:                     "GT_INT",
	opGeInt:                     "GE_INT",
	opEqDouble:                  "EQ_DOUBLE",
	opNeDouble:                  "NE_DOUBLE",
	opLtDouble:                  "LT_DOUBLE",
	opLeDouble:                  "LE_DOUBLE",
	opGtDouble:                  "GT_DOUBLE",
	opGeDouble:                  "GE_DOUBLE",
	opEqLong:                    "EQ_LONG",
	opNeLong:                    "NE_LONG",
	opLtLong:                    "LT_LONG",
	opLeLong:                    "LE_LONG",
	opGtLong:                    "GT_LONG",
	opGeLong:                    "GE_LONG",
	opEqULong:                   "EQ_ULONG",
	opNeULong:                   "NE_ULONG",
	opLtULong:                   "LT_ULONG",
	opLeULong:                   "LE_ULONG",
	opGtULong:                   "GT_ULONG",
	opGeULong:                   "GE_ULONG",
	opEqFloat:                   "EQ_FLOAT",
	opNeFloat:                   "NE_FLOAT",
	opLtFloat:                   "LT_FLOAT",
	opLeFloat:                   "LE_FLOAT",
	opGtFloat:                   "GT_FLOAT",
	opGeFloat:                   "GE_FLOAT",
	opAnd:                       "AND",
	opOr:                        "OR",
	opNot:                       "NOT",
	opJump:                      "JUMP",
	opJumpIfFalse:               "JUMP_IF_FALSE",
	opJumpIfTrue:                "JUMP_IF_TRUE",
	opJumpIfNull:                "JUMP_IF_NULL",
	opJumpIfNotNull:             "JUMP_IF_NOT_NULL",
	opCall:                      "CALL",
	opCallDirect:                "CALL_DIRECT",
	opCallValue:                 "CALL_VALUE",
	opReturn:                    "RETURN",
	opReturnVoid:                "RETURN_VOID",
	opYield:                     "YIELD",
	opYieldVoid:                 "YIELD_VOID",
	opNewObject:                 "NEW_OBJECT",
	opGetField:                  "GET_FIELD",
	opSetField:                  "SET_FIELD",
	opCallMethod:                "CALL_METHOD",
	opCallSuperMethod:           "CALL_SUPER_METHOD",
	opIs:                        "IS",
	opAs:                        "AS",
	opNewArray:                  "NEW_ARRAY",
	opGetElement:                "GET_ELEMENT",
	opSetElement:                "SET_ELEMENT",
	opArrayPush:                 "ARRAY_PUSH",
	opArrayLen:                  "ARRAY_LEN",
	opArrayGetInt:               "ARRAY_GET_INT",
	opArraySetInt:               "ARRAY_SET_INT",
	opArrayPushLocalConstString: "ARRAY_PUSH_LOCAL_CONST_STRING",
	opNewMap:                    "NEW_MAP",
	opMapGet:                    "MAP_GET",
	opMapSet:                    "MAP_SET",
	opMapDelete:                 "MAP_DELETE",
	opMapLen:                    "MAP_LEN",
	opMapGetString:              "MAP_GET_STRING",
	opMapSetString:              "MAP_SET_STRING",
	opNewStructInstance:         "NEW_STRUCT_INSTANCE",
	opSetLine:                   "SET_LINE",
	opHalt:                      "HALT",
	opStoreLocalPop:             "STORE_LOCAL_POP",
	opStoreGlobalPop:            "STORE_GLOBAL_POP",
	opIncLocal:                  "INC_LOCAL",
	opDecLocal:                  "DEC_LOCAL",
	opAddLocalInt:               "ADD_LOCAL_INT",
	opJumpLocalLtInt:            "JUMP_LOCAL_LT_INT",
	opIterLen:                   "ITER_LEN",
	opIterItem:                  "ITER_ITEM",
	opMakeCell:                  "MAKE_CELL",
	opLoadCell:                  "LOAD_CELL",
	opStoreCell:                 "STORE_CELL",
	opStoreCellPop:              "STORE_CELL_POP",
	opMakeClosure:               "MAKE_CLOSURE",
	opPushHandler:               "PUSH_HANDLER",
	opPopHandler:                "POP_HANDLER",
	opPushDefer:                 "PUSH_DEFER",
	opEndDefer:                  "END_DEFER",
	opEnumValue:                 "ENUM_VALUE",
}
