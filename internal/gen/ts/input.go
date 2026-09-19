// Package ts provides TypeScript code generation from spore schema descriptors.
//
// It is internal to the spore module: the public-facing entry points are the
// `spore-gen-ts` CLI in cmd/spore-gen-ts and the re-exporting wrapper in
// gen/render, which reads a JSON manifest of NamedObjectDesc entries.
//
// The input contract (Visibility, Options, NamedObjectDesc, NamedCallableDesc)
// lives in internal/gen/common and is aliased here so consumers keep using the
// ts-qualified names.
package ts

import "github.com/qomos-w/spore/internal/gen/common"

// Visibility classifies the exposure level of a callable or component.
// See internal/gen/common.Visibility for the full contract.
type Visibility = common.Visibility

const (
	VisibilityInternal   = common.VisibilityInternal
	VisibilityPublic     = common.VisibilityPublic
	VisibilityAdmin      = common.VisibilityAdmin
	VisibilityDiagnostic = common.VisibilityDiagnostic
)

// NamedObjectDesc is one entry in the codegen input: a struct/class shape keyed
// by its (Namespace, SchemaID, Name) triple plus its visibility. See
// internal/gen/common.NamedObjectDesc.
type NamedObjectDesc = common.NamedObjectDesc

// NamedCallableDesc is one callable entry in the codegen input, keyed by its
// (Namespace, Name) pair.
//
// Mode is one of schema.CallableMode ("unary" or "streaming"). For streaming
// callables ChunkSchemaID and Chunk MUST be populated; for unary callables
// ChunkSchemaID is zero and Chunk is nil. See
// internal/gen/common.NamedCallableDesc.
type NamedCallableDesc = common.NamedCallableDesc

// Options controls how Generate filters and decorates the rendered output.
//
// Visibilities is the inclusion filter; an empty slice is treated as
// {VisibilityPublic}. Header is prepended verbatim to every produced file
// (typical use: "// AUTO-GENERATED — DO NOT EDIT").
type Options = common.Options

// ParseVisibility accepts the canonical string form produced by
// Visibility.String and returns the matching enum value. Unknown inputs yield
// VisibilityInternal and ok=false so callers can decide whether to reject.
func ParseVisibility(s string) (Visibility, bool) {
	return common.ParseVisibility(s)
}
