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
	"strconv"

	"github.com/qomos-w/spore/schema"
)

// MutationKind classifies the type of change observed during a binding operation.
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
