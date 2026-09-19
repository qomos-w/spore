package runtime

import (
	"strings"
	"testing"
)

type oopUnit struct {
	Ref
	Position typedPosition
	Health   typedHealth
}

var (
	oopPosC = NewComponent[typedPosition]("Position")
	oopHpC  = NewComponent[typedHealth]("Health")
	oopVelC = NewComponent[typedVelocity]("Velocity")
)

func bindUnit(t *testing.T, w *World) (*oopUnit, Entity) {
	t.Helper()
	u := &oopUnit{Position: typedPosition{X: 1, Y: 2}, Health: typedHealth{Value: 100}}
	e, err := Bind(w, u).
		Attach(oopPosC, &u.Position).
		Attach(oopHpC, &u.Health).
		Register()
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	return u, e
}

func TestRef_ZeroValueIsSafe(t *testing.T) {
	var r Ref
	if r.IsRegistered() {
		t.Fatal("zero Ref must not be registered")
	}
	if r.IsAlive() {
		t.Fatal("zero Ref must not be alive")
	}
	r.Dispose()                                    // no-op, must not panic
	r.Mark(oopPosC)                                // no-op
	if _, ok := r.Get(oopPosC); ok {               // no-op
		t.Fatal("zero Ref Get must return false")
	}
	if r.Has(oopPosC) {
		t.Fatal("zero Ref Has must return false")
	}
	if err := r.Set(oopPosC, typedPosition{}); err == nil {
		t.Fatal("zero Ref Set must return error")
	}
}

func TestRef_RegisterZeroCopyAliasing(t *testing.T) {
	w := NewWorld()
	u, e := bindUnit(t, w)

	// The component stored by the World aliases the host field.
	p, ok := w.Get(e, oopPosC)
	if !ok {
		t.Fatal("Get via World failed")
	}
	if p != &u.Position {
		t.Fatalf("World.Get must alias &u.Position (zero-copy), got separate pointer")
	}

	// Direct field write is visible through the ECS view without any copy.
	u.Position.X = 42
	if p.X != 42 {
		t.Fatal("field write must be visible via component pointer (aliasing broken)")
	}

	// Ref.Get also aliases.
	if gp, _ := u.Get(oopPosC); gp != &u.Position {
		t.Fatal("Ref.Get must alias the host field")
	}
}

func TestRef_DirectFieldMutationNeedsMark(t *testing.T) {
	w := NewWorld()
	u, _ := bindUnit(t, w)

	// Fresh registration: components are in the added set (not changed).
	changed := w.Execute(NewQuery().OnChanged(oopPosC))
	if len(changed) != 0 {
		t.Fatalf("fresh registration must not be in changed set, matched %d", len(changed))
	}
	added := w.Execute(NewQuery().OnAdded(oopPosC))
	if len(added) != 1 {
		t.Fatalf("fresh registration must be in added set, matched %d", len(added))
	}

	// Direct field write without Mark: not visible to WhenChanged.
	u.Position.X = 10
	if got := len(w.Execute(NewQuery().OnChanged(oopPosC))); got != 0 {
		t.Fatalf("unmarked field write must not appear in changed set, matched %d", got)
	}

	// Mark makes it visible.
	u.Mark(oopPosC)
	if got := len(w.Execute(NewQuery().OnChanged(oopPosC))); got != 1 {
		t.Fatalf("marked field write must appear in changed set, matched %d", got)
	}
}

func TestRef_SetWritesBackToHostField(t *testing.T) {
	w := NewWorld()
	u, _ := bindUnit(t, w)

	if err := u.Set(oopPosC, typedPosition{X: 7, Y: 8}); err != nil {
		t.Fatalf("Ref.Set: %v", err)
	}
	// Value copied back into the host field.
	if u.Position.X != 7 || u.Position.Y != 8 {
		t.Fatalf("Ref.Set must copy the value into the host field, got %+v", u.Position)
	}
	// ECS view reflects it and change tracking fired.
	if p, _ := w.Get(u.Entity, oopPosC); p.X != 7 {
		t.Fatal("ECS view must see Ref.Set value")
	}
	if got := len(w.Execute(NewQuery().OnChanged(oopPosC))); got != 1 {
		t.Fatalf("Ref.Set must mark the component changed, matched %d", got)
	}
}

func TestRef_SetNewComponent(t *testing.T) {
	w := NewWorld()
	u, e := bindUnit(t, w)

	// Registering a NEW component (not a host field) is legal: the
	// registered entity is indistinguishable from a plain entity.
	if err := u.Set(oopVelC, typedVelocity{DX: 1}); err != nil {
		t.Fatalf("Ref.Set for a new component: %v", err)
	}
	if !w.Has(e, oopVelC) {
		t.Fatal("new component must be visible on the entity")
	}
	// Plain World.Set works too.
	if err := w.Set(e, NewComponent[int]("Level"), new(int)); err != nil {
		t.Fatalf("World.Set on registered entity: %v", err)
	}
	// Query sees the union of field components and added components.
	got := len(w.Execute(NewQuery().With(oopPosC).With(oopVelC)))
	if got != 1 {
		t.Fatalf("query over field+added components matched %d, want 1", got)
	}
}

func TestRef_DisposeDoesNotFireComponentRemoval(t *testing.T) {
	w := NewWorld()
	u, e := bindUnit(t, w)

	u.Dispose()

	if w.IsAlive(e) {
		t.Fatal("entity must be dead after Ref.Dispose")
	}
	if u.IsAlive() {
		t.Fatal("Ref.IsAlive must be false after Dispose")
	}
	// Dispose is an entity-level event: components must NOT appear in
	// the removed set (RemoveComponent is the component-level path).
	if got := len(w.Execute(NewQuery().OnRemoved(oopPosC))); got != 0 {
		t.Fatalf("Dispose must not fire component removal, matched %d", got)
	}
	// Host fields survive and remain owned by the host.
	if u.Position.X != 1 {
		t.Fatal("host field must survive Dispose")
	}
}

func TestRef_RemoveFieldComponentKeepsHostValue(t *testing.T) {
	w := NewWorld()
	u, e := bindUnit(t, w)

	w.Remove(e, oopPosC)

	if w.Has(e, oopPosC) {
		t.Fatal("component view must be removed")
	}
	if got := len(w.Execute(NewQuery().OnRemoved(oopPosC))); got != 1 {
		t.Fatalf("RemoveComponent must fire removal tracking, matched %d", got)
	}
	// Host field value is preserved (host owns its memory).
	if u.Position.X != 1 {
		t.Fatal("host field value must survive component removal")
	}

	// Re-set with the same field pointer restores the alignment.
	if err := w.Set(e, oopPosC, &u.Position); err != nil {
		t.Fatalf("re-set field pointer: %v", err)
	}
	if p, _ := w.Get(e, oopPosC); p != &u.Position {
		t.Fatal("re-set must restore field aliasing")
	}
}

func TestRef_OverwritingFieldComponentPointerIsDocumentedFootgun(t *testing.T) {
	w := NewWorld()
	u, e := bindUnit(t, w)

	// Overwriting the component with a DIFFERENT pointer detaches the
	// ECS view from the host field. This is documented contract behavior:
	// use field writes + Mark, or Ref.Set, for host-registered components.
	other := &typedPosition{X: 99}
	if err := w.Set(e, oopPosC, other); err != nil {
		t.Fatalf("World.Set overwrite: %v", err)
	}
	if u.Position.X == 99 {
		t.Fatal("precondition: host field must be untouched by pointer overwrite")
	}
	p, _ := w.Get(e, oopPosC)
	if p != other {
		t.Fatal("component storage must hold the overwritten pointer")
	}
}

func TestBind_Errors(t *testing.T) {
	w := NewWorld()

	type withRef struct {
		Ref
		A int
	}
	host := &withRef{}

	// Nil pointer is expressible through the Bind signature and must fail.
	var nilHost *withRef
	if _, err := Bind(w, nilHost).Register(); err == nil {
		t.Fatal("Bind(nil pointer) must fail at Register")
	}

	// Host without Ref field.
	type noRef struct{ A int }
	if _, err := Bind(w, &noRef{}).Register(); err == nil {
		t.Fatal("Bind(host without Ref) must fail at Register")
	}

	// Host with multiple Ref fields.
	type multiRef struct {
		Ref
		Extra Ref
	}
	if _, err := Bind(w, &multiRef{}).Register(); err == nil {
		t.Fatal("Bind(host with multiple Ref) must fail at Register")
	}

	// Attach with a pointer outside the host.
	var outside typedPosition
	if _, err := Bind(w, host).Attach(oopPosC, &outside).Register(); err == nil {
		t.Fatal("Attach(pointer outside host) must fail at Register")
	}

	// Attach nil pointer.
	if _, err := Bind(w, host).Attach(oopPosC, (*typedPosition)(nil)).Register(); err == nil {
		t.Fatal("Attach(nil pointer) must fail at Register")
	}

	// Nil world.
	if _, err := Bind(nil, host).Register(); err == nil {
		t.Fatal("Bind(nil world) must fail at Register")
	}
}

func TestBind_DoubleRegisterFails(t *testing.T) {
	w := NewWorld()
	u, _ := bindUnit(t, w)

	// Re-registering the same host must fail and leave the Ref intact.
	_, err := Bind(w, u).Register()
	if err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("double Register must fail with 'already registered', got %v", err)
	}
	if !u.IsRegistered() {
		t.Fatal("Ref must remain registered after failed re-register")
	}
}

func TestCoexistence_RegisteredAndPlainEntitiesSameWorldQueryTick(t *testing.T) {
	w := NewWorld()

	// One OOP host and two plain entities in the same World.
	u, eHost := bindUnit(t, w)
	ePlain1 := w.Create()
	ePlain2 := w.Create()
	w.Set(ePlain1, oopPosC, &typedPosition{X: 10})
	w.Set(ePlain1, oopVelC, &typedVelocity{DX: 1})
	w.Set(ePlain2, oopPosC, &typedPosition{X: 20})

	// Same query sees both kinds without distinction.
	got := w.Execute(NewQuery().With(oopPosC))
	if len(got) != 3 {
		t.Fatalf("query must see registered + plain entities, got %d", len(got))
	}
	seen := map[string]bool{}
	for _, e := range got {
		seen[e.ID().String()] = true
	}
	for _, want := range []string{eHost.ID().String(), ePlain1.ID().String(), ePlain2.ID().String()} {
		if !seen[want] {
			t.Fatalf("query result missing entity %s", want)
		}
	}

	// Each2 iterates both kinds; the registered entity's components are
	// the same storage as the plain entities'.
	visits := 0
	w.Each2(NewQuery().With(oopPosC), oopPosC, oopVelC,
		func(e Entity, p *typedPosition, v *typedVelocity) {
			visits++
			_ = p
			_ = v
		})
	// Only ePlain1 carries both Position+Velocity; the host has only
	// Position (its Velocity was never attached).
	if visits != 1 {
		t.Fatalf("Each2 matched %d, want 1 (plain entity with both components)", visits)
	}

	// Same Tick clears change tracking for both kinds identically.
	u.Mark(oopPosC)
	w.Mark(ePlain2, oopPosC)
	if n := len(w.Execute(NewQuery().OnChanged(oopPosC))); n != 2 {
		t.Fatalf("pre-Tick changed query matched %d, want 2 (both kinds)", n)
	}
	w.Tick()
	if n := len(w.Execute(NewQuery().OnChanged(oopPosC))); n != 0 {
		t.Fatalf("post-Tick changed query matched %d, want 0 for both kinds", n)
	}
}

func TestCoexistence_IterationDisposeRegisteredEntityIsSafe(t *testing.T) {
	w := NewWorld()
	_, eHost := bindUnit(t, w)
	ePlain := w.Create()
	w.Set(ePlain, oopPosC, &typedPosition{X: 5})
	w.Set(ePlain, oopHpC, &typedHealth{Value: 50})

	// Snapshot iteration disposes the registered entity mid-flight.
	visited := 0
	w.Each2(NewQuery().With(oopPosC), oopPosC, oopHpC, func(e Entity, p *typedPosition, h *typedHealth) {
		if e.ID() == eHost.ID() {
			w.Dispose(eHost)
		}
		visited++
	})
	if visited != 2 {
		t.Fatalf("iteration must visit both entities (snapshot), visited %d", visited)
	}

	if w.IsAlive(eHost) {
		t.Fatal("registered entity must be disposed after mid-iteration Dispose")
	}
	if !w.IsAlive(ePlain) {
		t.Fatal("plain entity must be unaffected")
	}
}

func TestCoexistence_EachHostSkipsPlainEntities(t *testing.T) {
	w := NewWorld()
	bindUnit(t, w)

	for i := 0; i < 3; i++ {
		e := w.Create()
		w.Set(e, oopPosC, &typedPosition{X: i})
	}

	visited := 0
	w.EachHost(NewQuery().With(oopPosC), func(e Entity, host *oopUnit) {
		visited++
	})
	if visited != 1 {
		t.Fatalf("EachHost must visit only the registered host, got %d", visited)
	}
}

func TestHost_Retrieval(t *testing.T) {
	w := NewWorld()
	u, e := bindUnit(t, w)

	// Registered entity: Host returns the aggregate.
	h, ok := w.Host(e)
	if !ok || h != any(u) {
		t.Fatalf("Host must return the registered host, got %v %v", h, ok)
	}

	// Plain entity: no host.
	plain := w.Create()
	if _, ok := w.Host(plain); ok {
		t.Fatal("plain entity must not have a host")
	}

	// EachHost iterates only hosts of the right type.
	visited := 0
	w.EachHost(NewQuery().With(oopPosC), func(e Entity, host *oopUnit) {
		visited++
		if host != u {
			t.Fatal("EachHost must yield the typed host pointer")
		}
	})
	if visited != 1 {
		t.Fatalf("EachHost visited %d, want 1", visited)
	}

	// EachHost skips hosts of the wrong type without error.
	w.EachHost(NewQuery().With(oopPosC), func(e Entity, host *int) {
		t.Fatal("wrong-typed EachHost must not visit")
	})

	// Dispose clears host visibility.
	u.Dispose()
	if _, ok := w.Host(e); ok {
		t.Fatal("Host must return false after Dispose")
	}
}