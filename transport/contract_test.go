package transport_test

import (
	"bytes"
	"math"
	"testing"

	"github.com/qomos-w/spore/identity"
	"github.com/qomos-w/spore/schema"
	"github.com/qomos-w/spore/transport"
)

// ============================================================================
// Codec-specific contract tests
//
// These tests cover JSONCodec-specific behavior that is not part of the
// generic Codec SPI conformance test (e.g., OrderedMap projection,
// SortedOrderedMap projection).
//
// The generic Codec SPI conformance tests live in
// codec_conformance_test.go and are reused via CodecConformance().
// ============================================================================

// codecUnderTest is the default codec backend for contract testing.
var codecUnderTest transport.Codec = &transport.JSONCodec{}

func TestCodec_RoundTrip_OrderedMapAsEntrySequence(t *testing.T) {
	id := mustCanonicalID(t, 1000, 1, 0, 32)
	mapDesc := schema.TypeDesc{
		Kind:  schema.TypeKindMap,
		Name:  "map",
		Key:   &scalarStringDesc,
		Value: &scalarIntDesc,
	}

	// Create an OrderedMap with specific insertion order
	om := schema.NewOrderedMap[string, int32]()
	om.Set("z", 26)
	om.Set("a", 1)
	om.Set("m", 13)

	view, err := codecUnderTest.Encode(mapDesc, id, om)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	decoded, err := codecUnderTest.Decode(view)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	// OrderedMap encodes as entry sequence (JSON array), not JSON object
	entries, ok := decoded.([]any)
	if !ok {
		t.Fatalf("expected []any for ordered map decode, got %T", decoded)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}

	// Verify insertion order is preserved: z, a, m
	expectedKeys := []string{"z", "a", "m"}
	for i, entry := range entries {
		entryMap, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("entry %d: expected map[string]any, got %T", i, entry)
		}
		key, _ := entryMap["Key"].(string)
		if key != expectedKeys[i] {
			t.Fatalf("entry %d: expected key %q, got %q", i, expectedKeys[i], key)
		}
	}
}

func TestCodec_RoundTrip_SortedOrderedMapAsEntrySequence(t *testing.T) {
	id := mustCanonicalID(t, 1000, 10, 0, 1)
	mapDesc := schema.TypeDesc{
		Kind:  schema.TypeKindMap,
		Name:  "map",
		Key:   &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"},
		Value: &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"},
	}

	sm := schema.NewSortedOrderedMap[string, int](schema.NaturalOrder[string]())
	sm.Set("gamma", 3)
	sm.Set("alpha", 1)
	sm.Set("beta", 2)

	view, err := codecUnderTest.Encode(mapDesc, id, sm)
	if err != nil {
		t.Fatalf("Encode SortedOrderedMap: %v", err)
	}

	// Decode should produce an entry sequence as []any
	decoded, err := codecUnderTest.Decode(view)
	if err != nil {
		t.Fatalf("Decode SortedOrderedMap: %v", err)
	}

	entries, ok := decoded.([]any)
	if !ok {
		t.Fatalf("expected []any, got %T", decoded)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}

	// Verify sorted order preserved through encode/decode
	e0 := entries[0].(map[string]any)
	e1 := entries[1].(map[string]any)
	e2 := entries[2].(map[string]any)
	if e0["Key"] != "alpha" || e1["Key"] != "beta" || e2["Key"] != "gamma" {
		t.Fatalf("sorted order not preserved: %v %v %v", e0, e1, e2)
	}
}

func TestCodec_RoundTrip_StructAsMapProjection(t *testing.T) {
	id := mustCanonicalID(t, 1000, 10, 0, 2)

	// binding.ProjectView produces map[string]any for struct fields.
	// JSONCodec must accept map[string]any as a valid struct value.
	m := map[string]any{
		"Name":  "bob",
		"Level": int32(10),
	}
	structDesc := schema.TypeDesc{
		Kind:      schema.TypeKindStruct,
		Name:      "struct",
		ClassName: "player",
	}

	view, err := codecUnderTest.Encode(structDesc, id, m)
	if err != nil {
		t.Fatalf("Encode map[string]any as struct: %v", err)
	}

	decoded, err := codecUnderTest.Decode(view)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	decodedMap, ok := decoded.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", decoded)
	}
	if decodedMap["Name"] != "bob" {
		t.Fatalf("expected Name=bob, got %v", decodedMap["Name"])
	}
}

// ============================================================================
// Registry contract tests
// ============================================================================

func TestRegistry_RegisterAndLookup(t *testing.T) {
	reg := transport.NewRegistry()
	if _, ok := reg.Lookup("int"); ok {
		t.Fatal("expected empty registry to not find int")
	}

	reg.Register("int", codecUnderTest)
	_, ok := reg.Lookup("int")
	if !ok {
		t.Fatal("expected to find registered codec")
	}
}

func TestRegistry_LookupMissing(t *testing.T) {
	reg := transport.NewRegistry()
	_, ok := reg.Lookup("nonexistent")
	if ok {
		t.Fatal("expected missing lookup to return false")
	}
}

func TestRegistry_RegisterType_AcceptsCodec(t *testing.T) {
	reg := transport.NewRegistry()
	if err := reg.RegisterType("int", &transport.JSONCodec{}); err != nil {
		t.Fatalf("expected RegisterType to accept Codec, got %v", err)
	}
	c, ok := reg.Lookup("int")
	if !ok {
		t.Fatal("expected to find registered type")
	}
	if c == nil {
		t.Fatal("expected non-nil codec")
	}
}

func TestRegistry_RegisterType_RejectsNonCodec(t *testing.T) {
	reg := transport.NewRegistry()
	if err := reg.RegisterType("bad", "not-a-codec"); err == nil {
		t.Fatal("expected RegisterType to reject non-Codec value")
	}
}

// ============================================================================
// Envelope contract tests
// ============================================================================

func TestEnvelope_RouteKind(t *testing.T) {
	id := mustCanonicalID(t, 3000, 1, 0, 1)
	env := transport.Envelope{
		Kind:  transport.EnvelopeKindRoute,
		Route: "player.update",
		View: transport.View{
			Kind:     transport.ViewKindFull,
			Identity: id,
		},
	}
	if env.Kind != transport.EnvelopeKindRoute {
		t.Fatalf("expected route envelope, got %s", env.Kind)
	}
	if env.Route != "player.update" {
		t.Fatalf("expected route player.update, got %s", env.Route)
	}
}

func TestEnvelope_BinaryKind(t *testing.T) {
	id := mustCanonicalID(t, 3000, 1, 0, 2)
	env := transport.Envelope{
		Kind: transport.EnvelopeKindBinary,
		View: transport.View{
			Kind:     transport.ViewKindFull,
			Identity: id,
			Data:     []byte{0x01, 0x02},
		},
	}
	if env.Kind != transport.EnvelopeKindBinary {
		t.Fatalf("expected binary envelope, got %s", env.Kind)
	}
}

// ============================================================================
// EncodeError / DecodeError structure tests (no codec needed)
// ============================================================================

func TestEncodeError_CarriesSchemaPathIdentity(t *testing.T) {
	id := mustCanonicalID(t, 1000, 1, 0, 1)
	err := &transport.EncodeError{
		SchemaName: "int",
		Path:       ".level",
		Identity:   id,
		Err:        testErr("type mismatch"),
	}
	if err.SchemaName != "int" {
		t.Fatalf("expected SchemaName=int, got %s", err.SchemaName)
	}
	if err.Path != ".level" {
		t.Fatalf("expected Path=.level, got %s", err.Path)
	}
	if err.Identity != id {
		t.Fatal("identity not preserved in EncodeError")
	}
	if err.Error() != "type mismatch" {
		t.Fatalf("unexpected Error() output: %s", err.Error())
	}
	if err.Unwrap() == nil {
		t.Fatal("expected Unwrap to return inner error")
	}
}

func TestDecodeError_CarriesSchemaPathIdentity(t *testing.T) {
	id := mustCanonicalID(t, 1000, 1, 0, 1)
	err := &transport.DecodeError{
		SchemaName: "map",
		Path:       ".items[2]",
		Identity:   id,
		Err:        testErr("unexpected EOF"),
	}
	if err.SchemaName != "map" {
		t.Fatalf("expected SchemaName=map, got %s", err.SchemaName)
	}
	if err.Path != ".items[2]" {
		t.Fatalf("expected Path=.items[2], got %s", err.Path)
	}
	if err.Identity != id {
		t.Fatal("identity not preserved in DecodeError")
	}
	if err.Error() != "unexpected EOF" {
		t.Fatalf("unexpected Error() output: %s", err.Error())
	}
	if err.Unwrap() == nil {
		t.Fatal("expected Unwrap to return inner error")
	}
}

// ============================================================================
// View kind tests (no codec needed)
// ============================================================================

func TestViewKind_FullAndPatch(t *testing.T) {
	if transport.ViewKindFull != "full" {
		t.Fatalf("expected ViewKindFull=full, got %s", transport.ViewKindFull)
	}
	if transport.ViewKindPatch != "patch" {
		t.Fatalf("expected ViewKindPatch=patch, got %s", transport.ViewKindPatch)
	}
}

// ============================================================================
// View construction tests (no codec needed)
// ============================================================================

func TestView_ConstructWithIdentity(t *testing.T) {
	id := mustCanonicalID(t, 5000, 3, 0, 1)
	view := transport.View{
		Kind:     transport.ViewKindFull,
		Schema:   scalarIntDesc,
		Identity: id,
		Data:     []byte{0x00},
	}
	if view.Identity != id {
		t.Fatal("view identity mismatch after construction")
	}
	if view.Kind != transport.ViewKindFull {
		t.Fatalf("expected full, got %s", view.Kind)
	}
	if view.Schema.Kind != schema.TypeKindScalar {
		t.Fatalf("expected scalar, got %s", view.Schema.Kind)
	}
}

func TestJSONCodec_RoundTrip_FloatAndDouble(t *testing.T) {
	codec := &transport.JSONCodec{}
	cases := []struct {
		name  string
		desc  schema.TypeDesc
		value any
		want  float64
	}{
		{name: "float", desc: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "float"}, value: float64(3.14), want: float64(3.14)},
		{name: "double", desc: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "double"}, value: float64(2.718), want: float64(2.718)},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id := mustCanonicalID(t, 1100, 1, 0, uint64(i+1))
			view, err := codec.Encode(tc.desc, id, tc.value)
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}
			decoded, err := codec.Decode(view)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			got, ok := decoded.(float64)
			if !ok {
				t.Fatalf("expected float64, got %T", decoded)
			}
			if got != tc.want {
				t.Fatalf("expected %v, got %v", tc.want, got)
			}
		})
	}
}

func TestJSONCodec_RoundTrip_ExtraScalarTypes(t *testing.T) {
	codec := &transport.JSONCodec{}
	cases := []struct {
		name  string
		desc  schema.TypeDesc
		value any
		want  any
	}{
		{name: "long", desc: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "long"}, value: int64(42), want: int64(42)},
		{name: "ulong", desc: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "ulong"}, value: uint64(42), want: uint64(42)},
		{name: "uint", desc: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "uint"}, value: uint32(42), want: uint32(42)},
		{name: "short", desc: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "short"}, value: int32(42), want: int32(42)},
		{name: "ushort", desc: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "ushort"}, value: int32(42), want: int32(42)},
		{name: "byte_scalar", desc: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "byte"}, value: int32(42), want: int32(42)},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id := mustCanonicalID(t, 1200, 1, 0, uint64(i+1))
			view, err := codec.Encode(tc.desc, id, tc.value)
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}
			decoded, err := codec.Decode(view)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if decoded != tc.want {
				t.Fatalf("expected %v (%T), got %v (%T)", tc.want, tc.want, decoded, decoded)
			}
		})
	}
}

func TestJSONCodec_RoundTrip_CustomScalarPassthrough(t *testing.T) {
	codec := &transport.JSONCodec{}
	id := mustCanonicalID(t, 1100, 1, 0, 10)
	customDesc := schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "custom"}
	view, err := codec.Encode(customDesc, id, "arbitrary")
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	decoded, err := codec.Decode(view)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if decoded != "arbitrary" {
		t.Fatalf("expected passthrough, got %v", decoded)
	}
}

func TestJSONCodec_EncodeError_TypeMismatch(t *testing.T) {
	codec := &transport.JSONCodec{}
	id := mustCanonicalID(t, 1100, 1, 0, 30)
	cases := []struct {
		name  string
		desc  schema.TypeDesc
		value any
	}{
		{name: "int_rejects_string", desc: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}, value: "not-an-int"},
		{name: "float_rejects_int", desc: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "float"}, value: int32(7)},
		{name: "bool_rejects_string", desc: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "bool"}, value: "not-a-bool"},
		{name: "struct_rejects_int", desc: schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "struct"}, value: int32(7)},
		{name: "array_rejects_string", desc: schema.TypeDesc{Kind: schema.TypeKindArray, Name: "array"}, value: "not-an-array"},
		{name: "map_rejects_int", desc: schema.TypeDesc{Kind: schema.TypeKindMap, Name: "map"}, value: int32(7)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := codec.Encode(tc.desc, id, tc.value)
			if err == nil {
				t.Fatal("expected encode error for type mismatch")
			}
			var encErr *transport.EncodeError
			if !isEncodeError(err, &encErr) {
				t.Fatalf("expected *transport.EncodeError, got %T", err)
			}
		})
	}
}

func TestJSONCodec_EncodeError_NilValue(t *testing.T) {
	codec := &transport.JSONCodec{}
	id := mustCanonicalID(t, 1100, 1, 0, 40)
	_, err := codec.Encode(scalarIntDesc, id, nil)
	if err == nil {
		t.Fatal("expected encode error for nil value")
	}
}

func TestJSONCodec_DecodeError_InvalidScalarJSON(t *testing.T) {
	codec := &transport.JSONCodec{}
	id := mustCanonicalID(t, 1100, 1, 0, 20)
	cases := []struct {
		name string
		desc schema.TypeDesc
		data []byte
	}{
		{name: "int_with_string_json", desc: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}, data: []byte(`"not-an-int"`)},
		{name: "bool_with_number_json", desc: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "bool"}, data: []byte(`123`)},
		{name: "float_with_string_json", desc: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "double"}, data: []byte(`"not-a-float"`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			view := transport.View{Kind: transport.ViewKindFull, Schema: tc.desc, Identity: id, Data: tc.data}
			_, err := codec.Decode(view)
			if err == nil {
				t.Fatal("expected decode error for invalid scalar JSON")
			}
			var decErr *transport.DecodeError
			if !isDecodeError(err, &decErr) {
				t.Fatalf("expected *transport.DecodeError, got %T", err)
			}
			// Top-level scalar failures carry the empty path.
			if decErr.Path != "" {
				t.Fatalf("expected empty path for top-level scalar failure, got %q", decErr.Path)
			}
		})
	}
}

// ============================================================================
// Deep path validation: json/v2 JSON pointer context in error Path fields
// ============================================================================

func TestJSONCodec_DecodeError_DeepPathDecodeInto(t *testing.T) {
	codec := &transport.JSONCodec{}
	id := mustCanonicalID(t, 1100, 1, 0, 60)
	structDesc := schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "struct", ClassName: "Nested"}

	type inner struct {
		Level int32
	}
	type outer struct {
		Name string
		Sub  inner
	}

	view := transport.View{
		Kind:     transport.ViewKindFull,
		Schema:   structDesc,
		Identity: id,
		Data:     []byte(`{"Name":"a","Sub":{"Level":"not-an-int"}}`),
	}

	var dst outer
	err := codec.DecodeInto(view, &dst)
	if err == nil {
		t.Fatal("expected DecodeInto error for nested type mismatch")
	}
	var decErr *transport.DecodeError
	if !isDecodeError(err, &decErr) {
		t.Fatalf("expected *transport.DecodeError, got %T: %v", err, err)
	}
	if decErr.Path != ".Sub.Level" {
		t.Fatalf("expected deep path .Sub.Level, got %q", decErr.Path)
	}
	if decErr.SchemaName != "struct" || decErr.Identity != id {
		t.Fatalf("decode error missing context: %#v", decErr)
	}
}

func TestJSONCodec_DecodeError_DeepPathDecodeStructMap(t *testing.T) {
	codec := &transport.JSONCodec{}
	id := mustCanonicalID(t, 1100, 1, 0, 61)
	structDesc := schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "struct", ClassName: "Dup"}

	// json/v2 rejects duplicate object member names; the failure must
	// carry the offending member as path context.
	view := transport.View{
		Kind:     transport.ViewKindFull,
		Schema:   structDesc,
		Identity: id,
		Data:     []byte(`{"Count":1,"Count":2}`),
	}

	_, err := codec.Decode(view)
	if err == nil {
		t.Fatal("expected decode error for duplicate member name")
	}
	var decErr *transport.DecodeError
	if !isDecodeError(err, &decErr) {
		t.Fatalf("expected *transport.DecodeError, got %T: %v", err, err)
	}
	if decErr.Path != ".Count" {
		t.Fatalf("expected deep path .Count, got %q", decErr.Path)
	}
}

func TestJSONCodec_DecodeError_DeepPathArrayIndex(t *testing.T) {
	codec := &transport.JSONCodec{}
	id := mustCanonicalID(t, 1100, 1, 0, 62)
	structDesc := schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "struct", ClassName: "Arr"}

	type item struct {
		Tags []int32
	}
	type container struct {
		Item item
	}

	view := transport.View{
		Kind:     transport.ViewKindFull,
		Schema:   structDesc,
		Identity: id,
		Data:     []byte(`{"Item":{"Tags":[1,2,"x"]}}`),
	}

	var dst container
	err := codec.DecodeInto(view, &dst)
	if err == nil {
		t.Fatal("expected DecodeInto error for array element mismatch")
	}
	var decErr *transport.DecodeError
	if !isDecodeError(err, &decErr) {
		t.Fatalf("expected *transport.DecodeError, got %T: %v", err, err)
	}
	if decErr.Path != ".Item.Tags[2]" {
		t.Fatalf("expected deep path .Item.Tags[2], got %q", decErr.Path)
	}
}

func TestJSONCodec_EncodeError_DeepPathMarshalFailure(t *testing.T) {
	codec := &transport.JSONCodec{}
	id := mustCanonicalID(t, 1100, 1, 0, 63)
	structDesc := schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "struct", ClassName: "Bad"}

	// Passes top-level schema validation (map[string]any is the canonical
	// struct projection), but the nested chan cannot be marshaled. The
	// encode error must point at the failing field, not the top level.
	value := map[string]any{
		"OK":  int32(1),
		"Bad": map[string]any{"Ch": make(chan int)},
	}

	_, err := codec.Encode(structDesc, id, value)
	if err == nil {
		t.Fatal("expected encode error for unsupported nested type")
	}
	var encErr *transport.EncodeError
	if !isEncodeError(err, &encErr) {
		t.Fatalf("expected *transport.EncodeError, got %T: %v", err, err)
	}
	if encErr.Path != ".Bad.Ch" {
		t.Fatalf("expected deep path .Bad.Ch, got %q", encErr.Path)
	}
	if encErr.SchemaName != "struct" || encErr.Identity != id {
		t.Fatalf("encode error missing context: %#v", encErr)
	}
}

func TestBinaryCodec_RoundTrip_OrderedMapAsEntrySequence(t *testing.T) {
	codec := &transport.BinaryCodec{}
	id := mustCanonicalID(t, 1000, 1, 0, 132)
	mapDesc := schema.TypeDesc{
		Kind:  schema.TypeKindMap,
		Name:  "map",
		Key:   &scalarStringDesc,
		Value: &scalarIntDesc,
	}

	om := schema.NewOrderedMap[string, int32]()
	om.Set("z", 26)
	om.Set("a", 1)
	om.Set("m", 13)

	view, err := codec.Encode(mapDesc, id, om)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	decoded, err := codec.Decode(view)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	entries, ok := decoded.([]any)
	if !ok {
		t.Fatalf("expected []any for ordered map decode, got %T", decoded)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	expectedKeys := []string{"z", "a", "m"}
	for i, entry := range entries {
		entryMap, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("entry %d: expected map[string]any, got %T", i, entry)
		}
		key, _ := entryMap["Key"].(string)
		if key != expectedKeys[i] {
			t.Fatalf("entry %d: expected key %q, got %q", i, expectedKeys[i], key)
		}
	}
}

func TestBinaryCodec_RoundTrip_SortedOrderedMapAsEntrySequence(t *testing.T) {
	codec := &transport.BinaryCodec{}
	id := mustCanonicalID(t, 1000, 10, 0, 101)
	mapDesc := schema.TypeDesc{
		Kind:  schema.TypeKindMap,
		Name:  "map",
		Key:   &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"},
		Value: &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"},
	}

	sm := schema.NewSortedOrderedMap[string, int](schema.NaturalOrder[string]())
	sm.Set("gamma", 3)
	sm.Set("alpha", 1)
	sm.Set("beta", 2)

	view, err := codec.Encode(mapDesc, id, sm)
	if err != nil {
		t.Fatalf("Encode SortedOrderedMap: %v", err)
	}
	decoded, err := codec.Decode(view)
	if err != nil {
		t.Fatalf("Decode SortedOrderedMap: %v", err)
	}
	entries, ok := decoded.([]any)
	if !ok {
		t.Fatalf("expected []any, got %T", decoded)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	e0 := entries[0].(map[string]any)
	e1 := entries[1].(map[string]any)
	e2 := entries[2].(map[string]any)
	if e0["Key"] != "alpha" || e1["Key"] != "beta" || e2["Key"] != "gamma" {
		t.Fatalf("sorted order not preserved: %v %v %v", e0, e1, e2)
	}
}

func TestBinaryCodec_RoundTrip_StructAsMapProjection(t *testing.T) {
	codec := &transport.BinaryCodec{}
	id := mustCanonicalID(t, 1000, 10, 0, 102)
	m := map[string]any{
		"Name":  "bob",
		"Level": int32(10),
	}
	structDesc := schema.TypeDesc{
		Kind:      schema.TypeKindStruct,
		Name:      "struct",
		ClassName: "player",
	}

	view, err := codec.Encode(structDesc, id, m)
	if err != nil {
		t.Fatalf("Encode map[string]any as struct: %v", err)
	}
	decoded, err := codec.Decode(view)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	decodedMap, ok := decoded.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", decoded)
	}
	if decodedMap["Name"] != "bob" {
		t.Fatalf("expected Name=bob, got %v", decodedMap["Name"])
	}
}

func TestBinaryCodec_RoundTrip_PatchViewPayload(t *testing.T) {
	codec := &transport.BinaryCodec{}
	id := mustCanonicalID(t, 2000, 1, 0, 1)
	patchDesc := schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "struct", ClassName: "EntityPatch"}
	payload := map[string]any{
		"Added": map[string]any{
			"Health": map[string]any{"HP": int32(100)},
		},
		"Changed": map[string]any{
			"Position": map[string]any{"X": float64(1.5), "Y": float64(2.5)},
		},
		"Removed": []any{"Velocity"},
	}

	view, err := codec.Encode(patchDesc, id, payload)
	if err != nil {
		t.Fatalf("Encode patch payload: %v", err)
	}
	view.Kind = transport.ViewKindPatch
	decoded, err := codec.Decode(view)
	if err != nil {
		t.Fatalf("Decode patch payload: %v", err)
	}
	decodedMap, ok := decoded.(map[string]any)
	if !ok {
		t.Fatalf("expected patch decode as map[string]any, got %T", decoded)
	}
	if view.Kind != transport.ViewKindPatch {
		t.Fatalf("expected patch view kind, got %s", view.Kind)
	}
	if _, ok := decodedMap["Added"].(map[string]any); !ok {
		t.Fatalf("expected Added map, got %T", decodedMap["Added"])
	}
	if _, ok := decodedMap["Changed"].(map[string]any); !ok {
		t.Fatalf("expected Changed map, got %T", decodedMap["Changed"])
	}
	removed, ok := decodedMap["Removed"].([]any)
	if !ok {
		t.Fatalf("expected Removed []any, got %T", decodedMap["Removed"])
	}
	if len(removed) != 1 || removed[0] != "Velocity" {
		t.Fatalf("expected Removed to contain Velocity, got %#v", removed)
	}
}

func TestBinaryCodec_DecodeError_EmptyPayload(t *testing.T) {
	codec := &transport.BinaryCodec{}
	id := mustCanonicalID(t, 2000, 1, 0, 2)
	view := transport.View{Kind: transport.ViewKindFull, Schema: scalarIntDesc, Identity: id, Data: nil}

	_, err := codec.Decode(view)
	if err == nil {
		t.Fatal("expected decode error for empty payload")
	}
	var decErr *transport.DecodeError
	if !isDecodeError(err, &decErr) {
		t.Fatalf("expected *transport.DecodeError, got %T: %v", err, err)
	}
	if decErr.SchemaName == "" || decErr.Identity != id {
		t.Fatalf("decode error missing context: %#v", decErr)
	}
}

func TestBinaryCodec_DecodeError_CorruptPayload(t *testing.T) {
	codec := &transport.BinaryCodec{}
	id := mustCanonicalID(t, 2000, 1, 0, 3)
	cases := []struct {
		name string
		data []byte
	}{
		{name: "bad magic", data: []byte{0x00, 0x01, 0x02, 0x03}},
		{name: "truncated string", data: []byte{'T', 'B', 'C', 0x01, 0x07, 0x05, 'h', 'i'}},
		{name: "invalid tag", data: []byte{'T', 'B', 'C', 0x01, 0xFF}},
		{name: "trailing bytes", data: []byte{'T', 'B', 'C', 0x01, 0x03, 0x00, 0x00, 0x00, 0x07, 0x99}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			view := transport.View{Kind: transport.ViewKindFull, Schema: scalarIntDesc, Identity: id, Data: tc.data}
			_, err := codec.Decode(view)
			if err == nil {
				t.Fatal("expected decode error")
			}
			var decErr *transport.DecodeError
			if !isDecodeError(err, &decErr) {
				t.Fatalf("expected *transport.DecodeError, got %T: %v", err, err)
			}
			if decErr.SchemaName == "" || decErr.Identity != id {
				t.Fatalf("decode error missing context: %#v", decErr)
			}
			_ = decErr.Path
		})
	}
}

func TestBinaryCodec_RoundTrip_Bytes(t *testing.T) {
	codec := &transport.BinaryCodec{}
	bytesDesc := schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "bytes"}
	cases := []struct {
		name string
		data []byte
	}{
		{name: "empty", data: []byte{}},
		{name: "embedded zero", data: []byte{0x00, 0x01, 0xFF}},
		{name: "non utf8", data: []byte{0xFE, 0xFD, 0xFC}},
	}

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id := mustCanonicalID(t, 2100, 1, 0, uint64(i+1))
			view, err := codec.Encode(bytesDesc, id, tc.data)
			if err != nil {
				t.Fatalf("Encode bytes: %v", err)
			}
			decoded, err := codec.Decode(view)
			if err != nil {
				t.Fatalf("Decode bytes: %v", err)
			}
			got, ok := decoded.([]byte)
			if !ok {
				t.Fatalf("expected []byte, got %T", decoded)
			}
			if !bytes.Equal(got, tc.data) {
				t.Fatalf("expected %v, got %v", tc.data, got)
			}
		})
	}
}

func TestBinaryCodec_RoundTrip_NumericEdges(t *testing.T) {
	codec := &transport.BinaryCodec{}
	intDesc := schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}
	floatDesc := schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "double"}

	intCases := []int32{0, -1, math.MinInt32, math.MaxInt32}
	for i, want := range intCases {
		t.Run("int32", func(t *testing.T) {
			id := mustCanonicalID(t, 2200, 1, 0, uint64(i+1))
			view, err := codec.Encode(intDesc, id, want)
			if err != nil {
				t.Fatalf("Encode int edge: %v", err)
			}
			decoded, err := codec.Decode(view)
			if err != nil {
				t.Fatalf("Decode int edge: %v", err)
			}
			got, ok := decoded.(int32)
			if !ok || got != want {
				t.Fatalf("expected int32 %d, got %T %v", want, decoded, decoded)
			}
		})
	}

	floatCases := []struct {
		name  string
		want  float64
		check func(float64) bool
	}{
		{name: "negative zero", want: math.Copysign(0, -1), check: func(v float64) bool { return math.Signbit(v) && v == 0 }},
		{name: "positive inf", want: math.Inf(1), check: func(v float64) bool { return math.IsInf(v, 1) }},
		{name: "negative inf", want: math.Inf(-1), check: func(v float64) bool { return math.IsInf(v, -1) }},
		{name: "nan", want: math.NaN(), check: func(v float64) bool { return math.IsNaN(v) }},
	}
	for i, tc := range floatCases {
		t.Run(tc.name, func(t *testing.T) {
			id := mustCanonicalID(t, 2300, 1, 0, uint64(i+1))
			view, err := codec.Encode(floatDesc, id, tc.want)
			if err != nil {
				t.Fatalf("Encode float edge: %v", err)
			}
			decoded, err := codec.Decode(view)
			if err != nil {
				t.Fatalf("Decode float edge: %v", err)
			}
			got, ok := decoded.(float64)
			if !ok || !tc.check(got) {
				t.Fatalf("unexpected float result: %T %v", decoded, decoded)
			}
		})
	}

	t.Run("wide numeric values inside struct projection", func(t *testing.T) {
		id := mustCanonicalID(t, 2400, 1, 0, 1)
		structDesc := schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "struct", ClassName: "wide"}
		payload := map[string]any{
			"MinInt64":  int64(math.MinInt64),
			"MaxUint64": uint64(math.MaxUint64),
		}
		view, err := codec.Encode(structDesc, id, payload)
		if err != nil {
			t.Fatalf("Encode wide numerics: %v", err)
		}
		decoded, err := codec.Decode(view)
		if err != nil {
			t.Fatalf("Decode wide numerics: %v", err)
		}
		decodedMap := decoded.(map[string]any)
		if decodedMap["MinInt64"].(int64) != math.MinInt64 {
			t.Fatalf("expected MinInt64 round-trip, got %v", decodedMap["MinInt64"])
		}
		if decodedMap["MaxUint64"].(uint64) != math.MaxUint64 {
			t.Fatalf("expected MaxUint64 round-trip, got %v", decodedMap["MaxUint64"])
		}
	})
}

func TestBinaryCodec_RoundTrip_NestedCompositeValue(t *testing.T) {
	codec := &transport.BinaryCodec{}
	id := mustCanonicalID(t, 2500, 1, 0, 1)
	structDesc := schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "struct", ClassName: "nested"}
	payload := map[string]any{
		"Name": "nested",
		"Items": []any{
			map[string]any{"Key": "a", "Value": []any{int32(1), int32(2)}},
			map[string]any{"Key": "b", "Value": map[string]any{"ok": true}},
		},
		"Blob": []byte{0x00, 0xFF},
	}

	view, err := codec.Encode(structDesc, id, payload)
	if err != nil {
		t.Fatalf("Encode nested composite: %v", err)
	}
	decoded, err := codec.Decode(view)
	if err != nil {
		t.Fatalf("Decode nested composite: %v", err)
	}
	decodedMap := decoded.(map[string]any)
	if decodedMap["Name"] != "nested" {
		t.Fatalf("expected Name=nested, got %v", decodedMap["Name"])
	}
	items, ok := decodedMap["Items"].([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("expected nested Items []any, got %T %#v", decodedMap["Items"], decodedMap["Items"])
	}
	entry0 := items[0].(map[string]any)
	value0, ok := entry0["Value"].([]any)
	if !ok || len(value0) != 2 {
		t.Fatalf("expected entry sequence nested array, got %T %#v", entry0["Value"], entry0["Value"])
	}
	entry1 := items[1].(map[string]any)
	value1, ok := entry1["Value"].(map[string]any)
	if !ok || value1["ok"] != true {
		t.Fatalf("expected nested map value, got %T %#v", entry1["Value"], entry1["Value"])
	}
	blob, ok := decodedMap["Blob"].([]byte)
	if !ok || !bytes.Equal(blob, []byte{0x00, 0xFF}) {
		t.Fatalf("expected Blob bytes round-trip, got %T %v", decodedMap["Blob"], decodedMap["Blob"])
	}
}

func TestBinaryCodec_EncodeDeterministicForMapAndStruct(t *testing.T) {
	codec := &transport.BinaryCodec{}
	id1 := mustCanonicalID(t, 2600, 1, 0, 1)
	id2 := mustCanonicalID(t, 2600, 1, 0, 2)
	structDesc := schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "struct", ClassName: "deterministic"}

	mapA := map[string]any{"b": int32(2), "a": int32(1)}
	mapB := map[string]any{"a": int32(1), "b": int32(2)}
	viewA, err := codec.Encode(structDesc, id1, mapA)
	if err != nil {
		t.Fatalf("Encode mapA: %v", err)
	}
	viewB, err := codec.Encode(structDesc, id2, mapB)
	if err != nil {
		t.Fatalf("Encode mapB: %v", err)
	}
	if !bytes.Equal(viewA.Data, viewB.Data) {
		t.Fatalf("expected deterministic bytes for equivalent maps, got %v vs %v", viewA.Data, viewB.Data)
	}

	type deterministicStruct struct {
		Name  string
		Level int32
	}
	payload := deterministicStruct{Name: "same", Level: 7}
	view1, err := codec.Encode(structDesc, id1, payload)
	if err != nil {
		t.Fatalf("Encode struct 1: %v", err)
	}
	view2, err := codec.Encode(structDesc, id2, payload)
	if err != nil {
		t.Fatalf("Encode struct 2: %v", err)
	}
	if !bytes.Equal(view1.Data, view2.Data) {
		t.Fatalf("expected deterministic bytes for same struct, got %v vs %v", view1.Data, view2.Data)
	}
}

func TestBinaryCodec_RuntimePatchIntegration(t *testing.T) {
	codec := &transport.BinaryCodec{}
	id := mustCanonicalID(t, 2700, 1, 0, 1)
	patchDesc := schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "struct", ClassName: "EntityPatch"}
	payload := map[string]any{
		"Added":   map[string]any{"Health": map[string]any{"Value": int32(100)}},
		"Changed": map[string]any{"Position": map[string]any{"X": int32(9), "Y": int32(10)}},
		"Removed": []any{"Velocity"},
	}
	view, err := codec.Encode(patchDesc, id, payload)
	if err != nil {
		t.Fatalf("Encode runtime-style patch: %v", err)
	}
	view.Kind = transport.ViewKindPatch
	if view.Kind != transport.ViewKindPatch {
		t.Fatalf("expected patch kind, got %s", view.Kind)
	}
	if view.Identity != id {
		t.Fatalf("expected identity %s, got %s", id, view.Identity)
	}
	decoded, err := codec.Decode(view)
	if err != nil {
		t.Fatalf("Decode runtime-style patch: %v", err)
	}
	decodedMap := decoded.(map[string]any)
	if _, ok := decodedMap["Added"].(map[string]any); !ok {
		t.Fatalf("expected Added map, got %T", decodedMap["Added"])
	}
	if _, ok := decodedMap["Changed"].(map[string]any); !ok {
		t.Fatalf("expected Changed map, got %T", decodedMap["Changed"])
	}
	removed := decodedMap["Removed"].([]any)
	if len(removed) != 1 || removed[0] != "Velocity" {
		t.Fatalf("expected Removed to contain Velocity, got %#v", removed)
	}
}

func TestBinaryCodec_Envelope_BinaryCarriesBinaryPayload(t *testing.T) {
	codec := &transport.BinaryCodec{}
	id := mustCanonicalID(t, 3000, 1, 0, 22)
	view, err := codec.Encode(scalarIntDesc, id, int32(7))
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	env := transport.Envelope{Kind: transport.EnvelopeKindBinary, View: view}
	if env.Kind != transport.EnvelopeKindBinary {
		t.Fatalf("expected binary envelope, got %s", env.Kind)
	}
	if len(env.View.Data) < 4 || string(env.View.Data[:3]) != "TBC" || env.View.Data[3] != 0x03 {
		t.Fatalf("expected BinaryCodec magic+version header, got %#v", env.View.Data)
	}
	decoded, err := codec.Decode(env.View)
	if err != nil {
		t.Fatalf("Decode envelope payload: %v", err)
	}
	if decoded.(int32) != 7 {
		t.Fatalf("expected round-trip 7, got %v", decoded)
	}
}

func TestBinaryCodec_RoundTrip_ExtraScalarTypes(t *testing.T) {
	codec := &transport.BinaryCodec{}
	cases := []struct {
		name  string
		desc  schema.TypeDesc
		value any
		want  any
	}{
		{name: "long", desc: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "long"}, value: int64(42), want: int64(42)},
		{name: "ulong", desc: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "ulong"}, value: uint64(42), want: uint64(42)},
		{name: "short", desc: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "short"}, value: int32(42), want: int32(42)},
		{name: "ushort", desc: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "ushort"}, value: int32(42), want: int32(42)},
		{name: "byte_scalar", desc: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "byte"}, value: int32(42), want: int32(42)},
		{name: "float", desc: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "float"}, value: float64(3.14), want: float64(3.14)},
		{name: "unknown_scalar_passthrough", desc: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "custom"}, value: "hello", want: "hello"},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id := mustCanonicalID(t, 2800, 1, 0, uint64(i+1))
			view, err := codec.Encode(tc.desc, id, tc.value)
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}
			decoded, err := codec.Decode(view)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if decoded != tc.want {
				t.Fatalf("expected %v (%T), got %v (%T)", tc.want, tc.want, decoded, decoded)
			}
		})
	}
}

func TestBinaryCodec_DecodeError_ScalarTypeMismatch(t *testing.T) {
	codec := &transport.BinaryCodec{}
	id := mustCanonicalID(t, 2900, 1, 0, 1)

	cases := []struct {
		name    string
		desc    schema.TypeDesc
		encode  any
		corrupt func([]byte) []byte
	}{
		{
			name:   "int_rejects_string",
			desc:   schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"},
			encode: int32(7),
			corrupt: func(data []byte) []byte {
				// magic + string tag + uvarint(5) + "hello"
				return append(data[:4], []byte{0x07, 0x05, 'h', 'e', 'l', 'l', 'o'}...)
			},
		},
		{
			name:   "bool_rejects_int",
			desc:   schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "bool"},
			encode: true,
			corrupt: func(data []byte) []byte {
				// magic + int32 tag + uint32(0)
				return append(data[:4], []byte{0x03, 0x00, 0x00, 0x00, 0x00}...)
			},
		},
		{
			name:   "string_rejects_bool",
			desc:   schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"},
			encode: "x",
			corrupt: func(data []byte) []byte {
				// magic + bool false tag
				return append(data[:4], 0x01)
			},
		},
		{
			name:   "bytes_rejects_int",
			desc:   schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "bytes"},
			encode: []byte{0x01},
			corrupt: func(data []byte) []byte {
				// magic + int32 tag + uint32(0)
				return append(data[:4], []byte{0x03, 0x00, 0x00, 0x00, 0x00}...)
			},
		},
		{
			name:   "double_rejects_string",
			desc:   schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "double"},
			encode: float64(1.5),
			corrupt: func(data []byte) []byte {
				// magic + string tag + uvarint(5) + "hello"
				return append(data[:4], []byte{0x07, 0x05, 'h', 'e', 'l', 'l', 'o'}...)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			view, err := codec.Encode(tc.desc, id, tc.encode)
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}
			view.Data = tc.corrupt(view.Data)
			_, err = codec.Decode(view)
			if err == nil {
				t.Fatal("expected decode error for scalar type mismatch")
			}
			var decErr *transport.DecodeError
			if !isDecodeError(err, &decErr) {
				t.Fatalf("expected *transport.DecodeError, got %T", err)
			}
		})
	}
}

func TestBinaryCodec_EncodeError_NilPointer(t *testing.T) {
	codec := &transport.BinaryCodec{}
	id := mustCanonicalID(t, 2900, 1, 0, 10)
	var ptr *int32
	_, err := codec.Encode(scalarIntDesc, id, ptr)
	if err == nil {
		t.Fatal("expected encode error for nil pointer")
	}
	var encErr *transport.EncodeError
	if !isEncodeError(err, &encErr) {
		t.Fatalf("expected *transport.EncodeError, got %T", err)
	}
}

func TestBinaryCodec_EncodeError_InvalidEntrySequence(t *testing.T) {
	codec := &transport.BinaryCodec{}
	id := mustCanonicalID(t, 2900, 1, 0, 11)
	mapDesc := schema.TypeDesc{
		Kind:  schema.TypeKindMap,
		Name:  "map",
		Key:   &scalarStringDesc,
		Value: &scalarIntDesc,
	}
	// Slice of structs that look like entry sequence but one element is invalid
	invalid := []any{
		map[string]any{"Key": "a", "Value": int32(1)},
		map[string]any{"Bad": "x"},
	}
	_, err := codec.Encode(mapDesc, id, invalid)
	if err == nil {
		t.Fatal("expected encode error for invalid entry sequence element")
	}
}

func TestBinaryCodec_RoundTrip_MapAsEntrySequence(t *testing.T) {
	codec := &transport.BinaryCodec{}
	id := mustCanonicalID(t, 2900, 1, 0, 20)
	mapDesc := schema.TypeDesc{
		Kind:  schema.TypeKindMap,
		Name:  "map",
		Key:   &scalarStringDesc,
		Value: &scalarIntDesc,
	}
	entries := []any{
		map[string]any{"Key": "a", "Value": int32(1)},
		map[string]any{"Key": "b", "Value": int32(2)},
	}
	view, err := codec.Encode(mapDesc, id, entries)
	if err != nil {
		t.Fatalf("Encode entry sequence: %v", err)
	}
	decoded, err := codec.Decode(view)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	decEntries, ok := decoded.([]any)
	if !ok || len(decEntries) != 2 {
		t.Fatalf("expected 2 entries, got %T %v", decoded, decoded)
	}
}

// ---------------------------------------------------------------------------
// DecodeInto: nested struct with bytes field (voice pipeline regression)
// ---------------------------------------------------------------------------

func TestBinaryCodec_DecodeInto_FlatStructWithBytes(t *testing.T) {
	codec := &transport.BinaryCodec{}
	id := mustCanonicalID(t, 5000, 1, 0, 1)

	type FlatReq struct {
		AudioType int32  `json:"AudioType"`
		Data      []byte `json:"Data"`
	}

	pcmData := make([]byte, 256)
	for i := range pcmData {
		pcmData[i] = byte(i & 0xff)
	}

	original := FlatReq{AudioType: 2, Data: pcmData}
	structDesc := schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "struct", ClassName: "FlatReq"}

	view, err := codec.Encode(structDesc, id, original)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	var decoded FlatReq
	if err := codec.DecodeInto(view, &decoded); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if decoded.AudioType != 2 {
		t.Fatalf("expected AudioType=2, got %d", decoded.AudioType)
	}
	if !bytes.Equal(decoded.Data, pcmData) {
		t.Fatalf("Data mismatch: len=%d vs expected=%d", len(decoded.Data), len(pcmData))
	}
}

func TestBinaryCodec_DecodeInto_NestedStructWithBytes(t *testing.T) {
	codec := &transport.BinaryCodec{}
	id := mustCanonicalID(t, 5100, 1, 0, 1)

	type VoiceAudio struct {
		AudioType int32  `json:"AudioType"`
		Data      []byte `json:"Data"`
	}
	type VoiceRecognizeReq struct {
		Audio VoiceAudio `json:"Audio"`
	}

	pcmData := make([]byte, 1024)
	for i := range pcmData {
		pcmData[i] = byte(i & 0xff)
	}

	original := VoiceRecognizeReq{
		Audio: VoiceAudio{AudioType: 2, Data: pcmData},
	}
	structDesc := schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "struct", ClassName: "VoiceRecognizeReq"}

	view, err := codec.Encode(structDesc, id, original)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	var decoded VoiceRecognizeReq
	if err := codec.DecodeInto(view, &decoded); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if decoded.Audio.AudioType != 2 {
		t.Fatalf("expected Audio.AudioType=2, got %d", decoded.Audio.AudioType)
	}
	if len(decoded.Audio.Data) != 1024 {
		t.Fatalf("expected Audio.Data length 1024, got %d", len(decoded.Audio.Data))
	}
	if !bytes.Equal(decoded.Audio.Data, pcmData) {
		t.Fatalf("Audio.Data content mismatch")
	}
}

func TestBinaryCodec_DecodeInto_DeepNestedStruct(t *testing.T) {
	codec := &transport.BinaryCodec{}
	id := mustCanonicalID(t, 5200, 1, 0, 1)

	type Inner struct {
		Value int32  `json:"Value"`
		Blob  []byte `json:"Blob"`
	}
	type Middle struct {
		Name  string `json:"Name"`
		Child Inner  `json:"Child"`
	}
	type Outer struct {
		Label string `json:"Label"`
		Entry Middle `json:"Entry"`
	}

	original := Outer{
		Label: "test",
		Entry: Middle{
			Name:  "mid",
			Child: Inner{Value: 42, Blob: []byte{0xde, 0xad, 0xbe, 0xef}},
		},
	}
	structDesc := schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "struct", ClassName: "Outer"}

	view, err := codec.Encode(structDesc, id, original)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	var decoded Outer
	if err := codec.DecodeInto(view, &decoded); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if decoded.Label != "test" {
		t.Fatalf("expected Label=test, got %q", decoded.Label)
	}
	if decoded.Entry.Name != "mid" {
		t.Fatalf("expected Entry.Name=mid, got %q", decoded.Entry.Name)
	}
	if decoded.Entry.Child.Value != 42 {
		t.Fatalf("expected Entry.Child.Value=42, got %d", decoded.Entry.Child.Value)
	}
	if !bytes.Equal(decoded.Entry.Child.Blob, []byte{0xde, 0xad, 0xbe, 0xef}) {
		t.Fatalf("Entry.Child.Blob mismatch: %v", decoded.Entry.Child.Blob)
	}
}

func TestBinaryCodec_DecodeInto_NestedStructWithArray(t *testing.T) {
	codec := &transport.BinaryCodec{}
	id := mustCanonicalID(t, 5300, 1, 0, 1)

	type Item struct {
		Tags []string `json:"Tags"`
	}
	type Container struct {
		Item Item `json:"Item"`
	}

	original := Container{Item: Item{Tags: []string{"a", "b", "c"}}}
	structDesc := schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "struct", ClassName: "Container"}

	view, err := codec.Encode(structDesc, id, original)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	var decoded Container
	if err := codec.DecodeInto(view, &decoded); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if len(decoded.Item.Tags) != 3 {
		t.Fatalf("expected 3 tags, got %d", len(decoded.Item.Tags))
	}
	if decoded.Item.Tags[0] != "a" || decoded.Item.Tags[1] != "b" || decoded.Item.Tags[2] != "c" {
		t.Fatalf("Tags mismatch: %v", decoded.Item.Tags)
	}
}

type testErr string

func (e testErr) Error() string { return string(e) }

func mustCanonicalID(t *testing.T, ts uint64, slot uint16, inc uint16, seq uint64) identity.CanonicalID {
	t.Helper()
	id, err := identity.NewCanonicalID(ts, slot, inc, seq)
	if err != nil {
		t.Fatalf("NewCanonicalID: %v", err)
	}
	return id
}

func isEncodeError(err error, target **transport.EncodeError) bool {
	for e := err; e != nil; {
		if enc, ok := e.(*transport.EncodeError); ok {
			*target = enc
			return true
		}
		if unwrapper, ok := e.(interface{ Unwrap() error }); ok {
			e = unwrapper.Unwrap()
		} else {
			break
		}
	}
	return false
}

func isDecodeError(err error, target **transport.DecodeError) bool {
	for e := err; e != nil; {
		if dec, ok := e.(*transport.DecodeError); ok {
			*target = dec
			return true
		}
		if unwrapper, ok := e.(interface{ Unwrap() error }); ok {
			e = unwrapper.Unwrap()
		} else {
			break
		}
	}
	return false
}
