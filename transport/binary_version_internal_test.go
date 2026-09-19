package transport

import (
	"testing"

	"github.com/qomos-w/spore/schema"
)

// The wire version is now an explicit header field, decoupled from the magic:
// the frame is magic ("TBC") followed by one version byte. These tests pin the
// new/old frame determination: the current version is accepted, the legacy
// "TBC\x02" version is rejected, and an unknown version is rejected.

func TestBinaryWireVersion_HeaderLayout(t *testing.T) {
	id := mustBinaryCanonicalID(t, 300)
	view, err := (&BinaryCodec{}).Encode(
		schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "bool"}, id, true)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if len(view.Data) < binaryHeaderLen {
		t.Fatalf("frame shorter than header: %d bytes", len(view.Data))
	}
	if string(view.Data[:len(binaryMagic)]) != binaryMagic {
		t.Fatalf("expected magic %q, got %q", binaryMagic, view.Data[:len(binaryMagic)])
	}
	if got := view.Data[len(binaryMagic)]; got != binaryWireVersion {
		t.Fatalf("expected wire version %d, got %d", binaryWireVersion, got)
	}
}

func TestBinaryWireVersion_LegacyVersionRejected(t *testing.T) {
	id := mustBinaryCanonicalID(t, 301)
	view, err := (&BinaryCodec{}).Encode(
		schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "bool"}, id, true)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	// Rewrite the version byte to the legacy glued-on version ("TBC\x02").
	legacy := append([]byte(nil), view.Data...)
	legacy[len(binaryMagic)] = binaryLegacyWireVersion

	legacyView := View{Kind: ViewKindFull, Schema: view.Schema, Identity: id, Data: legacy}
	if _, err := (&BinaryCodec{}).Decode(legacyView); err == nil {
		t.Fatal("expected legacy wire version to be rejected")
	}
	if err := (&BinaryCodec{}).DecodeInto(legacyView, new(bool)); err == nil {
		t.Fatal("expected legacy wire version to be rejected by DecodeInto")
	}
}

func TestBinaryWireVersion_UnknownVersionRejected(t *testing.T) {
	id := mustBinaryCanonicalID(t, 302)
	view, err := (&BinaryCodec{}).Encode(
		schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "bool"}, id, true)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	unknown := append([]byte(nil), view.Data...)
	unknown[len(binaryMagic)] = 0x7f

	if _, err := (&BinaryCodec{}).Decode(View{Kind: ViewKindFull, Schema: view.Schema, Identity: id, Data: unknown}); err == nil {
		t.Fatal("expected unknown wire version to be rejected")
	}
}

func TestBinaryWireVersion_BadMagicRejected(t *testing.T) {
	id := mustBinaryCanonicalID(t, 303)
	view, err := (&BinaryCodec{}).Encode(
		schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "bool"}, id, true)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	bad := append([]byte(nil), view.Data...)
	bad[0] = 0x00

	if _, err := (&BinaryCodec{}).Decode(View{Kind: ViewKindFull, Schema: view.Schema, Identity: id, Data: bad}); err == nil {
		t.Fatal("expected bad magic to be rejected")
	}
}

func TestBinaryWireVersion_TruncatedHeaderRejected(t *testing.T) {
	id := mustBinaryCanonicalID(t, 304)
	// Magic only, missing the version byte.
	short := []byte(binaryMagic)
	if _, err := (&BinaryCodec{}).Decode(View{Kind: ViewKindFull, Schema: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "bool"}, Identity: id, Data: short}); err == nil {
		t.Fatal("expected truncated header to be rejected")
	}
}

// TestWireVersionSupportsEntrySequence pins the explicit, version-driven
// entry-sequence capability that replaced the old value-shape heuristic.
func TestWireVersionSupportsEntrySequence(t *testing.T) {
	if !wireVersionSupportsEntrySequence() {
		t.Fatalf("current wire version %d must support entry sequences", binaryWireVersion)
	}
	if binaryWireVersion < entrySequenceWireVersion {
		t.Fatalf("current wire version %d predates entry sequences (%d)", binaryWireVersion, entrySequenceWireVersion)
	}
}

// TestBinaryCodec_EntrySequenceDeterminedBySchemaKind asserts the container is
// chosen by the schema kind, not by the runtime shape of the value: a map-kind
// scope always emits TAG_ENTRY_SEQUENCE, and an array-kind scope never does,
// even when both carry the same []struct{Key,Value} shape.
func TestBinaryCodec_EntrySequenceDeterminedBySchemaKind(t *testing.T) {
	codec := &BinaryCodec{}
	mapDesc := schema.TypeDesc{
		Kind:  schema.TypeKindMap,
		Name:  "map",
		Key:   &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"},
		Value: &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"},
	}
	arrDesc := schema.TypeDesc{
		Kind:    schema.TypeKindArray,
		Name:    "array",
		Element: &schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "kvRow"},
	}

	entries := []any{
		map[string]any{"Key": "a", "Value": int32(1)},
		map[string]any{"Key": "b", "Value": int32(2)},
	}

	mapView, err := codec.Encode(mapDesc, mustBinaryCanonicalID(t, 305), entries)
	if err != nil {
		t.Fatalf("Encode map: %v", err)
	}
	if mapView.Data[binaryHeaderLen] != binaryTagEntrySequence {
		t.Fatalf("map scope: expected TAG_ENTRY_SEQUENCE, got 0x%02x", mapView.Data[binaryHeaderLen])
	}

	arrView, err := codec.Encode(arrDesc, mustBinaryCanonicalID(t, 306), []kvRow{{Key: "a", Value: 1, Extra: "x"}})
	if err != nil {
		t.Fatalf("Encode array: %v", err)
	}
	if arrView.Data[binaryHeaderLen] != binaryTagArray {
		t.Fatalf("array scope: expected TAG_ARRAY, got 0x%02x", arrView.Data[binaryHeaderLen])
	}
}
