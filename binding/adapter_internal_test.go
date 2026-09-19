package binding

import (
	"reflect"
	"testing"

	"github.com/qomos-w/spore/schema"
)

func TestBindGoFunctionArg_StructFromMap(t *testing.T) {
	type inner struct {
		Name string
		Val  int32
	}
	target := reflect.TypeOf(inner{})
	m := map[string]any{"Name": "test", "Val": int32(42)}
	val := reflect.ValueOf(m)
	got, err := bindGoFunctionArg(target, val)
	if err != nil {
		t.Fatalf("bindGoFunctionArg: %v", err)
	}
	result := got.Interface().(inner)
	if result.Name != "test" || result.Val != 42 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestBindGoFunctionArg_PointerAssignable(t *testing.T) {
	target := reflect.TypeOf((*int32)(nil))
	val := reflect.ValueOf(int32(42))
	got, err := bindGoFunctionArg(target, val)
	if err != nil {
		t.Fatalf("bindGoFunctionArg: %v", err)
	}
	if got.Kind() != reflect.Pointer || *got.Interface().(*int32) != 42 {
		t.Fatalf("expected *int32 with value 42, got %v", got.Interface())
	}
}

func TestBindGoFunctionArg_AssignableDirect(t *testing.T) {
	target := reflect.TypeOf(int32(0))
	val := reflect.ValueOf(int32(42))
	got, err := bindGoFunctionArg(target, val)
	if err != nil {
		t.Fatalf("bindGoFunctionArg: %v", err)
	}
	if got.Interface().(int32) != 42 {
		t.Fatalf("expected 42, got %v", got.Interface())
	}
}

func TestBindGoFunctionArg_Convertible(t *testing.T) {
	target := reflect.TypeOf(int64(0))
	val := reflect.ValueOf(int32(42))
	got, err := bindGoFunctionArg(target, val)
	if err != nil {
		t.Fatalf("bindGoFunctionArg: %v", err)
	}
	if got.Interface().(int64) != 42 {
		t.Fatalf("expected int64(42), got %v", got.Interface())
	}
}

func TestBindGoFunctionArg_FallbackReturnsValue(t *testing.T) {
	// When value is not assignable/convertible, function returns it as-is
	target := reflect.TypeOf(int32(0))
	val := reflect.ValueOf("incompatible")
	got, _ := bindGoFunctionArg(target, val)
	if got.Interface() != "incompatible" {
		t.Fatalf("expected fallback passthrough, got %v", got.Interface())
	}
}

func TestValidateScalarKind_String(t *testing.T) {
	fd := schema.FieldDesc{Name: "Name", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}}
	if err := validateScalarKind(fd, reflect.String); err != nil {
		t.Fatalf("expected nil for string kind, got %v", err)
	}
	if err := validateScalarKind(fd, reflect.Int); err == nil {
		t.Fatal("expected error for int vs string")
	}
}

func TestValidateScalarKind_Bool(t *testing.T) {
	fd := schema.FieldDesc{Name: "Active", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "bool"}}
	if err := validateScalarKind(fd, reflect.Bool); err != nil {
		t.Fatalf("expected nil for bool kind, got %v", err)
	}
	if err := validateScalarKind(fd, reflect.String); err == nil {
		t.Fatal("expected error for string vs bool")
	}
}

func TestValidateScalarKind_IntegerTypes(t *testing.T) {
	cases := []string{"int", "byte", "short", "ushort", "uint", "long", "ulong"}
	for _, name := range cases {
		fd := schema.FieldDesc{Name: "Val", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: name}}
		if err := validateScalarKind(fd, reflect.Int); err != nil {
			t.Fatalf("expected nil for %s with int kind, got %v", name, err)
		}
		if err := validateScalarKind(fd, reflect.Int8); err != nil {
			t.Fatalf("expected nil for %s with int8 kind, got %v", name, err)
		}
		if err := validateScalarKind(fd, reflect.Uint64); err != nil {
			t.Fatalf("expected nil for %s with uint64 kind, got %v", name, err)
		}
		if err := validateScalarKind(fd, reflect.Float64); err == nil {
			t.Fatalf("expected error for %s with float kind", name)
		}
	}
}

func TestValidateScalarKind_FloatTypes(t *testing.T) {
	for _, name := range []string{"float", "double"} {
		fd := schema.FieldDesc{Name: "Val", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: name}}
		if err := validateScalarKind(fd, reflect.Float32); err != nil {
			t.Fatalf("expected nil for %s with float32 kind, got %v", name, err)
		}
		if err := validateScalarKind(fd, reflect.Float64); err != nil {
			t.Fatalf("expected nil for %s with float64 kind, got %v", name, err)
		}
		if err := validateScalarKind(fd, reflect.Int); err == nil {
			t.Fatalf("expected error for %s with int kind", name)
		}
	}
}

func TestValidateScalarKind_UnknownScalarAllowed(t *testing.T) {
	fd := schema.FieldDesc{Name: "Custom", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "custom_scalar"}}
	if err := validateScalarKind(fd, reflect.Slice); err != nil {
		t.Fatalf("expected nil for unknown scalar type, got %v", err)
	}
}

// --- Narrowing conversion guards ---

func TestBindGoFunctionArg_Narrowing_Int64ToInt32_Overflow(t *testing.T) {
	target := reflect.TypeOf(int32(0))
	val := reflect.ValueOf(int64(3000000000))
	_, err := bindGoFunctionArg(target, val)
	if err == nil {
		t.Fatal("expected error for int64 overflowing int32")
	}
}

func TestBindGoFunctionArg_Narrowing_Int64ToInt32_InRange(t *testing.T) {
	target := reflect.TypeOf(int32(0))
	val := reflect.ValueOf(int64(42))
	got, err := bindGoFunctionArg(target, val)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Interface().(int32) != 42 {
		t.Fatalf("expected 42, got %v", got.Interface())
	}
}

func TestBindGoFunctionArg_Narrowing_Float64ToFloat32_Overflow(t *testing.T) {
	target := reflect.TypeOf(float32(0))
	// A value far outside float32 range should be rejected.
	val := reflect.ValueOf(float64(1e40))
	_, err := bindGoFunctionArg(target, val)
	if err == nil {
		t.Fatal("expected error for float64 overflowing float32 range")
	}
}

func TestBindGoFunctionArg_Narrowing_Float64ToFloat32_InRange(t *testing.T) {
	target := reflect.TypeOf(float32(0))
	val := reflect.ValueOf(float64(3.14))
	got, err := bindGoFunctionArg(target, val)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Interface().(float32) != float32(3.14) {
		t.Fatalf("expected 3.14, got %v", got.Interface())
	}
}

func TestBindGoFunctionArg_Narrowing_Uint64ToInt32(t *testing.T) {
	target := reflect.TypeOf(int32(0))
	val := reflect.ValueOf(uint64(3000000000))
	_, err := bindGoFunctionArg(target, val)
	if err == nil {
		t.Fatal("expected error for uint64 overflowing int32")
	}
}

func TestBindGoFunctionArg_Narrowing_SignedToUnsigned_Negative(t *testing.T) {
	target := reflect.TypeOf(uint32(0))
	val := reflect.ValueOf(int64(-1))
	_, err := bindGoFunctionArg(target, val)
	if err == nil {
		t.Fatal("expected error for negative value in unsigned target")
	}
}

func TestBindGoFunctionArg_Widening_Allowed(t *testing.T) {
	// int32 -> int64 is widening; should always succeed.
	target := reflect.TypeOf(int64(0))
	val := reflect.ValueOf(int32(42))
	got, err := bindGoFunctionArg(target, val)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Interface().(int64) != 42 {
		t.Fatalf("expected 42, got %v", got.Interface())
	}
}
