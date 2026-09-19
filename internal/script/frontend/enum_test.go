package frontend

import (
	"strings"
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/schema"
)

func TestEnum_Parser_BasicDeclaration(t *testing.T) {
	prog, err := parseModule("enum Color { Red, Green, Blue }")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(prog.Stmts) != 1 {
		t.Fatalf("expected 1 statement, got %d", len(prog.Stmts))
	}
	s, ok := prog.Stmts[0].(*enumStmt)
	if !ok {
		t.Fatalf("expected *enumStmt, got %T", prog.Stmts[0])
	}
	if s.Name.Value != "Color" || s.Exported {
		t.Fatalf("unexpected enum decl: name=%q exported=%v", s.Name.Value, s.Exported)
	}
	if len(s.Members) != 3 {
		t.Fatalf("expected 3 members, got %d", len(s.Members))
	}
	for i, want := range []string{"Red", "Green", "Blue"} {
		if s.Members[i].Name.Value != want {
			t.Fatalf("member %d: expected %q, got %q", i, want, s.Members[i].Name.Value)
		}
		if s.Members[i].HasValue {
			t.Fatalf("member %q should not have explicit value", want)
		}
	}
}

func TestEnum_Parser_ExplicitValues(t *testing.T) {
	prog, err := parseModule("enum Status { Pending = 0, Active = 1, Closed = 2 }")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	s := prog.Stmts[0].(*enumStmt)
	wantValues := []int64{0, 1, 2}
	for i, m := range s.Members {
		if !m.HasValue || m.Value != wantValues[i] {
			t.Fatalf("member %d: expected HasValue=%v value=%d, got HasValue=%v value=%d", i, true, wantValues[i], m.HasValue, m.Value)
		}
	}
}

func TestEnum_Parser_Exported(t *testing.T) {
	prog, err := parseModule("export enum Mode { Off, On }")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	s := prog.Stmts[0].(*enumStmt)
	if !s.Exported {
		t.Fatal("expected Exported=true")
	}
}

func TestEnum_Parser_DuplicateMemberRejected(t *testing.T) {
	_, err := parseModule("enum Bad { A, B, A }")
	if err == nil {
		t.Fatal("expected duplicate member parse error")
	}
	if !strings.Contains(err.Error(), "duplicate enum member") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnum_Parser_NonIntValueRejected(t *testing.T) {
	_, err := parseModule("enum Bad { A = \"x\" }")
	if err == nil {
		t.Fatal("expected parse error for non-int enum value")
	}
	if !strings.Contains(err.Error(), "expected integer enum value") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnum_Parser_MissingBraceRejected(t *testing.T) {
	_, err := parseModule("enum Bad { A, B")
	if err == nil {
		t.Fatal("expected parse error for missing closing brace")
	}
}

func TestEnum_Declarations_ExportedWithResolvedValues(t *testing.T) {
	f, err := New(mustNewScriptBinding(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := f.LoadSource(`
export enum Status { Pending = 10, Active, Closed = 40 }
enum Hidden { A, B }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	compiled := f.CompiledDeclarations()
	all := compiled.EnumDeclarations()
	if len(all) != 2 {
		t.Fatalf("expected 2 total enum declarations, got %d (%+v)", len(all), all)
	}
	var status schema.EnumDesc
	for _, e := range all {
		if e.Name == "Status" {
			status = e
		}
	}
	if status.Name != "Status" {
		t.Fatal("Status not found in enum declarations")
	}
	want := []struct {
		name  string
		value int
	}{{"Pending", 10}, {"Active", 11}, {"Closed", 40}}
	if len(status.Members) != len(want) {
		t.Fatalf("expected %d members, got %d", len(want), len(status.Members))
	}
	for i, w := range want {
		m := status.Members[i]
		if m.Name != w.name || m.Value != w.value {
			t.Fatalf("member %d: expected %q=%d, got %q=%d", i, w.name, w.value, m.Name, m.Value)
		}
	}
}

func TestEnum_TypeAnnotationsResolveToEnumKind(t *testing.T) {
	f, err := New(mustNewScriptBinding(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := f.LoadSource(`
enum Color { Red, Green, Blue }

struct Pixel { color: Color }

export fun tint(c: Color): Color { return c }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	decls := f.Declarations()
	var pixel schema.ObjectDesc
	for _, obj := range decls.Objects {
		if obj.Name == "Pixel" {
			pixel = obj
		}
	}
	if pixel.Name != "Pixel" {
		t.Fatal("Pixel not found in objects")
	}
	if len(pixel.Fields) != 1 {
		t.Fatalf("expected 1 field, got %d", len(pixel.Fields))
	}
	if td := pixel.Fields[0].Type; td.Kind != schema.TypeKindEnum || td.Name != "Color" {
		t.Fatalf("expected enum Color field type, got %+v", td)
	}
	if len(decls.ExportedCallables) != 1 {
		t.Fatalf("expected 1 exported callable, got %d", len(decls.ExportedCallables))
	}
	c := decls.ExportedCallables[0]
	if len(c.Parameters) != 1 || c.Parameters[0].Type.Kind != schema.TypeKindEnum {
		t.Fatalf("expected enum parameter type, got %+v", c.Parameters)
	}
	if len(c.Returns) != 1 || c.Returns[0].Kind != schema.TypeKindEnum {
		t.Fatalf("expected enum return type, got %+v", c.Returns)
	}
}

func TestEnum_ModuleExportsReflectEnum(t *testing.T) {
	f, err := New(mustNewScriptBinding(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	f.SetModuleResolver(MapModuleResolver{
		"colors": `export enum Color { Red = 1, Green, Blue }
enum Hidden { A, B }`,
	})
	if err := f.LoadSource(`import Color from "colors"`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	exports, ok := f.ModuleExports("colors")
	if !ok {
		t.Fatal("expected colors module exports")
	}
	if len(exports.Enums) != 1 || exports.Enums[0].Name != "Color" {
		t.Fatalf("expected exported enum Color only (Hidden unexported), got %+v", exports.Enums)
	}
	if _, lookupOK := f.ModuleExports("colors"); !lookupOK {
		t.Fatal("module exports lookup failed")
	}
	if len(exports.Enums[0].Members) != 3 || exports.Enums[0].Members[2].Value != 3 {
		t.Fatalf("unexpected members: %+v", exports.Enums[0].Members)
	}
	syntax := exports.SporeSyntax()
	if !strings.Contains(syntax, "enum Color { Red = 1, Green = 2, Blue = 3 }") {
		t.Fatalf("unexpected spore syntax: %q", syntax)
	}
	resolved, ok := f.ExplainRootImport("Color")
	if !ok || resolved.Kind != "enum" {
		t.Fatalf("expected enum root import resolution, got %+v %v", resolved, ok)
	}
	summary, ok := f.ModuleSummary("colors")
	if !ok || summary.ExportCounts.Enums != 1 {
		t.Fatalf("expected enum export count 1, got %+v %v", summary, ok)
	}
}

func TestEnum_ReExportWildcardCarriesEnums(t *testing.T) {
	f, err := New(mustNewScriptBinding(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	f.SetModuleResolver(MapModuleResolver{
		"colors": `export enum Color { Red, Green, Blue }`,
		"util":   `export * from "colors"`,
	})
	if err := f.LoadSource(`import Color from "util"`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	exports, ok := f.ModuleExports("util")
	if !ok {
		t.Fatal("expected util module exports")
	}
	if len(exports.Enums) != 1 || exports.Enums[0].Name != "Color" {
		t.Fatalf("expected re-exported enum Color, got %+v", exports.Enums)
	}
}

func mustNewScriptBinding(t *testing.T) *binding.ScriptBinding {
	t.Helper()
	return binding.NewScriptBinding()
}
