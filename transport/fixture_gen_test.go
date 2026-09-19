package transport_test

// fixture_gen_test.go — emits canonical hex fixtures for cross-language
// contract testing with the TS BinaryCodec. Run manually with:
//
//   go test -run TestBinaryCodec_EmitFixtures -v ./transport/...
//
// then copy the printed hex strings into spore/ts/tests/binary_codec_contract.test.ts.

import (
	"encoding/hex"
	"testing"

	"github.com/qomos-w/spore/identity"
	"github.com/qomos-w/spore/schema"
	"github.com/qomos-w/spore/transport"
)

func TestBinaryCodec_EmitFixtures(t *testing.T) {
	if testing.Short() {
		t.Skip("fixture emitter; skipped under -short")
	}
	codec := &transport.BinaryCodec{}
	id, err := identity.NewCanonicalID(1000, 1, 0, 200)
	if err != nil {
		t.Fatal(err)
	}

	emit := func(name string, desc schema.TypeDesc, v any) {
		view, err := codec.Encode(desc, id, v)
		if err != nil {
			t.Fatalf("%s encode: %v", name, err)
		}
		t.Logf("FIXTURE %s = %s", name, hex.EncodeToString(view.Data))
	}

	boolDesc := schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "bool"}
	intDesc := schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}
	int32Desc := schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int32"}
	stringDesc := schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}
	bytesDesc := schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "bytes"}
	floatDesc := schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "float64"}

	emit("bool_true", boolDesc, true)
	emit("bool_false", boolDesc, false)
	emit("int32_42", int32Desc, int32(42))
	emit("int32_neg", int32Desc, int32(-7))
	emit("int64_small", intDesc, int64(123456789))
	emit("string_hello", stringDesc, "hello")
	emit("string_utf8", stringDesc, "héllo 世界")
	emit("bytes_deadbeef", bytesDesc, []byte{0xde, 0xad, 0xbe, 0xef})
	emit("float_pi", floatDesc, 3.14159)

	arrDesc := schema.TypeDesc{Kind: schema.TypeKindArray, Element: &stringDesc}
	emit("array_strings", arrDesc, []string{"a", "b", "c"})

	mapDesc := schema.TypeDesc{Kind: schema.TypeKindMap, Key: &stringDesc, Value: &int32Desc}
	emit("map_three", mapDesc, map[string]int32{"z": 1, "a": 2, "m": 3})

	omDesc := schema.TypeDesc{Kind: schema.TypeKindMap, Key: &stringDesc, Value: &int32Desc}
	om := schema.NewOrderedMap[string, int32]()
	om.Set("z", 26)
	om.Set("a", 1)
	om.Set("m", 13)
	emit("ordered_map_zam", omDesc, om)

	// Struct fixtures use real Go structs so TAG_STRUCT encoding is exercised.
	type fixtureVoiceAudio struct {
		AudioType int32
		Data      []byte
	}
	type fixtureVoiceRecognizeReq struct {
		Audio fixtureVoiceAudio
	}

	audioDesc := schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "VoiceAudio", ClassID: 447}
	emit("struct_voice_audio", audioDesc, fixtureVoiceAudio{
		AudioType: 2,
		Data:      []byte{0x01, 0x02, 0x03, 0x04},
	})

	reqDesc := schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "VoiceRecognizeReq", ClassID: 446}
	emit("struct_voice_recognize_req", reqDesc, fixtureVoiceRecognizeReq{
		Audio: fixtureVoiceAudio{
			AudioType: 2,
			Data:      []byte{0xde, 0xad, 0xbe, 0xef, 0x00, 0xff},
		},
	})
}
