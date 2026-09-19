package runtime_test

import (
	"testing"

	"github.com/qomos-w/spore/identity"
	"github.com/qomos-w/spore/runtime"
)

// Range is the streaming counterpart of Execute: same filters, no slice.
// These tests follow gameexample_test.go's component conventions.

type rngPos struct{ X, Y float64 }
type rngVel struct{ DX, DY float64 }
type rngTag struct{ On bool }

var (
	rngPosC = runtime.NewComponent[rngPos]("rng.Position")
	rngVelC = runtime.NewComponent[rngVel]("rng.Velocity")
	rngTagC = runtime.NewComponent[rngTag]("rng.Tag")
)

func rngSetup(t *testing.T) (*runtime.World, runtime.Entity, runtime.Entity) {
	t.Helper()
	w := runtime.NewWorld()

	mover := w.Create()
	if err := w.Set(mover, rngPosC, &rngPos{X: 0, Y: 0}); err != nil {
		t.Fatal(err)
	}
	if err := w.Set(mover, rngVelC, &rngVel{DX: 1, DY: 0}); err != nil {
		t.Fatal(err)
	}

	idler := w.Create()
	if err := w.Set(idler, rngPosC, &rngPos{X: 5, Y: 5}); err != nil {
		t.Fatal(err)
	}

	tagged := w.Create()
	if err := w.Set(tagged, rngTagC, &rngTag{On: true}); err != nil {
		t.Fatal(err)
	}
	return w, mover, idler
}

func TestRange_MatchesSameSetAsExecute(t *testing.T) {
	w, mover, idler := rngSetup(t)
	q := runtime.NewQuery().With(rngPosC).Without(rngTagC)

	executed := map[identity.CanonicalID]bool{}
	for _, e := range w.Execute(q) {
		executed[e.ID()] = true
	}
	if len(executed) != 2 || !executed[mover.ID()] || !executed[idler.ID()] {
		t.Fatalf("Execute baseline wrong: %v", executed)
	}

	ranged := map[identity.CanonicalID]bool{}
	for e := range w.Range(q) {
		if ranged[e.ID()] {
			t.Fatal("Range yielded an entity twice")
		}
		ranged[e.ID()] = true
	}
	if len(ranged) != 2 || !ranged[mover.ID()] || !ranged[idler.ID()] {
		t.Fatalf("Range and Execute disagree: %v", ranged)
	}
}

func TestRange_BreakStopsTraversal(t *testing.T) {
	w, _, _ := rngSetup(t)
	q := runtime.NewQuery().With(rngPosC)

	seen := 0
	for range w.Range(q) {
		seen++
		if seen == 1 {
			break // stop after the first match regardless of order
		}
	}
	if seen != 1 {
		t.Fatalf("break visited %d entities, want 1", seen)
	}
}

func TestRange_AllowsComponentMutationAndDispose(t *testing.T) {
	w, mover, idler := rngSetup(t)
	q := runtime.NewQuery().With(rngPosC)

	// Systems often strip components or kill entities mid-walk; Range must
	// tolerate both (only entity creation is off-limits during iteration).
	for e := range w.Range(q) {
		if e == mover {
			w.Remove(e, rngVelC)
			w.Dispose(e)
		}
	}

	if w.IsAlive(mover) {
		t.Fatal("mover must be disposed after Range loop")
	}
	if !w.IsAlive(idler) {
		t.Fatal("idler must survive the loop")
	}
	if got := len(w.Execute(runtime.NewQuery().With(rngPosC))); got != 1 {
		t.Fatalf("alive Position holders = %d, want 1 (idler)", got)
	}
}

func TestRange_ZeroCopyAliasesStoredComponents(t *testing.T) {
	w, _, _ := rngSetup(t)
	q := runtime.NewQuery().With(rngVelC)

	for e := range w.Range(q) {
		vel, ok := w.Get(e, rngVelC)
		if !ok {
			t.Fatal("matched entity missing Velocity")
		}
		vel.DX = 42 // direct write through the yielded handle
	}
	for _, e := range w.Execute(q) {
		vel, _ := w.Get(e, rngVelC)
		if vel.DX != 42 {
			t.Fatalf("DX = %v, want 42 (Range handle must alias storage)", vel.DX)
		}
	}
}

func TestRange_EmptyWorldAndNoMatches(t *testing.T) {
	w := runtime.NewWorld()
	for range w.Range(runtime.NewQuery().With(rngPosC)) {
		t.Fatal("empty world must yield nothing")
	}

	w2, _, _ := rngSetup(t)
	for range w2.Range(runtime.NewQuery().With(rngPosC).Without(rngPosC)) {
		t.Fatal("contradictory query must yield nothing")
	}
}

func TestRange_ChangeFilters(t *testing.T) {
	w, mover, _ := rngSetup(t)

	// SetComponent during spawn marks components added for this tick;
	// WhenAdded must behave the same through Range as through Execute.
	q := runtime.NewQuery().OnAdded(rngPosC)

	ranged := 0
	for range w.Range(q) {
		ranged++
	}
	if ranged != 2 {
		t.Fatalf("WhenAdded via Range matched %d, want 2", ranged)
	}

	w.Tick() // clears added/changed/removed sets
	if got := len(w.Execute(q)); got != 0 {
		t.Fatalf("after Tick, WhenAdded should match 0, got %d", got)
	}

	w.Mark(mover, rngPosC)
	marked := 0
	for range w.Range(runtime.NewQuery().OnChanged(rngPosC)) {
		marked++
	}
	if marked != 1 {
		t.Fatalf("WhenChanged via Range matched %d, want 1", marked)
	}
}
