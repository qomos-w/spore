package vm

import (
	"math"
	"strings"
	"testing"
)

func TestExportedEncodeBool(t *testing.T) {
	for _, b := range []bool{true, false} {
		v := EncodeBool(b)
		if !IsBool(v) {
			t.Errorf("IsBool(EncodeBool(%v)) = false", b)
		}
		if DecodeBool(v) != b {
			t.Errorf("DecodeBool(EncodeBool(%v)) = %v", b, DecodeBool(v))
		}
	}
}

func TestExportedEncodeFloat(t *testing.T) {
	for _, f := range []float32{0, 1.5, -3.14, math.MaxFloat32} {
		v := EncodeFloat(f)
		if !IsFloat(v) {
			t.Errorf("IsFloat(EncodeFloat(%g)) = false", f)
		}
		if DecodeFloat(v) != f {
			t.Errorf("DecodeFloat(EncodeFloat(%g)) = %g", f, DecodeFloat(v))
		}
	}
}

func TestExportedEncodeLong(t *testing.T) {
	v := NewVM(1024, 256)
	// Inline range values report IsLong=true
	for _, n := range []int64{0, 1, -1, 42, -42} {
		val := EncodeLong(n, v)
		if !IsLong(val) {
			t.Errorf("IsLong(EncodeLong(%d)) = false", n)
		}
		if v.DecodeLong(val) != n {
			t.Errorf("DecodeLong(EncodeLong(%d)) = %d", n, v.DecodeLong(val))
		}
	}
	// Out-of-inline values are heap-allocated but still round-trip correctly
	for _, n := range []int64{int64(1 << 47), math.MaxInt64, math.MinInt64} {
		val := EncodeLong(n, v)
		if v.DecodeLong(val) != n {
			t.Errorf("DecodeLong(EncodeLong(%d)) = %d", n, v.DecodeLong(val))
		}
	}
}

func TestExportedEncodeULong(t *testing.T) {
	v := NewVM(1024, 256)
	for _, n := range []uint64{0, 1, 42, math.MaxUint64} {
		val := EncodeULong(n, v)
		if v.DecodeULong(val) != n {
			t.Errorf("DecodeULong(EncodeULong(%d)) = %d", n, v.DecodeULong(val))
		}
	}
}

func TestExportedEncodeDouble(t *testing.T) {
	v := NewVM(1024, 256)
	for _, f := range []float64{0, 1.5, -3.14, math.MaxFloat64, math.SmallestNonzeroFloat64} {
		val := EncodeDouble(f, v)
		if v.DecodeDouble(val) != f {
			t.Errorf("DecodeDouble(EncodeDouble(%g)) = %g", f, v.DecodeDouble(val))
		}
	}
}

func TestExportedIsNull(t *testing.T) {
	if !IsNull(EncodeHandle(InvalidHandle)) {
		t.Error("IsNull(InvalidHandle) should be true")
	}
	if IsNull(EncodeHandle(0)) {
		t.Error("IsNull(handle 0) should be false")
	}
}

func TestExportedIsInt(t *testing.T) {
	if !IsInt(EncodeInt(42)) {
		t.Error("IsInt(EncodeInt(42)) should be true")
	}
	if IsInt(EncodeBool(true)) {
		t.Error("IsInt(bool) should be false")
	}
}

func TestExportedIsBool(t *testing.T) {
	if !IsBool(EncodeBool(true)) {
		t.Error("IsBool(EncodeBool(true)) should be true")
	}
	if IsBool(EncodeInt(1)) {
		t.Error("IsBool(int) should be false")
	}
}

func TestExportedIsFloat(t *testing.T) {
	if !IsFloat(EncodeFloat(1.5)) {
		t.Error("IsFloat(EncodeFloat(1.5)) should be true")
	}
	if IsFloat(EncodeInt(1)) {
		t.Error("IsFloat(int) should be false")
	}
}

func TestExportedIsLong(t *testing.T) {
	v := NewVM(1024, 256)
	if !IsLong(EncodeLong(42, v)) {
		t.Error("IsLong(EncodeLong(42)) should be true")
	}
	if IsLong(EncodeInt(1)) {
		t.Error("IsLong(int) should be false")
	}
}

func TestExportedIsULong(t *testing.T) {
	v := NewVM(1024, 256)
	if !IsULong(EncodeULong(42, v)) {
		t.Error("IsULong(EncodeULong(42)) should be true")
	}
	if IsULong(EncodeInt(1)) {
		t.Error("IsULong(int) should be false")
	}
}

func TestExportedIsDouble(t *testing.T) {
	v := NewVM(1024, 256)
	if !IsDouble(EncodeDouble(3.14, v)) {
		t.Error("IsDouble(EncodeDouble(3.14)) should be true")
	}
	if IsDouble(EncodeInt(1)) {
		t.Error("IsDouble(int) should be false")
	}
}

func TestExportedIsString(t *testing.T) {
	v := NewVM(1024, 256)
	strVal := v.EncodeString("hello")
	if !IsString(strVal) {
		t.Error("IsString(EncodeString(\"hello\")) should be true")
	}
	if IsString(EncodeInt(1)) {
		t.Error("IsString(int) should be false")
	}
}

func TestExportedVMIsStringValue(t *testing.T) {
	v := NewVM(1024, 256)
	large := v.EncodeString(strings.Repeat("x", 257))
	if IsString(large) {
		t.Error("IsString(large string) should be false without VM memory")
	}
	if !v.IsStringValue(large) {
		t.Error("VM IsStringValue(large string) should be true")
	}
	if v.IsStringValue(EncodeInt(1)) {
		t.Error("VM IsStringValue(int) should be false")
	}
}

func TestExportedIsHandle(t *testing.T) {
	if !IsHandle(EncodeHandle(0)) {
		t.Error("IsHandle(handle 0) should be true")
	}
	// InvalidHandle is encoded as null, not a pointer
	if IsHandle(EncodeHandle(InvalidHandle)) {
		t.Error("IsHandle(InvalidHandle) should be false (encoded as null)")
	}
	if IsHandle(EncodeInt(1)) {
		t.Error("IsHandle(int) should be false")
	}
}

func TestExportedDecodeHandle(t *testing.T) {
	if DecodeHandle(EncodeHandle(InvalidHandle)) != InvalidHandle {
		t.Error("DecodeHandle(InvalidHandle) should be InvalidHandle")
	}
	if DecodeHandle(EncodeHandle(0)) != 0 {
		t.Error("DecodeHandle(handle 0) should be 0")
	}
}

func TestOperandStackCapacityReflectsNewVM(t *testing.T) {
	v := NewVM(1024, 384)
	if got := v.OperandStackCapacity(); got != 384 {
		t.Errorf("OperandStackCapacity() = %d, want 384", got)
	}
}

func TestExportedEncodeAndDecodeString(t *testing.T) {
	v := NewVM(1024, 256)
	for _, s := range []string{"", "hello", "world", "a longer string for testing"} {
		encoded := v.EncodeString(s)
		if !IsString(encoded) {
			t.Errorf("IsString(EncodeString(%q)) = false", s)
		}
		if decoded := v.DecodeString(encoded); decoded != s {
			t.Errorf("DecodeString(EncodeString(%q)) = %q", s, decoded)
		}
	}
}

func TestExportedFunctionRegistry(t *testing.T) {
	v := NewVM(1024, 256)
	fr := v.FuncReg()
	fr.RegisterFunction(&functionDef{name: "add", body: &nativeFunctionBody{fn: func(v *vm, args []value) value { return encodeInt(0) }}})

	if !fr.HasFunction("add") {
		t.Fatal("expected HasFunction(add) to be true")
	}
	if fr.HasFunction("missing") {
		t.Fatal("expected HasFunction(missing) to be false")
	}
	if got := fr.GetFunction("add"); got == nil || got.name != "add" {
		t.Fatalf("expected GetFunction(add) to return def, got %v", got)
	}
	if got := fr.GetFunction("missing"); got != nil {
		t.Fatalf("expected GetFunction(missing) to be nil, got %v", got)
	}
}

func TestExportedGetStructAndFields(t *testing.T) {
	v := NewVM(1024, 256)
	sr := v.StructReg()
	sr.RegisterStruct("Point", []FieldDef{NewFieldDef("x", 1, 0), NewFieldDef("y", 1, 0)})

	sd := sr.GetStruct("Point")
	if sd == nil {
		t.Fatal("expected GetStruct(Point) to return struct def")
	}
	fields := sd.Fields()
	if len(fields) != 2 || fields[0].name != "x" || fields[1].name != "y" {
		t.Fatalf("unexpected fields: %+v", fields)
	}
	if sr.GetStruct("Missing") != nil {
		t.Fatal("expected GetStruct(Missing) to be nil")
	}
}

func TestExportedNewStructInstance(t *testing.T) {
	v := NewVM(1024, 256)
	sr := v.StructReg()
	sr.RegisterStruct("Pair", []FieldDef{NewFieldDef("a", 1, 0), NewFieldDef("b", 1, 0)})

	h := v.NewStructInstance("Pair", []Value{EncodeInt(1), EncodeInt(2)})
	if h == InvalidHandle {
		t.Fatal("expected valid handle for struct instance")
	}
	if v.NewStructInstance("Missing", nil) != InvalidHandle {
		t.Fatal("expected InvalidHandle for missing struct")
	}
}

func TestExportedGetClassByName(t *testing.T) {
	v := NewVM(1024, 256)
	cr := v.ClassReg()
	c := NewClass(1, "Player", nil)
	cr.RegisterClass(c)

	if got := cr.GetClassByName("Player"); got == nil {
		t.Fatal("expected GetClassByName(Player) to return class")
	}
	if cr.GetClassByName("Missing") != nil {
		t.Fatal("expected GetClassByName(Missing) to be nil")
	}
}

func TestExportedMemoryAt(t *testing.T) {
	v := NewVM(1024, 256)
	if v.MemoryAt(0) != 0 {
		t.Fatal("expected initial memory to be zero")
	}
}

func TestExportedHeapHeaderChecks(t *testing.T) {
	v := NewVM(1024, 256)
	if v.IsLongHeapHeader(0) {
		t.Error("IsLongHeapHeader(0) should be false")
	}
	if v.IsULongHeapHeader(0) {
		t.Error("IsULongHeapHeader(0) should be false")
	}
	if v.IsLargeStringHeader(0) {
		t.Error("IsLargeStringHeader(0) should be false")
	}
	if !v.IsLongHeapHeader(encodeLongHeapHeader()) {
		t.Error("IsLongHeapHeader should match encoded long header")
	}
	if !v.IsULongHeapHeader(encodeULongHeapHeader()) {
		t.Error("IsULongHeapHeader should match encoded ulong header")
	}
	if !v.IsLargeStringHeader(encodeLargeStringHeader()) {
		t.Error("IsLargeStringHeader should match encoded large string header")
	}
}
