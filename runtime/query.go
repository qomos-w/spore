package runtime

import "iter"

// Query describes a component-name-based filter for entities.
// It follows Donburi's NewQuery().Has(...) pattern but uses schema
// name strings instead of type objects, since components in spore
// are schema-described.
//
// Query is immutable after construction — each filter method returns
// a new Query with the additional constraint.
//
// Contract: public semantic contract — component-based entity filter
// for the runtime carrier.
type Query struct {
	hasAll     []string
	hasNone    []string
	hasEither  []string
	whenAdded  []string
	whenChanged []string
	whenRemoved []string
}

// NewQuery creates a new Query with no filters.
func NewQuery() *Query {
	return &Query{}
}

// Has filters for entities that have ALL of the specified components.
func (q *Query) Has(schemaNames ...string) *Query {
	return &Query{
		hasAll:      append(q.hasAll, schemaNames...),
		hasNone:     q.hasNone,
		hasEither:   q.hasEither,
		whenAdded:   q.whenAdded,
		whenChanged: q.whenChanged,
		whenRemoved: q.whenRemoved,
	}
}

// HasNone filters for entities that have NONE of the specified components.
func (q *Query) HasNone(schemaNames ...string) *Query {
	return &Query{
		hasAll:      q.hasAll,
		hasNone:     append(q.hasNone, schemaNames...),
		hasEither:   q.hasEither,
		whenAdded:   q.whenAdded,
		whenChanged: q.whenChanged,
		whenRemoved: q.whenRemoved,
	}
}

// HasEither filters for entities that have at least one of the specified components.
func (q *Query) HasEither(schemaNames ...string) *Query {
	return &Query{
		hasAll:      q.hasAll,
		hasNone:     q.hasNone,
		hasEither:   append(q.hasEither, schemaNames...),
		whenAdded:   q.whenAdded,
		whenChanged: q.whenChanged,
		whenRemoved: q.whenRemoved,
	}
}

// WhenAdded filters for entities that had the specified components added
// during the current tick.
func (q *Query) WhenAdded(schemaNames ...string) *Query {
	return &Query{
		hasAll:      q.hasAll,
		hasNone:     q.hasNone,
		hasEither:   q.hasEither,
		whenAdded:   append(q.whenAdded, schemaNames...),
		whenChanged: q.whenChanged,
		whenRemoved: q.whenRemoved,
	}
}

// WhenChanged filters for entities that had the specified components
// marked as changed during the current tick.
func (q *Query) WhenChanged(schemaNames ...string) *Query {
	return &Query{
		hasAll:      q.hasAll,
		hasNone:     q.hasNone,
		hasEither:   q.hasEither,
		whenAdded:   q.whenAdded,
		whenChanged: append(q.whenChanged, schemaNames...),
		whenRemoved: q.whenRemoved,
	}
}

// WhenRemoved filters for entities that had the specified components
// removed during the current tick.
func (q *Query) WhenRemoved(schemaNames ...string) *Query {
	return &Query{
		hasAll:      q.hasAll,
		hasNone:     q.hasNone,
		hasEither:   q.hasEither,
		whenAdded:   q.whenAdded,
		whenChanged: q.whenChanged,
		whenRemoved: append(q.whenRemoved, schemaNames...),
	}
}

// With filters for entities that have the described component.
// It composes across heterogeneous types by chaining:
//
//	q.With(Position).Without(Velocity).OnAdded(Health)
func (q *Query) With[T any](c Component[T]) *Query {
	return q.Has(c.Name())
}

// Without filters for entities that do not have the described component.
func (q *Query) Without[T any](c Component[T]) *Query {
	return q.HasNone(c.Name())
}

// OnAdded filters for entities that had the described component added
// during the current tick.
func (q *Query) OnAdded[T any](c Component[T]) *Query {
	return q.WhenAdded(c.Name())
}

// OnChanged filters for entities that had the described component marked
// as changed during the current tick.
func (q *Query) OnChanged[T any](c Component[T]) *Query {
	return q.WhenChanged(c.Name())
}

// OnRemoved filters for entities that had the described component removed
// during the current tick.
func (q *Query) OnRemoved[T any](c Component[T]) *Query {
	return q.WhenRemoved(c.Name())
}

// Execute runs the query against the world and returns matching entities.
// The returned slice is a snapshot: it stays valid while the caller mutates
// the world (including creating or disposing entities) during iteration.
func (w *World) Execute(q *Query) []Entity {
	var result []Entity
	cq := w.compileQuery(q)
	w.rangeStates(&cq, func(st *entityState) bool {
		result = append(result, Entity{id: st.id, world: w, gen: st.gen})
		return true
	})
	return result
}

// Range returns a streaming iterator over the entities matched by the
// query, for range loops that never materialize a slice:
//
//	for e := range w.Range(q) {
//		...
//	}
//
// Matching semantics are identical to Execute, but traversal is lazy: the
// loop body runs during the world walk, so breaking out stops all further
// matching work and no []Entity is allocated. Entities may be disposed and
// components added, removed, or marked inside the loop; creating entities
// during iteration is not allowed — use Execute when the loop body spawns
// entities.
//
// Internally, hasAll-filtered queries iterate the smallest matching
// component set instead of the whole entity table; an entity removed from
// that driver set by the loop body itself is not skipped.
//
// Contract: public semantic contract — streaming entity traversal; same
// filter semantics as Execute.
func (w *World) Range(q *Query) iter.Seq[Entity] {
	return func(yield func(Entity) bool) {
		cq := w.compileQuery(q)
		w.rangeStates(&cq, func(st *entityState) bool {
			return yield(Entity{id: st.id, world: w, gen: st.gen})
		})
	}
}
