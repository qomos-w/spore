package ts

import (
	"strings"
	"testing"

	"github.com/qomos-w/spore/schema"
)

func mediaSchemas() []NamedObjectDesc {
	return []NamedObjectDesc{
		{
			Namespace:  "media",
			SchemaID:   1,
			Name:       "Avatar",
			Visibility: VisibilityPublic,
			Object: schema.ObjectDesc{
				Kind: schema.TypeKindStruct,
				Name: "Avatar",
				Fields: []schema.FieldDesc{
					{Name: "Name", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
					{Name: "Photo", Type: schema.TypeDesc{Kind: schema.TypeKindMedia, Name: "media"}},
					{Name: "Gallery", Type: schema.TypeDesc{
						Kind:    schema.TypeKindArray,
						Element: &schema.TypeDesc{Kind: schema.TypeKindMedia, Name: "media"},
					}},
				},
			},
		},
	}
}

func TestGenerate_MediaEmitsInterfaceAndFields(t *testing.T) {
	files, err := Generate(mediaSchemas(), nil, Options{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	types, ok := files["media/types.ts"]
	if !ok {
		t.Fatalf("expected media/types.ts, got keys: %v", fileKeys(files))
	}
	if !strings.Contains(types, "export interface Media {") {
		t.Fatalf("expected Media interface:\n%s", types)
	}
	if !strings.Contains(types, "mime: string;") || !strings.Contains(types, "src: string;") {
		t.Fatalf("expected {mime, src} fields:\n%s", types)
	}
	if !strings.Contains(types, "Photo: Media;") {
		t.Fatalf("expected Photo: Media field:\n%s", types)
	}
	if !strings.Contains(types, "Gallery: Media[];") {
		t.Fatalf("expected Gallery: Media[] field:\n%s", types)
	}
}

func TestGenerate_NoMediaNoInterface(t *testing.T) {
	schemas := []NamedObjectDesc{
		{
			Namespace:  "plain",
			SchemaID:   1,
			Name:       "Plain",
			Visibility: VisibilityPublic,
			Object: schema.ObjectDesc{
				Kind:   schema.TypeKindStruct,
				Name:   "Plain",
				Fields: []schema.FieldDesc{{Name: "Note", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}}},
			},
		},
	}
	files, err := Generate(schemas, nil, Options{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if types, ok := files["plain/types.ts"]; ok && strings.Contains(types, "interface Media") {
		t.Fatalf("unexpected Media interface without media usage:\n%s", types)
	}
}

func TestGenerate_CallableOnlyMediaEmitsTypes(t *testing.T) {
	callables := []NamedCallableDesc{
		{
			Namespace:  "media",
			Name:       "Upload",
			Visibility: VisibilityPublic,
			Mode:       schema.CallableModeUnary,
			Req:        schema.TypeDesc{Kind: schema.TypeKindMedia, Name: "media"},
			Final:      schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"},
		},
	}
	files, err := Generate(nil, callables, Options{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	types, ok := files["media/types.ts"]
	if !ok {
		t.Fatalf("callable-only-media namespace must still emit types.ts; got keys: %v", fileKeys(files))
	}
	if !strings.Contains(types, "export interface Media {") {
		t.Fatalf("expected Media interface:\n%s", types)
	}
}

func fileKeys(files map[string]string) []string {
	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	return keys
}
