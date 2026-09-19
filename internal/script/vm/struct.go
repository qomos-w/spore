package vm

import "fmt"

const maxStructFields = 32

type structID uint32

// structDef holds a struct type's metadata.
type structDef struct {
	id         structID
	name       string
	fields     []fieldDef
	fieldCount int
}

func newStructDef(id structID, name string, fields []fieldDef) *structDef {
	return &structDef{
		id:         id,
		name:       name,
		fields:     fields,
		fieldCount: len(fields),
	}
}

func (s *structDef) getFieldIndex(name string) int {
	for i := range s.fields {
		if s.fields[i].name == name {
			return i
		}
	}
	return -1
}

// structRegistry manages all registered struct types.
type structRegistry struct {
	structs  map[structID]*structDef
	nameToID map[string]structID
	nextID   structID
}

func newStructRegistry() *structRegistry {
	return &structRegistry{
		structs:  make(map[structID]*structDef),
		nameToID: make(map[string]structID),
		nextID:   1,
	}
}

func (sr *structRegistry) register(name string, fields []fieldDef) (structID, error) {
	if len(fields) > maxStructFields {
		return 0, fmt.Errorf("struct %s has %d fields, exceeds maximum of %d", name, len(fields), maxStructFields)
	}
	if id, exists := sr.nameToID[name]; exists {
		return id, nil
	}
	id := sr.nextID
	sr.nextID++
	sd := newStructDef(id, name, fields)
	sr.structs[id] = sd
	sr.nameToID[name] = id
	return id, nil
}

func (sr *structRegistry) get(id structID) *structDef {
	return sr.structs[id]
}

func (sr *structRegistry) getByName(name string) *structDef {
	if id, exists := sr.nameToID[name]; exists {
		return sr.structs[id]
	}
	return nil
}

// updateFields replaces the field definitions of an already-registered struct.
// This is used when fields need correct TypeIDs that depend on other structs
// being registered first (e.g. nested struct fields).
func (sr *structRegistry) updateFields(name string, fields []fieldDef) error {
	id, exists := sr.nameToID[name]
	if !exists {
		return fmt.Errorf("struct %s not found", name)
	}
	if len(fields) > maxStructFields {
		return fmt.Errorf("struct %s has %d fields, exceeds maximum of %d", name, len(fields), maxStructFields)
	}
	sr.structs[id] = newStructDef(id, name, fields)
	return nil
}

// --- Struct memory layout ---
// [header:64][field0:64][field1:64]...[fieldN:64]
// Header: [GC_mark:1][struct_marker:1][structID:30][fieldCount:32]

const structMarkerBit = uint64(1) << 62

func encodeStructHeader(sid structID, fieldCount int) uint64 {
	return structMarkerBit | (uint64(sid) << 32) | uint64(fieldCount)
}

func decodeStructHeader(header uint64) (structID, int) {
	header = header & ^structMarkerBit
	sid := structID(header >> 32)
	fieldCount := int(header & 0xFFFFFFFF)
	return sid, fieldCount
}

func isStructHeader(header uint64) bool {
	if (header & structMarkerBit) == 0 {
		return false
	}
	header = header & ^structMarkerBit
	sid := structID(header >> 32)
	fieldCount := int(header & 0xFFFFFFFF)
	return sid > 0 && sid < 1000000 && fieldCount >= 0 && fieldCount <= maxStructFields
}

func getStructSize(header uint64) int {
	_, fieldCount := decodeStructHeader(header)
	return 1 + fieldCount
}

func (v *vm) newStruct(sid structID, fieldValues []value) handle {
	sd := v.structRegistry.get(sid)
	if sd == nil {
		panic(fmt.Sprintf("struct not found: %d", sid))
	}
	if len(fieldValues) != sd.fieldCount {
		panic(fmt.Sprintf("struct %s expects %d fields, got %d", sd.name, sd.fieldCount, len(fieldValues)))
	}

	size := 1 + sd.fieldCount
	idx := v.allocMemory(size)
	v.memory[idx] = encodeStructHeader(sid, sd.fieldCount)
	for i, val := range fieldValues {
		v.memory[idx+1+i] = uint64(val)
	}
	return v.createHandle(idx)
}

func (v *vm) getStructField(h handle, fieldIndex int) value {
	idx := v.getMemoryIndex(h)
	header := v.memory[idx]
	_, fieldCount := decodeStructHeader(header)
	if fieldIndex < 0 || fieldIndex >= fieldCount {
		panic(fmt.Sprintf("struct field index out of range: %d (count: %d)", fieldIndex, fieldCount))
	}
	return value(v.memory[idx+1+fieldIndex])
}

func (v *vm) setStructField(h handle, fieldIndex int, val value) {
	idx := v.getMemoryIndex(h)
	header := v.memory[idx]
	_, fieldCount := decodeStructHeader(header)
	if fieldIndex < 0 || fieldIndex >= fieldCount {
		panic(fmt.Sprintf("struct field index out of range: %d (count: %d)", fieldIndex, fieldCount))
	}
	v.memory[idx+1+fieldIndex] = uint64(val)
}

func (v *vm) getStructFieldByName(h handle, fieldName string) value {
	idx := v.getMemoryIndex(h)
	header := v.memory[idx]
	sid, _ := decodeStructHeader(header)
	sd := v.structRegistry.get(sid)
	if sd == nil {
		panic(fmt.Sprintf("struct not found: %d", sid))
	}
	fieldIndex := sd.getFieldIndex(fieldName)
	if fieldIndex < 0 {
		panic(fmt.Sprintf("field not found: %s in struct %s", fieldName, sd.name))
	}
	return v.getStructField(h, fieldIndex)
}

func (v *vm) setStructFieldByName(h handle, fieldName string, val value) {
	idx := v.getMemoryIndex(h)
	header := v.memory[idx]
	sid, _ := decodeStructHeader(header)
	sd := v.structRegistry.get(sid)
	if sd == nil {
		panic(fmt.Sprintf("struct not found: %d", sid))
	}
	fieldIndex := sd.getFieldIndex(fieldName)
	if fieldIndex < 0 {
		panic(fmt.Sprintf("field not found: %s in struct %s", fieldName, sd.name))
	}
	v.setStructField(h, fieldIndex, val)
}

func (v *vm) isStruct(h handle) bool {
	idx := v.getMemoryIndex(h)
	if idx < 0 || idx >= v.memTop {
		return false
	}
	header := v.memory[idx]
	if !isStructHeader(header) {
		return false
	}
	sid, _ := decodeStructHeader(header)
	return v.structRegistry.get(sid) != nil
}
