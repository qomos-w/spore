package vm

import (
	"math"
	"testing"
)

func TestBoolRoundTrip(t *testing.T) {
	for _, b := range []bool{true, false} {
		v := encodeBool(b)
		if !v.isBool() {
			t.Errorf("encodeBool(%v).isBool() = false", b)
		}
		if v.decodeBool() != b {
			t.Errorf("decodeBool(%v) = %v, want %v", v, v.decodeBool(), b)
		}
	}
}

func TestIntRoundTrip(t *testing.T) {
	tests := []int32{0, 1, -1, 42, -42, 2147483647, -2147483648}
	for _, n := range tests {
		v := encodeInt(n)
		if !v.isInt() {
			t.Errorf("encodeInt(%d).isInt() = false", n)
		}
		if v.decodeInt() != n {
			t.Errorf("decodeInt() = %d, want %d", v.decodeInt(), n)
		}
	}
}

func TestUIntRoundTrip(t *testing.T) {
	tests := []uint32{0, 1, 42, 4294967295}
	for _, n := range tests {
		v := encodeUInt(n)
		if v.decodeUInt() != n {
			t.Errorf("decodeUInt() = %d, want %d", v.decodeUInt(), n)
		}
	}
}

func TestFloatRoundTrip(t *testing.T) {
	tests := []float32{0, 1.5, -3.14, math.MaxFloat32, math.SmallestNonzeroFloat32}
	for _, f := range tests {
		v := encodeFloat(f)
		if !v.isFloat() {
			t.Errorf("encodeFloat(%g).isFloat() = false", f)
		}
		if v.decodeFloat() != f {
			t.Errorf("decodeFloat() = %g, want %g", v.decodeFloat(), f)
		}
	}
}

func TestHandleRoundTrip(t *testing.T) {
	tests := []handle{0, 1, 100, 255}
	for _, h := range tests {
		v := encodeHandle(h)
		if !v.isPointer() {
			t.Errorf("encodeHandle(%d).isPointer() = false", h)
		}
		if v.decodeHandle() != h {
			t.Errorf("decodeHandle() = %d, want %d", v.decodeHandle(), h)
		}
	}
}

func TestSmallStringRoundTrip(t *testing.T) {
	tests := []string{"", "a", "ab", "abc", "test", "hello!"}
	for _, s := range tests {
		v := encodeSmallString([]byte(s))
		if !v.isSmallString() {
			t.Errorf("encodeSmallString(%q).isSmallString() = false", s)
		}
		got := string(v.decodeSmallString())
		if got != s {
			t.Errorf("decodeSmallString() = %q, want %q", got, s)
		}
	}
}

func TestSmallStringLength(t *testing.T) {
	for _, s := range []string{"", "a", "abc", "123456"} {
		v := encodeSmallString([]byte(s))
		if v.smallStringLength() != len(s) {
			t.Errorf("smallStringLength() = %d, want %d for %q", v.smallStringLength(), len(s), s)
		}
	}
}

func TestIsNull(t *testing.T) {
	v := encodeHandle(invalidHandle)
	if !v.isNull() {
		t.Error("invalidHandle should encode as null")
	}
	if v.isPointer() {
		t.Error("null should not be an actual pointer")
	}
	v2 := encodeHandle(0)
	if v2.isNull() {
		t.Error("handle 0 should not be null")
	}
}

func TestTagIsolation(t *testing.T) {
	// Ensure different value types produce different tags.
	vi := encodeInt(42)
	vf := encodeFloat(1.0)
	vb := encodeBool(true)
	vs := encodeSmallString([]byte("x"))
	vh := encodeHandle(1)

	tags := map[string]uint64{
		"int":    vi.tag(),
		"float":  vf.tag(),
		"bool":   vb.tag(),
		"string": vs.tag(),
		"handle": vh.tag(),
	}

	seen := make(map[uint64]string)
	for name, tag := range tags {
		if prev, dup := seen[tag]; dup {
			t.Errorf("tag collision: %s and %s both have tag %d", prev, name, tag)
		}
		seen[tag] = name
	}
}

func TestByteShortRoundTrip(t *testing.T) {
	v := encodeByte(255)
	if v.decodeByte() != 255 {
		t.Errorf("decodeByte() = %d, want 255", v.decodeByte())
	}
	v = encodeShort(-1000)
	if v.decodeShort() != -1000 {
		t.Errorf("decodeShort() = %d, want -1000", v.decodeShort())
	}
	v = encodeUShort(60000)
	if v.decodeUShort() != 60000 {
		t.Errorf("decodeUShort() = %d, want 60000", v.decodeUShort())
	}
}

func TestTypeIDBasicNames(t *testing.T) {
	tests := []struct {
		tid  typeID
		want string
	}{
		{typeBool, "bool"},
		{typeInt, "int"},
		{typeFloat, "float"},
		{typeDouble, "double"},
		{typeString, "string"},
		{typeVoid, "void"},
		{typeAny, "any"},
		{typeObject, "object"},
	}
	for _, tt := range tests {
		if got := typeName(tt.tid); got != tt.want {
			t.Errorf("typeName(%d) = %q, want %q", tt.tid, got, tt.want)
		}
	}
}

func TestArrayTypeEncoding(t *testing.T) {
	arrType := makeArrayType(typeInt)
	if getTypeCategory(arrType) != categoryArray {
		t.Error("expected array category")
	}
	if getElementType(arrType) != typeInt {
		t.Errorf("element type = %d, want %d", getElementType(arrType), typeInt)
	}
	name := typeName(arrType)
	if name != "array<int>" {
		t.Errorf("typeName() = %q, want %q", name, "array<int>")
	}
}

func TestMapTypeEncoding(t *testing.T) {
	mapType := makeMapType(typeString, typeInt)
	if getTypeCategory(mapType) != categoryMap {
		t.Error("expected map category")
	}
	if getKeyType(mapType) != typeString {
		t.Errorf("key type = %d, want %d", getKeyType(mapType), typeString)
	}
	if getValueType(mapType) != typeInt {
		t.Errorf("value type = %d, want %d", getValueType(mapType), typeInt)
	}
	name := typeName(mapType)
	if name != "map<string,int>" {
		t.Errorf("typeName() = %q, want %q", name, "map<string,int>")
	}
}

func TestClassTypeEncoding(t *testing.T) {
	classType := makeClassType(42)
	if getTypeCategory(classType) != categoryClass {
		t.Error("expected class category")
	}
	if getClassIDFromType(classType) != 42 {
		t.Errorf("class ID = %d, want 42", getClassIDFromType(classType))
	}
}
