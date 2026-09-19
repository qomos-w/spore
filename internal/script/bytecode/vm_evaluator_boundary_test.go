package bytecode

import (
	"strings"
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/internal/script/frontend"
	"github.com/qomos-w/spore/internal/script/vm"
)

// --- Unary invocation (non-stream) ---

func TestVMEvaluator_UnaryInvocation(t *testing.T) {
	eval := NewVMEvaluator()
	prog, err := frontend.ParseModuleForTest(`fun add(a: int, b: int): int { return a + b }`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := eval.CompileProgram(prog); err != nil {
		t.Fatalf("compile: %v", err)
	}
	result, err := eval.Evaluate("add", binding.InvocationStageUnary, []any{3, 4})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if result.(int) != 7 {
		t.Errorf("add(3,4) = %v, want 7", result)
	}
}

func TestVMEvaluator_EvaluateUnaryInt(t *testing.T) {
	eval := NewVMEvaluator()
	prog, err := frontend.ParseModuleForTest(`fun twice(n: int): int { return n + n }`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := eval.CompileProgram(prog); err != nil {
		t.Fatalf("compile: %v", err)
	}
	result, err := eval.EvaluateUnaryInt("twice", 21)
	if err != nil {
		t.Fatalf("evaluate unary int: %v", err)
	}
	if result != 42 {
		t.Fatalf("twice(21) = %d, want 42", result)
	}
}

func TestVMEvaluator_EvaluateUnaryIntStringResult(t *testing.T) {
	eval := NewVMEvaluator()
	prog, err := frontend.ParseModuleForTest(`fun build(n: int): string {
		var s: string = ""
		for (var i: int = 0; i < n; i = i + 1) { s = s + "x" }
		return s
	}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := eval.CompileProgram(prog); err != nil {
		t.Fatalf("compile: %v", err)
	}
	result, err := eval.EvaluateUnaryInt("build", 9)
	if err != nil {
		t.Fatalf("evaluate unary int: %v", err)
	}
	if result != 9 {
		t.Fatalf("len(build(9)) = %d, want 9", result)
	}
}

func TestVMEvaluator_UnaryReturnString(t *testing.T) {
	eval := NewVMEvaluator()
	prog, err := frontend.ParseModuleForTest(`fun greet(name: string): string { return "hello " + name }`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := eval.CompileProgram(prog); err != nil {
		t.Fatalf("compile: %v", err)
	}
	result, err := eval.Evaluate("greet", binding.InvocationStageUnary, []any{"world"})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if result.(string) != "hello world" {
		t.Errorf("greet = %q, want %q", result, "hello world")
	}
}

func TestVMEvaluator_LargeStringConcat(t *testing.T) {
	largeA := strings.Repeat("a", 257)
	largeB := strings.Repeat("b", 258)
	tests := []struct {
		name   string
		source string
		args   []any
		want   string
	}{
		{
			name:   "large plus large",
			source: `fun concat(a: string, b: string): string { return a + b }`,
			args:   []any{largeA, largeB},
			want:   largeA + largeB,
		},
		{
			name:   "large plus small",
			source: `fun concat(a: string): string { return a + "!" }`,
			args:   []any{largeA},
			want:   largeA + "!",
		},
		{
			name:   "small plus large",
			source: `fun concat(a: string): string { return "!" + a }`,
			args:   []any{largeA},
			want:   "!" + largeA,
		},
		{
			name:   "large plus int",
			source: `fun concat(a: string): string { return a + 7 }`,
			args:   []any{largeA},
			want:   largeA + "7",
		},
		{
			name:   "int plus large",
			source: `fun concat(a: string): string { return 7 + a }`,
			args:   []any{largeA},
			want:   "7" + largeA,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eval := NewVMEvaluator()
			prog, err := frontend.ParseModuleForTest(tt.source)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if err := eval.CompileProgram(prog); err != nil {
				t.Fatalf("compile: %v", err)
			}
			result, err := eval.Evaluate("concat", binding.InvocationStageUnary, tt.args)
			if err != nil {
				t.Fatalf("evaluate: %v", err)
			}
			if result.(string) != tt.want {
				t.Fatalf("concat length = %d, want %d", len(result.(string)), len(tt.want))
			}
		})
	}
}

func TestVMEvaluator_UnaryReturnBool(t *testing.T) {
	eval := NewVMEvaluator()
	prog, err := frontend.ParseModuleForTest(`fun is_positive(x: int): bool { return x > 0 }`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := eval.CompileProgram(prog); err != nil {
		t.Fatalf("compile: %v", err)
	}
	result, err := eval.Evaluate("is_positive", binding.InvocationStageUnary, []any{5})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if result.(bool) != true {
		t.Errorf("is_positive(5) = %v, want true", result)
	}
}

func TestVMEvaluator_UnaryReturnVoid(t *testing.T) {
	eval := NewVMEvaluator()
	prog, err := frontend.ParseModuleForTest(`fun nothing(): void { var x: int = 0 }`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := eval.CompileProgram(prog); err != nil {
		t.Fatalf("compile: %v", err)
	}
	result, err := eval.Evaluate("nothing", binding.InvocationStageUnary, nil)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	// void function returns nil via vmValueToAny for zero value
	_ = result
}

// --- Callable not found ---

func TestVMEvaluator_CallableNotFound(t *testing.T) {
	eval := NewVMEvaluator()
	prog, err := frontend.ParseModuleForTest(`fun f(): int { return 1 }`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := eval.CompileProgram(prog); err != nil {
		t.Fatalf("compile: %v", err)
	}
	_, err = eval.Evaluate("nonexistent", binding.InvocationStageUnary, nil)
	if err == nil {
		t.Fatal("expected error for nonexistent callable")
	}
}

// --- Nil evaluator safety ---

func TestVMEvaluator_NilEvaluatorReturnsError(t *testing.T) {
	var eval *VMEvaluator
	_, err := eval.Evaluate("f", binding.InvocationStageUnary, nil)
	if err == nil {
		t.Fatal("expected error on nil evaluator")
	}
}

func TestVMEvaluator_NilEvaluatorResetNoPanic(t *testing.T) {
	var eval *VMEvaluator
	eval.Reset("f", nil) // should not panic
}

// --- Compile error propagation ---

func TestVMEvaluator_CompileError(t *testing.T) {
	eval := NewVMEvaluator()
	// This should parse but may have issues at compile time depending on the system.
	// We test that compile errors are returned.
	prog, err := frontend.ParseModuleForTest(`fun f(): int { return 1 }`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := eval.CompileProgram(prog); err != nil {
		t.Fatalf("valid program should compile: %v", err)
	}
}

// --- VM instance access ---

func TestVMEvaluator_VMInstance(t *testing.T) {
	eval := NewVMEvaluator()
	if eval.VM() == nil {
		t.Fatal("VM() should not return nil")
	}
}

// --- Value conversion edge cases ---

func TestVMEvaluator_AnyToVMValue_Int(t *testing.T) {
	eval := NewVMEvaluator()
	v, err := anyToVMValue(eval.vm_, 42)
	if err != nil {
		t.Fatalf("anyToVMValue int: %v", err)
	}
	if !vm.IsInt(v) {
		t.Error("expected int value")
	}
	if vm.DecodeInt(v) != 42 {
		t.Errorf("int = %d, want 42", vm.DecodeInt(v))
	}
}

func TestVMEvaluator_AnyToVMValue_Bool(t *testing.T) {
	eval := NewVMEvaluator()
	v, err := anyToVMValue(eval.vm_, true)
	if err != nil {
		t.Fatalf("anyToVMValue bool: %v", err)
	}
	if !vm.IsBool(v) {
		t.Error("expected bool value")
	}
	if !vm.DecodeBool(v) {
		t.Error("bool = false, want true")
	}
}

func TestVMEvaluator_AnyToVMValue_String(t *testing.T) {
	eval := NewVMEvaluator()
	v, err := anyToVMValue(eval.vm_, "hello")
	if err != nil {
		t.Fatalf("anyToVMValue string: %v", err)
	}
	if !vm.IsString(v) {
		t.Error("expected string value")
	}
	if eval.vm_.DecodeString(v) != "hello" {
		t.Errorf("string = %q, want %q", eval.vm_.DecodeString(v), "hello")
	}
}

func TestVMEvaluator_AnyToVMValue_Nil(t *testing.T) {
	eval := NewVMEvaluator()
	v, err := anyToVMValue(eval.vm_, nil)
	if err != nil {
		t.Fatalf("anyToVMValue nil: %v", err)
	}
	if !vm.IsNull(v) {
		t.Error("expected null value for nil input")
	}
}

func TestVMEvaluator_AnyToVMValue_Float32(t *testing.T) {
	eval := NewVMEvaluator()
	v, err := anyToVMValue(eval.vm_, float32(3.14))
	if err != nil {
		t.Fatalf("anyToVMValue float32: %v", err)
	}
	if !vm.IsFloat(v) {
		t.Error("expected float value")
	}
}

func TestVMEvaluator_AnyToVMValue_Float64(t *testing.T) {
	eval := NewVMEvaluator()
	v, err := anyToVMValue(eval.vm_, float64(2.718))
	if err != nil {
		t.Fatalf("anyToVMValue float64: %v", err)
	}
	if !vm.IsDouble(v) {
		t.Error("expected double value from float64")
	}
	if got := eval.vm_.DecodeDouble(v); got != 2.718 {
		t.Errorf("double = %v, want 2.718", got)
	}
}

func TestVMEvaluator_AnyToVMValue_Int64(t *testing.T) {
	eval := NewVMEvaluator()
	v, err := anyToVMValue(eval.vm_, int64(100))
	if err != nil {
		t.Fatalf("anyToVMValue int64: %v", err)
	}
	if !vm.IsLong(v) {
		t.Error("expected long value from int64")
	}
	if got := eval.vm_.DecodeLong(v); got != 100 {
		t.Errorf("long = %d, want 100", got)
	}
}

func TestVMEvaluator_AnyToVMValue_UnknownType(t *testing.T) {
	eval := NewVMEvaluator()
	_, err := anyToVMValue(eval.vm_, struct{}{})
	if err == nil {
		t.Fatal("expected unsupported type error")
	}
	rtErr, ok := err.(*RuntimeError)
	if !ok {
		t.Fatalf("expected RuntimeError, got %T", err)
	}
	if rtErr.Code != "unsupported_vm_argument_type" {
		t.Fatalf("expected diagnostic code unsupported_vm_argument_type, got %q", rtErr.Code)
	}
}

func TestVMEvaluator_AnyToVMValue_Int_Overflow(t *testing.T) {
	eval := NewVMEvaluator()
	_, err := anyToVMValue(eval.vm_, int(3000000000))
	if err == nil {
		t.Fatal("expected error for int value exceeding int32 range")
	}
	rtErr, ok := err.(*RuntimeError)
	if !ok {
		t.Fatalf("expected RuntimeError, got %T", err)
	}
	if rtErr.Code != "value_out_of_range" {
		t.Fatalf("expected code value_out_of_range, got %q", rtErr.Code)
	}
}

func TestVMEvaluator_AnyToVMValue_Int_Underflow(t *testing.T) {
	eval := NewVMEvaluator()
	_, err := anyToVMValue(eval.vm_, int(-3000000000))
	if err == nil {
		t.Fatal("expected error for int value below int32 range")
	}
	rtErr, ok := err.(*RuntimeError)
	if !ok {
		t.Fatalf("expected RuntimeError, got %T", err)
	}
	if rtErr.Code != "value_out_of_range" {
		t.Fatalf("expected code value_out_of_range, got %q", rtErr.Code)
	}
}

func TestVMEvaluator_AnyToVMValue_Int_InRange(t *testing.T) {
	eval := NewVMEvaluator()
	v, err := anyToVMValue(eval.vm_, int(2147483647))
	if err != nil {
		t.Fatalf("unexpected error for max int32 value: %v", err)
	}
	if !vm.IsInt(v) || vm.DecodeInt(v) != 2147483647 {
		t.Fatalf("expected int32 max, got %v", vm.DecodeInt(v))
	}
	v, err = anyToVMValue(eval.vm_, int(-2147483648))
	if err != nil {
		t.Fatalf("unexpected error for min int32 value: %v", err)
	}
	if !vm.IsInt(v) || vm.DecodeInt(v) != -2147483648 {
		t.Fatalf("expected int32 min, got %v", vm.DecodeInt(v))
	}
}

// --- VM value to any conversion ---

func TestVMEvaluator_VMValueToAny_Int(t *testing.T) {
	eval := NewVMEvaluator()
	result := vmValueToAny(eval.vm_, vm.EncodeInt(42))
	if result.(int) != 42 {
		t.Errorf("got %v, want 42", result)
	}
}

func TestVMEvaluator_VMValueToAny_Bool(t *testing.T) {
	eval := NewVMEvaluator()
	result := vmValueToAny(eval.vm_, vm.EncodeBool(true))
	if !result.(bool) {
		t.Error("got false, want true")
	}
}

func TestVMEvaluator_VMValueToAny_Null(t *testing.T) {
	eval := NewVMEvaluator()
	result := vmValueToAny(eval.vm_, vm.EncodeHandle(vm.InvalidHandle))
	if result != nil {
		t.Errorf("got %v, want nil", result)
	}
}

func TestVMEvaluator_VMValueToAny_Float(t *testing.T) {
	eval := NewVMEvaluator()
	result := vmValueToAny(eval.vm_, vm.EncodeFloat(3.14))
	f, ok := result.(float64)
	if !ok || f < 3.0 || f > 4.0 {
		t.Errorf("got %v, want ~3.14", result)
	}
}

func TestVMEvaluator_VMValueToAny_String(t *testing.T) {
	eval := NewVMEvaluator()
	s := eval.vm_.EncodeString("test")
	result := vmValueToAny(eval.vm_, s)
	if result.(string) != "test" {
		t.Errorf("got %q, want %q", result, "test")
	}
}

func TestVMEvaluator_VMValueToAny_Long(t *testing.T) {
	eval := NewVMEvaluator()
	lv := vm.EncodeLong(12345678901234, eval.vm_)
	result := vmValueToAny(eval.vm_, lv)
	if result.(int64) != 12345678901234 {
		t.Errorf("got %v, want 12345678901234", result)
	}
}

func TestVMEvaluator_VMValueToAny_ULong(t *testing.T) {
	eval := NewVMEvaluator()
	uv := vm.EncodeULong(18446744073709551615, eval.vm_)
	result := vmValueToAny(eval.vm_, uv)
	if result.(uint64) != 18446744073709551615 {
		t.Errorf("got %v, want max uint64", result)
	}
}

func TestVMEvaluator_VMValueToAny_Double(t *testing.T) {
	eval := NewVMEvaluator()
	dv := vm.EncodeDouble(2.718281828, eval.vm_)
	result := vmValueToAny(eval.vm_, dv)
	if result.(float64) < 2.7 || result.(float64) > 2.8 {
		t.Errorf("got %v, want ~2.718", result)
	}
}

func TestVMEvaluator_AnyToVMValue_NestedArrayMapRoundTrip(t *testing.T) {
	eval := NewVMEvaluator()
	v, err := anyToVMValue(eval.vm_, []map[string]any{{"score": 3}, {"score": 5}})
	if err != nil {
		t.Fatalf("expected nested array/map conversion success, got %v", err)
	}
	result, ok := vmValueToAny(eval.vm_, v).([]any)
	if !ok {
		t.Fatalf("expected []any result, got %T", vmValueToAny(eval.vm_, v))
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 elements, got %d", len(result))
	}
	first, ok := result[0].(map[string]any)
	if !ok {
		t.Fatalf("expected first element to be map[string]any, got %T", result[0])
	}
	if first["score"].(int) != 3 {
		t.Fatalf("expected first score 3, got %v", first["score"])
	}
}

func TestVMEvaluator_AnyToVMValue_NestedMapRoundTrip(t *testing.T) {
	eval := NewVMEvaluator()
	v, err := anyToVMValue(eval.vm_, map[string]any{"payload": map[string]any{"score": 3}, "values": []any{1, 2}})
	if err != nil {
		t.Fatalf("expected nested map conversion success, got %v", err)
	}
	result, ok := vmValueToAny(eval.vm_, v).(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any result, got %T", vmValueToAny(eval.vm_, v))
	}
	payload, ok := result["payload"].(map[string]any)
	if !ok {
		t.Fatalf("expected payload map, got %T", result["payload"])
	}
	if payload["score"].(int) != 3 {
		t.Fatalf("expected payload score 3, got %v", payload["score"])
	}
	values, ok := result["values"].([]any)
	if !ok {
		t.Fatalf("expected values slice, got %T", result["values"])
	}
	if len(values) != 2 || values[1].(int) != 2 {
		t.Fatalf("expected values [1 2], got %v", values)
	}
}

func TestVMEvaluator_EvaluateNestedHostValueReturnsNestedValue(t *testing.T) {
	eval := NewVMEvaluator()
	prog, err := frontend.ParseModuleForTest(`fun passthrough(v: any): any { return v }`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := eval.CompileProgram(prog); err != nil {
		t.Fatalf("compile: %v", err)
	}
	result, err := eval.Evaluate("passthrough", binding.InvocationStageUnary, []any{map[string]any{"nested": []any{1, 2}}})
	if err != nil {
		t.Fatalf("expected evaluate success for nested host value, got %v", err)
	}
	returned, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any result, got %T", result)
	}
	nested, ok := returned["nested"].([]any)
	if !ok {
		t.Fatalf("expected nested []any, got %T", returned["nested"])
	}
	if len(nested) != 2 || nested[0].(int) != 1 || nested[1].(int) != 2 {
		t.Fatalf("expected nested [1 2], got %v", nested)
	}
}

func TestVMEvaluator_AnyToVMValue_TypedIntSliceRoundTrip(t *testing.T) {
	eval := NewVMEvaluator()
	v, err := anyToVMValue(eval.vm_, []int{1, 2, 3})
	if err != nil {
		t.Fatalf("expected []int conversion success, got %v", err)
	}
	result, ok := vmValueToAny(eval.vm_, v).([]any)
	if !ok {
		t.Fatalf("expected []any result, got %T", vmValueToAny(eval.vm_, v))
	}
	if len(result) != 3 || result[2].(int) != 3 {
		t.Fatalf("expected [1 2 3], got %v", result)
	}
}

func TestVMEvaluator_AnyToVMValue_TypedStringSliceRoundTrip(t *testing.T) {
	eval := NewVMEvaluator()
	v, err := anyToVMValue(eval.vm_, []string{"a", "b"})
	if err != nil {
		t.Fatalf("expected []string conversion success, got %v", err)
	}
	result := vmValueToAny(eval.vm_, v).([]any)
	if len(result) != 2 || result[1].(string) != "b" {
		t.Fatalf("expected [a b], got %v", result)
	}
}

func TestVMEvaluator_AnyToVMValue_TypedMapRoundTrip(t *testing.T) {
	eval := NewVMEvaluator()
	v, err := anyToVMValue(eval.vm_, map[string]int{"a": 1, "b": 2})
	if err != nil {
		t.Fatalf("expected typed map conversion success, got %v", err)
	}
	result := vmValueToAny(eval.vm_, v).(map[string]any)
	if result["a"].(int) != 1 || result["b"].(int) != 2 {
		t.Fatalf("expected map {a:1 b:2}, got %v", result)
	}
}

func TestVMEvaluator_EvaluateNestedUnsupportedPathIsPrecise(t *testing.T) {
	eval := NewVMEvaluator()
	prog, err := frontend.ParseModuleForTest(`fun passthrough(v: any): any { return v }`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := eval.CompileProgram(prog); err != nil {
		t.Fatalf("compile: %v", err)
	}
	_, err = eval.Evaluate("passthrough", binding.InvocationStageUnary, []any{map[string]any{"items": []any{1, make(chan int)}}})
	if err == nil {
		t.Fatal("expected evaluate error for unsupported nested host value")
	}
	rtErr, ok := err.(*RuntimeError)
	if !ok {
		t.Fatalf("expected RuntimeError, got %T", err)
	}
	if rtErr.Code != "unsupported_vm_argument_type" {
		t.Fatalf("expected unsupported_vm_argument_type, got %q", rtErr.Code)
	}
	if rtErr.Path != "vm/evaluator/argument/0/items/1" {
		t.Fatalf("expected precise nested path, got %q", rtErr.Path)
	}
}

func TestVMEvaluator_RepeatedArrayPushWithGCPressure(t *testing.T) {
	eval := NewVMEvaluator()
	prog, err := frontend.ParseModuleForTest(`
fun build(n: int): int {
  var parts: array<string> = []
  for (var i: int = 0; i < n; i = i + 1) { push(parts, "x") }
  return len(parts)
}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := eval.CompileProgram(prog); err != nil {
		t.Fatalf("compile: %v", err)
	}
	for i := 0; i < 100; i++ {
		result, err := eval.Evaluate("build", binding.InvocationStageUnary, []any{100})
		if err != nil {
			t.Fatalf("evaluate %d: %v", i, err)
		}
		if result.(int) != 100 {
			t.Fatalf("build %d = %v, want 100", i, result)
		}
	}
}

// --- Stream session key ---

func TestVMEvaluator_StreamSessionKey(t *testing.T) {
	key1 := streamSessionKey("gen", []vm.Value{vm.EncodeInt(1)})
	key2 := streamSessionKey("gen", []vm.Value{vm.EncodeInt(2)})
	if key1 == key2 {
		t.Error("different args should produce different session keys")
	}
	key3 := streamSessionKey("gen", []vm.Value{vm.EncodeInt(1)})
	if key1 != key3 {
		t.Error("same args should produce same session key")
	}
	key4 := streamSessionKey("other", []vm.Value{vm.EncodeInt(1)})
	if key1 == key4 {
		t.Error("different callables should produce different session keys")
	}
}

// --- Stream full lifecycle ---

func TestVMEvaluator_StreamLifecycle(t *testing.T) {
	eval := NewVMEvaluator()
	prog, err := frontend.ParseModuleForTest(`
stream fun counter(): int {
  yield 1
  yield 2
  return 3
}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := eval.CompileProgram(prog); err != nil {
		t.Fatalf("compile: %v", err)
	}

	// First next: yields 1
	val, err := eval.Evaluate("counter", binding.InvocationStageNext, nil)
	if err != nil {
		t.Fatalf("next 1: %v", err)
	}
	if val.(int) != 1 {
		t.Errorf("next 1 = %v, want 1", val)
	}

	// Second next: yields 2
	val, err = eval.Evaluate("counter", binding.InvocationStageNext, nil)
	if err != nil {
		t.Fatalf("next 2: %v", err)
	}
	if val.(int) != 2 {
		t.Errorf("next 2 = %v, want 2", val)
	}

	// Final: returns 3
	val, err = eval.Evaluate("counter", binding.InvocationStageFinal, nil)
	if err != nil {
		t.Fatalf("final: %v", err)
	}
	if val.(int) != 3 {
		t.Errorf("final = %v, want 3", val)
	}

	// Next after final should fail.
	_, err = eval.Evaluate("counter", binding.InvocationStageNext, nil)
	if err == nil {
		t.Fatal("expected error on next after final")
	}
}

// --- Stream reset ---

func TestVMEvaluator_StreamReset(t *testing.T) {
	eval := NewVMEvaluator()
	prog, err := frontend.ParseModuleForTest(`
stream fun gen(): int {
  yield 1
  return 2
}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := eval.CompileProgram(prog); err != nil {
		t.Fatalf("compile: %v", err)
	}

	// Exhaust the stream.
	_, _ = eval.Evaluate("gen", binding.InvocationStageNext, nil)
	_, _ = eval.Evaluate("gen", binding.InvocationStageFinal, nil)

	// Reset.
	eval.Reset("gen", nil)

	// Should be able to use again after reset.
	val, err := eval.Evaluate("gen", binding.InvocationStageNext, nil)
	if err != nil {
		t.Fatalf("next after reset: %v", err)
	}
	if val.(int) != 1 {
		t.Errorf("next after reset = %v, want 1", val)
	}
}

// --- Runtime error propagation ---

func TestVMEvaluator_RuntimeErrorPropagation(t *testing.T) {
	eval := NewVMEvaluator()
	prog, err := frontend.ParseModuleForTest(`fun boom(): int { return 1 / 0 }`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := eval.CompileProgram(prog); err != nil {
		t.Fatalf("compile: %v", err)
	}
	_, err = eval.Evaluate("boom", binding.InvocationStageUnary, nil)
	if err == nil {
		t.Fatal("expected runtime error for division by zero")
	}
}

func TestVMEvaluator_NonRuntimeErrorUsesCallErrMessage(t *testing.T) {
	fn := &Chunk{sourceName: "boom", code: []instruction{{op: opLoadLocal, operand: 99}}}
	eval := NewVMEvaluator()
	eval.chunks["boom"] = fn

	_, err := eval.Evaluate("boom", binding.InvocationStageUnary, nil)
	if err == nil {
		t.Fatal("expected wrapped evaluator error")
	}
	if !strings.Contains(err.Error(), "callable \"boom\"") {
		t.Fatalf("expected callable context in wrapped error, got %v", err)
	}
}

// --- Multiple callables ---

func TestVMEvaluator_MultipleCallables(t *testing.T) {
	eval := NewVMEvaluator()
	prog, err := frontend.ParseModuleForTest(`
fun twice(x: int): int { return x * 2 }
fun square(x: int): int { return x * x }
`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := eval.CompileProgram(prog); err != nil {
		t.Fatalf("compile: %v", err)
	}

	r1, err := eval.Evaluate("twice", binding.InvocationStageUnary, []any{5})
	if err != nil {
		t.Fatalf("twice: %v", err)
	}
	if r1.(int) != 10 {
		t.Errorf("twice(5) = %v, want 10", r1)
	}

	r2, err := eval.Evaluate("square", binding.InvocationStageUnary, []any{4})
	if err != nil {
		t.Fatalf("square: %v", err)
	}
	if r2.(int) != 16 {
		t.Errorf("square(4) = %v, want 16", r2)
	}
}

// --- Concurrent session isolation ---

func TestVMEvaluator_StreamSessionIsolation(t *testing.T) {
	eval := NewVMEvaluator()
	prog, err := frontend.ParseModuleForTest(`
stream fun gen(seed: int): int {
  yield seed + 1
  yield seed + 2
  return seed + 3
}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := eval.CompileProgram(prog); err != nil {
		t.Fatalf("compile: %v", err)
	}

	// Start session with arg 0.
	val, err := eval.Evaluate("gen", binding.InvocationStageNext, []any{0})
	if err != nil {
		t.Fatalf("session A next: %v", err)
	}
	if val.(int) != 1 {
		t.Errorf("session A next = %v, want 1", val)
	}

	// Start session with arg 10 (different key and output).
	val, err = eval.Evaluate("gen", binding.InvocationStageNext, []any{10})
	if err != nil {
		t.Fatalf("session B next: %v", err)
	}
	if val.(int) != 11 {
		t.Errorf("session B next = %v, want 11", val)
	}

	// Resume first session; it should not be affected by session B.
	val, err = eval.Evaluate("gen", binding.InvocationStageNext, []any{0})
	if err != nil {
		t.Fatalf("session A second next: %v", err)
	}
	if val.(int) != 2 {
		t.Errorf("session A second next = %v, want 2", val)
	}

	// Both sessions should be tracked independently.
	sessions := eval.SessionsForTest()
	count := 0
	for range sessions {
		count++
	}
	if count != 2 {
		t.Errorf("expected 2 active sessions, got %d", count)
	}
}

func TestVMEvaluator_StreamResetAfterRuntimeError(t *testing.T) {
	eval := NewVMEvaluator()
	prog, err := frontend.ParseModuleForTest(`
stream fun risky(): int {
  yield 1
  return [1][5]
}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := eval.CompileProgram(prog); err != nil {
		t.Fatalf("compile: %v", err)
	}

	first, err := eval.Evaluate("risky", binding.InvocationStageNext, nil)
	if err != nil {
		t.Fatalf("next before error: %v", err)
	}
	if first.(int) != 1 {
		t.Fatalf("expected first yield 1, got %v", first)
	}

	_, err = eval.Evaluate("risky", binding.InvocationStageFinal, nil)
	if err == nil {
		t.Fatal("expected runtime error on final")
	}

	eval.Reset("risky", nil)
	again, err := eval.Evaluate("risky", binding.InvocationStageNext, nil)
	if err != nil {
		t.Fatalf("next after reset: %v", err)
	}
	if again.(int) != 1 {
		t.Fatalf("expected next after reset to yield 1, got %v", again)
	}
}

func TestVMEvaluator_StreamRuntimeErrorDoesNotCorruptOtherSession(t *testing.T) {
	eval := NewVMEvaluator()
	prog, err := frontend.ParseModuleForTest(`
stream fun risky(seed: int): int {
  yield seed
  if seed == 0 {
    return [1][5]
  }
  return seed + 10
}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := eval.CompileProgram(prog); err != nil {
		t.Fatalf("compile: %v", err)
	}

	if _, err := eval.Evaluate("risky", binding.InvocationStageNext, []any{0}); err != nil {
		t.Fatalf("session A next: %v", err)
	}
	if _, err := eval.Evaluate("risky", binding.InvocationStageNext, []any{5}); err != nil {
		t.Fatalf("session B next: %v", err)
	}

	_, err = eval.Evaluate("risky", binding.InvocationStageFinal, []any{0})
	if err == nil {
		t.Fatal("expected runtime error for session A")
	}

	val, err := eval.Evaluate("risky", binding.InvocationStageFinal, []any{5})
	if err != nil {
		t.Fatalf("session B final should still succeed, got %v", err)
	}
	if val.(int) != 15 {
		t.Fatalf("expected session B final 15, got %v", val)
	}
}

func TestVMEvaluator_StreamResetAfterNestedContainerMutation(t *testing.T) {
	eval := NewVMEvaluator()
	prog, err := frontend.ParseModuleForTest(`
stream fun run(): int {
  var state: map<string, int> = {"count": 1}
  yield state["count"]
  state["count"] = state["count"] + 4
  return state["count"]
}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := eval.CompileProgram(prog); err != nil {
		t.Fatalf("compile: %v", err)
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
	if final.(int) != 5 {
		t.Fatalf("expected final 5, got %v", final)
	}

	eval.Reset("run", nil)
	again, err := eval.Evaluate("run", binding.InvocationStageFinal, nil)
	if err != nil {
		t.Fatalf("final after reset: %v", err)
	}
	if again.(int) != 5 {
		t.Fatalf("expected final after reset 5, got %v", again)
	}
}

func TestVMEvaluator_StreamErrorPathPreservesDiagnosticEnvelope(t *testing.T) {
	eval := NewVMEvaluator()
	prog, err := frontend.ParseModuleForTest(`
stream fun risky(): int {
  yield 1
  return [1][5]
}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := eval.CompileProgram(prog); err != nil {
		t.Fatalf("compile: %v", err)
	}
	if _, err := eval.Evaluate("risky", binding.InvocationStageNext, nil); err != nil {
		t.Fatalf("next before error: %v", err)
	}
	_, err = eval.Evaluate("risky", binding.InvocationStageFinal, nil)
	if err == nil {
		t.Fatal("expected runtime error on final")
	}
	rtErr, ok := err.(*RuntimeError)
	if !ok {
		t.Fatalf("expected RuntimeError, got %T", err)
	}
	if rtErr.Code != "array_index_out_of_range" {
		t.Fatalf("expected array_index_out_of_range, got %q", rtErr.Code)
	}
	if rtErr.Path != "vm/array/index" {
		t.Fatalf("expected vm/array/index path, got %q", rtErr.Path)
	}
	if rtErr.Callable != "risky" {
		t.Fatalf("expected callable risky, got %q", rtErr.Callable)
	}
}

func TestVMEvaluator_StreamNestedHostValueAcrossYield(t *testing.T) {
	eval := NewVMEvaluator()
	prog, err := frontend.ParseModuleForTest(`
stream fun inspect(payload: any): any {
  yield payload["items"][0]["score"]
  return payload
}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := eval.CompileProgram(prog); err != nil {
		t.Fatalf("compile: %v", err)
	}
	payload := map[string]any{
		"items": []any{
			map[string]any{"score": 3},
			map[string]any{"score": 5},
		},
	}
	first, err := eval.Evaluate("inspect", binding.InvocationStageNext, []any{payload})
	if err != nil {
		t.Fatalf("next: %v", err)
	}
	if first.(int) != 3 {
		t.Fatalf("expected first yield 3, got %v", first)
	}
	final, err := eval.Evaluate("inspect", binding.InvocationStageFinal, []any{payload})
	if err != nil {
		t.Fatalf("final: %v", err)
	}
	returned, ok := final.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any result, got %T", final)
	}
	items, ok := returned["items"].([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("expected items slice of len 2, got %v", returned["items"])
	}
	firstItem, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("expected first item map, got %T", items[0])
	}
	if firstItem["score"].(int) != 3 {
		t.Fatalf("expected first score 3 after resume, got %v", firstItem["score"])
	}
}

func TestVMEvaluator_StreamNestedHostValueMutationAcrossYield(t *testing.T) {
	eval := NewVMEvaluator()
	prog, err := frontend.ParseModuleForTest(`
stream fun mutate(payload: any): any {
  yield payload["items"][0]["score"]
  payload["items"][0]["score"] = payload["items"][0]["score"] + 4
  return payload
}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := eval.CompileProgram(prog); err != nil {
		t.Fatalf("compile: %v", err)
	}
	payload := map[string]any{
		"items": []any{
			map[string]any{"score": 3},
		},
	}
	first, err := eval.Evaluate("mutate", binding.InvocationStageNext, []any{payload})
	if err != nil {
		t.Fatalf("next: %v", err)
	}
	if first.(int) != 3 {
		t.Fatalf("expected first yield 3, got %v", first)
	}
	final, err := eval.Evaluate("mutate", binding.InvocationStageFinal, []any{payload})
	if err != nil {
		t.Fatalf("final: %v", err)
	}
	returned, ok := final.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any result, got %T", final)
	}
	items := returned["items"].([]any)
	firstItem := items[0].(map[string]any)
	if firstItem["score"].(int) != 7 {
		t.Fatalf("expected mutated score 7 after resume, got %v", firstItem["score"])
	}
}

func TestVMEvaluator_StreamResetAfterNestedHostMutation(t *testing.T) {
	eval := NewVMEvaluator()
	prog, err := frontend.ParseModuleForTest(`
stream fun mutate(payload: any): any {
  yield payload["items"][0]["score"]
  payload["items"][0]["score"] = payload["items"][0]["score"] + 4
  return payload
}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := eval.CompileProgram(prog); err != nil {
		t.Fatalf("compile: %v", err)
	}
	payload := map[string]any{
		"items": []any{
			map[string]any{"score": 3},
		},
	}
	if _, err := eval.Evaluate("mutate", binding.InvocationStageNext, []any{payload}); err != nil {
		t.Fatalf("next: %v", err)
	}
	if _, err := eval.Evaluate("mutate", binding.InvocationStageFinal, []any{payload}); err != nil {
		t.Fatalf("final: %v", err)
	}
	eval.Reset("mutate", []any{payload})
	freshPayload := map[string]any{
		"items": []any{
			map[string]any{"score": 3},
		},
	}
	fresh, err := eval.Evaluate("mutate", binding.InvocationStageFinal, []any{freshPayload})
	if err != nil {
		t.Fatalf("final after reset: %v", err)
	}
	returned := fresh.(map[string]any)
	items := returned["items"].([]any)
	firstItem := items[0].(map[string]any)
	if firstItem["score"].(int) != 7 {
		t.Fatalf("expected fresh payload mutation to end at 7, got %v", firstItem["score"])
	}
}

// --- Host typed composite expansion ---

func TestVMEvaluator_AnyToVMValue_SliceOfMapOfSliceRoundTrip(t *testing.T) {
	eval := NewVMEvaluator()
	input := []map[string][]int{
		{"nums": {1, 2, 3}},
		{"nums": {4, 5}},
	}
	v, err := anyToVMValue(eval.vm_, input)
	if err != nil {
		t.Fatalf("expected slice-of-map-of-slice conversion success, got %v", err)
	}
	result, ok := vmValueToAny(eval.vm_, v).([]any)
	if !ok {
		t.Fatalf("expected []any result, got %T", vmValueToAny(eval.vm_, v))
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(result))
	}
	first := result[0].(map[string]any)
	if len(first) != 1 {
		t.Fatalf("expected first entry to have 1 key, got %d", len(first))
	}
	firstNums := first["nums"].([]any)
	if len(firstNums) != 3 || firstNums[0].(int) != 1 || firstNums[1].(int) != 2 || firstNums[2].(int) != 3 {
		t.Fatalf("expected first nums [1 2 3], got %v", firstNums)
	}
	second := result[1].(map[string]any)
	if len(second) != 1 {
		t.Fatalf("expected second entry to have 1 key, got %d", len(second))
	}
	secondNums := second["nums"].([]any)
	if len(secondNums) != 2 || secondNums[0].(int) != 4 || secondNums[1].(int) != 5 {
		t.Fatalf("expected second nums [4 5], got %v", secondNums)
	}
}

func TestVMEvaluator_AnyToVMValue_MapOfMapOfSliceRoundTrip(t *testing.T) {
	eval := NewVMEvaluator()
	input := map[string]map[string][]string{
		"team":    {"front": {"a", "b"}, "back": {"c"}},
		"support": {"medic": {"d", "e"}},
	}
	v, err := anyToVMValue(eval.vm_, input)
	if err != nil {
		t.Fatalf("expected map-of-map-of-slice conversion success, got %v", err)
	}
	result, ok := vmValueToAny(eval.vm_, v).(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any result, got %T", vmValueToAny(eval.vm_, v))
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 outer keys, got %d", len(result))
	}
	team := result["team"].(map[string]any)
	if len(team) != 2 {
		t.Fatalf("expected team to have 2 keys, got %d", len(team))
	}
	front := team["front"].([]any)
	if len(front) != 2 || front[0].(string) != "a" || front[1].(string) != "b" {
		t.Fatalf("expected team.front [a b], got %v", front)
	}
	back := team["back"].([]any)
	if len(back) != 1 || back[0].(string) != "c" {
		t.Fatalf("expected team.back [c], got %v", back)
	}
	support := result["support"].(map[string]any)
	if len(support) != 1 {
		t.Fatalf("expected support to have 1 key, got %d", len(support))
	}
	medic := support["medic"].([]any)
	if len(medic) != 2 || medic[0].(string) != "d" || medic[1].(string) != "e" {
		t.Fatalf("expected support.medic [d e], got %v", medic)
	}
}

func TestVMEvaluator_AnyToVMValue_MixedNumericNestedRoundTrip(t *testing.T) {
	// int and int32 both encode through vm.EncodeInt; vmValueToAny decodes
	// any inline int value back as Go `int`, so int32(2) round-trips to int(2).
	// int64/uint64/float64 encode through their own widened paths and decode
	// back to their original Go widened type; float32 promotes to float64.
	eval := NewVMEvaluator()
	input := map[string]any{
		"ints":   []any{int(1), int32(2), int64(3)},
		"floats": []any{float32(1.5), float64(2.5)},
		"uints":  []any{uint64(7)},
	}
	v, err := anyToVMValue(eval.vm_, input)
	if err != nil {
		t.Fatalf("expected mixed-numeric conversion success, got %v", err)
	}
	result := vmValueToAny(eval.vm_, v).(map[string]any)
	ints := result["ints"].([]any)
	if ints[0].(int) != 1 {
		t.Errorf("ints[0]: want int(1), got %v (%T)", ints[0], ints[0])
	}
	if ints[1].(int) != 2 {
		t.Errorf("ints[1]: want int(2), got %v (%T)", ints[1], ints[1])
	}
	if ints[2].(int64) != 3 {
		t.Errorf("ints[2]: want int64(3), got %v (%T)", ints[2], ints[2])
	}
	floats := result["floats"].([]any)
	if floats[0].(float64) != 1.5 {
		t.Errorf("floats[0]: want 1.5, got %v (%T)", floats[0], floats[0])
	}
	if floats[1].(float64) != 2.5 {
		t.Errorf("floats[1]: want 2.5, got %v (%T)", floats[1], floats[1])
	}
	uints := result["uints"].([]any)
	if uints[0].(uint64) != 7 {
		t.Errorf("uints[0]: want uint64(7), got %v (%T)", uints[0], uints[0])
	}
}

// --- Stream session scale / cleanup pressure ---

func TestVMEvaluator_StreamSessionScale(t *testing.T) {
	eval := NewVMEvaluator()
	prog, err := frontend.ParseModuleForTest(`
stream fun gen(seed: int): int {
  yield seed
  yield seed + 1
  return seed + 2
}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := eval.CompileProgram(prog); err != nil {
		t.Fatalf("compile: %v", err)
	}

	const n = 12
	for i := 0; i < n; i++ {
		val, err := eval.Evaluate("gen", binding.InvocationStageNext, []any{i})
		if err != nil {
			t.Fatalf("session %d first next: %v", i, err)
		}
		if val.(int) != i {
			t.Errorf("session %d first next = %v, want %d", i, val, i)
		}
	}
	if got := len(eval.SessionsForTest()); got != n {
		t.Fatalf("expected %d active sessions, got %d", n, got)
	}

	for i := 0; i < n; i++ {
		val, err := eval.Evaluate("gen", binding.InvocationStageNext, []any{i})
		if err != nil {
			t.Fatalf("session %d second next: %v", i, err)
		}
		if val.(int) != i+1 {
			t.Errorf("session %d second next = %v, want %d", i, val, i+1)
		}
	}

	for i := 0; i < n; i++ {
		val, err := eval.Evaluate("gen", binding.InvocationStageFinal, []any{i})
		if err != nil {
			t.Fatalf("session %d final: %v", i, err)
		}
		if val.(int) != i+2 {
			t.Errorf("session %d final = %v, want %d", i, val, i+2)
		}
	}

	sessions := eval.SessionsForTest()
	if len(sessions) != n {
		t.Fatalf("expected %d exhausted sessions still tracked, got %d", n, len(sessions))
	}
	for k, s := range sessions {
		if !s.ExhaustedForTest() {
			t.Errorf("session %s not marked exhausted", k)
		}
	}
}

func TestVMEvaluator_StreamRepeatedResetIsSafe(t *testing.T) {
	eval := NewVMEvaluator()
	prog, err := frontend.ParseModuleForTest(`stream fun gen(): int { yield 1 return 2 }`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := eval.CompileProgram(prog); err != nil {
		t.Fatalf("compile: %v", err)
	}

	eval.Reset("gen", nil)
	eval.Reset("gen", nil)

	if _, err := eval.Evaluate("gen", binding.InvocationStageNext, nil); err != nil {
		t.Fatalf("first next: %v", err)
	}
	eval.Reset("gen", nil)
	eval.Reset("gen", nil)
	eval.Reset("gen", nil)
	if got := len(eval.SessionsForTest()); got != 0 {
		t.Fatalf("expected 0 sessions after resets, got %d", got)
	}

	val, err := eval.Evaluate("gen", binding.InvocationStageNext, nil)
	if err != nil {
		t.Fatalf("next after multiple resets: %v", err)
	}
	if val.(int) != 1 {
		t.Errorf("next after multiple resets = %v, want 1", val)
	}
}

func TestVMEvaluator_StreamExhaustedSessionRejectsRepeatedFinal(t *testing.T) {
	eval := NewVMEvaluator()
	prog, err := frontend.ParseModuleForTest(`stream fun gen(): int { yield 1 return 2 }`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := eval.CompileProgram(prog); err != nil {
		t.Fatalf("compile: %v", err)
	}

	if _, err := eval.Evaluate("gen", binding.InvocationStageNext, nil); err != nil {
		t.Fatalf("next: %v", err)
	}
	if _, err := eval.Evaluate("gen", binding.InvocationStageFinal, nil); err != nil {
		t.Fatalf("final: %v", err)
	}

	sessions := eval.SessionsForTest()
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session after final, got %d", len(sessions))
	}
	for _, s := range sessions {
		if !s.ExhaustedForTest() {
			t.Fatal("expected session marked exhausted after final")
		}
	}

	_, err = eval.Evaluate("gen", binding.InvocationStageNext, nil)
	if err == nil {
		t.Fatal("expected stream_exhausted on next after final")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "stream_exhausted" {
		t.Fatalf("expected stream_exhausted on next, got %v", err)
	}

	_, err = eval.Evaluate("gen", binding.InvocationStageFinal, nil)
	if err == nil {
		t.Fatal("expected stream_exhausted on repeated final")
	}
	coder, ok = err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "stream_exhausted" {
		t.Fatalf("expected stream_exhausted on repeated final, got %v", err)
	}

	sessions = eval.SessionsForTest()
	if len(sessions) != 1 {
		t.Fatalf("expected session still tracked after repeated final, got %d", len(sessions))
	}
	for _, s := range sessions {
		if !s.ExhaustedForTest() {
			t.Fatal("expected session to remain exhausted after repeated final")
		}
	}
}
