package vm

import "testing"

func TestMapBasic(t *testing.T) {
	v := newVM(16384, 256)

	m := v.newMap(typeString, typeInt, 0)
	if v.mapSize(m) != 0 {
		t.Errorf("initial size = %d, want 0", v.mapSize(m))
	}

	// Put.
	v.mapSet(m, v.encodeString("a"), encodeInt(1))
	v.mapSet(m, v.encodeString("b"), encodeInt(2))
	v.mapSet(m, v.encodeString("c"), encodeInt(3))

	if v.mapSize(m) != 3 {
		t.Errorf("size after 3 puts = %d, want 3", v.mapSize(m))
	}

	// Get.
	val, ok := v.mapGet(m, v.encodeString("a"))
	if !ok || val.decodeInt() != 1 {
		t.Errorf("get(a) = %d, %v; want 1, true", val.decodeInt(), ok)
	}

	val, ok = v.mapGet(m, v.encodeString("b"))
	if !ok || val.decodeInt() != 2 {
		t.Errorf("get(b) = %d, %v; want 2, true", val.decodeInt(), ok)
	}

	// Missing key.
	_, ok = v.mapGet(m, v.encodeString("z"))
	if ok {
		t.Error("get(z) should return false")
	}
}

func TestMapDelete(t *testing.T) {
	v := newVM(16384, 256)

	m := v.newMap(typeString, typeInt, 0)
	v.mapSet(m, v.encodeString("x"), encodeInt(10))
	v.mapSet(m, v.encodeString("y"), encodeInt(20))

	if !v.mapDelete(m, v.encodeString("x")) {
		t.Error("delete(x) should return true")
	}
	if v.mapSize(m) != 1 {
		t.Errorf("size after delete = %d, want 1", v.mapSize(m))
	}

	_, ok := v.mapGet(m, v.encodeString("x"))
	if ok {
		t.Error("get(x) after delete should return false")
	}
}

func TestMapOverwrite(t *testing.T) {
	v := newVM(16384, 256)

	m := v.newMap(typeString, typeInt, 0)
	v.mapSet(m, v.encodeString("key"), encodeInt(1))
	v.mapSet(m, v.encodeString("key"), encodeInt(99))

	if v.mapSize(m) != 1 {
		t.Errorf("size after overwrite = %d, want 1", v.mapSize(m))
	}

	val, ok := v.mapGet(m, v.encodeString("key"))
	if !ok || val.decodeInt() != 99 {
		t.Errorf("get(key) after overwrite = %d, want 99", val.decodeInt())
	}
}

func TestMapInsertionOrder(t *testing.T) {
	v := newVM(16384, 256)

	m := v.newMap(typeString, typeInt, 0)
	keys := []string{"first", "second", "third"}
	for i, k := range keys {
		v.mapSet(m, v.encodeString(k), encodeInt(int32(i)))
	}

	var order []string
	v.mapIterate(m, func(key, val value) bool {
		order = append(order, v.decodeString(key))
		return true
	})

	if len(order) != 3 {
		t.Fatalf("iteration count = %d, want 3", len(order))
	}
	for i, k := range keys {
		if order[i] != k {
			t.Errorf("order[%d] = %q, want %q", i, order[i], k)
		}
	}
}

// mapKeyAt walks the same insertion-order linked list that mapIterate
// traverses; it powers for-in over maps in the bytecode layer.
func TestMapKeyAtInsertionOrder(t *testing.T) {
	v := newVM(16384, 256)

	m := v.newMap(typeString, typeInt, 0)
	keys := []string{"first", "second", "third"}
	for i, k := range keys {
		v.mapSet(m, v.encodeString(k), encodeInt(int32(i)))
	}

	for i, want := range keys {
		key, ok := v.mapKeyAt(m, i)
		if !ok {
			t.Fatalf("mapKeyAt(%d) returned !ok", i)
		}
		if got := v.decodeString(key); got != want {
			t.Errorf("mapKeyAt(%d) = %q, want %q", i, got, want)
		}
	}
	if _, ok := v.mapKeyAt(m, 3); ok {
		t.Error("mapKeyAt(3) should return !ok for a 3-entry map")
	}
	if _, ok := v.mapKeyAt(m, -1); ok {
		t.Error("mapKeyAt(-1) should return !ok")
	}
}

// --- Map edge case tests ---

func TestMapIntegerKeys(t *testing.T) {
	v := newVM(16384, 256)

	m := v.newMap(typeInt, typeString, 0)
	v.mapSet(m, encodeInt(1), v.encodeString("one"))
	v.mapSet(m, encodeInt(2), v.encodeString("two"))
	v.mapSet(m, encodeInt(42), v.encodeString("answer"))

	if v.mapSize(m) != 3 {
		t.Errorf("size = %d, want 3", v.mapSize(m))
	}

	val, ok := v.mapGet(m, encodeInt(42))
	if !ok || v.decodeString(val) != "answer" {
		t.Errorf("get(42) = %q, %v; want 'answer', true", v.decodeString(val), ok)
	}

	_, ok = v.mapGet(m, encodeInt(99))
	if ok {
		t.Error("get(99) should return false")
	}
}

func TestMapObjectValues(t *testing.T) {
	v := newVM(16384, 256)

	class := newClass(1, "Item", nil)
	class.addField("price", typeInt)
	v.classRegistry.register(class)

	m := v.newMap(typeString, typeAny, 0)

	obj1 := v.createObject(1)
	v.setField(obj1, "price", encodeInt(100))
	v.mapSet(m, v.encodeString("apple"), encodeHandle(obj1))

	obj2 := v.createObject(1)
	v.setField(obj2, "price", encodeInt(200))
	v.mapSet(m, v.encodeString("banana"), encodeHandle(obj2))

	val, ok := v.mapGet(m, v.encodeString("apple"))
	if !ok || !val.isPointer() {
		t.Fatal("map value for apple should be a pointer")
	}
	h := val.decodeHandle()
	price := v.getField(h, "price")
	if price.decodeInt() != 100 {
		t.Errorf("apple price = %d, want 100", price.decodeInt())
	}
}

func TestMapDeleteNonExistent(t *testing.T) {
	v := newVM(16384, 256)

	m := v.newMap(typeString, typeInt, 0)
	v.mapSet(m, v.encodeString("a"), encodeInt(1))

	if v.mapDelete(m, v.encodeString("missing")) {
		t.Error("delete of missing key should return false")
	}
	if v.mapSize(m) != 1 {
		t.Errorf("size after failed delete = %d, want 1", v.mapSize(m))
	}
}

func TestMapDeleteAndReInsert(t *testing.T) {
	v := newVM(16384, 256)

	m := v.newMap(typeString, typeInt, 0)
	v.mapSet(m, v.encodeString("k"), encodeInt(1))
	v.mapDelete(m, v.encodeString("k"))

	if v.mapSize(m) != 0 {
		t.Errorf("size after delete = %d, want 0", v.mapSize(m))
	}

	// Re-insert the same key.
	v.mapSet(m, v.encodeString("k"), encodeInt(2))
	if v.mapSize(m) != 1 {
		t.Errorf("size after re-insert = %d, want 1", v.mapSize(m))
	}

	val, ok := v.mapGet(m, v.encodeString("k"))
	if !ok || val.decodeInt() != 2 {
		t.Errorf("re-inserted value = %d, want 2", val.decodeInt())
	}
}

func TestMapGrowOnLoadFactor(t *testing.T) {
	v := newVM(16384, 256)

	m := v.newMap(typeInt, typeInt, 0)
	mapIdx := v.getMemoryIndex(m)
	initialCapacity := int(v.memory[mapIdx+1])
	if initialCapacity != mapMinCapacity {
		t.Fatalf("initial capacity = %d, want %d", initialCapacity, mapMinCapacity)
	}

	for i := int32(0); i < 12; i++ {
		v.mapSet(m, encodeInt(i), encodeInt(i*10))
	}
	mapIdx = v.getMemoryIndex(m)
	if capacity := int(v.memory[mapIdx+1]); capacity != initialCapacity {
		t.Fatalf("capacity after 12 inserts = %d, want %d", capacity, initialCapacity)
	}

	v.mapSet(m, encodeInt(12), encodeInt(120))
	mapIdx = v.getMemoryIndex(m)
	if capacity := int(v.memory[mapIdx+1]); capacity != initialCapacity*2 {
		t.Fatalf("capacity after 13th insert = %d, want %d", capacity, initialCapacity*2)
	}

	if v.mapSize(m) != 13 {
		t.Errorf("size after growth = %d, want 13", v.mapSize(m))
	}
	for i := int32(0); i < 13; i++ {
		val, ok := v.mapGet(m, encodeInt(i))
		if !ok || val.decodeInt() != i*10 {
			t.Errorf("get(%d) = %d, %v; want %d, true", i, val.decodeInt(), ok, i*10)
		}
	}
}

func TestMapIterateEarlyBreak(t *testing.T) {
	v := newVM(16384, 256)

	m := v.newMap(typeString, typeInt, 0)
	v.mapSet(m, v.encodeString("a"), encodeInt(1))
	v.mapSet(m, v.encodeString("b"), encodeInt(2))
	v.mapSet(m, v.encodeString("c"), encodeInt(3))

	count := 0
	v.mapIterate(m, func(key, val value) bool {
		count++
		return count < 2 // stop after 2
	})

	if count != 2 {
		t.Errorf("iteration count = %d, want 2 (early break)", count)
	}
}

func TestMapEmptySize(t *testing.T) {
	v := newVM(16384, 256)

	m := v.newMap(typeInt, typeInt, 0)
	if v.mapSize(m) != 0 {
		t.Errorf("empty map size = %d, want 0", v.mapSize(m))
	}

	_, ok := v.mapGet(m, encodeInt(1))
	if ok {
		t.Error("get on empty map should return false")
	}
}

func TestMapDeleteAllThenReInsert(t *testing.T) {
	v := newVM(16384, 256)

	m := v.newMap(typeString, typeInt, 0)
	v.mapSet(m, v.encodeString("a"), encodeInt(1))
	v.mapSet(m, v.encodeString("b"), encodeInt(2))

	v.mapDelete(m, v.encodeString("a"))
	v.mapDelete(m, v.encodeString("b"))

	if v.mapSize(m) != 0 {
		t.Errorf("size after deleting all = %d, want 0", v.mapSize(m))
	}

	// Re-insert into map with tombstones.
	v.mapSet(m, v.encodeString("c"), encodeInt(3))
	if v.mapSize(m) != 1 {
		t.Errorf("size after re-insert = %d, want 1", v.mapSize(m))
	}
	val, ok := v.mapGet(m, v.encodeString("c"))
	if !ok || val.decodeInt() != 3 {
		t.Errorf("get(c) = %d, want 3", val.decodeInt())
	}
}

func TestMapDeleteAndReInsertAppendsToTail(t *testing.T) {
	v := newVM(16384, 256)

	m := v.newMap(typeString, typeInt, 0)
	v.mapSet(m, v.encodeString("a"), encodeInt(1))
	v.mapSet(m, v.encodeString("b"), encodeInt(2))
	v.mapSet(m, v.encodeString("c"), encodeInt(3))

	if !v.mapDelete(m, v.encodeString("b")) {
		t.Fatal("delete(b) should return true")
	}
	v.mapSet(m, v.encodeString("b"), encodeInt(20))

	if v.mapSize(m) != 3 {
		t.Fatalf("size after delete+reinsert = %d, want 3", v.mapSize(m))
	}

	var order []string
	v.mapIterate(m, func(key, val value) bool {
		order = append(order, v.decodeString(key))
		return true
	})

	want := []string{"a", "c", "b"}
	if len(order) != len(want) {
		t.Fatalf("iteration count = %d, want %d", len(order), len(want))
	}
	for i, k := range want {
		if order[i] != k {
			t.Errorf("order[%d] = %q, want %q", i, order[i], k)
		}
	}
}

func TestMapReinsertedTombstoneCountsTowardGrowth(t *testing.T) {
	v := newVM(16384, 256)

	m := v.newMap(typeInt, typeInt, 0)
	mapIdx := v.getMemoryIndex(m)
	initialCapacity := int(v.memory[mapIdx+1])

	for i := int32(0); i < 12; i++ {
		v.mapSet(m, encodeInt(i), encodeInt(i*10))
	}
	if !v.mapDelete(m, encodeInt(5)) {
		t.Fatal("delete(5) should return true")
	}
	v.mapSet(m, encodeInt(5), encodeInt(50))
	if v.mapSize(m) != 12 {
		t.Fatalf("size after tombstone reinsertion = %d, want 12", v.mapSize(m))
	}

	v.mapSet(m, encodeInt(12), encodeInt(120))
	mapIdx = v.getMemoryIndex(m)
	if capacity := int(v.memory[mapIdx+1]); capacity != initialCapacity*2 {
		t.Fatalf("capacity after growth = %d, want %d", capacity, initialCapacity*2)
	}
	for i := int32(0); i < 13; i++ {
		val, ok := v.mapGet(m, encodeInt(i))
		if !ok || val.decodeInt() != i*10 {
			t.Errorf("get(%d) = %d, %v; want %d, true", i, val.decodeInt(), ok, i*10)
		}
	}
}

func TestMapManyEntries(t *testing.T) {
	v := newVM(65536, 256)

	m := v.newMap(typeInt, typeInt, 0)

	const n = 100
	for i := int32(0); i < n; i++ {
		v.mapSet(m, encodeInt(i), encodeInt(i*i))
	}

	if v.mapSize(m) != n {
		t.Errorf("size = %d, want %d", v.mapSize(m), n)
	}
	for i := int32(0); i < n; i++ {
		val, ok := v.mapGet(m, encodeInt(i))
		if !ok || val.decodeInt() != i*i {
			t.Errorf("get(%d) = %d, want %d", i, val.decodeInt(), i*i)
		}
	}
}

func TestMapChurnIterationStableAfterGCPressure(t *testing.T) {
	v := newVM(65536, 256)
	m := v.newMap(typeString, typeInt, 0)
	release := v.addTemporaryRoot(encodeHandle(m))
	defer release()

	for i := int32(0); i < 64; i++ {
		v.mapSet(m, v.encodeString(string(rune('a'+(i%26)))+v.decodeString(v.encodeString("_"))+string(rune('A'+(i%26)))), encodeInt(i))
	}
	for i := int32(0); i < 64; i += 2 {
		v.mapDelete(m, v.encodeString(string(rune('a'+(i%26)))+v.decodeString(v.encodeString("_"))+string(rune('A'+(i%26)))))
	}
	for i := int32(64); i < 96; i++ {
		v.mapSet(m, v.encodeString(string(rune('a'+(i%26)))+v.decodeString(v.encodeString("_"))+string(rune('A'+(i%26)))), encodeInt(i))
	}
	for i := 0; i < 128; i++ {
		_ = v.newArray(typeAny, 8)
	}

	seen := 0
	v.mapIterate(m, func(key, val value) bool {
		if _, ok := v.mapGet(m, key); !ok {
			t.Fatalf("iterated key %q was not readable", v.decodeString(key))
		}
		seen++
		return true
	})
	if seen != v.mapSize(m) {
		t.Fatalf("iteration count %d != map size %d", seen, v.mapSize(m))
	}
}

func TestMapArrayValues(t *testing.T) {
	v := newVM(16384, 256)

	m := v.newMap(typeString, typeAny, 0)
	arr1 := v.newArray(typeInt, 2)
	v.setArrayElement(arr1, 0, encodeInt(10))
	v.setArrayElement(arr1, 1, encodeInt(20))
	v.mapSet(m, v.encodeString("a"), encodeHandle(arr1))

	val, ok := v.mapGet(m, v.encodeString("a"))
	if !ok || !val.isPointer() {
		t.Fatal("map value for a should be a pointer")
	}
	h := val.decodeHandle()
	if v.getArrayElement(h, 0).decodeInt() != 10 {
		t.Errorf("arr[0] = %d, want 10", v.getArrayElement(h, 0).decodeInt())
	}
	if v.getArrayElement(h, 1).decodeInt() != 20 {
		t.Errorf("arr[1] = %d, want 20", v.getArrayElement(h, 1).decodeInt())
	}

	// Overwrite the array value with another array.
	arr2 := v.newArray(typeInt, 1)
	v.setArrayElement(arr2, 0, encodeInt(99))
	v.mapSet(m, v.encodeString("a"), encodeHandle(arr2))

	val, ok = v.mapGet(m, v.encodeString("a"))
	if !ok || !val.isPointer() {
		t.Fatal("map value for a after overwrite should be a pointer")
	}
	h = val.decodeHandle()
	if v.getArrayElement(h, 0).decodeInt() != 99 {
		t.Errorf("arr[0] after overwrite = %d, want 99", v.getArrayElement(h, 0).decodeInt())
	}
}
