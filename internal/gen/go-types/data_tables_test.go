package gotypes

import (
	"go/parser"
	"go/token"
	"regexp"
	"strings"
	"testing"

	"github.com/qomos-w/spore/schema"
)

func strField(name string, optional bool) schema.FieldDesc {
	return schema.FieldDesc{Name: name, Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}, Optional: optional}
}

func animalTypeObj() schema.ObjectDesc {
	return schema.ObjectDesc{
		Kind:   schema.TypeKindStruct,
		Name:   "AnimalType",
		IsData: true,
		Fields: []schema.FieldDesc{
			{Name: "id", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}, IsKey: true},
			strField("name", false),
			{Name: "biomes", Type: schema.TypeDesc{Kind: schema.TypeKindArray, Element: &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}}},
		},
	}
}

func TestRender_DataStructIsPlain(t *testing.T) {
	out, err := Render([]schema.ObjectDesc{animalTypeObj()}, Options{Package: "gamedata"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	got := string(out)
	if !strings.Contains(got, "type AnimalType struct {") {
		t.Fatalf("missing struct declaration:\n%s", got)
	}
	for _, banned := range []string{"spore/runtime", "spore/schema", "NewComponent", "SchemaID", "RegisterStructType"} {
		if strings.Contains(got, banned) {
			t.Errorf("plain @data render must not contain %q:\n%s", banned, got)
		}
	}
}

func TestRender_DataStructWithEmitComponentsStillPlain(t *testing.T) {
	// --emit-components must only affect @component structs; @data stays plain
	// even when the flag is on.
	out, err := Render([]schema.ObjectDesc{animalTypeObj()}, Options{Package: "gamedata", EmitComponents: true})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.Contains(string(out), "NewComponent") || strings.Contains(string(out), "spore/runtime") {
		t.Fatalf("@data must not emit component artifacts:\n%s", out)
	}
}

func TestAssignSequentialSchemaIDs_SkipsData(t *testing.T) {
	objs := []schema.ObjectDesc{
		{Kind: schema.TypeKindStruct, Name: "Component"},
		animalTypeObj(),
	}
	AssignSequentialSchemaIDs(objs, "tables._9201.spore")
	if objs[0].SchemaID != 9201 {
		t.Errorf("plain struct should receive 9201, got %d", objs[0].SchemaID)
	}
	if objs[1].SchemaID != 0 {
		t.Errorf("@data struct must stay ID-less, got %d", objs[1].SchemaID)
	}
}

func TestValidateDataTables(t *testing.T) {
	enumNames := map[string]bool{"Biome": true}
	str := func(n string) schema.FieldDesc { return strField(n, false) }
	key := func(name, scalar string) schema.FieldDesc {
		return schema.FieldDesc{Name: name, Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: scalar}, IsKey: true}
	}
	enumKey := func(name string) schema.FieldDesc {
		return schema.FieldDesc{Name: name, Type: schema.TypeDesc{Kind: schema.TypeKindEnum, Name: "Biome"}, IsKey: true}
	}
	arrStr := func(name string) schema.FieldDesc {
		return schema.FieldDesc{Name: name, Type: schema.TypeDesc{Kind: schema.TypeKindArray, Element: &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}}}
	}
	ref := func(f schema.FieldDesc, target, field string) schema.FieldDesc {
		f.Ref = &schema.FieldRef{Target: target, Field: field}
		return f
	}

	cases := []struct {
		name    string
		objs    []schema.ObjectDesc
		wantErr string
	}{
		{
			name: "valid string and enum keys with refs",
			objs: []schema.ObjectDesc{
				{Kind: schema.TypeKindStruct, Name: "AnimalType", IsData: true, Fields: []schema.FieldDesc{key("id", "string"), str("name")}},
				{Kind: schema.TypeKindStruct, Name: "EnumTable", IsData: true, Fields: []schema.FieldDesc{enumKey("kind")}},
				{Kind: schema.TypeKindStruct, Name: "Biome", IsData: true, Fields: []schema.FieldDesc{key("id", "string"), ref(arrStr("animals"), "AnimalType", "")}},
			},
		},
		{
			name:    "no key",
			objs:    []schema.ObjectDesc{{Kind: schema.TypeKindStruct, Name: "T", IsData: true, Fields: []schema.FieldDesc{str("id")}}},
			wantErr: "has no key field",
		},
		{
			name: "two keys",
			objs: []schema.ObjectDesc{{Kind: schema.TypeKindStruct, Name: "T", IsData: true, Fields: []schema.FieldDesc{
				{Name: "a", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}, IsKey: true},
				{Name: "b", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}, IsKey: true},
			}}},
			wantErr: "multiple key fields",
		},
		{
			name:    "optional key",
			objs:    []schema.ObjectDesc{{Kind: schema.TypeKindStruct, Name: "T", IsData: true, Fields: []schema.FieldDesc{{Name: "id", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}, IsKey: true, Optional: true}}}},
			wantErr: "cannot be optional",
		},
		{
			name:    "bad key type float",
			objs:    []schema.ObjectDesc{{Kind: schema.TypeKindStruct, Name: "T", IsData: true, Fields: []schema.FieldDesc{key("id", "float")}}},
			wantErr: "must be string, an integer scalar, or an enum type",
		},
		{
			name:    "unknown enum key",
			objs:    []schema.ObjectDesc{{Kind: schema.TypeKindStruct, Name: "T", IsData: true, Fields: []schema.FieldDesc{{Name: "id", Type: schema.TypeDesc{Kind: schema.TypeKindEnum, Name: "Missing"}, IsKey: true}}}},
			wantErr: "must be string, an integer scalar, or an enum type",
		},
		{
			name:    "schema id on data",
			objs:    []schema.ObjectDesc{{Kind: schema.TypeKindStruct, Name: "T", IsData: true, SchemaID: 9201, Fields: []schema.FieldDesc{key("id", "string")}}},
			wantErr: "must not carry a schema ID",
		},
		{
			name:    "key outside data",
			objs:    []schema.ObjectDesc{{Kind: schema.TypeKindStruct, Name: "T", Fields: []schema.FieldDesc{key("id", "string")}}},
			wantErr: "only valid inside a @data struct",
		},
		{
			name:    "ref outside data",
			objs:    []schema.ObjectDesc{{Kind: schema.TypeKindStruct, Name: "T", Fields: []schema.FieldDesc{ref(str("x"), "U", "")}}},
			wantErr: "only valid inside a @data struct",
		},
		{
			name:    "ref target not data",
			objs: []schema.ObjectDesc{
				{Kind: schema.TypeKindStruct, Name: "T", IsData: true, Fields: []schema.FieldDesc{key("id", "string"), ref(str("x"), "U", "")}},
				{Kind: schema.TypeKindStruct, Name: "U"},
			},
			wantErr: "is not a @data struct",
		},
		{
			name:    "ref target not string-keyed",
			objs: []schema.ObjectDesc{
				{Kind: schema.TypeKindStruct, Name: "T", IsData: true, Fields: []schema.FieldDesc{key("id", "string"), ref(str("x"), "U", "")}},
				{Kind: schema.TypeKindStruct, Name: "U", IsData: true, Fields: []schema.FieldDesc{key("id", "int")}},
			},
			wantErr: "must be string-keyed",
		},
		{
			name:    "ref field wrong type",
			objs: []schema.ObjectDesc{
				{Kind: schema.TypeKindStruct, Name: "T", IsData: true, Fields: []schema.FieldDesc{key("id", "string"), {Name: "x", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}, Ref: &schema.FieldRef{Target: "U"}}}},
				{Kind: schema.TypeKindStruct, Name: "U", IsData: true, Fields: []schema.FieldDesc{key("id", "string")}},
			},
			wantErr: "must be string or array<string>",
		},
		{
			name:    "ref explicit field missing on target",
			objs: []schema.ObjectDesc{
				{Kind: schema.TypeKindStruct, Name: "T", IsData: true, Fields: []schema.FieldDesc{key("id", "string"), ref(str("x"), "U", "nope")}},
				{Kind: schema.TypeKindStruct, Name: "U", IsData: true, Fields: []schema.FieldDesc{key("id", "string")}},
			},
			wantErr: "has no field \"nope\"",
		},
		{
			name: "optional ref allowed",
			objs: []schema.ObjectDesc{
				{Kind: schema.TypeKindStruct, Name: "T", IsData: true, Fields: []schema.FieldDesc{key("id", "string"), {Name: "x", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}, Optional: true, Ref: &schema.FieldRef{Target: "U"}}}},
				{Kind: schema.TypeKindStruct, Name: "U", IsData: true, Fields: []schema.FieldDesc{key("id", "string")}},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateDataTables(tc.objs, enumNames)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected valid, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestRenderDataTables(t *testing.T) {
	objs := []schema.ObjectDesc{
		{
			Kind: schema.TypeKindStruct, Name: "AnimalType", IsData: true, DataVersion: 2,
			Fields: []schema.FieldDesc{
				{Name: "id", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}, IsKey: true},
				strField("name", false),
			},
		},
		{
			Kind: schema.TypeKindStruct, Name: "Biome", IsData: true,
			Fields: []schema.FieldDesc{
				{Name: "id", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}, IsKey: true},
				{Name: "animals", Type: schema.TypeDesc{Kind: schema.TypeKindArray, Element: &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}}, Ref: &schema.FieldRef{Target: "AnimalType"}},
				{Name: "boss", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}, Optional: true, Ref: &schema.FieldRef{Target: "AnimalType", Field: "name"}},
			},
		},
	}
	if err := ValidateDataTables(objs, nil); err != nil {
		t.Fatalf("ValidateDataTables: %v", err)
	}
	tables, refs, err := BuildDataTables(objs, Options{})
	if err != nil {
		t.Fatalf("BuildDataTables: %v", err)
	}
	if len(tables) != 2 || len(refs) != 2 {
		t.Fatalf("expected 2 tables and 2 refs, got %d/%d", len(tables), len(refs))
	}
	out, err := RenderDataTables(tables, refs, Options{Package: "gamedata"})
	if err != nil {
		t.Fatalf("RenderDataTables: %v", err)
	}
	got := string(out)

	for _, want := range []string{
		"type DataTable struct {",
		"var DataTables = []DataTable{",
		`{Name: "AnimalType", KeyField: "id", KeyType: reflect.TypeOf(""), RowType: reflect.TypeOf(AnimalType{}), Version: 2},`,
		"type RefShape struct {",
		`{Table: "Biome", Field: "animals", Target: "AnimalType", TargetField: "id", IsArray: true, Optional: false},`,
		`{Table: "Biome", Field: "boss", Target: "AnimalType", TargetField: "name", IsArray: false, Optional: true},`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("manifest missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "qomos-w/spore") {
		t.Errorf("manifest must stay spore-free:\n%s", got)
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "datatables.gen.go", got, 0); err != nil {
		t.Fatalf("manifest must parse as Go: %v", err)
	}
}

func TestRenderDataTables_IntegerKey(t *testing.T) {
	objs := []schema.ObjectDesc{{
		Kind: schema.TypeKindStruct, Name: "EnumTable", IsData: true,
		Fields: []schema.FieldDesc{{Name: "kind", Type: schema.TypeDesc{Kind: schema.TypeKindEnum, Name: "Biome"}, IsKey: true}},
	}}
	tables, refs, err := BuildDataTables(objs, Options{})
	if err != nil {
		t.Fatalf("BuildDataTables: %v", err)
	}
	if len(refs) != 0 {
		t.Fatalf("expected no refs, got %d", len(refs))
	}
	out, err := RenderDataTables(tables, refs, Options{Package: "gamedata"})
	if err != nil {
		t.Fatalf("RenderDataTables: %v", err)
	}
	if !strings.Contains(string(out), "KeyType: reflect.TypeOf(Biome(0))") {
		t.Fatalf("enum key must render as Biome(0):\n%s", out)
	}
}

func TestRenderDataTables_EnumImportQualifiesKey(t *testing.T) {
	objs := []schema.ObjectDesc{{
		Kind: schema.TypeKindStruct, Name: "EnumTable", IsData: true,
		Fields: []schema.FieldDesc{{Name: "kind", Type: schema.TypeDesc{Kind: schema.TypeKindEnum, Name: "Biome"}, IsKey: true}},
	}}
	opts := Options{Package: "gamedata", EnumImports: []EnumImport{
		{ImportPath: "example.com/barcraft/src/code/model/world", Enums: []string{"Biome"}},
	}}
	tables, _, err := BuildDataTables(objs, opts)
	if err != nil {
		t.Fatalf("BuildDataTables: %v", err)
	}
	if tables[0].KeyType != "world.Biome" {
		t.Fatalf("expected qualified key, got %q", tables[0].KeyType)
	}
	out, err := RenderDataTables(tables, nil, opts)
	if err != nil {
		t.Fatalf("RenderDataTables: %v", err)
	}
	got := string(out)
	if !strings.Contains(got, "KeyType: reflect.TypeOf(world.Biome(0))") {
		t.Fatalf("qualified key missing:\n%s", got)
	}
	if !strings.Contains(got, `"example.com/barcraft/src/code/model/world"`) {
		t.Fatalf("external import missing:\n%s", got)
	}
}

func TestRender_EnumImportQualifiesFields(t *testing.T) {
	objs := []schema.ObjectDesc{{
		Kind: schema.TypeKindStruct, Name: "Biome", IsData: true,
		Fields: []schema.FieldDesc{
			{Name: "id", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}, IsKey: true},
			{Name: "kind", Type: schema.TypeDesc{Kind: schema.TypeKindEnum, Name: "BiomeKind"}},
			{Name: "kinds", Type: schema.TypeDesc{Kind: schema.TypeKindArray, Element: &schema.TypeDesc{Kind: schema.TypeKindEnum, Name: "BiomeKind"}}},
		},
	}}
	opts := Options{Package: "gamedata", EnumImports: []EnumImport{
		{ImportPath: "example.com/barcraft/src/code/model/world", Enums: []string{"BiomeKind"}},
	}}
	out, err := Render(objs, opts)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	got := string(out)
	kindField := regexp.MustCompile(`Kind\s+world\.BiomeKind\s+`)
	kindsField := regexp.MustCompile(`Kinds\s+\[\]world\.BiomeKind\s+`)
	if !kindField.MatchString(got) || !kindsField.MatchString(got) {
		t.Fatalf("qualified enum fields missing:\n%s", got)
	}
	if !strings.Contains(got, `"example.com/barcraft/src/code/model/world"`) {
		t.Fatalf("external import missing:\n%s", got)
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "biome.gen.go", got, 0); err != nil {
		t.Fatalf("output must parse as Go: %v", err)
	}
}

func TestRender_EnumImportUnusedEmitsNoImport(t *testing.T) {
	objs := []schema.ObjectDesc{animalTypeObj()}
	opts := Options{Package: "gamedata", EnumImports: []EnumImport{
		{ImportPath: "example.com/world", Enums: []string{"Biome"}},
	}}
	out, err := Render(objs, opts)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.Contains(string(out), "example.com/world") {
		t.Fatalf("unused enum import must not be emitted:\n%s", out)
	}
}

func TestRenderDataTables_EmptyTablesRejected(t *testing.T) {
	if _, err := RenderDataTables(nil, nil, Options{Package: "p"}); err == nil {
		t.Fatal("expected error for zero tables")
	}
}
