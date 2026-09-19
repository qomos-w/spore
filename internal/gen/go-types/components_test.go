package gotypes

import (
	"strings"
	"testing"

	"github.com/qomos-w/spore/schema"
)

func TestRender_EmitComponents(t *testing.T) {
	objs := []schema.ObjectDesc{
		{
			Kind:        schema.TypeKindStruct,
			Name:        "Position",
			SchemaID:    7,
			IsComponent: true,
			Fields: []schema.FieldDesc{
				{Name: "x", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "float"}},
				{Name: "y", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "float"}},
			},
		},
		{
			Kind: schema.TypeKindStruct,
			Name: "NotAComponent",
			Fields: []schema.FieldDesc{
				{Name: "id", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
			},
		},
	}

	out, err := Render(objs, Options{
		Package:        "gamedomain",
		EmitComponents: true,
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	want := `package gamedomain

import (
	"reflect"

	"github.com/qomos-w/spore/runtime"
	"github.com/qomos-w/spore/schema"
)

// Schema IDs for value types defined in this package.
const (
	PositionSchemaID uint64 = 7
)

func init() {
	schema.RegisterStructType(PositionSchemaID, reflect.TypeOf(Position{}))
}

// ECS component descriptors (declared @component in the schema).
var (
	PositionC = runtime.NewComponent[Position]("Position")
)

type NotAComponent struct {
	ID string ` + "`json:\"id\"`" + `
}

type Position struct {
	X float32 ` + "`json:\"x\"`" + `
	Y float32 ` + "`json:\"y\"`" + `
}
`
	if string(out) != want {
		t.Fatalf("Render mismatch:\n--- got ---\n%s\n--- want ---\n%s", out, want)
	}
	if !strings.Contains(string(out), `PositionC = runtime.NewComponent[Position]("Position")`) {
		t.Fatal("component descriptor var missing")
	}
	if strings.Contains(string(out), "RegisterComponentType") {
		t.Fatal("per-file registration init must not be emitted (registration is centralized in registry.gen.go)")
	}
	if strings.Contains(string(out), "NotAComponentC") {
		t.Fatal("non-@component struct must not get a descriptor var")
	}
}

// Component ⇒ schema: an @component struct without a schema ID cannot be keyed
// in the registry table, so rendering components must reject it loudly.
func TestRender_EmitComponentsRequiresSchemaID(t *testing.T) {
	objs := []schema.ObjectDesc{
		{
			Kind:        schema.TypeKindStruct,
			Name:        "FreeComponent",
			IsComponent: true,
			Fields: []schema.FieldDesc{
				{Name: "x", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "float"}},
			},
		},
	}

	_, err := Render(objs, Options{Package: "gamedomain", EmitComponents: true})
	if err == nil || !strings.Contains(err.Error(), "has no schema ID") {
		t.Fatalf("expected schema-ID error, got %v", err)
	}
}

func TestRender_EmitComponentsOffByDefault(t *testing.T) {
	objs := []schema.ObjectDesc{
		{
			Kind:        schema.TypeKindStruct,
			Name:        "Position",
			IsComponent: true,
			Fields: []schema.FieldDesc{
				{Name: "x", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "float"}},
			},
		},
	}

	out, err := Render(objs, Options{Package: "gamedomain"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.Contains(string(out), "NewComponent") {
		t.Fatal("component vars must not be emitted without EmitComponents")
	}
	if strings.Contains(string(out), "spore/runtime") {
		t.Fatal("spore/runtime import must not appear without EmitComponents")
	}
}

// ComponentShapes is the consumer-side (ecsbind) handoff for script
// BindStruct wiring. It is registry-level data (one entry per @component in
// the package), so per-file Render must NOT emit it: the per-file Render
// output is keyed by individual source files, and emitting a single global
// slice from each file would duplicate entries across the package. The
// registry file (RenderRegistry) is the single source of truth.
func TestRender_PerFileOmitsComponentShapes(t *testing.T) {
	objs := []schema.ObjectDesc{
		{
			Kind:        schema.TypeKindStruct,
			Name:        "Position",
			SchemaID:    7,
			IsComponent: true,
			Fields: []schema.FieldDesc{
				{Name: "x", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "float"}},
			},
		},
	}

	out, err := Render(objs, Options{Package: "gamedomain", EmitComponents: true})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	s := string(out)
	for _, banned := range []string{"ComponentShape", "ComponentShapes"} {
		if strings.Contains(s, banned) {
			t.Fatalf("per-file Render must not emit %q (registry-level artifact only):\n%s", banned, s)
		}
	}
}

// Per-file Render output must never import script or reference BindStruct;
// the binding call lives on the ecsbind consumer side. This pins the
// "codegen does not reverse-reference script" hard constraint at the
// per-file level too.
func TestRender_PerFileNoScriptReverseImport(t *testing.T) {
	objs := []schema.ObjectDesc{
		{
			Kind:        schema.TypeKindStruct,
			Name:        "Position",
			SchemaID:    7,
			IsComponent: true,
			Fields: []schema.FieldDesc{
				{Name: "x", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "float"}},
			},
		},
	}

	out, err := Render(objs, Options{Package: "gamedomain", EmitComponents: true})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	s := string(out)
	for _, banned := range []string{
		`"github.com/qomos-w/spore/script"`,
		"script.Runtime",
		"BindStruct(",
	} {
		if strings.Contains(s, banned) {
			t.Fatalf("per-file Render must not reference %q:\n%s", banned, s)
		}
	}
}

func TestRender_EmitComponentsWithSchemaIDs(t *testing.T) {
	objs := []schema.ObjectDesc{
		{
			Kind:        schema.TypeKindStruct,
			Name:        "Position",
			SchemaID:    3,
			IsComponent: true,
			Fields: []schema.FieldDesc{
				{Name: "x", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "float"}},
			},
		},
	}

	out, err := Render(objs, Options{
		Package:        "gamedomain",
		EmitComponents: true,
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	// Both imports must appear in a single import block.
	if !strings.Contains(string(out), `"github.com/qomos-w/spore/runtime"`) {
		t.Fatal("runtime import missing")
	}
	if !strings.Contains(string(out), `"github.com/qomos-w/spore/schema"`) {
		t.Fatal("schema import missing")
	}
	if !strings.Contains(string(out), `PositionC = runtime.NewComponent[Position]("Position")`) {
		t.Fatal("component descriptor var missing")
	}
}
