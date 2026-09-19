package schema

import (
	"reflect"
	"sync"
)

// schemaTypeRegistry maps schema ID -> one or more Go reflect.Type values.
// It is populated by generated init() functions so runtime dispatchers can
// resolve a wire schema ID back to concrete Go struct types. Multiple types
// may share an ID for cross-package value types such as PromptRef.
var schemaTypeRegistry = struct {
	mu sync.RWMutex
	m  map[uint64][]reflect.Type
}{m: make(map[uint64][]reflect.Type)}

// RegisterStructType binds a schema ID to a Go reflect.Type. Duplicate
// registrations of the exact same type are ignored; additional distinct types
// for the same ID are appended.
func RegisterStructType(id uint64, typ reflect.Type) {
	if id == 0 {
		return
	}
	if typ == nil {
		panic("spore/schema: RegisterStructType called with nil type")
	}

	schemaTypeRegistry.mu.Lock()
	defer schemaTypeRegistry.mu.Unlock()

	for _, existing := range schemaTypeRegistry.m[id] {
		if existing == typ {
			return
		}
	}
	schemaTypeRegistry.m[id] = append(schemaTypeRegistry.m[id], typ)
}

// StructTypeByID returns the first Go reflect.Type bound to schema ID, if any.
// For IDs shared by identical wire shapes across packages, this returns any
// one of the registered types; use StructTypesByID for the full list.
func StructTypeByID(id uint64) (reflect.Type, bool) {
	types, ok := StructTypesByID(id)
	if !ok || len(types) == 0 {
		return nil, false
	}
	return types[0], true
}

// StructTypesByID returns all Go reflect.Type values bound to schema ID.
func StructTypesByID(id uint64) ([]reflect.Type, bool) {
	schemaTypeRegistry.mu.RLock()
	defer schemaTypeRegistry.mu.RUnlock()
	types, ok := schemaTypeRegistry.m[id]
	if !ok || len(types) == 0 {
		return nil, false
	}
	out := make([]reflect.Type, len(types))
	copy(out, types)
	return out, true
}
