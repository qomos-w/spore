package gotypes

import (
	"regexp"
	"strings"
	"testing"

	"github.com/qomos-w/spore/schema"
)

func mediaSchema() []schema.ObjectDesc {
	return []schema.ObjectDesc{
		{
			Kind: schema.TypeKindStruct,
			Name: "Avatar",
			Fields: []schema.FieldDesc{
				{Name: "name", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
				{Name: "photo", Type: schema.TypeDesc{Kind: schema.TypeKindMedia, Name: "media"}},
				{Name: "gallery", Type: schema.TypeDesc{
					Kind:    schema.TypeKindArray,
					Element: &schema.TypeDesc{Kind: schema.TypeKindMedia, Name: "media"},
				}},
			},
		},
	}
}

func TestRender_MediaFieldEmitsCarrierAndValidation(t *testing.T) {
	out, err := Render(mediaSchema(), Options{Package: "p"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	got := string(out)

	if !strings.Contains(got, "type Media struct {") {
		t.Fatalf("expected Media carrier type in output:\n%s", got)
	}
	if !strings.Contains(got, "func (m Media) Validate() error {") {
		t.Fatalf("expected Media.Validate method in output:\n%s", got)
	}
	if !strings.Contains(got, "return schema.ValidateMediaValue(m)") {
		t.Fatalf("expected Validate to delegate to schema.ValidateMediaValue:\n%s", got)
	}
	photoField := regexp.MustCompile(`Photo\s+Media\s+` + "`" + `json:"photo"` + "`")
	if !photoField.MatchString(got) {
		t.Fatalf("expected photo Media field:\n%s", got)
	}
	galleryField := regexp.MustCompile(`Gallery\s+\[\]Media\s+` + "`" + `json:"gallery"` + "`")
	if !galleryField.MatchString(got) {
		t.Fatalf("expected gallery []Media field:\n%s", got)
	}
	if !strings.Contains(got, "import \"github.com/qomos-w/spore/schema\"") {
		t.Fatalf("expected schema import for media validation:\n%s", got)
	}
}

func TestRender_NoMediaNoCarrier(t *testing.T) {
	objs := []schema.ObjectDesc{
		{
			Kind:   schema.TypeKindStruct,
			Name:   "Plain",
			Fields: []schema.FieldDesc{{Name: "note", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}}},
		},
	}
	out, err := Render(objs, Options{Package: "p"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.Contains(string(out), "type Media struct {") {
		t.Fatalf("unexpected Media carrier without media usage:\n%s", string(out))
	}
}

func TestRender_UserStructNamedMediaWins(t *testing.T) {
	objs := mediaSchema()
	objs = append(objs, schema.ObjectDesc{
		Kind:   schema.TypeKindStruct,
		Name:   "Media",
		Fields: []schema.FieldDesc{{Name: "note", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}}},
	})
	out, err := Render(objs, Options{Package: "p"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	got := string(out)
	count := strings.Count(got, "type Media struct {")
	if count != 1 {
		t.Fatalf("expected exactly one Media declaration, got %d:\n%s", count, got)
	}
	if strings.Contains(got, "func (m Media) Validate() error {") {
		t.Fatalf("generated Validate must not collide with user struct Media:\n%s", got)
	}
}

func TestRender_MediaWithComponentsKeepsImports(t *testing.T) {
	objs := mediaSchema()
	for i := range objs {
		objs[i].SchemaID = 310
		objs[i].IsComponent = true
	}
	out, err := Render(objs, Options{Package: "p", EmitComponents: true})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	got := string(out)
	if !strings.Contains(got, "\"github.com/qomos-w/spore/runtime\"") {
		t.Fatalf("expected runtime import:\n%s", got)
	}
	if !strings.Contains(got, "\"github.com/qomos-w/spore/schema\"") {
		t.Fatalf("expected schema import:\n%s", got)
	}
	if !strings.Contains(got, "type Media struct {") {
		t.Fatalf("expected Media carrier:\n%s", got)
	}
}
