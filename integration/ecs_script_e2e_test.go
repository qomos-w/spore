// Package integration_test exercises the script-side ECS binding
// (ecsbind.WorldBinding + ecsbind.HostFleet) end-to-end. These tests
// are not redundant with ecsbind/*_test.go: they exercise the full
// pipeline — World facade + Host fleet facade, sharing one runtime.World
// — through a real script runtime. Each test pins a contract that
// "scripts can drive both facades against the same World storage" rather
// than exercising one facade in isolation.
//
// The four contracts locked here:
//
//  1. Script full-link lifecycle: spawn → set → tick → query(whenChanged)
//     → view/apply batch RMW. The script owns the world; the test only
//     seeds initial component values and observes post-tick state.
//
//  2. Host factory mixed with World facade: Fleet.spawn_proxy from
//     script returns a live Unit proxy whose move/hp/dispose methods
//     mutate the same World the World facade reads from. The two
//     facades read the same storage — there is no "host side" and
//     "ECS side", just one World with two projections.
//
//  3. Dual-facade interop: a script that imports both `host` (Fleet/Unit)
//     and `ecs` (World) writes through one and reads through the other
//     without divergence. Plain ECS entities (created via World.spawn +
//     SetComponent) and host entities (created via Fleet.spawn_proxy)
//     show up in the same Has()/query result.
//
//  4. Full-chain observability: changes, whenChanged, auto-Mark from
//     PatchEntity — the four change-tracking buckets must be visible
//     to scripts through the same facade.
package integration_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/qomos-w/spore/ecsbind"
	"github.com/qomos-w/spore/identity"
	"github.com/qomos-w/spore/runtime"
	"github.com/qomos-w/spore/schema"
	"github.com/qomos-w/spore/script"
)

// ----------------------------------------------------------------------------
// Shared scaffolding
// ----------------------------------------------------------------------------

// e2eHealth is the runtime component shape used by the script-side
// full-link test. Same shape as ecsbind.ecsHealth but redeclared here so
// the integration package does not depend on internal test helpers.
type e2eHealth struct {
	Value int
}

// e2ePos is the runtime component shape for plain ECS entities. Matches
// the host aggregate's Position field so the dual-facade test can use
// the same descriptor name for both surfaces.
type e2ePos struct {
	X, Y float64
}

func e2eHealthDesc() schema.ObjectDesc {
	return schema.ObjectDesc{
		Name: "Health",
		Fields: []schema.FieldDesc{
			{Name: "Value", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}},
		},
	}
}

func e2ePosDesc() schema.ObjectDesc {
	return schema.ObjectDesc{
		Name: "Position",
		Fields: []schema.FieldDesc{
			{Name: "X", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "double"}},
			{Name: "Y", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "double"}},
		},
	}
}

// e2eHostPosDesc is the schema descriptor for the host aggregate's
// UnitPosition. The host registry (ecsbind.HostRegistry) registers this
// exact name with reflect.TypeOf(UnitPosition{}), so the descriptor
// below must mirror UnitPosition's field shape for the World facade to
// read the same storage slot the host aggregate writes through.
func e2eHostPosDesc() schema.ObjectDesc {
	return schema.ObjectDesc{
		Name: "host.Position",
		Fields: []schema.FieldDesc{
			{Name: "X", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "double"}},
			{Name: "Y", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "double"}},
		},
	}
}

// seedWorldE2E creates a World with N entities each carrying Health
// and Position components. Used by the full-link RMW test to give the
// script a starting state.
func seedWorldE2E(t *testing.T, w *runtime.World, n int) []string {
	t.Helper()
	ids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		e := w.Create()
		if err := w.SetComponent(e, "Health", &e2eHealth{Value: 100}); err != nil {
			t.Fatalf("seed SetComponent Health: %v", err)
		}
		if err := w.SetComponent(e, "Position", &e2ePos{X: float64(i), Y: 0}); err != nil {
			t.Fatalf("seed SetComponent Position: %v", err)
		}
		w.ClearChanges(e)
		ids = append(ids, e.ID().String())
	}
	return ids
}

// lookupEntity decodes a hex canonical id and asks the World for the
// entity handle. Mirrors ecsbind_test.go:lookupEntity but lives here so
// the test file does not depend on the ecsbind_test package.
func lookupEntity(t *testing.T, w *runtime.World, hex string) runtime.Entity {
	t.Helper()
	cid, err := identity.ParseCanonicalID(hex)
	if err != nil {
		t.Fatalf("ParseCanonicalID(%q): %v", hex, err)
	}
	return runtime.MakeEntity(cid, w)
}

// ----------------------------------------------------------------------------
// 1. Script full-link lifecycle: spawn → set → tick → query(whenChanged) →
//    view/apply batch RMW. The script owns the world; the test seeds a
//    starting Health=100, then drives a single RMW cycle from script and
//    asserts the resulting World state.
// ----------------------------------------------------------------------------

// TestE2E_ScriptFullLinkRMWSingleCycle is the canonical happy-path
// end-to-end test. It walks every script-callable surface method on the
// World facade in the order documented in the host API:
//
//   spawn → set (chatty) → tick → query (whenChanged) → view →
//   mutate in script → apply (batch) → tick
//
// The script returns a small summary map (count, sum) so the host test
// can verify all three stages completed correctly without poking the
// World storage directly during the run.
func TestE2E_ScriptFullLinkRMWSingleCycle(t *testing.T) {
	rt, err := script.NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	w := runtime.NewWorld()
	bind := ecsbind.New(w).
		RegisterComponent("Health", e2eHealthDesc()).
		RegisterComponent("Position", e2ePosDesc())
	if err := bind.Bind(rt); err != nil {
		t.Fatalf("WorldBinding.Bind: %v", err)
	}

	// Seed 5 entities with Health=100.
	seededIDs := seedWorldE2E(t, w, 5)

	const src = `import World from "ecs"

// Stage 1: spawn one more entity (caller pre-installs Health/Position
// via the Go side because PatchEntity requires the component to exist
// first). Returns the new id so the host can verify the spawn was
// visible in the World, then mutates the pre-installed component via
// the facade so the mutation goes through the same path as a script
// author would use.
export fun spawn_then_set(id: string): int {
    var n: int = World.set(id, "Health", {"Value": 50})
    n = World.set(id, "Position", {"X": 999.0, "Y": 999.0}) + n
    return n
}

// Stage 2: query the entities whose Health changed THIS tick (i.e.
// since the last Tick). Returns the count of changed entities.
export fun count_changed(): int {
    var ids: array<string> = World.query([], [], [], [], ["Health"], [])
    return len(ids)
}

// Stage 3: batch RMW. View all Health-having entities, decrement by 10
// (floor at 0), apply the patch, tick. Returns a summary map with
// count + total value so the host can verify the drain arithmetic
// without poking the World storage directly. The map uses <string, any>
// to keep the FFI conversion simple — the script casts on its end.
export fun drain(): map<string, any> {
    var out: map<string, any> = World.view(["Health"], [])
    var ids: array<string> = (out["ids"] as array<string>)
    var data: map<string, any> = (out["data"] as map<string, any>)
    var healthArr: array<map<string, any>> = (data["Health"] as array<map<string, any>>)
    var i: int = 0
    var total: int = 0
    var n: int = len(ids)
    while (i < n) {
        var h: map<string, any> = healthArr[i]
        var v: int = (h["Value"] as int) - 10
        if (v < 0) { v = 0 }
        h["Value"] = v
        total = total + v
        i = i + 1
    }
    var applied: int = World.apply("Health", ids, healthArr)
    World.tick()
    return {"count": n, "applied": applied, "total": total}
}`
	if err := rt.LoadSource("e2e", src); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	// Stage 1: spawn a new entity from the Go side (PatchEntity
	// requires the component to exist first), pre-install Health and
	// Position, clear the bookkeeping, then have the script mutate
	// them via the facade. This exercises the same spawn-and-mutate
	// pattern a script author would use, with the host standing in
	// for the install.
	newEnt := w.Create()
	if err := w.SetComponent(newEnt, "Health", &e2eHealth{Value: 0}); err != nil {
		t.Fatalf("install Health: %v", err)
	}
	if err := w.SetComponent(newEnt, "Position", &e2ePos{}); err != nil {
		t.Fatalf("install Position: %v", err)
	}
	w.ClearChanges(newEnt)
	newID := newEnt.ID().String()
	for _, sid := range seededIDs {
		if sid == newID {
			t.Fatalf("seed returned an id colliding with the new spawn %q", sid)
		}
	}
	res, err := rt.Call("spawn_then_set", newID)
	if err != nil {
		t.Fatalf("Call spawn_then_set: %v", err)
	}
	n, ok := res.Value.(int)
	if !ok {
		t.Fatalf("spawn_then_set: want int, got (%T, %v)", res.Value, res.Value)
	}
	if n != 3 {
		t.Fatalf("spawn_then_set: want 3 mutations (1 Health + 2 Position), got %d", n)
	}
	// The new entity must be alive in the World and not in the seeded set.
	ent := lookupEntity(t, w, newID)
	if !w.IsAlive(ent) {
		t.Fatalf("spawn_then_set: entity %s not alive in World", newID)
	}
	// Position should be (999, 999) from stage 1.
	posRaw, ok := w.GetComponent(ent, "Position")
	if !ok {
		t.Fatalf("spawn_then_set: Position missing on %s", newID)
	}
	pos := posRaw.(*e2ePos)
	if pos.X != 999.0 || pos.Y != 999.0 {
		t.Fatalf("spawn_then_set: Position want (999, 999), got (%v, %v)", pos.X, pos.Y)
	}

	// Stage 2: query WhenChanged Health. Stage 1's two Set calls
	// marked Health on the new entity as changed; the 5 seeded entities
	// were ClearChanges'd so they don't show up. Expected count = 1.
	res, err = rt.Call("count_changed")
	if err != nil {
		t.Fatalf("Call count_changed: %v", err)
	}
	cnt, ok := res.Value.(int)
	if !ok {
		t.Fatalf("count_changed: want int, got %T", res.Value)
	}
	if cnt != 1 {
		t.Fatalf("count_changed after spawn_then_set: want 1, got %d", cnt)
	}

	// Stage 2 cleanup: tick clears the change set so stage 3 starts
	// from a clean baseline.
	w.Tick()

	// Stage 3: batch RMW.
	res, err = rt.Call("drain")
	if err != nil {
		t.Fatalf("Call drain: %v", err)
	}
	summary, ok := res.Value.(map[string]any)
	if !ok {
		t.Fatalf("drain: want map[string]any, got %T (%v)", res.Value, res.Value)
	}
	// Drain decrements every Health by 10 (floor 0). Seeded 5 = 100
	// each; new 1 = 50. After: 5 * 90 + 40 = 490. Count = 6.
	wantCount, _ := summary["count"].(int)
	wantApplied, _ := summary["applied"].(int)
	wantTotal, _ := summary["total"].(int)
	if wantCount != 6 {
		t.Fatalf("drain.count: want 6 (5 seeded + 1 spawned), got %d", wantCount)
	}
	if wantApplied != 6 {
		t.Fatalf("drain.applied: want 6, got %d", wantApplied)
	}
	if wantTotal != 490 {
		t.Fatalf("drain.total: want 490 (5*90 + 40), got %d", wantTotal)
	}

	// Post-drain World state must reflect the same totals.
	ents := w.Execute(runtime.NewQuery().Has("Health"))
	if len(ents) != 6 {
		t.Fatalf("after drain: want 6 alive entities, got %d", len(ents))
	}
	var gotTotal int
	for _, e := range ents {
		h, ok := w.GetComponent(e, "Health")
		if !ok {
			t.Fatalf("after drain: entity %v has no Health", e.ID())
		}
		gotTotal += h.(*e2eHealth).Value
	}
	if gotTotal != 490 {
		t.Fatalf("after drain: World total want 490, got %d", gotTotal)
	}
}

// TestE2E_ScriptFullLinkRMWSingleCycle_Large is the same shape as the
// test above but at N=200, exercising the view/apply batch path against
// a non-trivial entity count. The batch envelope is kept comfortable
// with an explicit 1 MiB heap (the default is 4 MiB since v0.1.2; this
// pin predates that and doubles as an explicit-budget exercise).
func TestE2E_ScriptFullLinkRMWSingleCycle_Large(t *testing.T) {
	rt, err := script.NewRuntimeWith(script.RuntimeOptions{
		VMHeapBytes: 1 << 20,
		VMHeapSlots: 4096,
	})
	if err != nil {
		t.Fatalf("NewRuntimeWith: %v", err)
	}
	w := runtime.NewWorld()
	bind := ecsbind.New(w).
		RegisterComponent("Health", e2eHealthDesc()).
		RegisterComponent("Position", e2ePosDesc())
	if err := bind.Bind(rt); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	seedWorldE2E(t, w, 200)

	const src = `import World from "ecs"

export fun drain(): int {
    var out: map<string, any> = World.view(["Health"], [])
    var ids: array<string> = (out["ids"] as array<string>)
    var data: map<string, any> = (out["data"] as map<string, any>)
    var arr: array<map<string, any>> = (data["Health"] as array<map<string, any>>)
    var i: int = 0
    while (i < len(ids)) {
        var h: map<string, any> = arr[i]
        h["Value"] = 0
        i = i + 1
    }
    var applied: int = World.apply("Health", ids, arr)
    World.tick()
    return applied
}`
	if err := rt.LoadSource("e2e-large", src); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	res, err := rt.Call("drain")
	if err != nil {
		t.Fatalf("Call drain: %v", err)
	}
	applied, ok := res.Value.(int)
	if !ok {
		t.Fatalf("drain: want int, got %T", res.Value)
	}
	if applied != 200 {
		t.Fatalf("drain N=200: want applied=200, got %d", applied)
	}
	// Post-drain: every Health.Value == 0.
	ents := w.Execute(runtime.NewQuery().Has("Health"))
	if len(ents) != 200 {
		t.Fatalf("after drain: want 200 alive, got %d", len(ents))
	}
	for _, e := range ents {
		h, _ := w.GetComponent(e, "Health")
		if h.(*e2eHealth).Value != 0 {
			t.Fatalf("entity %s after drain: want Value=0, got %v", e.ID(), h.(*e2eHealth).Value)
		}
	}
}

// ----------------------------------------------------------------------------
// 2. Host factory mixed with World facade: spawn_proxy returns a live
//    Unit proxy; both Fleet.* and World.* see the same storage.
// ----------------------------------------------------------------------------

// TestE2E_HostFactoryMixedWithWorldFacade drives Fleet.spawn_proxy from
// script, exercises the proxy's move/hp, and confirms every mutation
// is observable through the World facade on the same World. Also seeds
// a plain ECS entity with the same "host.Position" schema name and
// confirms both surfaces see it together.
func TestE2E_HostFactoryMixedWithWorldFacade(t *testing.T) {
	rt, err := script.NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	w := runtime.NewWorld()
	w.AddRegistry(ecsbind.HostRegistry())
	wb := ecsbind.New(w).RegisterComponent("host.Position", e2eHostPosDesc())
	fleet := ecsbind.NewHostFleet(w)
	if err := fleet.Bind(rt); err != nil {
		t.Fatalf("HostFleet.Bind: %v", err)
	}
	if err := wb.Bind(rt); err != nil {
		t.Fatalf("WorldBinding.Bind: %v", err)
	}

	// Pre-seed a plain ECS entity with host.Position so the dual
	// surface starts with both kinds present.
	plainEntity := w.Create()
	if err := w.SetComponent(plainEntity, "host.Position", &ecsbind.UnitPosition{X: 7, Y: 0}); err != nil {
		t.Fatalf("install host.Position on plain entity: %v", err)
	}
	w.ClearChanges(plainEntity)
	plainID := plainEntity.ID().String()

	const src = `import Fleet from "host"
import World from "ecs"

export fun factory_then_drive(x: double): map<string, any> {
    var u: Unit = Fleet.spawn_proxy(x)
    u.move(1.0)
    u.move(2.0)
    var hp: int = u.hp()
    return {"hp": hp, "count": Fleet.count()}
}

// World-side cross-check: query all entities with host.Position, count
// them and sum their X values into a summary map.
export fun world_summary(): map<string, any> {
    var ids: array<string> = World.query(["host.Position"], [], [], [], [], [])
    var total_x: double = 0.0
    var i: int = 0
    while (i < len(ids)) {
        var v: map<string, any> = World.get(ids[i], "host.Position")
        if (v != null) {
            var xv: double = (v["X"] as double)
            total_x = total_x + xv
        }
        i = i + 1
    }
    return {"count": len(ids), "total_x": total_x}
}

// Dispose the proxy and check the world count. Same root module as
// the other exports to keep the script runtime single-root.
export fun dispose_one(): int {
    var u: Unit = Fleet.spawn_proxy(0.0)
    u.dispose()
    return Fleet.count()
}

export fun world_count_position(): int {
    var ids: array<string> = World.query(["host.Position"], [], [], [], [], [])
    return len(ids)
}`
	if err := rt.LoadSource("host-ecs", src); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	// Factory round-trip.
	res, err := rt.Call("factory_then_drive", 10.0)
	if err != nil {
		t.Fatalf("Call factory_then_drive: %v", err)
	}
	sum, ok := res.Value.(map[string]any)
	if !ok {
		t.Fatalf("factory_then_drive: want map[string]any, got %T (%v)", res.Value, res.Value)
	}
	hp, _ := sum["hp"].(int)
	count, _ := sum["count"].(int)
	if hp != 100 {
		t.Fatalf("factory_then_drive.hp: want 100 (default), got %d", hp)
	}
	if count != 1 {
		t.Fatalf("factory_then_drive.count: want 1, got %d", count)
	}

	// World-side cross-check. The fleet proxy and the pre-seeded
	// plain entity are BOTH visible to World.query.
	res, err = rt.Call("world_summary")
	if err != nil {
		t.Fatalf("Call world_summary: %v", err)
	}
	ws, ok := res.Value.(map[string]any)
	if !ok {
		t.Fatalf("world_summary: want map[string]any, got %T", res.Value)
	}
	wcount, _ := ws["count"].(int)
	wtot, _ := ws["total_x"].(float64)
	if wcount != 2 {
		t.Fatalf("world_summary.count: want 2 (1 fleet + 1 plain), got %d", wcount)
	}
	// Plain X=7, Fleet proxy X=13. Sum = 20.
	if wtot != 20.0 {
		t.Fatalf("world_summary.total_x: want 20.0 (7+13), got %v", wtot)
	}

	// Host-side ground truth: the plain entity must NOT be in the
	// fleet map (Fleet only tracks its own spawned hosts).
	if got, ok := fleet.LookUp(plainID); ok {
		t.Fatalf("plain entity %s should not be in the fleet map (got host=%v)", plainID, got)
	}
	if !w.IsAlive(plainEntity) {
		t.Fatalf("plain entity %s not alive after script run", plainID)
	}

	// Confirm via runtime.Get: the fleet proxy's host.Position.X == 13.0.
	hostPos := w.Execute(runtime.NewQuery().Has("host.Position"))
	var saw13, saw7 bool
	for _, e := range hostPos {
		raw, ok := w.GetComponent(e, "host.Position")
		if !ok {
			t.Fatalf("entity %v missing host.Position", e.ID())
		}
		p := raw.(*ecsbind.UnitPosition)
		switch p.X {
		case 13.0:
			saw13 = true
		case 7.0:
			saw7 = true
		}
	}
	if !saw13 {
		t.Fatal("fleet proxy's host.Position.X=13.0 not found via World.Get")
	}
	if !saw7 {
		t.Fatal("plain entity's host.Position.X=7.0 not found via World.Get")
	}

	// Dispose the proxy from script; the fleet already has 1 host
	// from the earlier factory_then_drive call, so after this
	// spawn-and-dispose the count stays at 1.
	res, err = rt.Call("dispose_one")
	if err != nil {
		t.Fatalf("Call dispose_one: %v", err)
	}
	postCount, _ := res.Value.(int)
	if postCount != 1 {
		t.Fatalf("post-spawn-and-dispose fleet.count: want 1 (the factory_then_drive host still alive), got %d", postCount)
	}

	// The plain entity should still be alive — disposing the fleet
	// proxy must not touch other entities. The query should still
	// see 2 entities: the plain one + the factory_then_drive host.
	res, err = rt.Call("world_count_position")
	if err != nil {
		t.Fatalf("Call world_count_position: %v", err)
	}
	postWorld, _ := res.Value.(int)
	if postWorld != 2 {
		t.Fatalf("post-dispose World.query count: want 2 (plain + factory host), got %d", postWorld)
	}

	if !w.IsAlive(plainEntity) {
		t.Fatal("plain entity should still be alive after Fleet proxy dispose")
	}
}

// ----------------------------------------------------------------------------
// 3. Dual-facade interop stress: many entities of mixed origin, one
//    query, both facades agree on every value.
// ----------------------------------------------------------------------------

// TestE2E_DualFacadeMixedEntities drives the two facades from the same
// script and asserts no divergence: every host-entity mutation is
// visible to World.query, and every World-driven spawn is visible to
// Fleet.count (Fleet only knows about hosts it created, but the World
// facade knows about all entities).
func TestE2E_DualFacadeMixedEntities(t *testing.T) {
	rt, err := script.NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	w := runtime.NewWorld()
	w.AddRegistry(ecsbind.HostRegistry())
	wb := ecsbind.New(w).RegisterComponent("host.Position", e2eHostPosDesc())
	fleet := ecsbind.NewHostFleet(w)
	if err := fleet.Bind(rt); err != nil {
		t.Fatalf("fleet.Bind: %v", err)
	}
	if err := wb.Bind(rt); err != nil {
		t.Fatalf("wb.Bind: %v", err)
	}

	// Seed two plain ECS entities with host.Position. The script will
	// spawn two fleet proxies on top and assert all four coexist.
	for i := 0; i < 2; i++ {
		e := w.Create()
		if err := w.SetComponent(e, "host.Position", &ecsbind.UnitPosition{X: float64(i * 100), Y: 0}); err != nil {
			t.Fatalf("seed plain SetComponent: %v", err)
		}
		w.ClearChanges(e)
	}

	const src = `import Fleet from "host"
import World from "ecs"

export fun mix(): map<string, any> {
    // Two fleet proxies at X=10 and X=20. After move(5) and move(7)
    // the X values become 15 and 32.
    var u1: Unit = Fleet.spawn_proxy(10.0)
    var u2: Unit = Fleet.spawn_proxy(20.0)
    u1.move(5.0)
    u2.move(7.0)
    u2.move(5.0)

    // World-side observation. Total entity count should be 4 (2 plain
    // + 2 fleet). Sum of X should be 0 + 100 + 15 + 32 = 147.
    var ids: array<string> = World.query(["host.Position"], [], [], [], [], [])
    var total: double = 0.0
    var i: int = 0
    while (i < len(ids)) {
        var v: map<string, any> = World.get(ids[i], "host.Position")
        if (v != null) {
            total = total + (v["X"] as double)
        }
        i = i + 1
    }
    return {"total": total, "n": len(ids), "fleet_count": Fleet.count()}
}

// Disposing a fleet proxy via the proxy class (u.dispose()) must
// remove it from the World, the fleet map, AND from any subsequent
// World.query. Then re-running the same World query must see only
// the 3 remaining entities. Same root module as mix.
export fun kill_one_and_count(): int {
    var u: Unit = Fleet.spawn_proxy(50.0)
    u.dispose()
    var ids: array<string> = World.query(["host.Position"], [], [], [], [], [])
    return len(ids)
}

export fun world_count_only(): int {
    var ids: array<string> = World.query(["host.Position"], [], [], [], [], [])
    return len(ids)
}`
	if err := rt.LoadSource("mix", src); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	res, err := rt.Call("mix")
	if err != nil {
		t.Fatalf("Call mix: %v", err)
	}
	sum, ok := res.Value.(map[string]any)
	if !ok {
		t.Fatalf("mix: want map[string]any, got %T", res.Value)
	}
	total, _ := sum["total"].(float64)
	n, _ := sum["n"].(int)
	fleetN, _ := sum["fleet_count"].(int)
	if total != 147.0 {
		t.Fatalf("mix.total: want 147.0 (0+100+15+32), got %v", total)
	}
	if n != 4 {
		t.Fatalf("mix.n: want 4, got %d", n)
	}
	if fleetN != 2 {
		t.Fatalf("mix.fleet_count: want 2, got %d", fleetN)
	}

	// World-side ground truth: every X matches one of the four
	// expected values (plain 0, plain 100, host 15, host 32).
	expected := map[float64]bool{0: false, 100: false, 15: false, 32: false}
	ents := w.Execute(runtime.NewQuery().Has("host.Position"))
	if len(ents) != 4 {
		t.Fatalf("ground truth: want 4 entities with host.Position, got %d", len(ents))
	}
	for _, e := range ents {
		raw, ok := w.GetComponent(e, "host.Position")
		if !ok {
			t.Fatalf("entity %v missing host.Position", e.ID())
		}
		p := raw.(*ecsbind.UnitPosition)
		if _, ok := expected[p.X]; !ok {
			t.Fatalf("unexpected X=%v on %v", p.X, e.ID())
		}
		expected[p.X] = true
	}
	for x, seen := range expected {
		if !seen {
			t.Fatalf("missing X=%v in ground truth", x)
		}
	}

	// Disposing a fleet proxy via the proxy class (u.dispose()) must
	// remove it from the World, the fleet map, AND from any subsequent
	// World.query. The mix export already created 2 fleet proxies;
	// kill_one_and_count spawns and disposes 1 more, leaving 2 fleet
	// proxies + 2 plain = 4 entities. The kill_one_and_count and
	// world_count_only exports live in the same root module as the
	// earlier mix export — see the combined src above.
	res, err = rt.Call("kill_one_and_count")
	if err != nil {
		t.Fatalf("Call kill_one_and_count: %v", err)
	}
	postN, _ := res.Value.(int)
	if postN != 4 {
		t.Fatalf("post-kill World.query count: want 4 (2 fleet + 2 plain), got %d", postN)
	}

	// A subsequent count from a fresh query should still see 4 (no
	// further mutations).
	res, err = rt.Call("world_count_only")
	if err != nil {
		t.Fatalf("Call world_count_only: %v", err)
	}
	postN2, _ := res.Value.(int)
	if postN2 != 4 {
		t.Fatalf("post-kill World.query (fresh) count: want 4, got %d", postN2)
	}
}

// ----------------------------------------------------------------------------
// 4. Full-chain RMW with whenChanged observability: the script does a
//    batch read+mutate, then queries WhenChanged WITHOUT ticking. The
//    query must surface every entity that Apply auto-Marked.
// ----------------------------------------------------------------------------

// TestE2E_WhenChangedSurfacesBatchMutation pins the "WhenChanged
// observes the auto-Mark from PatchEntity" contract through a real
// script runtime. The script:
//  1. View all entities with Health.
//  2. Mutate each Health in place.
//  3. Apply the patched fields.
//  4. Do NOT tick — the auto-Mark from PatchEntity must leave the
//     entities in the whenChanged set for this tick.
//  5. Query WhenChanged(Health) — must return every entity.
//
// This is the contract the feasibility analysis hinges on: a single
// batch read+write round trip must be observable to the next tick's
// query without a follow-up Mark().
func TestE2E_WhenChangedSurfacesBatchMutation(t *testing.T) {
	rt, err := script.NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	w := runtime.NewWorld()
	bind := ecsbind.New(w).
		RegisterComponent("Health", e2eHealthDesc()).
		RegisterComponent("Position", e2ePosDesc())
	if err := bind.Bind(rt); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	// Seed 7 entities. After seed we clear changes so the new tick
	// has a clean baseline.
	seeded := seedWorldE2E(t, w, 7)
	if len(seeded) != 7 {
		t.Fatalf("seed: want 7 ids, got %d", len(seeded))
	}

	const src = `import World from "ecs"

export fun mutate_and_observe(): map<string, any> {
    var out: map<string, any> = World.view(["Health"], [])
    var ids: array<string> = (out["ids"] as array<string>)
    var data: map<string, any> = (out["data"] as map<string, any>)
    var arr: array<map<string, any>> = (data["Health"] as array<map<string, any>>)
    var i: int = 0
    while (i < len(ids)) {
        var h: map<string, any> = arr[i]
        h["Value"] = (h["Value"] as int) - 1
        i = i + 1
    }
    var applied: int = World.apply("Health", ids, arr)
    // DO NOT tick — we want the auto-Mark to remain in the changed
    // set for THIS tick.
    var changed: array<string> = World.query([], [], [], [], ["Health"], [])
    return {"n": len(ids), "applied": applied, "changed": len(changed)}
}`
	if err := rt.LoadSource("wc", src); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	res, err := rt.Call("mutate_and_observe")
	if err != nil {
		t.Fatalf("Call mutate_and_observe: %v", err)
	}
	sum, ok := res.Value.(map[string]any)
	if !ok {
		t.Fatalf("mutate_and_observe: want map[string]any, got %T", res.Value)
	}
	n, _ := sum["n"].(int)
	applied, _ := sum["applied"].(int)
	changed, _ := sum["changed"].(int)
	if n != 7 {
		t.Fatalf("want n=7, got %d", n)
	}
	if applied != 7 {
		t.Fatalf("want applied=7, got %d", applied)
	}
	if changed != 7 {
		t.Fatalf("auto-Mark via Apply: want changed=7 (every entity), got %d", changed)
	}

	// After the script runs (and the script does NOT tick), a Tick
	// must clear the bookkeeping. Re-running the same query from the
	// host side must return 0 ids.
	w.Tick()
	cs := w.Execute(runtime.NewQuery().WhenChanged("Health"))
	if len(cs) != 0 {
		t.Fatalf("post-tick WhenChanged: want 0, got %d", len(cs))
	}
}

// ----------------------------------------------------------------------------
// 5. Host-side stale-handle contract end-to-end: script-driven dispose
//    + Fleet.* follow-up call surfaces the ErrStaleEntity panic that
//    script/runtime.go:1315 propagates. Catches regressions where the
//    stale-handle convention slips through script binding.
// ----------------------------------------------------------------------------

// TestE2E_StaleHandlePanicEndToEnd is the integration-level safety net
// for the host-entity binding idioms: after a Fleet.destroy call, any
// Fleet.move / Fleet.hp on the same id must panic with "stale or
// unknown id". Per-instance proxies (Unit.dispose then u.move) follow
// the same contract. The World facade surfaces stale IDs as "not alive"
// (no panic on destroy of an already-disposed entity; panic on
// destroy of a never-existed entity).
func TestE2E_StaleHandlePanicEndToEnd(t *testing.T) {
	rt, err := script.NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	w := runtime.NewWorld()
	w.AddRegistry(ecsbind.HostRegistry())
	fleet := ecsbind.NewHostFleet(w)
	wb := ecsbind.New(w).RegisterComponent("host.Position", e2eHostPosDesc())
	if err := fleet.Bind(rt); err != nil {
		t.Fatalf("fleet.Bind: %v", err)
	}
	if err := wb.Bind(rt); err != nil {
		t.Fatalf("wb.Bind: %v", err)
	}

	const src = `import Fleet from "host"
import World from "ecs"

export fun fleet_stale_move(): void {
    var id: string = Fleet.spawn(0.0)
    Fleet.move(id, 1.0)
    Fleet.destroy(id)
    Fleet.move(id, 1.0)
}

export fun fleet_stale_hp(): int {
    var id: string = Fleet.spawn(0.0)
    Fleet.destroy(id)
    return Fleet.hp(id)
}

export fun proxy_stale_move(): void {
    var u: Unit = Fleet.spawn_proxy(0.0)
    u.dispose()
    u.move(1.0)
}

export fun proxy_stale_hp(): int {
    var u: Unit = Fleet.spawn_proxy(0.0)
    u.dispose()
    return u.hp()
}

export fun world_stale_destroy(): void {
    var id: string = World.spawn()
    World.destroy(id)
    World.destroy(id)
}`
	if err := rt.LoadSource("stale", src); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	cases := []struct {
		name       string
		fn         string
		wantSubstr string
	}{
		{"fleet_stale_move", "fleet_stale_move", "stale"},
		{"fleet_stale_hp", "fleet_stale_hp", "stale"},
		{"proxy_stale_move", "proxy_stale_move", "stale"},
		{"proxy_stale_hp", "proxy_stale_hp", "stale"},
		{"world_stale_destroy", "world_stale_destroy", "not alive"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatalf("%s: expected panic, got nil", tc.name)
				}
				msg := fmt.Sprint(r)
				if !strings.Contains(msg, tc.wantSubstr) {
					t.Fatalf("%s: panic want substring %q, got %q", tc.name, tc.wantSubstr, msg)
				}
			}()
			if _, err := rt.Call(tc.fn); err != nil {
				t.Fatalf("Call %s: %v", tc.name, err)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// 6. E2E lifecycle through change-tracking: a full tick cycle where the
//    script observes added/changed/removed via Changes(id) and Tick.
// ----------------------------------------------------------------------------

// TestE2E_ChangesRoundTrip walks the per-entity changes observation:
//   - install Health (added)
//   - Set Health via the facade (changed)
//   - Remove Health (removed)
//   - Tick (clears the bookkeeping)
//
// All four phases run from script. The script returns a map of
// expected bucket sizes after each phase so the host can assert without
// reading script-internal state.
func TestE2E_ChangesRoundTrip(t *testing.T) {
	rt, err := script.NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	w := runtime.NewWorld()
	bind := ecsbind.New(w).RegisterComponent("Health", e2eHealthDesc())
	if err := bind.Bind(rt); err != nil {
		t.Fatalf("Bind: %v", err)
	}

	// Seed one entity with Health so the script can mutate it.
	e := w.Create()
	if err := w.SetComponent(e, "Health", &e2eHealth{Value: 1}); err != nil {
		t.Fatalf("install Health: %v", err)
	}
	w.ClearChanges(e)
	id := e.ID().String()

	src := fmt.Sprintf(`import World from "ecs"

export fun phase1_changed(): int {
    var n: int = World.set(%q, "Health", {"Value": 5})
    var ch: map<string, any> = World.changes(%q)
    var changed_arr: array<any> = (ch["changed"] as array<any>)
    return len(changed_arr)
}

export fun phase2_tick_then_changes(): map<string, any> {
    World.tick()
    var ch: map<string, any> = World.changes(%q)
    var added: int = len((ch["added"] as array<any>))
    var changed: int = len((ch["changed"] as array<any>))
    var removed: int = len((ch["removed"] as array<any>))
    return {"added": added, "changed": changed, "removed": removed}
}

export fun phase3_remove(): int {
    World.remove(%q, "Health")
    var ch: map<string, any> = World.changes(%q)
    var removed_arr: array<any> = (ch["removed"] as array<any>)
    return len(removed_arr)
}`, id, id, id, id, id)
	if err := rt.LoadSource("lifecycle", src); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	// Phase 1: Set marks Health as changed.
	res, err := rt.Call("phase1_changed")
	if err != nil {
		t.Fatalf("Call phase1_changed: %v", err)
	}
	phase1Changed, ok := res.Value.(int)
	if !ok {
		t.Fatalf("phase1_changed: want int, got %T", res.Value)
	}
	if phase1Changed != 1 {
		t.Fatalf("phase1_changed: want changed=1, got %d", phase1Changed)
	}

	// Phase 2: Tick clears everything.
	res, err = rt.Call("phase2_tick_then_changes")
	if err != nil {
		t.Fatalf("Call phase2_tick_then_changes: %v", err)
	}
	m, ok := res.Value.(map[string]any)
	if !ok {
		t.Fatalf("phase2: want map, got %T", res.Value)
	}
	for _, k := range []string{"added", "changed", "removed"} {
		v, _ := m[k].(int)
		if v != 0 {
			t.Fatalf("post-tick %s: want 0, got %d", k, v)
		}
	}

	// Phase 3: Remove marks Health as removed.
	res, err = rt.Call("phase3_remove")
	if err != nil {
		t.Fatalf("Call phase3_remove: %v", err)
	}
	phase3Removed, _ := res.Value.(int)
	if phase3Removed != 1 {
		t.Fatalf("phase3_remove: want removed=1, got %d", phase3Removed)
	}
}
