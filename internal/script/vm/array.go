package vm

const arrayHeaderSize = 4

const (
	arrayStorageMarker = uint64(0xFFFFFFFD00000001)
)

func encodeArrayHeader(elemType typeID) uint64 {
	header := uint64(0xAAAAAAAA) << 32
	header |= uint64(elemType) << 16
	return header
}

func encodeArrayStorageHeader(capacity int) uint64 {
	return (arrayStorageMarker & 0xFFFFFFFF00000000) | uint64(uint32(capacity))
}

func isArrayStorageHeader(header uint64) bool {
	return header>>32 == arrayStorageMarker>>32 && header&0xFFFFFFFF00000000 == arrayStorageMarker&0xFFFFFFFF00000000
}

func getArrayStorageCapacity(header uint64) int {
	return int(header & 0xFFFFFFFF)
}

func (v *vm) newArray(elemType typeID, length int) handle {
	capacity := length
	if capacity == 0 {
		capacity = 4
	}
	storage := v.newArrayStorage(capacity)
	scope := v.beginRootScope()
	scope.add(encodeHandle(storage))
	idx := v.allocMemory(arrayHeaderSize)
	v.memory[idx+0] = encodeArrayHeader(elemType)
	v.memory[idx+1] = uint64(length)
	v.memory[idx+2] = uint64(capacity)
	v.memory[idx+3] = uint64(encodeHandle(storage))
	scope.end()
	return v.createHandle(idx)
}

func (v *vm) newArrayStorage(capacity int) handle {
	if capacity <= 0 {
		capacity = 4
	}
	idx := v.allocMemory(1 + capacity)
	v.memory[idx] = encodeArrayStorageHeader(capacity)
	for i := 0; i < capacity; i++ {
		v.memory[idx+1+i] = 0
	}
	return v.createHandle(idx)
}

func (v *vm) getArrayLength(arr handle) int {
	arrIdx := v.getMemoryIndex(arr)
	return int(v.memory[arrIdx+1])
}

func (v *vm) getArrayElement(arr handle, index int) value {
	arrIdx := v.getMemoryIndex(arr)
	length := int(v.memory[arrIdx+1])
	if index < 0 || index >= length {
		vmPanic("array index out of bounds", "index", index, "length", length)
	}
	storageIdx := v.getArrayStorageIndex(arrIdx)
	return value(v.memory[storageIdx+1+index])
}

func (v *vm) setArrayElement(arr handle, index int, val value) {
	arrIdx := v.getMemoryIndex(arr)
	length := int(v.memory[arrIdx+1])
	if index < 0 || index >= length {
		vmPanic("array index out of bounds", "index", index, "length", length)
	}
	storageIdx := v.getArrayStorageIndex(arrIdx)
	v.memory[storageIdx+1+index] = uint64(val)
}

func (v *vm) arrayPush(arr handle, val value) {
	arrIdx := v.getMemoryIndex(arr)
	length := int(v.memory[arrIdx+1])
	capacity := int(v.memory[arrIdx+2])
	if length >= capacity {
		// Only need GC roots while allocating; the fast path skips them.
		scope := v.beginRootScope()
		scope.add(encodeHandle(arr), val)
		v.arrayGrow(arr)
		scope.end()
		arrIdx = v.getMemoryIndex(arr)
	}
	v.memory[arrIdx+1] = uint64(length + 1)
	storageIdx := v.getArrayStorageIndex(arrIdx)
	v.memory[storageIdx+1+length] = uint64(val)
}

func (v *vm) arrayGrow(arr handle) {
	// One batched root scope covers the header, the old storage (copied
	// from) and the new storage (copied into) across the reallocation.
	scope := v.beginRootScope()
	defer scope.end()
	scope.add(encodeHandle(arr))

	arrIdx := v.getMemoryIndex(arr)
	length := int(v.memory[arrIdx+1])
	capacity := int(v.memory[arrIdx+2])
	newCapacity := capacity * 2
	if newCapacity == 0 {
		newCapacity = 4
	}
	oldStorageIdx := v.getArrayStorageIndex(arrIdx)
	oldStorage := value(v.memory[arrIdx+3])
	scope.add(oldStorage)
	newStorage := v.newArrayStorage(newCapacity)
	scope.add(encodeHandle(newStorage))
	newStorageIdx := v.getMemoryIndex(newStorage)
	for i := 0; i < length; i++ {
		v.memory[newStorageIdx+1+i] = v.memory[oldStorageIdx+1+i]
	}
	v.memory[arrIdx+2] = uint64(newCapacity)
	v.memory[arrIdx+3] = uint64(encodeHandle(newStorage))
}

func (v *vm) getArrayStorageIndex(arrIdx int) int {
	storageVal := value(v.memory[arrIdx+3])
	return v.getMemoryIndex(storageVal.decodeHandle())
}
