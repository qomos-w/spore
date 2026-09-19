package binding

import (
	"fmt"

	"github.com/qomos-w/spore/schema"
)

// CallableRegistry maintains the set of registered callable descriptors.
// It is part of the Script Binding plane — it holds runtime state that
// maps callable names to their schema descriptors.
// Contract: public semantic contract — callable descriptor registration and lookup.
// Not an SPI: currently a concrete type. Promoted to SPI only when a real
// alternative registry backend is needed (per ARCHITECTURE §5.2).
type CallableRegistry struct {
	callables map[string]schema.CallableDesc
	ordered   []schema.CallableDesc
}

// NewCallableRegistry creates an empty callable registry.
func NewCallableRegistry() *CallableRegistry {
	return &CallableRegistry{
		callables: make(map[string]schema.CallableDesc),
		ordered:   make([]schema.CallableDesc, 0),
	}
}

// Register adds a callable descriptor to the registry.
// Returns an error if the descriptor is invalid or already registered.
func (r *CallableRegistry) Register(desc schema.CallableDesc) error {
	if err := schema.ValidateCallableDesc(desc); err != nil {
		return err
	}
	if _, exists := r.callables[desc.Name]; exists {
		return fmt.Errorf("callable %q already registered", desc.Name)
	}

	copied := schema.CloneCallableDesc(desc)
	r.callables[desc.Name] = copied
	r.ordered = append(r.ordered, copied)
	return nil
}

// RegisterGoFunction describes a Go function via reflection and registers
// it as a callable descriptor.
func (r *CallableRegistry) RegisterGoFunction(name string, fn any) (schema.CallableDesc, error) {
	desc, err := schema.DescribeGoFunction(name, fn)
	if err != nil {
		return schema.CallableDesc{}, err
	}
	if err := r.Register(desc); err != nil {
		return schema.CallableDesc{}, err
	}
	return schema.CloneCallableDesc(desc), nil
}

// Lookup returns a deep-cloned callable descriptor for the given name.
func (r *CallableRegistry) Lookup(name string) (schema.CallableDesc, bool) {
	desc, ok := r.callables[name]
	if !ok {
		return schema.CallableDesc{}, false
	}
	return schema.CloneCallableDesc(desc), true
}

// LookupRef returns the stored descriptor without cloning. The returned
// value must be treated as immutable — its slices are shared with the
// registry. Hot dispatch paths use this to avoid per-invoke deep clones.
func (r *CallableRegistry) LookupRef(name string) (schema.CallableDesc, bool) {
	desc, ok := r.callables[name]
	if !ok {
		return schema.CallableDesc{}, false
	}
	return desc, true
}

// List returns all registered callable descriptors in registration order.
func (r *CallableRegistry) List() []schema.CallableDesc {
	list := make([]schema.CallableDesc, 0, len(r.ordered))
	for _, desc := range r.ordered {
		list = append(list, schema.CloneCallableDesc(desc))
	}
	return list
}
