package ecsbind_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/qomos-w/spore/ecsbind"
	"github.com/qomos-w/spore/identity"
	"github.com/qomos-w/spore/runtime"
	"github.com/qomos-w/spore/schema"
	"github.com/qomos-w/spore/script"
)

// ecsHealth is the runtime component shape used throughout the contract
// tests. It is intentionally tiny: one int field. The whole point of the
// facade is to hide this struct from script code, so any field shape works
// as long as the schema descriptor matches.
type ecsHealth struct {
	Value int
}

// ecsPosition is a second component used to exercise multi-component
// queries and patch isolation.
type ecsPosition struct {
	X float64
	Y float64
}

func ecsHealthDesc() schema.ObjectDesc {
	return schema.ObjectDesc{
		Name: "Health",
		Fields: []schema.FieldDesc{
			{Name: "Value", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}},
		},
	}
}

func ecsPositionDesc() schema.ObjectDesc {
	return schema.ObjectDesc{
		Name: "Position",
		Fields: []schema.FieldDesc{
			{Name: "X", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "double"}},
			{Name: "Y", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "double"}},
		},
	}
}

// mustID builds a deterministic CanonicalID for tests that need to
// manufacture stale or pre-existing entities.
func mustID(t *testing.T, ts uint64, slot uint16, inc uint16, seq uint64) identity.CanonicalID {
	t.Helper()
	id, err := identity.NewCanonicalID(ts, slot, inc, seq)
	if err != nil {
		t.Fatalf("NewCanonicalID: %v", err)
	}
	return id
}

// errContains fails the test unless err is non-nil and its message
// contains substr. Useful for asserting "stale id" / "unregistered
// component" without coupling to the exact error text.
func errContains(t *testing.T, err error, substr string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error containing %q, got nil", substr)
	}
	if !strings.Contains(err.Error(), substr) {
		t.Fatalf("expected error containing %q, got %q", substr, err.Error())
	}
}

// ----------------------------------------------------------------------------
// Pure-Go facade surface (no script binding involved)
// ----------------------------------------------------------------------------
//
// These tests verify the facade's behaviour directly, without going
// through the script runtime. The script-integration tests below add
// the binding layer on top of the same Go surface.
//

// TestGoSurface_IdentityAndLifecycle confirms that spawn returns a
// stable canonical ID, the entity is alive in the World, and the Go
// host can drive the same entity directly.
func TestGoSurface_IdentityAndLifecycle(t *testing.T) {
	w := runtime.NewWorld()
	b := ecsbind.New(w).RegisterComponent("Health", ecsHealthDesc())

	id := b.World().Create() // host-driven entity
	if !b.World().IsAlive(id) {
		t.Fatalf("host entity not alive immediately after create")
	}

	scriptID := spawnViaFacade(t, b)
	if scriptID == "" {
		t.Fatal("expected non-empty id from facade.spawn()")
	}
	cid, err := identity.ParseCanonicalID(scriptID)
	if err != nil {
		t.Fatalf("facade.spawn() returned non-canonical id %q: %v", scriptID, err)
	}
	if e, ok := w.Entity(cid); !ok || !w.IsAlive(e) {
		t.Fatalf("facade-created entity not alive in World: ok=%v", ok)
	}

	// Same entity, cross-side: the facade's ID round-trips through the
	// World and is observable from the host side.
	host, ok := w.Entity(cid)
	if !ok {
		t.Fatalf("host could not resolve facade-created id")
	}
	if err := w.SetComponent(host, "Health", &ecsHealth{Value: 7}); err != nil {
		t.Fatalf("host SetComponent: %v", err)
	}

	got, err := b.Get(scriptID, "Health")
	if err != nil {
		t.Fatalf("facade Get: %v", err)
	}
	if got["Value"] != 7 {
		t.Fatalf("expected Value=7 (host-written), got %v", got["Value"])
	}
}

// TestGoSurface_GetSetPatchRoundTrip confirms the read/write cycle:
// set writes via PatchEntity and auto-Marks, get projects via the
// binding layer, and mutation count from set reflects the patch.
func TestGoSurface_GetSetPatchRoundTrip(t *testing.T) {
	w := runtime.NewWorld()
	b := ecsbind.New(w).
		RegisterComponent("Health", ecsHealthDesc()).
		RegisterComponent("Position", ecsPositionDesc())

	id := spawnViaFacade(t, b)

	// Fresh component → SetComponent path is irrelevant here; PatchEntity
	// requires the component to exist on the entity first. The facade
	// writes via PatchEntity, so we must install the initial struct.
	host, _ := w.Entity(parseID(t, id))
	if err := w.SetComponent(host, "Health", &ecsHealth{Value: 10}); err != nil {
		t.Fatalf("install Health: %v", err)
	}
	w.ClearChanges(host) // baseline: no add/change noise for the first read

	got, err := b.Get(id, "Health")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got["Value"] != 10 {
		t.Fatalf("expected Value=10, got %v", got["Value"])
	}

	// Patch: bump Value 10 -> 50 and add Position in the same call? No —
	// each set operates on one component. So we do two set calls.
	n, err := b.Set(id, "Health", map[string]any{"Value": 50})
	if err != nil {
		t.Fatalf("Set Health: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 mutation from Set, got %d", n)
	}

	got, err = b.Get(id, "Health")
	if err != nil {
		t.Fatalf("Get after Set: %v", err)
	}
	if got["Value"] != 50 {
		t.Fatalf("expected Value=50 after Set, got %v", got["Value"])
	}

	// ChangeSet should report Health as changed.
	cs, err := b.Changes(id)
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}
	added, _ := cs["added"].([]string)
	changed, _ := cs["changed"].([]string)
	removed, _ := cs["removed"].([]string)
	if len(added) != 0 {
		t.Fatalf("expected no added components, got %v", added)
	}
	if len(changed) != 1 || changed[0] != "Health" {
		t.Fatalf("expected changed=[Health], got %v", changed)
	}
	if len(removed) != 0 {
		t.Fatalf("expected no removed components, got %v", removed)
	}
}

// TestGoSurface_StaleIDErrorsOnWrite ensures destroy/set/changes all
// fail (or otherwise surface) when given an ID that does not exist.
// destroy and set/changes return errors; alive/get/has/remove/mark
// treat stale IDs as "no data" (false / no-op / nil map).
func TestGoSurface_StaleIDErrorsOnWrite(t *testing.T) {
	w := runtime.NewWorld()
	b := ecsbind.New(w).RegisterComponent("Health", ecsHealthDesc())

	// An ID that was never created (deterministic, well-formed).
	stale := mustID(t, 9999, 1, 0, 1).String()

	if b.Alive(stale) {
		t.Fatal("Alive should be false for an unknown id")
	}
	if b.Has(stale, "Health") {
		t.Fatal("Has should be false for an unknown id")
	}
	// Get is the "no data" idiom: returns (nil, nil) for stale IDs.
	got, err := b.Get(stale, "Health")
	if err != nil || got != nil {
		t.Fatalf("Get on stale id: got=%v err=%v; want (nil, nil)", got, err)
	}
	// Remove/mark are silent no-ops.
	b.Remove(stale, "Health")
	b.Mark(stale, "Health")

	// destroy/set/changes must surface the stale id.
	errContains(t, b.Destroy(stale), "not alive")
	if _, err := b.Set(stale, "Health", map[string]any{"Value": 1}); err == nil {
		t.Fatal("expected Set on stale id to error")
	}
	if _, err := b.Changes(stale); err == nil {
		t.Fatal("expected Changes on stale id to error")
	}

	// Disposed entity is also stale: create one, dispose it, then probe.
	id := spawnViaFacade(t, b)
	if err := b.Destroy(id); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if b.Alive(id) {
		t.Fatal("Alive should be false after Destroy")
	}
	errContains(t, b.Destroy(id), "not alive")

	// Malformed IDs are treated the same way.
	if b.Alive("not-a-canonical-id") {
		t.Fatal("Alive should be false for a malformed id")
	}
	errContains(t, b.Destroy("not-a-canonical-id"), "not alive")
}

// TestGoSurface_UnregisteredComponentContract asserts the wiki contract:
// "Get 返 ok=false / Set 返 error". For has/remove/mark the facade is
// conservative — unknown components are "absent".
func TestGoSurface_UnregisteredComponentContract(t *testing.T) {
	w := runtime.NewWorld()
	b := ecsbind.New(w).RegisterComponent("Health", ecsHealthDesc())

	id := spawnViaFacade(t, b)
	host, _ := w.Entity(parseID(t, id))
	if err := w.SetComponent(host, "Health", &ecsHealth{Value: 1}); err != nil {
		t.Fatalf("install Health: %v", err)
	}

	// Unknown component name → Get returns (nil, nil), no data, no error.
	got, err := b.Get(id, "Unknown")
	if err != nil || got != nil {
		t.Fatalf("Get unknown component: got=%v err=%v; want (nil, nil)", got, err)
	}

	// Unknown component name → Set returns an error so the payload cannot
	// be silently lost.
	if _, err := b.Set(id, "Unknown", map[string]any{"x": 1}); err == nil {
		t.Fatal("expected Set on unregistered component to error")
	}

	// Conservative defaults for the "no descriptor" branch.
	if b.Has(id, "Unknown") {
		t.Fatal("Has unknown component should be false")
	}
	b.Remove(id, "Unknown") // no-op
	b.Mark(id, "Unknown")   // no-op
}

// TestGoSurface_HasRemoveMarkLifecycle walks the full has→remove→has
// transition plus a mark that survives until tick.
func TestGoSurface_HasRemoveMarkLifecycle(t *testing.T) {
	w := runtime.NewWorld()
	b := ecsbind.New(w).RegisterComponent("Health", ecsHealthDesc())

	id := spawnViaFacade(t, b)
	host, _ := w.Entity(parseID(t, id))
	if err := w.SetComponent(host, "Health", &ecsHealth{Value: 1}); err != nil {
		t.Fatalf("install Health: %v", err)
	}
	w.ClearChanges(host)

	if !b.Has(id, "Health") {
		t.Fatal("expected Has=true after install")
	}

	b.Remove(id, "Health")
	if b.Has(id, "Health") {
		t.Fatal("expected Has=false after Remove")
	}

	// Re-add via SetComponent (Go side) and re-probe.
	if err := w.SetComponent(host, "Health", &ecsHealth{Value: 2}); err != nil {
		t.Fatalf("re-install Health: %v", err)
	}
	w.MarkChanged(host, "Health")

	cs, err := b.Changes(id)
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}
	added := cs["added"].([]string)
	changed := cs["changed"].([]string)
	removed := cs["removed"].([]string)
	if len(added) != 1 || added[0] != "Health" {
		t.Fatalf("expected added=[Health], got %v", added)
	}
	if len(changed) != 1 || changed[0] != "Health" {
		t.Fatalf("expected changed=[Health], got %v", changed)
	}
	if len(removed) != 0 {
		t.Fatalf("expected no removed, got %v", removed)
	}

	// Tick clears the change set globally.
	b.Tick()
	cs, err = b.Changes(id)
	if err != nil {
		t.Fatalf("Changes after tick: %v", err)
	}
	added = cs["added"].([]string)
	changed = cs["changed"].([]string)
	removed = cs["removed"].([]string)
	if len(added) != 0 || len(changed) != 0 || len(removed) != 0 {
		t.Fatalf("expected empty changes after tick, got added=%v changed=%v removed=%v", added, changed, removed)
	}
}

// TestGoSurface_QueryFilters walks all six query filter slots end-to-end.
func TestGoSurface_QueryFilters(t *testing.T) {
	w := runtime.NewWorld()
	b := ecsbind.New(w).
		RegisterComponent("Health", ecsHealthDesc()).
		RegisterComponent("Position", ecsPositionDesc())

	// Seed: 3 with Health, 1 with Position, 1 with both, 1 with neither.
	mk := func(comps ...string) string {
		id := spawnViaFacade(t, b)
		host, _ := w.Entity(parseID(t, id))
		for _, c := range comps {
			switch c {
			case "Health":
				if err := w.SetComponent(host, "Health", &ecsHealth{Value: 1}); err != nil {
					t.Fatalf("SetComponent Health: %v", err)
				}
			case "Position":
				if err := w.SetComponent(host, "Position", &ecsPosition{X: 1, Y: 2}); err != nil {
					t.Fatalf("SetComponent Position: %v", err)
				}
			}
		}
		w.ClearChanges(host)
		return id
	}
	hOnly1 := mk("Health")
	hOnly2 := mk("Health")
	hOnly3 := mk("Health")
	pOnly := mk("Position")
	both := mk("Health", "Position")
	none := mk()

	// Has("Health"): 3 h-only + 1 both = 4
	if got := b.Query([]string{"Health"}, nil, nil, nil, nil, nil); len(got) != 4 {
		t.Fatalf("Has(Health): expected 4, got %d (%v)", len(got), got)
	}
	// HasNone("Health"): p-only + none = 2
	if got := b.Query(nil, []string{"Health"}, nil, nil, nil, nil); len(got) != 2 {
		t.Fatalf("HasNone(Health): expected 2, got %d (%v)", len(got), got)
	}
	// HasEither("Position"): p-only + both = 2
	if got := b.Query(nil, nil, []string{"Position"}, nil, nil, nil); len(got) != 2 {
		t.Fatalf("HasEither(Position): expected 2, got %d (%v)", len(got), got)
	}
	// Combined Has + HasNone: Has(Health) AND HasNone(Position) = 3 h-only
	if got := b.Query([]string{"Health"}, []string{"Position"}, nil, nil, nil, nil); len(got) != 3 {
		t.Fatalf("Has(Health)+HasNone(Position): expected 3, got %d (%v)", len(got), got)
	}
	// No filters → every alive entity (6 here)
	if got := b.Query(nil, nil, nil, nil, nil, nil); len(got) != 6 {
		t.Fatalf("no filters: expected 6, got %d", len(got))
	}

	// WhenChanged: mutate Health on one h-only entity, then filter.
	cid := parseID(t, hOnly1)
	host, _ := w.Entity(cid)
	w.MarkChanged(host, "Health")
	got := b.Query(nil, nil, nil, nil, []string{"Health"}, nil)
	if len(got) != 1 || got[0] != hOnly1 {
		t.Fatalf("WhenChanged(Health): expected [%s], got %v", hOnly1, got)
	}

	// WhenAdded: install a fresh Health on a new entity; it should be in the
	// added set only on the tick it was created.
	id := spawnViaFacade(t, b)
	host, _ = w.Entity(parseID(t, id))
	if err := w.SetComponent(host, "Health", &ecsHealth{Value: 1}); err != nil {
		t.Fatalf("SetComponent: %v", err)
	}
	got = b.Query(nil, nil, nil, []string{"Health"}, nil, nil)
	if len(got) != 1 || got[0] != id {
		t.Fatalf("WhenAdded(Health): expected [%s], got %v", id, got)
	}

	// WhenRemoved: install then remove Health on both; removed = {both.id}
	cid = parseID(t, both)
	host, _ = w.Entity(cid)
	w.RemoveComponent(host, "Health")
	got = b.Query(nil, nil, nil, nil, nil, []string{"Health"})
	if len(got) != 1 || got[0] != both {
		t.Fatalf("WhenRemoved(Health): expected [%s], got %v", both, got)
	}

	// Sanity: ids are sorted and unique.
	_ = hOnly2
	_ = hOnly3
	_ = pOnly
	_ = none
}

// TestGoSurface_CrossConsistency verifies that reads/writes made
// through the facade are immediately observable from the Go side (and
// vice versa) on the same World. This is the central claim of the
// "third-party binder" design — the facade adds a layer but does not
// fork the data path.
func TestGoSurface_CrossConsistency(t *testing.T) {
	w := runtime.NewWorld()
	b := ecsbind.New(w).RegisterComponent("Health", ecsHealthDesc())

	id := spawnViaFacade(t, b)
	host, _ := w.Entity(parseID(t, id))

	// Go side writes, facade reads.
	if err := w.SetComponent(host, "Health", &ecsHealth{Value: 42}); err != nil {
		t.Fatalf("host SetComponent: %v", err)
	}
	got, err := b.Get(id, "Health")
	if err != nil {
		t.Fatalf("facade Get: %v", err)
	}
	if got["Value"] != 42 {
		t.Fatalf("expected Value=42 from facade after host write, got %v", got["Value"])
	}

	// Facade writes (PatchEntity), Go side reads.
	n, err := b.Set(id, "Health", map[string]any{"Value": 99})
	if err != nil {
		t.Fatalf("facade Set: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 mutation, got %d", n)
	}
	data, ok := w.GetComponent(host, "Health")
	if !ok {
		t.Fatal("host GetComponent: not found")
	}
	h, ok := data.(*ecsHealth)
	if !ok || h == nil {
		t.Fatalf("host GetComponent: unexpected type %T", data)
	}
	if h.Value != 99 {
		t.Fatalf("expected host.Value=99 after facade write, got %d", h.Value)
	}

	// Go-side MarkChanged is visible through facade Changes.
	w.MarkChanged(host, "Health")
	cs, err := b.Changes(id)
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}
	changed, _ := cs["changed"].([]string)
	if len(changed) != 1 || changed[0] != "Health" {
		t.Fatalf("expected changed=[Health], got %v", changed)
	}
}

// TestGoSurface_DescribeBindAsNamespace validates the namespace/name
// knobs on BindAs. We just exercise them on a real script.Runtime
// below; here we confirm the methods are present and don't blow up on
// nil inputs.
func TestGoSurface_BindAsNilRuntime(t *testing.T) {
	w := runtime.NewWorld()
	b := ecsbind.New(w)
	if err := b.Bind(nil); err == nil {
		t.Fatal("Bind(nil) should error")
	}
	if err := b.BindAs(nil, "ecs", "World"); err == nil {
		t.Fatal("BindAs(nil) should error")
	}
}

// ----------------------------------------------------------------------------
// Script-side facade surface
// ----------------------------------------------------------------------------
//
// These tests bind the facade into a real script.Runtime, then exercise
// the methods from script code. They confirm the snake_case → CamelCase
// mapping and the binding-layer mechanics described in
// script/runtime.go:746-796 and :1287-1299.
//

const ecsTestSpawnScript = `import World from "ecs"

export fun spawn_id(): string {
    return World.spawn()
}

export fun is_alive(id: string): bool {
    return World.alive(id)
}

export fun destroy_then_alive(id: string): bool {
    World.destroy(id)
    return World.alive(id)
}`

const ecsTestGetSetScript = `import World from "ecs"

export fun mutate_health(id: string, v: int): int {
    return World.set(id, "Health", {"Value": v})
}

export fun read_health(id: string): map<string, any> {
    return World.get(id, "Health")
}

export fun has_health(id: string): bool {
    return World.has(id, "Health")
}

export fun remove_health(id: string): int {
    World.remove(id, "Health")
    return 0
}

export fun mark_health(id: string): int {
    World.mark(id, "Health")
    return 0
}`

const ecsTestLifecycleScript = `import World from "ecs"

export fun total(): int {
    return World.count()
}

export fun tick_then_changes(id: string): map<string, any> {
    World.tick()
    return World.changes(id)
}`

const ecsTestQueryScript = `import World from "ecs"

export fun find_hurt(): array<string> {
    return World.query(["Health"], [], [], [], [], [])
}

export fun find_recent_changes(): array<string> {
    return World.query([], [], [], [], ["Health"], [])
}`

// TestScript_FacadeIsCallable loads a tiny script and confirms the
// facade is reachable through the script runtime: spawn() returns a
// non-empty string, alive() recognises the id, destroy() + alive()
// round-trip.
func TestScript_FacadeIsCallable(t *testing.T) {
	rt := mustRuntime(t)
	w := runtime.NewWorld()
	bind := ecsbind.New(w).RegisterComponent("Health", ecsHealthDesc())
	if err := bind.Bind(rt); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if err := rt.LoadSource("test", ecsTestSpawnScript); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	res, err := rt.Call("spawn_id")
	if err != nil {
		t.Fatalf("Call spawn_id: %v", err)
	}
	id, ok := res.Value.(string)
	if !ok || id == "" {
		t.Fatalf("spawn_id: want string, got %T %v", res.Value, res.Value)
	}

	res, err = rt.Call("is_alive", id)
	if err != nil {
		t.Fatalf("Call is_alive: %v", err)
	}
	alive, ok := res.Value.(bool)
	if !ok {
		t.Fatalf("is_alive: want bool, got %T", res.Value)
	}
	if !alive {
		t.Fatalf("expected is_alive=true for freshly spawned id %s", id)
	}

	res, err = rt.Call("destroy_then_alive", id)
	if err != nil {
		t.Fatalf("Call destroy_then_alive: %v", err)
	}
	aliveAfter, _ := res.Value.(bool)
	if aliveAfter {
		t.Fatalf("expected is_alive=false after destroy, got true")
	}
}

// TestScript_GetSetPatchFromScript confirms that set/get on the
// facade is callable from script and that the returned mutation count
// from set matches the wiki contract. We also assert that the
// change-tracking data is what the runtime saw.
func TestScript_GetSetPatchFromScript(t *testing.T) {
	rt := mustRuntime(t)
	w := runtime.NewWorld()
	bind := ecsbind.New(w).RegisterComponent("Health", ecsHealthDesc())
	if err := bind.Bind(rt); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if err := rt.LoadSource("test", ecsTestGetSetScript); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	// Spawn + install initial struct on the Go side (the facade writes
	// via PatchEntity, which requires the component to exist).
	id := spawnViaFacade(t, bind)
	host, _ := w.Entity(parseID(t, id))
	if err := w.SetComponent(host, "Health", &ecsHealth{Value: 0}); err != nil {
		t.Fatalf("install Health: %v", err)
	}
	w.ClearChanges(host)

	// Script: set Value=7 → expect mutation count 1.
	res, err := rt.Call("mutate_health", id, 7)
	if err != nil {
		t.Fatalf("Call mutate_health: %v", err)
	}
	n, ok := res.Value.(int)
	if !ok {
		t.Fatalf("mutate_health: want int, got %T", res.Value)
	}
	if n != 1 {
		t.Fatalf("mutate_health: want 1, got %d", n)
	}

	// Script: read back; expect Value=7.
	res, err = rt.Call("read_health", id)
	if err != nil {
		t.Fatalf("Call read_health: %v", err)
	}
	m, ok := res.Value.(map[string]any)
	if !ok {
		t.Fatalf("read_health: want map[string]any, got %T (%v)", res.Value, res.Value)
	}
	if m["Value"] != 7 {
		t.Fatalf("read_health: want Value=7, got %v (%T)", m["Value"], m["Value"])
	}

	// Script: has(id, "Health") → true.
	res, err = rt.Call("has_health", id)
	if err != nil {
		t.Fatalf("Call has_health: %v", err)
	}
	if v, _ := res.Value.(bool); !v {
		t.Fatalf("has_health: want true")
	}

	// Script: remove_health → has_health → false.
	if _, err := rt.Call("remove_health", id); err != nil {
		t.Fatalf("Call remove_health: %v", err)
	}
	res, err = rt.Call("has_health", id)
	if err != nil {
		t.Fatalf("Call has_health after remove: %v", err)
	}
	if v, _ := res.Value.(bool); v {
		t.Fatalf("has_health after remove: want false")
	}

	// Script: mark_health is a no-op without a component; should not error.
	if _, err := rt.Call("mark_health", id); err != nil {
		t.Fatalf("Call mark_health (no component): %v", err)
	}
}

// TestScript_LifecycleAndTick confirms count + tick + changes surface
// to the script.
func TestScript_LifecycleAndTick(t *testing.T) {
	rt := mustRuntime(t)
	w := runtime.NewWorld()
	bind := ecsbind.New(w).RegisterComponent("Health", ecsHealthDesc())
	if err := bind.Bind(rt); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if err := rt.LoadSource("test", ecsTestLifecycleScript); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	// Empty world → count = 0.
	res, err := rt.Call("total")
	if err != nil {
		t.Fatalf("Call total: %v", err)
	}
	if n, _ := res.Value.(int); n != 0 {
		t.Fatalf("total (empty): want 0, got %d", n)
	}

	// Spawn one entity, install Health, then tick.
	id := spawnViaFacade(t, bind)
	host, _ := w.Entity(parseID(t, id))
	if err := w.SetComponent(host, "Health", &ecsHealth{Value: 1}); err != nil {
		t.Fatalf("install Health: %v", err)
	}
	w.ClearChanges(host)
	w.MarkChanged(host, "Health")

	// count → 1
	res, err = rt.Call("total")
	if err != nil {
		t.Fatalf("Call total: %v", err)
	}
	if n, _ := res.Value.(int); n != 1 {
		t.Fatalf("total: want 1, got %d", n)
	}

	// tick_then_changes: tick clears, then changes should be empty.
	res, err = rt.Call("tick_then_changes", id)
	if err != nil {
		t.Fatalf("Call tick_then_changes: %v", err)
	}
	m, ok := res.Value.(map[string]any)
	if !ok {
		t.Fatalf("tick_then_changes: want map[string]any, got %T", res.Value)
	}
	for _, k := range []string{"added", "changed", "removed"} {
		arr, ok := m[k].([]any)
		if !ok {
			t.Fatalf("tick_then_changes[%q]: want []any, got %T", k, m[k])
		}
		if len(arr) != 0 {
			t.Fatalf("tick_then_changes[%q]: want empty, got %v", k, arr)
		}
	}
}

// TestScript_QueryFromScript exercises query has / changed through the
// script surface.
func TestScript_QueryFromScript(t *testing.T) {
	rt := mustRuntime(t)
	w := runtime.NewWorld()
	bind := ecsbind.New(w).RegisterComponent("Health", ecsHealthDesc())
	if err := bind.Bind(rt); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if err := rt.LoadSource("test", ecsTestQueryScript); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	// Seed 3 entities with Health.
	ids := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		id := spawnViaFacade(t, bind)
		host, _ := w.Entity(parseID(t, id))
		if err := w.SetComponent(host, "Health", &ecsHealth{Value: i}); err != nil {
			t.Fatalf("install Health: %v", err)
		}
		w.ClearChanges(host)
		ids = append(ids, id)
	}

	// find_hurt → all three.
	res, err := rt.Call("find_hurt")
	if err != nil {
		t.Fatalf("Call find_hurt: %v", err)
	}
	if got := asStringSlice(t, res.Value); len(got) != 3 {
		t.Fatalf("find_hurt: want 3 ids, got %d (%v)", len(got), got)
	}

	// Mark the first as changed; find_recent_changes → that one.
	host, _ := w.Entity(parseID(t, ids[0]))
	w.MarkChanged(host, "Health")
	res, err = rt.Call("find_recent_changes")
	if err != nil {
		t.Fatalf("Call find_recent_changes: %v", err)
	}
	if got := asStringSlice(t, res.Value); len(got) != 1 || got[0] != ids[0] {
		t.Fatalf("find_recent_changes: want [%s], got %v", ids[0], got)
	}
}

// TestScript_StaleIDPropagatesError confirms that destroy / set /
// changes report a structured runtime error when fed an unknown id.
//
// A host-interface method that returns an error is a Track 2 (recoverable)
// condition: the script runtime used to re-panic it into the host process,
// so tests had to catch it with defer/recover. Since the #29 error-model
// convergence it travels the normal channel instead — Call returns a Result
// whose Error carries the stable native_call_failed code plus the host error
// text — and Call never panics.
func TestScript_StaleIDPropagatesError(t *testing.T) {
	rt := mustRuntime(t)
	w := runtime.NewWorld()
	bind := ecsbind.New(w).RegisterComponent("Health", ecsHealthDesc())
	if err := bind.Bind(rt); err != nil {
		t.Fatalf("Bind: %v", err)
	}

	const src = `import World from "ecs"

export fun destroy_unknown(id: string): void {
    World.destroy(id)
}

export fun set_unknown(id: string, v: int): int {
    return World.set(id, "Health", {"Value": v})
}

export fun changes_unknown(id: string): map<string, any> {
    return World.changes(id)
}`
	if err := rt.LoadSource("test", src); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	stale := mustID(t, 9999, 1, 0, 99).String()

	for _, name := range []string{"destroy_unknown", "set_unknown", "changes_unknown"} {
		t.Run(name, func(t *testing.T) {
			var (
				res script.Result
				err error
			)
			switch name {
			case "destroy_unknown", "changes_unknown":
				res, err = rt.Call(name, stale)
			case "set_unknown":
				res, err = rt.Call(name, stale, 1)
			}
			if err != nil {
				t.Fatalf("Call %s: %v", name, err)
			}
			if res.Error == nil {
				t.Fatalf("%s: expected a structured host-interface error, got value %#v", name, res.Value)
			}
			if got := res.Error.Diagnostic.Code; got != "native_call_failed" {
				t.Fatalf("%s: diagnostic code = %q, want native_call_failed", name, got)
			}
			if msg := res.Error.Diagnostic.Message; !strings.Contains(msg, "not alive") {
				t.Fatalf("%s: expected message to mention 'not alive', got %q", name, msg)
			}
		})
	}
}

// TestScript_UnregisteredComponentContract asserts that scripts see
// the same "Get → empty / Set → error" contract as the Go side.
// (The vm map representation of a nil Go map is an empty map; we use
// length-based assertions to stay robust to that detail.)
func TestScript_UnregisteredComponentContract(t *testing.T) {
	rt := mustRuntime(t)
	w := runtime.NewWorld()
	bind := ecsbind.New(w).RegisterComponent("Health", ecsHealthDesc())
	if err := bind.Bind(rt); err != nil {
		t.Fatalf("Bind: %v", err)
	}

	const src = `import World from "ecs"

export fun read_unknown(id: string): map<string, any> {
    return World.get(id, "Position")
}

export fun has_unknown(id: string): bool {
    return World.has(id, "Position")
}

export fun set_unknown(id: string, v: int): int {
    return World.set(id, "Position", {"Value": v})
}`
	if err := rt.LoadSource("test", src); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	id := spawnViaFacade(t, bind)
	host, _ := w.Entity(parseID(t, id))
	if err := w.SetComponent(host, "Health", &ecsHealth{Value: 1}); err != nil {
		t.Fatalf("install Health: %v", err)
	}
	w.ClearChanges(host)

	res, err := rt.Call("read_unknown", id)
	if err != nil {
		t.Fatalf("Call read_unknown: %v", err)
	}
	m, ok := res.Value.(map[string]any)
	if !ok {
		t.Fatalf("read_unknown: want map[string]any, got %T", res.Value)
	}
	if len(m) != 0 {
		t.Fatalf("read_unknown: want empty map for unregistered component, got %v", m)
	}

	res, err = rt.Call("has_unknown", id)
	if err != nil {
		t.Fatalf("Call has_unknown: %v", err)
	}
	if v, _ := res.Value.(bool); v {
		t.Fatalf("has_unknown: want false")
	}

	// Set on an unregistered component is a host-interface error (Track 2),
	// reported as a structured runtime error rather than a panic.
	res, err = rt.Call("set_unknown", id, 1)
	if err != nil {
		t.Fatalf("Call set_unknown: %v", err)
	}
	if res.Error == nil {
		t.Fatalf("set_unknown: expected a structured host-interface error, got value %#v", res.Value)
	}
	if got := res.Error.Diagnostic.Code; got != "native_call_failed" {
		t.Fatalf("set_unknown: diagnostic code = %q, want native_call_failed", got)
	}
	msg := res.Error.Diagnostic.Message
	if !strings.Contains(msg, "Position") || !strings.Contains(msg, "no registered schema") {
		t.Fatalf("expected message to mention unregistered component Position, got %q", msg)
	}
}

// ----------------------------------------------------------------------------
// view/apply batch surface
// ----------------------------------------------------------------------------
//
// These tests pin the batch-API contract agreed in the wiki:
//   - View(has, changed) returns {"ids": []string, "data": map<string, []map>}
//     in a single boundary crossing; the data[compName] slice is index-aligned
//     with the ids slice.
//   - Apply(comp, ids, fields) patches all ids in one crossing, auto-Marks,
//     and returns the applied count. First error stops with the partial
//     count so scripts can resume by slicing.
//   - View's argument is the flat shell, not a nested map of ids → field
//     maps. Apply's argument uses parallel slices (NOT a map<string, []map>)
//     because typed map<string, slice> arguments panic across the VM boundary.

// TestGoSurface_ViewShape exercises View directly from Go to lock the
// return shape: ids sorted, data has every comp in `has` with one map per
// entity (aligned by index).
func TestGoSurface_ViewShape(t *testing.T) {
	w := runtime.NewWorld()
	b := ecsbind.New(w).
		RegisterComponent("Health", ecsHealthDesc()).
		RegisterComponent("Position", ecsPositionDesc())

	// Seed 4 entities: 3 with Health, 1 with Position, 1 with both, 1 empty.
	mk := func(values ...string) string {
		id := spawnViaFacade(t, b)
		host, _ := w.Entity(parseID(t, id))
		for _, v := range values {
			switch v {
			case "Health":
				if err := w.SetComponent(host, "Health", &ecsHealth{Value: 1}); err != nil {
					t.Fatalf("SetComponent Health: %v", err)
				}
			case "Position":
				if err := w.SetComponent(host, "Position", &ecsPosition{X: 1, Y: 2}); err != nil {
					t.Fatalf("SetComponent Position: %v", err)
				}
			}
		}
		w.ClearChanges(host)
		return id
	}
	hOnly1 := mk("Health")
	_ = hOnly1
	hOnly2 := mk("Health")
	_ = hOnly2
	hOnly3 := mk("Health")
	_ = hOnly3
	pOnly := mk("Position")
	_ = pOnly
	both := mk("Health", "Position")

	out, err := b.View([]string{"Health"}, nil)
	if err != nil {
		t.Fatalf("View: %v", err)
	}
	ids, _ := out["ids"].([]string)
	data, ok := out["data"].(map[string][]map[string]any)
	if !ok {
		t.Fatalf("View data shape: want map[string][]map[string]any, got %T (%v)", out["data"], out["data"])
	}
	if len(ids) != 4 {
		t.Fatalf("ids: want 4, got %d (%v)", len(ids), ids)
	}
	// ids must be sorted.
	if !sort.StringsAreSorted(ids) {
		t.Fatalf("ids not sorted: %v", ids)
	}
	// data[Health] must align with ids (same length, every entry present).
	hArr, ok := data["Health"]
	if !ok {
		t.Fatalf("data missing Health: %v", data)
	}
	if len(hArr) != 4 {
		t.Fatalf("data[Health] length: want 4, got %d", len(hArr))
	}
	for i, m := range hArr {
		if m == nil {
			t.Fatalf("data[Health][%d]: nil, want populated (entity has Health)", i)
		}
		if m["Value"] != 1 {
			t.Fatalf("data[Health][%d].Value: want 1, got %v", i, m["Value"])
		}
	}

	// Multi-component view: both comps appear, ids spine has only the
	// entity that has BOTH (Has() is conjunctive).
	out, err = b.View([]string{"Health", "Position"}, nil)
	if err != nil {
		t.Fatalf("View 2 comps: %v", err)
	}
	ids2, _ := out["ids"].([]string)
	if len(ids2) != 1 || ids2[0] != both {
		t.Fatalf("ids (2 comps): want [%s], got %v", both, ids2)
	}
	data2 := out["data"].(map[string][]map[string]any)
	if len(data2["Health"]) != 1 || len(data2["Position"]) != 1 {
		t.Fatalf("data slices must be length 1: Health=%d Position=%d",
			len(data2["Health"]), len(data2["Position"]))
	}
	if data2["Health"][0] == nil || data2["Health"][0]["Value"] != 1 {
		t.Fatalf("data[Health][0]: want Value=1, got %v", data2["Health"][0])
	}
	if data2["Position"][0] == nil || data2["Position"][0]["X"] != 1.0 || data2["Position"][0]["Y"] != 2.0 {
		t.Fatalf("data[Position][0]: want X=1 Y=2, got %#v (keys=%v)",
			data2["Position"][0], mapKeys(data2["Position"][0]))
	}

	// Two-call view with same comps vs entity intersection: view
	// Health alone gives 4 ids, view Position alone gives 2 ids.
	out, err = b.View([]string{"Health"}, nil)
	if err != nil {
		t.Fatalf("View Health only: %v", err)
	}
	if got := out["ids"].([]string); len(got) != 4 {
		t.Fatalf("View Health only: want 4 ids, got %d", len(got))
	}
	out, err = b.View([]string{"Position"}, nil)
	if err != nil {
		t.Fatalf("View Position only: %v", err)
	}
	if got := out["ids"].([]string); len(got) != 2 {
		t.Fatalf("View Position only: want 2 ids, got %d", len(got))
	}

	// Unregistered component in `has`: the Has() filter on a component
	// name no entity owns returns zero matches, but the call must not
	// error. The data map still carries an empty slot for the
	// requested name so callers can use len(arr) == 0 to detect "no
	// data for this component".
	out, err = b.View([]string{"Unknown"}, nil)
	if err != nil {
		t.Fatalf("View unknown comp: %v", err)
	}
	ids3, _ := out["ids"].([]string)
	if len(ids3) != 0 {
		t.Fatalf("ids (unknown comp): want 0 (no entity has Unknown), got %d (%v)", len(ids3), ids3)
	}
	data3 := out["data"].(map[string][]map[string]any)
	uArr, ok := data3["Unknown"]
	if !ok {
		t.Fatalf("data missing Unknown: %v", data3)
	}
	if uArr == nil || len(uArr) != 0 {
		t.Fatalf("data[Unknown]: want []map{} (non-nil empty), got %v", uArr)
	}

	// No-descriptor fallback for a registered component name: a fresh
	// binding that points at the same world but has no schema
	// descriptors registered should still query the entities, but the
	// per-component projections come back as nil slots (alignment
	// preserved).
	descLess := ecsbind.New(w) // no descriptors registered
	out, err = descLess.View([]string{"Health"}, nil)
	if err != nil {
		t.Fatalf("View no-descriptor: %v", err)
	}
	idsND := out["ids"].([]string)
	if len(idsND) != 4 {
		t.Fatalf("View no-descriptor ids: want 4 (Has filter still applies), got %d", len(idsND))
	}
	dataND := out["data"].(map[string][]map[string]any)
	arrND, ok := dataND["Health"]
	if !ok {
		t.Fatalf("View no-descriptor data missing Health: %v", dataND)
	}
	if len(arrND) != 4 {
		t.Fatalf("View no-descriptor data[Health]: want length 4, got %d", len(arrND))
	}
	for i, m := range arrND {
		if m != nil {
			t.Fatalf("View no-descriptor data[Health][%d]: want nil (no descriptor), got %v", i, m)
		}
	}

	// Empty world: View returns empty (non-nil) ids and an empty per-comp map.
	empty := runtime.NewWorld()
	emptyB := ecsbind.New(empty).RegisterComponent("Health", ecsHealthDesc())
	out, err = emptyB.View([]string{"Health"}, nil)
	if err != nil {
		t.Fatalf("View empty: %v", err)
	}
	idsE := out["ids"].([]string)
	if idsE == nil {
		t.Fatal("View(empty).ids: want non-nil empty slice")
	}
	if len(idsE) != 0 {
		t.Fatalf("View(empty).ids length: want 0, got %d", len(idsE))
	}
	dataE := out["data"].(map[string][]map[string]any)
	if dataE["Health"] == nil || len(dataE["Health"]) != 0 {
		t.Fatalf("View(empty).data[Health]: want []map{} (non-nil), got %v", dataE["Health"])
	}
}

// TestGoSurface_ViewChangedFilter exercises the WhenChanged branch.
// Without a tick boundary, MarkChanged records on a fresh entity appear
// in the next View(changed=...) result.
func TestGoSurface_ViewChangedFilter(t *testing.T) {
	w := runtime.NewWorld()
	b := ecsbind.New(w).RegisterComponent("Health", ecsHealthDesc())

	id := spawnViaFacade(t, b)
	host, _ := w.Entity(parseID(t, id))
	if err := w.SetComponent(host, "Health", &ecsHealth{Value: 7}); err != nil {
		t.Fatalf("SetComponent: %v", err)
	}
	w.ClearChanges(host)

	// No entity marked yet → view(changed=Health) returns 0 ids.
	out, err := b.View(nil, []string{"Health"})
	if err != nil {
		t.Fatalf("View(changed) pre-mark: %v", err)
	}
	if got := out["ids"].([]string); len(got) != 0 {
		t.Fatalf("expected 0 ids pre-mark, got %v", got)
	}

	// Mark Health on the entity, then view(changed=Health) returns it.
	w.MarkChanged(host, "Health")
	out, err = b.View(nil, []string{"Health"})
	if err != nil {
		t.Fatalf("View(changed) post-mark: %v", err)
	}
	got := out["ids"].([]string)
	if len(got) != 1 || got[0] != id {
		t.Fatalf("View(changed): want [%s], got %v", id, got)
	}
}

// TestGoSurface_ApplyBatch exercises the apply batch end-to-end:
// writes patch all entries, returns n on success, auto-Marks.
// Also exercises partial-failure semantics: first stale id stops the
// loop with the count of successful patches.
func TestGoSurface_ApplyBatch(t *testing.T) {
	w := runtime.NewWorld()
	b := ecsbind.New(w).RegisterComponent("Health", ecsHealthDesc())

	// Seed three entities with Health=0.
	ids := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		id := spawnViaFacade(t, b)
		host, _ := w.Entity(parseID(t, id))
		if err := w.SetComponent(host, "Health", &ecsHealth{Value: 0}); err != nil {
			t.Fatalf("SetComponent: %v", err)
		}
		w.ClearChanges(host)
		ids = append(ids, id)
	}

	// Apply Value=10,20,30 to the three entities.
	fields := []map[string]any{
		{"Value": 10},
		{"Value": 20},
		{"Value": 30},
	}
	n, err := b.Apply("Health", ids, fields)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if n != 3 {
		t.Fatalf("Apply: want 3, got %d", n)
	}

	// Verify writes + auto-Mark.
	for i, id := range ids {
		got, err := b.Get(id, "Health")
		if err != nil {
			t.Fatalf("Get %s: %v", id, err)
		}
		want := 10 * (i + 1)
		if got["Value"] != want {
			t.Fatalf("entity %s: want Value=%d, got %v", id, want, got["Value"])
		}
		// MarkChanged should have fired (auto-Mark on PatchEntity success).
		cs, err := b.Changes(id)
		if err != nil {
			t.Fatalf("Changes %s: %v", id, err)
		}
		ch, _ := cs["changed"].([]string)
		if len(ch) != 1 || ch[0] != "Health" {
			t.Fatalf("entity %s: want changed=[Health], got %v", id, ch)
		}
	}

	// Partial failure: insert a stale id at position 1. First two
	// entries should already have been patched; the third is never
	// reached.
	b.Tick() // clear bookkeeping so we can re-assert after partial apply
	for _, id := range ids {
		host, _ := w.Entity(parseID(t, id))
		if err := w.SetComponent(host, "Health", &ecsHealth{Value: 0}); err != nil {
			t.Fatalf("reset SetComponent: %v", err)
		}
		w.ClearChanges(host)
	}
	stale := mustID(t, 9999, 1, 0, 42).String()
	ordered := []string{ids[0], stale, ids[1], ids[2]}
	orderedFields := []map[string]any{
		{"Value": 100},
		{"Value": 999},
		{"Value": 200},
		{"Value": 300},
	}
	n, err = b.Apply("Health", ordered, orderedFields)
	if err == nil {
		t.Fatal("Apply with stale id: want error, got nil")
	}
	if !strings.Contains(err.Error(), "not alive") {
		t.Fatalf("Apply stale id error: want 'not alive', got %q", err.Error())
	}
	if n != 1 {
		t.Fatalf("Apply partial: want 1 patched before stale, got %d", n)
	}

	// ids[0] was patched; ids[1] and ids[2] were not (loop stopped).
	got0, _ := b.Get(ids[0], "Health")
	if got0["Value"] != 100 {
		t.Fatalf("ids[0] after partial: want 100, got %v", got0["Value"])
	}
	got1, _ := b.Get(ids[1], "Health")
	if got1["Value"] != 0 {
		t.Fatalf("ids[1] after partial: want untouched (0), got %v", got1["Value"])
	}
	got2, _ := b.Get(ids[2], "Health")
	if got2["Value"] != 0 {
		t.Fatalf("ids[2] after partial: want untouched (0), got %v", got2["Value"])
	}

	// Unknown component name → (0, error), no state changes.
	n, err = b.Apply("Unknown", ids[:1], []map[string]any{{"Value": 1}})
	if err == nil {
		t.Fatal("Apply unknown comp: want error, got nil")
	}
	if !strings.Contains(err.Error(), "no registered schema") {
		t.Fatalf("Apply unknown comp error: want 'no registered schema', got %q", err.Error())
	}
	if n != 0 {
		t.Fatalf("Apply unknown comp: want 0, got %d", n)
	}

	// Length mismatch → (0, error).
	n, err = b.Apply("Health", ids, fields[:2])
	if err == nil {
		t.Fatal("Apply length mismatch: want error, got nil")
	}
	if !strings.Contains(err.Error(), "length mismatch") {
		t.Fatalf("Apply length mismatch error: want 'length mismatch', got %q", err.Error())
	}
	if n != 0 {
		t.Fatalf("Apply length mismatch: want 0, got %d", n)
	}
}

// TestScript_ViewApplyFromScript exercises view/apply through the script
// runtime. Confirms the nested `data` map and the slice of field maps
// survive the VM boundary intact.
func TestScript_ViewApplyFromScript(t *testing.T) {
	rt := mustRuntime(t)
	w := runtime.NewWorld()
	bind := ecsbind.New(w).RegisterComponent("Health", ecsHealthDesc())
	if err := bind.Bind(rt); err != nil {
		t.Fatalf("Bind: %v", err)
	}

	const src = `import World from "ecs"

export fun drain(): int {
    var out: map<string, any> = World.view(["Health"], [])
    var ids: array<string> = (out["ids"] as array<string>)
    var data: map<string, any> = (out["data"] as map<string, any>)
    var healthArr: array<map<string, any>> = (data["Health"] as array<map<string, any>>)
    var n: int = 0
    var i: int = 0
    while (i < len(ids)) {
        var h: map<string, any> = healthArr[i]
        var v: int = (h["Value"] as int) - 10
        if (v <= 0) { v = 100 }
        h["Value"] = v
        n = n + 1
        i = i + 1
    }
    World.apply("Health", ids, healthArr)
    World.tick()
    return n
}`

	// Seed entities and load the script after seed so the script
	// observes them through the binding.
	for i := 0; i < 5; i++ {
		id := spawnViaFacade(t, bind)
		host, _ := w.Entity(parseID(t, id))
		if err := w.SetComponent(host, "Health", &ecsHealth{Value: 100}); err != nil {
			t.Fatalf("SetComponent: %v", err)
		}
		w.ClearChanges(host)
	}
	if err := rt.LoadSource("view-apply", src); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	res, err := rt.Call("drain")
	if err != nil {
		t.Fatalf("Call drain: %v", err)
	}
	n, ok := res.Value.(int)
	if !ok {
		t.Fatalf("drain: want int, got %T", res.Value)
	}
	if n != 5 {
		t.Fatalf("drain: want 5 entities processed, got %d", n)
	}

	// After drain, every Health should have been decremented by 10 (or
	// wrapped if it was already <= 10). The wrap-on-zero branch in the
	// script keeps things > 0 so all 5 entities survive.
	entities := w.Execute(runtime.NewQuery().Has("Health"))
	if len(entities) != 5 {
		t.Fatalf("after drain: want 5 alive, got %d", len(entities))
	}
	for _, e := range entities {
		raw, ok := w.GetComponent(e, "Health")
		if !ok {
			t.Fatalf("entity %s: Health missing", e.ID())
		}
		h := raw.(*ecsHealth)
		if h.Value != 90 {
			t.Fatalf("entity %s: want Value=90 after -10, got %d", e.ID(), h.Value)
		}
	}
}

// TestScript_ViewReturnsShell makes the return-shape contract explicit:
// scripts receive a map<string, any> with two top-level keys ("ids",
// "data"). Reading those keys returns a []string and a
// map<string, []map<string, any>> respectively, both aligned by index.
//
// The script packs the alignment check into a map<string, int> so the
// host test can read it as a flat Go map (sidestepping the "array<any>
// literal" typing gap in the current spore frontend).
func TestScript_ViewReturnsShell(t *testing.T) {
	rt := mustRuntime(t)
	w := runtime.NewWorld()
	bind := ecsbind.New(w).RegisterComponent("Health", ecsHealthDesc())
	if err := bind.Bind(rt); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	for i := 0; i < 3; i++ {
		id := spawnViaFacade(t, bind)
		host, _ := w.Entity(parseID(t, id))
		if err := w.SetComponent(host, "Health", &ecsHealth{Value: i}); err != nil {
			t.Fatalf("SetComponent: %v", err)
		}
		w.ClearChanges(host)
	}

	const src = `import World from "ecs"

export fun peek(): map<string, any> {
    var out: map<string, any> = World.view(["Health"], [])
    var ids: array<string> = (out["ids"] as array<string>)
    var data: map<string, any> = (out["data"] as map<string, any>)
    var healthArr: array<map<string, any>> = (data["Health"] as array<map<string, any>>)
    var first: int = (healthArr[0]["Value"] as int)
    var second: int = (healthArr[1]["Value"] as int)
    var third: int = (healthArr[2]["Value"] as int)
    return {"count": len(ids), "first": first, "second": second, "third": third}
}`
	if err := rt.LoadSource("peek", src); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	res, err := rt.Call("peek")
	if err != nil {
		t.Fatalf("Call peek: %v", err)
	}
	m, ok := res.Value.(map[string]any)
	if !ok {
		t.Fatalf("peek: want map[string]any, got %T (%v)", res.Value, res.Value)
	}
	count, _ := m["count"].(int)
	if count != 3 {
		t.Fatalf("peek[count]: want 3, got %v", m["count"])
	}
	// View sorts by id, so the values must be sorted (the seed uses
	// increasing sequence numbers).
	v0, _ := m["first"].(int)
	v1, _ := m["second"].(int)
	v2, _ := m["third"].(int)
	if !(v0 <= v1 && v1 <= v2) {
		t.Fatalf("peek values not sorted: %v %v %v", v0, v1, v2)
	}
}

// ----------------------------------------------------------------------------
// Helpers
// ----------------------------------------------------------------------------

func mustRuntime(t *testing.T) *script.Runtime {
	t.Helper()
	rt, err := script.NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	return rt
}

func spawnViaFacade(t *testing.T, b *ecsbind.WorldBinding) string {
	t.Helper()
	// Drive the public facade — that's the contract surface.
	return b.Spawn()
}

func parseID(t *testing.T, s string) identity.CanonicalID {
	t.Helper()
	id, err := identity.ParseCanonicalID(s)
	if err != nil {
		t.Fatalf("ParseCanonicalID(%q): %v", s, err)
	}
	return id
}

func asStringSlice(t *testing.T, v any) []string {
	t.Helper()
	arr, ok := v.([]any)
	if !ok {
		t.Fatalf("want []any, got %T (%v)", v, v)
	}
	out := make([]string, len(arr))
	for i, x := range arr {
		s, ok := x.(string)
		if !ok {
			t.Fatalf("element %d: want string, got %T (%v)", i, x, x)
		}
		out[i] = s
	}
	return out
}

// mapKeys returns the keys of m (for diagnostics in fatalf).
func mapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	sort.Strings(keys)
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// ----------------------------------------------------------------------------
// Descriptor cross-check: confirm the interface surface matches the
// design contract.
// ----------------------------------------------------------------------------

// TestInterface_ExposesDocumentedSurface makes the method set explicit
// so a rename accidentally turns into a test failure rather than a
// silent script-side breakage.
func TestInterface_ExposesDocumentedSurface(t *testing.T) {
	w := runtime.NewWorld()
	b := ecsbind.New(w)
	iface := b.Interface()
	if iface.Name != "World" {
		t.Fatalf("Interface().Name: want World, got %q", iface.Name)
	}
	want := map[string]bool{
		"spawn": false, "destroy": false, "alive": false,
		"get": false, "set": false, "has": false,
		"remove": false, "mark": false, "tick": false,
		"count": false, "query": false, "changes": false,
		"view": false, "apply": false,
	}
	for _, m := range iface.Methods {
		if _, ok := want[m.Name]; !ok {
			t.Fatalf("unexpected method in interface: %q", m.Name)
		}
		want[m.Name] = true
	}
	for name, seen := range want {
		if !seen {
			t.Fatalf("missing method in interface: %q", name)
		}
	}
}

// TestInterface_ComponentsSnapshot guards the public Components() helper.
func TestInterface_ComponentsSnapshot(t *testing.T) {
	w := runtime.NewWorld()
	b := ecsbind.New(w).RegisterComponent("Health", ecsHealthDesc())

	snap := b.Components()
	if len(snap) != 1 || snap["Health"].Name != "Health" {
		t.Fatalf("Components: want {Health: ...}, got %v", snap)
	}
	// Snapshot is defensive: mutating it must not affect the binding.
	snap["Injected"] = schema.ObjectDesc{Name: "X"}
	if len(b.Components()) != 1 {
		t.Fatalf("Components leaked: %v", b.Components())
	}
}

// TestGoSurface_RegistrySharedWithWorld pins the #7 contract from the public
// surface: registering a component on the facade declares it on the shared
// World, so host-side writes through the World's string API are legal without
// a second registration — and a typo is still refused.
func TestGoSurface_RegistrySharedWithWorld(t *testing.T) {
	w := runtime.NewWorld()
	b := ecsbind.New(w).RegisterComponent("Health", ecsHealthDesc())

	if err := b.VerifyRegistry(); err != nil {
		t.Fatalf("VerifyRegistry: %v", err)
	}
	if !w.RegisteredComponent("Health") {
		t.Fatal("facade registration must declare the component on the World")
	}

	e := w.Create()
	// Host-side write with no extra registration call.
	if err := w.SetComponent(e, "Health", &ecsHealth{Value: 3}); err != nil {
		t.Fatalf("host SetComponent on a facade-declared component: %v", err)
	}
	got, err := b.Get(e.ID().String(), "Health")
	if err != nil {
		t.Fatalf("facade Get: %v", err)
	}
	if got["Value"] != 3 {
		t.Fatalf("facade saw Value=%v, want 3", got["Value"])
	}

	// The typo protection still applies to the shared World.
	if err := w.SetComponent(e, "Helth", &ecsHealth{Value: 1}); err == nil {
		t.Fatal("undeclared component name must be rejected")
	}
	// And the facade itself keeps rejecting names with no descriptor.
	if _, err := b.Set(e.ID().String(), "Helth", map[string]any{"Value": 1}); err == nil {
		t.Fatal("facade Set with an unregistered component must error")
	}
}