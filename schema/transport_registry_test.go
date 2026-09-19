package schema_test

import (
	"errors"
	"testing"

	"github.com/qomos-w/spore/schema"
	"github.com/qomos-w/spore/transport"
)

// TestSchemaSurface_CanMapToTransportRegistry locks §9.1 item 5:
// "可映射到 transport registry"
//
// Verifies that an ObjectDesc can be registered into a transport.Registry
// via the schema.TypeRegistry seam, and that the registered codec can be
// looked up by the class name.
func TestSchemaSurface_CanMapToTransportRegistry(t *testing.T) {
	desc := schema.ObjectDesc{
		Name: "Player",
		Fields: []schema.FieldDesc{
			{Name: "Name", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
			{Name: "Level", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}},
		},
	}

	registry := transport.NewRegistry()
	codec := &transport.JSONCodec{}

	// Register schema into transport registry via TypeRegistry seam
	err := schema.RegisterInTransport(desc, registry, codec)
	if err != nil {
		t.Fatalf("RegisterInTransport: %v", err)
	}

	// Lookup should find the codec under the class name
	found, ok := registry.Lookup("Player")
	if !ok {
		t.Fatal("expected to find codec for Player in transport registry")
	}
	if found != codec {
		t.Fatal("expected the same codec instance")
	}

	// Non-registered name should not be found
	_, ok = registry.Lookup("Unknown")
	if ok {
		t.Fatal("expected not to find codec for unregistered type name")
	}
}

func TestSchemaSurface_RegisterInTransport_NilSafety(t *testing.T) {
	desc := schema.ObjectDesc{Name: "Safe"}

	err := schema.RegisterInTransport(desc, nil, &transport.JSONCodec{})
	if err == nil {
		t.Fatal("expected error for nil registry")
	}
	if !errors.Is(err, schema.ErrNilRegistry) {
		t.Fatalf("expected ErrNilRegistry, got %v", err)
	}

	err = schema.RegisterInTransport(desc, transport.NewRegistry(), nil)
	if err == nil {
		t.Fatal("expected error for nil codec")
	}
	if !errors.Is(err, schema.ErrNilCodec) {
		t.Fatalf("expected ErrNilCodec, got %v", err)
	}

	err = schema.RegisterInTransport(desc, nil, nil)
	if err == nil {
		t.Fatal("expected error for nil registry and codec")
	}
	if !errors.Is(err, schema.ErrNilRegistry) {
		t.Fatalf("expected ErrNilRegistry precedence, got %v", err)
	}
}

func TestSchemaSurface_RegisterInTransport_NonCodecReturnsError(t *testing.T) {
	desc := schema.ObjectDesc{Name: "NonCodec"}
	registry := transport.NewRegistry()

	// Registering a non-Codec value should return a structured error
	err := schema.RegisterInTransport(desc, registry, "not-a-codec")
	if err == nil {
		t.Fatal("expected error for non-Codec value")
	}
	if !errors.Is(err, schema.ErrCodecTypeMismatch) {
		t.Fatalf("expected ErrCodecTypeMismatch, got %v", err)
	}
	if !errors.Is(err, transport.ErrCodecTypeMismatch) {
		t.Fatalf("expected transport ErrCodecTypeMismatch cause, got %v", err)
	}

	// The entry should not be stored
	_, ok := registry.Lookup("NonCodec")
	if ok {
		t.Fatal("expected non-Codec value to not be stored in transport Registry")
	}
}
