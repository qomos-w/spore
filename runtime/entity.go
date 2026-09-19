// Package runtime defines the Runtime Carrier — the schema-aware state
// carrier that owns entity lifecycle, component storage, and query execution.
//
// The runtime carrier is NOT a hosting substrate, NOT a control plane, and
// NOT a goflora semantic center. It carries state, tracks changes, and
// provides query and projection services for the script/schema/binding layer.
//
// Design influences:
//   - Donburi-style public semantics: lightweight Entity handle, World-owned storage
//   - ECS-style dirty tracking: added/changed/removed component sets
//   - Spore-specific: schema-name-keyed components, CanonicalID entity identity
package runtime

import (
	"github.com/qomos-w/spore/identity"
)

// Entity is a lightweight identity-bearing handle to a runtime entity.
// It carries a CanonicalID, a reference to its owning World, and the
// generation (creation epoch) of the entity state it was issued against.
//
// Entity does not own component data — that belongs to the World.
//
// Entity is comparable and can be used as a map key.
// The zero value represents a null entity; use IsZero to test for it.
//
// Generation semantics: every entity state created by Create/CreateWithID
// is stamped with a fresh per-World epoch. A handle only resolves while
// the current state for its ID carries the same epoch, so after an ID is
// reused (CreateWithID replacing a live entity or a swept tombstone), old
// handles correctly report stale instead of silently pointing at the new
// entity (the ABA problem). Handles issued by Execute/Range/Each and
// World.Entity always carry the current epoch.
//
// Contract: public semantic contract — identity-bearing entity handle
// for the runtime carrier.
type Entity struct {
	id    identity.CanonicalID
	world *World
	gen   uint32
}

// ID returns the canonical identity of the entity.
func (e Entity) ID() identity.CanonicalID {
	return e.id
}

// IsZero reports whether the entity is the null entity.
func (e Entity) IsZero() bool {
	return e.id.IsZero() && e.world == nil
}

// MakeEntity constructs an Entity with the given ID and World reference,
// stamped with the current generation of the existing state for that ID
// (so it resolves like a handle issued by World.Entity). For an ID with
// no state yet — including a nil World, the detached-handle test pattern —
// it returns a handle carrying generation 0, which never matches a
// later-created state.
// This is primarily useful for tests. In normal usage, entities are created
// via World.Create or World.CreateWithID.
func MakeEntity(id identity.CanonicalID, w *World) Entity {
	if w != nil {
		if st := w.entities[id]; st != nil {
			return Entity{id: id, world: w, gen: st.gen}
		}
	}
	return Entity{id: id, world: w}
}
