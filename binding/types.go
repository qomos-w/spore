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
	"math"
	"reflect"
	"sort"
	"strconv"
	"sync/atomic"

	"github.com/qomos-w/spore/diagnostics"
	"github.com/qomos-w/spore/identity"
	"github.com/qomos-w/spore/schema"
)

// MutationKind classifies the type of change observed during a binding operation.
// Contract: public semantic contract — mutation classification for binding observability.
type MutationKind string

const (
	MutationInserted MutationKind = "inserted"
	MutationReplaced MutationKind = "replaced"
	MutationRemoved  MutationKind = "removed"
	MutationMissing  MutationKind = "missing"
	MutationSkipped  MutationKind = "skipped"
)

// MutationResult records the observable outcome of a binding mutation.
// Contract: public semantic contract — mutation result for binding observability.
type MutationResult struct {
	Kind   MutationKind
	Key    string
	Reason string // diagnostic reason; set when Kind is MutationSkipped
}

const CodeBindingError = "binding_error"

func init() {
	diagnostics.RegisterCode(diagnostics.CodeInfo{
		Code:        CodeBindingError,
		Category:    diagnostics.CategoryContract,
		Description: "Binding target does not match its schema descriptor",
		Hint:        "检查绑定目标对象的类型是否与 schema 描述符匹配",
	})
}

// ObjectBinding represents the binding between a schema descriptor and a
// runtime Go object. It does not own the object — it projects schema-visible
// state from it.
// Contract: public semantic contract — schema↔runtime binding handle with
// identity, validation, and lifecycle semantics.
type ObjectBinding struct {
	Schema   schema.ObjectDesc
	Identity identity.CanonicalID
	target   any
	valid    atomic.Bool
}

// NewObjectBinding creates a binding between a schema descriptor and a
// runtime Go object. The object must be a struct pointer compatible with
// the schema's field descriptions. Validation is performed eagerly —
// incompatible targets are rejected at creation time, not deferred to
// projection or patch operations.
func NewObjectBinding(classDesc schema.ObjectDesc, id identity.CanonicalID, target any) (*ObjectBinding, error) {
	if target == nil {
		return nil, errBindingNilTarget(id)
	}
	if err := ValidateBinding(classDesc, target); err != nil {
		return nil, err
	}
	b := &ObjectBinding{
		Schema:   classDesc,
		Identity: id,
		target:   target,
	}
	b.valid.Store(true)
	return b, nil
}

// ValidateBinding checks that a runtime Go object is compatible with a
// schema class descriptor. It verifies that every field declared in the
// schema exists on the target struct, is exported, and has a Go type
// compatible with the schema's TypeDesc.
//
// Fields not declared in the schema are ignored — the schema is the
// projection authority, not the struct.
func ValidateBinding(classDesc schema.ObjectDesc, target any) error {
	v := reflect.ValueOf(target)
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return &BindingError{
				Schema: classDesc.Name,
				Err:    errNil("target object is nil pointer"),
			}
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return &BindingError{
			Schema: classDesc.Name,
			Err:    fmt.Errorf("binding target must be a struct, got %s", v.Kind()),
		}
	}

	for _, fd := range classDesc.Fields {
		fv := v.FieldByName(fd.Name)
		if !fv.IsValid() {
			return &BindingError{
				Schema: classDesc.Name,
				Path:   "." + fd.Name,
				Err:    fmt.Errorf("field %q not found in runtime struct", fd.Name),
			}
		}

		// Unexported fields cannot be read or written by the binding layer
		if !fv.CanInterface() {
			return &BindingError{
				Schema: classDesc.Name,
				Path:   "." + fd.Name,
				Err:    fmt.Errorf("field %q is unexported and cannot be bound", fd.Name),
			}
		}

		if err := validateFieldType(fd, fv); err != nil {
			return &BindingError{
				Schema: classDesc.Name,
				Path:   "." + fd.Name,
				Err:    err,
			}
		}
	}

	return nil
}

// validateFieldType checks that a struct field's Go type is compatible
// with the schema's TypeDesc. The rules are aligned with
// schema/describe.go's describeReflectType mapping.
func validateFieldType(fd schema.FieldDesc, fv reflect.Value) error {
	// Unwrap pointer to get the concrete kind
	kind := fv.Kind()
	concreteType := fv.Type()
	if kind == reflect.Pointer || kind == reflect.Interface {
		if fv.IsNil() {
			// Nil pointer/interface is acceptable — projection will handle it
			return nil
		}
		kind = fv.Elem().Kind()
		concreteType = fv.Elem().Type()
	}

	switch fd.Type.Kind {
	case schema.TypeKindScalar:
		return validateScalarKind(fd, kind)
	case schema.TypeKindStruct:
		if kind != reflect.Struct {
			return &fieldTypeError{message: fmt.Sprintf("schema expects struct for field %q, got %s", fd.Name, kind), expected: fd.Type.String(), actual: kind.String()}
		}
	case schema.TypeKindArray:
		if kind != reflect.Slice && kind != reflect.Array {
			return &fieldTypeError{message: fmt.Sprintf("schema expects array/slice for field %q, got %s", fd.Name, kind), expected: fd.Type.String(), actual: kind.String()}
		}
	case schema.TypeKindMap:
		// Map fields may be backed by Go map or OrderedMap.
		// OrderedMap is struct-typed but belongs to the map semantic family.
		if kind != reflect.Map && !(kind == reflect.Struct && isOrderedMapType(concreteType)) {
			return &fieldTypeError{message: fmt.Sprintf("schema expects map for field %q, got %s", fd.Name, kind), expected: fd.Type.String(), actual: kind.String()}
		}
	case schema.TypeKindMedia:
		// Media fields may be schema.Media or a string-keyed {mime, src} map.
		if kind != reflect.Struct && kind != reflect.Map {
			return &fieldTypeError{message: fmt.Sprintf("schema expects media for field %q, got %s", fd.Name, kind), expected: fd.Type.String(), actual: kind.String()}
		}
	}
	return nil
}

// validateScalarKind checks that a Go reflect.Kind matches the schema
// scalar type name. This follows the same mapping as
// schema/describe.go's describeReflectType.
func validateScalarKind(fd schema.FieldDesc, kind reflect.Kind) error {
	switch fd.Type.Name {
	case "string":
		if kind != reflect.String {
			return &fieldTypeError{message: fmt.Sprintf("schema expects string for field %q, got %s", fd.Name, kind), expected: fd.Type.String(), actual: kind.String()}
		}
	case "bool":
		if kind != reflect.Bool {
			return &fieldTypeError{message: fmt.Sprintf("schema expects bool for field %q, got %s", fd.Name, kind), expected: fd.Type.String(), actual: kind.String()}
		}
	case "int", "byte", "short", "ushort", "uint", "long", "ulong":
		switch kind {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			return nil
		default:
			return &fieldTypeError{message: fmt.Sprintf("schema expects integer type for field %q, got %s", fd.Name, kind), expected: fd.Type.String(), actual: kind.String()}
		}
	case "float", "double":
		switch kind {
		case reflect.Float32, reflect.Float64:
			return nil
		default:
			return &fieldTypeError{message: fmt.Sprintf("schema expects float type for field %q, got %s", fd.Name, kind), expected: fd.Type.String(), actual: kind.String()}
		}
	}
	// Unknown scalar names are allowed — the schema may describe types
	// that this binding version doesn't have a concrete mapping for.
	return nil
}

// Valid reports whether the binding is still alive.
// A binding becomes invalid when its target object is disposed.
func (b *ObjectBinding) Valid() bool {
	return b != nil && b.valid.Load()
}

// Invalidate marks the binding as no longer usable.
// After invalidation, Valid returns false and projection methods fail.
func (b *ObjectBinding) Invalidate() {
	if b != nil {
		b.valid.Store(false)
	}
}

// Target returns the bound runtime object.
// Returns nil if the binding is invalid.
func (b *ObjectBinding) Target() any {
	if !b.Valid() {
		return nil
	}
	return b.target
}

// ViewProjection is a schema-aware snapshot of a runtime object,
// suitable for transport encoding.
// Contract: public semantic contract — the stable snapshot shape for
// schema-guided projection from runtime to transport.
type ViewProjection struct {
	Schema   schema.ObjectDesc
	Identity identity.CanonicalID
	Fields   map[string]any
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
	for _, fd := range binding.Schema.Fields {
		fv := v.FieldByName(fd.Name)
		if !fv.IsValid() {
			return nil, &BindingError{
				Identity: binding.Identity,
				Schema:   binding.Schema.Name,
				Path:     "." + fd.Name,
				Err:      fmt.Errorf("field %q not found in runtime struct", fd.Name),
			}
		}
		projected, err := projectFieldValue(fd, fv)
		if err != nil {
			return nil, &BindingError{
				Identity: binding.Identity,
				Schema:   binding.Schema.Name,
				Path:     "." + fd.Name,
				Err:      err,
			}
		}
		fields[fd.Name] = projected
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

// isOrderedMapType detects an OrderedMap or SortedOrderedMap by checking its
// package path and type name prefix, using the same pattern as
// schema/describe.go's describeOrderedMapType without importing the concrete
// type. Both variants share the same public surface (Entries/Set/Delete)
// and are handled identically by the binding layer.
func isOrderedMapType(t reflect.Type) bool {
	if t.PkgPath() != "github.com/qomos-w/spore/schema" {
		return false
	}
	name := t.Name()
	return (len(name) > 11 && name[:11] == "OrderedMap[") ||
		(len(name) > 17 && name[:17] == "SortedOrderedMap[")
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
//     and a new map is constructed if key/value types are assignable
func ApplyViewPatch(b *ObjectBinding, view *ViewProjection) ([]MutationResult, error) {
	if b == nil || !b.Valid() {
		return nil, errBindingInvalid(b)
	}

	v := reflect.ValueOf(b.target)
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil, &BindingError{
				Identity: b.Identity,
				Schema:   b.Schema.Name,
				Err:      errNil("target object is nil pointer"),
			}
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil, &BindingError{
			Identity: b.Identity,
			Schema:   b.Schema.Name,
			Err:      fmt.Errorf("binding target must be a struct, got %s", v.Kind()),
		}
	}

	// Build a lookup of schema-declared fields
	schemaFields := make(map[string]schema.FieldDesc, len(b.Schema.Fields))
	for _, fd := range b.Schema.Fields {
		schemaFields[fd.Name] = fd
	}

	var mutations []MutationResult

	for fieldName, fieldValue := range view.Fields {
		fd, inSchema := schemaFields[fieldName]
		if !inSchema {
			continue
		}

		fv := v.FieldByName(fieldName)
		if !fv.IsValid() || !fv.CanSet() {
			continue
		}

		oldVal := fv.Interface()
		if err := setFieldValue(fd, fv, fieldValue); err != nil {
			mutations = append(mutations, MutationResult{
				Kind:   MutationSkipped,
				Key:    fieldName,
				Reason: err.Error(),
			})
			continue
		}
		if !valuesEqual(oldVal, fieldValue) {
			mutations = append(mutations, MutationResult{
				Kind: MutationReplaced,
				Key:  fieldName,
			})
		}
	}

	return mutations, nil
}

// setFieldValue writes a projected view value into a struct field,
// performing schema-guided conversion for non-trivial types.
func setFieldValue(fd schema.FieldDesc, fv reflect.Value, value any) error {
	if value == nil {
		return nil
	}

	newVal := reflect.ValueOf(value)

	// Direct assignment: view value type matches the struct field type
	if newVal.Type().AssignableTo(fv.Type()) {
		fv.Set(newVal)
		return nil
	}

	// Schema-guided numeric conversion: JSON decodes all numbers as float64,
	// but the struct field may be int, int32, etc. When the schema describes
	// a scalar integer type and the incoming value is numeric (float or any
	// integer width, e.g. int64 from script VMs), convert it. Named integer
	// targets (enums such as world.GROWTH_STATE) are covered too: conversion
	// is kind-based.
	if fd.Type.Kind == schema.TypeKindScalar && isScalarIntegerName(fd.Type.Name) {
		converted, err := convertToIntegerKind(value, fv.Type())
		if err != nil {
			return err
		}
		fv.Set(converted)
		return nil
	}

	// Schema-guided conversion for projected types that differ from Go types
	switch fd.Type.Kind {
	case schema.TypeKindStruct:
		return setStructField(fv, value)
	case schema.TypeKindArray:
		return setSliceField(fd, fv, value)
	case schema.TypeKindMap:
		return setMapField(fd, fv, value)
	}

	return fmt.Errorf("cannot assign %T to %s for field %q", value, fv.Type(), fd.Name)
}

// isScalarIntegerName checks whether a scalar type name describes an integer
// type in the schema vocabulary (matching the names from schema/describe.go).
func isScalarIntegerName(name string) bool {
	switch name {
	case "int", "byte", "short", "ushort", "uint", "long", "ulong":
		return true
	}
	return false
}

// convertToIntegerKind converts a numeric view value (float or any integer
// width) to the target integer type. Targets may be named types (enums) —
// conversion is kind-based, with valueFitsInTarget guarding narrowing wraps.
func convertToIntegerKind(value any, targetType reflect.Type) (reflect.Value, error) {
	switch v := value.(type) {
	case float64:
		return convertFloat64ToInt(v, targetType)
	case float32:
		return convertFloat64ToInt(float64(v), targetType)
	}
	rv := reflect.ValueOf(value)
	if !rv.IsValid() {
		return reflect.Value{}, fmt.Errorf("cannot assign nil to %s", targetType)
	}
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if !valueFitsInTarget(rv, targetType) {
			return reflect.Value{}, fmt.Errorf("value %v out of range for %s", value, targetType)
		}
		return rv.Convert(targetType), nil
	}
	return reflect.Value{}, fmt.Errorf("cannot assign %T to %s", value, targetType)
}

// convertFloat64ToInt converts a float64 value to the target integer type
// via reflect. This handles the common case where JSON decode produces
// float64 but the struct field is an integer type.
func convertFloat64ToInt(f float64, targetType reflect.Type) (reflect.Value, error) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return reflect.Value{}, fmt.Errorf("cannot convert non-finite float64 to %s", targetType)
	}
	if math.Trunc(f) != f {
		return reflect.Value{}, fmt.Errorf("cannot convert fractional float64 %v to %s", f, targetType)
	}

	switch targetType.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		bits := targetType.Bits()
		if bits == 0 {
			bits = strconv.IntSize
		}
		min := -(int64(1) << (bits - 1))
		max := (int64(1) << (bits - 1)) - 1
		if f < float64(min) || f > float64(max) {
			return reflect.Value{}, fmt.Errorf("float64 %v out of range for %s", f, targetType)
		}
		v := reflect.New(targetType).Elem()
		v.SetInt(int64(f))
		return v, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		if f < 0 {
			return reflect.Value{}, fmt.Errorf("cannot convert negative float64 %v to %s", f, targetType)
		}
		bits := targetType.Bits()
		if bits == 0 {
			bits = strconv.IntSize
		}
		max := uint64(1<<bits) - 1
		if bits == 64 {
			max = ^uint64(0)
		}
		if f > float64(max) {
			return reflect.Value{}, fmt.Errorf("float64 %v out of range for %s", f, targetType)
		}
		v := reflect.New(targetType).Elem()
		v.SetUint(uint64(f))
		return v, nil
	default:
		return reflect.Value{}, fmt.Errorf("cannot convert float64 to %s", targetType)
	}
}

// setStructField writes a map[string]any projection back into a struct field.
// Each key in the map is matched to a corresponding exported field on the
// target struct. Unmatched keys and unexported fields are silently skipped.
func setStructField(fv reflect.Value, value any) error {
	m, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("expected map[string]any for struct field, got %T", value)
	}

	// Unwrap pointer to get the concrete struct
	target := fv
	for target.Kind() == reflect.Pointer {
		if target.IsNil() {
			return fmt.Errorf("cannot set fields on nil struct pointer")
		}
		target = target.Elem()
	}
	if target.Kind() != reflect.Struct {
		return fmt.Errorf("expected struct, got %s", target.Kind())
	}

	for key, val := range m {
		sf := target.FieldByName(key)
		if !sf.IsValid() || !sf.CanSet() {
			continue
		}
		rv := reflect.ValueOf(val)
		if rv.IsValid() && rv.Type().AssignableTo(sf.Type()) {
			sf.Set(rv)
		}
	}

	return nil
}

// setSliceField writes a []any projection back into a slice field.
// Elements are individually converted if their types are directly
// assignable to the slice element type.
func setSliceField(fd schema.FieldDesc, fv reflect.Value, value any) error {
	arr, ok := value.([]any)
	if !ok {
		return fmt.Errorf("expected []any for slice field, got %T", value)
	}

	elemType := fv.Type().Elem()
	slice := reflect.MakeSlice(fv.Type(), 0, len(arr))

	for _, elem := range arr {
		ev := reflect.ValueOf(elem)
		if !ev.IsValid() {
			continue
		}
		if ev.Type().AssignableTo(elemType) {
			slice = reflect.Append(slice, ev)
		}
	}

	fv.Set(slice)
	return nil
}

// setMapField writes a projected map value back into a map field.
// The only accepted projection shape is the canonical entry sequence:
//
//	[]any{map[string]any{"Key": ..., "Value": ...}, ...}
//
// For OrderedMap-backed fields, it reconstructs via Set() calls to preserve
// insertion order. For plain Go maps, it populates directly from entries.
// Type-incompatible entries are silently skipped.
func setMapField(fd schema.FieldDesc, fv reflect.Value, value any) error {
	entries, ok := value.([]any)
	if !ok {
		return fmt.Errorf("expected []any entry sequence for map field, got %T", value)
	}

	return setMapFieldFromEntries(fd, fv, entries)
}

// setMapFieldFromEntries reconstructs a map field from an entry sequence.
// For OrderedMap-backed fields, it creates a new OrderedMap and calls Set()
// to preserve insertion order. For plain Go maps, it populates directly.
func setMapFieldFromEntries(fd schema.FieldDesc, fv reflect.Value, entries []any) error {
	// Detect OrderedMap field
	fieldType := fv.Type()
	for fieldType.Kind() == reflect.Pointer {
		fieldType = fieldType.Elem()
	}
	if fieldType.Kind() == reflect.Struct && isOrderedMapType(fieldType) {
		return setOrderedMapFromEntries(fv, entries)
	}

	// Plain Go map — populate from entries
	return setPlainMapFromEntries(fv, entries)
}

// setOrderedMapFromEntries populates an OrderedMap or SortedOrderedMap field
// from an entry sequence using Set() calls. For OrderedMap, this preserves
// insertion order. For SortedOrderedMap, the comparator determines position.
//
// Rather than creating a new instance (which would lose SortedOrderedMap's
// comparator), this function clears the existing map and repopulates it.
// If the field is a nil pointer, it is initialized first.
func setOrderedMapFromEntries(fv reflect.Value, entries []any) error {
	// Determine the concrete OrderedMap type
	concreteType := fv.Type()
	isPointer := concreteType.Kind() == reflect.Pointer
	if isPointer {
		concreteType = concreteType.Elem()
	}

	// Initialize nil pointer if needed
	if isPointer && fv.IsNil() {
		fv.Set(reflect.New(concreteType))
	}

	// Get the actual struct value (dereference pointer)
	target := fv
	if isPointer {
		target = fv.Elem()
	}

	// Clear existing entries by calling Delete on each existing key.
	// This preserves the comparator for SortedOrderedMap.
	methodType := reflect.PointerTo(concreteType)

	// Get existing entries to clear
	entriesMethod, ok := methodType.MethodByName("Entries")
	if ok {
		receiver := target.Addr()
		results := entriesMethod.Func.Call([]reflect.Value{receiver})
		if len(results) == 1 && results[0].Kind() == reflect.Slice {
			existingEntries := results[0]
			deleteMethod, hasDelete := methodType.MethodByName("Delete")
			if hasDelete {
				for i := 0; i < existingEntries.Len(); i++ {
					key := existingEntries.Index(i).Field(0) // Entry.Key
					deleteMethod.Func.Call([]reflect.Value{receiver, key})
				}
			}
		}
	}

	// Get the Set method
	setMethod, ok := methodType.MethodByName("Set")
	if !ok {
		return fmt.Errorf("OrderedMap missing Set method")
	}

	receiver := target.Addr()

	for _, entry := range entries {
		entryMap, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		keyVal, ok := entryMap["Key"]
		if !ok {
			continue
		}
		valVal, _ := entryMap["Value"]

		keyV := reflect.ValueOf(keyVal)
		valV := reflect.ValueOf(valVal)

		// Call Set(key, value) — the method will handle type checking
		results := setMethod.Func.Call([]reflect.Value{receiver, keyV, valV})
		_ = results // Set returns SetResult, not needed here
	}

	return nil
}

// setPlainMapFromEntries populates a plain Go map from an entry sequence.
func setPlainMapFromEntries(fv reflect.Value, entries []any) error {
	keyType := fv.Type().Key()
	valType := fv.Type().Elem()
	newMap := reflect.MakeMapWithSize(fv.Type(), len(entries))

	for _, entry := range entries {
		entryMap, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		keyVal, ok := entryMap["Key"]
		if !ok {
			continue
		}
		valVal, _ := entryMap["Value"]

		kv := reflect.ValueOf(keyVal)
		vv := reflect.ValueOf(valVal)
		if kv.Type().AssignableTo(keyType) && vv.IsValid() && vv.Type().AssignableTo(valType) {
			newMap.SetMapIndex(kv, vv)
		}
	}

	fv.Set(newMap)
	return nil
}

// BindingError carries identity and schema context for binding failures.
// Contract: public semantic contract — structured binding error with
// identity/schema/path context for diagnosability.
type BindingError struct {
	Identity identity.CanonicalID
	Schema   string
	Path     string
	Err      error
	Expected string
	Actual   string
}

func (e *BindingError) Error() string {
	return e.Err.Error()
}

func (e *BindingError) Unwrap() error {
	return e.Err
}

func (e *BindingError) DiagnosticCode() string {
	if e == nil {
		return ""
	}
	if coder, ok := e.Err.(interface{ DiagnosticCode() string }); ok {
		return coder.DiagnosticCode()
	}
	return CodeBindingError
}

func (e *BindingError) DiagnosticCategory() diagnostics.Category {
	if e == nil {
		return ""
	}
	if categorizer, ok := e.Err.(interface{ DiagnosticCategory() diagnostics.Category }); ok {
		return categorizer.DiagnosticCategory()
	}
	return diagnostics.CategoryContract
}

func (e *BindingError) DiagnosticSpan() diagnostics.Span {
	if e == nil {
		return diagnostics.Span{}
	}
	if spanner, ok := e.Err.(interface{ DiagnosticSpan() diagnostics.Span }); ok {
		return spanner.DiagnosticSpan()
	}
	return diagnostics.Span{}
}

func (e *BindingError) DiagnosticPath() string {
	if e == nil {
		return ""
	}
	if e.Path != "" {
		return e.Path
	}
	if pather, ok := e.Err.(interface{ DiagnosticPath() string }); ok {
		return pather.DiagnosticPath()
	}
	return ""
}

func (e *BindingError) DiagnosticStack() []diagnostics.Frame {
	if e == nil {
		return nil
	}
	if stacker, ok := e.Err.(interface{ DiagnosticStack() []diagnostics.Frame }); ok {
		stack := stacker.DiagnosticStack()
		if len(stack) > 0 {
			return append([]diagnostics.Frame(nil), stack...)
		}
	}
	return nil
}

func (e *BindingError) DiagnosticCause() *diagnostics.Descriptor {
	if e == nil || e.Err == nil {
		return nil
	}
	d := diagnostics.FromError(e.Err, diagnostics.Descriptor{})
	return &d
}

func (e *BindingError) DiagnosticExpected() string {
	if e == nil {
		return ""
	}
	if e.Expected != "" {
		return e.Expected
	}
	if ex, ok := e.Err.(interface{ DiagnosticExpected() string }); ok {
		return ex.DiagnosticExpected()
	}
	return ""
}

func (e *BindingError) DiagnosticActual() string {
	if e == nil {
		return ""
	}
	if e.Actual != "" {
		return e.Actual
	}
	if ac, ok := e.Err.(interface{ DiagnosticActual() string }); ok {
		return ac.DiagnosticActual()
	}
	return ""
}

// fieldTypeError carries expected/actual type context for binding field validation.
type fieldTypeError struct {
	message  string
	expected string
	actual   string
}

func (e *fieldTypeError) Error() string              { return e.message }
func (e *fieldTypeError) DiagnosticExpected() string { return e.expected }
func (e *fieldTypeError) DiagnosticActual() string   { return e.actual }

func errBindingNilTarget(id identity.CanonicalID) *BindingError {
	return &BindingError{
		Identity: id,
		Err:      errNil("target object is nil"),
	}
}

func errBindingInvalid(b *ObjectBinding) *BindingError {
	id := identity.CanonicalID{}
	schema := ""
	if b != nil {
		id = b.Identity
		schema = b.Schema.Name
	}
	return &BindingError{
		Identity: id,
		Schema:   schema,
		Err:      errNil("binding is invalid"),
	}
}

type errNil string

func (e errNil) Error() string { return string(e) }

// MapBinding adapts a Go map or OrderedMap into a script-visible
// ordered-map surface. It distinguishes two paths:
//   - If the backing is already an OrderedMap, it preserves the existing
//     insertion order.
//   - If the backing is a plain Go map, it produces a deterministic
//     projection (sorted by key for orderedKey types).
//
// Contract: public semantic contract — ordered map surface adapter
// distinguishing ordered backing from plain Go maps.
//
// MapBinding does NOT fabricate insertion history for plain Go maps.
type MapBinding struct {
	backing any
	ordered bool
}

// BindMap creates a MapBinding from a plain Go map[K]V.
// The resulting binding uses deterministic key-sorted projection,
// not fabricated insertion history.
func BindMap(m any) *MapBinding {
	return &MapBinding{backing: m, ordered: false}
}

// BindOrderedMap creates a MapBinding from an existing OrderedMap.
// The resulting binding preserves the OrderedMap's insertion order.
func BindOrderedMap(om any) *MapBinding {
	return &MapBinding{backing: om, ordered: true}
}

// IsOrdered reports whether the backing preserves insertion order.
func (b *MapBinding) IsOrdered() bool {
	return b != nil && b.ordered
}

// Backing returns the underlying map or OrderedMap.
func (b *MapBinding) Backing() any {
	if b == nil {
		return nil
	}
	return b.backing
}
