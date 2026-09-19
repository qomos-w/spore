package runtime

import (
	"testing"
)

type typedPosition struct{ X, Y int }
type typedVelocity struct{ DX, DY int }
type typedHealth struct{ Value int }

var (
	posC = NewComponent[typedPosition]("Position")
	velC = NewComponent[typedVelocity]("Velocity")
	hpC  = NewComponent[typedHealth]("Health")
)

func TestTypedSetGetRoundTrip(t *testing.T) {
	w := NewWorld()
	e := w.Create()

	if err := w.Set(e, posC, &typedPosition{X: 1, Y: 2}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, ok := w.Get(e, posC)
	if !ok || got == nil {
		t.Fatalf("Get: not found")
	}
	if got.X != 1 || got.Y != 2 {
		t.Fatalf("Get: got %+v", got)
	}
}

func TestTypedGetInteropsWithStringAPI(t *testing.T) {
	w := NewWorld()
	e := w.Create()

	if err := w.SetComponent(e, "Position", &typedPosition{X: 7}); err != nil {
		t.Fatalf("SetComponent: %v", err)
	}
	got, ok := w.Get(e, posC)
	if !ok || got.X != 7 {
		t.Fatalf("typed Get over string-set component: got %v ok=%v", got, ok)
	}

	if err := w.Set(e, velC, &typedVelocity{DX: 3}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	raw, ok := w.GetComponent(e, "Velocity")
	if !ok {
		t.Fatalf("string Get over typed-set component: not found")
	}
	if v, ok := raw.(*typedVelocity); !ok || v.DX != 3 {
		t.Fatalf("string GetComponent returned wrong type/value")
	}
}

func TestTypedGetTypeMismatch(t *testing.T) {
	w := NewWorld()
	e := w.Create()

	if err := w.Set(e, posC, &typedPosition{}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	// Same name, wrong type — Get must fail cleanly, not panic.
	if got, ok := w.Get(e, NewComponent[typedHealth]("Position")); ok || got != nil {
		t.Fatalf("Get with mismatched type: got %v ok=%v", got, ok)
	}
}

func TestTypedChangeTracking(t *testing.T) {
	w := NewWorld()
	e := w.Create()

	w.Set(e, posC, &typedPosition{X: 1})
	w.Set(e, posC, &typedPosition{X: 2}) // existing → changed
	w.Mark(e, velC)                      // absent → no-op

	cs := w.ChangeSet(e)
	if len(cs.Added) != 1 || cs.Added[0] != "Position" {
		t.Fatalf("Added = %v, want [Position]", cs.Added)
	}
	if len(cs.Changed) != 1 || cs.Changed[0] != "Position" {
		t.Fatalf("Changed = %v, want [Position]", cs.Changed)
	}
	if len(cs.Removed) != 0 {
		t.Fatalf("Removed = %v, want empty", cs.Removed)
	}

	w.Remove(e, posC)
	if w.Has(e, posC) {
		t.Fatal("Has after Remove: want false")
	}
	cs = w.ChangeSet(e)
	if len(cs.Removed) != 1 || cs.Removed[0] != "Position" {
		t.Fatalf("Removed after Remove = %v", cs.Removed)
	}
}

func TestTypedQueryFilters(t *testing.T) {
	w := NewWorld()
	e1 := w.Create()
	e2 := w.Create()

	w.Set(e1, posC, &typedPosition{})
	w.Set(e1, velC, &typedVelocity{})
	w.Set(e2, posC, &typedPosition{})

	both := w.Execute(NewQuery().With(posC).With(velC))
	if len(both) != 1 || both[0].ID() != e1.ID() {
		t.Fatalf("With+With: got %d entities, want only e1", len(both))
	}

	without := w.Execute(NewQuery().With(posC).Without(velC))
	if len(without) != 1 || without[0].ID() != e2.ID() {
		t.Fatalf("With+Without: got %d entities, want only e2", len(without))
	}

	changed := w.Execute(NewQuery().OnChanged(posC))
	if len(changed) != 0 {
		t.Fatalf("OnChanged before any change: got %d, want 0", len(changed))
	}
	w.Set(e1, posC, &typedPosition{X: 9}) // re-set → changed
	changed = w.Execute(NewQuery().OnChanged(posC))
	if len(changed) != 1 || changed[0].ID() != e1.ID() {
		t.Fatalf("OnChanged: got %d entities, want e1", len(changed))
	}

	w.Remove(e2, posC)
	removed := w.Execute(NewQuery().OnRemoved(posC))
	if len(removed) != 1 || removed[0].ID() != e2.ID() {
		t.Fatalf("OnRemoved: got %d entities, want e2", len(removed))
	}

	added := w.Execute(NewQuery().OnAdded(velC))
	if len(added) != 1 || added[0].ID() != e1.ID() {
		t.Fatalf("OnAdded: got %d entities, want e1", len(added))
	}
}

func TestTypedEach2(t *testing.T) {
	w := NewWorld()
	e1 := w.Create()
	e2 := w.Create()

	w.Set(e1, posC, &typedPosition{X: 10})
	w.Set(e1, velC, &typedVelocity{DX: 5})
	w.Set(e2, posC, &typedPosition{X: 20}) // no Velocity

	var visited int
	w.Each2(NewQuery().With(posC).With(velC), posC, velC, func(e Entity, p *typedPosition, v *typedVelocity) {
		visited++
		if e.ID() != e1.ID() || p.X != 10 || v.DX != 5 {
			t.Fatalf("Each2 visited wrong entity/values: %+v %+v", p, v)
		}
	})
	if visited != 1 {
		t.Fatalf("Each2 visited %d entities, want 1", visited)
	}
}

func TestTypedEach3(t *testing.T) {
	w := NewWorld()
	e1 := w.Create()

	w.Set(e1, posC, &typedPosition{X: 1})
	w.Set(e1, velC, &typedVelocity{DX: 2})
	w.Set(e1, hpC, &typedHealth{Value: 3})

	visited := 0
	w.Each3(NewQuery().With(posC).With(velC).With(hpC), posC, velC, hpC,
		func(e Entity, p *typedPosition, v *typedVelocity, h *typedHealth) {
			visited++
			if p.X != 1 || v.DX != 2 || h.Value != 3 {
				t.Fatalf("Each3 wrong values: %+v %+v %+v", p, v, h)
			}
		})
	if visited != 1 {
		t.Fatalf("Each3 visited %d entities, want 1", visited)
	}
}

func TestTypedEachSkipsWrongPointerType(t *testing.T) {
	w := NewWorld()
	e := w.Create()

	// Component stored via string API with an incompatible pointer type.
	w.SetComponent(e, "Position", &typedVelocity{})
	visited := 0
	w.Each2(NewQuery(), posC, velC, func(Entity, *typedPosition, *typedVelocity) {
		visited++
	})
	if visited != 0 {
		t.Fatalf("Each2 should skip mismatched pointer, visited %d", visited)
	}
}

func TestTypedSetOnDisposedEntity(t *testing.T) {
	w := NewWorld()
	e := w.Create()
	w.Dispose(e)

	if err := w.Set(e, posC, &typedPosition{}); err == nil {
		t.Fatal("Set on disposed entity: want error")
	}
	if _, ok := w.Get(e, posC); ok {
		t.Fatal("Get on disposed entity: want ok=false")
	}
	if w.Has(e, posC) {
		t.Fatal("Has on disposed entity: want false")
	}
}