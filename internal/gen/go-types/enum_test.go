package gotypes

import (
	"regexp"
	"strings"
	"testing"

	"github.com/qomos-w/spore/schema"
	"github.com/qomos-w/spore/script"
)

func TestRender_EnumsEmitTypedConstants(t *testing.T) {
	objs := []schema.ObjectDesc{
		{
			Kind: schema.TypeKindStruct,
			Name: "Pixel",
			Fields: []schema.FieldDesc{
				{Name: "color", Type: schema.TypeDesc{Kind: schema.TypeKindEnum, Name: "Color"}},
				{Name: "trail", Type: schema.TypeDesc{
					Kind:    schema.TypeKindArray,
					Element: &schema.TypeDesc{Kind: schema.TypeKindEnum, Name: "Color"},
				}},
			},
		},
	}
	enums := []schema.EnumDesc{
		{Name: "Color", Members: []schema.EnumMemberDesc{
			{Name: "Red", Value: 1},
			{Name: "Green", Value: 2},
			{Name: "Blue", Value: 3},
		}},
	}

	out, err := Render(objs, Options{Package: "p", Enums: enums})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	got := string(out)

	if !strings.Contains(got, "type Color int") {
		t.Fatalf("expected enum type declaration:\n%s", got)
	}
	constRe := regexp.MustCompile(`ColorRed\s+Color\s+=\s+1`)
	if !constRe.MatchString(got) {
		t.Fatalf("expected ColorRed constant:\n%s", got)
	}
	if strings.Count(got, "Color =") != 3 {
		t.Fatalf("expected 3 Color constants:\n%s", got)
	}
	colorField := regexp.MustCompile(`Color\s+Color\s+` + "`" + `json:"color"` + "`")
	if !colorField.MatchString(got) {
		t.Fatalf("expected Color-typed field:\n%s", got)
	}
	trailField := regexp.MustCompile(`Trail\s+\[\]Color\s+` + "`" + `json:"trail"` + "`")
	if !trailField.MatchString(got) {
		t.Fatalf("expected []Color-typed field:\n%s", got)
	}
}

func TestRender_EnumFieldWithoutDeclarationStillMapsName(t *testing.T) {
	// Cross-file enum references: the enum type is declared in another file
	// of the same package, so no local emission — the field still maps.
	objs := []schema.ObjectDesc{
		{
			Kind: schema.TypeKindStruct,
			Name: "Late",
			Fields: []schema.FieldDesc{
				{Name: "status", Type: schema.TypeDesc{Kind: schema.TypeKindEnum, Name: "Status"}},
			},
		},
	}
	out, err := Render(objs, Options{Package: "p"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(string(out), "Status") {
		t.Fatalf("expected Status field type:\n%s", string(out))
	}
}

func TestExportName_NoInitialisms(t *testing.T) {
	opts := exportNameOptions{noInitialisms: true}
	cases := map[string]string{
		"pawnId":    "PawnId",
		"apiUrl":    "ApiUrl",
		"id":        "Id",
		"projectId": "ProjectId",
		"html":      "Html",
		"aiShell":   "AiShell",
		"file_id":   "FileId",
		// non-initialism words unchanged by the toggle
		"agentType": "AgentType",
		"old_string": "OldString",
	}
	for in, want := range cases {
		got := exportNameWith(in, opts)
		if got != want {
			t.Errorf("exportNameWith(%q, noInitialisms) = %q, want %q", in, got, want)
		}
	}
	// default path keeps initialisms
	if got := exportNameWith("pawnId", exportNameOptions{}); got != "PawnID" {
		t.Errorf("default exportNameWith(pawnId) = %q, want PawnID", got)
	}
}

func TestRender_NoInitialisms(t *testing.T) {
	objs := []schema.ObjectDesc{
		{
			Kind: schema.TypeKindStruct,
			Name: "Pawn",
			Fields: []schema.FieldDesc{
				{Name: "pawnId", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
				{Name: "apiUrl", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
			},
		},
	}
	out, err := Render(objs, Options{Package: "p", NoInitialisms: true})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	got := string(out)
	pawnID := regexp.MustCompile(`PawnId\s+string\s+` + "`" + `json:"pawnId"` + "`")
	apiURL := regexp.MustCompile(`ApiUrl\s+string\s+` + "`" + `json:"apiUrl"` + "`")
	if !pawnID.MatchString(got) || !apiURL.MatchString(got) {
		t.Fatalf("expected PawnId/ApiUrl fields:\n%s", got)
	}
}

func TestRender_EnumMemberNamesAreVerbatim(t *testing.T) {
	// Enum constants concatenate EnumGo + MemberName with NO identifier
	// transformation: underscores, digits, and casing survive as declared.
	// Consumers porting legacy SCREAMING_SNAKE const sets rely on this.
	enums := []schema.EnumDesc{
		{Name: "AnimPawnPart", Members: []schema.EnumMemberDesc{
			{Name: "_FRONT_HAIR_1", Value: 3},
			{Name: "BACK_HAIR_2", Value: 4},
		}},
	}
	out, err := Render(nil, Options{Package: "p", Enums: enums})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	got := string(out)
	frontHair := regexp.MustCompile(`AnimPawnPart_FRONT_HAIR_1\s+AnimPawnPart\s+=\s+3`)
	backHair := regexp.MustCompile(`AnimPawnPartBACK_HAIR_2\s+AnimPawnPart\s+=\s+4`)
	if !frontHair.MatchString(got) {
		t.Fatalf("expected verbatim AnimPawnPart_FRONT_HAIR_1:\n%s", got)
	}
	if !backHair.MatchString(got) {
		t.Fatalf("expected verbatim AnimPawnPartBACK_HAIR_2:\n%s", got)
	}
}

func TestParseEnumsMemberNamesPreservedVerbatim(t *testing.T) {
	src, err := script.ParseEnums("enum E { _A_1 = 1, B_2 = 2 }")
	if err != nil {
		t.Fatalf("ParseEnums: %v", err)
	}
	if src[0].Members[0].Name != "_A_1" || src[0].Members[1].Name != "B_2" {
		t.Fatalf("member names not verbatim: %+v", src[0].Members)
	}
}

func TestParseToRender_EnumPipeline(t *testing.T) {
	src := `
enum Color { Red = 1, Green, Blue }

@component
@schema(320)
struct Pixel {
  color: Color
}
`
	objs, err := script.ParseObjects(src)
	if err != nil {
		t.Fatalf("ParseObjects: %v", err)
	}
	enums, err := script.ParseEnums(src)
	if err != nil {
		t.Fatalf("ParseEnums: %v", err)
	}
	if len(enums) != 1 || len(enums[0].Members) != 3 {
		t.Fatalf("unexpected enums: %+v", enums)
	}

	out, err := Render(objs, Options{Package: "p", Enums: enums, EmitComponents: true})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	got := string(out)
	if !strings.Contains(got, "type Color int") {
		t.Fatalf("expected enum type:\n%s", got)
	}
	if !strings.Contains(got, "PixelC = runtime.NewComponent[Pixel](\"Pixel\")") {
		t.Fatalf("expected component descriptor:\n%s", got)
	}
	colorField := regexp.MustCompile(`Color\s+Color\s+` + "`" + `json:"color"` + "`")
	if !colorField.MatchString(got) {
		t.Fatalf("expected Color-typed field:\n%s", got)
	}
}
