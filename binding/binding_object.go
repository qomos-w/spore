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
	"sync/atomic"

	"github.com/qomos-w/spore/diagnostics"
	"github.com/qomos-w/spore/identity"
	"github.com/qomos-w/spore/schema"
)

// MutationKind classifies the type of change observed during a binding operation.
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
