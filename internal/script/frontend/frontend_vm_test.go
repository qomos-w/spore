package frontend_test

import (
	"strings"
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/internal/script/bytecode"
	"github.com/qomos-w/spore/internal/script/frontend"
	"github.com/qomos-w/spore/schema"
)

// newVMFrontend creates a Frontend with bytecode VM compilation and evaluation.
func newVMFrontend(t *testing.T) *frontend.Frontend {
	t.Helper()
	sb := binding.NewScriptBinding()
	f, err := frontend.New(sb)
	if err != nil {
		t.Fatalf("frontend.New: %v", err)
	}
	vmEval := bytecode.NewVMEvaluator()
	f.SetVMCompileHook(vmEval)
	return f
}

func newVMFrontendWithResolver(t *testing.T, resolver frontend.ModuleResolver) *frontend.Frontend {
	t.Helper()
	f := newVMFrontend(t)
	f.SetModuleResolver(resolver)
	return f
}

func TestVMFrontend_YieldOutsideStreamFunRejected(t *testing.T) {
	f := newVMFrontend(t)
	err := f.LoadSource(`fun bad(): int { yield 1 return 2 }`)
	if err == nil {
		t.Fatal("expected yield outside stream fun diagnostic")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "yield_requires_stream_fun" {
		t.Fatalf("expected yield_requires_stream_fun, got %v", err)
	}
}

func TestVMFrontend_StreamFunStateDoesNotLeakToFollowingFun(t *testing.T) {
	f := newVMFrontend(t)
	err := f.LoadSource(`stream fun ok(): int { yield 1 return 2 }
fun bad(): int { yield 3 return 4 }`)
	if err == nil {
		t.Fatal("expected yield outside stream fun diagnostic")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "yield_requires_stream_fun" {
		t.Fatalf("expected yield_requires_stream_fun, got %v", err)
	}
}

func TestVMFrontend_ClassMethodYieldRejected(t *testing.T) {
	f := newVMFrontend(t)
	err := f.LoadSource(`class Box { fun bad(): int { yield 1 return 2 } }
fun call(): int { var b: Box = new Box() return b.bad() }`)
	if err == nil {
		t.Fatal("expected yield in method diagnostic")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "yield_requires_stream_fun" {
		t.Fatalf("expected yield_requires_stream_fun, got %v", err)
	}
}

func TestVMFrontend_PackageDeclarationAccepted(t *testing.T) {
	f := newVMFrontend(t)
	if err := f.LoadSource("package demo.net\nfun calc(): int { return 40 + 2 }"); err != nil {
		t.Fatalf("LoadSource with package declaration: %v", err)
	}
	if got := f.CompiledDeclarations().PackageName(); got != "demo.net" {
		t.Fatalf("expected package name 'demo.net', got %q", got)
	}
	outcome, err := f.Invoke("calc", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Error != nil {
		t.Fatalf("unexpected invocation error: %+v", outcome.Result.Error)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 42 {
		t.Fatalf("expected payload 42, got outcome=%+v payload=%+v", outcome, outcome.Payload)
	}
}

func TestVMFrontend_ScriptImportFunctionExecutes(t *testing.T) {
	resolver := frontend.MapModuleResolver{
		"math": `export fun add(a: int, b: int): int { return a + b }`,
	}
	f := newVMFrontendWithResolver(t, resolver)
	if err := f.LoadSource("import add from \"math\"\nfun calc(): int { return add(1, 2) }"); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	outcome, err := f.Invoke("calc", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Error != nil {
		t.Fatalf("unexpected invocation error: %+v", outcome.Result.Error)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 3 {
		t.Fatalf("expected payload 3, got outcome=%+v payload=%+v", outcome, outcome.Payload)
	}
}

func TestVMFrontend_ScriptImportVariableReadsValue(t *testing.T) {
	resolver := frontend.MapModuleResolver{
		"math": `export var answer: int = 41`,
	}
	f := newVMFrontendWithResolver(t, resolver)
	if err := f.LoadSource("import answer from \"math\"\nfun calc(): int { return answer }"); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	outcome, err := f.Invoke("calc", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 41 {
		t.Fatalf("expected payload 41, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_AssignImportedVariableRejected(t *testing.T) {
	resolver := frontend.MapModuleResolver{
		"math": `export var answer: int = 41`,
	}
	f := newVMFrontendWithResolver(t, resolver)
	err := f.LoadSource("import answer from \"math\"\nfun calc(): int { answer = 42 return answer }")
	if err == nil {
		t.Fatal("expected imported variable assignment error")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "assign_imported_variable" {
		t.Fatalf("expected assign_imported_variable, got %v", err)
	}
}

func TestVMFrontend_ImportedGlobalPrivateFieldAccessRejected(t *testing.T) {
	resolver := frontend.MapModuleResolver{
		"secret": `class Box { private value: string constructor(v: string) { this.value = v } }
export var box: Box = new Box("secret")`,
	}
	f := newVMFrontendWithResolver(t, resolver)
	err := f.LoadSource(`import box from "secret"
fun leak(): string { return box.value }`)
	if err == nil {
		t.Fatal("expected imported private field access diagnostic")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "private_field_access_denied" {
		t.Fatalf("expected private_field_access_denied, got %v", err)
	}
}

func TestVMFrontend_ImportedGlobalPrivateMethodAccessRejected(t *testing.T) {
	resolver := frontend.MapModuleResolver{
		"secret": `class Box { private fun reveal(): string { return "secret" } }
export var box: Box = new Box()`,
	}
	f := newVMFrontendWithResolver(t, resolver)
	err := f.LoadSource(`import box from "secret"
fun leak(): string { return box.reveal() }`)
	if err == nil {
		t.Fatal("expected imported private method access diagnostic")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "private_method_access_denied" {
		t.Fatalf("expected private_method_access_denied, got %v", err)
	}
}

func TestVMFrontend_ImportedGlobalPublicFieldAccessExecutes(t *testing.T) {
	resolver := frontend.MapModuleResolver{
		"secret": `class Box { value: double }
export var box: Box = new Box()`,
	}
	f := newVMFrontendWithResolver(t, resolver)
	if err := f.LoadSource(`import box from "secret"
fun read_field(): double { return box.value }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	fieldOutcome, err := f.Invoke("read_field", nil)
	if err != nil {
		t.Fatalf("Invoke read_field: %v", err)
	}
	if fieldOutcome.Payload == nil || fieldOutcome.Payload.Value != 0.0 {
		t.Fatalf("expected zero public field, got %+v", fieldOutcome.Payload)
	}
}

func TestVMFrontend_ImportedGlobalPublicMethodDispatchExecutes(t *testing.T) {
	resolver := frontend.MapModuleResolver{
		"secret": `class Box { value: int constructor(v: int) { this.value = v } fun read(): int { return this.value } }
export var box: Box = new Box(42)`,
	}
	f := newVMFrontendWithResolver(t, resolver)
	if err := f.LoadSource(`import box from "secret"
fun read_method(): int { return box.read() }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	outcome, err := f.Invoke("read_method", nil)
	if err != nil {
		t.Fatalf("Invoke read_method: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 42 {
		t.Fatalf("expected 42 method result, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_ImportedGlobalMethodWithoutFieldAccessExecutes(t *testing.T) {
	resolver := frontend.MapModuleResolver{
		"lib": `class Magic { fun number(): int { return 7 } }
export var m: Magic = new Magic()`,
	}
	f := newVMFrontendWithResolver(t, resolver)
	if err := f.LoadSource(`import m from "lib"
fun call_method(): int { return m.number() }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	outcome, err := f.Invoke("call_method", nil)
	if err != nil {
		t.Fatalf("Invoke call_method: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 7 {
		t.Fatalf("expected 7, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_ReExportImportedGlobalPrivateFieldAccessRejected(t *testing.T) {
	resolver := frontend.MapModuleResolver{
		"secret": `class Box { private value: string constructor(v: string) { this.value = v } }
export var box: Box = new Box("secret")`,
		"api": `export box from "secret"`,
	}
	f := newVMFrontendWithResolver(t, resolver)
	err := f.LoadSource(`import box from "api"
fun leak(): string { return box.value }`)
	if err == nil {
		t.Fatal("expected re-exported imported private field access diagnostic")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "private_field_access_denied" {
		t.Fatalf("expected private_field_access_denied, got %v", err)
	}
}

func TestVMFrontend_ReExportImportedGlobalPrivateMethodAccessRejected(t *testing.T) {
	resolver := frontend.MapModuleResolver{
		"secret": `class Box { private fun reveal(): string { return "secret" } }
export var box: Box = new Box()`,
		"api": `export box from "secret"`,
	}
	f := newVMFrontendWithResolver(t, resolver)
	err := f.LoadSource(`import box from "api"
fun leak(): string { return box.reveal() }`)
	if err == nil {
		t.Fatal("expected re-exported imported private method access diagnostic")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "private_method_access_denied" {
		t.Fatalf("expected private_method_access_denied, got %v", err)
	}
}

func TestVMFrontend_WildcardReExportImportedGlobalPrivateAccessRejected(t *testing.T) {
	resolver := frontend.MapModuleResolver{
		"secret": `class Box { private value: string constructor(v: string) { this.value = v } }
export var box: Box = new Box("secret")`,
		"api": `export * from "secret"`,
	}
	f := newVMFrontendWithResolver(t, resolver)
	err := f.LoadSource(`import box from "api"
fun leak(): string { return box.value }`)
	if err == nil {
		t.Fatal("expected wildcard re-exported imported private field access diagnostic")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "private_field_access_denied" {
		t.Fatalf("expected private_field_access_denied, got %v", err)
	}
}

func TestVMFrontend_NativeImportAliasExecutes(t *testing.T) {
	sb := binding.NewScriptBinding()
	builder := binding.NewCapability("tool", "service")
	type toolInput struct{ Text string }
	type toolOutput struct{ Text string }
	if err := builder.AddFunction("execute", func(in toolInput) (toolOutput, error) {
		return toolOutput{Text: "ok:" + in.Text}, nil
	}); err != nil {
		t.Fatalf("AddFunction: %v", err)
	}
	cap, err := builder.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := sb.RegisterCapability(cap); err != nil {
		t.Fatalf("RegisterCapability: %v", err)
	}
	if err := sb.ExposeCapabilityCallables("tool"); err != nil {
		t.Fatalf("ExposeCapabilityCallables: %v", err)
	}
	f, err := frontend.New(sb)
	if err != nil {
		t.Fatalf("frontend.New: %v", err)
	}
	vmEval := bytecode.NewVMEvaluator()
	vmEval.SetNativeBinding(sb)
	f.SetVMCompileHook(vmEval)
	f.SetModuleResolver(frontend.MapModuleResolver{})
	if err := f.LoadSource("import execute from \"tool\"\nfun call_tool(): string { return execute({\"Text\": \"hello\"})[\"Text\"] }"); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	outcome, err := f.Invoke("call_tool", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != "ok:hello" {
		t.Fatalf("expected ok:hello, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_NativeValueImportExecutes(t *testing.T) {
	f := newVMFrontendWithNativeValues(t, frontend.MapModuleResolver{}, map[string]any{"answer": 42})
	if err := f.LoadSource("import answer from \"config\"\nfun read_answer(): int { return answer + 1 }"); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	outcome, err := f.Invoke("read_answer", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 43 {
		t.Fatalf("expected 43, got %+v", outcome.Payload)
	}
}

func newVMFrontendWithNativeValues(t *testing.T, resolver frontend.ModuleResolver, values map[string]any) *frontend.Frontend {
	t.Helper()
	sb := binding.NewScriptBinding()
	builder := binding.NewCapability("config", "module")
	for name, value := range values {
		if err := builder.AddValue(name, value); err != nil {
			t.Fatalf("AddValue %s: %v", name, err)
		}
	}
	cap, err := builder.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := sb.RegisterCapability(cap); err != nil {
		t.Fatalf("RegisterCapability: %v", err)
	}
	f, err := frontend.New(sb)
	if err != nil {
		t.Fatalf("frontend.New: %v", err)
	}
	vmEval := bytecode.NewVMEvaluator()
	vmEval.SetNativeBinding(sb)
	f.SetVMCompileHook(vmEval)
	f.SetModuleResolver(resolver)
	return f
}

func TestVMFrontend_NativeValueStringImportExecutes(t *testing.T) {
	f := newVMFrontendWithNativeValues(t, frontend.MapModuleResolver{}, map[string]any{"prefix": "spore"})
	if err := f.LoadSource(`import prefix from "config"
fun read_prefix(): string { return prefix + "-vm" }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	outcome, err := f.Invoke("read_prefix", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != "spore-vm" {
		t.Fatalf("expected spore-vm, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_NativeValueReExportExecutes(t *testing.T) {
	f := newVMFrontendWithNativeValues(t, frontend.MapModuleResolver{
		"api": `export answer from "config"`,
	}, map[string]any{"answer": 42})
	if err := f.LoadSource(`import answer from "api"
fun read_answer(): int { return answer + 1 }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	outcome, err := f.Invoke("read_answer", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 43 {
		t.Fatalf("expected 43, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_WildcardReExportNativeValueExecutes(t *testing.T) {
	f := newVMFrontendWithNativeValues(t, frontend.MapModuleResolver{
		"api": `export * from "config"`,
	}, map[string]any{"answer": 42, "offset": 8})
	if err := f.LoadSource(`import answer from "api"
import offset from "api"
fun read_total(): int { return answer + offset }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	outcome, err := f.Invoke("read_total", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 50 {
		t.Fatalf("expected 50, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_NativeValueAssignmentRejected(t *testing.T) {
	f := newVMFrontendWithNativeValues(t, frontend.MapModuleResolver{}, map[string]any{"answer": 42})
	err := f.LoadSource(`import answer from "config"
fun bad(): int { answer = 1 return answer }`)
	if err == nil {
		t.Fatal("expected imported native value assignment diagnostic")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "assign_imported_variable" {
		t.Fatalf("expected assign_imported_variable, got %v", err)
	}
}

func TestVMFrontend_NativeFreeFunctionImportExecutes(t *testing.T) {
	sb := binding.NewScriptBinding()
	builder := binding.NewCapability("math", "module")
	if err := builder.AddFreeFunction("abs", func(x float64) float64 {
		if x < 0 {
			return -x
		}
		return x
	}); err != nil {
		t.Fatalf("AddFreeFunction: %v", err)
	}
	cap, err := builder.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := sb.RegisterCapability(cap); err != nil {
		t.Fatalf("RegisterCapability: %v", err)
	}
	if err := sb.ExposeCapabilityCallables("math"); err != nil {
		t.Fatalf("ExposeCapabilityCallables: %v", err)
	}
	f, err := frontend.New(sb)
	if err != nil {
		t.Fatalf("frontend.New: %v", err)
	}
	vmEval := bytecode.NewVMEvaluator()
	vmEval.SetNativeBinding(sb)
	f.SetVMCompileHook(vmEval)
	f.SetModuleResolver(frontend.MapModuleResolver{})
	if err := f.LoadSource("import abs from \"math\"\nfun call_abs(): double { return abs(3.14) }"); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	outcome, err := f.Invoke("call_abs", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 3.14 {
		t.Fatalf("expected 3.14, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_FunctionCallFromScriptFunction(t *testing.T) {
	f := newVMFrontend(t)
	source := `
fun inc(x: int): int { return x + 1 }
fun calc(): int { return inc(4) }`
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("calc", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 5 {
		t.Fatalf("expected payload 5, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_NestedFunctionCallUsesReturnedValue(t *testing.T) {
	f := newVMFrontend(t)
	source := `
fun add(a: int, b: int): int { return a + b }
fun calc(): int { return add(add(1, 2), 3) }`
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("calc", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 6 {
		t.Fatalf("expected payload 6, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_RecursiveFactorialExecutes(t *testing.T) {
	f := newVMFrontend(t)
	if err := f.LoadSource("fun fact(n: int): int { if n <= 1 { return 1 } return n * fact(n - 1) }"); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("fact", []any{5})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 120 {
		t.Fatalf("expected payload 120, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_NestedControlFlowExecutes(t *testing.T) {
	f := newVMFrontend(t)
	source := `
fun sum_even(n: int): int {
  var total: int = 0
  while n > 0 {
    if n % 2 == 0 {
      total = total + n
    }
    n = n - 1
  }
  return total
}`
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("sum_even", []any{6})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 12 {
		t.Fatalf("expected payload 12, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_LenBuiltinExecutesForArrayAndMap(t *testing.T) {
	f := newVMFrontend(t)
	source := `
fun arrayLen(): int { return len([1, 2, 3]) }
fun mapLen(): int { return len({"hp": 10, "mp": 20}) }`
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	arrayOutcome, err := f.Invoke("arrayLen", nil)
	if err != nil {
		t.Fatalf("Invoke arrayLen: %v", err)
	}
	if arrayOutcome.Payload == nil || arrayOutcome.Payload.Value != 3 {
		t.Fatalf("expected array len 3, got %+v", arrayOutcome.Payload)
	}
	mapOutcome, err := f.Invoke("mapLen", nil)
	if err != nil {
		t.Fatalf("Invoke mapLen: %v", err)
	}
	if mapOutcome.Payload == nil || mapOutcome.Payload.Value != 2 {
		t.Fatalf("expected map len 2, got %+v", mapOutcome.Payload)
	}
}

func TestVMFrontend_PrivateAccessControlMatrix(t *testing.T) {
	t.Run("field_denied_outside_class", func(t *testing.T) {
		f := newVMFrontend(t)
		err := f.LoadSource(`class Foo { private name: string constructor(n: string) { this.name = n } fun greet(): string { return this.name } }
fun test(): string { var f: Foo = new Foo("hello") return f.name }`)
		if err == nil {
			t.Fatal("expected private field access diagnostic")
		}
		coder, ok := err.(interface{ DiagnosticCode() string })
		if !ok || coder.DiagnosticCode() != "private_field_access_denied" {
			t.Fatalf("expected private_field_access_denied, got %v", err)
		}
	})

	t.Run("method_denied_outside_class", func(t *testing.T) {
		f := newVMFrontend(t)
		err := f.LoadSource(`class Foo { private fun helper(): string { return "hidden" } fun visible(): string { return this.helper() } }
fun test(): string { var f: Foo = new Foo() return f.helper() }`)
		if err == nil {
			t.Fatal("expected private method access diagnostic")
		}
		coder, ok := err.(interface{ DiagnosticCode() string })
		if !ok || coder.DiagnosticCode() != "private_method_access_denied" {
			t.Fatalf("expected private_method_access_denied, got %v", err)
		}
	})

	t.Run("method_allowed_inside_class", func(t *testing.T) {
		f := newVMFrontend(t)
		if err := f.LoadSource(`class Foo { private fun helper(): string { return "hidden" } fun visible(): string { return this.helper() } }
fun test(): string { var f: Foo = new Foo() return f.visible() }`); err != nil {
			t.Fatalf("LoadSource: %v", err)
		}
		outcome, err := f.Invoke("test", nil)
		if err != nil {
			t.Fatalf("Invoke: %v", err)
		}
		if outcome.Payload == nil || outcome.Payload.Value != "hidden" {
			t.Fatalf("expected hidden, got %+v", outcome.Payload)
		}
	})

	t.Run("same_field_name_on_other_class_allowed", func(t *testing.T) {
		f := newVMFrontend(t)
		if err := f.LoadSource(`class Foo { private name: string constructor(n: string) { this.name = n } }
class Bar { name: string constructor(n: string) { this.name = n } }
fun test(): string { var b: Bar = new Bar("public") return b.name }`); err != nil {
			t.Fatalf("LoadSource: %v", err)
		}
		outcome, err := f.Invoke("test", nil)
		if err != nil {
			t.Fatalf("Invoke: %v", err)
		}
		if outcome.Payload == nil || outcome.Payload.Value != "public" {
			t.Fatalf("expected public, got %+v", outcome.Payload)
		}
	})

	t.Run("same_method_name_on_other_class_allowed", func(t *testing.T) {
		f := newVMFrontend(t)
		if err := f.LoadSource(`class Foo { private fun helper(): string { return "hidden" } }
class Bar { fun helper(): string { return "public" } }
fun test(): string { var b: Bar = new Bar() return b.helper() }`); err != nil {
			t.Fatalf("LoadSource: %v", err)
		}
		outcome, err := f.Invoke("test", nil)
		if err != nil {
			t.Fatalf("Invoke: %v", err)
		}
		if outcome.Payload == nil || outcome.Payload.Value != "public" {
			t.Fatalf("expected public, got %+v", outcome.Payload)
		}
	})
}

func TestVMFrontend_SuperConstructorInitializesParentFields(t *testing.T) {
	f := newVMFrontend(t)
	source := `open class Base { x: int constructor(v: int) { this.x = v } }
class Child : Base { y: int constructor(a: int, b: int) { super(a) this.y = b } fun sum(): int { return this.x + this.y } }
fun calc(): int { var c: Child = new Child(10, 20) return c.sum() }`
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	outcome, err := f.Invoke("calc", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 30 {
		t.Fatalf("expected payload 30, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_InterfaceIsCheckNegative(t *testing.T) {
	f := newVMFrontend(t)
	source := `interface Greeter { fun greet(): string }
class Dog { name: string fun bark(): string { return this.name } }
fun check(): bool { var d: Dog = new Dog() d.name = "woof" return d is Greeter }`
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	outcome, err := f.Invoke("check", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != false {
		t.Fatalf("expected false, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_RuntimeDiagnosticMatrix(t *testing.T) {
	cases := []struct {
		name     string
		source   string
		callable string
		code     string
		path     string
	}{
		{name: "division_by_zero", source: `fun broken(): int { return 1 / 0 }`, callable: "broken", code: "division_by_zero", path: "vm/arithmetic/div"},
		{name: "undefined_function", source: `fun broken(): int { return missing() }`, callable: "broken", code: "undefined_function", path: "vm/call/function"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newVMFrontend(t)
			if err := f.LoadSource(tc.source); err != nil {
				t.Fatalf("LoadSource: %v", err)
			}
			outcome, err := f.Invoke(tc.callable, nil)
			if err != nil {
				t.Fatalf("Invoke: %v", err)
			}
			if outcome.Result.Kind != binding.InvocationResultError {
				t.Fatalf("expected error result, got %s", outcome.Result.Kind)
			}
			if outcome.Result.Error == nil || outcome.Result.Error.DiagnosticCode != tc.code {
				t.Fatalf("expected %s diagnostic, got %+v", tc.code, outcome.Result.Error)
			}
		})
	}
}

func TestVMFrontend_NumericAndAliasFullScriptPaths(t *testing.T) {
	f := newVMFrontend(t)
	source := `
fun idLong(x: long): long { return x }
fun idDouble(x: double): double { return x }
fun calcULong(): ulong { var a: ulong = 100 var b: ulong = 50 return a - b }
type Id = long
type UserId = Id
fun aliasValue(): UserId { return 100 }`
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	longOutcome, err := f.Invoke("idLong", []any{int64(1 << 40)})
	if err != nil {
		t.Fatalf("Invoke idLong: %v", err)
	}
	if longOutcome.Payload == nil || longOutcome.Payload.Value != int64(1<<40) {
		t.Fatalf("expected int64 payload, got %+v", longOutcome.Payload)
	}
	doubleOutcome, err := f.Invoke("idDouble", []any{123.5})
	if err != nil {
		t.Fatalf("Invoke idDouble: %v", err)
	}
	if doubleOutcome.Payload == nil || doubleOutcome.Payload.Value != 123.5 {
		t.Fatalf("expected double payload 123.5, got %+v", doubleOutcome.Payload)
	}
	ulongOutcome, err := f.Invoke("calcULong", nil)
	if err != nil {
		t.Fatalf("Invoke calcULong: %v", err)
	}
	if ulongOutcome.Payload == nil || ulongOutcome.Payload.Value != uint64(50) {
		t.Fatalf("expected ulong payload 50, got %+v", ulongOutcome.Payload)
	}
	aliasOutcome, err := f.Invoke("aliasValue", nil)
	if err != nil {
		t.Fatalf("Invoke aliasValue: %v", err)
	}
	if aliasOutcome.Payload == nil || aliasOutcome.Payload.Value != int64(100) {
		t.Fatalf("expected alias payload 100, got %+v", aliasOutcome.Payload)
	}
}

func TestVMFrontend_UnsupportedHostArgumentDiagnostic(t *testing.T) {
	f := newVMFrontend(t)
	if err := f.LoadSource(`fun id(x: int): int { return x }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	_, err := f.Invoke("id", []any{struct{}{}})
	if err == nil {
		t.Fatal("expected unsupported host argument error")
	}
	if !strings.Contains(err.Error(), `payload type "struct {}" does not match result schema "int"`) {
		t.Fatalf("expected schema payload mismatch message, got %v", err)
	}
}

func TestVMFrontend_ScenarioRuleEngineWorkflowMatrix(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   int
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
			f := newVMFrontend(t)
			if err := f.LoadSource(tc.source); err != nil {
				t.Fatalf("LoadSource: %v", err)
			}
			outcome, err := f.Invoke("classify", nil)
			if err != nil {
				t.Fatalf("Invoke: %v", err)
			}
			if outcome.Result.Kind != binding.InvocationResultValue {
				t.Fatalf("expected value result, got %s", outcome.Result.Kind)
			}
			if outcome.Payload == nil || outcome.Payload.Value != tc.want {
				t.Fatalf("expected payload %d, got %+v", tc.want, outcome.Payload)
			}
		})
	}
}

func TestVMFrontend_ScenarioRuleEngineWorkflow(t *testing.T) {
	f := newVMFrontend(t)
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
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("evaluate", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 2 {
		t.Fatalf("expected payload 2, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_ScenarioInterfaceOverrideClassification(t *testing.T) {
	f := newVMFrontend(t)
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
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("classify", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 11 {
		t.Fatalf("expected payload 11, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_ScenarioNestedCollectionTransform(t *testing.T) {
	f := newVMFrontend(t)
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
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("transform", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 84 {
		t.Fatalf("expected payload 84, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_ScenarioNestedStateUpdateWorkflow(t *testing.T) {
	f := newVMFrontend(t)
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
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("advance", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 15 {
		t.Fatalf("expected payload 15, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_ScenarioGlobalsLoopAccumulation(t *testing.T) {
	f := newVMFrontend(t)
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
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("accumulate", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 22 {
		t.Fatalf("expected payload 22, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_ScenarioInventoryTotalAggregation(t *testing.T) {
	f := newVMFrontend(t)
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
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("total", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 31 {
		t.Fatalf("expected payload 31, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_ScenarioMapBackedStatusClassification(t *testing.T) {
	f := newVMFrontend(t)
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
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("classify", []any{5, 3})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 2 {
		t.Fatalf("expected payload 2, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_ScenarioPolymorphicDispatchWorkflow(t *testing.T) {
	f := newVMFrontend(t)
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
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("apply", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 30 {
		t.Fatalf("expected payload 30, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_StructLiteralFieldReadReturnsExpectedValue(t *testing.T) {
	f := newVMFrontend(t)
	source := `
struct Point {
  x: int
  y: int
}
fun read(): int = Point{x: 1, y: 2}.x`
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("read", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 1 {
		t.Fatalf("expected payload 1, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_StructVariableFieldReadReturnsExpectedValue(t *testing.T) {
	f := newVMFrontend(t)
	source := `
struct Point {
  x: int
  y: int
}
fun read(): int {
  var p: Point = Point{x: 7, y: 9}
  return p.x
}`
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("read", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 7 {
		t.Fatalf("expected payload 7, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_NestedStructFieldReadReturnsExpectedValue(t *testing.T) {
	f := newVMFrontend(t)
	source := `
struct Point {
  x: int
  y: int
}
struct Box {
  point: Point
}
fun read(): int = Box{point: Point{x: 5, y: 6}}.point.x`
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("read", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 5 {
		t.Fatalf("expected payload 5, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_ArrayIndexReturnsExpectedValue(t *testing.T) {
	f := newVMFrontend(t)
	if err := f.LoadSource("fun first(): int = [10, 20, 30][0]"); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("first", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 10 {
		t.Fatalf("expected payload 10, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_ArrayIndexUsesLatestElement(t *testing.T) {
	f := newVMFrontend(t)
	if err := f.LoadSource("fun second(): int = [10, 20, 30][1]"); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("second", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 20 {
		t.Fatalf("expected payload 20, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_MapIndexReturnsExpectedValue(t *testing.T) {
	f := newVMFrontend(t)
	if err := f.LoadSource(`fun hp(): int = {"hp": 10, "mp": 20}["hp"]`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("hp", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 10 {
		t.Fatalf("expected payload 10, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_MapIndexSelectsCorrectKey(t *testing.T) {
	f := newVMFrontend(t)
	if err := f.LoadSource(`fun mp(): int = {"hp": 10, "mp": 20}["mp"]`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("mp", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 20 {
		t.Fatalf("expected payload 20, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_PrivateFieldAccessAllowedInsideClass(t *testing.T) {
	f := newVMFrontend(t)
	source := `
class Foo {
  private name: string
  constructor(n: string) { this.name = n }
  fun greet(): string { return this.name }
}
fun read(): string {
  var f: Foo = new Foo("hello")
  return f.greet()
}`
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("read", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != "hello" {
		t.Fatalf("expected payload hello, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_InterfaceIsCheck(t *testing.T) {
	f := newVMFrontend(t)
	source := `
interface Greeter { fun greet(): string }
class Person : Greeter { name: string fun greet(): string { return this.name } }
fun check(): bool {
  var p: Person = new Person()
  p.name = "hi"
  return p is Greeter
}`
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("check", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != true {
		t.Fatalf("expected payload true, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_InterfaceDefaultMethodInherited(t *testing.T) {
	f := newVMFrontend(t)
	source := `
interface Greeter { fun greet(): string { return "default hello" } }
class Person : Greeter {}
fun read(): string {
  var p: Person = new Person()
  return p.greet()
}`
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	outcome, err := f.Invoke("read", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != "default hello" {
		t.Fatalf("expected inherited default method result, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_InterfaceDefaultMethodOverridden(t *testing.T) {
	f := newVMFrontend(t)
	source := `
interface Greeter { fun greet(): string { return "default hello" } }
class Person : Greeter { fun greet(): string { return "custom hello" } }
fun read(): string {
  var p: Person = new Person()
  return p.greet()
}`
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	outcome, err := f.Invoke("read", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != "custom hello" {
		t.Fatalf("expected overridden method result, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_InterfaceDefaultMethodDispatchesThisMethods(t *testing.T) {
	f := newVMFrontend(t)
	source := `
interface Greeter {
  fun label(): string { return "hi, " + this.name() }
  fun name(): string
}
class Person : Greeter { fun name(): string { return "alice" } }
fun read(): string {
  var p: Person = new Person()
  return p.label()
}`
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	outcome, err := f.Invoke("read", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != "hi, alice" {
		t.Fatalf("expected default method to dispatch this.name(), got %+v", outcome.Payload)
	}
}

func TestVMFrontend_InterfaceDefaultOverrideAcrossInheritance(t *testing.T) {
	f := newVMFrontend(t)
	source := `
interface Greeter { fun greet(): string { return "default" } }
open class Base : Greeter {}
class Loud : Base { override fun greet(): string { return "loud" } }
fun read(): string {
  var b: Base = new Base()
  var l: Loud = new Loud()
  return b.greet() + "|" + l.greet()
}`
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	outcome, err := f.Invoke("read", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != "default|loud" {
		t.Fatalf("expected inherited default plus override, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_InterfaceDefaultMethodWithParameters(t *testing.T) {
	f := newVMFrontend(t)
	source := `
interface Formatter { fun wrap(text: string, times: int): string { var out: string = "[" + text + "]" var i: int = 1 while i < times { out = out + out i = i + 1 } return out } }
class Box : Formatter {}
fun read(): string {
  var b: Box = new Box()
  return b.wrap("hi", 3)
}`
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	outcome, err := f.Invoke("read", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != "[hi][hi][hi][hi]" {
		t.Fatalf("expected default method with parameters result, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_InterfaceDefaultFromOneInterfaceSatisfiesAnother(t *testing.T) {
	f := newVMFrontend(t)
	source := `
interface A { fun f(): string { return "from-a" } }
interface B { fun f(): string }
class C : A, B {}
fun read(): string {
  var c: C = new C()
  return c.f()
}`
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	outcome, err := f.Invoke("read", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != "from-a" {
		t.Fatalf("expected default from interface A to satisfy interface B, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_SuperMethodDispatch(t *testing.T) {
	f := newVMFrontend(t)
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
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("read", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != "animal" {
		t.Fatalf("expected payload animal, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_OpenOverrideDispatch(t *testing.T) {
	f := newVMFrontend(t)
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
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("read", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != "dog" {
		t.Fatalf("expected payload dog, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_ClassConstructorAssignsField(t *testing.T) {
	f := newVMFrontend(t)
	source := `
class Dog {
  name: string
  constructor(n: string) { this.name = n }
}
fun read(): string {
  var d: Dog = new Dog("Rex")
  return d.name
}`
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("read", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != "Rex" {
		t.Fatalf("expected payload Rex, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_TypeAliasLongReturnPath(t *testing.T) {
	f := newVMFrontend(t)
	source := `
type UserId = long
fun value(): UserId = 42`
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("value", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil {
		t.Fatal("expected non-nil payload")
	}
	value, ok := outcome.Payload.Value.(int64)
	if !ok || value != 42 {
		t.Fatalf("expected int64 payload 42, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_StructFieldAssignmentUpdatesValue(t *testing.T) {
	f := newVMFrontend(t)
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
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("set", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 9 {
		t.Fatalf("expected payload 9, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_ArrayElementAssignmentUpdatesValue(t *testing.T) {
	f := newVMFrontend(t)
	source := `
fun set(): int {
  var xs: array<int> = [1, 2, 3]
  xs[1] = 9
  return xs[1]
}`
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("set", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 9 {
		t.Fatalf("expected payload 9, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_MapKeyAssignmentUpdatesValue(t *testing.T) {
	f := newVMFrontend(t)
	source := `
fun set(): int {
  var attrs: map<string, int> = {"hp": 10}
  attrs["hp"] = 20
  return attrs["hp"]
}`
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("set", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 20 {
		t.Fatalf("expected payload 20, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_WhenCaseMatchesExpectedBranch(t *testing.T) {
	f := newVMFrontend(t)
	source := `
fun label(x: int): int {
  when (x) {
    case 1 { return 10 }
    case 2 { return 20 }
    else { return 30 }
  }
}`
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

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
			outcome, err := f.Invoke("label", []any{tc.arg})
			if err != nil {
				t.Fatalf("Invoke: %v", err)
			}
			if outcome.Result.Kind != binding.InvocationResultValue {
				t.Fatalf("expected value result, got %s", outcome.Result.Kind)
			}
			if outcome.Payload == nil || outcome.Payload.Value != tc.want {
				t.Fatalf("expected payload %d, got %+v", tc.want, outcome.Payload)
			}
		})
	}
}

func TestVMFrontend_WhenCaseWithMultipleValuesMatchesExpectedBranch(t *testing.T) {
	f := newVMFrontend(t)
	source := `
fun label(x: int): int {
  when (x) {
    case 1, 2 { return 10 }
    else { return 30 }
  }
}`
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

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
			outcome, err := f.Invoke("label", []any{tc.arg})
			if err != nil {
				t.Fatalf("Invoke: %v", err)
			}
			if outcome.Payload == nil || outcome.Payload.Value != tc.want {
				t.Fatalf("expected payload %d, got %+v", tc.want, outcome.Payload)
			}
		})
	}
}

func TestVMFrontend_ForLoopAccumulatesValue(t *testing.T) {
	f := newVMFrontend(t)
	source := `
fun sum(): int {
  var total: int = 0
  for (var i: int = 0; i < 5; i = i + 1) {
    total = total + i
  }
  return total
}`
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("sum", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 10 {
		t.Fatalf("expected payload 10, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_ForLoopConditionFalseSkipsBody(t *testing.T) {
	f := newVMFrontend(t)
	source := `
fun calc(): int {
  var total: int = 7
  for (var i: int = 5; i < 5; i = i + 1) {
    total = total + i
  }
  return total
}`
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("calc", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 7 {
		t.Fatalf("expected payload 7, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_ForLoopUpdateRunsAfterBody(t *testing.T) {
	f := newVMFrontend(t)
	source := `
fun calc(): int {
  var total: int = 0
  for (var i: int = 1; i < 8; i = i * 2) {
    total = total + i
  }
  return total
}`
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("calc", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 7 {
		t.Fatalf("expected payload 7, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_ForInArrayAccumulatesValue(t *testing.T) {
	f := newVMFrontend(t)
	source := `
fun sum(): int {
  var total: int = 0
  for (item in [1, 2, 3, 4]) {
    total = total + item
  }
  return total
}`
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("sum", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 10 {
		t.Fatalf("expected payload 10, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_BreakExitsWhileLoop(t *testing.T) {
	f := newVMFrontend(t)
	source := `
fun calc(): int {
  var i: int = 0
  while i < 10 {
    if i == 3 { break }
    i = i + 1
  }
  return i
}`
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("calc", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 3 {
		t.Fatalf("expected payload 3, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_ContinueSkipsRemainingLoopBody(t *testing.T) {
	f := newVMFrontend(t)
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
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("calc", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 12 {
		t.Fatalf("expected payload 12, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_BreakSkipsRemainingLoopBody(t *testing.T) {
	f := newVMFrontend(t)
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
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("calc", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 3 {
		t.Fatalf("expected payload 3, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_WhileLoopAccumulatesValue(t *testing.T) {
	f := newVMFrontend(t)
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
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("sum", []any{5})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 10 {
		t.Fatalf("expected payload 10, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_WhileConditionFalseSkipsBody(t *testing.T) {
	f := newVMFrontend(t)
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
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("sum", []any{0})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 7 {
		t.Fatalf("expected payload 7, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_WhileLoopUsesUpdatedVariables(t *testing.T) {
	f := newVMFrontend(t)
	source := `
fun calc(): int {
  var x: int = 1
  while x < 8 {
    x = x * 2
  }
  return x
}`
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("calc", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 8 {
		t.Fatalf("expected payload 8, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_IfTrueBranchReturnsExpectedValue(t *testing.T) {
	f := newVMFrontend(t)
	if err := f.LoadSource("fun check(flag: bool): int { if flag { return 1 } return 2 }"); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("check", []any{true})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 1 {
		t.Fatalf("expected payload 1, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_IfFalseFallsThroughToTrailingReturn(t *testing.T) {
	f := newVMFrontend(t)
	if err := f.LoadSource("fun check(flag: bool): int { if flag { return 1 } return 2 }"); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("check", []any{false})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 2 {
		t.Fatalf("expected payload 2, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_IfElseChoosesCorrectBranch(t *testing.T) {
	f := newVMFrontend(t)
	if err := f.LoadSource("fun check(flag: bool): int { if flag { return 1 } else { return 2 } }"); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	trueOutcome, err := f.Invoke("check", []any{true})
	if err != nil {
		t.Fatalf("Invoke true: %v", err)
	}
	if trueOutcome.Payload == nil || trueOutcome.Payload.Value != 1 {
		t.Fatalf("expected true payload 1, got %+v", trueOutcome.Payload)
	}

	falseOutcome, err := f.Invoke("check", []any{false})
	if err != nil {
		t.Fatalf("Invoke false: %v", err)
	}
	if falseOutcome.Payload == nil || falseOutcome.Payload.Value != 2 {
		t.Fatalf("expected false payload 2, got %+v", falseOutcome.Payload)
	}
}

func TestVMFrontend_NestedIfElseChoosesInnerBranch(t *testing.T) {
	f := newVMFrontend(t)
	source := `
fun check(a: bool, b: bool): int {
  if a {
    if b { return 1 } else { return 2 }
  }
  return 3
}`
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	cases := []struct {
		name string
		args []any
		want int
	}{
		{name: "a_true_b_true", args: []any{true, true}, want: 1},
		{name: "a_true_b_false", args: []any{true, false}, want: 2},
		{name: "a_false_b_true", args: []any{false, true}, want: 3},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			outcome, err := f.Invoke("check", tc.args)
			if err != nil {
				t.Fatalf("Invoke: %v", err)
			}
			if outcome.Result.Kind != binding.InvocationResultValue {
				t.Fatalf("expected value result, got %s", outcome.Result.Kind)
			}
			if outcome.Payload == nil || outcome.Payload.Value != tc.want {
				t.Fatalf("expected payload %d, got %+v", tc.want, outcome.Payload)
			}
		})
	}
}

func TestVMFrontend_StreamSingleYieldThenFinal(t *testing.T) {
	f := newVMFrontend(t)
	if err := f.LoadSource("stream fun emit(): int { yield 1 return 2 }"); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	next, err := f.InvokeStage("emit", binding.InvocationStageNext, nil)
	if err != nil {
		t.Fatalf("InvokeStage next: %v", err)
	}
	if next.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected next value result, got %s", next.Result.Kind)
	}
	if next.Payload == nil || next.Payload.Value != 1 {
		t.Fatalf("expected next payload 1, got %+v", next.Payload)
	}

	final, err := f.InvokeStage("emit", binding.InvocationStageFinal, nil)
	if err != nil {
		t.Fatalf("InvokeStage final: %v", err)
	}
	if final.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected final value result, got %s", final.Result.Kind)
	}
	if final.Payload == nil || final.Payload.Value != 2 {
		t.Fatalf("expected final payload 2, got %+v", final.Payload)
	}
}

func TestVMFrontend_StreamMultipleYieldsThenFinal(t *testing.T) {
	f := newVMFrontend(t)
	if err := f.LoadSource("stream fun emit(): int { yield 1 yield 2 return 3 }"); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	first, err := f.InvokeStage("emit", binding.InvocationStageNext, nil)
	if err != nil {
		t.Fatalf("first next: %v", err)
	}
	if first.Payload == nil || first.Payload.Value != 1 {
		t.Fatalf("expected first payload 1, got %+v", first.Payload)
	}

	second, err := f.InvokeStage("emit", binding.InvocationStageNext, nil)
	if err != nil {
		t.Fatalf("second next: %v", err)
	}
	if second.Payload == nil || second.Payload.Value != 2 {
		t.Fatalf("expected second payload 2, got %+v", second.Payload)
	}

	final, err := f.InvokeStage("emit", binding.InvocationStageFinal, nil)
	if err != nil {
		t.Fatalf("final: %v", err)
	}
	if final.Payload == nil || final.Payload.Value != 3 {
		t.Fatalf("expected final payload 3, got %+v", final.Payload)
	}
}

func TestVMFrontend_StreamCallableRejectsUnaryStage(t *testing.T) {
	f := newVMFrontend(t)
	if err := f.LoadSource("stream fun emit(): int { yield 1 return 2 }"); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	_, err := f.InvokeStage("emit", binding.InvocationStageUnary, nil)
	if err == nil {
		t.Fatal("expected stream callable to reject unary stage")
	}

	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "invalid_invocation_stage" {
		t.Fatalf("expected invalid_invocation_stage code, got %v", err)
	}
}

func TestVMFrontend_BlockVarInitializationAndRead(t *testing.T) {
	f := newVMFrontend(t)
	if err := f.LoadSource("fun calc(): int { var count: int = 7 return count }"); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("calc", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 7 {
		t.Fatalf("expected payload 7, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_BlockVarAssignmentUpdatesValue(t *testing.T) {
	f := newVMFrontend(t)
	if err := f.LoadSource("fun calc(): int { var count: int = 1 count = 2 return count }"); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("calc", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 2 {
		t.Fatalf("expected payload 2, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_GlobalAssignmentThenRead(t *testing.T) {
	f := newVMFrontend(t)
	source := `
var count: int = 1
count = 9
fun read(): int { return count }
`
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("read", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 9 {
		t.Fatalf("expected payload 9, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_MultipleVarsPreserveAssignmentOrder(t *testing.T) {
	f := newVMFrontend(t)
	source := `
fun calc(): int {
  var a: int = 1
  var b: int = 2
  a = a + b
  b = a + b
  return b
}`
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("calc", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 5 {
		t.Fatalf("expected payload 5, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_StreamNextAfterExhaustion(t *testing.T) {
	f := newVMFrontend(t)
	if err := f.LoadSource("stream fun emit(): int { yield 1 return 2 }"); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	_, err := f.InvokeStage("emit", binding.InvocationStageNext, nil)
	if err != nil {
		t.Fatalf("first next: %v", err)
	}
	_, err = f.InvokeStage("emit", binding.InvocationStageFinal, nil)
	if err != nil {
		t.Fatalf("final: %v", err)
	}
	outcome, err := f.InvokeStage("emit", binding.InvocationStageNext, nil)
	if err != nil {
		t.Fatalf("next after final: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultError {
		t.Fatalf("expected error result after exhaustion, got %s", outcome.Result.Kind)
	}
	if outcome.Result.Error == nil || outcome.Result.Error.DiagnosticCode != "stream_exhausted" {
		t.Fatalf("expected stream_exhausted envelope, got %+v", outcome.Result.Error)
	}
}

func TestVMFrontend_StreamRepeatedFinal(t *testing.T) {
	f := newVMFrontend(t)
	if err := f.LoadSource("stream fun emit(): int { yield 1 return 2 }"); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	_, err := f.InvokeStage("emit", binding.InvocationStageNext, nil)
	if err != nil {
		t.Fatalf("first next: %v", err)
	}
	firstFinal, err := f.InvokeStage("emit", binding.InvocationStageFinal, nil)
	if err != nil {
		t.Fatalf("first final: %v", err)
	}
	if firstFinal.Payload == nil || firstFinal.Payload.Value != 2 {
		t.Fatalf("expected first final payload 2, got %+v", firstFinal.Payload)
	}
	secondFinal, err := f.InvokeStage("emit", binding.InvocationStageFinal, nil)
	if err != nil {
		t.Fatalf("second final: %v", err)
	}
	if secondFinal.Result.Kind != binding.InvocationResultError {
		t.Fatalf("expected error result for repeated final, got %s", secondFinal.Result.Kind)
	}
	if secondFinal.Result.Error == nil || secondFinal.Result.Error.DiagnosticCode != "stream_exhausted" {
		t.Fatalf("expected stream_exhausted envelope, got %+v", secondFinal.Result.Error)
	}
}

func TestVMFrontend_StreamSessionsDoNotLeakAcrossArgs(t *testing.T) {
	f := newVMFrontend(t)
	source := `
stream fun emit(flag: bool): int {
  if flag { yield 1 } else { yield 2 }
  return 3
}`
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	first, err := f.InvokeStage("emit", binding.InvocationStageNext, []any{true})
	if err != nil {
		t.Fatalf("true next: %v", err)
	}
	if first.Payload == nil || first.Payload.Value != 1 {
		t.Fatalf("expected true path payload 1, got %+v", first.Payload)
	}

	second, err := f.InvokeStage("emit", binding.InvocationStageNext, []any{false})
	if err != nil {
		t.Fatalf("false next: %v", err)
	}
	if second.Payload == nil || second.Payload.Value != 2 {
		t.Fatalf("expected false path payload 2, got %+v", second.Payload)
	}
}

func TestVMFrontend_StreamMultipleCallablesDoNotLeakSessions(t *testing.T) {
	f := newVMFrontend(t)
	source := `
stream fun emitA(): int { yield 1 return 2 }
stream fun emitB(): int { yield 10 return 20 }
`
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	aNext, err := f.InvokeStage("emitA", binding.InvocationStageNext, nil)
	if err != nil {
		t.Fatalf("emitA next: %v", err)
	}
	if aNext.Payload == nil || aNext.Payload.Value != 1 {
		t.Fatalf("expected emitA next payload 1, got %+v", aNext.Payload)
	}

	bNext, err := f.InvokeStage("emitB", binding.InvocationStageNext, nil)
	if err != nil {
		t.Fatalf("emitB next: %v", err)
	}
	if bNext.Payload == nil || bNext.Payload.Value != 10 {
		t.Fatalf("expected emitB next payload 10, got %+v", bNext.Payload)
	}

	aFinal, err := f.InvokeStage("emitA", binding.InvocationStageFinal, nil)
	if err != nil {
		t.Fatalf("emitA final: %v", err)
	}
	if aFinal.Payload == nil || aFinal.Payload.Value != 2 {
		t.Fatalf("expected emitA final payload 2, got %+v", aFinal.Payload)
	}

	bFinal, err := f.InvokeStage("emitB", binding.InvocationStageFinal, nil)
	if err != nil {
		t.Fatalf("emitB final: %v", err)
	}
	if bFinal.Payload == nil || bFinal.Payload.Value != 20 {
		t.Fatalf("expected emitB final payload 20, got %+v", bFinal.Payload)
	}
}

func TestVMFrontend_StreamLocalsPersistAcrossYields(t *testing.T) {
	f := newVMFrontend(t)
	source := `
stream fun emit(): int {
  var x: int = 1
  yield x
  x = x + 1
  yield x
  return x + 1
}`
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	first, err := f.InvokeStage("emit", binding.InvocationStageNext, nil)
	if err != nil {
		t.Fatalf("first next: %v", err)
	}
	if first.Payload == nil || first.Payload.Value != 1 {
		t.Fatalf("expected first payload 1, got %+v", first.Payload)
	}

	second, err := f.InvokeStage("emit", binding.InvocationStageNext, nil)
	if err != nil {
		t.Fatalf("second next: %v", err)
	}
	if second.Payload == nil || second.Payload.Value != 2 {
		t.Fatalf("expected second payload 2, got %+v", second.Payload)
	}

	final, err := f.InvokeStage("emit", binding.InvocationStageFinal, nil)
	if err != nil {
		t.Fatalf("final: %v", err)
	}
	if final.Payload == nil || final.Payload.Value != 3 {
		t.Fatalf("expected final payload 3, got %+v", final.Payload)
	}
}

func TestVMFrontend_StructLiteralFieldTypeMismatchDiagnosticShape(t *testing.T) {
	f := newVMFrontend(t)
	err := f.LoadSource(`
struct Point {
  x: int
  y: int
}
fun read(): Point {
  return Point{x: "oops", y: 2}
}`)
	if err == nil {
		t.Fatal("expected struct field type mismatch compile/load error")
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

func TestVMFrontend_StructLiteralDuplicateFieldDiagnosticShape(t *testing.T) {
	f := newVMFrontend(t)
	err := f.LoadSource(`
struct Point {
  x: int
  y: int
}
fun read(): Point {
  return Point{x: 1, x: 2, y: 3}
}`)
	if err == nil {
		t.Fatal("expected duplicate struct field compile/load error")
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

func TestVMFrontend_ArrayOutOfRangeReturnsStructuredError(t *testing.T) {
	f := newVMFrontend(t)
	if err := f.LoadSource("fun read(): int = [10, 20][99]"); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("read", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultError {
		t.Fatalf("expected error result, got %s", outcome.Result.Kind)
	}
	if outcome.Result.Error == nil || outcome.Result.Error.DiagnosticCode != "array_index_out_of_range" {
		t.Fatalf("expected array_index_out_of_range envelope, got %+v", outcome.Result.Error)
	}
}

func TestVMFrontend_ArrayInvalidIndexTypeReturnsStructuredError(t *testing.T) {
	f := newVMFrontend(t)
	if err := f.LoadSource("fun read(): int = [10, 20][true]"); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("read", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultError {
		t.Fatalf("expected error result, got %s", outcome.Result.Kind)
	}
	if outcome.Result.Error == nil || outcome.Result.Error.DiagnosticCode != "invalid_array_index_type" {
		t.Fatalf("expected invalid_array_index_type envelope, got %+v", outcome.Result.Error)
	}
}

func TestVMFrontend_MapMissingKeyReturnsStructuredError(t *testing.T) {
	f := newVMFrontend(t)
	if err := f.LoadSource(`fun read(): int = {"x": 1}["missing"]`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("read", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultError {
		t.Fatalf("expected error result, got %s", outcome.Result.Kind)
	}
	if outcome.Result.Error == nil || outcome.Result.Error.DiagnosticCode != "map_key_not_found" {
		t.Fatalf("expected map_key_not_found envelope, got %+v", outcome.Result.Error)
	}
}

func TestVMFrontend_MapInvalidKeyTypeReturnsStructuredError(t *testing.T) {
	f := newVMFrontend(t)
	if err := f.LoadSource(`fun read(): int = {"x": 1}[true]`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("read", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultError {
		t.Fatalf("expected error result, got %s", outcome.Result.Kind)
	}
	if outcome.Result.Error == nil || outcome.Result.Error.DiagnosticCode != "invalid_map_key_type" {
		t.Fatalf("expected invalid_map_key_type envelope, got %+v", outcome.Result.Error)
	}
}

func TestVMFrontend_MissingStructFieldDiagnosticShape(t *testing.T) {
	f := newVMFrontend(t)
	err := f.LoadSource(`
struct Point {
  x: int
  y: int
}
fun read(): int {
  var p: Point = Point{x: 1}
  return p.y
}`)
	if err == nil {
		t.Fatal("expected missing struct field compile/load error")
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

func TestVMFrontend_UnknownStructFieldDiagnosticShape(t *testing.T) {
	f := newVMFrontend(t)
	err := f.LoadSource(`
struct Point {
  x: int
  y: int
}
fun read(): int {
  var p: Point = Point{x: 1, z: 2}
  return p.x
}`)
	if err == nil {
		t.Fatal("expected unknown struct field compile/load error")
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

func TestVMFrontend_UndefinedVariableReturnsCompileError(t *testing.T) {
	f := newVMFrontend(t)
	err := f.LoadSource("fun bad(): string { return missing }")
	if err == nil {
		t.Fatal("expected compile/load error for undefined variable")
	}
	if err.Error() == "" {
		t.Fatal("expected non-empty error message")
	}
	if !strings.Contains(err.Error(), "undefined variable") {
		t.Fatalf("expected undefined variable error, got %v", err)
	}
}

func TestVMFrontend_InvokesUnaryExpressionBodyCallable(t *testing.T) {
	f := newVMFrontend(t)
	if err := f.LoadSource(`fun greet(name: string): string = name`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("greet", []any{"alice"})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != "alice" {
		t.Fatalf("expected payload alice, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_InvokesUnaryBlockBodyCallable(t *testing.T) {
	f := newVMFrontend(t)
	if err := f.LoadSource(`fun wrap(x: int): int { return x }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("wrap", []any{42})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 42 {
		t.Fatalf("expected payload 42, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_InvokesBinaryExpressionBodyCallable(t *testing.T) {
	f := newVMFrontend(t)
	if err := f.LoadSource(`fun add(a: int, b: int): int = a + b`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("add", []any{7, 5})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 12 {
		t.Fatalf("expected payload 12, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_InvokesExportedFunLikeNormalCallable(t *testing.T) {
	f := newVMFrontend(t)
	if err := f.LoadSource(`export fun id(x: int): int = x`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("id", []any{9})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 9 {
		t.Fatalf("expected payload 9, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_ExpressionPrecedence_MulBeforeAdd(t *testing.T) {
	f := newVMFrontend(t)
	if err := f.LoadSource("fun calc(): int = 1 + 2 * 3"); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("calc", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 7 {
		t.Fatalf("expected payload 7, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_ExpressionPrecedence_ParenOverridesDefault(t *testing.T) {
	f := newVMFrontend(t)
	if err := f.LoadSource("fun calc(): int = (1 + 2) * 3"); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("calc", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 9 {
		t.Fatalf("expected payload 9, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_LogicalPrecedence_AndBeforeOr(t *testing.T) {
	f := newVMFrontend(t)
	if err := f.LoadSource("fun calc(a: bool, b: bool, c: bool): bool = a && b || c"); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	cases := []struct {
		name string
		args []any
		want bool
	}{
		{name: "false_true_false", args: []any{false, true, false}, want: false},
		{name: "true_true_false", args: []any{true, true, false}, want: true},
		{name: "false_false_true", args: []any{false, false, true}, want: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			outcome, err := f.Invoke("calc", tc.args)
			if err != nil {
				t.Fatalf("Invoke: %v", err)
			}
			if outcome.Result.Kind != binding.InvocationResultValue {
				t.Fatalf("expected value result, got %s", outcome.Result.Kind)
			}
			if outcome.Payload == nil || outcome.Payload.Value != tc.want {
				t.Fatalf("expected payload %v, got %+v", tc.want, outcome.Payload)
			}
		})
	}
}

func TestVMFrontend_UnaryNegationBeforeCompare(t *testing.T) {
	f := newVMFrontend(t)
	if err := f.LoadSource("fun calc(a: bool, b: bool): bool = !a == b"); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	cases := []struct {
		name string
		args []any
		want bool
	}{
		{name: "true_false", args: []any{true, false}, want: true},
		{name: "false_false", args: []any{false, false}, want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			outcome, err := f.Invoke("calc", tc.args)
			if err != nil {
				t.Fatalf("Invoke: %v", err)
			}
			if outcome.Result.Kind != binding.InvocationResultValue {
				t.Fatalf("expected value result, got %s", outcome.Result.Kind)
			}
			if outcome.Payload == nil || outcome.Payload.Value != tc.want {
				t.Fatalf("expected payload %v, got %+v", tc.want, outcome.Payload)
			}
		})
	}
}

func TestVMFrontend_EvaluatesStreamingYieldAndReturn(t *testing.T) {
	f := newVMFrontend(t)
	if err := f.LoadSource("stream fun emit(): int { yield 1 return 2 }"); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	next, err := f.InvokeStage("emit", binding.InvocationStageNext, nil)
	if err != nil {
		t.Fatalf("InvokeStage next: %v", err)
	}
	if next.Result.Kind != binding.InvocationResultValue || next.Payload == nil || next.Payload.Value != 1 {
		t.Fatalf("expected next payload 1, got %+v", next)
	}
	final, err := f.InvokeStage("emit", binding.InvocationStageFinal, nil)
	if err != nil {
		t.Fatalf("InvokeStage final: %v", err)
	}
	if final.Result.Kind != binding.InvocationResultValue || final.Payload == nil || final.Payload.Value != 2 {
		t.Fatalf("expected final payload 2, got %+v", final)
	}
}

func TestVMFrontend_StreamingMultipleYieldsResume(t *testing.T) {
	f := newVMFrontend(t)
	if err := f.LoadSource("stream fun emit(): int { yield 1 yield 2 return 3 }"); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	first, err := f.InvokeStage("emit", binding.InvocationStageNext, nil)
	if err != nil {
		t.Fatalf("first next: %v", err)
	}
	if first.Payload == nil || first.Payload.Value != 1 {
		t.Fatalf("expected first next payload 1, got %+v", first.Payload)
	}
	second, err := f.InvokeStage("emit", binding.InvocationStageNext, nil)
	if err != nil {
		t.Fatalf("second next: %v", err)
	}
	if second.Payload == nil || second.Payload.Value != 2 {
		t.Fatalf("expected second next payload 2, got %+v", second.Payload)
	}
	final, err := f.InvokeStage("emit", binding.InvocationStageFinal, nil)
	if err != nil {
		t.Fatalf("final: %v", err)
	}
	if final.Payload == nil || final.Payload.Value != 3 {
		t.Fatalf("expected final payload 3, got %+v", final.Payload)
	}
}

func TestVMFrontend_EvaluatesReturnBody(t *testing.T) {
	f := newVMFrontend(t)
	if err := f.LoadSource("fun pick(value: string): string = value"); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	outcome, err := f.Invoke("pick", []any{"ok"})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != "ok" {
		t.Fatalf("expected payload ok, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_EvaluatesSingleReturnBlock(t *testing.T) {
	f := newVMFrontend(t)
	if err := f.LoadSource("fun pick(value: string): string { return value }"); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	outcome, err := f.Invoke("pick", []any{"ok"})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != "ok" {
		t.Fatalf("expected payload ok, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_EvaluatesVarReturnBlock(t *testing.T) {
	f := newVMFrontend(t)
	if err := f.LoadSource("fun pick(value: string): string { var x: string = value return x }"); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	outcome, err := f.Invoke("pick", []any{"ok"})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != "ok" {
		t.Fatalf("expected payload ok, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_EvaluatesVarArithmeticBlock(t *testing.T) {
	f := newVMFrontend(t)
	if err := f.LoadSource("fun boost(base: int, bonus: int): int { var total: int = base + bonus return total }"); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	outcome, err := f.Invoke("boost", []any{7, 5})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 12 {
		t.Fatalf("expected payload 12, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_EvaluatesTwoVarBlock(t *testing.T) {
	f := newVMFrontend(t)
	if err := f.LoadSource("fun boost(base: int, bonus: int): int { var a: int = base var b: int = bonus return a + b }"); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	outcome, err := f.Invoke("boost", []any{7, 5})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 12 {
		t.Fatalf("expected payload 12, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_EvaluatesThreeVarBlock(t *testing.T) {
	f := newVMFrontend(t)
	if err := f.LoadSource("fun boost(base: int, bonus: int): int { var a: int = base var b: int = bonus var c: int = a + b return c }"); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	outcome, err := f.Invoke("boost", []any{7, 5})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 12 {
		t.Fatalf("expected payload 12, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_EvaluatesTwoVarReturnBlock(t *testing.T) {
	f := newVMFrontend(t)
	if err := f.LoadSource("fun boost(base: int, bonus: int): int { var left: int = base var right: int = bonus return left + right }"); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	outcome, err := f.Invoke("boost", []any{7, 5})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 12 {
		t.Fatalf("expected payload 12, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_EvaluatesLiteralBodies(t *testing.T) {
	f := newVMFrontend(t)
	source := "fun greet(): string = \"hello\"\nfun size(): int = 7"
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	greet, err := f.Invoke("greet", nil)
	if err != nil {
		t.Fatalf("Invoke greet: %v", err)
	}
	if greet.Payload == nil || greet.Payload.Value != "hello" {
		t.Fatalf("expected hello literal payload, got %+v", greet.Payload)
	}

	size, err := f.Invoke("size", nil)
	if err != nil {
		t.Fatalf("Invoke size: %v", err)
	}
	if size.Payload == nil || size.Payload.Value != 7 {
		t.Fatalf("expected int literal payload 7, got %+v", size.Payload)
	}
}

func TestVMFrontend_EvaluatesIntegerAddition(t *testing.T) {
	f := newVMFrontend(t)
	if err := f.LoadSource("fun add(base: int, bonus: int): int = base + bonus"); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	outcome, err := f.Invoke("add", []any{7, 5})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 12 {
		t.Fatalf("expected payload 12, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_EvaluatesStringConcatenation(t *testing.T) {
	f := newVMFrontend(t)
	if err := f.LoadSource("fun join(left: string, right: string): string = left + right"); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	outcome, err := f.Invoke("join", []any{"hello", " world"})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != "hello world" {
		t.Fatalf("expected concatenated payload, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_EvaluatesBoolAndComparisonBodies(t *testing.T) {
	f := newVMFrontend(t)
	source := "fun yes(): bool = true\nfun eq(): bool = 5 == 5\nfun lt(left: int, right: int): bool = left < right"
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	yes, err := f.Invoke("yes", nil)
	if err != nil {
		t.Fatalf("Invoke yes: %v", err)
	}
	if yes.Payload == nil || yes.Payload.Value != true {
		t.Fatalf("expected bool literal true, got %+v", yes.Payload)
	}

	eq, err := f.Invoke("eq", nil)
	if err != nil {
		t.Fatalf("Invoke eq: %v", err)
	}
	if eq.Payload == nil || eq.Payload.Value != true {
		t.Fatalf("expected equality result true, got %+v", eq.Payload)
	}

	lt, err := f.Invoke("lt", []any{3, 5})
	if err != nil {
		t.Fatalf("Invoke lt: %v", err)
	}
	if lt.Payload == nil || lt.Payload.Value != true {
		t.Fatalf("expected comparison result true, got %+v", lt.Payload)
	}
}

func TestVMFrontend_EvaluatesLogicalBodies(t *testing.T) {
	f := newVMFrontend(t)
	source := "fun both(): bool = true && false\nfun either(): bool = false || true\nfun flip(): bool = !false"
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	both, err := f.Invoke("both", nil)
	if err != nil {
		t.Fatalf("Invoke both: %v", err)
	}
	if both.Payload == nil || both.Payload.Value != false {
		t.Fatalf("expected both=false, got %+v", both.Payload)
	}

	either, err := f.Invoke("either", nil)
	if err != nil {
		t.Fatalf("Invoke either: %v", err)
	}
	if either.Payload == nil || either.Payload.Value != true {
		t.Fatalf("expected either=true, got %+v", either.Payload)
	}

	flip, err := f.Invoke("flip", nil)
	if err != nil {
		t.Fatalf("Invoke flip: %v", err)
	}
	if flip.Payload == nil || flip.Payload.Value != true {
		t.Fatalf("expected flip=true, got %+v", flip.Payload)
	}
}

func TestVMFrontend_TypeCastFailureHasStructuredDiagnostic(t *testing.T) {
	f := newVMFrontend(t)
	source := "class Alpha { x: int }\nclass Beta { y: int }\nfun fail(): bool { var a: Alpha = new Alpha() return a as Beta }"
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	outcome, err := f.Invoke("fail", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultError {
		t.Fatalf("expected error result, got %s", outcome.Result.Kind)
	}
	if outcome.Result.Error == nil {
		t.Fatal("expected InvocationErrorDesc")
	}
	// Diagnostic code must be the structured "type_cast_failed", not empty.
	if outcome.Result.Error.DiagnosticCode != "type_cast_failed" {
		t.Fatalf("expected diagnostic code type_cast_failed, got %q", outcome.Result.Error.DiagnosticCode)
	}
	// Error message must contain the target type name for diagnosability.
	if !strings.Contains(outcome.Result.Error.Message, "Beta") {
		t.Fatalf("error should mention target type Beta, got: %q", outcome.Result.Error.Message)
	}
	// Callable and stage context must be populated.
	if outcome.Result.Error.Callable != "fail" {
		t.Fatalf("expected callable fail, got %q", outcome.Result.Error.Callable)
	}
}

func TestVMFrontend_WhenFallthroughDoesNotLeakToCallableSurface(t *testing.T) {
	f := newVMFrontend(t)
	source := "fun check(x: int): int { when (x) { case 1 { return 10 } case 2 { return 20 } } return 0 }\nfun other(): int = 42"
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	// Verify callable surface only has top-level functions, not when internals.
	callables := f.Callables()
	if _, ok := callables["check"]; !ok {
		t.Fatal("expected callable 'check'")
	}
	if _, ok := callables["other"]; !ok {
		t.Fatal("expected callable 'other'")
	}
	if len(callables) != 2 {
		t.Fatalf("expected exactly 2 callables, got %d: %v", len(callables), callables)
	}
	// When fallthrough returns the default value.
	outcome, err := f.Invoke("check", []any{99})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 0 {
		t.Fatalf("expected 0 (fallthrough), got %+v", outcome.Payload)
	}
}

func TestVMFrontend_DescriptorStabilityAfterComplexSyntax(t *testing.T) {
	f := newVMFrontend(t)
	source := strings.Join([]string{
		"fun add(a: int, b: int): int = a + b",
		"class Counter { value: int }",
		"struct Point { x: int y: int }",
		"fun loop_sum(n: int): int { var total: int = 0 var i: int = 0 while i < n { total = total + i i = i + 1 } return total }",
		"export fun exported_add(a: int, b: int): int = a + b",
	}, "\n")
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	// Verify callable surface: only top-level funs, not class methods or struct methods.
	callables := f.Callables()
	for name := range callables {
		if name == "add" || name == "exported_add" || name == "loop_sum" {
			continue
		}
		t.Fatalf("unexpected callable %q in surface — class/struct internals leaked", name)
	}

	// Verify exported callables only has the explicitly exported function.
	exports := f.ExportedCallables()
	if len(exports) != 1 || exports[0].Name != "exported_add" {
		t.Fatalf("expected only exported_add, got %+v", exports)
	}

	// Objects include both class and struct.
	if _, ok := f.Object("Counter"); !ok {
		t.Fatal("expected Counter object descriptor")
	}
	if _, ok := f.Object("Point"); !ok {
		t.Fatal("expected Point object descriptor")
	}
}

func TestVMFrontend_AssignmentDoesNotLeakToDescriptorSurface(t *testing.T) {
	f := newVMFrontend(t)
	source := strings.Join([]string{
		"class Counter { value: int }",
		"fun assign_value(seed: int): int { var total: int = seed total = total + 1 return total }",
		"export fun api(): int = 7",
	}, "\n")
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	callables := f.Callables()
	if _, ok := callables["assign_value"]; !ok {
		t.Fatal("expected callable 'assign_value'")
	}
	if _, ok := callables["api"]; !ok {
		t.Fatal("expected callable 'api'")
	}
	if len(callables) != 2 {
		t.Fatalf("expected exactly 2 callables, got %d: %v", len(callables), callables)
	}
	for _, leaked := range []string{"total", "value", "Counter.value"} {
		if _, ok := callables[leaked]; ok {
			t.Fatalf("unexpected leaked callable %q in surface: %v", leaked, callables)
		}
	}

	exports := f.ExportedCallables()
	if len(exports) != 1 || exports[0].Name != "api" {
		t.Fatalf("expected only exported callable api, got %+v", exports)
	}
	if _, ok := f.Object("Counter"); !ok {
		t.Fatal("expected Counter object descriptor")
	}

	outcome, err := f.Invoke("assign_value", []any{4})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 5 {
		t.Fatalf("expected 5, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_ForLoopDoesNotLeakToDescriptorSurface(t *testing.T) {
	f := newVMFrontend(t)
	source := strings.Join([]string{
		"fun sum_to(n: int): int { var total: int = 0 for (var i: int = 0; i < n; i = i + 1) { total = total + i } return total }",
		"export fun api(): int = 1",
	}, "\n")
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	callables := f.Callables()
	if _, ok := callables["sum_to"]; !ok {
		t.Fatal("expected callable 'sum_to'")
	}
	if _, ok := callables["api"]; !ok {
		t.Fatal("expected callable 'api'")
	}
	if len(callables) != 2 {
		t.Fatalf("expected exactly 2 callables, got %d: %v", len(callables), callables)
	}
	for _, leaked := range []string{"i", "total", "for"} {
		if _, ok := callables[leaked]; ok {
			t.Fatalf("unexpected leaked callable %q in surface: %v", leaked, callables)
		}
	}

	exports := f.ExportedCallables()
	if len(exports) != 1 || exports[0].Name != "api" {
		t.Fatalf("expected only exported callable api, got %+v", exports)
	}

	outcome, err := f.Invoke("sum_to", []any{4})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 6 {
		t.Fatalf("expected 6, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_ForInLoopDoesNotLeakToDescriptorSurface(t *testing.T) {
	f := newVMFrontend(t)
	source := strings.Join([]string{
		"fun count_items(): int { var count: int = 0 var items: array = [10, 20, 30] for (item in items) { count = count + 1 } return count }",
		"export fun api(): int = 2",
	}, "\n")
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	callables := f.Callables()
	if _, ok := callables["count_items"]; !ok {
		t.Fatal("expected callable 'count_items'")
	}
	if _, ok := callables["api"]; !ok {
		t.Fatal("expected callable 'api'")
	}
	if len(callables) != 2 {
		t.Fatalf("expected exactly 2 callables, got %d: %v", len(callables), callables)
	}
	for _, leaked := range []string{"item", "items", "count", "for"} {
		if _, ok := callables[leaked]; ok {
			t.Fatalf("unexpected leaked callable %q in surface: %v", leaked, callables)
		}
	}

	exports := f.ExportedCallables()
	if len(exports) != 1 || exports[0].Name != "api" {
		t.Fatalf("expected only exported callable api, got %+v", exports)
	}

	outcome, err := f.Invoke("count_items", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 3 {
		t.Fatalf("expected 3, got %+v", outcome.Payload)
	}
}

// --- Batch 5: Descriptor boundary verification for new constructs ---

func TestVMFrontend_MapCRUDDoesNotLeakToDescriptorSurface(t *testing.T) {
	f := newVMFrontend(t)
	source := strings.Join([]string{
		"fun map_ops(): int { var m: map = {\"x\": 1} m[\"y\"] = 2 return m[\"x\"] }",
		"export fun api(): int = 0",
	}, "\n")
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	callables := f.Callables()
	if _, ok := callables["map_ops"]; !ok {
		t.Fatal("expected callable 'map_ops'")
	}
	if _, ok := callables["api"]; !ok {
		t.Fatal("expected callable 'api'")
	}
	if len(callables) != 2 {
		t.Fatalf("expected exactly 2 callables, got %d: %v", len(callables), callables)
	}

	exports := f.ExportedCallables()
	if len(exports) != 1 || exports[0].Name != "api" {
		t.Fatalf("expected only exported callable api, got %+v", exports)
	}
}

func TestVMFrontend_LenBuiltinDoesNotLeakToDescriptorSurface(t *testing.T) {
	f := newVMFrontend(t)
	source := strings.Join([]string{
		"fun size(): int { return len([1, 2, 3]) }",
		"export fun api(): int = 0",
	}, "\n")
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	callables := f.Callables()
	if _, ok := callables["size"]; !ok {
		t.Fatal("expected callable 'size'")
	}
	if _, ok := callables["api"]; !ok {
		t.Fatal("expected callable 'api'")
	}
	if len(callables) != 2 {
		t.Fatalf("expected exactly 2 callables, got %d: %v", len(callables), callables)
	}

	exports := f.ExportedCallables()
	if len(exports) != 1 || exports[0].Name != "api" {
		t.Fatalf("expected only exported callable api, got %+v", exports)
	}
}

func TestVMFrontend_StructLiteralDoesNotLeakToDescriptorSurface(t *testing.T) {
	f := newVMFrontend(t)
	source := strings.Join([]string{
		"struct Point { x: int y: int }",
		"fun mk(): int { var p: Point = Point{x: 1, y: 2} return p.x }",
		"export fun api(): int = 0",
	}, "\n")
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	callables := f.Callables()
	if _, ok := callables["mk"]; !ok {
		t.Fatal("expected callable 'mk'")
	}
	if _, ok := callables["api"]; !ok {
		t.Fatal("expected callable 'api'")
	}
	if len(callables) != 2 {
		t.Fatalf("expected exactly 2 callables, got %d: %v", len(callables), callables)
	}

	if _, ok := f.Object("Point"); !ok {
		t.Fatal("expected Point object descriptor")
	}

	exports := f.ExportedCallables()
	if len(exports) != 1 || exports[0].Name != "api" {
		t.Fatalf("expected only exported callable api, got %+v", exports)
	}
}

func TestVMFrontend_WhenWithDefaultDoesNotLeakToDescriptorSurface(t *testing.T) {
	f := newVMFrontend(t)
	source := strings.Join([]string{
		"fun classify(x: int): int { when (x) { case 1 { return 10 } else { return 0 } } }",
		"export fun api(): int = 1",
	}, "\n")
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	callables := f.Callables()
	if _, ok := callables["classify"]; !ok {
		t.Fatal("expected callable 'classify'")
	}
	if _, ok := callables["api"]; !ok {
		t.Fatal("expected callable 'api'")
	}
	if len(callables) != 2 {
		t.Fatalf("expected exactly 2 callables, got %d: %v", len(callables), callables)
	}

	exports := f.ExportedCallables()
	if len(exports) != 1 || exports[0].Name != "api" {
		t.Fatalf("expected only exported callable api, got %+v", exports)
	}
}

func TestVMFrontend_RecursiveFunctionDoesNotLeakToDescriptorSurface(t *testing.T) {
	f := newVMFrontend(t)
	source := strings.Join([]string{
		"fun fact(n: int): int { if n <= 1 { return 1 } return n * fact(n - 1) }",
		"export fun api(): int = 3",
	}, "\n")
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	callables := f.Callables()
	if _, ok := callables["fact"]; !ok {
		t.Fatal("expected callable 'fact'")
	}
	if _, ok := callables["api"]; !ok {
		t.Fatal("expected callable 'api'")
	}
	if len(callables) != 2 {
		t.Fatalf("expected exactly 2 callables, got %d: %v", len(callables), callables)
	}

	exports := f.ExportedCallables()
	if len(exports) != 1 || exports[0].Name != "api" {
		t.Fatalf("expected only exported callable api, got %+v", exports)
	}
}

func TestVMFrontend_BreakContinueDoesNotLeakToDescriptorSurface(t *testing.T) {
	f := newVMFrontend(t)
	source := strings.Join([]string{
		"fun loop_test(n: int): int { var total: int = 0 for (var i: int = 0; i < n; i = i + 1) { if i == 3 { continue } if i == 5 { break } total = total + i } return total }",
		"export fun api(): int = 4",
	}, "\n")
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	callables := f.Callables()
	if _, ok := callables["loop_test"]; !ok {
		t.Fatal("expected callable 'loop_test'")
	}
	if _, ok := callables["api"]; !ok {
		t.Fatal("expected callable 'api'")
	}
	if len(callables) != 2 {
		t.Fatalf("expected exactly 2 callables, got %d: %v", len(callables), callables)
	}

	exports := f.ExportedCallables()
	if len(exports) != 1 || exports[0].Name != "api" {
		t.Fatalf("expected only exported callable api, got %+v", exports)
	}
}

func TestVMFrontend_NestedControlFlowDoesNotLeakToDescriptorSurface(t *testing.T) {
	f := newVMFrontend(t)
	source := strings.Join([]string{
		"fun deep(n: int): int { var total: int = 0 while n > 0 { if n % 2 == 0 { total = total + n } n = n - 1 } return total }",
		"export fun api(): int = 5",
	}, "\n")
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	callables := f.Callables()
	if _, ok := callables["deep"]; !ok {
		t.Fatal("expected callable 'deep'")
	}
	if _, ok := callables["api"]; !ok {
		t.Fatal("expected callable 'api'")
	}
	if len(callables) != 2 {
		t.Fatalf("expected exactly 2 callables, got %d: %v", len(callables), callables)
	}

	exports := f.ExportedCallables()
	if len(exports) != 1 || exports[0].Name != "api" {
		t.Fatalf("expected only exported callable api, got %+v", exports)
	}
}

func TestVMFrontend_ScriptImportStructTypeExecutes(t *testing.T) {
	resolver := frontend.MapModuleResolver{
		"geom": `export struct Point { x: int, y: int }
export fun makePoint(): Point { return Point{x: 3, y: 4} }`,
	}
	f := newVMFrontendWithResolver(t, resolver)
	if err := f.LoadSource(`import Point from "geom"
import makePoint from "geom"
fun use(): int {
	var p: Point = makePoint()
	return p.x + p.y
}`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("use", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 7 {
		t.Fatalf("expected payload 7, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_ScriptImportTypeAliasExecutes(t *testing.T) {
	resolver := frontend.MapModuleResolver{
		"types": `export type UserId = string
export fun getAdmin(): UserId { return "admin" }`,
	}
	f := newVMFrontendWithResolver(t, resolver)
	if err := f.LoadSource(`import UserId from "types"
import getAdmin from "types"
fun greet(): string {
	var name: UserId = getAdmin()
	return "hello " + name
}`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("greet", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != "hello admin" {
		t.Fatalf("expected payload 'hello admin', got %+v", outcome.Payload)
	}
}

func newVMFrontendWithNativeTypeCapability(t *testing.T) *frontend.Frontend {
	t.Helper()
	sb := binding.NewScriptBinding()
	builder := binding.NewCapability("types", "module")
	if err := builder.AddTypeAlias("UserId", schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}); err != nil {
		t.Fatalf("AddTypeAlias: %v", err)
	}
	if err := builder.AddObject("Point", schema.ObjectDesc{Kind: schema.TypeKindStruct, Fields: []schema.FieldDesc{
		{Name: "x", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}},
		{Name: "y", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}},
	}}); err != nil {
		t.Fatalf("AddObject: %v", err)
	}
	cap, err := builder.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := sb.RegisterCapability(cap); err != nil {
		t.Fatalf("RegisterCapability: %v", err)
	}
	f, err := frontend.New(sb)
	if err != nil {
		t.Fatalf("frontend.New: %v", err)
	}
	vmEval := bytecode.NewVMEvaluator()
	vmEval.SetNativeBinding(sb)
	f.SetVMCompileHook(vmEval)
	f.SetModuleResolver(frontend.MapModuleResolver{})
	return f
}

func TestVMFrontend_NativeTypeAliasInVarAnnotationExecutes(t *testing.T) {
	f := newVMFrontendWithNativeTypeCapability(t)
	if err := f.LoadSource(`import UserId from "types"
fun read_id(): string {
	var id: UserId = "admin"
	return id
}`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	outcome, err := f.Invoke("read_id", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != "admin" {
		t.Fatalf("expected admin, got %+v", outcome.Payload)
	}
	for _, sym := range f.CompiledDeclarations().ImportedSymbols() {
		if sym.LocalName == "UserId" {
			t.Fatalf("native imported type alias should not appear as runtime symbol: %+v", sym)
		}
	}
	importedTypes := f.CompiledDeclarations().ImportedTypes()
	var aliasMeta *frontend.ImportedTypeMetadata
	for i := range importedTypes {
		if importedTypes[i].LocalName == "UserId" {
			aliasMeta = &importedTypes[i]
			break
		}
	}
	if aliasMeta == nil {
		t.Fatalf("expected UserId in ImportedTypes, got %+v", importedTypes)
	}
	if aliasMeta.Kind != "type" {
		t.Fatalf("expected UserId Kind=type, got %q", aliasMeta.Kind)
	}
	if aliasMeta.SourceKind != "native" {
		t.Fatalf("expected UserId SourceKind=native, got %q", aliasMeta.SourceKind)
	}
	if aliasMeta.Type != "string" {
		t.Fatalf("expected UserId Type=string, got %q", aliasMeta.Type)
	}
}

func TestVMFrontend_NativeObjectTypeInSignatureCompiles(t *testing.T) {
	f := newVMFrontendWithNativeTypeCapability(t)
	if err := f.LoadSource(`import Point from "types"
fun accept(p: Point): int { return 1 }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	callable, ok := f.Callable("accept")
	if !ok {
		t.Fatal("expected callable accept")
	}
	if len(callable.Parameters) != 1 || callable.Parameters[0].Type.Kind != schema.TypeKindStruct || callable.Parameters[0].Type.Name != "Point" {
		t.Fatalf("expected Point struct parameter, got %+v", callable.Parameters)
	}
	for _, sym := range f.CompiledDeclarations().ImportedSymbols() {
		if sym.LocalName == "Point" {
			t.Fatalf("native imported object type should not appear as runtime symbol: %+v", sym)
		}
	}
	importedTypes := f.CompiledDeclarations().ImportedTypes()
	var pointMeta *frontend.ImportedTypeMetadata
	for i := range importedTypes {
		if importedTypes[i].LocalName == "Point" {
			pointMeta = &importedTypes[i]
			break
		}
	}
	if pointMeta == nil {
		t.Fatalf("expected Point in ImportedTypes, got %+v", importedTypes)
	}
	if pointMeta.Kind != "struct" {
		t.Fatalf("expected Point Kind=struct, got %q", pointMeta.Kind)
	}
	if pointMeta.SourceKind != "native" {
		t.Fatalf("expected Point SourceKind=native, got %q", pointMeta.SourceKind)
	}
	if pointMeta.ObjectDesc == nil || len(pointMeta.ObjectDesc.Fields) != 2 {
		t.Fatalf("expected Point ObjectDesc with 2 fields, got %+v", pointMeta.ObjectDesc)
	}
}

func TestVMFrontend_BatchImportScriptFunctions(t *testing.T) {
	resolver := frontend.MapModuleResolver{
		"math": `export fun add(a: int, b: int): int { return a + b }
export fun sub(a: int, b: int): int { return a - b }`,
	}
	f := newVMFrontendWithResolver(t, resolver)
	if err := f.LoadSource(`import { add, sub } from "math"
fun calc(): int {
	return add(3, 4) + sub(10, 2)
}`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("calc", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 15 {
		t.Fatalf("expected payload 15, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_BatchImportWithAlias(t *testing.T) {
	resolver := frontend.MapModuleResolver{
		"math": `export fun add(a: int, b: int): int { return a + b }`,
	}
	f := newVMFrontendWithResolver(t, resolver)
	if err := f.LoadSource(`import { add as plus } from "math"
fun calc(): int {
	return plus(5, 6)
}`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("calc", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 11 {
		t.Fatalf("expected payload 11, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_BatchImportStructAndCallable(t *testing.T) {
	resolver := frontend.MapModuleResolver{
		"geom": `export struct Point { x: int, y: int }
export fun makePoint(): Point { return Point{x: 3, y: 4} }`,
	}
	f := newVMFrontendWithResolver(t, resolver)
	if err := f.LoadSource(`import { Point, makePoint } from "geom"
fun use(): int {
	var p: Point = makePoint()
	return p.x + p.y
}`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("use", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 7 {
		t.Fatalf("expected payload 7, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_ReExportNativeFreeFunctionExecutes(t *testing.T) {
	sb := binding.NewScriptBinding()
	builder := binding.NewCapability("math", "module")
	if err := builder.AddFreeFunction("abs", func(x float64) float64 {
		if x < 0 {
			return -x
		}
		return x
	}); err != nil {
		t.Fatalf("AddFreeFunction: %v", err)
	}
	cap, err := builder.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := sb.RegisterCapability(cap); err != nil {
		t.Fatalf("RegisterCapability: %v", err)
	}
	if err := sb.ExposeCapabilityCallables("math"); err != nil {
		t.Fatalf("ExposeCapabilityCallables: %v", err)
	}
	f, err := frontend.New(sb)
	if err != nil {
		t.Fatalf("frontend.New: %v", err)
	}
	vmEval := bytecode.NewVMEvaluator()
	vmEval.SetNativeBinding(sb)
	f.SetVMCompileHook(vmEval)
	f.SetModuleResolver(frontend.MapModuleResolver{
		"api": `export abs from "math"`,
	})
	if err := f.LoadSource(`import abs from "api"
fun call_abs(): double { return abs(-3.14) }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("call_abs", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 3.14 {
		t.Fatalf("expected payload 3.14, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_WildcardReExportNativeFreeFunctionExecutes(t *testing.T) {
	sb := binding.NewScriptBinding()
	builder := binding.NewCapability("math", "module")
	if err := builder.AddFreeFunction("abs", func(x float64) float64 {
		if x < 0 {
			return -x
		}
		return x
	}); err != nil {
		t.Fatalf("AddFreeFunction abs: %v", err)
	}
	if err := builder.AddFreeFunction("twice", func(x float64) float64 { return x * 2 }); err != nil {
		t.Fatalf("AddFreeFunction twice: %v", err)
	}
	cap, err := builder.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := sb.RegisterCapability(cap); err != nil {
		t.Fatalf("RegisterCapability: %v", err)
	}
	if err := sb.ExposeCapabilityCallables("math"); err != nil {
		t.Fatalf("ExposeCapabilityCallables: %v", err)
	}
	f, err := frontend.New(sb)
	if err != nil {
		t.Fatalf("frontend.New: %v", err)
	}
	vmEval := bytecode.NewVMEvaluator()
	vmEval.SetNativeBinding(sb)
	f.SetVMCompileHook(vmEval)
	f.SetModuleResolver(frontend.MapModuleResolver{
		"api": `export * from "math"`,
	})
	if err := f.LoadSource(`import twice from "api"
import abs from "api"
fun call_twice(): double { return twice(abs(-2.5)) }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("call_twice", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 5.0 {
		t.Fatalf("expected payload 5.0, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_ReExportCallable(t *testing.T) {
	resolver := frontend.MapModuleResolver{
		"math": `export fun add(a: int, b: int): int { return a + b }`,
		"api":  `export add from "math"`,
	}
	f := newVMFrontendWithResolver(t, resolver)
	if err := f.LoadSource(`import add from "api"
fun calc(): int { return add(2, 3) }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("calc", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 5 {
		t.Fatalf("expected payload 5, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_ReExportStructType(t *testing.T) {
	resolver := frontend.MapModuleResolver{
		"geom": `export struct Point { x: int, y: int }
export fun makePoint(): Point { return Point{x: 3, y: 4} }`,
		"types": `export Point from "geom"`,
	}
	f := newVMFrontendWithResolver(t, resolver)
	if err := f.LoadSource(`import Point from "types"
import makePoint from "geom"
fun use(): int {
	var p: Point = makePoint()
	return p.x + p.y
}`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("use", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 7 {
		t.Fatalf("expected payload 7, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_WildcardReExportCallable(t *testing.T) {
	resolver := frontend.MapModuleResolver{
		"math": `export fun add(a: int, b: int): int { return a + b }
export fun mul(a: int, b: int): int { return a * b }`,
		"api": `export * from "math"`,
	}
	f := newVMFrontendWithResolver(t, resolver)
	if err := f.LoadSource(`import add from "api"
fun calc(): int { return add(2, 3) }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("calc", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 5 {
		t.Fatalf("expected payload 5, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_WildcardReExportMixed(t *testing.T) {
	resolver := frontend.MapModuleResolver{
		"math": `export struct Point { x: int, y: int }
export fun add(a: int, b: int): int { return a + b }
export type IntPair = array<int>`,
		"api": `export * from "math"`,
	}
	f := newVMFrontendWithResolver(t, resolver)
	if err := f.LoadSource(`import Point from "api"
import add from "api"
fun calc(): int {
	var p: Point = Point{x: 1, y: 2}
	return add(p.x, p.y)
}`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("calc", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 3 {
		t.Fatalf("expected payload 3, got %+v", outcome.Payload)
	}
}
