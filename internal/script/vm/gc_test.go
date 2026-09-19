package vm

import (
	"testing"
)

// --- GC mark/sweep basics ---

func TestGC_NewGCHasZeroCounters(t *testing.T) {
	v := newVM(4096, 256)
	if v.gc == nil {
		t.Fatal("gc not initialized")
	}
	if v.gc.freed != 0 {
		t.Errorf("freed = %d, want 0", v.gc.freed)
	}
	if v.gc.collections != 0 {
		t.Errorf("collections = %d, want 0", v.gc.collections)
	}
}

func TestGC_ShouldCollectBelowThreshold(t *testing.T) {
	v := newVM(1024, 256)
	// memTop starts at 1; threshold = 1024 * 3/4 = 768
	if v.gc.shouldCollect() {
		t.Error("should not collect when memTop is well below threshold")
	}
}

func TestGC_ShouldCollectAboveThreshold(t *testing.T) {
	v := newVM(128, 256)
	// threshold = 128 * 3/4 = 96; allocate raw memory to exceed
	for i := 0; i < 48; i++ {
		v.memTop += 2
	}
	// memTop = 1 + 96 = 97 > 96
	if !v.gc.shouldCollect() {
		t.Errorf("should collect when memTop=%d exceeds threshold=%d", v.memTop, len(v.memory)*3/4)
	}
}

func TestGC_MarkStackValue(t *testing.T) {
	v := newVM(4096, 256)

	sid, _ := v.structRegistry.register("S", []fieldDef{
		{name: "a", typeID: typeInt},
	})

	h := v.newStruct(sid, []value{encodeInt(42)})
	encoded := encodeHandle(h)
	if !encoded.isPointer() {
		t.Fatalf("encoded handle should be pointer, raw=%#x tag=%d", uint64(encoded), encoded.tag())
	}
	v.push(encoded)

	g := v.gc
	g.mark()

	idx := v.getMemoryIndex(h)
	if idx < 0 || idx >= len(g.marked) {
		t.Fatalf("handle %d maps to index %d, out of marked range", h, idx)
	}
	if !g.marked[idx] {
		t.Error("struct reachable from stack should be marked")
	}
}

func TestGC_SweepUnmarkedStruct(t *testing.T) {
	v := newVM(4096, 256)

	sid, _ := v.structRegistry.register("S", []fieldDef{
		{name: "a", typeID: typeInt},
	})

	// Allocate a struct but don't push it — unreachable.
	h := v.newStruct(sid, []value{encodeInt(10)})

	v.gc.collect()

	// After collection, the handle should be invalidated.
	newIdx := v.getMemoryIndex(h)
	if newIdx != -1 {
		t.Errorf("unreachable struct handle should be -1 after GC, got %d", newIdx)
	}
	if v.gc.freed == 0 {
		t.Error("expected freed > 0 after collecting unreachable object")
	}
}

func TestGC_SweepKeepsReachableStruct(t *testing.T) {
	v := newVM(4096, 256)

	sid, _ := v.structRegistry.register("S", []fieldDef{
		{name: "a", typeID: typeInt},
	})

	h := v.newStruct(sid, []value{encodeInt(55)})
	v.push(encodeHandle(h))

	v.gc.collect()

	newIdx := v.getMemoryIndex(h)
	if newIdx < 0 {
		t.Fatal("reachable struct handle should still be valid after GC")
	}

	header := v.memory[newIdx]
	if !isStructHeader(header) {
		t.Fatal("memory at handle index is not a struct header after GC")
	}
	if got := value(v.memory[newIdx+1]).decodeInt(); got != 55 {
		t.Fatalf("raw memory field = %d, want 55; idx=%d memTop=%d freeList=%+v", got, newIdx, v.memTop, v.freeList)
	}

	val := v.getStructFieldByName(h, "a")
	if val.decodeInt() != 55 {
		t.Errorf("struct field = %d, want 55 after GC", val.decodeInt())
	}
}

func TestGC_SweepPopulatesFreeList(t *testing.T) {
	v := newVM(4096, 256)

	sid, _ := v.structRegistry.register("S", []fieldDef{{name: "a", typeID: typeInt}})
	_ = v.newStruct(sid, []value{encodeInt(1)})
	_ = v.newStruct(sid, []value{encodeInt(2)})

	v.gc.collect()

	if len(v.freeList) == 0 {
		t.Fatal("expected free list entries after sweeping unreachable structs")
	}
}

func TestGC_MultipleCollectionsCounter(t *testing.T) {
	v := newVM(4096, 256)

	sid, _ := v.structRegistry.register("S", []fieldDef{
		{name: "a", typeID: typeInt},
	})

	for i := 0; i < 3; i++ {
		_ = v.newStruct(sid, []value{encodeInt(int32(i))})
		v.gc.collect()
	}

	if v.gc.collections != 3 {
		t.Errorf("collections = %d, want 3", v.gc.collections)
	}
}

// --- GC with arrays ---

func TestGC_MarkArrayReachable(t *testing.T) {
	v := newVM(4096, 256)

	arr := v.newArray(typeInt, 4)
	v.setArrayElement(arr, 0, encodeInt(10))
	v.setArrayElement(arr, 1, encodeInt(20))
	v.push(encodeHandle(arr))

	v.gc.collect()

	length := v.getArrayLength(arr)
	if length != 4 {
		t.Errorf("array length = %d, want 4 after GC", length)
	}
	if v.getArrayElement(arr, 0).decodeInt() != 10 {
		t.Error("array element corrupted after GC")
	}
}

func TestGC_SweepUnreachableArray(t *testing.T) {
	v := newVM(4096, 256)

	arr := v.newArray(typeInt, 4)
	// Don't push — unreachable.

	v.gc.collect()

	newIdx := v.getMemoryIndex(arr)
	if newIdx != -1 {
		t.Errorf("unreachable array handle should be -1, got %d", newIdx)
	}
}

// --- GC with maps ---

func TestGC_MarkMapReachable(t *testing.T) {
	v := newVM(4096, 256)

	m := v.newMap(typeInt, typeInt, 0)
	v.mapSet(m, encodeInt(1), encodeInt(100))
	v.mapSet(m, encodeInt(2), encodeInt(200))
	v.push(encodeHandle(m))

	v.gc.collect()

	size := v.mapSize(m)
	if size != 2 {
		t.Errorf("map size = %d, want 2 after GC", size)
	}
	val, ok := v.mapGet(m, encodeInt(1))
	if !ok || val.decodeInt() != 100 {
		t.Error("map entry corrupted after GC")
	}
}

func TestGC_SweepUnreachableMap(t *testing.T) {
	v := newVM(4096, 256)

	m := v.newMap(typeInt, typeInt, 0)
	v.mapSet(m, encodeInt(1), encodeInt(100))
	// Don't push — unreachable.

	v.gc.collect()

	newIdx := v.getMemoryIndex(m)
	if newIdx != -1 {
		t.Errorf("unreachable map handle should be -1, got %d", newIdx)
	}
}

// --- GC with objects ---

func TestGC_MarkObjectReachable(t *testing.T) {
	v := newVM(4096, 256)

	class := newClass(1, "C", nil)
	class.addField("x", typeInt)
	v.classRegistry.register(class)

	obj := v.createObject(1)
	v.setField(obj, "x", encodeInt(77))
	v.push(encodeHandle(obj))

	v.gc.collect()

	val := v.getField(obj, "x")
	if val.decodeInt() != 77 {
		t.Errorf("object field = %d, want 77 after GC", val.decodeInt())
	}
}

func TestGC_SweepUnreachableObject(t *testing.T) {
	v := newVM(4096, 256)

	class := newClass(1, "C", nil)
	class.addField("x", typeInt)
	v.classRegistry.register(class)

	obj := v.createObject(1)
	// Don't push — unreachable.

	v.gc.collect()

	newIdx := v.getMemoryIndex(obj)
	if newIdx != -1 {
		t.Errorf("unreachable object handle should be -1, got %d", newIdx)
	}
}

func TestGC_RootProviderMarksValueOutsideVMStack(t *testing.T) {
	v := newVM(4096, 256)

	arr := v.newArray(typeInt, 1)
	v.setArrayElement(arr, 0, encodeInt(99))
	providerID := v.addRootProvider(func(visit func(value)) {
		visit(encodeHandle(arr))
	})
	defer v.removeRootProvider(providerID)

	v.gc.collect()

	if idx := v.getMemoryIndex(arr); idx < 0 {
		t.Fatal("root provider should keep array alive outside vm.stack")
	}
	if got := v.getArrayElement(arr, 0).decodeInt(); got != 99 {
		t.Fatalf("array element = %d, want 99 after GC", got)
	}
}

func TestGC_RemoveRootProviderStopsProtectingValue(t *testing.T) {
	v := newVM(4096, 256)

	arr := v.newArray(typeInt, 1)
	providerID := v.addRootProvider(func(visit func(value)) {
		visit(encodeHandle(arr))
	})

	v.gc.collect()
	if idx := v.getMemoryIndex(arr); idx < 0 {
		t.Fatal("root provider should keep array alive before removal")
	}

	v.removeRootProvider(providerID)
	v.gc.collect()

	if idx := v.getMemoryIndex(arr); idx != -1 {
		t.Fatalf("array should be reclaimed after removing root provider, got index %d", idx)
	}
}

func TestGC_RootProviderMarksNestedReachability(t *testing.T) {
	v := newVM(4096, 256)

	inner := v.newArray(typeInt, 1)
	v.setArrayElement(inner, 0, encodeInt(7))
	outer := v.newArray(typeArray, 1)
	v.setArrayElement(outer, 0, encodeHandle(inner))
	providerID := v.addRootProvider(func(visit func(value)) {
		visit(encodeHandle(outer))
	})
	defer v.removeRootProvider(providerID)

	v.gc.collect()

	if idx := v.getMemoryIndex(inner); idx < 0 {
		t.Fatal("nested array should remain reachable through provider-rooted outer array")
	}
	stored := v.getArrayElement(outer, 0)
	if !stored.isPointer() {
		t.Fatal("outer array element should still be a handle after GC")
	}
	if got := v.getArrayElement(stored.decodeHandle(), 0).decodeInt(); got != 7 {
		t.Fatalf("nested array element = %d, want 7 after GC", got)
	}
}

// --- GC with nested references ---

func TestGC_ArrayReachableKeepsInnerStruct(t *testing.T) {
	v := newVM(4096, 256)

	sid, _ := v.structRegistry.register("Inner", []fieldDef{
		{name: "v", typeID: typeInt},
	})

	inner := v.newStruct(sid, []value{encodeInt(42)})

	arr := v.newArray(typeAny, 1)
	v.setArrayElement(arr, 0, encodeHandle(inner))
	v.push(encodeHandle(arr))

	// Mark the array. The inner struct is referenced by the array element.
	g := v.gc
	g.mark()

	// Both array and inner struct should be marked.
	arrIdx := v.getMemoryIndex(arr)
	if arrIdx < 0 || arrIdx >= len(g.marked) || !g.marked[arrIdx] {
		t.Error("array should be marked")
	}
	innerIdx := v.getMemoryIndex(inner)
	if innerIdx < 0 || innerIdx >= len(g.marked) || !g.marked[innerIdx] {
		t.Error("inner struct reachable through array should be marked")
	}
}

func TestGC_ObjectInMapValue(t *testing.T) {
	v := newVM(4096, 256)

	class := newClass(1, "C", nil)
	class.addField("x", typeInt)
	v.classRegistry.register(class)

	obj := v.createObject(1)
	v.setField(obj, "x", encodeInt(99))

	m := v.newMap(typeInt, typeAny, 0)
	v.mapSet(m, encodeInt(1), encodeHandle(obj))
	v.push(encodeHandle(m))

	// Mark the map. The object should be reachable via map value.
	g := v.gc
	g.mark()

	objIdx := v.getMemoryIndex(obj)
	if objIdx < 0 || objIdx >= len(g.marked) || !g.marked[objIdx] {
		t.Error("object in map should be marked as reachable")
	}
}

// --- GC heap numeric values ---

func TestGC_HeapLongKeepsReachable(t *testing.T) {
	v := newVM(4096, 256)

	lv := v.encodeLong(maxInlineLong + 1)
	v.push(lv)

	v.gc.collect()

	result := v.decodeLong(v.pop())
	if result != maxInlineLong+1 {
		t.Errorf("long value = %d, want %d after GC", result, maxInlineLong+1)
	}
}

// --- GC edge cases ---

func TestGC_EmptyStackCollectsAll(t *testing.T) {
	v := newVM(4096, 256)

	sid, _ := v.structRegistry.register("S", []fieldDef{
		{name: "a", typeID: typeInt},
	})

	_ = v.newStruct(sid, []value{encodeInt(1)})
	_ = v.newStruct(sid, []value{encodeInt(2)})
	_ = v.newStruct(sid, []value{encodeInt(3)})

	v.gc.collect()

	if v.gc.freed == 0 {
		t.Error("expected freed > 0 when collecting with empty stack")
	}
	if len(v.freeList) == 0 {
		t.Error("expected free list to contain collected slots")
	}
}

func TestGC_AllocReusesFreeList(t *testing.T) {
	v := newVM(4096, 256)

	sid, _ := v.structRegistry.register("S", []fieldDef{{name: "a", typeID: typeInt}})
	_ = v.newStruct(sid, []value{encodeInt(1)})
	_ = v.newStruct(sid, []value{encodeInt(2)})
	v.gc.collect()

	if len(v.freeList) == 0 {
		t.Fatal("expected free list after collection")
	}
	freeStart := v.freeList[0].start
	freeSize := v.freeList[0].size

	h := v.newStruct(sid, []value{encodeInt(3)})
	idx := v.getMemoryIndex(h)
	if idx != freeStart && !(idx > freeStart && idx < freeStart+freeSize) {
		t.Fatalf("expected allocation to reuse free slot [%d,%d), got %d", freeStart, freeStart+freeSize, idx)
	}
	if freeSize <= 2 && len(v.freeList) != 0 {
		t.Fatalf("expected exact free slot reuse to consume free list entry, got %+v", v.freeList)
	}
}

func TestGC_TriggeredByAllocation(t *testing.T) {
	// Use a very small memory pool so GC triggers early.
	// 75% threshold = 384 words, 128 words per struct (2: header + field).
	// Need ~192 structs to reach threshold.
	v := newVM(512, 256)

	sid, _ := v.structRegistry.register("S", []fieldDef{
		{name: "a", typeID: typeInt},
	})

	// Push a reachable struct.
	h := v.newStruct(sid, []value{encodeInt(42)})
	v.push(encodeHandle(h))

	collectionsBefore := v.gc.collections

	// Keep allocating to trigger auto-collection.
	for i := 0; i < 200; i++ {
		_ = v.newStruct(sid, []value{encodeInt(int32(i))})
	}

	if v.gc.collections <= collectionsBefore {
		t.Errorf("expected GC to be triggered: collections before=%d after=%d",
			collectionsBefore, v.gc.collections)
	}

	// Original reachable struct should survive.
	newIdx := v.getMemoryIndex(h)
	if newIdx < 0 {
		t.Fatal("reachable struct handle was invalidated by GC")
	}
}

func TestGC_MarkOnlyScansStackRange(t *testing.T) {
	v := newVM(4096, 256)

	sid, _ := v.structRegistry.register("S", []fieldDef{
		{name: "a", typeID: typeInt},
	})

	h1 := v.newStruct(sid, []value{encodeInt(1)})
	h2 := v.newStruct(sid, []value{encodeInt(2)})

	// Push h1, then push and pop h2.
	v.push(encodeHandle(h1))
	v.push(encodeHandle(h2))
	v.pop() // h2 is now outside sp

	g := v.gc
	g.mark()

	// h1 should be marked (still on stack).
	idx1 := v.getMemoryIndex(h1)
	if !g.marked[idx1] {
		t.Error("h1 should be marked (on stack)")
	}

	// h2 should NOT be marked (popped, outside sp).
	idx2 := v.getMemoryIndex(h2)
	if g.marked[idx2] {
		t.Error("h2 should not be marked (popped, outside sp)")
	}
}
