package binding

import (
	"fmt"

	"github.com/qomos-w/spore/schema"
)

// ExecutableRegistry binds callable descriptors to executable adapters
// and dispatches invocations. It is part of the Script Binding plane —
// it holds runtime state that connects schema-described callables to
// their executable implementations.
// Contract: public semantic contract — adapter registration and invocation dispatch.
// Not an SPI: currently a concrete type. Promoted to SPI only when a real
// alternative dispatch backend is needed (per ARCHITECTURE §5.2).
type ExecutableRegistry struct {
	callables *CallableRegistry
	adapters  map[string]ExecutableAdapter
}

// NewExecutableRegistry creates an executable registry backed by the
// given callable registry.
func NewExecutableRegistry(callables *CallableRegistry) *ExecutableRegistry {
	return &ExecutableRegistry{
		callables: callables,
		adapters:  make(map[string]ExecutableAdapter),
	}
}

// RegisterAdapter registers an executable adapter for a callable that
// is already in the backing callable registry. The adapter's descriptor
// must match the registered descriptor.
func (r *ExecutableRegistry) RegisterAdapter(adapter ExecutableAdapter) error {
	if r.callables == nil {
		return fmt.Errorf("callable registry is required")
	}
	desc := adapter.Callable()
	registered, ok := r.callables.LookupRef(desc.Name)
	if !ok {
		return fmt.Errorf("callable %q is not registered", desc.Name)
	}
	if !callableDescriptorsEqual(registered, desc) {
		return fmt.Errorf("callable %q descriptor does not match registry", desc.Name)
	}
	if _, exists := r.adapters[desc.Name]; exists {
		return fmt.Errorf("callable %q adapter already registered", desc.Name)
	}
	r.adapters[desc.Name] = adapter
	return nil
}

// Lookup returns the executable adapter for the given callable name.
func (r *ExecutableRegistry) Lookup(name string) (ExecutableAdapter, bool) {
	adapter, ok := r.adapters[name]
	return adapter, ok
}

// ForEachAdapter visits registered executable adapters until fn returns false.
func (r *ExecutableRegistry) ForEachAdapter(fn func(name string, adapter ExecutableAdapter) bool) {
	if r == nil || fn == nil {
		return
	}
	for name, adapter := range r.adapters {
		if !fn(name, adapter) {
			return
		}
	}
}

// Invoke dispatches an invocation request to the appropriate adapter.
func (r *ExecutableRegistry) Invoke(req InvocationRequest) (InvocationOutcome, error) {
	if r.callables == nil {
		return InvocationOutcome{}, fmt.Errorf("callable registry is required")
	}
	desc, ok := r.callables.LookupRef(req.Callable)
	if !ok {
		return InvocationOutcome{}, fmt.Errorf("callable %q is not registered", req.Callable)
	}
	if err := ValidateInvocationStage(desc, req.Stage); err != nil {
		return InvocationOutcome{}, err
	}
	adapter, ok := r.adapters[req.Callable]
	if !ok {
		return InvocationOutcome{}, fmt.Errorf("callable %q has no executable adapter", req.Callable)
	}
	return adapter.Invoke(req)
}

func callableDescriptorsEqual(left, right schema.CallableDesc) bool {
	left = schema.CloneCallableDesc(left)
	right = schema.CloneCallableDesc(right)
	if normalizedCallableMode(left) != normalizedCallableMode(right) || left.Name != right.Name || left.HasError != right.HasError {
		return false
	}
	if len(left.Parameters) != len(right.Parameters) || len(left.Returns) != len(right.Returns) {
		return false
	}
	for i := range left.Parameters {
		if left.Parameters[i].Name != right.Parameters[i].Name || !typeDescsEqual(left.Parameters[i].Type, right.Parameters[i].Type) {
			return false
		}
	}
	for i := range left.Returns {
		if !typeDescsEqual(left.Returns[i], right.Returns[i]) {
			return false
		}
	}
	if left.Streaming == nil || right.Streaming == nil {
		return left.Streaming == nil && right.Streaming == nil
	}
	return typeDescPtrsEqual(left.Streaming.Next, right.Streaming.Next) &&
		typeDescPtrsEqual(left.Streaming.Final, right.Streaming.Final) &&
		messageStreamingDescsEqual(left.Streaming.Message, right.Streaming.Message)
}

// messageStreamingDescsEqual compares the start/delta/end message payload
// schemas of a streaming callable. Callable descriptors that differ only in
// their message protocol must not be treated as equal by the registration
// dedup path (ExposeCapabilityCallables).
func messageStreamingDescsEqual(left, right *schema.MessageStreamingDesc) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return typeDescPtrsEqual(left.Start, right.Start) &&
		typeDescPtrsEqual(left.Delta, right.Delta) &&
		typeDescPtrsEqual(left.End, right.End)
}

func normalizedCallableMode(desc schema.CallableDesc) schema.CallableMode {
	if desc.Mode == "" {
		return schema.CallableModeUnary
	}
	return desc.Mode
}

func typeDescPtrsEqual(left, right *schema.TypeDesc) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return typeDescsEqual(*left, *right)
}

func typeDescsEqual(left, right schema.TypeDesc) bool {
	if left.Kind != right.Kind || left.Name != right.Name || left.TypeID != right.TypeID || left.ClassName != right.ClassName || left.ClassID != right.ClassID {
		return false
	}
	return typeDescPtrsEqual(left.Element, right.Element) && typeDescPtrsEqual(left.Key, right.Key) && typeDescPtrsEqual(left.Value, right.Value)
}
