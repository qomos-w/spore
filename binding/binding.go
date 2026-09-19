package binding

import (
	"context"
	"fmt"
	"sync"

	"github.com/qomos-w/spore/identity"
	"github.com/qomos-w/spore/invoke"
	"github.com/qomos-w/spore/schema"
)

// ScriptBinding is the unified entry point for the Script Binding plane.
// It combines callable binding (Go function → script-callable) and
// data binding (Go struct → script-visible object) under a single surface.
// Contract: public semantic contract — the unified facade for the
// Script Binding plane.
//
// Script Binding is the runtime projection layer of schema — it binds
// Go structs to script-visible objects and Go functions to script-callable
// functions. It consumes schema descriptors and produces bound runtime
// artifacts.
//
// ScriptBinding is NOT a truth authority. It projects and connects layers.
// It does NOT define lifecycle authority — that belongs to the hosting layer.
// Instead, it provides an observability surface (InvalidateBindingsFor) that
// external callers use to notify bindings when their backing entities expire.
//
// Registration state lives in a single Registry (see registry.go). The
// Callables and Executors fields are the engine-facing invoke contract views
// over that one store, so the callable plane, the executable plane, and the
// capability plane share one lock and one descriptor-validation path.
type ScriptBinding struct {
	// Callables is the invoke.CallableSource view over the unified registry.
	Callables invoke.CallableSource
	// Executors is the invoke.ExecutorSource view over the unified registry.
	Executors invoke.ExecutorSource

	mu       sync.RWMutex
	registry *Registry
	objects  map[identity.CanonicalID]*ObjectBinding
}

// NewScriptBinding creates a ScriptBinding with a fresh unified registry.
func NewScriptBinding() *ScriptBinding {
	sb := &ScriptBinding{}
	sb.ensureRegistry()
	return sb
}

// ensureRegistry lazily creates the unified registry and its engine-facing
// views. The check-then-act runs under sb.mu so concurrent first accesses
// cannot race, while reads use registryReadOnly to leave an absent registry
// observable as absent (the documented fallback contract).
func (sb *ScriptBinding) ensureRegistry() *Registry {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	if sb.registry == nil {
		reg := NewRegistry()
		sb.registry = reg
		sb.Callables = reg.CallableSource()
		sb.Executors = reg.ExecutorSource()
	}
	return sb.registry
}

// registryReadOnly snapshots the registry under sb.mu for a race-free read;
// it returns nil when the registry has not been initialized.
func (sb *ScriptBinding) registryReadOnly() *Registry {
	sb.mu.RLock()
	defer sb.mu.RUnlock()
	return sb.registry
}

// CallableSource exposes the callable plane as the engine-facing
// invoke.CallableSource. Returns nil when the registry is absent so
// engine-side nil checks stay meaningful.
func (sb *ScriptBinding) CallableSource() invoke.CallableSource {
	if sb == nil {
		return nil
	}
	sb.mu.RLock()
	defer sb.mu.RUnlock()
	return sb.Callables
}

// ExecutorSource exposes the executable plane as the engine-facing
// invoke.ExecutorSource. Returns nil when the registry is absent.
func (sb *ScriptBinding) ExecutorSource() invoke.ExecutorSource {
	if sb == nil {
		return nil
	}
	sb.mu.RLock()
	defer sb.mu.RUnlock()
	return sb.Executors
}

// BindFunction describes a Go function via reflection, registers it as a
// callable, creates an executable adapter, and registers the adapter.
// This is the convenience path for the common case of exposing a Go function
// as a script-callable.
func (sb *ScriptBinding) BindFunction(name string, fn any) error {
	reg := sb.ensureRegistry()
	if _, err := reg.RegisterGoFunction(name, fn); err != nil {
		return err
	}
	adapter, err := NewGoFunctionAdapter(name, fn)
	if err != nil {
		return err
	}
	return reg.RegisterAdapter(adapter)
}

// Invoke dispatches an invocation request to the appropriate adapter.
func (sb *ScriptBinding) Invoke(req InvocationRequest) (InvocationOutcome, error) {
	if req.Context == nil {
		req.Context = context.Background()
	}
	return sb.ensureRegistry().Invoke(req)
}

// RegisterCapability registers a native capability namespace.
func (sb *ScriptBinding) RegisterCapability(cap RegisteredCapability) error {
	return sb.ensureRegistry().RegisterCapability(cap)
}

// DescribeCapability returns a registered capability descriptor.
func (sb *ScriptBinding) DescribeCapability(name string) (CapabilityDesc, bool) {
	reg := sb.registryReadOnly()
	if reg == nil {
		return CapabilityDesc{}, false
	}
	return reg.DescribeCapability(name)
}

// DescribeCapabilities returns all registered capability descriptors.
func (sb *ScriptBinding) DescribeCapabilities() []CapabilityDesc {
	reg := sb.registryReadOnly()
	if reg == nil {
		return nil
	}
	return reg.DescribeCapabilities()
}

// FindCapabilityObject looks up a native capability exported object descriptor.
func (sb *ScriptBinding) FindCapabilityObject(capabilityName, objectName string) (schema.ObjectDesc, bool) {
	reg := sb.registryReadOnly()
	if reg == nil {
		return schema.ObjectDesc{}, false
	}
	return reg.FindCapabilityObject(capabilityName, objectName)
}

// FindCapabilityInterface looks up a native capability exported interface descriptor.
func (sb *ScriptBinding) FindCapabilityInterface(capabilityName, interfaceName string) (schema.InterfaceDesc, bool) {
	reg := sb.registryReadOnly()
	if reg == nil {
		return schema.InterfaceDesc{}, false
	}
	return reg.FindCapabilityInterface(capabilityName, interfaceName)
}

// FindCapabilityTypeAlias looks up a native capability exported type alias.
func (sb *ScriptBinding) FindCapabilityTypeAlias(capabilityName, aliasName string) (schema.TypeDesc, bool) {
	reg := sb.registryReadOnly()
	if reg == nil {
		return schema.TypeDesc{}, false
	}
	return reg.FindCapabilityTypeAlias(capabilityName, aliasName)
}

// FindCapabilityValue looks up a native capability exported value.
func (sb *ScriptBinding) FindCapabilityValue(capabilityName, valueName string) (CapabilityValueDesc, any, bool) {
	reg := sb.registryReadOnly()
	if reg == nil {
		return CapabilityValueDesc{}, nil, false
	}
	return reg.FindCapabilityValue(capabilityName, valueName)
}

// InvokeCapability dispatches a native capability callable.
func (sb *ScriptBinding) InvokeCapability(ctx context.Context, capabilityName, callableName string, input any) (any, error) {
	reg := sb.registryReadOnly()
	if reg == nil {
		return nil, fmt.Errorf("capability registry is required")
	}
	return reg.InvokeCapability(ctx, capabilityName, callableName, input)
}

// ExposeCapabilityCallables registers flattened callable descriptors and
// adapters for a capability. The exposure is performed by the unified registry
// as a single transaction (see Registry.ExposeCapabilityCallables), so the
// descriptor-consistency check is not duplicated at this call site.
func (sb *ScriptBinding) ExposeCapabilityCallables(capabilityName string) error {
	reg := sb.registryReadOnly()
	if reg == nil {
		return fmt.Errorf("capability registry is required")
	}
	return reg.ExposeCapabilityCallables(capabilityName)
}

// BindObject creates an ObjectBinding between a schema descriptor and a
// runtime Go object. This is the data binding path for exposing Go structs
// as script-visible objects. The binding is tracked by identity for lifecycle
// observability.
//
// If an identity already has a valid binding, the old binding is
// invalidated before the new one is stored. This ensures that external
// holders of the old binding handle observe the lifecycle transition
// — they must re-acquire via LookupBinding to continue use.
func (sb *ScriptBinding) BindObject(classDesc schema.ObjectDesc, id identity.CanonicalID, target any) (*ObjectBinding, error) {
	b, err := NewObjectBinding(classDesc, id, target)
	if err != nil {
		return nil, err
	}
	sb.mu.Lock()
	if sb.objects == nil {
		sb.objects = make(map[identity.CanonicalID]*ObjectBinding)
	}
	if old, ok := sb.objects[id]; ok && old.Valid() {
		old.Invalidate()
	}
	sb.objects[id] = b
	sb.mu.Unlock()
	return b, nil
}

// InvalidateBindingsFor marks the binding associated with the given identity
// as invalid. This is the lifecycle observability mechanism — external callers
// (e.g., runtime carrier consumers) invoke this when a backing entity is
// disposed or otherwise no longer available.
//
// This does NOT define lifecycle authority. The decision of *when* to
// invalidate belongs to the caller, not to the binding layer.
func (sb *ScriptBinding) InvalidateBindingsFor(id identity.CanonicalID) {
	sb.mu.RLock()
	b, ok := sb.objects[id]
	sb.mu.RUnlock()
	if ok {
		b.Invalidate()
	}
}

// LookupBinding returns the ObjectBinding associated with the given identity,
// if one exists and is still valid.
func (sb *ScriptBinding) LookupBinding(id identity.CanonicalID) (*ObjectBinding, bool) {
	sb.mu.RLock()
	b, ok := sb.objects[id]
	sb.mu.RUnlock()
	if !ok || !b.Valid() {
		return nil, false
	}
	return b, true
}

// BoundIdentities returns the identities of all currently valid bindings.
// The returned slice is in no particular order.
func (sb *ScriptBinding) BoundIdentities() []identity.CanonicalID {
	sb.mu.RLock()
	defer sb.mu.RUnlock()
	ids := make([]identity.CanonicalID, 0, len(sb.objects))
	for id, b := range sb.objects {
		if b.Valid() {
			ids = append(ids, id)
		}
	}
	return ids
}
