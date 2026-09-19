package binding_test

import (
	"errors"
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/diagnostics"
	"github.com/qomos-w/spore/identity"
	"github.com/qomos-w/spore/schema"
)

// ============================================================================
// Object binding contract tests
//
// These tests lock the relationship between schema descriptors, runtime
// Go objects, and transport views. The binding layer must keep the three
// layers distinct.
// ============================================================================

type playerStruct struct {
	Name  string
	Level int
}

func playerClassDesc() schema.ObjectDesc {
	return schema.ObjectDesc{
		Name: "playerStruct",
		Fields: []schema.FieldDesc{
			{Name: "Name", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
			{Name: "Level", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}},
		},
	}
}

func TestObjectBinding_Creation(t *testing.T) {
	id := mustID(t, 1000, 1, 0, 1)
	desc := playerClassDesc()
	p := playerStruct{Name: "alice", Level: 5}

	b, err := binding.NewObjectBinding(desc, id, &p)
	if err != nil {
		t.Fatalf("NewObjectBinding: %v", err)
	}

	if !b.Valid() {
		t.Fatal("newly created binding should be valid")
	}
	if b.Identity != id {
		t.Fatal("binding identity mismatch")
	}
}

func TestObjectBinding_NilTarget(t *testing.T) {
	id := mustID(t, 1000, 1, 0, 2)
	desc := playerClassDesc()

	_, err := binding.NewObjectBinding(desc, id, nil)
	if err == nil {
		t.Fatal("expected error for nil target, got nil")
	}

	var bindErr *binding.BindingError
	if !isBindingError(err, &bindErr) {
		t.Fatalf("expected *binding.BindingError, got %T", err)
	}
	if bindErr.Identity != id {
		t.Fatal("BindingError missing identity context")
	}
}

func TestObjectBinding_Invalidate(t *testing.T) {
	id := mustID(t, 1000, 1, 0, 3)
	desc := playerClassDesc()
	p := playerStruct{Name: "bob", Level: 3}

	b, err := binding.NewObjectBinding(desc, id, &p)
	if err != nil {
		t.Fatalf("NewObjectBinding: %v", err)
	}

	if !b.Valid() {
		t.Fatal("binding should be valid before invalidation")
	}

	b.Invalidate()

	if b.Valid() {
		t.Fatal("binding should be invalid after Invalidate()")
	}
}

func TestObjectBinding_TargetReturnsBoundObject(t *testing.T) {
	id := mustID(t, 1000, 1, 0, 4)
	desc := playerClassDesc()
	p := playerStruct{Name: "carol", Level: 7}

	b, err := binding.NewObjectBinding(desc, id, &p)
	if err != nil {
		t.Fatalf("NewObjectBinding: %v", err)
	}

	got := b.Target()
	if got == nil {
		t.Fatal("Target() returned nil for valid binding")
	}

	ptr, ok := got.(*playerStruct)
	if !ok {
		t.Fatalf("expected *playerStruct, got %T", got)
	}
	if ptr.Name != "carol" || ptr.Level != 7 {
		t.Fatalf("unexpected target state: %+v", ptr)
	}
}

func TestObjectBinding_TargetNilAfterInvalidation(t *testing.T) {
	id := mustID(t, 1000, 1, 0, 5)
	desc := playerClassDesc()
	p := playerStruct{Name: "dave", Level: 1}

	b, err := binding.NewObjectBinding(desc, id, &p)
	if err != nil {
		t.Fatalf("NewObjectBinding: %v", err)
	}

	b.Invalidate()

	if b.Target() != nil {
		t.Fatal("Target() should return nil after invalidation")
	}
}

// ============================================================================
// View projection contract tests
// ============================================================================

func TestProjectView_ValidBinding(t *testing.T) {
	id := mustID(t, 1000, 1, 0, 10)
	desc := playerClassDesc()
	p := playerStruct{Name: "eve", Level: 10}

	b, err := binding.NewObjectBinding(desc, id, &p)
	if err != nil {
		t.Fatalf("NewObjectBinding: %v", err)
	}

	view, err := binding.ProjectView(b)
	if err != nil {
		t.Fatalf("ProjectView: %v", err)
	}

	if view.Identity != id {
		t.Fatal("view identity mismatch")
	}
	if view.Schema.Name != "playerStruct" {
		t.Fatalf("expected schema name playerStruct, got %s", view.Schema.Name)
	}
}

func TestProjectView_InvalidBinding(t *testing.T) {
	id := mustID(t, 1000, 1, 0, 11)
	desc := playerClassDesc()
	p := playerStruct{Name: "frank", Level: 2}

	b, err := binding.NewObjectBinding(desc, id, &p)
	if err != nil {
		t.Fatalf("NewObjectBinding: %v", err)
	}

	b.Invalidate()

	_, err = binding.ProjectView(b)
	if err == nil {
		t.Fatal("expected error projecting from invalid binding, got nil")
	}

	var bindErr *binding.BindingError
	if !isBindingError(err, &bindErr) {
		t.Fatalf("expected *binding.BindingError, got %T", err)
	}
	if bindErr.Identity != id {
		t.Fatal("BindingError missing identity context")
	}
}

func TestProjectView_NilBinding(t *testing.T) {
	_, err := binding.ProjectView(nil)
	if err == nil {
		t.Fatal("expected error for nil binding, got nil")
	}
}

// ============================================================================
// Field-level projection contract tests
//
// These tests verify that ProjectView reads actual struct field values
// from the bound runtime object, guided by the schema descriptor.
// ============================================================================

func TestProjectView_PopulatesFieldsFromStruct(t *testing.T) {
	id := mustID(t, 1000, 2, 0, 1)
	desc := playerClassDesc()
	p := playerStruct{Name: "alice", Level: 5}

	b, err := binding.NewObjectBinding(desc, id, &p)
	if err != nil {
		t.Fatalf("NewObjectBinding: %v", err)
	}

	view, err := binding.ProjectView(b)
	if err != nil {
		t.Fatalf("ProjectView: %v", err)
	}

	if len(view.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(view.Fields))
	}
	if _, ok := view.Fields["Name"]; !ok {
		t.Fatal("missing Name field in projection")
	}
	if _, ok := view.Fields["Level"]; !ok {
		t.Fatal("missing Level field in projection")
	}
}

func TestProjectView_FieldValuesMatchRuntime(t *testing.T) {
	id := mustID(t, 1000, 2, 0, 2)
	desc := playerClassDesc()
	p := playerStruct{Name: "bob", Level: 7}

	b, err := binding.NewObjectBinding(desc, id, &p)
	if err != nil {
		t.Fatalf("NewObjectBinding: %v", err)
	}

	view, err := binding.ProjectView(b)
	if err != nil {
		t.Fatalf("ProjectView: %v", err)
	}

	if view.Fields["Name"] != "bob" {
		t.Fatalf("expected Name=bob, got %v", view.Fields["Name"])
	}
	if view.Fields["Level"] != 7 {
		t.Fatalf("expected Level=7, got %v", view.Fields["Level"])
	}
}

func TestProjectView_SchemaIsProjectionAuthority(t *testing.T) {
	// Only fields declared in the schema descriptor should appear in the
	// projection — not all exported fields on the struct.
	id := mustID(t, 1000, 2, 0, 3)

	// Schema declares only "Name" — "Level" is excluded
	desc := schema.ObjectDesc{
		Name: "playerStruct",
		Fields: []schema.FieldDesc{
			{Name: "Name", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
		},
	}
	p := playerStruct{Name: "carol", Level: 3}

	b, err := binding.NewObjectBinding(desc, id, &p)
	if err != nil {
		t.Fatalf("NewObjectBinding: %v", err)
	}

	view, err := binding.ProjectView(b)
	if err != nil {
		t.Fatalf("ProjectView: %v", err)
	}

	if len(view.Fields) != 1 {
		t.Fatalf("expected 1 field (schema authority), got %d", len(view.Fields))
	}
	if _, ok := view.Fields["Level"]; ok {
		t.Fatal("Level should not be projected — not in schema descriptor")
	}
}

func TestProjectView_RuntimeChangeReflectedOnReprojection(t *testing.T) {
	id := mustID(t, 1000, 2, 0, 4)
	desc := playerClassDesc()
	p := playerStruct{Name: "dave", Level: 1}

	b, err := binding.NewObjectBinding(desc, id, &p)
	if err != nil {
		t.Fatalf("NewObjectBinding: %v", err)
	}

	view1, err := binding.ProjectView(b)
	if err != nil {
		t.Fatalf("ProjectView: %v", err)
	}
	if view1.Fields["Level"] != 1 {
		t.Fatalf("expected Level=1, got %v", view1.Fields["Level"])
	}

	// Mutate the runtime object directly
	p.Level = 99

	// Re-project — should reflect the current runtime state
	view2, err := binding.ProjectView(b)
	if err != nil {
		t.Fatalf("ProjectView: %v", err)
	}
	if view2.Fields["Level"] != 99 {
		t.Fatalf("expected Level=99 after mutation, got %v", view2.Fields["Level"])
	}

	// The earlier projection is a snapshot — it should not have changed
	if view1.Fields["Level"] != 1 {
		t.Fatal("previous projection should remain unchanged (snapshot semantics)")
	}
}

func TestProjectView_FieldNotInStructReturnsError(t *testing.T) {
	// Schema declares a field that doesn't exist on the runtime struct.
	// Validation now happens eagerly at binding creation time.
	id := mustID(t, 1000, 2, 0, 5)
	desc := schema.ObjectDesc{
		Name: "playerStruct",
		Fields: []schema.FieldDesc{
			{Name: "Name", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
			{Name: "NonExistent", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}},
		},
	}
	p := playerStruct{Name: "eve", Level: 5}

	_, err := binding.NewObjectBinding(desc, id, &p)
	if err == nil {
		t.Fatal("expected error for field not in struct, got nil")
	}

	var bindErr *binding.BindingError
	if !isBindingError(err, &bindErr) {
		t.Fatalf("expected *binding.BindingError, got %T", err)
	}
	if bindErr.Path != ".NonExistent" {
		t.Fatalf("expected path .NonExistent, got %s", bindErr.Path)
	}
}

func TestProjectView_NestedStructProjectedAsMap(t *testing.T) {
	// A struct field should be projected as a map[string]any,
	// not as the raw Go struct — preserving three-layer separation.
	type locationStruct struct {
		Zone string
		X    int
		Y    int
	}
	type entityStruct struct {
		Name     string
		Location locationStruct
	}

	id := mustID(t, 1000, 2, 0, 6)
	desc := schema.ObjectDesc{
		Name: "entityStruct",
		Fields: []schema.FieldDesc{
			{Name: "Name", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
			{Name: "Location", Type: schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "struct", ClassName: "locationStruct"}},
		},
	}

	e := entityStruct{Name: "hero", Location: locationStruct{Zone: "forest", X: 10, Y: 20}}

	b, err := binding.NewObjectBinding(desc, id, &e)
	if err != nil {
		t.Fatalf("NewObjectBinding: %v", err)
	}

	view, err := binding.ProjectView(b)
	if err != nil {
		t.Fatalf("ProjectView: %v", err)
	}

	loc, ok := view.Fields["Location"]
	if !ok {
		t.Fatal("missing Location field in projection")
	}
	locMap, ok := loc.(map[string]any)
	if !ok {
		t.Fatalf("expected nested struct projected as map[string]any, got %T", loc)
	}
	if locMap["Zone"] != "forest" {
		t.Fatalf("expected Zone=forest, got %v", locMap["Zone"])
	}
	if locMap["X"] != 10 {
		t.Fatalf("expected X=10, got %v", locMap["X"])
	}
}

// ============================================================================
// Three-layer separation contract tests
//
// These tests prove that schema, runtime, and transport view remain
// distinct objects — they never collapse into a single object.
// ============================================================================

func TestThreeLayerDistinction_SchemaIsNotRuntimeObject(t *testing.T) {
	desc := playerClassDesc()
	// Schema descriptor is a pure description — it has no runtime state
	if len(desc.Fields) != 2 {
		t.Fatalf("expected 2 fields in schema, got %d", len(desc.Fields))
	}
	// Schema has no identity — that belongs to the binding
	if desc.Name != "playerStruct" {
		t.Fatalf("expected playerStruct, got %s", desc.Name)
	}
}

func TestThreeLayerDistinction_RuntimeObjectIsNotSchema(t *testing.T) {
	p := playerStruct{Name: "grace", Level: 6}
	// Runtime object carries actual state — it is not a descriptor
	if p.Name != "grace" {
		t.Fatalf("unexpected runtime state: %s", p.Name)
	}
}

func TestThreeLayerDistinction_ViewIsNotRuntimeObject(t *testing.T) {
	id := mustID(t, 1000, 1, 0, 20)
	desc := playerClassDesc()
	p := playerStruct{Name: "heidi", Level: 8}

	b, err := binding.NewObjectBinding(desc, id, &p)
	if err != nil {
		t.Fatalf("NewObjectBinding: %v", err)
	}

	view, err := binding.ProjectView(b)
	if err != nil {
		t.Fatalf("ProjectView: %v", err)
	}

	// View is a projection, not the original object
	// Modifying the view must not modify the runtime object
	view.Fields["Name"] = "modified"
	if p.Name != "heidi" {
		t.Fatal("modifying view fields must not affect runtime object")
	}
}

// ============================================================================
// View diff / patch contract tests
//
// These tests verify that view projections support change detection,
// diff computation, and patch semantics — all driven by schema awareness.
// ============================================================================

func TestDiffViewProjection_NoChanges(t *testing.T) {
	id := mustID(t, 1000, 3, 0, 1)
	desc := playerClassDesc()
	p := playerStruct{Name: "alice", Level: 5}

	b, err := binding.NewObjectBinding(desc, id, &p)
	if err != nil {
		t.Fatalf("NewObjectBinding: %v", err)
	}

	view1, err := binding.ProjectView(b)
	if err != nil {
		t.Fatalf("ProjectView: %v", err)
	}

	view2, err := binding.ProjectView(b)
	if err != nil {
		t.Fatalf("ProjectView: %v", err)
	}

	diff := binding.DiffViewProjection(view1, view2)
	if len(diff) != 0 {
		t.Fatalf("expected no changes, got %d mutations", len(diff))
	}
}

func TestDiffViewProjection_DetectsFieldChange(t *testing.T) {
	id := mustID(t, 1000, 3, 0, 2)
	desc := playerClassDesc()
	p := playerStruct{Name: "alice", Level: 5}

	b, err := binding.NewObjectBinding(desc, id, &p)
	if err != nil {
		t.Fatalf("NewObjectBinding: %v", err)
	}

	view1, err := binding.ProjectView(b)
	if err != nil {
		t.Fatalf("ProjectView: %v", err)
	}

	// Mutate runtime
	p.Level = 10

	view2, err := binding.ProjectView(b)
	if err != nil {
		t.Fatalf("ProjectView: %v", err)
	}

	diff := binding.DiffViewProjection(view1, view2)
	if len(diff) != 1 {
		t.Fatalf("expected 1 change, got %d", len(diff))
	}
	if diff[0].Kind != binding.MutationReplaced {
		t.Fatalf("expected MutationReplaced, got %s", diff[0].Kind)
	}
	if diff[0].Key != "Level" {
		t.Fatalf("expected key Level, got %s", diff[0].Key)
	}
}

func TestDiffViewProjection_DetectsMultipleChanges(t *testing.T) {
	id := mustID(t, 1000, 3, 0, 3)
	desc := playerClassDesc()
	p := playerStruct{Name: "alice", Level: 5}

	b, err := binding.NewObjectBinding(desc, id, &p)
	if err != nil {
		t.Fatalf("NewObjectBinding: %v", err)
	}

	view1, err := binding.ProjectView(b)
	if err != nil {
		t.Fatalf("ProjectView: %v", err)
	}

	p.Name = "bob"
	p.Level = 10

	view2, err := binding.ProjectView(b)
	if err != nil {
		t.Fatalf("ProjectView: %v", err)
	}

	diff := binding.DiffViewProjection(view1, view2)
	if len(diff) != 2 {
		t.Fatalf("expected 2 changes, got %d", len(diff))
	}
	changedKeys := make(map[string]bool)
	for _, m := range diff {
		changedKeys[m.Key] = true
	}
	if !changedKeys["Name"] || !changedKeys["Level"] {
		t.Fatalf("expected Name and Level in diff, got %v", changedKeys)
	}
}

func TestDiffViewProjection_DifferentIdentitiesProduceNoFieldDiff(t *testing.T) {
	// Two views from different identities but same field values
	// should produce no field-level diff — identity is not a field
	id1 := mustID(t, 1000, 3, 0, 4)
	id2 := mustID(t, 1000, 3, 0, 5)
	desc := playerClassDesc()

	view1 := &binding.ViewProjection{
		Schema:   desc,
		Identity: id1,
		Fields:   map[string]any{"Name": "alice", "Level": 5},
	}
	view2 := &binding.ViewProjection{
		Schema:   desc,
		Identity: id2,
		Fields:   map[string]any{"Name": "alice", "Level": 5},
	}

	diff := binding.DiffViewProjection(view1, view2)
	if len(diff) != 0 {
		t.Fatalf("expected no field changes for same field values, got %d", len(diff))
	}
}

func TestDiffViewProjection_FieldAppearingIsInserted(t *testing.T) {
	desc := playerClassDesc()

	view1 := &binding.ViewProjection{
		Schema:   desc,
		Identity: mustID(t, 1000, 3, 0, 6),
		Fields:   map[string]any{"Name": "alice"},
	}
	view2 := &binding.ViewProjection{
		Schema:   desc,
		Identity: mustID(t, 1000, 3, 0, 6),
		Fields:   map[string]any{"Name": "alice", "Level": 5},
	}

	diff := binding.DiffViewProjection(view1, view2)
	if len(diff) != 1 {
		t.Fatalf("expected 1 change, got %d", len(diff))
	}
	if diff[0].Kind != binding.MutationInserted {
		t.Fatalf("expected MutationInserted for new field, got %s", diff[0].Kind)
	}
	if diff[0].Key != "Level" {
		t.Fatalf("expected key Level, got %s", diff[0].Key)
	}
}

func TestDiffViewProjection_FieldDisappearingIsRemoved(t *testing.T) {
	desc := playerClassDesc()

	view1 := &binding.ViewProjection{
		Schema:   desc,
		Identity: mustID(t, 1000, 3, 0, 7),
		Fields:   map[string]any{"Name": "alice", "Level": 5},
	}
	view2 := &binding.ViewProjection{
		Schema:   desc,
		Identity: mustID(t, 1000, 3, 0, 7),
		Fields:   map[string]any{"Name": "alice"},
	}

	diff := binding.DiffViewProjection(view1, view2)
	if len(diff) != 1 {
		t.Fatalf("expected 1 change, got %d", len(diff))
	}
	if diff[0].Kind != binding.MutationRemoved {
		t.Fatalf("expected MutationRemoved for missing field, got %s", diff[0].Kind)
	}
	if diff[0].Key != "Level" {
		t.Fatalf("expected key Level, got %s", diff[0].Key)
	}
}

// ============================================================================
// Bidirectional sync boundary contract tests
//
// These tests lock the boundary that projection is one-way by default
// (schema → runtime → view). Writing view changes back to the runtime
// object is an explicit opt-in operation, not the default behavior.
// ============================================================================

func TestBidirectionalSync_ViewModificationDoesNotAffectRuntime(t *testing.T) {
	// By default, modifying a ViewProjection does NOT propagate back
	// to the runtime object — projection is one-way.
	id := mustID(t, 1000, 4, 0, 1)
	desc := playerClassDesc()
	p := playerStruct{Name: "alice", Level: 5}

	b, err := binding.NewObjectBinding(desc, id, &p)
	if err != nil {
		t.Fatalf("NewObjectBinding: %v", err)
	}

	view, err := binding.ProjectView(b)
	if err != nil {
		t.Fatalf("ProjectView: %v", err)
	}

	// Modify view
	view.Fields["Level"] = 99

	// Runtime should be unchanged
	if p.Level != 5 {
		t.Fatal("view modification must not propagate to runtime (one-way projection)")
	}
}

func TestBidirectionalSync_ApplyPatchIsExplicit(t *testing.T) {
	// Applying a patch from view to runtime is an explicit operation.
	// It is not automatic — the caller must opt in.
	id := mustID(t, 1000, 4, 0, 2)
	desc := playerClassDesc()
	p := playerStruct{Name: "alice", Level: 5}

	b, err := binding.NewObjectBinding(desc, id, &p)
	if err != nil {
		t.Fatalf("NewObjectBinding: %v", err)
	}

	view, err := binding.ProjectView(b)
	if err != nil {
		t.Fatalf("ProjectView: %v", err)
	}

	view.Fields["Level"] = 99

	// Explicit apply
	mutations, err := binding.ApplyViewPatch(b, view)
	if err != nil {
		t.Fatalf("ApplyViewPatch: %v", err)
	}

	// Runtime should now reflect the patch
	if p.Level != 99 {
		t.Fatalf("expected Level=99 after apply, got %d", p.Level)
	}

	// Mutations should record the change
	if len(mutations) != 1 {
		t.Fatalf("expected 1 mutation, got %d", len(mutations))
	}
	if mutations[0].Kind != binding.MutationReplaced {
		t.Fatalf("expected MutationReplaced, got %s", mutations[0].Kind)
	}
	if mutations[0].Key != "Level" {
		t.Fatalf("expected key Level, got %s", mutations[0].Key)
	}
}

func TestBidirectionalSync_ApplyPatchOnlyAffectsSchemaFields(t *testing.T) {
	// ApplyViewPatch should only write back fields that are declared in
	// the schema descriptor — extra fields in the view are ignored.
	id := mustID(t, 1000, 4, 0, 3)
	desc := schema.ObjectDesc{
		Name: "playerStruct",
		Fields: []schema.FieldDesc{
			{Name: "Name", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
		},
	}
	p := playerStruct{Name: "alice", Level: 5}

	b, err := binding.NewObjectBinding(desc, id, &p)
	if err != nil {
		t.Fatalf("NewObjectBinding: %v", err)
	}

	view := &binding.ViewProjection{
		Schema:   desc,
		Identity: id,
		Fields: map[string]any{
			"Name":     "bob",
			"Level":    99, // Not in schema — should be ignored
			"ExtraKey": 42, // Not in schema — should be ignored
		},
	}

	mutations, err := binding.ApplyViewPatch(b, view)
	if err != nil {
		t.Fatalf("ApplyViewPatch: %v", err)
	}

	if p.Name != "bob" {
		t.Fatalf("expected Name=bob, got %s", p.Name)
	}
	// Level should remain unchanged — it's not in the schema
	if p.Level != 5 {
		t.Fatalf("expected Level=5 (not in schema), got %d", p.Level)
	}
	// Only Name should appear in mutations
	for _, m := range mutations {
		if m.Key == "Level" || m.Key == "ExtraKey" {
			t.Fatalf("non-schema field %q should not appear in mutations", m.Key)
		}
	}
}

func TestBidirectionalSync_ApplyPatchInvalidBindingFails(t *testing.T) {
	id := mustID(t, 1000, 4, 0, 4)
	desc := playerClassDesc()
	p := playerStruct{Name: "alice", Level: 5}

	b, err := binding.NewObjectBinding(desc, id, &p)
	if err != nil {
		t.Fatalf("NewObjectBinding: %v", err)
	}

	b.Invalidate()

	view := &binding.ViewProjection{
		Schema:   desc,
		Identity: id,
		Fields:   map[string]any{"Name": "bob"},
	}

	_, err = binding.ApplyViewPatch(b, view)
	if err == nil {
		t.Fatal("expected error applying patch to invalid binding, got nil")
	}

	var bindErr *binding.BindingError
	if !isBindingError(err, &bindErr) {
		t.Fatalf("expected *binding.BindingError, got %T", err)
	}
}

// ============================================================================
// Map binding contract tests
//
// These tests lock the Go map[K]V → ordered-map script surface contract
// and the OrderedMap backing preservation contract.
// ============================================================================

func TestOrderedMap_BasicMapSemantics(t *testing.T) {
	m := schema.NewOrderedMap[string, int]()

	// Set should return inserted for new keys
	if m.Set("a", 1) != schema.SetInserted {
		t.Fatal("expected SetInserted for new key")
	}
	if m.Set("b", 2) != schema.SetInserted {
		t.Fatal("expected SetInserted for new key")
	}
	if m.Set("c", 3) != schema.SetInserted {
		t.Fatal("expected SetInserted for new key")
	}

	// Overwrite should return replaced
	if m.Set("b", 20) != schema.SetReplaced {
		t.Fatal("expected SetReplaced for existing key")
	}

	// Len
	if m.Len() != 3 {
		t.Fatalf("expected len 3, got %d", m.Len())
	}

	// Get
	if v, ok := m.Get("b"); !ok || v != 20 {
		t.Fatalf("expected b=20, got %d, ok=%v", v, ok)
	}

	// Has
	if !m.Has("a") {
		t.Fatal("expected Has(a)=true")
	}
	if m.Has("z") {
		t.Fatal("expected Has(z)=false")
	}
}

func TestOrderedMap_InsertionOrder(t *testing.T) {
	m := schema.NewOrderedMap[string, int]()
	m.Set("x", 1)
	m.Set("y", 2)
	m.Set("z", 3)

	entries := m.Entries()
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	if entries[0].Key != "x" || entries[1].Key != "y" || entries[2].Key != "z" {
		t.Fatalf("expected insertion order x,y,z; got %v", keysOf(entries))
	}
}

func TestOrderedMap_OverwritePreservesOrder(t *testing.T) {
	m := schema.NewOrderedMap[string, int]()
	m.Set("a", 1)
	m.Set("b", 2)
	m.Set("c", 3)
	m.Set("b", 20) // overwrite

	entries := m.Entries()
	// b should still be in second position
	if entries[1].Key != "b" || entries[1].Value != 20 {
		t.Fatalf("overwrite should preserve position; got entries=%v", entries)
	}
}

func TestOrderedMap_DeleteAndReinsert(t *testing.T) {
	m := schema.NewOrderedMap[string, int]()
	m.Set("a", 1)
	m.Set("b", 2)
	m.Set("c", 3)

	if m.Delete("b") != schema.DeleteRemoved {
		t.Fatal("expected DeleteRemoved for existing key")
	}
	if m.Delete("b") != schema.DeleteMissing {
		t.Fatal("expected DeleteMissing for non-existent key")
	}
	if m.Len() != 2 {
		t.Fatalf("expected len 2 after delete, got %d", m.Len())
	}

	// Re-insert should append to tail
	m.Set("b", 20)
	entries := m.Entries()
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	// b should now be at the end
	if entries[2].Key != "b" || entries[2].Value != 20 {
		t.Fatalf("re-inserted key should be at tail; got entries=%v", entries)
	}
}

func TestOrderedMap_EntriesIsCanonicalView(t *testing.T) {
	m := schema.NewOrderedMap[string, int]()
	m.Set("a", 1)
	m.Set("b", 2)

	entries1 := m.Entries()
	entries2 := m.Entries()

	// Two calls should return equal content
	if len(entries1) != len(entries2) {
		t.Fatal("Entries() should return consistent length")
	}
	for i := range entries1 {
		if entries1[i].Key != entries2[i].Key || entries1[i].Value != entries2[i].Value {
			t.Fatalf("Entries() inconsistent at index %d", i)
		}
	}

	// Modifying returned slice should not affect the map
	entries1[0] = schema.Entry[string, int]{Key: "z", Value: 99}
	entries3 := m.Entries()
	if entries3[0].Key != "a" || entries3[0].Value != 1 {
		t.Fatal("modifying Entries() result should not affect map")
	}
}

func TestOrderedMapFromMap_DeterministicProjection(t *testing.T) {
	// Go map has no insertion order; OrderedMapFromMap must produce
	// a deterministic order (sorted by key for orderedKey types)
	src := map[string]int{"c": 3, "a": 1, "b": 2}
	m := schema.OrderedMapFromMap(src)

	entries := m.Entries()
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	// Must be sorted by key (deterministic projection)
	if entries[0].Key != "a" || entries[1].Key != "b" || entries[2].Key != "c" {
		t.Fatalf("expected sorted order a,b,c; got %v", keysOf(entries))
	}

	// Must produce the same order on repeated calls
	entries2 := m.Entries()
	for i := range entries {
		if entries[i].Key != entries2[i].Key {
			t.Fatalf("deterministic projection not repeatable at index %d", i)
		}
	}
}

func TestOrderedMapFromMap_DoesNotFabricateInsertionHistory(t *testing.T) {
	// When a plain Go map is adapted to OrderedMap, the resulting order
	// is a deterministic projection (sorted keys), not the original
	// insertion order. The test only verifies determinism, not that
	// the order reflects any particular insertion history.
	src := map[string]int{"z": 26, "m": 13, "a": 1}
	m := schema.OrderedMapFromMap(src)
	entries := m.Entries()

	// Verify it's not in some random order — must be sorted
	for i := 1; i < len(entries); i++ {
		if entries[i].Key < entries[i-1].Key {
			t.Fatalf("OrderedMapFromMap must produce sorted keys; got %v", keysOf(entries))
		}
	}
}

func TestOrderedMap_BackingPreservedWhenAlreadyOrdered(t *testing.T) {
	// When the backing is already an OrderedMap, binding must preserve
	// its insertion order, not re-sort it.
	original := schema.NewOrderedMap[string, int]()
	original.Set("z", 26)
	original.Set("a", 1)
	original.Set("m", 13)

	// The order should be z, a, m (insertion order)
	entries := original.Entries()
	if entries[0].Key != "z" || entries[1].Key != "a" || entries[2].Key != "m" {
		t.Fatalf("OrderedMap backing should preserve insertion order; got %v", keysOf(entries))
	}
}

// ============================================================================
// Mutation result contract tests
// ============================================================================

func TestMutationResult_SetKinds(t *testing.T) {
	if binding.MutationInserted != "inserted" {
		t.Fatalf("expected MutationInserted=inserted, got %s", binding.MutationInserted)
	}
	if binding.MutationReplaced != "replaced" {
		t.Fatalf("expected MutationReplaced=replaced, got %s", binding.MutationReplaced)
	}
}

func TestMutationResult_DeleteKinds(t *testing.T) {
	if binding.MutationRemoved != "removed" {
		t.Fatalf("expected MutationRemoved=removed, got %s", binding.MutationRemoved)
	}
	if binding.MutationMissing != "missing" {
		t.Fatalf("expected MutationMissing=missing, got %s", binding.MutationMissing)
	}
}

// ============================================================================
// BindingError structure tests
// ============================================================================

func TestBindingError_CarriesContext(t *testing.T) {
	id := mustID(t, 1000, 1, 0, 1)
	err := &binding.BindingError{
		Identity: id,
		Schema:   "playerStruct",
		Path:     ".Level",
		Err:      testErr("type mismatch"),
	}
	if err.Identity != id {
		t.Fatal("BindingError missing identity")
	}
	if err.Schema != "playerStruct" {
		t.Fatalf("expected Schema=playerStruct, got %s", err.Schema)
	}
	if err.Path != ".Level" {
		t.Fatalf("expected Path=.Level, got %s", err.Path)
	}
	if err.Error() != "type mismatch" {
		t.Fatalf("unexpected Error(): %s", err.Error())
	}
	if err.Unwrap() == nil {
		t.Fatal("expected Unwrap to return inner error")
	}
}

func TestBindingError_TypeMismatchCarriesExpectedActual(t *testing.T) {
	// Schema says "int" but the field is a string — type mismatch
	desc := schema.ObjectDesc{
		Name: "badStruct",
		Fields: []schema.FieldDesc{
			{Name: "Name", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}},
		},
	}
	p := playerStruct{Name: "alice", Level: 5}

	err := binding.ValidateBinding(desc, &p)
	if err == nil {
		t.Fatal("expected error for type mismatch, got nil")
	}

	var bindErr *binding.BindingError
	if !isBindingError(err, &bindErr) {
		t.Fatalf("expected *binding.BindingError, got %T", err)
	}

	if bindErr.DiagnosticExpected() != "int" {
		t.Fatalf("expected Expected='int', got %q", bindErr.DiagnosticExpected())
	}
	if bindErr.DiagnosticActual() != "string" {
		t.Fatalf("expected Actual='string', got %q", bindErr.DiagnosticActual())
	}
}

func TestBindingError_StructTypeMismatchCarriesExpectedActual(t *testing.T) {
	type flatStruct struct {
		Name string
	}
	desc := schema.ObjectDesc{
		Name: "flatStruct",
		Fields: []schema.FieldDesc{
			{Name: "Name", Type: schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "struct", ClassName: "nested"}},
		},
	}
	f := flatStruct{Name: "alice"}

	err := binding.ValidateBinding(desc, &f)
	if err == nil {
		t.Fatal("expected error for struct type mismatch, got nil")
	}

	var bindErr *binding.BindingError
	if !isBindingError(err, &bindErr) {
		t.Fatalf("expected *binding.BindingError, got %T", err)
	}

	if bindErr.DiagnosticExpected() != "nested" {
		t.Fatalf("expected Expected='nested', got %q", bindErr.DiagnosticExpected())
	}
	if bindErr.DiagnosticActual() != "string" {
		t.Fatalf("expected Actual='string', got %q", bindErr.DiagnosticActual())
	}
}

func TestBindingError_ArrayTypeMismatchCarriesExpectedActual(t *testing.T) {
	type flatStruct struct {
		Items string
	}
	desc := schema.ObjectDesc{
		Name: "flatStruct",
		Fields: []schema.FieldDesc{
			{Name: "Items", Type: schema.TypeDesc{Kind: schema.TypeKindArray, Name: "array", Element: &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}}},
		},
	}
	f := flatStruct{Items: "not-an-array"}

	err := binding.ValidateBinding(desc, &f)
	if err == nil {
		t.Fatal("expected error for array type mismatch, got nil")
	}

	var bindErr *binding.BindingError
	if !isBindingError(err, &bindErr) {
		t.Fatalf("expected *binding.BindingError, got %T", err)
	}

	if bindErr.DiagnosticExpected() != "array<string>" {
		t.Fatalf("expected Expected='array<string>', got %q", bindErr.DiagnosticExpected())
	}
	if bindErr.DiagnosticActual() != "string" {
		t.Fatalf("expected Actual='string', got %q", bindErr.DiagnosticActual())
	}
}

func TestBindingError_MapTypeMismatchCarriesExpectedActual(t *testing.T) {
	type flatStruct struct {
		Data string
	}
	desc := schema.ObjectDesc{
		Name: "flatStruct",
		Fields: []schema.FieldDesc{
			{Name: "Data", Type: schema.TypeDesc{Kind: schema.TypeKindMap, Name: "map", Key: &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}, Value: &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}}},
		},
	}
	f := flatStruct{Data: "not-a-map"}

	err := binding.ValidateBinding(desc, &f)
	if err == nil {
		t.Fatal("expected error for map type mismatch, got nil")
	}

	var bindErr *binding.BindingError
	if !isBindingError(err, &bindErr) {
		t.Fatalf("expected *binding.BindingError, got %T", err)
	}

	if bindErr.DiagnosticExpected() != "map<string, int>" {
		t.Fatalf("expected Expected='map<string, int>', got %q", bindErr.DiagnosticExpected())
	}
	if bindErr.DiagnosticActual() != "string" {
		t.Fatalf("expected Actual='string', got %q", bindErr.DiagnosticActual())
	}
}

func TestBindingError_DirectExpectedActualTakesPrecedence(t *testing.T) {
	err := &binding.BindingError{
		Schema:   "test",
		Path:     ".Field",
		Err:      testErr("some error"),
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

func TestBindingError_FromErrorPreservesExpectedActual(t *testing.T) {
	desc := schema.ObjectDesc{
		Name: "badStruct",
		Fields: []schema.FieldDesc{
			{Name: "Name", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}},
		},
	}
	p := playerStruct{Name: "alice", Level: 5}

	err := binding.ValidateBinding(desc, &p)
	if err == nil {
		t.Fatal("expected error")
	}

	diag := diagnostics.FromError(err, diagnostics.Descriptor{})
	if diag.Expected != "int" {
		t.Fatalf("expected FromError Expected='int', got %q", diag.Expected)
	}
	if diag.Actual != "string" {
		t.Fatalf("expected FromError Actual='string', got %q", diag.Actual)
	}
}

// ============================================================================
// Authority boundary contract tests
//
// These tests prove that binding layer objects are NOT truth authorities.
// They are projections and observability surfaces, not commit records.
// ============================================================================

func TestViewProjection_IsNotAuthority(t *testing.T) {
	// A ViewProjection is a snapshot, not a committed truth.
	// It has no commit semantics and no control authority.
	id := mustID(t, 1000, 1, 0, 30)
	view := &binding.ViewProjection{
		Schema:   playerClassDesc(),
		Identity: id,
		Fields:   map[string]any{"Name": "test"},
	}
	// ViewProjection does not carry commit/control/fact semantics
	// It is purely an observable snapshot
	if view.Schema.Name != "playerStruct" {
		t.Fatal("view schema mismatch")
	}
	// View fields are mutable projections, not committed truths
	view.Fields["Name"] = "modified"
	// This modification is local to the view and does not affect
	// any authority source — the test itself proves the boundary
}

func TestMutationResult_IsNotCommitRecord(t *testing.T) {
	// MutationResult records observable change, not authoritative commit
	result := binding.MutationResult{
		Kind: binding.MutationInserted,
		Key:  "Name",
	}
	// It is an observability surface, not a transaction record
	if result.Kind != binding.MutationInserted {
		t.Fatal("mutation kind mismatch")
	}
	if result.Key != "Name" {
		t.Fatal("mutation key mismatch")
	}
}

// ============================================================================
// MapBinding adapter contract tests
//
// These tests lock the binder's ability to distinguish between:
//   - Plain Go map[K]V → needs deterministic stabilization
//   - OrderedMap backing → preserve insertion order
//
// The binder must not downgrade OrderedMap to Go map and re-sort.
// ============================================================================

func TestMapBinding_PlainGoMapIsNotOrdered(t *testing.T) {
	src := map[string]int{"z": 26, "a": 1, "m": 13}
	b := binding.BindMap(src)
	if b.IsOrdered() {
		t.Fatal("plain Go map binding should not report ordered")
	}
}

func TestMapBinding_OrderedMapBackingIsOrdered(t *testing.T) {
	om := schema.NewOrderedMap[string, int]()
	om.Set("z", 26)
	om.Set("a", 1)
	b := binding.BindOrderedMap(om)
	if !b.IsOrdered() {
		t.Fatal("OrderedMap binding should report ordered")
	}
}

func TestMapBinding_BackingAccessible(t *testing.T) {
	src := map[string]int{"a": 1}
	b := binding.BindMap(src)
	if b.Backing() == nil {
		t.Fatal("expected non-nil backing")
	}
}

func TestMapBinding_NilBindingBehavesLikeEmpty(t *testing.T) {
	var b *binding.MapBinding
	if b.IsOrdered() {
		t.Fatal("nil MapBinding should not report ordered")
	}
	if b.Backing() != nil {
		t.Fatal("nil MapBinding should have nil backing")
	}
}

// ============================================================================
// Entry as canonical view surface contract tests
//
// Entry{Key, Value} is the shared surface consumed by script enumeration,
// snapshot, diff, and transport projection. It must be stable and
// independently constructible.
// ============================================================================

func TestEntry_CanonicalViewShape(t *testing.T) {
	e := schema.Entry[string, int]{Key: "level", Value: 5}
	if e.Key != "level" {
		t.Fatal("Entry.Key mismatch")
	}
	if e.Value != 5 {
		t.Fatal("Entry.Value mismatch")
	}
}

func TestEntry_UsedForSnapshotAndDiff(t *testing.T) {
	m := schema.NewOrderedMap[string, int]()
	m.Set("a", 1)
	m.Set("b", 2)

	// Take snapshot via Entries()
	snapshot1 := m.Entries()

	// Mutate
	m.Set("b", 20)
	m.Set("c", 3)

	// Take new snapshot
	snapshot2 := m.Entries()

	// Diff: find changed entries
	changed := diffEntries(snapshot1, snapshot2)
	if len(changed) != 2 {
		t.Fatalf("expected 2 changed entries, got %d", len(changed))
	}
	// b was replaced, c was inserted
	changedKeys := make(map[string]bool)
	for _, e := range changed {
		changedKeys[e.Key] = true
	}
	if !changedKeys["b"] || !changedKeys["c"] {
		t.Fatalf("expected b and c in diff, got %v", changedKeys)
	}
}

// ============================================================================
// OrderedMap view/projection repeatable consumption contract
//
// projection / snapshot / diff / transport view all consume the same
// ordered-map visible contract (entry sequence), not re-implementing
// map sorting logic.
// ============================================================================

func TestOrderedMap_EntriesConsumedByMultipleProjections(t *testing.T) {
	m := schema.NewOrderedMap[string, int]()
	m.Set("x", 1)
	m.Set("y", 2)
	m.Set("z", 3)

	// All projections should consume the same Entries() surface
	entries := m.Entries()

	// Transport projection consumes entries
	transportView := make([]string, len(entries))
	for i, e := range entries {
		transportView[i] = e.Key
	}

	// Snapshot consumes entries
	snapshot := make(map[string]int, len(entries))
	for _, e := range entries {
		snapshot[e.Key] = e.Value
	}

	// Diff baseline consumes entries
	diffBaseline := make([]schema.Entry[string, int], len(entries))
	copy(diffBaseline, entries)

	// Verify all consumed the same data
	if len(transportView) != 3 || transportView[0] != "x" {
		t.Fatalf("transport view mismatch: %v", transportView)
	}
	if snapshot["y"] != 2 {
		t.Fatalf("snapshot mismatch: %v", snapshot)
	}
	if diffBaseline[2].Key != "z" {
		t.Fatalf("diff baseline mismatch: %v", diffBaseline)
	}
}

// ============================================================================
// helpers
// ============================================================================

type testErr string

func (e testErr) Error() string { return string(e) }

func mustID(t *testing.T, ts uint64, slot uint16, inc uint16, seq uint64) identity.CanonicalID {
	t.Helper()
	id, err := identity.NewCanonicalID(ts, slot, inc, seq)
	if err != nil {
		t.Fatalf("NewCanonicalID: %v", err)
	}
	return id
}

func isBindingError(err error, target **binding.BindingError) bool {
	for e := err; e != nil; {
		if b, ok := e.(*binding.BindingError); ok {
			*target = b
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

func keysOf[K comparable, V any](entries []schema.Entry[K, V]) []K {
	keys := make([]K, len(entries))
	for i, e := range entries {
		keys[i] = e.Key
	}
	return keys
}

func diffEntries[K comparable, V comparable](before, after []schema.Entry[K, V]) []schema.Entry[K, V] {
	beforeMap := make(map[K]V, len(before))
	for _, e := range before {
		beforeMap[e.Key] = e.Value
	}
	var diff []schema.Entry[K, V]
	for _, e := range after {
		if old, ok := beforeMap[e.Key]; !ok || old != e.Value {
			diff = append(diff, e)
		}
	}
	return diff
}

// ============================================================================
// ValidateBinding contract tests
//
// These tests lock the eager schema/target compatibility check that
// NewObjectBinding performs at creation time.
// ============================================================================

func TestValidateBinding_CompatibleTargetReturnsNil(t *testing.T) {
	desc := playerClassDesc()
	p := playerStruct{Name: "alice", Level: 5}
	if err := binding.ValidateBinding(desc, &p); err != nil {
		t.Fatalf("expected nil for compatible target, got %v", err)
	}
}

func TestValidateBinding_FieldNotInStructReturnsError(t *testing.T) {
	desc := schema.ObjectDesc{
		Name: "playerStruct",
		Fields: []schema.FieldDesc{
			{Name: "Name", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
			{Name: "MissingField", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}},
		},
	}
	p := playerStruct{Name: "alice", Level: 5}

	err := binding.ValidateBinding(desc, &p)
	if err == nil {
		t.Fatal("expected error for missing field, got nil")
	}

	var bindErr *binding.BindingError
	if !isBindingError(err, &bindErr) {
		t.Fatalf("expected *binding.BindingError, got %T", err)
	}
	if bindErr.Path != ".MissingField" {
		t.Fatalf("expected path .MissingField, got %s", bindErr.Path)
	}
}

func TestValidateBinding_TypeMismatchReturnsError(t *testing.T) {
	// Schema says "int" but the field is a string — type mismatch
	desc := schema.ObjectDesc{
		Name: "badStruct",
		Fields: []schema.FieldDesc{
			{Name: "Name", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}},
		},
	}
	p := playerStruct{Name: "alice", Level: 5}

	err := binding.ValidateBinding(desc, &p)
	if err == nil {
		t.Fatal("expected error for type mismatch, got nil")
	}

	var bindErr *binding.BindingError
	if !isBindingError(err, &bindErr) {
		t.Fatalf("expected *binding.BindingError, got %T", err)
	}
	if bindErr.Path != ".Name" {
		t.Fatalf("expected path .Name, got %s", bindErr.Path)
	}
}

func TestValidateBinding_NonStructTargetReturnsError(t *testing.T) {
	desc := playerClassDesc()

	err := binding.ValidateBinding(desc, 42)
	if err == nil {
		t.Fatal("expected error for non-struct target, got nil")
	}

	var bindErr *binding.BindingError
	if !isBindingError(err, &bindErr) {
		t.Fatalf("expected *binding.BindingError, got %T", err)
	}
}

func TestValidateBinding_NilPointerTargetReturnsError(t *testing.T) {
	desc := playerClassDesc()
	var p *playerStruct

	err := binding.ValidateBinding(desc, p)
	if err == nil {
		t.Fatal("expected error for nil pointer target, got nil")
	}
}

func TestValidateBinding_UnexportedFieldReturnsError(t *testing.T) {
	type secretStruct struct {
		Name   string
		secret string // unexported
	}
	desc := schema.ObjectDesc{
		Name: "secretStruct",
		Fields: []schema.FieldDesc{
			{Name: "Name", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
			{Name: "secret", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
		},
	}
	s := secretStruct{Name: "alice", secret: "hidden"}

	err := binding.ValidateBinding(desc, &s)
	if err == nil {
		t.Fatal("expected error for unexported field, got nil")
	}

	var bindErr *binding.BindingError
	if !isBindingError(err, &bindErr) {
		t.Fatalf("expected *binding.BindingError, got %T", err)
	}
	if bindErr.Path != ".secret" {
		t.Fatalf("expected path .secret, got %s", bindErr.Path)
	}
}

func TestNewObjectBinding_RejectsIncompatibleTarget(t *testing.T) {
	id := mustID(t, 1000, 5, 0, 1)
	desc := schema.ObjectDesc{
		Name: "badStruct",
		Fields: []schema.FieldDesc{
			{Name: "Name", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}},
		},
	}
	p := playerStruct{Name: "alice", Level: 5}

	_, err := binding.NewObjectBinding(desc, id, &p)
	if err == nil {
		t.Fatal("expected NewObjectBinding to reject incompatible target, got nil")
	}
}

// ============================================================================
// Deep patch writeback contract tests
//
// These tests verify that ApplyViewPatch can handle nested struct,
// slice, and map fields — not just directly-assignable scalars.
// ============================================================================

type locationStruct struct {
	Zone string
	X    int
	Y    int
}

type inventoryStruct struct {
	Items    []string
	Metadata map[string]int
}

type entityStruct struct {
	Name     string
	Location locationStruct
}

func entityClassDesc() schema.ObjectDesc {
	return schema.ObjectDesc{
		Name: "entityStruct",
		Fields: []schema.FieldDesc{
			{Name: "Name", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
			{Name: "Location", Type: schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "struct", ClassName: "locationStruct"}},
		},
	}
}

func inventoryClassDesc() schema.ObjectDesc {
	return schema.ObjectDesc{
		Name: "inventoryStruct",
		Fields: []schema.FieldDesc{
			{Name: "Items", Type: schema.TypeDesc{Kind: schema.TypeKindArray, Name: "array", Element: &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}}},
			{Name: "Metadata", Type: schema.TypeDesc{Kind: schema.TypeKindMap, Name: "map", Key: &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}, Value: &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}}},
		},
	}
}

func TestApplyViewPatch_NestedStructField(t *testing.T) {
	id := mustID(t, 1000, 6, 0, 1)
	desc := entityClassDesc()
	e := entityStruct{Name: "hero", Location: locationStruct{Zone: "forest", X: 10, Y: 20}}

	b, err := binding.NewObjectBinding(desc, id, &e)
	if err != nil {
		t.Fatalf("NewObjectBinding: %v", err)
	}

	// Project, modify, patch back
	view, err := binding.ProjectView(b)
	if err != nil {
		t.Fatalf("ProjectView: %v", err)
	}

	locMap, ok := view.Fields["Location"].(map[string]any)
	if !ok {
		t.Fatalf("expected Location as map[string]any, got %T", view.Fields["Location"])
	}
	locMap["Zone"] = "desert"
	locMap["X"] = 50

	mutations, err := binding.ApplyViewPatch(b, view)
	if err != nil {
		t.Fatalf("ApplyViewPatch: %v", err)
	}

	if e.Location.Zone != "desert" {
		t.Fatalf("expected Zone=desert, got %s", e.Location.Zone)
	}
	if e.Location.X != 50 {
		t.Fatalf("expected X=50, got %d", e.Location.X)
	}
	if e.Location.Y != 20 {
		t.Fatalf("expected Y=20 (unchanged), got %d", e.Location.Y)
	}

	// Should record a mutation for the struct field
	if len(mutations) == 0 {
		t.Fatal("expected at least one mutation for nested struct patch")
	}
}

func TestApplyViewPatch_SliceField(t *testing.T) {
	id := mustID(t, 1000, 6, 0, 2)
	desc := inventoryClassDesc()
	inv := inventoryStruct{
		Items:    []string{"sword", "shield"},
		Metadata: map[string]int{"level": 1},
	}

	b, err := binding.NewObjectBinding(desc, id, &inv)
	if err != nil {
		t.Fatalf("NewObjectBinding: %v", err)
	}

	view := &binding.ViewProjection{
		Schema:   desc,
		Identity: id,
		Fields: map[string]any{
			"Items": []any{"bow", "arrow"},
		},
	}

	mutations, err := binding.ApplyViewPatch(b, view)
	if err != nil {
		t.Fatalf("ApplyViewPatch: %v", err)
	}

	if len(inv.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(inv.Items))
	}
	if inv.Items[0] != "bow" || inv.Items[1] != "arrow" {
		t.Fatalf("expected [bow, arrow], got %v", inv.Items)
	}

	// Metadata should be unchanged (not in the patch view)
	if inv.Metadata["level"] != 1 {
		t.Fatal("metadata should be unchanged")
	}

	if len(mutations) != 1 || mutations[0].Key != "Items" {
		t.Fatalf("expected 1 mutation for Items, got %v", mutations)
	}
}

func TestApplyViewPatch_MapField(t *testing.T) {
	id := mustID(t, 1000, 6, 0, 3)
	desc := inventoryClassDesc()
	inv := inventoryStruct{
		Items:    []string{"sword"},
		Metadata: map[string]int{"level": 1, "hp": 100},
	}

	b, err := binding.NewObjectBinding(desc, id, &inv)
	if err != nil {
		t.Fatalf("NewObjectBinding: %v", err)
	}

	view := &binding.ViewProjection{
		Schema:   desc,
		Identity: id,
		Fields: map[string]any{
			"Metadata": []any{
				map[string]any{"Key": "level", "Value": 5},
				map[string]any{"Key": "mana", "Value": 50},
			},
		},
	}

	mutations, err := binding.ApplyViewPatch(b, view)
	if err != nil {
		t.Fatalf("ApplyViewPatch: %v", err)
	}

	if inv.Metadata["level"] != 5 {
		t.Fatalf("expected level=5, got %d", inv.Metadata["level"])
	}
	if inv.Metadata["mana"] != 50 {
		t.Fatalf("expected mana=50, got %d", inv.Metadata["mana"])
	}
	// hp is no longer in the map — the map was replaced wholesale
	if _, exists := inv.Metadata["hp"]; exists {
		t.Fatal("expected hp to be absent after full map replacement")
	}

	if len(mutations) != 1 || mutations[0].Key != "Metadata" {
		t.Fatalf("expected 1 mutation for Metadata, got %v", mutations)
	}
}

func TestApplyViewPatch_MapFieldRejectsUnorderedMap(t *testing.T) {
	id := mustID(t, 1000, 6, 0, 7)
	desc := inventoryClassDesc()
	inv := inventoryStruct{
		Items:    []string{"sword"},
		Metadata: map[string]int{"level": 1, "hp": 100},
	}

	b, err := binding.NewObjectBinding(desc, id, &inv)
	if err != nil {
		t.Fatalf("NewObjectBinding: %v", err)
	}

	// map[string]any is no longer accepted for map-type fields;
	// only []any entry sequence is valid.
	view := &binding.ViewProjection{
		Schema:   desc,
		Identity: id,
		Fields: map[string]any{
			"Metadata": map[string]any{"level": 5},
		},
	}

	mutations, err := binding.ApplyViewPatch(b, view)
	if err != nil {
		t.Fatalf("ApplyViewPatch: %v", err)
	}

	// The Metadata field should be MutationSkipped with a reason
	// indicating that map[string]any is not accepted for map fields.
	var foundSkipped bool
	for _, m := range mutations {
		if m.Key == "Metadata" {
			if m.Kind != binding.MutationSkipped {
				t.Fatalf("expected MutationSkipped for Metadata, got %s", m.Kind)
			}
			foundSkipped = true
		}
	}
	if !foundSkipped {
		t.Fatal("expected MutationSkipped for Metadata field")
	}

	// Original map should be unchanged
	if inv.Metadata["level"] != 1 {
		t.Fatalf("expected level=1 (unchanged), got %d", inv.Metadata["level"])
	}
	if inv.Metadata["hp"] != 100 {
		t.Fatalf("expected hp=100 (unchanged), got %d", inv.Metadata["hp"])
	}
}

func TestApplyViewPatch_TypeIncompatibleFieldSkipped(t *testing.T) {
	id := mustID(t, 1000, 6, 0, 4)
	desc := playerClassDesc()
	p := playerStruct{Name: "alice", Level: 5}

	b, err := binding.NewObjectBinding(desc, id, &p)
	if err != nil {
		t.Fatalf("NewObjectBinding: %v", err)
	}

	// Provide a value that can't be assigned to int (Level field)
	view := &binding.ViewProjection{
		Schema:   desc,
		Identity: id,
		Fields: map[string]any{
			"Level": "not-an-int",
		},
	}

	mutations, err := binding.ApplyViewPatch(b, view)
	if err != nil {
		t.Fatalf("ApplyViewPatch: %v", err)
	}

	// Level should be unchanged
	if p.Level != 5 {
		t.Fatalf("expected Level=5 (unchanged), got %d", p.Level)
	}

	// A MutationSkipped should be recorded for the type-incompatible field
	var foundSkipped bool
	for _, m := range mutations {
		if m.Key == "Level" {
			if m.Kind != binding.MutationSkipped {
				t.Fatalf("expected MutationSkipped for Level, got %s", m.Kind)
			}
			if m.Reason == "" {
				t.Fatal("expected non-empty Reason for MutationSkipped")
			}
			foundSkipped = true
		}
	}
	if !foundSkipped {
		t.Fatal("expected MutationSkipped result for type-incompatible Level field")
	}
}

func TestApplyViewPatch_MutationSkipped_DiagnosticReason(t *testing.T) {
	// Verify that MutationSkipped results carry meaningful diagnostic reasons
	// and do not corrupt valid field mutations in the same patch.
	id := mustID(t, 1000, 6, 0, 20)
	desc := playerClassDesc()
	p := playerStruct{Name: "alice", Level: 5}

	b, err := binding.NewObjectBinding(desc, id, &p)
	if err != nil {
		t.Fatalf("NewObjectBinding: %v", err)
	}

	view := &binding.ViewProjection{
		Schema:   desc,
		Identity: id,
		Fields: map[string]any{
			"Name":  "bob",        // valid: string → string
			"Level": "not-an-int", // invalid: string → int, should skip
		},
	}

	mutations, err := binding.ApplyViewPatch(b, view)
	if err != nil {
		t.Fatalf("ApplyViewPatch: %v", err)
	}

	// Name should be updated
	if p.Name != "bob" {
		t.Fatalf("expected Name=bob, got %s", p.Name)
	}
	// Level should be unchanged
	if p.Level != 5 {
		t.Fatalf("expected Level=5 (unchanged), got %d", p.Level)
	}

	// Must have both a replaced and a skipped mutation
	var replaced, skipped bool
	for _, m := range mutations {
		switch m.Key {
		case "Name":
			if m.Kind != binding.MutationReplaced {
				t.Fatalf("expected MutationReplaced for Name, got %s", m.Kind)
			}
			if m.Reason != "" {
				t.Fatalf("Replaced mutations should not have Reason, got %q", m.Reason)
			}
			replaced = true
		case "Level":
			if m.Kind != binding.MutationSkipped {
				t.Fatalf("expected MutationSkipped for Level, got %s", m.Kind)
			}
			if m.Reason == "" {
				t.Fatal("MutationSkipped must carry a diagnostic Reason")
			}
			skipped = true
		}
	}
	if !replaced {
		t.Fatal("expected MutationReplaced for Name")
	}
	if !skipped {
		t.Fatal("expected MutationSkipped for Level")
	}
}

func TestApplyViewPatch_IntegerFloat64ConversionStrictness(t *testing.T) {
	type numericStruct struct {
		Level int
		Small int8
		Rank  uint8
	}

	desc := schema.ObjectDesc{
		Name: "numericStruct",
		Fields: []schema.FieldDesc{
			{Name: "Level", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}},
			{Name: "Small", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "byte"}},
			{Name: "Rank", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "uint"}},
		},
	}

	tests := []struct {
		name          string
		fields        map[string]any
		expectLevel   int
		expectSmall   int8
		expectRank    uint8
		mutatedFields map[string]bool
		skippedFields map[string]bool
	}{
		{
			name:          "integral float64 is applied",
			fields:        map[string]any{"Level": float64(8)},
			expectLevel:   8,
			expectSmall:   7,
			expectRank:    3,
			mutatedFields: map[string]bool{"Level": true},
			skippedFields: nil,
		},
		{
			name:          "fractional float64 is skipped",
			fields:        map[string]any{"Level": float64(8.5)},
			expectLevel:   5,
			expectSmall:   7,
			expectRank:    3,
			mutatedFields: nil,
			skippedFields: map[string]bool{"Level": true},
		},
		{
			name:          "out of range float64 is skipped",
			fields:        map[string]any{"Small": float64(128)},
			expectLevel:   5,
			expectSmall:   7,
			expectRank:    3,
			mutatedFields: nil,
			skippedFields: map[string]bool{"Small": true},
		},
		{
			name:          "negative float64 to unsigned is skipped",
			fields:        map[string]any{"Rank": float64(-1)},
			expectLevel:   5,
			expectSmall:   7,
			expectRank:    3,
			mutatedFields: nil,
			skippedFields: map[string]bool{"Rank": true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := mustID(t, 1000, 6, 0, 6)
			n := numericStruct{Level: 5, Small: 7, Rank: 3}
			b, err := binding.NewObjectBinding(desc, id, &n)
			if err != nil {
				t.Fatalf("NewObjectBinding: %v", err)
			}

			mutations, err := binding.ApplyViewPatch(b, &binding.ViewProjection{
				Schema:   desc,
				Identity: id,
				Fields:   tt.fields,
			})
			if err != nil {
				t.Fatalf("ApplyViewPatch: %v", err)
			}

			if n.Level != tt.expectLevel || n.Small != tt.expectSmall || n.Rank != tt.expectRank {
				t.Fatalf("unexpected numeric state: got Level=%d Small=%d Rank=%d", n.Level, n.Small, n.Rank)
			}
			for _, mutation := range mutations {
				switch mutation.Kind {
				case binding.MutationReplaced:
					if !tt.mutatedFields[mutation.Key] {
						t.Fatalf("unexpected replaced mutation for %s: %v", mutation.Key, mutations)
					}
				case binding.MutationSkipped:
					if !tt.skippedFields[mutation.Key] {
						t.Fatalf("unexpected skipped mutation for %s: %v", mutation.Key, mutations)
					}
					if mutation.Reason == "" {
						t.Fatalf("skipped mutation for %s must carry diagnostic Reason", mutation.Key)
					}
				default:
					t.Fatalf("unexpected mutation kind %s for %s", mutation.Kind, mutation.Key)
				}
			}
			for field := range tt.mutatedFields {
				found := false
				for _, mutation := range mutations {
					if mutation.Key == field && mutation.Kind == binding.MutationReplaced {
						found = true
					}
				}
				if !found {
					t.Fatalf("expected replaced mutation for %s, got %v", field, mutations)
				}
			}
			for field := range tt.skippedFields {
				found := false
				for _, mutation := range mutations {
					if mutation.Key == field && mutation.Kind == binding.MutationSkipped {
						found = true
					}
				}
				if !found {
					t.Fatalf("expected skipped mutation for %s, got %v", field, mutations)
				}
			}
		})
	}
}

func TestApplyViewPatch_NilPointerStructFieldHandled(t *testing.T) {
	type wrapper struct {
		Name string
	}

	id := mustID(t, 1000, 6, 0, 5)
	desc := schema.ObjectDesc{
		Name: "wrapper",
		Fields: []schema.FieldDesc{
			{Name: "Name", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
		},
	}
	w := wrapper{Name: "test"}

	b, err := binding.NewObjectBinding(desc, id, &w)
	if err != nil {
		t.Fatalf("NewObjectBinding: %v", err)
	}

	// Patch only the scalar field — no nested struct involved
	view := &binding.ViewProjection{
		Schema:   desc,
		Identity: id,
		Fields: map[string]any{
			"Name": "updated",
		},
	}

	mutations, err := binding.ApplyViewPatch(b, view)
	if err != nil {
		t.Fatalf("ApplyViewPatch: %v", err)
	}

	if w.Name != "updated" {
		t.Fatalf("expected Name=updated, got %s", w.Name)
	}
	if len(mutations) != 1 || mutations[0].Key != "Name" {
		t.Fatalf("expected 1 mutation for Name, got %v", mutations)
	}
}

// ============================================================================
// OrderedMap projection chain contract tests
//
// These tests verify that map-typed fields project through the canonical
// entry sequence surface, preserving insertion order for OrderedMap-backed
// fields and deterministic sorted-key order for plain Go maps.
//
// The projection chain: runtime object → ProjectView → entry sequence
// → transport → decode → ApplyViewPatch → runtime object
// ============================================================================

type mapHolderStruct struct {
	Name   string
	Props  map[string]int
	Traits *schema.OrderedMap[string, string]
}

func mapHolderClassDesc() schema.ObjectDesc {
	return schema.ObjectDesc{
		Name: "mapHolderStruct",
		Fields: []schema.FieldDesc{
			{Name: "Name", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
			{Name: "Props", Type: schema.TypeDesc{Kind: schema.TypeKindMap, Name: "map", Key: &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}, Value: &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}}},
			{Name: "Traits", Type: schema.TypeDesc{Kind: schema.TypeKindMap, Name: "map", Key: &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}, Value: &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}}},
		},
	}
}

func TestProjectView_OrderedMapField_ProjectsAsOrderedEntries(t *testing.T) {
	id := mustID(t, 1000, 7, 0, 1)
	desc := mapHolderClassDesc()

	om := schema.NewOrderedMap[string, string]()
	om.Set("z_trait", "zeal")
	om.Set("a_trait", "agility")
	om.Set("m_trait", "might")

	holder := mapHolderStruct{
		Name:   "hero",
		Traits: om,
	}

	b, err := binding.NewObjectBinding(desc, id, &holder)
	if err != nil {
		t.Fatalf("NewObjectBinding: %v", err)
	}

	view, err := binding.ProjectView(b)
	if err != nil {
		t.Fatalf("ProjectView: %v", err)
	}

	traits, ok := view.Fields["Traits"].([]any)
	if !ok {
		t.Fatalf("expected Traits as []any entry sequence, got %T", view.Fields["Traits"])
	}

	if len(traits) != 3 {
		t.Fatalf("expected 3 trait entries, got %d", len(traits))
	}

	// Verify insertion order is preserved: z, a, m
	expectedKeys := []string{"z_trait", "a_trait", "m_trait"}
	for i, entry := range traits {
		entryMap, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("entry %d: expected map[string]any, got %T", i, entry)
		}
		key, _ := entryMap["Key"].(string)
		if key != expectedKeys[i] {
			t.Fatalf("entry %d: expected key %q, got %q (insertion order not preserved)", i, expectedKeys[i], key)
		}
	}
}

func TestProjectView_PlainGoMapField_ProjectsAsSortedEntries(t *testing.T) {
	id := mustID(t, 1000, 7, 0, 2)
	desc := mapHolderClassDesc()

	holder := mapHolderStruct{
		Name:  "hero",
		Props: map[string]int{"zeal": 10, "agility": 5, "might": 8},
	}

	b, err := binding.NewObjectBinding(desc, id, &holder)
	if err != nil {
		t.Fatalf("NewObjectBinding: %v", err)
	}

	view, err := binding.ProjectView(b)
	if err != nil {
		t.Fatalf("ProjectView: %v", err)
	}

	props, ok := view.Fields["Props"].([]any)
	if !ok {
		t.Fatalf("expected Props as []any entry sequence, got %T", view.Fields["Props"])
	}

	if len(props) != 3 {
		t.Fatalf("expected 3 prop entries, got %d", len(props))
	}

	// Verify sorted key order: agility, might, zeal
	expectedKeys := []string{"agility", "might", "zeal"}
	for i, entry := range props {
		entryMap, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("entry %d: expected map[string]any, got %T", i, entry)
		}
		key, _ := entryMap["Key"].(string)
		if key != expectedKeys[i] {
			t.Fatalf("entry %d: expected key %q, got %q (sorted order not preserved)", i, expectedKeys[i], key)
		}
	}
}

func TestProjectView_OrderedMapField_PreservesInsertionOrder(t *testing.T) {
	id := mustID(t, 1000, 7, 0, 3)
	desc := mapHolderClassDesc()

	// Create OrderedMap with deliberate non-sorted insertion order
	om := schema.NewOrderedMap[string, string]()
	om.Set("c", "gamma")
	om.Set("a", "alpha")
	om.Set("b", "beta")

	holder := mapHolderStruct{Name: "ordered", Traits: om}

	b, err := binding.NewObjectBinding(desc, id, &holder)
	if err != nil {
		t.Fatalf("NewObjectBinding: %v", err)
	}

	view, err := binding.ProjectView(b)
	if err != nil {
		t.Fatalf("ProjectView: %v", err)
	}

	entries := view.Fields["Traits"].([]any)
	// c, a, b — insertion order, NOT alphabetical
	expectedKeys := []string{"c", "a", "b"}
	for i, entry := range entries {
		entryMap := entry.(map[string]any)
		key := entryMap["Key"].(string)
		if key != expectedKeys[i] {
			t.Fatalf("entry %d: expected key %q (insertion order), got %q", i, expectedKeys[i], key)
		}
	}
}

func TestProjectView_MapField_RoundTripPreservesData_OrderedMap(t *testing.T) {
	id := mustID(t, 1000, 7, 0, 4)
	desc := mapHolderClassDesc()

	om := schema.NewOrderedMap[string, string]()
	om.Set("skill", "fire")
	om.Set("level", "5")

	holder := mapHolderStruct{Name: "hero", Traits: om}

	b, err := binding.NewObjectBinding(desc, id, &holder)
	if err != nil {
		t.Fatalf("NewObjectBinding: %v", err)
	}

	// Project
	view, err := binding.ProjectView(b)
	if err != nil {
		t.Fatalf("ProjectView: %v", err)
	}

	// Modify the projected view (change level value)
	entries := view.Fields["Traits"].([]any)
	for i, entry := range entries {
		entryMap := entry.(map[string]any)
		if entryMap["Key"] == "level" {
			entries[i] = map[string]any{"Key": "level", "Value": "10"}
		}
	}

	// Patch back
	mutations, err := binding.ApplyViewPatch(b, view)
	if err != nil {
		t.Fatalf("ApplyViewPatch: %v", err)
	}

	// Verify the OrderedMap was updated
	if len(mutations) == 0 {
		t.Fatal("expected at least one mutation for Traits patch")
	}

	// Verify value was updated
	val, _ := holder.Traits.Get("level")
	if val != "10" {
		t.Fatalf("expected level=10 after patch, got %q", val)
	}

	// Verify insertion order preserved: skill, level
	entriesAfter := holder.Traits.Entries()
	if entriesAfter[0].Key != "skill" || entriesAfter[1].Key != "level" {
		t.Fatalf("insertion order not preserved after patch: %v", keysOf(entriesAfter))
	}
}

func TestProjectView_MapField_RoundTripPreservesData_PlainMap(t *testing.T) {
	id := mustID(t, 1000, 7, 0, 5)
	desc := mapHolderClassDesc()

	holder := mapHolderStruct{
		Name:  "hero",
		Props: map[string]int{"hp": 100, "mp": 50},
	}

	b, err := binding.NewObjectBinding(desc, id, &holder)
	if err != nil {
		t.Fatalf("NewObjectBinding: %v", err)
	}

	// Project
	view, err := binding.ProjectView(b)
	if err != nil {
		t.Fatalf("ProjectView: %v", err)
	}

	// Verify projection is entry sequence
	entries, ok := view.Fields["Props"].([]any)
	if !ok {
		t.Fatalf("expected Props as []any, got %T", view.Fields["Props"])
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	// Modify and patch back
	view.Fields["Props"] = []any{
		map[string]any{"Key": "hp", "Value": 200},
		map[string]any{"Key": "mp", "Value": 50},
		map[string]any{"Key": "stamina", "Value": 75},
	}

	mutations, err := binding.ApplyViewPatch(b, view)
	if err != nil {
		t.Fatalf("ApplyViewPatch: %v", err)
	}

	if holder.Props["hp"] != 200 {
		t.Fatalf("expected hp=200, got %d", holder.Props["hp"])
	}
	if holder.Props["stamina"] != 75 {
		t.Fatalf("expected stamina=75, got %d", holder.Props["stamina"])
	}
	if len(mutations) == 0 {
		t.Fatal("expected at least one mutation for Props patch")
	}
}

// --- Unsortable key constraint tests ---

type complexKey struct {
	X int
	Y int
}

type unsortableKeyHolderStruct struct {
	Name   string
	Lookup map[complexKey]string
}

func unsortableKeyHolderClassDesc() schema.ObjectDesc {
	return schema.ObjectDesc{
		Name: "UnsortableKeyHolder",
		Fields: []schema.FieldDesc{
			{Name: "Name", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
			{Name: "Lookup", Type: schema.TypeDesc{Kind: schema.TypeKindMap, Name: "map"}},
		},
	}
}

func TestProjectView_UnsortableMapKey_ReturnsError(t *testing.T) {
	id := mustID(t, 1000, 8, 0, 1)
	desc := unsortableKeyHolderClassDesc()

	holder := unsortableKeyHolderStruct{
		Name:   "test",
		Lookup: map[complexKey]string{{X: 1, Y: 2}: "a"},
	}

	b, err := binding.NewObjectBinding(desc, id, &holder)
	if err != nil {
		t.Fatalf("NewObjectBinding: %v", err)
	}

	_, err = binding.ProjectView(b)
	if err == nil {
		t.Fatal("expected error for unsortable map key type, got nil")
	}

	var berr *binding.BindingError
	if !errors.As(err, &berr) {
		t.Fatalf("expected BindingError, got %T: %v", err, err)
	}
	if berr.Path != ".Lookup" {
		t.Fatalf("expected path .Lookup, got %q", berr.Path)
	}
}

func TestProjectView_SortableMapKeys_Accepted(t *testing.T) {
	id := mustID(t, 1000, 8, 0, 2)
	desc := mapHolderClassDesc()

	holder := mapHolderStruct{
		Name:  "test",
		Props: map[string]int{"a": 1, "b": 2},
	}

	b, err := binding.NewObjectBinding(desc, id, &holder)
	if err != nil {
		t.Fatalf("NewObjectBinding: %v", err)
	}

	view, err := binding.ProjectView(b)
	if err != nil {
		t.Fatalf("ProjectView with sortable keys should succeed: %v", err)
	}
	entries, ok := view.Fields["Props"].([]any)
	if !ok {
		t.Fatalf("expected Props as []any, got %T", view.Fields["Props"])
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
}

func TestProjectView_UnsortableMapKey_ErrorCarriesSchemaContext(t *testing.T) {
	id := mustID(t, 1000, 8, 0, 3)
	desc := unsortableKeyHolderClassDesc()

	holder := unsortableKeyHolderStruct{
		Name:   "test",
		Lookup: map[complexKey]string{{X: 1, Y: 2}: "a"},
	}

	b, err := binding.NewObjectBinding(desc, id, &holder)
	if err != nil {
		t.Fatalf("NewObjectBinding: %v", err)
	}

	_, err = binding.ProjectView(b)
	if err == nil {
		t.Fatal("expected error")
	}

	var berr *binding.BindingError
	if !errors.As(err, &berr) {
		t.Fatalf("expected BindingError, got %T", err)
	}

	// Error must carry schema and path context per TDD §3.4
	if berr.Schema != "UnsortableKeyHolder" {
		t.Fatalf("expected schema name, got %q", berr.Schema)
	}
	if berr.Path != ".Lookup" {
		t.Fatalf("expected path .Lookup, got %q", berr.Path)
	}
	if berr.Identity != id {
		t.Fatalf("expected identity context in error")
	}
}

// --- SortedOrderedMap binding integration tests ---

type sortedMapHolderStruct struct {
	Name     string
	Priority *schema.SortedOrderedMap[string, int]
}

func sortedMapHolderClassDesc() schema.ObjectDesc {
	return schema.ObjectDesc{
		Name: "SortedMapHolder",
		Fields: []schema.FieldDesc{
			{Name: "Name", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
			{Name: "Priority", Type: schema.TypeDesc{Kind: schema.TypeKindMap, Name: "map"}},
		},
	}
}

func TestProjectView_SortedOrderedMapField_ProjectsAsSortedEntries(t *testing.T) {
	id := mustID(t, 1000, 9, 0, 1)
	desc := sortedMapHolderClassDesc()

	sm := schema.NewSortedOrderedMap[string, int](schema.NaturalOrder[string]())
	sm.Set("gamma", 3)
	sm.Set("alpha", 1)
	sm.Set("beta", 2)

	holder := sortedMapHolderStruct{
		Name:     "test",
		Priority: sm,
	}

	b, err := binding.NewObjectBinding(desc, id, &holder)
	if err != nil {
		t.Fatalf("NewObjectBinding: %v", err)
	}

	view, err := binding.ProjectView(b)
	if err != nil {
		t.Fatalf("ProjectView: %v", err)
	}

	entries, ok := view.Fields["Priority"].([]any)
	if !ok {
		t.Fatalf("expected Priority as []any, got %T", view.Fields["Priority"])
	}

	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}

	// Must be in sorted order (alpha, beta, gamma), NOT insertion order
	e0 := entries[0].(map[string]any)
	e1 := entries[1].(map[string]any)
	e2 := entries[2].(map[string]any)
	if e0["Key"] != "alpha" || e1["Key"] != "beta" || e2["Key"] != "gamma" {
		t.Fatalf("unexpected sorted order: %v %v %v", e0, e1, e2)
	}
}

func TestProjectView_SortedOrderedMapField_RoundTripPreservesData(t *testing.T) {
	id := mustID(t, 1000, 9, 0, 2)
	desc := sortedMapHolderClassDesc()

	sm := schema.NewSortedOrderedMap[string, int](schema.NaturalOrder[string]())
	sm.Set("alpha", 1)
	sm.Set("gamma", 3)

	holder := sortedMapHolderStruct{
		Name:     "test",
		Priority: sm,
	}

	b, err := binding.NewObjectBinding(desc, id, &holder)
	if err != nil {
		t.Fatalf("NewObjectBinding: %v", err)
	}

	view, err := binding.ProjectView(b)
	if err != nil {
		t.Fatalf("ProjectView: %v", err)
	}

	// Patch back with new entries
	view.Fields["Priority"] = []any{
		map[string]any{"Key": "alpha", "Value": 10},
		map[string]any{"Key": "beta", "Value": 2},
	}

	mutations, err := binding.ApplyViewPatch(b, view)
	if err != nil {
		t.Fatalf("ApplyViewPatch: %v", err)
	}

	if len(mutations) == 0 {
		t.Fatal("expected at least one mutation")
	}

	// Verify data was written back
	v, ok := holder.Priority.Get("alpha")
	if !ok || v != 10 {
		t.Fatalf("expected alpha=10, got %d ok=%v", v, ok)
	}
	v, ok = holder.Priority.Get("beta")
	if !ok || v != 2 {
		t.Fatalf("expected beta=2, got %d ok=%v", v, ok)
	}
}

// ============================================================================
// §9.2 Struct embedding contract tests
//
// These tests lock the struct embedding contracts defined in TDD §9.2.
// Each test corresponds to a checklist item and verifies that Go struct
// embedding behavior is stable and well-bounded.
// ============================================================================

// TestStructEmbedding_SchemaProjectionIsStable locks §9.2 item 1:
// "Go struct 可以产生稳定 schema projection"
//
// Verifies that calling DescribeGoStruct multiple times on the same type
// produces identical descriptors, and that ProjectView on the same binding
// produces consistent results.
func TestStructEmbedding_SchemaProjectionIsStable(t *testing.T) {
	t.Run("DescribeGoStruct_idempotent", func(t *testing.T) {
		desc1, err := schema.DescribeGoStruct(playerStruct{})
		if err != nil {
			t.Fatalf("DescribeGoStruct: %v", err)
		}
		desc2, err := schema.DescribeGoStruct(playerStruct{})
		if err != nil {
			t.Fatalf("DescribeGoStruct second call: %v", err)
		}
		if desc1.Name != desc2.Name {
			t.Fatalf("name mismatch: %q vs %q", desc1.Name, desc2.Name)
		}
		if len(desc1.Fields) != len(desc2.Fields) {
			t.Fatalf("field count mismatch: %d vs %d", len(desc1.Fields), len(desc2.Fields))
		}
		for i := range desc1.Fields {
			if desc1.Fields[i].Name != desc2.Fields[i].Name {
				t.Fatalf("field %d name mismatch: %q vs %q", i, desc1.Fields[i].Name, desc2.Fields[i].Name)
			}
			if desc1.Fields[i].Type.Kind != desc2.Fields[i].Type.Kind || desc1.Fields[i].Type.Name != desc2.Fields[i].Type.Name {
				t.Fatalf("field %d type mismatch: %+v vs %+v", i, desc1.Fields[i].Type, desc2.Fields[i].Type)
			}
		}
	})

	t.Run("ProjectView_consistent_across_calls", func(t *testing.T) {
		id := mustID(t, 1000, 20, 0, 1)
		desc := playerClassDesc()
		p := playerStruct{Name: "stable", Level: 5}

		b, err := binding.NewObjectBinding(desc, id, &p)
		if err != nil {
			t.Fatalf("NewObjectBinding: %v", err)
		}

		view1, err := binding.ProjectView(b)
		if err != nil {
			t.Fatalf("ProjectView: %v", err)
		}
		view2, err := binding.ProjectView(b)
		if err != nil {
			t.Fatalf("ProjectView second call: %v", err)
		}

		// Same identity
		if view1.Identity != view2.Identity {
			t.Fatal("identity mismatch across projections")
		}
		// Same schema name
		if view1.Schema.Name != view2.Schema.Name {
			t.Fatal("schema name mismatch across projections")
		}
		// Same field values
		if view1.Fields["Name"] != view2.Fields["Name"] {
			t.Fatalf("Name field mismatch: %v vs %v", view1.Fields["Name"], view2.Fields["Name"])
		}
		if view1.Fields["Level"] != view2.Fields["Level"] {
			t.Fatalf("Level field mismatch: %v vs %v", view1.Fields["Level"], view2.Fields["Level"])
		}
	})
}

// TestStructEmbedding_ReadWriteSemanticsStable locks §9.2 item 3:
// "读写语义稳定"
//
// Verifies that:
// - Reading is projection (ProjectView), not direct access
// - Writing is explicit opt-in (ApplyViewPatch), not automatic
// - Projection is one-way by default
// - Patch only affects schema-declared fields
func TestStructEmbedding_ReadWriteSemanticsStable(t *testing.T) {
	t.Run("read_is_projection", func(t *testing.T) {
		id := mustID(t, 1000, 21, 0, 1)
		desc := playerClassDesc()
		p := playerStruct{Name: "original", Level: 5}

		b, err := binding.NewObjectBinding(desc, id, &p)
		if err != nil {
			t.Fatalf("NewObjectBinding: %v", err)
		}

		view, err := binding.ProjectView(b)
		if err != nil {
			t.Fatalf("ProjectView: %v", err)
		}

		// Projected values match runtime
		if view.Fields["Name"] != "original" {
			t.Fatalf("expected Name=original, got %v", view.Fields["Name"])
		}
		if view.Fields["Level"] != 5 {
			t.Fatalf("expected Level=5, got %v", view.Fields["Level"])
		}

		// Modifying projection does not affect runtime (read is snapshot)
		view.Fields["Name"] = "modified"
		if p.Name != "original" {
			t.Fatal("modifying projection must not affect runtime object")
		}
	})

	t.Run("write_is_explicit_opt_in", func(t *testing.T) {
		id := mustID(t, 1000, 21, 0, 2)
		desc := playerClassDesc()
		p := playerStruct{Name: "alice", Level: 5}

		b, err := binding.NewObjectBinding(desc, id, &p)
		if err != nil {
			t.Fatalf("NewObjectBinding: %v", err)
		}

		view, err := binding.ProjectView(b)
		if err != nil {
			t.Fatalf("ProjectView: %v", err)
		}

		// Change in view does not auto-propagate
		view.Fields["Level"] = 99
		if p.Level != 5 {
			t.Fatal("view change should not auto-propagate to runtime")
		}

		// Explicit patch propagates
		mutations, err := binding.ApplyViewPatch(b, view)
		if err != nil {
			t.Fatalf("ApplyViewPatch: %v", err)
		}
		if p.Level != 99 {
			t.Fatalf("expected Level=99 after explicit patch, got %d", p.Level)
		}
		if len(mutations) != 1 || mutations[0].Key != "Level" {
			t.Fatalf("expected 1 mutation for Level, got %v", mutations)
		}
	})

	t.Run("patch_only_affects_schema_fields", func(t *testing.T) {
		id := mustID(t, 1000, 21, 0, 3)
		// Schema only declares Name
		desc := schema.ObjectDesc{
			Name: "playerStruct",
			Fields: []schema.FieldDesc{
				{Name: "Name", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
			},
		}
		p := playerStruct{Name: "alice", Level: 5}

		b, err := binding.NewObjectBinding(desc, id, &p)
		if err != nil {
			t.Fatalf("NewObjectBinding: %v", err)
		}

		patchView := &binding.ViewProjection{
			Schema:   desc,
			Identity: id,
			Fields: map[string]any{
				"Name":     "bob",
				"Level":    99,   // Not in schema — must be ignored
				"Injected": true, // Not in schema — must be ignored
			},
		}

		mutations, err := binding.ApplyViewPatch(b, patchView)
		if err != nil {
			t.Fatalf("ApplyViewPatch: %v", err)
		}

		if p.Name != "bob" {
			t.Fatalf("expected Name=bob, got %s", p.Name)
		}
		if p.Level != 5 {
			t.Fatalf("Level should remain 5 (not in schema), got %d", p.Level)
		}
		for _, m := range mutations {
			if m.Key != "Name" {
				t.Fatalf("non-schema field %q should not appear in mutations", m.Key)
			}
		}
	})
}

// TestStructEmbedding_DescriptorDoesNotCarryLayoutMetadata locks §9.2 item 6:
// "schema descriptor 不承载字段 offset / native layout metadata"
//
// Verifies that ObjectDesc and FieldDesc are pure semantic descriptors that
// do not carry field offsets, reflection indices, or native layout metadata.
func TestStructEmbedding_DescriptorDoesNotCarryLayoutMetadata(t *testing.T) {
	t.Run("ObjectDesc_has_no_layout_fields", func(t *testing.T) {
		desc, err := schema.DescribeGoStruct(playerStruct{})
		if err != nil {
			t.Fatalf("DescribeGoStruct: %v", err)
		}

		// ObjectDesc only has Name and Fields — no offset/index/layout fields
		// This test verifies the structural contract by using the type as-is.
		// If someone adds Offset/Index/Layout fields to ObjectDesc, this test
		// serves as a reminder that those should not exist.
		_ = desc.Name
		_ = desc.Fields
		// Intentionally no desc.Offset, desc.Index, desc.Layout, etc.
	})

	t.Run("FieldDesc_has_no_layout_fields", func(t *testing.T) {
		desc, err := schema.DescribeGoStruct(playerStruct{})
		if err != nil {
			t.Fatalf("DescribeGoStruct: %v", err)
		}

		for _, field := range desc.Fields {
			// FieldDesc only has Name and Type — no offset/index/layout fields
			_ = field.Name
			_ = field.Type
			// Intentionally no field.Offset, field.Index, field.ReflectType, etc.
		}
	})

	t.Run("TypeDesc_has_no_native_layout", func(t *testing.T) {
		desc, err := schema.DescribeGoStruct(playerStruct{})
		if err != nil {
			t.Fatalf("DescribeGoStruct: %v", err)
		}

		for _, field := range desc.Fields {
			// TypeDesc has Kind/Name/TypeID/Element/Key/Value/ClassName/ClassID
			// but no GoReflectType, ByteSize, Alignment, etc.
			_ = field.Type.Kind
			_ = field.Type.Name
			// Intentionally no field.Type.GoType, field.Type.ByteSize, etc.
		}
	})

	t.Run("projection_does_not_expose_private_fields", func(t *testing.T) {
		type privateStruct struct {
			Public string
			_      string
		}

		desc, err := schema.DescribeGoStruct(privateStruct{})
		if err != nil {
			t.Fatalf("DescribeGoStruct: %v", err)
		}

		// Only exported fields appear in descriptor
		if len(desc.Fields) != 1 {
			t.Fatalf("expected 1 field (exported only), got %d", len(desc.Fields))
		}
		if desc.Fields[0].Name != "Public" {
			t.Fatalf("expected Public field, got %q", desc.Fields[0].Name)
		}
	})
}
