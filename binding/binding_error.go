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
	"github.com/qomos-w/spore/diagnostics"
	"github.com/qomos-w/spore/identity"
)

// MutationKind classifies the type of change observed during a binding operation.
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
