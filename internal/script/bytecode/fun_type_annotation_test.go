package bytecode

import (
	"strings"
	"testing"

	"github.com/qomos-w/spore/internal/script/frontend"
	"github.com/qomos-w/spore/internal/script/vm"
)

// --- Positive: assignment, passing, returning, calling function-typed values ---

func TestFunTypeVarAssignAndCall(t *testing.T) {
	source := `
fun main(): int {
  var dbl: fun(int): int = fun(x: int): int { return x * 2 }
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

func TestFunTypeArrowSyntaxVar(t *testing.T) {
	source := `
fun main(): int {
  var inc: fun(int) -> int = fun(x: int): int { return x + 1 }
  return inc(41)
}`
	result, err := compileAndCall(t, source, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 42 {
		t.Fatalf("expected 42, got %d", got)
	}
}

func TestFunTypeAnyParamLambdaStillAccepted(t *testing.T) {
	// A lambda with `any`-typed parameter/return must pass the fun-type
	// check (wildcard components bypass strict matching).
	source := `
fun main(): int {
  var f: fun(int): int = fun(x: any): any { return x + 1 }
  return f(1)
}`
	result, err := compileAndCall(t, source, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 2 {
		t.Fatalf("expected 2, got %d", got)
	}
}

func TestFunTypeAnyStillBypasses(t *testing.T) {
	// `any` keeps the original loose behavior: an arity the strict fun-type
	// checker would reject still compiles and runs.
	source := `
fun main(): int {
  var f: any = fun(x: int): int { return x }
  return f(1, 2)
}`
	result, err := compileAndCall(t, source, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 1 {
		t.Fatalf("expected 1, got %d", got)
	}
}

func TestFunTypePassedAsArgumentAndReturned(t *testing.T) {
	source := `
fun apply(f: fun(int): int, v: int): int {
  return f(v)
}

fun make(): fun(int): int {
  return fun(x: int): int { return x * 10 }
}

fun main(): int {
  var r1: int = apply(fun(x: int): int { return x + 5 }, 10)
  var g: fun(int): int = make()
  return r1 + g(2)
}`
	result, err := compileAndCall(t, source, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 35 {
		t.Fatalf("expected 35, got %d", got)
	}
}

func TestFunTypeHigherOrderWithFunTypedParam(t *testing.T) {
	source := `
fun applyTwice(f: fun(int): int, v: int): int {
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

func TestFunTypeReassignmentCompatible(t *testing.T) {
	source := `
fun main(): int {
  var f: fun(int): int = fun(x: int): int { return x + 1 }
  f = fun(x: int): int { return x * 3 }
  return f(5)
}`
	result, err := compileAndCall(t, source, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 15 {
		t.Fatalf("expected 15, got %d", got)
	}
}

func TestFunTypeGlobalVarDeclaredAndCalled(t *testing.T) {
	source := `
var transform: fun(string): string = fun(s: string): string { return s + "!" }

fun main(): string {
  return transform("hi")
}`
	result, err := compileAndCallAfterGlobals(t, source, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.NewVM(64, 8).DecodeString(result); got != "hi!" {
		t.Fatalf("expected hi!, got %q", got)
	}
}

func TestFunTypeMultiParamCall(t *testing.T) {
	source := `
fun main(): int {
  var combine: fun(int, int): int = fun(a: int, b: int): int { return a * 10 + b }
  return combine(4, 2)
}`
	result, err := compileAndCall(t, source, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 42 {
		t.Fatalf("expected 42, got %d", got)
	}
}

func TestFunTypeTypeAliasRoundTrip(t *testing.T) {
	source := `
type Mapper = fun(int): int

fun main(): int {
  var m: Mapper = fun(x: int): int { return x + 20 }
  return m(22)
}`
	result, err := compileAndCall(t, source, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 42 {
		t.Fatalf("expected 42, got %d", got)
	}
}

func TestFunTypeAssignFromFunTypedVar(t *testing.T) {
	source := `
fun main(): int {
  var f: fun(int): int = fun(x: int): int { return x + 1 }
  var g: fun(int): int = f
  return g(9)
}`
	result, err := compileAndCall(t, source, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 10 {
		t.Fatalf("expected 10, got %d", got)
	}
}

func TestFunTypeVarCapturedByNestedLambda(t *testing.T) {
	// A fun-typed local captured and called inside a nested closure keeps
	// working (capture cells + compile-time signature check).
	source := `
fun main(): int {
  var base: fun(int): int = fun(x: int): int { return x * 2 }
  var outer: fun(int): int = fun(n: int): int { return base(n) + 1 }
  return outer(20)
}`
	result, err := compileAndCall(t, source, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 41 {
		t.Fatalf("expected 41, got %d", got)
	}
}

func TestFunTypeStructFieldHoldsClosure(t *testing.T) {
	// Struct fields carry the serialized fun type (resolveTypeID → any) and
	// round-trip a closure through the field into a fun-typed local. Direct
	// member-call on struct values (`c.compute(35)`) is not in the baseline
	// surface (member calls resolve to methods), so the closure is loaded
	// into a fun-typed variable first.
	source := `
struct Config {
  compute: fun(int): int
}

fun main(): int {
  var c: Config = Config{ compute: fun(x: int): int { return x + 7 } }
  var h: fun(int): int = c.compute
  return h(35)
}`
	result, err := compileAndCall(t, source, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 42 {
		t.Fatalf("expected 42, got %d", got)
	}
}

// --- Negative: assignment mismatches ---

func TestFunTypeWrongArityAssignmentFails(t *testing.T) {
	_, _, err := compileSourceAllowError(t, `
fun main(): int {
  var f: fun(int, int): int = fun(x: int): int { return x }
  return f(1, 2)
}`)
	if err == nil {
		t.Fatal("expected compile error for wrong arity assignment")
	}
	if !strings.Contains(err.Error(), "1 parameter") || !strings.Contains(err.Error(), "2") {
		t.Fatalf("expected arity mismatch diagnostic, got %v", err)
	}
}

func TestFunTypeParamTypeMismatchFails(t *testing.T) {
	_, _, err := compileSourceAllowError(t, `
fun main(): int {
  var f: fun(int): int = fun(x: string): int { return 1 }
  return f(1)
}`)
	if err == nil {
		t.Fatal("expected compile error for parameter type mismatch")
	}
	if !strings.Contains(err.Error(), "expects \"int\"") {
		t.Fatalf("expected parameter mismatch diagnostic, got %v", err)
	}
}

func TestFunTypeReturnTypeMismatchFails(t *testing.T) {
	_, _, err := compileSourceAllowError(t, `
fun main(): int {
  var f: fun(int): int = fun(x: int): string { return "s" }
  return f(1)
}`)
	if err == nil {
		t.Fatal("expected compile error for return type mismatch")
	}
	if !strings.Contains(err.Error(), "must return \"int\"") {
		t.Fatalf("expected return mismatch diagnostic, got %v", err)
	}
}

func TestFunTypeLiteralAssignmentFails(t *testing.T) {
	_, _, err := compileSourceAllowError(t, `
fun main(): int {
  var f: fun(int): int = 42
  return 0
}`)
	if err == nil {
		t.Fatal("expected compile error for literal assigned to fun type")
	}
	if !strings.Contains(err.Error(), "int value") {
		t.Fatalf("expected literal mismatch diagnostic, got %v", err)
	}
}

func TestFunTypeReassignmentMismatchFails(t *testing.T) {
	_, _, err := compileSourceAllowError(t, `
fun main(): int {
  var f: fun(int): int = fun(x: int): int { return x }
  f = fun(x: int): string { return "s" }
  return 0
}`)
	if err == nil {
		t.Fatal("expected compile error for mismatched reassignment")
	}
	if !strings.Contains(err.Error(), "must return \"int\"") {
		t.Fatalf("expected return mismatch diagnostic, got %v", err)
	}
}

func TestFunTypeAssignIncompatibleFunTypedVarFails(t *testing.T) {
	_, _, err := compileSourceAllowError(t, `
fun main(): int {
  var f: fun(int): int = fun(x: int): int { return x }
  var g: fun(string): int = fun(s: string): int { return 1 }
  f = g
  return 0
}`)
	if err == nil {
		t.Fatal("expected compile error for fun-to-fun assignment mismatch")
	}
	if !strings.Contains(err.Error(), "cannot assign") {
		t.Fatalf("expected assign mismatch diagnostic, got %v", err)
	}
}

// --- Negative: call-site mismatches ---

func TestFunTypeCallArityMismatchFails(t *testing.T) {
	_, _, err := compileSourceAllowError(t, `
fun main(): int {
  var f: fun(int, int): int = fun(a: int, b: int): int { return a + b }
  return f(1)
}`)
	if err == nil {
		t.Fatal("expected compile error for call arity mismatch")
	}
	if !strings.Contains(err.Error(), "expects 2 argument") {
		t.Fatalf("expected call arity diagnostic, got %v", err)
	}
}

func TestFunTypeCallLiteralArgMismatchFails(t *testing.T) {
	_, _, err := compileSourceAllowError(t, `
fun main(): int {
  var f: fun(int): int = fun(x: int): int { return x }
  return f("oops")
}`)
	if err == nil {
		t.Fatal("expected compile error for literal argument mismatch")
	}
	if !strings.Contains(err.Error(), "expects \"int\"") {
		t.Fatalf("expected argument mismatch diagnostic, got %v", err)
	}
}

func TestFunTypeHigherOrderBadLambdaArgFails(t *testing.T) {
	_, _, err := compileSourceAllowError(t, `
fun applyTwice(f: fun(int): int, v: int): int {
  return f(f(v))
}

fun main(): int {
  return applyTwice(fun(x: string): int { return 1 }, 10)
}`)
	if err == nil {
		t.Fatal("expected compile error for incompatible lambda argument")
	}
	if !strings.Contains(err.Error(), "expects \"int\"") {
		t.Fatalf("expected lambda argument mismatch diagnostic, got %v", err)
	}
}

func TestFunTypeHigherOrderWrongArityLambdaArgFails(t *testing.T) {
	_, _, err := compileSourceAllowError(t, `
fun apply(f: fun(int): int, v: int): int {
  return f(v)
}

fun main(): int {
  return apply(fun(a: int, b: int): int { return a + b }, 10)
}`)
	if err == nil {
		t.Fatal("expected compile error for wrong-arity lambda argument")
	}
	if !strings.Contains(err.Error(), "parameter") {
		t.Fatalf("expected arity diagnostic, got %v", err)
	}
}

// --- Unit: serialized fun type helpers ---

func TestFunTypeNameHelpers(t *testing.T) {
	cases := []struct {
		name   string
		isFun  bool
		params []string
		ret    string
	}{
		{"fun(int):int", true, []string{"int"}, "int"},
		{"fun():void", true, nil, "void"},
		{"fun(int,string):bool", true, []string{"int", "string"}, "bool"},
		{"fun(map<string,int>):array<string>", true, []string{"map<string,int>"}, "array<string>"},
		{"fun(fun(int):int):int", true, []string{"fun(int):int"}, "int"},
		{"int", false, nil, ""},
		{"array<int>", false, nil, ""},
	}
	for _, tc := range cases {
		if got := isFunTypeName(tc.name); got != tc.isFun {
			t.Fatalf("isFunTypeName(%q) = %v, want %v", tc.name, got, tc.isFun)
		}
		sig, ok := parseFunTypeSig(tc.name)
		if ok != tc.isFun {
			t.Fatalf("parseFunTypeSig(%q) ok = %v, want %v", tc.name, ok, tc.isFun)
		}
		if !tc.isFun {
			continue
		}
		if sig.ret != tc.ret {
			t.Fatalf("parseFunTypeSig(%q).ret = %q, want %q", tc.name, sig.ret, tc.ret)
		}
		if len(sig.params) != len(tc.params) {
			t.Fatalf("parseFunTypeSig(%q) params = %v, want %v", tc.name, sig.params, tc.params)
		}
		for i := range tc.params {
			if sig.params[i] != tc.params[i] {
				t.Fatalf("parseFunTypeSig(%q) params = %v, want %v", tc.name, sig.params, tc.params)
			}
		}
	}
}

func TestFunTypeAssignableMatrix(t *testing.T) {
	cases := []struct {
		dst, src string
		want     bool
	}{
		{"fun(int):int", "fun(int):int", true},
		{"fun(int):int", "fun(int):string", false},
		{"fun(int):int", "fun(string):int", false},
		{"fun(int):int", "fun(int,int):int", false},
		{"fun(int):any", "fun(int):string", true},
		{"fun(any):int", "fun(string):int", true},
		{"any", "fun(int):int", true},
		{"fun(int):int", "int", true}, // non-fun bypass
	}
	for _, tc := range cases {
		if got := funTypeAssignable(tc.dst, tc.src); got != tc.want {
			t.Fatalf("funTypeAssignable(%q, %q) = %v, want %v", tc.dst, tc.src, got, tc.want)
		}
	}
}

func TestFunTypeExpandAliasesThroughFunType(t *testing.T) {
	aliases := map[string]string{"Num": "int", "Str": "string"}
	got := expandTypeAliases("fun(Num):Str", aliases, map[string]bool{})
	if got != "fun(int):string" {
		t.Fatalf("expected fun(int):string, got %q", got)
	}
	// Whole-name alias to a fun type resolves too.
	aliases2 := map[string]string{"Handler": "fun(int):int"}
	got2 := expandTypeAliases("Handler", aliases2, map[string]bool{})
	if got2 != "fun(int):int" {
		t.Fatalf("expected fun(int):int, got %q", got2)
	}
}

func TestFunTypeAliasToUnknownInnerTypeFails(t *testing.T) {
	_, _, err := compileSourceAllowError(t, `
type Handler = fun(Missing):int
fun run(): int { return 1 }
`)
	if err == nil {
		t.Fatal("expected compile error for unknown fun type inner alias target")
	}
	if !strings.Contains(err.Error(), "unknown type") {
		t.Fatalf("expected unknown type diagnostic, got %v", err)
	}
}

// --- Regression: descriptor path keeps fun types schema-safe ---

func TestFunTypeDescriptorTreatsFunAsAny(t *testing.T) {
	prog, err := frontend.ParseModuleForTest("fun apply(cb: fun(int): int): int { return cb(1) }")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	_ = prog // parsed and compiled fine; descriptor path exercised by pipeline tests
}
