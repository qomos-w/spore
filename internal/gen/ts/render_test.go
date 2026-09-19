package ts

import (
	"strings"
	"testing"

	"github.com/qomos-w/spore/schema"
)

func TestRenderScalar(t *testing.T) {
	cases := map[string]string{
		"bool":    "boolean",
		"int":     "number",
		"int64":   "number",
		"uint32":  "number",
		"float64": "number",
		"string":  "string",
		"bytes":   "Uint8Array",
		"any":     "unknown",
		"null":    "null",
		"":        "unknown",
		"weird":   "weird",
	}
	for in, want := range cases {
		got := renderScalar(in)
		if got != want {
			t.Errorf("renderScalar(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRenderType_Array(t *testing.T) {
	td := schema.TypeDesc{
		Kind: schema.TypeKindArray,
		Element: &schema.TypeDesc{
			Kind: schema.TypeKindScalar,
			Name: "string",
		},
	}
	got, err := renderType(td)
	if err != nil {
		t.Fatal(err)
	}
	if got != "string[]" {
		t.Errorf("got %q, want %q", got, "string[]")
	}
}

func TestRenderType_NestedArray(t *testing.T) {
	td := schema.TypeDesc{
		Kind: schema.TypeKindArray,
		Element: &schema.TypeDesc{
			Kind: schema.TypeKindArray,
			Element: &schema.TypeDesc{
				Kind: schema.TypeKindScalar,
				Name: "int",
			},
		},
	}
	got, err := renderType(td)
	if err != nil {
		t.Fatal(err)
	}
	if got != "number[][]" {
		t.Errorf("got %q, want %q", got, "number[][]")
	}
}

func TestRenderType_Map(t *testing.T) {
	td := schema.TypeDesc{
		Kind: schema.TypeKindMap,
		Key:  &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"},
		Value: &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"},
	}
	got, err := renderType(td)
	if err != nil {
		t.Fatal(err)
	}
	if got != "Record<string, number>" {
		t.Errorf("got %q, want %q", got, "Record<string, number>")
	}
}

func TestRenderType_StructReference(t *testing.T) {
	td := schema.TypeDesc{
		Kind:      schema.TypeKindStruct,
		Name:      "LoginReq",
		ClassName: "LoginReq",
	}
	got, err := renderType(td)
	if err != nil {
		t.Fatal(err)
	}
	if got != "LoginReq" {
		t.Errorf("got %q, want %q", got, "LoginReq")
	}
}

func TestRenderInterface_Simple(t *testing.T) {
	obj := schema.ObjectDesc{
		Kind: schema.TypeKindStruct,
		Name: "LoginReq",
		Fields: []schema.FieldDesc{
			{Name: "User", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
			{Name: "Pass", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
		},
	}
	got, err := renderInterface(obj)
	if err != nil {
		t.Fatal(err)
	}
	want := "export interface LoginReq {\n  User: string;\n  Pass: string;\n}\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderInterface_PrivateFieldDropped(t *testing.T) {
	obj := schema.ObjectDesc{
		Kind: schema.TypeKindStruct,
		Name: "S",
		Fields: []schema.FieldDesc{
			{Name: "Public", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}},
			{Name: "Private", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}, Private: true},
		},
	}
	got, err := renderInterface(obj)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "Private") {
		t.Errorf("private field leaked into output:\n%s", got)
	}
	if !strings.Contains(got, "Public:") {
		t.Errorf("public field missing:\n%s", got)
	}
}

func TestRenderInterface_EmptyName(t *testing.T) {
	obj := schema.ObjectDesc{Kind: schema.TypeKindStruct}
	if _, err := renderInterface(obj); err == nil {
		t.Errorf("expected error on empty Name")
	}
}

func TestRenderRegistry(t *testing.T) {
	entries := []NamedObjectDesc{
		{
			Namespace:  "auth",
			SchemaID:   2,
			Name:       "LoginResp",
			Visibility: VisibilityPublic,
			Object: schema.ObjectDesc{
				Kind: schema.TypeKindStruct,
				Name: "LoginResp",
				Fields: []schema.FieldDesc{{Name: "Token", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}}},
			},
		},
		{
			Namespace:  "auth",
			SchemaID:   1,
			Name:       "LoginReq",
			Visibility: VisibilityPublic,
			Object: schema.ObjectDesc{
				Kind: schema.TypeKindStruct,
				Name: "LoginReq",
				Fields: []schema.FieldDesc{{Name: "User", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}}},
			},
		},
	}
	got := renderRegistry("auth", entries)
	idx1 := strings.Index(got, "LoginReq: 1")
	idx2 := strings.Index(got, "LoginResp: 2")
	if idx1 < 0 || idx2 < 0 || idx1 > idx2 {
		t.Errorf("entries not sorted by SchemaID:\n%s", got)
	}
	for _, needle := range []string{
		`import { SchemaRegistry, type SchemaEntry } from "@qomos/spore-ts/registry";`,
		`export const schemaEntries: SchemaEntry[] = [`,
		`namespace: "auth"`,
		`schemaId: 1`,
		`visibility: "public"`,
		`className: "LoginReq"`,
		`fields: [`,
		`export function buildSchemaRegistry(): SchemaRegistry {`,
	} {
		if !strings.Contains(got, needle) {
			t.Errorf("missing %q in registry output:\n%s", needle, got)
		}
	}
}
