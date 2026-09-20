// Host entity binding idioms for the ecsbind package.
//
// Implements the four host-side contract idioms listed in the
// host-entity-binding-idioms card:
//
//  1. Collection facade (HostFleet): an id-addressed collection over a
//     concrete aggregate host struct (UnitHost). Methods do direct field
//     writes on the host struct + u.Mark(...) so the World's change set
//     sees the mutation. Stale/disposed ids surface as errors.
//
//  2. Per-instance proxy factory (HostFleet.SpawnProxy): registers the
//     newly-created host as a new instance of the pre-bound Unit interface
//     and returns it. The script runtime's HostInterfaceHandleForValue
//     (script/runtime.go:1274) automatically converts the returned Go
//     target into a live proxy object the script can call methods on.
//
//  3. Stale-handle error convention: every collection method and every
//     per-instance method checks u.IsAlive() (Ref.IsAlive, runtime/ref.go:45)
//     and returns ErrStaleEntity for stale/unknown ids. The script runtime
//     reports the error as a structured runtime error with the stable
//     native_call_failed code (script/runtime_hostiface.go), which is the
//     documented behaviour for error-returning host methods
//     (ecsbind/ecsbind_test.go:TestScript_StaleIDPropagatesError).
//
//  4. Interop contract: the same World is shared with the World facade
//     (ecsbind.WorldBinding). Host-registered entities appear in
//     World.query(...) together with plain ECS entities, and Tick /
//     WhenChanged observe the same bookkeeping across both. Plain-entity
//     writes through WorldBinding.Set show up through the host struct's
//     field aliasing (runtime/ref.go:autoComponentFields → zero-copy) and
//     vice-versa.
//
// The unit registry is built via runtime.SchemaTable (runtime/type_registry.go)
// so that runtime.Bind(w, &UnitHost{}).Register() auto-collects the
// Position / Health fields (runtime/ref.go:autoComponentFields). Codegen
// is not required — HostRegistry() is the hand-rolled SchemaTable.
//
// All host-interface methods are exposed under snake_case names that the
// script runtime maps to CamelCase Go methods
// (script/runtime.go:1287-1299 / exportedMethodName). No reflect
// duplication is needed at the call site.
package ecsbind

import (
	"fmt"
	"reflect"

	"github.com/qomos-w/spore/runtime"
	"github.com/qomos-w/spore/schema"
	"github.com/qomos-w/spore/script"
)

// UnitPosition is the Position component for UnitHost. Exported so the
// aggregate auto-collection path (runtime/ref.go:autoComponentFields)
// recognizes the corresponding host field by Go type and resolves it
// against the World's nameForComponentType lookup.
type UnitPosition struct {
	X, Y float64
}

// UnitHealth is the Health component for UnitHost.
type UnitHealth struct {
	Value int
}

// PositionC / HealthC are the typed Component[T] descriptors callers use
// to drive runtime.Ref.Mark / runtime.Ref.Set / runtime.Ref.Get without
// repeating the schema name.
var (
	PositionC = runtime.NewComponent[UnitPosition]("host.Position")
	HealthC   = runtime.NewComponent[UnitHealth]("host.Health")
)

// hostRegistryTable is a minimal runtime.SchemaTable built by hand. The
// runtime's own testTable (runtime/type_registry_test.go) is package
// private; this table is the ecsbind-side equivalent. Schema IDs are
// chosen to be unambiguous relative to runtime-internal test tables.
type hostRegistryTable struct {
	types map[uint64]reflect.Type
	names map[uint64]string
	comps []uint64
}

func (t hostRegistryTable) SchemaTypes() map[uint64]reflect.Type { return t.types }
func (t hostRegistryTable) SchemaIDs() map[uint64]string         { return t.names }
func (t hostRegistryTable) ComponentIDs() []uint64               { return t.comps }

// HostRegistry returns a SchemaTable that registers UnitPosition and
// UnitHealth as components. Hosts call w.AddRegistry(HostRegistry())
// before Bind or Register so the aggregate auto-collection path sees the
// fields on UnitHost.
func HostRegistry() runtime.SchemaTable {
	return hostRegistryTable{
		types: map[uint64]reflect.Type{
			700: reflect.TypeOf(UnitPosition{}),
			701: reflect.TypeOf(UnitHealth{}),
		},
		names: map[uint64]string{
			700: PositionC.Name(),
			701: HealthC.Name(),
		},
		comps: []uint64{700, 701},
	}
}

// UnitHost is the aggregate host struct. It embeds runtime.Ref and the
// component value fields so runtime.Bind(w, &UnitHost{}).Register()
// auto-collects the fields via the World's AddRegistry table. The host
// owns the memory for the component values — the World stores pointers
// into the struct, which is why UnitHost must not be copied after
// Register (runtime/ref.go:24-26).
type UnitHost struct {
	runtime.Ref
	Position UnitPosition
	Health   UnitHealth
}

// Move advances the unit's X by dx and marks Position as modified.
// Returns ErrStaleEntity for stale/unknown handles (Ref not registered,
// entity disposed, or nil receiver). Direct field write + Mark is the
// idiomatic "host owns the component value" path
// (runtime/ref.go:Mark → MarkChanged).
func (u *UnitHost) Move(dx float64) error {
	if u == nil || !u.IsAlive() {
		return ErrStaleEntity()
	}
	u.Position.X += dx
	u.Mark(PositionC)
	return nil
}

// Hp returns the unit's current health value. Stale handles surface as
// (0, ErrStaleEntity) — the same shape as the collection facade's Hp.
func (u *UnitHost) Hp() (int, error) {
	if u == nil || !u.IsAlive() {
		return 0, ErrStaleEntity()
	}
	return u.Health.Value, nil
}

// Dispose tears the entity down. Safe to call repeatedly and on the zero
// value: Ref.Dispose is itself idempotent and zero-value safe
// (runtime/ref.go:52-59).
func (u *UnitHost) Dispose() error {
	if u == nil || !u.IsAlive() {
		return ErrStaleEntity()
	}
	u.Ref.Dispose()
	return nil
}

// ErrStaleEntity is the canonical stale-handle error for host entity
// operations. The script runtime reports non-nil errors returned from
// host-interface methods as a structured runtime error carrying the stable
// native_call_failed code (script/runtime_hostiface.go), so script callers
// see the same ErrStaleEntity signal in Result.Error.Diagnostic rather than
// as a panic. Go callers can errors.Is against this sentinel.
//
// ErrStaleEntity is implemented as a function (rather than a package
// variable) so callers cannot mutate the shared sentinel across
// goroutines; each call returns a fresh error with the canonical
// "stale or unknown id" message.
func ErrStaleEntity() error {
	return fmt.Errorf("host entity: stale or unknown id")
}

// ----------------------------------------------------------------------------
// HostFleet: id-addressed collection facade over UnitHost.
// ----------------------------------------------------------------------------

// HostFleet is the collection facade over a concrete aggregate host
// struct. It implements the script-facing "fleet" of units: id-addressed
// move / hp / destroy, count, and the per-instance proxy factory.
//
// HostFleet is the collection's Go-side root. The script runtime sees a
// single snake_case-named host interface; method names map to CamelCase
// Go methods on *HostFleet via the runtime's automatic mapping
// (script/runtime.go:1287-1299).
//
// HostFleet inherits the World's single-goroutine contract. Hosts that
// drive it from multiple goroutines must serialise externally.
type HostFleet struct {
	w     *runtime.World
	units map[string]*UnitHost
	rt    *script.Runtime
	ns    string // script namespace (default "host")

	// disposeSeen is the fleet's disposal-journal watermark (reap).
	disposeSeen uint64
}

// NewHostFleet creates a fleet over the given World. The fleet does not
// own the World; the caller continues to drive Tick / Dispose / Query on
// it through the WorldBinding facade.
func NewHostFleet(w *runtime.World) *HostFleet {
	return &HostFleet{
		w:     w,
		units: make(map[string]*UnitHost),
		ns:    "host",
	}
}

// Namespace returns the script namespace under which Bind registers the
// fleet facade and the per-instance Unit interface. Defaults to "host".
func (f *HostFleet) Namespace() string { return f.ns }

// SetNamespace overrides the script namespace before Bind. Must not be
// called after Bind.
func (f *HostFleet) SetNamespace(ns string) {
	if ns == "" {
		return
	}
	f.ns = ns
}

// World exposes the underlying runtime.World. Used by tests that want to
// interop with WorldBinding or inspect entity storage directly.
func (f *HostFleet) World() *runtime.World { return f.w }

// Runtime returns the script.Runtime the fleet is bound to, or nil if
// Bind has not been called.
func (f *HostFleet) Runtime() *script.Runtime { return f.rt }

// Bind registers the fleet facade and the per-instance Unit interface
// into the given script.Runtime. Must be called BEFORE
// LoadSource / LoadModule.
//
//   - Fleet exposes spawn / spawn_proxy / move / hp / destroy / count.
//     snake_case names map to CamelCase Go methods on *HostFleet.
//   - Unit exposes move / hp / dispose. snake_case names map to
//     CamelCase Go methods on *UnitHost.
//
// A zero-value *UnitHost is used as the seed target for the Unit class so
// the proxy class is materialised pre-LoadSource; the seed is never
// registered with the World, so any accidental script-side use of it
// hits the stale-handle error path.
//
// Bind returns an error if the runtime is nil, the namespace is empty,
// or the underlying BindInterfaceObject fails.
func (f *HostFleet) Bind(rt *script.Runtime) error {
	if rt == nil {
		return fmt.Errorf("ecsbind: HostFleet.Bind requires a non-nil script.Runtime")
	}
	if f.ns == "" {
		return fmt.Errorf("ecsbind: HostFleet namespace cannot be empty")
	}
	// Fleet facade: a single host-bound interface object exposing the
	// collection methods.
	if err := rt.BindInterfaceObject(f.ns, "Fleet", fleetInterfaceDesc(), f); err != nil {
		return fmt.Errorf("ecsbind: bind Fleet interface: %w", err)
	}
	// Per-instance Unit class: seed with a zero host so the proxy
	// class is allocated. Subsequent SpawnProxy calls add real
	// instances via RegisterHostInterfaceInstance.
	if err := rt.BindInterfaceObject(f.ns, "Unit", unitInterfaceDesc(), &UnitHost{}); err != nil {
		return fmt.Errorf("ecsbind: bind Unit interface: %w", err)
	}
	f.rt = rt
	return nil
}

// Spawn creates a new host at position (x, 0) with HP 100 and registers
// it on the World. Returns the canonical id string. The Go caller can
// keep using *HostFleet methods against this id; scripts that need a
// live proxy for the new entity must use SpawnProxy instead.
func (f *HostFleet) Spawn(x float64) (string, error) {
	u, err := f.spawnHost(x)
	if err != nil {
		return "", err
	}
	return u.Ref.Entity.ID().String(), nil
}

// SpawnProxy creates a new host, registers it on the World AND on the
// script Runtime under the Unit interface, then returns the live
// *UnitHost. The script runtime's HostInterfaceHandleForValue converts
// the returned Go target into a Unit proxy object the script can call
// methods on (internal/script/bytecode/value_codec.go:anyToVMValueAtPath).
//
// This is the "factory / per-instance proxy" idiom: scripts see the
// proxy, not the Go pointer. The proxy methods (Move / Hp / Dispose on
// *UnitHost) do the same direct field writes + Mark as the collection
// facade methods, so both surfaces read and write the same World
// storage.
//
// SpawnProxy returns an error if the fleet has not been Bound, the World
// is missing, or registration fails.
func (f *HostFleet) SpawnProxy(x float64) (*UnitHost, error) {
	if f.rt == nil {
		return nil, fmt.Errorf("ecsbind: HostFleet.SpawnProxy requires Bind to have been called")
	}
	u, err := f.spawnHost(x)
	if err != nil {
		return nil, err
	}
	if err := f.rt.RegisterHostInterfaceInstance(f.ns, "Unit", u); err != nil {
		// Roll back: dispose the host we just created so the World
		// doesn't leak a half-built aggregate.
		u.Ref.Dispose()
		delete(f.units, u.Ref.Entity.ID().String())
		return nil, fmt.Errorf("ecsbind: register Unit instance: %w", err)
	}
	return u, nil
}

// spawnHost creates the host struct and registers it on the World. The
// fleet map is updated under the canonical id. Returns the host so the
// caller can decide whether to also register a script proxy.
func (f *HostFleet) spawnHost(x float64) (*UnitHost, error) {
	if f.w == nil {
		return nil, fmt.Errorf("ecsbind: fleet has no world")
	}
	u := &UnitHost{
		Position: UnitPosition{X: x, Y: 0},
		Health:   UnitHealth{Value: 100},
	}
	if _, err := runtime.Bind(f.w, u).Register(); err != nil {
		return nil, fmt.Errorf("ecsbind: register unit: %w", err)
	}
	f.units[u.Ref.Entity.ID().String()] = u
	return u, nil
}

// reap drops fleet entries for entities disposed outside the fleet API —
// World.Dispose called directly, or a live entity replaced by
// CreateWithID. It polls the World's disposal journal; when the fleet
// fell behind the journal ring (Truncated) it falls back to a liveness
// sweep of its own map. Correctness never depends on reaping: call sites
// still check IsAlive; reap only reclaims the map entries.
func (f *HostFleet) reap() {
	if f.w == nil {
		return
	}
	res := f.w.DisposalsSince(f.disposeSeen)
	if res.Truncated {
		for id, u := range f.units {
			if !u.IsAlive() {
				delete(f.units, id)
			}
		}
	} else {
		for _, id := range res.Entries {
			delete(f.units, id.String())
		}
	}
	f.disposeSeen = res.ResumeSeq
}

// Move resolves the host by id, advances its X by dx, and marks Position
// as changed on the World (runtime.MarkChanged). Stale or unknown ids
// return ErrStaleEntity().
func (f *HostFleet) Move(id string, dx float64) error {
	f.reap()
	u, ok := f.units[id]
	if !ok || !u.IsAlive() {
		return ErrStaleEntity()
	}
	u.Position.X += dx
	u.Mark(PositionC)
	return nil
}

// Hp returns the unit's current health value. Stale or unknown ids
// return (0, ErrStaleEntity).
func (f *HostFleet) Hp(id string) (int, error) {
	f.reap()
	u, ok := f.units[id]
	if !ok || !u.IsAlive() {
		return 0, ErrStaleEntity()
	}
	return u.Health.Value, nil
}

// Dispose disposes the entity and drops it from the fleet map. Idempotent
// (Ref.Dispose is itself idempotent). Stale or unknown ids return
// ErrStaleEntity so the script surface can surface the error.
//
// HostFleet exposes both Dispose (Go-idiomatic name) and Destroy
// (script-idiomatic snake_case mapping) so scripts can call
// `Fleet.destroy(id)` from spore-lang without naming friction.
func (f *HostFleet) Dispose(id string) error {
	f.reap()
	u, ok := f.units[id]
	if !ok || !u.IsAlive() {
		return ErrStaleEntity()
	}
	u.Ref.Dispose()
	delete(f.units, id)
	return nil
}

// Destroy is the snake_case-script mapping target for Fleet.destroy.
// The binding layer maps "destroy" → "Destroy" automatically
// (script/runtime.go:1287-1299).
func (f *HostFleet) Destroy(id string) error { return f.Dispose(id) }

// Count returns the number of alive hosts the fleet knows about. The
// value matches the World's EntityCount for the host subset, since every
// fleet-tracked entity is registered via runtime.Bind.
func (f *HostFleet) Count() int {
	f.reap()
	n := 0
	for _, u := range f.units {
		if u.IsAlive() {
			n++
		}
	}
	return n
}

// LookUp exposes the host map to other Go-side systems that want to walk
// the fleet. Returns (host, true) when the id is alive, (nil, false)
// otherwise.
func (f *HostFleet) LookUp(id string) (*UnitHost, bool) {
	f.reap()
	u, ok := f.units[id]
	if !ok {
		return nil, false
	}
	if !u.IsAlive() {
		return nil, false
	}
	return u, true
}

// ----------------------------------------------------------------------------
// Interface descriptors
// ----------------------------------------------------------------------------

// fleetInterfaceDesc returns the script-facing interface for HostFleet.
// Method names are snake_case; the script runtime maps them to CamelCase
// (script/runtime.go:1287-1299) when resolving Go methods on *HostFleet.
//
// Returns are declared per-method:
//   - spawn returns string (id).
//   - spawn_proxy returns the live Unit proxy; the script runtime
//     converts the returned *UnitHost Go value via HostInterfaceHandleForValue
//     (internal/script/bytecode/value_codec.go:anyToVMValueAtPath).
//   - move / destroy are void on the script side. The Go methods return
//     error so stale handles surface as ErrStaleEntity, which the
//     script runtime reports as a structured native_call_failed runtime
//     error (script/runtime_hostiface.go).
//   - hp returns int. The Go method returns (int, error) so stale
//     handles surface as ErrStaleEntity too.
//   - count returns int.
func fleetInterfaceDesc() schema.InterfaceDesc {
	return schema.CloneInterfaceDesc(schema.InterfaceDesc{
		Name: "Fleet",
		Methods: []schema.MethodDesc{
			{
				Name: "spawn",
				Parameters: []schema.ParameterDesc{
					{Name: "x", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "double"}},
				},
				Returns: []schema.TypeDesc{{Kind: schema.TypeKindScalar, Name: "string"}},
			},
			{
				Name: "spawn_proxy",
				Parameters: []schema.ParameterDesc{
					{Name: "x", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "double"}},
				},
				Returns: []schema.TypeDesc{{Kind: schema.TypeKindClass, ClassName: "Unit"}},
			},
			{
				Name: "move",
				Parameters: []schema.ParameterDesc{
					{Name: "id", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
					{Name: "dx", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "double"}},
				},
				// void on the script side.
			},
			{
				Name: "hp",
				Parameters: []schema.ParameterDesc{
					{Name: "id", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
				},
				Returns: []schema.TypeDesc{{Kind: schema.TypeKindScalar, Name: "int"}},
			},
			{
				Name: "destroy",
				Parameters: []schema.ParameterDesc{
					{Name: "id", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
				},
				// void on the script side.
			},
			{
				Name: "count",
				Returns: []schema.TypeDesc{{Kind: schema.TypeKindScalar, Name: "int"}},
			},
		},
	})
}

// unitInterfaceDesc returns the per-instance Unit interface. Same
// snake_case → CamelCase mapping, this time over *UnitHost.
func unitInterfaceDesc() schema.InterfaceDesc {
	return schema.CloneInterfaceDesc(schema.InterfaceDesc{
		Name: "Unit",
		Methods: []schema.MethodDesc{
			{
				Name: "move",
				Parameters: []schema.ParameterDesc{
					{Name: "dx", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "double"}},
				},
				// void on the script side.
			},
			{
				Name: "hp",
				Returns: []schema.TypeDesc{{Kind: schema.TypeKindScalar, Name: "int"}},
			},
			{
				Name: "dispose",
				// void on the script side.
			},
		},
	})
}