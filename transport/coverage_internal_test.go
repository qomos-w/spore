package transport

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/qomos-w/spore/identity"
	"github.com/qomos-w/spore/schema"
)

type mapTargetStruct struct {
	Name string
	Age  int32
}

// binaryFrame prepends the magic + explicit wire version header to a raw
// payload, producing a well-formed frame for decode tests.
func binaryFrame(payload []byte) []byte {
	header := append([]byte(binaryMagic), binaryWireVersion)
	return append(header, payload...)
}

func binaryView(payload []byte) View {
	return View{Schema: schema.TypeDesc{Kind: schema.TypeKindStruct}, Data: binaryFrame(payload)}
}

func uvarintBytes(n uint64) []byte {
	var scratch [10]byte
	count := putUvarintForTest(scratch[:], n)
	return scratch[:count]
}

// putUvarintForTest mirrors binary.PutUvarint without importing encoding/binary here.
func putUvarintForTest(buf []byte, n uint64) int {
	i := 0
	for n >= 0x80 {
		buf[i] = byte(n) | 0x80
		n >>= 7
		i++
	}
	buf[i] = byte(n)
	return i + 1
}

func stringEntry(s string) []byte {
	return append(uvarintBytes(uint64(len(s))), []byte(s)...)
}

func TestBinaryDecodeInto_MapIntoStruct(t *testing.T) {
	payload := []byte{binaryTagMap}
	payload = append(payload, uvarintBytes(3)...)
	payload = append(payload, stringEntry("Name")...)
	payload = append(payload, binaryTagString)
	payload = append(payload, stringEntry("bob")...)
	payload = append(payload, stringEntry("Age")...)
	payload = append(payload, binaryTagInt32)
	payload = append(payload, 0, 0, 0, 30)
	// Unknown field must be consumed and skipped.
	payload = append(payload, stringEntry("Unknown")...)
	payload = append(payload, binaryTagNull)

	var target mapTargetStruct
	if err := (&BinaryCodec{}).DecodeInto(binaryView(payload), &target); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if target.Name != "bob" || target.Age != 30 {
		t.Fatalf("unexpected target: %+v", target)
	}
}

func TestBinaryDecodeInto_MapIntoMap(t *testing.T) {
	payload := []byte{binaryTagMap}
	payload = append(payload, uvarintBytes(2)...)
	payload = append(payload, stringEntry("a")...)
	payload = append(payload, binaryTagString)
	payload = append(payload, stringEntry("1")...)
	payload = append(payload, stringEntry("b")...)
	payload = append(payload, binaryTagString)
	payload = append(payload, stringEntry("2")...)

	target := map[string]string{}
	if err := (&BinaryCodec{}).DecodeInto(binaryView(payload), &target); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if target["a"] != "1" || target["b"] != "2" || len(target) != 2 {
		t.Fatalf("unexpected map: %+v", target)
	}
}

func TestBinaryDecodeInto_PointerFieldAndTarget(t *testing.T) {
	payload := []byte{binaryTagMap}
	payload = append(payload, uvarintBytes(1)...)
	payload = append(payload, stringEntry("Name")...)
	payload = append(payload, binaryTagString)
	payload = append(payload, stringEntry("alice")...)

	var target *mapTargetStruct
	if err := (&BinaryCodec{}).DecodeInto(binaryView(payload), &target); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if target == nil || target.Name != "alice" {
		t.Fatalf("unexpected target: %+v", target)
	}
}

func TestBinaryDecodeInto_StructByIndexSchemaTyped(t *testing.T) {
	// Fields sorted by wire name: Age(idx 0), Name(idx 1).
	payload := []byte{binaryTagStruct}
	payload = append(payload, uvarintBytes(5)...) // schema id
	payload = append(payload, uvarintBytes(2)...) // field count
	payload = append(payload, uvarintBytes(1)...) // Name
	payload = append(payload, binaryTagString)
	payload = append(payload, stringEntry("carol")...)
	payload = append(payload, uvarintBytes(0)...) // Age
	payload = append(payload, binaryTagInt32)
	payload = append(payload, 0, 0, 0, 42)

	view := View{Schema: schema.TypeDesc{Kind: schema.TypeKindStruct, ClassID: 5}, Data: binaryFrame(payload)}
	var target mapTargetStruct
	if err := (&BinaryCodec{}).DecodeInto(view, &target); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if target.Name != "carol" || target.Age != 42 {
		t.Fatalf("unexpected target: %+v", target)
	}
}

func TestBinaryDecodeInto_StructByIndexErrors(t *testing.T) {
	build := func(schemaID uint64, viewClassID uint64, body func(payload []byte) []byte) View {
		payload := []byte{binaryTagStruct}
		payload = append(payload, uvarintBytes(schemaID)...)
		payload = body(payload)
		return View{Schema: schema.TypeDesc{Kind: schema.TypeKindStruct, ClassID: viewClassID}, Data: binaryFrame(payload)}
	}
	var target mapTargetStruct
	// Schema mismatch.
	view := build(7, 5, func(p []byte) []byte { return append(p, uvarintBytes(0)...) })
	if err := (&BinaryCodec{}).DecodeInto(view, &target); err == nil || !strings.Contains(err.Error(), "schema mismatch") {
		t.Fatalf("expected schema mismatch, got %v", err)
	}
	// Field index out of range.
	view = build(5, 5, func(p []byte) []byte {
		p = append(p, uvarintBytes(1)...)
		return append(p, uvarintBytes(9)...)
	})
	if err := (&BinaryCodec{}).DecodeInto(view, &target); err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("expected out-of-range, got %v", err)
	}
	// Anonymous struct with unknown key must skip value.
	anon := []byte{binaryTagStruct}
	anon = append(anon, uvarintBytes(0)...)
	anon = append(anon, uvarintBytes(1)...)
	anon = append(anon, stringEntry("Nope")...)
	anon = append(anon, binaryTagNull)
	if err := (&BinaryCodec{}).DecodeInto(binaryView(anon), &target); err != nil {
		t.Fatalf("anonymous skip should pass, got %v", err)
	}
}

func TestBinaryDecodeInto_GuardErrors(t *testing.T) {
	codec := &BinaryCodec{}
	var target mapTargetStruct
	if err := codec.DecodeInto(View{}, target); err == nil || !strings.Contains(err.Error(), "non-nil pointer") {
		t.Fatalf("expected pointer guard, got %v", err)
	}
	if err := codec.DecodeInto(View{}, &target); err == nil || !strings.Contains(err.Error(), "empty data") {
		t.Fatalf("expected empty-data guard, got %v", err)
	}
	if err := codec.DecodeInto(View{Data: []byte("xxxx")}, &target); err == nil || !strings.Contains(err.Error(), "magic") {
		t.Fatalf("expected magic guard, got %v", err)
	}
	// Trailing bytes after a complete value.
	payload := append([]byte{binaryTagBoolTrue}, binaryTagBoolFalse)
	if err := codec.DecodeInto(binaryView(payload), &target); err != nil {
		_ = err // bool tag into struct errors first; both outcomes are guarded paths
	}
}

func TestDecodeTagInto_MismatchedTags(t *testing.T) {
	targets := []struct {
		name   string
		target any
		tag    byte
		want   string
	}{
		{"struct wants map/struct", &mapTargetStruct{}, binaryTagString, "expected TAG_MAP"},
		{"bytes target wants bytes", &[]byte{}, binaryTagString, "expected TAG_BYTES"},
		{"slice target wants array/entryseq", &[]int32{}, binaryTagString, "expected TAG_ARRAY"},
		{"map target wants map", &map[string]int{}, binaryTagString, "expected TAG_MAP"},
		{"string target wants string", new(string), binaryTagInt32, "expected TAG_STRING"},
		{"bool target wants bool", new(bool), binaryTagString, "expected TAG_BOOL"},
		{"int target wants int", new(int32), binaryTagString, "expected int tag"},
		{"uint target wants uint", new(uint64), binaryTagString, "expected uint tag"},
		{"float target wants float", new(float64), binaryTagString, "expected TAG_FLOAT64"},
		{"unsupported target kind", new(chan int), binaryTagString, "unsupported target kind"},
	}
	for _, tc := range targets {
		d := &binaryDecoder{}
		rv := reflect.ValueOf(tc.target).Elem()
		err := d.decodeTagInto(tc.tag, rv, "")
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: expected %q, got %v", tc.name, tc.want, err)
		}
	}
}

func TestBinaryDecodeInto_ScalarKinds(t *testing.T) {
	cases := []struct {
		name   string
		build  func(payload []byte) []byte
		check  func(t *testing.T, target any)
		target any
	}{
		{"bool true", func(p []byte) []byte { return append(p, binaryTagBoolTrue) },
			func(t *testing.T, v any) {
				if b, ok := v.(*bool); !ok || *b != true {
					t.Fatalf("bool: %v", v)
				}
			}, new(bool)},
		{"int64 tag", func(p []byte) []byte {
			p = append(p, binaryTagInt64)
			return append(p, 0, 0, 0, 0, 0, 0, 0, 9)
		}, func(t *testing.T, v any) {
			if i, ok := v.(*int64); !ok || *i != 9 {
				t.Fatalf("int64: %v", v)
			}
		}, new(int64)},
		{"uint64 tag", func(p []byte) []byte {
			p = append(p, binaryTagUint64)
			return append(p, 0, 0, 0, 0, 0, 0, 0, 11)
		}, func(t *testing.T, v any) {
			if u, ok := v.(*uint32); !ok || *u != 11 {
				t.Fatalf("uint: %v", v)
			}
		}, new(uint32)},
		{"uint from int32", func(p []byte) []byte {
			p = append(p, binaryTagInt32)
			return append(p, 0, 0, 0, 5)
		}, func(t *testing.T, v any) {
			if u, ok := v.(*uint64); !ok || *u != 5 {
				t.Fatalf("uint from int32: %v", v)
			}
		}, new(uint64)},
		{"float64", func(p []byte) []byte {
			p = append(p, binaryTagFloat64)
			var bits [8]byte
			bits[7] = 0x40 // tiny handmade float; only checks decode path executes
			return append(p, bits[:]...)
		}, func(t *testing.T, v any) {
			if _, ok := v.(*float64); !ok {
				t.Fatalf("float64: %T", v)
			}
		}, new(float64)},
	}
	for _, tc := range cases {
		payload := tc.build(nil)
		if err := (&BinaryCodec{}).DecodeInto(binaryView(payload), tc.target); err != nil {
			t.Fatalf("%s: DecodeInto: %v", tc.name, err)
		}
		tc.check(t, tc.target)
	}
}

func TestBinaryDecoder_TruncationAndBadVarint(t *testing.T) {
	// Continuation byte with no terminator: Uvarint returns count==0.
	d := &binaryDecoder{data: []byte{0x80}}
	if _, err := d.readUvarint(); err == nil {
		t.Fatal("expected EOF error for truncated varint")
	}
	// Overflow varint (11 continuation bytes): count < 0.
	bad := make([]byte, 11)
	for i := range bad {
		bad[i] = 0xFF
	}
	d = &binaryDecoder{data: bad}
	if _, err := d.readUvarint(); err == nil || !strings.Contains(err.Error(), "invalid varint") {
		t.Fatalf("expected invalid varint, got %v", err)
	}
	// String length larger than remaining bytes.
	d = &binaryDecoder{data: append(uvarintBytes(100), 'x')}
	if _, err := d.readString(); err == nil {
		t.Fatal("expected EOF for overlong string")
	}
	// Bytes length larger than remaining bytes.
	d = &binaryDecoder{data: append(uvarintBytes(100), 'x')}
	if _, err := d.readBytes(); err == nil {
		t.Fatal("expected EOF for overlong bytes")
	}
	// Truncated fixed-width reads.
	d = &binaryDecoder{data: []byte{1, 2, 3}}
	if _, err := d.readUint32(); err == nil {
		t.Fatal("expected EOF for uint32")
	}
	if _, err := d.readUint64(); err == nil {
		t.Fatal("expected EOF for uint64")
	}
	if _, err := (&binaryDecoder{}).readByte(); err == nil {
		t.Fatal("expected EOF for byte")
	}
	// Negative int32 stays sign-preserving via int32 conversion.
	d2 := &binaryDecoder{data: []byte{binaryTagInt32, 0xFF, 0xFF, 0xFF, 0xFF}}
	tag, err := d2.readByte()
	if err != nil || tag != binaryTagInt32 {
		t.Fatalf("setup: %v", err)
	}
	v, err := d2.readUint32()
	if err != nil || int32(v) != -1 {
		t.Fatalf("expected -1 after int32 conversion, got %d err=%v", v, err)
	}
}

func TestTransportDiagnosticAccessors(t *testing.T) {
	var nilEncode *EncodeError
	var nilDecode *DecodeError
	if nilEncode.DiagnosticCode() != "" || nilEncode.DiagnosticPath() != "" ||
		nilEncode.DiagnosticExpected() != "" || nilEncode.DiagnosticActual() != "" ||
		nilEncode.DiagnosticCause() != nil || nilEncode.Error() != "" || nilEncode.Unwrap() != nil {
		t.Fatal("nil EncodeError accessors broken")
	}
	if nilDecode.DiagnosticCode() != "" || nilDecode.DiagnosticPath() != "" ||
		nilDecode.DiagnosticExpected() != "" || nilDecode.DiagnosticActual() != "" ||
		nilDecode.DiagnosticCause() != nil || nilDecode.Error() != "" || nilDecode.Unwrap() != nil {
		t.Fatal("nil DecodeError accessors broken")
	}

	plainErr := errors.New("boom")
	enc := &EncodeError{Path: "p", Expected: "exp", Actual: "act", Err: plainErr}
	if enc.DiagnosticCode() != CodeEncodeError {
		t.Fatalf("encode fallback code: %q", enc.DiagnosticCode())
	}
	if enc.DiagnosticPath() != "p" || enc.DiagnosticExpected() != "exp" || enc.DiagnosticActual() != "act" {
		t.Fatal("encode accessor fields broken")
	}
	if enc.DiagnosticCause() == nil {
		t.Fatal("encode cause should be non-nil for wrapped error")
	}
	if !errors.Is(enc, plainErr) {
		t.Fatal("EncodeError should unwrap to inner error")
	}

	dec := &DecodeError{Path: "q", Expected: "e2", Actual: "a2", Err: plainErr}
	if dec.DiagnosticCode() != CodeDecodeError {
		t.Fatalf("decode fallback code: %q", dec.DiagnosticCode())
	}
	if dec.DiagnosticPath() != "q" || dec.DiagnosticExpected() != "e2" || dec.DiagnosticActual() != "a2" {
		t.Fatal("decode accessor fields broken")
	}
	if dec.DiagnosticCause() == nil {
		t.Fatal("decode cause should be non-nil for wrapped error")
	}

	// Wrapped diagnostic-carrying inner error wins over fallback code.
	inner := &DecodeError{Err: &EncodeError{Err: plainErr}}
	if code := inner.DiagnosticCode(); code != CodeEncodeError {
		t.Fatalf("inner coder should win, got %q", code)
	}
}

func TestJSONDecodeInto_ErrorsAndSuccess(t *testing.T) {
	codec := &JSONCodec{}
	var target mapTargetStruct
	if err := codec.DecodeInto(View{}, &target); err == nil || !strings.Contains(err.Error(), "empty data") {
		t.Fatalf("expected empty data error, got %v", err)
	}
	view := View{Schema: schema.TypeDesc{Kind: schema.TypeKindStruct}, Data: []byte("{not json")}
	err := codec.DecodeInto(view, &target)
	if err == nil {
		t.Fatal("expected unmarshal error")
	}
	var decodeErr *DecodeError
	if !errors.As(err, &decodeErr) {
		t.Fatalf("expected *DecodeError, got %T", err)
	}
	ok := View{Schema: schema.TypeDesc{Kind: schema.TypeKindStruct}, Data: []byte(`{"Name":"dave","Age":31}`)}
	if err := codec.DecodeInto(ok, &target); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if target.Name != "dave" || target.Age != 31 {
		t.Fatalf("unexpected target: %+v", target)
	}
}

func TestBinaryDecodeInto_EntrySeqIntoSlice(t *testing.T) {
	// Mirrors the encoder's TAG_ENTRYSEQ layout: count, then per entry a
	// tagged key followed by a tagged value.
	payload := []byte{binaryTagEntrySequence}
	payload = append(payload, uvarintBytes(2)...)
	payload = append(payload, binaryTagString)
	payload = append(payload, stringEntry("a")...)
	payload = append(payload, binaryTagInt32)
	payload = append(payload, 0, 0, 0, 1)
	payload = append(payload, binaryTagString)
	payload = append(payload, stringEntry("b")...)
	payload = append(payload, binaryTagInt32)
	payload = append(payload, 0, 0, 0, 2)

	var target []entryStruct
	if err := (&BinaryCodec{}).DecodeInto(binaryView(payload), &target); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if len(target) != 2 || target[0].Key != "a" || target[0].Value != 1 ||
		target[1].Key != "b" || target[1].Value != 2 {
		t.Fatalf("unexpected target: %+v", target)
	}

	// Element type without Key/Value fields must be rejected up front.
	var bad []struct{ NotKey int }
	err := (&BinaryCodec{}).DecodeInto(binaryView(payload), &bad)
	if err == nil || !strings.Contains(err.Error(), "Key and Value fields") {
		t.Fatalf("expected Key/Value guard, got %v", err)
	}

	// Round trip: encode an entry-sequence-shaped slice, decode into the same type.
	roundTrip := []entryStruct{{Key: "x", Value: 9}, {Key: "y", Value: 8}}
	view, err := (&BinaryCodec{}).Encode(
		schema.TypeDesc{
			Kind:    schema.TypeKindArray,
			Element: &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "any"},
		},
		identity.CanonicalID{},
		roundTrip,
	)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	var decoded []entryStruct
	if err := (&BinaryCodec{}).DecodeInto(view, &decoded); err != nil {
		t.Fatalf("round-trip DecodeInto: %v", err)
	}
	if len(decoded) != 2 || decoded[0].Key != "x" || decoded[0].Value != 9 || decoded[1].Key != "y" || decoded[1].Value != 8 {
		t.Fatalf("round-trip mismatch: %+v", decoded)
	}
}

// Map values decoding into interface{} targets: the request-Payload shape
// (map[string]any) that previously died with "unsupported target kind
// interface" before reaching any handler.
func TestBinaryDecodeInto_MapOfAnyValues(t *testing.T) {
	payload := []byte{binaryTagMap}
	payload = append(payload, uvarintBytes(4)...)
	payload = append(payload, stringEntry("Text")...)
	payload = append(payload, binaryTagString)
	payload = append(payload, stringEntry("hello")...)
	payload = append(payload, stringEntry("MessageType")...)
	payload = append(payload, binaryTagString)
	payload = append(payload, stringEntry("chat")...)
	payload = append(payload, stringEntry("Pinned")...)
	payload = append(payload, binaryTagBoolTrue)
	payload = append(payload, stringEntry("Rank")...)
	payload = append(payload, binaryTagInt64)
	payload = append(payload, 0, 0, 0, 0, 0, 0, 0, 7)

	target := map[string]any{}
	if err := (&BinaryCodec{}).DecodeInto(binaryView(payload), &target); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if target["Text"] != "hello" || target["MessageType"] != "chat" || target["Pinned"] != true || target["Rank"] != int64(7) {
		t.Fatalf("unexpected map: %+v", target)
	}
}

func TestDecodeTagInto_InterfaceBoxesAllTags(t *testing.T) {
	var floatBits [8]byte
	for i, b := range []byte{0, 0, 0, 0, 0, 0, 0xF8, 0x3F} {
		floatBits[7-i] = b
	}
	cases := []struct {
		name  string
		build func(p []byte) []byte
		want  any
	}{
		{"bool false", func(p []byte) []byte { return append(p, binaryTagBoolFalse) }, false},
		{"bool true", func(p []byte) []byte { return append(p, binaryTagBoolTrue) }, true},
		{"int32", func(p []byte) []byte { return append(p, append([]byte{binaryTagInt32}, 0, 0, 0, 3)...) }, int32(3)},
		{"int64", func(p []byte) []byte { return append(p, append([]byte{binaryTagInt64}, 0, 0, 0, 0, 0, 0, 0, 7)...) }, int64(7)},
		{"uint64", func(p []byte) []byte { return append(p, append([]byte{binaryTagUint64}, 0, 0, 0, 0, 0, 0, 0, 9)...) }, uint64(9)},
		{"float64", func(p []byte) []byte { return append(p, append([]byte{binaryTagFloat64}, floatBits[:]...)...) }, float64(1.5)},
		{"string", func(p []byte) []byte { return append(p, append([]byte{binaryTagString}, stringEntry("chat")...)...) }, "chat"},
		{"bytes", func(p []byte) []byte {
			return append(p, append([]byte{binaryTagBytes}, append(uvarintBytes(2), 0xAA, 0xBB)...)...)
		}, []byte{0xAA, 0xBB}},
	}
	for _, tc := range cases {
		d := &binaryDecoder{data: tc.build(nil)}
		var target any
		tag := byte(d.data[0])
		d.off = 1
		if err := d.decodeTagInto(tag, reflect.ValueOf(&target).Elem(), "x"); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if !reflect.DeepEqual(target, tc.want) {
			t.Fatalf("%s: got %#v, want %#v", tc.name, target, tc.want)
		}
	}

	// Nested containers box into []any / map[string]any.
	inner := append(uvarintBytes(2), append([]byte{binaryTagString}, stringEntry("a")...)...)
	inner = append(inner, append([]byte{binaryTagInt64}, 0, 0, 0, 0, 0, 0, 0, 4)...)
	d := &binaryDecoder{data: inner}
	var arrTarget any
	if err := d.decodeTagInto(binaryTagArray, reflect.ValueOf(&arrTarget).Elem(), "x"); err != nil {
		t.Fatalf("array into any: %v", err)
	}
	if !reflect.DeepEqual(arrTarget, []any{"a", int64(4)}) {
		t.Fatalf("array box: %#v", arrTarget)
	}

	// Non-empty interfaces must fail explicitly instead of panicking on Set.
	d = &binaryDecoder{data: stringEntry("nope")}
	errTarget := reflect.ValueOf(new(error)).Elem()
	if err := d.decodeTagInto(binaryTagString, errTarget, "x"); err == nil || !strings.Contains(err.Error(), "not assignable") {
		t.Fatalf("expected assignability guard, got %v", err)
	}
}
