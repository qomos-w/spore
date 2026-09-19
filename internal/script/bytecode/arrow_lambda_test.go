package bytecode

import (
	"strings"
	"testing"

	"github.com/qomos-w/spore/internal/script/vm"
)

// --- Arrow lambda short form: execution ---

func TestArrowLambdaExecution(t *testing.T) {
	source := `
fun main(): int {
  var dbl: any = (x): int => x * 2
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

func TestArrowLambdaSingleParamNoParens(t *testing.T) {
	source := `
fun main(): int {
  var inc: any = x => x + 1
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

func TestArrowLambdaUntypedParams(t *testing.T) {
	source := `
fun main(): int {
  var add: any = (a, b) => a + b
  return add(19, 23)
}`

	result, err := compileAndCall(t, source, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 42 {
		t.Fatalf("expected 42, got %d", got)
	}
}

func TestArrowLambdaZeroParams(t *testing.T) {
	source := `
fun main(): int {
  var mk: any = (): int => 42
  return mk()
}`

	result, err := compileAndCall(t, source, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 42 {
		t.Fatalf("expected 42, got %d", got)
	}
}

func TestArrowLambdaNestedCurrying(t *testing.T) {
	source := `
fun main(): int {
  var add: any = (a): int => (b): int => a + b
  return add(20)(22)
}`

	result, err := compileAndCall(t, source, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 42 {
		t.Fatalf("expected 42, got %d", got)
	}
}

func TestArrowLambdaCapturesEnclosingVar(t *testing.T) {
	source := `
fun main(): int {
  var base: int = 40
  var add: any = (x): int => base + x
  return add(2)
}`

	result, err := compileAndCall(t, source, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 42 {
		t.Fatalf("expected 42, got %d", got)
	}
}

func TestArrowLambdaAsHigherOrderArgument(t *testing.T) {
	source := `
fun apply(f: fun(int): int, v: int): int { return f(v) }

fun main(): int {
  return apply(x => x + 1, 41)
}`

	result, err := compileAndCall(t, source, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 42 {
		t.Fatalf("expected 42, got %d", got)
	}
}

func TestArrowLambdaTypedAsHigherOrderArgument(t *testing.T) {
	source := `
fun apply(f: fun(int): int, v: int): int { return f(v) }

fun main(): int {
  return apply((x): int => x * 3, 14)
}`

	result, err := compileAndCall(t, source, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 42 {
		t.Fatalf("expected 42, got %d", got)
	}
}

func TestArrowLambdaWithFunTypeSlot(t *testing.T) {
	source := `
fun main(): int {
  var f: fun(int): int = (x): int => x + 1
  return f(41)
}`

	result, err := compileAndCall(t, source, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 42 {
		t.Fatalf("expected 42, got %d", got)
	}
}

func TestArrowLambdaStringBody(t *testing.T) {
	source := `
fun main(): string {
  var exclaim: any = (s): string => s + "!"
  return exclaim("arrow")
}`

	result, v, err := compileAndCallWithVM(t, source, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := v.DecodeString(result); got != "arrow!" {
		t.Fatalf("expected \"arrow!\", got %q", got)
	}
}

func TestArrowLambdaGroupedExprStillExecutes(t *testing.T) {
	// The speculative arrow parse must not disturb plain grouped expressions.
	source := `
fun main(): int {
  var a: int = 1
  var b: int = 2
  return (a + b) * 3
}`

	result, err := compileAndCall(t, source, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 9 {
		t.Fatalf("expected 9, got %d", got)
	}
}

func TestArrowLambdaSyntaxDocExample(t *testing.T) {
	// Mirrors the SYNTAX.md §5.8.1 "Arrow lambda short form" example so the
	// documented surface stays executable.
	source := `
fun main(): int {
  var dbl: any = (x): int => x * 2
  var inc: any = x => x + 1
  var add: any = (a, b) => a + b
  var mk: any = (): int => 42
  return dbl(21) + inc(41) + add(1, 2) + mk()
}`

	result, err := compileAndCall(t, source, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 129 {
		t.Fatalf("expected 129, got %d", got)
	}
}

// --- Arrow lambda short form: compile-time checks ---

func TestArrowLambdaArityMismatchFails(t *testing.T) {
	_, _, err := compileSourceAllowError(t, `
fun main(): int {
  var f: fun(int, int): int = (a): int => a
  return f(1, 2)
}`)
	if err == nil {
		t.Fatal("expected compile error for arrow lambda arity mismatch")
	}
	if !strings.Contains(err.Error(), "expects 2") {
		t.Fatalf("expected arity mismatch diagnostic, got %v", err)
	}
}

func TestArrowLambdaTypedParamMismatchFails(t *testing.T) {
	_, _, err := compileSourceAllowError(t, `
fun main(): int {
  var f: fun(int): int = (x: string): int => 1
  return f(1)
}`)
	if err == nil {
		t.Fatal("expected compile error for arrow lambda parameter type mismatch")
	}
	if !strings.Contains(err.Error(), "expects \"int\"") {
		t.Fatalf("expected parameter mismatch diagnostic, got %v", err)
	}
}

func TestArrowLambdaReturnTypeMismatchFails(t *testing.T) {
	_, _, err := compileSourceAllowError(t, `
fun main(): int {
  var f: fun(int): int = (x: int): string => "s"
  return f(1)
}`)
	if err == nil {
		t.Fatal("expected compile error for arrow lambda return type mismatch")
	}
	if !strings.Contains(err.Error(), "must return \"int\"") {
		t.Fatalf("expected return mismatch diagnostic, got %v", err)
	}
}
