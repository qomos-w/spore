package script_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/qomos-w/spore/schema"
	"github.com/qomos-w/spore/script"
)

// mapResolver is a tiny illustrative ModuleResolver for tests/examples; it
// resolves import paths against an in-memory map and is the kind of helper a
// host might write to feed source from any storage layer.
type mapResolver map[string]string

func (m mapResolver) ResolveModule(path string) (string, error) {
	src, ok := m[path]
	if !ok {
		return "", fmt.Errorf("module %q not found", path)
	}
	return src, nil
}

// TestExternalHostHappyPath exercises the canonical embedding flow that an
// external host should follow without importing any internal/* packages:
//
//	NewRuntime → Bind* → LoadSource → Exports → Call → DecodeInto
//
// The package declaration (script_test, not script) ensures only the public
// surface is reachable; if any step requires an internal/* import, this file
// cannot compile.
func TestCallContextCancellation(t *testing.T) {
	rt, err := script.NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.LoadSource("cancel", `export fun answer(): int = 42`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := rt.CallContext(script.CallContext{Context: ctx, Budget: script.ExecutionBudget{MaxDuration: time.Second}}, "answer")
	if err != nil || result.Error == nil {
		t.Fatalf("expected cancelled call, result=%+v err=%v", result, err)
	}
}

func TestCallContextInstructionBudget(t *testing.T) {
	rt, err := script.NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.LoadSource("budget", `export fun answer(): int = 42`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	result, err := rt.CallContext(script.CallContext{Budget: script.ExecutionBudget{MaxInstructions: 1}}, "answer")
	if err != nil || result.Error == nil {
		t.Fatalf("expected instruction budget error, result=%+v err=%v", result, err)
	}
}

func TestLoadPackageAndReloadPackage(t *testing.T) {
	rt, err := script.NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	pkg := script.Package{
		AppID:       "com.example.app",
		Version:     "1.0.0",
		EntryModule: "main",
		Modules: map[string]string{
			"main": `import { value } from "lib"
export fun answer(): int = value()`,
			"lib": `export fun value(): int = 42`,
		},
	}
	pkg.Hash = pkg.ComputeHash()
	if err := rt.LoadPackage(pkg); err != nil {
		t.Fatalf("LoadPackage: %v", err)
	}
	result, err := rt.Call("answer")
	if err != nil || !result.Ok() {
		t.Fatalf("call package: result=%+v err=%v", result, err)
	}
	var got int32
	if err := result.DecodeInto(&got); err != nil || got != 42 {
		t.Fatalf("package result=%d err=%v", got, err)
	}
	bad := pkg
	bad.Modules = map[string]string{"main": `export fun answer(): int = 99`}
	if err := rt.ReloadPackage(bad); err == nil {
		t.Fatal("expected hash mismatch")
	}
	good := script.Package{AppID: pkg.AppID, Version: "2.0.0", EntryModule: "main", Modules: map[string]string{"main": `export fun answer(): int = 99`}}
	good.Hash = good.ComputeHash()
	if err := rt.ReloadPackage(good); err != nil {
		t.Fatalf("ReloadPackage: %v", err)
	}
	result, err = rt.Call("answer")
	if err != nil || !result.Ok() {
		t.Fatalf("call reloaded package: result=%+v err=%v", result, err)
	}
	if err := result.DecodeInto(&got); err != nil || got != 99 {
		t.Fatalf("reloaded result=%d err=%v", got, err)
	}
}

func TestExternalHostHappyPath(t *testing.T) {
	rt, err := script.NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}

	if err := rt.BindValue("env", "AppName", "spore"); err != nil {
		t.Fatalf("BindValue: %v", err)
	}
	if err := rt.BindFunc("tool", "double", func(n int64) int64 { return n * 2 }); err != nil {
		t.Fatalf("BindFunc: %v", err)
	}

	if err := rt.LoadSource("demo", `export fun answer(): int = 42`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	if got := rt.RootModule(); got != "demo" {
		t.Fatalf("RootModule = %q, want %q", got, "demo")
	}

	exports, err := rt.Exports("demo")
	if err != nil {
		t.Fatalf("Exports: %v", err)
	}
	if len(exports) != 1 || exports[0].Name != "answer" {
		t.Fatalf("unexpected exports: %+v", exports)
	}

	info, err := rt.LookupCallable("demo", "answer")
	if err != nil {
		t.Fatalf("LookupCallable: %v", err)
	}
	if info.Name != "answer" {
		t.Fatalf("LookupCallable name = %q, want %q", info.Name, "answer")
	}

	result, err := rt.Call("answer")
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if result.Error != nil {
		t.Fatalf("unexpected runtime error: %v", result.Error)
	}

	var n int
	if err := result.DecodeInto(&n); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if n != 42 {
		t.Fatalf("DecodeInto value = %d, want 42", n)
	}
}

// TestExternalHostCompileErrorIsClassified verifies that an external host can
// distinguish compile-time failures using only the public surface.
func TestExternalHostCompileErrorIsClassified(t *testing.T) {
	rt, err := script.NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	err = rt.LoadSource("broken", `fun bad(: int) {}`)
	if err == nil {
		t.Fatal("expected compile error from broken source")
	}
	if !script.IsCompileError(err) {
		t.Fatalf("expected compile error, got %T: %v", err, err)
	}
}

// TestExternalHostBoundSurfacesAreDiscoverable verifies that an external host
// can introspect what it has bound through the public BoundFunctions and
// BoundValues accessors.
func TestExternalHostBoundSurfacesAreDiscoverable(t *testing.T) {
	rt, err := script.NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.BindFunc("tool", "echo", func(s string) string { return s }); err != nil {
		t.Fatalf("BindFunc: %v", err)
	}
	if err := rt.BindValue("env", "Region", "local"); err != nil {
		t.Fatalf("BindValue: %v", err)
	}
	funcs := rt.BoundFunctions()
	if len(funcs) != 1 || funcs[0].Callable != "tool.echo" {
		t.Fatalf("unexpected bound functions: %+v", funcs)
	}
	values := rt.BoundValues()
	if len(values) != 1 || values[0].Namespace != "env" || values[0].Name != "Region" {
		t.Fatalf("unexpected bound values: %+v", values)
	}
}

// TestExternalHostMultiBindOneNamespace verifies the post-A1 contract: a
// host can mix BindFunc/BindValue/BindObject calls under the same namespace
// before LoadSource, and the script can import every name that was bound.
func TestExternalHostMultiBindOneNamespace(t *testing.T) {
	rt, err := script.NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.BindFunc("math", "abs", func(x float64) float64 {
		if x < 0 {
			return -x
		}
		return x
	}); err != nil {
		t.Fatalf("BindFunc abs: %v", err)
	}
	if err := rt.BindFunc("math", "twice", func(x float64) float64 { return x * 2 }); err != nil {
		t.Fatalf("BindFunc twice on same namespace should succeed: %v", err)
	}
	if err := rt.BindObject("math", map[string]any{
		"PI":   3.14,
		"half": func(x float64) float64 { return x / 2 },
	}); err != nil {
		t.Fatalf("BindObject on same namespace should succeed: %v", err)
	}

	src := `import abs from "math"
import twice from "math"
import PI from "math"
import half from "math"
export fun ring(): double { return abs(twice(-PI)) }
export fun midPI(): double { return half(PI) }`
	if err := rt.LoadSource("demo", src); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	resultRing, err := rt.Call("ring")
	if err != nil {
		t.Fatalf("Call ring: %v", err)
	}
	var ring float64
	if err := resultRing.DecodeInto(&ring); err != nil {
		t.Fatalf("DecodeInto ring: %v", err)
	}
	if ring < 6.27 || ring > 6.29 {
		t.Fatalf("expected ring ≈ 6.28, got %v", ring)
	}

	resultMid, err := rt.Call("midPI")
	if err != nil {
		t.Fatalf("Call midPI: %v", err)
	}
	var mid float64
	if err := resultMid.DecodeInto(&mid); err != nil {
		t.Fatalf("DecodeInto midPI: %v", err)
	}
	if mid < 1.56 || mid > 1.58 {
		t.Fatalf("expected midPI ≈ 1.57, got %v", mid)
	}
}

// TestExternalHostBindAfterLoadIsSealed verifies the post-A1 contract: once
// LoadSource has been called, further Bind* calls are rejected and the
// existing Runtime cannot have its host surface mutated.
func TestExternalHostBindAfterLoadIsSealed(t *testing.T) {
	rt, err := script.NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.LoadSource("demo", `export fun a(): int = 1`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	if err := rt.BindFunc("late", "f", func() int64 { return 0 }); err == nil {
		t.Fatal("expected BindFunc to fail after LoadSource")
	}
}

// TestExternalHostModuleResolverEndToEnd exercises the full module resolution
// path through the public Runtime:
//
//	SetModuleResolver → LoadSource (with imports) → Exports of imported module →
//	Call root callable that uses the imported function.
//
// This proves a host can supply source for transitively-imported modules
// without reaching into internal/*.
func TestExternalHostModuleResolverEndToEnd(t *testing.T) {
	resolver := mapResolver{
		"math": `export fun add(a: int, b: int): int { return a + b }`,
	}

	rt, err := script.NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	rt.SetModuleResolver(resolver)

	rootSrc := `import add from "math"
export fun sum(a: int, b: int): int { return add(a, b) }`
	if err := rt.LoadSource("root", rootSrc); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	loaded := rt.LoadedModules()
	if len(loaded) < 2 {
		t.Fatalf("expected root + math in LoadedModules, got %v", loaded)
	}
	var sawMath bool
	for _, name := range loaded {
		if name == "math" {
			sawMath = true
			break
		}
	}
	if !sawMath {
		t.Fatalf("expected LoadedModules to include %q, got %v", "math", loaded)
	}

	mathExports, err := rt.Exports("math")
	if err != nil {
		t.Fatalf("Exports(math): %v", err)
	}
	if len(mathExports) != 1 || mathExports[0].Name != "add" {
		t.Fatalf("unexpected math exports: %+v", mathExports)
	}

	if _, err := rt.LookupCallable("math", "add"); err != nil {
		t.Fatalf("LookupCallable(math, add): %v", err)
	}

	result, err := rt.Call("sum", 2, 3)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if result.Error != nil {
		t.Fatalf("unexpected runtime error: %v", result.Error)
	}
	var n int
	if err := result.DecodeInto(&n); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if n != 5 {
		t.Fatalf("DecodeInto value = %d, want 5", n)
	}
}

// TestExternalHostUnresolvableImportReturnsCompileError verifies that a
// missing import surfaces as a compile error through the public surface.
func TestExternalHostUnresolvableImportReturnsCompileError(t *testing.T) {
	rt, err := script.NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	rt.SetModuleResolver(mapResolver{})

	err = rt.LoadSource("root", `import add from "missing"
export fun use(a: int): int { return add(a, 1) }`)
	if err == nil {
		t.Fatal("expected compile error for missing module")
	}
	if !script.IsCompileError(err) {
		t.Fatalf("expected compile error, got %T: %v", err, err)
	}
}

// TestExternalHostCompileErrorCarriesStructuredDiagnostic verifies the public
// CompileError exposes structured fields (Code, Category, Path, Span) rather
// than collapsing to opaque text.
func TestExternalHostCompileErrorCarriesStructuredDiagnostic(t *testing.T) {
	rt, err := script.NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	err = rt.LoadSource("broken", `fun bad(: int) {}`)
	if err == nil {
		t.Fatal("expected compile error")
	}
	ce, ok := err.(*script.CompileError)
	if !ok {
		t.Fatalf("expected *script.CompileError, got %T", err)
	}
	d := ce.Diagnostic
	if d.Code == "" {
		t.Errorf("expected non-empty Diagnostic.Code")
	}
	if d.Category == "" {
		t.Errorf("expected non-empty Diagnostic.Category")
	}
	if d.Message == "" {
		t.Errorf("expected non-empty Diagnostic.Message")
	}
	if d.Span.Start.Line == 0 && d.Span.Start.Column == 0 && d.Span.End.Line == 0 && d.Span.End.Column == 0 {
		t.Errorf("expected non-zero Diagnostic.Span for parse error: %+v", d.Span)
	}
}

// TestExternalHostRuntimeErrorCarriesStructuredDiagnostic verifies the public
// RuntimeError surfaces include structured fields preserved from the
// invocation pipeline (Code, Category, Stack).
func TestExternalHostRuntimeErrorCarriesStructuredDiagnostic(t *testing.T) {
	rt, err := script.NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	err = rt.LoadSource("demo", `class Alpha { x: int }
class Beta { y: int }
export fun fail(): bool { var a: Alpha = new Alpha() return a as Beta }`)
	if err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	result, err := rt.Call("fail")
	if err != nil {
		t.Fatalf("Call: unexpected pipeline error: %v", err)
	}
	if result.Error == nil {
		t.Fatal("expected structured runtime error result")
	}
	d := result.Error.Diagnostic
	if d.Code == "" {
		t.Errorf("expected non-empty Diagnostic.Code")
	}
	if d.Category == "" {
		t.Errorf("expected non-empty Diagnostic.Category")
	}
	if d.Message == "" {
		t.Errorf("expected non-empty Diagnostic.Message")
	}
	if len(d.Stack) == 0 {
		t.Errorf("expected non-empty Diagnostic.Stack for runtime error")
	} else if d.Stack[0].Callable != "fail" {
		t.Errorf("expected stack frame callable %q, got %q", "fail", d.Stack[0].Callable)
	}
}

// TestExternalHostStreamingCallable exercises the public streaming invocation
// surface end-to-end without importing any internal/* packages.
func TestExternalHostStreamingCallable(t *testing.T) {
	rt, err := script.NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}

	if err := rt.LoadSource("demo", `export stream fun counter(): int { yield 10 yield 20 return 30 }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	exports, err := rt.Exports("demo")
	if err != nil {
		t.Fatalf("Exports: %v", err)
	}
	if len(exports) != 1 || exports[0].Name != "counter" {
		t.Fatalf("unexpected exports: %+v", exports)
	}
	if exports[0].Mode != "streaming" {
		t.Fatalf("expected streaming mode, got %q", exports[0].Mode)
	}

	// CallNext produces intermediate values
	result, err := rt.CallNext("counter")
	if err != nil {
		t.Fatalf("CallNext first: %v", err)
	}
	if result.Error != nil {
		t.Fatalf("unexpected runtime error: %v", result.Error)
	}
	var n int
	if err := result.DecodeInto(&n); err != nil {
		t.Fatalf("DecodeInto first: %v", err)
	}
	if n != 10 {
		t.Fatalf("first CallNext got %d, want 10", n)
	}

	result, err = rt.CallNext("counter")
	if err != nil {
		t.Fatalf("CallNext second: %v", err)
	}
	if err := result.DecodeInto(&n); err != nil {
		t.Fatalf("DecodeInto second: %v", err)
	}
	if n != 20 {
		t.Fatalf("second CallNext got %d, want 20", n)
	}

	// CallFinal produces terminal value
	result, err = rt.CallFinal("counter")
	if err != nil {
		t.Fatalf("CallFinal: %v", err)
	}
	if result.Error != nil {
		t.Fatalf("unexpected runtime error: %v", result.Error)
	}
	if err := result.DecodeInto(&n); err != nil {
		t.Fatalf("DecodeInto final: %v", err)
	}
	if n != 30 {
		t.Fatalf("CallFinal got %d, want 30", n)
	}
}

// TestExternalHostStreamingCallableHandle verifies that PrepareCallable works
// for streaming callables and that the handle's Next/Final methods are usable.
func TestExternalHostStreamingCallableHandle(t *testing.T) {
	rt, err := script.NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}

	if err := rt.LoadSource("demo", `export stream fun gen(): int { yield 7 return 42 }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	h, err := rt.PrepareCallable("gen")
	if err != nil {
		t.Fatalf("PrepareCallable: %v", err)
	}
	if h.Info().Mode != "streaming" {
		t.Fatalf("expected streaming mode, got %q", h.Info().Mode)
	}

	result, err := h.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	var n int
	if err := result.DecodeInto(&n); err != nil {
		t.Fatalf("DecodeInto next: %v", err)
	}
	if n != 7 {
		t.Fatalf("Next got %d, want 7", n)
	}

	result, err = h.Final()
	if err != nil {
		t.Fatalf("Final: %v", err)
	}
	if err := result.DecodeInto(&n); err != nil {
		t.Fatalf("DecodeInto final: %v", err)
	}
	if n != 42 {
		t.Fatalf("Final got %d, want 42", n)
	}
}

// TestExternalHostUnaryCallableRejectsStreamingStages verifies that CallNext
// and CallFinal on a unary callable return host-side runtime errors with the
// correct diagnostic code.
func TestExternalHostUnaryCallableRejectsStreamingStages(t *testing.T) {
	rt, err := script.NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}

	if err := rt.LoadSource("demo", `export fun one(): int = 1`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	_, err = rt.CallNext("one")
	if err == nil {
		t.Fatal("expected CallNext on unary callable to return error")
	}
	if !script.IsRuntimeError(err) {
		t.Fatalf("expected runtime error, got %T", err)
	}
}

// TestExternalHostBindStructDesc verifies that a host can register a schema
// descriptor through the public surface and introspect it via BoundObjects.
func TestExternalHostBindStructDesc(t *testing.T) {
	rt, err := script.NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}

	if err := rt.BindStructDesc("geom", "Point", schema.ObjectDesc{
		Kind: schema.TypeKindStruct,
		Fields: []schema.FieldDesc{
			{Name: "X", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}},
			{Name: "Y", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}},
		},
	}); err != nil {
		t.Fatalf("BindStructDesc: %v", err)
	}

	objs := rt.BoundObjects()
	if len(objs) != 1 {
		t.Fatalf("expected 1 bound object, got %d", len(objs))
	}
	if objs[0].Namespace != "geom" || objs[0].Name != "Point" {
		t.Fatalf("unexpected bound object: %+v", objs[0])
	}
	if len(objs[0].ObjectDesc.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(objs[0].ObjectDesc.Fields))
	}
}

// TestExternalHostBindStruct verifies that a host can register a Go struct type
// through the public surface and introspect it via BoundObjects.
func TestExternalHostBindStruct(t *testing.T) {
	rt, err := script.NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}

	type Point struct {
		X int
		Y int
	}
	if err := rt.BindStruct("geom", "Point", Point{}); err != nil {
		t.Fatalf("BindStruct: %v", err)
	}

	objs := rt.BoundObjects()
	if len(objs) != 1 {
		t.Fatalf("expected 1 bound object, got %d", len(objs))
	}
	if objs[0].Namespace != "geom" || objs[0].Name != "Point" {
		t.Fatalf("unexpected bound object: %+v", objs[0])
	}
	if len(objs[0].ObjectDesc.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(objs[0].ObjectDesc.Fields))
	}
}

// TestExternalHostBindInterfaceDesc verifies that a host can register a schema
// interface descriptor through the public surface and introspect it via BoundInterfaces.
func TestExternalHostBindInterfaceDesc(t *testing.T) {
	rt, err := script.NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}

	if err := rt.BindInterfaceDesc("contracts", "Greeter", schema.InterfaceDesc{
		Methods: []schema.MethodDesc{{
			Name:       "greet",
			Parameters: []schema.ParameterDesc{{Name: "name", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}}},
			Returns:    []schema.TypeDesc{{Kind: schema.TypeKindScalar, Name: "string"}},
		}},
	}); err != nil {
		t.Fatalf("BindInterfaceDesc: %v", err)
	}

	ifaces := rt.BoundInterfaces()
	if len(ifaces) != 1 {
		t.Fatalf("expected 1 bound interface, got %d", len(ifaces))
	}
	if ifaces[0].Namespace != "contracts" || ifaces[0].Name != "Greeter" {
		t.Fatalf("unexpected bound interface: %+v", ifaces[0])
	}
	if len(ifaces[0].InterfaceDesc.Methods) != 1 {
		t.Fatalf("expected 1 method, got %d", len(ifaces[0].InterfaceDesc.Methods))
	}
}

type externalTestHostGreeter struct{}

func (g *externalTestHostGreeter) Greet(name string) string { return "hello " + name }

// TestExternalHostBindInterfaceObject verifies that a host can register an
// interface object through the public surface and call it from script.
func TestExternalHostBindInterfaceObject(t *testing.T) {
	rt, err := script.NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.BindInterfaceObject("host", "Greeter", schema.InterfaceDesc{
		Methods: []schema.MethodDesc{{
			Name:       "greet",
			Parameters: []schema.ParameterDesc{{Name: "name", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}}},
			Returns:    []schema.TypeDesc{{Kind: schema.TypeKindScalar, Name: "string"}},
		}},
	}, &externalTestHostGreeter{}); err != nil {
		t.Fatalf("BindInterfaceObject: %v", err)
	}
	if err := rt.LoadSource("demo", `import Greeter from "host"
export fun speak(name: string): string { return Greeter.greet(name) }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	result, err := rt.Call("speak", "bob")
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var spoken string
	if err := result.DecodeInto(&spoken); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if spoken != "hello bob" {
		t.Fatalf("got %q, want %q", spoken, "hello bob")
	}
}

// TestExternalHostBindTypeAlias verifies that a host can register a type alias
// through the public surface and introspect it via BoundTypeAliases.
func TestExternalHostBindTypeAlias(t *testing.T) {
	rt, err := script.NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}

	if err := rt.BindTypeAlias("types", "UserId", schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}); err != nil {
		t.Fatalf("BindTypeAlias: %v", err)
	}

	aliases := rt.BoundTypeAliases()
	if len(aliases) != 1 {
		t.Fatalf("expected 1 bound alias, got %d", len(aliases))
	}
	if aliases[0].Namespace != "types" || aliases[0].Name != "UserId" {
		t.Fatalf("unexpected bound alias: %+v", aliases[0])
	}
}

// TestExternalHostBindStructDescComposesWithBindFunc verifies that
// BindStructDesc and BindFunc can be used on the same namespace before
// LoadSource.
func TestExternalHostBindStructDescComposesWithBindFunc(t *testing.T) {
	rt, err := script.NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}

	if err := rt.BindStructDesc("geom", "Point", schema.ObjectDesc{
		Kind:   schema.TypeKindStruct,
		Fields: []schema.FieldDesc{{Name: "X", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}}},
	}); err != nil {
		t.Fatalf("BindStructDesc: %v", err)
	}
	if err := rt.BindFunc("geom", "origin", func() int { return 0 }); err != nil {
		t.Fatalf("BindFunc: %v", err)
	}

	if len(rt.BoundObjects()) != 1 {
		t.Fatalf("expected 1 object, got %d", len(rt.BoundObjects()))
	}
	if len(rt.BoundFunctions()) != 1 {
		t.Fatalf("expected 1 function, got %d", len(rt.BoundFunctions()))
	}
}

// TestExternalHostBindStructComposesWithBindFunc verifies that BindStruct and
// BindFunc can be used on the same namespace before LoadSource.
func TestExternalHostBindStructComposesWithBindFunc(t *testing.T) {
	rt, err := script.NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}

	type Point struct{ X, Y int }
	if err := rt.BindStruct("geom", "Point", Point{}); err != nil {
		t.Fatalf("BindStruct: %v", err)
	}
	if err := rt.BindFunc("geom", "origin", func() int { return 0 }); err != nil {
		t.Fatalf("BindFunc: %v", err)
	}

	if len(rt.BoundObjects()) != 1 {
		t.Fatalf("expected 1 object, got %d", len(rt.BoundObjects()))
	}
	if len(rt.BoundFunctions()) != 1 {
		t.Fatalf("expected 1 function, got %d", len(rt.BoundFunctions()))
	}
}

// TestExternalHostBindObjectDoesNotRegisterStructTypes verifies the documented
// contract: BindObject registers functions and values, not struct/object types.
func TestExternalHostBindObjectDoesNotRegisterStructTypes(t *testing.T) {
	rt, err := script.NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}

	if err := rt.BindObject("env", map[string]any{
		"AppName": "spore",
		"echo":    func(s string) string { return s },
	}); err != nil {
		t.Fatalf("BindObject: %v", err)
	}

	if len(rt.BoundFunctions()) != 1 {
		t.Fatalf("expected 1 function, got %d", len(rt.BoundFunctions()))
	}
	if len(rt.BoundValues()) != 1 {
		t.Fatalf("expected 1 value, got %d", len(rt.BoundValues()))
	}
	if len(rt.BoundObjects()) != 0 {
		t.Fatalf("expected 0 objects from BindObject, got %d", len(rt.BoundObjects()))
	}
}

// ---------------------------------------------------------------------------
// Host bridge stream: Recv pattern
//
// The host wraps an external stream source (Go channel, script stream fun,
// network connection, …) behind a Recv() function that returns a StreamChunk.
// The script consumes the stream in a while-loop, checking ok/err fields.
// ---------------------------------------------------------------------------

// StreamChunk is the universal bridge contract: Data carries the payload
// (typed any so any Go type can flow through), Ok signals "more data", and
// Err carries an error message when the stream fails.
type StreamChunk struct {
	Data any    `json:"data"`
	Ok   bool   `json:"ok"`
	Err  string `json:"err"`
}

func TestHostBridgeStreamRecvStringYield(t *testing.T) {
	items := []StreamChunk{
		{Data: "hello", Ok: true},
		{Data: " ", Ok: true},
		{Data: "world", Ok: true},
		{Ok: false},
	}
	idx := 0

	rt, err := script.NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.BindStruct("stream", "StreamChunk", StreamChunk{}); err != nil {
		t.Fatalf("BindStruct Chunk: %v", err)
	}
	if err := rt.BindFunc("stream", "recv", func() StreamChunk {
		if idx >= len(items) {
			return StreamChunk{Err: "exhausted"}
		}
		c := items[idx]
		idx++
		return c
	}); err != nil {
		t.Fatalf("BindFunc recv: %v", err)
	}
	if err := rt.BindFunc("stream", "cat", func(a, b string) string { return a + b }); err != nil {
		t.Fatalf("BindFunc cat: %v", err)
	}
	if err := rt.LoadSource("app", `
		import StreamChunk from "stream"
		import { recv, cat } from "stream"

		export fun consume(): string {
			var result: string = ""
			while true {
				var c: StreamChunk = recv()
				if c.err != "" { return "ERR:" + c.err }
				if !c.ok { break }
				result = cat(result, c.data)
			}
			return result
		}
	`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	result, err := rt.Call("consume")
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !result.Ok() {
		t.Fatalf("result error: %v", result.Error)
	}
	var s string
	if err := result.DecodeInto(&s); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if s != "hello world" {
		t.Fatalf("got %q, want %q", s, "hello world")
	}
}

func TestHostBridgeStreamRecvIntYield(t *testing.T) {
	items := []StreamChunk{
		{Data: 10, Ok: true},
		{Data: 20, Ok: true},
		{Data: 30, Ok: true},
		{Ok: false},
	}
	idx := 0

	rt, err := script.NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.BindStruct("stream", "StreamChunk", StreamChunk{}); err != nil {
		t.Fatalf("BindStruct Chunk: %v", err)
	}
	if err := rt.BindFunc("stream", "recv", func() StreamChunk {
		if idx >= len(items) {
			return StreamChunk{Err: "exhausted"}
		}
		c := items[idx]
		idx++
		return c
	}); err != nil {
		t.Fatalf("BindFunc recv: %v", err)
	}
	if err := rt.BindFunc("stream", "add", func(a, b int) int { return a + b }); err != nil {
		t.Fatalf("BindFunc add: %v", err)
	}
	if err := rt.LoadSource("app", `
		import StreamChunk from "stream"
		import { recv, add } from "stream"

		export fun sum(): int {
			var total: int = 0
			while true {
				var c: StreamChunk = recv()
				if c.err != "" { return -1 }
				if !c.ok { break }
				total = add(total, c.data)
			}
			return total
		}
	`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	result, err := rt.Call("sum")
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var n int
	if err := result.DecodeInto(&n); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if n != 60 {
		t.Fatalf("got %d, want 60", n)
	}
}

func TestAsCastOnAnyScalarString(t *testing.T) {
	items := []StreamChunk{
		{Data: "hello", Ok: true},
		{Data: "world", Ok: true},
		{Ok: false},
	}
	idx := 0

	rt, err := script.NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.BindStruct("stream", "StreamChunk", StreamChunk{}); err != nil {
		t.Fatalf("BindStruct: %v", err)
	}
	if err := rt.BindFunc("stream", "recv", func() StreamChunk {
		if idx >= len(items) {
			return StreamChunk{Ok: false}
		}
		c := items[idx]
		idx++
		return c
	}); err != nil {
		t.Fatalf("BindFunc: %v", err)
	}
	if err := rt.LoadSource("app", `
		import { recv } from "stream"

		export fun run(): string {
			var result: string = ""
			while true {
				var c: StreamChunk = recv()
				if !c.ok { break }
				var s: string = c.data as string
				result = result + s
			}
			return result
		}
	`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	result, err := rt.Call("run")
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var s string
	if err := result.DecodeInto(&s); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if s != "helloworld" {
		t.Fatalf("got %q, want %q", s, "helloworld")
	}
}

func TestAsCastOnAnyScalarInt(t *testing.T) {
	items := []StreamChunk{
		{Data: 10, Ok: true},
		{Data: 20, Ok: true},
		{Ok: false},
	}
	idx := 0

	rt, err := script.NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.BindStruct("stream", "StreamChunk", StreamChunk{}); err != nil {
		t.Fatalf("BindStruct: %v", err)
	}
	if err := rt.BindFunc("stream", "recv", func() StreamChunk {
		if idx >= len(items) {
			return StreamChunk{Ok: false}
		}
		c := items[idx]
		idx++
		return c
	}); err != nil {
		t.Fatalf("BindFunc: %v", err)
	}
	if err := rt.LoadSource("app", `
		import { recv } from "stream"

		export fun run(): int {
			var total: int = 0
			while true {
				var c: StreamChunk = recv()
				if !c.ok { break }
				var n: int = c.data as int
				total = total + n
			}
			return total
		}
	`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	result, err := rt.Call("run")
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var n int
	if err := result.DecodeInto(&n); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if n != 30 {
		t.Fatalf("got %d, want 30", n)
	}
}

func TestAsCastOnAnyScalarBool(t *testing.T) {
	items := []StreamChunk{
		{Data: true, Ok: true},
		{Data: false, Ok: true},
		{Data: true, Ok: true},
		{Ok: false},
	}
	idx := 0

	rt, err := script.NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.BindStruct("stream", "StreamChunk", StreamChunk{}); err != nil {
		t.Fatalf("BindStruct: %v", err)
	}
	if err := rt.BindFunc("stream", "recv", func() StreamChunk {
		if idx >= len(items) {
			return StreamChunk{Ok: false}
		}
		c := items[idx]
		idx++
		return c
	}); err != nil {
		t.Fatalf("BindFunc: %v", err)
	}
	if err := rt.LoadSource("app", `
		import { recv } from "stream"

		export fun run(): int {
			var count: int = 0
			while true {
				var c: StreamChunk = recv()
				if !c.ok { break }
				var b: bool = c.data as bool
				if b { count = count + 1 }
			}
			return count
		}
	`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	result, err := rt.Call("run")
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var n int
	if err := result.DecodeInto(&n); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if n != 2 {
		t.Fatalf("got %d, want 2", n)
	}
}

func TestAsCastOnAnyScalarFail(t *testing.T) {
	items := []StreamChunk{
		{Data: "not_a_int", Ok: true},
		{Ok: false},
	}
	idx := 0

	rt, err := script.NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.BindStruct("stream", "StreamChunk", StreamChunk{}); err != nil {
		t.Fatalf("BindStruct: %v", err)
	}
	if err := rt.BindFunc("stream", "recv", func() StreamChunk {
		if idx >= len(items) {
			return StreamChunk{Ok: false}
		}
		c := items[idx]
		idx++
		return c
	}); err != nil {
		t.Fatalf("BindFunc: %v", err)
	}
	if err := rt.LoadSource("app", `
		import { recv } from "stream"

		export fun run(): int {
			var c: StreamChunk = recv()
			var n: int = c.data as int
			return n
		}
	`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	result, err := rt.Call("run")
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if result.Error == nil {
		t.Fatalf("expected runtime error for casting string to int, got nil")
	}
}

func TestAsCastOnAnyScalarFloat(t *testing.T) {
	items := []StreamChunk{
		{Data: 1.5, Ok: true},
		{Data: 2.5, Ok: true},
		{Ok: false},
	}
	idx := 0

	rt, err := script.NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.BindStruct("stream", "StreamChunk", StreamChunk{}); err != nil {
		t.Fatalf("BindStruct: %v", err)
	}
	if err := rt.BindFunc("stream", "recv", func() StreamChunk {
		if idx >= len(items) {
			return StreamChunk{Ok: false}
		}
		c := items[idx]
		idx++
		return c
	}); err != nil {
		t.Fatalf("BindFunc: %v", err)
	}
	if err := rt.LoadSource("app", `
		import { recv } from "stream"

		export fun run(): double {
			var total: double = 0.0
			while true {
				var c: StreamChunk = recv()
				if !c.ok { break }
				var f: double = c.data as double
				total = total + f
			}
			return total
		}
	`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	result, err := rt.Call("run")
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var f float64
	if err := result.DecodeInto(&f); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if f != 4.0 {
		t.Fatalf("got %f, want 4.0", f)
	}
}

func TestIsCheckOnAnyScalarString(t *testing.T) {
	items := []StreamChunk{
		{Data: "hello", Ok: true},
		{Data: 42, Ok: true},
		{Ok: false},
	}
	idx := 0

	rt, err := script.NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.BindStruct("stream", "StreamChunk", StreamChunk{}); err != nil {
		t.Fatalf("BindStruct: %v", err)
	}
	if err := rt.BindFunc("stream", "recv", func() StreamChunk {
		if idx >= len(items) {
			return StreamChunk{Ok: false}
		}
		c := items[idx]
		idx++
		return c
	}); err != nil {
		t.Fatalf("BindFunc: %v", err)
	}
	if err := rt.LoadSource("app", `
		import { recv } from "stream"

		export fun run(): int {
			var count: int = 0
			while true {
				var c: StreamChunk = recv()
				if !c.ok { break }
				if c.data is string { count = count + 1 }
			}
			return count
		}
	`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	result, err := rt.Call("run")
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var n int
	if err := result.DecodeInto(&n); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if n != 1 {
		t.Fatalf("got %d, want 1", n)
	}
}

func TestIsCheckOnAnyScalarInt(t *testing.T) {
	items := []StreamChunk{
		{Data: "hello", Ok: true},
		{Data: 42, Ok: true},
		{Ok: false},
	}
	idx := 0

	rt, err := script.NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.BindStruct("stream", "StreamChunk", StreamChunk{}); err != nil {
		t.Fatalf("BindStruct: %v", err)
	}
	if err := rt.BindFunc("stream", "recv", func() StreamChunk {
		if idx >= len(items) {
			return StreamChunk{Ok: false}
		}
		c := items[idx]
		idx++
		return c
	}); err != nil {
		t.Fatalf("BindFunc: %v", err)
	}
	if err := rt.LoadSource("app", `
		import { recv } from "stream"

		export fun run(): int {
			var count: int = 0
			while true {
				var c: StreamChunk = recv()
				if !c.ok { break }
				if c.data is int { count = count + 1 }
			}
			return count
		}
	`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	result, err := rt.Call("run")
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var n int
	if err := result.DecodeInto(&n); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if n != 1 {
		t.Fatalf("got %d, want 1", n)
	}
}

func TestIsCheckOnAnyScalarBool(t *testing.T) {
	items := []StreamChunk{
		{Data: "hello", Ok: true},
		{Data: true, Ok: true},
		{Ok: false},
	}
	idx := 0

	rt, err := script.NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.BindStruct("stream", "StreamChunk", StreamChunk{}); err != nil {
		t.Fatalf("BindStruct: %v", err)
	}
	if err := rt.BindFunc("stream", "recv", func() StreamChunk {
		if idx >= len(items) {
			return StreamChunk{Ok: false}
		}
		c := items[idx]
		idx++
		return c
	}); err != nil {
		t.Fatalf("BindFunc: %v", err)
	}
	if err := rt.LoadSource("app", `
		import { recv } from "stream"

		export fun run(): int {
			var count: int = 0
			while true {
				var c: StreamChunk = recv()
				if !c.ok { break }
				if c.data is bool { count = count + 1 }
			}
			return count
		}
	`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	result, err := rt.Call("run")
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var n int
	if err := result.DecodeInto(&n); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if n != 1 {
		t.Fatalf("got %d, want 1", n)
	}
}

func TestHostBridgeStreamRecvStructYield(t *testing.T) {
	type Point struct {
		X int `json:"x"`
		Y int `json:"y"`
	}
	items := []StreamChunk{
		{Data: Point{X: 1, Y: 2}, Ok: true},
		{Data: Point{X: 3, Y: 4}, Ok: true},
		{Ok: false},
	}
	idx := 0

	rt, err := script.NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.BindStruct("stream", "StreamChunk", StreamChunk{}); err != nil {
		t.Fatalf("BindStruct Chunk: %v", err)
	}
	if err := rt.BindFunc("stream", "recv", func() StreamChunk {
		if idx >= len(items) {
			return StreamChunk{Err: "exhausted"}
		}
		c := items[idx]
		idx++
		return c
	}); err != nil {
		t.Fatalf("BindFunc recv: %v", err)
	}
	// Host receives the struct (arrives as map[string]any after pass-by-value bridge)
	// and extracts the distance.
	if err := rt.BindFunc("stream", "distance", func(p map[string]any) int {
		x := p["x"].(int)
		y := p["y"].(int)
		return x*x + y*y
	}); err != nil {
		t.Fatalf("BindFunc distance: %v", err)
	}
	if err := rt.BindFunc("stream", "add_int", func(a, b int) int { return a + b }); err != nil {
		t.Fatalf("BindFunc add_int: %v", err)
	}
	if err := rt.LoadSource("app", `
		import StreamChunk from "stream"
		import { recv, distance, add_int } from "stream"

		export fun sum_dist(): int {
			var total: int = 0
			while true {
				var c: StreamChunk = recv()
				if c.err != "" { return -1 }
				if !c.ok { break }
				total = add_int(total, distance(c.data))
			}
			return total
		}
	`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	result, err := rt.Call("sum_dist")
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var n int
	if err := result.DecodeInto(&n); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	// 1*1+2*2=5, 3*3+4*4=25, total=30
	if n != 30 {
		t.Fatalf("got %d, want 30", n)
	}
}

func TestHostBridgeStreamRecvError(t *testing.T) {
	items := []StreamChunk{
		{Data: "ok", Ok: true},
		{Err: "connection reset", Ok: true},
	}
	idx := 0

	rt, err := script.NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.BindStruct("stream", "StreamChunk", StreamChunk{}); err != nil {
		t.Fatalf("BindStruct Chunk: %v", err)
	}
	if err := rt.BindFunc("stream", "recv", func() StreamChunk {
		if idx >= len(items) {
			return StreamChunk{Ok: false}
		}
		c := items[idx]
		idx++
		return c
	}); err != nil {
		t.Fatalf("BindFunc recv: %v", err)
	}
	if err := rt.LoadSource("app", `
		import StreamChunk from "stream"
		import { recv } from "stream"

		export fun run(): string {
			var result: string = ""
			while true {
				var c: StreamChunk = recv()
				if c.err != "" {
					result = "ERROR:" + c.err
					break
				}
				if !c.ok { break }
				result = result + c.data
			}
			return result
		}
	`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	result, err := rt.Call("run")
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var s string
	if err := result.DecodeInto(&s); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if s != "ERROR:connection reset" {
		t.Fatalf("got %q, want %q", s, "ERROR:connection reset")
	}
}
