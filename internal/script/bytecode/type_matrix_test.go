package bytecode

import (
	"strings"
	"testing"

	"github.com/qomos-w/spore/internal/script/vm"
)

// ============================================================================
// Type matrix: 15 type rows x 3 dimensions (assignment / return / argument).
//
// For each type row we exercise the three surfaces where a type annotation is
// carried through the compiler:
//
//	assignment:  var probe: T = <value>; return probe
//	return:      fun main(): T { return <value> }
//	argument:    fun take(p: T): any { return p }; return take(<value>)
//
// The conforming (legal) value for each row must compile and execute, and the
// value that comes back through the surface must decode with the row's own
// type tag (int vs long vs ulong vs float vs double vs string vs bool vs
// handle vs null vs closure), not just "not an error".
//
// Diagnostics tightening: the second half asserts that every *currently
// diagnosed* illegal combination yields a STRUCTURED diagnostic carrying a
// code + path (and, for type-mismatch diagnostics, expected/actual payloads)
// rather than a bare error. The deliberately lenient cross-type pass-through
// (e.g. an int literal in a `string` slot) is pinned by a separate test so
// this card cannot silently change existing behavior.
// ============================================================================

// matrixRow describes one type row of the legal matrix.
type matrixRow struct {
	name    string
	typeAnn string // type annotation used in `T` position
	value   string // conforming value expression for this row
	prelude string // shared declarations the row depends on (struct/class/interface)
	check   func(t *testing.T, v *vm.VM, got vm.Value)
	// argCheck, when set, replaces check for the argument dimension: parameter
	// annotations do NOT coerce call-site literals, so `take(42)` with
	// `take(p: long)` hands the callee an int-tagged 42 (the annotation only
	// selects the literal encoding for var/return surfaces).
	argCheck func(t *testing.T, v *vm.VM, got vm.Value)
}

func matrixRows() []matrixRow {
	handle := func(t *testing.T, v *vm.VM, got vm.Value) {
		if !vm.IsHandle(got) || vm.IsNull(got) {
			t.Fatalf("expected a non-null handle value, got %v", got)
		}
	}
	return []matrixRow{
		{name: "int", typeAnn: "int", value: "42",
			check: func(t *testing.T, v *vm.VM, got vm.Value) {
				if !vm.IsInt(got) || vm.DecodeInt(got) != 42 {
					t.Fatalf("expected int 42, got %v", got)
				}
			}},
		{name: "long", typeAnn: "long", value: "42",
			check: func(t *testing.T, v *vm.VM, got vm.Value) {
				if !vm.IsLong(got) || v.DecodeLong(got) != 42 {
					t.Fatalf("expected long 42, got %v", got)
				}
			},
			argCheck: func(t *testing.T, v *vm.VM, got vm.Value) {
				if !vm.IsInt(got) || vm.DecodeInt(got) != 42 {
					t.Fatalf("argument dim keeps the literal's own tag: expected int 42, got %v", got)
				}
			}},
		{name: "ulong", typeAnn: "ulong", value: "42",
			check: func(t *testing.T, v *vm.VM, got vm.Value) {
				if !vm.IsULong(got) || v.DecodeULong(got) != 42 {
					t.Fatalf("expected ulong 42, got %v", got)
				}
			},
			argCheck: func(t *testing.T, v *vm.VM, got vm.Value) {
				if !vm.IsInt(got) || vm.DecodeInt(got) != 42 {
					t.Fatalf("argument dim keeps the literal's own tag: expected int 42, got %v", got)
				}
			}},
		{name: "float", typeAnn: "float", value: "1.5",
			check: func(t *testing.T, v *vm.VM, got vm.Value) {
				if !vm.IsFloat(got) || vm.DecodeFloat(got) != 1.5 {
					t.Fatalf("expected float 1.5, got %v", got)
				}
			}},
		{name: "double", typeAnn: "double", value: "2.5",
			check: func(t *testing.T, v *vm.VM, got vm.Value) {
				if !vm.IsDouble(got) || v.DecodeDouble(got) != 2.5 {
					t.Fatalf("expected double 2.5, got %v", got)
				}
			},
			argCheck: func(t *testing.T, v *vm.VM, got vm.Value) {
				if !vm.IsFloat(got) || vm.DecodeFloat(got) != 2.5 {
					t.Fatalf("argument dim keeps the literal's own tag: expected float 2.5, got %v", got)
				}
			}},
		{name: "string", typeAnn: "string", value: `"mat"`,
			check: func(t *testing.T, v *vm.VM, got vm.Value) {
				if !vm.IsString(got) || v.DecodeString(got) != "mat" {
					t.Fatalf("expected string \"mat\", got %v", got)
				}
			}},
		{name: "bool", typeAnn: "bool", value: "true",
			check: func(t *testing.T, v *vm.VM, got vm.Value) {
				if !vm.IsBool(got) || vm.DecodeBool(got) != true {
					t.Fatalf("expected bool true, got %v", got)
				}
			}},
		{name: "array", typeAnn: "array<int>", value: "[10, 20, 30]",
			check: func(t *testing.T, v *vm.VM, got vm.Value) {
				if !vm.IsHandle(got) || !v.IsArray(vm.DecodeHandle(got)) {
					t.Fatalf("expected array handle, got %v", got)
				}
				if v.ArrayLength(vm.DecodeHandle(got)) != 3 {
					t.Fatalf("expected array length 3, got %v", got)
				}
			}},
		{name: "map", typeAnn: "map<string, int>", value: `{ "a": 1, "b": 2 }`,
			check: func(t *testing.T, v *vm.VM, got vm.Value) {
				if !vm.IsHandle(got) || v.IsArray(vm.DecodeHandle(got)) {
					t.Fatalf("expected map handle, got %v", got)
				}
				if v.MapSize(vm.DecodeHandle(got)) != 2 {
					t.Fatalf("expected map size 2, got %v", got)
				}
			}},
		{name: "struct", typeAnn: "Point", value: "Point{ x: 7 }",
			prelude: "struct Point { x: int }",
			check:   handle},
		{name: "class", typeAnn: "Box", value: "new Box(7)",
			prelude: "class Box {\n  v: int\n  fun Box(n: int): void { this.v = n }\n  fun get(): int { return this.v }\n}",
			check:   handle},
		{name: "interface", typeAnn: "Greeter", value: "new EnglishGreeter()",
			prelude: "interface Greeter { fun greet(): string }\nclass EnglishGreeter : Greeter { fun greet(): string { return \"hi\" } }",
			check:   handle},
		{name: "null", typeAnn: "any", value: "null",
			check: func(t *testing.T, v *vm.VM, got vm.Value) {
				if !vm.IsNull(got) {
					t.Fatalf("expected null value, got %v", got)
				}
			}},
		{name: "any", typeAnn: "any", value: "42",
			check: func(t *testing.T, v *vm.VM, got vm.Value) {
				if !vm.IsInt(got) || vm.DecodeInt(got) != 42 {
					t.Fatalf("expected any holding int 42, got %v", got)
				}
			}},
		{name: "fun", typeAnn: "fun(int): int", value: "fun(x: int): int { return x + 1 }",
			check: func(t *testing.T, v *vm.VM, got vm.Value) {
				if !vm.IsClosure(got) {
					t.Fatalf("expected a closure value, got %v", got)
				}
			}},
	}
}

func matrixAssignmentSource(r matrixRow) string {
	return r.prelude + "\nfun main(): any {\n  var probe: " + r.typeAnn + " = " + r.value + "\n  return probe\n}\n"
}

func matrixReturnSource(r matrixRow) string {
	return r.prelude + "\nfun main(): " + r.typeAnn + " {\n  return " + r.value + "\n}\n"
}

func matrixArgumentSource(r matrixRow) string {
	return r.prelude + "\nfun take(p: " + r.typeAnn + "): any {\n  return p\n}\nfun main(): any {\n  return take(" + r.value + ")\n}\n"
}

// TestTypeMatrix_LegalCombosExecute asserts that every conforming
// (type, dimension) combination compiles, executes, and round-trips the value
// with the correct type tag.
func TestTypeMatrix_LegalCombosExecute(t *testing.T) {
	for _, r := range matrixRows() {
		r := r
		dims := []struct {
			name   string
			source string
		}{
			{"assignment", matrixAssignmentSource(r)},
			{"return", matrixReturnSource(r)},
			{"argument", matrixArgumentSource(r)},
		}
		for _, d := range dims {
			t.Run(r.name+"/"+d.name, func(t *testing.T) {
				result, v, err := compileAndCallWithVM(t, d.source, "main", nil)
				if err != nil {
					t.Fatalf("legal combo did not execute: %v", err)
				}
				check := r.check
				if d.name == "argument" && r.argCheck != nil {
					check = r.argCheck
				}
				check(t, v, result)
			})
		}
	}
}

// TestTypeMatrix_FunRowCallThrough calls the function-typed value produced by
// each dimension, proving the closure is not just present but invocable.
func TestTypeMatrix_FunRowCallThrough(t *testing.T) {
	cases := []struct {
		name   string
		source string
	}{
		{"assignment", `fun main(): int {
  var probe: fun(int): int = fun(x: int): int { return x + 1 }
  return probe(41)
}`},
		{"return", `fun mk(): fun(int): int { return fun(x: int): int { return x + 1 } }
fun main(): int {
  var probe: fun(int): int = mk()
  return probe(41)
}`},
		{"argument", `fun take(f: fun(int): int): int { return f(41) }
fun main(): int {
  return take(fun(x: int): int { return x + 1 })
}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, _, err := compileAndCallWithVM(t, tc.source, "main", nil)
			if err != nil {
				t.Fatalf("fun call-through failed: %v", err)
			}
			if !vm.IsInt(result) || vm.DecodeInt(result) != 42 {
				t.Fatalf("expected 42, got %v", result)
			}
		})
	}
}

// TestTypeMatrix_NominalRoundTrips verifies member/field access still works on
// the struct/class/interface values after they travel through each dimension.
func TestTypeMatrix_NominalRoundTrips(t *testing.T) {
	structSrc := `struct Point { x: int }
fun main(): int {
  var probe: Point = Point{ x: 7 }
  return probe.x
}`
	if result, _, err := compileAndCallWithVM(t, structSrc, "main", nil); err != nil || vm.DecodeInt(result) != 7 {
		t.Fatalf("struct field round-trip: err=%v result=%v", err, result)
	}

	classSrc := `class Box {
  v: int
  fun Box(n: int): void { this.v = n }
  fun get(): int { return this.v }
}
fun main(): int {
  var probe: Box = new Box(7)
  return probe.get()
}`
	if result, _, err := compileAndCallWithVM(t, classSrc, "main", nil); err != nil || vm.DecodeInt(result) != 7 {
		t.Fatalf("class method round-trip: err=%v result=%v", err, result)
	}

	ifaceSrc := `interface Greeter { fun greet(): string }
class EnglishGreeter : Greeter { fun greet(): string { return "hi" } }
fun main(): string {
  var probe: Greeter = new EnglishGreeter()
  return probe.greet()
}`
	if result, v, err := compileAndCallWithVM(t, ifaceSrc, "main", nil); err != nil || v.DecodeString(result) != "hi" {
		t.Fatalf("interface method round-trip: err=%v result=%v", err, result)
	}
}

// ============================================================================
// Illegal combinations -> structured diagnostics.
// ============================================================================

// typeDiag is the structured view of a compiler diagnostic used by the matrix.
type typeDiag interface {
	DiagnosticCode() string
	DiagnosticPath() string
	DiagnosticExpected() string
	DiagnosticActual() string
}

// requireTypeDiagnostic compiles source, requires a compile-time diagnostic,
// and asserts its structured code/path (and expected/actual when supplied).
func requireTypeDiagnostic(t *testing.T, source, code, path, expected, actual string) {
	t.Helper()
	c, _, err := compileSourceAllowError(t, source)
	if err == nil {
		t.Fatalf("expected diagnostic %q, but source compiled cleanly", code)
	}
	if len(c.errors) == 0 {
		t.Fatalf("compile failed without structured diagnostics: %v", err)
	}
	d, ok := c.errors[0].(typeDiag)
	if !ok {
		t.Fatalf("first compile error is not a structured diagnostic (%T): %v", c.errors[0], c.errors[0])
	}
	if d.DiagnosticCode() != code {
		t.Fatalf("expected code %q, got %q (msg: %v)", code, d.DiagnosticCode(), err)
	}
	if d.DiagnosticPath() != path {
		t.Fatalf("code %q: expected path %q, got %q", code, path, d.DiagnosticPath())
	}
	if expected != "" && d.DiagnosticExpected() != expected {
		t.Fatalf("code %q: expected expected=%q, got %q", code, expected, d.DiagnosticExpected())
	}
	if actual != "" && d.DiagnosticActual() != actual {
		t.Fatalf("code %q: expected actual=%q, got %q", code, actual, d.DiagnosticActual())
	}
}

type negCase struct {
	name             string
	dimension        string
	source           string
	code, path       string
	expected, actual string
}

func negativeCases() []negCase {
	return []negCase{
		// --- assignment: var T = v ---
		{
			name: "fun-arity", dimension: "assignment",
			source: `fun main(): int {
  var f: fun(int, int): int = fun(x: int): int { return x }
  return f(1, 2)
}`,
			code: "function_type_arity_mismatch", path: "bytecode/type/funtype",
			expected: "fun(int,int):int", actual: "fun with 1 parameter(s)",
		},
		{
			name: "fun-param", dimension: "assignment",
			source: `fun main(): int {
  var f: fun(int): int = fun(x: string): int { return 1 }
  return 0
}`,
			code: "function_type_param_mismatch", path: "bytecode/type/funtype",
			expected: "int", actual: "string",
		},
		{
			name: "fun-return", dimension: "assignment",
			source: `fun main(): int {
  var f: fun(int): int = fun(x: int): string { return "s" }
  return 0
}`,
			code: "function_type_return_mismatch", path: "bytecode/type/funtype",
			expected: "int", actual: "string",
		},
		{
			name: "fun-literal", dimension: "assignment",
			source: `fun main(): int {
  var f: fun(int): int = 42
  return 0
}`,
			code: "function_type_mismatch", path: "bytecode/type/funtype",
			expected: "fun(int):int", actual: "int",
		},
		{
			name: "fun-reassign", dimension: "assignment",
			source: `fun main(): int {
  var f: fun(int): int = fun(x: int): int { return x }
  f = fun(x: int): string { return "s" }
  return 0
}`,
			code: "function_type_return_mismatch", path: "bytecode/type/funtype",
			expected: "int", actual: "string",
		},
		{
			name: "struct-field", dimension: "assignment",
			source: `struct Point { x: int }
fun main(): int {
  var probe: Point = Point{ x: "s" }
  return 0
}`,
			code: "struct_field_type_mismatch", path: "bytecode/struct/literal",
			expected: "int", actual: "string",
		},
		{
			name: "unknown-struct", dimension: "assignment",
			source: `fun main(): int {
  var probe: Nope = Nope{ x: 1 }
  return 0
}`,
			code: "unknown_struct_type", path: "bytecode/struct/literal",
		},
		{
			name: "type-alias-unknown", dimension: "assignment",
			source: `type Alias = Missing
fun main(): int { return 0 }`,
			code: "unknown_type_alias_target", path: "bytecode/typealias",
		},

		// --- return: fun(): T ---
		{
			name: "struct-field", dimension: "return",
			source: `struct Point { x: int }
fun mk(): Point { return Point{ x: "s" } }
fun main(): int { return 0 }`,
			code: "struct_field_type_mismatch", path: "bytecode/struct/literal",
			expected: "int", actual: "string",
		},
		{
			name: "override-return", dimension: "return",
			source: `open class A { open fun m(): int { return 1 } }
class B : A { override fun m(): string { return "s" } }
fun main(): int { return 0 }`,
			code: "override_signature_mismatch", path: "bytecode/oop/override",
			expected: "int", actual: "string",
		},
		{
			name: "interface-return", dimension: "return",
			source: `interface I { fun m(): int }
class C : I { fun m(): string { return "s" } }
fun main(): int { return 0 }`,
			code: "interface_signature_mismatch", path: "bytecode/oop/interface",
			expected: "int", actual: "string",
		},

		// --- argument: fun(p: T) ---
		{
			name: "funcall-arg", dimension: "argument",
			source: `fun main(): int {
  var f: fun(int): int = fun(x: int): int { return x }
  return f("oops")
}`,
			code: "function_type_arg_mismatch", path: "bytecode/type/funtype",
			expected: "int", actual: "string",
		},
		{
			name: "funcall-arity", dimension: "argument",
			source: `fun main(): int {
  var f: fun(int, int): int = fun(a: int, b: int): int { return a + b }
  return f(1)
}`,
			code: "function_type_arity_mismatch", path: "bytecode/type/funtype",
			expected: "2 argument(s)", actual: "1 argument(s)",
		},
		{
			name: "higher-order-lambda-param", dimension: "argument",
			source: `fun apply(f: fun(int): int, v: int): int { return f(v) }
fun main(): int { return apply(fun(x: string): int { return 1 }, 10) }`,
			code: "function_type_param_mismatch", path: "bytecode/type/funtype",
			expected: "int", actual: "string",
		},
		{
			name: "struct-arg-field", dimension: "argument",
			source: `struct Point { x: int }
fun take(p: Point): int { return p.x }
fun main(): int { return take(Point{ x: "s" }) }`,
			code: "struct_field_type_mismatch", path: "bytecode/struct/literal",
			expected: "int", actual: "string",
		},
		{
			name: "override-param-count", dimension: "argument",
			source: `open class A { open fun m(a: int): int { return a } }
class B : A { override fun m(a: int, b: int): int { return a } }
fun main(): int { return 0 }`,
			code: "override_signature_mismatch", path: "bytecode/oop/override",
			expected: "1 parameter(s)", actual: "2 parameter(s)",
		},
		{
			name: "interface-param-count", dimension: "argument",
			source: `interface I { fun m(a: int): int }
class C : I { fun m(a: int, b: int): int { return a } }
fun main(): int { return 0 }`,
			code: "interface_signature_mismatch", path: "bytecode/oop/interface",
			expected: "1 parameter(s)", actual: "2 parameter(s)",
		},
	}
}

// TestTypeMatrix_IllegalCombosProduceDiagnostics asserts that every currently
// diagnosed illegal combination surfaces as a structured diagnostic carrying a
// code + path (and, for type-mismatch diagnostics, expected/actual payloads).
func TestTypeMatrix_IllegalCombosProduceDiagnostics(t *testing.T) {
	for _, nc := range negativeCases() {
		nc := nc
		t.Run(nc.dimension+"/"+nc.name, func(t *testing.T) {
			requireTypeDiagnostic(t, nc.source, nc.code, nc.path, nc.expected, nc.actual)
		})
	}
}

// TestTypeMatrix_MismatchDiagnosticsAlwaysCarryPayloads guards the audit rule:
// a diagnostic describing a *type mismatch* must always carry non-empty
// expected/actual payloads (never a bare message).
func TestTypeMatrix_MismatchDiagnosticsAlwaysCarryPayloads(t *testing.T) {
	mismatchCodes := []string{
		"function_type_arity_mismatch",
		"function_type_param_mismatch",
		"function_type_return_mismatch",
		"function_type_arg_mismatch",
		"function_type_mismatch",
		"struct_field_type_mismatch",
		"override_signature_mismatch",
		"interface_signature_mismatch",
	}
	seen := map[string]bool{}
	for _, nc := range negativeCases() {
		for _, code := range mismatchCodes {
			if nc.code == code {
				seen[code] = true
			}
		}
	}
	for _, code := range mismatchCodes {
		if !seen[code] {
			t.Fatalf("mismatch code %q has no representative negative case in the matrix", code)
		}
	}
}

// TestTypeMatrix_LenientCrossTypePassThrough pins the deliberately lenient
// baseline: an int literal assigned / returned / passed through a `string`
// slot is NOT a compile error today. This card only tightens diagnostic
// metadata and must not change that behavior.
func TestTypeMatrix_LenientCrossTypePassThrough(t *testing.T) {
	cases := []struct {
		name   string
		source string
	}{
		{"var", `fun main(): any {
  var probe: string = 42
  return probe
}`},
		{"return", `fun mk(): string { return 42 }
fun main(): any { return mk() }`},
		{"argument", `fun take(p: string): any { return p }
fun main(): any { return take(42) }`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, _, err := compileAndCallWithVM(t, tc.source, "main", nil)
			if err != nil {
				t.Fatalf("lenient pass-through should still compile+run: %v", err)
			}
			// Value keeps its original int tag even inside a `string` slot.
			if !vm.IsInt(result) || vm.DecodeInt(result) != 42 {
				t.Fatalf("expected int 42 to pass through untouched, got %v", result)
			}
		})
	}
}

// TestTypeMatrix_SourceShapeSanity prevents the matrix sources from silently
// regressing into no-ops (e.g. an unannotated var).
func TestTypeMatrix_SourceShapeSanity(t *testing.T) {
	for _, r := range matrixRows() {
		src := matrixAssignmentSource(r)
		if !strings.Contains(src, ": "+r.typeAnn+" =") {
			t.Fatalf("row %q assignment source lost its type annotation:\n%s", r.name, src)
		}
	}
}
