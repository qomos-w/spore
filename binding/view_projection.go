// Package binding defines the Script Binding surface — the unified plane
// that connects schema descriptors to runtime objects for both callable
// binding and data binding.
//
// Script Binding is the runtime projection layer of schema:
//   - Callable binding: Go function → script-callable function
//   - Data binding: Go struct → script-visible object
//
// It consumes schema descriptors and produces bound runtime artifacts.
//
// Binding is NOT a truth authority. It projects and connects layers:
//   - Schema defines what is visible
//   - Runtime objects carry state
//   - Transport views are stable, encodable projections
//
// The binding layer ensures these three layers remain distinct and that
// projection is one-way (schema → runtime → transport) unless explicit
// bidirectional sync is enabled.
package binding

import (
	"fmt"
	"reflect"
	"sort"
	"sync"

	"github.com/qomos-w/spore/identity"
	"github.com/qomos-w/spore/schema"
)

// MutationKind classifies the type of change observed during a binding operation.
type ViewProjection struct {
	Schema   schema.ObjectDesc
	Identity identity.CanonicalID
	Fields   map[string]any
}

// projectionField is one resolved schema→struct-field entry of a projection
// plan. A nil index marks a schema field with no matching struct field
// (exact name or json tag); projection reports it per call.
type projectionField struct {
	fd    schema.FieldDesc
	index []int
}

type projectionPlanKey struct {
	typ    reflect.Type
	schema string
	fields int
}

// projectionPlans caches schema→struct resolution per (struct type, schema
// name, field count). Struct layouts and registered schemas are immutable
// at projection time, so plans are reusable across entities — the per-entity
// cost of ViewBody-style column iteration drops to a map lookup plus a
// FieldByIndex.
var projectionPlans sync.Map // projectionPlanKey -> []projectionField

// projectionPlan resolves every schema-declared field of desc against t,
// following the same name semantics as ApplyViewPatch: exact Go field name
// first, json tag (JSONTagName) second.
func projectionPlan(t reflect.Type, desc schema.ObjectDesc) []projectionField {
	key := projectionPlanKey{typ: t, schema: desc.Name, fields: len(desc.Fields)}
	if cached, ok := projectionPlans.Load(key); ok {
		return cached.([]projectionField)
	}
	tags := jsonTagIndex(t)
	plan := make([]projectionField, len(desc.Fields))
	for i, fd := range desc.Fields {
		sf, ok := t.FieldByName(fd.Name)
		if !ok {
			if goName, tagOk := tags[fd.Name]; tagOk {
				sf, ok = t.FieldByName(goName)
			}
		}
		if ok {
			plan[i] = projectionField{fd: fd, index: sf.Index}
		} else {
			plan[i] = projectionField{fd: fd}
		}
	}
	actual, _ := projectionPlans.LoadOrStore(key, plan)
	return actual.([]projectionField)
}

// ProjectView creates a transport-consumable view from a valid object binding.
// It reads struct fields from the bound runtime object, guided by the schema
// descriptor. Only fields declared in the schema are projected — the schema
// is the projection authority.
func ProjectView(binding *ObjectBinding) (*ViewProjection, error) {
	if binding == nil || !binding.Valid() {
		return nil, errBindingInvalid(binding)
	}

	v := reflect.ValueOf(binding.target)
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil, &BindingError{
				Identity: binding.Identity,
				Schema:   binding.Schema.Name,
				Err:      errNil("target object is nil pointer"),
			}
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil, &BindingError{
			Identity: binding.Identity,
			Schema:   binding.Schema.Name,
			Err:      fmt.Errorf("binding target must be a struct, got %s", v.Kind()),
		}
	}

	fields := make(map[string]any, len(binding.Schema.Fields))
	for _, pf := range projectionPlan(v.Type(), binding.Schema) {
		if pf.index == nil {
			return nil, &BindingError{
				Identity: binding.Identity,
				Schema:   binding.Schema.Name,
				Path:     "." + pf.fd.Name,
				Err:      fmt.Errorf("field %q not found in runtime struct", pf.fd.Name),
			}
		}
		fv := v.FieldByIndex(pf.index)
		projected, err := projectFieldValue(pf.fd, fv)
		if err != nil {
			return nil, &BindingError{
				Identity: binding.Identity,
				Schema:   binding.Schema.Name,
				Path:     "." + pf.fd.Name,
				Err:      err,
			}
		}
		fields[pf.fd.Name] = projected
	}

	return &ViewProjection{
		Schema:   binding.Schema,
		Identity: binding.Identity,
		Fields:   fields,
	}, nil
}

// projectFieldValue converts a reflect.Value to a projection-safe Go value
// guided by its schema FieldDesc. Nested structs are projected as
// map[string]any to maintain three-layer separation. Map types are
// projected as ordered entry sequences (via Entries() for OrderedMap,
// or sorted-key iteration for plain Go maps) to preserve order through
// the binding → transport chain.
func projectFieldValue(fd schema.FieldDesc, fv reflect.Value) (any, error) {
	// Unwrap interface or pointer to get the concrete value
	for fv.Kind() == reflect.Interface || fv.Kind() == reflect.Pointer {
		if fv.IsNil() {
			return nil, nil
		}
		fv = fv.Elem()
	}

	switch fd.Type.Kind {
	case schema.TypeKindStruct:
		if fv.Kind() != reflect.Struct {
			return nil, fmt.Errorf("expected struct for field %q, got %s", fd.Name, fv.Kind())
		}
		return projectStructFields(fv)
	case schema.TypeKindMap:
		return projectMapField(fd, fv)
	default:
		// Scalars, arrays — return the Go value directly.
		// The transport layer is responsible for further encoding.
		// Named scalar types (enums such as world.GROWTH_STATE) box as
		// their defined type, which script VMs and JSON consumers may not
		// recognise; normalise to the builtin underlying kind.
		if v, ok := builtinScalarValue(fv); ok {
			return v, nil
		}
		return fv.Interface(), nil
	}
}

// builtinScalarValue returns fv as its builtin Go scalar value when fv's type
// is a named (defined) scalar type. Non-named types and non-scalar kinds
// return ok=false so callers keep their original behaviour.
func builtinScalarValue(fv reflect.Value) (any, bool) {
	if fv.Type().PkgPath() == "" {
		return nil, false
	}
	switch fv.Kind() {
	case reflect.Bool:
		return fv.Bool(), true
	case reflect.Int:
		return int(fv.Int()), true
	case reflect.Int8:
		return int8(fv.Int()), true
	case reflect.Int16:
		return int16(fv.Int()), true
	case reflect.Int32:
		return int32(fv.Int()), true
	case reflect.Int64:
		return fv.Int(), true
	case reflect.Uint:
		return uint(fv.Uint()), true
	case reflect.Uint8:
		return uint8(fv.Uint()), true
	case reflect.Uint16:
		return uint16(fv.Uint()), true
	case reflect.Uint32:
		return uint32(fv.Uint()), true
	case reflect.Uint64:
		return fv.Uint(), true
	case reflect.Float32:
		return float32(fv.Float()), true
	case reflect.Float64:
		return fv.Float(), true
	case reflect.String:
		return fv.String(), true
	}
	return nil, false
}

// projectStructFields projects a struct's exported fields into a
// map[string]any, preserving the three-layer separation (the result
// is a copy, not a reference to the runtime struct).
func projectStructFields(sv reflect.Value) (map[string]any, error) {
	st := sv.Type()
	result := make(map[string]any, st.NumField())
	for i := 0; i < st.NumField(); i++ {
		field := st.Field(i)
		if !field.IsExported() {
			continue
		}
		fv := sv.Field(i)
		// Recursively unwrap pointers/interfaces
		for fv.Kind() == reflect.Pointer || fv.Kind() == reflect.Interface {
			if fv.IsNil() {
				break
			}
			fv = fv.Elem()
		}
		if fv.Kind() == reflect.Struct {
			nested, err := projectStructFields(fv)
			if err != nil {
				return nil, fmt.Errorf("field %s: %w", field.Name, err)
			}
			result[field.Name] = nested
		} else {
			result[field.Name] = fv.Interface()
		}
	}
	return result, nil
}

// projectMapField projects a map-typed runtime value as an ordered entry
// sequence. The projection shape is []any where each element is
// map[string]any{"Key": k, "Value": v}. This preserves insertion order
// for OrderedMap-backed fields and uses deterministic sorted-key order
// for plain Go maps with orderable key types.
//
// This is the canonical projection that both binding and transport
// consume — it ensures order is preserved through the entire chain
// rather than being lost when the Go map's random iteration order
// meets JSON's unordered object serialization.
func projectMapField(fd schema.FieldDesc, fv reflect.Value) (any, error) {
	// Check for OrderedMap backing first — preserve insertion order
	if fv.Kind() == reflect.Struct && isOrderedMapType(fv.Type()) {
		return projectOrderedMapEntries(fv)
	}

	// Plain Go map — project as sorted-key entry sequence
	if fv.Kind() == reflect.Map {
		return projectGoMapEntries(fv)
	}

	// Fallback: return the value as-is (shouldn't happen after validateFieldType)
	return fv.Interface(), nil
}

// isOrderedMapType reports whether t is an OrderedMap or SortedOrderedMap,
// delegating to schema.IsOrderedMapType — the single authoritative
// detection. Both variants share the same public surface (Entries/Set/Delete)
// and are handled identically by the binding layer.
func isOrderedMapType(t reflect.Type) bool {
	return schema.IsOrderedMapType(t)
}

// projectOrderedMapEntries calls Entries() on an OrderedMap via reflection
// and converts the result to []any of {"Key": k, "Value": v} maps.
// This preserves insertion order through the projection chain.
func projectOrderedMapEntries(fv reflect.Value) ([]any, error) {
	// Get the Entries method — works on both value and pointer receiver
	methodType := fv.Type()
	if methodType.Kind() != reflect.Pointer {
		methodType = reflect.PointerTo(methodType)
	}
	method, ok := methodType.MethodByName("Entries")
	if !ok {
		return nil, fmt.Errorf("OrderedMap missing Entries method")
	}

	// Call Entries() — need pointer receiver
	var receiver reflect.Value
	if fv.CanAddr() {
		receiver = fv.Addr()
	} else {
		ptr := reflect.New(fv.Type())
		ptr.Elem().Set(fv)
		receiver = ptr
	}

	results := method.Func.Call([]reflect.Value{receiver})
	if len(results) != 1 {
		return nil, fmt.Errorf("Entries() returned %d values, expected 1", len(results))
	}

	entriesSlice := results[0]
	if entriesSlice.Kind() != reflect.Slice {
		return nil, fmt.Errorf("Entries() returned %s, expected slice", entriesSlice.Kind())
	}

	return entrySliceToProjection(entriesSlice)
}

// projectGoMapEntries projects a plain Go map as a sorted-key entry
// sequence. Keys are sorted for deterministic output; this does NOT
// fabricate insertion history — it provides a stable projection order.
func projectGoMapEntries(fv reflect.Value) ([]any, error) {
	keys := fv.MapKeys()

	// Validate key type is sortable before attempting deterministic projection.
	// This aligns with schema/ordered_map.go's orderedKey constraint: only
	// string, int*, uint*, and float* keys can be sorted reliably.
	// Unsortable key types (struct, array, slice, etc.) are rejected explicitly
	// rather than silently degraded to nonsensical comparison.
	keyKind := fv.Type().Key().Kind()
	switch keyKind {
	case reflect.String,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		// Sortable — continue
	default:
		return nil, fmt.Errorf("cannot produce deterministic projection for map with unsortable key type %s", fv.Type().Key())
	}

	// Sort keys for deterministic projection order
	sort.Slice(keys, func(i, j int) bool {
		ki, kj := keys[i], keys[j]
		switch ki.Kind() {
		case reflect.String:
			return ki.String() < kj.String()
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			return ki.Int() < kj.Int()
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			return ki.Uint() < kj.Uint()
		case reflect.Float32, reflect.Float64:
			return ki.Float() < kj.Float()
		default:
			// Unreachable: validated above
			return false
		}
	})

	out := make([]any, len(keys))
	for i, key := range keys {
		val := fv.MapIndex(key)
		out[i] = map[string]any{
			"Key":   key.Interface(),
			"Value": val.Interface(),
		}
	}
	return out, nil
}

// entrySliceToProjection converts a reflected []Entry{K,V} slice to
// the canonical []any of map[string]any{"Key": ..., "Value": ...} form.
func entrySliceToProjection(entriesSlice reflect.Value) ([]any, error) {
	out := make([]any, entriesSlice.Len())
	for i := 0; i < entriesSlice.Len(); i++ {
		entry := entriesSlice.Index(i)
		if entry.Kind() != reflect.Struct || entry.NumField() < 2 {
			return nil, fmt.Errorf("entry %d has unexpected shape", i)
		}
		keyField := entry.Field(0)
		valField := entry.Field(1)
		out[i] = map[string]any{
			"Key":   keyField.Interface(),
			"Value": valField.Interface(),
		}
	}
	return out, nil
}

// DiffViewProjection compares two view projections and returns the field-level
// mutations needed to transform before into after. Only field values are
// compared — identity differences are not treated as field mutations.
func DiffViewProjection(before, after *ViewProjection) []MutationResult {
	if before == nil && after == nil {
		return nil
	}
	if before == nil {
		// All fields in after are inserted
		result := make([]MutationResult, 0, len(after.Fields))
		for k := range after.Fields {
			result = append(result, MutationResult{Kind: MutationInserted, Key: k})
		}
		return result
	}
	if after == nil {
		// All fields in before are removed
		result := make([]MutationResult, 0, len(before.Fields))
		for k := range before.Fields {
			result = append(result, MutationResult{Kind: MutationRemoved, Key: k})
		}
		return result
	}

	var result []MutationResult

	// Fields in before that are missing or changed in after
	for k, beforeVal := range before.Fields {
		afterVal, exists := after.Fields[k]
		if !exists {
			result = append(result, MutationResult{Kind: MutationRemoved, Key: k})
		} else if !valuesEqual(beforeVal, afterVal) {
			result = append(result, MutationResult{Kind: MutationReplaced, Key: k})
		}
	}

	// Fields in after that are new (not in before)
	for k := range after.Fields {
		if _, exists := before.Fields[k]; !exists {
			result = append(result, MutationResult{Kind: MutationInserted, Key: k})
		}
	}

	return result
}

// valuesEqual compares two projected values for equality.
// It handles nested map[string]any recursively.
func valuesEqual(a, b any) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}

	// Handle nested map[string]any
	aMap, aIsMap := a.(map[string]any)
	bMap, bIsMap := b.(map[string]any)
	if aIsMap && bIsMap {
		if len(aMap) != len(bMap) {
			return false
		}
		for k, av := range aMap {
			bv, ok := bMap[k]
			if !ok || !valuesEqual(av, bv) {
				return false
			}
		}
		return true
	}

	return a == b
}

// ApplyViewPatch applies field values from a view projection back to the
// runtime object bound by the given ObjectBinding. Only fields declared in
// the binding's schema descriptor are written — extra fields in the view
// are silently ignored.
//
// This is an explicit opt-in operation. By default, projection is one-way
// (runtime → view). ApplyViewPatch enables the reverse direction under
// caller control.
//
// The following field types are supported for patch writeback:
//   - Scalar fields: directly assigned if the view value's type is assignable
//   - Struct fields: when the view provides map[string]any, fields are set
//     recursively on the target struct
//   - Slice fields: when the view provides []any, elements are converted and
//     a new slice is constructed if element types are assignable
//   - Map fields: when the view provides map[string]any, entries are converted
