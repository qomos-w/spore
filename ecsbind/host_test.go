// host_test.go — contract tests for the host entity binding idioms in
// ecsbind/host.go. Drives both the collection facade (HostFleet) and the
// per-instance proxy (UnitHost) through script.Runtime and asserts:
//
//   1. script-driven Fleet.move is observable via WorldBinding (interop
//      with the generic World facade; same storage, same Tick, same query);
//   2. script-driven Fleet.spawn_proxy returns a live Unit proxy whose
//      methods mutate the same component storage;
//   3. disposing a host makes subsequent fleet- AND proxy-side method
//      calls report the "stale or unknown id" error as a structured
//      runtime error (script/runtime.go:1315 converts a host method
//      error into the stable native_call_failed diagnostic);
//   4. host entities and plain ECS entities coexist in the same World
//      query (the "same table / same Tick / same query" contract).
//
// The test file is intentionally self-contained: nothing is shared with
// ecsbind_test.go so the two files evolve independently and the file
// scope remains narrow.
package ecsbind_test

import (
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

// newHostWorld builds a World with the host registry pre-loaded so the
// aggregate auto-collection path (runtime/ref.go:autoComponentFields)
// recognises UnitPosition and UnitHealth. It returns the World together
// with a fresh *HostFleet and *WorldBinding the test can drive.
func newHostWorld(t *testing.T) (*runtime.World, *ecsbind.HostFleet, *ecsbind.WorldBinding) {
	t.Helper()
	w := runtime.NewWorld()
	w.AddRegistry(ecsbind.HostRegistry())
	wb := ecsbind.New(w)
	fleet := ecsbind.NewHostFleet(w)
	return w, fleet, wb
}

func newRuntime(t *testing.T) *script.Runtime {
	t.Helper()
	rt, err := script.NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	return rt
}

// bindFleetAndWorld wires HostFleet.Bind (registers Fleet + Unit
// interfaces) and WorldBinding.Bind (registers the World facade) into
// the same Runtime. Both must complete BEFORE LoadSource.
func bindFleetAndWorld(t *testing.T, rt *script.Runtime, fleet *ecsbind.HostFleet, wb *ecsbind.WorldBinding) {
	t.Helper()
	if err := fleet.Bind(rt); err != nil {
		t.Fatalf("HostFleet.Bind: %v", err)
	}
	if err := wb.Bind(rt); err != nil {
		t.Fatalf("WorldBinding.Bind: %v", err)
	}
}

// hostPosDesc is a minimal schema.ObjectDesc so the WorldBinding.Set path
// can drive plain ECS entities with the same "host.Position" name as
// the host registry. The descriptor fields must match the auto-generated
// UnitPosition layout so World.get reads the same storage slot.
func hostPosDesc() schema.ObjectDesc {
	return schema.ObjectDesc{
		Kind: schema.TypeKindStruct,
		Name: "Position",
		Fields: []schema.FieldDesc{
			{Name: "X", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "double"}},
			{Name: "Y", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "double"}},
		},
	}
}

// lookupEntity parses the canonical id hex string and returns the
// corresponding runtime.Entity for use with World.Get / World.Execute.
// Identity round-tripping uses runtime.MakeEntity (runtime/entity.go:45).
func lookupEntity(t *testing.T, w *runtime.World, hex string) runtime.Entity {
	t.Helper()
	cid, err := identity.ParseCanonicalID(hex)
	if err != nil {
		t.Fatalf("ParseCanonicalID(%q): %v", hex, err)
	}
	return runtime.MakeEntity(cid, w)
}

// ----------------------------------------------------------------------------
// 1. Pure Go surface: spawn / move / hp / dispose stale-handle contract
// ----------------------------------------------------------------------------

// TestHostGoSurface pins the Go-side contract. Spawn registers a host
// under the World, Move marks Position (so OnChanged observes it), Hp
// reads the health value, Dispose flips IsAlive so subsequent operations
// return ErrStaleEntity.
func TestHostGoSurface(t *testing.T) {
	w, fleet, _ := newHostWorld(t)

	id, err := fleet.Spawn(1.0)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if id == "" {
		t.Fatal("Spawn: empty id")
	}
	if got, err := fleet.Hp(id); err != nil || got != 100 {
		t.Fatalf("Hp after spawn: got (%d, %v), want (100, nil)", got, err)
	}
	if err := fleet.Move(id, 0.5); err != nil {
		t.Fatalf("Move: %v", err)
	}

	// Confirm via the World: Position.X == 1.5 and OnChanged reports
	// exactly one entity.
	ent := lookupEntity(t, w, id)
	if got, ok := w.Get(ent, ecsbind.PositionC); !ok || got.X != 1.5 || got.Y != 0 {
		t.Fatalf("World.Get Position after Move: got (%+v, %v), want X=1.5 Y=0", got, ok)
	}
	if got := len(w.Execute(runtime.NewQuery().OnChanged(ecsbind.PositionC))); got != 1 {
		t.Fatalf("OnChanged(Position) after Mark: got %d entities, want 1", got)
	}

	// Dispose + stale-handle contract.
	if err := fleet.Dispose(id); err != nil {
		t.Fatalf("Dispose: %v", err)
	}
	if _, err := fleet.Hp(id); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("Hp after dispose: want stale error, got %v", err)
	}
	if err := fleet.Move(id, 1.0); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("Move after dispose: want stale error, got %v", err)
	}
}

// ----------------------------------------------------------------------------
// 2. Interop: script-driven Fleet.move is visible via WorldBinding
// ----------------------------------------------------------------------------

// TestScript_InteropFleetMoveSeenByWorldQuery binds both facades into a
// Runtime, drives Fleet.move from script, then asks the script to
// enumerate entities whose Position was marked this tick (via
// WhenChanged). The test confirms the mutation went through the same
// storage that WorldBinding observes.
func TestScript_InteropFleetMoveSeenByWorldQuery(t *testing.T) {
	rt := newRuntime(t)
	w, fleet, wb := newHostWorld(t)
	bindFleetAndWorld(t, rt, fleet, wb)

	const src = `import Fleet from "host"
import World from "ecs"

export fun spawn_and_move(x: double, dx: double): string {
    var id: string = Fleet.spawn(x)
    Fleet.move(id, dx)
    return id
}

export fun changed_count(): int {
    var n: array<string> = World.query([], [], [], [], ["host.Position"], [])
    return len(n)
}

export fun hp(id: string): int {
    return Fleet.hp(id)
}`
	if err := rt.LoadSource("demo", src); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	res, err := rt.Call("spawn_and_move", 2.0, 3.5)
	if err != nil {
		t.Fatalf("Call spawn_and_move: %v", err)
	}
	id, ok := res.Value.(string)
	if !ok || id == "" {
		t.Fatalf("spawn_and_move return: want non-empty string, got (%T, %v)", res.Value, res.Value)
	}

	// After Tick, World.query with WhenChanged=["host.Position"] must
	// surface the just-moved entity.
	res, err = rt.Call("changed_count")
	if err != nil {
		t.Fatalf("Call changed_count: %v", err)
	}
	count, ok := res.Value.(int)
	if !ok {
		t.Fatalf("changed_count return: want int, got %T (%v)", res.Value, res.Value)
	}
	if count != 1 {
		t.Fatalf("changed_count after spawn_and_move: want 1, got %d", count)
	}

	// End the tick from the Go side so subsequent reads see a clean
	// change set.
	w.Tick()

	// Cross-check via Fleet.hp (round-trips through the same host struct).
	res, err = rt.Call("hp", id)
	if err != nil {
		t.Fatalf("Call hp: %v", err)
	}
	hp, ok := res.Value.(int)
	if !ok || hp != 100 {
		t.Fatalf("hp after move: want 100, got (%T, %v)", res.Value, res.Value)
	}
}

// ----------------------------------------------------------------------------
// 3. Per-instance proxy factory: SpawnProxy returns a live Unit
// ----------------------------------------------------------------------------

// TestScript_FactoryReturnsLiveProxy drives Fleet.spawn_proxy from a
// script and then drives Unit.move on the returned proxy. The mutation
// must land in the same World storage.
func TestScript_FactoryReturnsLiveProxy(t *testing.T) {
	rt := newRuntime(t)
	w, fleet, wb := newHostWorld(t)
	bindFleetAndWorld(t, rt, fleet, wb)

	const src = `import Fleet from "host"
import Unit from "host"

export fun make_and_move(x: double, dx: double): int {
    var u: Unit = Fleet.spawn_proxy(x)
    u.move(dx)
    u.move(dx)
    return u.hp()
}

export fun make_and_hp(x: double): int {
    var u: Unit = Fleet.spawn_proxy(x)
    return u.hp()
}`
	if err := rt.LoadSource("demo", src); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	// make_and_move: spawn at 1.0, move by 0.5 twice → final X = 2.0,
	// hp still 100.
	res, err := rt.Call("make_and_move", 1.0, 0.5)
	if err != nil {
		t.Fatalf("Call make_and_move: %v", err)
	}
	if got, ok := res.Value.(int); !ok || got != 100 {
		t.Fatalf("make_and_move hp: want 100, got (%T, %v)", res.Value, res.Value)
	}

	// Confirm via the World: exactly one host with Position.X == 2.0.
	ents := w.Execute(runtime.NewQuery().Has(ecsbind.PositionC.Name()))
	if len(ents) == 0 {
		t.Fatal("no Position entities after make_and_move")
	}
	found := 0
	for _, e := range ents {
		got, ok := w.Get(e, ecsbind.PositionC)
		if !ok {
			continue
		}
		if got.X == 2.0 {
			found++
		}
	}
	if found != 1 {
		t.Fatalf("Position.X==2.0 count: got %d, want 1 (total ents=%d)", found, len(ents))
	}

	// make_and_hp: a fresh proxy's hp is 100 by default.
	res, err = rt.Call("make_and_hp", 7.5)
	if err != nil {
		t.Fatalf("Call make_and_hp: %v", err)
	}
	if got, ok := res.Value.(int); !ok || got != 100 {
		t.Fatalf("make_and_hp: want 100, got (%T, %v)", res.Value, res.Value)
	}
}

// ----------------------------------------------------------------------------
// 4. Stale handle error convention: dispatch surfaces ErrStaleEntity as a
//    structured runtime error
// ----------------------------------------------------------------------------

// TestScript_StaleHandlesSurfaceStructuredError confirms that calling Fleet.move
// / Fleet.hp on a disposed id, AND calling u.move after u.dispose on a
// per-instance proxy, reports the "stale or unknown id" error as a structured
// runtime error. Host-interface method errors are Track 2 (recoverable)
// conditions: the script runtime used to re-panic them into the host process
// (tests then had to recover), and since the #29 error-model convergence they
// arrive as Result.Error with the stable native_call_failed code while Call
// itself never panics.
func TestScript_StaleHandlesSurfaceStructuredError(t *testing.T) {
	rt := newRuntime(t)
	_, fleet, wb := newHostWorld(t)
	bindFleetAndWorld(t, rt, fleet, wb)

	const src = `import Fleet from "host"
import Unit from "host"

export fun fleet_move_after_destroy(x: double, dx: double): void {
    var id: string = Fleet.spawn(x)
    Fleet.move(id, dx)
    Fleet.destroy(id)
    Fleet.move(id, dx)
}

export fun fleet_hp_after_destroy(x: double): int {
    var id: string = Fleet.spawn(x)
    Fleet.destroy(id)
    return Fleet.hp(id)
}

export fun proxy_move_after_dispose(x: double, dx: double): void {
    var u: Unit = Fleet.spawn_proxy(x)
    u.dispose()
    u.move(dx)
}

export fun proxy_hp_after_dispose(x: double): int {
    var u: Unit = Fleet.spawn_proxy(x)
    u.dispose()
    return u.hp()
}`
	if err := rt.LoadSource("demo", src); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	cases := []struct {
		name string
		args []any
	}{
		{"fleet_move_after_destroy", []any{0.0, 1.0}},
		{"fleet_hp_after_destroy", []any{0.0}},
		{"proxy_move_after_dispose", []any{0.0, 1.0}},
		{"proxy_hp_after_dispose", []any{0.0}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			res, err := rt.Call(tc.name, tc.args...)
			if err != nil {
				t.Fatalf("Call %s: %v", tc.name, err)
			}
			if res.Error == nil {
				t.Fatalf("%s: expected a structured host-interface error, got value %#v", tc.name, res.Value)
			}
			if got := res.Error.Diagnostic.Code; got != "native_call_failed" {
				t.Fatalf("%s: diagnostic code = %q, want native_call_failed", tc.name, got)
			}
			if msg := res.Error.Diagnostic.Message; !strings.Contains(msg, "stale") {
				t.Fatalf("%s: message: want 'stale', got %q", tc.name, msg)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// 5. Coexistence contract: host entities + plain ECS entities in one query
// ----------------------------------------------------------------------------

// TestScript_HostAndPureECSEntitiesCoexistInQuery creates one host unit
// (via Fleet.spawn) and one plain ECS entity (via World.spawn + a
// SetComponent pre-install, then World.set with a Position descriptor
// registered against the same World). Both must appear in a
// Has("host.Position") query, and the host's mutation must be visible to
// the World facade.
func TestScript_HostAndPureECSEntitiesCoexistInQuery(t *testing.T) {
	rt := newRuntime(t)
	w, fleet, wb := newHostWorld(t)
	// Register the same descriptor name as the host registry so plain
	// ECS writes land in the same storage slot.
	wb.RegisterComponent("host.Position", hostPosDesc())
	bindFleetAndWorld(t, rt, fleet, wb)

	// Pre-install the Position component on a plain ECS entity from the
	// Go side. World.set goes through PatchEntity which requires the
	// component to exist; SetComponent is the canonical way to add a
	// component to a fresh entity.
	plainEntity := w.Create()
	if err := w.SetComponent(plainEntity, "host.Position", &ecsbind.UnitPosition{X: 0, Y: 0}); err != nil {
		t.Fatalf("install Position on plain entity: %v", err)
	}
	plainID := plainEntity.ID().String()

	const src = `import Fleet from "host"
import World from "ecs"

export fun make_two(plain_id: string): string {
    var host_id: string = Fleet.spawn(0.0)
    World.set(plain_id, "host.Position", {"X": 10.0, "Y": 0.0})
    Fleet.move(host_id, 1.0)
    World.tick()
    return host_id
}

export fun total(): int {
    var n: array<string> = World.query(["host.Position"], [], [], [], [], [])
    return len(n)
}`
	if err := rt.LoadSource("demo", src); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	res, err := rt.Call("make_two", plainID)
	if err != nil {
		t.Fatalf("Call make_two: %v", err)
	}
	hostID, ok := res.Value.(string)
	if !ok || hostID == "" {
		t.Fatalf("make_two: want non-empty host id, got (%T, %v)", res.Value, res.Value)
	}

	res, err = rt.Call("total")
	if err != nil {
		t.Fatalf("Call total: %v", err)
	}
	total, ok := res.Value.(int)
	if !ok || total != 2 {
		t.Fatalf("total: want 2 entities with Position, got (%T, %v)", res.Value, res.Value)
	}

	// Final cross-check: the World lists both entities, one with X=1.0
	// (host) and one with X=10.0 (plain ECS).
	ents := w.Execute(runtime.NewQuery().Has(ecsbind.PositionC.Name()))
	if len(ents) != 2 {
		t.Fatalf("coexistence: want 2 Position entities, got %d", len(ents))
	}
	var hostSeen, plainSeen bool
	for _, e := range ents {
		got, ok := w.Get(e, ecsbind.PositionC)
		if !ok {
			t.Fatalf("Position missing on %v", e.ID())
		}
		switch got.X {
		case 1.0:
			hostSeen = true
		case 10.0:
			plainSeen = true
		default:
			t.Fatalf("Position.X on %v: got %v, want 1.0 or 10.0", e.ID(), got.X)
		}
	}
	if !hostSeen {
		t.Fatal("host entity (X=1.0) missing from World")
	}
	if !plainSeen {
		t.Fatal("plain ECS entity (X=10.0) missing from World")
	}
}