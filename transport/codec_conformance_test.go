package transport_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/qomos-w/spore/schema"
	"github.com/qomos-w/spore/transport"
)

// ============================================================================
// Codec SPI conformance test
//
// CodecConformance verifies that a Codec implementation satisfies the
// transport.Codec SPI contract. Any alternative codec backend must pass
// this conformance test to be considered a valid implementation.
//
// Usage:
//
//	func TestMyCodec_Conformance(t *testing.T) {
//	    transport.CodecConformance(t, func() transport.Codec {
//	        return &MyCodec{}
//	    })
//	}
//
// The newCodec function is called for each test to get a fresh instance.
// ============================================================================

// scalarIntDesc and scalarStringDesc are canonical scalar TypeDesc values
// for use in codec contract tests. They are constructed directly rather than
// via schema.DescribeType to avoid depending on foundational TypeID constants
// in the transport test package.
var (
	scalarIntDesc    = schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}
	scalarStringDesc = schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}
)

// CodecConformance tests that a Codec implementation satisfies the
// transport.Codec SPI contract. It covers round-trip fidelity, error
// context preservation, identity preservation, and schema-aware validation.
func CodecConformance(t *testing.T, newCodec func() transport.Codec) {
	t.Run("RoundTrip_ScalarInt", func(t *testing.T) {
		codec := newCodec()
		id := mustCanonicalID(t, 1000, 1, 0, 1)

		view, err := codec.Encode(scalarIntDesc, id, int32(42))
		if err != nil {
			t.Fatalf("Encode: %v", err)
		}
		if view.Kind != transport.ViewKindFull {
			t.Fatalf("expected full view, got %s", view.Kind)
		}
		if view.Identity != id {
			t.Fatalf("identity not preserved in view")
		}

		decoded, err := codec.Decode(view)
		if err != nil {
			t.Fatalf("Decode: %v", err)
		}
		got, ok := decoded.(int32)
		if !ok {
			t.Fatalf("expected int32, got %T", decoded)
		}
		if got != 42 {
			t.Fatalf("expected 42, got %d", got)
		}
	})

	t.Run("RoundTrip_ScalarString", func(t *testing.T) {
		codec := newCodec()
		id := mustCanonicalID(t, 1000, 1, 0, 2)

		view, err := codec.Encode(scalarStringDesc, id, "hello")
		if err != nil {
			t.Fatalf("Encode: %v", err)
		}

		decoded, err := codec.Decode(view)
		if err != nil {
			t.Fatalf("Decode: %v", err)
		}
		got, ok := decoded.(string)
		if !ok {
			t.Fatalf("expected string, got %T", decoded)
		}
		if got != "hello" {
			t.Fatalf("expected hello, got %s", got)
		}
	})

	t.Run("RoundTrip_ScalarBool", func(t *testing.T) {
		codec := newCodec()
		id := mustCanonicalID(t, 1000, 1, 0, 3)
		boolDesc := schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "bool"}

		view, err := codec.Encode(boolDesc, id, true)
		if err != nil {
			t.Fatalf("Encode: %v", err)
		}

		decoded, err := codec.Decode(view)
		if err != nil {
			t.Fatalf("Decode: %v", err)
		}
		got, ok := decoded.(bool)
		if !ok {
			t.Fatalf("expected bool, got %T", decoded)
		}
		if got != true {
			t.Fatalf("expected true, got %v", got)
		}
	})

	t.Run("RoundTrip_Struct", func(t *testing.T) {
		codec := newCodec()
		id := mustCanonicalID(t, 1000, 1, 0, 4)

		type player struct {
			Name  string
			Level int
		}
		p := player{Name: "alice", Level: 5}
		view, err := codec.Encode(schema.TypeDesc{
			Kind:      schema.TypeKindStruct,
			Name:      "struct",
			ClassName: "player",
		}, id, p)
		if err != nil {
			t.Fatalf("Encode: %v", err)
		}

		decoded, err := codec.Decode(view)
		if err != nil {
			t.Fatalf("Decode: %v", err)
		}
		decodedMap, ok := decoded.(map[string]any)
		if !ok {
			t.Fatalf("expected map[string]any for struct, got %T", decoded)
		}
		if decodedMap["Name"] != "alice" {
			t.Fatalf("expected Name=alice, got %v", decodedMap["Name"])
		}
	})

	t.Run("RoundTrip_StructAsMap", func(t *testing.T) {
		codec := newCodec()
		id := mustCanonicalID(t, 1000, 1, 0, 50)

		// map[string]any is the canonical projection form produced by
		// binding.ProjectView for struct-typed fields. The codec must
		// accept it as a valid encode input for TypeKindStruct.
		m := map[string]any{
			"Name":  "alice",
			"Level": int32(5),
		}
		view, err := codec.Encode(schema.TypeDesc{
			Kind:      schema.TypeKindStruct,
			Name:      "struct",
			ClassName: "player",
		}, id, m)
		if err != nil {
			t.Fatalf("Encode map[string]any as struct: %v", err)
		}
		if view.Kind != transport.ViewKindFull {
			t.Fatalf("expected full view, got %s", view.Kind)
		}
		if view.Identity != id {
			t.Fatalf("identity not preserved in view")
		}

		decoded, err := codec.Decode(view)
		if err != nil {
			t.Fatalf("Decode: %v", err)
		}
		decodedMap, ok := decoded.(map[string]any)
		if !ok {
			t.Fatalf("expected map[string]any, got %T", decoded)
		}
		if decodedMap["Name"] != "alice" {
			t.Fatalf("expected Name=alice, got %v", decodedMap["Name"])
		}
	})

	t.Run("EncodeError_StructRejectsNonCanonicalMap", func(t *testing.T) {
		codec := newCodec()
		id := mustCanonicalID(t, 1000, 1, 0, 51)
		structDesc := schema.TypeDesc{
			Kind:      schema.TypeKindStruct,
			Name:      "struct",
			ClassName: "player",
		}

		tests := []struct {
			name  string
			value any
		}{
			{name: "non string key map", value: map[int]any{1: "alice"}},
			{name: "typed string map", value: map[string]string{"Name": "alice"}},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				_, err := codec.Encode(structDesc, id, tt.value)
				if err == nil {
					t.Fatal("expected encode error for non-canonical struct map, got nil")
				}

				var encErr *transport.EncodeError
				if !isEncodeError(err, &encErr) {
					t.Fatalf("expected *transport.EncodeError, got %T: %v", err, err)
				}
				if encErr.SchemaName == "" {
					t.Fatal("EncodeError missing SchemaName")
				}
				if encErr.Identity != id {
					t.Fatal("EncodeError missing identity context")
				}
			})
		}
	})

	t.Run("RoundTrip_ArrayOfInts", func(t *testing.T) {
		codec := newCodec()
		id := mustCanonicalID(t, 1000, 1, 0, 5)
		arrayDesc := schema.TypeDesc{
			Kind:    schema.TypeKindArray,
			Name:    "array",
			Element: &scalarIntDesc,
		}

		view, err := codec.Encode(arrayDesc, id, []int32{1, 2, 3})
		if err != nil {
			t.Fatalf("Encode: %v", err)
		}

		decoded, err := codec.Decode(view)
		if err != nil {
			t.Fatalf("Decode: %v", err)
		}
		arr, ok := decoded.([]any)
		if !ok {
			t.Fatalf("expected []any, got %T", decoded)
		}
		if len(arr) != 3 {
			t.Fatalf("expected 3 elements, got %d", len(arr))
		}
	})

	t.Run("RoundTrip_MapStringInt", func(t *testing.T) {
		codec := newCodec()
		id := mustCanonicalID(t, 1000, 1, 0, 6)
		mapDesc := schema.TypeDesc{
			Kind:  schema.TypeKindMap,
			Name:  "map",
			Key:   &scalarStringDesc,
			Value: &scalarIntDesc,
		}

		view, err := codec.Encode(mapDesc, id, map[string]int32{"a": 1, "b": 2})
		if err != nil {
			t.Fatalf("Encode: %v", err)
		}

		decoded, err := codec.Decode(view)
		if err != nil {
			t.Fatalf("Decode: %v", err)
		}
		m, ok := decoded.(map[string]any)
		if !ok {
			t.Fatalf("expected map[string]any, got %T", decoded)
		}
		if len(m) != 2 {
			t.Fatalf("expected 2 keys, got %d", len(m))
		}
	})

	t.Run("EncodeError_ContainsSchemaAndIdentity", func(t *testing.T) {
		codec := newCodec()
		id := mustCanonicalID(t, 1000, 1, 0, 10)

		_, err := codec.Encode(scalarIntDesc, id, "not-an-int")
		if err == nil {
			t.Fatal("expected encode error for type mismatch, got nil")
		}

		var encErr *transport.EncodeError
		if !isEncodeError(err, &encErr) {
			t.Fatalf("expected *transport.EncodeError, got %T: %v", err, err)
		}
		if encErr.SchemaName == "" {
			t.Fatal("EncodeError missing SchemaName")
		}
		if encErr.Identity != id {
			t.Fatal("EncodeError missing identity context")
		}
	})

	t.Run("EncodeError_PathFieldExists", func(t *testing.T) {
		codec := newCodec()
		id := mustCanonicalID(t, 1000, 1, 0, 20)

		_, err := codec.Encode(scalarIntDesc, id, "not-an-int")
		if err == nil {
			t.Fatal("expected encode error, got nil")
		}

		var encErr *transport.EncodeError
		if !isEncodeError(err, &encErr) {
			t.Fatalf("expected *transport.EncodeError, got %T: %v", err, err)
		}
		// Path must be present (even if empty for top-level failures).
		// This verifies Path is explicitly considered, not accidentally zero.
		_ = encErr.Path
		if encErr.SchemaName == "" {
			t.Fatal("EncodeError missing SchemaName")
		}
		if encErr.Identity != id {
			t.Fatal("EncodeError missing identity context")
		}
	})

	t.Run("DecodeError_ContainsSchemaAndIdentity", func(t *testing.T) {
		codec := newCodec()
		id := mustCanonicalID(t, 1000, 1, 0, 11)

		// Encode successfully first
		view, err := codec.Encode(scalarIntDesc, id, int32(7))
		if err != nil {
			t.Fatalf("Encode: %v", err)
		}

		// Corrupt the data to force a decode failure
		view.Data = []byte{0xFF, 0xFE, 0xFD}

		_, err = codec.Decode(view)
		if err == nil {
			t.Fatal("expected decode error for corrupt data, got nil")
		}

		var decErr *transport.DecodeError
		if !isDecodeError(err, &decErr) {
			t.Fatalf("expected *transport.DecodeError, got %T: %v", err, err)
		}
		if decErr.SchemaName == "" {
			t.Fatal("DecodeError missing SchemaName")
		}
	})

	t.Run("DecodeError_PathFieldExists", func(t *testing.T) {
		codec := newCodec()
		id := mustCanonicalID(t, 1000, 1, 0, 21)

		view, err := codec.Encode(scalarIntDesc, id, int32(7))
		if err != nil {
			t.Fatalf("Encode: %v", err)
		}
		view.Data = []byte{0xFF, 0xFE, 0xFD}

		_, err = codec.Decode(view)
		if err == nil {
			t.Fatal("expected decode error, got nil")
		}

		var decErr *transport.DecodeError
		if !isDecodeError(err, &decErr) {
			t.Fatalf("expected *transport.DecodeError, got %T: %v", err, err)
		}
		// Path must be present (even if empty for top-level failures)
		_ = decErr.Path
		if decErr.SchemaName == "" {
			t.Fatal("DecodeError missing SchemaName")
		}
	})

	t.Run("EncodeRejectsNilValue", func(t *testing.T) {
		codec := newCodec()
		id := mustCanonicalID(t, 1000, 1, 0, 40)

		_, err := codec.Encode(scalarIntDesc, id, nil)
		if err == nil {
			t.Fatal("expected encode error for nil value, got nil")
		}
	})

	t.Run("ViewCarriesIdentity", func(t *testing.T) {
		codec := newCodec()
		id := mustCanonicalID(t, 2000, 2, 1, 42)

		view, err := codec.Encode(scalarIntDesc, id, int32(99))
		if err != nil {
			t.Fatalf("Encode: %v", err)
		}
		if view.Identity.IsZero() {
			t.Fatal("view identity must not be zero")
		}
		if view.Identity != id {
			t.Fatalf("identity mismatch: expected %s, got %s", id, view.Identity)
		}
	})

	t.Run("ViewCarriesSchema", func(t *testing.T) {
		codec := newCodec()
		id := mustCanonicalID(t, 2000, 2, 1, 43)

		view, err := codec.Encode(scalarIntDesc, id, int32(1))
		if err != nil {
			t.Fatalf("Encode: %v", err)
		}
		if view.Schema.Kind != schema.TypeKindScalar {
			t.Fatalf("expected scalar schema, got %s", view.Schema.Kind)
		}
	})

	t.Run("SequentialOperations_StateIndependent", func(t *testing.T) {
		// Verify that multiple encode/decode cycles on the same codec instance
		// produce independent results with no state leakage.
		codec := newCodec()
		id := mustCanonicalID(t, 3000, 1, 0, 1)

		// First encode/decode: int
		view1, err := codec.Encode(scalarIntDesc, id, int32(10))
		if err != nil {
			t.Fatalf("Encode int: %v", err)
		}
		dec1, err := codec.Decode(view1)
		if err != nil {
			t.Fatalf("Decode int: %v", err)
		}
		if dec1.(int32) != 10 {
			t.Fatalf("first decode: expected 10, got %v", dec1)
		}

		// Second encode/decode: string (different type, same codec)
		id2 := mustCanonicalID(t, 3000, 1, 0, 2)
		view2, err := codec.Encode(scalarStringDesc, id2, "second")
		if err != nil {
			t.Fatalf("Encode string: %v", err)
		}
		dec2, err := codec.Decode(view2)
		if err != nil {
			t.Fatalf("Decode string: %v", err)
		}
		if dec2.(string) != "second" {
			t.Fatalf("second decode: expected second, got %v", dec2)
		}

		// Third encode/decode: re-decode first view — must still produce original
		dec1Again, err := codec.Decode(view1)
		if err != nil {
			t.Fatalf("Re-decode first view: %v", err)
		}
		if dec1Again.(int32) != 10 {
			t.Fatalf("re-decode first view: expected 10, got %v", dec1Again)
		}
	})
	t.Run("ConcurrentEncodeDecode_NoDataRace", func(t *testing.T) {
		// Verify that concurrent Encode and Decode on the same codec instance
		// do not cause data races. Use -race to detect issues.
		codec := newCodec()

		const goroutines = 10
		const opsPer = 20
		done := make(chan error, goroutines*opsPer)
		var wg sync.WaitGroup

		for i := 0; i < goroutines; i++ {
			wg.Add(1)
			go func(seq int) {
				defer wg.Done()
				id := mustCanonicalID(t, uint64(4000+seq), 1, 0, uint64(seq))
				for j := 0; j < opsPer; j++ {
					// Encode
					view, err := codec.Encode(scalarIntDesc, id, int32(seq*100+j))
					if err != nil {
						done <- fmt.Errorf("encode seq=%d j=%d: %w", seq, j, err)
						continue
					}
					// Decode
					decoded, err := codec.Decode(view)
					if err != nil {
						done <- fmt.Errorf("decode seq=%d j=%d: %w", seq, j, err)
						continue
					}
					got, ok := decoded.(int32)
					if !ok || got != int32(seq*100+j) {
						done <- fmt.Errorf("round-trip mismatch seq=%d j=%d: expected %d, got %v", seq, j, seq*100+j, decoded)
						continue
					}
				}
			}(i)
		}

		// Wait for all goroutines to finish, then collect errors
		wg.Wait()
		close(done)
		for err := range done {
			t.Errorf("concurrent operation error: %v", err)
		}
	})
}

// TestJSONCodec_Conformance runs the Codec SPI conformance test suite
// against the default JSONCodec backend.
func TestJSONCodec_Conformance(t *testing.T) {
	CodecConformance(t, func() transport.Codec {
		return &transport.JSONCodec{}
	})
}

// TestBinaryCodec_Conformance runs the Codec SPI conformance test suite
// against the BinaryCodec backend.
func TestBinaryCodec_Conformance(t *testing.T) {
	CodecConformance(t, func() transport.Codec {
		return &transport.BinaryCodec{}
	})
}
