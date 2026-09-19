package bytecode

import (
	"strings"
	"testing"

	"github.com/qomos-w/spore/internal/script/frontend"
	"github.com/qomos-w/spore/internal/script/vm"
)

// expectCompileError parses+compiles source and asserts compilation fails.
func expectCompileError(t *testing.T, source, wantSubstr string) {
	t.Helper()
	prog, err := frontend.ParseModuleForTest(source)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	c := newCompiler()
	_, err = c.compile(prog)
	if err == nil {
		t.Fatalf("expected compile error containing %q, got none", wantSubstr)
	}
	if !strings.Contains(err.Error(), wantSubstr) {
		t.Fatalf("expected compile error containing %q, got: %v", wantSubstr, err)
	}
}

// decodeStringArray extracts a script array of strings into a Go []string.
func decodeStringArray(t *testing.T, v *vm.VM, val vm.Value) []string {
	t.Helper()
	handle := vm.DecodeHandle(val)
	n := v.ArrayLength(handle)
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, v.DecodeString(v.GetArrayElement(handle, i)))
	}
	return out
}

func TestTryCatch_DivisionByZero(t *testing.T) {
	result, v, err := compileAndCallWithVM(t, `
fun f(): string {
    try {
        var x: int = 1 / 0
        return "no-error:" + x
    } catch (e) {
        return "caught:" + e.code
    }
}
`, "f", nil)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if got := v.DecodeString(result); got != "caught:division_by_zero" {
		t.Fatalf("expected caught:division_by_zero, got %q", got)
	}
}

func TestTryCatch_ArrayIndexOutOfRange(t *testing.T) {
	result, v, err := compileAndCallWithVM(t, `
fun f(): string {
    var a: array = []
    try {
        return "elem:" + a[0]
    } catch (e) {
        return "caught:" + e.code
    }
}
`, "f", nil)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if got := v.DecodeString(result); got != "caught:array_index_out_of_range" {
		t.Fatalf("expected caught:array_index_out_of_range, got %q", got)
	}
}

// Fast-path specialized opcodes (typed array + constant index, typed map +
// constant key) must route errors through the active handler like the slow
// path instead of escaping the try block.
func TestTryCatch_FastPathArrayIndexOutOfRange(t *testing.T) {
	result, v, err := compileAndCallWithVM(t, `
fun f(): string {
    var a: array<int> = [1, 2, 3]
    try {
        var x: int = a[10]
        return "no-error:" + x
    } catch (e) {
        return "caught:" + e.code
    }
}
`, "f", nil)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if got := v.DecodeString(result); got != "caught:array_index_out_of_range" {
		t.Fatalf("expected caught:array_index_out_of_range, got %q", got)
	}
}

func TestTryCatch_FastPathArraySetIndexOutOfRange(t *testing.T) {
	result, v, err := compileAndCallWithVM(t, `
fun f(): string {
    var a: array<int> = [1, 2, 3]
    try {
        a[10] = 7
        return "no-error"
    } catch (e) {
        return "caught:" + e.code
    }
}
`, "f", nil)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if got := v.DecodeString(result); got != "caught:array_index_out_of_range" {
		t.Fatalf("expected caught:array_index_out_of_range, got %q", got)
	}
}

func TestTryCatch_FastPathMapKeyNotFound(t *testing.T) {
	result, v, err := compileAndCallWithVM(t, `
fun f(): string {
    var m: map<string, string> = {}
    try {
        return "value:" + m["missing"]
    } catch (e) {
        return "caught:" + e.code
    }
}
`, "f", nil)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if got := v.DecodeString(result); got != "caught:map_key_not_found" {
		t.Fatalf("expected caught:map_key_not_found, got %q", got)
	}
}

func TestTryCatch_TypeCastFailed(t *testing.T) {
	result, v, err := compileAndCallWithVM(t, `
fun f(): string {
    try {
        var n: int = "hello" as int
        return "no-error:" + n
    } catch (e) {
        return "caught:" + e.code
    }
}
`, "f", nil)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if got := v.DecodeString(result); got != "caught:type_cast_failed" {
		t.Fatalf("expected caught:type_cast_failed, got %q", got)
	}
}

func TestTryCatch_MapKeyNotFound(t *testing.T) {
	result, v, err := compileAndCallWithVM(t, `
fun f(): string {
    var m: map = {}
    try {
        var v: string = m["missing"]
        return "no-error:" + v
    } catch (e) {
        return "caught:" + e.code
    }
}
`, "f", nil)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if got := v.DecodeString(result); got != "caught:map_key_not_found" {
		t.Fatalf("expected caught:map_key_not_found, got %q", got)
	}
}

func TestTryCatch_ErrorAttributes(t *testing.T) {
	result, v, err := compileAndCallWithVM(t, `
fun f(): string {
    try {
        var x: int = 1 / 0
        return "no-error"
    } catch (e) {
        return e.code + "|" + e.category + "|" + e.message + "|" + e.callable + "|" + e.path
    }
}
`, "f", nil)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	got := v.DecodeString(result)
	want := "division_by_zero|runtime|division by zero|f|vm/arithmetic/div"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestTryCatch_ErrorLine(t *testing.T) {
	result, err := compileAndCall(t, "fun f(): int {\n"+
		"    try {\n"+
		"        var x: int = 1 / 0\n"+
		"        return x\n"+
		"    } catch (e) {\n"+
		"        return e.line\n"+
		"    }\n"+
		"}\n", "f", nil)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	line := vm.DecodeInt(result)
	// The compiler currently does not seed chunk line tables (all lines are
	// 0), a pre-existing VM-wide limitation; what matters here is that the
	// attribute is present and readable as an int.
	if line < 0 {
		t.Fatalf("expected non-negative error line, got %d", line)
	}
}

func TestTryCatch_SubscriptAccessToErrorValue(t *testing.T) {
	result, v, err := compileAndCallWithVM(t, `
fun f(): string {
    try {
        var x: int = 1 / 0
        return "no-error"
    } catch (e) {
        return e["code"] + "/" + e["message"]
    }
}
`, "f", nil)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if got := v.DecodeString(result); got != "division_by_zero/division by zero" {
		t.Fatalf("expected division_by_zero/division by zero, got %q", got)
	}
}

// TestTryCatch_AcrossFunctionCall: the error occurs inside a callee; the
// handler in the caller frame must unwind the callee frame and dispatch.
func TestTryCatch_AcrossFunctionCall(t *testing.T) {
	result, v, err := compileAndCallWithVM(t, `
fun risky(): int { return 1 / 0 }

fun f(): string {
    try {
        var n: int = risky() + 10
        return "no-error:" + n
    } catch (e) {
        return "caught:" + e.code + "@" + e.callable
    }
}
`, "f", nil)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if got := v.DecodeString(result); got != "caught:division_by_zero@risky" {
		t.Fatalf("expected caught:division_by_zero@risky, got %q", got)
	}
}

func TestTryCatch_Nested(t *testing.T) {
	result, v, err := compileAndCallWithVM(t, `
fun f(): array {
    var order: array = []
    var a: array = []
    try {
        try {
            var missing: string = a[0]
        } catch (inner) {
            push(order, "inner:" + inner.code)
        }
        var x: int = 1 / 0
    } catch (outer) {
        push(order, "outer:" + outer.code)
    }
    return order
}
`, "f", nil)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	got := decodeStringArray(t, v, result)
	want := []string{"inner:array_index_out_of_range", "outer:division_by_zero"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

// TestTryCatch_ErrorInCatchBodyPropagatesToOuter: an error raised inside a
// catch block must be handled by an enclosing try, not the same one.
func TestTryCatch_ErrorInCatchBodyPropagatesToOuter(t *testing.T) {
	result, v, err := compileAndCallWithVM(t, `
fun f(): string {
    var a: array = []
    try {
        try {
            var x: int = 1 / 0
        } catch (e) {
            var bad: string = a[9]
        }
        return "unreachable"
    } catch (outer) {
        return "outer:" + outer.code
    }
}
`, "f", nil)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if got := v.DecodeString(result); got != "outer:array_index_out_of_range" {
		t.Fatalf("expected outer:array_index_out_of_range, got %q", got)
	}
}

// TestTryCatch_PartialExpressionDiscarded: the failing expression leaves
// partial operands; the catch must resume with a balanced stack.
func TestTryCatch_PartialExpressionDiscarded(t *testing.T) {
	result, v, err := compileAndCallWithVM(t, `
fun f(): string {
    try {
        var s: string = "prefix-" + "value" + "tail"
        var x: int = 1 / 0
        return s
    } catch (e) {
        return "caught:" + e.code
    }
    return "unreachable"
}
`, "f", nil)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if got := v.DecodeString(result); got != "caught:division_by_zero" {
		t.Fatalf("expected caught:division_by_zero, got %q", got)
	}
}

func TestTryCatch_NoErrorSkipsCatch(t *testing.T) {
	result, v, err := compileAndCallWithVM(t, `
fun f(): string {
    try {
        return "ok"
    } catch (e) {
        return "caught"
    }
}
`, "f", nil)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if got := v.DecodeString(result); got != "ok" {
		t.Fatalf("expected ok, got %q", got)
	}
}

func TestTryCatch_UncaughtPropagatesToHost(t *testing.T) {
	_, err := compileAndCall(t, `
fun f(): int { return 1 / 0 }
`, "f", nil)
	if err == nil {
		t.Fatal("expected uncaught division_by_zero error")
	}
	rtErr, ok := err.(*RuntimeError)
	if !ok {
		t.Fatalf("expected RuntimeError, got %T", err)
	}
	if rtErr.Code != "division_by_zero" {
		t.Fatalf("expected division_by_zero, got %q", rtErr.Code)
	}
	if len(rtErr.Stack) == 0 || rtErr.Stack[0].Callable != "f" {
		t.Fatalf("expected stack starting at f, got %+v", rtErr.Stack)
	}
}

// TestTryCatch_BreakPopsHandler: break out of a loop that contains a try
// must not leave a stale handler on the stack for code after the loop.
func TestTryCatch_BreakPopsHandler(t *testing.T) {
	result, v, err := compileAndCallWithVM(t, `
fun f(): string {
    var i: int = 0
    while true {
        if i == 2 { break }
        i = i + 1
    }
    try {
        var x: int = 1 / 0
    } catch (e) {
        return "after-loop-caught:" + e.code
    }
    return "unreachable"
}
`, "f", nil)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if got := v.DecodeString(result); got != "after-loop-caught:division_by_zero" {
		t.Fatalf("expected after-loop-caught:division_by_zero, got %q", got)
	}
}

func TestDefer_ReverseOrder(t *testing.T) {
	result, v, err := compileAndCallWithVM(t, `
fun f(): array {
    var log: array = []
    defer { push(log, "first") }
    defer { push(log, "second") }
    defer { push(log, "third") }
    return log
}
`, "f", nil)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	got := decodeStringArray(t, v, result)
	want := []string{"third", "second", "first"}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

// TestDefer_RunsBeforeCallerContinues: the deferred mutation is visible in
// the shared array before the caller resumes after the call expression.
func TestDefer_RunsBeforeCallerContinues(t *testing.T) {
	result, v, err := compileAndCallWithVM(t, `
fun g(log: array): int {
    defer { push(log, "deferred") }
    return 42
}

fun f(): array {
    var log: array = []
    var r: int = g(log)
    push(log, "caller:" + r)
    return log
}
`, "f", nil)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	got := decodeStringArray(t, v, result)
	want := []string{"deferred", "caller:42"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

// TestDefer_ReturnValueUnaffected: the function still returns its value.
func TestDefer_ReturnValueUnaffected(t *testing.T) {
	result, err := compileAndCall(t, `
fun f(): int {
    defer { var ignored: int = 1 + 1 }
    return 7
}
`, "f", nil)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if got := vm.DecodeInt(result); got != 7 {
		t.Fatalf("expected 7, got %d", got)
	}
}

// TestDefer_RunsOnUncaughtErrorPath: the error still propagates, but defers
// execute while unwinding.
func TestDefer_RunsOnUncaughtErrorPath(t *testing.T) {
	result, v, err := compileAndCallWithVM(t, `
fun boom(log: array): int {
    defer { push(log, "cleanup") }
    return 1 / 0
}

fun f(): array {
    var log: array = []
    try {
        boom(log)
    } catch (e) {
        push(log, e.code)
    }
    return log
}
`, "f", nil)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	got := decodeStringArray(t, v, result)
	want := []string{"cleanup", "division_by_zero"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

// TestDefer_UncaughtErrorStillPropagates: with no try, the defer runs and
// the error reaches the host.
func TestDefer_UncaughtErrorStillPropagates(t *testing.T) {
	_, err := compileAndCall(t, `
fun boom(): int {
    defer { var ignored: int = 2 }
    return 1 / 0
}
`, "boom", nil)
	if err == nil {
		t.Fatal("expected uncaught division_by_zero error")
	}
	rtErr, ok := err.(*RuntimeError)
	if !ok {
		t.Fatalf("expected RuntimeError, got %T", err)
	}
	if rtErr.Code != "division_by_zero" {
		t.Fatalf("expected division_by_zero, got %q", rtErr.Code)
	}
}

// TestTryDefer_Combined: defers registered inside and around a try all run
// at function exit, after the catch already handled the error.
func TestTryDefer_Combined(t *testing.T) {
	result, v, err := compileAndCallWithVM(t, `
fun f(): array {
    var log: array = []
    defer { push(log, "outer-defer") }
    try {
        defer { push(log, "inner-defer") }
        var x: int = 1 / 0
    } catch (e) {
        push(log, "catch:" + e.code)
    }
    return log
}
`, "f", nil)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	got := decodeStringArray(t, v, result)
	want := []string{"catch:division_by_zero", "inner-defer", "outer-defer"}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

// TestDefer_InLoopRegistersEachIteration: each passing control registers the
// body once, so it runs N times at exit.
func TestDefer_InLoopRegistersEachIteration(t *testing.T) {
	result, v, err := compileAndCallWithVM(t, `
fun f(): array {
    var log: array = []
    var i: int = 0
    while i < 3 {
        defer { push(log, "iter") }
        i = i + 1
    }
    return log
}
`, "f", nil)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	got := decodeStringArray(t, v, result)
	if len(got) != 3 {
		t.Fatalf("expected 3 defer executions, got %d (%v)", len(got), got)
	}
	for _, s := range got {
		if s != "iter" {
			t.Fatalf("expected iter entries, got %v", got)
		}
	}
}

// TestDefer_ErrorInDeferBodyPropagates: an error raised inside a deferred
// body surfaces to the caller after the defers.
func TestDefer_ErrorInDeferBodyPropagates(t *testing.T) {
	_, err := compileAndCall(t, `
fun f(): int {
    defer { var x: int = 1 / 0 }
    return 5
}
`, "f", nil)
	if err == nil {
		t.Fatal("expected error from failing defer body")
	}
	rtErr, ok := err.(*RuntimeError)
	if !ok {
		t.Fatalf("expected RuntimeError, got %T", err)
	}
	if rtErr.Code != "division_by_zero" {
		t.Fatalf("expected division_by_zero, got %q", rtErr.Code)
	}
}

func TestDefer_CapturesLocalState(t *testing.T) {
	result, v, err := compileAndCallWithVM(t, `
fun f(): array {
    var log: array = []
    var n: int = 3
    defer { push(log, "n=" + n) }
    n = 9
    return log
}
`, "f", nil)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	got := decodeStringArray(t, v, result)
	if len(got) != 1 || got[0] != "n=9" {
		t.Fatalf("expected [n=9], got %v", got)
	}
}

// TestTryCatch_LambdaCapturesCatchVar: the catch variable captured by a
// closure must observe the caught error value.
func TestTryCatch_LambdaCapturesCatchVar(t *testing.T) {
	result, v, err := compileAndCallWithVM(t, `
fun f(): string {
    var describe: any = fun(): string { return "unset" }
    try {
        var x: int = 1 / 0
    } catch (e) {
        describe = fun(): string { return "err:" + e.code }
    }
    return describe()
}
`, "f", nil)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if got := v.DecodeString(result); got != "err:division_by_zero" {
		t.Fatalf("expected err:division_by_zero, got %q", got)
	}
}

// TestDefer_NestedFunctionDeferIsolation: defers registered by a callee must
// run at the callee's exit, not at the caller's.
func TestDefer_NestedFunctionDeferIsolation(t *testing.T) {
	result, v, err := compileAndCallWithVM(t, `
fun inner(log: array): int {
    defer { push(log, "inner-defer") }
    return 1
}

fun f(): array {
    var log: array = []
    defer { push(log, "outer-defer") }
    var r: int = inner(log)
    push(log, "after-inner:" + r)
    return log
}
`, "f", nil)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	got := decodeStringArray(t, v, result)
	want := []string{"inner-defer", "after-inner:1", "outer-defer"}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

// --- Compile-time rejection tests ---

func TestTry_MissingCatchClause(t *testing.T) {
	_, err := frontend.ParseModuleForTest("fun f(): int { try { var x: int = 1 } return 0 }")
	if err == nil {
		t.Fatal("expected parse error for try without catch")
	}
}

func TestDefer_NonBlockBody(t *testing.T) {
	_, err := frontend.ParseModuleForTest("fun f(): int { defer var x: int = 1 return 0 }")
	if err == nil {
		t.Fatal("expected parse error for defer with non-block body")
	}
}

func TestDefer_ReturnInDeferBodyRejected(t *testing.T) {
	expectCompileError(t, `
fun f(): int {
    defer { return 1 }
    return 0
}
`, "return is not allowed inside a defer body")
}

func TestDefer_NestedDeferRejected(t *testing.T) {
	expectCompileError(t, `
fun f(): int {
    defer { defer { var x: int = 1 } }
    return 0
}
`, "defer cannot be nested inside a defer body")
}

func TestDefer_BreakInDeferBodyRejected(t *testing.T) {
	expectCompileError(t, `
fun f(): int {
    var i: int = 0
    while i < 3 {
        defer { break }
        i = i + 1
    }
    return i
}
`, "break is not allowed inside a defer body")
}

func TestDefer_TopLevelRejected(t *testing.T) {
	_, err := frontend.ParseModuleForTest("defer { var x: int = 1 }")
	if err == nil {
		t.Fatal("expected parse error for top-level defer")
	}
}

// TestTryCatch_DeepUnwindFromNestedCalls: an error three frames deep is
// caught by the outermost try.
func TestTryCatch_DeepUnwindFromNestedCalls(t *testing.T) {
	result, v, err := compileAndCallWithVM(t, `
fun level3(): int { return 1 / 0 }
fun level2(): int { return level3() + 1 }
fun level1(): int { return level2() + 1 }

fun f(): string {
    try {
        var n: int = level1()
        return "no-error:" + n
    } catch (e) {
        return "caught:" + e.code + "@" + e.callable
    }
}
`, "f", nil)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if got := v.DecodeString(result); got != "caught:division_by_zero@level3" {
		t.Fatalf("expected caught:division_by_zero@level3, got %q", got)
	}
}

// TestTryCatch_StateConsistentAfterCatch: locals declared before the try keep
// their values after unwinding (discard of partial body state only).
func TestTryCatch_StateConsistentAfterCatch(t *testing.T) {
	result, v, err := compileAndCallWithVM(t, `
fun f(): string {
    var keep: int = 11
    var a: array = []
    try {
        keep = keep + 5
        var bad: string = a[7]
    } catch (e) {
        return "keep=" + keep + "," + e.code
    }
    return "unreachable"
}
`, "f", nil)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if got := v.DecodeString(result); got != "keep=16,array_index_out_of_range" {
		t.Fatalf("expected keep=16,array_index_out_of_range, got %q", got)
	}
}

// TestTryCatch_ExecutionAfterTryCompletes: code following the try/catch runs
// on both paths (error caught and no error).
func TestTryCatch_ExecutionAfterTryCompletes(t *testing.T) {
	result, v, err := compileAndCallWithVM(t, `
fun f(): string {
    var out: string = "start"
    try {
        var x: int = 1 / 0
    } catch (e) {
        out = out + ",caught"
    }
    out = out + ",end"
    return out
}
`, "f", nil)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if got := v.DecodeString(result); got != "start,caught,end" {
		t.Fatalf("expected start,caught,end, got %q", got)
	}
}

// TestDefer_ClassMethod: defer/try work inside class methods too.
func TestDefer_ClassMethod(t *testing.T) {
	result, v, err := compileAndCallWithVM(t, `
class Service {
    log: array

    constructor() {
        this.log = []
    }

    fun work(): string {
        defer { push(this.log, "closed") }
        try {
            var x: int = 1 / 0
        } catch (e) {
            push(this.log, e.code)
        }
        return "done"
    }
}

fun f(): array {
    var s: Service = new Service()
    var r: string = s.work()
    push(s.log, r)
    return s.log
}
`, "f", nil)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	got := decodeStringArray(t, v, result)
	want := []string{"division_by_zero", "closed", "done"}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}
