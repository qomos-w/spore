// host_reap_test.go — contract tests for the HostFleet disposal-journal
// reap (ecsbind/host.go:reap). Entities disposed outside the fleet API —
// World.Dispose called directly, as script-side WorldBinding dispose or
// Go systems do — must be reclaimed from the fleet map on the next
// access-path call (Move/Hp/Dispose/LookUp/Count), without the fleet
// ever having been told.
package ecsbind_test

import (
	"testing"

	"github.com/qomos-w/spore/ecsbind"
)

// spawnLive spawns n hosts via the Go surface (no script runtime needed)
// and returns their handles for external disposal.
func spawnLive(t *testing.T, fleet *ecsbind.HostFleet, n int) []*ecsbind.UnitHost {
	t.Helper()
	hosts := make([]*ecsbind.UnitHost, 0, n)
	for i := 0; i < n; i++ {
		id, err := fleet.Spawn(float64(i))
		if err != nil {
			t.Fatalf("Spawn %d: %v", i, err)
		}
		u, ok := fleet.LookUp(id)
		if !ok {
			t.Fatalf("Spawn %d: host not visible via LookUp", i)
		}
		hosts = append(hosts, u)
	}
	return hosts
}

// TestHostFleetReapsExternalDispose: a host disposed through the World
// (not through the fleet) stops resolving and stops counting.
func TestHostFleetReapsExternalDispose(t *testing.T) {
	w, fleet, _ := newHostWorld(t)
	hosts := spawnLive(t, fleet, 1)
	id := hosts[0].Ref.Entity.ID().String()
	if fleet.Count() != 1 {
		t.Fatalf("expected 1 host, got %d", fleet.Count())
	}

	w.Dispose(hosts[0].Ref.Entity) // external disposal, fleet not involved

	if _, ok := fleet.LookUp(id); ok {
		t.Fatal("externally disposed host must not resolve after reap")
	}
	if fleet.Count() != 0 {
		t.Fatalf("expected 0 hosts after reap, got %d", fleet.Count())
	}
	if err := fleet.Move(id, 1); err == nil {
		t.Fatal("Move on reaped id must return ErrStaleEntity")
	}
}

// TestHostFleetReapTruncatedFallsBackToSweep: when more entities are
// disposed externally than the journal ring retains, the fleet falls
// back to a liveness sweep of its own map (DisposalLogResult.Truncated).
func TestHostFleetReapTruncatedFallsBackToSweep(t *testing.T) {
	w, fleet, _ := newHostWorld(t)
	hosts := spawnLive(t, fleet, 300)
	if fleet.Count() != 300 {
		t.Fatalf("expected 300 hosts, got %d", fleet.Count())
	}

	// Dispose 299 externally — more than the journal ring retains, and
	// enough to leave a mix of stale and live entries behind.
	for _, u := range hosts[:299] {
		w.Dispose(u.Ref.Entity)
	}

	if fleet.Count() != 1 {
		t.Fatalf("expected 1 surviving host after sweep, got %d", fleet.Count())
	}
	if _, ok := fleet.LookUp(hosts[299].Ref.Entity.ID().String()); !ok {
		t.Fatal("surviving host must still resolve")
	}
}

// TestHostFleetReapCreateWithIDReplacement: replacing a live entity via
// CreateWithID is journaled as an implicit disposal and reaped too.
func TestHostFleetReapCreateWithIDReplacement(t *testing.T) {
	w, fleet, _ := newHostWorld(t)
	hosts := spawnLive(t, fleet, 1)
	id := hosts[0].Ref.Entity.ID()

	w.CreateWithID(id) // reload-style replacement, fleet not involved

	if fleet.Count() != 0 {
		t.Fatalf("replaced host must be reaped, got %d", fleet.Count())
	}
}
