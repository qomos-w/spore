package runtime

import (
	"sync"
	"testing"
)

// This file locks the operational contracts of the runtime carrier:
// tick boundaries, iteration snapshot semantics, and concurrency model.

func TestWorld_TickClearsAllChangeSets(t *testing.T) {
	w := NewWorld()
	e1 := w.Create()
	e2 := w.Create()

	w.Set(e1, posC, &typedPosition{X: 1})           // added
	w.Set(e2, posC, &typedPosition{X: 1})             // added
	w.Set(e2, posC, &typedPosition{X: 2})              // changed
	w.Mark(e1, velC)                                  // changed (absent → no-op)
	w.Remove(e2, NewComponent[int]("NeverExisted"))   // removed (absent → no-op)
	w.Remove(e1, posC)                                // removed

	if cs := w.ChangeSet(e1); cs.Empty() {
		t.Fatal("precondition: e1 change set should be non-empty")
	}
	if cs := w.ChangeSet(e2); cs.Empty() {
		t.Fatal("precondition: e2 change set should be non-empty")
	}

	w.Tick()

	if cs := w.ChangeSet(e1); !cs.Empty() {
		t.Fatalf("after Tick: e1 change set = %+v, want empty", cs)
	}
	if cs := w.ChangeSet(e2); !cs.Empty() {
		t.Fatalf("after Tick: e2 change set = %+v, want empty", cs)
	}
}

func TestWorld_TickResetsChangeQueries(t *testing.T) {
	w := NewWorld()
	e := w.Create()
	w.Set(e, posC, &typedPosition{X: 1})
	w.Set(e, posC, &typedPosition{X: 2}) // re-set → changed

	changed := w.Execute(NewQuery().OnChanged(posC))
	if len(changed) != 1 {
		t.Fatalf("precondition: OnChanged matched %d, want 1", len(changed))
	}

	w.Tick()

	changed = w.Execute(NewQuery().OnChanged(posC))
	if len(changed) != 0 {
		t.Fatalf("after Tick: OnChanged matched %d, want 0", len(changed))
	}
	added := w.Execute(NewQuery().OnAdded(posC))
	if len(added) != 0 {
		t.Fatalf("after Tick: OnAdded matched %d, want 0", len(added))
	}
	removed := w.Execute(NewQuery().OnRemoved(posC))
	if len(removed) != 0 {
		t.Fatalf("after Tick: OnRemoved matched %d, want 0", len(removed))
	}

	// Component data survives the tick — only change tracking resets.
	if !w.Has(e, posC) {
		t.Fatal("component data must survive Tick")
	}
	got, ok := w.Get(e, posC)
	if !ok || got.X != 2 {
		t.Fatalf("component value after Tick = %+v, want X=2", got)
	}
}

func TestWorld_TickOnEmptyWorld(t *testing.T) {
	w := NewWorld()
	w.Tick() // must not panic
}

// Iteration snapshot semantics: Each2 iterates a snapshot of matched
// entities; structural changes to the world during iteration must not
// corrupt the in-flight iteration.

func TestEach2_DisposeCurrentEntityDuringIteration(t *testing.T) {
	w := NewWorld()
	for i := 0; i < 4; i++ {
		e := w.Create()
		w.Set(e, posC, &typedPosition{X: i})
		w.Set(e, velC, &typedVelocity{DX: 1})
	}

	// Dispose every entity as it is visited; iteration must visit all 4
	// without panicking, because the match list is a snapshot.
	visited := 0
	q := NewQuery().With(posC).With(velC)
	w.Each2(q, posC, velC, func(e Entity, p *typedPosition, v *typedVelocity) {
		visited++
		w.Dispose(e)
	})
	if visited != 4 {
		t.Fatalf("visited %d entities, want 4 (snapshot iteration)", visited)
	}
	if w.EntityCount() != 0 {
		t.Fatalf("EntityCount after disposing all = %d, want 0", w.EntityCount())
	}
}

func TestEach2_RemoveOtherComponentDuringIteration(t *testing.T) {
	w := NewWorld()
	entities := make([]Entity, 4)
	for i := 0; i < 4; i++ {
		e := w.Create()
		entities[i] = e
		w.Set(e, posC, &typedPosition{X: i})
		w.Set(e, velC, &typedVelocity{DX: 1})
	}

	// Mid-iteration, remove the Velocity component of the LAST entity in
	// our entities slice — whichever entity is currently being visited
	// removes a different snapshot member's component. The snapshot
	// iterator does not corrupt state; whether the affected entity is
	// still visited depends on map iteration order, so we only assert
	// no-panic and that the visited count is 3 or 4 (its own visit may
	// or may not already have happened when the removal occurred).
	visited := 0
	q := NewQuery().With(posC).With(velC)
	w.Each2(q, posC, velC, func(e Entity, p *typedPosition, v *typedVelocity) {
		if e.ID() == entities[0].ID() {
			w.Remove(entities[3], velC)
		}
		visited++
	})
	if visited < 3 || visited > 4 {
		t.Fatalf("visited %d entities, want 3 or 4 (snapshot member lost a component mid-iteration)", visited)
	}
}

// Concurrency contract: World is documented as not safe for concurrent
// use. This test locks the two supported usage patterns under -race:
// (1) single-goroutine sequential use, and (2) externally serialized
// access via a mutex (the recommended pattern for sharing a World across
// goroutines). Unserialized concurrent access is NOT tested because it
// is explicitly outside the contract.

func TestWorld_SingleGoroutine_NoRace(t *testing.T) {
	w := NewWorld()
	for i := 0; i < 200; i++ {
		e := w.Create()
		w.Set(e, posC, &typedPosition{X: i, Y: i})
		if i%3 == 0 {
			w.Set(e, velC, &typedVelocity{DX: i})
		}
	}
	w.Each2(NewQuery().With(posC).With(velC), posC, velC, func(e Entity, p *typedPosition, v *typedVelocity) {
		p.X += v.DX
	})
	w.Tick()
	for _, e := range w.Execute(NewQuery().With(posC)) {
		w.Get(e, posC)
		w.Has(e, velC)
		w.ChangeSet(e)
	}
}

func TestWorld_MutexSerializedConcurrent_NoRace(t *testing.T) {
	w := NewWorld()
	var mu sync.Mutex

	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				mu.Lock()
				e := w.Create()
				w.Set(e, posC, &typedPosition{X: g, Y: i})
				mu.Unlock()
			}
		}(g)
	}
	for g := 0; g < 2; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				mu.Lock()
				_ = w.Execute(NewQuery().With(posC))
				w.Tick()
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if n := w.EntityCount(); n != 400 {
		t.Fatalf("EntityCount = %d, want 400", n)
	}
}

func TestEach3_DisposeDuringIterationIsSafe(t *testing.T) {
	w := NewWorld()
	for i := 0; i < 3; i++ {
		e := w.Create()
		w.Set(e, posC, &typedPosition{X: i})
		w.Set(e, velC, &typedVelocity{DX: 1})
		w.Set(e, hpC, &typedHealth{Value: 100})
	}

	visited := 0
	q := NewQuery().With(posC).With(velC).With(hpC)
	w.Each3(q, posC, velC, hpC, func(e Entity, p *typedPosition, v *typedVelocity, h *typedHealth) {
		visited++
		w.Dispose(e)
	})
	if visited != 3 {
		t.Fatalf("visited %d entities, want 3", visited)
	}
}