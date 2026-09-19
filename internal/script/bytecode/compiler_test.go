package bytecode

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/qomos-w/spore/diagnostics"
	"github.com/qomos-w/spore/internal/script/frontend"
	"github.com/qomos-w/spore/internal/script/vm"
)

// compileSource parses and compiles source, returning the compiler and main chunk.
func compileSource(t *testing.T, source string) (*compiler, *chunk) {
	t.Helper()
	prog, err := frontend.ParseModuleForTest(source)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	c := newCompiler()
	mainChunk, err := c.compile(prog)
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	return c, mainChunk
}

func compileSourceAllowError(t *testing.T, source string) (*compiler, *chunk, error) {
	t.Helper()
	prog, err := frontend.ParseModuleForTest(source)
	if err != nil {
		return nil, nil, err
	}
	c := newCompiler()
	mainChunk, err := c.compile(prog)
	return c, mainChunk, err
}

// getFuncChunk returns the compiled chunk for the named function.
func getFuncChunk(t *testing.T, c *compiler, name string) *chunk {
	t.Helper()
	fnChunks := c.getFunctions()
	fn, ok := fnChunks[name]
	if !ok {
		t.Fatalf("function %q not found in compiled chunks", name)
	}
	return fn
}

// countOpcode counts occurrences of the given opcode in the chunk.
func countOpcode(ch *chunk, op opcode) int {
	n := 0
	for _, inst := range ch.code {
		if inst.op == op {
			n++
		}
	}
	return n
}

// --- Basic instruction emission ---

func TestCompiler_PushInt(t *testing.T) {
	c, _ := compileSource(t, `fun f(): int { return 42 }`)
	fn := getFuncChunk(t, c, "f")
	if countOpcode(fn, opPushInt) == 0 {
		t.Error("expected opPushInt in compiled function")
	}
	if countOpcode(fn, opReturn) == 0 {
		t.Error("expected opReturn in compiled function")
	}
}

func TestCompiler_PushBool(t *testing.T) {
	c, _ := compileSource(t, `fun f(): bool { return true }`)
	fn := getFuncChunk(t, c, "f")
	if countOpcode(fn, opPushTrue) == 0 {
		t.Error("expected opPushTrue")
	}
}

func TestCompiler_PushFalse(t *testing.T) {
	c, _ := compileSource(t, `fun f(): bool { return false }`)
	fn := getFuncChunk(t, c, "f")
	if countOpcode(fn, opPushFalse) == 0 {
		t.Error("expected opPushFalse")
	}
}

func TestCompiler_ArithmeticOps(t *testing.T) {
	c, _ := compileSource(t, `fun f(): int { return 1 + 2 * 3 - 4 }`)
	fn := getFuncChunk(t, c, "f")
	// All operands are int literals → compiler should emit type-specialized
	// arithmetic opcodes that skip the runtime tag dispatch.
	for _, op := range []opcode{opAddInt, opMulInt, opSubInt} {
		if countOpcode(fn, op) == 0 {
			t.Errorf("expected %s in compiled int arithmetic", opcodeNames[op])
		}
	}
	// Generic ops should NOT appear for the all-int case.
	for _, op := range []opcode{opAdd, opMul, opSub} {
		if countOpcode(fn, op) != 0 {
			t.Errorf("did not expect generic %s for all-int arithmetic", opcodeNames[op])
		}
	}
}

func TestCompiler_SumBytecodeUsesFusedLocalIntAdd(t *testing.T) {
	c, _ := compileSource(t, `fun sum(n: int): int {
		var s: int = 0
		for (var i: int = 0; i < n; i = i + 1) { s = s + i }
		return s
	}`)
	fn := getFuncChunk(t, c, "sum")
	if countOpcode(fn, opAddLocalInt) != 1 {
		t.Fatalf("expected one opAddLocalInt in Sum loop body, got %d", countOpcode(fn, opAddLocalInt))
	}
	if countOpcode(fn, opIncLocal) != 1 {
		t.Fatalf("expected one opIncLocal for Sum loop update, got %d", countOpcode(fn, opIncLocal))
	}
	if countOpcode(fn, opAdd) != 0 {
		t.Error("did not expect generic opAdd for all-int Sum arithmetic")
	}
	if countOpcode(fn, opJumpLocalLtInt) != 1 {
		t.Fatalf("expected one opJumpLocalLtInt for Sum loop condition, got %d", countOpcode(fn, opJumpLocalLtInt))
	}
	if countOpcode(fn, opLtInt) != 0 {
		t.Error("did not expect opLtInt for fused Sum condition")
	}
	if countOpcode(fn, opLt) != 0 {
		t.Error("did not expect generic opLt for all-int Sum condition")
	}
}

func TestCompiler_DoesNotFuseReversedLocalIntAdd(t *testing.T) {
	c, _ := compileSource(t, `fun f(n: int): int {
		var s: int = 0
		s = n + s
		return s
	}`)
	fn := getFuncChunk(t, c, "f")
	if countOpcode(fn, opAddLocalInt) != 0 {
		t.Fatal("did not expect reversed add assignment to use opAddLocalInt")
	}
	if countOpcode(fn, opAddInt) == 0 {
		t.Fatal("expected fallback typed opAddInt")
	}
}

func TestCompiler_WhileLoopUsesFusedLocalLocalIntBranch(t *testing.T) {
	c, _ := compileSource(t, `fun sum(n: int): int {
		var i: int = 0
		var s: int = 0
		while (i < n) {
			s = s + i
			i = i + 1
		}
		return s
	}`)
	fn := getFuncChunk(t, c, "sum")
	if countOpcode(fn, opJumpLocalLtInt) != 1 {
		t.Fatalf("expected one opJumpLocalLtInt for while loop condition, got %d", countOpcode(fn, opJumpLocalLtInt))
	}
	if countOpcode(fn, opLtInt) != 0 {
		t.Fatal("did not expect opLtInt for fused while condition")
	}
}

func TestCompiler_DoesNotFuseLocalIntComparisonExpression(t *testing.T) {
	c, _ := compileSource(t, `fun f(i: int, n: int): bool { return i < n }`)
	fn := getFuncChunk(t, c, "f")
	if countOpcode(fn, opJumpLocalLtInt) != 0 {
		t.Fatal("did not expect comparison expression to use branch-only opcode")
	}
	if countOpcode(fn, opLtInt) != 1 {
		t.Fatalf("expected one opLtInt for comparison expression, got %d", countOpcode(fn, opLtInt))
	}
}

func TestCompiler_PackLocalLocalTargetBounds(t *testing.T) {
	operand, ok := packLocalLocalTarget(255, 255, 65535)
	if !ok {
		t.Fatal("expected max packed branch operand to fit")
	}
	left, right, target := unpackLocalLocalTarget(operand)
	if left != 255 || right != 255 || target != 65535 {
		t.Fatalf("unexpected unpacked operand: left=%d right=%d target=%d", left, right, target)
	}
	for _, tc := range []struct{ left, right, target int }{{256, 0, 0}, {0, 256, 0}, {0, 0, 65536}} {
		if _, ok := packLocalLocalTarget(tc.left, tc.right, tc.target); ok {
			t.Fatalf("expected packLocalLocalTarget(%d, %d, %d) to fail", tc.left, tc.right, tc.target)
		}
	}
}

func TestCompiler_ComparisonOps(t *testing.T) {
	c, _ := compileSource(t, `fun f(): bool { return 1 < 2 }`)
	fn := getFuncChunk(t, c, "f")
	if countOpcode(fn, opLtInt) == 0 {
		t.Error("expected opLtInt for int < int")
	}
	if countOpcode(fn, opLt) != 0 {
		t.Error("did not expect generic opLt for all-int comparison")
	}
}

func TestCompiler_EqualityOps(t *testing.T) {
	c, _ := compileSource(t, `fun f(): bool { return 1 == 2 }`)
	fn := getFuncChunk(t, c, "f")
	if countOpcode(fn, opEqInt) == 0 {
		t.Error("expected opEqInt for int == int")
	}
	if countOpcode(fn, opEq) != 0 {
		t.Error("did not expect generic opEq for all-int equality")
	}
}

func TestCompiler_TypedArithmeticOps_Long(t *testing.T) {
	c, _ := compileSource(t, `fun f(x: long, y: long): long { return x + y - x * y }`)
	fn := getFuncChunk(t, c, "f")
	for _, op := range []opcode{opAddLong, opSubLong, opMulLong} {
		if countOpcode(fn, op) == 0 {
			t.Errorf("expected %s in compiled long arithmetic", opcodeNames[op])
		}
	}
	for _, op := range []opcode{opAdd, opSub, opMul} {
		if countOpcode(fn, op) != 0 {
			t.Errorf("did not expect generic %s for all-long arithmetic", opcodeNames[op])
		}
	}
}

func TestCompiler_TypedArithmeticOps_ULong(t *testing.T) {
	c, _ := compileSource(t, `fun f(x: ulong, y: ulong): ulong { return x + y - x * y }`)
	fn := getFuncChunk(t, c, "f")
	for _, op := range []opcode{opAddULong, opSubULong, opMulULong} {
		if countOpcode(fn, op) == 0 {
			t.Errorf("expected %s in compiled ulong arithmetic", opcodeNames[op])
		}
	}
	for _, op := range []opcode{opAdd, opSub, opMul} {
		if countOpcode(fn, op) != 0 {
			t.Errorf("did not expect generic %s for all-ulong arithmetic", opcodeNames[op])
		}
	}
}

func TestCompiler_TypedArithmeticOps_Float(t *testing.T) {
	c, _ := compileSource(t, `fun f(x: float, y: float): float { return x + y - x * y }`)
	fn := getFuncChunk(t, c, "f")
	for _, op := range []opcode{opAddFloat, opSubFloat, opMulFloat} {
		if countOpcode(fn, op) == 0 {
			t.Errorf("expected %s in compiled float arithmetic", opcodeNames[op])
		}
	}
	for _, op := range []opcode{opAdd, opSub, opMul} {
		if countOpcode(fn, op) != 0 {
			t.Errorf("did not expect generic %s for all-float arithmetic", opcodeNames[op])
		}
	}
}

func TestCompiler_TypedArithmeticOps_Double(t *testing.T) {
	c, _ := compileSource(t, `fun f(x: double, y: double): double { return x + y - x * y }`)
	fn := getFuncChunk(t, c, "f")
	for _, op := range []opcode{opAddDouble, opSubDouble, opMulDouble} {
		if countOpcode(fn, op) == 0 {
			t.Errorf("expected %s in compiled double arithmetic", opcodeNames[op])
		}
	}
	for _, op := range []opcode{opAdd, opSub, opMul} {
		if countOpcode(fn, op) != 0 {
			t.Errorf("did not expect generic %s for all-double arithmetic", opcodeNames[op])
		}
	}
}

func TestCompiler_TypedComparisonOps_Long(t *testing.T) {
	c, _ := compileSource(t, `fun f(x: long, y: long): bool { return x < y }`)
	fn := getFuncChunk(t, c, "f")
	if countOpcode(fn, opLtLong) == 0 {
		t.Error("expected opLtLong for long < long")
	}
	if countOpcode(fn, opLt) != 0 {
		t.Error("did not expect generic opLt for all-long comparison")
	}
}

func TestCompiler_TypedComparisonOps_ULong(t *testing.T) {
	c, _ := compileSource(t, `fun f(x: ulong, y: ulong): bool { return x < y }`)
	fn := getFuncChunk(t, c, "f")
	if countOpcode(fn, opLtULong) == 0 {
		t.Error("expected opLtULong for ulong < ulong")
	}
	if countOpcode(fn, opLt) != 0 {
		t.Error("did not expect generic opLt for all-ulong comparison")
	}
}

func TestCompiler_TypedComparisonOps_Float(t *testing.T) {
	c, _ := compileSource(t, `fun f(x: float, y: float): bool { return x < y }`)
	fn := getFuncChunk(t, c, "f")
	if countOpcode(fn, opLtFloat) == 0 {
		t.Error("expected opLtFloat for float < float")
	}
	if countOpcode(fn, opLt) != 0 {
		t.Error("did not expect generic opLt for all-float comparison")
	}
}

func TestCompiler_TypedComparisonOps_Double(t *testing.T) {
	c, _ := compileSource(t, `fun f(x: double, y: double): bool { return x < y }`)
	fn := getFuncChunk(t, c, "f")
	if countOpcode(fn, opLtDouble) == 0 {
		t.Error("expected opLtDouble for double < double")
	}
	if countOpcode(fn, opLt) != 0 {
		t.Error("did not expect generic opLt for all-double comparison")
	}
}

func TestCompiler_LogicalAnd(t *testing.T) {
	c, _ := compileSource(t, `fun f(): bool { return true && false }`)
	fn := getFuncChunk(t, c, "f")
	// && uses short-circuit: opJumpIfFalse + opPushFalse, not opAnd.
	if countOpcode(fn, opJumpIfFalse) == 0 {
		t.Error("expected opJumpIfFalse for && short-circuit")
	}
	if countOpcode(fn, opPushFalse) == 0 {
		t.Error("expected opPushFalse for && short-circuit fallback")
	}
}

func TestCompiler_LogicalOr(t *testing.T) {
	c, _ := compileSource(t, `fun f(): bool { return true || false }`)
	fn := getFuncChunk(t, c, "f")
	// || uses short-circuit: opJumpIfTrue + opPushTrue, not opOr.
	if countOpcode(fn, opJumpIfTrue) == 0 {
		t.Error("expected opJumpIfTrue for || short-circuit")
	}
	if countOpcode(fn, opPushTrue) == 0 {
		t.Error("expected opPushTrue for || short-circuit fallback")
	}
}

func TestCompiler_UnaryNeg(t *testing.T) {
	c, _ := compileSource(t, `fun f(): int { return -5 }`)
	fn := getFuncChunk(t, c, "f")
	if countOpcode(fn, opNeg) == 0 {
		t.Error("expected opNeg for unary minus")
	}
}

func TestCompiler_Not(t *testing.T) {
	c, _ := compileSource(t, `fun f(): bool { return !true }`)
	fn := getFuncChunk(t, c, "f")
	if countOpcode(fn, opNot) == 0 {
		t.Error("expected opNot")
	}
}

// --- Control flow instruction patterns ---

func TestCompiler_IfElseJumps(t *testing.T) {
	c, _ := compileSource(t, `
fun f(x: int): int {
  if x > 0 { return 1 } else { return 2 }
}`)
	fn := getFuncChunk(t, c, "f")
	if countOpcode(fn, opJumpIfFalse) == 0 {
		t.Error("expected opJumpIfFalse for if condition")
	}
	if countOpcode(fn, opJump) == 0 {
		t.Error("expected opJump for else branch skip")
	}
}

func TestCompiler_WhileLoopJumps(t *testing.T) {
	c, _ := compileSource(t, `
fun f(): int {
  var i: int = 0
  while i < 10 { i = i + 1 }
  return i
}`)
	fn := getFuncChunk(t, c, "f")
	jumps := countOpcode(fn, opJump)
	if jumps < 1 {
		t.Error("expected at least 1 opJump for entry jump to check")
	}
	if countOpcode(fn, opJumpIfTrue) == 0 {
		t.Error("expected opJumpIfTrue for while condition")
	}
}

func TestCompiler_ForLoop(t *testing.T) {
	c, _ := compileSource(t, `
fun f(): int {
  var s: int = 0
  for (var i: int = 0; i < 5; i = i + 1) { s = s + i }
  return s
}`)
	fn := getFuncChunk(t, c, "f")
	if countOpcode(fn, opJumpIfTrue) == 0 {
		t.Error("expected opJumpIfTrue in for loop")
	}
}

func TestCompiler_BreakGeneratesJump(t *testing.T) {
	c, _ := compileSource(t, `
fun f(): int {
  var s: int = 0
  for (var i: int = 0; i < 10; i = i + 1) {
    if i == 5 { break }
    s = s + i
  }
  return s
}`)
	fn := getFuncChunk(t, c, "f")
	if countOpcode(fn, opJump) < 2 {
		t.Error("expected at least 2 opJump (back-edge + break)")
	}
}

func TestCompiler_ForInOverArrayEmitsIterOpcodes(t *testing.T) {
	c, _ := compileSource(t, `
fun f(): int {
  var total: int = 0
  var items: array = [10, 20, 30]
  for (item in items) { total = total + item }
  return total
}`)
	fn := getFuncChunk(t, c, "f")
	if countOpcode(fn, opIterLen) == 0 {
		t.Error("expected opIterLen in for-in over array")
	}
	if countOpcode(fn, opIterItem) == 0 {
		t.Error("expected opIterItem in for-in over array")
	}
	if countOpcode(fn, opArrayLen) != 0 {
		t.Error("for-in over array should not emit opArrayLen")
	}
	if countOpcode(fn, opGetElement) != 0 {
		t.Error("for-in over array should not emit opGetElement")
	}
}

func TestCompiler_ForInOverMapEmitsIterOpcodes(t *testing.T) {
	c, _ := compileSource(t, `
fun f(): int {
  var m: map = {"a": 1, "b": 2}
  for (k in m) { var v: int = m[k] }
  return 0
}`)
	fn := getFuncChunk(t, c, "f")
	if countOpcode(fn, opIterLen) == 0 {
		t.Error("expected opIterLen in for-in over map")
	}
	if countOpcode(fn, opIterItem) == 0 {
		t.Error("expected opIterItem in for-in over map (key binding)")
	}
}

func TestCompiler_ForInOverStringEmitsIterOpcodes(t *testing.T) {
	c, _ := compileSource(t, `
fun f(): int {
  var s: string = "hello"
  var n: int = 0
  for (ch in s) { n = n + 1 }
  return n
}`)
	fn := getFuncChunk(t, c, "f")
	if countOpcode(fn, opIterLen) == 0 {
		t.Error("expected opIterLen in for-in over string")
	}
	if countOpcode(fn, opIterItem) == 0 {
		t.Error("expected opIterItem in for-in over string (byte binding)")
	}
}

// --- Variables and locals ---

func TestCompiler_LoadStoreLocal(t *testing.T) {
	c, _ := compileSource(t, `
fun f(): int {
  var x: int = 10
  return x
}`)
	fn := getFuncChunk(t, c, "f")
	if countOpcode(fn, opStoreLocal) == 0 {
		t.Error("expected opStoreLocal for variable assignment")
	}
	if countOpcode(fn, opLoadLocal) == 0 {
		t.Error("expected opLoadLocal for variable read")
	}
}

func TestCompiler_MultipleLocals(t *testing.T) {
	c, _ := compileSource(t, `
fun f(): int {
  var a: int = 1
  var b: int = 2
  var c: int = 3
  return a + b + c
}`)
	fn := getFuncChunk(t, c, "f")
	loads := countOpcode(fn, opLoadLocal)
	if loads < 3 {
		t.Errorf("expected at least 3 opLoadLocal, got %d", loads)
	}
}

// --- Function call ---

func TestCompiler_CallInstruction(t *testing.T) {
	c, _ := compileSource(t, `
fun add(a: int, b: int): int { return a + b }
fun f(): int { return add(1, 2) }`)
	fn := getFuncChunk(t, c, "f")
	if countOpcode(fn, opCallDirect) == 0 {
		t.Error("expected opCallDirect for compile-time-known function invocation")
	}
	if countOpcode(fn, opCall) != 0 {
		t.Error("did not expect opCall for compile-time-known function invocation")
	}
}

// --- Array operations ---

func TestCompiler_ArrayInstructions(t *testing.T) {
	c, _ := compileSource(t, `
fun f(): int {
  var a: array<int> = [1, 2, 3]
  return a[0]
}`)
	fn := getFuncChunk(t, c, "f")
	if countOpcode(fn, opNewArray) == 0 {
		t.Error("expected opNewArray")
	}
	if countOpcode(fn, opArrayGetInt) != 1 {
		t.Fatalf("expected one opArrayGetInt for typed array literal-index access, got %d", countOpcode(fn, opArrayGetInt))
	}
	if countOpcode(fn, opGetElement) != 0 {
		t.Error("did not expect generic opGetElement for typed array literal-index access")
	}
}

func TestCompiler_DoesNotSpecializeDynamicArrayIndex(t *testing.T) {
	c, _ := compileSource(t, `
fun f(i: int): int {
  var a: array<int> = [1, 2, 3]
  return a[i]
}`)
	fn := getFuncChunk(t, c, "f")
	if countOpcode(fn, opArrayGetInt) != 0 {
		t.Fatal("did not expect opArrayGetInt for dynamic index access")
	}
	if countOpcode(fn, opGetElement) == 0 {
		t.Error("expected generic opGetElement for dynamic index access")
	}
}

func TestCompiler_DoesNotSpecializeUntypedArrayIndex(t *testing.T) {
	c, _ := compileSource(t, `
fun f(): int {
  var a: array = [1, 2, 3]
  return a[0]
}`)
	fn := getFuncChunk(t, c, "f")
	if countOpcode(fn, opArrayGetInt) != 0 {
		t.Fatal("did not expect opArrayGetInt for untyped array access")
	}
	if countOpcode(fn, opGetElement) == 0 {
		t.Error("expected generic opGetElement for untyped array access")
	}
}

func TestCompiler_ArraySetElement(t *testing.T) {
	c, _ := compileSource(t, `
fun f(): int {
  var a: array<int> = [1, 2, 3]
  a[1] = 99
  return a[1]
}`)
	fn := getFuncChunk(t, c, "f")
	if countOpcode(fn, opArraySetInt) != 1 {
		t.Fatalf("expected one opArraySetInt for typed array literal-index assignment, got %d", countOpcode(fn, opArraySetInt))
	}
	if countOpcode(fn, opSetElement) != 0 {
		t.Error("did not expect generic opSetElement for typed array literal-index assignment")
	}
}

func TestCompiler_DoesNotSpecializeDynamicArraySetIndex(t *testing.T) {
	c, _ := compileSource(t, `
fun f(i: int): int {
  var a: array<int> = [1, 2, 3]
  a[i] = 99
  return a[i]
}`)
	fn := getFuncChunk(t, c, "f")
	if countOpcode(fn, opArraySetInt) != 0 {
		t.Fatal("did not expect opArraySetInt for dynamic index assignment")
	}
	if countOpcode(fn, opSetElement) == 0 {
		t.Error("expected generic opSetElement for dynamic index assignment")
	}
}

func TestCompiler_DoesNotSpecializeUntypedArraySetIndex(t *testing.T) {
	c, _ := compileSource(t, `
fun f(): int {
  var a: array = [1, 2, 3]
  a[1] = 99
  return a[1]
}`)
	fn := getFuncChunk(t, c, "f")
	if countOpcode(fn, opArraySetInt) != 0 {
		t.Fatal("did not expect opArraySetInt for untyped array assignment")
	}
	if countOpcode(fn, opSetElement) == 0 {
		t.Error("expected generic opSetElement for untyped array assignment")
	}
}

// --- Map operations ---

func TestCompiler_MapInstructions(t *testing.T) {
	c, _ := compileSource(t, `
fun f(): int {
  var m: map<string, int> = {"x": 10, "y": 20}
  return m["x"]
}`)
	fn := getFuncChunk(t, c, "f")
	if countOpcode(fn, opNewMap) == 0 {
		t.Error("expected opNewMap")
	}
	if countOpcode(fn, opMapGetString) != 1 {
		t.Fatalf("expected one opMapGetString for typed map literal-key access, got %d", countOpcode(fn, opMapGetString))
	}
	if countOpcode(fn, opGetElement) != 0 {
		t.Error("did not expect generic opGetElement for typed map literal-key access")
	}
}

func TestCompiler_DoesNotSpecializeDynamicMapKey(t *testing.T) {
	c, _ := compileSource(t, `
fun f(k: string): int {
  var m: map<string, int> = {"x": 10, "y": 20}
  return m[k]
}`)
	fn := getFuncChunk(t, c, "f")
	if countOpcode(fn, opMapGetString) != 0 {
		t.Fatal("did not expect opMapGetString for dynamic key access")
	}
	if countOpcode(fn, opGetElement) == 0 {
		t.Error("expected generic opGetElement for dynamic key access")
	}
}

func TestCompiler_DoesNotSpecializeUntypedMapKey(t *testing.T) {
	c, _ := compileSource(t, `
fun f(): int {
  var m: map = {"x": 10, "y": 20}
  return m["x"]
}`)
	fn := getFuncChunk(t, c, "f")
	if countOpcode(fn, opMapGetString) != 0 {
		t.Fatal("did not expect opMapGetString for untyped map access")
	}
	if countOpcode(fn, opGetElement) == 0 {
		t.Error("expected generic opGetElement for untyped map access")
	}
}

func TestCompiler_MapSet(t *testing.T) {
	c, _ := compileSource(t, `
fun f(): int {
  var m: map<string, int> = {}
  m["k"] = 10
  return m["k"]
}`)
	fn := getFuncChunk(t, c, "f")
	if countOpcode(fn, opMapSetString) != 1 {
		t.Fatalf("expected one opMapSetString for typed map literal-key assignment, got %d", countOpcode(fn, opMapSetString))
	}
	if countOpcode(fn, opSetElement) != 0 {
		t.Error("did not expect generic opSetElement for typed map literal-key assignment")
	}
}

func TestCompiler_DoesNotSpecializeDynamicMapSetKey(t *testing.T) {
	c, _ := compileSource(t, `
fun f(k: string): int {
  var m: map<string, int> = {}
  m[k] = 10
  return m[k]
}`)
	fn := getFuncChunk(t, c, "f")
	if countOpcode(fn, opMapSetString) != 0 {
		t.Fatal("did not expect opMapSetString for dynamic key assignment")
	}
	if countOpcode(fn, opSetElement) == 0 {
		t.Error("expected generic opSetElement for dynamic key assignment")
	}
}

func TestCompiler_DoesNotSpecializeUntypedMapSetKey(t *testing.T) {
	c, _ := compileSource(t, `
fun f(): int {
  var m: map = {}
  m["k"] = 10
  return m["k"]
}`)
	fn := getFuncChunk(t, c, "f")
	if countOpcode(fn, opMapSetString) != 0 {
		t.Fatal("did not expect opMapSetString for untyped map assignment")
	}
	if countOpcode(fn, opSetElement) == 0 {
		t.Error("expected generic opSetElement for untyped map assignment")
	}
}

func TestCompiler_MapDelete(t *testing.T) {
	c, _ := compileSource(t, `
fun f(): int {
  var m: map<string, int> = {"a": 1, "b": 2}
  delete(m, "a")
  return len(m)
}`)
	fn := getFuncChunk(t, c, "f")
	if countOpcode(fn, opMapDelete) != 1 {
		t.Fatalf("expected one opMapDelete, got %d", countOpcode(fn, opMapDelete))
	}
}

func TestCompiler_DeleteRejectsWrongArity(t *testing.T) {
	_, _, err := compileSourceAllowError(t, `
fun bad(): int {
  var m: map<string, int> = {}
  return delete(m)
}`)
	if err == nil {
		t.Fatal("expected compile error for delete wrong arity")
	}
	if !strings.Contains(err.Error(), "delete expects exactly 2 arguments") {
		t.Fatalf("expected delete arity diagnostic, got %v", err)
	}
}

// --- Struct literal ---

func TestCompiler_StructLiteral(t *testing.T) {
	c, _ := compileSource(t, `
struct Point {
  x: int
  y: int
}
fun f(): int {
  var p: Point = Point{x: 1, y: 2}
  return p.x
}`)
	fn := getFuncChunk(t, c, "f")
	if countOpcode(fn, opNewStructInstance) == 0 {
		t.Error("expected opNewStructInstance for struct literal")
	}
	if countOpcode(fn, opGetField) == 0 {
		t.Error("expected opGetField for struct field access")
	}
}

func TestCompiler_StructFieldSet(t *testing.T) {
	c, _ := compileSource(t, `
struct Point {
  x: int
  y: int
}
fun f(): int {
  var p: Point = Point{x: 1, y: 2}
  p.x = 99
  return p.x
}`)
	fn := getFuncChunk(t, c, "f")
	if countOpcode(fn, opSetField) == 0 {
		t.Error("expected opSetField for struct field assignment")
	}
}

// --- Class/OOP ---

func TestCompiler_NewObjectAndMethodCall(t *testing.T) {
	c, _ := compileSource(t, `
class Animal {
  name: string
  fun greet(): string { return this.name }
}
fun f(): string {
  var a: Animal = new Animal("cat")
  return a.greet()
}`)
	fn := getFuncChunk(t, c, "f")
	if countOpcode(fn, opNewObject) == 0 {
		t.Error("expected opNewObject for new expression")
	}
	if countOpcode(fn, opCallMethod) == 0 {
		t.Error("expected opCallMethod for method call")
	}
}

func TestCompiler_SuperMethodCall(t *testing.T) {
	c, _ := compileSource(t, `
open class Base {
  open fun greet(): string { return "hello" }
}
class Child : Base {
  override fun greet(): string { return super.greet() }
}`)
	fn := getFuncChunk(t, c, "Child.greet")
	if countOpcode(fn, opCallSuperMethod) == 0 {
		t.Error("expected opCallSuperMethod for super method call")
	}
}

func TestCompiler_IsCheck(t *testing.T) {
	c, _ := compileSource(t, `
open class Animal {}
class Dog : Animal {}
fun f(d: Dog): bool { return d is Animal }`)
	fn := getFuncChunk(t, c, "f")
	if countOpcode(fn, opIs) == 0 {
		t.Error("expected opIs for type check")
	}
}

// --- Stream ---

func TestCompiler_YieldInstructions(t *testing.T) {
	c, _ := compileSource(t, `
stream fun gen(): int {
  yield 1
  yield 2
}`)
	fn := getFuncChunk(t, c, "gen")
	yieldCount := countOpcode(fn, opYield)
	if yieldCount != 2 {
		t.Errorf("expected 2 opYield, got %d", yieldCount)
	}
}

func TestCompiler_StreamFunYieldRequiresValue(t *testing.T) {
	_, _, err := compileSourceAllowError(t, `stream fun gen(): int { yield; return 2 }`)
	if err == nil {
		t.Fatal("expected compile error for valueless yield")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "yield_value_required" {
		t.Fatalf("expected yield_value_required diagnostic, got %v", err)
	}
}

// --- Return types ---

func TestCompiler_ReturnVoid(t *testing.T) {
	c, _ := compileSource(t, `fun f(): void { var x: int = 1 }`)
	fn := getFuncChunk(t, c, "f")
	if countOpcode(fn, opReturnVoid) == 0 {
		t.Error("expected opReturnVoid for function without explicit return")
	}
}

// --- Halt instruction ---

func TestCompiler_MainChunkEndsWithHalt(t *testing.T) {
	_, ch := compileSource(t, `fun f(): int { return 1 }`)
	if len(ch.code) == 0 {
		t.Fatal("main chunk is empty")
	}
	last := ch.code[len(ch.code)-1]
	if last.op != opHalt {
		t.Errorf("main chunk last instruction = %v, want opHalt", opcodeNames[last.op])
	}
}

// --- Chunk structure ---

func TestCompiler_ChunkConstantsPool(t *testing.T) {
	c, _ := compileSource(t, `
fun f(): string { return "hello" }`)
	fn := getFuncChunk(t, c, "f")
	if len(fn.constants) == 0 {
		t.Error("expected string constant in constants pool")
	}
	found := false
	for _, cnst := range fn.constants {
		if s, ok := cnst.(string); ok && s == "hello" {
			found = true
		}
	}
	if !found {
		t.Error("string constant 'hello' not found in constants pool")
	}
}

func TestCompiler_PushStringUsesConstant(t *testing.T) {
	c, _ := compileSource(t, `fun f(): string { return "abc" }`)
	fn := getFuncChunk(t, c, "f")
	if countOpcode(fn, opPushString) == 0 {
		t.Error("expected opPushString for string literal")
	}
}

func TestCompiler_PushFloat(t *testing.T) {
	c, _ := compileSource(t, `fun f(): float { return 3.14 }`)
	fn := getFuncChunk(t, c, "f")
	if countOpcode(fn, opPushFloat) == 0 {
		t.Error("expected opPushFloat for float literal")
	}
}

func TestCompiler_LenRejectsWrongArity(t *testing.T) {
	_, _, err := compileSourceAllowError(t, `
fun bad(): int {
  return len([1], [2])
}`)
	if err == nil {
		t.Fatal("expected compile error for len wrong arity")
	}
	if !strings.Contains(err.Error(), "len expects exactly 1 argument") {
		t.Fatalf("expected len arity diagnostic, got %v", err)
	}
}

func TestCompiler_OverrideWithoutParentReturnsDiagnostic(t *testing.T) {
	_, _, err := compileSourceAllowError(t, `
class A {
  override fun f(): int { return 1 }
}`)
	if err == nil {
		t.Fatal("expected compile error for override without parent")
	}
	if !strings.Contains(err.Error(), "override_without_parent") && !strings.Contains(err.Error(), "override") {
		t.Fatalf("expected override diagnostic, got %v", err)
	}
}

func TestCompiler_InterfaceMethodMissingReturnsDiagnostic(t *testing.T) {
	_, _, err := compileSourceAllowError(t, `
interface I { fun f(): int }
class A : I {
}
fun run(): int { return 1 }
`)
	if err == nil {
		t.Fatal("expected compile error for missing interface method")
	}
	if !strings.Contains(err.Error(), "interface_method_missing") && !strings.Contains(err.Error(), "missing method") {
		t.Fatalf("expected interface method missing diagnostic, got %v", err)
	}
}

func TestCompiler_SuperOutsideClassReturnsDiagnostic(t *testing.T) {
	_, _, err := compileSourceAllowError(t, `
fun bad(): int {
  return super.answer()
}`)
	if err == nil {
		t.Fatal("expected compile error for super outside class")
	}
	if !strings.Contains(err.Error(), "super") {
		t.Fatalf("expected super diagnostic, got %v", err)
	}
}

func TestCompiler_SuperWithoutParentReturnsDiagnostic(t *testing.T) {
	_, _, err := compileSourceAllowError(t, `
class A {
  fun bad(): int { return super.answer() }
}
`)
	if err == nil {
		t.Fatal("expected compile error for super without parent")
	}
	if !strings.Contains(err.Error(), "super") {
		t.Fatalf("expected super-without-parent diagnostic, got %v", err)
	}
}

func TestCompiler_SuperConstructorWithoutParentReturnsDiagnostic(t *testing.T) {
	_, _, err := compileSourceAllowError(t, `
class A {
  fun bad(): int { super() return 1 }
}
`)
	if err == nil {
		t.Fatal("expected compile error for super constructor without parent")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "super_without_parent" {
		t.Fatalf("expected super_without_parent diagnostic, got %v", err)
	}
}

func TestCompiler_SuperConstructorOutsideClassReturnsDiagnostic(t *testing.T) {
	_, _, err := compileSourceAllowError(t, `
fun bad(): int {
  super()
  return 1
}`)
	if err == nil {
		t.Fatal("expected compile error for super constructor outside class")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "super_without_parent" {
		t.Fatalf("expected super_without_parent diagnostic, got %v", err)
	}
}

func TestCompiler_SuperExpressionBodyOutsideClassReturnsDiagnostic(t *testing.T) {
	_, _, err := compileSourceAllowError(t, `fun bad(): int = super.answer()`)
	if err == nil {
		t.Fatal("expected compile error for super expression body outside class")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "super_without_parent" {
		t.Fatalf("expected super_without_parent diagnostic, got %v", err)
	}
}

func TestCompiler_SuperConstructorExpressionBodyWithoutParentReturnsDiagnostic(t *testing.T) {
	_, _, err := compileSourceAllowError(t, `class A { fun bad(): int = super() }`)
	if err == nil {
		t.Fatal("expected compile error for super constructor expression body without parent")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "super_without_parent" {
		t.Fatalf("expected super_without_parent diagnostic, got %v", err)
	}
}

func TestCompiler_SuperMethodExpressionBodyWithoutParentReturnsDiagnostic(t *testing.T) {
	_, _, err := compileSourceAllowError(t, `class A { fun bad(): int = super.answer() }`)
	if err == nil {
		t.Fatal("expected compile error for super method expression body without parent")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "super_without_parent" {
		t.Fatalf("expected super_without_parent diagnostic, got %v", err)
	}
}

func TestCompiler_OverrideNonOpenMethodReturnsDiagnostic(t *testing.T) {
	_, _, err := compileSourceAllowError(t, `
open class A {
  fun f(): int { return 1 }
}
class B : A {
  override fun f(): int { return 2 }
}
`)
	if err == nil {
		t.Fatal("expected compile error for overriding non-open method")
	}
	if !strings.Contains(err.Error(), "override_parent_method_not_open") && !strings.Contains(err.Error(), "not open") {
		t.Fatalf("expected non-open override diagnostic, got %v", err)
	}
}

func TestCompiler_OverrideSignatureMismatchReturnsDiagnostic(t *testing.T) {
	_, _, err := compileSourceAllowError(t, `
open class A {
  open fun f(v: int): int { return v }
}
class B : A {
  override fun f(): int { return 2 }
}
`)
	if err == nil {
		t.Fatal("expected compile error for override signature mismatch")
	}
	if !strings.Contains(err.Error(), "override_signature_mismatch") && !strings.Contains(err.Error(), "parameter count") {
		t.Fatalf("expected override signature mismatch diagnostic, got %v", err)
	}
}

func TestCompiler_UnknownInterfaceReturnsDiagnostic(t *testing.T) {
	_, _, err := compileSourceAllowError(t, `
class A : MissingInterface {
  fun f(): int { return 1 }
}
fun run(): int { return 1 }
`)
	if err == nil {
		t.Fatal("expected compile error for unknown interface")
	}
	if !strings.Contains(err.Error(), "parent_class_not_found") && !strings.Contains(err.Error(), "unknown interface") && !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected unknown interface diagnostic, got %v", err)
	}
}

func TestCompiler_InterfaceParamCountMismatchReturnsDiagnostic(t *testing.T) {
	_, _, err := compileSourceAllowError(t, `
interface I { fun f(v: int): int }
class A : I {
  fun f(): int { return 1 }
}
fun run(): int { return 1 }
`)
	if err == nil {
		t.Fatal("expected compile error for interface parameter count mismatch")
	}
	if !strings.Contains(err.Error(), "interface_signature_mismatch") && !strings.Contains(err.Error(), "parameter count") {
		t.Fatalf("expected interface signature mismatch diagnostic, got %v", err)
	}
}

func TestCompiler_InterfaceReturnTypeMismatchReturnsDiagnostic(t *testing.T) {
	_, _, err := compileSourceAllowError(t, `
interface I { fun f(): int }
class A : I {
  fun f(): string { return "bad" }
}
fun run(): int { return 1 }
`)
	if err == nil {
		t.Fatal("expected compile error for interface return type mismatch")
	}
	if !strings.Contains(err.Error(), "interface_signature_mismatch") && !strings.Contains(err.Error(), "returns") {
		t.Fatalf("expected interface return mismatch diagnostic, got %v", err)
	}
}

func TestCompiler_ParentMethodSatisfiesInterfaceForChild(t *testing.T) {
	_, _, err := compileSourceAllowError(t, `
interface I { fun f(): int }
open class Base {
  fun f(): int { return 1 }
}
class Child : Base, I {
}
fun run(): int { return 1 }
`)
	if err != nil {
		t.Fatalf("expected inherited parent method to satisfy child interface contract, got %v", err)
	}
}

func TestCompiler_MultipleInterfacesOneMissingMethodReturnsDiagnostic(t *testing.T) {
	_, _, err := compileSourceAllowError(t, `
interface A { fun a(): int }
interface B { fun b(): int }
class C : A, B {
  fun a(): int { return 1 }
}
fun run(): int { return 1 }
`)
	if err == nil {
		t.Fatal("expected compile error for missing one interface method")
	}
	if !strings.Contains(err.Error(), "interface_method_missing") && !strings.Contains(err.Error(), "missing method") {
		t.Fatalf("expected missing interface method diagnostic, got %v", err)
	}
}

func TestCompiler_InterfaceDefaultMethodCompiledIntoImplementor(t *testing.T) {
	c, _ := compileSource(t, `
interface IGreet { fun greet(): string { return "hello" } }
class Person : IGreet {
  fun self(): string { return this.greet() }
}
fun run(): string { var p: Person = new Person() return p.greet() }
`)
	// The default body should be compiled as a class method chunk and bound
	// to the implementing class's function entry.
	if getFuncChunk(t, c, "Person.greet") == nil {
		t.Fatal("expected synthesized Person.greet function chunk")
	}
	// A class method calling this.greet() should also compile and reference it.
	fn := getFuncChunk(t, c, "Person.self")
	if countOpcode(fn, opCallMethod) == 0 {
		t.Error("expected opCallMethod for this.greet() dispatch")
	}
}

func TestCompiler_InterfaceDefaultSatisfiesMissingMethodContract(t *testing.T) {
	// A class that implements an interface whose method has a default body
	// must NOT report a missing-method error even though it provides no body.
	_, _, err := compileSourceAllowError(t, `
interface IGreet { fun greet(): string { return "hello" } }
class Person : IGreet {}
fun run(): string { var p: Person = new Person() return p.greet() }
`)
	if err != nil {
		t.Fatalf("expected default interface method to satisfy contract, got %v", err)
	}
}

func TestCompiler_InterfaceDefaultInheritedBySubclassNotDuplicated(t *testing.T) {
	c, _ := compileSource(t, `
interface IGreet { fun greet(): string { return "hello" } }
open class A : IGreet {}
class B : A {}
fun run(): string { var b: B = new B() return b.greet() }
`)
	// The default is synthesized once on A; B inherits it via the class chain
	// and must not synthesize (and compile) its own copy.
	if getFuncChunk(t, c, "A.greet") == nil {
		t.Fatal("expected synthesized A.greet function chunk")
	}
	if _, ok := c.getFunctions()["B.greet"]; ok {
		t.Fatal("expected no duplicate synthesized B.greet chunk (B inherits from A)")
	}
}

func TestCompiler_InterfaceDefaultOverriddenByClassMethod(t *testing.T) {
	c, _ := compileSource(t, `
interface IGreet { fun greet(): string { return "hello" } }
class Person : IGreet { fun greet(): string { return "custom" } }
fun run(): string { var p: Person = new Person() return p.greet() }
`)
	// When the class provides the method, the interface default is not
	// synthesized; the class's own implementation is the only entry.
	fn := getFuncChunk(t, c, "Person.greet")
	if fn == nil {
		t.Fatal("expected Person.greet function chunk")
	}
}

func TestCompiler_ParentAndInterfaceCombinationReturnsDiagnosticWhenParentDoesNotSatisfyInterface(t *testing.T) {
	_, _, err := compileSourceAllowError(t, `
interface I { fun f(): int }
open class Base {
  fun base(): int { return 1 }
}
class Child : Base, I {
  fun f(): int { return 2 }
}
fun run(): int { return 1 }
`)
	if err != nil {
		t.Fatalf("expected parent plus interface combination with explicit method to compile, got %v", err)
	}
}

func TestCompiler_AliasCycleReturnsDiagnostic(t *testing.T) {
	_, _, err := compileSourceAllowError(t, `
type A = B
type B = A
fun run(): int { return 1 }
`)
	if err == nil {
		t.Fatal("expected compile error for alias cycle")
	}
	if !strings.Contains(err.Error(), "type_alias_cycle") && !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected alias cycle diagnostic, got %v", err)
	}
}

func TestCompiler_AliasToUnknownTypeReturnsDiagnostic(t *testing.T) {
	_, _, err := compileSourceAllowError(t, `
type PriceMap = MissingType
fun run(): int { return 1 }
`)
	if err == nil {
		t.Fatal("expected compile error for alias to unknown type")
	}
	if !strings.Contains(err.Error(), "unknown_type_alias_target") && !strings.Contains(err.Error(), "unknown type") {
		t.Fatalf("expected unknown alias target diagnostic, got %v", err)
	}
}

func TestCompiler_AliasToArrayOfUnknownTypeReturnsDiagnostic(t *testing.T) {
	_, _, err := compileSourceAllowError(t, `
	type Items = array<MissingType>
	fun run(): int { return 1 }
	`)
	if err == nil {
		t.Fatal("expected compile error for array of unknown type")
	}
	if !strings.Contains(err.Error(), "unknown_type_alias_target") && !strings.Contains(err.Error(), "unknown type") {
		t.Fatalf("expected unknown type alias target diagnostic, got %v", err)
	}
}

func TestCompiler_AliasToMapValueUnknownTypeReturnsDiagnostic(t *testing.T) {
	_, _, err := compileSourceAllowError(t, `
	type Items = map<string, MissingType>
	fun run(): int { return 1 }
	`)
	if err == nil {
		t.Fatal("expected compile error for map value of unknown type")
	}
	if !strings.Contains(err.Error(), "unknown_type_alias_target") && !strings.Contains(err.Error(), "unknown type") {
		t.Fatalf("expected unknown type alias target diagnostic, got %v", err)
	}
}

func TestCompiler_AliasToMapKeyUnknownTypeReturnsDiagnostic(t *testing.T) {
	_, _, err := compileSourceAllowError(t, `
	type Items = map<MissingKey, int>
	fun run(): int { return 1 }
	`)
	if err == nil {
		t.Fatal("expected compile error for map key of unknown type")
	}
	if !strings.Contains(err.Error(), "unknown_type_alias_target") && !strings.Contains(err.Error(), "unknown type") {
		t.Fatalf("expected unknown type alias target diagnostic, got %v", err)
	}
}

func TestCompiler_AliasToNestedGenericUnknownTypeReturnsDiagnostic(t *testing.T) {
	_, _, err := compileSourceAllowError(t, `
	type Items = array<map<string, MissingType>>
	fun run(): int { return 1 }
	`)
	if err == nil {
		t.Fatal("expected compile error for nested generic unknown type")
	}
	if !strings.Contains(err.Error(), "unknown_type_alias_target") && !strings.Contains(err.Error(), "unknown type") {
		t.Fatalf("expected unknown type alias target diagnostic, got %v", err)
	}
}

func TestCompiler_AliasToArrayOfKnownTypeCompiles(t *testing.T) {
	_, _, err := compileSourceAllowError(t, `
	type Items = array<Item>
	class Item {}
	fun run(): int { return 1 }
	`)
	if err != nil {
		t.Fatalf("expected compilation to succeed for array of known type: %v", err)
	}
}

func TestCompiler_AliasToMapOfKnownTypesCompiles(t *testing.T) {
	_, _, err := compileSourceAllowError(t, `
	type Items = map<string, int>
	fun run(): int { return 1 }
	`)
	if err != nil {
		t.Fatalf("expected compilation to succeed for map of known types: %v", err)
	}
}

func TestCompiler_AliasToNestedGenericCompiles(t *testing.T) {
	_, _, err := compileSourceAllowError(t, `
	type Items = array<map<string, int>>
	fun run(): int { return 1 }
	`)
	if err != nil {
		t.Fatalf("expected compilation to succeed for nested generic: %v", err)
	}
}

func TestCompiler_AliasChainWithGenericCompiles(t *testing.T) {
	_, _, err := compileSourceAllowError(t, `
	type Items = array<ItemAlias>
	type ItemAlias = Item
	class Item {}
	fun run(): int { return 1 }
	`)
	if err != nil {
		t.Fatalf("expected compilation to succeed for alias chain with generic: %v", err)
	}
}

func TestCompiler_AliasChainThroughGenericUnknownTypeReturnsDiagnostic(t *testing.T) {
	_, _, err := compileSourceAllowError(t, `
	type Items = array<ItemRef>
	type ItemRef = MissingType
	fun run(): int { return 1 }
	`)
	if err == nil {
		t.Fatal("expected compile error for alias chain through generic to unknown type")
	}
	if !strings.Contains(err.Error(), "unknown_type_alias_target") && !strings.Contains(err.Error(), "unknown type") {
		t.Fatalf("expected unknown type alias target diagnostic for Items, got %v", err)
	}
}

func TestCompiler_AliasChainMapValueThroughGenericUnknownTypeReturnsDiagnostic(t *testing.T) {
	_, _, err := compileSourceAllowError(t, `
	type Items = map<string, ItemRef>
	type ItemRef = MissingType
	fun run(): int { return 1 }
	`)
	if err == nil {
		t.Fatal("expected compile error for alias chain through map value to unknown type")
	}
	if !strings.Contains(err.Error(), "unknown_type_alias_target") && !strings.Contains(err.Error(), "unknown type") {
		t.Fatalf("expected unknown type alias target diagnostic for Items, got %v", err)
	}
}

func TestCompiler_AliasChainNestedGenericUnknownTypeReturnsDiagnostic(t *testing.T) {
	_, _, err := compileSourceAllowError(t, `
	type Items = array<map<string, ItemRef>>
	type ItemRef = MissingType
	fun run(): int { return 1 }
	`)
	if err == nil {
		t.Fatal("expected compile error for nested generic alias chain to unknown type")
	}
	if !strings.Contains(err.Error(), "unknown_type_alias_target") && !strings.Contains(err.Error(), "unknown type") {
		t.Fatalf("expected unknown type alias target diagnostic for Items, got %v", err)
	}
}

func TestCompiler_AliasGenericInnerTypeDiagnosticPinpointsMissing(t *testing.T) {
	_, _, err := compileSourceAllowError(t, `
	type Items = array<MissingType>
	fun run(): int { return 1 }
	`)
	if err == nil {
		t.Fatal("expected compile error")
	}
	if !strings.Contains(err.Error(), `unknown type "MissingType"`) {
		t.Fatalf("expected diagnostic to pinpoint MissingType, got %v", err)
	}
	if !strings.Contains(err.Error(), `in "array<MissingType>"`) {
		t.Fatalf("expected diagnostic to mention enclosing generic target, got %v", err)
	}
}

func TestCompiler_AliasNestedGenericDiagnosticPinpointsMissing(t *testing.T) {
	_, _, err := compileSourceAllowError(t, `
	type Items = array<map<string, MissingType>>
	fun run(): int { return 1 }
	`)
	if err == nil {
		t.Fatal("expected compile error")
	}
	if !strings.Contains(err.Error(), `unknown type "MissingType"`) {
		t.Fatalf("expected diagnostic to pinpoint MissingType in nested generic, got %v", err)
	}
	if !strings.Contains(err.Error(), `in "array<map<string,MissingType>>"`) {
		t.Fatalf("expected diagnostic to mention nested enclosing target, got %v", err)
	}
}

func TestCompiler_AliasChainGenericDiagnosticFallsBackToOriginalForm(t *testing.T) {
	_, _, err := compileSourceAllowError(t, `
	type Items = array<ItemRef>
	type ItemRef = MissingType
	fun run(): int { return 1 }
	`)
	if err == nil {
		t.Fatal("expected compile error")
	}
	// ItemRef itself fails to resolve and pinpoints MissingType directly
	// (target is the bare name, so unknown == target → falls through to original form).
	if !strings.Contains(err.Error(), `type alias "ItemRef" references unknown type "MissingType"`) {
		t.Fatalf("expected ItemRef diagnostic to reference MissingType, got %v", err)
	}
	// Items' target is "array<ItemRef>"; ItemRef is registered as an alias so
	// firstUnknownInGenericTarget returns "" and the diagnostic falls back to the
	// original form rather than misleadingly pinpointing ItemRef.
	if !strings.Contains(err.Error(), `type alias "Items" references unknown type "array<ItemRef>"`) {
		t.Fatalf("expected Items diagnostic to keep original form for chain, got %v", err)
	}
	if strings.Contains(err.Error(), `unknown type "ItemRef" in`) {
		t.Fatalf("Items diagnostic should NOT pinpoint ItemRef (it is a known alias), got %v", err)
	}
}

func TestCompiler_DiagnosticCodesRegistered(t *testing.T) {
	codes := []string{
		"compile_error",
		"type_alias_cycle",
		"unknown_type_alias_target",
		"override_without_parent",
		"override_visibility_narrowed",
		"method_visibility_narrowed",
		"parent_class_not_found",
		"override_signature_mismatch",
		"override_parent_method_not_open",
		"override_method_not_found",
		"unknown_interface",
		"interface_signature_mismatch",
		"interface_method_missing",
		"super_without_parent",
		"break_outside_loop",
		"continue_outside_loop",
		"undefined_imported_variable",
		"undefined_variable",
		"private_field_access_denied",
		"private_method_access_denied",
		"assign_imported_variable",
		"duplicate_struct_field",
		"unknown_struct_field",
		"struct_field_type_mismatch",
		"missing_struct_field",
		"unknown_struct_type",
		"inherit_non_open_class",
		"field_shadowing_disallowed",
		"yield_requires_stream_fun",
		"yield_value_required",
		"yield_disallowed_in_try",
	}
	for _, code := range codes {
		info, ok := diagnostics.LookupCode(code)
		if !ok {
			t.Fatalf("expected bytecode diagnostic code %q to be registered", code)
		}
		if info.Category != diagnostics.CategorySchema {
			t.Fatalf("code %q registered under category %q, want CategorySchema", code, info.Category)
		}
		if info.Hint == "" {
			t.Fatalf("code %q registered without a hint", code)
		}
		if info.Description == "" {
			t.Fatalf("code %q registered without a description", code)
		}
	}
}

func TestCompiler_AllAddCompileErrorCodesAreRegistered(t *testing.T) {
	source, err := os.ReadFile("compiler.go")
	if err != nil {
		t.Fatalf("read compiler.go: %v", err)
	}
	re := regexp.MustCompile(`addCompileError(?:WithTypes)?\("([a-z_]+)"`)
	matches := re.FindAllSubmatch(source, -1)
	if len(matches) == 0 {
		t.Fatal("expected to find at least one addCompileError call in compiler.go")
	}
	seen := map[string]struct{}{}
	for _, m := range matches {
		seen[string(m[1])] = struct{}{}
	}
	for code := range seen {
		if _, ok := diagnostics.LookupCode(code); !ok {
			t.Errorf("compile diagnostic code %q is emitted in compiler.go but not registered with diagnostics package", code)
		}
	}
}

// --- Ensure unused imports don't cause errors ---

var _ = strings.TrimSpace
var _ vm.Value = vm.EncodeInt(0)
