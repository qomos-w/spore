// Package render provides TypeScript code generation from spore schema descriptors.
//
// It is the public-facing library that re-exports the rendering core from
// internal/gen/ts so external modules (e.g. gospore) can embed spore
// rendering without shelling out to a separate binary.
package render

import (
	"github.com/qomos-w/spore/internal/gen/ts"
)

// NamedObjectDesc is one entry in the codegen input: a struct/class shape
// keyed by its (Namespace, SchemaID, Name) triple plus its visibility.
type NamedObjectDesc = ts.NamedObjectDesc

// NamedCallableDesc is one callable entry in the codegen input.
type NamedCallableDesc = ts.NamedCallableDesc

// Visibility classifies the exposure level of a callable or component.
type Visibility = ts.Visibility

const (
	VisibilityInternal   = ts.VisibilityInternal
	VisibilityPublic     = ts.VisibilityPublic
	VisibilityAdmin      = ts.VisibilityAdmin
	VisibilityDiagnostic = ts.VisibilityDiagnostic
)

// Options controls how Generate filters and decorates the rendered output.
type Options = ts.Options

// Generate renders schema descriptors and callable descriptors into a
// path → file content map. See internal/gen/ts.Generate for full docs.
var Generate = ts.Generate

// ParseVisibility accepts the canonical string form and returns the matching
// enum value and ok=true. If the string is unknown, ok=false is returned.
func ParseVisibility(s string) (Visibility, bool) {
	return ts.ParseVisibility(s)
}
