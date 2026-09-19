package runtime

import (
	"testing"
)

// Entity generation closes the ABA hole: an ID reused by CreateWithID
// must not silently re-validate old handles issued against the previous
// entity (world.go:entityState checks the handle's epoch).

func TestEntityGenerationStaleAfterSweepAndReuse(t *testing.T) {
	w := NewWorld()
	id := w.generateID()
	old := w.CreateWithID(id)
	if !w.IsAlive(old) {
		t.Fatal("expected fresh entity alive")
	}

	w.Dispose(old)
	w.Tick() // sweep the tombstone so reuse is a fresh create, not a replace

	reborn := w.CreateWithID(id)
	if !w.IsAlive(reborn) {
		t.Fatal("expected reused-ID entity alive")
	}
	if w.IsAlive(old) {
		t.Fatal("old handle must be stale after ID reuse (ABA)")
	}
	if err := w.SetComponent(old, "Position", map[string]any{"X": 1}); err == nil {
		t.Fatal("SetComponent via stale handle must error")
	}
	if err := w.SetComponent(reborn, "Position", map[string]any{"X": 2}); err != nil {
		t.Fatalf("SetComponent via new handle: %v", err)
	}
}

func TestEntityGenerationStaleAfterLiveReplacement(t *testing.T) {
	w := NewWorld()
	id := w.generateID()
	first := w.CreateWithID(id)
	second := w.CreateWithID(id) // live replacement

	if w.IsAlive(first) {
		t.Fatal("handle from replaced entity must be stale")
	}
	if !w.IsAlive(second) {
		t.Fatal("replacement handle must be alive")
	}
}

func TestEntityGenerationStaleHandleCannotKillRebornEntity(t *testing.T) {
	w := NewWorld()
	id := w.generateID()
	old := w.CreateWithID(id)
	w.Dispose(old)
	w.Tick()

	reborn := w.CreateWithID(id)
	w.Dispose(old) // must be a no-op on the current entity

	if !w.IsAlive(reborn) {
		t.Fatal("stale handle Dispose must not kill the reborn entity")
	}
}

func TestEntityGenerationDisposalJournalRecordsReuseOnlyOnce(t *testing.T) {
	w := NewWorld()
	id := w.generateID()
	old := w.CreateWithID(id)
	w.Dispose(old)
	w.Tick()

	res := w.DisposalsSince(0)
	if len(res.Entries) != 1 {
		t.Fatalf("expected exactly the Dispose journal entry, got %d", len(res.Entries))
	}
	w.CreateWithID(id) // fresh create after sweep: no replacement disposal
	res = w.DisposalsSince(res.ResumeSeq)
	if len(res.Entries) != 0 {
		t.Fatalf("fresh create after sweep must not journal, got %d", len(res.Entries))
	}
}

func TestEntityGenerationQueryHandlesResolve(t *testing.T) {
	w := NewWorld()
	e := w.Create()
	if err := w.SetComponent(e, "Position", map[string]any{"X": 1}); err != nil {
		t.Fatalf("SetComponent: %v", err)
	}

	for qe := range w.Range(NewQuery().Has("Position")) {
		if !w.IsAlive(qe) {
			t.Fatal("query-issued handle must resolve")
		}
	}
}

func TestEntityGenerationMakeEntitySemantics(t *testing.T) {
	w := NewWorld()
	id := w.generateID()
	e := w.CreateWithID(id)

	made := MakeEntity(id, w)
	if !w.IsAlive(made) {
		t.Fatal("MakeEntity against an existing state must resolve")
	}
	_ = e

	fresh := w.generateID()
	pre := MakeEntity(fresh, w) // no state yet: generation 0
	w.CreateWithID(fresh)
	if w.IsAlive(pre) {
		t.Fatal("pre-creation MakeEntity handle must be stale after the entity exists")
	}
}

// MakeEntity(id, nil) — the detached-handle pattern used by downstream
// tests — must not panic (regression: v0.1.2 dereferenced the World).
func TestEntityGenerationMakeEntityNilWorld(t *testing.T) {
	w := NewWorld()
	id := w.generateID()
	w.CreateWithID(id)

	detached := MakeEntity(id, nil)
	if detached.world != nil {
		t.Fatal("MakeEntity with nil World must keep a nil World")
	}
	if w.IsAlive(detached) {
		t.Fatal("nil-World handle must never resolve")
	}
}
