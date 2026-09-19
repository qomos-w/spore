package runtime

import (
	"fmt"
	"reflect"
)

// SchemaTable is the schema/component table a World consumes. It is produced
// by codegen (registry.gen.go) as a plain package value and registered onto a
// World instance by the host:
//
//	w := runtime.NewWorld()
//	w.AddRegistry(gen.Registry)
//
// The interface is deliberately structural — every method signature uses only
// stdlib types (uint64, string, reflect.Type) — so generated code satisfies it
// without importing runtime. Codegen output must stay dependency-free; the
// host code imports both the generated package and runtime.
//
// Component ⇒ schema: every @component struct also carries a schema ID, so
// ComponentIDs is a subset of the SchemaTypes/SchemaIDs keys. A schema that
// is not a component simply never appears in ComponentIDs.
type SchemaTable interface {
	// SchemaTypes maps every schema ID to its Go struct type.
	SchemaTypes() map[uint64]reflect.Type
	// SchemaIDs maps every schema ID to its canonical schema name.
	SchemaIDs() map[uint64]string
	// ComponentIDs lists the schema IDs of structs declared @component.
	ComponentIDs() []uint64
}

// AddRegistry registers the codegen schema/component table onto the World,
// enabling the descriptor-free facade (SetT/GetT/HasT/MarkT/RemoveT) for every
// @component struct listed in the table.
//
// Only component IDs are indexed: for each such ID the Go type from
// SchemaTypes is bound to the canonical name from SchemaIDs. Registering the
// same Go type under two different schema names panics (early and loud beats
// a silent wrong-slot write later). Calling AddRegistry again — for a second
// generated package or the same one — merges entries idempotently.
//
// The schema IDs themselves are not stored here: wire/schema resolution for
// later serialization stays with the schema registry (RegisterStructType /
// SchemaTypes), which the same codegen output already feeds.
//
// Contract: public semantic contract — per-World component type registry fed
// by codegen output (no process-global state, no codegen→runtime imports).
func (w *World) AddRegistry(t SchemaTable) {
	if t == nil {
		return
	}
	for _, id := range t.ComponentIDs() {
		typ, ok := t.SchemaTypes()[id]
		if !ok || typ == nil {
			continue
		}
		name, ok := t.SchemaIDs()[id]
		if !ok || name == "" {
			continue
		}
		typ = elem(typ)
		if existing, ok := w.typeToName[typ]; ok {
			if existing != name {
				panic(fmt.Sprintf("spore/runtime: AddRegistry conflict: type %s already registered as %q, cannot also register as %q", typ, existing, name))
			}
			continue // idempotent re-registration
		}
		w.typeToName[typ] = name
	}
}

// elem dereferences a pointer type so registration and lookups agree on both
// T and *T shapes.
func elem(t reflect.Type) reflect.Type {
	if t.Kind() == reflect.Pointer {
		return t.Elem()
	}
	return t
}

// LookupComponent resolves the registered Component[T] descriptor for the Go
// type T from the World's codegen-fed registry. It lets ECS code obtain the
// type→name mapping without referencing a generated <Name>C variable:
//
//	c, ok := w.LookupComponent[Position]()
//	if ok { w.Set(e, c, &pos) }
//
// ok is false when T was never registered via AddRegistry. Prefer the
// generated <Name>C variable when it is in scope — a compile-time binding
// with no lookup; this method exists for generic/library code holding only T.
//
// Contract: public semantic contract — per-World type→descriptor resolution
// over the registered component table.
func (w *World) LookupComponent[T any]() (Component[T], bool) {
	name, ok := w.componentName[T]()
	if !ok {
		return Component[T]{}, false
	}
	return Component[T]{name: name}, true
}

// componentName resolves the schema name for T from the World's registry.
func (w *World) componentName[T any]() (string, bool) {
	typ := elem(reflect.TypeOf((*T)(nil)).Elem())
	name, ok := w.typeToName[typ]
	return name, ok
}

// nameForComponentType resolves the registered schema name for a value type,
// normalizing pointer shapes. Unlike componentName[T] it takes a reflect.Type
// directly, which the aggregate auto-recognition path needs when scanning
// host fields.
func (w *World) nameForComponentType(t reflect.Type) (string, bool) {
	name, ok := w.typeToName[elem(t)]
	return name, ok
}

// componentDescriptorFor returns the registered Component[T] for T, or an
// error carrying the type name (used by the descriptor-free facade).
func (w *World) componentDescriptorFor[T any]() (Component[T], error) {
	name, ok := w.componentName[T]()
	if !ok {
		typ := reflect.TypeOf((*T)(nil)).Elem()
		return Component[T]{}, fmt.Errorf("spore/runtime: component type %s is not registered; register the codegen table on this World via World.AddRegistry(pkg.Registry), or use an explicit Component[T] descriptor", typ.String())
	}
	return Component[T]{name: name}, nil
}
