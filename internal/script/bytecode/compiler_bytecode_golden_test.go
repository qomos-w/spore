// compiler_bytecode_golden_test.go pins the compiler's emitted bytecode for a
// corpus of representative programs. Every chunk (code, operands, source
// lines, constants pool, local count, nested function refs) is serialized
// deterministically and diffed against committed goldens under
// testdata/golden.
//
// This is the regression net for the dual-AST convergence work: collapsing the
// compiler's private statement IR onto the frontend AST must not change a single
// emitted byte. Any lowering drift fails here with a byte-for-byte diff.
//
// Regenerate goldens (only when an intentional lowering change lands):
//
//	go test ./internal/script/bytecode -run TestCompilerGoldenBytecode -update
package bytecode

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/qomos-w/spore/internal/script/frontend"
)

// updateBytecodeGolden rewrites testdata/golden from the live compiler.
var updateBytecodeGolden = flag.Bool("update", false, "rewrite bytecode testdata/golden")

// goldenPrograms is the corpus pinned by TestCompilerGoldenBytecode. It is
// chosen to exercise every statement form and every top-level declaration the
// compiler lowers: var/return/yield/if/while/for/for-in/when/break/continue/
// block/expression/try/defer statements plus fun/struct/enum/class/interface/
// type-alias/global declarations, closures, optional chaining and typed
// arithmetic.
var goldenPrograms = []struct {
	name   string
	source string
}{
	{
		name: "arith_locals",
		source: `
fun main(): int {
  var a: int = 1
  var b: int = 2
  a = a + b * 3
  return a - b
}`,
	},
	{
		name: "typed_arithmetic",
		source: `
fun calc(): double {
  var d: double = 1.5
  var e: double = 2.5
  return d * e + 1.0
}
fun longs(): long {
  var a: long = 3
  var b: long = 4
  return a * b
}
fun mixed(): int {
  var i: int = 2
  var j: int = 3
  return i * j - 1
}`,
	},
	{
		name: "control_flow",
		source: `
fun classify(n: int): string {
  var out: string = ""
  if n < 0 {
    out = "neg"
  } else if n == 0 {
    out = "zero"
  } else {
    out = "pos"
  }
  var i: int = 0
  while i < n {
    i = i + 1
    if i == 2 { continue }
    if i == 4 { break }
    out = out + "w"
  }
  for (var j: int = 0; j < n; j = j + 1) {
    out = out + "x"
  }
  return out
}`,
	},
	{
		name: "for_in",
		source: `
fun sumall(items: array): int {
  var total: int = 0
  for (item in items) {
    total = total + item
  }
  return total
}
fun keys(m: map): string {
  var out: string = ""
  for (k in m) {
    out = out + k
  }
  return out
}`,
	},
	{
		name: "when_cases",
		source: `
fun label(x: any): string {
  when (x) {
    case 1 { return "one" }
    case 2, 3 { return "few" }
    else { return "many" }
  }
  return "?"
}
fun classify(x: int): int {
  when (x) {
    case 1 when x > 0 { return 100 }
    case 2 { return 200 }
    else { return 999 }
  }
  return 0
}
enum Kind { A, B }
fun kindName(x: any): string {
  when (x) {
    case k: Kind { return "kind" }
    else { return "other" }
  }
  return "none"
}`,
	},
	{
		name: "try_defer",
		source: `
fun guarded(): int {
  defer { var cleanup: int = 1 }
  var r: int = 0
  try {
    r = 10
  } catch (e) {
    r = 20
  }
  return r
}`,
	},
	{
		name: "blocks_and_expr_stmts",
		source: `
fun main(): void {
  {
    var x: int = 1
  }
  var m: map = {"a": 1}
  m["b"] = 2
  var arr: array = [1, 2, 3]
  arr[0] = 9
  var s: string = "hi" + 1
  var n: int = arr[1]
}`,
	},
	{
		name: "stream_yield",
		source: `
stream fun gen(): int {
  yield 1
  yield 2
  return 3
}
fun consume(): int {
  var total: int = 0
  return total
}`,
	},
	{
		name: "functions_and_expr_body",
		source: `
fun add(a: int, b: int): int { return a + b }
fun square(n: int): int = n * n
fun apply(f: fun(int): int, v: int): int { return f(v) }
fun main(): int {
  return add(square(3), apply(fun(x: int): int = x + 1, 4))
}`,
	},
	{
		name: "lambdas_closures",
		source: `
fun makeAdder(base: int): any {
  return fun(x: int): int { return x + base }
}
fun run(): int {
  var base: int = 10
  var dbl: any = fun(x: int): int { return x + base }
  var acc: int = 0
  for (i in [1, 2, 3]) {
    acc = acc + dbl(i)
  }
  return acc
}`,
	},
	{
		name: "structs_enums",
		source: `
enum Color { Red, Green = 5, Blue }
struct Pixel {
  x: int
  y: int
  color: Color
}
fun make(): Pixel {
  var p: Pixel = Pixel{ x: 1, y: 2, color: Color.Blue }
  return p
}
fun value(): int {
  return Color.Red as int
}
fun check(p: Pixel): bool {
  return p.color == Color.Blue && p.color is Color
}`,
	},
	{
		name: "classes_oop",
		source: `
open class Animal {
  name: string
  constructor(n: string) { this.name = n }
  open fun speak(): string { return "..." }
}
class Dog : Animal {
  constructor(n: string) { super(n) }
  override fun speak(): string { return super.speak() + " woof" }
  fun describe(): string { return this.name }
}
fun make(): string {
  var d: Dog = new Dog("rex")
  return d.speak() + d.describe()
}`,
	},
	{
		name: "interface_defaults",
		source: `
interface IGreet { fun greet(): string { return "hello" } }
class Person : IGreet {
  fun self(): string { return this.greet() }
}
fun run(): string {
  var p: Person = new Person()
  return p.greet() + p.self()
}`,
	},
	{
		name: "type_alias",
		source: `
type Money = long
type PriceMap = map<string, long>
fun price(): Money { return 100 }
fun lookup(prices: PriceMap): Money { return prices["total"] }`,
	},
	{
		name: "optionals",
		source: `
class Box { value: int }
fun get(b: Box): any {
  var r: any = b?.value
  return r ?? 0
}
fun chain(b: Box): any {
  return b?.value
}`,
	},
	{
		name: "globals",
		source: `
var counter: int = 1
var label: string = "n"
fun bump(): int {
  counter = counter + 1
  return counter
}
counter = 5`,
	},
	{
		name: "method_this_capture",
		source: `
class C {
  base: int
  constructor(b: int) { this.base = b }
  fun make(): any { return fun(x: int): int { return x + this.base } }
}`,
	},
}

// TestCompilerGoldenBytecode compiles each corpus program and compares the
// full serialized bytecode against the committed golden.
func TestCompilerGoldenBytecode(t *testing.T) {
	goldenDir := filepath.Join("testdata", "golden")
	if *updateBytecodeGolden {
		if err := os.RemoveAll(goldenDir); err != nil {
			t.Fatalf("remove golden dir: %v", err)
		}
		if err := os.MkdirAll(goldenDir, 0o755); err != nil {
			t.Fatalf("create golden dir: %v", err)
		}
	}

	for _, tc := range goldenPrograms {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			prog, err := frontend.ParseModuleForTest(tc.source)
			if err != nil {
				t.Fatalf("parse %s: %v", tc.name, err)
			}
			c := newCompiler()
			main, err := c.compile(prog)
			if err != nil {
				t.Fatalf("compile %s: %v", tc.name, err)
			}
			got := dumpCompiledProgram(main, c.getFunctions())
			path := filepath.Join(goldenDir, tc.name+".txt")

			if *updateBytecodeGolden {
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatalf("write golden %s: %v", path, err)
				}
				return
			}

			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("missing golden %s: %v", path, err)
			}
			if string(want) != got {
				t.Errorf("bytecode golden mismatch for %s\n--- want\n%s\n--- got\n%s", tc.name, want, got)
			}
		})
	}
}

// dumpCompiledProgram serializes the main chunk plus every function chunk in
// deterministic (sorted) order.
func dumpCompiledProgram(main *chunk, funcs map[string]*chunk) string {
	var b strings.Builder
	b.WriteString("== main ==\n")
	dumpChunk(&b, main, "")

	names := make([]string, 0, len(funcs))
	for name := range funcs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(&b, "== fun %s ==\n", name)
		dumpChunk(&b, funcs[name], "")
	}
	return b.String()
}

// dumpChunk writes one chunk: header, instructions (opcode name + operand +
// source line), constants pool, then nested function references.
func dumpChunk(b *strings.Builder, ch *chunk, indent string) {
	fmt.Fprintf(b, "%ssource=%q localCount=%d\n", indent, ch.sourceName, ch.LocalCount)
	for i, inst := range ch.code {
		name, ok := opcodeNames[inst.op]
		if !ok {
			name = fmt.Sprintf("OP(%d)", byte(inst.op))
		}
		line := 0
		if i < len(ch.lines) {
			line = ch.lines[i]
		}
		fmt.Fprintf(b, "%s  %04d %-28s %d line=%d\n", indent, i, name, inst.operand, line)
	}
	for i, cst := range ch.constants {
		fmt.Fprintf(b, "%s  const[%d]=%s\n", indent, i, formatGoldenConstant(cst))
	}
	for _, fn := range ch.funcChunks {
		fmt.Fprintf(b, "%s  -- nested --\n", indent)
		dumpChunk(b, fn, indent+"    ")
	}
}

// formatGoldenConstant renders a constants-pool entry deterministically,
// including its Go type so tag changes are visible in the golden diff.
func formatGoldenConstant(v any) string {
	if ec, ok := v.(enumConst); ok {
		return fmt.Sprintf("enumConst{%s,%d}", ec.enum, ec.value)
	}
	return fmt.Sprintf("%T(%v)", v, v)
}
