// Package ecsbind exposes the runtime.World to spore-lang scripts as a
// single interface object ("World"). The facade keeps runtime storage and
// the script sealing contract untouched: it sits between the runtime
// carrier and the script runtime as a third-party binder, mirroring the
// pattern used by integration/ecs_binding_bench_test.go but productionised.
//
// The facade follows three hard rules:
//
//  1. Scripts only see values that can cross the VM boundary safely:
//     entity IDs as hex strings, component payloads as map[string]any,
//     primitives, slices, and errors. No Go pointers ever leak.
//  2. Every read goes through ecsbind.ProjectEntity (one-way projection)
//     and every write goes through ecsbind.PatchEntity (explicit opt-in +
//     automatic Mark on success). This keeps the binding layer's
//     authority model intact — scripts never reach into the runtime
//     carrier's storage directly.
//  3. Stale or unknown entity IDs surface as explicit errors, not as a
//     silent no-op. The same applies to component names that have no
//     registered schema descriptor: reads return (nil, nil) (the
//     "ok=false" idiom, scripts treat nil map as "no data"), writes
//     return an error so a script cannot silently lose a payload.
//
// The set of methods mirrors the design sketched in the wiki:
// spawn/destroy/alive/get/set/has/remove/mark/tick/count/query/changes.
// The Interface descriptor uses snake_case names; the script runtime's
// binding layer automatically maps snake_case to CamelCase
// (script/runtime.go:1287-1299) when looking up the target's exported
// Go method, so the binding's methods are exported (Spawn/Destroy/...)
// and the script-side surface stays snake_case.
package ecsbind

import (
	"bytes"
	"fmt"
	"sort"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/identity"
	"github.com/qomos-w/spore/runtime"
	"github.com/qomos-w/spore/schema"
	"github.com/qomos-w/spore/script"
)

// DefaultNamespace is the script namespace under which Bind registers the
// World facade. Scripts import it via `import World from "ecs"` (or whatever
// the host chooses to map).
const DefaultNamespace = "ecs"

// WorldBinding is the Go-side facade exposed to scripts. Construct it with
// New, optionally attach component descriptors via RegisterComponent, and
// bind the whole facade into a script.Runtime with Bind before loading any
// script source.
//
// WorldBinding is not safe for concurrent use — it inherits the World
// carrier's single-goroutine contract (see runtime.World contract notes).
// Hosts that drive it from multiple goroutines must serialise externally.
type WorldBinding struct {
	w     *runtime.World
	descs map[string]schema.ObjectDesc
}

// New constructs a WorldBinding over the given runtime World. The
// descriptor map is owned by the binding and may be mutated through
// RegisterComponent before Bind is called.
//
// Passing a nil World is a programmer error and panics: there is no
// meaningful zero-value behaviour for an entity store.
func New(w *runtime.World) *WorldBinding {
	if w == nil {
		panic("ecsbind: New requires a non-nil runtime.World")
	}
	return &WorldBinding{
		w:     w,
		descs: make(map[string]schema.ObjectDesc),
	}
}

// RegisterComponent associates a component name (the key used by
// SetComponent/GetComponent) with its schema descriptor. The descriptor
// must declare every field that the runtime struct exposes; mismatch is
// caught by binding.NewObjectBinding and surfaces as an error from Set/Get.
//
// Registering the same name twice overwrites the previous descriptor;
// this matches the runtime carrier's "one component per name" model.
// Returns the receiver for fluent chaining: `bind.New(w).RegisterComponent(...).RegisterComponent(...)`.
func (b *WorldBinding) RegisterComponent(name string, desc schema.ObjectDesc) *WorldBinding {
	if name == "" {
		panic("ecsbind: RegisterComponent requires a non-empty component name")
	}
	b.descs[name] = desc
	return b
}

// Components returns a snapshot of the registered component descriptors.
// The map is a defensive copy so callers can iterate without affecting
// the binding's internal state.
func (b *WorldBinding) Components() map[string]schema.ObjectDesc {
	out := make(map[string]schema.ObjectDesc, len(b.descs))
	for k, v := range b.descs {
		out[k] = v
	}
	return out
}

// World returns the underlying runtime.World. Exposed so hosts can drive
// the same World from Go-side systems while scripts work through the
// facade (see the ecs-script-binding-impl contract test for an example).
func (b *WorldBinding) World() *runtime.World {
	return b.w
}

// Interface returns the script-facing InterfaceDesc for the World facade.
// snake_case method names map to CamelCase in the script runtime
// (script/runtime.go:1287-1299), so scripts call Spawn/Destroy/Alive/etc.
//
// The descriptor is generated on each call; RegisterComponent changes do
// not retroactively mutate previously returned descriptors, so callers
// should keep the latest one when binding into a script Runtime.
func (b *WorldBinding) Interface() schema.InterfaceDesc {
	clone := func(m schema.MethodDesc) schema.MethodDesc {
		return schema.CloneMethodDesc(m)
	}
	return schema.CloneInterfaceDesc(schema.InterfaceDesc{
		Name: "World",
		Methods: []schema.MethodDesc{
			clone(worldMethod("spawn")),
			clone(worldMethod("destroy")),
			clone(worldMethod("alive")),
			clone(worldMethod("get")),
			clone(worldMethod("set")),
			clone(worldMethod("has")),
			clone(worldMethod("remove")),
			clone(worldMethod("mark")),
			clone(worldMethod("tick")),
			clone(worldMethod("count")),
			clone(worldMethod("query")),
			clone(worldMethod("changes")),
			clone(worldMethod("view")),
			clone(worldMethod("apply")),
		},
	})
}

// Bind registers the World facade into the given script Runtime under
// DefaultNamespace ("ecs") and the name "World". It must be called before
// LoadSource/LoadModule, matching the script binding sealing contract
// (script/runtime_test.go:492-509).
//
// On success the script can `import World from "ecs"` and call any of
// the facade methods. On failure the Runtime is left untouched (the
// underlying BindInterfaceObject rolls back on duplicate namespaces by
// removing the prior handle, so a fresh retry is safe).
func (b *WorldBinding) Bind(rt *script.Runtime) error {
	return b.BindAs(rt, DefaultNamespace, "World")
}

// BindAs is the namespace-aware variant of Bind. Use it when the host
// wants to expose the facade under a different module path, or when
// running multiple WorldBindings in the same Runtime under distinct
// aliases.
func (b *WorldBinding) BindAs(rt *script.Runtime, namespace, name string) error {
	if rt == nil {
		return fmt.Errorf("ecsbind: Bind requires a non-nil script.Runtime")
	}
	return rt.BindInterfaceObject(namespace, name, b.Interface(), b)
}

// ----------------------------------------------------------------------------
// Script-facing method implementations
// ----------------------------------------------------------------------------
//
// Methods are exported (CamelCase) because the script runtime resolves
// them through reflect.MethodByName, which only sees exported members.
// The Interface() descriptor declares them in snake_case; the script
// runtime's automatic mapping (script/runtime.go:1287-1299) bridges the
// two naming conventions. All methods take/return only FFI-safe values:
// strings (entity IDs), map[string]any (component payloads), primitives,
// slices, and an optional trailing error. The script runtime
// (script/runtime.go:1300) surfaces only the first return value to script
// code and converts a non-nil error into a script runtime error.
//

// Spawn creates a new entity and returns its canonical ID hex form.
// Matches runtime.World.Create + Entity.ID().String().
func (b *WorldBinding) Spawn() string {
	e := b.w.Create()
	return e.ID().String()
}

// Destroy marks the entity as disposed. Stale or unknown IDs are
// reported as errors — Dispose itself is idempotent, but the script
// surface should not silently swallow a typo.
func (b *WorldBinding) Destroy(id string) error {
	e, ok := b.resolveEntity(id)
	if !ok {
		return b.errStaleEntity(id)
	}
	b.w.Dispose(e)
	return nil
}

// Alive reports whether the entity is currently alive in the World.
// Returns false (not an error) for unknown / disposed IDs.
func (b *WorldBinding) Alive(id string) bool {
	e, ok := b.resolveEntity(id)
	if !ok {
		return false
	}
	return b.w.IsAlive(e)
}

// Get projects the named component into a map[string]any snapshot via
// the binding layer. Returns (nil, nil) — the "ok=false" idiom — when
// the component name has no schema descriptor registered, the entity
// has no such component, or the entity is unknown. Returns
// (nil, err) only for projection/validation failures (which usually
// mean the script fed bad data and the host should surface it).
//
// A nil map with nil error is the canonical "no data" signal — scripts
// treat it the same way they treat a missing map key.
func (b *WorldBinding) Get(id, comp string) (map[string]any, error) {
	desc, registered := b.descs[comp]
	if !registered {
		return nil, nil
	}
	e, ok := b.resolveEntity(id)
	if !ok {
		// Stale IDs on read: return "no data" rather than error. This
		// keeps the read path symmetric with the unregistered-component
		// case ("no data") and avoids forcing every read site into a
		// defensive error check. Destroy is the write-side tool that
		// surfaces stale IDs explicitly.
		return nil, nil
	}
	view, err := ProjectEntity(b.w, e, comp, desc)
	if err != nil {
		return nil, err
	}
	return view.Fields, nil
}

// Set writes the field map back to the entity's component via
// PatchEntity. On success the component is automatically MarkChanged
// (ecsbind.PatchEntity contract, projection.go), and the mutation
// count is returned so it can be used as an "applied fields" indicator
// in scripts.
//
// Returns an error for unknown component names (no descriptor), stale
// entity IDs, or patch validation failures — none of these may
// silently drop the payload.
func (b *WorldBinding) Set(id, comp string, fields map[string]any) (int, error) {
	desc, registered := b.descs[comp]
	if !registered {
		return 0, b.errUnknownComponent(comp)
	}
	e, ok := b.resolveEntity(id)
	if !ok {
		return 0, b.errStaleEntity(id)
	}
	muts, err := PatchEntity(b.w, e, comp, desc, &binding.ViewProjection{
		Schema: desc,
		Fields: fields,
	})
	if err != nil {
		return 0, err
	}
	return len(muts), nil
}

// Has reports whether the entity has the named component. Returns false
// for unknown entity IDs or components with no descriptor — the facade
// stays conservative so a script cannot observe phantom state.
func (b *WorldBinding) Has(id, comp string) bool {
	if _, registered := b.descs[comp]; !registered {
		return false
	}
	e, ok := b.resolveEntity(id)
	if !ok {
		return false
	}
	return b.w.HasComponent(e, comp)
}

// Remove removes the named component from the entity. No-op for unknown
// IDs / components — the script surface prefers "safe no-op" over
// errors for removals, matching runtime.RemoveComponent's semantics.
func (b *WorldBinding) Remove(id, comp string) {
	if _, registered := b.descs[comp]; !registered {
		return
	}
	e, ok := b.resolveEntity(id)
	if !ok {
		return
	}
	b.w.RemoveComponent(e, comp)
}

// Mark marks the named component as changed, for use after the script
// obtained a component snapshot through get and then mutated the
// returned map in-place. The runtime's MarkChanged is itself a no-op
// when the component is absent on the entity, so no extra guard needed.
func (b *WorldBinding) Mark(id, comp string) {
	if _, registered := b.descs[comp]; !registered {
		return
	}
	e, ok := b.resolveEntity(id)
	if !ok {
		return
	}
	b.w.MarkChanged(e, comp)
}

// Tick is the per-frame tick boundary: it clears added/changed/removed
// bookkeeping across every entity in the World. See runtime.Tick's
// contract notes (world.go:282-302).
func (b *WorldBinding) Tick() {
	b.w.Tick()
}

// Count returns the number of alive entities.
func (b *WorldBinding) Count() int {
	return b.w.EntityCount()
}

// Query executes a component-set filter against the World and returns
// the matching entity IDs as hex strings. The six optional filters
// mirror runtime.Query (Has/HasNone/HasEither/WhenAdded/WhenChanged/
// WhenRemoved); any of them may be nil. An empty call (all filters
// nil) returns every alive entity, matching runtime.NewQuery().
func (b *WorldBinding) Query(has, none, either, added, changed, removed []string) []string {
	q := runtime.NewQuery()
	if len(has) > 0 {
		q = q.Has(has...)
	}
	if len(none) > 0 {
		q = q.HasNone(none...)
	}
	if len(either) > 0 {
		q = q.HasEither(either...)
	}
	if len(added) > 0 {
		q = q.WhenAdded(added...)
	}
	if len(changed) > 0 {
		q = q.WhenChanged(changed...)
	}
	if len(removed) > 0 {
		q = q.WhenRemoved(removed...)
	}
	entities := b.w.Execute(q)
	ids := make([]string, len(entities))
	for i, e := range entities {
		ids[i] = e.ID().String()
	}
	return ids
}

// Changes returns the current tick's added/changed/removed component
// names for the given entity, wrapped in a map with three keys so the
// single VM return value can carry all three buckets:
//
//	{ "added": [...], "changed": [...], "removed": [...] }
//
// Stale or unknown IDs surface as an error. Empty buckets are returned
// as empty (non-nil) slices so scripts can iterate without nil checks.
func (b *WorldBinding) Changes(id string) (map[string]any, error) {
	e, ok := b.resolveEntity(id)
	if !ok {
		return nil, b.errStaleEntity(id)
	}
	cs := b.w.ChangeSet(e)
	return map[string]any{
		"added":   cloneStrings(cs.Added),
		"changed": cloneStrings(cs.Changed),
		"removed": cloneStrings(cs.Removed),
	}, nil
}

// View executes a single component-set + when-changed query against the
// World and returns every matched entity's id together with one aligned
// component-array per component name, in a single boundary crossing.
//
// Return shape (flat map shell so the VM does not have to convert a
// nested `map[string][]map[string]any` directly — see
// verify-vm-batch-encoding: the nested form IS safe on the return path,
// but a flat shell is the chosen contract for clarity and aligns with
// how scripts naturally destructure `r.ids` / `r.data[compName][i]`):
//
//	{
//	  "ids":  []string,                    // entity ids, sorted
//	  "data": map[string][]map[string]any, // comp name -> aligned with ids
//	}
//
// The query is "Has(has...) AND WhenChanged(changed...)" — the same
// semantics as runtime.NewQuery().Has(...).WhenChanged(...). When
// `changed` is empty the change filter is skipped (every Has-match is
// returned); when `has` is empty no components are projected (data is
// an empty map) but the ids spine is still computed.
//
// ProjectEntity runs once per (entity × component), no per-entity
// re-query: see ViewBody below.
func (b *WorldBinding) View(has []string, changed []string) (map[string]any, error) {
	ids, perComp, err := b.ViewBody(has, changed)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"ids":  ids,
		"data": perComp,
	}, nil
}

// ViewBody is the Go-side variant of View that returns the ids slice
// and the per-component parallel arrays directly, without the flat
// `{"ids","data"}` shell. Useful for Go hosts that want to consume the
// batch view without the FFI-marshalling envelope (e.g. integration
// benchmarks and tests).
//
// Returns a defensive copy of the id list (sorted) and a fresh
// per-component map whose arrays are aligned with ids; mutation of the
// returned values does not affect the World.
func (b *WorldBinding) ViewBody(has []string, changed []string) ([]string, map[string][]map[string]any, error) {
	q := runtime.NewQuery()
	if len(has) > 0 {
		q = q.Has(has...)
	}
	if len(changed) > 0 {
		q = q.WhenChanged(changed...)
	}
	entities := b.w.Execute(q)
	if len(entities) == 0 {
		return []string{}, emptyPerComponentMap(has), nil
	}

	// Sort entities by id so we can build a sorted-ids spine and
	// per-component arrays that index-by-id-position. We collect
	// (id, entity) pairs to keep the link after sorting.
	type idEntity struct {
		cid identity.CanonicalID
		e   runtime.Entity
	}
	pairs := make([]idEntity, len(entities))
	for i, e := range entities {
		pairs[i] = idEntity{cid: e.ID(), e: e}
	}
	sort.Slice(pairs, func(i, j int) bool {
		return bytes.Compare(pairs[i].cid[:], pairs[j].cid[:]) < 0
	})

	ids := make([]string, len(pairs))
	for i, p := range pairs {
		ids[i] = p.cid.String()
	}

	// Project each (entity × component in `has`) once. The slice at
	// data[comp][i] aligns with ids[i]; a ProjectEntity failure for a
	// query-guaranteed-present component (e.g. a race with a concurrent
	// remove) is recorded as a nil entry so callers see consistent
	// alignment.
	perComp := make(map[string][]map[string]any, len(has))
	for _, comp := range has {
		desc, registered := b.descs[comp]
		arr := make([]map[string]any, len(ids))
		if !registered {
			// No schema descriptor → cannot project; leave nil entries.
			perComp[comp] = arr
			continue
		}
		for i, p := range pairs {
			view, err := ProjectEntity(b.w, p.e, comp, desc)
			if err != nil {
				arr[i] = nil
				continue
			}
			arr[i] = view.Fields
		}
		perComp[comp] = arr
	}
	return ids, perComp, nil
}

// emptyPerComponentMap returns the data map View would produce when
// the query matched no entities: ids=[] and per-component arrays of
// length 0 (not nil), so scripts can iterate without nil checks.
func emptyPerComponentMap(has []string) map[string][]map[string]any {
	out := make(map[string][]map[string]any, len(has))
	for _, comp := range has {
		out[comp] = []map[string]any{}
	}
	return out
}

// Apply patches the named component on every id in `ids` from the
// matching field map in `fields`. Returns the number of entities that
// were successfully patched; on the first error the loop stops and the
// error is returned alongside the partial count.
//
// Contract:
//   - Unknown component name → (0, error). The payload must not be
//     silently dropped; the facade surfaces it as the same "no
//     registered schema descriptor" error used by Set.
//   - Stale entity IDs → first stale id stops the loop with an error;
//     the returned count is the number of entities patched *before*
//     the failure, so a script can resume by slicing the input.
//   - PatchEntity validates the field map against the schema; a
//     per-entity validation error also short-circuits the batch.
//
// The arg signature `[]map[string]any` (slice of field maps) is
// REQUIRED: a nested `map[string][]map[string]any` parameter would
// panic in the VM→Go direction (binding/adapter.go slice branch
// recurses through elements but does not project keys for typed maps).
// See verify-vm-batch-encoding for the VM-side evidence.
//
// Each successful PatchEntity auto-Marks the component as changed
// (ecsbind.PatchEntity contract, projection.go), so the caller
// does not need a follow-up Mark() call.
func (b *WorldBinding) Apply(comp string, ids []string, fields []map[string]any) (int, error) {
	desc, registered := b.descs[comp]
	if !registered {
		return 0, b.errUnknownComponent(comp)
	}
	n := len(ids)
	if n == 0 {
		return 0, nil
	}
	if len(fields) != n {
		return 0, fmt.Errorf("ecsbind: apply(comp=%q): ids/fields length mismatch (%d vs %d)", comp, n, len(fields))
	}
	for i := 0; i < n; i++ {
		e, ok := b.resolveEntity(ids[i])
		if !ok {
			return i, b.errStaleEntity(ids[i])
		}
		view := fields[i]
		if view == nil {
			// Treat nil entry as "skip but keep alignment"; this
			// matches the View projection's nil-for-missing behaviour
			// and lets scripts overwrite a subset of entities.
			continue
		}
		if _, err := PatchEntity(b.w, e, comp, desc, &binding.ViewProjection{
			Schema: desc,
			Fields: view,
		}); err != nil {
			return i, err
		}
	}
	return n, nil
}

// ----------------------------------------------------------------------------
// Internals
// ----------------------------------------------------------------------------

// resolveEntity turns a script-side hex ID back into a runtime Entity
// handle. Returns (zero, false) for parse errors, unknown IDs, or
// disposed entities — the caller decides whether to surface that as an
// error (Destroy/Set/Changes) or as a "no data" / false return
// (Alive/Has/Remove/Mark/Get).
func (b *WorldBinding) resolveEntity(id string) (runtime.Entity, bool) {
	if id == "" {
		return runtime.Entity{}, false
	}
	cid, err := identity.ParseCanonicalID(id)
	if err != nil {
		return runtime.Entity{}, false
	}
	e, ok := b.w.Entity(cid)
	if !ok {
		return runtime.Entity{}, false
	}
	return e, true
}

// errStaleEntity is the standard error for any operation that requires
// a live entity but received an unknown / disposed / malformed ID.
func (b *WorldBinding) errStaleEntity(id string) error {
	return fmt.Errorf("ecsbind: entity %q is not alive in this world", id)
}

// errUnknownComponent is the standard error for Set when the component
// name has no registered descriptor. Get treats the same situation as
// "not present" (returns nil map, nil error) instead of an error; this
// preserves the "ok=false" idiom and avoids forcing every read site
// into an explicit error check.
func (b *WorldBinding) errUnknownComponent(comp string) error {
	return fmt.Errorf("ecsbind: component %q has no registered schema descriptor", comp)
}

// worldMethod returns a single MethodDesc entry. Centralising the
// descriptor construction keeps Interface() readable and lets each
// method's parameter/return types live next to its implementation.
func worldMethod(name string) schema.MethodDesc {
	strArr := schema.TypeDesc{
		Kind:    schema.TypeKindArray,
		Name:    "array",
		Element: &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"},
	}
	strAnyMap := schema.TypeDesc{
		Kind:  schema.TypeKindMap,
		Name:  "map",
		Key:   &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"},
		Value: &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "any"},
	}
	switch name {
	case "spawn":
		return schema.MethodDesc{
			Name:    "spawn",
			Returns: []schema.TypeDesc{{Kind: schema.TypeKindScalar, Name: "string"}},
		}
	case "destroy":
		return schema.MethodDesc{
			Name: "destroy",
			Parameters: []schema.ParameterDesc{
				{Name: "id", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
			},
		}
	case "alive":
		return schema.MethodDesc{
			Name: "alive",
			Parameters: []schema.ParameterDesc{
				{Name: "id", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
			},
			Returns: []schema.TypeDesc{{Kind: schema.TypeKindScalar, Name: "bool"}},
		}
	case "get":
		return schema.MethodDesc{
			Name: "get",
			Parameters: []schema.ParameterDesc{
				{Name: "id", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
				{Name: "comp", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
			},
			Returns: []schema.TypeDesc{strAnyMap},
		}
	case "set":
		return schema.MethodDesc{
			Name: "set",
			Parameters: []schema.ParameterDesc{
				{Name: "id", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
				{Name: "comp", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
				{Name: "fields", Type: strAnyMap},
			},
			Returns: []schema.TypeDesc{{Kind: schema.TypeKindScalar, Name: "int"}},
		}
	case "has":
		return schema.MethodDesc{
			Name: "has",
			Parameters: []schema.ParameterDesc{
				{Name: "id", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
				{Name: "comp", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
			},
			Returns: []schema.TypeDesc{{Kind: schema.TypeKindScalar, Name: "bool"}},
		}
	case "remove":
		return schema.MethodDesc{
			Name: "remove",
			Parameters: []schema.ParameterDesc{
				{Name: "id", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
				{Name: "comp", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
			},
		}
	case "mark":
		return schema.MethodDesc{
			Name: "mark",
			Parameters: []schema.ParameterDesc{
				{Name: "id", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
				{Name: "comp", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
			},
		}
	case "tick":
		return schema.MethodDesc{Name: "tick"}
	case "count":
		return schema.MethodDesc{
			Name:    "count",
			Returns: []schema.TypeDesc{{Kind: schema.TypeKindScalar, Name: "int"}},
		}
	case "query":
		return schema.MethodDesc{
			Name: "query",
			Parameters: []schema.ParameterDesc{
				{Name: "has", Type: strArr},
				{Name: "none", Type: strArr},
				{Name: "either", Type: strArr},
				{Name: "added", Type: strArr},
				{Name: "changed", Type: strArr},
				{Name: "removed", Type: strArr},
			},
			Returns: []schema.TypeDesc{strArr},
		}
	case "changes":
		return schema.MethodDesc{
			Name: "changes",
			Parameters: []schema.ParameterDesc{
				{Name: "id", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
			},
			Returns: []schema.TypeDesc{{
				Kind:  schema.TypeKindMap,
				Name:  "map",
				Key:   &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"},
				Value: &strArr,
			}},
		}
	case "view":
		// View: Has(has...) AND WhenChanged(changed...) → single-shot batch read.
		// Return shape: {"ids": []string, "data": map<string, array<map<string, any>>>}.
		// Declared as a plain map<string, any> at the script descriptor layer;
		// the flat shell + nested `data` map (each value an aligned array of
		// field maps) round-trips through the VM because map[string][]map
		// is supported on the return path (see verify-vm-batch-encoding).
		return schema.MethodDesc{
			Name: "view",
			Parameters: []schema.ParameterDesc{
				{Name: "has", Type: strArr},
				{Name: "changed", Type: strArr},
			},
			Returns: []schema.TypeDesc{{
				Kind: schema.TypeKindMap,
				Name: "map",
				Key:  &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"},
				Value: &schema.TypeDesc{
					Kind:  schema.TypeKindScalar,
					Name:  "any",
				},
			}},
		}
	case "apply":
		// Apply: batch patch + Mark. ids and fields are kept as parallel
		// slices (NOT a nested map of ids → field maps) because typed
		// map<string, slice<map<string, any>>> parameter values panic
		// during VM→Go conversion (binding/adapter.go slice branch).
		return schema.MethodDesc{
			Name: "apply",
			Parameters: []schema.ParameterDesc{
				{Name: "comp", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
				{Name: "ids", Type: strArr},
				{Name: "fields", Type: schema.TypeDesc{
					Kind: schema.TypeKindArray,
					Name: "array",
					Element: &schema.TypeDesc{
						Kind:  schema.TypeKindMap,
						Name:  "map",
						Key:   &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"},
						Value: &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "any"},
					},
				}},
			},
			Returns: []schema.TypeDesc{{Kind: schema.TypeKindScalar, Name: "int"}},
		}
	default:
		return schema.MethodDesc{Name: name}
	}
}

// cloneStrings returns a fresh copy of the input slice, sorted.
// ChangeSet already sorts internally, but cloneStrings is defensive
// against future runtime changes and gives callers an isolated slice
// they can mutate without leaking into the World.
func cloneStrings(src []string) []string {
	if len(src) == 0 {
		return []string{}
	}
	out := make([]string, len(src))
	copy(out, src)
	sort.Strings(out)
	return out
}