package runtime

// This file provides the Go 1.27 typed facade over the World's
// name-keyed component storage. These are methods with type parameters
// declared on non-generic structs (World, Query) — the capability
// unlocked by Go 1.27 method type parameters.
//
// Every typed method delegates to the string-keyed implementation, so
// storage layout, change tracking, error semantics, and the
// schema/binding/transport integration points are unchanged. The string
// API remains the interop surface for scripts and bindings; the typed
// API is the compile-time-safe authoring surface for Go host code.
//
// Note: generic methods cannot satisfy interfaces in Go, so consumers
// that abstract World behind an interface must keep using the string API.

// Set stores v as the entity's component of the given descriptor,
// overwriting any existing component with the same schema name.
// Returns an error if the entity is disposed or does not exist.
//
// Using a Component[T] descriptor is itself a declaration of the component
// vocabulary: Set declares c.Name() on this World before writing (see
// type_registry.go, component-name whitelist), so hand-written components keep
// working through the descriptor API without a separate registration call.
// Read paths (Get/Has/Remove/Mark) never declare.
func (w *World) Set[T any](e Entity, c Component[T], v *T) error {
	return w.setDeclared(e, c.Name(), v)
}

// setDeclared is the descriptor-path write: a Component[T] descriptor is a
// Go-level declaration of the component vocabulary, so it declares its own name
// on this World before writing (see setComponent). Read paths (Get/Has/Remove/
// Mark) never declare.
func (w *World) setDeclared(e Entity, name string, data any) error {
	return w.setComponent(e, name, data, true)
}

// Get retrieves the entity's component as *T.
// Returns (nil, false) if the entity is not alive, the component is
// absent, or the stored value is not a *T.
func (w *World) Get[T any](e Entity, c Component[T]) (*T, bool) {
	data, ok := w.GetComponent(e, c.Name())
	if !ok {
		return nil, false
	}
	v, ok := data.(*T)
	if !ok {
		return nil, false
	}
	return v, true
}

// Has reports whether the entity has the described component.
func (w *World) Has[T any](e Entity, c Component[T]) bool {
	return w.HasComponent(e, c.Name())
}

// Remove removes the described component from the entity.
// It is safe to call on a non-existent component.
func (w *World) Remove[T any](e Entity, c Component[T]) {
	w.RemoveComponent(e, c.Name())
}

// Mark marks the described component as changed on the entity,
// for use when a *T obtained from Get is mutated through its reference.
func (w *World) Mark[T any](e Entity, c Component[T]) {
	w.MarkChanged(e, c.Name())
}

// --- Descriptor-free facade (codegen-backed) ---
//
// These resolve the Component[T] from the registry the host registered onto
// this World with w.AddRegistry(gen.Registry) (see type_registry.go), so
// callers write only the value: w.SetT(e, v). They exist for codegen-managed
// components (@component structs); types not in the registered table return
// an error, and hand-written components keep using the explicit descriptor
// API.

// SetT stores v as the entity's component of type T, resolving the schema
// name from the World's component registry. Returns an error if T was never
// registered (AddRegistry was not called for the generated table) or the
// entity is dead.
func (w *World) SetT[T any](e Entity, v *T) error {
	c, err := w.componentDescriptorFor[T]()
	if err != nil {
		return err
	}
	return w.Set(e, c, v)
}

// GetT retrieves the entity's component of type T via the type registry.
func (w *World) GetT[T any](e Entity) (*T, bool) {
	c, err := w.componentDescriptorFor[T]()
	if err != nil {
		return nil, false
	}
	return w.Get(e, c)
}

// HasT reports whether the entity has a component of type T via the registry.
func (w *World) HasT[T any](e Entity) bool {
	c, err := w.componentDescriptorFor[T]()
	if err != nil {
		return false
	}
	return w.Has(e, c)
}

// RemoveT removes the entity's component of type T via the registry.
func (w *World) RemoveT[T any](e Entity) {
	c, err := w.componentDescriptorFor[T]()
	if err != nil {
		return
	}
	w.Remove(e, c)
}

// MarkT marks the entity's component of type T as changed via the registry.
func (w *World) MarkT[T any](e Entity) {
	c, err := w.componentDescriptorFor[T]()
	if err != nil {
		return
	}
	w.Mark(e, c)
}

// eachState pairs a matched entity with its state snapshot, so typed
// iteration does not re-resolve the entity through World.entities.
type entityMatch struct {
	e     Entity
	state *entityState
}

// executeMatches runs the query and returns matched entities together with
// their states, avoiding a second map lookup per entity during iteration.
func (w *World) executeMatches(q *Query) []entityMatch {
	var matches []entityMatch
	cq := w.compileQuery(q)
	w.rangeStates(&cq, func(st *entityState) bool {
		matches = append(matches, entityMatch{e: Entity{id: st.id, world: w, gen: st.gen}, state: st})
		return true
	})
	return matches
}

// eachTyped iterates matched entities, resolving component names to
// component IDs once per call and fetching components by ID from the
// state's storage slots.
func eachTyped[T1, T2, T3 any](w *World, q *Query, c1 Component[T1], c2 Component[T2], c3 Component[T3], hasC3 bool, fn func(e Entity, v1 *T1, v2 *T2, v3 *T3)) {
	cq := w.compileQuery(q)
	id1, ok1 := w.compIDs[c1.Name()]
	id2, ok2 := w.compIDs[c2.Name()]
	var id3 uint32
	ok3 := true
	if hasC3 {
		id3, ok3 = w.compIDs[c3.Name()]
	}
	if !ok1 || !ok2 || (hasC3 && !ok3) {
		return
	}
	w.rangeStates(&cq, func(st *entityState) bool {
		if !ok1 || !ok2 || !maskHas(st.has, id1) || !maskHas(st.has, id2) {
			return true
		}
		p1, ok := st.components[id1].(*T1)
		if !ok {
			return true
		}
		p2, ok := st.components[id2].(*T2)
		if !ok {
			return true
		}
		if hasC3 {
			if !ok3 || !maskHas(st.has, id3) {
				return true
			}
			p3, ok := st.components[id3].(*T3)
			if !ok {
				return true
			}
			fn(Entity{id: st.id, world: w, gen: st.gen}, p1, p2, p3)
		} else {
			fn(Entity{id: st.id, world: w, gen: st.gen}, p1, p2, nil)
		}
		return true
	})
}

// Each2 iterates the entities matched by the query that carry both c1 and
// c2, invoking fn with the entity and its two typed components.
// Entities whose stored values are not the expected pointer types are
// skipped.
func (w *World) Each2[T1, T2 any](q *Query, c1 Component[T1], c2 Component[T2], fn func(e Entity, v1 *T1, v2 *T2)) {
	eachTyped(w, q, c1, c2, Component[struct{}]{}, false, func(e Entity, v1 *T1, v2 *T2, _ *struct{}) {
		fn(e, v1, v2)
	})
}

// Each3 iterates the entities matched by the query that carry all three
// components, invoking fn with the entity and its typed components.
// Entities whose stored values are not the expected pointer types are
// skipped.
func (w *World) Each3[T1, T2, T3 any](q *Query, c1 Component[T1], c2 Component[T2], c3 Component[T3], fn func(e Entity, v1 *T1, v2 *T2, v3 *T3)) {
	eachTyped(w, q, c1, c2, c3, true, fn)
}
