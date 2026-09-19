package vm

// Object layout in vm.memory:
//
//	[Header:64][Field0:64][Field1:64]...
//
// Header: [classID:31][marked:1][size:32]
// - classID in bits 63-33 (31 bits, bit 63 is GC mark)
// - size in bits 31-0 (32 bits, includes header)

func encodeObjectHeader(classID uint32, size uint32) uint64 {
	return uint64(classID)<<32 | uint64(size)
}

func getObjectSize(header uint64) int {
	return int(header & 0xFFFFFFFF)
}

func getClassID(header uint64) uint32 {
	return uint32((header >> 32) & 0x7FFFFFFF)
}

func isObjectHeader(header uint64) bool {
	classID := getClassID(header)
	return classID > 0 && classID < 0x7FFFFFFF
}

// newObject allocates and initializes a class instance in VM memory.
func (v *vm) newObject(classID uint32, fieldCount int) handle {
	size := 1 + fieldCount
	idx := v.allocMemory(size)

	v.memory[idx] = encodeObjectHeader(classID, uint32(size))

	for i := 1; i < size; i++ {
		v.memory[idx+i] = 0
	}

	return v.createHandle(idx)
}

func (v *vm) getObjectField(obj handle, fieldIndex int) value {
	objIdx := v.getMemoryIndex(obj)
	header := v.memory[objIdx]
	size := getObjectSize(header)

	if fieldIndex < 0 || fieldIndex >= size-1 {
		panic("field index out of bounds")
	}

	return value(v.memory[objIdx+1+fieldIndex])
}

func (v *vm) setObjectField(obj handle, fieldIndex int, val value) {
	objIdx := v.getMemoryIndex(obj)
	header := v.memory[objIdx]
	size := getObjectSize(header)

	if fieldIndex < 0 || fieldIndex >= size-1 {
		panic("field index out of bounds")
	}

	v.memory[objIdx+1+fieldIndex] = uint64(val)
}

func (v *vm) getObjectClassID(obj handle) uint32 {
	objIdx := v.getMemoryIndex(obj)
	if objIdx < 0 || objIdx >= v.memTop {
		panic("invalid object handle")
	}
	header := v.memory[objIdx]
	return getClassID(header)
}

func (v *vm) createObject(cid classID) handle {
	class := v.classRegistry.getByID(cid)
	if class == nil {
		panic("class not found")
	}
	return v.newObject(uint32(cid), class.fieldCount)
}

func (v *vm) getField(obj handle, fieldName string) value {
	idx := v.getMemoryIndex(obj)
	if idx >= 0 && idx < v.memTop && isStructHeader(v.memory[idx]) {
		return v.getStructFieldByName(obj, fieldName)
	}
	cid := classID(v.getObjectClassID(obj))
	class := v.classRegistry.getByID(cid)
	if class == nil {
		panic("class not found")
	}
	field := class.getField(fieldName)
	if field == nil {
		panic("field not found")
	}
	return v.getObjectField(obj, field.offset-1)
}

func (v *vm) setField(obj handle, fieldName string, val value) {
	idx := v.getMemoryIndex(obj)
	if idx >= 0 && idx < v.memTop && isStructHeader(v.memory[idx]) {
		v.setStructFieldByName(obj, fieldName, val)
		return
	}
	cid := classID(v.getObjectClassID(obj))
	class := v.classRegistry.getByID(cid)
	if class == nil {
		panic("class not found")
	}
	field := class.getField(fieldName)
	if field == nil {
		panic("field not found")
	}
	v.setObjectField(obj, field.offset-1, val)
}
