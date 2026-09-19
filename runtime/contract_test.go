package runtime_test

import (
	"errors"
	"testing"
	"time"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/diagnostics"
	"github.com/qomos-w/spore/identity"
	"github.com/qomos-w/spore/runtime"
	"github.com/qomos-w/spore/schema"
	"github.com/qomos-w/spore/transport"
)

// ============================================================================
// Entity contract tests
//
// Entity is a lightweight identity-bearing handle. It carries a CanonicalID
// and a reference to the World that owns it. It does not own component data.
// ============================================================================

func TestEntity_ID(t *testing.T) {
	id := mustID(t, 1000, 1, 0, 1)
	w := runtime.NewWorld()
	e := w.CreateWithID(id)

	if e.ID() != id {
		t.Fatalf("expected entity ID %s, got %s", id, e.ID())
	}
}

func TestEntity_ZeroValue_IsZero(t *testing.T) {
	var e runtime.Entity
	if !e.IsZero() {
		t.Fatal("zero-value Entity should report IsZero=true")
	}
}

func TestEntity_CreatedEntity_NotZero(t *testing.T) {
	w := runtime.NewWorld()
	e := w.Create()
	if e.IsZero() {
		t.Fatal("newly created entity should not be zero")
	}
}

func TestEntity_Comparable(t *testing.T) {
	// Entity must be usable as a map key
	w := runtime.NewWorld()
	e1 := w.Create()
	e2 := w.Create()

	m := map[runtime.Entity]bool{}
	m[e1] = true
	m[e2] = true
	if len(m) != 2 {
		t.Fatalf("expected 2 distinct map entries, got %d", len(m))
	}
}

func TestEntity_SameEntity_Equal(t *testing.T) {
	w := runtime.NewWorld()
	e1 := w.Create()

	// Look up the same entity from the world
	e2, ok := w.Entity(e1.ID())
	if !ok {
		t.Fatal("expected to find entity by ID")
	}
	if e1 != e2 {
		t.Fatal("same entity retrieved from world should be equal")
	}
}

// ============================================================================
// World contract tests — Entity lifecycle
// ============================================================================

func TestWorld_Create_GeneratesCanonicalID(t *testing.T) {
	w := runtime.NewWorld()
	e := w.Create()

	if e.IsZero() {
		t.Fatal("created entity should have a non-zero ID")
	}
	if e.ID().IsZero() {
		t.Fatal("created entity ID should not be zero")
	}
}

func TestWorld_CreateWithID_UsesProvidedID(t *testing.T) {
	id := mustID(t, 2000, 1, 0, 42)
	w := runtime.NewWorld()
	e := w.CreateWithID(id)

	if e.ID() != id {
		t.Fatalf("expected ID %s, got %s", id, e.ID())
	}
}

func TestWorld_CreateIDs_MonotonicallyIncreasing(t *testing.T) {
	w := runtime.NewWorld()
	e1 := w.Create()
	e2 := w.Create()

	// Sequence should be increasing
	if e1.ID().Sequence() >= e2.ID().Sequence() {
		t.Fatalf("expected sequence %d < %d", e1.ID().Sequence(), e2.ID().Sequence())
	}
}

func TestWorld_Dispose(t *testing.T) {
	w := runtime.NewWorld()
	e := w.Create()

	if !w.IsAlive(e) {
		t.Fatal("newly created entity should be alive")
	}

	w.Dispose(e)

	if w.IsAlive(e) {
		t.Fatal("disposed entity should not be alive")
	}
}

func TestWorld_Dispose_Idempotent(t *testing.T) {
	w := runtime.NewWorld()
	e := w.Create()

	w.Dispose(e)
	w.Dispose(e) // second dispose should not panic

	if w.IsAlive(e) {
		t.Fatal("disposed entity should not be alive")
	}
}

func TestWorld_IsAlive_NonExistent(t *testing.T) {
	w := runtime.NewWorld()
	id := mustID(t, 1000, 1, 0, 99)
	fakeEntity := runtime.MakeEntity(id, w)

	if w.IsAlive(fakeEntity) {
		t.Fatal("non-existent entity should not be alive")
	}
}

func TestWorld_EntityCount(t *testing.T) {
	w := runtime.NewWorld()
	if w.EntityCount() != 0 {
		t.Fatalf("expected 0 entities, got %d", w.EntityCount())
	}

	e1 := w.Create()
	_ = e1
	if w.EntityCount() != 1 {
		t.Fatalf("expected 1 entity, got %d", w.EntityCount())
	}

	e2 := w.Create()
	_ = e2
	if w.EntityCount() != 2 {
		t.Fatalf("expected 2 entities, got %d", w.EntityCount())
	}

	w.Dispose(e1)
	if w.EntityCount() != 1 {
		t.Fatalf("expected 1 entity after dispose, got %d", w.EntityCount())
	}
}

func TestWorld_Entity_Lookup(t *testing.T) {
	w := runtime.NewWorld()
	e := w.Create()

	found, ok := w.Entity(e.ID())
	if !ok {
		t.Fatal("expected to find entity by ID")
	}
	if found != e {
		t.Fatal("found entity should match created entity")
	}
}

func TestWorld_Entity_NotFound(t *testing.T) {
	w := runtime.NewWorld()
	id := mustID(t, 1000, 1, 0, 99)
	_, ok := w.Entity(id)
	if ok {
		t.Fatal("expected not to find non-existent entity")
	}
}

// ============================================================================
// World contract tests — Component management
// ============================================================================

type positionComponent struct {
	X float64
	Y float64
}

type velocityComponent struct {
	DX float64
	DY float64
}

func TestWorld_SetComponent(t *testing.T) {
	w := runtime.NewWorld()
	e := w.Create()

	pos := positionComponent{X: 10, Y: 20}
	w.SetComponent(e, "Position", &pos)

	got, ok := w.GetComponent(e, "Position")
	if !ok {
		t.Fatal("expected to find Position component")
	}
	gotPos, ok := got.(*positionComponent)
	if !ok {
		t.Fatalf("expected *positionComponent, got %T", got)
	}
	if gotPos.X != 10 || gotPos.Y != 20 {
		t.Fatalf("unexpected component data: %+v", gotPos)
	}
}

func TestWorld_SetComponent_Overwrite(t *testing.T) {
	w := runtime.NewWorld()
	e := w.Create()

	w.SetComponent(e, "Position", &positionComponent{X: 1, Y: 2})
	w.SetComponent(e, "Position", &positionComponent{X: 99, Y: 88})

	got, _ := w.GetComponent(e, "Position")
	pos := got.(*positionComponent)
	if pos.X != 99 || pos.Y != 88 {
		t.Fatalf("expected overwritten data, got %+v", pos)
	}
}

func TestWorld_GetComponent_NotFound(t *testing.T) {
	w := runtime.NewWorld()
	e := w.Create()

	_, ok := w.GetComponent(e, "Position")
	if ok {
		t.Fatal("expected not to find component that was never set")
	}
}

func TestWorld_RemoveComponent(t *testing.T) {
	w := runtime.NewWorld()
	e := w.Create()

	w.SetComponent(e, "Position", &positionComponent{X: 1, Y: 2})
	w.RemoveComponent(e, "Position")

	_, ok := w.GetComponent(e, "Position")
	if ok {
		t.Fatal("expected component to be removed")
	}
}

func TestWorld_RemoveComponent_NonExistent(t *testing.T) {
	w := runtime.NewWorld()
	e := w.Create()

	// Removing non-existent component should not panic
	w.RemoveComponent(e, "Position")
}

func TestWorld_HasComponent(t *testing.T) {
	w := runtime.NewWorld()
	e := w.Create()

	if w.HasComponent(e, "Position") {
		t.Fatal("expected HasComponent=false before set")
	}

	w.SetComponent(e, "Position", &positionComponent{})
	if !w.HasComponent(e, "Position") {
		t.Fatal("expected HasComponent=true after set")
	}
}

func TestWorld_Component_OnDisposedEntity(t *testing.T) {
	w := runtime.NewWorld()
	e := w.Create()
	w.SetComponent(e, "Position", &positionComponent{X: 1, Y: 2})
	w.Dispose(e)

	// Getting component on disposed entity should return false
	_, ok := w.GetComponent(e, "Position")
	if ok {
		t.Fatal("expected no component access on disposed entity")
	}

	// Setting component on disposed entity should fail
	err := w.SetComponent(e, "Velocity", &velocityComponent{})
	if err == nil {
		t.Fatal("expected error setting component on disposed entity")
	}
}

func TestWorld_AllComponentNames(t *testing.T) {
	w := runtime.NewWorld()
	e := w.Create()
	w.SetComponent(e, "Position", &positionComponent{})
	w.SetComponent(e, "Velocity", &velocityComponent{})

	names := w.ComponentNames(e)
	if len(names) != 2 {
		t.Fatalf("expected 2 component names, got %d", len(names))
	}
	nameSet := map[string]bool{}
	for _, n := range names {
		nameSet[n] = true
	}
	if !nameSet["Position"] || !nameSet["Velocity"] {
		t.Fatalf("expected Position and Velocity, got %v", names)
	}
}

// ============================================================================
// World options / configuration
// ============================================================================

func TestNewWorld_WithSlot(t *testing.T) {
	w := runtime.NewWorld(runtime.WithSlot(5))
	e := w.Create()
	if e.ID().RuntimeSlot() != 5 {
		t.Fatalf("expected RuntimeSlot=5, got %d", e.ID().RuntimeSlot())
	}
}

func TestNewWorld_WithIncarnation(t *testing.T) {
	w := runtime.NewWorld(runtime.WithIncarnation(3))
	e := w.Create()
	if e.ID().Incarnation() != 3 {
		t.Fatalf("expected Incarnation=3, got %d", e.ID().Incarnation())
	}
}

func TestWorld_Create_GeneratedEntityID_UsesCanonicalLayoutFromWorldOptions(t *testing.T) {
	w := runtime.NewWorld(runtime.WithSlot(5), runtime.WithIncarnation(3))
	e := w.Create()
	id := e.ID()

	if id.IsZero() {
		t.Fatal("expected generated entity ID to be non-zero")
	}
	if id.RuntimeSlot() != 5 {
		t.Fatalf("expected RuntimeSlot=5, got %d", id.RuntimeSlot())
	}
	if id.Incarnation() != 3 {
		t.Fatalf("expected Incarnation=3, got %d", id.Incarnation())
	}
	if id.Sequence() != 1 {
		t.Fatalf("expected first generated entity Sequence=1, got %d", id.Sequence())
	}
	if id.TimestampMS() == 0 {
		t.Fatal("expected generated entity TimestampMS to be populated")
	}

	ts, slot, inc, seq := id.Split()
	if ts != id.TimestampMS() || slot != id.RuntimeSlot() || inc != id.Incarnation() || seq != id.Sequence() {
		t.Fatal("expected Split() segments to match CanonicalID accessors")
	}
}

// ============================================================================
// Query contract tests
//
// Query is a component-name-based filter for entities. It follows Donburi's
// pattern of NewQuery().Has(...) but uses schema name strings instead of
// type objects, since components in spore are schema-described.
// ============================================================================

func TestQuery_Has(t *testing.T) {
	w := runtime.NewWorld()

	e1 := w.Create()
	w.SetComponent(e1, "Position", &positionComponent{X: 1, Y: 2})
	w.SetComponent(e1, "Velocity", &velocityComponent{DX: 3, DY: 4})

	e2 := w.Create()
	w.SetComponent(e2, "Position", &positionComponent{X: 5, Y: 6})

	_ = w.Create() // entity with no components

	q := runtime.NewQuery().Has("Position")
	result := w.Execute(q)

	if len(result) != 2 {
		t.Fatalf("expected 2 entities with Position, got %d", len(result))
	}
	found := map[identity.CanonicalID]bool{}
	for _, e := range result {
		found[e.ID()] = true
	}
	if !found[e1.ID()] || !found[e2.ID()] {
		t.Fatal("expected e1 and e2 in results")
	}
}

func TestQuery_HasMultiple(t *testing.T) {
	w := runtime.NewWorld()

	e1 := w.Create()
	w.SetComponent(e1, "Position", &positionComponent{})
	w.SetComponent(e1, "Velocity", &velocityComponent{})

	e2 := w.Create()
	w.SetComponent(e2, "Position", &positionComponent{})

	q := runtime.NewQuery().Has("Position", "Velocity")
	result := w.Execute(q)

	if len(result) != 1 {
		t.Fatalf("expected 1 entity with both Position and Velocity, got %d", len(result))
	}
	if result[0].ID() != e1.ID() {
		t.Fatal("expected e1 in results")
	}
}

func TestQuery_HasNone(t *testing.T) {
	w := runtime.NewWorld()

	e1 := w.Create()
	w.SetComponent(e1, "Position", &positionComponent{})

	e2 := w.Create()
	// No components

	q := runtime.NewQuery().HasNone("Position")
	result := w.Execute(q)

	if len(result) != 1 {
		t.Fatalf("expected 1 entity without Position, got %d", len(result))
	}
	if result[0].ID() != e2.ID() {
		t.Fatal("expected e2 (no Position) in results")
	}
}

func TestQuery_HasEither(t *testing.T) {
	w := runtime.NewWorld()

	e1 := w.Create()
	w.SetComponent(e1, "Position", &positionComponent{})

	e2 := w.Create()
	w.SetComponent(e2, "Velocity", &velocityComponent{})

	_ = w.Create() // entity with no components

	q := runtime.NewQuery().HasEither("Position", "Velocity")
	result := w.Execute(q)

	if len(result) != 2 {
		t.Fatalf("expected 2 entities with Position or Velocity, got %d", len(result))
	}
}

func TestQuery_WhenAdded(t *testing.T) {
	w := runtime.NewWorld()

	e1 := w.Create()
	w.SetComponent(e1, "Position", &positionComponent{})

	e2 := w.Create()
	w.SetComponent(e2, "Velocity", &velocityComponent{})

	// Clear changes so we can test "when added" from this point
	w.ClearChanges(e1)
	w.ClearChanges(e2)

	// Add a new component to e1
	w.SetComponent(e1, "Velocity", &velocityComponent{})

	q := runtime.NewQuery().WhenAdded("Velocity")
	result := w.Execute(q)

	if len(result) != 1 {
		t.Fatalf("expected 1 entity with Velocity added, got %d", len(result))
	}
	if result[0].ID() != e1.ID() {
		t.Fatal("expected e1 in results (Velocity was just added)")
	}
}

func TestQuery_WhenChanged(t *testing.T) {
	w := runtime.NewWorld()

	e1 := w.Create()
	pos := positionComponent{X: 1, Y: 2}
	w.SetComponent(e1, "Position", &pos)
	w.ClearChanges(e1)

	// Mutate the component via reference and mark changed
	pos.X = 99
	w.MarkChanged(e1, "Position")

	q := runtime.NewQuery().WhenChanged("Position")
	result := w.Execute(q)

	if len(result) != 1 {
		t.Fatalf("expected 1 entity with Position changed, got %d", len(result))
	}
}

func TestQuery_WhenRemoved(t *testing.T) {
	w := runtime.NewWorld()

	e1 := w.Create()
	w.SetComponent(e1, "Position", &positionComponent{})
	w.ClearChanges(e1)

	w.RemoveComponent(e1, "Position")

	q := runtime.NewQuery().WhenRemoved("Position")
	result := w.Execute(q)

	if len(result) != 1 {
		t.Fatalf("expected 1 entity with Position removed, got %d", len(result))
	}
}

func TestQuery_EmptyQuery_ReturnsAllAlive(t *testing.T) {
	w := runtime.NewWorld()
	e1 := w.Create()
	e2 := w.Create()
	w.Dispose(e1)

	q := runtime.NewQuery()
	result := w.Execute(q)

	if len(result) != 1 {
		t.Fatalf("expected 1 alive entity, got %d", len(result))
	}
	if result[0].ID() != e2.ID() {
		t.Fatal("expected e2 (alive) in results")
	}
}

func TestQuery_CombinedFilters(t *testing.T) {
	w := runtime.NewWorld()

	e1 := w.Create()
	w.SetComponent(e1, "Position", &positionComponent{})
	w.SetComponent(e1, "Velocity", &velocityComponent{})

	e2 := w.Create()
	w.SetComponent(e2, "Position", &positionComponent{})
	w.SetComponent(e2, "Static", struct{}{})

	// Has Position AND Velocity, but NOT Static
	q := runtime.NewQuery().Has("Position", "Velocity").HasNone("Static")
	result := w.Execute(q)

	if len(result) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(result))
	}
	if result[0].ID() != e1.ID() {
		t.Fatal("expected e1 in results")
	}
}

// ============================================================================
// Change tracking contract tests
// ============================================================================

func TestWorld_ChangeSet_Added(t *testing.T) {
	w := runtime.NewWorld()
	e := w.Create()
	w.SetComponent(e, "Position", &positionComponent{})

	cs := w.ChangeSet(e)
	if len(cs.Added) != 1 || cs.Added[0] != "Position" {
		t.Fatalf("expected Position in Added, got %v", cs.Added)
	}
}

func TestWorld_ChangeSet_Changed(t *testing.T) {
	w := runtime.NewWorld()
	e := w.Create()
	w.SetComponent(e, "Position", &positionComponent{X: 1})
	w.ClearChanges(e)

	pos, _ := w.GetComponent(e, "Position")
	posTyped := pos.(*positionComponent)
	posTyped.X = 99
	w.MarkChanged(e, "Position")

	cs := w.ChangeSet(e)
	if len(cs.Changed) != 1 || cs.Changed[0] != "Position" {
		t.Fatalf("expected Position in Changed, got %v", cs.Changed)
	}
	if len(cs.Added) != 0 {
		t.Fatalf("expected no Added after ClearChanges, got %v", cs.Added)
	}
}

func TestWorld_ChangeSet_Removed(t *testing.T) {
	w := runtime.NewWorld()
	e := w.Create()
	w.SetComponent(e, "Position", &positionComponent{})
	w.ClearChanges(e)

	w.RemoveComponent(e, "Position")

	cs := w.ChangeSet(e)
	if len(cs.Removed) != 1 || cs.Removed[0] != "Position" {
		t.Fatalf("expected Position in Removed, got %v", cs.Removed)
	}
}

func TestWorld_ClearChanges(t *testing.T) {
	w := runtime.NewWorld()
	e := w.Create()
	w.SetComponent(e, "Position", &positionComponent{})

	w.ClearChanges(e)

	cs := w.ChangeSet(e)
	if !cs.Empty() {
		t.Fatalf("expected empty ChangeSet after ClearChanges, got %+v", cs)
	}
}

func TestChangeSet_Empty(t *testing.T) {
	cs := runtime.ChangeSet{}
	if !cs.Empty() {
		t.Fatal("zero ChangeSet should be empty")
	}
}

func TestWorld_ChangeSet_DisposedEntity(t *testing.T) {
	w := runtime.NewWorld()
	e := w.Create()
	w.Dispose(e)

	cs := w.ChangeSet(e)
	if !cs.Empty() {
		t.Fatalf("disposed entity should have empty ChangeSet, got %+v", cs)
	}
}

func TestWorld_SetComponent_OverwriteTrackedAsChanged(t *testing.T) {
	w := runtime.NewWorld()
	e := w.Create()
	w.SetComponent(e, "Position", &positionComponent{X: 1})
	w.ClearChanges(e)

	// Overwrite counts as changed, not added
	w.SetComponent(e, "Position", &positionComponent{X: 99})

	cs := w.ChangeSet(e)
	if len(cs.Changed) != 1 || cs.Changed[0] != "Position" {
		t.Fatalf("expected Position in Changed (overwrite), got %v", cs.Changed)
	}
	if len(cs.Added) != 0 {
		t.Fatalf("expected no Added for overwrite, got %v", cs.Added)
	}
}

// ============================================================================
// Schema projection contract tests
//
// These tests verify that runtime carrier can project entity component
// data through the schema binding layer, producing transport-consumable
// views. This is the critical integration point between the runtime
// carrier and the script/schema/binding/projection layer.
// ============================================================================

func TestWorld_ProjectEntity(t *testing.T) {
	w := runtime.NewWorld()
	e := w.Create()
	w.SetComponent(e, "Position", &positionComponent{X: 10.5, Y: 20.3})

	posDesc := positionClassDesc()
	view, err := w.ProjectEntity(e, "Position", posDesc)
	if err != nil {
		t.Fatalf("ProjectEntity: %v", err)
	}

	if view.Identity != e.ID() {
		t.Fatal("projected view should carry entity identity")
	}
	if view.Schema.Name != "Position" {
		t.Fatalf("expected schema name Position, got %s", view.Schema.Name)
	}
	if view.Fields["X"] != 10.5 {
		t.Fatalf("expected X=10.5, got %v", view.Fields["X"])
	}
	if view.Fields["Y"] != 20.3 {
		t.Fatalf("expected Y=20.3, got %v", view.Fields["Y"])
	}
}

func TestWorld_ProjectEntity_ComponentNotFound(t *testing.T) {
	w := runtime.NewWorld()
	e := w.Create()

	posDesc := positionClassDesc()
	_, err := w.ProjectEntity(e, "Position", posDesc)
	if err == nil {
		t.Fatal("expected error for missing component, got nil")
	}

	var entityErr *runtime.EntityError
	if !isEntityError(err, &entityErr) {
		t.Fatalf("expected *EntityError, got %T", err)
	}
	if entityErr.EntityID != e.ID() {
		t.Fatal("EntityError should carry entity identity")
	}
}

func TestWorld_ProjectEntity_DisposedEntity(t *testing.T) {
	w := runtime.NewWorld()
	e := w.Create()
	w.SetComponent(e, "Position", &positionComponent{X: 1})
	w.Dispose(e)

	posDesc := positionClassDesc()
	_, err := w.ProjectEntity(e, "Position", posDesc)
	if err == nil {
		t.Fatal("expected error for disposed entity, got nil")
	}
}

func TestWorld_ProjectEntityAll(t *testing.T) {
	w := runtime.NewWorld()
	e := w.Create()
	w.SetComponent(e, "Position", &positionComponent{X: 10, Y: 20})
	w.SetComponent(e, "Velocity", &velocityComponent{DX: 1, DY: -1})

	descs := map[string]schema.ObjectDesc{
		"Position": positionClassDesc(),
		"Velocity": velocityClassDesc(),
	}

	view, err := w.ProjectEntityAll(e, descs)
	if err != nil {
		t.Fatalf("ProjectEntityAll: %v", err)
	}

	if view.Identity != e.ID() {
		t.Fatal("projected view should carry entity identity")
	}

	// Each component should appear as a nested map
	posFields, ok := view.Fields["Position"].(map[string]any)
	if !ok {
		t.Fatalf("expected Position as map[string]any, got %T", view.Fields["Position"])
	}
	if posFields["X"] != 10.0 {
		t.Fatalf("expected X=10, got %v", posFields["X"])
	}

	velFields, ok := view.Fields["Velocity"].(map[string]any)
	if !ok {
		t.Fatalf("expected Velocity as map[string]any, got %T", view.Fields["Velocity"])
	}
	if velFields["DX"] != 1.0 {
		t.Fatalf("expected DX=1, got %v", velFields["DX"])
	}
}

func TestWorld_PatchEntity(t *testing.T) {
	w := runtime.NewWorld()
	e := w.Create()
	pos := &positionComponent{X: 10, Y: 20}
	w.SetComponent(e, "Position", pos)
	w.ClearChanges(e)

	posDesc := positionClassDesc()

	// Create a patch view
	patchView := &binding.ViewProjection{
		Schema:   posDesc,
		Identity: e.ID(),
		Fields:   map[string]any{"X": 99.0, "Y": 88.0},
	}

	mutations, err := w.PatchEntity(e, "Position", posDesc, patchView)
	if err != nil {
		t.Fatalf("PatchEntity: %v", err)
	}

	if pos.X != 99 {
		t.Fatalf("expected X=99 after patch, got %f", pos.X)
	}
	if pos.Y != 88 {
		t.Fatalf("expected Y=88 after patch, got %f", pos.Y)
	}
	if len(mutations) == 0 {
		t.Fatal("expected at least one mutation from patch")
	}

	// Patch should mark the component as changed
	cs := w.ChangeSet(e)
	if len(cs.Changed) == 0 {
		t.Fatal("expected Position in ChangeSet.Changed after patch")
	}
}

func TestWorld_ProjectEntityChangesToTransport_ChangedComponent(t *testing.T) {
	w := runtime.NewWorld()
	e := w.Create()
	pos := &positionComponent{X: 10, Y: 20}
	if err := w.SetComponent(e, "Position", pos); err != nil {
		t.Fatalf("SetComponent: %v", err)
	}
	w.ClearChanges(e)
	pos.X = 99
	w.MarkChanged(e, "Position")

	codec := &transport.JSONCodec{}
	componentDescs := map[string]schema.ObjectDesc{"Position": positionClassDesc()}

	tv, err := w.ProjectEntityChangesToTransport(e, componentDescs, codec)
	if err != nil {
		t.Fatalf("ProjectEntityChangesToTransport: %v", err)
	}
	if tv.Kind != transport.ViewKindPatch {
		t.Fatalf("expected ViewKindPatch, got %s", tv.Kind)
	}
	if tv.Identity != e.ID() {
		t.Fatal("transport patch view should carry entity identity")
	}

	decoded, err := codec.Decode(tv)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	decodedMap, ok := decoded.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", decoded)
	}

	changed, ok := decodedMap["Changed"].(map[string]any)
	if !ok {
		t.Fatalf("expected Changed as map[string]any, got %T", decodedMap["Changed"])
	}
	position, ok := changed["Position"].(map[string]any)
	if !ok {
		t.Fatalf("expected Position patch payload as map[string]any, got %T", changed["Position"])
	}
	if position["X"] != 99.0 {
		t.Fatalf("expected patched X=99, got %v", position["X"])
	}
	if position["Y"] != 20.0 {
		t.Fatalf("expected projected Y=20, got %v", position["Y"])
	}
}

func TestWorld_ProjectEntityChangesToTransport_AddedAndRemovedComponents(t *testing.T) {
	w := runtime.NewWorld()
	e := w.Create()
	if err := w.SetComponent(e, "Position", &positionComponent{X: 10, Y: 20}); err != nil {
		t.Fatalf("SetComponent Position: %v", err)
	}
	if err := w.SetComponent(e, "Velocity", &velocityComponent{DX: 1, DY: 2}); err != nil {
		t.Fatalf("SetComponent Velocity: %v", err)
	}
	w.ClearChanges(e)
	w.RemoveComponent(e, "Velocity")
	if err := w.SetComponent(e, "Position", &positionComponent{X: 30, Y: 40}); err != nil {
		t.Fatalf("SetComponent Position overwrite: %v", err)
	}
	if err := w.SetComponent(e, "Health", &healthComponent{Value: 100}); err != nil {
		t.Fatalf("SetComponent Health: %v", err)
	}

	codec := &transport.JSONCodec{}
	componentDescs := map[string]schema.ObjectDesc{
		"Position": positionClassDesc(),
		"Velocity": velocityClassDesc(),
		"Health":   healthClassDesc(),
	}

	tv, err := w.ProjectEntityChangesToTransport(e, componentDescs, codec)
	if err != nil {
		t.Fatalf("ProjectEntityChangesToTransport: %v", err)
	}

	decoded, err := codec.Decode(tv)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	decodedMap := decoded.(map[string]any)

	added, ok := decodedMap["Added"].(map[string]any)
	if !ok {
		t.Fatalf("expected Added as map[string]any, got %T", decodedMap["Added"])
	}
	if _, ok := added["Health"]; !ok {
		t.Fatal("expected Health under Added")
	}

	changed, ok := decodedMap["Changed"].(map[string]any)
	if !ok {
		t.Fatalf("expected Changed as map[string]any, got %T", decodedMap["Changed"])
	}
	if _, ok := changed["Position"]; !ok {
		t.Fatal("expected Position under Changed")
	}
	if _, ok := changed["Health"]; ok {
		t.Fatal("did not expect added component Health under Changed")
	}

	removed, ok := decodedMap["Removed"].([]any)
	if !ok {
		t.Fatalf("expected Removed as []any, got %T", decodedMap["Removed"])
	}
	if len(removed) != 1 || removed[0] != "Velocity" {
		t.Fatalf("expected Removed=[Velocity], got %v", removed)
	}
}

func TestWorld_ProjectEntityChangesToTransport_MissingDescriptorForChangedComponent(t *testing.T) {
	w := runtime.NewWorld()
	e := w.Create()
	if err := w.SetComponent(e, "Position", &positionComponent{X: 10, Y: 20}); err != nil {
		t.Fatalf("SetComponent Position: %v", err)
	}
	if err := w.SetComponent(e, "Velocity", &velocityComponent{DX: 1, DY: 2}); err != nil {
		t.Fatalf("SetComponent Velocity: %v", err)
	}
	w.ClearChanges(e)
	w.MarkChanged(e, "Velocity")

	codec := &transport.JSONCodec{}
	_, err := w.ProjectEntityChangesToTransport(e, map[string]schema.ObjectDesc{"Position": positionClassDesc()}, codec)
	if err == nil {
		t.Fatal("expected error for missing changed descriptor, got nil")
	}

	var entityErr *runtime.EntityError
	if !isEntityError(err, &entityErr) {
		t.Fatalf("expected EntityError, got %T", err)
	}
	if entityErr.EntityID != e.ID() {
		t.Fatal("expected EntityError to carry entity identity")
	}
	if entityErr.Error() != "schema descriptor for changed component \"Velocity\" not found" {
		t.Fatalf("unexpected error message: %v", entityErr)
	}
}

func TestWorld_ProjectEntityChangesToTransport_MissingDescriptorForAddedComponent(t *testing.T) {
	w := runtime.NewWorld()
	e := w.Create()
	if err := w.SetComponent(e, "Velocity", &velocityComponent{DX: 1, DY: 2}); err != nil {
		t.Fatalf("SetComponent Velocity: %v", err)
	}
	w.ClearChanges(e)
	if err := w.SetComponent(e, "Health", &healthComponent{Value: 100}); err != nil {
		t.Fatalf("SetComponent Health: %v", err)
	}

	codec := &transport.JSONCodec{}
	_, err := w.ProjectEntityChangesToTransport(e, map[string]schema.ObjectDesc{"Velocity": velocityClassDesc()}, codec)
	if err == nil {
		t.Fatal("expected error for missing added descriptor, got nil")
	}

	var entityErr *runtime.EntityError
	if !isEntityError(err, &entityErr) {
		t.Fatalf("expected EntityError, got %T", err)
	}
	if entityErr.EntityID != e.ID() {
		t.Fatal("expected EntityError to carry entity identity")
	}
	if entityErr.Error() != "schema descriptor for added component \"Health\" not found" {
		t.Fatalf("unexpected error message: %v", entityErr)
	}
}

func TestWorld_ProjectEntityChangesToTransport_EmptyPatch(t *testing.T) {
	w := runtime.NewWorld()
	e := w.Create()
	if err := w.SetComponent(e, "Position", &positionComponent{X: 10, Y: 20}); err != nil {
		t.Fatalf("SetComponent: %v", err)
	}
	w.ClearChanges(e)

	codec := &transport.JSONCodec{}
	componentDescs := map[string]schema.ObjectDesc{"Position": positionClassDesc()}

	tv, err := w.ProjectEntityChangesToTransport(e, componentDescs, codec)
	if err != nil {
		t.Fatalf("ProjectEntityChangesToTransport: %v", err)
	}
	if tv.Kind != transport.ViewKindPatch {
		t.Fatalf("expected ViewKindPatch, got %s", tv.Kind)
	}

	decoded, err := codec.Decode(tv)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	decodedMap := decoded.(map[string]any)
	added, ok := decodedMap["Added"].(map[string]any)
	if !ok {
		t.Fatalf("expected Added as map[string]any, got %T", decodedMap["Added"])
	}
	if len(added) != 0 {
		t.Fatalf("expected empty Added, got %v", added)
	}
	changed, ok := decodedMap["Changed"].(map[string]any)
	if !ok {
		t.Fatalf("expected Changed as map[string]any, got %T", decodedMap["Changed"])
	}
	if len(changed) != 0 {
		t.Fatalf("expected empty Changed, got %v", changed)
	}
	removed, ok := decodedMap["Removed"].([]any)
	if !ok {
		t.Fatalf("expected Removed as []any, got %T", decodedMap["Removed"])
	}
	if len(removed) != 0 {
		t.Fatalf("expected empty Removed, got %v", removed)
	}
}

func TestWorld_ProjectEntityChangesToTransport_MissingDescriptor(t *testing.T) {
	w := runtime.NewWorld()
	e := w.Create()
	if err := w.SetComponent(e, "Position", &positionComponent{X: 10, Y: 20}); err != nil {
		t.Fatalf("SetComponent: %v", err)
	}

	codec := &transport.JSONCodec{}
	_, err := w.ProjectEntityChangesToTransport(e, map[string]schema.ObjectDesc{}, codec)
	if err == nil {
		t.Fatal("expected error for missing descriptor, got nil")
	}

	var entityErr *runtime.EntityError
	if !isEntityError(err, &entityErr) {
		t.Fatalf("expected EntityError, got %T", err)
	}
}

func TestWorld_ProjectEntityChangesToTransport_DisposedEntity(t *testing.T) {
	w := runtime.NewWorld()
	e := w.Create()
	if err := w.SetComponent(e, "Position", &positionComponent{X: 10, Y: 20}); err != nil {
		t.Fatalf("SetComponent: %v", err)
	}
	w.Dispose(e)

	codec := &transport.JSONCodec{}
	_, err := w.ProjectEntityChangesToTransport(e, map[string]schema.ObjectDesc{"Position": positionClassDesc()}, codec)
	if err == nil {
		t.Fatal("expected error for disposed entity, got nil")
	}

	var entityErr *runtime.EntityError
	if !isEntityError(err, &entityErr) {
		t.Fatalf("expected EntityError, got %T", err)
	}
}

// ============================================================================
// helpers
// ============================================================================

func mustID(t *testing.T, ts uint64, slot uint16, inc uint16, seq uint64) identity.CanonicalID {
	t.Helper()
	id, err := identity.NewCanonicalID(ts, slot, inc, seq)
	if err != nil {
		t.Fatalf("NewCanonicalID: %v", err)
	}
	return id
}

func init() {
	// Ensure consistent timestamp baseline for tests
	_ = time.Now()
}

type healthComponent struct {
	Value int
}

func positionClassDesc() schema.ObjectDesc {
	return schema.ObjectDesc{
		Name: "Position",
		Fields: []schema.FieldDesc{
			{Name: "X", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "double"}},
			{Name: "Y", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "double"}},
		},
	}
}

func velocityClassDesc() schema.ObjectDesc {
	return schema.ObjectDesc{
		Name: "Velocity",
		Fields: []schema.FieldDesc{
			{Name: "DX", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "double"}},
			{Name: "DY", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "double"}},
		},
	}
}

func healthClassDesc() schema.ObjectDesc {
	return schema.ObjectDesc{
		Name: "Health",
		Fields: []schema.FieldDesc{
			{Name: "Value", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}},
		},
	}
}

func isEntityError(err error, target **runtime.EntityError) bool {
	for e := err; e != nil; {
		if ee, ok := e.(*runtime.EntityError); ok {
			*target = ee
			return true
		}
		if unwrapper, ok := e.(interface{ Unwrap() error }); ok {
			e = unwrapper.Unwrap()
		} else {
			break
		}
	}
	return false
}

func TestEntityError_Unwrap(t *testing.T) {
	cause := errors.New("root cause")
	err := &runtime.EntityError{EntityID: mustID(t, 1000, 1, 0, 1), Err: cause}
	if err.Error() != "root cause" {
		t.Fatalf("expected error message from cause, got %q", err.Error())
	}
	if err.Unwrap() != cause {
		t.Fatalf("expected unwrap to return cause, got %v", err.Unwrap())
	}
}

func TestEntityError_DirectExpectedActual(t *testing.T) {
	err := &runtime.EntityError{
		EntityID: mustID(t, 1000, 1, 0, 1),
		Err:      errors.New("type mismatch"),
		Expected: "int",
		Actual:   "string",
	}
	if err.DiagnosticExpected() != "int" {
		t.Fatalf("expected Expected='int', got %q", err.DiagnosticExpected())
	}
	if err.DiagnosticActual() != "string" {
		t.Fatalf("expected Actual='string', got %q", err.DiagnosticActual())
	}
}

func TestEntityError_DelegatesExpectedActualFromWrapped(t *testing.T) {
	// Create a mock error that implements DiagnosticExpected/DiagnosticActual
	wrapped := &mockDiagnosticError{
		msg:      "inner error",
		expected: "struct",
		actual:   "map",
	}
	err := &runtime.EntityError{
		EntityID: mustID(t, 1000, 1, 0, 2),
		Err:      wrapped,
	}
	if err.DiagnosticExpected() != "struct" {
		t.Fatalf("expected Expected='struct' from wrapped, got %q", err.DiagnosticExpected())
	}
	if err.DiagnosticActual() != "map" {
		t.Fatalf("expected Actual='map' from wrapped, got %q", err.DiagnosticActual())
	}
}

func TestEntityError_DirectTakesPrecedenceOverWrapped(t *testing.T) {
	wrapped := &mockDiagnosticError{
		msg:      "inner error",
		expected: "wrapped_expected",
		actual:   "wrapped_actual",
	}
	err := &runtime.EntityError{
		EntityID: mustID(t, 1000, 1, 0, 3),
		Err:      wrapped,
		Expected: "direct_expected",
		Actual:   "direct_actual",
	}
	if err.DiagnosticExpected() != "direct_expected" {
		t.Fatalf("expected direct Expected to take precedence, got %q", err.DiagnosticExpected())
	}
	if err.DiagnosticActual() != "direct_actual" {
		t.Fatalf("expected direct Actual to take precedence, got %q", err.DiagnosticActual())
	}
}

func TestEntityError_FromErrorPreservesExpectedActual(t *testing.T) {
	err := &runtime.EntityError{
		EntityID: mustID(t, 1000, 1, 0, 4),
		Err:      errors.New("test"),
		Expected: "int",
		Actual:   "float",
	}
	diag := diagnostics.FromError(err, diagnostics.Descriptor{})
	if diag.Expected != "int" {
		t.Fatalf("expected FromError Expected='int', got %q", diag.Expected)
	}
	if diag.Actual != "float" {
		t.Fatalf("expected FromError Actual='float', got %q", diag.Actual)
	}
}

func TestEntityError_DiagnosticCodeDelegates(t *testing.T) {
	wrapped := &mockDiagnosticError{msg: "inner", code: "binding_failure"}
	err := &runtime.EntityError{EntityID: mustID(t, 1000, 1, 0, 5), Err: wrapped}
	if err.DiagnosticCode() != "binding_failure" {
		t.Fatalf("expected code from wrapped, got %q", err.DiagnosticCode())
	}
}

func TestEntityError_DiagnosticCodeFallback(t *testing.T) {
	err := &runtime.EntityError{EntityID: mustID(t, 1000, 1, 0, 6), Err: errors.New("plain")}
	if err.DiagnosticCode() != runtime.CodeEntityError {
		t.Fatalf("expected fallback code %q, got %q", runtime.CodeEntityError, err.DiagnosticCode())
	}
}

func TestEntityError_DiagnosticCategoryDelegates(t *testing.T) {
	wrapped := &mockDiagnosticError{msg: "inner", category: diagnostics.CategoryContract}
	err := &runtime.EntityError{EntityID: mustID(t, 1000, 1, 0, 7), Err: wrapped}
	if err.DiagnosticCategory() != diagnostics.CategoryContract {
		t.Fatalf("expected delegated category contract, got %q", err.DiagnosticCategory())
	}
}

func TestEntityError_DiagnosticCategoryFallback(t *testing.T) {
	err := &runtime.EntityError{EntityID: mustID(t, 1000, 1, 0, 8), Err: errors.New("plain")}
	if err.DiagnosticCategory() != diagnostics.CategoryRuntime {
		t.Fatalf("expected fallback category runtime, got %q", err.DiagnosticCategory())
	}
}

func TestEntityError_DiagnosticSpanDelegates(t *testing.T) {
	span := diagnostics.Span{Start: diagnostics.Position{Line: 2, Column: 3}, End: diagnostics.Position{Line: 2, Column: 5}}
	wrapped := &mockDiagnosticError{msg: "inner", span: span}
	err := &runtime.EntityError{EntityID: mustID(t, 1000, 1, 0, 9), Err: wrapped}
	if err.DiagnosticSpan() != span {
		t.Fatalf("expected delegated span %+v, got %+v", span, err.DiagnosticSpan())
	}
}

func TestEntityError_DiagnosticStackDelegates(t *testing.T) {
	stack := []diagnostics.Frame{{Callable: "tick", Stage: "update"}}
	wrapped := &mockDiagnosticError{msg: "inner", stack: stack}
	err := &runtime.EntityError{EntityID: mustID(t, 1000, 1, 0, 10), Err: wrapped}
	got := err.DiagnosticStack()
	if len(got) != 1 || got[0].Callable != "tick" || got[0].Stage != "update" {
		t.Fatalf("expected delegated stack, got %+v", got)
	}
}

func TestEntityError_NilSafe(t *testing.T) {
	var err *runtime.EntityError
	if err.DiagnosticExpected() != "" {
		t.Fatal("nil EntityError Expected should be empty")
	}
	if err.DiagnosticActual() != "" {
		t.Fatal("nil EntityError Actual should be empty")
	}
	if err.DiagnosticCode() != "" {
		t.Fatal("nil EntityError Code should be empty")
	}
	if err.DiagnosticCategory() != "" {
		t.Fatal("nil EntityError Category should be empty")
	}
	if err.DiagnosticPath() != "" {
		t.Fatal("nil EntityError Path should be empty")
	}
	if err.DiagnosticSpan() != (diagnostics.Span{}) {
		t.Fatal("nil EntityError Span should be zero")
	}
	if err.DiagnosticStack() != nil {
		t.Fatal("nil EntityError Stack should be nil")
	}
}

// mockDiagnosticError is a test helper that implements diagnostic interfaces.
type mockDiagnosticError struct {
	msg      string
	code     string
	category diagnostics.Category
	path     string
	expected string
	actual   string
	span     diagnostics.Span
	stack    []diagnostics.Frame
}

func (e *mockDiagnosticError) Error() string               { return e.msg }
func (e *mockDiagnosticError) DiagnosticCode() string       { return e.code }
func (e *mockDiagnosticError) DiagnosticCategory() diagnostics.Category { return e.category }
func (e *mockDiagnosticError) DiagnosticPath() string       { return e.path }
func (e *mockDiagnosticError) DiagnosticExpected() string   { return e.expected }
func (e *mockDiagnosticError) DiagnosticActual() string     { return e.actual }
func (e *mockDiagnosticError) DiagnosticSpan() diagnostics.Span { return e.span }
func (e *mockDiagnosticError) DiagnosticStack() []diagnostics.Frame { return append([]diagnostics.Frame(nil), e.stack...) }

// ============================================================================
// Transport projection contract tests
//
// These tests verify the full chain: runtime component → schema binding →
// view projection → transport encoding → transport decoding.
// ============================================================================

func TestWorld_ProjectEntityToTransport(t *testing.T) {
	w := runtime.NewWorld()
	e := w.Create()
	w.SetComponent(e, "Position", &positionComponent{X: 10.5, Y: 20.3})

	codec := &transport.JSONCodec{}
	posDesc := positionClassDesc()

	tv, err := w.ProjectEntityToTransport(e, "Position", posDesc, codec)
	if err != nil {
		t.Fatalf("ProjectEntityToTransport: %v", err)
	}

	// Verify transport view carries identity
	if tv.Identity != e.ID() {
		t.Fatal("transport view should carry entity identity")
	}

	// Verify data round-trips through transport
	decoded, err := codec.Decode(tv)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	decodedMap, ok := decoded.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", decoded)
	}
	if decodedMap["X"] != 10.5 {
		t.Fatalf("expected X=10.5, got %v", decodedMap["X"])
	}
	if decodedMap["Y"] != 20.3 {
		t.Fatalf("expected Y=20.3, got %v", decodedMap["Y"])
	}
}

func TestWorld_ProjectEntityToTransport_FullRoundTrip(t *testing.T) {
	// Create entity, project to transport, decode, patch back
	w := runtime.NewWorld()
	e := w.Create()
	pos := &positionComponent{X: 100, Y: 200}
	w.SetComponent(e, "Position", pos)

	codec := &transport.JSONCodec{}
	posDesc := positionClassDesc()

	// Project to transport
	tv, err := w.ProjectEntityToTransport(e, "Position", posDesc, codec)
	if err != nil {
		t.Fatalf("ProjectEntityToTransport: %v", err)
	}

	// Decode from transport
	decoded, err := codec.Decode(tv)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	decodedMap := decoded.(map[string]any)

	// Patch back to a different entity
	e2 := w.Create()
	pos2 := &positionComponent{X: 0, Y: 0}
	w.SetComponent(e2, "Position", pos2)

	patchView := &binding.ViewProjection{
		Schema:   posDesc,
		Identity: e2.ID(),
		Fields:   decodedMap,
	}
	mutations, err := w.PatchEntity(e2, "Position", posDesc, patchView)
	if err != nil {
		t.Fatalf("PatchEntity: %v", err)
	}
	if len(mutations) == 0 {
		t.Fatal("expected mutations from patch")
	}
	if pos2.X != 100 {
		t.Fatalf("expected X=100 after round-trip, got %f", pos2.X)
	}
}
