package transport_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/qomos-w/spore/schema"
	"github.com/qomos-w/spore/transport"
)

// mediaDesc is the canonical media TypeDesc for codec contract tests.
var mediaDesc = schema.TypeDesc{Kind: schema.TypeKindMedia, Name: "media"}

func TestJSONCodec_MediaRoundTrip(t *testing.T) {
	codec := &transport.JSONCodec{}
	id := mustCanonicalID(t, 1000, 1, 0, 900)

	in := map[string]any{"mime": "image/png", "src": "https://cdn.example.com/a.png"}
	view, err := codec.Encode(mediaDesc, id, in)
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
	if m["mime"] != "image/png" || m["src"] != "https://cdn.example.com/a.png" {
		t.Fatalf("round trip mismatch: %v", m)
	}
}

func TestBinaryCodec_MediaRoundTrip(t *testing.T) {
	codec := &transport.BinaryCodec{}
	id := mustCanonicalID(t, 1000, 1, 0, 901)

	in := map[string]any{"mime": "image/png", "src": "data:image/png;base64,iVBORw0KGgo="}
	view, err := codec.Encode(mediaDesc, id, in)
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
	if m["mime"] != "image/png" || m["src"] != "data:image/png;base64,iVBORw0KGgo=" {
		t.Fatalf("round trip mismatch: %v", m)
	}
}

func TestBinaryCodec_MediaStructRoundTrip(t *testing.T) {
	codec := &transport.BinaryCodec{}
	id := mustCanonicalID(t, 1000, 1, 0, 902)

	in := schema.Media{Mime: "audio/ogg", Src: "file:///media/voip/1.ogg"}
	view, err := codec.Encode(mediaDesc, id, in)
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
	if m["mime"] != "audio/ogg" || m["src"] != "file:///media/voip/1.ogg" {
		t.Fatalf("round trip mismatch: %v", m)
	}
}

func TestCodecs_RejectOversizedInlineData(t *testing.T) {
	oversized := "data:image/png;base64," + strings.Repeat("A", 1<<21)
	cases := []struct {
		name  string
		codec transport.Codec
	}{
		{"json", &transport.JSONCodec{}},
		{"binary", &transport.BinaryCodec{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id := mustCanonicalID(t, 1000, 1, 0, 903)
			_, err := tc.codec.Encode(mediaDesc, id, map[string]any{"mime": "image/png", "src": oversized})
			if err == nil {
				t.Fatal("expected encode rejection of oversized inline data:")
			}
			var tooLarge *schema.MediaInlineTooLargeError
			if !errors.As(err, &tooLarge) {
				t.Fatalf("expected MediaInlineTooLargeError, got %T: %v", err, err)
			}
			if tooLarge.Limit != schema.MediaInlineLimit {
				t.Fatalf("limit = %d, want %d", tooLarge.Limit, schema.MediaInlineLimit)
			}
		})
	}
}

func TestCodecs_RejectBadMediaShape(t *testing.T) {
	bad := []struct {
		name  string
		value any
		want  string
	}{
		{"missing_src", map[string]any{"mime": "image/png"}, `"src"`},
		{"missing_mime", map[string]any{"src": "https://x/y.png"}, `"mime"`},
		{"bad_mime", map[string]any{"mime": "imagepng", "src": "https://x/y.png"}, "type/subtype"},
		{"bad_scheme", map[string]any{"mime": "image/png", "src": "http://x/y.png"}, "data:, file:, or https:"},
		{"wrong_type", "image/png", "{mime, src}"},
	}
	for _, bc := range bad {
		t.Run(bc.name, func(t *testing.T) {
			id := mustCanonicalID(t, 1000, 1, 0, 904)
			_, err := (&transport.JSONCodec{}).Encode(mediaDesc, id, bc.value)
			if err == nil {
				t.Fatalf("expected encode rejection, got nil")
			}
			if !strings.Contains(err.Error(), bc.want) {
				t.Fatalf("error %q does not contain %q", err.Error(), bc.want)
			}
		})
	}
}

func TestCodecs_DecodeRejectsTamperedMedia(t *testing.T) {
	codec := &transport.JSONCodec{}
	id := mustCanonicalID(t, 1000, 1, 0, 905)

	view, err := codec.Encode(mediaDesc, id, map[string]any{"mime": "image/png", "src": "https://cdn.example.com/a.png"})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	view.Data = []byte(`{"mime":"image/png","src":"http://insecure.example.com/a.png"}`)
	if _, err := codec.Decode(view); err == nil {
		t.Fatal("expected decode rejection of non-whitelisted scheme")
	}
}
