package goserver

import (
	"strings"
	"testing"

	"github.com/qomos-w/spore/internal/gen/ts"
	"github.com/qomos-w/spore/schema"
)

// TestRenderScalarGoType_SharedTableCoverage locks the scalar contract
// go-server now inherits from internal/gen/common. `any` is a canonical Spore
// keyword that the local table used to reject ("unsupported scalar"), and
// uint8 is the one alias the local table was missing.
func TestRenderScalarGoType_SharedTableCoverage(t *testing.T) {
	cases := map[string]string{
		"bool":    "bool",
		"string":  "string",
		"byte":    "int8",
		"int8":    "int8",
		"short":   "int16",
		"int16":   "int16",
		"ushort":  "uint16",
		"uint16":  "uint16",
		"uint8":   "uint8",
		"int":     "int",
		"int32":   "int32",
		"uint":    "uint",
		"uint32":  "uint32",
		"long":    "int64",
		"int64":   "int64",
		"ulong":   "uint64",
		"uint64":  "uint64",
		"float":   "float32",
		"float32": "float32",
		"double":  "float64",
		"float64": "float64",
		"bytes":   "[]byte",
		"any":     "any",
	}
	for name, want := range cases {
		got, err := renderScalarGoType(scalarDesc(name))
		if err != nil {
			t.Errorf("renderScalarGoType(%q): unexpected error %v", name, err)
			continue
		}
		if got != want {
			t.Errorf("renderScalarGoType(%q) = %q, want %q", name, got, want)
		}
	}
}

// TestRenderScalarGoType_RejectsUnsupported keeps the failure path intact:
// null has no Go rendering in the shared table, and non-scalar kinds are still
// rejected by the MVP restriction.
func TestRenderScalarGoType_RejectsUnsupported(t *testing.T) {
	for _, name := range []string{"", "null", "nope"} {
		if _, err := renderScalarGoType(scalarDesc(name)); err == nil {
			t.Errorf("renderScalarGoType(%q): expected error, got nil", name)
		}
	}
	if _, err := renderScalarGoType(schema.TypeDesc{Kind: schema.TypeKindArray}); err == nil {
		t.Errorf("renderScalarGoType(array): expected error, got nil")
	}
}

// TestGenerate_NewScalarCoverage is the positive end-to-end check for the drift
// repair: a request struct with an `any` field (plus byte/int8/short/uint8)
// used to make Generate fail with "unsupported scalar".
func TestGenerate_NewScalarCoverage(t *testing.T) {
	reqObj := schema.ObjectDesc{
		Kind: schema.TypeKindStruct, Name: "Req", SchemaID: 1,
		Fields: []schema.FieldDesc{
			{Name: "v", Type: scalarDesc("any")},
			{Name: "b", Type: scalarDesc("byte")},
			{Name: "i8", Type: scalarDesc("int8")},
			{Name: "sh", Type: scalarDesc("short")},
			{Name: "u8", Type: scalarDesc("uint8")},
		},
	}
	finObj := schema.ObjectDesc{
		Kind: schema.TypeKindStruct, Name: "Final", SchemaID: 2,
		Fields: []schema.FieldDesc{{Name: "ok", Type: scalarDesc("bool")}},
	}
	schemas := []ts.NamedObjectDesc{
		{Namespace: "probe", SchemaID: 1, Name: "Req", Object: reqObj, Visibility: ts.VisibilityPublic},
		{Namespace: "probe", SchemaID: 2, Name: "Final", Object: finObj, Visibility: ts.VisibilityPublic},
	}
	callables := []ts.NamedCallableDesc{{
		Namespace: "probe", Name: "call", Visibility: ts.VisibilityPublic,
		Mode: schema.CallableModeUnary, ReqSchemaID: 1, FinalSchemaID: 2,
		Req:   structDesc("Req"),
		Final: structDesc("Final"),
	}}

	files, err := Generate(schemas, callables, Options{Package: "probe"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	body, ok := files["probe_dispatcher_gen.go"]
	if !ok {
		t.Fatalf("expected probe_dispatcher_gen.go, got %v", fileKeysOf(files))
	}
	wantSnippets := []string{
		`payload["v"].(any)`,
		`payload["b"].(int8)`,
		`payload["i8"].(int8)`,
		`payload["sh"].(int16)`,
		`payload["u8"].(uint8)`,
	}
	for _, w := range wantSnippets {
		if !strings.Contains(body, w) {
			t.Errorf("missing %q in generated dispatcher:\n%s", w, body)
		}
	}
}

// TestGenerate_RejectsUnrenderableScalarField pins that a scalar the shared
// table cannot render still fails generation loudly instead of emitting a
// bogus Go cast.
func TestGenerate_RejectsUnrenderableScalarField(t *testing.T) {
	reqObj := schema.ObjectDesc{
		Kind: schema.TypeKindStruct, Name: "Req", SchemaID: 1,
		Fields: []schema.FieldDesc{{Name: "n", Type: scalarDesc("null")}},
	}
	finObj := schema.ObjectDesc{
		Kind: schema.TypeKindStruct, Name: "Final", SchemaID: 2,
		Fields: []schema.FieldDesc{{Name: "ok", Type: scalarDesc("bool")}},
	}
	schemas := []ts.NamedObjectDesc{
		{Namespace: "probe", SchemaID: 1, Name: "Req", Object: reqObj, Visibility: ts.VisibilityPublic},
		{Namespace: "probe", SchemaID: 2, Name: "Final", Object: finObj, Visibility: ts.VisibilityPublic},
	}
	callables := []ts.NamedCallableDesc{{
		Namespace: "probe", Name: "call", Visibility: ts.VisibilityPublic,
		Mode: schema.CallableModeUnary, ReqSchemaID: 1, FinalSchemaID: 2,
		Req:   structDesc("Req"),
		Final: structDesc("Final"),
	}}
	if _, err := Generate(schemas, callables, Options{Package: "probe"}); err == nil {
		t.Errorf("expected rejection for a null scalar field, got nil")
	}
}

func scalarDesc(name string) schema.TypeDesc {
	return schema.TypeDesc{Kind: schema.TypeKindScalar, Name: name}
}

func structDesc(name string) schema.TypeDesc {
	return schema.TypeDesc{Kind: schema.TypeKindStruct, Name: name, ClassName: name}
}

func fileKeysOf(files map[string]string) []string {
	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	return keys
}
