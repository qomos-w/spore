package binding_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/identity"
	"github.com/qomos-w/spore/schema"
)

// ============================================================================
// ScriptBinding facade tests
// ============================================================================

func TestScriptBinding_BindFunction_RegistersAndInvokes(t *testing.T) {
	sb := binding.NewScriptBinding()

	if err := sb.BindFunction("greet", greet); err != nil {
		t.Fatalf("BindFunction: %v", err)
	}

	// Verify callable is registered
	desc, ok := sb.Callables.Lookup("greet")
	if !ok {
		t.Fatal("expected to find registered callable")
	}
	if desc.Name != "greet" {
		t.Fatalf("expected callable name greet, got %s", desc.Name)
	}

	// Verify adapter is registered
	adapter, ok := sb.Executors.Lookup("greet")
	if !ok {
		t.Fatal("expected to find registered adapter")
	}
	if adapter.Callable().Name != "greet" {
		t.Fatalf("expected adapter callable name greet, got %s", adapter.Callable().Name)
	}

	// Verify invocation works
	outcome, err := sb.Invoke(binding.InvocationRequest{
		Callable: "greet",
		Stage:    binding.InvocationStageUnary,
		Args:     []any{1, "Alice"},
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != "Alice" {
		t.Fatalf("expected payload Alice, got %v", outcome.Payload)
	}
}

func TestScriptBinding_Invoke_ProjectsError(t *testing.T) {
	sb := binding.NewScriptBinding()

	if err := sb.BindFunction("fail", fail); err != nil {
		t.Fatalf("BindFunction: %v", err)
	}

	outcome, err := sb.Invoke(binding.InvocationRequest{
		Callable: "fail",
		Stage:    binding.InvocationStageUnary,
		Args:     []any{1},
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}

	if outcome.Result.Kind != binding.InvocationResultError {
		t.Fatalf("expected error result, got %s", outcome.Result.Kind)
	}
	if outcome.Result.Error == nil {
		t.Fatal("expected error descriptor")
	}
	if outcome.Result.Error.Message != "boom" {
		t.Fatalf("expected error message boom, got %s", outcome.Result.Error.Message)
	}
	if outcome.Payload != nil {
		t.Fatalf("expected nil payload for error result, got %+v", outcome.Payload)
	}
}

func TestScriptBinding_ExposeCapabilityCallables_RegistersAndInvokes(t *testing.T) {
	sb := binding.NewScriptBinding()
	builder := binding.NewCapability("tool", "service")
	if err := builder.AddFunction("execute", capabilityEchoContext); err != nil {
		t.Fatalf("AddFunction: %v", err)
	}
	cap, err := builder.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := sb.RegisterCapability(cap); err != nil {
		t.Fatalf("RegisterCapability: %v", err)
	}
	if err := sb.ExposeCapabilityCallables("tool"); err != nil {
		t.Fatalf("ExposeCapabilityCallables: %v", err)
	}
	desc, ok := sb.Callables.Lookup("tool.execute")
	if !ok {
		t.Fatal("expected flattened callable registration")
	}
	if desc.Parameters[0].Name != "input" {
		t.Fatalf("expected input parameter, got %s", desc.Parameters[0].Name)
	}
	adapter, ok := sb.Executors.Lookup("tool.execute")
	if !ok {
		t.Fatal("expected flattened executable adapter")
	}
	if adapter.Callable().Name != "tool.execute" {
		t.Fatalf("expected adapter callable tool.execute, got %s", adapter.Callable().Name)
	}
	outcome, err := sb.Invoke(binding.InvocationRequest{
		Callable: "tool.execute",
		Stage:    binding.InvocationStageUnary,
		Args:     []any{map[string]any{"Text": "hello"}},
		Context:  context.WithValue(context.Background(), "prefix", "ctx:"),
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	out := outcome.Payload.Value.(capabilityOutput)
	if out.Text != "ctx:hello" {
		t.Fatalf("expected ctx:hello, got %q", out.Text)
	}
}

func TestScriptBinding_ExposeCapabilityCallables_RejectsConflictsBeforePartialRegistration(t *testing.T) {
	sb := binding.NewScriptBinding()
	builder := binding.NewCapability("tool", "service")
	if err := builder.AddFunction("execute", capabilityEcho); err != nil {
		t.Fatalf("AddFunction: %v", err)
	}
	if err := builder.AddFunction("specs", capabilityEcho); err != nil {
		t.Fatalf("AddFunction specs: %v", err)
	}
	cap, err := builder.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := sb.RegisterCapability(cap); err != nil {
		t.Fatalf("RegisterCapability: %v", err)
	}
	if err := sb.BindFunction("tool.specs", func(id int) string { return "conflict" }); err != nil {
		t.Fatalf("BindFunction conflict: %v", err)
	}
	if err := sb.ExposeCapabilityCallables("tool"); err == nil {
		t.Fatal("expected conflict error")
	}
	if _, ok := sb.Callables.Lookup("tool.execute"); ok {
		t.Fatal("expected no partial callable registration")
	}
	if _, ok := sb.Executors.Lookup("tool.execute"); ok {
		t.Fatal("expected no partial adapter registration")
	}
}

func TestScriptBinding_ExposeCapabilityCallables_RejectsDuplicateExposure(t *testing.T) {
	sb := binding.NewScriptBinding()
	builder := binding.NewCapability("tool", "service")
	if err := builder.AddFunction("execute", capabilityEcho); err != nil {
		t.Fatalf("AddFunction: %v", err)
	}
	cap, err := builder.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := sb.RegisterCapability(cap); err != nil {
		t.Fatalf("RegisterCapability: %v", err)
	}
	if err := sb.ExposeCapabilityCallables("tool"); err != nil {
		t.Fatalf("first ExposeCapabilityCallables: %v", err)
	}
	if err := sb.ExposeCapabilityCallables("tool"); err == nil {
		t.Fatal("expected duplicate exposure error")
	}
}

func TestScriptBinding_ExposeCapabilityCallables_UnknownCapability(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := sb.ExposeCapabilityCallables("tool"); err == nil {
		t.Fatal("expected unknown capability error")
	}
}

func TestScriptBinding_BindObject_CreatesValidBinding(t *testing.T) {
	sb := binding.NewScriptBinding()

	type testStruct struct {
		Name  string
		Level int
	}

	classDesc := schema.ObjectDesc{
		Name: "testStruct",
		Fields: []schema.FieldDesc{
			{Name: "Name", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
			{Name: "Level", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}},
		},
	}

	id := mustTestID(t, 2000, 1, 0, 1)
	obj := testStruct{Name: "hero", Level: 5}

	b, err := sb.BindObject(classDesc, id, &obj)
	if err != nil {
		t.Fatalf("BindObject: %v", err)
	}
	if !b.Valid() {
		t.Fatal("expected valid binding")
	}
	if b.Identity != id {
		t.Fatal("binding identity mismatch")
	}
	if b.Schema.Name != "testStruct" {
		t.Fatalf("expected schema name testStruct, got %s", b.Schema.Name)
	}
}

// ============================================================================
// helpers
// ============================================================================

func mustTestID(t *testing.T, ts uint64, slot uint16, inc uint16, seq uint64) identity.CanonicalID {
	t.Helper()
	id, err := identity.NewCanonicalID(ts, slot, inc, seq)
	if err != nil {
		t.Fatalf("NewCanonicalID: %v", err)
	}
	return id
}

// Ensure errors import is used
var _ = errors.New("")

// ============================================================================
// ScriptBinding lifecycle observability tests
//
// These tests lock the script-visible object lifecycle contract:
// ScriptBinding tracks bound objects and provides an InvalidateBindingsFor
// mechanism that external callers use to notify bindings when their
// backing entities expire. ScriptBinding does NOT define lifecycle
// authority — that belongs to the hosting layer.
// ============================================================================

func TestScriptBinding_BindObject_Tracked(t *testing.T) {
	sb := binding.NewScriptBinding()

	type item struct{ Name string }
	desc := schema.ObjectDesc{
		Name:   "item",
		Fields: []schema.FieldDesc{{Name: "Name", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}}},
	}

	id := mustTestID(t, 3000, 1, 0, 1)
	obj := item{Name: "sword"}
	b, err := sb.BindObject(desc, id, &obj)
	if err != nil {
		t.Fatalf("BindObject: %v", err)
	}

	// Lookup by identity should find the binding
	found, ok := sb.LookupBinding(id)
	if !ok {
		t.Fatal("expected to find binding by identity")
	}
	if found != b {
		t.Fatal("expected same binding instance")
	}

	// Unknown identity should not be found
	_, ok = sb.LookupBinding(mustTestID(t, 9999, 0, 0, 0))
	if ok {
		t.Fatal("expected not to find binding for unknown identity")
	}
}

func TestScriptBinding_InvalidateBindingsFor_InvalidatesBoundObject(t *testing.T) {
	sb := binding.NewScriptBinding()

	type item struct{ Name string }
	desc := schema.ObjectDesc{
		Name:   "item",
		Fields: []schema.FieldDesc{{Name: "Name", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}}},
	}

	id := mustTestID(t, 3000, 2, 0, 1)
	obj := item{Name: "shield"}
	b, err := sb.BindObject(desc, id, &obj)
	if err != nil {
		t.Fatalf("BindObject: %v", err)
	}
	if !b.Valid() {
		t.Fatal("expected valid binding before invalidation")
	}

	// Invalidate
	sb.InvalidateBindingsFor(id)

	if b.Valid() {
		t.Fatal("expected binding to be invalid after InvalidateBindingsFor")
	}

	// Lookup should no longer find it (valid-only)
	_, ok := sb.LookupBinding(id)
	if ok {
		t.Fatal("expected LookupBinding to return false for invalidated binding")
	}
}

func TestScriptBinding_BindObject_RebindInvalidatesOldBinding(t *testing.T) {
	sb := binding.NewScriptBinding()

	type item struct{ Name string }
	desc := schema.ObjectDesc{
		Name:   "item",
		Fields: []schema.FieldDesc{{Name: "Name", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}}},
	}

	id := mustTestID(t, 3000, 7, 0, 1)

	old, err := sb.BindObject(desc, id, &item{Name: "v1"})
	if err != nil {
		t.Fatalf("BindObject v1: %v", err)
	}

	// Rebind same identity — old binding should be invalidated
	rebound, err := sb.BindObject(desc, id, &item{Name: "v2"})
	if err != nil {
		t.Fatalf("BindObject v2: %v", err)
	}

	if old.Valid() {
		t.Fatal("expected old binding to be invalidated after rebind")
	}
	if !rebound.Valid() {
		t.Fatal("expected new binding to be valid")
	}

	// LookupBinding should return the new binding
	found, ok := sb.LookupBinding(id)
	if !ok {
		t.Fatal("expected LookupBinding to find new binding")
	}
	if found != rebound {
		t.Fatal("expected LookupBinding to return new binding instance")
	}

	// Projection on old binding should fail
	_, err = binding.ProjectView(old)
	if err == nil {
		t.Fatal("expected error projecting invalidated old binding")
	}
}

func TestScriptBinding_InvalidateBindingsFor_DoesNotAffectOtherBindings(t *testing.T) {
	sb := binding.NewScriptBinding()

	type item struct{ Name string }
	desc := schema.ObjectDesc{
		Name:   "item",
		Fields: []schema.FieldDesc{{Name: "Name", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}}},
	}

	id1 := mustTestID(t, 3000, 3, 0, 1)
	id2 := mustTestID(t, 3000, 3, 0, 2)

	b1, _ := sb.BindObject(desc, id1, &item{Name: "sword"})
	b2, _ := sb.BindObject(desc, id2, &item{Name: "shield"})

	// Invalidate only id1
	sb.InvalidateBindingsFor(id1)

	if b1.Valid() {
		t.Fatal("expected b1 to be invalidated")
	}
	if !b2.Valid() {
		t.Fatal("expected b2 to remain valid")
	}

	_, ok := sb.LookupBinding(id2)
	if !ok {
		t.Fatal("expected id2 binding to still be lookup-able")
	}
}

func TestScriptBinding_BoundIdentities_ReturnsActiveOnly(t *testing.T) {
	sb := binding.NewScriptBinding()

	type item struct{ Name string }
	desc := schema.ObjectDesc{
		Name:   "item",
		Fields: []schema.FieldDesc{{Name: "Name", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}}},
	}

	id1 := mustTestID(t, 3000, 4, 0, 1)
	id2 := mustTestID(t, 3000, 4, 0, 2)
	id3 := mustTestID(t, 3000, 4, 0, 3)

	sb.BindObject(desc, id1, &item{Name: "a"})
	sb.BindObject(desc, id2, &item{Name: "b"})
	sb.BindObject(desc, id3, &item{Name: "c"})

	// All 3 should be active
	active := sb.BoundIdentities()
	if len(active) != 3 {
		t.Fatalf("expected 3 active identities, got %d", len(active))
	}

	// Invalidate id2
	sb.InvalidateBindingsFor(id2)

	// Only 2 should remain active
	active = sb.BoundIdentities()
	if len(active) != 2 {
		t.Fatalf("expected 2 active identities after invalidation, got %d", len(active))
	}

	// Verify the remaining identities are id1 and id3
	activeSet := make(map[string]bool, len(active))
	for _, id := range active {
		activeSet[id.String()] = true
	}
	if !activeSet[id1.String()] || !activeSet[id3.String()] {
		t.Fatalf("expected id1 and id3 to remain active, got %v", activeSet)
	}
}

func TestScriptBinding_Lifecycle_ProjectionFailsAfterInvalidation(t *testing.T) {
	sb := binding.NewScriptBinding()

	type item struct{ Name string }
	desc := schema.ObjectDesc{
		Name:   "item",
		Fields: []schema.FieldDesc{{Name: "Name", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}}},
	}

	id := mustTestID(t, 3000, 5, 0, 1)
	b, _ := sb.BindObject(desc, id, &item{Name: "active"})

	// Projection should work before invalidation
	_, err := binding.ProjectView(b)
	if err != nil {
		t.Fatalf("ProjectView before invalidation: %v", err)
	}

	// Invalidate
	sb.InvalidateBindingsFor(id)

	// Projection should fail after invalidation
	_, err = binding.ProjectView(b)
	if err == nil {
		t.Fatal("expected error projecting invalidated binding")
	}
}

func TestScriptBinding_Lifecycle_PatchFailsAfterInvalidation(t *testing.T) {
	sb := binding.NewScriptBinding()

	type item struct{ Name string }
	desc := schema.ObjectDesc{
		Name:   "item",
		Fields: []schema.FieldDesc{{Name: "Name", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}}},
	}

	id := mustTestID(t, 3000, 6, 0, 1)
	b, _ := sb.BindObject(desc, id, &item{Name: "active"})

	// Patch should work before invalidation
	_, err := binding.ApplyViewPatch(b, &binding.ViewProjection{
		Schema:   desc,
		Identity: id,
		Fields:   map[string]any{"Name": "updated"},
	})
	if err != nil {
		t.Fatalf("ApplyViewPatch before invalidation: %v", err)
	}

	// Invalidate
	sb.InvalidateBindingsFor(id)

	// Patch should fail after invalidation
	_, err = binding.ApplyViewPatch(b, &binding.ViewProjection{
		Schema:   desc,
		Identity: id,
		Fields:   map[string]any{"Name": "stale"},
	})
	if err == nil {
		t.Fatal("expected error patching invalidated binding")
	}
}

// ============================================================================
// ScriptBinding concurrency smoke test
//
// Verifies that concurrent Bind/Lookup/Invalidate/Enumerate operations
// do not cause data races. Uses -race to detect issues at the Go level.
// ============================================================================

func TestScriptBinding_ConcurrentAccess_NoDataRace(t *testing.T) {
	sb := binding.NewScriptBinding()

	type item struct{ Name string }
	desc := schema.ObjectDesc{
		Name:   "item",
		Fields: []schema.FieldDesc{{Name: "Name", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}}},
	}

	const goroutines = 10
	const opsPer = 50

	var wg sync.WaitGroup
	wg.Add(goroutines * 3)

	// Concurrent writers: BindObject
	for i := 0; i < goroutines; i++ {
		go func(g int) {
			defer wg.Done()
			for j := 0; j < opsPer; j++ {
				id := mustTestID(t, uint64(g), uint16(j), 0, uint64(j))
				sb.BindObject(desc, id, &item{Name: "obj"})
			}
		}(i)
	}

	// Concurrent readers: LookupBinding
	for i := 0; i < goroutines; i++ {
		go func(g int) {
			defer wg.Done()
			for j := 0; j < opsPer; j++ {
				id := mustTestID(t, uint64(g), uint16(j), 0, uint64(j))
				sb.LookupBinding(id)
			}
		}(i)
	}

	// Concurrent invalidators
	for i := 0; i < goroutines; i++ {
		go func(g int) {
			defer wg.Done()
			for j := 0; j < opsPer; j++ {
				id := mustTestID(t, uint64(g), uint16(j), 0, uint64(j))
				sb.InvalidateBindingsFor(id)
			}
		}(i)
	}

	wg.Wait()

	// Enumerate should not panic
	_ = sb.BoundIdentities()
}
