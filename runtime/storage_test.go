package runtime

import (
	"testing"

	"github.com/qomos-w/spore/identity"
)

type stPos struct{ X, Y float64 }
type stVel struct{ DX, DY float64 }

var (
	stPosC = NewComponent[stPos]("st.Position")
	stVelC = NewComponent[stVel]("st.Velocity")
)

func stWorld(n int) *World {
	w := NewWorld()
	for i := 0; i < n; i++ {
		e := w.Create()
		if err := w.Set(e, stPosC, &stPos{X: float64(i)}); err != nil {
			panic(err)
		}
		if i%10 == 0 {
			if err := w.Set(e, stVelC, &stVel{DX: 1}); err != nil {
				panic(err)
			}
		}
	}
	return w
}

// Disposed tombstones must be reclaimed by Tick: the entity table shrinks,
// and sparse-set entries are drained so dead entities never match again.
func TestTickSweepsTombstones(t *testing.T) {
	w := stWorld(100)
	for e := range w.Range(NewQuery().With(stPosC)) {
		w.Dispose(e)
	}
	if got := len(w.entities); got != 100 {
		t.Fatalf("entity table = %d before Tick, want 100 tombstones", got)
	}
	w.Tick()
	if got := len(w.entities); got != 0 {
		t.Fatalf("entity table = %d after Tick, want 0 (tombstones must be reclaimed)", got)
	}
	for _, cs := range w.compSets {
		if len(cs.dense) != 0 {
			t.Fatalf("sparse set %q holds %d dead entities after sweep", w.compNames[len(w.compSets)-1], len(cs.dense))
		}
	}
}

// The driving sparse set is mutated during iteration (self-disposal and
// self-component-removal): every originally matching entity must be
// visited exactly once — swap-removes must not skip the backfilled slot.
func TestRangeDriverSurvivesSelfMutation(t *testing.T) {
	for _, mode := range []string{"dispose", "remove-driving-component"} {
		t.Run(mode, func(t *testing.T) {
			w := stWorld(200)
			q := NewQuery().With(stPosC)

			visited := map[identity.CanonicalID]bool{}
			for e := range w.Range(q) {
				if visited[e.ID()] {
					t.Fatalf("entity %s visited twice", e.ID())
				}
				visited[e.ID()] = true
				switch mode {
				case "dispose":
					w.Dispose(e)
				case "remove-driving-component":
					w.Remove(e, stPosC)
				}
			}
			if len(visited) != 200 {
				t.Fatalf("visited %d entities, want 200 (swap-removes skipped slots)", len(visited))
			}
			if got := len(w.Execute(NewQuery().With(stPosC))); got != 0 {
				t.Fatalf("Position query matched %d after loop, want 0", got)
			}
			if mode == "dispose" && w.EntityCount() != 0 {
				t.Fatalf("EntityCount = %d, want 0", w.EntityCount())
			}
		})
	}
}

// CreateWithID overwriting an existing entry must strip the old state from
// the inverted index; otherwise stale component membership leaks into queries.
func TestCreateWithIDOverwriteCleansIndex(t *testing.T) {
	w := NewWorld()
	id := w.Create().ID()
	e0, ok := w.Entity(id)
	if !ok {
		t.Fatal("entity lookup failed")
	}
	if err := w.Set(e0, stPosC, &stPos{X: 1}); err != nil {
		t.Fatal(err)
	}
	w.Dispose(e0)

	e1 := w.CreateWithID(id) // overwrite the disposed tombstone
	if got := len(w.Execute(NewQuery().With(stPosC))); got != 0 {
		t.Fatalf("overwritten entity still matches Position query: %d", got)
	}
	if err := w.Set(e1, stVelC, &stVel{DX: 2}); err != nil {
		t.Fatal(err)
	}
	if got := len(w.Execute(NewQuery().With(stVelC))); got != 1 {
		t.Fatalf("Velocity query matched %d, want 1", got)
	}
}

// Re-adding a removed component within the same tick clears the removed
// marker and records the component as changed (existing semantics).
func TestReAddAfterRemoveChangeTracking(t *testing.T) {
	w := NewWorld()
	e := w.Create()
	_ = w.Set(e, stPosC, &stPos{X: 1})
	w.Remove(e, stPosC)
	cs := w.ChangeSet(e)
	if len(cs.Removed) != 1 || cs.Removed[0] != "st.Position" {
		t.Fatalf("after remove: %+v, want Removed=[st.Position]", cs)
	}
	_ = w.Set(e, stPosC, &stPos{X: 2})
	cs = w.ChangeSet(e)
	if len(cs.Removed) != 0 {
		t.Fatalf("after re-add: %+v, removed set must be cleared", cs)
	}
	if len(cs.Added) != 1 || len(cs.Changed) != 0 {
		t.Fatalf("after re-add: %+v, want Added=[st.Position]", cs)
	}
}

// Components stored with nil data count as present (map semantics). The
// component name is declared at construction: the World rejects undeclared
// names (type_registry.go, component-name whitelist).
func TestNilComponentDataCountsAsPresent(t *testing.T) {
	w := NewWorld(WithComponents("st.Nil"))
	e := w.Create()
	if err := w.SetComponent(e, "st.Nil", nil); err != nil {
		t.Fatal(err)
	}
	if !w.HasComponent(e, "st.Nil") {
		t.Fatal("nil-valued component must count as present")
	}
	data, ok := w.GetComponent(e, "st.Nil")
	if !ok || data != nil {
		t.Fatalf("GetComponent = (%v, %v), want (nil, true)", data, ok)
	}
}
