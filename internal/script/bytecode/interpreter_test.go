package bytecode

import (
	"strings"
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/diagnostics"
	"github.com/qomos-w/spore/internal/script/frontend"
	"github.com/qomos-w/spore/internal/script/vm"
)

// compileAndRun parses source, compiles it, and runs the main chunk.
// Returns the top-of-stack value from the program or the named function.
func compileAndRun(t *testing.T, source string) *vm.VM {
	t.Helper()

	// Parse.
	prog, err := frontend.ParseModuleForTest(source)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	// Create VM.
	v := vm.NewVM(4096, 256)

	// Compile.
	c := newCompiler()
	mainChunk, err := c.compile(prog)
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	// Create interpreter.
	interp := newInterpreter(v)

	// Register functions and classes.
	c.registerFunctions(v, interp)
	c.registerClasses(v)
	c.registerStructs(v)
	c.registerEnums(v)

	// Execute.
	_, err = interp.Execute(mainChunk, c.getFunctions())
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	return v
}

// compileAndCall parses, compiles, and calls a named function.
func compileAndCall(t *testing.T, source, fnName string, args []vm.Value) (vm.Value, error) {
	t.Helper()
	result, _, err := compileAndCallWithVM(t, source, fnName, args)
	return result, err
}

// compileAndCallWithVM is like compileAndCall but also returns the VM instance.
func compileAndCallWithVM(t *testing.T, source, fnName string, args []vm.Value) (vm.Value, *vm.VM, error) {
	t.Helper()

	prog, err := frontend.ParseModuleForTest(source)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	v := vm.NewVM(4096, 256)

	c := newCompiler()
	_, err = c.compile(prog)
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	interp := newInterpreter(v)
	c.registerFunctions(v, interp)
	c.registerClasses(v)
	c.registerStructs(v)
	c.registerEnums(v)

	fnChunks := c.getFunctions()
	fnChunk, ok := fnChunks[fnName]
	if !ok {
		t.Fatalf("function %q not found in compiled chunks", fnName)
	}

	result, err := interp.ExecuteFunction(fnChunk, binding.InvocationStageUnary, args)
	return result, v, err
}

func TestInterpreter_DebugConstructorRegisteredOnClass(t *testing.T) {
	prog, err := frontend.ParseModuleForTest(`
interface Greeter { fun greet(): string }
class Person : Greeter { name: string fun greet(): string { return this.name } }
fun check(): bool {
  var p: Person = new Person()
  return p is Greeter
}`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	c := newCompiler()
	_, err = c.compile(prog)
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	for name := range c.getFunctions() {
		t.Logf("function %s", name)
	}
	v := vm.NewVM(4096, 256)
	interp := newInterpreter(v)
	c.registerFunctions(v, interp)
	c.registerClasses(v)
	cls := v.ClassReg().GetClassByName("Person")
	if cls == nil {
		t.Fatal("class Person not registered")
	}
	if cls.GetMethod("Person") != nil {
		t.Fatal("unexpected constructor registered on class without constructor source")
	}
}

func TestInterpreter_DebugClassMapFieldLenOpcodes(t *testing.T) {
	prog, err := frontend.ParseModuleForTest(`
class Shop {
  prices: map<string, int>
  constructor(base: int) { this.prices = {"potion": base, "ether": base + 2} }
}
class Market {
  shops: array<Shop>
  constructor() { this.shops = [new Shop(3), new Shop(7)] }
  fun size(): int { return len(this.shops[0].prices) }
}
fun simulate(): int {
  var market: Market = new Market()
  return market.size()
}`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	c := newCompiler()
	_, err = c.compile(prog)
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	ch := c.getFunctions()["Market.size"]
	if ch == nil {
		t.Fatal("Market.size chunk not found")
	}
	foundMapLen := false
	for _, inst := range ch.code {
		if inst.op == opMapLen {
			foundMapLen = true
		}
		if inst.op == opArrayLen {
			t.Fatal("expected indexed map field len to compile to MAP_LEN, got ARRAY_LEN")
		}
	}
	if !foundMapLen {
		t.Fatal("expected indexed map field len to compile to MAP_LEN")
	}
}

func TestInterpreter_VMMapSizeUsesEntryCount(t *testing.T) {
	v := vm.NewVM(4096, 256)
	m := v.NewMap(vm.TypeInvalid, vm.TypeInvalid, 16)
	if got := v.MapSize(m); got != 0 {
		t.Fatalf("expected empty map size 0, got %d", got)
	}
	v.MapSet(m, v.EncodeString("potion"), vm.EncodeInt(3))
	v.MapSet(m, v.EncodeString("ether"), vm.EncodeInt(5))
	if got := v.MapSize(m); got != 2 {
		t.Fatalf("expected map size 2 after two inserts, got %d", got)
	}
}

func TestInterpreter_VMMapHeaderLayoutUsesCapacityAndSizeSeparately(t *testing.T) {
	v := vm.NewVM(4096, 256)
	m := v.NewMap(vm.TypeInvalid, vm.TypeInvalid, 16)
	idx := v.ResolveHandle(m)
	if got := int(v.MemoryAt(idx + 1)); got != 16 {
		t.Fatalf("expected map header capacity slot 16, got %d", got)
	}
	if got := int(v.MemoryAt(idx + 2)); got != 0 {
		t.Fatalf("expected empty map header size slot 0, got %d", got)
	}
	v.MapSet(m, v.EncodeString("potion"), vm.EncodeInt(3))
	v.MapSet(m, v.EncodeString("ether"), vm.EncodeInt(5))
	if got := int(v.MemoryAt(idx + 1)); got != 16 {
		t.Fatalf("expected map capacity slot to remain 16, got %d", got)
	}
	if got := int(v.MemoryAt(idx + 2)); got != 2 {
		t.Fatalf("expected map size slot 2, got %d", got)
	}
}
func TestInterpreter_StructLiteralFieldReadReturnsExpectedValue(t *testing.T) {
	source := `
struct Point {
  x: int
  y: int
}
fun read(): int { return Point{x: 1, y: 2}.x }`

	result, err := compileAndCall(t, source, "read", nil)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 1 {
		t.Fatalf("expected 1, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_StructVariableFieldReadReturnsExpectedValue(t *testing.T) {
	source := `
struct Point {
  x: int
  y: int
}
fun read(): int {
  var p: Point = Point{x: 7, y: 9}
  return p.x
}`

	result, err := compileAndCall(t, source, "read", nil)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 7 {
		t.Fatalf("expected 7, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_StructLiteralAllFieldsSpread(t *testing.T) {
	source := `
struct Point {
  x: int
  y: int
}
fun read(): int {
  var p: Point = Point{..}
  return p.x + p.y
}`

	result, err := compileAndCall(t, source, "read", nil)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 0 {
		t.Fatalf("expected zero-fill sum 0, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_StructLiteralPartialSpread(t *testing.T) {
	source := `
struct Point {
  x: int
  y: int
}
fun readX(): int {
  var p: Point = Point{x: 7, ..}
  return p.x
}
fun readY(): int {
  var p: Point = Point{x: 7, ..}
  return p.y
}`

	x, err := compileAndCall(t, source, "readX", nil)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(x) != 7 {
		t.Fatalf("expected x=7, got %d", vm.DecodeInt(x))
	}
	y, err := compileAndCall(t, source, "readY", nil)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(y) != 0 {
		t.Fatalf("expected y=0 from spread, got %d", vm.DecodeInt(y))
	}
}

func TestInterpreter_StructLiteralSpreadEmitsTypedZero(t *testing.T) {
	source := `
struct Mixed {
  i: int
  l: long
  s: string
  b: bool
}
fun ok(): bool {
  var m: Mixed = Mixed{..}
  if m.i != 0 { return false }
  if m.l != 0 { return false }
  if m.s != "" { return false }
  if m.b != false { return false }
  return true
}`

	result, err := compileAndCall(t, source, "ok", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !vm.DecodeBool(result) {
		t.Fatalf("expected true from typed zero check, got false")
	}
}

func TestInterpreter_StructLiteralSpreadEmitsEmptyContainers(t *testing.T) {
	// Verifies that array<T> / map<K,V> fields filled by spread compile and
	// reach runtime as fresh empty containers (opNewArray / opNewMap), not
	// null handles. We construct the struct twice and confirm both runs
	// succeed; emitZeroValue picks opNewArray/opNewMap for these field
	// kinds, so a regression to opPushNull would fail field-type validation
	// or panic on subsequent indexing.
	source := `
struct WithContainers {
  items: array<int>
  tags:  map<string, string>
}
fun build(): WithContainers {
  return WithContainers{..}
}
fun rebuild(): WithContainers {
  return WithContainers{..}
}`

	if _, err := compileAndCall(t, source, "build", nil); err != nil {
		t.Fatalf("build with spread: %v", err)
	}
	if _, err := compileAndCall(t, source, "rebuild", nil); err != nil {
		t.Fatalf("rebuild with spread: %v", err)
	}
}

func TestInterpreter_StructLiteralSpreadEmitsNullForNestedStruct(t *testing.T) {
	// Nested struct/class fields collapse to a null handle on spread, since
	// emitZeroValue does not recursively construct nested struct zero values.
	source := `
struct Inner {
  x: int
}
struct Outer {
  inner: Inner
  count: int
}
fun read(): int {
  var o: Outer = Outer{count: 5, ..}
  return o.count
}`

	result, err := compileAndCall(t, source, "read", nil)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 5 {
		t.Fatalf("expected count=5 with nested-struct spread, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_StructLiteralSpreadResolvesTypeAlias(t *testing.T) {
	// Type aliases must resolve through emitZeroValue so an aliased long
	// (e.g. type Money = long) still picks opPushLong on spread.
	source := `
type Money = long
struct Wallet {
  balance: Money
}
fun read(): long {
  var w: Wallet = Wallet{..}
  return w.balance
}`

	result, v, err := compileAndCallWithVM(t, source, "read", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !vm.IsLong(result) {
		t.Fatalf("expected long-typed zero from spread+alias, got tag check failed (raw=0x%016x)", result)
	}
	if v.DecodeLong(result) != 0 {
		t.Fatalf("expected balance=0 from spread, got %d", v.DecodeLong(result))
	}
}

func TestInterpreter_StructLiteralWithoutSpreadStillRequiresAllFields(t *testing.T) {
	source := `
struct Point {
  x: int
  y: int
}
fun bad(): int {
  var p: Point = Point{x: 1}
  return p.x
}`

	prog, err := frontend.ParseModuleForTest(source)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	c := newCompiler()
	if _, err := c.compile(prog); err == nil {
		t.Fatal("expected missing_struct_field compile error without spread")
	} else if !strings.Contains(err.Error(), "missing required field") {
		t.Fatalf("expected missing_struct_field message, got %q", err.Error())
	}
}

func TestInterpreter_StructLiteralSpreadInMiddleIsRejected(t *testing.T) {
	source := `
struct Point {
  x: int
  y: int
}
fun bad(): int {
  var p: Point = Point{x: 1, .., y: 2}
  return p.x
}`

	if _, err := frontend.ParseModuleForTest(source); err == nil {
		t.Fatal("expected parse error for spread in middle")
	} else if !strings.Contains(err.Error(), "spread") {
		t.Fatalf("expected spread-related error, got %q", err.Error())
	}
}

func TestInterpreter_StructLiteralDuplicateSpreadIsRejected(t *testing.T) {
	source := `
struct Point {
  x: int
  y: int
}
fun bad(): int {
  var p: Point = Point{x: 1, .., ..}
  return p.x
}`

	if _, err := frontend.ParseModuleForTest(source); err == nil {
		t.Fatal("expected parse error for duplicate spread")
	} else if !strings.Contains(err.Error(), "spread") {
		t.Fatalf("expected spread-related error, got %q", err.Error())
	}
}

func TestInterpreter_StructLiteralIntLiteralCoercesToLongFieldType(t *testing.T) {
	source := `
struct RateLimit {
  remaining: long
  capacity: long
}
fun read(): long {
  var r: RateLimit = RateLimit{remaining: 0, capacity: 10}
  return r.capacity
}`

	result, err := compileAndCall(t, source, "read", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !vm.IsLong(result) {
		t.Fatalf("expected long-typed result for long-typed field literal, got tag check failed (raw=0x%016x)", result)
	}
}

func TestInterpreter_NestedStructFieldReadReturnsExpectedValue(t *testing.T) {
	source := `
struct Point {
  x: int
  y: int
}
struct Box {
  point: Point
}
fun read(): int { return Box{point: Point{x: 5, y: 6}}.point.x }`

	result, err := compileAndCall(t, source, "read", nil)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 5 {
		t.Fatalf("expected 5, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_ArrayIndexReturnsExpectedValue(t *testing.T) {
	result, err := compileAndCall(t,
		"fun first(): int { return [10, 20, 30][0] }",
		"first",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 10 {
		t.Fatalf("expected 10, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_ArrayIndexUsesLatestElement(t *testing.T) {
	result, err := compileAndCall(t,
		"fun second(): int { return [10, 20, 30][1] }",
		"second",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 20 {
		t.Fatalf("expected 20, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_MapIndexReturnsExpectedValue(t *testing.T) {
	result, err := compileAndCall(t,
		`fun hp(): int { return {"hp": 10, "mp": 20}["hp"] }`,
		"hp",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 10 {
		t.Fatalf("expected 10, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_MapIndexSelectsCorrectKey(t *testing.T) {
	result, err := compileAndCall(t,
		`fun mp(): int { return {"hp": 10, "mp": 20}["mp"] }`,
		"mp",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 20 {
		t.Fatalf("expected 20, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_InterfaceIsCheckCurrentContract(t *testing.T) {
	source := `
interface Greeter { fun greet(): string }
class Person : Greeter { name: string fun greet(): string { return this.name } }
fun check(): bool {
  var p: Person = new Person()
  p.name = "hi"
  return p is Greeter
}`

	result, err := compileAndCall(t, source, "check", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !vm.DecodeBool(result) {
		t.Fatal("expected true for interface is-check")
	}
}

func TestInterpreter_SuperMethodDispatch(t *testing.T) {
	source := `
open class Animal {
  open fun speak(): string { return "animal" }
}
class Dog : Animal {
  override fun speak(): string { return super.speak() }
}
fun read(): string {
  var d: Dog = new Dog()
  return d.speak()
}`

	result, _, err := compileAndCallWithVM(t, source, "read", nil)
	if err != nil {
		t.Fatal(err)
	}
	v := vm.NewVM(4096, 256)
	if v.DecodeString(result) != "animal" {
		t.Fatalf("expected animal, got %q", v.DecodeString(result))
	}
}

func TestInterpreter_OpenOverrideDispatch(t *testing.T) {
	source := `
open class Animal {
  open fun speak(): string { return "animal" }
}
class Dog : Animal {
  override fun speak(): string { return "dog" }
}
fun read(): string {
  var d: Dog = new Dog()
  return d.speak()
}`

	result, _, err := compileAndCallWithVM(t, source, "read", nil)
	if err != nil {
		t.Fatal(err)
	}
	v := vm.NewVM(4096, 256)
	if v.DecodeString(result) != "dog" {
		t.Fatalf("expected dog, got %q", v.DecodeString(result))
	}
}

func TestInterpreter_ClassConstructorAssignsField(t *testing.T) {
	source := `
class Dog {
  name: string
  constructor(n: string) { this.name = n }
}
fun read(): string {
  var d: Dog = new Dog("Rex")
  return d.name
}`

	result, _, err := compileAndCallWithVM(t, source, "read", nil)
	if err != nil {
		t.Fatal(err)
	}
	v := vm.NewVM(4096, 256)
	if v.DecodeString(result) != "Rex" {
		t.Fatalf("expected Rex, got %q", v.DecodeString(result))
	}
}

func TestInterpreter_TypeAliasChainResolvesRuntimeEncoding(t *testing.T) {
	source := `
	type Id = long
	type UserId = Id
	fun value(): UserId { return 42 }`

	result, executionVM, err := compileAndCallWithVM(t, source, "value", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !vm.IsLong(result) {
		t.Fatal("expected alias chain to produce encoded long")
	}
	if got := executionVM.DecodeLong(result); got != 42 {
		t.Fatalf("expected long payload 42, got %d", got)
	}
}

func TestInterpreter_BlockReturnUsesResolvedLongAliasHint(t *testing.T) {
	source := `
	type Id = long
	fun value(): Id {
	  return 42
	}`

	result, executionVM, err := compileAndCallWithVM(t, source, "value", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !vm.IsLong(result) {
		t.Fatal("expected block-body return to produce encoded long")
	}
	if got := executionVM.DecodeLong(result); got != 42 {
		t.Fatalf("expected long payload 42, got %d", got)
	}
}

func TestInterpreter_StructFieldAssignmentUpdatesValue(t *testing.T) {
	source := `
struct Point {
  x: int
  y: int
}
fun set(): int {
  var p: Point = Point{x: 1, y: 2}
  p.x = 9
  return p.x
}`

	result, err := compileAndCall(t, source, "set", nil)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 9 {
		t.Fatalf("expected 9, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_ArrayElementAssignmentUpdatesValue(t *testing.T) {
	source := `
fun set(): int {
  var xs: array<int> = [1, 2, 3]
  xs[1] = 9
  return xs[1]
}`

	result, err := compileAndCall(t, source, "set", nil)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 9 {
		t.Fatalf("expected 9, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_ArrayPushBuiltinGrowsArray(t *testing.T) {
	source := `
fun build(): int {
  var xs: array<int> = []
  push(xs, 10)
  push(xs, 20)
  push(xs, 30)
  return xs[0] + xs[1] + xs[2]
}`

	result, err := compileAndCall(t, source, "build", nil)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 60 {
		t.Fatalf("expected 60, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_ArrayPushBuiltinExtendsLength(t *testing.T) {
	source := `
fun count(): int {
  var xs: array<int> = [1]
  push(xs, 2)
  push(xs, 3)
  return len(xs)
}`

	result, err := compileAndCall(t, source, "count", nil)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 3 {
		t.Fatalf("expected 3, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_ArrayPushBuiltinKeepsArrayAcrossRepeatedCalls(t *testing.T) {
	source := `
fun build(n: int): int {
  var xs: array<string> = []
  for (var i: int = 0; i < n; i = i + 1) { push(xs, "x") }
  return len(xs)
}`

	arg := vm.EncodeInt(100)
	for i := 0; i < 2; i++ {
		result, err := compileAndCall(t, source, "build", []vm.Value{arg})
		if err != nil {
			t.Fatal(err)
		}
		if vm.DecodeInt(result) != 100 {
			t.Fatalf("expected 100, got %d", vm.DecodeInt(result))
		}
	}
}

func TestInterpreter_MapKeyAssignmentUpdatesValue(t *testing.T) {
	source := `
fun set(): int {
  var attrs: map<string, int> = {"hp": 10}
  attrs["hp"] = 20
  return attrs["hp"]
}`

	result, err := compileAndCall(t, source, "set", nil)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 20 {
		t.Fatalf("expected 20, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_WhenCaseMatchesExpectedBranch(t *testing.T) {
	source := `
fun label(x: int): int {
  when (x) {
    case 1 { return 10 }
    case 2 { return 20 }
    else { return 30 }
  }
}`

	cases := []struct {
		name string
		arg  int
		want int
	}{
		{"one", 1, 10},
		{"two", 2, 20},
		{"other", 9, 30},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := compileAndCall(t, source, "label", []vm.Value{vm.EncodeInt(int32(tc.arg))})
			if err != nil {
				t.Fatal(err)
			}
			if vm.DecodeInt(result) != int32(tc.want) {
				t.Fatalf("expected %d, got %d", tc.want, vm.DecodeInt(result))
			}
		})
	}
}

func TestInterpreter_WhenCaseWithMultipleValuesMatchesExpectedBranch(t *testing.T) {
	source := `
fun label(x: int): int {
  when (x) {
    case 1, 2 { return 10 }
    else { return 30 }
  }
}`

	cases := []struct {
		name string
		arg  int
		want int
	}{
		{"one", 1, 10},
		{"two", 2, 10},
		{"other", 9, 30},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := compileAndCall(t, source, "label", []vm.Value{vm.EncodeInt(int32(tc.arg))})
			if err != nil {
				t.Fatal(err)
			}
			if vm.DecodeInt(result) != int32(tc.want) {
				t.Fatalf("expected %d, got %d", tc.want, vm.DecodeInt(result))
			}
		})
	}
}

func TestInterpreter_WhenCaseGuardSelectsBranchWhenTrue(t *testing.T) {
	source := `
fun classify(x: int): int {
  when (x) {
    case 1 when x > 0 { return 100 }
    case 2 { return 200 }
    else { return 999 }
  }
}`
	result, err := compileAndCall(t, source, "classify", []vm.Value{vm.EncodeInt(1)})
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 100 {
		t.Fatalf("expected guard-true branch 100, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_WhenCaseGuardFalseFallsThroughToNextCase(t *testing.T) {
	source := `
fun classify(x: int): int {
  when (x) {
    case 1 when x > 99 { return 100 }
    case 1 { return 1 }
    else { return 999 }
  }
}`
	result, err := compileAndCall(t, source, "classify", []vm.Value{vm.EncodeInt(1)})
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 1 {
		t.Fatalf("expected guard-fail to fall through to second case (1), got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_WhenCaseGuardFalseFallsThroughToElse(t *testing.T) {
	source := `
fun classify(x: int): int {
  when (x) {
    case 1 when x > 99 { return 100 }
    else { return 999 }
  }
}`
	result, err := compileAndCall(t, source, "classify", []vm.Value{vm.EncodeInt(1)})
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 999 {
		t.Fatalf("expected guard-fail with no following case to land in else (999), got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_WhenCaseGuardWithMultipleValues(t *testing.T) {
	source := `
fun classify(x: int): int {
  when (x) {
    case 1, 2, 3 when x > 1 { return 100 }
    else { return 0 }
  }
}`
	cases := []struct {
		name string
		arg  int32
		want int32
	}{
		{"value_match_guard_fail", 1, 0}, // matches value but x>1 false
		{"value_match_guard_pass", 2, 100},
		{"value_match_guard_pass_3", 3, 100},
		{"value_no_match", 99, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := compileAndCall(t, source, "classify", []vm.Value{vm.EncodeInt(tc.arg)})
			if err != nil {
				t.Fatal(err)
			}
			if vm.DecodeInt(result) != tc.want {
				t.Fatalf("arg %d: expected %d, got %d", tc.arg, tc.want, vm.DecodeInt(result))
			}
		})
	}
}

func TestInterpreter_ForLoopAccumulatesValue(t *testing.T) {
	source := `
fun sum(): int {
  var total: int = 0
  for (var i: int = 0; i < 5; i = i + 1) {
    total = total + i
  }
  return total
}`

	result, err := compileAndCall(t, source, "sum", nil)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 10 {
		t.Fatalf("expected 10, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_ForLoopConditionFalseSkipsBody(t *testing.T) {
	source := `
fun calc(): int {
  var total: int = 7
  for (var i: int = 5; i < 5; i = i + 1) {
    total = total + i
  }
  return total
}`

	result, err := compileAndCall(t, source, "calc", nil)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 7 {
		t.Fatalf("expected 7, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_ForLoopUpdateRunsAfterBody(t *testing.T) {
	source := `
fun calc(): int {
  var total: int = 0
  for (var i: int = 1; i < 8; i = i * 2) {
    total = total + i
  }
  return total
}`

	result, err := compileAndCall(t, source, "calc", nil)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 7 {
		t.Fatalf("expected 7, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_ForInArrayAccumulatesValue(t *testing.T) {
	source := `
fun sum(): int {
  var total: int = 0
  for (item in [1, 2, 3, 4]) {
    total = total + item
  }
  return total
}`

	result, err := compileAndCall(t, source, "sum", nil)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 10 {
		t.Fatalf("expected 10, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_BreakExitsWhileLoop(t *testing.T) {
	source := `
fun calc(): int {
  var i: int = 0
  while i < 10 {
    if i == 3 { break }
    i = i + 1
  }
  return i
}`

	result, err := compileAndCall(t, source, "calc", nil)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 3 {
		t.Fatalf("expected 3, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_ContinueSkipsRemainingLoopBody(t *testing.T) {
	source := `
fun calc(): int {
  var i: int = 0
  var total: int = 0
  while i < 5 {
    i = i + 1
    if i == 3 { continue }
    total = total + i
  }
  return total
}`

	result, err := compileAndCall(t, source, "calc", nil)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 12 {
		t.Fatalf("expected 12, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_BreakSkipsRemainingLoopBody(t *testing.T) {
	source := `
fun calc(): int {
  var i: int = 0
  var total: int = 0
  while i < 5 {
    i = i + 1
    if i == 3 { break }
    total = total + i
  }
  return total
}`

	result, err := compileAndCall(t, source, "calc", nil)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 3 {
		t.Fatalf("expected 3, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_WhileLoopAccumulatesValue(t *testing.T) {
	source := `
fun sum(n: int): int {
  var total: int = 0
  var i: int = 0
  while i < n {
    total = total + i
    i = i + 1
  }
  return total
}`

	result, err := compileAndCall(t, source, "sum", []vm.Value{vm.EncodeInt(5)})
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 10 {
		t.Fatalf("expected 10, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_WhileConditionFalseSkipsBody(t *testing.T) {
	source := `
fun sum(n: int): int {
  var total: int = 7
  var i: int = 0
  while i < n {
    total = total + i
    i = i + 1
  }
  return total
}`

	result, err := compileAndCall(t, source, "sum", []vm.Value{vm.EncodeInt(0)})
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 7 {
		t.Fatalf("expected 7, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_WhileLoopUsesUpdatedVariables(t *testing.T) {
	source := `
fun calc(): int {
  var x: int = 1
  while x < 8 {
    x = x * 2
  }
  return x
}`

	result, err := compileAndCall(t, source, "calc", nil)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 8 {
		t.Fatalf("expected 8, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_IfTrueBranchReturnsExpectedValue(t *testing.T) {
	result, err := compileAndCall(t,
		"fun check(flag: bool): int { if flag { return 1 } return 2 }",
		"check",
		[]vm.Value{vm.EncodeBool(true)},
	)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 1 {
		t.Fatalf("expected 1, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_IfFalseFallsThroughToTrailingReturn(t *testing.T) {
	result, err := compileAndCall(t,
		"fun check(flag: bool): int { if flag { return 1 } return 2 }",
		"check",
		[]vm.Value{vm.EncodeBool(false)},
	)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 2 {
		t.Fatalf("expected 2, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_IfElseChoosesCorrectBranch(t *testing.T) {
	source := "fun check(flag: bool): int { if flag { return 1 } else { return 2 } }"

	trueResult, err := compileAndCall(t, source, "check", []vm.Value{vm.EncodeBool(true)})
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(trueResult) != 1 {
		t.Fatalf("expected true branch 1, got %d", vm.DecodeInt(trueResult))
	}

	falseResult, err := compileAndCall(t, source, "check", []vm.Value{vm.EncodeBool(false)})
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(falseResult) != 2 {
		t.Fatalf("expected false branch 2, got %d", vm.DecodeInt(falseResult))
	}
}

func TestInterpreter_NestedIfElseChoosesInnerBranch(t *testing.T) {
	source := `
fun check(a: bool, b: bool): int {
  if a {
    if b { return 1 } else { return 2 }
  }
  return 3
}`

	cases := []struct {
		name string
		args []vm.Value
		want int
	}{
		{name: "a_true_b_true", args: []vm.Value{vm.EncodeBool(true), vm.EncodeBool(true)}, want: 1},
		{name: "a_true_b_false", args: []vm.Value{vm.EncodeBool(true), vm.EncodeBool(false)}, want: 2},
		{name: "a_false_b_true", args: []vm.Value{vm.EncodeBool(false), vm.EncodeBool(true)}, want: 3},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := compileAndCall(t, source, "check", tc.args)
			if err != nil {
				t.Fatal(err)
			}
			if vm.DecodeInt(result) != int32(tc.want) {
				t.Fatalf("expected %d, got %d", tc.want, vm.DecodeInt(result))
			}
		})
	}
}

func TestInterpreter_LocalVarInitializationAndRead(t *testing.T) {
	result, err := compileAndCall(t,
		"fun calc(): int { var count: int = 7 return count }",
		"calc",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 7 {
		t.Fatalf("expected 7, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_LocalVarAssignmentUpdatesValue(t *testing.T) {
	result, err := compileAndCall(t,
		"fun calc(): int { var count: int = 1 count = 2 return count }",
		"calc",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 2 {
		t.Fatalf("expected 2, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_GlobalAssignmentThenReadBack(t *testing.T) {
	source := `
var count: int = 1
count = 9
fun read(): int { return count }`
	v := compileAndRun(t, source)
	fn := v.FuncReg().GetFunction("read")
	if fn == nil {
		t.Fatal("function 'read' not found")
	}
	res := fn.ExecuteBody(v, nil)
	if vm.DecodeInt(res) != 9 {
		t.Fatalf("expected 9, got %d", vm.DecodeInt(res))
	}
}

func TestInterpreter_AssignmentUsesLatestValue(t *testing.T) {
	result, err := compileAndCall(t,
		"fun calc(): int { var count: int = 1 count = count + 4 return count }",
		"calc",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 5 {
		t.Fatalf("expected 5, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_MultipleVarsPreserveAssignmentOrder(t *testing.T) {
	result, err := compileAndCall(t,
		"fun calc(): int { var a: int = 1 var b: int = 2 a = a + b b = a + b return b }",
		"calc",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 5 {
		t.Fatalf("expected 5, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_UndefinedVariableReturnsCompileError(t *testing.T) {
	prog, err := frontend.ParseModuleForTest("fun bad(): string { return missing }")
	if err != nil {
		t.Fatalf("expected parse to succeed before compile, got %v", err)
	}

	c := newCompiler()
	_, err = c.compile(prog)
	if err == nil {
		t.Fatal("expected undefined variable compile error")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "undefined_variable" {
		t.Fatalf("expected undefined_variable diagnostic, got %v", err)
	}
	pather, ok := err.(interface{ DiagnosticPath() string })
	if !ok || pather.DiagnosticPath() != "bytecode/scope/identifier" {
		t.Fatalf("expected bytecode/scope/identifier path, got %v", err)
	}
	if err.Error() == "" {
		t.Fatal("expected non-empty error message")
	}
}

func TestInterpreter_FunctionCall_PassesSingleArgument(t *testing.T) {
	argVM := vm.NewVM(4096, 256)
	result, err := compileAndCall(t,
		`fun greet(name: string): string { return name }`,
		"greet",
		[]vm.Value{argVM.EncodeString("alice")},
	)
	if err != nil {
		t.Fatal(err)
	}

	v := vm.NewVM(4096, 256)
	if v.DecodeString(result) != "alice" {
		t.Fatalf("expected alice, got %q", v.DecodeString(result))
	}
}

func TestInterpreter_FunctionCall_PassesMultipleArgs(t *testing.T) {
	result, err := compileAndCall(t,
		`fun add(a: int, b: int): int { return a + b }`,
		"add",
		[]vm.Value{vm.EncodeInt(7), vm.EncodeInt(5)},
	)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 12 {
		t.Fatalf("expected 12, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_FunctionCall_ExpressionBodyStringReturn(t *testing.T) {
	argVM := vm.NewVM(4096, 256)
	result, err := compileAndCall(t,
		`fun greet(name: string): string = name`,
		"greet",
		[]vm.Value{argVM.EncodeString("bob")},
	)
	if err != nil {
		t.Fatal(err)
	}

	v := vm.NewVM(4096, 256)
	if v.DecodeString(result) != "bob" {
		t.Fatalf("expected bob, got %q", v.DecodeString(result))
	}
}

func TestInterpreter_FunctionCall_BlockBodyReturn(t *testing.T) {
	result, err := compileAndCall(t,
		`fun wrap(x: int): int { return x }`,
		"wrap",
		[]vm.Value{vm.EncodeInt(99)},
	)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 99 {
		t.Fatalf("expected 99, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_ExpressionPrecedence_MulBeforeAdd(t *testing.T) {
	result, err := compileAndCall(t,
		"fun calc(): int { return 1 + 2 * 3 }",
		"calc",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 7 {
		t.Fatalf("expected 7, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_ExpressionPrecedence_ParenOverridesDefault(t *testing.T) {
	result, err := compileAndCall(t,
		"fun calc(): int { return (1 + 2) * 3 }",
		"calc",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 9 {
		t.Fatalf("expected 9, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_LogicalPrecedence_AndBeforeOr(t *testing.T) {
	source := "fun calc(a: bool, b: bool, c: bool): bool { return a && b || c }"
	cases := []struct {
		name string
		args []vm.Value
		want bool
	}{
		{
			name: "false_true_false",
			args: []vm.Value{vm.EncodeBool(false), vm.EncodeBool(true), vm.EncodeBool(false)},
			want: false,
		},
		{
			name: "true_true_false",
			args: []vm.Value{vm.EncodeBool(true), vm.EncodeBool(true), vm.EncodeBool(false)},
			want: true,
		},
		{
			name: "false_false_true",
			args: []vm.Value{vm.EncodeBool(false), vm.EncodeBool(false), vm.EncodeBool(true)},
			want: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := compileAndCall(t, source, "calc", tc.args)
			if err != nil {
				t.Fatal(err)
			}
			if vm.DecodeBool(result) != tc.want {
				t.Fatalf("expected %v, got %v", tc.want, vm.DecodeBool(result))
			}
		})
	}
}

func TestInterpreter_UnaryNegationBeforeCompare(t *testing.T) {
	source := "fun calc(a: bool, b: bool): bool { return !a == b }"
	cases := []struct {
		name string
		args []vm.Value
		want bool
	}{
		{
			name: "true_false",
			args: []vm.Value{vm.EncodeBool(true), vm.EncodeBool(false)},
			want: true,
		},
		{
			name: "false_false",
			args: []vm.Value{vm.EncodeBool(false), vm.EncodeBool(false)},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := compileAndCall(t, source, "calc", tc.args)
			if err != nil {
				t.Fatal(err)
			}
			if vm.DecodeBool(result) != tc.want {
				t.Fatalf("expected %v, got %v", tc.want, vm.DecodeBool(result))
			}
		})
	}
}

func TestInterpreter_IntAddition(t *testing.T) {
	result, err := compileAndCall(t,
		"fun add(a: int, b: int): int { return a + b }",
		"add",
		[]vm.Value{vm.EncodeInt(3), vm.EncodeInt(4)},
	)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 7 {
		t.Fatalf("expected 7, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_IntSubtraction(t *testing.T) {
	result, err := compileAndCall(t,
		"fun sub(a: int, b: int): int { return a - b }",
		"sub",
		[]vm.Value{vm.EncodeInt(10), vm.EncodeInt(3)},
	)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 7 {
		t.Fatalf("expected 7, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_FloatMultiplication(t *testing.T) {
	result, err := compileAndCall(t,
		"fun mul(a: float, b: float): float { return a * b }",
		"mul",
		[]vm.Value{vm.EncodeFloat(2.5), vm.EncodeFloat(4.0)},
	)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeFloat(result) != 10.0 {
		t.Fatalf("expected 10.0, got %f", vm.DecodeFloat(result))
	}
}

func TestInterpreter_Comparison(t *testing.T) {
	result, err := compileAndCall(t,
		"fun cmp(a: int, b: int): bool { return a < b }",
		"cmp",
		[]vm.Value{vm.EncodeInt(3), vm.EncodeInt(5)},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !vm.DecodeBool(result) {
		t.Fatal("expected true")
	}
}

func TestInterpreter_IfElse(t *testing.T) {
	source := "fun abs(x: int): int {\n  if x < 0 {\n    return -x\n  }\n  return x\n}"

	result, err := compileAndCall(t, source, "abs", []vm.Value{vm.EncodeInt(-5)})
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 5 {
		t.Fatalf("expected 5, got %d", vm.DecodeInt(result))
	}

	result, err = compileAndCall(t, source, "abs", []vm.Value{vm.EncodeInt(3)})
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 3 {
		t.Fatalf("expected 3, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_WhileLoop(t *testing.T) {
	// Use separate statements instead of semicolons which the parser may not handle.
	result, err := compileAndCall(t,
		"fun sum(n: int): int { var total: int = 0 var i: int = 0 while i < n { total = total + i i = i + 1 } return total }",
		"sum",
		[]vm.Value{vm.EncodeInt(5)},
	)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 10 { // 0+1+2+3+4
		t.Fatalf("expected 10, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_LogicalAnd(t *testing.T) {
	// Test with local variables to avoid short-circuit stack issues.
	result, err := compileAndCall(t,
		"fun both(a: bool, b: bool): bool { var c: bool = a && b return c }",
		"both",
		[]vm.Value{vm.EncodeBool(true), vm.EncodeBool(true)},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !vm.DecodeBool(result) {
		t.Fatal("expected true")
	}
}

func TestInterpreter_LogicalOr(t *testing.T) {
	result, err := compileAndCall(t,
		"fun either(a: bool, b: bool): bool { return a || b }",
		"either",
		[]vm.Value{vm.EncodeBool(false), vm.EncodeBool(true)},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !vm.DecodeBool(result) {
		t.Fatal("expected true")
	}
}

func TestInterpreter_Not(t *testing.T) {
	result, err := compileAndCall(t,
		"fun neg(a: bool): bool { return !a }",
		"neg",
		[]vm.Value{vm.EncodeBool(true)},
	)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeBool(result) {
		t.Fatal("expected false")
	}
}

func TestInterpreter_VarAssignment(t *testing.T) {
	result, err := compileAndCall(t,
		"fun f(): int { var x: int = 10 x = x + 5 return x }",
		"f",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 15 {
		t.Fatalf("expected 15, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_ForInLoopOverArray(t *testing.T) {
	result, err := compileAndCall(t,
		"fun sum(): int { var total: int = 0 var items: array = [10, 20, 30] for (item in items) { total = total + item } return total }",
		"sum",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 60 {
		t.Fatalf("expected 60, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_MapLiteral(t *testing.T) {
	source := "fun mk(): int { var m: map = {\"x\": 1, \"y\": 2} return 0 }"
	_, err := compileAndCall(t, source, "mk", nil)
	if err != nil {
		t.Fatal(err)
	}
}

func TestInterpreter_ForInLoopOverMap(t *testing.T) {
	result, err := compileAndCall(t,
		"fun sum(): int { var total: int = 0 var m: map = {\"a\": 1, \"b\": 2, \"c\": 3} for (k in m) { total = total + m[k] } return total }",
		"sum",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 6 {
		t.Fatalf("expected 6, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_ForInLoopOverMapKeys(t *testing.T) {
	result, v, err := compileAndCallWithVM(t,
		"fun keys(): string { var out: string = \"\" var m: map = {\"a\": 1, \"b\": 2, \"c\": 3} for (k in m) { out = out + k } return out }",
		"keys",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if v.DecodeString(result) != "abc" {
		t.Fatalf("expected %q, got %q", "abc", v.DecodeString(result))
	}
}

func TestInterpreter_ForInLoopOverString(t *testing.T) {
	result, err := compileAndCall(t,
		"fun count(): int { var n: int = 0 for (ch in \"hello\") { n = n + 1 } return n }",
		"count",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 5 {
		t.Fatalf("expected 5, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_ForInLoopOverStringConcatBytes(t *testing.T) {
	result, v, err := compileAndCallWithVM(t,
		"fun echo(): string { var out: string = \"\" for (ch in \"ab\") { out = out + ch } return out }",
		"echo",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if v.DecodeString(result) != "ab" {
		t.Fatalf("expected %q, got %q", "ab", v.DecodeString(result))
	}
}
func TestInterpreter_NewWithoutArgumentsAllocatesObject(t *testing.T) {
	result, err := compileAndCall(t,
		"class Counter { value: int }\nfun make(): bool { var c: Counter = new Counter() return c is Counter }",
		"make",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !vm.DecodeBool(result) {
		t.Fatal("expected zero-arg new to allocate a Counter")
	}
}

func TestInterpreter_NewWithConstructorArgumentsCompiles(t *testing.T) {
	prog, err := frontend.ParseModuleForTest("class Counter { value: int\nconstructor(v: int) { this.value = v } }\nfun make(): Counter { return new Counter(1) }")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	c := newCompiler()
	_, err = c.compile(prog)
	if err != nil {
		t.Fatalf("expected compilation to succeed with constructor arguments, got: %v", err)
	}
}

func TestInterpreter_InheritMissingParentStructuredDiagnostic(t *testing.T) {
	c := newCompiler()
	c.classes["Dog"] = classInfo{name: "Dog", parent: "MissingParent"}
	v := vm.NewVM(4096, 256)
	c.registerClasses(v)
	if len(c.errors) == 0 {
		t.Fatal("expected registration-time diagnostic for missing parent class")
	}
	err := c.errors[0]
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "parent_class_not_found" {
		t.Fatalf("expected parent_class_not_found diagnostic, got %v", err)
	}
	pather, ok := err.(interface{ DiagnosticPath() string })
	if !ok || pather.DiagnosticPath() != "bytecode/oop/inheritance" {
		t.Fatalf("expected bytecode/oop/inheritance path, got %v", err)
	}
}

func TestInterpreter_InheritNonOpenClassStructuredDiagnostic(t *testing.T) {
	c := newCompiler()
	c.classes["Animal"] = classInfo{name: "Animal", isOpen: false}
	c.classes["Dog"] = classInfo{name: "Dog", parent: "Animal"}
	v := vm.NewVM(4096, 256)
	c.registerClasses(v)
	if len(c.errors) == 0 {
		t.Fatal("expected registration-time diagnostic for inheriting non-open class")
	}
	err := c.errors[0]
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "inherit_non_open_class" {
		t.Fatalf("expected inherit_non_open_class diagnostic, got %v", err)
	}
	pather, ok := err.(interface{ DiagnosticPath() string })
	if !ok || pather.DiagnosticPath() != "bytecode/oop/inheritance" {
		t.Fatalf("expected bytecode/oop/inheritance path, got %v", err)
	}
}

func TestInterpreter_OverrideWithoutParentStructuredDiagnostic(t *testing.T) {
	c := newCompiler()
	c.classes["Dog"] = classInfo{name: "Dog", methods: []*frontend.FunStmt{{Name: &frontend.Ident{Value: "speak"}, IsOverride: true}}}
	v := vm.NewVM(4096, 256)
	c.registerClasses(v)
	if len(c.errors) == 0 {
		t.Fatal("expected registration-time diagnostic for override without parent")
	}
	err := c.errors[0]
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "override_without_parent" {
		t.Fatalf("expected override_without_parent diagnostic, got %v", err)
	}
	pather, ok := err.(interface{ DiagnosticPath() string })
	if !ok || pather.DiagnosticPath() != "bytecode/oop/override" {
		t.Fatalf("expected bytecode/oop/override path, got %v", err)
	}
}

func TestInterpreter_OverrideNonOpenParentMethodStructuredDiagnostic(t *testing.T) {
	c := newCompiler()
	c.classes["Animal"] = classInfo{name: "Animal", isOpen: true, methods: []*frontend.FunStmt{{Name: &frontend.Ident{Value: "speak"}, IsOpen: false}}}
	c.classes["Dog"] = classInfo{name: "Dog", parent: "Animal", methods: []*frontend.FunStmt{{Name: &frontend.Ident{Value: "speak"}, IsOverride: true}}}
	v := vm.NewVM(4096, 256)
	c.registerClasses(v)
	if len(c.errors) == 0 {
		t.Fatal("expected registration-time diagnostic for overriding non-open parent method")
	}
	err := c.errors[0]
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "override_parent_method_not_open" {
		t.Fatalf("expected override_parent_method_not_open diagnostic, got %v", err)
	}
	pather, ok := err.(interface{ DiagnosticPath() string })
	if !ok || pather.DiagnosticPath() != "bytecode/oop/override" {
		t.Fatalf("expected bytecode/oop/override path, got %v", err)
	}
}

func TestInterpreter_OverrideMissingMethodStructuredDiagnostic(t *testing.T) {
	c := newCompiler()
	c.classes["Animal"] = classInfo{name: "Animal", isOpen: true, methods: []*frontend.FunStmt{{Name: &frontend.Ident{Value: "speak"}, IsOpen: true}}}
	c.classes["Dog"] = classInfo{name: "Dog", parent: "Animal", methods: []*frontend.FunStmt{{Name: &frontend.Ident{Value: "bark"}, IsOverride: true}}}
	v := vm.NewVM(4096, 256)
	c.registerClasses(v)
	if len(c.errors) == 0 {
		t.Fatal("expected registration-time diagnostic for overriding missing method")
	}
	err := c.errors[0]
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "override_method_not_found" {
		t.Fatalf("expected override_method_not_found diagnostic, got %v", err)
	}
	pather, ok := err.(interface{ DiagnosticPath() string })
	if !ok || pather.DiagnosticPath() != "bytecode/oop/override" {
		t.Fatalf("expected bytecode/oop/override path, got %v", err)
	}
}

func TestInterpreter_UnknownInterfaceStructuredDiagnostic(t *testing.T) {
	c := newCompiler()
	c.classes["Dog"] = classInfo{name: "Dog", implements: []string{"MissingInterface"}}
	v := vm.NewVM(4096, 256)
	c.registerClasses(v)
	if len(c.errors) == 0 {
		t.Fatal("expected registration-time diagnostic for unknown interface")
	}
	err := c.errors[0]
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "unknown_interface" {
		t.Fatalf("expected unknown_interface diagnostic, got %v", err)
	}
	pather, ok := err.(interface{ DiagnosticPath() string })
	if !ok || pather.DiagnosticPath() != "bytecode/oop/interface" {
		t.Fatalf("expected bytecode/oop/interface path, got %v", err)
	}
}

func TestInterpreter_InterfaceMethodMissingStructuredDiagnostic(t *testing.T) {
	c := newCompiler()
	c.interfaces["Greeter"] = interfaceInfo{name: "Greeter", methods: []*frontend.MethodSignature{{Name: &frontend.Ident{Value: "greet"}}}}
	c.classes["Dog"] = classInfo{name: "Dog", implements: []string{"Greeter"}, methods: []*frontend.FunStmt{{Name: &frontend.Ident{Value: "bark"}}}}
	v := vm.NewVM(4096, 256)
	c.registerClasses(v)
	if len(c.errors) == 0 {
		t.Fatal("expected registration-time diagnostic for missing interface method")
	}
	err := c.errors[0]
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "interface_method_missing" {
		t.Fatalf("expected interface_method_missing diagnostic, got %v", err)
	}
	pather, ok := err.(interface{ DiagnosticPath() string })
	if !ok || pather.DiagnosticPath() != "bytecode/oop/interface" {
		t.Fatalf("expected bytecode/oop/interface path, got %v", err)
	}
}

func TestInterpreter_InterfaceDefaultMethodRegisteredOnClass(t *testing.T) {
	v := compileAndRun(t, `
interface IGreet { fun greet(): string { return "default" } }
class Person : IGreet {}
`)
	if !v.IfaceReg().IsInstanceOfInterface("Person", "IGreet", v.ClassReg()) {
		t.Fatal("expected Person registered as IGreet implementor")
	}
	cls := v.ClassReg().GetClassByName("Person")
	if cls == nil {
		t.Fatal("class Person not registered")
	}
	if cls.GetMethod("greet") == nil {
		t.Fatal("expected inherited interface default method greet on Person (vtable/method registration)")
	}
}

func TestInterpreter_InterfaceDefaultMethodWithoutDefaultStillReportsMissing(t *testing.T) {
	_, _, err := compileSourceAllowError(t, `
interface IGreet { fun greet(): string }
class Person : IGreet {}
fun run(): string { return "x" }
`)
	if err == nil {
		t.Fatal("expected compile error when interface method lacks default and class lacks implementation")
	}
	if !strings.Contains(err.Error(), "interface_method_missing") && !strings.Contains(err.Error(), "missing method") {
		t.Fatalf("expected interface_method_missing diagnostic, got %v", err)
	}
}

func TestInterpreter_OverrideSignatureMismatchStructuredDiagnostic(t *testing.T) {
	c := newCompiler()
	c.classes["Animal"] = classInfo{name: "Animal", isOpen: true, methods: []*frontend.FunStmt{{Name: &frontend.Ident{Value: "speak"}, ReturnType: &frontend.TypeAnnotation{Name: "string"}, IsOpen: true}}}
	c.classes["Dog"] = classInfo{name: "Dog", parent: "Animal", methods: []*frontend.FunStmt{{Name: &frontend.Ident{Value: "speak"}, Params: []*frontend.Param{{Name: &frontend.Ident{Value: "volume"}}}, ReturnType: &frontend.TypeAnnotation{Name: "string"}, IsOverride: true}}}
	v := vm.NewVM(4096, 256)
	c.registerClasses(v)
	if len(c.errors) == 0 {
		t.Fatal("expected registration-time diagnostic for override signature mismatch")
	}
	err := c.errors[0]
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "override_signature_mismatch" {
		t.Fatalf("expected override_signature_mismatch diagnostic, got %v", err)
	}
	pather, ok := err.(interface{ DiagnosticPath() string })
	if !ok || pather.DiagnosticPath() != "bytecode/oop/override" {
		t.Fatalf("expected bytecode/oop/override path, got %v", err)
	}
}

func TestInterpreter_InterfaceSignatureMismatchStructuredDiagnostic(t *testing.T) {
	c := newCompiler()
	c.interfaces["Greeter"] = interfaceInfo{name: "Greeter", methods: []*frontend.MethodSignature{{Name: &frontend.Ident{Value: "greet"}}}}
	c.classes["Dog"] = classInfo{name: "Dog", implements: []string{"Greeter"}, methods: []*frontend.FunStmt{{Name: &frontend.Ident{Value: "greet"}, Params: []*frontend.Param{{Name: &frontend.Ident{Value: "volume"}}}}}}
	v := vm.NewVM(4096, 256)
	c.registerClasses(v)
	if len(c.errors) == 0 {
		t.Fatal("expected registration-time diagnostic for interface signature mismatch")
	}
	err := c.errors[0]
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "interface_signature_mismatch" {
		t.Fatalf("expected interface_signature_mismatch diagnostic, got %v", err)
	}
	pather, ok := err.(interface{ DiagnosticPath() string })
	if !ok || pather.DiagnosticPath() != "bytecode/oop/interface" {
		t.Fatalf("expected bytecode/oop/interface path, got %v", err)
	}
}

func TestInterpreter_BreakOutsideLoopFailsCompile(t *testing.T) {
	prog, err := frontend.ParseModuleForTest("fun bad(): void { break }")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	c := newCompiler()
	_, err = c.compile(prog)
	if err == nil {
		t.Fatal("expected compile error for break outside loop")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "break_outside_loop" {
		t.Fatalf("expected break_outside_loop diagnostic, got %v", err)
	}
	pather, ok := err.(interface{ DiagnosticPath() string })
	if !ok || pather.DiagnosticPath() != "bytecode/control/loop" {
		t.Fatalf("expected bytecode/control/loop path, got %v", err)
	}
}

func TestInterpreter_ContinueOutsideLoopFailsCompile(t *testing.T) {
	prog, err := frontend.ParseModuleForTest("fun bad(): void { continue }")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	c := newCompiler()
	_, err = c.compile(prog)
	if err == nil {
		t.Fatal("expected compile error for continue outside loop")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "continue_outside_loop" {
		t.Fatalf("expected continue_outside_loop diagnostic, got %v", err)
	}
	pather, ok := err.(interface{ DiagnosticPath() string })
	if !ok || pather.DiagnosticPath() != "bytecode/control/loop" {
		t.Fatalf("expected bytecode/control/loop path, got %v", err)
	}
}

func TestInterpreter_NestedFunctionCalls(t *testing.T) {
	source := "fun twice(x: int): int { return x * 2 }\nfun quad(x: int): int { return twice(twice(x)) }"
	result, err := compileAndCall(t, source, "quad", []vm.Value{vm.EncodeInt(3)})
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 12 {
		t.Fatalf("expected 12, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_ForLoopParsesAndCompiles(t *testing.T) {
	prog, err := frontend.ParseModuleForTest("fun f(): int { for (var i: int = 0; i < 5; i = i + 1) { } return 42 }")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	c := newCompiler()
	if _, err := c.compile(prog); err != nil {
		t.Fatalf("compile error: %v", err)
	}
}

func TestInterpreter_FieldAssignment(t *testing.T) {
	result, err := compileAndCall(t,
		"class Counter { value: int }\nfun set(): int { var c: Counter = new Counter() c.value = 9 return c.value }",
		"set",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 9 {
		t.Fatalf("expected 9, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_IndexAssignment(t *testing.T) {
	result, err := compileAndCall(t,
		"fun set(): int { var items: array<int> = [1, 2, 3] items[1] = 7 return items[1] }",
		"set",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 7 {
		t.Fatalf("expected 7, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_Equality(t *testing.T) {
	result, err := compileAndCall(t,
		"fun eq(a: int, b: int): bool { return a == b }",
		"eq",
		[]vm.Value{vm.EncodeInt(5), vm.EncodeInt(5)},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !vm.DecodeBool(result) {
		t.Fatal("expected true for equal values")
	}
}

func TestInterpreter_Inequality(t *testing.T) {
	result, err := compileAndCall(t,
		"fun ne(a: int, b: int): bool { return a != b }",
		"ne",
		[]vm.Value{vm.EncodeInt(3), vm.EncodeInt(7)},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !vm.DecodeBool(result) {
		t.Fatal("expected true for unequal values")
	}
}

func TestInterpreter_GreaterOrEqual(t *testing.T) {
	result, err := compileAndCall(t,
		"fun ge(a: int, b: int): bool { return a >= b }",
		"ge",
		[]vm.Value{vm.EncodeInt(5), vm.EncodeInt(5)},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !vm.DecodeBool(result) {
		t.Fatal("expected true for 5 >= 5")
	}
}

func TestInterpreter_AsFailureIsDiagnosable(t *testing.T) {
	// Successful cast should not error.
	_, err := compileAndCall(t,
		"class Counter { value: int }\nfun check(): bool { var c: Counter = new Counter() return c as Counter }",
		"check",
		nil,
	)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}

	// Failed cast should produce a structured RuntimeError.
	_, err = compileAndCall(t,
		"class Alpha { x: int }\nclass Beta { y: int }\nfun fail(): bool { var a: Alpha = new Alpha() return a as Beta }",
		"fail",
		nil,
	)
	if err == nil {
		t.Fatal("expected type cast failure error")
	}
	rtErr, ok := err.(*RuntimeError)
	if !ok {
		t.Fatalf("expected *RuntimeError, got %T: %v", err, err)
	}
	if rtErr.Code != "type_cast_failed" {
		t.Fatalf("expected diagnostic code type_cast_failed, got %q", rtErr.Code)
	}
	if rtErr.Target != "Beta" {
		t.Fatalf("expected target type Beta, got %q", rtErr.Target)
	}
	if !strings.Contains(rtErr.Message, "Beta") {
		t.Fatalf("error message should mention target type 'Beta', got: %q", rtErr.Message)
	}
}

func TestInterpreter_IntDivision(t *testing.T) {
	result, err := compileAndCall(t,
		"fun div(a: int, b: int): int { return a / b }",
		"div",
		[]vm.Value{vm.EncodeInt(10), vm.EncodeInt(3)},
	)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 3 {
		t.Fatalf("expected 3, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_Modulo(t *testing.T) {
	result, err := compileAndCall(t,
		"fun mod(a: int, b: int): int { return a % b }",
		"mod",
		[]vm.Value{vm.EncodeInt(10), vm.EncodeInt(3)},
	)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 1 {
		t.Fatalf("expected 1, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_Negation(t *testing.T) {
	result, err := compileAndCall(t,
		"fun neg(x: int): int { return -x }",
		"neg",
		[]vm.Value{vm.EncodeInt(5)},
	)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != -5 {
		t.Fatalf("expected -5, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_FloatAddition(t *testing.T) {
	result, err := compileAndCall(t,
		"fun fadd(a: float, b: float): float { return a + b }",
		"fadd",
		[]vm.Value{vm.EncodeFloat(2.5), vm.EncodeFloat(3.5)},
	)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeFloat(result) != 6.0 {
		t.Fatalf("expected 6.0, got %f", vm.DecodeFloat(result))
	}
}

func TestInterpreter_FloatComparison(t *testing.T) {
	result, err := compileAndCall(t,
		"fun flt(a: float, b: float): bool { return a < b }",
		"flt",
		[]vm.Value{vm.EncodeFloat(1.5), vm.EncodeFloat(2.5)},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !vm.DecodeBool(result) {
		t.Fatal("expected true for 1.5 < 2.5")
	}
}

func TestInterpreter_LessOrEqual(t *testing.T) {
	result, err := compileAndCall(t,
		"fun le(a: int, b: int): bool { return a <= b }",
		"le",
		[]vm.Value{vm.EncodeInt(3), vm.EncodeInt(3)},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !vm.DecodeBool(result) {
		t.Fatal("expected true for 3 <= 3")
	}
}

func TestInterpreter_GreaterThan(t *testing.T) {
	result, err := compileAndCall(t,
		"fun gt(a: int, b: int): bool { return a > b }",
		"gt",
		[]vm.Value{vm.EncodeInt(7), vm.EncodeInt(3)},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !vm.DecodeBool(result) {
		t.Fatal("expected true for 7 > 3")
	}
}

func TestInterpreter_StringConcatenation(t *testing.T) {
	// Use literals to avoid argument passing complexity for string args.
	result, err := compileAndCall(t,
		"fun hi(): string { return \"hel\" + \"lo\" }",
		"hi",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !vm.IsString(result) {
		t.Fatalf("expected string value, got type tag %v", result>>56)
	}
}

func TestInterpreter_DivisionByZero(t *testing.T) {
	_, err := compileAndCall(t,
		"fun bad(): int { return 1 / 0 }",
		"bad",
		nil,
	)
	if err == nil {
		t.Fatal("expected division by zero error")
	}
	if !strings.Contains(err.Error(), "division by zero") {
		t.Fatalf("expected division by zero error, got: %v", err)
	}
}

func TestInterpreter_WhenTypeMatchFallthroughDiagnosable(t *testing.T) {
	// when with type match that doesn't match should fall through without panic.
	result, err := compileAndCall(t,
		"fun check(x: int): int { when (x) { case 1 { return 10 } case 2 { return 20 } } return 0 }",
		"check",
		[]vm.Value{vm.EncodeInt(99)},
	)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 0 {
		t.Fatalf("expected 0 (fallthrough), got %d", vm.DecodeInt(result))
	}
}

// --- Batch 2: Map CRUD from source ---

func TestInterpreter_StreamMultipleCallablesDoNotLeakSessions(t *testing.T) {
	eval := NewVMEvaluator()
	prog, err := frontend.ParseModuleForTest(`
stream fun emitA(): int { yield 1 return 2 }
stream fun emitB(): int { yield 10 return 20 }
`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if err := eval.CompileProgram(prog); err != nil {
		t.Fatalf("CompileProgram: %v", err)
	}

	aNext, err := eval.Evaluate("emitA", binding.InvocationStageNext, nil)
	if err != nil {
		t.Fatalf("emitA next: %v", err)
	}
	if aNext != 1 {
		t.Fatalf("expected emitA next 1, got %v", aNext)
	}

	bNext, err := eval.Evaluate("emitB", binding.InvocationStageNext, nil)
	if err != nil {
		t.Fatalf("emitB next: %v", err)
	}
	if bNext != 10 {
		t.Fatalf("expected emitB next 10, got %v", bNext)
	}

	aFinal, err := eval.Evaluate("emitA", binding.InvocationStageFinal, nil)
	if err != nil {
		t.Fatalf("emitA final: %v", err)
	}
	if aFinal != 2 {
		t.Fatalf("expected emitA final 2, got %v", aFinal)
	}

	bFinal, err := eval.Evaluate("emitB", binding.InvocationStageFinal, nil)
	if err != nil {
		t.Fatalf("emitB final: %v", err)
	}
	if bFinal != 20 {
		t.Fatalf("expected emitB final 20, got %v", bFinal)
	}
}

func TestInterpreter_StreamLocalsPersistAcrossYields(t *testing.T) {
	eval := NewVMEvaluator()
	prog, err := frontend.ParseModuleForTest(`
stream fun emit(): int {
  var x: int = 1
  yield x
  x = x + 1
  yield x
  return x + 1
}`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if err := eval.CompileProgram(prog); err != nil {
		t.Fatalf("CompileProgram: %v", err)
	}

	first, err := eval.Evaluate("emit", binding.InvocationStageNext, nil)
	if err != nil {
		t.Fatalf("first next: %v", err)
	}
	if first != 1 {
		t.Fatalf("expected first next 1, got %v", first)
	}

	second, err := eval.Evaluate("emit", binding.InvocationStageNext, nil)
	if err != nil {
		t.Fatalf("second next: %v", err)
	}
	if second != 2 {
		t.Fatalf("expected second next 2, got %v", second)
	}

	final, err := eval.Evaluate("emit", binding.InvocationStageFinal, nil)
	if err != nil {
		t.Fatalf("final: %v", err)
	}
	if final != 3 {
		t.Fatalf("expected final 3, got %v", final)
	}
}

func TestInterpreter_MapInvalidKeyTypeReturnsStructuredError(t *testing.T) {
	_, err := compileAndCall(t, `fun read(): int { return {"x": 1}[true] }`, "read", nil)
	if err == nil {
		t.Fatal("expected invalid map key type error")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "invalid_map_key_type" {
		t.Fatalf("expected invalid_map_key_type diagnostic, got %v", err)
	}
	pather, ok := err.(interface{ DiagnosticPath() string })
	if !ok || pather.DiagnosticPath() != "vm/map/key_type" {
		t.Fatalf("expected vm/map/key_type path, got %v", err)
	}
}

func TestInterpreter_ArrayInvalidIndexTypeReturnsStructuredError(t *testing.T) {
	_, err := compileAndCall(t, `fun read(): int { return [10, 20][true] }`, "read", nil)
	if err == nil {
		t.Fatal("expected invalid array index type error")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "invalid_array_index_type" {
		t.Fatalf("expected invalid_array_index_type diagnostic, got %v", err)
	}
	pather, ok := err.(interface{ DiagnosticPath() string })
	if !ok || pather.DiagnosticPath() != "vm/array/index_type" {
		t.Fatalf("expected vm/array/index_type path, got %v", err)
	}
}

func TestInterpreter_ArrayOutOfRangeReturnsStructuredError(t *testing.T) {
	_, err := compileAndCall(t, `fun read(): int { return [10, 20][99] }`, "read", nil)
	if err == nil {
		t.Fatal("expected array out-of-range error")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "array_index_out_of_range" {
		t.Fatalf("expected array_index_out_of_range diagnostic, got %v", err)
	}
	pather, ok := err.(interface{ DiagnosticPath() string })
	if !ok || pather.DiagnosticPath() != "vm/array/index" {
		t.Fatalf("expected vm/array/index path, got %v", err)
	}
}

func TestInterpreter_MapMissingKeyReturnsStructuredError(t *testing.T) {
	source := `fun read(): int { return {"x": 1}["missing"] }`
	_, err := compileAndCall(t, source, "read", nil)
	if err == nil {
		t.Fatal("expected missing-key error")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "map_key_not_found" {
		t.Fatalf("expected map_key_not_found diagnostic, got %v", err)
	}
	pather, ok := err.(interface{ DiagnosticPath() string })
	if !ok || pather.DiagnosticPath() != "vm/map/key" {
		t.Fatalf("expected vm/map/key path, got %v", err)
	}
}

func TestInterpreter_StructLiteralMissingFieldStructuredDiagnostic(t *testing.T) {
	source := `
struct Point {
  x: int
  y: int
}
fun read(): int {
  var p: Point = Point{x: 1}
  return p.y
}`
	prog, err := frontend.ParseModuleForTest(source)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	c := newCompiler()
	_, err = c.compile(prog)
	if err == nil {
		t.Fatal("expected compile diagnostic for missing struct field")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "missing_struct_field" {
		t.Fatalf("expected missing_struct_field diagnostic, got %v", err)
	}
	pather, ok := err.(interface{ DiagnosticPath() string })
	if !ok || pather.DiagnosticPath() != "bytecode/struct/literal" {
		t.Fatalf("expected bytecode/struct/literal path, got %v", err)
	}
}

func TestInterpreter_StructLiteralUnknownFieldStructuredDiagnostic(t *testing.T) {
	source := `
struct Point {
  x: int
  y: int
}
fun read(): int {
  var p: Point = Point{x: 1, z: 2}
  return p.x
}`
	prog, err := frontend.ParseModuleForTest(source)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	c := newCompiler()
	_, err = c.compile(prog)
	if err == nil {
		t.Fatal("expected compile diagnostic for unknown struct field")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "unknown_struct_field" {
		t.Fatalf("expected unknown_struct_field diagnostic, got %v", err)
	}
	pather, ok := err.(interface{ DiagnosticPath() string })
	if !ok || pather.DiagnosticPath() != "bytecode/struct/literal" {
		t.Fatalf("expected bytecode/struct/literal path, got %v", err)
	}
}

func TestInterpreter_StructLiteralFieldTypeMismatchStructuredDiagnostic(t *testing.T) {
	source := `
struct Point {
  x: int
  y: int
}
fun read(): Point {
  return Point{x: "oops", y: 2}
}`
	prog, err := frontend.ParseModuleForTest(source)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	c := newCompiler()
	_, err = c.compile(prog)
	if err == nil {
		t.Fatal("expected compile diagnostic for struct field type mismatch")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "struct_field_type_mismatch" {
		t.Fatalf("expected struct_field_type_mismatch diagnostic, got %v", err)
	}
	pather, ok := err.(interface{ DiagnosticPath() string })
	if !ok || pather.DiagnosticPath() != "bytecode/struct/literal" {
		t.Fatalf("expected bytecode/struct/literal path, got %v", err)
	}
}

func TestInterpreter_StructLiteralDuplicateFieldStructuredDiagnostic(t *testing.T) {
	source := `
struct Point {
  x: int
  y: int
}
fun read(): int {
  var p: Point = Point{x: 1, x: 2, y: 3}
  return p.x
}`
	prog, err := frontend.ParseModuleForTest(source)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	c := newCompiler()
	_, err = c.compile(prog)
	if err == nil {
		t.Fatal("expected compile diagnostic for duplicate struct field")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "duplicate_struct_field" {
		t.Fatalf("expected duplicate_struct_field diagnostic, got %v", err)
	}
	pather, ok := err.(interface{ DiagnosticPath() string })
	if !ok || pather.DiagnosticPath() != "bytecode/struct/literal" {
		t.Fatalf("expected bytecode/struct/literal path, got %v", err)
	}
}

func TestInterpreter_MapGetFromSource(t *testing.T) {
	result, err := compileAndCall(t,
		"fun get(): int { var m: map = {\"x\": 10} return m[\"x\"] }",
		"get",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 10 {
		t.Fatalf("expected 10, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_MapSetFromSource(t *testing.T) {
	result, err := compileAndCall(t,
		"fun set(): int { var m: map = {} m[\"k\"] = 42 return m[\"k\"] }",
		"set",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 42 {
		t.Fatalf("expected 42, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_MapOverwrite(t *testing.T) {
	result, err := compileAndCall(t,
		"fun ow(): int { var m: map = {\"k\": 1} m[\"k\"] = 2 return m[\"k\"] }",
		"ow",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 2 {
		t.Fatalf("expected 2, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_MapDeleteBuiltinRemovesKey(t *testing.T) {
	source := `
fun remove(): int {
  var m: map<string, int> = {"a": 1, "b": 2, "c": 3}
  delete(m, "b")
  return len(m)
}`
	result, err := compileAndCall(t, source, "remove", nil)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 2 {
		t.Fatalf("expected map length 2 after delete, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_MapDeleteBuiltinMakesKeyInaccessible(t *testing.T) {
	source := `
fun access(): int {
  var m: map<string, int> = {"a": 1, "b": 2}
  delete(m, "a")
  return m["a"]
}`
	_, err := compileAndCall(t, source, "access", nil)
	if err == nil {
		t.Fatal("expected missing-key error after delete")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "map_key_not_found" {
		t.Fatalf("expected map_key_not_found diagnostic, got %v", err)
	}
}

func TestInterpreter_MapDeleteBuiltinWorksWithDynamicKey(t *testing.T) {
	source := `
fun remove(key: string): int {
  var m: map<string, int> = {"a": 1, "b": 2}
  delete(m, key)
  return len(m)
}`
	argVM := vm.NewVM(4096, 256)
	result, err := compileAndCall(t, source, "remove", []vm.Value{argVM.EncodeString("b")})
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 1 {
		t.Fatalf("expected map length 1 after dynamic delete, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_MapMissingKeyReturnsStructuredError_EmptyMap(t *testing.T) {
	_, err := compileAndCall(t,
		"fun miss(): int { var m: map = {} return m[\"nope\"] }",
		"miss",
		nil,
	)
	if err == nil {
		t.Fatal("expected missing-key error")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "map_key_not_found" {
		t.Fatalf("expected map_key_not_found diagnostic, got %v", err)
	}
}

func TestInterpreter_LenArrayLiteral(t *testing.T) {
	result, err := compileAndCall(t,
		"fun size(): int { return len([1, 2, 3]) }",
		"size",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 3 {
		t.Fatalf("expected array length 3, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_LenMapLiteral(t *testing.T) {
	result, err := compileAndCall(t,
		"fun size(): int { return len({\"a\": 1, \"b\": 2}) }",
		"size",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 2 {
		t.Fatalf("expected map length 2, got %d", vm.DecodeInt(result))
	}
}

// --- Batch 3: Struct literal & loop execution completeness ---

func TestInterpreter_StructLiteralCreation(t *testing.T) {
	result, err := compileAndCall(t,
		"struct Point { x: int y: int }\nfun mk(): int { var p: Point = Point{x: 3, y: 4} return p.x }",
		"mk",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 3 {
		t.Fatalf("expected 3, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_StructLiteralMissingFieldStructuredDiagnostic_DefaultFieldPath(t *testing.T) {
	prog, err := frontend.ParseModuleForTest(
		"struct Point { x: int y: int }\nfun mk(): int { var p: Point = Point{x: 5} return p.x }",
	)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	c := newCompiler()
	_, err = c.compile(prog)
	if err == nil {
		t.Fatal("expected compile diagnostic for missing default field")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "missing_struct_field" {
		t.Fatalf("expected missing_struct_field diagnostic, got %v", err)
	}
}

func TestInterpreter_CStyleForExecution(t *testing.T) {
	result, err := compileAndCall(t,
		"fun sum(n: int): int { var total: int = 0 for (var i: int = 0; i < n; i = i + 1) { total = total + i } return total }",
		"sum",
		[]vm.Value{vm.EncodeInt(5)},
	)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 10 { // 0+1+2+3+4
		t.Fatalf("expected 10, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_BreakInLoop(t *testing.T) {
	result, err := compileAndCall(t,
		"fun early(): int { var total: int = 0 for (var i: int = 0; i < 10; i = i + 1) { if i == 3 { break } total = total + i } return total }",
		"early",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	// 0 + 1 + 2 = 3 (breaks before adding 3)
	if vm.DecodeInt(result) != 3 {
		t.Fatalf("expected 3, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_ContinueInLoop(t *testing.T) {
	result, err := compileAndCall(t,
		"fun skip(): int { var total: int = 0 for (var i: int = 0; i < 5; i = i + 1) { if i == 2 { continue } total = total + i } return total }",
		"skip",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	// 0 + 1 + 3 + 4 = 8 (skips 2)
	if vm.DecodeInt(result) != 8 {
		t.Fatalf("expected 8, got %d", vm.DecodeInt(result))
	}
}

// --- Batch 4: Control flow completeness & recursion ---

func TestInterpreter_WhenWithDefault(t *testing.T) {
	result, err := compileAndCall(t,
		"fun classify(x: int): int { when (x) { case 1 { return 10 } else { return 0 } } }",
		"classify",
		[]vm.Value{vm.EncodeInt(5)},
	)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 0 {
		t.Fatalf("expected 0 (else branch), got %d", vm.DecodeInt(result))
	}

	// Verify the case branch still works.
	result, err = compileAndCall(t,
		"fun classify(x: int): int { when (x) { case 1 { return 10 } else { return 0 } } }",
		"classify",
		[]vm.Value{vm.EncodeInt(1)},
	)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 10 {
		t.Fatalf("expected 10 (case branch), got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_RecursiveFactorial(t *testing.T) {
	result, err := compileAndCall(t,
		"fun fact(n: int): int { if n <= 1 { return 1 } return n * fact(n - 1) }",
		"fact",
		[]vm.Value{vm.EncodeInt(5)},
	)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 120 {
		t.Fatalf("expected 120, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_NestedControlFlow(t *testing.T) {
	// Sum even numbers from n down to 1: for n=6, that's 6+4+2 = 12
	result, err := compileAndCall(t,
		"fun sum_even(n: int): int { var total: int = 0 while n > 0 { if n % 2 == 0 { total = total + n } n = n - 1 } return total }",
		"sum_even",
		[]vm.Value{vm.EncodeInt(6)},
	)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 12 {
		t.Fatalf("expected 12, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_MethodCallFromSource(t *testing.T) {
	result, err := compileAndCall(t,
		"class Counter { value: int fun get(): int { return this.value } }\nfun read(): int { var c: Counter = new Counter() c.value = 7 return c.get() }",
		"read",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 7 {
		t.Fatalf("expected 7, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_GlobalVariableAccess(t *testing.T) {
	// Use compileAndRun to test global variable initialization and access.
	v := compileAndRun(t, "var g: int = 10\nfun read(): int { return g }")

	fnChunks := v.FuncReg().GetFunction("read")
	if fnChunks == nil {
		t.Fatal("function 'read' not found")
	}
	// The function should have been compiled and registered.
	// Test by invoking through the function registry.
	result := fnChunks.ExecuteBody(v, nil)
	if vm.DecodeInt(result) != 10 {
		t.Fatalf("expected 10, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_LocalHeapValueSurvivesGCTrigger(t *testing.T) {
	source := `
struct Box { value: int }
fun read(): int {
  var box: Box = Box{value: 77}
  var noise: array<int> = []
  for (var i: int = 0; i < 64; i = i + 1) {
    noise = [i, i + 1, i + 2, i + 3]
  }
  return box.value
}`

	result, err := compileAndCall(t, source, "read", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 77 {
		t.Fatalf("expected rooted local struct value 77 after GC pressure, got %d", got)
	}
}

func TestInterpreter_GlobalHeapValueSurvivesGCTrigger(t *testing.T) {
	// Globals are initialized by the program's main chunk, so the test must use
	// compileAndRun (which executes main) instead of compileAndCall (which only
	// invokes the named function and skips global init). The heap-allocated
	// global is then exercised through the VM function registry.
	source := `
struct Box { value: int }
var box: Box = Box{value: 77}
fun read(): int {
  var noise: array<int> = []
  for (var i: int = 0; i < 64; i = i + 1) {
    noise = [i, i + 1, i + 2, i + 3]
  }
  return box.value
}`

	v := compileAndRun(t, source)
	fn := v.FuncReg().GetFunction("read")
	if fn == nil {
		t.Fatal("function read not registered")
	}
	result := fn.ExecuteBody(v, nil)
	if got := vm.DecodeInt(result); got != 77 {
		t.Fatalf("expected rooted global struct value 77 after GC pressure, got %d", got)
	}
}

func TestInterpreter_StreamSessionHeapStateSurvivesGCTrigger(t *testing.T) {
	prog, err := frontend.ParseModuleForTest(`
struct Box { value: int }
stream fun emit(): int {
  var box: Box = Box{value: 123}
  yield 1
  return box.value
}`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	eval := NewVMEvaluator()
	if err := eval.CompileProgram(prog); err != nil {
		t.Fatalf("CompileProgram: %v", err)
	}

	first, err := eval.Evaluate("emit", binding.InvocationStageNext, nil)
	if err != nil {
		t.Fatalf("next: %v", err)
	}
	if first.(int) != 1 {
		t.Fatalf("expected first yield 1, got %v", first)
	}

	for i := 0; i < 64; i++ {
		_ = eval.vm_.NewArray(vm.TypeID(1), 4)
	}

	final, err := eval.Evaluate("emit", binding.InvocationStageFinal, nil)
	if err != nil {
		t.Fatalf("final after GC pressure: %v", err)
	}
	if final.(int) != 123 {
		t.Fatalf("expected final 123 from suspended stream state, got %v", final)
	}
}

func TestInterpreter_ScenarioInventoryTotalAggregation(t *testing.T) {
	source := `
struct Item {
  name: string
  qty: int
  price: int
}
fun total(): int {
  var items: array<Item> = [
    Item{name: "apple", qty: 2, price: 5},
    Item{name: "stone", qty: 0, price: 9},
    Item{name: "bread", qty: 3, price: 7}
  ]
  var sum: int = 0
  for (item in items) {
    if item.qty > 0 {
      sum = sum + item.qty * item.price
    }
  }
  return sum
}`

	result, err := compileAndCall(t, source, "total", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 31 {
		t.Fatalf("expected total 31, got %d", got)
	}
}

func TestInterpreter_ScenarioMapBackedStatusClassification(t *testing.T) {
	source := `
fun classify(hp: int, shield: int): int {
  var stats: map<string, int> = {"hp": hp, "shield": shield}
  stats["total"] = stats["hp"] + stats["shield"]
  when (stats["total"]) {
    case 0, 1, 2 { return 0 }
    case 3, 4, 5, 6 { return 1 }
    else { return 2 }
  }
}`

	cases := []struct {
		name   string
		hp     int32
		shield int32
		want   int32
	}{
		{name: "low", hp: 1, shield: 1, want: 0},
		{name: "mid", hp: 4, shield: 1, want: 1},
		{name: "high", hp: 5, shield: 3, want: 2},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := compileAndCall(t, source, "classify", []vm.Value{vm.EncodeInt(tc.hp), vm.EncodeInt(tc.shield)})
			if err != nil {
				t.Fatal(err)
			}
			if got := vm.DecodeInt(result); got != tc.want {
				t.Fatalf("expected class %d, got %d", tc.want, got)
			}
		})
	}
}

func TestInterpreter_ScenarioPolymorphicDispatchWorkflow(t *testing.T) {
	source := `
open class Rule {
  open fun score(): int { return 10 }
}
class BonusRule : Rule {
  override fun score(): int { return super.score() + 5 }
}
fun apply(): int {
  var rule: BonusRule = new BonusRule()
  return rule.score() * 2
}`

	result, err := compileAndCall(t, source, "apply", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 30 {
		t.Fatalf("expected polymorphic result 30, got %d", got)
	}
}

func TestInterpreter_ScenarioNestedStateUpdateWorkflow(t *testing.T) {
	source := `
struct Point {
  x: int
  y: int
}
struct Player {
  pos: Point
  hp: int
}
fun advance(): int {
  var p: Player = Player{pos: Point{x: 1, y: 2}, hp: 5}
  for (var i: int = 0; i < 3; i = i + 1) {
    p.pos.x = p.pos.x + 2
    p.hp = p.hp + 1
  }
  return p.pos.x + p.hp
}`

	result, err := compileAndCall(t, source, "advance", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 15 {
		t.Fatalf("expected nested workflow result 15, got %d", got)
	}
}

func TestInterpreter_ScenarioGlobalsLoopAccumulation(t *testing.T) {
	source := `
	var base: int = 10
	fun accumulate(): int {
	  var total: int = base
	  for (var i: int = 0; i < 4; i = i + 1) {
	    total = total + 3
	  }
	  base = total
	  return base
	}`
	v := compileAndRun(t, source)
	fn := v.FuncReg().GetFunction("accumulate")
	if fn == nil {
		t.Fatal("function 'accumulate' not found")
	}
	result := fn.ExecuteBody(v, nil)
	if got := vm.DecodeInt(result); got != 22 {
		t.Fatalf("expected global accumulation result 22, got %d", got)
	}
}

func TestInterpreter_ScenarioStreamWorkflowProgression(t *testing.T) {
	prog, err := frontend.ParseModuleForTest(`
stream fun build(): int {
  var total: int = 1
  yield total
  total = total + 2
  yield total
  return total + 4
}`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	eval := NewVMEvaluator()
	if err := eval.CompileProgram(prog); err != nil {
		t.Fatalf("CompileProgram: %v", err)
	}

	first, err := eval.Evaluate("build", binding.InvocationStageNext, nil)
	if err != nil {
		t.Fatalf("first next: %v", err)
	}
	if first.(int) != 1 {
		t.Fatalf("expected first yield 1, got %v", first)
	}

	second, err := eval.Evaluate("build", binding.InvocationStageNext, nil)
	if err != nil {
		t.Fatalf("second next: %v", err)
	}
	if second.(int) != 3 {
		t.Fatalf("expected second yield 3, got %v", second)
	}

	final, err := eval.Evaluate("build", binding.InvocationStageFinal, nil)
	if err != nil {
		t.Fatalf("final: %v", err)
	}
	if final.(int) != 7 {
		t.Fatalf("expected final 7, got %v", final)
	}
}

func TestInterpreter_ScenarioInterfaceOverrideClassification(t *testing.T) {
	source := `
interface Scorable { fun score(): int }
open class BaseRule {
  open fun score(): int { return 4 }
}
class BoostRule : BaseRule, Scorable {
  override fun score(): int { return super.score() + 6 }
}
fun classify(): int {
  var rule: BoostRule = new BoostRule()
  if rule is Scorable {
    return rule.score() + 1
  }
  return 0
}`

	result, err := compileAndCall(t, source, "classify", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 11 {
		t.Fatalf("expected interface override classification 11, got %d", got)
	}
}

func TestInterpreter_ScenarioNestedCollectionTransform(t *testing.T) {
	source := `
struct Entry {
  key: string
  value: int
}
fun transform(): int {
  var entries: array<Entry> = [
    Entry{key: "hp", value: 3},
    Entry{key: "mp", value: 4},
    Entry{key: "hp", value: 5}
  ]
  var totals: map<string, int> = {"hp": 0, "mp": 0}
  for (entry in entries) {
    totals[entry.key] = totals[entry.key] + entry.value
  }
  return totals["hp"] * 10 + totals["mp"]
}`

	result, err := compileAndCall(t, source, "transform", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 84 {
		t.Fatalf("expected nested collection transform 84, got %d", got)
	}
}

func TestInterpreter_ScenarioInvalidMapKeyDiagnostic(t *testing.T) {
	_, err := compileAndCall(t, `
fun broken(): int {
  var stats: map<string, int> = {"hp": 10}
  var idx: int = 1
  return stats[idx]
}`, "broken", nil)
	if err == nil {
		t.Fatal("expected invalid map key diagnostic")
	}
	rtErr, ok := err.(*RuntimeError)
	if !ok {
		t.Fatalf("expected RuntimeError, got %T", err)
	}
	if rtErr.Code != "invalid_map_key_type" {
		t.Fatalf("expected invalid_map_key_type, got %q", rtErr.Code)
	}
	if rtErr.Path != "vm/map/key_type" {
		t.Fatalf("expected vm/map/key_type path, got %q", rtErr.Path)
	}
}

func TestInterpreter_ScenarioRuleEngineWorkflow(t *testing.T) {
	source := `
struct Input {
  score: int
  vip: bool
}
fun bonus(input: Input): int {
  if input.vip {
    return 5
  }
  return 0
}
fun classify(input: Input): int {
  var total: int = input.score + bonus(input)
  when (total) {
    case 0, 1, 2, 3, 4 { return 0 }
    case 5, 6, 7, 8, 9 { return 1 }
    else { return 2 }
  }
}
fun evaluate(): int {
  var a: Input = Input{score: 4, vip: false}
  var b: Input = Input{score: 6, vip: true}
  return classify(a) + classify(b)
}`

	result, err := compileAndCall(t, source, "evaluate", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 2 {
		t.Fatalf("expected rule engine result 2, got %d", got)
	}
}

func TestInterpreter_ScenarioBusinessStreamProgression(t *testing.T) {
	prog, err := frontend.ParseModuleForTest(`
stream fun process(): int {
  var total: int = 2
  yield total
  total = total * 3
  yield total
  return total + 1
}`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	eval := NewVMEvaluator()
	if err := eval.CompileProgram(prog); err != nil {
		t.Fatalf("CompileProgram: %v", err)
	}

	first, err := eval.Evaluate("process", binding.InvocationStageNext, nil)
	if err != nil {
		t.Fatalf("first next: %v", err)
	}
	if first.(int) != 2 {
		t.Fatalf("expected first yield 2, got %v", first)
	}

	second, err := eval.Evaluate("process", binding.InvocationStageNext, nil)
	if err != nil {
		t.Fatalf("second next: %v", err)
	}
	if second.(int) != 6 {
		t.Fatalf("expected second yield 6, got %v", second)
	}

	final, err := eval.Evaluate("process", binding.InvocationStageFinal, nil)
	if err != nil {
		t.Fatalf("final: %v", err)
	}
	if final.(int) != 7 {
		t.Fatalf("expected final 7, got %v", final)
	}
}

func TestInterpreter_ScenarioOrderPipelineWithMapAggregation(t *testing.T) {
	source := `
struct Item {
  sku: string
  qty: int
  price: int
}
fun total(): int {
  var items: array<Item> = [
    Item{sku: "apple", qty: 2, price: 5},
    Item{sku: "bread", qty: 1, price: 7},
    Item{sku: "apple", qty: 1, price: 5}
  ]
  var totals: map<string, int> = {"apple": 0, "bread": 0}
  for (item in items) {
    totals[item.sku] = totals[item.sku] + item.qty * item.price
  }
  return totals["apple"] + totals["bread"]
}`

	result, err := compileAndCall(t, source, "total", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 22 {
		t.Fatalf("expected order pipeline total 22, got %d", got)
	}
}

func TestInterpreter_ScenarioRuleEngineWithInheritanceAndInterface(t *testing.T) {
	source := `
interface Scorable { fun score(base: int): int }
open class Rule {
  open fun score(base: int): int { return base }
}
class BonusRule : Rule, Scorable {
  bonus: int
  constructor(v: int) { this.bonus = v }
  override fun score(base: int): int { return super.score(base) + this.bonus }
}
fun evaluate(): int {
  var rule: BonusRule = new BonusRule(4)
  if rule is Scorable {
    return rule.score(6)
  }
  return 0
}`

	result, err := compileAndCall(t, source, "evaluate", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 10 {
		t.Fatalf("expected rule engine score 10, got %d", got)
	}
}

func TestInterpreter_ScenarioStringScriptMembershipWorkflow(t *testing.T) {
	source := `
struct Member {
  name: string
  tier: string
  visits: int
}
fun score(): int {
  var members: array<Member> = [
    Member{name: "ann", tier: "gold", visits: 3},
    Member{name: "bob", tier: "silver", visits: 2},
    Member{name: "ann", tier: "gold", visits: 1}
  ]
  var totals: map<string, int> = {"gold": 0, "silver": 0}
  for (member in members) {
    totals[member.tier] = totals[member.tier] + member.visits
  }
  return totals["gold"] * 10 + totals["silver"]
}`

	result, err := compileAndCall(t, source, "score", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 42 {
		t.Fatalf("expected membership workflow score 42, got %d", got)
	}
}

func TestInterpreter_ScenarioStringScriptPromotionRules(t *testing.T) {
	source := `
interface Priced { fun total(): int }
open class OrderBase {
  open fun subtotal(): int { return 10 }
}
class PromoOrder : OrderBase, Priced {
  discount: int
  constructor(v: int) { this.discount = v }
  override fun subtotal(): int { return super.subtotal() + 8 }
  fun total(): int { return this.subtotal() - this.discount }
}
fun evaluate(): int {
  var order: PromoOrder = new PromoOrder(3)
  if order is Priced {
    return order.total()
  }
  return 0
}`

	result, err := compileAndCall(t, source, "evaluate", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 15 {
		t.Fatalf("expected promotion rules result 15, got %d", got)
	}
}

func TestInterpreter_ScenarioStringScriptSuspendedStateSurvivesGCPass(t *testing.T) {
	prog, err := frontend.ParseModuleForTest(`
struct Snapshot {
  count: int
}
stream fun capture(): int {
  var snap: Snapshot = Snapshot{count: 4}
  yield snap.count
  snap.count = snap.count + 9
  return snap.count
}`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	eval := NewVMEvaluator()
	if err := eval.CompileProgram(prog); err != nil {
		t.Fatalf("CompileProgram: %v", err)
	}

	first, err := eval.Evaluate("capture", binding.InvocationStageNext, nil)
	if err != nil {
		t.Fatalf("first next: %v", err)
	}
	if first.(int) != 4 {
		t.Fatalf("expected first yield 4, got %v", first)
	}

	for i := 0; i < 128; i++ {
		_ = eval.vm_.NewArray(vm.TypeID(1), 8)
	}

	final, err := eval.Evaluate("capture", binding.InvocationStageFinal, nil)
	if err != nil {
		t.Fatalf("final after GC pressure: %v", err)
	}
	if final.(int) != 13 {
		t.Fatalf("expected final 13 after suspended state, got %v", final)
	}
}

func TestInterpreter_ScenarioStringScriptCollectionSummary(t *testing.T) {
	source := `
fun summarize(): int {
  var values: array<int> = [2, 4, 6]
  var stats: map<string, int> = {"count": len(values), "sum": 0}
  for (value in values) {
    stats["sum"] = stats["sum"] + value
  }
  return stats["count"] * 10 + stats["sum"]
}`

	result, err := compileAndCall(t, source, "summarize", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 42 {
		t.Fatalf("expected collection summary result 42, got %d", got)
	}
}

func TestInterpreter_ScenarioStringScriptConstructorSuperChain(t *testing.T) {
	source := `
open class Account {
  balance: int
  constructor(v: int) { this.balance = v }
}
class RewardAccount : Account {
  bonus: int
  constructor(balance: int, bonus: int) {
    super(balance)
    this.bonus = bonus
  }
  fun total(): int { return this.balance + this.bonus }
}
fun read(): int {
  var acct: RewardAccount = new RewardAccount(9, 6)
  return acct.total()
}`

	result, err := compileAndCall(t, source, "read", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 15 {
		t.Fatalf("expected constructor super chain result 15, got %d", got)
	}
}

func TestInterpreter_ScenarioStringScriptInvalidMapKeyDiagnostic(t *testing.T) {
	_, err := compileAndCall(t, `
fun broken(): int {
  var totals: map<string, int> = {"ok": 1}
  var idx: int = 2
  return totals[idx]
}`, "broken", nil)
	if err == nil {
		t.Fatal("expected invalid map key diagnostic")
	}
	rtErr, ok := err.(*RuntimeError)
	if !ok {
		t.Fatalf("expected RuntimeError, got %T", err)
	}
	if rtErr.Code != "invalid_map_key_type" {
		t.Fatalf("expected invalid_map_key_type, got %q", rtErr.Code)
	}
	if rtErr.Path != "vm/map/key_type" {
		t.Fatalf("expected vm/map/key_type path, got %q", rtErr.Path)
	}
}

func TestInterpreter_ScenarioMultiClassPartyCombatWorkflow(t *testing.T) {
	source := `
class Weapon {
  power: int
  constructor(v: int) { this.power = v }
  fun damage(): int { return this.power }
}
class Fighter {
  hp: int
  weapon: Weapon
  constructor(hp: int, power: int) {
    this.hp = hp
    this.weapon = new Weapon(power)
  }
  fun strike(enemy: Monster): int {
    var dealt: int = this.weapon.damage()
    enemy.hp = enemy.hp - dealt
    return enemy.hp
  }
}
class Monster {
  hp: int
  constructor(v: int) { this.hp = v }
  fun alive(): bool { return this.hp > 0 }
}
fun simulate(): int {
  var hero: Fighter = new Fighter(20, 6)
  var slime: Monster = new Monster(15)
  hero.strike(slime)
  hero.strike(slime)
  if slime.alive() {
    return slime.hp
  }
  return hero.hp + slime.hp
}`

	result, err := compileAndCall(t, source, "simulate", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 3 {
		t.Fatalf("expected party combat result 3, got %d", got)
	}
}

func TestInterpreter_ScenarioMultiClassBankTransferWorkflow(t *testing.T) {
	source := `
class Account {
  balance: int
  constructor(v: int) { this.balance = v }
  fun deposit(v: int): int {
    this.balance = this.balance + v
    return this.balance
  }
  fun withdraw(v: int): int {
    this.balance = this.balance - v
    return this.balance
  }
}
class Bank {
  fun transfer(source: Account, target: Account, amount: int): int {
    source.withdraw(amount)
    return target.deposit(amount)
  }
}
fun simulate(): int {
  var checking: Account = new Account(30)
  var savings: Account = new Account(10)
  var bank: Bank = new Bank()
  bank.transfer(checking, savings, 7)
  return checking.balance * 10 + savings.balance
}`

	result, err := compileAndCall(t, source, "simulate", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 247 {
		t.Fatalf("expected bank transfer result 247, got %d", got)
	}
}

func TestInterpreter_ScenarioMultiClassInheritancePartyBuffWorkflow(t *testing.T) {
	source := `
open class Buff {
  open fun apply(base: int): int { return base }
}
class RageBuff : Buff {
  bonus: int
  constructor(v: int) { this.bonus = v }
  override fun apply(base: int): int { return super.apply(base) + this.bonus }
}
class Fighter {
  power: int
  buff: RageBuff
  constructor(power: int, bonus: int) {
    this.power = power
    this.buff = new RageBuff(bonus)
  }
  fun attack(): int { return this.buff.apply(this.power) }
}
fun simulate(): int {
  var hero: Fighter = new Fighter(8, 5)
  return hero.attack()
}`

	result, err := compileAndCall(t, source, "simulate", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 13 {
		t.Fatalf("expected inheritance buff result 13, got %d", got)
	}
}

func TestInterpreter_ScenarioMultiClassInterfaceTeamWorkflow(t *testing.T) {
	source := `
interface Healable { fun recover(): int }
class Potion : Healable {
  amount: int
  constructor(v: int) { this.amount = v }
  fun recover(): int { return this.amount }
}
class Hero {
  hp: int
  constructor(v: int) { this.hp = v }
  fun use(item: Potion): int {
    if item is Healable {
      this.hp = this.hp + item.recover()
    }
    return this.hp
  }
}
fun simulate(): int {
  var hero: Hero = new Hero(9)
  var potion: Potion = new Potion(6)
  return hero.use(potion)
}`

	result, err := compileAndCall(t, source, "simulate", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 15 {
		t.Fatalf("expected interface team workflow result 15, got %d", got)
	}
}

func TestInterpreter_ScenarioMultiClassQuestBoardWorkflow(t *testing.T) {
	source := `
class Quest {
  reward: int
  constructor(v: int) { this.reward = v }
  fun payout(): int { return this.reward }
}
class Board {
  current: Quest
  constructor(v: int) { this.current = new Quest(v) }
  fun claim(): int { return this.current.payout() }
}
class Player {
  gold: int
  constructor(v: int) { this.gold = v }
  fun finish(board: Board): int {
    this.gold = this.gold + board.claim()
    return this.gold
  }
}
fun simulate(): int {
  var board: Board = new Board(11)
  var player: Player = new Player(4)
  return player.finish(board)
}`

	result, err := compileAndCall(t, source, "simulate", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 15 {
		t.Fatalf("expected quest board workflow result 15, got %d", got)
	}
}

func TestInterpreter_ScenarioMultiClassShopInventoryItemWorkflow(t *testing.T) {
	source := `
class Item {
  price: int
  constructor(v: int) { this.price = v }
}
class Inventory {
  stock: Item
  constructor(v: int) { this.stock = new Item(v) }
  fun cost(): int { return this.stock.price }
}
class Shop {
  inventory: Inventory
  constructor(v: int) { this.inventory = new Inventory(v) }
  fun quote(): int { return this.inventory.cost() }
}
class Customer {
  coins: int
  constructor(v: int) { this.coins = v }
  fun buy(shop: Shop): int {
    this.coins = this.coins - shop.quote()
    return this.coins
  }
}
fun simulate(): int {
  var shop: Shop = new Shop(9)
  var customer: Customer = new Customer(25)
  return customer.buy(shop)
}`

	result, err := compileAndCall(t, source, "simulate", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 16 {
		t.Fatalf("expected shop inventory result 16, got %d", got)
	}
}

func TestInterpreter_ScenarioMultiClassCustomerCouponPaymentWorkflow(t *testing.T) {
	source := `
interface Discountable { fun discount(): int }
class Coupon : Discountable {
  value: int
  constructor(v: int) { this.value = v }
  fun discount(): int { return this.value }
}
class Payment {
  amount: int
  constructor(v: int) { this.amount = v }
  fun settle(coupon: Coupon): int {
    if coupon is Discountable {
      this.amount = this.amount - coupon.discount()
    }
    return this.amount
  }
}
class Order {
  payment: Payment
  constructor(v: int) { this.payment = new Payment(v) }
  fun total(coupon: Coupon): int { return this.payment.settle(coupon) }
}
class Customer {
  coupon: Coupon
  constructor(v: int) { this.coupon = new Coupon(v) }
  fun checkout(order: Order): int { return order.total(this.coupon) }
}
fun simulate(): int {
  var customer: Customer = new Customer(4)
  var order: Order = new Order(19)
  return customer.checkout(order)
}`

	result, err := compileAndCall(t, source, "simulate", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 15 {
		t.Fatalf("expected customer coupon payment result 15, got %d", got)
	}
}

func TestInterpreter_ScenarioMultiClassManagerWorkerInheritanceWorkflow(t *testing.T) {
	source := `
open class Worker {
  open fun output(): int { return 5 }
}
class SkilledWorker : Worker {
  bonus: int
  constructor(v: int) { this.bonus = v }
  override fun output(): int { return super.output() + this.bonus }
}
class Team {
  worker: SkilledWorker
  constructor(v: int) { this.worker = new SkilledWorker(v) }
  fun total(): int { return this.worker.output() }
}
class Manager {
  team: Team
  constructor(v: int) { this.team = new Team(v) }
  fun review(): int { return this.team.total() }
}
fun simulate(): int {
  var manager: Manager = new Manager(8)
  return manager.review()
}`

	result, err := compileAndCall(t, source, "simulate", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 13 {
		t.Fatalf("expected manager worker inheritance result 13, got %d", got)
	}
}

func TestInterpreter_ScenarioNestedClassFieldMethodWorkflow(t *testing.T) {
	source := `
class Meter {
  value: int
  constructor(v: int) { this.value = v }
  fun read(): int { return this.value }
}
class Device {
  meter: Meter
  constructor(v: int) { this.meter = new Meter(v) }
  fun report(): int { return this.meter.read() }
}
fun simulate(): int {
  var device: Device = new Device(12)
  return device.report()
}`

	result, err := compileAndCall(t, source, "simulate", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 12 {
		t.Fatalf("expected nested class field method result 12, got %d", got)
	}
}

func TestInterpreter_ScenarioNestedClassFieldMethodWorkflowDeepChain(t *testing.T) {
	source := `
class Member {
  power: int
  constructor(v: int) { this.power = v }
  fun contribution(): int { return this.power }
}
class Team {
  leader: Member
  constructor(v: int) { this.leader = new Member(v) }
  fun total(): int { return this.leader.contribution() }
}
class Guild {
  team: Team
  constructor(v: int) { this.team = new Team(v) }
  fun rating(): int { return this.team.total() }
}
fun simulate(): int {
  var guild: Guild = new Guild(9)
  return guild.rating()
}`

	result, err := compileAndCall(t, source, "simulate", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 9 {
		t.Fatalf("expected nested class deep chain result 9, got %d", got)
	}
}

func TestInterpreter_ScenarioClassFieldConstructorArrayAssignment(t *testing.T) {
	source := `
class Party {
  first: int
  second: int
  third: int
  constructor() {
    var values: array<int> = [4, 7, 5]
    this.first = values[0]
    this.second = values[1]
    this.third = values[2]
  }
  fun total(): int {
    return this.first + this.second + this.third
  }
}
fun simulate(): int {
  var party: Party = new Party()
  return party.total()
}`

	result, err := compileAndCall(t, source, "simulate", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 16 {
		t.Fatalf("expected constructor array assignment to total 16, got %d", got)
	}
}

func TestInterpreter_ScenarioClassFieldConstructorMapAssignment(t *testing.T) {
	source := `
class Shop {
  potion: int
  ether: int
  constructor() {
    var prices: map<string, int> = {"potion": 3, "ether": 5}
    this.potion = prices["potion"]
    this.ether = prices["ether"]
  }
  fun total(): int {
    return this.potion + this.ether
  }
}
fun simulate(): int {
  var shop: Shop = new Shop()
  return shop.total()
}`

	result, err := compileAndCall(t, source, "simulate", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 8 {
		t.Fatalf("expected constructor map assignment to total 8, got %d", got)
	}
}

func TestInterpreter_ScenarioClassWithArrayField(t *testing.T) {
	result, err := compileAndCall(t, `
class Bag {
  values: array<int>
  constructor() { this.values = [1, 2, 3] }
  fun first(): int { return this.values[0] }
}
fun simulate(): int {
  var bag: Bag = new Bag()
  return bag.first()
}`, "simulate", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 1 {
		t.Fatalf("expected class-held array field access to return 1, got %d", got)
	}
}

func TestInterpreter_ScenarioClassWithMapField(t *testing.T) {
	result, err := compileAndCall(t, `
class Shop {
  prices: map<string, int>
  constructor() { this.prices = {"potion": 3} }
  fun read(): int { return this.prices["potion"] }
}
fun simulate(): int {
  var shop: Shop = new Shop()
  return shop.read()
}`, "simulate", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 3 {
		t.Fatalf("expected class-held map field access to return 3, got %d", got)
	}
}

func TestInterpreter_ScenarioClassWithArrayFieldLen(t *testing.T) {
	result, err := compileAndCall(t, `
class Bag {
  values: array<int>
  constructor() { this.values = [1, 2, 3] }
  fun size(): int { return len(this.values) }
}
fun simulate(): int {
  var bag: Bag = new Bag()
  return bag.size()
}`, "simulate", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 3 {
		t.Fatalf("expected class-held array len to return 3, got %d", got)
	}
}

func TestInterpreter_ScenarioClassWithMapFieldLen(t *testing.T) {
	result, err := compileAndCall(t, `
class Shop {
  prices: map<string, int>
  constructor() { this.prices = {"potion": 3, "ether": 5} }
  fun size(): int { return len(this.prices) }
}
fun simulate(): int {
  var shop: Shop = new Shop()
  return shop.size()
}`, "simulate", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 2 {
		t.Fatalf("expected class-held map len to return 2, got %d", got)
	}
}

func TestInterpreter_ScenarioClassWithArrayFieldMutation(t *testing.T) {
	result, err := compileAndCall(t, `
class Bag {
  values: array<int>
  constructor() { this.values = [1, 2, 3] }
  fun update(): int {
    this.values[1] = 9
    return this.values[1]
  }
}
fun simulate(): int {
  var bag: Bag = new Bag()
  return bag.update()
}`, "simulate", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 9 {
		t.Fatalf("expected class-held array field mutation to return 9, got %d", got)
	}
}

func TestInterpreter_ScenarioClassWithMapFieldMutation(t *testing.T) {
	result, err := compileAndCall(t, `
class Shop {
  prices: map<string, int>
  constructor() { this.prices = {"potion": 3} }
  fun update(): int {
    this.prices["potion"] = 8
    return this.prices["potion"]
  }
}
fun simulate(): int {
  var shop: Shop = new Shop()
  return shop.update()
}`, "simulate", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 8 {
		t.Fatalf("expected class-held map field mutation to return 8, got %d", got)
	}
}

func TestInterpreter_ScenarioNestedClassWithArrayField(t *testing.T) {
	result, err := compileAndCall(t, `
class Bag {
  values: array<int>
  constructor() { this.values = [2, 4] }
  fun first(): int { return this.values[0] }
}
class Carrier {
  bag: Bag
  constructor() { this.bag = new Bag() }
  fun read(): int { return this.bag.first() }
}
fun simulate(): int {
  var carrier: Carrier = new Carrier()
  return carrier.read()
}`, "simulate", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 2 {
		t.Fatalf("expected nested class array field to return 2, got %d", got)
	}
}

func TestInterpreter_ScenarioNestedClassWithMapField(t *testing.T) {
	result, err := compileAndCall(t, `
class Shop {
  prices: map<string, int>
  constructor() { this.prices = {"potion": 3} }
  fun read(): int { return this.prices["potion"] }
}
class Market {
  shop: Shop
  constructor() { this.shop = new Shop() }
  fun quote(): int { return this.shop.read() }
}
fun simulate(): int {
  var market: Market = new Market()
  return market.quote()
}`, "simulate", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 3 {
		t.Fatalf("expected nested class map field to return 3, got %d", got)
	}
}

func TestInterpreter_ScenarioStreamClassTaskProgressWorkflow(t *testing.T) {
	prog, err := frontend.ParseModuleForTest(`
class Task {
  value: int
  constructor(v: int) { this.value = v }
  fun advance(step: int): int {
    this.value = this.value + step
    return this.value
  }
}
stream fun run(): int {
  var task: Task = new Task(1)
  yield task.advance(2)
  yield task.advance(3)
  return task.advance(4)
}`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	eval := NewVMEvaluator()
	if err := eval.CompileProgram(prog); err != nil {
		t.Fatalf("CompileProgram: %v", err)
	}

	first, err := eval.Evaluate("run", binding.InvocationStageNext, nil)
	if err != nil {
		t.Fatalf("first next: %v", err)
	}
	if first.(int) != 3 {
		t.Fatalf("expected first yield 3, got %v", first)
	}

	second, err := eval.Evaluate("run", binding.InvocationStageNext, nil)
	if err != nil {
		t.Fatalf("second next: %v", err)
	}
	if second.(int) != 6 {
		t.Fatalf("expected second yield 6, got %v", second)
	}

	final, err := eval.Evaluate("run", binding.InvocationStageFinal, nil)
	if err != nil {
		t.Fatalf("final: %v", err)
	}
	if final.(int) != 10 {
		t.Fatalf("expected final 10, got %v", final)
	}
}

func TestInterpreter_ScenarioStreamClassNestedGraph(t *testing.T) {
	prog, err := frontend.ParseModuleForTest(`
class Member {
  energy: int
  constructor(v: int) { this.energy = v }
  fun spend(v: int): int {
    this.energy = this.energy - v
    return this.energy
  }
}
class Team {
  front: Member
  back: Member
  constructor() {
    this.front = new Member(10)
    this.back = new Member(8)
  }
  fun cycle(): int {
    return this.front.spend(2) + this.back.spend(1)
  }
}
stream fun dispatch(): int {
  var team: Team = new Team()
  yield team.cycle()
  yield team.cycle()
  return team.cycle()
}`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	eval := NewVMEvaluator()
	if err := eval.CompileProgram(prog); err != nil {
		t.Fatalf("CompileProgram: %v", err)
	}
	first, err := eval.Evaluate("dispatch", binding.InvocationStageNext, nil)
	if err != nil {
		t.Fatalf("first next: %v", err)
	}
	if first.(int) != 15 {
		t.Fatalf("expected first yield 15, got %v", first)
	}
	second, err := eval.Evaluate("dispatch", binding.InvocationStageNext, nil)
	if err != nil {
		t.Fatalf("second next: %v", err)
	}
	if second.(int) != 12 {
		t.Fatalf("expected second yield 12, got %v", second)
	}
	final, err := eval.Evaluate("dispatch", binding.InvocationStageFinal, nil)
	if err != nil {
		t.Fatalf("final: %v", err)
	}
	if final.(int) != 9 {
		t.Fatalf("expected final 9, got %v", final)
	}
}

func TestInterpreter_ScenarioStreamClassContainerField(t *testing.T) {
	prog, err := frontend.ParseModuleForTest(`
class Bag {
  values: array<int>
  constructor() { this.values = [1, 2, 3] }
  fun read(): int { return this.values[0] }
}
stream fun run(): int {
  var bag: Bag = new Bag()
  yield 1
  return bag.read()
}`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	eval := NewVMEvaluator()
	if err := eval.CompileProgram(prog); err != nil {
		t.Fatalf("CompileProgram: %v", err)
	}
	first, err := eval.Evaluate("run", binding.InvocationStageNext, nil)
	if err != nil {
		t.Fatalf("first next: %v", err)
	}
	if first.(int) != 1 {
		t.Fatalf("expected first yield 1, got %v", first)
	}
	final, err := eval.Evaluate("run", binding.InvocationStageFinal, nil)
	if err != nil {
		t.Fatalf("final: %v", err)
	}
	if final.(int) != 1 {
		t.Fatalf("expected final 1, got %v", final)
	}
}

func TestInterpreter_ScenarioStreamClassMapField(t *testing.T) {
	prog, err := frontend.ParseModuleForTest(`
class Shop {
  prices: map<string, int>
  constructor() { this.prices = {"potion": 3} }
  fun read(): int { return this.prices["potion"] }
}
stream fun run(): int {
  var shop: Shop = new Shop()
  yield 1
  return shop.read()
}`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	eval := NewVMEvaluator()
	if err := eval.CompileProgram(prog); err != nil {
		t.Fatalf("CompileProgram: %v", err)
	}
	first, err := eval.Evaluate("run", binding.InvocationStageNext, nil)
	if err != nil {
		t.Fatalf("first next: %v", err)
	}
	if first.(int) != 1 {
		t.Fatalf("expected first yield 1, got %v", first)
	}
	final, err := eval.Evaluate("run", binding.InvocationStageFinal, nil)
	if err != nil {
		t.Fatalf("final: %v", err)
	}
	if final.(int) != 3 {
		t.Fatalf("expected final 3, got %v", final)
	}
}

func TestInterpreter_ScenarioFieldStoresSubclass(t *testing.T) {
	source := `
open class Worker {
  open fun output(): int { return 5 }
}
class SkilledWorker : Worker {
  bonus: int
  constructor(v: int) { this.bonus = v }
  override fun output(): int { return super.output() + this.bonus }
}
class Manager {
  worker: SkilledWorker
  constructor(v: int) { this.worker = new SkilledWorker(v) }
  fun review(): int { return this.worker.output() }
}
fun simulate(): int {
  var manager: Manager = new Manager(8)
  return manager.review()
}`

	result, err := compileAndCall(t, source, "simulate", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 13 {
		t.Fatalf("expected subclass field to return 13, got %d", got)
	}
}

func TestInterpreter_ScenarioFieldStoresInterfaceImplementation(t *testing.T) {
	source := `
interface Healable { fun recover(): int }
class Potion : Healable {
  amount: int
  constructor(v: int) { this.amount = v }
  fun recover(): int { return this.amount }
}
class Hero {
  potion: Potion
  constructor(v: int) { this.potion = new Potion(v) }
  fun use(): int {
    if this.potion is Healable {
      return this.potion.recover()
    }
    return 0
  }
}
fun simulate(): int {
  var hero: Hero = new Hero(6)
  return hero.use()
}`

	result, err := compileAndCall(t, source, "simulate", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 6 {
		t.Fatalf("expected interface implementation field to return 6, got %d", got)
	}
}

func TestInterpreter_ScenarioFieldStoresArrayOfClass(t *testing.T) {
	result, err := compileAndCall(t, `
class Member {
  score: int
  constructor(v: int) { this.score = v }
  fun points(): int { return this.score }
}
class Party {
  members: array<Member>
  constructor() { this.members = [new Member(4), new Member(7)] }
  fun first(): int { return this.members[0].points() }
}
fun simulate(): int {
  var party: Party = new Party()
  return party.first()
}`, "simulate", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 4 {
		t.Fatalf("expected array<class> field access to return 4, got %d", got)
	}
}

func TestInterpreter_ScenarioFieldStoresMapOfClass(t *testing.T) {
	result, err := compileAndCall(t, `
class Item {
  value: int
  constructor(v: int) { this.value = v }
  fun price(): int { return this.value }
}
class Catalog {
  items: map<string, Item>
  constructor() { this.items = {"potion": new Item(3)} }
  fun read(): int { return this.items["potion"].price() }
}
fun simulate(): int {
  var catalog: Catalog = new Catalog()
  return catalog.read()
}`, "simulate", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 3 {
		t.Fatalf("expected map<class> field access to return 3, got %d", got)
	}
}

func TestInterpreter_ScenarioStreamInheritance(t *testing.T) {
	prog, err := frontend.ParseModuleForTest(`
open class Counter {
  open fun next(v: int): int { return v + 1 }
}
class FastCounter : Counter {
  step: int
  constructor(v: int) { this.step = v }
  override fun next(v: int): int { return super.next(v) + this.step }
}
stream fun run(): int {
  var counter: FastCounter = new FastCounter(2)
  yield counter.next(1)
  return counter.next(4)
}`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	eval := NewVMEvaluator()
	if err := eval.CompileProgram(prog); err != nil {
		t.Fatalf("CompileProgram: %v", err)
	}
	first, err := eval.Evaluate("run", binding.InvocationStageNext, nil)
	if err != nil {
		t.Fatalf("first next: %v", err)
	}
	if first.(int) != 4 {
		t.Fatalf("expected first yield 4, got %v", first)
	}
	final, err := eval.Evaluate("run", binding.InvocationStageFinal, nil)
	if err != nil {
		t.Fatalf("final: %v", err)
	}
	if final.(int) != 7 {
		t.Fatalf("expected final 7, got %v", final)
	}
}

func TestInterpreter_ScenarioStreamInterfaceField(t *testing.T) {
	prog, err := frontend.ParseModuleForTest(`
interface Healable { fun recover(): int }
class Potion : Healable {
  amount: int
  constructor(v: int) { this.amount = v }
  fun recover(): int { return this.amount }
}
class Hero {
  potion: Potion
  constructor(v: int) { this.potion = new Potion(v) }
  fun use(): int { return this.potion.recover() }
}
stream fun run(): int {
  var hero: Hero = new Hero(6)
  yield 1
  return hero.use()
}`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	eval := NewVMEvaluator()
	if err := eval.CompileProgram(prog); err != nil {
		t.Fatalf("CompileProgram: %v", err)
	}
	first, err := eval.Evaluate("run", binding.InvocationStageNext, nil)
	if err != nil {
		t.Fatalf("first next: %v", err)
	}
	if first.(int) != 1 {
		t.Fatalf("expected first yield 1, got %v", first)
	}
	final, err := eval.Evaluate("run", binding.InvocationStageFinal, nil)
	if err != nil {
		t.Fatalf("final: %v", err)
	}
	if final.(int) != 6 {
		t.Fatalf("expected final 6, got %v", final)
	}
}

func TestInterpreter_ScenarioStringScriptStreamResetWorkflow(t *testing.T) {
	prog, err := frontend.ParseModuleForTest(`stream fun emit(): int { yield 2 return 5 }`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	eval := NewVMEvaluator()
	if err := eval.CompileProgram(prog); err != nil {
		t.Fatalf("CompileProgram: %v", err)
	}

	first, err := eval.Evaluate("emit", binding.InvocationStageNext, nil)
	if err != nil {
		t.Fatalf("first next: %v", err)
	}
	if first.(int) != 2 {
		t.Fatalf("expected first yield 2, got %v", first)
	}

	final, err := eval.Evaluate("emit", binding.InvocationStageFinal, nil)
	if err != nil {
		t.Fatalf("final: %v", err)
	}
	if final.(int) != 5 {
		t.Fatalf("expected final 5, got %v", final)
	}

	eval.Reset("emit", nil)
	again, err := eval.Evaluate("emit", binding.InvocationStageFinal, nil)
	if err != nil {
		t.Fatalf("final after reset: %v", err)
	}
	if again.(int) != 5 {
		t.Fatalf("expected final 5 after reset, got %v", again)
	}
}

func TestInterpreter_ScenarioSuspendedStreamKeepsNestedStateUnderGCPressure(t *testing.T) {
	prog, err := frontend.ParseModuleForTest(`
struct Counter {
  total: int
}
stream fun build(): int {
  var c: Counter = Counter{total: 3}
  yield c.total
  c.total = c.total * 4
  return c.total + 1
}`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	eval := NewVMEvaluator()
	if err := eval.CompileProgram(prog); err != nil {
		t.Fatalf("CompileProgram: %v", err)
	}

	first, err := eval.Evaluate("build", binding.InvocationStageNext, nil)
	if err != nil {
		t.Fatalf("first next: %v", err)
	}
	if first.(int) != 3 {
		t.Fatalf("expected first yield 3, got %v", first)
	}

	for i := 0; i < 128; i++ {
		_ = eval.vm_.NewArray(vm.TypeID(1), 8)
	}

	final, err := eval.Evaluate("build", binding.InvocationStageFinal, nil)
	if err != nil {
		t.Fatalf("final after GC pressure: %v", err)
	}
	if final.(int) != 13 {
		t.Fatalf("expected final 13 after suspended nested state, got %v", final)
	}
}

func TestInterpreter_CyclicMapArrayClassGraphSurvivesGCPressure(t *testing.T) {
	prog, err := frontend.ParseModuleForTest(`
class Node {
  links: map<string, array<Node>>
  value: int
  constructor(v: int) {
    this.value = v
    this.links = {}
  }
  fun link(key: string, other: Node) {
    this.links[key] = [other]
  }
  fun read(key: string): int {
    return this.links[key][0].value
  }
}
stream fun run(): int {
  var a: Node = new Node(4)
  var b: Node = new Node(9)
  a.link("next", b)
  b.link("next", a)
  yield a.read("next")
  return b.read("next")
}`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	eval := NewVMEvaluator()
	if err := eval.CompileProgram(prog); err != nil {
		t.Fatalf("CompileProgram: %v", err)
	}
	first, err := eval.Evaluate("run", binding.InvocationStageNext, nil)
	if err != nil {
		t.Fatalf("first next: %v", err)
	}
	if first.(int) != 9 {
		t.Fatalf("expected first yield 9, got %v", first)
	}
	for i := 0; i < 256; i++ {
		_ = eval.vm_.NewMap(vm.TypeInvalid, vm.TypeInvalid, 8)
	}
	final, err := eval.Evaluate("run", binding.InvocationStageFinal, nil)
	if err != nil {
		t.Fatalf("final after GC pressure: %v", err)
	}
	if final.(int) != 4 {
		t.Fatalf("expected final 4 after cyclic graph GC pressure, got %v", final)
	}
}

func TestInterpreter_StreamCyclicGraphMutationAcrossMultipleYields(t *testing.T) {
	prog, err := frontend.ParseModuleForTest(`
class Node {
  links: map<string, array<Node>>
  value: int
  constructor(v: int) {
    this.value = v
    this.links = {}
  }
  fun link(key: string, other: Node) {
    this.links[key] = [other]
  }
  fun bumpOther(key: string, delta: int): int {
    this.links[key][0].value = this.links[key][0].value + delta
    return this.links[key][0].value
  }
}
stream fun run(): int {
  var a: Node = new Node(4)
  var b: Node = new Node(9)
  a.link("next", b)
  b.link("next", a)
  yield a.bumpOther("next", 1)
  yield b.bumpOther("next", 2)
  return a.bumpOther("next", 3)
}`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	eval := NewVMEvaluator()
	if err := eval.CompileProgram(prog); err != nil {
		t.Fatalf("CompileProgram: %v", err)
	}
	first, err := eval.Evaluate("run", binding.InvocationStageNext, nil)
	if err != nil {
		t.Fatalf("first next: %v", err)
	}
	if first.(int) != 10 {
		t.Fatalf("expected first yield 10, got %v", first)
	}
	for i := 0; i < 128; i++ {
		_ = eval.vm_.NewArray(vm.TypeInvalid, 4)
	}
	second, err := eval.Evaluate("run", binding.InvocationStageNext, nil)
	if err != nil {
		t.Fatalf("second next: %v", err)
	}
	if second.(int) != 6 {
		t.Fatalf("expected second yield 6, got %v", second)
	}
	for i := 0; i < 128; i++ {
		_ = eval.vm_.NewMap(vm.TypeInvalid, vm.TypeInvalid, 8)
	}
	final, err := eval.Evaluate("run", binding.InvocationStageFinal, nil)
	if err != nil {
		t.Fatalf("final: %v", err)
	}
	if final.(int) != 13 {
		t.Fatalf("expected final 13, got %v", final)
	}
}

func TestInterpreter_LenMapReachedThroughIndexChainUsesEntryCount(t *testing.T) {
	result, err := compileAndCall(t, `
class Shop {
  prices: map<string, int>
  constructor(base: int) { this.prices = {"potion": base, "ether": base + 2} }
}
class Market {
  shops: array<Shop>
  constructor() { this.shops = [new Shop(3), new Shop(7)] }
  fun size(): int { return len(this.shops[0].prices) }
}
fun simulate(): int {
  var market: Market = new Market()
  return market.size()
}`,
		"simulate", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 2 {
		t.Fatalf("expected indexed map field len 2, got %d", got)
	}
}

func TestInterpreter_LenMapReachedThroughMapValueClassUsesEntryCount(t *testing.T) {
	result, err := compileAndCall(t, `
class Shop {
  prices: map<string, int>
  constructor(base: int) { this.prices = {"potion": base, "ether": base + 2} }
}
class Market {
  groups: map<string, Shop>
  constructor() { this.groups = {"alpha": new Shop(3)} }
  fun size(): int { return len(this.groups["alpha"].prices) }
}
fun simulate(): int {
  var market: Market = new Market()
  return market.size()
}`,
		"simulate", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 2 {
		t.Fatalf("expected map value class map len 2, got %d", got)
	}
}

func TestInterpreter_LenMapReachedThroughCallReturnedArrayUsesEntryCount(t *testing.T) {
	result, err := compileAndCall(t, `
class Shop {
  prices: map<string, int>
  constructor(base: int) { this.prices = {"potion": base, "ether": base + 2} }
}
class Market {
  shops: array<Shop>
  constructor() { this.shops = [new Shop(3), new Shop(7)] }
}
fun factory(): Market {
  return new Market()
}
fun simulate(): int {
  return len(factory().shops[0].prices)
}`,
		"simulate", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 2 {
		t.Fatalf("expected call-returned array map len 2, got %d", got)
	}
}

func TestInterpreter_LenMapReachedThroughAliasArrayUsesEntryCount(t *testing.T) {
	result, err := compileAndCall(t, `
type ShopList = array<Shop>
type PriceMap = map<string, int>
class Shop {
  prices: PriceMap
  constructor(base: int) { this.prices = {"potion": base, "ether": base + 2} }
}
class Market {
  shops: ShopList
  constructor() { this.shops = [new Shop(3), new Shop(7)] }
  fun size(): int { return len(this.shops[0].prices) }
}
fun simulate(): int {
  var market: Market = new Market()
  return market.size()
}`,
		"simulate", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 2 {
		t.Fatalf("expected alias array map len 2, got %d", got)
	}
}

func TestInterpreter_LenMapReachedThroughAliasMixedContainersUsesEntryCount(t *testing.T) {
	result, err := compileAndCall(t, `
type ShopMap = map<string, Shop>
type MarketGroups = array<ShopMap>
type PriceMap = map<string, int>
class Shop {
  prices: PriceMap
  constructor(base: int) { this.prices = {"potion": base, "ether": base + 2} }
}
class Market {
  groups: MarketGroups
  constructor() { this.groups = [{"alpha": new Shop(3)}] }
  fun size(): int { return len(this.groups[0]["alpha"].prices) }
}
fun simulate(): int {
  var market: Market = new Market()
  return market.size()
}`,
		"simulate", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 2 {
		t.Fatalf("expected alias mixed container map len 2, got %d", got)
	}
}

func TestInterpreter_LenMapReachedThroughAliasOfAliasArrayUsesEntryCount(t *testing.T) {
	result, err := compileAndCall(t, `
type PriceMap = map<string, int>
type ShopList = array<Shop>
type MarketShops = ShopList
class Shop {
  prices: PriceMap
  constructor(base: int) { this.prices = {"potion": base, "ether": base + 2} }
}
class Market {
  shops: MarketShops
  constructor() { this.shops = [new Shop(3), new Shop(7)] }
  fun size(): int { return len(this.shops[0].prices) }
}
fun simulate(): int {
  var market: Market = new Market()
  return market.size()
}`,
		"simulate", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 2 {
		t.Fatalf("expected alias-of-alias array map len 2, got %d", got)
	}
}

func TestInterpreter_LenNestedAliasContainerAlternatingMapArrayUsesEntryCount(t *testing.T) {
	result, err := compileAndCall(t, `
type PriceMap = map<string, int>
type ShopArray = array<Shop>
type ShopMap = map<string, ShopArray>
class Shop {
  prices: PriceMap
  constructor(base: int) { this.prices = {"potion": base, "ether": base + 2} }
}
class Market {
  groups: ShopMap
  constructor() { this.groups = {"alpha": [new Shop(3)]} }
  fun size(): int { return len(this.groups["alpha"][0].prices) }
}
fun simulate(): int {
  var market: Market = new Market()
  return market.size()
}`,
		"simulate", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 2 {
		t.Fatalf("expected alternating alias container map len 2, got %d", got)
	}
}

func TestInterpreter_LenMapReachedThroughDeepAliasChainUsesEntryCount(t *testing.T) {
	result, err := compileAndCall(t, `
type PriceMap = map<string, int>
type P1 = PriceMap
type P2 = P1
type P3 = P2
type ShopList = array<Shop>
type S1 = ShopList
type S2 = S1
class Shop {
  prices: P3
  constructor(base: int) { this.prices = {"potion": base, "ether": base + 2} }
}
class Market {
  shops: S2
  constructor() { this.shops = [new Shop(3), new Shop(7)] }
  fun size(): int { return len(this.shops[0].prices) }
}
fun simulate(): int {
  var market: Market = new Market()
  return market.size()
}`,
		"simulate", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 2 {
		t.Fatalf("expected deep alias chain map len 2, got %d", got)
	}
}

func TestInterpreter_ScenarioCollectionWorkflowWithLenAndMutation(t *testing.T) {
	source := `
fun summarize(): int {
  var values: array<int> = [1, 2, 3]
  var stats: map<string, int> = {"count": len(values), "sum": 0}
  for (value in values) {
    stats["sum"] = stats["sum"] + value
  }
  return stats["count"] * 10 + stats["sum"]
}`

	result, err := compileAndCall(t, source, "summarize", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 36 {
		t.Fatalf("expected collection workflow result 36, got %d", got)
	}
}

func TestInterpreter_ScenarioConstructorSuperChainProducesExpectedState(t *testing.T) {
	source := `
open class Account {
  balance: int
  constructor(v: int) { this.balance = v }
}
class RewardAccount : Account {
  bonus: int
  constructor(balance: int, bonus: int) {
    super(balance)
    this.bonus = bonus
  }
  fun total(): int { return this.balance + this.bonus }
}
fun read(): int {
  var acct: RewardAccount = new RewardAccount(8, 5)
  return acct.total()
}`

	result, err := compileAndCall(t, source, "read", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 13 {
		t.Fatalf("expected constructor super chain result 13, got %d", got)
	}
}

func TestInterpreter_ScenarioInvalidMapKeyInWorkflowReturnsRuntimeDiagnostic(t *testing.T) {
	_, err := compileAndCall(t, `
fun broken(): int {
  var totals: map<string, int> = {"ok": 1}
  var idx: int = 2
  return totals[idx]
}`, "broken", nil)
	if err == nil {
		t.Fatal("expected invalid map key diagnostic")
	}
	rtErr, ok := err.(*RuntimeError)
	if !ok {
		t.Fatalf("expected RuntimeError, got %T", err)
	}
	if rtErr.Code != "invalid_map_key_type" {
		t.Fatalf("expected invalid_map_key_type, got %q", rtErr.Code)
	}
	if rtErr.Path != "vm/map/key_type" {
		t.Fatalf("expected vm/map/key_type path, got %q", rtErr.Path)
	}
}

func TestInterpreter_ScenarioStreamContractAcrossReset(t *testing.T) {
	prog, err := frontend.ParseModuleForTest(`stream fun emit(): int { yield 2 return 5 }`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	eval := NewVMEvaluator()
	if err := eval.CompileProgram(prog); err != nil {
		t.Fatalf("CompileProgram: %v", err)
	}

	first, err := eval.Evaluate("emit", binding.InvocationStageNext, nil)
	if err != nil {
		t.Fatalf("first next: %v", err)
	}
	if first.(int) != 2 {
		t.Fatalf("expected first yield 2, got %v", first)
	}

	final, err := eval.Evaluate("emit", binding.InvocationStageFinal, nil)
	if err != nil {
		t.Fatalf("final: %v", err)
	}
	if final.(int) != 5 {
		t.Fatalf("expected final 5, got %v", final)
	}

	eval.Reset("emit", nil)
	again, err := eval.Evaluate("emit", binding.InvocationStageFinal, nil)
	if err != nil {
		t.Fatalf("final after reset: %v", err)
	}
	if again.(int) != 5 {
		t.Fatalf("expected final 5 after reset, got %v", again)
	}
}
func TestInterpreter_ScenarioRuleEngineWorkflowMatrix(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   int32
	}{
		{
			name: "low",
			source: `
struct Input {
  score: int
  vip: bool
}
fun bonus(input: Input): int {
  if input.vip {
    return 5
  }
  return 0
}
fun classify(): int {
  var input: Input = Input{score: 2, vip: false}
  var total: int = input.score + bonus(input)
  when (total) {
    case 0, 1, 2, 3, 4 { return 0 }
    case 5, 6, 7, 8, 9 { return 1 }
    else { return 2 }
  }
}`,
			want: 0,
		},
		{
			name: "mid",
			source: `
struct Input {
  score: int
  vip: bool
}
fun bonus(input: Input): int {
  if input.vip {
    return 5
  }
  return 0
}
fun classify(): int {
  var input: Input = Input{score: 6, vip: false}
  var total: int = input.score + bonus(input)
  when (total) {
    case 0, 1, 2, 3, 4 { return 0 }
    case 5, 6, 7, 8, 9 { return 1 }
    else { return 2 }
  }
}`,
			want: 1,
		},
		{
			name: "high_with_vip",
			source: `
struct Input {
  score: int
  vip: bool
}
fun bonus(input: Input): int {
  if input.vip {
    return 5
  }
  return 0
}
fun classify(): int {
  var input: Input = Input{score: 6, vip: true}
  var total: int = input.score + bonus(input)
  when (total) {
    case 0, 1, 2, 3, 4 { return 0 }
    case 5, 6, 7, 8, 9 { return 1 }
    else { return 2 }
  }
}`,
			want: 2,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := compileAndCall(t, tc.source, "classify", nil)
			if err != nil {
				t.Fatal(err)
			}
			if got := vm.DecodeInt(result); got != tc.want {
				t.Fatalf("expected class %d, got %d", tc.want, got)
			}
		})
	}
}

func TestInterpreter_ScenarioDiagnosticMatrix(t *testing.T) {
	cases := []struct {
		name     string
		source   string
		callable string
		code     string
		path     string
	}{
		{
			name: "invalid_map_key_type",
			source: `
fun broken(): int {
  var stats: map<string, int> = {"hp": 10}
  var idx: int = 1
  return stats[idx]
}`,
			callable: "broken",
			code:     "invalid_map_key_type",
			path:     "vm/map/key_type",
		},
		{
			name: "invalid_array_index_type",
			source: `
fun broken(): int {
  var xs: array<int> = [1, 2, 3]
  var idx: string = "1"
  return xs[idx]
}`,
			callable: "broken",
			code:     "invalid_array_index_type",
			path:     "vm/array/index_type",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := compileAndCall(t, tc.source, tc.callable, nil)
			if err == nil {
				t.Fatal("expected diagnostic error")
			}
			coder, ok := err.(interface{ DiagnosticCode() string })
			if !ok || coder.DiagnosticCode() != tc.code {
				t.Fatalf("expected %s diagnostic, got %v", tc.code, err)
			}
			pather, ok := err.(interface{ DiagnosticPath() string })
			if !ok || pather.DiagnosticPath() != tc.path {
				t.Fatalf("expected %s path, got %v", tc.path, err)
			}
		})
	}
}

func TestInterpreter_ScenarioExtendedDiagnosticMatrix(t *testing.T) {
	t.Run("missing_struct_field", func(t *testing.T) {
		prog, err := frontend.ParseModuleForTest(`
struct Point { x: int y: int }
fun broken(): int {
  var p: Point = Point{x: 1}
  return p.x
}`)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}
		_, err = NewCompiler().Compile(prog)
		if err == nil {
			t.Fatal("expected diagnostic error")
		}
		coder, ok := err.(interface{ DiagnosticCode() string })
		if !ok || coder.DiagnosticCode() != "missing_struct_field" {
			t.Fatalf("expected missing_struct_field diagnostic, got %v", err)
		}
		pather, ok := err.(interface{ DiagnosticPath() string })
		if !ok || pather.DiagnosticPath() != "bytecode/struct/literal" {
			t.Fatalf("expected bytecode/struct/literal path, got %v", err)
		}
	})

	cases := []struct {
		name     string
		source   string
		callable string
		code     string
		path     string
	}{
		{
			name: "map_key_not_found",
			source: `
fun broken(): int {
  var stats: map<string, int> = {"hp": 10}
  return stats["mp"]
}`,
			callable: "broken",
			code:     "map_key_not_found",
			path:     "vm/map/key",
		},
		{
			name: "array_index_out_of_range",
			source: `
fun broken(): int {
  var xs: array<int> = [1, 2, 3]
  return xs[5]
}`,
			callable: "broken",
			code:     "array_index_out_of_range",
			path:     "vm/array/index",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := compileAndCall(t, tc.source, tc.callable, nil)
			if err == nil {
				t.Fatal("expected diagnostic error")
			}
			coder, ok := err.(interface{ DiagnosticCode() string })
			if !ok || coder.DiagnosticCode() != tc.code {
				t.Fatalf("expected %s diagnostic, got %v", tc.code, err)
			}
			pather, ok := err.(interface{ DiagnosticPath() string })
			if !ok || pather.DiagnosticPath() != tc.path {
				t.Fatalf("expected %s path, got %v", tc.path, err)
			}
		})
	}
}

func TestInterpreter_StreamContractMatrix(t *testing.T) {
	prog, err := frontend.ParseModuleForTest(`stream fun emit(): int { yield 1 return 2 }`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	eval := NewVMEvaluator()
	if err := eval.CompileProgram(prog); err != nil {
		t.Fatalf("CompileProgram: %v", err)
	}

	t.Run("next_next_final", func(t *testing.T) {
		first, err := eval.Evaluate("emit", binding.InvocationStageNext, nil)
		if err != nil || first.(int) != 1 {
			t.Fatalf("expected first next 1, got %v err=%v", first, err)
		}
		_, err = eval.Evaluate("emit", binding.InvocationStageNext, nil)
		coder, ok := err.(interface{ DiagnosticCode() string })
		if err == nil || !ok || coder.DiagnosticCode() != "stream_exhausted" {
			t.Fatalf("expected stream_exhausted on second next, got %v", err)
		}
	})

	eval.Reset("emit", nil)
	t.Run("final_before_next", func(t *testing.T) {
		result, err := eval.Evaluate("emit", binding.InvocationStageFinal, nil)
		if err != nil {
			t.Fatalf("expected final result, got %v", err)
		}
		if result.(int) != 2 {
			t.Fatalf("expected final payload 2, got %v", result)
		}
		state, ok := eval.SessionsForTest()["emit|"]
		if !ok || !state.ExhaustedForTest() {
			t.Fatalf("expected exhausted session after final-before-next, got %+v", state)
		}
	})

	eval.Reset("emit", nil)
	t.Run("next_final_repeated_final", func(t *testing.T) {
		_, err := eval.Evaluate("emit", binding.InvocationStageNext, nil)
		if err != nil {
			t.Fatalf("first next: %v", err)
		}
		_, err = eval.Evaluate("emit", binding.InvocationStageFinal, nil)
		if err != nil {
			t.Fatalf("first final: %v", err)
		}
		_, err = eval.Evaluate("emit", binding.InvocationStageFinal, nil)
		coder, ok := err.(interface{ DiagnosticCode() string })
		if err == nil || !ok || coder.DiagnosticCode() != "stream_exhausted" {
			t.Fatalf("expected stream_exhausted on repeated final, got %v", err)
		}
	})
}

func TestInterpreter_InheritanceFieldAccess(t *testing.T) {
	source := "open class Animal { name: string fun greet(): string { return this.name } }\n" +
		"class Dog : Animal { breed: string fun say(): string { return this.breed } }\n" +
		"fun test(): string { var d: Dog = new Dog() d.name = \"Rex\" d.breed = \"Labrador\" return d.greet() }"
	v := compileAndRun(t, source)
	fn := v.FuncReg().GetFunction("test")
	if fn == nil {
		t.Fatal("function 'test' not found")
	}
	res := fn.ExecuteBody(v, nil)
	s := v.DecodeString(res)
	if s != "Rex" {
		t.Fatalf("expected 'Rex', got %q", s)
	}
}

func TestInterpreter_MethodOverride(t *testing.T) {
	source := "open class Base { open fun who(): int { return 1 } }\n" +
		"class Child : Base { override fun who(): int { return 2 } }\n" +
		"fun test(): int { var c: Child = new Child() return c.who() }"
	v := compileAndRun(t, source)
	fn := v.FuncReg().GetFunction("test")
	if fn == nil {
		t.Fatal("function 'test' not found")
	}
	res := fn.ExecuteBody(v, nil)
	if vm.DecodeInt(res) != 2 {
		t.Fatalf("expected 2, got %d", vm.DecodeInt(res))
	}
}

func TestInterpreter_SuperMethodCall(t *testing.T) {
	source := "open class Base { open fun value(): int { return 10 } }\n" +
		"class Child : Base { override fun value(): int { return 20 } fun parentValue(): int { return super.value() } }\n" +
		"fun test(): int { var c: Child = new Child() return c.parentValue() }"
	v := compileAndRun(t, source)
	fn := v.FuncReg().GetFunction("test")
	if fn == nil {
		t.Fatal("function 'test' not found")
	}
	res := fn.ExecuteBody(v, nil)
	if vm.DecodeInt(res) != 10 {
		t.Fatalf("expected 10 (super), got %d", vm.DecodeInt(res))
	}
}

func TestInterpreter_ConstructorWithArguments(t *testing.T) {
	source := "class Point { x: int y: int constructor(px: int, py: int) { this.x = px this.y = py } fun sum(): int { return this.x + this.y } }\n" +
		"fun test(): int { var p: Point = new Point(3, 4) return p.sum() }"
	v := compileAndRun(t, source)
	fn := v.FuncReg().GetFunction("test")
	if fn == nil {
		t.Fatal("function 'test' not found")
	}
	res := fn.ExecuteBody(v, nil)
	if vm.DecodeInt(res) != 7 {
		t.Fatalf("expected 7, got %d", vm.DecodeInt(res))
	}
}

func TestInterpreter_DivisionByZeroHasDiagnosticCode(t *testing.T) {
	_, err := compileAndCall(t,
		"fun div(a: int, b: int): int { return a / b }",
		"div",
		[]vm.Value{vm.EncodeInt(4), vm.EncodeInt(0)},
	)
	if err == nil {
		t.Fatal("expected division by zero error")
	}
	rtErr, ok := err.(*RuntimeError)
	if !ok {
		t.Fatalf("expected RuntimeError, got %T", err)
	}
	if rtErr.Code != "division_by_zero" {
		t.Fatalf("expected division_by_zero code, got %q", rtErr.Code)
	}
	if rtErr.Path != "vm/arithmetic/div" {
		t.Fatalf("expected vm/arithmetic/div path, got %q", rtErr.Path)
	}
	if rtErr.Category != diagnostics.CategoryRuntime {
		t.Fatalf("expected runtime category, got %q", rtErr.Category)
	}
	if len(rtErr.Stack) == 0 || rtErr.Stack[0].Callable != "div" {
		t.Fatalf("expected stack starting at div, got %+v", rtErr.Stack)
	}
}

func TestInterpreter_UndefinedFunctionHasDiagnosticCode(t *testing.T) {
	_, err := compileAndCall(t, "fun outer(): int { return missing() }", "outer", nil)
	if err == nil {
		t.Fatal("expected undefined function error")
	}
	rtErr, ok := err.(*RuntimeError)
	if !ok {
		t.Fatalf("expected RuntimeError, got %T", err)
	}
	if rtErr.Code != "undefined_function" {
		t.Fatalf("expected undefined_function code, got %q", rtErr.Code)
	}
	if rtErr.Path != "vm/call/function" {
		t.Fatalf("expected vm/call/function path, got %q", rtErr.Path)
	}
}

func TestInterpreter_StreamYieldParses(t *testing.T) {
	source := `stream fun emit(): int { yield 1 return 2 }`
	prog, err := frontend.ParseModuleForTest(source)
	if err != nil {
		t.Fatalf("expected yield to parse, got %v", err)
	}
	if prog == nil || len(prog.Stmts) != 1 {
		t.Fatalf("expected one parsed stmt, got %+v", prog)
	}
}

func TestInterpreter_StreamSingleYieldThenFinal(t *testing.T) {
	prog, err := frontend.ParseModuleForTest("stream fun emit(): int { yield 1 return 2 }")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	v := vm.NewVM(4096, 256)
	c := newCompiler()
	_, err = c.compile(prog)
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	interp := newInterpreter(v)
	chunk := c.getFunctions()["emit"]
	first, ip, stack, locals, err := interp.ExecuteUntilYield(chunk, nil)
	if err != nil {
		t.Fatalf("first yield: %v", err)
	}
	if vm.DecodeInt(first) != 1 {
		t.Fatalf("expected first yield 1, got %d", vm.DecodeInt(first))
	}
	final, _, _, _, err := interp.runUntilBoundary(chunk, ip, stack, locals, nil, false, false)
	if err != nil {
		t.Fatalf("final return: %v", err)
	}
	if vm.DecodeInt(final) != 2 {
		t.Fatalf("expected final 2, got %d", vm.DecodeInt(final))
	}
}

func TestInterpreter_StreamUnaryStageBoundary(t *testing.T) {
	prog, err := frontend.ParseModuleForTest("stream fun emit(): int { yield 1 return 2 }")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	v := vm.NewVM(4096, 256)
	c := newCompiler()
	_, err = c.compile(prog)
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	interp := newInterpreter(v)
	c.registerFunctions(v, interp)
	chunk := c.getFunctions()["emit"]

	_, err = interp.ExecuteFunction(chunk, binding.InvocationStageUnary, nil)
	if err != nil {
		t.Fatalf("expected current unary stage behavior to avoid interpreter error, got %v", err)
	}
}

func TestInterpreter_StreamNextAfterExhaustion(t *testing.T) {
	prog, err := frontend.ParseModuleForTest("stream fun emit(): int { yield 1 return 2 }")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	eval := NewVMEvaluator()
	if err := eval.CompileProgram(prog); err != nil {
		t.Fatalf("CompileProgram: %v", err)
	}
	_, err = eval.Evaluate("emit", binding.InvocationStageNext, nil)
	if err != nil {
		t.Fatalf("first next: %v", err)
	}
	_, err = eval.Evaluate("emit", binding.InvocationStageFinal, nil)
	if err != nil {
		t.Fatalf("final: %v", err)
	}
	_, err = eval.Evaluate("emit", binding.InvocationStageNext, nil)
	if err == nil {
		t.Fatal("expected stream_exhausted after final")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "stream_exhausted" {
		t.Fatalf("expected stream_exhausted diagnostic, got %v", err)
	}
}

func TestInterpreter_StreamRepeatedFinal(t *testing.T) {
	prog, err := frontend.ParseModuleForTest("stream fun emit(): int { yield 1 return 2 }")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	eval := NewVMEvaluator()
	if err := eval.CompileProgram(prog); err != nil {
		t.Fatalf("CompileProgram: %v", err)
	}
	_, err = eval.Evaluate("emit", binding.InvocationStageNext, nil)
	if err != nil {
		t.Fatalf("first next: %v", err)
	}
	_, err = eval.Evaluate("emit", binding.InvocationStageFinal, nil)
	if err != nil {
		t.Fatalf("first final: %v", err)
	}
	_, err = eval.Evaluate("emit", binding.InvocationStageFinal, nil)
	if err == nil {
		t.Fatal("expected stream_exhausted on repeated final")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "stream_exhausted" {
		t.Fatalf("expected stream_exhausted diagnostic, got %v", err)
	}
}

func TestInterpreter_StreamMultipleYieldsResume(t *testing.T) {
	prog, err := frontend.ParseModuleForTest("stream fun emit(): int { yield 1 yield 2 return 3 }")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	v := vm.NewVM(4096, 256)
	c := newCompiler()
	_, err = c.compile(prog)
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	interp := newInterpreter(v)
	chunk := c.getFunctions()["emit"]
	first, ip, stack, locals, err := interp.ExecuteUntilYield(chunk, nil)
	if err != nil {
		t.Fatalf("first yield: %v", err)
	}
	if vm.DecodeInt(first) != 1 {
		t.Fatalf("expected first yield 1, got %d", vm.DecodeInt(first))
	}
	second, ip2, stack2, locals2, err := interp.runUntilBoundary(chunk, ip, stack, locals, nil, true, true)
	if err != nil {
		t.Fatalf("second yield: %v", err)
	}
	if vm.DecodeInt(second) != 2 {
		t.Fatalf("expected second yield 2, got %d", vm.DecodeInt(second))
	}
	final, _, _, _, err := interp.runUntilBoundary(chunk, ip2, stack2, locals2, nil, false, false)
	if err != nil {
		t.Fatalf("final return: %v", err)
	}
	if vm.DecodeInt(final) != 3 {
		t.Fatalf("expected final 3, got %d", vm.DecodeInt(final))
	}
}

func TestInterpreter_ReservedTokenAwaitRejected(t *testing.T) {
	source := `fun bad(): int { await x() }`
	_, err := frontend.ParseModuleForTest(source)
	if err == nil {
		t.Fatal("expected parse error for await keyword")
	}
	if !strings.Contains(err.Error(), "await") {
		t.Fatalf("expected await-related error, got: %v", err)
	}
}

func TestInterpreter_GlobalAssignment(t *testing.T) {
	source := `
var count: int = 5
count = 10
fun read(): int { return count }`
	v := compileAndRun(t, source)
	fn := v.FuncReg().GetFunction("read")
	if fn == nil {
		t.Fatal("function 'read' not found")
	}
	res := fn.ExecuteBody(v, nil)
	if vm.DecodeInt(res) != 10 {
		t.Fatalf("expected 10 after global assignment, got %d", vm.DecodeInt(res))
	}
}

// --- Interface runtime tests ---

func TestInterpreter_InterfaceIsCheck(t *testing.T) {
	source := `interface Greeter { fun greet(): string }
class Person : Greeter { name: string fun greet(): string { return this.name } }
fun check(): bool { var p: Person = new Person() p.name = "hi" return p is Greeter }`
	result, err := compileAndCall(t, source, "check", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !vm.DecodeBool(result) {
		t.Fatal("expected Person instance to pass 'is Greeter' check")
	}
}

func TestInterpreter_InterfaceIsCheckNegative(t *testing.T) {
	source := `interface Greeter { fun greet(): string }
class Dog { name: string fun bark(): string { return this.name } }
fun check(): bool { var d: Dog = new Dog() d.name = "woof" return d is Greeter }`
	result, err := compileAndCall(t, source, "check", nil)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeBool(result) {
		t.Fatal("expected Dog instance to fail 'is Greeter' check")
	}
}

func TestInterpreter_InterfaceRegistryPopulated(t *testing.T) {
	source := `interface Printable { fun toString(): string }
class Doc : Printable { fun toString(): string { return "doc" } }
fun main(): int { return 1 }`
	v := compileAndRun(t, source)
	iface := v.IfaceReg().GetInterfaceByName("Printable")
	if iface == nil {
		t.Fatal("expected interface 'Printable' to be registered")
	}
}

func TestInterpreter_ClassImplementsInterfaceInRegistry(t *testing.T) {
	source := `interface Flyable { fun fly(): string }
class Bird : Flyable { fun fly(): string { return "soar" } }
fun main(): int { return 1 }`
	v := compileAndRun(t, source)
	cls := v.ClassReg().GetClassByName("Bird")
	if cls == nil {
		t.Fatal("class Bird not found")
	}
	ok := v.IfaceReg().IsInstanceOfInterface("Bird", "Flyable", v.ClassReg())
	if !ok {
		t.Fatal("expected Bird to implement Flyable in registry")
	}
}

func TestInterpreter_SuperConstructorCall(t *testing.T) {
	source := `open class Animal { name: string constructor(n: string) { this.name = n } }
class Dog : Animal { breed: string constructor(n: string, b: string) { super(n) this.breed = b } fun info(): string { return this.name } }
fun make(): string { var d: Dog = new Dog("Rex", "Lab") return d.info() }`
	v := compileAndRun(t, source)
	fn := v.FuncReg().GetFunction("make")
	if fn == nil {
		t.Fatal("function 'make' not found")
	}
	res := fn.ExecuteBody(v, nil)
	s := v.DecodeString(res)
	if s != "Rex" {
		t.Fatalf("expected 'Rex', got %q", s)
	}
}

func TestInterpreter_SuperConstructorInitializesParentFields(t *testing.T) {
	source := `open class Base { x: int constructor(v: int) { this.x = v } }
class Child : Base { y: int constructor(a: int, b: int) { super(a) this.y = b } fun sum(): int { return this.x + this.y } }
fun calc(): int { var c: Child = new Child(10, 20) return c.sum() }`
	result, err := compileAndCall(t, source, "calc", nil)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 30 {
		t.Fatalf("expected 30, got %d", vm.DecodeInt(result))
	}
}

// --- long/ulong/double tests ---

func TestInterpreter_LongLiteral(t *testing.T) {
	v := compileAndRun(t, `fun main() { var x: long = 1000000000 }`)
	_ = v // just verify no crash; long stored in VM
}

func TestInterpreter_LongAddition(t *testing.T) {
	source := `fun add(): long { var a: long = 2000000000 var b: long = 2000000000 return a + b }`
	result, v, err := compileAndCallWithVM(t, source, "add", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !vm.IsLong(result) {
		t.Fatalf("expected long result, got tag %d", result)
	}
	if v.DecodeLong(result) != 4000000000 {
		t.Fatalf("expected 4000000000, got %d", v.DecodeLong(result))
	}
}

func TestInterpreter_ULongLiteral(t *testing.T) {
	// Use a value that fits in int64 during parsing, then gets encoded as ulong.
	source := `fun calc(): ulong { var x: ulong = 4294967295 return x }`
	result, v, err := compileAndCallWithVM(t, source, "calc", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !vm.IsULong(result) {
		t.Fatalf("expected ulong result, got tag %d", result)
	}
	if v.DecodeULong(result) != 4294967295 {
		t.Fatalf("expected 4294967295, got %d", v.DecodeULong(result))
	}
}

func TestInterpreter_DoubleLiteral(t *testing.T) {
	source := `fun calc(): double { var x: double = 3.141592653589793 return x }`
	result, v, err := compileAndCallWithVM(t, source, "calc", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !vm.IsDouble(result) {
		t.Fatalf("expected double result, got tag %d", result)
	}
	if v.DecodeDouble(result) != 3.141592653589793 {
		t.Fatalf("expected 3.141592653589793, got %f", v.DecodeDouble(result))
	}
}

func TestInterpreter_LongComparison(t *testing.T) {
	source := `fun cmp(): bool { var a: long = 100 var b: long = 200 return a < b }`
	result, err := compileAndCall(t, source, "cmp", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !vm.DecodeBool(result) {
		t.Fatal("expected true for 100 < 200")
	}
}

func TestInterpreter_ULongArithmetic(t *testing.T) {
	source := `fun calc(): ulong { var a: ulong = 100 var b: ulong = 50 return a - b }`
	result, v, err := compileAndCallWithVM(t, source, "calc", nil)
	if err != nil {
		t.Fatal(err)
	}
	if v.DecodeULong(result) != 50 {
		t.Fatalf("expected 50, got %d", v.DecodeULong(result))
	}
}

func TestInterpreter_DoubleMultiplication(t *testing.T) {
	source := `fun calc(): double { var a: double = 2.5 var b: double = 4.0 return a * b }`
	result, v, err := compileAndCallWithVM(t, source, "calc", nil)
	if err != nil {
		t.Fatal(err)
	}
	if v.DecodeDouble(result) != 10.0 {
		t.Fatalf("expected 10.0, got %f", v.DecodeDouble(result))
	}
}

func TestInterpreter_SuperConstructorWithoutParentReturnsDiagnostic(t *testing.T) {
	err := compileAndExpectError(t, `class A { fun bad(): int { super() return 1 } }
fun test(): int { var a: A = new A() return a.bad() }`, "test")
	if err == nil {
		t.Fatal("expected compile error for super constructor without parent")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "super_without_parent" {
		t.Fatalf("expected super_without_parent, got %v", err)
	}
}

func TestInterpreter_SuperConstructorOutsideClassReturnsDiagnostic(t *testing.T) {
	err := compileAndExpectError(t, `fun test(): int { super() return 1 }`, "test")
	if err == nil {
		t.Fatal("expected compile error for super constructor outside class")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "super_without_parent" {
		t.Fatalf("expected super_without_parent, got %v", err)
	}
}

func TestInterpreter_SuperExpressionBodyOutsideClassReturnsDiagnostic(t *testing.T) {
	err := compileAndExpectError(t, `fun test(): int = super.answer()`, "test")
	if err == nil {
		t.Fatal("expected compile error for super expression body outside class")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "super_without_parent" {
		t.Fatalf("expected super_without_parent, got %v", err)
	}
}

func TestInterpreter_SuperConstructorExpressionBodyWithoutParentReturnsDiagnostic(t *testing.T) {
	err := compileAndExpectError(t, `class A { fun bad(): int = super() }
fun test(): int { var a: A = new A() return a.bad() }`, "test")
	if err == nil {
		t.Fatal("expected compile error for super constructor expression body without parent")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "super_without_parent" {
		t.Fatalf("expected super_without_parent, got %v", err)
	}
}

func TestInterpreter_SuperMethodExpressionBodyWithoutParentReturnsDiagnostic(t *testing.T) {
	err := compileAndExpectError(t, `class A { fun bad(): int = super.answer() }
fun test(): int { var a: A = new A() return a.bad() }`, "test")
	if err == nil {
		t.Fatal("expected compile error for super method expression body without parent")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "super_without_parent" {
		t.Fatalf("expected super_without_parent, got %v", err)
	}
}

// --- Access control tests ---

// compileAndExpectError parses and compiles source, returning any error.
// It does NOT call t.Fatalf on compile errors — the caller checks err.
func compileAndExpectError(t *testing.T, source, fnName string) error {
	t.Helper()

	prog, err := frontend.ParseModuleForTest(source)
	if err != nil {
		return err
	}

	v := vm.NewVM(4096, 256)
	c := newCompiler()
	_, err = c.compile(prog)
	if err != nil {
		return err
	}

	interp := newInterpreter(v)
	c.registerFunctions(v, interp)
	c.registerClasses(v)
	c.registerStructs(v)
	return nil
}

func TestInterpreter_PrivateFieldAccessDeniedOutsideClass(t *testing.T) {
	source := `class Foo { private name: string constructor(n: string) { this.name = n } fun greet(): string { return this.name } }
fun test(): string { var f: Foo = new Foo("hello") return f.name }`
	err := compileAndExpectError(t, source, "test")
	if err == nil {
		t.Fatal("expected compile error for accessing private field outside class")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "private_field_access_denied" {
		t.Fatalf("expected private_field_access_denied diagnostic, got %v", err)
	}
	pather, ok := err.(interface{ DiagnosticPath() string })
	if !ok || pather.DiagnosticPath() != "bytecode/access/field" {
		t.Fatalf("expected bytecode/access/field path, got %v", err)
	}
}

func TestInterpreter_PrivateFieldAccessAllowedInsideClass(t *testing.T) {
	source := `class Foo { private name: string constructor(n: string) { this.name = n } fun greet(): string { return this.name } }
fun test(): string { var f: Foo = new Foo("hello") return f.greet() }`
	result, err := compileAndCall(t, source, "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	v := vm.NewVM(4096, 256)
	if v.DecodeString(result) != "hello" {
		t.Fatalf("expected 'hello', got %q", v.DecodeString(result))
	}
}

func TestInterpreter_PrivateMethodCallDeniedOutsideClass(t *testing.T) {
	source := `class Foo { private fun helper(): string { return "hidden" } fun visible(): string { return this.helper() } }
fun test(): string { var f: Foo = new Foo() return f.helper() }`
	err := compileAndExpectError(t, source, "test")
	if err == nil {
		t.Fatal("expected compile error for calling private method outside class")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "private_method_access_denied" {
		t.Fatalf("expected private_method_access_denied diagnostic, got %v", err)
	}
	pather, ok := err.(interface{ DiagnosticPath() string })
	if !ok || pather.DiagnosticPath() != "bytecode/access/method" {
		t.Fatalf("expected bytecode/access/method path, got %v", err)
	}
}

func TestInterpreter_PrivateMethodCallAllowedInsideClass(t *testing.T) {
	source := `class Foo { private fun helper(): string { return "hidden" } fun visible(): string { return this.helper() } }
fun test(): string { var f: Foo = new Foo() return f.visible() }`
	result, err := compileAndCall(t, source, "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	v := vm.NewVM(4096, 256)
	if v.DecodeString(result) != "hidden" {
		t.Fatalf("expected 'hidden', got %q", v.DecodeString(result))
	}
}

func TestInterpreter_PrivateFieldSameNameOnOtherClassDoesNotDenyPublicAccess(t *testing.T) {
	source := `class Foo { private name: string constructor(n: string) { this.name = n } }
class Bar { name: string constructor(n: string) { this.name = n } }
fun test(): string { var b: Bar = new Bar("public") return b.name }`
	result, err := compileAndCall(t, source, "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	v := vm.NewVM(4096, 256)
	if v.DecodeString(result) != "public" {
		t.Fatalf("expected 'public', got %q", v.DecodeString(result))
	}
}

func TestInterpreter_PrivateMethodSameNameOnOtherClassDoesNotDenyPublicCall(t *testing.T) {
	source := `class Foo { private fun helper(): string { return "hidden" } }
class Bar { fun helper(): string { return "public" } }
fun test(): string { var b: Bar = new Bar() return b.helper() }`
	result, err := compileAndCall(t, source, "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	v := vm.NewVM(4096, 256)
	if v.DecodeString(result) != "public" {
		t.Fatalf("expected 'public', got %q", v.DecodeString(result))
	}
}

func TestInterpreter_GlobalPrivateFieldAccessDeniedOutsideClass(t *testing.T) {
	source := `class Foo { private name: string constructor(n: string) { this.name = n } }
var f: Foo = new Foo("secret")
fun test(): string { return f.name }`
	err := compileAndExpectError(t, source, "test")
	if err == nil {
		t.Fatal("expected compile error for global private field access")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "private_field_access_denied" {
		t.Fatalf("expected private_field_access_denied, got %v", err)
	}
}

func TestInterpreter_GlobalPrivateMethodAccessDeniedOutsideClass(t *testing.T) {
	source := `class Foo { private fun helper(): string { return "secret" } }
var f: Foo = new Foo()
fun test(): string { return f.helper() }`
	err := compileAndExpectError(t, source, "test")
	if err == nil {
		t.Fatal("expected compile error for global private method access")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "private_method_access_denied" {
		t.Fatalf("expected private_method_access_denied, got %v", err)
	}
}

func TestInterpreter_ChildAccessParentPrivateFieldDenied(t *testing.T) {
	source := `open class Base { private value: string constructor(v: string) { this.value = v } }
class Child : Base { fun reveal(): string { return this.value } }
fun test(): string { var c: Child = new Child("secret") return c.reveal() }`
	err := compileAndExpectError(t, source, "test")
	if err == nil {
		t.Fatal("expected compile error for child private field access")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "private_field_access_denied" {
		t.Fatalf("expected private_field_access_denied, got %v", err)
	}
}

func TestInterpreter_FieldShadowingAncestorFieldRejected(t *testing.T) {
	source := `open class Base { private value: string }
class Child : Base { value: string }
fun test(): int { return 1 }`
	err := compileAndExpectError(t, source, "test")
	if err == nil {
		t.Fatal("expected compile error for field shadowing")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "field_shadowing_disallowed" {
		t.Fatalf("expected field_shadowing_disallowed, got %v", err)
	}
}

func TestInterpreter_PrivateOverrideRejected(t *testing.T) {
	source := `open class Base { open fun value(): string { return "base" } }
class Child : Base { private override fun value(): string { return "child" } }
fun test(): int { return 1 }`
	err := compileAndExpectError(t, source, "test")
	if err == nil {
		t.Fatal("expected compile error for private override")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "override_visibility_narrowed" {
		t.Fatalf("expected override_visibility_narrowed, got %v", err)
	}
}

func TestInterpreter_ChainedParentFieldPrivateAccessDenied(t *testing.T) {
	source := `class Secret { private value: string constructor(v: string) { this.value = v } }
open class Base { holder: Secret constructor(s: Secret) { this.holder = s } }
class Child : Base {}
fun test(): string { var c: Child = new Child(new Secret("secret")) return c.holder.value }`
	err := compileAndExpectError(t, source, "test")
	if err == nil {
		t.Fatal("expected compile error for chained private field access")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "private_field_access_denied" {
		t.Fatalf("expected private_field_access_denied, got %v", err)
	}
}

func TestInterpreter_PrivateMethodShadowingAncestorMethodRejected(t *testing.T) {
	source := `open class Base { open fun value(): string { return "base" } }
class Child : Base { private fun value(): string { return "child" } }
fun test(): string { var b: Base = new Child() return b.value() }`
	err := compileAndExpectError(t, source, "test")
	if err == nil {
		t.Fatal("expected compile error for private method shadowing")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "method_visibility_narrowed" {
		t.Fatalf("expected method_visibility_narrowed, got %v", err)
	}
}

func TestInterpreter_YieldOutsideStreamFunRejected(t *testing.T) {
	err := compileAndExpectError(t, `fun test(): int { yield 1 return 2 }`, "test")
	if err == nil {
		t.Fatal("expected compile error for yield outside stream fun")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "yield_requires_stream_fun" {
		t.Fatalf("expected yield_requires_stream_fun, got %v", err)
	}
}

func TestInterpreter_StreamFunYieldStillCompiles(t *testing.T) {
	result, err := compileAndCall(t, `stream fun test(): int { yield 1 return 2 }`, "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 2 {
		t.Fatalf("expected unary stream call to return final 2, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_StreamYieldInsideTryRejected(t *testing.T) {
	err := compileAndExpectError(t, `
stream fun gen(): int {
  var arr: array<int> = [1, 2, 3]
  try {
    yield 1
    var x: int = arr[10]
    return x
  } catch (e) {
    return -1
  }
}`, "gen")
	if err == nil {
		t.Fatal("expected compile error for yield inside try block")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "yield_disallowed_in_try" {
		t.Fatalf("expected yield_disallowed_in_try, got %v", err)
	}
}

func TestInterpreter_StreamYieldInsideCatchAllowed(t *testing.T) {
	result, err := compileAndCall(t, `
stream fun gen(): int {
  var arr: array<int> = [1, 2, 3]
  try {
    var x: int = arr[10]
    return x
  } catch (e) {
    yield -1
    return 42
  }
}`, "gen", nil)
	if err != nil {
		t.Fatal(err)
	}
	if vm.DecodeInt(result) != 42 {
		t.Fatalf("expected final 42 after catch-body suspension, got %d", vm.DecodeInt(result))
	}
}

func TestInterpreter_StreamFunYieldRequiresValue(t *testing.T) {
	err := compileAndExpectError(t, `stream fun test(): int { yield; return 2 }`, "test")
	if err == nil {
		t.Fatal("expected compile error for valueless yield")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "yield_value_required" {
		t.Fatalf("expected yield_value_required, got %v", err)
	}
}

func TestInterpreter_StreamFunStateDoesNotLeakToFollowingFun(t *testing.T) {
	err := compileAndExpectError(t, `stream fun ok(): int { yield 1 return 2 }
fun test(): int { yield 3 return 4 }`, "test")
	if err == nil {
		t.Fatal("expected compile error for yield after stream fun")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "yield_requires_stream_fun" {
		t.Fatalf("expected yield_requires_stream_fun, got %v", err)
	}
}

func TestInterpreter_ClassMethodYieldRejected(t *testing.T) {
	err := compileAndExpectError(t, `class Box { fun test(): int { yield 1 return 2 } }
fun call(): int { var b: Box = new Box() return b.test() }`, "call")
	if err == nil {
		t.Fatal("expected compile error for yield inside class method")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "yield_requires_stream_fun" {
		t.Fatalf("expected yield_requires_stream_fun, got %v", err)
	}
}

// --- Type alias tests ---

func TestInterpreter_TypeAliasLong(t *testing.T) {
	source := `type UserId = long
fun calc(): long { var id: UserId = 42 return id }`
	result, v, err := compileAndCallWithVM(t, source, "calc", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !vm.IsLong(result) {
		t.Fatalf("expected long result, got tag %d", result)
	}
	if v.DecodeLong(result) != 42 {
		t.Fatalf("expected 42, got %d", v.DecodeLong(result))
	}
}

func TestInterpreter_TypeAliasDouble(t *testing.T) {
	source := `type Distance = double
fun calc(): double { var d: Distance = 3.14 return d }`
	result, v, err := compileAndCallWithVM(t, source, "calc", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !vm.IsDouble(result) {
		t.Fatalf("expected double result, got tag %d", result)
	}
	if v.DecodeDouble(result) != 3.14 {
		t.Fatalf("expected 3.14, got %f", v.DecodeDouble(result))
	}
}

func TestInterpreter_TypeAliasChain(t *testing.T) {
	source := `type Id = long
type UserId = Id
fun calc(): long { var uid: UserId = 100 return uid }`
	result, v, err := compileAndCallWithVM(t, source, "calc", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !vm.IsLong(result) {
		t.Fatalf("expected long result, got tag %d", result)
	}
	if v.DecodeLong(result) != 100 {
		t.Fatalf("expected 100, got %d", v.DecodeLong(result))
	}
}

func TestInterpreter_NestedArrayLiteral(t *testing.T) {
	result, err := compileAndCall(t, `
fun test(): int {
  var xs: array<array<int>> = [[1, 2], [3, 4]]
  return xs[0][1]
}`, "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 2 {
		t.Fatalf("expected nested array literal index 2, got %d", got)
	}
}

func TestInterpreter_NestedMapLiteral(t *testing.T) {
	result, err := compileAndCall(t, `
fun test(): int {
  var m: map<string, map<string, int>> = {"a": {"b": 1}}
  return m["a"]["b"]
}`, "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 1 {
		t.Fatalf("expected nested map literal value 1, got %d", got)
	}
}

func TestInterpreter_NestedMapArrayLiteral(t *testing.T) {
	result, err := compileAndCall(t, `
fun test(): int {
  var m: map<string, array<int>> = {"a": [10, 20, 30]}
  return m["a"][1]
}`, "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 20 {
		t.Fatalf("expected nested map-array literal value 20, got %d", got)
	}
}
