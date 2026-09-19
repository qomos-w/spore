package bytecode

import (
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/internal/script/frontend"
	"github.com/qomos-w/spore/internal/script/vm"
)

// compileAndCallAfterGlobals parses and compiles the source, executes the
// top-level chunk (so global initializers run), then calls fnName.
func compileAndCallAfterGlobals(t *testing.T, source, fnName string, args []vm.Value) (vm.Value, error) {
	t.Helper()

	prog, err := frontend.ParseModuleForTest(source)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	v := vm.NewVM(4096, 256)
	c := newCompiler()
	mainChunk, err := c.compile(prog)
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	interp := newInterpreter(v)
	c.registerFunctions(v, interp)
	c.registerClasses(v)
	c.registerStructs(v)

	if _, err := interp.Execute(mainChunk, c.getFunctions()); err != nil {
		t.Fatalf("global init error: %v", err)
	}

	fnChunk, ok := c.getFunctions()[fnName]
	if !ok {
		t.Fatalf("function %q not found in compiled chunks", fnName)
	}
	return interp.ExecuteFunction(fnChunk, binding.InvocationStageUnary, args)
}

// --- Lambda as value: assignment and direct call ---

func TestLambdaAssignToLocalAndCall(t *testing.T) {
	source := `
fun main(): int {
  var dbl: any = fun(x: int): int { return x * 2 }
  return dbl(21)
}`

	result, err := compileAndCall(t, source, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 42 {
		t.Fatalf("expected 42, got %d", got)
	}
}

func TestLambdaExpressionBody(t *testing.T) {
	source := `
fun main(): int {
  var square: any = fun(x: int): int = x * x
  return square(9)
}`

	result, err := compileAndCall(t, source, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 81 {
		t.Fatalf("expected 81, got %d", got)
	}
}

func TestLambdaReassignmentKeepsIndependentState(t *testing.T) {
	source := `
fun main(): int {
  var total: int = 0
  var add: any = fun(x: int): int { total = total + x; return total }
  add(5)
  add(7)
  var add2: any = fun(x: int): int { total = total + x; return total }
  add2(1)
  return total
}`

	result, err := compileAndCall(t, source, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 13 {
		t.Fatalf("expected 13, got %d", got)
	}
}

// --- Lambda passed as argument (higher-order functions) ---

func TestLambdaAsArgumentHigherOrder(t *testing.T) {
	source := `
fun applyTwice(f: any, v: int): int {
  return f(f(v))
}

fun main(): int {
  return applyTwice(fun(x: int): int { return x + 3 }, 10)
}`

	result, err := compileAndCall(t, source, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 16 {
		t.Fatalf("expected 16, got %d", got)
	}
}

func TestLambdaMapFilterPattern(t *testing.T) {
	source := `
fun transform(arr: array, f: any): array {
  var out: array = []
  for (item in arr) {
    push(out, f(item))
  }
  return out
}

fun sum(arr: array): int {
  var total: int = 0
  for (item in arr) {
    total = total + item
  }
  return total
}

fun main(): int {
  var doubled: array = transform([1, 2, 3, 4], fun(x: int): int { return x * 2 })
  return sum(doubled)
}`

	result, err := compileAndCall(t, source, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 20 {
		t.Fatalf("expected 20, got %d", got)
	}
}

// --- Closures: read-only capture ---

func TestClosureCaptureReadOnly(t *testing.T) {
	source := `
fun main(): int {
  var factor: int = 10
  var scale: any = fun(x: int): int { return x * factor }
  return scale(4) + scale(5)
}`

	result, err := compileAndCall(t, source, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 90 {
		t.Fatalf("expected 90, got %d", got)
	}
}

// --- Closures: writable capture (counter pattern) ---

func TestClosureCaptureWritableCounter(t *testing.T) {
	source := `
fun main(): int {
  var count: int = 0
  var inc: any = fun(by: int): int { count = count + by; return count }
  inc(1)
  inc(2)
  inc(3)
  return count
}`

	result, err := compileAndCall(t, source, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 6 {
		t.Fatalf("expected 6, got %d", got)
	}
}

func TestClosureCaptureOuterWriteVisibleInside(t *testing.T) {
	source := `
fun main(): int {
  var base: int = 100
  var read: any = fun(): int { return base }
  var first: int = read()
  base = 200
  var second: int = read()
  return first + second
}`

	result, err := compileAndCall(t, source, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 300 {
		t.Fatalf("expected 300, got %d", got)
	}
}

func TestClosureCaptureParameter(t *testing.T) {
	source := `
fun makeAdder(n: int): any {
  var add: any = fun(x: int): int { return x + n }
  return add
}

fun main(): int {
  var add5: any = makeAdder(5)
  var add7: any = makeAdder(7)
  return add5(10) + add7(10)
}`

	result, err := compileAndCall(t, source, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 32 {
		t.Fatalf("expected 32, got %d", got)
	}
}

// --- Nested lambdas ---

func TestNestedLambdasTransitiveCapture(t *testing.T) {
	source := `
fun main(): int {
  var x: int = 2
  var outer: any = fun(y: int): any {
    var inner: any = fun(z: int): int { return x + y + z }
    return inner
  }
  var fn: any = outer(10)
  return fn(100)
}`

	result, err := compileAndCall(t, source, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 112 {
		t.Fatalf("expected 112, got %d", got)
	}
}

// --- Closure capture of loop variables ---

func TestClosureCapturesForInLoopVariable(t *testing.T) {
	// Per-iteration binding: each closure remembers its own iteration value.
	source := `
fun main(): int {
  var fns: array = []
  for (i in [10, 20, 30]) {
    push(fns, fun(): int { return i })
  }
  var first: any = fns[0]
  var second: any = fns[1]
  var third: any = fns[2]
  return first() + second() + third()
}`

	result, err := compileAndCall(t, source, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 60 {
		t.Fatalf("expected 60, got %d", got)
	}
}

func TestClosureCapturesCStyleForVariable(t *testing.T) {
	// Single binding for C-style loops: closures share the one variable.
	source := `
fun main(): int {
	var total: int = 0
  var i: int = 0
  var step: any = fun(): int { total = total + i; return total }
  for (i = 0; i < 3; i = i + 1) {
    step()
  }
  return total
}`

	result, err := compileAndCall(t, source, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 3 {
		t.Fatalf("expected 3, got %d", got)
	}
}

// --- Lambda stored in a global and called ---

func TestLambdaStoredInGlobalVar(t *testing.T) {
	source := `
var negate: any = fun(x: int): int { return -x }

fun main(): int {
  return negate(17)
}`

	result, err := compileAndCallAfterGlobals(t, source, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != -17 {
		t.Fatalf("expected -17, got %d", got)
	}
}

// --- Lambda using global state ---

func TestLambdaReadsGlobal(t *testing.T) {
	source := `
var threshold: int = 5

fun main(): int {
  var check: any = fun(x: int): int {
    if (x > threshold) { return 1 }
    return 0
  }
  return check(10) * 10 + check(3)
}`

	result, err := compileAndCallAfterGlobals(t, source, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 10 {
		t.Fatalf("expected 10, got %d", got)
	}
}

// --- Lambda in class method capturing `this` ---

func TestLambdaInMethodCapturesThis(t *testing.T) {
	source := `
class Box {
  value: int
  fun get(): int { return this.value }
}

fun main(): int {
  var b: Box = new Box()
  b.value = 7
  var read: any = fun(): int { return b.value }
  return read() + b.get()
}`

	result, err := compileAndCall(t, source, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 14 {
		t.Fatalf("expected 14, got %d", got)
	}
}

func TestLambdaInsideMethodCapturesReceiver(t *testing.T) {
	source := `
class Counter {
  total: int
  fun bump(by: int): int {
    var add: any = fun(): int { this.total = this.total + by; return this.total }
    add()
    return add()
  }
}

fun main(): int {
  var c: Counter = new Counter()
  return c.bump(4)
}`

	result, err := compileAndCall(t, source, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 8 {
		t.Fatalf("expected 8, got %d", got)
	}
}

// --- Lambda returned from function and called with captured loop var ---

func TestLambdaFactoryWithLoopCapture(t *testing.T) {
	source := `
fun makeMultipliers(): array {
  var fns: array = []
  for (m in [2, 3]) {
    push(fns, fun(x: int): int { return x * m })
  }
  return fns
}

fun main(): int {
  var fns: array = makeMultipliers()
  var by2: any = fns[0]
  var by3: any = fns[1]
  return by2(5) + by3(5)
}`

	result, err := compileAndCall(t, source, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 25 {
		t.Fatalf("expected 25, got %d", got)
	}
}

// --- Lambda capturing a block-scoped variable that outlives its block ---

func TestClosureCapturesBlockScopedVariable(t *testing.T) {
	source := `
fun main(): int {
  var f: any = fun(): int { return 0 }
  if (true) {
    var x: int = 5
    f = fun(): int { return x }
  }
  return f()
}`

	result, err := compileAndCall(t, source, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 5 {
		t.Fatalf("expected 5, got %d", got)
	}
}

// --- Negative: capture before declaration is a compile error ---

func TestLambdaCaptureBeforeDeclarationIsError(t *testing.T) {
	source := `
fun main(): int {
  var f: any = fun(): int { return x }
  var x: int = 1
  return f()
}`

	prog, err := frontend.ParseModuleForTest(source)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	_ = vm.NewVM(4096, 256)
	c := newCompiler()
	_, err = c.compile(prog)
	if err == nil {
		t.Fatal("expected compile error for capture-before-declaration")
	}
}

// --- Negative: yield is not allowed inside a lambda ---

func TestLambdaYieldIsRejected(t *testing.T) {
	source := `
stream fun gen(): int {
  var f: any = fun(): int { yield 1; return 0 }
  return f()
}`

	prog, err := frontend.ParseModuleForTest(source)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	_ = vm.NewVM(4096, 256)
	c := newCompiler()
	_, err = c.compile(prog)
	if err == nil {
		t.Fatal("expected compile error for yield inside lambda")
	}
}
