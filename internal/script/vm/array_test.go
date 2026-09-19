package vm

import "testing"

func TestArraySmall(t *testing.T) {
	v := newVM(8192, 256)

	arr := v.newArray(typeInt, 4)
	if v.getArrayLength(arr) != 4 {
		t.Errorf("length = %d, want 4", v.getArrayLength(arr))
	}

	v.setArrayElement(arr, 0, encodeInt(10))
	v.setArrayElement(arr, 1, encodeInt(20))
	v.setArrayElement(arr, 2, encodeInt(30))
	v.setArrayElement(arr, 3, encodeInt(40))

	for i, want := range []int32{10, 20, 30, 40} {
		got := v.getArrayElement(arr, i).decodeInt()
		if got != want {
			t.Errorf("arr[%d] = %d, want %d", i, got, want)
		}
	}
}

func TestArrayPush(t *testing.T) {
	v := newVM(8192, 256)

	arr := v.newArray(typeInt, 0)

	for i := int32(0); i < 10; i++ {
		v.arrayPush(arr, encodeInt(i))
	}

	if v.getArrayLength(arr) != 10 {
		t.Errorf("length = %d, want 10", v.getArrayLength(arr))
	}

	for i := int32(0); i < 10; i++ {
		got := v.getArrayElement(arr, int(i)).decodeInt()
		if got != i {
			t.Errorf("arr[%d] = %d, want %d", i, got, i)
		}
	}
}

// --- Array edge case tests ---

func TestArrayZeroLengthDefaultsToCapacity4(t *testing.T) {
	v := newVM(8192, 256)

	arr := v.newArray(typeInt, 0)
	// Initial capacity should be 4 even for length 0.
	arrIdx := v.getMemoryIndex(arr)
	capacity := int(v.memory[arrIdx+2])
	if capacity < 4 {
		t.Errorf("initial capacity = %d, want >= 4", capacity)
	}
}

func TestArrayGrowOnPush(t *testing.T) {
	v := newVM(8192, 256)

	arr := v.newArray(typeInt, 2) // capacity = 2
	// Fill up capacity.
	v.setArrayElement(arr, 0, encodeInt(1))
	v.setArrayElement(arr, 1, encodeInt(2))
	// Push beyond capacity should trigger grow.
	v.arrayPush(arr, encodeInt(3))

	if v.getArrayLength(arr) != 3 {
		t.Errorf("length after grow = %d, want 3", v.getArrayLength(arr))
	}
	// Old elements should survive the grow.
	if v.getArrayElement(arr, 0).decodeInt() != 1 {
		t.Error("element 0 lost after grow")
	}
	if v.getArrayElement(arr, 1).decodeInt() != 2 {
		t.Error("element 1 lost after grow")
	}
	if v.getArrayElement(arr, 2).decodeInt() != 3 {
		t.Error("element 2 incorrect after grow")
	}
}

func TestArrayOfPointers(t *testing.T) {
	v := newVM(8192, 256)

	sid, _ := v.structRegistry.register("S", []fieldDef{
		{name: "v", typeID: typeInt},
	})

	s1 := v.newStruct(sid, []value{encodeInt(10)})
	s2 := v.newStruct(sid, []value{encodeInt(20)})

	arr := v.newArray(typeAny, 2)
	v.setArrayElement(arr, 0, encodeHandle(s1))
	v.setArrayElement(arr, 1, encodeHandle(s2))

	// Access via array → handle → struct.
	elem0 := v.getArrayElement(arr, 0)
	if !elem0.isPointer() {
		t.Fatal("array element 0 should be a pointer")
	}
	h0 := elem0.decodeHandle()
	val0 := v.getStructFieldByName(h0, "v")
	if val0.decodeInt() != 10 {
		t.Errorf("struct[0].v = %d, want 10", val0.decodeInt())
	}

	elem1 := v.getArrayElement(arr, 1)
	h1 := elem1.decodeHandle()
	val1 := v.getStructFieldByName(h1, "v")
	if val1.decodeInt() != 20 {
		t.Errorf("struct[1].v = %d, want 20", val1.decodeInt())
	}
}

func TestArrayElementOverwrite(t *testing.T) {
	v := newVM(8192, 256)

	arr := v.newArray(typeInt, 2)
	v.setArrayElement(arr, 0, encodeInt(100))
	v.setArrayElement(arr, 1, encodeInt(200))

	// Overwrite element 0.
	v.setArrayElement(arr, 0, encodeInt(999))
	if v.getArrayElement(arr, 0).decodeInt() != 999 {
		t.Error("overwrite of element 0 failed")
	}
	if v.getArrayElement(arr, 1).decodeInt() != 200 {
		t.Error("element 1 should be unaffected")
	}
}

func TestArrayBoundsCheck(t *testing.T) {
	v := newVM(8192, 256)

	arr := v.newArray(typeInt, 2)

	// Positive out-of-bounds.
	defer func() {
		r := recover()
		if r == nil {
			t.Error("expected panic on out-of-bounds read")
		}
	}()
	v.getArrayElement(arr, 5)
}

func TestArrayBoundsCheckNegative(t *testing.T) {
	v := newVM(8192, 256)

	arr := v.newArray(typeInt, 2)

	defer func() {
		r := recover()
		if r == nil {
			t.Error("expected panic on negative index")
		}
	}()
	v.getArrayElement(arr, -1)
}

func TestArrayPushManyTriggersMultipleGrows(t *testing.T) {
	v := newVM(16384, 256)

	arr := v.newArray(typeInt, 0)

	// Push 100 elements — should trigger multiple grows.
	for i := int32(0); i < 100; i++ {
		v.arrayPush(arr, encodeInt(i))
	}

	if v.getArrayLength(arr) != 100 {
		t.Errorf("length = %d, want 100", v.getArrayLength(arr))
	}

	// Verify all elements.
	for i := int32(0); i < 100; i++ {
		got := v.getArrayElement(arr, int(i)).decodeInt()
		if got != i {
			t.Errorf("arr[%d] = %d, want %d", i, got, i)
		}
	}
}

func TestArrayOfStrings(t *testing.T) {
	v := newVM(8192, 256)

	arr := v.newArray(typeString, 3)
	v.setArrayElement(arr, 0, v.encodeString("hello"))
	v.setArrayElement(arr, 1, v.encodeString("world"))
	v.setArrayElement(arr, 2, v.encodeString("!"))

	if v.decodeString(v.getArrayElement(arr, 0)) != "hello" {
		t.Error("string array element 0 incorrect")
	}
	if v.decodeString(v.getArrayElement(arr, 1)) != "world" {
		t.Error("string array element 1 incorrect")
	}
	if v.decodeString(v.getArrayElement(arr, 2)) != "!" {
		t.Error("string array element 2 incorrect")
	}
}

func TestArrayGrowthRetainsObjectHandlesUnderGCPressure(t *testing.T) {
	v := newVM(65536, 256)
	cls := newClass(1, "Node", nil)
	cls.addField("value", typeInt)
	v.classRegistry.register(cls)

	arr := v.newArray(typeAny, 0)
	release := v.addTemporaryRoot(encodeHandle(arr))
	defer release()

	for i := int32(0); i < 64; i++ {
		obj := v.createObject(1)
		v.setField(obj, "value", encodeInt(i))
		v.arrayPush(arr, encodeHandle(obj))
	}
	for i := 0; i < 256; i++ {
		_ = v.newMap(typeString, typeInt, 8)
	}
	if v.getArrayLength(arr) != 64 {
		t.Fatalf("array length = %d, want 64", v.getArrayLength(arr))
	}
	for i := int32(0); i < 64; i++ {
		h := v.getArrayElement(arr, int(i)).decodeHandle()
		if got := v.getField(h, "value").decodeInt(); got != i {
			t.Fatalf("object at %d has value %d, want %d", i, got, i)
		}
	}
}

func TestArrayOfMapsSurvivesGrowthAndGCPressure(t *testing.T) {
	v := newVM(65536, 256)
	arr := v.newArray(typeAny, 0)
	release := v.addTemporaryRoot(encodeHandle(arr))
	defer release()

	for i := int32(0); i < 32; i++ {
		m := v.newMap(typeString, typeInt, 0)
		v.mapSet(m, v.encodeString("value"), encodeInt(i))
		v.arrayPush(arr, encodeHandle(m))
	}
	for i := 0; i < 128; i++ {
		_ = v.newArray(typeAny, 8)
	}
	if v.getArrayLength(arr) != 32 {
		t.Fatalf("array length = %d, want 32", v.getArrayLength(arr))
	}
	for i := int32(0); i < 32; i++ {
		h := v.getArrayElement(arr, int(i)).decodeHandle()
		val, ok := v.mapGet(h, v.encodeString("value"))
		if !ok || val.decodeInt() != i {
			t.Fatalf("map at %d has value %d, %v; want %d, true", i, val.decodeInt(), ok, i)
		}
	}
}
