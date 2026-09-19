package schema

import (
	"errors"
	"fmt"
)

var (
	ErrNilRegistry      = errors.New("spore/schema: type registry is nil")
	ErrNilCodec         = errors.New("spore/schema: codec is nil")
	ErrCodecTypeMismatch = errors.New("spore/schema: codec type mismatch")
)

// TypeRegistry is the schema-side contract for a type→codec registry.
// Transport.Registry can satisfy this interface via a thin adapter, allowing
// schema to register type→codec mappings without importing the transport
// package (avoiding import cycles). This is the schema surface's registration
// entry point into the transport layer, as defined in ARCHITECTURE §3.1.
//
// Contract: public semantic contract — schema→transport registry mapping seam.
type TypeRegistry interface {
	// RegisterType associates a codec with a type name.
	// Returns an error if the codec is not compatible.
	RegisterType(typeName string, codec any) error
}

// RegisterInTransport maps an ObjectDesc to a TypeRegistry by registering
// the provided codec under the object name. This is the canonical entry point
// for schema→transport registration.
//
// Contract: public semantic contract — schema→transport registry mapping.
func RegisterInTransport(desc ObjectDesc, registry TypeRegistry, codec any) error {
	if registry == nil {
		return fmt.Errorf("%w", ErrNilRegistry)
	}
	if codec == nil {
		return fmt.Errorf("%w: type %q", ErrNilCodec, desc.Name)
	}
	if err := registry.RegisterType(desc.Name, codec); err != nil {
		return fmt.Errorf("%w: type %q: %w", ErrCodecTypeMismatch, desc.Name, err)
	}
	return nil
}
