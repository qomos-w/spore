package vm

import (
	"fmt"
	"math"
	"testing"
)

// --- VM integration scenario tests ---

func TestObjectWithArrayField(t *testing.T) {
	v := newVM(8192, 256)

	class := newClass(1, "Container", nil)
	class.addField("items", typeArray)
	class.addField("count", typeInt)
	v.classRegistry.register(class)

	obj := v.createObject(1)

	arr := v.newArray(typeInt, 3)
	v.setArrayElement(arr, 0, encodeInt(10))
	v.setArrayElement(arr, 1, encodeInt(20))
	v.setArrayElement(arr, 2, encodeInt(30))

	v.setField(obj, "items", encodeHandle(arr))
	v.setField(obj, "count", encodeInt(3))

	// Navigate: object → field → array element.
	itemsVal := v.getField(obj, "items")
	if !itemsVal.isPointer() {
		t.Fatal("items field should be a pointer")
	}
	itemsHandle := itemsVal.decodeHandle()

	elem := v.getArrayElement(itemsHandle, 1)
	if elem.decodeInt() != 20 {
		t.Errorf("items[1] = %d, want 20", elem.decodeInt())
	}

	countVal := v.getField(obj, "count")
	if countVal.decodeInt() != 3 {
		t.Errorf("count = %d, want 3", countVal.decodeInt())
	}
}

func TestMapWithStructValues(t *testing.T) {
	v := newVM(16384, 256)

	sid, _ := v.structRegistry.register("Entry", []fieldDef{
		{name: "key", typeID: typeString},
		{name: "value", typeID: typeInt},
	})

	m := v.newMap(typeString, typeAny, 0)

	// Create structs and store them as map values.
	s1 := v.newStruct(sid, []value{v.encodeString("x"), encodeInt(100)})
	s2 := v.newStruct(sid, []value{v.encodeString("y"), encodeInt(200)})

	v.mapSet(m, v.encodeString("first"), encodeHandle(s1))
	v.mapSet(m, v.encodeString("second"), encodeHandle(s2))

	// Navigate: map → struct field.
	val, ok := v.mapGet(m, v.encodeString("first"))
	if !ok || !val.isPointer() {
		t.Fatal("map value should be a pointer")
	}
	h := val.decodeHandle()
	keyField := v.getStructFieldByName(h, "key")
	valField := v.getStructFieldByName(h, "value")

	if v.decodeString(keyField) != "x" {
		t.Errorf("struct.key = %q, want 'x'", v.decodeString(keyField))
	}
	if valField.decodeInt() != 100 {
		t.Errorf("struct.value = %d, want 100", valField.decodeInt())
	}
}

func TestGCWithClassInheritance(t *testing.T) {
	v := newVM(4096, 256)

	parent := newClass(1, "Base", nil)
	parent.addField("id", typeInt)
	v.classRegistry.register(parent)

	child := newClass(2, "Derived", parent)
	child.addField("extra", typeString)
	v.classRegistry.register(child)

	// Create child object with inherited + own fields.
	obj := v.createObject(2)
	v.setField(obj, "id", encodeInt(42))
	v.setField(obj, "extra", v.encodeString("hello"))
	v.push(encodeHandle(obj))

	v.gc.collect()

	// Verify both inherited and own fields survive GC.
	if v.getField(obj, "id").decodeInt() != 42 {
		t.Error("inherited field 'id' lost after GC")
	}
	if v.decodeString(v.getField(obj, "extra")) != "hello" {
		t.Error("own field 'extra' lost after GC")
	}
}

func TestGCWithObjectInArrayInMap(t *testing.T) {
	v := newVM(16384, 256)

	class := newClass(1, "Node", nil)
	class.addField("val", typeInt)
	v.classRegistry.register(class)

	// Create an object.
	obj := v.createObject(1)
	v.setField(obj, "val", encodeInt(77))

	// Put object in an array.
	arr := v.newArray(typeAny, 1)
	v.setArrayElement(arr, 0, encodeHandle(obj))

	// Put array in a map.
	m := v.newMap(typeString, typeAny, 0)
	v.mapSet(m, v.encodeString("nodes"), encodeHandle(arr))
	v.push(encodeHandle(m))

	v.gc.collect()

	// Verify the deep chain survived: map → array → object → field.
	mapArrVal, ok := v.mapGet(m, v.encodeString("nodes"))
	if !ok {
		t.Fatal("map key 'nodes' not found after GC")
	}
	arrHandle := mapArrVal.decodeHandle()
	objVal := v.getArrayElement(arrHandle, 0)
	objHandle := objVal.decodeHandle()
	fieldVal := v.getField(objHandle, "val")
	if fieldVal.decodeInt() != 77 {
		t.Errorf("nested value after GC = %d, want 77", fieldVal.decodeInt())
	}
}

func TestVMStackPushPopSequence(t *testing.T) {
	v := newVM(4096, 256)

	// Push multiple types.
	v.push(encodeInt(1))
	v.push(encodeBool(true))
	v.push(v.encodeString("hello"))
	v.push(encodeFloat(3.14))

	if v.sp != 4 {
		t.Errorf("sp = %d, want 4", v.sp)
	}

	// Pop in reverse order.
	f := v.pop()
	if !f.isFloat() {
		t.Error("expected float on top")
	}

	s := v.pop()
	if !s.isString() {
		t.Error("expected string next")
	}

	b := v.pop()
	if !b.isBool() {
		t.Error("expected bool next")
	}

	i := v.pop()
	if !i.isInt() {
		t.Error("expected int on bottom")
	}
}

func TestLongInlineAndHeap(t *testing.T) {
	v := newVM(4096, 256)

	// Inline long (within 47-bit range).
	inlineVal := v.encodeLong(1<<46 - 1)
	if !inlineVal.isLong() {
		t.Error("inline long should have tagLong")
	}
	if v.decodeLong(inlineVal) != 1<<46-1 {
		t.Errorf("inline long = %d, want %d", v.decodeLong(inlineVal), 1<<46-1)
	}

	// Heap long (beyond 47-bit range).
	bigVal := int64(1 << 48)
	heapVal := v.encodeLong(bigVal)
	if v.decodeLong(heapVal) != bigVal {
		t.Errorf("heap long = %d, want %d", v.decodeLong(heapVal), bigVal)
	}
}

func TestULongInlineAndHeap(t *testing.T) {
	v := newVM(4096, 256)

	// Inline ulong (within 48-bit range).
	inlineVal := v.encodeULong(1<<47 - 1)
	if v.decodeULong(inlineVal) != 1<<47-1 {
		t.Errorf("inline ulong = %d, want %d", v.decodeULong(inlineVal), 1<<47-1)
	}

	// Heap ulong (beyond 48-bit range).
	bigVal := uint64(1 << 49)
	heapVal := v.encodeULong(bigVal)
	if v.decodeULong(heapVal) != bigVal {
		t.Errorf("heap ulong = %d, want %d", v.decodeULong(heapVal), bigVal)
	}
}

func TestDoubleRoundTrip(t *testing.T) {
	v := newVM(4096, 256)

	tests := []float64{0, 1.0, -1.0, 3.14159265358979, math.MaxFloat64, math.SmallestNonzeroFloat64, math.Inf(1), math.Inf(-1)}
	for _, f := range tests {
		encoded := v.encodeDouble(f)
		decoded := v.decodeDouble(encoded)
		if decoded != f {
			t.Errorf("double round-trip: %g → %g", f, decoded)
		}
	}
}

func TestDoubleNaNIsBoxed(t *testing.T) {
	v := newVM(4096, 256)

	// NaN is stored as raw IEEE-754 bits. The quiet NaN bit pattern
	// (0x7FF8000000000000) does NOT have the top 8 bits as 0xFF,
	// so it is not detected by isBoxed(). However, isDouble() returns
	// true for both regular doubles AND the boxed double-NaN sentinel.
	encoded := v.encodeDouble(math.NaN())
	if !encoded.isDouble() {
		t.Error("NaN should be recognized as double")
	}
	// NaN is not equal to itself.
	decoded := v.decodeDouble(encoded)
	if !math.IsNaN(decoded) {
		t.Error("NaN round-trip should produce NaN")
	}
}

func TestStructMaxFieldsValidation(t *testing.T) {
	v := newVM(4096, 256)

	// 32 fields is the maximum.
	fields := make([]fieldDef, maxStructFields)
	for i := range fields {
		fields[i] = fieldDef{name: fmt.Sprintf("f%d", i), typeID: typeInt}
	}
	sid, err := v.structRegistry.register("MaxFields", fields)
	if err != nil {
		t.Fatalf("register with %d fields should succeed: %v", maxStructFields, err)
	}

	values := make([]value, maxStructFields)
	for i := range values {
		values[i] = encodeInt(int32(i))
	}
	h := v.newStruct(sid, values)

	// Verify first and last field.
	first := v.getStructFieldByName(h, "f0")
	if first.decodeInt() != 0 {
		t.Error("first field incorrect")
	}
	last := v.getStructFieldByName(h, fmt.Sprintf("f%d", maxStructFields-1))
	if last.decodeInt() != int32(maxStructFields-1) {
		t.Error("last field incorrect")
	}
}

func TestStructRegistryDuplicateNameIdempotent(t *testing.T) {
	sr := newStructRegistry()

	id1, _ := sr.register("Point", []fieldDef{{name: "x", typeID: typeInt}})
	id2, _ := sr.register("Point", []fieldDef{{name: "x", typeID: typeInt}})

	if id1 != id2 {
		t.Errorf("duplicate name should return same ID: id1=%d, id2=%d", id1, id2)
	}
}

func TestStructRegistryGetByName(t *testing.T) {
	sr := newStructRegistry()
	sr.register("Vec3", []fieldDef{
		{name: "x", typeID: typeFloat},
		{name: "y", typeID: typeFloat},
		{name: "z", typeID: typeFloat},
	})

	sd := sr.getByName("Vec3")
	if sd == nil {
		t.Fatal("getByName should find registered struct")
	}
	if sd.name != "Vec3" {
		t.Errorf("struct name = %q, want 'Vec3'", sd.name)
	}
	if sd.fieldCount != 3 {
		t.Errorf("fieldCount = %d, want 3", sd.fieldCount)
	}

	if sr.getByName("Missing") != nil {
		t.Error("getByName should return nil for unregistered struct")
	}
}

func TestMemoryAllocFromFreeList(t *testing.T) {
	v := newVM(4096, 256)

	// Allocate and then make unreachable to populate free list.
	sid, _ := v.structRegistry.register("S", []fieldDef{{name: "a", typeID: typeInt}})
	_ = v.newStruct(sid, []value{encodeInt(1)})
	_ = v.newStruct(sid, []value{encodeInt(2)})
	_ = v.newStruct(sid, []value{encodeInt(3)})

	// Collect to populate free list.
	v.gc.collect()
	freeListLen := len(v.freeList)
	if freeListLen == 0 {
		t.Fatal("expected free list entries")
	}

	// Allocate again — should reuse free list slots.
	h := v.newStruct(sid, []value{encodeInt(99)})
	val := v.getStructFieldByName(h, "a")
	if val.decodeInt() != 99 {
		t.Errorf("reused slot value = %d, want 99", val.decodeInt())
	}
}

func TestMemoryPoolExhaustion(t *testing.T) {
	v := newVM(128, 16) // Very small pool.

	sid, _ := v.structRegistry.register("S", []fieldDef{{name: "a", typeID: typeInt}})

	// Keep one reachable.
	h := v.newStruct(sid, []value{encodeInt(1)})
	v.push(encodeHandle(h))

	// Allocate until exhaustion — should panic.
	defer func() {
		r := recover()
		if r == nil {
			t.Error("expected panic on out of memory")
		}
	}()
	for i := 0; i < 100; i++ {
		s := v.newStruct(sid, []value{encodeInt(int32(i))})
		v.push(encodeHandle(s))
	}
}

func TestStackOverflow(t *testing.T) {
	v := newVM(4096, 8) // Very small stack.

	defer func() {
		r := recover()
		if r == nil {
			t.Error("expected panic on stack overflow")
		}
	}()
	for i := 0; i < 20; i++ {
		v.push(encodeInt(int32(i)))
	}
}

func TestStackUnderflow(t *testing.T) {
	v := newVM(4096, 256)

	defer func() {
		r := recover()
		if r == nil {
			t.Error("expected panic on stack underflow")
		}
	}()
	v.pop() // stack is empty
}

func TestValueNullEncoding(t *testing.T) {
	null := encodeHandle(invalidHandle)
	if !null.isNull() {
		t.Error("invalidHandle should encode as null")
	}
	if null.isPointer() {
		t.Error("null should not be a pointer")
	}
	if null.decodeHandle() != invalidHandle {
		t.Error("decoding null handle should return invalidHandle")
	}
}

func TestHandleZeroIsNotInvalid(t *testing.T) {
	v := encodeHandle(0)
	if v.isNull() {
		t.Error("handle 0 should not be null")
	}
	if !v.isPointer() {
		t.Error("handle 0 should be a pointer")
	}
	if v.decodeHandle() != 0 {
		t.Error("handle 0 round-trip failed")
	}
}

func TestEncodeDecodeByteShortUShort(t *testing.T) {
	// Byte
	bv := encodeByte(255)
	if bv.decodeByte() != 255 {
		t.Errorf("byte = %d, want 255", bv.decodeByte())
	}
	// Short
	sv := encodeShort(-1000)
	if sv.decodeShort() != -1000 {
		t.Errorf("short = %d, want -1000", sv.decodeShort())
	}
	// Unsigned short
	usv := encodeUShort(60000)
	if usv.decodeUShort() != 60000 {
		t.Errorf("ushort = %d, want 60000", usv.decodeUShort())
	}
}

func TestDumpMemoryNoPanic(t *testing.T) {
	v := newVM(4096, 256)

	sid, _ := v.structRegistry.register("S", []fieldDef{{name: "x", typeID: typeInt}})
	h := v.newStruct(sid, []value{encodeInt(42)})
	v.push(encodeHandle(h))

	// dumpMemory should not panic.
	result := v.dumpMemory()
	if len(result) == 0 {
		t.Error("dumpMemory should return non-empty string")
	}
}

func TestExportedAPIRoundTrip(t *testing.T) {
	v := NewVM(8192, 256)

	// Create class via exported API.
	cls := NewClass(1, "Point", nil)
	cls.AddField("x", typeInt)
	cls.AddField("y", typeInt)
	cls.AddMethod("magnitude", func(v *VM, receiver Handle, args []Value) Value {
		x := v.GetField(receiver, "x").decodeInt()
		y := v.GetField(receiver, "y").decodeInt()
		return EncodeInt(x*x + y*y)
	})
	cls.BuildVTable()
	v.ClassReg().RegisterClass(cls)

	obj := v.CreateObject(1)
	v.SetField(obj, "x", EncodeInt(3))
	v.SetField(obj, "y", EncodeInt(4))

	result := v.CallMethod(obj, "magnitude", nil)
	if DecodeInt(result) != 25 {
		t.Errorf("magnitude = %d, want 25", DecodeInt(result))
	}
}

func TestExportedStructAPI(t *testing.T) {
	v := NewVM(8192, 256)

	v.StructReg().RegisterStruct("Color", []FieldDef{
		NewFieldDef("r", typeInt, 0),
		NewFieldDef("g", typeInt, 0),
		NewFieldDef("b", typeInt, 0),
	})

	h := v.NewStructInstance("Color", []Value{EncodeInt(255), EncodeInt(128), EncodeInt(0)})
	if h == InvalidHandle {
		t.Fatal("NewStructInstance returned invalid handle")
	}

	r := v.GetField(h, "r")
	g := v.GetField(h, "g")
	b := v.GetField(h, "b")

	if DecodeInt(r) != 255 || DecodeInt(g) != 128 || DecodeInt(b) != 0 {
		t.Errorf("Color = (%d,%d,%d), want (255,128,0)", DecodeInt(r), DecodeInt(g), DecodeInt(b))
	}
}

func TestExportedFunctionAPI(t *testing.T) {
	v := NewVM(4096, 256)

	body := NewBytecodeFunctionBody(func(v *VM, args []Value) Value {
		return EncodeInt(DecodeInt(args[0]) + DecodeInt(args[1]))
	})
	fnDef := NewFunctionDef("add",
		[]ParameterDef{NewParameterDef("a", typeInt), NewParameterDef("b", typeInt)},
		typeInt, body)

	v.FuncReg().RegisterFunction(fnDef)
	retrieved := v.FuncReg().GetFunction("add")
	if retrieved == nil {
		t.Fatal("GetFunction should find registered function")
	}

	result := retrieved.ExecuteBody(v, []Value{EncodeInt(10), EncodeInt(20)})
	if DecodeInt(result) != 30 {
		t.Errorf("add(10,20) = %d, want 30", DecodeInt(result))
	}
}

func TestObjectArrayObjectChurnSurvivesMultipleGC(t *testing.T) {
	v := newVM(32768, 256)

	item := newClass(1, "Item", nil)
	item.addField("value", typeInt)
	item.computeFieldOffsets()
	item.buildVTable()
	v.classRegistry.register(item)

	owner := newClass(2, "Owner", nil)
	owner.addField("items", typeAny)
	owner.computeFieldOffsets()
	owner.buildVTable()
	v.classRegistry.register(owner)

	ownerObj := v.createObject(2)
	arr := v.newArray(typeAny, 0)
	for i := int32(0); i < 16; i++ {
		it := v.createObject(1)
		v.setField(it, "value", encodeInt(i*7))
		v.arrayPush(arr, encodeHandle(it))
	}
	v.setField(ownerObj, "items", encodeHandle(arr))
	release := v.addTemporaryRoot(encodeHandle(ownerObj))
	defer release()

	for round := 0; round < 4; round++ {
		for j := 0; j < 64; j++ {
			_ = v.newMap(typeString, typeInt, 4)
			_ = v.newArray(typeAny, 4)
		}
		v.gc.collect()
	}

	arrField := v.getField(ownerObj, "items")
	arrHandle := arrField.decodeHandle()
	if got := v.getArrayLength(arrHandle); got != 16 {
		t.Fatalf("array length after GC = %d, want 16", got)
	}
	for i := int32(0); i < 16; i++ {
		h := v.getArrayElement(arrHandle, int(i)).decodeHandle()
		got := v.getField(h, "value").decodeInt()
		if got != i*7 {
			t.Errorf("item %d value after GC = %d, want %d", i, got, i*7)
		}
	}
}
