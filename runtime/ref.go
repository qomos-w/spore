package runtime

import (
	"fmt"
	"reflect"
)

// Ref is the embeddable entity handle for the aggregate (OOP) authoring
// surface. A host struct embeds Ref and registers its fields as entity
// components via Bind/Attach/Register:
//
//	type Unit struct {
//	    runtime.Ref
//	    Position Position
//	    Health   Health
//	}
//	unit := &Unit{}
//	e, err := runtime.Bind(w, unit).
//	    Attach(PositionC, &unit.Position).
//	    Attach(HealthC, &unit.Health).
//	    Register()
//
// The zero-value Ref is safe: all methods are no-ops or return zero values.
// Ref must be embedded as a pointer receiver host; the host struct itself
// must not be copied after Register (component storage holds pointers into
// the host's fields — copying leaves the World pointing at the old copy).
//
// Ref is the authoring-view facade only: it does not own storage. All state
// lives in the World; registered entities are indistinguishable from plain
// ECS entities to queries, change tracking, Tick, and projection.
//
// Contract: public semantic contract — embeddable entity identity for
// aggregate host structs, backed by the same World storage as plain entities.
type Ref struct {
	Entity
}

// IsRegistered reports whether the Ref has been bound to a live entity
// by a successful Register.
func (r *Ref) IsRegistered() bool {
	return r != nil && !r.Entity.IsZero() && r.Entity.world != nil
}

// IsAlive reports whether the bound entity still exists in its World.
func (r *Ref) IsAlive() bool {
	if !r.IsRegistered() {
		return false
	}
	return r.Entity.world.IsAlive(r.Entity)
}

// Dispose removes the bound entity from its World. Safe to call on a
// zero-value or already-disposed Ref.
func (r *Ref) Dispose() {
	if !r.IsAlive() {
		return
	}
	r.Entity.world.Dispose(r.Entity)
}

// Mark records the described component as changed on the bound entity,
// for use after the host field was mutated directly. Direct field writes
// bypass World.Set, so change tracking must be triggered explicitly.
func (r *Ref) Mark[T any](c Component[T]) {
	if !r.IsAlive() {
		return
	}
	r.Entity.world.MarkChanged(r.Entity, c.Name())
}

// Set stores v into the described component of the bound entity and
// marks it changed. If the component was registered from a host field
// (the aggregate path), the value is copied back into that field so the
// struct view and the ECS view stay identical.
//
// The descriptor declares the component name on the World if it was not
// declared yet (same contract as World.Set).
func (r *Ref) Set[T any](c Component[T], v T) error {
	if !r.IsAlive() {
		return &EntityError{EntityID: r.Entity.ID(), Err: fmt.Errorf("ref not registered or entity not alive")}
	}
	name := c.Name()
	w := r.Entity.world
	data, ok := w.GetComponent(r.Entity, name)
	if !ok {
		return w.setDeclared(r.Entity, name, &v)
	}
	if dst, ok := data.(*T); ok {
		*dst = v
		return w.setDeclared(r.Entity, name, dst)
	}
	// Wrong pointer type stored under this name — do not silently overwrite.
	return &EntityError{EntityID: r.Entity.ID(), Err: fmt.Errorf("component %q stored as %T, not *%T", name, data, dst_type_name[T]())}
}

// Get retrieves the described component of the bound entity as *T.
// For host-registered fields the returned pointer aliases the host's own
// field (&host.Position); mutating through it mutates the struct.
func (r *Ref) Get[T any](c Component[T]) (*T, bool) {
	if !r.IsAlive() {
		return nil, false
	}
	return r.Entity.world.Get(r.Entity, c)
}

// Has reports whether the bound entity has the described component.
func (r *Ref) Has[T any](c Component[T]) bool {
	if !r.IsAlive() {
		return false
	}
	return r.Entity.world.Has(r.Entity, c)
}

// Remove removes the described component from the bound entity.
// For host-registered fields the component view is removed from the
// World; the host field keeps its value (the host owns its memory).
func (r *Ref) Remove[T any](c Component[T]) {
	if !r.IsAlive() {
		return
	}
	r.Entity.world.Remove(r.Entity, c)
}

// dst_type_name is a tiny helper so the error message can name T without
// fmt %T on a nilable interface value.
func dst_type_name[T any]() string {
	return reflect.TypeOf((*T)(nil)).Elem().String()
}

// Aggregate is the registration builder for a host struct that embeds Ref.
// It collects field-pointer attachments and, on Register, creates the
// entity and stores the field pointers as components.
//
// Aggregate is single-use: Register consumes it. Bind returns *Aggregate;
// Attach chains; Register finalizes.
type Aggregate struct {
	world   *World
	host    any
	ref     *Ref
	hostVal reflect.Value
	pending []pendingAttach
	errs    []error
}

type pendingAttach struct {
	name string
	ptr  any
	via  string // human description of where the attachment came from (error text)
}

func attachVia(name string) string { return fmt.Sprintf("Attach(%q)", name) }

// Bind prepares registration of a host struct pointer that embeds exactly
// one runtime.Ref. The host must be a non-nil struct pointer.
//
// Errors are collected and returned by Register; Bind itself returns the
// builder unconditionally so hosts can chain Attach calls and get one
// aggregated error at the end.
func Bind[H any](w *World, host *H) *Aggregate {
	agg := &Aggregate{world: w, host: host}
	if w == nil {
		agg.errs = append(agg.errs, fmt.Errorf("runtime.Bind: world is nil"))
		return agg
	}
	rv := reflect.ValueOf(host)
	if rv.Kind() != reflect.Pointer || rv.IsNil() || rv.Elem().Kind() != reflect.Struct {
		agg.errs = append(agg.errs, fmt.Errorf("runtime.Bind: host must be a non-nil struct pointer, got %T", host))
		return agg
	}
	agg.hostVal = rv.Elem()

	// Locate exactly one Ref field (value or pointer).
	for i := 0; i < agg.hostVal.NumField(); i++ {
		f := agg.hostVal.Field(i)
		if f.Kind() == reflect.Pointer {
			if r, ok := f.Interface().(*Ref); ok {
				if agg.ref != nil {
					agg.errs = append(agg.errs, fmt.Errorf("runtime.Bind: host has multiple Ref fields"))
					return agg
				}
				agg.ref = r
			}
			continue
		}
		if f.Kind() == reflect.Struct && f.Type() == reflect.TypeOf(Ref{}) {
			if agg.ref != nil {
				agg.errs = append(agg.errs, fmt.Errorf("runtime.Bind: host has multiple Ref fields"))
				return agg
			}
			if !f.CanAddr() {
				agg.errs = append(agg.errs, fmt.Errorf("runtime.Bind: host Ref field is not addressable"))
				return agg
			}
			agg.ref = f.Addr().Interface().(*Ref)
		}
	}
	if agg.ref == nil {
		agg.errs = append(agg.errs, fmt.Errorf("runtime.Bind: host has no *runtime.Ref field (embed as runtime.Ref with a pointer receiver)"))
	}
	return agg
}

// Attach registers the field pointed to by fieldPtr as the component
// described by c. The pointer must address an exported field of the bound
// host, and the compile-time type of the pointer must match the
// descriptor's T.
//
// Attach is a Go 1.27 generic method; the X vs T constraint is checked by
// the compiler at the call site.
func (a *Aggregate) Attach[X any](c Component[X], fieldPtr *X) *Aggregate {
	if fieldPtr == nil {
		a.errs = append(a.errs, fmt.Errorf("runtime.Attach(%s): field pointer is nil", c.Name()))
		return a
	}
	if !a.pointerInHost(fieldPtr) {
		a.errs = append(a.errs, fmt.Errorf("runtime.attach(%s): pointer does not address a field of the bound host", c.Name()))
		return a
	}
	a.pending = append(a.pending, pendingAttach{name: c.Name(), ptr: fieldPtr, via: attachVia(c.Name())})
	return a
}

// pointerInHost reports whether p addresses a field within the host's
// memory. It walks the host's addressable struct fields (one level plus
// embedded struct fields) comparing pointers.
func (a *Aggregate) pointerInHost(p any) bool {
	if !a.hostVal.IsValid() {
		return false
	}
	target := reflect.ValueOf(p).Pointer()
	base := a.hostVal.Addr().Pointer()
	size := a.hostVal.Type().Size()
	if target < base || target >= base+size {
		return false
	}
	return true
}

// Register creates the entity, writes the resolved component field pointers
// as components, records the host on the entity for Host()/EachHost retrieval,
// and returns the entity handle.
//
// Attachments come from two sources, merged before the entity is created:
//
//   - explicit Attach calls, and
//   - auto-recognized fields: every exported top-level value field of the
//     host whose type is in the World's AddRegistry component table. Both
//     anonymous embeds (field name = type name) and named fields qualify.
//
// A component name may resolve to at most one field pointer: if a host
// carries the same component type as both an anonymous embed and a named
// field (or as two named fields), Register errors and creates nothing.
// Explicit and auto attachments that map the same component to the same
// pointer are merged silently.
//
// On any collected error Register returns a zero Entity and the aggregate
// error, leaving the host unregistered.
func (a *Aggregate) Register() (Entity, error) {
	if len(a.errs) > 0 {
		return Entity{}, fmt.Errorf("runtime aggregate registration failed: %v", a.errs)
	}
	if a.ref.IsRegistered() {
		return Entity{}, fmt.Errorf("runtime aggregate registration failed: host Ref is already registered")
	}

	attaches, err := a.resolveAttachments()
	if err != nil {
		return Entity{}, fmt.Errorf("runtime aggregate registration failed: %w", err)
	}

	e := a.world.Create()
	a.ref.Entity = e

	// Store field pointers as components via the existing SetComponent path
	// so change tracking (added set) and projection see them natively. The
	// attachment descriptor declares the component name on the World first:
	// Attach is an explicit host-side declaration of the component vocabulary.
	for _, p := range attaches {
		if err := a.world.setDeclared(e, p.name, p.ptr); err != nil {
			a.ref.Entity = Entity{}
			return Entity{}, fmt.Errorf("runtime aggregate registration failed: %w", err)
		}
	}

	a.world.setHost(e, a.host)
	return e, nil
}

// resolveAttachments merges explicit Attach entries with fields the host
// auto-recognizes as components (their value type is in the World's
// AddRegistry table), then collapses same-name entries onto one field
// pointer. Two distinct pointers under one component name are an error:
// storage is name-keyed, so the second field would silently alias the first.
func (a *Aggregate) resolveAttachments() ([]pendingAttach, error) {
	merged := make([]pendingAttach, 0, len(a.pending)+2)
	merged = append(merged, a.pending...)
	merged = append(merged, a.autoComponentFields()...)

	byName := make(map[string]pendingAttach, len(merged))
	out := make([]pendingAttach, 0, len(merged))
	for _, pa := range merged {
		prev, ok := byName[pa.name]
		if !ok {
			byName[pa.name] = pa
			out = append(out, pa)
			continue
		}
		if prev.ptr == pa.ptr {
			continue // same component attached twice to the same field: harmless
		}
		return nil, fmt.Errorf("component %q resolves to multiple host fields (%s and %s): keep exactly one, either the anonymous embed or a single named field", pa.name, prev.via, pa.via)
	}
	return out, nil
}

// autoComponentFields returns attachments for host fields whose value type is
// registered as a component on the World (World.AddRegistry). Anonymous and
// named fields are treated alike — the storage identity is the schema name
// resolved from the field's Go type, never the field name. Unexported fields
// and pointer-typed fields are not auto-recognized (component storage holds
// pointers into the host; pointer fields have no stable in-host address).
func (a *Aggregate) autoComponentFields() []pendingAttach {
	if a.world == nil || !a.hostVal.IsValid() {
		return nil
	}
	typ := a.hostVal.Type()
	var out []pendingAttach
	for i := 0; i < a.hostVal.NumField(); i++ {
		sf := typ.Field(i)
		if sf.PkgPath != "" { // unexported field (incl. anonymous of unexported type)
			continue
		}
		f := a.hostVal.Field(i)
		if f.Kind() != reflect.Struct || !f.CanAddr() {
			continue
		}
		name, ok := a.world.nameForComponentType(f.Type())
		if !ok {
			continue // plain field, not a registered component
		}
		if sf.Anonymous {
			out = append(out, pendingAttach{name: name, ptr: f.Addr().Interface(), via: fmt.Sprintf("anonymous field %s", sf.Name)})
		} else {
			out = append(out, pendingAttach{name: name, ptr: f.Addr().Interface(), via: fmt.Sprintf("named field %s (%s)", sf.Name, f.Type())})
		}
	}
	return out
}
