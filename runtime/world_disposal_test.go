package runtime

import (
	"testing"

	"github.com/qomos-w/spore/identity"
)

func disposeN(t *testing.T, w *World, n int) []identity.CanonicalID {
	t.Helper()
	ids := make([]identity.CanonicalID, 0, n)
	for i := 0; i < n; i++ {
		e := w.Create()
		ids = append(ids, e.ID())
		w.Dispose(e)
	}
	return ids
}

func TestDisposalJournalRecordsDisposeInOrder(t *testing.T) {
	w := NewWorld()
	ids := disposeN(t, w, 3)

	res := w.DisposalsSince(0)
	if res.Truncated {
		t.Fatalf("no truncation expected for 3 entries, got truncated")
	}
	if len(res.Entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(res.Entries))
	}
	for i, id := range ids {
		if res.Entries[i] != id {
			t.Fatalf("entry %d: expected %v, got %v", i, id, res.Entries[i])
		}
	}
	if res.ResumeSeq != 3 {
		t.Fatalf("expected ResumeSeq=3, got %d", res.ResumeSeq)
	}

	// Second poll from the watermark returns nothing new.
	next := w.DisposalsSince(res.ResumeSeq)
	if len(next.Entries) != 0 || next.Truncated {
		t.Fatalf("expected empty delta, got %d entries truncated=%v", len(next.Entries), next.Truncated)
	}
}

func TestDisposalJournalDisposeIsIdempotent(t *testing.T) {
	w := NewWorld()
	e := w.Create()
	w.Dispose(e)
	w.Dispose(e)

	res := w.DisposalsSince(0)
	if len(res.Entries) != 1 {
		t.Fatalf("idempotent Dispose must not double-record, got %d entries", len(res.Entries))
	}
}

func TestDisposalJournalCreateWithIDReplacement(t *testing.T) {
	w := NewWorld()
	id := w.generateID()
	e := w.CreateWithID(id)

	// Replacing a live entity is an implicit disposal.
	w.CreateWithID(id)
	res := w.DisposalsSince(0)
	if len(res.Entries) != 1 || res.Entries[0] != id {
		t.Fatalf("live replacement must be journaled once, got %v", res.Entries)
	}

	// Replacing an unswept tombstone must not double-record: Dispose
	// already journaled it.
	w.Dispose(e) // e aliases the current entity state via {id, world}
	w.CreateWithID(id)
	res = w.DisposalsSince(res.ResumeSeq)
	if len(res.Entries) != 1 {
		t.Fatalf("tombstone replacement must not re-journal, got %d entries", len(res.Entries))
	}
}

func TestDisposalJournalSurvivesTickSweep(t *testing.T) {
	w := NewWorld()
	ids := disposeN(t, w, 2)
	w.Tick()

	res := w.DisposalsSince(0)
	if len(res.Entries) != 2 || res.Entries[0] != ids[0] || res.Entries[1] != ids[1] {
		t.Fatalf("Tick tombstone sweep must not consume journal entries, got %v", res.Entries)
	}
}

func TestDisposalJournalRingWrapTruncates(t *testing.T) {
	w := NewWorld()
	disposeN(t, w, disposeLogCap+300)

	// An observer current up to entry 100: the requested window
	// 101..cap+300 exceeds the retained ring, so it must see Truncated
	// and the retained tail.
	res := w.DisposalsSince(100)
	if !res.Truncated {
		t.Fatalf("expected Truncated for a watermark older than the ring")
	}
	if uint64(len(res.Entries)) != uint64(disposeLogCap) {
		t.Fatalf("expected the retained tail of %d entries, got %d", disposeLogCap, len(res.Entries))
	}

	// A recent watermark within the retained window sees the exact range.
	res = w.DisposalsSince(res.ResumeSeq - 10)
	if res.Truncated {
		t.Fatalf("recent watermark must not truncate")
	}
	if len(res.Entries) != 10 {
		t.Fatalf("expected exactly 10 entries, got %d", len(res.Entries))
	}
}

func TestDisposalJournalFreshObserverStartsAtHead(t *testing.T) {
	w := NewWorld()
	disposeN(t, w, 3)

	head := w.DisposalsSince(0).ResumeSeq
	fresh := w.DisposalsSince(head)
	if len(fresh.Entries) != 0 || fresh.Truncated {
		t.Fatalf("fresh observer at head must see empty history, got %d entries", len(fresh.Entries))
	}
}

func TestDisposalJournalIndependentObservers(t *testing.T) {
	w := NewWorld()
	disposeN(t, w, 2)
	slow := w.DisposalsSince(0)

	disposeN(t, w, 2)
	fast := w.DisposalsSince(slow.ResumeSeq)
	if len(fast.Entries) != 2 || fast.Truncated {
		t.Fatalf("fast observer expected a clean 2-entry delta, got %d truncated=%v", len(fast.Entries), fast.Truncated)
	}

	// A slow observer that fell behind the ring replays a truncated tail.
	disposeN(t, w, disposeLogCap+10)
	behind := w.DisposalsSince(0)
	if !behind.Truncated || uint64(len(behind.Entries)) != uint64(disposeLogCap) {
		t.Fatalf("slow observer expected truncated tail of %d, got %d truncated=%v", disposeLogCap, len(behind.Entries), behind.Truncated)
	}
}
