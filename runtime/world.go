package runtime

import (
	"fmt"
	"reflect"
	"sort"
	"time"

	"github.com/qomos-w/spore/diagnostics"
	"github.com/qomos-w/spore/identity"
)

// WorldOption configures a World at creation time.
type WorldOption func(*World)

const CodeEntityError = "entity_error"

func init() {
	diagnostics.RegisterCode(diagnostics.CodeInfo{
		Code:        CodeEntityError,
		Category:    diagnostics.CategoryRuntime,
		Description: "Entity lifecycle or component access failed",
		Hint:        "检查实体是否已创建且未被销毁，以及组件是否存在",
	})
}

// WithSlot sets the runtime slot for CanonicalID generation.
// Default is 0.
func WithSlot(slot uint16) WorldOption {
	return func(w *World) { w.runtimeSlot = slot }
}

// WithIncarnation sets the incarnation for CanonicalID generation.
// Default is 0.
func WithIncarnation(inc uint16) WorldOption {
	return func(w *World) { w.incarnation = inc }
}

// World is the runtime carrier that owns entity state, component storage,
// and provides query and change tracking services.
//
// World is NOT a hosting substrate. It carries state, tracks changes,
// and serves the script/schema/binding/projection layer.
//
// Concurrency model: World is not safe for concurrent use. All World
// operations — including reads (Get, Has, Execute, Each) — must happen
// from the owning goroutine, or be externally serialized. This mirrors
// the script Runtime model (HOST-API.md): single-goroutine ownership, or
// clone per goroutine. Cross-goroutine sharing requires the caller to
// add synchronization; internal maps are never locked.
//
// Entity data is stored inside the World (not in the Entity handle),
// following Donburi's model of lightweight entity handles with world-owned
// storage. This keeps Entity comparable and copy-safe.
//
// Contract: public semantic contract — schema-aware runtime state carrier.
type World struct {
	entities    map[identity.CanonicalID]*entityState
	compIDs     map[string]uint32
	compNames   []string
	compSets    []*compSet
	nextSeq     uint64
	runtimeSlot uint16
	incarnation uint16
	startTime   uint64

	// disposal journal: a bounded ring of recently disposed CanonicalIDs,
	// observed via DisposalsSince. Derived indexes (collection facades,
	// host registries, render caches) poll it instead of maintaining their
	// own liveness sweeps. See world_disposal_test.go for the contract.
	disposeSeq uint64
	disposeLog []identity.CanonicalID

	// genSeq is the per-World creation epoch counter. Every entity state
	// created by Create/CreateWithID is stamped with the next value;
	// handles only resolve while their epoch matches (see Entity.gen).
	genSeq uint32

	// typeToName indexes the @component subset of the registered codegen
	// table (see AddRegistry): Go component type → canonical schema name.
	// It backs the descriptor-free facade (SetT/GetT/...) and LookupComponent.
	typeToName map[reflect.Type]string
}

// entityState holds the per-entity component data and change tracking.
// Components are stored by dense per-World component ID (see storage.go):
// ID-indexed data slots, sparse-set backlinks, and presence/change bitmasks.
type entityState struct {
	id         identity.CanonicalID
	gen        uint32 // creation epoch; must match the handle's gen to resolve
	disposed   bool
	components []any   // by component ID; presence tracked by the has mask
	compPos    []int32 // by component ID: position in the component's dense set, -1 = absent
	has        []uint64
	added      []uint64
	changed    []uint64
	removed    []uint64
	host       any // aggregate host struct pointer, nil for plain entities
}

// NewWorld creates a new World with the given options.
func NewWorld(opts ...WorldOption) *World {
	now := uint64(time.Now().UnixMilli())
	w := &World{
		entities:   make(map[identity.CanonicalID]*entityState),
		compIDs:    make(map[string]uint32),
		typeToName: make(map[reflect.Type]string),
		startTime:  now,
	}
	for _, opt := range opts {
		opt(w)
	}
	return w
}

// generateID creates a new CanonicalID for entity creation.
func (w *World) generateID() identity.CanonicalID {
	w.nextSeq++
	ts := uint64(time.Now().UnixMilli())
	id, err := identity.NewCanonicalID(ts, w.runtimeSlot, w.incarnation, w.nextSeq)
	if err != nil {
		// Should never happen with sequence <= 2^48-1
		panic(fmt.Sprintf("runtime: failed to generate CanonicalID: %v", err))
	}
	return id
}

// Create creates a new entity with an auto-generated CanonicalID.
func (w *World) Create() Entity {
	id := w.generateID()
	return w.CreateWithID(id)
}

// disposeLogCap bounds the disposal journal ring. Batches larger than the
// cap are routine (stage teardown, world reload), not errors: consumers
// observe them through DisposalLogResult.Truncated and fall back to a
// liveness sweep of their own tables.
const disposeLogCap = 256

// DisposalLogResult is the outcome of polling the World's disposal journal
// with DisposalsSince.
type DisposalLogResult struct {
	// Entries holds the CanonicalIDs disposed after the caller's last
	// observed sequence, in disposal order. When Truncated is true it
	// holds only the most recently retained tail.
	Entries []identity.CanonicalID
	// Truncated reports that disposals in the requested range were already
	// overwritten in the ring. The caller must fall back to scanning its
	// own derived tables with liveness checks (IsAlive/Ref.IsAlive).
	Truncated bool
	// ResumeSeq is the journal sequence the caller should pass on the next
	// poll. Always the current journal head.
	ResumeSeq uint64
}

// DisposalsSince returns the entities disposed after journal sequence seq.
// It is the pull-side counterpart of a disposal notification: derived
// indexes key by CanonicalID or Entity poll this on their access paths and
// delete the returned ids from their own tables. Correctness never depends
// on the journal — IsAlive remains authoritative; the journal only bounds
// memory growth and reconciliation cost.
//
// The journal records every final disposal: World.Dispose, and the implicit
// disposal of a live entity replaced by CreateWithID. Tick's tombstone
// sweep does not consume journal entries. A first-time observer should
// start from the current sequence (DisposalsSince(0) replays the whole
// retained history, which includes load-time CreateWithID churn).
func (w *World) DisposalsSince(seq uint64) DisposalLogResult {
	res := DisposalLogResult{ResumeSeq: w.disposeSeq}
	if seq >= w.disposeSeq {
		return res
	}
	retained := uint64(len(w.disposeLog))
	from := seq + 1
	if oldest := w.disposeSeq - retained + 1; from < oldest {
		from = oldest
		res.Truncated = true
	}
	res.Entries = make([]identity.CanonicalID, 0, w.disposeSeq-from+1)
	for n := from; n <= w.disposeSeq; n++ {
		res.Entries = append(res.Entries, w.disposeLog[(n-1)%uint64(disposeLogCap)])
	}
	return res
}

// recordDispose appends id to the disposal journal ring.
func (w *World) recordDispose(id identity.CanonicalID) {
	if len(w.disposeLog) < disposeLogCap {
		w.disposeLog = append(w.disposeLog, id)
	} else {
		w.disposeLog[int(w.disposeSeq%uint64(disposeLogCap))] = id
	}
	w.disposeSeq++
}

// CreateWithID creates a new entity with the given CanonicalID.
// Returns the Entity handle. The entity is alive and has no components.
// If an entity with the same ID already exists (including a disposed
// tombstone awaiting the Tick sweep), it is replaced. Replacing a live
// entity is recorded in the disposal journal as an implicit disposal.
// The replacement gets a fresh generation, so handles issued against the
// previous entity become stale rather than silently resolving to the new
// one.
func (w *World) CreateWithID(id identity.CanonicalID) Entity {
	if old := w.entities[id]; old != nil {
		w.removeFromAllSets(old)
		if !old.disposed {
			w.recordDispose(id)
		}
	}
	w.genSeq++
	st := &entityState{id: id, gen: w.genSeq}
	w.entities[id] = st
	return Entity{id: id, world: w, gen: st.gen}
}

// Dispose marks an entity as no longer alive. Disposed entities cannot
// have components set, accessed, or removed. Dispose is idempotent.
// The tombstone (and its sparse-set entries) is reclaimed by Tick.
// Each final disposal is recorded in the disposal journal (see
// DisposalsSince); repeated calls on an already-disposed entity are not.
func (w *World) Dispose(e Entity) {
	state := w.entityState(e)
	if state == nil || state.disposed {
		return
	}
	state.disposed = true
	w.recordDispose(state.id)
}

// IsAlive reports whether the entity exists in this world and is not disposed.
func (w *World) IsAlive(e Entity) bool {
	state := w.entityState(e)
	if state == nil {
		return false
	}
	return !state.disposed
}

// EntityCount returns the number of alive entities in the world.
func (w *World) EntityCount() int {
	count := 0
	for _, state := range w.entities {
		if !state.disposed {
			count++
		}
	}
	return count
}

// Entity returns the Entity handle for the given CanonicalID.
// Returns the entity and true if found and alive, or zero Entity and false otherwise.
func (w *World) Entity(id identity.CanonicalID) (Entity, bool) {
	state, ok := w.entities[id]
	if !ok || state.disposed {
		return Entity{}, false
	}
	return Entity{id: id, world: w, gen: state.gen}, true
}

// SetComponent sets a component on the entity, keyed by its schema name.
// If the entity already has a component with the same name, it is overwritten.
// Returns an error if the entity is disposed or does not exist.
//
// For compile-time type safety in Go host code, prefer the typed method
// World.Set with a Component[T] descriptor (runtime/typed.go). The string
// API remains the interop surface for scripts and bindings.
func (w *World) SetComponent(e Entity, schemaName string, data any) error {
	state := w.entityState(e)
	if state == nil {
		return &EntityError{EntityID: e.ID(), Err: fmt.Errorf("entity not found")}
	}
	if state.disposed {
		return &EntityError{EntityID: e.ID(), Err: fmt.Errorf("entity is disposed")}
	}

	id := w.compID(schemaName)
	existed := maskHas(state.has, id)
	w.setAdd(state, id)
	maskSet(&state.has, id)
	state.components[id] = data
	maskClear(state.removed, id) // remove from removed set if re-added

	if existed {
		maskSet(&state.changed, id)
	} else {
		maskSet(&state.added, id)
	}
	return nil
}

// GetComponent retrieves a component by schema name.
// Returns the component data and true if found, or nil and false otherwise.
//
// For compile-time type safety in Go host code, prefer the typed method
// World.Get with a Component[T] descriptor (runtime/typed.go).
func (w *World) GetComponent(e Entity, schemaName string) (any, bool) {
	state := w.entityState(e)
	if state == nil || state.disposed {
		return nil, false
	}
	return w.stateComponent(state, schemaName)
}

// RemoveComponent removes a component by schema name.
// It is safe to call on a non-existent component.
func (w *World) RemoveComponent(e Entity, schemaName string) {
	state := w.entityState(e)
	if state == nil || state.disposed {
		return
	}
	id, ok := w.compIDs[schemaName]
	if !ok || !maskHas(state.has, id) {
		return
	}
	w.setRemove(state, id)
	state.components[id] = nil
	maskClear(state.has, id)
	maskClear(state.added, id)
	maskClear(state.changed, id)
	maskSet(&state.removed, id)
}

// HasComponent reports whether the entity has a component with the given schema name.
func (w *World) HasComponent(e Entity, schemaName string) bool {
	state := w.entityState(e)
	if state == nil || state.disposed {
		return false
	}
	_, ok := w.stateComponent(state, schemaName)
	return ok
}

// ComponentNames returns the sorted list of schema names for all components
// on the entity. Returns nil if the entity is not alive.
func (w *World) ComponentNames(e Entity) []string {
	state := w.entityState(e)
	if state == nil || state.disposed {
		return nil
	}
	names := make([]string, 0, 8)
	eachMaskBit(state.has, func(id uint32) {
		names = append(names, w.compNames[id])
	})
	sort.Strings(names)
	return names
}

// ChangeSet returns the change tracking data for an entity.
// Returns an empty ChangeSet if the entity is not alive.
func (w *World) ChangeSet(e Entity) ChangeSet {
	state := w.entityState(e)
	if state == nil || state.disposed {
		return ChangeSet{}
	}
	cs := ChangeSet{}
	collect := func(m []uint64, dst *[]string) {
		if m == nil {
			return
		}
		eachMaskBit(m, func(id uint32) {
			*dst = append(*dst, w.compNames[id])
		})
	}
	collect(state.added, &cs.Added)
	collect(state.changed, &cs.Changed)
	collect(state.removed, &cs.Removed)
	sort.Strings(cs.Added)
	sort.Strings(cs.Changed)
	sort.Strings(cs.Removed)
	return cs
}

// MarkChanged marks a component as changed on the entity.
// This should be called when a bound component's field is mutated
// through a reference obtained from GetComponent.
func (w *World) MarkChanged(e Entity, schemaName string) {
	state := w.entityState(e)
	if state == nil || state.disposed {
		return
	}
	id, ok := w.compIDs[schemaName]
	if !ok || !maskHas(state.has, id) {
		return
	}
	maskSet(&state.changed, id)
}

// ClearChanges clears all change tracking for the entity,
// preparing it for the next tick.
func (w *World) ClearChanges(e Entity) {
	state := w.entityState(e)
	if state == nil {
		return
	}
	maskZero(state.added)
	maskZero(state.changed)
	maskZero(state.removed)
}

// Tick ends the current tick: it clears the change tracking of every
// entity in the World at once and sweeps disposed-entity tombstones out
// of the entity table, making the tick boundary an explicit, single-point
// contract instead of per-entity ClearChanges discipline.
//
// Hosts that consume change sets (WhenChanged queries, incremental
// transport projection) should call Tick once per frame/step, after all
// consumers have read the current tick's changes. Per-entity
// ClearChanges remains available for finer-grained flows, but Tick is
// the canonical boundary.
//
// Tick only resets change tracking and reclaims tombstones; it does not
// execute systems or schedule work — the World is a state carrier, not
// a scheduler.
func (w *World) Tick() {
	for id, state := range w.entities {
		if state.disposed {
			w.removeFromAllSets(state)
			delete(w.entities, id)
			continue
		}
		maskZero(state.added)
		maskZero(state.changed)
		maskZero(state.removed)
	}
}

// entityState returns the internal state for an entity, or nil if not
// found. A handle whose generation does not match the current state for
// its ID (the state was replaced via CreateWithID) is treated as stale.
func (w *World) entityState(e Entity) *entityState {
	if e.world != w {
		return nil
	}
	st := w.entities[e.id]
	if st == nil || st.gen != e.gen {
		return nil
	}
	return st
}

// setHost records the aggregate host object on the entity's state.
// Called by Aggregate.Register; plain entities keep host nil.
func (w *World) setHost(e Entity, host any) {
	if state := w.entityState(e); state != nil {
		state.host = host
	}
}

// Host returns the aggregate host object registered with the entity via
// Bind/Attach/Register, for use with the OOP authoring surface. Returns
// (nil, false) for plain ECS entities.
//
// Contract: public semantic contract — aggregate host retrieval.
func (w *World) Host(e Entity) (any, bool) {
	state := w.entityState(e)
	if state == nil || state.disposed || state.host == nil {
		return nil, false
	}
	return state.host, true
}

// EachHost iterates the entities matched by the query that were registered
// as aggregate hosts of type *T, invoking fn with the entity and the typed
// host. Entities without a host, or whose host is not *T, are skipped.
//
// This is the OOP iteration counterpart to Each2/Each3: systems written
// against host structs call methods on the host directly.
func (w *World) EachHost[T any](q *Query, fn func(e Entity, host *T)) {
	for _, m := range w.executeMatches(q) {
		if m.state.host == nil {
			continue
		}
		h, ok := m.state.host.(*T)
		if !ok {
			continue
		}
		fn(m.e, h)
	}
}


// EntityError carries entity identity context for runtime errors.
// Contract: public semantic contract — structured runtime error with
// entity identity context for diagnosability.
type EntityError struct {
	EntityID identity.CanonicalID
	Err      error
	Expected string
	Actual   string
}

func (e *EntityError) Error() string {
	return e.Err.Error()
}

func (e *EntityError) Unwrap() error {
	return e.Err
}

func (e *EntityError) DiagnosticExpected() string {
	if e == nil {
		return ""
	}
	if e.Expected != "" {
		return e.Expected
	}
	if ex, ok := e.Err.(interface{ DiagnosticExpected() string }); ok {
		return ex.DiagnosticExpected()
	}
	return ""
}

func (e *EntityError) DiagnosticActual() string {
	if e == nil {
		return ""
	}
	if e.Actual != "" {
		return e.Actual
	}
	if ac, ok := e.Err.(interface{ DiagnosticActual() string }); ok {
		return ac.DiagnosticActual()
	}
	return ""
}

func (e *EntityError) DiagnosticCode() string {
	if e == nil {
		return ""
	}
	if coder, ok := e.Err.(interface{ DiagnosticCode() string }); ok {
		return coder.DiagnosticCode()
	}
	return CodeEntityError
}

func (e *EntityError) DiagnosticCategory() diagnostics.Category {
	if e == nil {
		return ""
	}
	if categorizer, ok := e.Err.(interface{ DiagnosticCategory() diagnostics.Category }); ok {
		return categorizer.DiagnosticCategory()
	}
	return diagnostics.CategoryRuntime
}

func (e *EntityError) DiagnosticPath() string {
	if e == nil {
		return ""
	}
	if pather, ok := e.Err.(interface{ DiagnosticPath() string }); ok {
		return pather.DiagnosticPath()
	}
	return ""
}

func (e *EntityError) DiagnosticSpan() diagnostics.Span {
	if e == nil {
		return diagnostics.Span{}
	}
	if spanner, ok := e.Err.(interface{ DiagnosticSpan() diagnostics.Span }); ok {
		return spanner.DiagnosticSpan()
	}
	return diagnostics.Span{}
}

func (e *EntityError) DiagnosticStack() []diagnostics.Frame {
	if e == nil {
		return nil
	}
	if stacker, ok := e.Err.(interface{ DiagnosticStack() []diagnostics.Frame }); ok {
		stack := stacker.DiagnosticStack()
		if len(stack) > 0 {
			return append([]diagnostics.Frame(nil), stack...)
		}
	}
	return nil
}

func (e *EntityError) DiagnosticCause() *diagnostics.Descriptor {
	if e == nil || e.Err == nil {
		return nil
	}
	d := diagnostics.FromError(e.Err, diagnostics.Descriptor{})
	return &d
}
