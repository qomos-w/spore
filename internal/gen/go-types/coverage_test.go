package gotypes

import (
	"strings"
	"testing"

	"github.com/qomos-w/spore/schema"
)

func TestRender_NoSchemaIDBlockWithoutBase(t *testing.T) {
	out, err := Render([]schema.ObjectDesc{
		{Kind: schema.TypeKindStruct, Name: "A", Fields: []schema.FieldDesc{
			{Name: "x", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}},
		}},
	}, Options{Package: "p", SourcePath: "plain.spore"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	s := string(out)
	if strings.Contains(s, "SchemaID") || strings.Contains(s, "reflect") {
		t.Fatalf("expected no schema-id block, got:\n%s", s)
	}
	if !strings.Contains(s, "X int32") {
		t.Fatalf("expected scalar field, got:\n%s", s)
	}
}

func TestRender_SequentialSchemaIDsAndExplicit(t *testing.T) {
	objs := []schema.ObjectDesc{
		{Kind: schema.TypeKindStruct, Name: "First", SchemaID: 999, Fields: []schema.FieldDesc{
			{Name: "x", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}},
		}},
		{Kind: schema.TypeKindStruct, Name: "Second", Fields: []schema.FieldDesc{
			{Name: "y", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}},
		}},
		{Kind: schema.TypeKindClass, Name: "Ignored", SchemaID: 500},
	}
	out, err := Render(objs, Options{Package: "p", SourcePath: "dir/agent.chat._300.spore"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	s := string(out)
	// Explicit 999 preserved; Second gets base 300; class ignored entirely.
	for _, want := range []string{
		"FirstSchemaID",
		"uint64 = 999",
		"SecondSchemaID",
		"uint64 = 300",
		"schema.RegisterStructType(FirstSchemaID, reflect.TypeOf(First{}))",
		"schema.RegisterStructType(SecondSchemaID, reflect.TypeOf(Second{}))",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in:\n%s", want, s)
		}
	}
	if strings.Contains(s, "Ignored") {
		t.Fatalf("class object must be skipped:\n%s", s)
	}
}

func TestRender_StructNamesPointerForNamedField(t *testing.T) {
	objs := []schema.ObjectDesc{
		{Kind: schema.TypeKindStruct, Name: "Holder", Fields: []schema.FieldDesc{
			{Name: "ref", Optional: true, Type: schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "Elsewhere"}},
		}},
	}
	out, err := Render(objs, Options{Package: "p", StructNames: map[string]bool{"Elsewhere": true}})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(string(out), "Ref *Elsewhere") {
		t.Fatalf("cross-file struct name must render pointer:\n%s", out)
	}
}

func TestRender_TypeErrors(t *testing.T) {
	field := func(td schema.TypeDesc) []schema.ObjectDesc {
		return []schema.ObjectDesc{{Kind: schema.TypeKindStruct, Name: "T", Fields: []schema.FieldDesc{{Name: "f", Type: td}}}}
	}
	cases := []struct {
		name string
		td   schema.TypeDesc
		want string
	}{
		{"unknown scalar", schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "quux"}, "unknown scalar"},
		{"array missing element", schema.TypeDesc{Kind: schema.TypeKindArray}, "array element missing"},
		{"map missing value", schema.TypeDesc{Kind: schema.TypeKindMap, Key: &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}}, "map key/value missing"},
		{"map bad key", schema.TypeDesc{Kind: schema.TypeKindMap, Key: &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "quux"}, Value: &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}}, "unknown scalar"},
		{"map bad value", schema.TypeDesc{Kind: schema.TypeKindMap, Key: &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}, Value: &schema.TypeDesc{Kind: schema.TypeKindArray}}, "array element missing"},
		{"named missing name", schema.TypeDesc{Kind: schema.TypeKindStruct}, "named type missing name"},
		{"enum missing name", schema.TypeDesc{Kind: schema.TypeKindEnum}, "enum type missing name"},
		{"unsupported kind", schema.TypeDesc{Kind: schema.TypeKindInvalid}, "unsupported type kind"},
	}
	for _, tc := range cases {
		_, err := Render(field(tc.td), Options{Package: "p"})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: expected %q error, got %v", tc.name, tc.want, err)
		}
		if err != nil && !strings.Contains(err.Error(), `struct "T"`) {
			t.Fatalf("%s: error should name the struct: %v", tc.name, err)
		}
	}
}

func TestAssignSequentialSchemaIDs(t *testing.T) {
	objs := []schema.ObjectDesc{
		{Kind: schema.TypeKindStruct, Name: "A"},
		{Kind: schema.TypeKindStruct, Name: "B", SchemaID: 7},
		{Kind: schema.TypeKindStruct, Name: "C"},
		{Kind: schema.TypeKindClass, Name: "D"},
	}
	AssignSequentialSchemaIDs(objs, "x._20.spore")
	if objs[0].SchemaID != 20 {
		t.Fatalf("A should get base 20, got %d", objs[0].SchemaID)
	}
	if objs[1].SchemaID != 7 {
		t.Fatalf("B must keep explicit 7, got %d", objs[1].SchemaID)
	}
	if objs[2].SchemaID != 21 {
		t.Fatalf("C should get 21, got %d", objs[2].SchemaID)
	}
	if objs[3].SchemaID != 0 {
		t.Fatalf("class must stay 0, got %d", objs[3].SchemaID)
	}

	// No "._N" suffix → no assignment.
	noBase := []schema.ObjectDesc{{Kind: schema.TypeKindStruct, Name: "A"}}
	AssignSequentialSchemaIDs(noBase, "plain.spore")
	if noBase[0].SchemaID != 0 {
		t.Fatalf("no suffix should leave 0, got %d", noBase[0].SchemaID)
	}
}

func TestParseSchemaBaseID(t *testing.T) {
	cases := []struct {
		path string
		want uint64
		ok   bool
	}{
		{"agent.chat._300.spore", 300, true},
		{"/deep/dir/x._7.spore", 7, true},
		{"x._0.spore", 0, true},
		{"x.spore", 0, false},
		{"x._abc.spore", 0, false},
		{"x._.spore", 0, false},
		{"", 0, false},
	}
	for _, tc := range cases {
		got, ok := parseSchemaBaseID(tc.path)
		if got != tc.want || ok != tc.ok {
			t.Fatalf("parseSchemaBaseID(%q) = (%d, %v), want (%d, %v)", tc.path, got, ok, tc.want, tc.ok)
		}
	}
}

func TestRenderRegistry(t *testing.T) {
	if _, err := RenderRegistry(nil, Options{}); err == nil || !strings.Contains(err.Error(), "Package is required") {
		t.Fatalf("expected package error, got %v", err)
	}
	if _, err := RenderRegistry(nil, Options{Package: "p"}); err == nil || !strings.Contains(err.Error(), "at least one entry") {
		t.Fatalf("expected empty-entries error, got %v", err)
	}

	entries := []RegistryEntry{
		{ID: 2, Name: "Beta", SourceFile: "b.spore"},
		{ID: 1, Name: "Alpha", SourceFile: "a.spore"},
	}
	out, err := RenderRegistry(entries, Options{Package: "p", Header: "// gen"})
	if err != nil {
		t.Fatalf("RenderRegistry: %v", err)
	}
	s := string(out)
	for _, want := range []string{
		"// gen",
		"2: \"Beta\"",
		"1: \"Alpha\"",
		"2: reflect.TypeOf(Beta{})",
		"1: reflect.TypeOf(Alpha{})",
		"schema.RegisterStructType(id, typ)",
		"SchemaIDs maps schema ID",
		"SchemaTypes maps schema ID",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in:\n%s", want, s)
		}
	}
}

// TestRenderRegistry_NoRegistryInit covers the dependency-free mode used for
// the distributed plugin SDK: schema IDs and reflect types are still emitted,
// but no spore/schema import and no init() registration block.
func TestRenderRegistry_NoRegistryInit(t *testing.T) {
	entries := []RegistryEntry{
		{ID: 300, Name: "AgentChatSubmitReq", SourceFile: "agent.chat._300.spore"},
	}
	out, err := RenderRegistry(entries, Options{Package: "p", NoRegistryInit: true})
	if err != nil {
		t.Fatalf("RenderRegistry: %v", err)
	}
	s := string(out)
	for _, want := range []string{
		"300: \"AgentChatSubmitReq\"",
		"300: reflect.TypeOf(AgentChatSubmitReq{})",
		"\"reflect\"",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in:\n%s", want, s)
		}
	}
	for _, banned := range []string{"spore/schema", "RegisterStructType", "func init()"} {
		if strings.Contains(s, banned) {
			t.Fatalf("unexpected %q in:\n%s", banned, s)
		}
	}
}

// TestRender_NoRegistryInit: per-file output keeps SchemaID constants and the
// struct itself, drops the spore/schema import and the init() block.
func TestRender_NoRegistryInit(t *testing.T) {
	objs := []schema.ObjectDesc{
		{
			Kind: schema.TypeKindStruct,
			Name: "AgentChatSubmitReq",
			Fields: []schema.FieldDesc{
				{Name: "text", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
			},
		},
	}
	out, err := Render(objs, Options{
		Package:        "agentchat",
		SourcePath:     "schemas/agent.chat._300.spore",
		NoRegistryInit: true,
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	s := string(out)
	for _, want := range []string{
		"AgentChatSubmitReqSchemaID uint64 = 300",
		"type AgentChatSubmitReq struct {",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in:\n%s", want, s)
		}
	}
	for _, banned := range []string{"spore/schema", "RegisterStructType", "func init()", "\"reflect\""} {
		if strings.Contains(s, banned) {
			t.Fatalf("unexpected %q in:\n%s", banned, s)
		}
	}
}

// TestRenderRegistry_RegistryObject: when EmitComponents is on and some entry
// is @component, the registry file carries the single Registry table object
// (ComponentIDs subset + stdlib accessors) with no runtime import.
func TestRenderRegistry_RegistryObject(t *testing.T) {
	entries := []RegistryEntry{
		{ID: 300, Name: "Position", SourceFile: "game.spore", IsComponent: true},
		{ID: 301, Name: "PlainMsg", SourceFile: "game.spore"},
	}
	out, err := RenderRegistry(entries, Options{Package: "p", EmitComponents: true})
	if err != nil {
		t.Fatalf("RenderRegistry: %v", err)
	}
	s := string(out)
	for _, want := range []string{
		"var componentIDs = []uint64{\n\t300,\n}",
		"type registryTable struct {",
		"func (t registryTable) SchemaTypes() map[uint64]reflect.Type",
		"func (t registryTable) SchemaIDs() map[uint64]string",
		"func (t registryTable) ComponentIDs() []uint64",
		"var Registry = registryTable{",
		"schemaTypes:  SchemaTypes,",
		"componentIDs: componentIDs,",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in:\n%s", want, s)
		}
	}
	if strings.Contains(s, "301") == false {
		t.Fatal("non-component schema must still appear in SchemaIDs/SchemaTypes (component ⊂ schema)")
	}
	if strings.Contains(s, "spore/runtime") {
		t.Fatalf("registry.gen.go must not import runtime:\n%s", s)
	}
}

// TestRenderRegistry_ComponentShapes: when EmitComponents is on, the registry
// file also emits the ComponentShapes slice of {Name, SchemaID} pairs in
// schema ID order. This is the pure-data handoff consumed by the ecsbind
// helper to drive script.Runtime.BindStruct without codegen importing script.
func TestRenderRegistry_ComponentShapes(t *testing.T) {
	entries := []RegistryEntry{
		{ID: 401, Name: "Velocity", SourceFile: "game.spore", IsComponent: true},
		{ID: 300, Name: "Position", SourceFile: "game.spore", IsComponent: true},
		{ID: 500, Name: "PlainMsg", SourceFile: "game.spore"}, // not a component
	}
	out, err := RenderRegistry(entries, Options{Package: "p", EmitComponents: true})
	if err != nil {
		t.Fatalf("RenderRegistry: %v", err)
	}
	s := string(out)

	// Struct definition with stdlib-only fields — no script/runtime types.
	for _, want := range []string{
		"type ComponentShape struct {",
		"Name     string",
		"SchemaID uint64",
		"var ComponentShapes = []ComponentShape{",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in:\n%s", want, s)
		}
	}

	// ComponentShapes must be in schema ID order regardless of input order:
	// Position (300) before Velocity (401). PlainMsg is not @component and
	// must not appear in ComponentShapes.
	wantOrdered := []string{
		`{Name: "Position", SchemaID: 300}`,
		`{Name: "Velocity", SchemaID: 401}`,
	}
	prev := -1
	for _, w := range wantOrdered {
		idx := strings.Index(s, w)
		if idx == -1 {
			t.Fatalf("missing %q in:\n%s", w, s)
		}
		if idx <= prev {
			t.Fatalf("ComponentShapes not in schema ID order: %q at %d after %d\n%s", w, idx, prev, s)
		}
		prev = idx
	}
	if strings.Contains(s, `"PlainMsg"`) && strings.Contains(s, "ComponentShapes = []ComponentShape{") {
		// Look only inside the ComponentShapes block: PlainMsg must not appear there.
		start := strings.Index(s, "var ComponentShapes = []ComponentShape{")
		end := strings.Index(s[start:], "\n}\n")
		if end == -1 {
			t.Fatalf("ComponentShapes block has no terminator:\n%s", s)
		}
		block := s[start : start+end]
		if strings.Contains(block, "PlainMsg") {
			t.Fatalf("non-component schema leaked into ComponentShapes:\n%s", block)
		}
	}

	// Hard contract: no script or runtime reverse-import in the codegen output.
	// The documentation comments mention `script.Runtime.BindStruct` and
	// `BindStruct(...)` to teach the ecsbind consumer-side wiring — that's
	// prose, not an actual import or call. We only ban code-level references.
	for _, banned := range []string{
		`"github.com/qomos-w/spore/script"`,
		`"github.com/qomos-w/spore/runtime"`,
		`import "github.com/qomos-w/spore/script"`,
	} {
		if strings.Contains(s, banned) {
			t.Fatalf("registry.gen.go must not reference %q (codegen is dependency-free):\n%s", banned, s)
		}
	}
}

// TestRenderRegistry_NoRegistryObjectWhenOff: without EmitComponents, no
// component subset or Registry object is emitted — the file stays schema-only.
func TestRenderRegistry_NoRegistryObjectWhenOff(t *testing.T) {
	entries := []RegistryEntry{
		{ID: 300, Name: "Position", SourceFile: "game.spore", IsComponent: true},
	}
	out, err := RenderRegistry(entries, Options{Package: "p"})
	if err != nil {
		t.Fatalf("RenderRegistry: %v", err)
	}
	s := string(out)
	for _, banned := range []string{
		"componentIDs", "registryTable", "var Registry =",
		"ComponentShape", "ComponentShapes",
	} {
		if strings.Contains(s, banned) {
			t.Fatalf("unexpected %q in:\n%s", banned, s)
		}
	}
}

// TestRenderRegistry_ComponentRequiresID: a component entry without a schema
// ID cannot be keyed in the table — RenderRegistry must reject it loudly.
func TestRenderRegistry_ComponentRequiresID(t *testing.T) {
	entries := []RegistryEntry{
		{ID: 0, Name: "FreeComponent", SourceFile: "game.spore", IsComponent: true},
	}
	_, err := RenderRegistry(entries, Options{Package: "p", EmitComponents: true})
	if err == nil || !strings.Contains(err.Error(), "schema ID") {
		t.Fatalf("expected schema-ID error, got %v", err)
	}
}

func TestSplitCamelWords(t *testing.T) {
	cases := map[string][]string{
		"":           nil,
		"_":          nil,
		"a":          {"a"},
		"userId":     {"user", "Id"},
		"JSONData":   {"JSON", "Data"},
		"old_string": {"old", "string"},
		"v2ray":      {"v2ray"},
		"AB":         {"AB"},
		"AbC":        {"Ab", "C"},
	}
	for in, want := range cases {
		got := splitCamelWords(in)
		if len(got) != len(want) {
			t.Fatalf("splitCamelWords(%q) = %v, want %v", in, got, want)
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("splitCamelWords(%q) = %v, want %v", in, got, want)
			}
		}
	}
}

func TestMapScalarUnknown(t *testing.T) {
	if _, err := mapScalar("nope"); err == nil {
		t.Fatal("expected unknown scalar error")
	}
}
