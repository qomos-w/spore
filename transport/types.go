// Package transport defines the schema-aware transport projection surface.
//
// Transport consumes schema descriptors and produces stable wire views.
// It does not define schema, does not drive runtime, and does not serve as
// truth authority. It is a projection layer: schema-aware, identity-attached,
// and diagnosable.
package transport

import (
	"errors"
	"fmt"

	"github.com/qomos-w/spore/diagnostics"
	"github.com/qomos-w/spore/identity"
	"github.com/qomos-w/spore/schema"
)

var (
	ErrCodecTypeMismatch = errors.New("spore/transport: codec does not satisfy transport.Codec")
)

const (
	CodeEncodeError = "encode_error"
	CodeDecodeError = "decode_error"
)

// transportDiagnosticCodes is this package's diagnostic-code table; init()
// registers it in order. Kept package-local so transport stays autonomous.
var transportDiagnosticCodes = []diagnostics.CodeInfo{
	{Code: CodeEncodeError, Category: diagnostics.CategoryTransport, Description: "Transport encoding failed", Hint: "检查待编码值是否符合 schema 类型要求"},
	{Code: CodeDecodeError, Category: diagnostics.CategoryTransport, Description: "Transport decoding failed", Hint: "检查传输数据是否完整且版本兼容"},
}

func init() {
	for _, info := range transportDiagnosticCodes {
		diagnostics.RegisterCode(info)
	}
}

// ViewKind classifies the transport view projection.
// Contract: public semantic contract — view projection classification.
type ViewKind string

const (
	ViewKindFull  ViewKind = "full"
	ViewKindPatch ViewKind = "patch"
)

// View is a schema-aware, identity-attached transport projection of runtime state.
// Contract: public semantic contract — the stable transport view shape carrying
// schema, identity, and encoded data.
type View struct {
	Kind     ViewKind
	Schema   schema.TypeDesc
	Identity identity.CanonicalID
	Data     []byte
}

// EncodeError carries schema/path/identity context for encode failures.
// Contract: public semantic contract — structured encode error with
// schema/path/identity context for diagnosability.
type EncodeError struct {
	SchemaName string
	Path       string
	Identity   identity.CanonicalID
	Err        error
	Expected   string // Expected schema/type for LLM-guided repair
	Actual     string // Actual value/type for LLM-guided repair
}

func (e *EncodeError) Error() string {
	if e == nil || e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func (e *EncodeError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func (e *EncodeError) DiagnosticCode() string {
	if e == nil {
		return ""
	}
	if coder, ok := e.Err.(interface{ DiagnosticCode() string }); ok {
		return coder.DiagnosticCode()
	}
	return CodeEncodeError
}

func (e *EncodeError) DiagnosticPath() string {
	if e == nil {
		return ""
	}
	return e.Path
}

func (e *EncodeError) DiagnosticExpected() string {
	if e == nil {
		return ""
	}
	return e.Expected
}

func (e *EncodeError) DiagnosticActual() string {
	if e == nil {
		return ""
	}
	return e.Actual
}

func (e *EncodeError) DiagnosticCause() *diagnostics.Descriptor {
	if e == nil || e.Err == nil {
		return nil
	}
	d := diagnostics.FromError(e.Err, diagnostics.Descriptor{})
	return diagnostics.ClonePtr(&d)
}

// DecodeError carries schema/path/identity context for decode failures.
// Contract: public semantic contract — structured decode error with
// schema/path/identity context for diagnosability.
type DecodeError struct {
	SchemaName string
	Path       string
	Identity   identity.CanonicalID
	Err        error
	Expected   string // Expected schema/type for LLM-guided repair
	Actual     string // Actual value/type for LLM-guided repair
}

func (e *DecodeError) Error() string {
	if e == nil || e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func (e *DecodeError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func (e *DecodeError) DiagnosticCode() string {
	if e == nil {
		return ""
	}
	if coder, ok := e.Err.(interface{ DiagnosticCode() string }); ok {
		return coder.DiagnosticCode()
	}
	return CodeDecodeError
}

func (e *DecodeError) DiagnosticPath() string {
	if e == nil {
		return ""
	}
	return e.Path
}

func (e *DecodeError) DiagnosticExpected() string {
	if e == nil {
		return ""
	}
	return e.Expected
}

func (e *DecodeError) DiagnosticActual() string {
	if e == nil {
		return ""
	}
	return e.Actual
}

func (e *DecodeError) DiagnosticCause() *diagnostics.Descriptor {
	if e == nil || e.Err == nil {
		return nil
	}
	d := diagnostics.FromError(e.Err, diagnostics.Descriptor{})
	return diagnostics.ClonePtr(&d)
}

// Codec is the schema-aware encode/decode contract.
// Implementations must preserve round-trip fidelity for any schema-described value.
// SPI: pluggable codec backend — alternative implementations can be registered
// with Registry. JSONCodec is the default backend.
type Codec interface {
	// Encode projects a Go value into a transport view, guided by schema.
	// The returned view carries identity and schema metadata.
	Encode(s schema.TypeDesc, id identity.CanonicalID, value any) (View, error)

	// Decode reconstructs a Go value from a transport view, validated against schema.
	Decode(view View) (any, error)
}

// IntoDecoder is an optional Codec extension that decodes wire data directly
// into a caller-supplied target value using reflection, avoiding the generic
// map[string]any intermediate form for struct types.
type IntoDecoder interface {
	DecodeInto(view View, target any) error
}

// EnvelopeKind classifies the wire envelope type.
// Contract: public semantic contract — wire envelope classification.
type EnvelopeKind string

const (
	EnvelopeKindRoute  EnvelopeKind = "route"
	EnvelopeKindBinary EnvelopeKind = "binary"
)

// Envelope is a route or binary wire message carrying a transport view.
// Contract: public semantic contract — wire envelope for route and binary messages.
type Envelope struct {
	Kind  EnvelopeKind
	Route string
	View  View
}

// Registry maps schema type names to codec backends.
// Contract: public semantic contract — type→codec mapping for transport dispatch.
// Not an SPI: currently a concrete type. Promoted to SPI only when a real
// alternative registry backend is needed (per ARCHITECTURE §5.2).
type Registry struct {
	codecs map[string]Codec
}

func NewRegistry() *Registry {
	return &Registry{
		codecs: make(map[string]Codec),
	}
}

// Register associates a codec with a schema type name.
func (r *Registry) Register(typeName string, codec Codec) {
	r.codecs[typeName] = codec
}

// RegisterType satisfies schema.TypeRegistry by accepting any codec value.
// If the value implements Codec, it is stored; otherwise an error is returned.
func (r *Registry) RegisterType(typeName string, codec any) error {
	if c, ok := codec.(Codec); ok {
		r.codecs[typeName] = c
		return nil
	}
	return fmt.Errorf("%w: type %q", ErrCodecTypeMismatch, typeName)
}

// Lookup returns the codec for a schema type name.
func (r *Registry) Lookup(typeName string) (Codec, bool) {
	c, ok := r.codecs[typeName]
	return c, ok
}
