package binding

import (
	"reflect"
	"testing"
)

func TestIsNarrowingConvertMatrix(t *testing.T) {
	cases := []struct {
		src, dst reflect.Kind
		want     bool
	}{
		{reflect.Int64, reflect.Int32, true},
		{reflect.Int32, reflect.Int32, false},
		{reflect.Int32, reflect.Int64, false},
		{reflect.Uint64, reflect.Uint8, true},
		{reflect.Uint8, reflect.Uint16, false},
		{reflect.Uint32, reflect.Int32, false},
		{reflect.Float64, reflect.Float32, true},
		{reflect.Float32, reflect.Float64, false},
		{reflect.Float64, reflect.Int64, true},
		{reflect.Float32, reflect.Int32, true},
		{reflect.String, reflect.String, false},
		{reflect.Int64, reflect.Float64, false},
		{reflect.String, reflect.Int, false},
	}
	for _, tc := range cases {
		if got := isNarrowingConvert(tc.src, tc.dst); got != tc.want {
			t.Fatalf("isNarrowingConvert(%v, %v) = %v, want %v", tc.src, tc.dst, got, tc.want)
		}
	}
	if !isIntKind(reflect.Int8) || isIntKind(reflect.String) {
		t.Fatal("isIntKind bounds broken")
	}
	if !isFloatKind(reflect.Float32) || isFloatKind(reflect.Int32) {
		t.Fatal("isFloatKind broken")
	}
}

func TestIntBitWidth(t *testing.T) {
	cases := map[reflect.Kind]int{
		reflect.Int8: 8, reflect.Uint8: 8,
		reflect.Int16: 16, reflect.Uint16: 16,
		reflect.Int32: 32, reflect.Uint32: 32,
		reflect.Int: 64, reflect.Uint: 64,
		reflect.Int64: 64, reflect.Uint64: 64,
		reflect.String: 0,
	}
	for kind, want := range cases {
		if got := intBitWidth(kind); got != want {
			t.Fatalf("intBitWidth(%v) = %d, want %d", kind, got, want)
		}
	}
}

func TestValueFitsInTargetMatrix(t *testing.T) {
	i64 := reflect.ValueOf(int64(1) << 40)
	if valueFitsInTarget(i64, reflect.TypeOf(int32(0))) {
		t.Fatal("int64 2^40 should not fit int32")
	}
	if !valueFitsInTarget(i64, reflect.TypeOf(int64(0))) {
		t.Fatal("int64 2^40 fits int64")
	}
	if !valueFitsInTarget(reflect.ValueOf(int64(-5)), reflect.TypeOf(int64(0))) {
		t.Fatal("negative int64 fits int64")
	}
	if valueFitsInTarget(reflect.ValueOf(int32(-1)), reflect.TypeOf(uint8(0))) {
		t.Fatal("negative should not fit unsigned")
	}
	if !valueFitsInTarget(reflect.ValueOf(uint8(255)), reflect.TypeOf(uint8(0))) {
		t.Fatal("255 fits uint8")
	}
	if valueFitsInTarget(reflect.ValueOf(uint16(256)), reflect.TypeOf(uint8(0))) {
		t.Fatal("256 should not fit uint8")
	}
	// float32 exactness: 2^24+1 is not representable exactly.
	if valueFitsInTarget(reflect.ValueOf(int64(1)<<24+1), reflect.TypeOf(float32(0))) {
		t.Fatal("2^24+1 loses exactness in float32")
	}
	if !valueFitsInTarget(reflect.ValueOf(int64(1<<24)), reflect.TypeOf(float32(0))) {
		t.Fatal("2^24 is exact in float32")
	}
	if !valueFitsInTarget(reflect.ValueOf(1e300), reflect.TypeOf(float64(0))) {
		t.Fatal("any float64 fits float64")
	}
	if !valueFitsInTarget(reflect.ValueOf("s"), reflect.TypeOf("")) {
		t.Fatal("unhandled kinds default to true")
	}
}

func TestFitsSignedRange(t *testing.T) {
	if !fitsSignedRange(reflect.ValueOf(int32(5)), -10, 10) {
		t.Fatal("5 fits [-10,10]")
	}
	if fitsSignedRange(reflect.ValueOf(int32(11)), -10, 10) {
		t.Fatal("11 does not fit [-10,10]")
	}
	if !fitsSignedRange(reflect.ValueOf(uint8(3)), 0, 10) {
		t.Fatal("uint 3 fits [0,10]")
	}
	if fitsSignedRange(reflect.ValueOf(uint8(3)), -1, 10) {
		t.Fatal("unsigned cannot fit negative-min range")
	}
	if fitsSignedRange(reflect.ValueOf("x"), 0, 10) {
		t.Fatal("non-numeric never fits")
	}
}

func TestFitsUnsignedRange(t *testing.T) {
	if !fitsUnsignedRange(reflect.ValueOf(int32(7)), 10) {
		t.Fatal("7 <= 10")
	}
	if fitsUnsignedRange(reflect.ValueOf(int32(-1)), 10) {
		t.Fatal("negative never fits unsigned")
	}
	if fitsUnsignedRange(reflect.ValueOf(int32(11)), 10) {
		t.Fatal("11 > 10")
	}
	if !fitsUnsignedRange(reflect.ValueOf(uint32(4)), 10) {
		t.Fatal("uint 4 <= 10")
	}
	if fitsUnsignedRange(reflect.ValueOf("x"), 10) {
		t.Fatal("non-numeric never fits")
	}
}

func TestToFloat64(t *testing.T) {
	if toFloat64(reflect.ValueOf(int8(-2))) != -2 {
		t.Fatal("int conversion broken")
	}
	if toFloat64(reflect.ValueOf(uint16(3))) != 3 {
		t.Fatal("uint conversion broken")
	}
	if toFloat64(reflect.ValueOf(float32(1.5))) != 1.5 {
		t.Fatal("float conversion broken")
	}
	if toFloat64(reflect.ValueOf("x")) != 0 {
		t.Fatal("non-numeric should return 0")
	}
}

// TestPipelineValueToAnyKinds moved to config/pipeline_binding_internal_test.go
// with the valueToAny adapter (binding no longer imports config).
