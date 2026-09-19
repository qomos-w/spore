package transport

import (
	"bytes"
	"testing"

	"github.com/qomos-w/spore/identity"
	"github.com/qomos-w/spore/schema"
)

// kvRow is an array<struct> element that happens to expose Key/Value fields.
// The binary codec must NOT treat it as a map entry sequence — it must encode
// every field, including Extra.
type kvRow struct {
	Key   string
	Value int
	Extra string
}

func mustBinaryCanonicalID(tb testing.TB, seq uint64) identity.CanonicalID {
	tb.Helper()
	id, err := identity.NewCanonicalID(1000, 1, 0, seq)
	if err != nil {
		tb.Fatalf("NewCanonicalID: %v", err)
	}
	return id
}

// TestBinaryCodec_ArrayOfStructNotEntrySeq guards against the regression where
// an array<Struct> whose element has exported Key/Value fields was encoded as a
// TAG_ENTRY_SEQUENCE, dropping all other fields and decoding as a sequence of
// {Key, Value} map entries instead of full structs.
func TestBinaryCodec_ArrayOfStructNotEntrySeq(t *testing.T) {
	codec := &BinaryCodec{}
	arrDesc := schema.TypeDesc{
		Kind:    schema.TypeKindArray,
		Name:    "array",
		Element: &schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "kvRow"},
	}
	id := mustBinaryCanonicalID(t, 210)

	rows := []kvRow{
		{Key: "a", Value: 1, Extra: "x"},
		{Key: "b", Value: 2, Extra: "y"},
	}

	view, err := codec.Encode(arrDesc, id, rows)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	// First wire byte after the magic header must be TAG_ARRAY (0x09), not
	// TAG_ENTRY_SEQUENCE (0x0B).
	if got := view.Data[binaryHeaderLen]; got != binaryTagArray {
		t.Fatalf("expected TAG_ARRAY(0x09) after magic, got 0x%02x", got)
	}

	decoded, err := codec.Decode(view)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	arr, ok := decoded.([]any)
	if !ok || len(arr) != 2 {
		t.Fatalf("expected 2-element array, got %T %v", decoded, decoded)
	}
	for i, want := range rows {
		m, ok := arr[i].(map[string]any)
		if !ok {
			t.Fatalf("element %d: expected map[string]any, got %T", i, arr[i])
		}
		// Go int encodes as TAG_INT64, so the decoded value is int64.
		val, ok := m["Value"].(int64)
		if !ok || val != int64(want.Value) || m["Key"] != want.Key || m["Extra"] != want.Extra {
			t.Fatalf("element %d: expected Key=%q Value=%d Extra=%q, got %v",
				i, want.Key, want.Value, want.Extra, m)
		}
	}

	// DecodeInto must round-trip the full struct including Extra.
	var got []kvRow
	if err := codec.DecodeInto(view, &got); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(got))
	}
	for i, want := range rows {
		if got[i] != want {
			t.Fatalf("row %d: expected %+v, got %+v", i, want, got[i])
		}
	}
}

// TestBinaryCodec_EmptyArrayRoundTrips covers the empty-slice case: an empty
// []string under an array<scalar> schema must encode as TAG_ARRAY and round-trip
// through DecodeInto (previously it was misencoded as TAG_ENTRY_SEQUENCE, which
// made DecodeInto reject it with "must have Key and Value fields").
func TestBinaryCodec_EmptyArrayRoundTrips(t *testing.T) {
	codec := &BinaryCodec{}
	strDesc := schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}
	arrDesc := schema.TypeDesc{Kind: schema.TypeKindArray, Name: "array", Element: &strDesc}
	id := mustBinaryCanonicalID(t, 211)

	view, err := codec.Encode(arrDesc, id, []string{})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if view.Data[binaryHeaderLen] != binaryTagArray {
		t.Fatalf("expected TAG_ARRAY for empty []string, got 0x%02x", view.Data[binaryHeaderLen])
	}

	var got []string
	if err := codec.DecodeInto(view, &got); err != nil {
		t.Fatalf("DecodeInto empty []string: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty slice, got %v", got)
	}
}

// TestBinaryCodec_MapStillEntrySequence ensures the map-kind entry-sequence path
// is preserved after the schema-gated fix.
func TestBinaryCodec_MapStillEntrySequence(t *testing.T) {
	codec := &BinaryCodec{}
	mapDesc := schema.TypeDesc{
		Kind:  schema.TypeKindMap,
		Name:  "map",
		Key:   &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"},
		Value: &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"},
	}
	id := mustBinaryCanonicalID(t, 212)

	entries := []any{
		map[string]any{"Key": "a", "Value": int32(1)},
		map[string]any{"Key": "b", "Value": int32(2)},
	}
	view, err := codec.Encode(mapDesc, id, entries)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if view.Data[binaryHeaderLen] != binaryTagEntrySequence {
		t.Fatalf("expected TAG_ENTRY_SEQUENCE(0x0B) for map kind, got 0x%02x",
			view.Data[binaryHeaderLen])
	}
	decoded, err := codec.Decode(view)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	arr, ok := decoded.([]any)
	if !ok || len(arr) != 2 {
		t.Fatalf("expected 2 entry maps, got %T %v", decoded, decoded)
	}
}

// TestBinaryCodec_NestedMapProjectionMatchesJSON asserts that a struct field
// holding a map-projected entry list produces the same canonical shape under
// both codecs: an array of {Key, Value} maps.
func TestBinaryCodec_NestedMapProjectionMatchesJSON(t *testing.T) {
	type holder struct {
		Name string
		Meta []any
	}
	// Meta is the binding-layer canonical entry projection for map<string,int>.
	h := holder{
		Name: "hero",
		Meta: []any{
			map[string]any{"Key": "level", "Value": int32(5)},
			map[string]any{"Key": "mana", "Value": int32(50)},
		},
	}
	structDesc := schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "holder"}
	id := mustBinaryCanonicalID(t, 213)

	bin, err := (&BinaryCodec{}).Encode(structDesc, id, h)
	if err != nil {
		t.Fatalf("binary Encode: %v", err)
	}
	js, err := (&JSONCodec{}).Encode(structDesc, id, h)
	if err != nil {
		t.Fatalf("json Encode: %v", err)
	}

	binDec, err := (&BinaryCodec{}).Decode(bin)
	if err != nil {
		t.Fatalf("binary Decode: %v", err)
	}
	jsDec, err := (&JSONCodec{}).Decode(js)
	if err != nil {
		t.Fatalf("json Decode: %v", err)
	}

	// Both must surface Meta as []any of {Key,Value} maps.
	for name, v := range map[string]any{"binary": binDec, "json": jsDec} {
		m, ok := v.(map[string]any)
		if !ok {
			t.Fatalf("%s: expected map[string]any, got %T", name, v)
		}
		meta, ok := m["Meta"].([]any)
		if !ok || len(meta) != 2 {
			t.Fatalf("%s: expected Meta as 2-element []any, got %T %v", name, m["Meta"], m["Meta"])
		}
		e0, ok := meta[0].(map[string]any)
		if !ok || e0["Key"] != "level" {
			t.Fatalf("%s: entry 0 mismatch: %v", name, e0)
		}
	}

	// Cross-codec: a binary-encoded value must be decodable by JSON? No — the
	// wire formats differ. Instead assert the canonical projections are deeply
	// equal by comparing their re-encoded JSON bytes.
	reJS, err := (&JSONCodec{}).Encode(structDesc, id, binDec)
	if err != nil {
		t.Fatalf("re-encode binary projection as json: %v", err)
	}
	// Cross-codec canonicality: normalize both decoded projections through the
	// same codec (map[string]any, sorted keys) before comparing bytes.
	normJS, err := (&JSONCodec{}).Encode(structDesc, id, jsDec)
	if err != nil {
		t.Fatalf("re-encode json projection: %v", err)
	}
	if !bytes.Equal(normJS.Data, reJS.Data) {
		t.Fatalf("canonical projection diverges:\n json:   %s\n binary: %s", normJS.Data, reJS.Data)
	}
}
