package bytecode

import (
	"strings"
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/internal/script/frontend"
	"github.com/qomos-w/spore/internal/script/vm"
)

// compileRunAndCallAfterGlobals parses and compiles the source, executes the
// top-level chunk (so global initializers run), calls fnName, and returns
// the result together with the VM instance for value inspection.
func compileRunAndCallAfterGlobals(t *testing.T, source, fnName string) (vm.Value, *vm.VM) {
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
	result, err := interp.ExecuteFunction(fnChunk, binding.InvocationStageUnary, nil)
	if err != nil {
		t.Fatalf("execute %q error: %v", fnName, err)
	}
	return result, v
}

// --- Null coalescing: `x ?? y` ---

func TestNullCoalesce_LeftNotNull(t *testing.T) {
	source := `
fun main(): any {
  var x: any = 41
  return x ?? 42
}`
	result, err := compileAndCall(t, source, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !vm.IsInt(result) || vm.DecodeInt(result) != 41 {
		t.Fatalf("expected 41 (left retained), got %v", result)
	}
}

func TestNullCoalesce_LeftNull(t *testing.T) {
	source := `
fun main(): any {
  var x: any = null
  return x ?? 42
}`
	result, err := compileAndCall(t, source, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !vm.IsInt(result) || vm.DecodeInt(result) != 42 {
		t.Fatalf("expected 42 (right taken), got %v", result)
	}
}

func TestNullCoalesce_FalseIsNotNull(t *testing.T) {
	// Only null triggers the right side — false, 0, "" are legitimate values.
	source := `
fun main(): any {
  var f: any = false
  return f ?? true
}`
	result, err := compileAndCall(t, source, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !vm.IsBool(result) || vm.DecodeBool(result) {
		t.Fatalf("expected false to be retained, got %v", result)
	}
}

func TestNullCoalesce_ZeroAndEmptyStringRetained(t *testing.T) {
	source := `
fun main(): any {
  var z: any = 0
  var s: any = "" ?? "dflt"
  return z ?? 9
}`
	result, err := compileAndCall(t, source, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !vm.IsInt(result) || vm.DecodeInt(result) != 0 {
		t.Fatalf("expected 0 retained, got %v", result)
	}
}

func TestNullCoalesce_Chained(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   int32
	}{
		{"all null", "var a: any = null\n  var b: any = null\n  return a ?? b ?? 3", 3},
		{"first wins", "var a: any = 1\n  var b: any = 2\n  return a ?? b ?? 3", 1},
		{"middle", "var a: any = null\n  var b: any = 2\n  return a ?? b ?? 3", 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source := "fun main(): any {\n  " + tc.source + "\n}"
			result, err := compileAndCall(t, source, "main", nil)
			if err != nil {
				t.Fatal(err)
			}
			if !vm.IsInt(result) || vm.DecodeInt(result) != tc.want {
				t.Fatalf("expected %d, got %v", tc.want, result)
			}
		})
	}
}

func TestNullCoalesce_LowerPrecedenceThanOr(t *testing.T) {
	// `false ?? true || false` must parse as `false ?? (true || false)`,
	// yielding false. If ?? bound tighter than || it would yield true.
	source := `
fun main(): any {
  var f: any = false
  return f ?? true || false
}`
	result, err := compileAndCall(t, source, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !vm.IsBool(result) || vm.DecodeBool(result) {
		t.Fatalf("expected false (?? lower precedence than ||), got %v", result)
	}
}

func TestNullCoalesce_OrOnRightSide(t *testing.T) {
	// `null ?? (false || true)` — the right side of ?? is a full || expression.
	source := `
fun main(): any {
  var n: any = null
  return n ?? (false || true)
}`
	result, err := compileAndCall(t, source, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !vm.IsBool(result) || !vm.DecodeBool(result) {
		t.Fatalf("expected true, got %v", result)
	}
}

func TestNullCoalesce_HigherPrecedenceThanAssign(t *testing.T) {
	// `x = a ?? b` — ?? binds tighter than assignment, so the coalesced
	// value is stored, not a partial expression.
	source := `
fun main(): any {
  var x: any
  var a: any = null
  x = a ?? 42
  return x
}`
	result, err := compileAndCall(t, source, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !vm.IsInt(result) || vm.DecodeInt(result) != 42 {
		t.Fatalf("expected 42 assigned, got %v", result)
	}
}

func TestNullCoalesce_RightSideSkippedWhenLeftNotNull(t *testing.T) {
	source := `
var hits: int = 0
fun bump(): int {
  hits = hits + 1
  return 9
}
fun main(): int {
  var x: any = 7
  var r: any = x ?? bump()
  return hits
}`
	result, err := compileAndCallAfterGlobals(t, source, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 0 {
		t.Fatalf("expected right side skipped (hits 0), got %d", got)
	}
}

func TestNullCoalesce_RightSideEvaluatedWhenLeftNull(t *testing.T) {
	source := `
var hits: int = 0
fun bump(): int {
  hits = hits + 1
  return 9
}
fun main(): int {
  var x: any = null
  var r: any = x ?? bump()
  return hits
}`
	result, err := compileAndCallAfterGlobals(t, source, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 1 {
		t.Fatalf("expected right side evaluated (hits 1), got %d", got)
	}
}

// --- Optional member access: `x?.field` ---

func TestOptionalMember_NullShortCircuits(t *testing.T) {
	source := `
struct Point { x: int }
fun main(): any {
  var p: any = null
  var r: any = p?.x
  return r
}`
	result, err := compileAndCall(t, source, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !vm.IsNull(result) {
		t.Fatalf("expected null from short-circuit, got %v", result)
	}
}

func TestOptionalMember_NonNullReadsField(t *testing.T) {
	source := `
struct Point { x: int }
fun main(): any {
  var p: any = Point{ x: 7 }
  var r: any = p?.x
  return r
}`
	result, err := compileAndCall(t, source, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !vm.IsInt(result) || vm.DecodeInt(result) != 7 {
		t.Fatalf("expected 7, got %v", result)
	}
}

func TestOptionalMember_TypedReceiver(t *testing.T) {
	source := `
class Dog {
  name: string
  constructor(n: string) { this.name = n }
}
fun main(): string {
  var d: Dog = new Dog("Rex")
  return d?.name
}`
	result, v, err := compileAndCallWithVM(t, source, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := v.DecodeString(result); got != "Rex" {
		t.Fatalf("expected Rex, got %q", got)
	}
}

// --- Nested chains: `a?.b?.c` ---

func TestOptionalChain_NestedAllLinks(t *testing.T) {
	source := `
class Node {
  v: int
  next: any
  constructor(val: int, nxt: any) {
    this.v = val
    this.next = nxt
  }
}
fun chainRootNull(): any {
  var a: any = null
  return a?.next?.v
}
fun chainMidNull(): any {
  var tail: any = null
  var a: any = new Node(1, tail)
  return a?.next?.v
}
fun chainAllSet(): any {
  var b: any = new Node(2, null)
  var a: any = new Node(1, b)
  return a?.next?.v
}`
	rootNull, err := compileAndCall(t, source, "chainRootNull", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !vm.IsNull(rootNull) {
		t.Fatalf("expected null for root-null chain, got %v", rootNull)
	}
	midNull, err := compileAndCall(t, source, "chainMidNull", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !vm.IsNull(midNull) {
		t.Fatalf("expected null for mid-null chain, got %v", midNull)
	}
	allSet, err := compileAndCall(t, source, "chainAllSet", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !vm.IsInt(allSet) || vm.DecodeInt(allSet) != 2 {
		t.Fatalf("expected 2 through full chain, got %v", allSet)
	}
}

func TestOptionalChain_PlainLinkAfterOptional(t *testing.T) {
	// `a?.b.c`: a null receiver must skip the plain `.c` link too.
	source := `
class Inner { c: int }
class Outer { b: any }
fun nullRoot(): any {
  var a: any = null
  return a?.b.c
}
fun setRoot(): any {
  var inner: any = new Inner()
  inner.c = 9
  var a: any = new Outer()
  a.b = inner
  return a?.b.c
}`
	rootNull, err := compileAndCall(t, source, "nullRoot", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !vm.IsNull(rootNull) {
		t.Fatalf("expected null, got %v", rootNull)
	}
	set, err := compileAndCall(t, source, "setRoot", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !vm.IsInt(set) || vm.DecodeInt(set) != 9 {
		t.Fatalf("expected 9 through optional then plain link, got %v", set)
	}
}

// --- Optional method call: `x?.method(args)` ---

func TestOptionalMethodCall_NullSkipsCall(t *testing.T) {
	source := `
class Counter {
  fun hit(): int { return 5 }
}
fun main(): any {
  var c: any = null
  var r: any = c?.hit()
  return r
}`
	result, err := compileAndCall(t, source, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !vm.IsNull(result) {
		t.Fatalf("expected null from short-circuited call, got %v", result)
	}
}

func TestOptionalMethodCall_NonNullInvokes(t *testing.T) {
	source := `
class Counter {
  fun hit(): int { return 5 }
}
fun main(): any {
  var c: any = new Counter()
  var r: any = c?.hit()
  return r
}`
	result, err := compileAndCall(t, source, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !vm.IsInt(result) || vm.DecodeInt(result) != 5 {
		t.Fatalf("expected 5, got %v", result)
	}
}

func TestOptionalMethodCall_NullSkipsArgumentEvaluation(t *testing.T) {
	// When the receiver is null, argument expressions must not run either.
	source := `
var hits: int = 0
fun sideEffect(): int {
  hits = hits + 1
  return 100
}
class Counter {
  fun hit(bonus: int): int { return 5 + bonus }
}
fun main(): int {
  var c: any = null
  var r: any = c?.hit(sideEffect())
  return hits
}`
	result, err := compileAndCallAfterGlobals(t, source, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 0 {
		t.Fatalf("expected arguments skipped (hits 0), got %d", got)
	}
}

func TestOptionalMethodCall_NonNullEvaluatesArguments(t *testing.T) {
	source := `
var hits: int = 0
fun sideEffect(): int {
  hits = hits + 1
  return 100
}
class Counter {
  fun hit(bonus: int): int { return 5 + bonus }
}
fun main(): any {
  var c: any = new Counter()
  var r: any = c?.hit(sideEffect())
  return [r, hits]
}`
	result, v := compileRunAndCallAfterGlobals(t, source, "main")
	arr := vm.DecodeHandle(result)
	if !vm.IsHandle(result) || !v.IsArray(arr) {
		t.Fatalf("expected array result, got %v", result)
	}
	r := v.GetArrayElement(arr, 0)
	hits := v.GetArrayElement(arr, 1)
	if !vm.IsInt(r) || vm.DecodeInt(r) != 105 {
		t.Fatalf("expected method result 105, got %v", r)
	}
	if !vm.IsInt(hits) || vm.DecodeInt(hits) != 1 {
		t.Fatalf("expected side effect to run once (hits 1), got %v", hits)
	}
}

// --- `?.` combined with `??` ---

func TestOptionalChainCombinedWithNullCoalesce(t *testing.T) {
	cases := []struct {
		name   string
		source string
		check  func(t *testing.T, v *vm.VM, got vm.Value)
	}{
		{
			name:   "null chain falls to default",
			source: "fun main(): string {\n  var p: any = null\n  return p?.name ?? \"anon\"\n}",
			check: func(t *testing.T, v *vm.VM, got vm.Value) {
				if s := v.DecodeString(got); s != "anon" {
					t.Fatalf("expected \"anon\", got %q", s)
				}
			},
		},
		{
			name:   "non-null chain wins over default",
			source: "struct P { name: string }\nfun main(): string {\n  var p: any = P{ name: \"Ada\" }\n  return p?.name ?? \"anon\"\n}",
			check: func(t *testing.T, v *vm.VM, got vm.Value) {
				if s := v.DecodeString(got); s != "Ada" {
					t.Fatalf("expected \"Ada\", got %q", s)
				}
			},
		},
		{
			name:   "chain of ?? after nested optional chain",
			source: "class N { v: any }\nfun main(): any {\n  var a: any = null\n  return a?.v ?? null ?? 3\n}",
			check: func(t *testing.T, v *vm.VM, got vm.Value) {
				if !vm.IsInt(got) || vm.DecodeInt(got) != 3 {
					t.Fatalf("expected 3, got %v", got)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, v, err := compileAndCallWithVM(t, tc.source, "main", nil)
			if err != nil {
				t.Fatal(err)
			}
			tc.check(t, v, got)
		})
	}
}

func TestOptionalChain_AsArgumentValue(t *testing.T) {
	// The chain result flows into surrounding expressions correctly.
	source := `
struct P { x: int }
fun add(a: int, b: int): int { return a + b }
fun main(): any {
  var p: any = null
  return add(p?.x ?? 10, 32)
}`
	result, err := compileAndCall(t, source, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 42 {
		t.Fatalf("expected 42, got %d", got)
	}
}

// --- Lambda captures through the new expression forms ---

func TestNullCoalesce_CapturedLocalInsideLambda(t *testing.T) {
	// The lambda's `a ?? fallback` must see `fallback` as captured so the
	// closure reads the enclosing local, not a fresh slot.
	source := `
fun main(): any {
  var fallback: any = 40
  var pick: any = (a: any): any => a ?? fallback
  var n: any = null
  return pick(n) + pick(2)
}`
	result, err := compileAndCall(t, source, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 42 {
		t.Fatalf("expected 42, got %d", got)
	}
}

func TestOptionalChain_CapturedReceiverInsideLambda(t *testing.T) {
	source := `
struct P { tag: int }
fun main(): any {
  var o: any = null
  var get: any = (): any => o?.tag
  var a: any = get()
  o = P{ tag: 7 }
  var b: any = get()
  return [a, b]
}`
	result, v := compileRunAndCallAfterGlobals(t, source, "main")
	arr := vm.DecodeHandle(result)
	if !vm.IsHandle(result) || !v.IsArray(arr) {
		t.Fatalf("expected array result, got %v", result)
	}
	a := v.GetArrayElement(arr, 0)
	b := v.GetArrayElement(arr, 1)
	if !vm.IsNull(a) {
		t.Fatalf("expected null before receiver set, got %v", a)
	}
	if !vm.IsInt(b) || vm.DecodeInt(b) != 7 {
		t.Fatalf("expected 7 after receiver set, got %v", b)
	}
}

// --- Optional link inside longer chains ---

func TestOptionalChain_FieldOffOptionalCallResult(t *testing.T) {
	// `o?.mk().tag`: the inner call uses the member-callee path (object and
	// null check compile before arguments), so a null receiver skips both
	// the call and the outer field read.
	source := `
class Inner { tag: int }
class Outer {
  fun mk(): any { return new Inner() }
}
fun nullRecv(): any {
  var o: any = null
  var r: any = o?.mk().tag
  return r
}`
	nullRecv, err := compileAndCall(t, source, "nullRecv", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !vm.IsNull(nullRecv) {
		t.Fatalf("expected null for null receiver, got %v", nullRecv)
	}
	// Positive path sanity: same shape with a set tag.
	source2 := `
class Inner { tag: int }
class Outer {
  inner: any
  fun mk(): any { return this.inner }
}
fun main(): any {
  var inner: any = new Inner()
  inner.tag = 4
  var o: any = new Outer()
  o.inner = inner
  return o?.mk().tag
}`
	setRecv, err := compileAndCall(t, source2, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !vm.IsInt(setRecv) || vm.DecodeInt(setRecv) != 4 {
		t.Fatalf("expected 4 through optional call then field, got %v", setRecv)
	}
}

func TestOptionalChain_DirectCallOfChainResultRejected(t *testing.T) {
	// `x?.m(y)(z)` would strand the pushed argument `z` when the null
	// short-circuit fires; the compiler rejects the shape instead.
	source := `
class M { fun m(a: int): any { return null } }
fun main(): any {
  var x: any = null
  return x?.m(1)(2)
}`
	prog, err := frontend.ParseModuleForTest(source)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	c := newCompiler()
	_, err = c.compile(prog)
	if err == nil {
		t.Fatal("expected compile error for direct call of optional-chain result")
	}
	if !strings.Contains(err.Error(), "optional chain") {
		t.Fatalf("expected optional-chain error, got %v", err)
	}
}

// --- Rejected forms ---

func TestOptionalChaining_AssignmentTargetRejected(t *testing.T) {
	source := `
struct P { x: int }
fun main(): void {
  var p: any = null
  p?.x = 1
}`
	prog, err := frontend.ParseModuleForTest(source)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	c := newCompiler()
	_, err = c.compile(prog)
	if err == nil {
		t.Fatal("expected compile error for optional-chaining assignment target")
	}
	if !strings.Contains(err.Error(), "assignment target") {
		t.Fatalf("expected assignment-target error, got %v", err)
	}
}
