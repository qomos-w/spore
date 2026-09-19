package runtime

import (
	"errors"
	"strings"
	"testing"
)

// whPos / whVel are hand-written components used by the component-name
// whitelist tests below. Names are namespaced so they never collide with the
// other internal test tables (storage_test.go st.Position, type_registry_test.go
// reg.Position, typed_test.go Position).
type whPos struct{ X int }
type whVel struct{ DX int }

var (
	whPosC = NewComponent[whPos]("whitelist.Position")
	whVelC = NewComponent[whVel]("whitelist.Velocity")
)

// The #22 defect: lazy interning accepted any string, so a typo became a
// permanent component slot. A typo'd name must now be refused on the write
// path, and no slot may be allocated anywhere on the read path.
func TestComponentWhitelist_RejectsUndeclaredName(t *testing.T) {
	w := NewWorld(WithComponents("whitelist.Position"))
	e := w.Create()

	if len(w.compNames) != 0 {
		t.Fatalf("declaration must not allocate slots, compNames=%v", w.compNames)
	}

	typo := "whitelist.Positio"
	err := w.SetComponent(e, typo, &whPos{X: 1})
	if err == nil {
		t.Fatal("SetComponent with an undeclared name must error")
	}
	var ee *EntityError
	if !errors.As(err, &ee) {
		t.Fatalf("undeclared-name error must be an *EntityError, got %T", err)
	}
	if !strings.Contains(err.Error(), "not declared") {
		t.Fatalf("error must name the declaration contract, got %q", err.Error())
	}

	// The typo declares nothing and allocates nothing.
	if len(w.compNames) != 0 {
		t.Fatalf("typo allocated a component slot: compNames=%v", w.compNames)
	}
	if len(w.compIDs) != 0 {
		t.Fatalf("typo interned a component ID: compIDs=%v", w.compIDs)
	}
	if len(w.compSets) != 0 {
		t.Fatalf("typo allocated a sparse set: compSets=%d", len(w.compSets))
	}
	if w.RegisteredComponent(typo) {
		t.Fatal("a typo must not declare itself")
	}

	// The read/remove/mark paths report absence and never allocate.
	if _, ok := w.GetComponent(e, typo); ok {
		t.Fatal("GetComponent on an undeclared name must report absent")
	}
	if w.HasComponent(e, typo) {
		t.Fatal("HasComponent on an undeclared name must be false")
	}
	// The strict read gate is where a typo is distinguishable from absence.
	if gateErr := w.RequireDeclaredComponent(typo); gateErr == nil {
		t.Fatal("RequireDeclaredComponent must flag an undeclared name")
	} else if !strings.Contains(gateErr.Error(), "not declared") {
		t.Fatalf("gate error must name the declaration contract, got %q", gateErr.Error())
	}
	if gateErr := w.RequireDeclaredComponent("whitelist.Position"); gateErr != nil {
		t.Fatalf("RequireDeclaredComponent on a declared name: %v", gateErr)
	}
	w.RemoveComponent(e, typo)
	w.MarkChanged(e, typo)
	if len(w.compNames) != 0 {
		t.Fatalf("read paths allocated a slot: compNames=%v", w.compNames)
	}

	// The declared name stays usable, and declaration alone allocates nothing:
	// the slot appears on the first write (lazy interning, now gated on
	// declaration).
	if err := w.SetComponent(e, "whitelist.Position", &whPos{X: 7}); err != nil {
		t.Fatalf("declared name must be writable: %v", err)
	}
	if len(w.compNames) != 1 || w.compNames[0] != "whitelist.Position" {
		t.Fatalf("compNames = %v, want [whitelist.Position]", w.compNames)
	}
	got, ok := w.GetComponent(e, "whitelist.Position")
	if !ok {
		t.Fatal("declared component not found after write")
	}
	if p, ok := got.(*whPos); !ok || p.X != 7 {
		t.Fatalf("GetComponent = (%v, %v), want *whPos{X:7}", got, ok)
	}
}

func TestComponentWhitelist_DeclarationAPI(t *testing.T) {
	w := NewWorld()

	if err := w.RegisterComponent(""); err == nil {
		t.Fatal("empty component name must be rejected")
	}
	if err := w.RegisterComponent("api.Position"); err != nil {
		t.Fatalf("RegisterComponent: %v", err)
	}
	if err := w.RegisterComponent("api.Position"); err != nil {
		t.Fatalf("re-RegisterComponent must be idempotent: %v", err)
	}
	w.RegisterComponents("api.Velocity", "", "api.Health") // empty entries are ignored

	if got, want := strings.Join(w.RegisteredComponents(), ","), "api.Health,api.Position,api.Velocity"; got != want {
		t.Fatalf("RegisteredComponents = %q, want %q", got, want)
	}
	if w.RegisteredComponent("api.Missing") {
		t.Fatal("RegisteredComponent must be false for an undeclared name")
	}
	if len(w.compNames) != 0 {
		t.Fatalf("declaration must allocate no slots, compNames=%v", w.compNames)
	}

	// Declared but never written is absent, not zero-valued.
	if _, ok := w.GetComponent(w.Create(), "api.Position"); ok {
		t.Fatal("declared-but-unwritten component must be absent")
	}
}

// A Component[T] descriptor is a Go-level declaration of the component
// vocabulary: the descriptor write path declares its own name, so hand-written
// components keep working without a separate registration call. Read paths
// never declare.
func TestComponentWhitelist_DescriptorWriteDeclares(t *testing.T) {
	w := NewWorld()
	e := w.Create()

	if err := w.Set(e, whPosC, &whPos{X: 1}); err != nil {
		t.Fatalf("descriptor write must declare its own name: %v", err)
	}
	if !w.RegisteredComponent(whPosC.Name()) {
		t.Fatalf("descriptor write must declare %q on the World", whPosC.Name())
	}
	// The declared name is now usable through the string API (the two layers
	// alias one slot).
	if err := w.SetComponent(e, whPosC.Name(), &whPos{X: 2}); err != nil {
		t.Fatalf("string write after descriptor write: %v", err)
	}

	// Read/remove with an untouched descriptor declares nothing.
	if w.RegisteredComponent(whVelC.Name()) {
		t.Fatalf("%q must not be declared before use", whVelC.Name())
	}
	if _, ok := w.Get(e, whVelC); ok {
		t.Fatal("Get with an unused descriptor must report absent")
	}
	never := "whitelist.NeverDeclared"
	w.Remove(e, NewComponent[whVel](never))
	w.Mark(e, NewComponent[whVel](never))
	if w.RegisteredComponent(never) {
		t.Fatal("Remove/Mark must not declare a component name")
	}
	if len(w.compNames) != 1 {
		t.Fatalf("compNames = %v, want exactly [whitelist.Position]", w.compNames)
	}
}

// The #7 invariant on the runtime side: the two registry layers (declared
// vocabulary vs interned component table) must not drift. Every interned name
// must be declared, and compIDs/compNames/compSets must stay index-aligned.
func TestComponentWhitelist_InternedNamesAreAlwaysDeclared(t *testing.T) {
	w := NewWorld(WithComponents("wl.Position"))
	e := w.Create()
	if err := w.SetComponent(e, "wl.Position", &whPos{X: 1}); err != nil {
		t.Fatalf("SetComponent: %v", err)
	}
	if err := w.Set(e, whVelC, &whVel{DX: 3}); err != nil {
		t.Fatalf("descriptor Set: %v", err)
	}
	if err := w.SetComponent(e, "wl.Typo", &whPos{}); err == nil {
		t.Fatal("undeclared name must be rejected")
	}

	if len(w.compNames) != 2 {
		t.Fatalf("compNames = %v, want 2 interned components", w.compNames)
	}
	if len(w.compIDs) != len(w.compNames) || len(w.compSets) != len(w.compNames) {
		t.Fatalf("component table layers out of sync: compIDs=%d compNames=%d compSets=%d",
			len(w.compIDs), len(w.compNames), len(w.compSets))
	}
	for name, id := range w.compIDs {
		if !w.RegisteredComponent(name) {
			t.Fatalf("interned name %q (id %d) is not declared: registry layers drifted", name, id)
		}
		if int(id) >= len(w.compNames) || w.compNames[id] != name {
			t.Fatalf("compIDs/compNames disagree for %q (id %d, compNames=%v)", name, id, w.compNames)
		}
	}
	for _, name := range w.RegisteredComponents() {
		if name == "wl.Typo" {
			t.Fatal("a rejected typo must never appear in the declared vocabulary")
		}
	}
}

// AddRegistry is a declaration source: the codegen component vocabulary is
// immediately usable through the string API, and undeclared names stay
// rejected even on a registry-backed World.
func TestAddRegistry_DeclaresComponentNames(t *testing.T) {
	w := NewWorld()
	w.AddRegistry(tableOf(compEntry[regPos](900, "wreg.Position")))

	if !w.RegisteredComponent("wreg.Position") {
		t.Fatal("AddRegistry must declare its component names")
	}
	e := w.Create()
	if err := w.SetComponent(e, "wreg.Position", &regPos{X: 1}); err != nil {
		t.Fatalf("codegen-declared name must be writable through the string API: %v", err)
	}
	if err := w.SetComponent(e, "wreg.Positio", &regPos{}); err == nil {
		t.Fatal("undeclared name must be rejected on a registry-backed World")
	}
	if len(w.compNames) != 1 {
		t.Fatalf("compNames = %v, want exactly [wreg.Position]", w.compNames)
	}
	// The descriptor-free facade resolves through the same registry.
	if _, ok := w.LookupComponent[regPos](); !ok {
		t.Fatal("LookupComponent must resolve after AddRegistry")
	}
	if err := w.SetT(e, &regPos{X: 2}); err != nil {
		t.Fatalf("SetT: %v", err)
	}
}

// A descriptor write that fails on the entity checks must not grow the
// vocabulary either: declaration is part of a viable write, not a side effect
// of attempting one.
func TestComponentWhitelist_FailedWriteDoesNotDeclare(t *testing.T) {
	w := NewWorld()
	e := w.Create()
	w.Dispose(e)

	c := NewComponent[whPos]("whitelist.AfterDispose")
	if err := w.Set(e, c, &whPos{X: 1}); err == nil {
		t.Fatal("descriptor write on a disposed entity must error")
	}
	if w.RegisteredComponent(c.Name()) {
		t.Fatalf("a failed write must not declare %q", c.Name())
	}
	if len(w.compNames) != 0 {
		t.Fatalf("failed write allocated slots: %v", w.compNames)
	}
}
