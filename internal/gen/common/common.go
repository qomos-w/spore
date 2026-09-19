// Package common holds the logic shared by the four spore code generators in
// internal/gen: ts, ts-client, go-server and go-types. It owns
//
//   - the generator input contract — Visibility, Options, NamedObjectDesc and
//     NamedCallableDesc — so every generator filters and validates the same
//     way;
//   - ValidateCallables, the shared callable descriptor validator;
//   - WriteHeader, the shared file-header writer;
//   - the single authoritative Spore scalar → target-type table (scalar.go).
//
// Convergence rationale: these generators used to carry verbatim copies of the
// validator/header helpers and four independently maintained scalar tables
// that had drifted — go-types could not render byte/short/int8/int16, and
// go-server could not render any. With one table here, drift is impossible by
// construction: a scalar is declared once and every target sees it.
package common

import "github.com/qomos-w/spore/schema"

// Visibility classifies the exposure level of a callable or component.
//
// The default zero-value is VisibilityInternal: only actor-to-actor traffic
// inside the same gospore process. Higher levels open the surface to external
// clients in a controlled way; codegen filters the rendered output by the same
// enum so each client bundle only sees its allowed subset.
type Visibility int

const (
	VisibilityInternal   Visibility = iota // default; not exposed externally
	VisibilityPublic                       // browsers, mobile clients
	VisibilityAdmin                        // ops consoles
	VisibilityDiagnostic                   // telemetry / observability tools
)

// String returns the lower-case canonical name written into generated output
// and used as the Annotation key value when scriptbridge surfaces the
// visibility to clients.
func (v Visibility) String() string {
	switch v {
	case VisibilityPublic:
		return "public"
	case VisibilityAdmin:
		return "admin"
	case VisibilityDiagnostic:
		return "diagnostic"
	default:
		return "internal"
	}
}

// ParseVisibility accepts the canonical string form produced by String and
// returns the matching enum value. Unknown inputs yield VisibilityInternal and
// ok=false so callers can decide whether to reject.
func ParseVisibility(s string) (Visibility, bool) {
	switch s {
	case "internal", "":
		return VisibilityInternal, true
	case "public":
		return VisibilityPublic, true
	case "admin":
		return VisibilityAdmin, true
	case "diagnostic":
		return VisibilityDiagnostic, true
	default:
		return VisibilityInternal, false
	}
}

// NamedObjectDesc is one entry in the codegen input: a struct/class shape keyed
// by its (Namespace, SchemaID, Name) triple plus its visibility.
//
// The generators render ObjectDesc instances as TypeScript interfaces or Go
// structs. Users obtain ObjectDesc via schema.DescribeGoStruct(MyStruct{}).
// Top-level non-struct schemas (a bare scalar or array exposed as a schema
// entry) are out of scope; wrap them in a struct on the Go side.
type NamedObjectDesc struct {
	Namespace  string
	SchemaID   uint64
	Name       string
	Object     schema.ObjectDesc
	Visibility Visibility
}

// NamedCallableDesc is one callable entry in the codegen input. Each callable
// is keyed by its (Namespace, Name) pair, scoped by Visibility, and carries the
// schema IDs that the wire layer uses to route request / chunk / final frames
// belonging to this callable.
//
// Mode is one of schema.CallableMode ("unary" or "streaming"). For streaming
// callables the consumer iterates Chunk-shaped frames before receiving the
// terminal Final value; ChunkSchemaID and Chunk MUST be populated. For unary
// callables ChunkSchemaID is zero and Chunk is nil — only Req and Final
// participate.
//
// Description is human-readable documentation forwarded into JSDoc on the
// generated TypeScript surface. It is optional; reflect-driven manifest
// producers leave it empty (Go method values have no doc-string source), but
// hand-written or annotation-driven manifests can supply it.
//
// The Req / Chunk / Final TypeDescs describe the wire payload shape. They
// typically point at struct types registered as separate NamedObjectDesc
// entries (so the schema registry holds the field-level shape), but nothing
// here enforces cross-referencing; the manifest producer is responsible for
// keeping the two lists consistent.
type NamedCallableDesc struct {
	Namespace     string
	Name          string
	Description   string
	Visibility    Visibility
	Mode          schema.CallableMode
	ReqSchemaID   uint64
	ChunkSchemaID uint64 // 0 for unary
	FinalSchemaID uint64
	Req           schema.TypeDesc
	Chunk         *schema.TypeDesc // nil for unary
	Final         schema.TypeDesc
}

// Options controls how a generator filters and decorates the rendered output.
//
// Visibilities is the inclusion filter; an empty slice is treated as
// {VisibilityPublic}. Header is prepended verbatim to every produced file
// (typical use: "// AUTO-GENERATED — DO NOT EDIT").
type Options struct {
	Visibilities []Visibility
	Header       string
}

// ShouldEmit reports whether the entry passes this Options visibility filter.
func (o Options) ShouldEmit(v Visibility) bool {
	return ShouldEmit(o.Visibilities, v)
}

// ShouldEmit reports whether v passes the visibility filter want. An empty want
// defaults to {VisibilityPublic} — the common case for browser-facing client
// bundles and for the generator CLIs' default --visibility.
func ShouldEmit(want []Visibility, v Visibility) bool {
	if len(want) == 0 {
		want = []Visibility{VisibilityPublic}
	}
	for _, w := range want {
		if w == v {
			return true
		}
	}
	return false
}
