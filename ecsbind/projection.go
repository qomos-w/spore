package ecsbind

import (
	"fmt"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/runtime"
	"github.com/qomos-w/spore/schema"
	"github.com/qomos-w/spore/transport"
)

// The projection and serialization entry points for runtime entities.
//
// These used to live as methods on runtime.World, which forced the runtime
// package (a state core that declares itself "NOT a hosting substrate") to
// import binding, schema, and transport. They now live here, next to their
// only in-tree consumers, so runtime stays a leaf: entity projection is a
// hosting concern and belongs above the carrier. The bodies are unchanged —
// they orchestrate World's public surface (IsAlive/GetComponent/MarkChanged/
// ChangeSet) plus the binding/schema/transport layers.

// ProjectEntity projects a single component of an entity through the schema
// binding layer into a ViewProjection.
//
// Contract: public semantic contract — schema-guided read-only projection
// of one runtime component.
func ProjectEntity(w *runtime.World, e runtime.Entity, componentName string, classDesc schema.ObjectDesc) (*binding.ViewProjection, error) {
	if !w.IsAlive(e) {
		return nil, &runtime.EntityError{EntityID: e.ID(), Err: fmt.Errorf("entity not alive")}
	}

	data, ok := w.GetComponent(e, componentName)
	if !ok {
		return nil, &runtime.EntityError{EntityID: e.ID(), Err: fmt.Errorf("component %q not found on entity", componentName)}
	}

	b, err := binding.NewObjectBinding(classDesc, e.ID(), data)
	if err != nil {
		return nil, &runtime.EntityError{EntityID: e.ID(), Err: fmt.Errorf("binding component %q: %w", componentName, err)}
	}

	view, err := binding.ProjectView(b)
	if err != nil {
		return nil, &runtime.EntityError{EntityID: e.ID(), Err: fmt.Errorf("projecting component %q: %w", componentName, err)}
	}

	return view, nil
}

// ProjectEntityAll projects all schema-described components of an entity
// into a single ViewProjection that merges all component fields.
// This is useful when an entity's full state needs to be projected as
// a single view (e.g., for snapshot or transport).
//
// Each component name is used as a top-level field in the projection,
// with the component's projected fields nested inside.
//
// Contract: public semantic contract — full entity state projection.
func ProjectEntityAll(w *runtime.World, e runtime.Entity, componentDescs map[string]schema.ObjectDesc) (*binding.ViewProjection, error) {
	if !w.IsAlive(e) {
		return nil, &runtime.EntityError{EntityID: e.ID(), Err: fmt.Errorf("entity not alive")}
	}

	mergedFields := make(map[string]any, len(componentDescs))

	for compName, classDesc := range componentDescs {
		data, ok := w.GetComponent(e, compName)
		if !ok {
			continue // component not present on this entity — skip
		}

		b, err := binding.NewObjectBinding(classDesc, e.ID(), data)
		if err != nil {
			return nil, &runtime.EntityError{EntityID: e.ID(), Err: fmt.Errorf("binding component %q: %w", compName, err)}
		}

		view, err := binding.ProjectView(b)
		if err != nil {
			return nil, &runtime.EntityError{EntityID: e.ID(), Err: fmt.Errorf("projecting component %q: %w", compName, err)}
		}

		mergedFields[compName] = view.Fields
	}

	return &binding.ViewProjection{
		Schema:   schema.ObjectDesc{Name: "Entity"},
		Identity: e.ID(),
		Fields:   mergedFields,
	}, nil
}

// PatchEntity applies a ViewProjection back to a component of the entity.
// This is the reverse of ProjectEntity — it writes view changes back
// to the runtime component data through the binding layer.
//
// Contract: public semantic contract — schema-guided patch writeback
// from view to runtime entity. The component is automatically marked
// changed when the patch produced mutations.
func PatchEntity(w *runtime.World, e runtime.Entity, componentName string, classDesc schema.ObjectDesc, view *binding.ViewProjection) ([]binding.MutationResult, error) {
	if !w.IsAlive(e) {
		return nil, &runtime.EntityError{EntityID: e.ID(), Err: fmt.Errorf("entity not alive")}
	}

	data, ok := w.GetComponent(e, componentName)
	if !ok {
		return nil, &runtime.EntityError{EntityID: e.ID(), Err: fmt.Errorf("component %q not found on entity", componentName)}
	}

	b, err := binding.NewObjectBinding(classDesc, e.ID(), data)
	if err != nil {
		return nil, &runtime.EntityError{EntityID: e.ID(), Err: fmt.Errorf("binding component %q for patch: %w", componentName, err)}
	}

	mutations, err := binding.ApplyViewPatch(b, view)
	if err != nil {
		return nil, &runtime.EntityError{EntityID: e.ID(), Err: fmt.Errorf("patching component %q: %w", componentName, err)}
	}

	// Mark the component as changed after successful patch
	if len(mutations) > 0 {
		w.MarkChanged(e, componentName)
	}

	return mutations, nil
}

// ProjectEntityToTransport projects a component of an entity through the
// full chain: runtime component → schema binding → view projection →
// transport encoding. This produces a transport.View ready for wire
// transmission.
//
// Contract: public semantic contract — full-chain projection from
// runtime entity to transport-encoded view.
func ProjectEntityToTransport(w *runtime.World, e runtime.Entity, componentName string, classDesc schema.ObjectDesc, codec transport.Codec) (transport.View, error) {
	view, err := ProjectEntity(w, e, componentName, classDesc)
	if err != nil {
		return transport.View{}, err
	}

	typeDesc := schema.TypeDesc{
		Kind:      schema.TypeKindStruct,
		Name:      "struct",
		ClassName: classDesc.Name,
	}

	tv, err := codec.Encode(typeDesc, e.ID(), view.Fields)
	if err != nil {
		return transport.View{}, &runtime.EntityError{EntityID: e.ID(), Err: fmt.Errorf("encoding component %q: %w", componentName, err)}
	}

	return tv, nil
}

// ProjectEntityChangesToTransport projects the current entity ChangeSet
// into a transport patch view. Added and changed components are
// schema-projected via the binding layer; removed components are emitted
// as explicit component names.
//
// Contract: public semantic contract — runtime state change projection
// to a transport patch view without making transport the change
// authority.
func ProjectEntityChangesToTransport(w *runtime.World, e runtime.Entity, componentDescs map[string]schema.ObjectDesc, codec transport.Codec) (transport.View, error) {
	if !w.IsAlive(e) {
		return transport.View{}, &runtime.EntityError{EntityID: e.ID(), Err: fmt.Errorf("entity not alive")}
	}

	changes := w.ChangeSet(e)
	removed := make([]string, 0, len(changes.Removed))
	removed = append(removed, changes.Removed...)
	payload := map[string]any{
		"Added":   map[string]any{},
		"Changed": map[string]any{},
		"Removed": removed,
	}

	add := func(bucketName string, componentName string, bucket map[string]any) error {
		classDesc, ok := componentDescs[componentName]
		if !ok {
			return &runtime.EntityError{EntityID: e.ID(), Err: fmt.Errorf("schema descriptor for %s component %q not found", bucketName, componentName)}
		}
		view, err := ProjectEntity(w, e, componentName, classDesc)
		if err != nil {
			return err
		}
		bucket[componentName] = view.Fields
		return nil
	}

	addedPayload := payload["Added"].(map[string]any)
	for _, componentName := range changes.Added {
		if err := add("added", componentName, addedPayload); err != nil {
			return transport.View{}, err
		}
	}

	changedPayload := payload["Changed"].(map[string]any)
	for _, componentName := range changes.Changed {
		if err := add("changed", componentName, changedPayload); err != nil {
			return transport.View{}, err
		}
	}

	typeDesc := schema.TypeDesc{
		Kind:      schema.TypeKindStruct,
		Name:      "struct",
		ClassName: "EntityPatch",
	}

	tv, err := codec.Encode(typeDesc, e.ID(), payload)
	if err != nil {
		return transport.View{}, &runtime.EntityError{EntityID: e.ID(), Err: fmt.Errorf("encoding entity changes: %w", err)}
	}
	tv.Kind = transport.ViewKindPatch
	return tv, nil
}
