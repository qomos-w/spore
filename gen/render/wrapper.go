// Package render is the public, embedding-facing façade for spore code
// generation.
//
// The four generators live in internal/gen:
//
//   - internal/gen/ts        TypeScript schema interfaces + callable metadata
//   - internal/gen/ts-client TypeScript client classes
//   - internal/gen/go-server Go server dispatchers
//   - internal/gen/go-types  Go type declarations
//
// Those packages are internal to the spore module, but external embedders (for
// example gospore) need to drive generation in-process instead of shelling out
// to the cmd/spore-gen-* binaries. This package is that seam: it re-exports all
// four generators behind one public entry point each, over the visibility /
// options contract they share, so external callers never import internal
// packages and the four generators present a symmetric public surface.
//
// The four CLIs consume this same façade. The only generator-owned code a CLI
// still imports directly is internal CLI plumbing — flag registration and
// manifest decoding (internal/gen/common, internal/gen/manifest) or .spore
// parsing (internal/script/frontend); the generation call itself always goes
// through this package.
//
// Entry points, one per generator, named after what they emit:
//
//	Generate              TypeScript schema types + callable metadata
//	GenerateTSClient      TypeScript client classes
//	GenerateGoServer      Go server dispatcher files
//	RenderGoTypes         Go type-declaration files
//	RenderGoTypesRegistry Go registry file (schema ID → type table)
//
// Visibility filtering is shared: an empty Options.Visibilities defaults to
// {VisibilityPublic}; see Visibility and ParseVisibility.
package render

import (
	goserver "github.com/qomos-w/spore/internal/gen/go-server"
	gotypes "github.com/qomos-w/spore/internal/gen/go-types"
	"github.com/qomos-w/spore/internal/gen/ts"
	tsclient "github.com/qomos-w/spore/internal/gen/ts-client"
	"github.com/qomos-w/spore/schema"
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

// Options controls how the TypeScript generators (Generate, GenerateTSClient)
// filter and decorate the rendered output.
type Options = ts.Options

// GoServerOptions configures GenerateGoServer. It mirrors Options and adds the
// Go Package name written into every emitted file.
type GoServerOptions = goserver.Options

// GoTypesOptions configures RenderGoTypes and RenderGoTypesRegistry.
type GoTypesOptions = gotypes.Options

// RegistryEntry is one row of the Go schema registry consumed by
// RenderGoTypesRegistry.
type RegistryEntry = gotypes.RegistryEntry

// ParseVisibility accepts the canonical string form and returns the matching
// enum value and ok=true. If the string is unknown, ok=false is returned.
func ParseVisibility(s string) (Visibility, bool) {
	return ts.ParseVisibility(s)
}

// Generate renders schema descriptors and callable descriptors into a
// path → file content map of TypeScript files. See internal/gen/ts.Generate
// for the output layout and retention rules.
func Generate(schemas []NamedObjectDesc, callables []NamedCallableDesc, opts Options) (map[string]string, error) {
	return ts.Generate(schemas, callables, opts)
}

// GenerateTSClient renders callable descriptors into a path → file content map
// of TypeScript client classes, one `<namespace>/client.ts` per surviving
// namespace. Schema entries are not needed. See internal/gen/ts-client.
func GenerateTSClient(callables []NamedCallableDesc, opts Options) (map[string]string, error) {
	return tsclient.Generate(callables, opts)
}

// GenerateGoServer renders callable descriptors into Go server dispatcher
// files, one `<namespace>_dispatcher_gen.go` per surviving namespace. schemas
// is consulted to resolve each callable's request/final field shapes. See
// internal/gen/go-server.
func GenerateGoServer(schemas []NamedObjectDesc, callables []NamedCallableDesc, opts GoServerOptions) (map[string]string, error) {
	return goserver.Generate(schemas, callables, opts)
}

// RenderGoTypes renders one Go source file declaring a struct per struct-kind
// ObjectDesc. See internal/gen/go-types.Render for the full option surface.
func RenderGoTypes(objs []schema.ObjectDesc, opts GoTypesOptions) ([]byte, error) {
	return gotypes.Render(objs, opts)
}

// RenderGoTypesRegistry renders the Go registry file that maps schema IDs to
// type names and (optionally) emits the @component registry table. See
// internal/gen/go-types.RenderRegistry.
func RenderGoTypesRegistry(entries []RegistryEntry, opts GoTypesOptions) ([]byte, error) {
	return gotypes.RenderRegistry(entries, opts)
}

// AssignSequentialSchemaIDs fills in schema IDs from a source-file label
// (e.g. "agent.chat._300.spore" → 300, 301, ...) while preserving explicit
// @schema(N) annotations. Callers that build entries from .spore files need it
// before RenderGoTypes; see internal/gen/go-types.
func AssignSequentialSchemaIDs(objs []schema.ObjectDesc, sourcePath string) {
	gotypes.AssignSequentialSchemaIDs(objs, sourcePath)
}
