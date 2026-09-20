package script

import (
	"testing"

	"github.com/qomos-w/spore/internal/script/vm"
	"github.com/qomos-w/spore/schema"
)

// ledgerGreeterDesc is a minimal single-method interface descriptor for the
// ledger tests.
func ledgerGreeterDesc() schema.InterfaceDesc {
	return schema.InterfaceDesc{
		Name: "Greeter",
		Methods: []schema.MethodDesc{{
			Name:       "greet",
			Parameters: []schema.ParameterDesc{{Name: "name", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}}},
			Returns:    []schema.TypeDesc{{Kind: schema.TypeKindScalar, Name: "string"}},
		}},
	}
}

// requireConsistentLedger asserts the three-index ledger invariant plus the
// cross-index agreement that makes it meaningful:
//
//   - consistencyError reports no violation of the structural invariant;
//   - every live object round-trips through objectForHandle;
//   - the number of live objects equals the number of handles;
//   - every binding resolves through lookupBound to the same object.
//
// It deliberately derives the handle->id reverse mapping itself (rather than
// asking the ledger) so it cannot mask a broken projection by trusting a
// same-bug accessor. Callers pass a stage label so a failure points at the
// lifecycle step that broke the invariant.
func requireConsistentLedger(t *testing.T, l *hostInterfaceLedger, stage string) {
	t.Helper()
	if msg := l.consistencyError(); msg != "" {
		t.Fatalf("%s: ledger inconsistent: %s", stage, msg)
	}
	handleByID := make(map[uint64]vm.Handle, len(l.handles))
	for h, id := range l.handles {
		handleByID[id] = h
	}
	live := 0
	for id := range l.objects {
		h, ok := handleByID[id]
		if !ok {
			continue
		}
		live++
		back, ok := l.objectForHandle(h)
		if !ok || back.ID != id {
			t.Fatalf("%s: handle %d for object %d did not round-trip", stage, uint64(h), id)
		}
	}
	if live != len(l.handles) {
		t.Fatalf("%s: %d live objects but %d handles", stage, live, len(l.handles))
	}
	for key, id := range l.bindings {
		bound, ok := l.lookupBound(key.Namespace, key.Name)
		if !ok || bound.ID != id {
			t.Fatalf("%s: binding %s/%s did not resolve to object %d", stage, key.Namespace, key.Name, id)
		}
	}
}

func requireAllObjectsLive(t *testing.T, l *hostInterfaceLedger, stage string) {
	t.Helper()
	live := l.liveIDs()
	for id := range l.objects {
		if _, ok := live[id]; !ok {
			t.Fatalf("%s: object %d is still pending after finalize", stage, id)
		}
	}
}

// TestHostInterfaceLedgerThreeIndexConsistency drives one ledger through bind,
// load, instance-registration, duplicate-registration, Reset and reload, and
// asserts the objects/handles/bindings invariant at every step.
func TestHostInterfaceLedgerThreeIndexConsistency(t *testing.T) {
	source := `import Greeter from "host"
export fun greet(): string { return Greeter.greet("x") }`

	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	a := &testHostGreeter{Prefix: "a-"}
	if err := rt.BindInterfaceObject("host", "Greeter", ledgerGreeterDesc(), a); err != nil {
		t.Fatalf("BindInterfaceObject: %v", err)
	}
	requireConsistentLedger(t, &rt.hostIface, "after bind (pending)")

	if err := rt.LoadSource("demo", source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	requireConsistentLedger(t, &rt.hostIface, "after load")
	requireAllObjectsLive(t, &rt.hostIface, "after load")
	if len(rt.hostIface.objects) == 0 || len(rt.hostIface.handles) == 0 || len(rt.hostIface.bindings) == 0 {
		t.Fatal("invariant check is vacuous: expected non-empty objects/handles/bindings")
	}

	// A second instance of the same symbol gets its own record and handle.
	b := &testHostGreeter{Prefix: "b-"}
	if err := rt.RegisterHostInterfaceInstance("host", "Greeter", b); err != nil {
		t.Fatalf("RegisterHostInterfaceInstance(b): %v", err)
	}
	requireConsistentLedger(t, &rt.hostIface, "after instance b")
	afterInstance := len(rt.hostIface.objects)
	if afterInstance != 2 {
		t.Fatalf("expected 2 object records after one extra instance, got %d", afterInstance)
	}

	// Re-registering an identical target must reuse the record, not duplicate.
	if err := rt.RegisterHostInterfaceInstance("host", "Greeter", b); err != nil {
		t.Fatalf("RegisterHostInterfaceInstance(b) again: %v", err)
	}
	if got := len(rt.hostIface.objects); got != afterInstance {
		t.Fatalf("identical target minted a duplicate record: %d -> %d", afterInstance, got)
	}
	requireConsistentLedger(t, &rt.hostIface, "after duplicate instance")

	// Reset invalidates the live projection locally and preserves the durable
	// indices verbatim.
	objectsBefore := len(rt.hostIface.objects)
	bindingsBefore := len(rt.hostIface.bindings)
	if err := rt.Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if len(rt.hostIface.objects) != objectsBefore {
		t.Fatalf("Reset dropped durable objects: %d -> %d", objectsBefore, len(rt.hostIface.objects))
	}
	if len(rt.hostIface.bindings) != bindingsBefore {
		t.Fatalf("Reset dropped durable bindings: %d -> %d", bindingsBefore, len(rt.hostIface.bindings))
	}
	if len(rt.hostIface.handles) != 0 {
		t.Fatalf("Reset must invalidate the live handle projection, got %d entries", len(rt.hostIface.handles))
	}
	requireConsistentLedger(t, &rt.hostIface, "after reset")

	// Reload re-materialises every durable record as a live proxy.
	if err := rt.LoadSource("demo2", source); err != nil {
		t.Fatalf("reload: %v", err)
	}
	requireConsistentLedger(t, &rt.hostIface, "after reload")
	requireAllObjectsLive(t, &rt.hostIface, "after reload")
	if len(rt.hostIface.handles) != len(rt.hostIface.objects) {
		t.Fatalf("after reload: %d handles for %d objects", len(rt.hostIface.handles), len(rt.hostIface.objects))
	}
}

// TestHostInterfaceLedgerResetIsLocalInvalidation pins the reshape's core
// property: Reset replaces the VM-scoped projection instead of rewriting every
// object record, so the durable records are bit-identical across the Reset
// (same IDs, same targets, same descriptors).
func TestHostInterfaceLedgerResetIsLocalInvalidation(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	greeter := &testHostGreeter{Prefix: "reset-"}
	if err := rt.BindInterfaceObject("host", "Greeter", ledgerGreeterDesc(), greeter); err != nil {
		t.Fatalf("BindInterfaceObject: %v", err)
	}
	if err := rt.LoadSource("demo", `import Greeter from "host"
export fun greet(): string { return Greeter.greet("x") }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	before := make(map[uint64]hostInterfaceObject, len(rt.hostIface.objects))
	for id, obj := range rt.hostIface.objects {
		before[id] = obj
	}
	nextBefore := rt.hostIface.nextID

	if err := rt.Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if rt.hostIface.nextID != nextBefore {
		t.Fatalf("Reset renumbered the identity space: %d -> %d", nextBefore, rt.hostIface.nextID)
	}
	if len(rt.hostIface.objects) != len(before) {
		t.Fatalf("object count changed across Reset: %d -> %d", len(before), len(rt.hostIface.objects))
	}
	for id, want := range before {
		got, ok := rt.hostIface.objects[id]
		if !ok {
			t.Fatalf("object %d missing after Reset", id)
		}
		if got.Namespace != want.Namespace || got.Name != want.Name || got.Target != want.Target {
			t.Fatalf("object %d changed across Reset: %+v -> %+v", id, want, got)
		}
	}
}

// TestHostInterfaceLedgerCloneIsIndependent checks that a clone owns a private
// durable copy and its own (initially empty, then VM-bound) live projection,
// and that mutating the clone cannot perturb the original.
func TestHostInterfaceLedgerCloneIsIndependent(t *testing.T) {
	source := `import Greeter from "host"
export fun greet(): string { return Greeter.greet("x") }`

	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	greeter := &testHostGreeter{Prefix: "orig-"}
	if err := rt.BindInterfaceObject("host", "Greeter", ledgerGreeterDesc(), greeter); err != nil {
		t.Fatalf("BindInterfaceObject: %v", err)
	}
	if err := rt.LoadSource("demo", source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	cloned, err := rt.Clone()
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	requireConsistentLedger(t, &cloned.hostIface, "cloned fresh")
	if len(cloned.hostIface.handles) != 0 {
		t.Fatalf("clone must start with an empty live projection, got %d handles", len(cloned.hostIface.handles))
	}
	if len(cloned.hostIface.objects) != len(rt.hostIface.objects) {
		t.Fatalf("clone copied %d objects, want %d", len(cloned.hostIface.objects), len(rt.hostIface.objects))
	}
	if cloned.hostIface.nextID != rt.hostIface.nextID {
		t.Fatalf("clone must inherit the identity allocator: %d != %d", cloned.hostIface.nextID, rt.hostIface.nextID)
	}

	if err := cloned.LoadSource("cloned", source); err != nil {
		t.Fatalf("cloned LoadSource: %v", err)
	}
	requireConsistentLedger(t, &cloned.hostIface, "cloned loaded")

	origObjects := len(rt.hostIface.objects)
	origHandles := len(rt.hostIface.handles)
	extra := &testHostGreeter{Prefix: "extra-"}
	if err := cloned.RegisterHostInterfaceInstance("host", "Greeter", extra); err != nil {
		t.Fatalf("cloned RegisterHostInterfaceInstance: %v", err)
	}
	requireConsistentLedger(t, &cloned.hostIface, "cloned after instance")
	if len(rt.hostIface.objects) != origObjects || len(rt.hostIface.handles) != origHandles {
		t.Fatal("mutating the clone perturbed the original ledger")
	}
}

// TestHostInterfaceLedgerCloseReleasesEverything checks that Close clears every
// index — including the VM-scoped projection and the root-provider bookkeeping
// that the old ledger left dangling.
func TestHostInterfaceLedgerCloseReleasesEverything(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.BindInterfaceObject("host", "Greeter", ledgerGreeterDesc(), &testHostGreeter{Prefix: "c-"}); err != nil {
		t.Fatalf("BindInterfaceObject: %v", err)
	}
	if err := rt.LoadSource("demo", `import Greeter from "host"
export fun greet(): string { return Greeter.greet("x") }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	if rt.hostIface.vm == nil || len(rt.hostIface.handles) == 0 || len(rt.hostIface.objects) == 0 {
		t.Fatal("precondition: ledger should hold a live VM projection before Close")
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	l := &rt.hostIface
	if l.objects != nil || l.bindings != nil || l.handles != nil || l.vm != nil || l.rootProviderID != 0 || l.nextID != 0 {
		t.Fatalf("Close left ledger state behind: %+v", *l)
	}
}
