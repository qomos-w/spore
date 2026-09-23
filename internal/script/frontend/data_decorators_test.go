package frontend

import (
	"strings"
	"testing"

	"github.com/qomos-w/spore/schema"
)

// parseErr parses source and returns the parse error, or nil when parsing
// succeeded.
func parseErr(t *testing.T, source string) error {
	t.Helper()
	l := newLexer(source)
	p := newParser(l, source)
	_, err := p.parse()
	return err
}

func TestParser_DataDecorators(t *testing.T) {
	src := `
@data
@version(2)
struct AnimalType {
  key id: string
  name: string
  biomes: array<string>
}
`
	prog := parseProg(t, src)
	st, ok := prog.Stmts[0].(*structStmt)
	if !ok {
		t.Fatalf("expected structStmt, got %T", prog.Stmts[0])
	}
	if !st.IsData {
		t.Error("expected IsData=true")
	}
	if st.IsComponent {
		t.Error("expected IsComponent=false")
	}
	if st.DataVersion != 2 {
		t.Errorf("expected DataVersion=2, got %d", st.DataVersion)
	}
	if st.SchemaID != 0 {
		t.Errorf("expected SchemaID=0, got %d", st.SchemaID)
	}
	if len(st.Fields) != 3 {
		t.Fatalf("expected 3 fields, got %d", len(st.Fields))
	}
	if !st.Fields[0].Key {
		t.Error("expected first field to be the key")
	}
	if st.Fields[1].Key || st.Fields[2].Key {
		t.Error("non-key fields must not be marked Key")
	}
}

func TestParser_DataVersionZeroMeansUnversioned(t *testing.T) {
	prog := parseProg(t, "@data\nstruct T {\n  key id: int\n}\n")
	st := prog.Stmts[0].(*structStmt)
	if !st.IsData || st.DataVersion != 0 {
		t.Errorf("expected IsData with DataVersion 0, got %v/%d", st.IsData, st.DataVersion)
	}
}

func TestParser_DataWithComponentRejected(t *testing.T) {
	err := parseErr(t, "@data\n@component\n@schema(300)\nstruct T {\n  key id: string\n}\n")
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("expected mutual-exclusion error, got %v", err)
	}
}

func TestParser_DataWithSchemaRejected(t *testing.T) {
	err := parseErr(t, "@data\n@schema(9201)\nstruct T {\n  key id: string\n}\n")
	if err == nil || !strings.Contains(err.Error(), "must not carry @schema(N)") {
		t.Fatalf("expected @schema rejection, got %v", err)
	}
}

func TestParser_VersionWithoutDataRejected(t *testing.T) {
	err := parseErr(t, "@version(2)\nstruct T {\n  id: string\n}\n")
	if err == nil || !strings.Contains(err.Error(), "@version is only valid on a @data struct") {
		t.Fatalf("expected @version misuse error, got %v", err)
	}
}

func TestParser_VersionAfterDataAccepted(t *testing.T) {
	// Decorator order must not matter: @version before @data is legal.
	prog := parseProg(t, "@version(3)\n@data\nstruct T {\n  key id: string\n}\n")
	st := prog.Stmts[0].(*structStmt)
	if !st.IsData || st.DataVersion != 3 {
		t.Fatalf("expected @data with version 3, got %v/%d", st.IsData, st.DataVersion)
	}
}

func TestParser_DuplicateDataAndVersionRejected(t *testing.T) {
	if err := parseErr(t, "@data\n@data\nstruct T {\n  key id: string\n}\n"); err == nil || !strings.Contains(err.Error(), "duplicate @data") {
		t.Fatalf("expected duplicate @data error, got %v", err)
	}
	if err := parseErr(t, "@data\n@version(1)\n@version(2)\nstruct T {\n  key id: string\n}\n"); err == nil || !strings.Contains(err.Error(), "duplicate @version") {
		t.Fatalf("expected duplicate @version error, got %v", err)
	}
}

func TestParser_DataWithArgumentsRejected(t *testing.T) {
	err := parseErr(t, "@data(9201)\nstruct T {\n  key id: string\n}\n")
	if err == nil || !strings.Contains(err.Error(), "@data takes no arguments") {
		t.Fatalf("expected @data argument rejection, got %v", err)
	}
	// The struct after the resync must still have parsed.
	if err := parseErr(t, "@data\nstruct T {\n  key id: string\n}\n"); err != nil {
		t.Fatalf("plain @data must parse, got %v", err)
	}
}

func TestParser_FieldNamedKey(t *testing.T) {
	// `key: T` (nothing before the colon) is a field literally named "key".
	prog := parseProg(t, "struct S {\n  key: string\n}\n")
	st := prog.Stmts[0].(*structStmt)
	if len(st.Fields) != 1 || st.Fields[0].Name.Value != "key" {
		t.Fatalf("expected one field named key, got %+v", st.Fields)
	}
	if st.Fields[0].Key {
		t.Error("field named 'key' must not itself be marked Key")
	}
}

func TestParser_RefDecorators(t *testing.T) {
	src := `
@data
struct Biome {
  key id: string
  @ref(AnimalType) predator: string
  @ref(CreatureType.habitat) spawns: array<string>
  @ref(AnimalType) optional prey: string
}
`
	prog := parseProg(t, src)
	st := prog.Stmts[0].(*structStmt)
	if len(st.Fields) != 4 {
		t.Fatalf("expected 4 fields, got %d", len(st.Fields))
	}
	if st.Fields[1].Ref == nil || st.Fields[1].Ref.Target != "AnimalType" || st.Fields[1].Ref.Field != "" {
		t.Errorf("expected @ref(AnimalType), got %+v", st.Fields[1].Ref)
	}
	if st.Fields[2].Ref == nil || st.Fields[2].Ref.Target != "CreatureType" || st.Fields[2].Ref.Field != "habitat" {
		t.Errorf("expected @ref(CreatureType.habitat), got %+v", st.Fields[2].Ref)
	}
	if !st.Fields[3].Optional || st.Fields[3].Ref == nil {
		t.Errorf("expected optional field with @ref, got optional=%v ref=%+v", st.Fields[3].Optional, st.Fields[3].Ref)
	}
}

func TestParser_RefErrors(t *testing.T) {
	if err := parseErr(t, "struct S {\n  @ref(3) x: string\n}\n"); err == nil {
		t.Error("@ref with non-ident argument must fail")
	}
	if err := parseErr(t, "struct S {\n  @bogus(3) x: string\n}\n"); err == nil || !strings.Contains(err.Error(), "expected ref annotation") {
		t.Errorf("expected unknown field annotation rejection, got %v", err)
	}
	if err := parseErr(t, "struct S {\n  @ref(A) @ref(B) x: string\n}\n"); err == nil || !strings.Contains(err.Error(), "duplicate @ref") {
		t.Errorf("expected duplicate @ref rejection, got %v", err)
	}
}

func TestParseAllObjectDescs_DataMapping(t *testing.T) {
	src := `
@data
@version(7)
struct AnimalType {
  key id: string
  @ref(Biome) biomes: array<string>
}

struct Plain {
  id: string
}
`
	objs, err := ParseAllObjectDescs(src)
	if err != nil {
		t.Fatalf("ParseAllObjectDescs: %v", err)
	}
	if len(objs) != 2 {
		t.Fatalf("expected 2 objects, got %d", len(objs))
	}
	d := objs[0]
	if !d.IsData || d.DataVersion != 7 || d.IsComponent || d.SchemaID != 0 {
		t.Errorf("unexpected @data mapping: %+v", d)
	}
	if !d.Fields[0].IsKey {
		t.Error("expected key field mapping")
	}
	if d.Fields[1].Ref == nil || d.Fields[1].Ref.Target != "Biome" {
		t.Errorf("expected ref mapping, got %+v", d.Fields[1].Ref)
	}
	if objs[1].IsData {
		t.Error("plain struct must not be @data")
	}
}

func TestParseAllObjectDescsWithEnums_ExternalEnumKind(t *testing.T) {
	src := `
@data
struct EnumTable {
  key kind: ExternalKind
}
`
	// Without the external seed an uppercase unknown ident resolves to the
	// TypeKindClass fallback; with it the key is a proper enum reference.
	objs, err := ParseAllObjectDescsWithEnums(src, []string{"ExternalKind"})
	if err != nil {
		t.Fatalf("ParseAllObjectDescsWithEnums: %v", err)
	}
	key := objs[0].Fields[0]
	if key.Type.Kind != schema.TypeKindEnum || key.Type.Name != "ExternalKind" {
		t.Errorf("expected TypeKindEnum ExternalKind key, got %+v", key.Type)
	}

	objs, err = ParseAllObjectDescs(src)
	if err != nil {
		t.Fatalf("ParseAllObjectDescs: %v", err)
	}
	key = objs[0].Fields[0]
	if key.Type.Kind == schema.TypeKindEnum {
		t.Errorf("without the external seed the kind must not be enum, got %+v", key.Type)
	}
}
