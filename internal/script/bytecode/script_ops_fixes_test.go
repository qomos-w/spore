package bytecode

import (
	"testing"

	"github.com/qomos-w/spore/internal/script/vm"
)

// These tests pin the script-ops fixes from the Octopus batch:
//   #1 typed array push (method form) + class container field zero-init
//   #2 catch error stringification (e as string)
//   #4 map get(m, k, default)
//   #5 as scalar conversion (int/float/bool <-> string) + honest diagnostics
//   #7 ternary conditional expression (cond ? a : b)
//
// Each uses the compileAndCall harness from interpreter_test.go to exercise
// the real parse→compile→execute path with typed declarations.

// --- #1 array push method form + container fields ---

func TestFix1_ArrayPushMethodFormWorks(t *testing.T) {
	src := `
class C {
	items: array<int>
	fun push_and_len(v: int): int {
		this.items.push(v)
		return len(this.items)
	}
}
fun main(): int {
	var c: C = new C()
	c.push_and_len(10)
	c.push_and_len(20)
	return len(c.items)
}
`
	result, err := compileAndCall(t, src, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 2 {
		t.Fatalf("expected 2, got %d", got)
	}
}

func TestFix1_MapFieldZeroInitNoPanic(t *testing.T) {
	src := `
class C {
	tags: map<string, int>
	fun main_in(): int {
		this.tags["k"] = 1
		return len(this.tags)
	}
}
fun main(): int {
	var c: C = new C()
	return c.main_in()
}
`
	result, err := compileAndCall(t, src, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 1 {
		t.Fatalf("expected 1, got %d", got)
	}
}

func TestFix1_UnknownArrayMethodStructuredError(t *testing.T) {
	src := `
fun main(): int {
	var xs: array<int> = [1]
	return xs.pop()
}
`
	_, err := compileAndCall(t, src, "main", nil)
	if err == nil {
		t.Fatal("expected error for unknown array method pop")
	}
}

// --- #2 catch error stringification ---

func TestFix2_CatchErrorAsStringable(t *testing.T) {
	src := `
fun main(): string {
	var out: string = "none"
	try {
		var m: map<string, int> = {"a": 1}
		var x: int = m["zzz"]
		out = "got"
	} catch (e) {
		out = e as string
	}
	return out
}
`
	result, v, err := compileAndCallWithVM(t, src, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	got := testDecodeString(t, v, result)
	if got == "none" || got == "got" {
		t.Fatalf("expected error message, got %q", got)
	}
}

func TestFix2_CatchErrorConcatenable(t *testing.T) {
	src := `
fun main(): string {
	var out: string = "none"
	try {
		var m: map<string, int> = {"a": 1}
		var x: int = m["zzz"]
		out = "got"
	} catch (e) {
		out = "err: " + (e as string)
	}
	return out
}
`
	result, v, err := compileAndCallWithVM(t, src, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	got := testDecodeString(t, v, result)
	if len(got) < 5 || got[:4] != "err:" {
		t.Fatalf("expected err: prefix, got %q", got)
	}
}

// --- #4 map get with default ---

func TestFix4_GetMissingKeyReturnsDefault(t *testing.T) {
	src := `
fun main(): int {
	var m: map<string, int> = {"a": 1}
	return get(m, "zzz", 99)
}
`
	result, err := compileAndCall(t, src, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 99 {
		t.Fatalf("expected default 99, got %d", got)
	}
}

func TestFix4_GetHitKeyReturnsValue(t *testing.T) {
	src := `
fun main(): int {
	var m: map<string, int> = {"a": 1}
	return get(m, "a", 99)
}
`
	result, err := compileAndCall(t, src, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 1 {
		t.Fatalf("expected value 1, got %d", got)
	}
}

// --- #5 as scalar conversion ---

func TestFix5_IntAsString(t *testing.T) {
	src := `fun main(): string { return 5 as string }`
	result, v, err := compileAndCallWithVM(t, src, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := testDecodeString(t, v, result); got != "5" {
		t.Fatalf("expected '5', got %q", got)
	}
}

func TestFix5_StringAsInt(t *testing.T) {
	src := `fun main(): int { return "42" as int }`
	result, err := compileAndCall(t, src, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 42 {
		t.Fatalf("expected 42, got %d", got)
	}
}

func TestFix5_StringAsIntParseFailureHonest(t *testing.T) {
	src := `fun main(): int { return "abc" as int }`
	_, err := compileAndCall(t, src, "main", nil)
	if err == nil {
		t.Fatal("expected type_cast_failed for unparseable int")
	}
}

func TestFix5_BoolAsString(t *testing.T) {
	src := `fun main(): string { return true as string }`
	result, v, err := compileAndCallWithVM(t, src, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := testDecodeString(t, v, result); got != "true" {
		t.Fatalf("expected 'true', got %q", got)
	}
}

func TestFix5_StringAsBool(t *testing.T) {
	src := `
fun main(): string {
	var b: bool = "true" as bool
	if (b) { return "yes" }
	return "no"
}
`
	result, v, err := compileAndCallWithVM(t, src, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := testDecodeString(t, v, result); got != "yes" {
		t.Fatalf("expected 'yes', got %q", got)
	}
}

func TestFix5_IsStillTypeCheck(t *testing.T) {
	src := `
fun main(): string {
	var m: map<string, int> = {"a": 1}
	if (m is map) { return "map" }
	return "other"
}
`
	result, v, err := compileAndCallWithVM(t, src, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := testDecodeString(t, v, result); got != "map" {
		t.Fatalf("expected 'map', got %q", got)
	}
}

// --- #7 ternary ---

func TestFix7_TernaryTrueBranch(t *testing.T) {
	src := `fun main(): string { return true ? "a" : "b" }`
	result, v, err := compileAndCallWithVM(t, src, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := testDecodeString(t, v, result); got != "a" {
		t.Fatalf("expected 'a', got %q", got)
	}
}

func TestFix7_TernaryFalseBranch(t *testing.T) {
	src := `fun main(): string { return false ? "a" : "b" }`
	result, v, err := compileAndCallWithVM(t, src, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := testDecodeString(t, v, result); got != "b" {
		t.Fatalf("expected 'b', got %q", got)
	}
}

func TestFix7_TernaryCondExpr(t *testing.T) {
	src := `
fun main(): string {
	var n: int = 7
	return n > 5 ? "big" : "small"
}
`
	result, v, err := compileAndCallWithVM(t, src, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := testDecodeString(t, v, result); got != "big" {
		t.Fatalf("expected 'big', got %q", got)
	}
}

func TestFix7_TernaryNestedRightAssoc(t *testing.T) {
	src := `
fun main(): string {
	var n: int = 42
	return n < 10 ? "low" : n < 50 ? "mid" : "high"
}
`
	result, v, err := compileAndCallWithVM(t, src, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := testDecodeString(t, v, result); got != "mid" {
		t.Fatalf("expected 'mid', got %q", got)
	}
}

func TestFix7_TernaryInAssignment(t *testing.T) {
	src := `
fun main(): string {
	var n: int = 3
	var s: string = n % 2 == 1 ? "odd" : "even"
	return s
}
`
	result, v, err := compileAndCallWithVM(t, src, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := testDecodeString(t, v, result); got != "odd" {
		t.Fatalf("expected 'odd', got %q", got)
	}
}

func TestFix7_TernaryExprBodyFun(t *testing.T) {
	src := `export fun classify(n: int): string = n >= 0 ? "nonneg" : "neg"`
	result, v, err := compileAndCallWithVM(t, src, "classify", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := testDecodeString(t, v, result); got != "nonneg" {
		t.Fatalf("expected 'nonneg', got %q", got)
	}
}

// testDecodeString unwraps a VM string value for test assertions.
func testDecodeString(t *testing.T, v *vm.VM, val vm.Value) string {
	t.Helper()
	if !v.IsStringValue(val) {
		t.Fatalf("expected string value, got %v", val)
	}
	return v.DecodeString(val)
}