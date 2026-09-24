package script

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/qomos-w/spore/internal/script/vm"
	"github.com/qomos-w/spore/schema"
)

var errFakeForTest = errors.New("fake runtime failure")

func TestRuntimeLoadCallAndExports(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.LoadSource("demo", `export fun greet(name: string): string = name`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	exports, err := rt.Exports("demo")
	if err != nil {
		t.Fatalf("Exports: %v", err)
	}
	if len(exports) != 1 || exports[0].Name != "greet" {
		t.Fatalf("unexpected exports: %+v", exports)
	}
	if exports[0].Mode != "unary" {
		t.Fatalf("expected unary mode, got %+v", exports[0])
	}
	if len(exports[0].Parameters) != 1 || exports[0].Parameters[0].Name != "name" {
		t.Fatalf("unexpected parameter surface: %+v", exports[0].Parameters)
	}
	if len(exports[0].Returns) != 1 || exports[0].Returns[0].Name != "string" {
		t.Fatalf("unexpected return surface: %+v", exports[0].Returns)
	}
	info, err := rt.LookupCallable("demo", "greet")
	if err != nil {
		t.Fatalf("LookupCallable: %v", err)
	}
	if info.Name != "greet" {
		t.Fatalf("unexpected callable info: %+v", info)
	}
	result, err := rt.Call("greet", "hello")
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if got, ok := result.Value.(string); !ok || got != "hello" {
		t.Fatalf("result.Value = %#v, want %q", result.Value, "hello")
	}
}

func TestRuntimeLoadSourceReturnsCompileError(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	err = rt.LoadSource("broken", `fun bad(: int) {}`)
	if err == nil {
		t.Fatal("expected compile error")
	}
	if !IsCompileError(err) {
		t.Fatalf("expected compile error, got %T", err)
	}
}

func TestRuntimeCallReturnsStructuredErrorResult(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.LoadSource("demo", `class Alpha { x: int }
class Beta { y: int }
fun fail(): bool { var a: Alpha = new Alpha() return a as Beta }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	result, err := rt.Call("fail")
	if err != nil {
		t.Fatalf("expected structured runtime result, got err=%v", err)
	}
	if result.Error == nil {
		t.Fatal("expected runtime error result")
	}
	if !IsRuntimeError(result.Error) {
		t.Fatalf("expected runtime error result, got %T", result.Error)
	}
	if result.Error.Diagnostic.Code != "type_cast_failed" {
		t.Fatalf("expected diagnostic code type_cast_failed, got %q", result.Error.Diagnostic.Code)
	}
	if result.Value != nil {
		t.Fatalf("expected nil result value for runtime error, got %#v", result.Value)
	}
}

func TestRuntimeBindValue(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.BindValue("env", "Name", "spore"); err != nil {
		t.Fatalf("BindValue: %v", err)
	}
	values := rt.BoundValues()
	if len(values) != 1 {
		t.Fatalf("expected 1 bound value, got %d", len(values))
	}
	if values[0].Namespace != "env" || values[0].Name != "Name" || values[0].Desc.Name != "Name" {
		t.Fatalf("unexpected bound value surface: %+v", values[0])
	}
}

func TestRuntimeBindFuncExposesBoundFunctionSurface(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.BindFunc("tool", "echo", func(text string) string { return text }); err != nil {
		t.Fatalf("BindFunc: %v", err)
	}
	funcs := rt.BoundFunctions()
	if len(funcs) != 1 {
		t.Fatalf("expected 1 bound function, got %d", len(funcs))
	}
	if funcs[0].Namespace != "tool" || funcs[0].Name != "echo" || funcs[0].Callable != "tool.echo" {
		t.Fatalf("unexpected bound function surface: %+v", funcs[0])
	}
	if funcs[0].Info.Name != "echo" {
		t.Fatalf("unexpected bound function info: %+v", funcs[0].Info)
	}
}

func TestRuntimeLongCounterLoopDecodesCountersCorrectly(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.LoadSource("loop", `export fun sum(k: long): long {
		var n: long = k
		var s: long = 0
		for (var i: long = 0; i < n; i = i + 1) { s = s + 1 }
		return s
	}`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	r, err := rt.Call("sum", int64(5))
	if err != nil {
		t.Fatalf("Call sum: %v", err)
	}
	if r.Error != nil {
		t.Fatalf("sum errored: %v", r.Error)
	}
	if got, ok := r.Value.(int64); !ok || got != 5 {
		t.Fatalf("sum(5) = %#v, want int64(5)", r.Value)
	}
}

func TestRuntimeRootModuleEmptyBeforeLoad(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if got := rt.RootModule(); got != "" {
		t.Fatalf("RootModule before load = %q, want empty", got)
	}
	if mods := rt.LoadedModules(); mods != nil {
		t.Fatalf("LoadedModules before load = %v, want nil", mods)
	}
}

func TestRuntimeRootModuleAfterLoad(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.LoadSource("demo", `export fun id(x: int): int = x`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	if got := rt.RootModule(); got != "demo" {
		t.Fatalf("RootModule after load = %q, want %q", got, "demo")
	}
	mods := rt.LoadedModules()
	if len(mods) == 0 || mods[0] != "demo" {
		t.Fatalf("LoadedModules after load = %v, want first entry %q", mods, "demo")
	}
}

func TestRuntimeLoadSourceTwiceReturnsStateError(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.LoadSource("first", `export fun a(): int = 1`); err != nil {
		t.Fatalf("first LoadSource: %v", err)
	}
	err = rt.LoadSource("second", `export fun b(): int = 2`)
	if err == nil {
		t.Fatal("expected state error on second LoadSource")
	}
	if !errors.Is(err, ErrRuntimeAlreadyLoaded) {
		t.Fatalf("expected ErrRuntimeAlreadyLoaded, got %v", err)
	}
	if IsCompileError(err) {
		t.Fatalf("second-load error should not be classified as compile error: %v", err)
	}
	if rt.RootModule() != "first" {
		t.Fatalf("RootModule should still be %q after rejected reload, got %q", "first", rt.RootModule())
	}
}

func TestRuntimeExportsBeforeLoadReturnsError(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if _, err := rt.Exports("anything"); err == nil {
		t.Fatal("expected error from Exports before load")
	}
}

func TestRuntimeCallBeforeLoadReturnsRuntimeError(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	_, err = rt.Call("missing")
	if err == nil {
		t.Fatal("expected runtime error from Call before load")
	}
	if !IsRuntimeError(err) {
		t.Fatalf("expected runtime error, got %T", err)
	}
}

func TestResultDecodeIntoAny(t *testing.T) {
	r := Result{Value: "hello"}
	var got any
	if err := r.DecodeInto(&got); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if got != "hello" {
		t.Fatalf("got %v, want %q", got, "hello")
	}
}

func TestResultDecodeIntoString(t *testing.T) {
	r := Result{Value: "spore"}
	var got string
	if err := r.DecodeInto(&got); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if got != "spore" {
		t.Fatalf("got %q, want %q", got, "spore")
	}
}

func TestResultDecodeIntoBool(t *testing.T) {
	r := Result{Value: true}
	var got bool
	if err := r.DecodeInto(&got); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if !got {
		t.Fatalf("got %v, want true", got)
	}
}

func TestResultDecodeIntoIntegers(t *testing.T) {
	r := Result{Value: int64(42)}
	var i int
	if err := r.DecodeInto(&i); err != nil {
		t.Fatalf("DecodeInto int: %v", err)
	}
	if i != 42 {
		t.Fatalf("int got %d, want 42", i)
	}
	var i32 int32
	if err := r.DecodeInto(&i32); err != nil {
		t.Fatalf("DecodeInto int32: %v", err)
	}
	if i32 != 42 {
		t.Fatalf("int32 got %d, want 42", i32)
	}
	var i64 int64
	if err := r.DecodeInto(&i64); err != nil {
		t.Fatalf("DecodeInto int64: %v", err)
	}
	if i64 != 42 {
		t.Fatalf("int64 got %d, want 42", i64)
	}
	var u uint64
	if err := r.DecodeInto(&u); err != nil {
		t.Fatalf("DecodeInto uint64: %v", err)
	}
	if u != 42 {
		t.Fatalf("uint64 got %d, want 42", u)
	}
}

func TestResultDecodeIntoFloats(t *testing.T) {
	r := Result{Value: 3.5}
	var f64 float64
	if err := r.DecodeInto(&f64); err != nil {
		t.Fatalf("DecodeInto float64: %v", err)
	}
	if f64 != 3.5 {
		t.Fatalf("float64 got %v, want 3.5", f64)
	}
	var f32 float32
	if err := r.DecodeInto(&f32); err != nil {
		t.Fatalf("DecodeInto float32: %v", err)
	}
	if f32 != 3.5 {
		t.Fatalf("float32 got %v, want 3.5", f32)
	}
}

func TestResultDecodeIntoSlice(t *testing.T) {
	src := []any{int64(1), "two", true}
	r := Result{Value: src}
	var got []any
	if err := r.DecodeInto(&got); err != nil {
		t.Fatalf("DecodeInto []any: %v", err)
	}
	if len(got) != 3 || got[0] != int64(1) || got[1] != "two" || got[2] != true {
		t.Fatalf("got %v, want %v", got, src)
	}
}

func TestResultDecodeIntoMap(t *testing.T) {
	src := map[string]any{"name": "spore", "age": int64(2)}
	r := Result{Value: src}
	var got map[string]any
	if err := r.DecodeInto(&got); err != nil {
		t.Fatalf("DecodeInto map[string]any: %v", err)
	}
	if got["name"] != "spore" || got["age"] != int64(2) {
		t.Fatalf("got %v, want %v", got, src)
	}
}

func TestResultDecodeIntoTypeMismatch(t *testing.T) {
	r := Result{Value: int64(1)}
	var s string
	if err := r.DecodeInto(&s); err == nil {
		t.Fatal("expected type mismatch error decoding int64 into *string")
	}
}

func TestResultDecodeIntoUnsupportedTarget(t *testing.T) {
	r := Result{Value: "anything"}
	var ch chan int
	if err := r.DecodeInto(&ch); err == nil {
		t.Fatal("expected error for unsupported target type")
	}
}

func TestResultDecodeIntoNilTarget(t *testing.T) {
	r := Result{Value: "anything"}
	if err := r.DecodeInto(nil); err == nil {
		t.Fatal("expected error decoding into nil target")
	}
}

func TestResultDecodeIntoSurfacesRuntimeError(t *testing.T) {
	r := Result{Error: &RuntimeError{Err: errFakeForTest}}
	var got string
	err := r.DecodeInto(&got)
	if err == nil {
		t.Fatal("expected runtime error to propagate")
	}
	if !IsRuntimeError(err) {
		t.Fatalf("expected runtime error, got %T", err)
	}
}

func TestResultOkAndVoidStates(t *testing.T) {
	cases := []struct {
		name   string
		result Result
		ok     bool
		void   bool
	}{
		{
			name:   "success with value",
			result: Result{Value: "hello"},
			ok:     true,
			void:   false,
		},
		{
			name:   "success without value (void)",
			result: Result{},
			ok:     true,
			void:   true,
		},
		{
			name:   "runtime error",
			result: Result{Error: &RuntimeError{Err: errFakeForTest}},
			ok:     false,
			void:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.result.Ok(); got != tc.ok {
				t.Errorf("Ok() = %v, want %v", got, tc.ok)
			}
			if got := tc.result.Void(); got != tc.void {
				t.Errorf("Void() = %v, want %v", got, tc.void)
			}
		})
	}
}

func TestRuntimeCallDecodeIntoIntEndToEnd(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.LoadSource("demo", `export fun seven(): int = 7`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	result, err := rt.Call("seven")
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var n int
	if err := result.DecodeInto(&n); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if n != 7 {
		t.Fatalf("got %d, want 7", n)
	}
}

func TestRuntimeBindObjectMixedFunctionsAndValues(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	err = rt.BindObject("env", map[string]any{
		"AppName": "spore",
		"Region":  "local",
		"echo":    func(s string) string { return s },
	})
	if err != nil {
		t.Fatalf("BindObject: %v", err)
	}
	funcs := rt.BoundFunctions()
	values := rt.BoundValues()
	if len(funcs) != 1 || funcs[0].Callable != "env.echo" {
		t.Fatalf("expected one bound function env.echo, got %+v", funcs)
	}
	if len(values) != 2 {
		t.Fatalf("expected two bound values, got %+v", values)
	}
	seen := map[string]bool{}
	for _, v := range values {
		seen[v.Name] = true
	}
	if !seen["AppName"] || !seen["Region"] {
		t.Fatalf("expected AppName and Region bound, got %+v", seen)
	}
}

func TestRuntimeBindObjectEmptyMapNoOp(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.BindObject("empty", nil); err != nil {
		t.Fatalf("BindObject(nil): %v", err)
	}
	if err := rt.BindObject("empty", map[string]any{}); err != nil {
		t.Fatalf("BindObject({}): %v", err)
	}
	if len(rt.BoundFunctions()) != 0 || len(rt.BoundValues()) != 0 {
		t.Fatal("expected no bindings after empty BindObject calls")
	}
}

func TestRuntimeBindObjectEmptyNamespaceFails(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.BindObject("", map[string]any{"x": 1}); err == nil {
		t.Fatal("expected error for empty namespace")
	}
}

func TestRuntimeBindObjectAccumulatesAcrossBindCalls(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.BindFunc("env", "echo", func(s string) string { return s }); err != nil {
		t.Fatalf("BindFunc: %v", err)
	}
	if err := rt.BindObject("env", map[string]any{"name": "spore"}); err != nil {
		t.Fatalf("BindObject after BindFunc on same namespace should succeed: %v", err)
	}
	if err := rt.BindValue("env", "region", "local"); err != nil {
		t.Fatalf("BindValue after prior binds should succeed: %v", err)
	}
	funcs := rt.BoundFunctions()
	values := rt.BoundValues()
	if len(funcs) != 1 || funcs[0].Callable != "env.echo" {
		t.Fatalf("expected one callable env.echo, got %+v", funcs)
	}
	if len(values) != 2 {
		t.Fatalf("expected 2 values under env, got %+v", values)
	}
}

func TestRuntimeBindAfterLoadIsSealed(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.LoadSource("demo", `export fun a(): int = 1`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	if err := rt.BindFunc("env", "echo", func(s string) string { return s }); err == nil {
		t.Fatal("expected BindFunc to fail after LoadSource")
	}
	if err := rt.BindValue("env", "name", "spore"); err == nil {
		t.Fatal("expected BindValue to fail after LoadSource")
	}
	if err := rt.BindObject("env", map[string]any{"x": 1}); err == nil {
		t.Fatal("expected BindObject to fail after LoadSource")
	}
}

func TestRuntimePrepareCallableInvokeRoundtrip(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.LoadSource("demo", `export fun twice(n: int): int = n * 2`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	h, err := rt.PrepareCallable("twice")
	if err != nil {
		t.Fatalf("PrepareCallable: %v", err)
	}
	if h.Name() != "twice" {
		t.Fatalf("unexpected handle name: %q", h.Name())
	}
	if h.Info().Name != "twice" {
		t.Fatalf("expected handle.Info().Name == twice, got %+v", h.Info())
	}
	result, err := h.Invoke(21)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	var n int
	if err := result.DecodeInto(&n); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if n != 42 {
		t.Fatalf("got %d, want 42", n)
	}
}

func TestRuntimePrepareCallableMissingCallable(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.LoadSource("demo", `export fun id(x: int): int = x`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	if _, err := rt.PrepareCallable("missing"); err == nil {
		t.Fatal("expected error for missing callable")
	}
}

func TestRuntimePrepareCallableBeforeLoad(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if _, err := rt.PrepareCallable("any"); err == nil {
		t.Fatal("expected error when preparing callable before load")
	}
}

func TestCallableHandleZeroValueInvoke(t *testing.T) {
	var h CallableHandle
	_, err := h.Invoke(nil)
	if err == nil {
		t.Fatal("expected error invoking zero-value handle")
	}
	if !IsRuntimeError(err) {
		t.Fatalf("expected runtime error, got %T", err)
	}
}

func TestCompileErrorUnwrapExposesInnerError(t *testing.T) {
	inner := errors.New("inner compile failure")
	ce := &CompileError{Err: inner}
	if got := ce.Unwrap(); got != inner {
		t.Fatalf("CompileError.Unwrap = %v, want %v", got, inner)
	}
	var nilErr *CompileError
	if got := nilErr.Unwrap(); got != nil {
		t.Fatalf("nil CompileError.Unwrap = %v, want nil", got)
	}
	if !errors.Is(ce, inner) {
		t.Fatal("errors.Is should reach inner sentinel through CompileError")
	}
}

func TestRuntimeErrorUnwrapExposesInnerError(t *testing.T) {
	inner := errors.New("inner runtime failure")
	re := &RuntimeError{Err: inner}
	if got := re.Unwrap(); got != inner {
		t.Fatalf("RuntimeError.Unwrap = %v, want %v", got, inner)
	}
	var nilErr *RuntimeError
	if got := nilErr.Unwrap(); got != nil {
		t.Fatalf("nil RuntimeError.Unwrap = %v, want nil", got)
	}
	if !errors.Is(re, inner) {
		t.Fatal("errors.Is should reach inner sentinel through RuntimeError")
	}
}

func TestRuntimeCallNextAndCallFinalStreaming(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.LoadSource("demo", `export stream fun emit(): int { yield 1 yield 2 return 3 }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	result, err := rt.CallNext("emit")
	if err != nil {
		t.Fatalf("CallNext first: %v", err)
	}
	if !result.Ok() {
		t.Fatalf("unexpected runtime error: %v", result.Error)
	}
	var n int
	if err := result.DecodeInto(&n); err != nil {
		t.Fatalf("DecodeInto first: %v", err)
	}
	if n != 1 {
		t.Fatalf("first CallNext got %d, want 1", n)
	}

	result, err = rt.CallNext("emit")
	if err != nil {
		t.Fatalf("CallNext second: %v", err)
	}
	if !result.Ok() {
		t.Fatalf("unexpected runtime error: %v", result.Error)
	}
	if err := result.DecodeInto(&n); err != nil {
		t.Fatalf("DecodeInto second: %v", err)
	}
	if n != 2 {
		t.Fatalf("second CallNext got %d, want 2", n)
	}

	result, err = rt.CallFinal("emit")
	if err != nil {
		t.Fatalf("CallFinal: %v", err)
	}
	if !result.Ok() {
		t.Fatalf("unexpected runtime error: %v", result.Error)
	}
	if err := result.DecodeInto(&n); err != nil {
		t.Fatalf("DecodeInto final: %v", err)
	}
	if n != 3 {
		t.Fatalf("CallFinal got %d, want 3", n)
	}
}

func TestRuntimeCallNextOnUnaryCallableReturnsError(t *testing.T) {
	rt, err := NewRuntime()
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
	if !IsRuntimeError(err) {
		t.Fatalf("expected runtime error, got %T", err)
	}
	re := err.(*RuntimeError)
	if re.Diagnostic.Code != "invalid_invocation_stage" {
		t.Fatalf("expected diagnostic code invalid_invocation_stage, got %q", re.Diagnostic.Code)
	}
}

func TestRuntimeCallOnStreamingCallableReturnsError(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.LoadSource("demo", `export stream fun emit(): int { yield 1 return 2 }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	_, err = rt.Call("emit")
	if err == nil {
		t.Fatal("expected Call on streaming callable to return error")
	}
	if !IsRuntimeError(err) {
		t.Fatalf("expected runtime error, got %T", err)
	}
	re := err.(*RuntimeError)
	if re.Diagnostic.Code != "invalid_invocation_stage" {
		t.Fatalf("expected diagnostic code invalid_invocation_stage, got %q", re.Diagnostic.Code)
	}
}

func TestRuntimeStreamingCallableExportsShowsStreamingMode(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.LoadSource("demo", `export stream fun emit(): int { yield 1 return 2 }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	exports, err := rt.Exports("demo")
	if err != nil {
		t.Fatalf("Exports: %v", err)
	}
	if len(exports) != 1 || exports[0].Name != "emit" {
		t.Fatalf("unexpected exports: %+v", exports)
	}
	if exports[0].Mode != "streaming" {
		t.Fatalf("expected streaming mode, got %q", exports[0].Mode)
	}
}

func TestRuntimePrepareCallableStreamingNextFinal(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.LoadSource("demo", `export stream fun emit(): int { yield 1 return 2 }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	h, err := rt.PrepareCallable("emit")
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
	if !result.Ok() {
		t.Fatalf("unexpected runtime error: %v", result.Error)
	}
	var n int
	if err := result.DecodeInto(&n); err != nil {
		t.Fatalf("DecodeInto next: %v", err)
	}
	if n != 1 {
		t.Fatalf("Next got %d, want 1", n)
	}

	result, err = h.Final()
	if err != nil {
		t.Fatalf("Final: %v", err)
	}
	if !result.Ok() {
		t.Fatalf("unexpected runtime error: %v", result.Error)
	}
	if err := result.DecodeInto(&n); err != nil {
		t.Fatalf("DecodeInto final: %v", err)
	}
	if n != 2 {
		t.Fatalf("Final got %d, want 2", n)
	}
}

func TestCallableHandleZeroValueNextFinal(t *testing.T) {
	var h CallableHandle
	_, err := h.Next(nil)
	if err == nil {
		t.Fatal("expected error invoking Next on zero-value handle")
	}
	if !IsRuntimeError(err) {
		t.Fatalf("expected runtime error, got %T", err)
	}
	_, err = h.Final(nil)
	if err == nil {
		t.Fatal("expected error invoking Final on zero-value handle")
	}
	if !IsRuntimeError(err) {
		t.Fatalf("expected runtime error, got %T", err)
	}
}

func TestRuntimeBindStructDesc(t *testing.T) {
	rt, err := NewRuntime()
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
	if objs[0].ObjectDesc.Kind != schema.TypeKindStruct {
		t.Fatalf("expected struct kind, got %q", objs[0].ObjectDesc.Kind)
	}
	if len(objs[0].ObjectDesc.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(objs[0].ObjectDesc.Fields))
	}
}

func TestRuntimeBindStructDescRejectsInvalidDescriptor(t *testing.T) {
	tests := []struct {
		name string
		desc schema.ObjectDesc
	}{
		{
			name: "class kind",
			desc: schema.ObjectDesc{Kind: schema.TypeKindClass},
		},
		{
			name: "name mismatch",
			desc: schema.ObjectDesc{Kind: schema.TypeKindStruct, Name: "Other"},
		},
		{
			name: "forbidden methods",
			desc: schema.ObjectDesc{Kind: schema.TypeKindStruct, Methods: []schema.MethodDesc{{Name: "m"}}},
		},
		{
			name: "duplicate field names",
			desc: schema.ObjectDesc{Kind: schema.TypeKindStruct, Fields: []schema.FieldDesc{
				{Name: "X", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}},
				{Name: "X", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}},
			}},
		},
		{
			name: "invalid field type",
			desc: schema.ObjectDesc{Kind: schema.TypeKindStruct, Fields: []schema.FieldDesc{{Name: "X", Type: schema.TypeDesc{Kind: schema.TypeKindInvalid}}}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rt, err := NewRuntime()
			if err != nil {
				t.Fatalf("NewRuntime: %v", err)
			}
			if err := rt.BindStructDesc("geom", "Point", tc.desc); err == nil {
				t.Fatal("expected BindStructDesc to reject invalid descriptor")
			}
		})
	}
}

func TestRuntimeBindStruct(t *testing.T) {
	rt, err := NewRuntime()
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
	if objs[0].ObjectDesc.Kind != schema.TypeKindStruct {
		t.Fatalf("expected struct kind, got %q", objs[0].ObjectDesc.Kind)
	}
	if len(objs[0].ObjectDesc.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(objs[0].ObjectDesc.Fields))
	}
}

func TestRuntimeBindStructNilValue(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.BindStruct("geom", "Point", nil); err == nil {
		t.Fatal("expected error for nil struct value")
	}
}

func TestRuntimeBindStructDescAfterLoadIsSealed(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.LoadSource("demo", `export fun a(): int = 1`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	if err := rt.BindStructDesc("geom", "Point", schema.ObjectDesc{Kind: schema.TypeKindStruct}); err == nil {
		t.Fatal("expected BindStructDesc to fail after LoadSource")
	}
}

func TestRuntimeBindStructAfterLoadIsSealed(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.LoadSource("demo", `export fun a(): int = 1`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	if err := rt.BindStruct("geom", "Point", struct{ X int }{}); err == nil {
		t.Fatal("expected BindStruct to fail after LoadSource")
	}
}

func TestRuntimeBindInterfaceDesc(t *testing.T) {
	rt, err := NewRuntime()
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
	if len(ifaces[0].InterfaceDesc.Methods) != 1 || ifaces[0].InterfaceDesc.Methods[0].Name != "greet" {
		t.Fatalf("unexpected interface descriptor: %+v", ifaces[0].InterfaceDesc)
	}
}

func TestRuntimeBindInterfaceDescRejectsInvalidDescriptor(t *testing.T) {
	tests := []struct {
		name string
		desc schema.InterfaceDesc
	}{
		{
			name: "name mismatch",
			desc: schema.InterfaceDesc{Name: "Other"},
		},
		{
			name: "empty method name",
			desc: schema.InterfaceDesc{Methods: []schema.MethodDesc{{Name: ""}}},
		},
		{
			name: "duplicate method names",
			desc: schema.InterfaceDesc{Methods: []schema.MethodDesc{{Name: "greet"}, {Name: "greet"}}},
		},
		{
			name: "empty parameter name",
			desc: schema.InterfaceDesc{Methods: []schema.MethodDesc{{
				Name:       "greet",
				Parameters: []schema.ParameterDesc{{Name: "", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}}},
			}}},
		},
		{
			name: "invalid parameter type",
			desc: schema.InterfaceDesc{Methods: []schema.MethodDesc{{
				Name:       "greet",
				Parameters: []schema.ParameterDesc{{Name: "name", Type: schema.TypeDesc{Kind: schema.TypeKindInvalid}}},
			}}},
		},
		{
			name: "invalid return type",
			desc: schema.InterfaceDesc{Methods: []schema.MethodDesc{{
				Name:    "greet",
				Returns: []schema.TypeDesc{{Kind: schema.TypeKindInvalid}},
			}}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rt, err := NewRuntime()
			if err != nil {
				t.Fatalf("NewRuntime: %v", err)
			}
			if err := rt.BindInterfaceDesc("contracts", "Greeter", tc.desc); err == nil {
				t.Fatal("expected BindInterfaceDesc to reject invalid descriptor")
			}
		})
	}
}

func TestRuntimeBindInterfaceDescAfterLoadIsSealed(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.LoadSource("demo", `export fun a(): int = 1`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	if err := rt.BindInterfaceDesc("contracts", "Greeter", schema.InterfaceDesc{}); err == nil {
		t.Fatal("expected BindInterfaceDesc to fail after LoadSource")
	}
}

type testHostGreeter struct {
	Prefix string
}

func (g *testHostGreeter) Greet(name string) string { return g.Prefix + name }




func TestRuntimeBindInterfaceObjectExposesMethodOnlyProxy(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	greeter := &testHostGreeter{Prefix: "hi "}
	if err := rt.BindInterfaceObject("host", "Greeter", schema.InterfaceDesc{
		Methods: []schema.MethodDesc{{
			Name:       "greet",
			Parameters: []schema.ParameterDesc{{Name: "name", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}}},
			Returns:    []schema.TypeDesc{{Kind: schema.TypeKindScalar, Name: "string"}},
		}},
	}, greeter); err != nil {
		t.Fatalf("BindInterfaceObject: %v", err)
	}
	if handle, ok := rt.HostInterfaceHandleForValue(greeter); ok || handle != vm.InvalidHandle {
		t.Fatalf("HostInterfaceHandleForValue() before load = %v, %v; want invalid handle + false", handle, ok)
	}
	if err := rt.LoadSource("demo", `import Greeter from "host"
export fun speak(name: string): string { return Greeter.greet(name) }
export fun check(): bool { return Greeter is Greeter }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	handle, handleOK := rt.HostInterfaceHandleForValue(greeter)
	if !handleOK || handle == vm.InvalidHandle {
		t.Fatalf("HostInterfaceHandleForValue() after load = %v, %v; want valid handle", handle, handleOK)
	}
	if back, ok := rt.HostInterfaceObjectForHandle(handle); !ok || back != greeter {
		t.Fatalf("HostInterfaceObjectForHandle() = %v, %v; want original target", back, ok)
	}
	result, err := rt.Call("speak", "alice")
	if err != nil {
		t.Fatalf("Call speak: %v", err)
	}
	var spoken string
	if err := result.DecodeInto(&spoken); err != nil {
		t.Fatalf("DecodeInto speak: %v", err)
	}
	if spoken != "hi alice" {
		t.Fatalf("got %q, want %q", spoken, "hi alice")
	}
	check, err := rt.Call("check")
	if err != nil {
		t.Fatalf("Call check: %v", err)
	}
	var ok bool
	if err := check.DecodeInto(&ok); err != nil {
		t.Fatalf("DecodeInto check: %v", err)
	}
	if !ok {
		t.Fatal("expected host proxy to satisfy interface")
	}
}

func TestRuntimeBindInterfaceObjectDoesNotExposeFields(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	greeter := &testHostGreeter{Prefix: "secret-"}
	if err := rt.BindInterfaceObject("host", "Greeter", schema.InterfaceDesc{
		Methods: []schema.MethodDesc{{
			Name:       "greet",
			Parameters: []schema.ParameterDesc{{Name: "name", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}}},
			Returns:    []schema.TypeDesc{{Kind: schema.TypeKindScalar, Name: "string"}},
		}},
	}, greeter); err != nil {
		t.Fatalf("BindInterfaceObject: %v", err)
	}
	if err := rt.LoadSource("demo", `import Greeter from "host"
export fun greet(name: string): string { return Greeter.greet(name) }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	result, err := rt.Call("greet", "alice")
	if err != nil {
		t.Fatalf("Call greet: %v", err)
	}
	var spoken string
	if err := result.DecodeInto(&spoken); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if spoken != "secret-alice" {
		t.Fatalf("got %q, want %q — proxy should use host target's internal state", spoken, "secret-alice")
	}
}



func TestRuntimeBindInterfaceObjectCoexistsWithOtherBinds(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	greeter := &testHostGreeter{Prefix: "hello "}
	if err := rt.BindInterfaceObject("host", "Greeter", schema.InterfaceDesc{
		Methods: []schema.MethodDesc{{
			Name:       "greet",
			Parameters: []schema.ParameterDesc{{Name: "name", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}}},
			Returns:    []schema.TypeDesc{{Kind: schema.TypeKindScalar, Name: "string"}},
		}},
	}, greeter); err != nil {
		t.Fatalf("BindInterfaceObject: %v", err)
	}
	if err := rt.BindFunc("host", "echo", func(s string) string { return s }); err != nil {
		t.Fatalf("BindFunc: %v", err)
	}
	if err := rt.BindValue("host", "version", 1); err != nil {
		t.Fatalf("BindValue: %v", err)
	}
	if err := rt.LoadSource("demo", `import Greeter from "host"
import { echo } from "host"
import { version } from "host"
export fun greet(name: string): string { return Greeter.greet(name) }
export fun echo_wrap(s: string): string { return echo(s) }
export fun ver(): int { return version }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	r1, err := rt.Call("greet", "world")
	if err != nil {
		t.Fatalf("Call greet: %v", err)
	}
	var s1 string
	if err := r1.DecodeInto(&s1); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if s1 != "hello world" {
		t.Fatalf("greet got %q, want %q", s1, "hello world")
	}
	r2, err := rt.Call("echo_wrap", "hi")
	if err != nil {
		t.Fatalf("Call echo_wrap: %v", err)
	}
	var s2 string
	if err := r2.DecodeInto(&s2); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if s2 != "hi" {
		t.Fatalf("echo_wrap got %q, want %q", s2, "hi")
	}
	r3, err := rt.Call("ver")
	if err != nil {
		t.Fatalf("Call ver: %v", err)
	}
	var v int
	if err := r3.DecodeInto(&v); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if v != 1 {
		t.Fatalf("ver got %d, want 1", v)
	}
}

func TestRuntimeBindInterfaceMethodReturnsBoundObject(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	greeter := &testHostGreeter{Prefix: "chain-"}
	if err := rt.BindInterfaceObject("host", "Greeter", schema.InterfaceDesc{
		Methods: []schema.MethodDesc{{
			Name:       "greet",
			Parameters: []schema.ParameterDesc{{Name: "name", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}}},
			Returns:    []schema.TypeDesc{{Kind: schema.TypeKindScalar, Name: "string"}},
		}},
	}, greeter); err != nil {
		t.Fatalf("BindInterfaceObject: %v", err)
	}
	if err := rt.BindFunc("host", "greet_via_host", func(g any) string {
		h, ok := g.(*testHostGreeter)
		if !ok {
			return "wrong type"
		}
		return h.Greet("from-host")
	}); err != nil {
		t.Fatalf("BindFunc: %v", err)
	}
	if err := rt.LoadSource("demo", `import Greeter from "host"
import { greet_via_host } from "host"
export fun chain(): string { return greet_via_host(Greeter) }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	result, err := rt.Call("chain")
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var s string
	if err := result.DecodeInto(&s); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if s != "chain-from-host" {
		t.Fatalf("got %q, want %q", s, "chain-from-host")
	}
}


func TestRuntimeBindInterfaceArgUnwrap(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	greeter := &testHostGreeter{Prefix: "wrapped-"}
	if err := rt.BindInterfaceObject("host", "Greeter", schema.InterfaceDesc{
		Methods: []schema.MethodDesc{{
			Name:       "greet",
			Parameters: []schema.ParameterDesc{{Name: "name", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}}},
			Returns:    []schema.TypeDesc{{Kind: schema.TypeKindScalar, Name: "string"}},
		}},
	}, greeter); err != nil {
		t.Fatalf("BindInterfaceObject: %v", err)
	}
	if err := rt.BindFunc("host", "use_greeter", func(g any) string {
		h, ok := g.(*testHostGreeter)
		if !ok {
			return "not a greeter"
		}
		return h.Greet("via-func")
	}); err != nil {
		t.Fatalf("BindFunc: %v", err)
	}
	if err := rt.LoadSource("demo", `import Greeter from "host"
import { use_greeter } from "host"
export fun call_func(): string { return use_greeter(Greeter) }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	result, err := rt.Call("call_func")
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var s string
	if err := result.DecodeInto(&s); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if s != "wrapped-via-func" {
		t.Fatalf("got %q, want %q", s, "wrapped-via-func")
	}
}

func TestRuntimeBindTypeAlias(t *testing.T) {
	rt, err := NewRuntime()
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
	if aliases[0].TypeDesc.Kind != schema.TypeKindScalar || aliases[0].TypeDesc.Name != "string" {
		t.Fatalf("unexpected type desc: %+v", aliases[0].TypeDesc)
	}
}

func TestRuntimeBindTypeAliasAfterLoadIsSealed(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.LoadSource("demo", `export fun a(): int = 1`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	if err := rt.BindTypeAlias("types", "UserId", schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}); err == nil {
		t.Fatal("expected BindTypeAlias to fail after LoadSource")
	}
}

func TestRuntimeBindStructDescAndFuncComposeOnSameNamespace(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.BindStructDesc("geom", "Point", schema.ObjectDesc{
		Kind: schema.TypeKindStruct,
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

func TestRuntimeBindStructAndFuncComposeOnSameNamespace(t *testing.T) {
	rt, err := NewRuntime()
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

func TestRuntimeBindStructRefreshEvictsOldEntries(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.BindStruct("geom", "Point", struct{ X int }{}); err != nil {
		t.Fatalf("BindStruct Point: %v", err)
	}
	if err := rt.BindStruct("geom", "Line", struct{ A, B int }{}); err != nil {
		t.Fatalf("BindStruct Line: %v", err)
	}
	objs := rt.BoundObjects()
	if len(objs) != 2 {
		t.Fatalf("expected 2 objects, got %d", len(objs))
	}
	// BindFunc on same namespace should not remove existing objects
	if err := rt.BindFunc("geom", "origin", func() int { return 0 }); err != nil {
		t.Fatalf("BindFunc: %v", err)
	}
	objs = rt.BoundObjects()
	if len(objs) != 2 {
		t.Fatalf("expected 2 objects after BindFunc, got %d", len(objs))
	}
}

func TestRuntimeBindStructDescAcceptsHostStructAsCallArgument(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	type Point struct {
		X int `json:"x"`
		Y int `json:"y"`
	}
	if err := rt.BindStructDesc("geom", "Point", schema.ObjectDesc{
		Kind: schema.TypeKindStruct,
		Fields: []schema.FieldDesc{
			{Name: "x", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}},
			{Name: "y", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}},
		},
	}); err != nil {
		t.Fatalf("BindStructDesc: %v", err)
	}
	if err := rt.LoadSource("demo", `import Point from "geom"
export fun sum(p: Point): int { return p.x + p.y }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	result, err := rt.Call("sum", Point{X: 3, Y: 4})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !result.Ok() {
		t.Fatalf("unexpected runtime error: %v", result.Error)
	}
	var n int
	if err := result.DecodeInto(&n); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if n != 7 {
		t.Fatalf("expected sum 7, got %d", n)
	}
}

func TestRuntimeBindStructAcceptsHostStructAsCallArgument(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	type Wallet struct {
		Balance int64 `json:"balance"`
	}
	if err := rt.BindStruct("self", "Wallet", Wallet{}); err != nil {
		t.Fatalf("BindStruct: %v", err)
	}
	if err := rt.LoadSource("demo", `import Wallet from "self"
export fun read(w: Wallet): long { return w.balance }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	result, err := rt.Call("read", Wallet{Balance: 42})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !result.Ok() {
		t.Fatalf("unexpected runtime error: %v", result.Error)
	}
	var n int64
	if err := result.DecodeInto(&n); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if n != 42 {
		t.Fatalf("expected balance 42, got %d", n)
	}
}

func TestRuntimeBindStructStillRejectsUnregisteredGoStructAsCallArgument(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.BindFunc("self", "noop", func() int { return 0 }); err != nil {
		t.Fatalf("BindFunc: %v", err)
	}
	if err := rt.LoadSource("demo", `import noop from "self"
export fun call(): int { return noop() }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	type Stranger struct {
		Foo int
	}
	result, err := rt.Call("call", Stranger{Foo: 1})
	// The runtime layer surfaces this as either a host err or a Result.Error;
	// we accept either path as long as the failure carries a runtime category.
	if err == nil && result.Ok() {
		t.Fatalf("expected unsupported_vm_argument_type for unregistered struct, got success")
	}
}

func TestRuntimeBindStructSurvivesResetForCallArguments(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	type Wallet struct {
		Balance int64 `json:"balance"`
	}
	if err := rt.BindStruct("self", "Wallet", Wallet{}); err != nil {
		t.Fatalf("BindStruct: %v", err)
	}
	const src = `import Wallet from "self"
export fun read(w: Wallet): long { return w.balance }`
	if err := rt.LoadSource("demo", src); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	if _, err := rt.Call("read", Wallet{Balance: 1}); err != nil {
		t.Fatalf("first Call: %v", err)
	}
	if err := rt.Reload("demo", src); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	result, err := rt.Call("read", Wallet{Balance: 99})
	if err != nil {
		t.Fatalf("post-reset Call: %v", err)
	}
	if !result.Ok() {
		t.Fatalf("unexpected runtime error after reset: %v", result.Error)
	}
	var n int64
	if err := result.DecodeInto(&n); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if n != 99 {
		t.Fatalf("expected balance 99 after reset, got %d", n)
	}
}

func TestRuntimeNativeStructImportEndToEnd(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	type Point struct {
		X int `json:"x"`
		Y int `json:"y"`
	}
	if err := rt.BindStruct("geom", "Point", Point{}); err != nil {
		t.Fatalf("BindStruct: %v", err)
	}

	// Script constructs native struct and accesses its fields.
	err = rt.LoadSource("demo", `import Point from "geom"
export fun dist(a: int, b: int): int {
	var p: Point = Point{x: a, y: b}
	return p.x + p.y
}`)
	if err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	result, err := rt.Call("dist", 3, 4)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !result.Ok() {
		t.Fatalf("unexpected runtime error: %v", result.Error)
	}
	var n int
	if err := result.DecodeInto(&n); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if n != 7 {
		t.Fatalf("got %d, want 7", n)
	}
}

func TestRuntimeNativeTypeAliasImportEndToEnd(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.BindTypeAlias("types", "UserId", schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}); err != nil {
		t.Fatalf("BindTypeAlias: %v", err)
	}
	if err := rt.BindFunc("types", "getName", func() string { return "alice" }); err != nil {
		t.Fatalf("BindFunc: %v", err)
	}

	err = rt.LoadSource("demo", `import UserId from "types"
import getName from "types"
export fun greet(): string {
	var id: UserId = getName()
	return "hello " + id
}`)
	if err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	result, err := rt.Call("greet")
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !result.Ok() {
		t.Fatalf("unexpected runtime error: %v", result.Error)
	}
	var s string
	if err := result.DecodeInto(&s); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if s != "hello alice" {
		t.Fatalf("got %q, want %q", s, "hello alice")
	}
}

func TestRuntimeNativeStructAllScalarFields(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	type AllScalars struct {
		I  int     `json:"i"`
		I64 int64  `json:"i64"`
		F  float64 `json:"f"`
		B  bool    `json:"b"`
		S  string  `json:"s"`
	}
	if err := rt.BindStruct("types", "AllScalars", AllScalars{}); err != nil {
		t.Fatalf("BindStruct: %v", err)
	}

	// Script constructs native struct directly and accesses each scalar field.
	err = rt.LoadSource("demo", `import AllScalars from "types"
export fun test(): string {
	var a: AllScalars = AllScalars{i: 1, i64: 2, f: 3.5, b: true, s: "ok"}
	if a.i != 1 { return "bad i" }
	if a.i64 != 2 { return "bad i64" }
	if a.f != 3.5 { return "bad f" }
	if a.b != true { return "bad b" }
	return a.s
}`)
	if err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	result, err := rt.Call("test")
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !result.Ok() {
		t.Fatalf("unexpected runtime error: %v", result.Error)
	}
	var s string
	if err := result.DecodeInto(&s); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if s != "ok" {
		t.Fatalf("got %q, want %q", s, "ok")
	}
}

func TestRuntimeNativeStructSliceField(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	type WithSlice struct {
		Items []int `json:"items"`
	}
	if err := rt.BindStruct("types", "WithSlice", WithSlice{}); err != nil {
		t.Fatalf("BindStruct: %v", err)
	}

	// Script constructs native struct with slice field and indexes it.
	err = rt.LoadSource("demo", `import WithSlice from "types"
export fun test(): int {
	var s: WithSlice = WithSlice{items: [10, 20, 30]}
	return s.items[0]
}`)
	if err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	result, err := rt.Call("test")
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !result.Ok() {
		t.Fatalf("unexpected runtime error: %v", result.Error)
	}
	var n int
	if err := result.DecodeInto(&n); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if n != 10 {
		t.Fatalf("got %d, want 10", n)
	}
}

func TestRuntimeNativeStructMapField(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	type WithMap struct {
		Attrs map[string]any `json:"attrs"`
	}
	if err := rt.BindStruct("types", "WithMap", WithMap{}); err != nil {
		t.Fatalf("BindStruct: %v", err)
	}

	// Script constructs native struct with map field and indexes it.
	err = rt.LoadSource("demo", `import WithMap from "types"
export fun test(): string {
	var m: WithMap = WithMap{attrs: {"name": "spore"}}
	return m.attrs["name"]
}`)
	if err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	result, err := rt.Call("test")
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !result.Ok() {
		t.Fatalf("unexpected runtime error: %v", result.Error)
	}
	var s string
	if err := result.DecodeInto(&s); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if s != "spore" {
		t.Fatalf("got %q, want %q", s, "spore")
	}
}

func TestRuntimeNativeStructNestedStructField(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	type Inner struct {
		V int `json:"v"`
	}
	type Outer struct {
		Inner Inner `json:"inner"`
	}
	if err := rt.BindStruct("types", "Inner", Inner{}); err != nil {
		t.Fatalf("BindStruct Inner: %v", err)
	}
	if err := rt.BindStruct("types", "Outer", Outer{}); err != nil {
		t.Fatalf("BindStruct Outer: %v", err)
	}

	// Script constructs nested native structs and accesses deep field.
	err = rt.LoadSource("demo", `import Inner from "types"
import Outer from "types"
export fun test(): int {
	var o: Outer = Outer{inner: Inner{v: 42}}
	return o.inner.v
}`)
	if err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	result, err := rt.Call("test")
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !result.Ok() {
		t.Fatalf("unexpected runtime error: %v", result.Error)
	}
	var n int
	if err := result.DecodeInto(&n); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if n != 42 {
		t.Fatalf("got %d, want 42", n)
	}
}

func TestRuntimeNativeStructFieldAccessAndMutation(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	type Counter struct {
		Value int `json:"value"`
	}
	if err := rt.BindStruct("types", "Counter", Counter{}); err != nil {
		t.Fatalf("BindStruct: %v", err)
	}

	// Script constructs native struct, mutates field, and returns mutated value.
	err = rt.LoadSource("demo", `import Counter from "types"
export fun test(): int {
	var c: Counter = Counter{value: 5}
	c.value = c.value + 3
	return c.value
}`)
	if err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	result, err := rt.Call("test")
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !result.Ok() {
		t.Fatalf("unexpected runtime error: %v", result.Error)
	}
	var n int
	if err := result.DecodeInto(&n); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if n != 8 {
		t.Fatalf("got %d, want 8", n)
	}
}

func TestRuntimeNativeStructLiteralWithVariableFields(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	type Rect struct {
		W int `json:"w"`
		H int `json:"h"`
	}
	if err := rt.BindStruct("geom", "Rect", Rect{}); err != nil {
		t.Fatalf("BindStruct: %v", err)
	}

	// Script constructs native struct with variable fields and computes area inline.
	err = rt.LoadSource("demo", `import Rect from "geom"
export fun test(w: int, h: int): int {
	var r: Rect = Rect{w: w, h: h}
	return r.w * r.h
}`)
	if err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	result, err := rt.Call("test", 4, 5)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !result.Ok() {
		t.Fatalf("unexpected runtime error: %v", result.Error)
	}
	var n int
	if err := result.DecodeInto(&n); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if n != 20 {
		t.Fatalf("got %d, want 20", n)
	}
}

func TestRuntimeNativeStructPassedToHostFunction(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	type Point struct {
		X int `json:"x"`
		Y int `json:"y"`
	}
	if err := rt.BindStruct("geom", "Point", Point{}); err != nil {
		t.Fatalf("BindStruct: %v", err)
	}
	if err := rt.BindFunc("geom", "dist", func(p Point) int {
		return p.X + p.Y
	}); err != nil {
		t.Fatalf("BindFunc: %v", err)
	}

	err = rt.LoadSource("demo", `import Point from "geom"
import dist from "geom"
export fun test(a: int, b: int): int {
	var p: Point = Point{x: a, y: b}
	return dist(p)
}`)
	if err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	result, err := rt.Call("test", 3, 4)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !result.Ok() {
		t.Fatalf("unexpected runtime error: %v", result.Error)
	}
	var n int
	if err := result.DecodeInto(&n); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if n != 7 {
		t.Fatalf("got %d, want 7", n)
	}
}

func TestRuntimeNativeStructReturnedFromHostFunction(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	type Point struct {
		X int `json:"x"`
		Y int `json:"y"`
	}
	if err := rt.BindStruct("geom", "Point", Point{}); err != nil {
		t.Fatalf("BindStruct: %v", err)
	}
	if err := rt.BindFunc("geom", "makePoint", func(x, y int) Point {
		return Point{X: x, Y: y}
	}); err != nil {
		t.Fatalf("BindFunc: %v", err)
	}

	err = rt.LoadSource("demo", `import Point from "geom"
import makePoint from "geom"
export fun test(a: int, b: int): int {
	var p: Point = makePoint(a, b)
	return p.x + p.y
}`)
	if err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	result, err := rt.Call("test", 3, 4)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !result.Ok() {
		t.Fatalf("unexpected runtime error: %v", result.Error)
	}
	var n int
	if err := result.DecodeInto(&n); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if n != 7 {
		t.Fatalf("got %d, want 7", n)
	}
}

func TestRuntimeStreamCancellation(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.LoadSource("demo", `export stream fun emit(): int { yield 1 yield 2 return 3 }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	result, err := rt.CallNext("emit")
	if err != nil {
		t.Fatalf("CallNext first: %v", err)
	}
	if !result.Ok() {
		t.Fatalf("unexpected runtime error: %v", result.Error)
	}
	var n int
	if err := result.DecodeInto(&n); err != nil {
		t.Fatalf("DecodeInto first: %v", err)
	}
	if n != 1 {
		t.Fatalf("first CallNext got %d, want 1", n)
	}

	rt.CancelCallable("emit")

	result, err = rt.CallNext("emit")
	if err != nil {
		t.Fatalf("CallNext after cancel should not return host err, got: %v", err)
	}
	if result.Ok() {
		t.Fatal("expected CallNext after cancel to return !Ok")
	}
	if result.Error == nil {
		t.Fatal("expected runtime error result after cancel")
	}
	if !IsRuntimeError(result.Error) {
		t.Fatalf("expected runtime error, got %T", result.Error)
	}
	if result.Error.Diagnostic.Code != "stream_cancelled" {
		t.Fatalf("expected diagnostic code stream_cancelled, got %q", result.Error.Diagnostic.Code)
	}
}

func TestRuntimeStreamCancellationFinal(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.LoadSource("demo", `export stream fun emit(): int { yield 1 yield 2 return 3 }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	result, err := rt.CallNext("emit")
	if err != nil {
		t.Fatalf("CallNext first: %v", err)
	}
	if !result.Ok() {
		t.Fatalf("unexpected runtime error: %v", result.Error)
	}

	rt.CancelCallable("emit")

	result, err = rt.CallFinal("emit")
	if err != nil {
		t.Fatalf("CallFinal after cancel should not return host err, got: %v", err)
	}
	if result.Ok() {
		t.Fatal("expected CallFinal after cancel to return !Ok")
	}
	if result.Error == nil {
		t.Fatal("expected runtime error result after cancel")
	}
	if result.Error.Diagnostic.Code != "stream_cancelled" {
		t.Fatalf("expected diagnostic code stream_cancelled, got %q", result.Error.Diagnostic.Code)
	}
}

func TestRuntimeCancelAllStreams(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.LoadSource("demo", `
		export stream fun a(): int { yield 1 return 2 }
		export stream fun b(): int { yield 3 return 4 }
	`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	_, err = rt.CallNext("a")
	if err != nil {
		t.Fatalf("CallNext a: %v", err)
	}
	_, err = rt.CallNext("b")
	if err != nil {
		t.Fatalf("CallNext b: %v", err)
	}

	rt.CancelAllStreams()

	result, _ := rt.CallNext("a")
	if result.Ok() || result.Error == nil || result.Error.Diagnostic.Code != "stream_cancelled" {
		t.Fatalf("expected stream_cancelled for a, got %+v", result)
	}
	result, _ = rt.CallNext("b")
	if result.Ok() || result.Error == nil || result.Error.Diagnostic.Code != "stream_cancelled" {
		t.Fatalf("expected stream_cancelled for b, got %+v", result)
	}
}

func TestRuntimeStreamCancelledSessionIsCleaned(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.LoadSource("demo", `export stream fun emit(): int { yield 1 yield 2 return 3 }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	result, err := rt.CallNext("emit")
	if err != nil {
		t.Fatalf("CallNext first: %v", err)
	}
	if !result.Ok() {
		t.Fatalf("unexpected runtime error: %v", result.Error)
	}

	rt.CancelCallable("emit")

	result, _ = rt.CallNext("emit")
	if result.Ok() || result.Error.Diagnostic.Code != "stream_cancelled" {
		t.Fatalf("expected stream_cancelled, got %+v", result)
	}

	result, err = rt.CallNext("emit")
	if err != nil {
		t.Fatalf("fresh CallNext after cancel cleanup: %v", err)
	}
	if !result.Ok() {
		t.Fatalf("fresh CallNext should start new session, got error: %v", result.Error)
	}
	var n int
	if err := result.DecodeInto(&n); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if n != 1 {
		t.Fatalf("fresh stream got %d, want 1", n)
	}
}

func TestRuntimeCloseReleasesResources(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.BindFunc("env", "echo", func(s string) string { return s }); err != nil {
		t.Fatalf("BindFunc: %v", err)
	}
	if err := rt.LoadSource("demo", `export fun greet(): string { return echo("hello") }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	if err := rt.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	_, err = rt.Call("greet")
	if err == nil {
		t.Fatal("expected error calling after Close")
	}
	if !IsRuntimeError(err) {
		t.Fatalf("expected runtime error, got %T", err)
	}
	if !strings.Contains(err.Error(), "closed") {
		t.Fatalf("expected closed error, got %v", err)
	}
}

func TestRuntimeCloseIsIdempotent(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestRuntimeResetAllowsReloadingNewSource(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.BindFunc("env", "echo", func(s string) string { return s }); err != nil {
		t.Fatalf("BindFunc: %v", err)
	}
	if err := rt.LoadSource("v1", `import { echo } from "env"
	export fun greet(): string { return echo("v1") }`); err != nil {
		t.Fatalf("LoadSource v1: %v", err)
	}

	result, err := rt.Call("greet")
	if err != nil {
		t.Fatalf("Call v1: %v", err)
	}
	var s string
	if err := result.DecodeInto(&s); err != nil {
		t.Fatalf("DecodeInto v1: %v", err)
	}
	if s != "v1" {
		t.Fatalf("got %q, want v1", s)
	}

	// Reset and load new source.
	if err := rt.Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}

	// Binding should be preserved.
	if len(rt.BoundFunctions()) != 1 {
		t.Fatalf("expected 1 bound function after reset, got %d", len(rt.BoundFunctions()))
	}

	if err := rt.LoadSource("v2", `import { echo } from "env"
	export fun greet(): string { return echo("v2") }`); err != nil {
		t.Fatalf("LoadSource v2: %v", err)
	}

	result, err = rt.Call("greet")
	if err != nil {
		t.Fatalf("Call v2: %v", err)
	}
	if err := result.DecodeInto(&s); err != nil {
		t.Fatalf("DecodeInto v2: %v", err)
	}
	if s != "v2" {
		t.Fatalf("got %q, want v2", s)
	}
}

func TestRuntimeResetPreservesBindings(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.BindValue("env", "version", "1.0"); err != nil {
		t.Fatalf("BindValue: %v", err)
	}
	if err := rt.BindStruct("geom", "Point", struct{ X, Y int }{}); err != nil {
		t.Fatalf("BindStruct: %v", err)
	}
	if err := rt.BindTypeAlias("types", "Id", schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}); err != nil {
		t.Fatalf("BindTypeAlias: %v", err)
	}

	if err := rt.Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}

	if len(rt.BoundValues()) != 1 {
		t.Fatalf("expected 1 bound value, got %d", len(rt.BoundValues()))
	}
	if len(rt.BoundObjects()) != 1 {
		t.Fatalf("expected 1 bound object, got %d", len(rt.BoundObjects()))
	}
	if len(rt.BoundTypeAliases()) != 1 {
		t.Fatalf("expected 1 bound alias, got %d", len(rt.BoundTypeAliases()))
	}
}

func TestRuntimeResetClearsStreamSessions(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.LoadSource("demo", `export stream fun emit(): int { yield 1 yield 2 return 3 }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	_, err = rt.CallNext("emit")
	if err != nil {
		t.Fatalf("CallNext: %v", err)
	}

	if err := rt.Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}

	// After reset, the module is gone so CallNext should fail differently.
	_, err = rt.CallNext("emit")
	if err == nil {
		t.Fatal("expected error after reset")
	}
}

func TestRuntimeResetPreservesModuleResolver(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	rt.SetModuleResolver(ModuleResolverFunc(func(path string) (string, error) {
		if path == "math" {
			return `export fun square(x: int): int { return x * x }`, nil
		}
		return "", fmt.Errorf("not found")
	}))

	if err := rt.Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}

	// Module resolver should still work after reset.
	if err := rt.LoadSource("app", `import { square } from "math"
	export fun test(): int { return square(5) }`); err != nil {
		t.Fatalf("LoadSource with resolver after reset: %v", err)
	}

	result, err := rt.Call("test")
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var n int
	if err := result.DecodeInto(&n); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if n != 25 {
		t.Fatalf("got %d, want 25", n)
	}
}

func TestRuntimeClonePreservesBindings(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.BindFunc("env", "echo", func(s string) string { return s }); err != nil {
		t.Fatalf("BindFunc: %v", err)
	}
	if err := rt.BindValue("env", "version", "1.0"); err != nil {
		t.Fatalf("BindValue: %v", err)
	}
	if err := rt.BindStruct("geom", "Point", struct{ X, Y int }{}); err != nil {
		t.Fatalf("BindStruct: %v", err)
	}
	if err := rt.BindTypeAlias("types", "Id", schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}); err != nil {
		t.Fatalf("BindTypeAlias: %v", err)
	}

	cloned, err := rt.Clone()
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}

	// Cloned runtime should have the same bindings but no loaded module.
	if cloned.RootModule() != "" {
		t.Fatalf("cloned RootModule should be empty, got %q", cloned.RootModule())
	}
	if len(cloned.BoundFunctions()) != 1 {
		t.Fatalf("expected 1 bound function, got %d", len(cloned.BoundFunctions()))
	}
	if len(cloned.BoundValues()) != 1 {
		t.Fatalf("expected 1 bound value, got %d", len(cloned.BoundValues()))
	}
	if len(cloned.BoundObjects()) != 1 {
		t.Fatalf("expected 1 bound object, got %d", len(cloned.BoundObjects()))
	}
	if len(cloned.BoundTypeAliases()) != 1 {
		t.Fatalf("expected 1 bound alias, got %d", len(cloned.BoundTypeAliases()))
	}

	// Cloned runtime should be able to load source independently.
	if err := cloned.LoadSource("demo", `import { echo } from "env"
	export fun greet(): string { return echo("hello") }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	result, err := cloned.Call("greet")
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var s string
	if err := result.DecodeInto(&s); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if s != "hello" {
		t.Fatalf("got %q, want hello", s)
	}
}

func TestRuntimeCloneIsIndependent(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.BindFunc("env", "echo", func(s string) string { return s }); err != nil {
		t.Fatalf("BindFunc: %v", err)
	}
	if err := rt.LoadSource("v1", `import { echo } from "env"
	export fun greet(): string { return echo("v1") }`); err != nil {
		t.Fatalf("LoadSource v1: %v", err)
	}

	cloned, err := rt.Clone()
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}

	// Original runtime still works.
	result, err := rt.Call("greet")
	if err != nil {
		t.Fatalf("Call original: %v", err)
	}
	var s string
	if err := result.DecodeInto(&s); err != nil {
		t.Fatalf("DecodeInto original: %v", err)
	}
	if s != "v1" {
		t.Fatalf("original got %q, want v1", s)
	}

	// Cloned runtime can load different source.
	if err := cloned.LoadSource("v2", `import { echo } from "env"
	export fun greet(): string { return echo("v2") }`); err != nil {
		t.Fatalf("LoadSource v2: %v", err)
	}

	result, err = cloned.Call("greet")
	if err != nil {
		t.Fatalf("Call cloned: %v", err)
	}
	if err := result.DecodeInto(&s); err != nil {
		t.Fatalf("DecodeInto cloned: %v", err)
	}
	if s != "v2" {
		t.Fatalf("cloned got %q, want v2", s)
	}

	// Original runtime is unaffected.
	result, err = rt.Call("greet")
	if err != nil {
		t.Fatalf("Call original after clone load: %v", err)
	}
	if err := result.DecodeInto(&s); err != nil {
		t.Fatalf("DecodeInto original after clone load: %v", err)
	}
	if s != "v1" {
		t.Fatalf("original after clone load got %q, want v1", s)
	}
}

func TestRuntimeCloneModuleResolver(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	rt.SetModuleResolver(ModuleResolverFunc(func(path string) (string, error) {
		if path == "math" {
			return `export fun square(x: int): int { return x * x }`, nil
		}
		return "", fmt.Errorf("not found")
	}))

	cloned, err := rt.Clone()
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}

	if err := cloned.LoadSource("app", `import { square } from "math"
	export fun test(): int { return square(5) }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	result, err := cloned.Call("test")
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var n int
	if err := result.DecodeInto(&n); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if n != 25 {
		t.Fatalf("got %d, want 25", n)
	}
}

func TestRuntimeCloneClosedReturnsError(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	rt.Close()
	if _, err := rt.Clone(); err == nil {
		t.Fatal("expected error cloning closed runtime")
	}
}

func TestRuntimeBindTypeAliasName(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.BindTypeAliasName("types", "UserId", "string"); err != nil {
		t.Fatalf("BindTypeAliasName: %v", err)
	}
	aliases := rt.BoundTypeAliases()
	if len(aliases) != 1 {
		t.Fatalf("expected 1 alias, got %d", len(aliases))
	}
	if aliases[0].TypeDesc.Name != "string" {
		t.Fatalf("expected scalar string, got %+v", aliases[0].TypeDesc)
	}
}

func TestResultAsString(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.LoadSource("demo", `export fun greet(): string = "hello"`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	result, err := rt.Call("greet")
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	s, err := result.AsString()
	if err != nil {
		t.Fatalf("AsString: %v", err)
	}
	if s != "hello" {
		t.Fatalf("got %q, want hello", s)
	}
}

func TestResultAsInt(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.LoadSource("demo", `export fun seven(): int = 7`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	result, err := rt.Call("seven")
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	n, err := result.AsInt()
	if err != nil {
		t.Fatalf("AsInt: %v", err)
	}
	if n != 7 {
		t.Fatalf("got %d, want 7", n)
	}
}

func TestResultUnwrap(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.LoadSource("demo", `export fun ok(): int = 42`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	result, _ := rt.Call("ok")
	if err := result.Unwrap(); err != nil {
		t.Fatalf("Unwrap on success: %v", err)
	}

	result, _ = rt.Call("missing")
	if err := result.Unwrap(); err == nil {
		t.Fatal("expected Unwrap to return error for missing callable")
	}
}

func TestRuntimeBindUnified(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	type Config struct {
		Name string `json:"name"`
	}
	if err := rt.Bind("env", map[string]any{
		"echo":   func(s string) string { return s },
		"region": "local",
		"Config": Config{},
	}); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if len(rt.BoundFunctions()) != 1 {
		t.Fatalf("expected 1 function, got %d", len(rt.BoundFunctions()))
	}
	if len(rt.BoundValues()) != 1 {
		t.Fatalf("expected 1 value, got %d", len(rt.BoundValues()))
	}
	if len(rt.BoundObjects()) != 1 {
		t.Fatalf("expected 1 object, got %d", len(rt.BoundObjects()))
	}
}

func TestRuntimeLoadSourceWithDeps(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.LoadSourceWithDeps("app", `import { add } from "math"
	export fun main(): int { return add(1, 2) }`, map[string]string{
		"math": `export fun add(a: int, b: int): int { return a + b }`,
	}); err != nil {
		t.Fatalf("LoadSourceWithDeps: %v", err)
	}
	result, err := rt.Call("main")
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	n, err := result.AsInt()
	if err != nil {
		t.Fatalf("AsInt: %v", err)
	}
	if n != 3 {
		t.Fatalf("got %d, want 3", n)
	}
}

func TestRuntimeBindStructTypeGeneric(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	type Point struct {
		X int `json:"x"`
		Y int `json:"y"`
	}
	if err := BindStructType[Point](rt, "geom", "Point"); err != nil {
		t.Fatalf("BindStructType: %v", err)
	}
	if len(rt.BoundObjects()) != 1 {
		t.Fatalf("expected 1 object, got %d", len(rt.BoundObjects()))
	}
	if rt.BoundObjects()[0].Name != "Point" {
		t.Fatalf("unexpected object: %+v", rt.BoundObjects()[0])
	}
}
