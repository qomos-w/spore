package schema

import (
	"strings"
	"testing"
)

func TestValidateMediaValue(t *testing.T) {
	cases := []struct {
		name    string
		value   any
		wantErr string
	}{
		{"struct_data", Media{Mime: "image/png", Src: "data:image/png;base64,iVBORw0KGgo="}, ""},
		{"struct_https", Media{Mime: "image/webp", Src: "https://cdn.example.com/a.webp"}, ""},
		{"struct_file", Media{Mime: "audio/ogg", Src: "file:///media/voip/1.ogg"}, ""},
		{"map_data", map[string]any{"mime": "image/gif", "src": "data:image/gif;base64,R0lGODlh"}, ""},
		{"map_string_values", map[string]string{"mime": "text/plain", "src": "https://x/y.txt"}, ""},
		{"pointer_struct", &Media{Mime: "video/mp4", Src: "https://x/y.mp4"}, ""},
		{"duck_typed_carrier", struct {
			Mime string
			Src  string
		}{Mime: "image/png", Src: "https://x/y.png"}, ""},
		{"nil", nil, "nil"},
		{"missing_mime", map[string]any{"src": "https://x/y.png"}, `"mime"`},
		{"missing_src", map[string]any{"mime": "image/png"}, `"src"`},
		{"mime_not_type_subtype", map[string]any{"mime": "imagepng", "src": "https://x/y.png"}, "type/subtype"},
		{"mime_leading_slash", map[string]any{"mime": "/png", "src": "https://x/y.png"}, "type/subtype"},
		{"mime_whitespace", map[string]any{"mime": "image /png", "src": "https://x/y.png"}, "type/subtype"},
		{"src_bad_scheme", map[string]any{"mime": "image/png", "src": "http://x/y.png"}, "data:, file:, or https:"},
		{"src_empty", map[string]any{"mime": "image/png", "src": ""}, `"src"`},
		{"wrong_carrier", "image/png", "{mime, src}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateMediaValue(tc.value)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestValidateMediaValue_InlineCap(t *testing.T) {
	ok := "data:image/png;base64," + strings.Repeat("A", MediaInlineLimit-22)
	if err := ValidateMediaValue(map[string]any{"mime": "image/png", "src": ok}); err != nil {
		t.Fatalf("value at limit rejected: %v", err)
	}
	over := "data:image/png;base64," + strings.Repeat("A", MediaInlineLimit)
	err := ValidateMediaValue(map[string]any{"mime": "image/png", "src": over})
	if err == nil {
		t.Fatal("expected oversized inline data to be rejected")
	}
	var tooLarge *MediaInlineTooLargeError
	if !asMediaTooLarge(err, &tooLarge) {
		t.Fatalf("expected *MediaInlineTooLargeError, got %T: %v", err, err)
	}
	if tooLarge.Limit != MediaInlineLimit {
		t.Fatalf("limit = %d, want %d", tooLarge.Limit, MediaInlineLimit)
	}
}

func asMediaTooLarge(err error, target **MediaInlineTooLargeError) bool {
	if e, ok := err.(*MediaInlineTooLargeError); ok {
		*target = e
		return true
	}
	return false
}

func TestTypeKindMediaString(t *testing.T) {
	if got := string(TypeKindMedia); got != "media" {
		t.Fatalf("TypeKindMedia = %q, want %q", got, "media")
	}
}
