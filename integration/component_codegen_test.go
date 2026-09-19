package integration

import (
	"reflect"
	"strings"
	"testing"

	gotypes "github.com/qomos-w/spore/internal/gen/go-types"
	"github.com/qomos-w/spore/runtime"
	"github.com/qomos-w/spore/schema"
	"github.com/qomos-w/spore/script"
)

// End-to-end: a .spore source with @component flows through ParseObjects
// (the exact pipeline the spore-gen-go-types CLI uses) and per-file codegen
// emits usable runtime.Component[T] descriptor vars.
func TestComponentAnnotation_CodegenPipeline(t *testing.T) {
	src := `
@component
@schema(300)
struct Position {
  x: float
  y: float
}

@component
@schema(301)
struct Health {
  value: int
}

struct PlainPayload {
  note: string
}
`

	objs, err := script.ParseObjects(src)
	if err != nil {
		t.Fatalf("ParseObjects: %v", err)
	}
	if len(objs) != 3 {
		t.Fatalf("expected 3 objects, got %d", len(objs))
	}

	var pos, health, plain = -1, -1, -1
	for i, o := range objs {
		switch o.Name {
		case "Position":
			pos = i
		case "Health":
			health = i
		case "PlainPayload":
			plain = i
		}
	}
	if pos < 0 || health < 0 || plain < 0 {
		t.Fatalf("missing objects: pos=%d health=%d plain=%d", pos, health, plain)
	}
	if !objs[pos].IsComponent {
		t.Fatal("Position should be IsComponent")
	}
	if !objs[health].IsComponent {
		t.Fatal("Health should be IsComponent")
	}
	if objs[plain].IsComponent {
		t.Fatal("PlainPayload must not be IsComponent")
	}

	rendered, err := gotypes.Render(objs, gotypes.Options{
		Package:        "integrationgen",
		EmitComponents: true,
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	srcOut := string(rendered)

	// Descriptor vars use the <Name>C convention. gofmt column-aligns the
	// var block, so compare with padding-tolerant forms.
	compact := strings.Join(strings.Fields(srcOut), " ")
	for _, want := range []string{
		`HealthC = runtime.NewComponent[Health]("Health")`,
		`PositionC = runtime.NewComponent[Position]("Position")`,
	} {
		if !strings.Contains(compact, want) {
			t.Fatalf("generated source missing %q:\n%s", want, srcOut)
		}
	}
	if strings.Contains(srcOut, "RegisterComponentType") {
		t.Fatal("per-file registration init must not be emitted (registration is centralized in the registry table)")
	}
	if strings.Contains(srcOut, "PlainPayloadC") {
		t.Fatal("non-@component struct got a descriptor var")
	}
}

// A @component struct must be a schema (carry a schema ID): with no
// @schema(N) and no numeric file base it cannot be keyed in the codegen
// registry table, so rendering must reject it.
func TestComponentAnnotation_NoSchemaIDRejected(t *testing.T) {
	src := `
@component
struct FreeComponent {
  flag: bool
}
`
	objs, err := script.ParseObjects(src)
	if err != nil {
		t.Fatalf("ParseObjects: %v", err)
	}
	_, err = gotypes.Render(objs, gotypes.Options{
		Package:        "integrationgen",
		EmitComponents: true,
	})
	if err == nil || !strings.Contains(err.Error(), "has no schema ID") {
		t.Fatalf("expected schema-ID error, got %v", err)
	}
}

// End-to-end: the registry file (RenderRegistry) carries the single
// codegen Registry object for AddRegistry — full schema table plus the
// @component subset — and the emitted shape matches what the runtime World
// consumes through the SchemaTable interface.
func TestComponentRegistry_SingleObjectFeedsWorld(t *testing.T) {
	// The same objects the per-file pipeline above renders; here collected
	// into one cross-file registry entry set.
	objs, err := script.ParseObjects(`
@component
@schema(300)
struct Position {
  x: float
}

@component
@schema(301)
struct Health {
  value: int
}

struct PlainPayload {
  note: string
}
`)
	if err != nil {
		t.Fatalf("ParseObjects: %v", err)
	}

	var entries []gotypes.RegistryEntry
	for _, o := range objs {
		if o.Kind != schema.TypeKindStruct || o.SchemaID == 0 {
			continue
		}
		entries = append(entries, gotypes.RegistryEntry{
			ID:          o.SchemaID,
			Name:        o.Name,
			SourceFile:  "integration.spore",
			IsComponent: o.IsComponent,
		})
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 registry entries (schema-carrying structs), got %d", len(entries))
	}

	out, err := gotypes.RenderRegistry(entries, gotypes.Options{
		Package:        "integrationgen",
		EmitComponents: true,
	})
	if err != nil {
		t.Fatalf("RenderRegistry: %v", err)
	}
	s := string(out)

	// The schema table object must be emitted and reference the existing
	// package-level maps; component ids must be the @component subset.
	for _, want := range []string{
		"300: \"Position\"",
		"301: \"Health\"",
		"var Registry = registryTable{",
		"componentIDs: componentIDs,",
		"func (t registryTable) SchemaTypes() map[uint64]reflect.Type",
		"func (t registryTable) SchemaIDs() map[uint64]string",
		"func (t registryTable) ComponentIDs() []uint64",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("registry file missing %q:\n%s", want, s)
		}
	}
	if !strings.Contains(s, "spore/runtime") {
		// Registry object itself is zero-dependency; the host imports runtime.
	}
	if strings.Contains(s, "import \"github.com/qomos-w/spore/runtime\"") {
		t.Fatalf("registry.gen.go must not import runtime:\n%s", s)
	}
	if strings.Contains(s, "PlainPayload") {
		t.Fatalf("PlainPayload is not a component and must not appear in componentIDs:\n%s", s)
	}
}

// End-to-end check of the script BindStruct wiring handoff: the registry file
// emits a ComponentShapes slice of {Name, SchemaID} pairs in schema ID order
// — pure stdlib data with no Go type references. The consumer-side ecsbind
// helper iterates this slice and uses Registry.SchemaTypes() to obtain a
// reflect.Type for each entry, then calls script.Runtime.BindStruct with a
// reflectively-constructed zero value. The test mirrors that exact contract:
// every shape must resolve to a real Go type via the registered SchemaTypes
// table, so ecsbind can drive the binding call without codegen importing
// script or runtime.
func TestComponentRegistry_ShapesHandedOffToEcsbind(t *testing.T) {
	objs, err := script.ParseObjects(`
@component
@schema(310)
struct Velocity {
  dx: float
}

@component
@schema(300)
struct Position {
  x: float
}

@component
@schema(320)
struct Health {
  value: int
}

struct PlainPayload {
  note: string
}
`)
	if err != nil {
		t.Fatalf("ParseObjects: %v", err)
	}

	var entries []gotypes.RegistryEntry
	for _, o := range objs {
		if o.Kind != schema.TypeKindStruct || o.SchemaID == 0 {
			continue
		}
		entries = append(entries, gotypes.RegistryEntry{
			ID:          o.SchemaID,
			Name:        o.Name,
			SourceFile:  "integration.spore",
			IsComponent: o.IsComponent,
		})
	}

	out, err := gotypes.RenderRegistry(entries, gotypes.Options{
		Package:        "integrationgen",
		EmitComponents: true,
	})
	if err != nil {
		t.Fatalf("RenderRegistry: %v", err)
	}
	s := string(out)

	// ComponentShape is a stdlib-only struct (string + uint64).
	for _, want := range []string{
		"type ComponentShape struct {",
		"Name     string",
		"SchemaID uint64",
		"var ComponentShapes = []ComponentShape{",
		`{Name: "Position", SchemaID: 300}`,
		`{Name: "Velocity", SchemaID: 310}`,
		`{Name: "Health", SchemaID: 320}`,
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("registry file missing %q:\n%s", want, s)
		}
	}

	// Order must be by SchemaID (300, 310, 320), not by the input order
	// (310, 300, 320) or alphabetical.
	wantOrdered := []string{
		`{Name: "Position", SchemaID: 300}`,
		`{Name: "Velocity", SchemaID: 310}`,
		`{Name: "Health", SchemaID: 320}`,
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

	// PlainPayload is a schema but not @component — must not leak into
	// ComponentShapes (only @component structs get the BindStruct hook).
	start := strings.Index(s, "var ComponentShapes = []ComponentShape{")
	end := strings.Index(s[start:], "\n}\n")
	if end == -1 {
		t.Fatalf("ComponentShapes block has no terminator:\n%s", s)
	}
	block := s[start : start+end]
	if strings.Contains(block, "PlainPayload") {
		t.Fatalf("non-component schema leaked into ComponentShapes:\n%s", block)
	}

	// Codegen hard contract: the registry file is the source of truth for
	// the BindStruct wiring handoff. It must not actually import or call
	// script/runtime — only document how the consumer side uses the data.
	for _, banned := range []string{
		`"github.com/qomos-w/spore/script"`,
		`"github.com/qomos-w/spore/runtime"`,
		`import "github.com/qomos-w/spore/script"`,
		`import "github.com/qomos-w/spore/runtime"`,
	} {
		if strings.Contains(s, banned) {
			t.Fatalf("registry.gen.go must not reference %q (codegen is dependency-free):\n%s", banned, s)
		}
	}
}

// handPos mirrors a generated @component struct + its descriptor.
type handPos struct {
	X, Y float32
}

// handRegistry implements runtime.SchemaTable the way the generated
// registryTable does (three stdlib-typed accessors).
type handRegistry struct {
	types  map[uint64]reflect.Type
	names  map[uint64]string
	compID []uint64
}

func (h handRegistry) SchemaTypes() map[uint64]reflect.Type { return h.types }
func (h handRegistry) SchemaIDs() map[uint64]string         { return h.names }
func (h handRegistry) ComponentIDs() []uint64               { return h.compID }

func newHandRegistry(t *testing.T) handRegistry {
	t.Helper()
	return handRegistry{
		types: map[uint64]reflect.Type{
			401: reflect.TypeOf(handPos{}),
		},
		names: map[uint64]string{
			401: "hand.Position",
		},
		compID: []uint64{401},
	}
}

// End-to-end behavioral check: a hand-declared registry table drives the
// descriptor-free facade on a runtime World, and the generated <Name>C
// descriptor naming convention still works for the explicit typed API.
func TestComponentRegistry_AddRegistryDrivesWorld(t *testing.T) {
	// Mirrors the registry.gen.go emitted shape from RenderRegistry.
	w := runtime.NewWorld()
	w.AddRegistry(newHandRegistry(t))

	// Descriptor-free facade through the registered table.
	e := w.Create()
	if err := w.SetT(e, &handPos{X: 1, Y: 2}); err != nil {
		t.Fatalf("SetT: %v", err)
	}
	p, ok := w.GetT[handPos](e)
	if !ok || p.X != 1 || p.Y != 2 {
		t.Fatalf("GetT: %v %v", p, ok)
	}

	// Generated descriptor convention interop on the same slot.
	handPosC := runtime.NewComponent[handPos]("hand.Position")
	if err := w.Set(e, handPosC, &handPos{X: 9}); err != nil {
		t.Fatalf("Set via descriptor: %v", err)
	}
	if p2, _ := w.Get(e, handPosC); p2.X != 9 {
		t.Fatalf("descriptor write must win on shared slot, got %v", p2.X)
	}
}
