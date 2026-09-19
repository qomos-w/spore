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
type ScriptBinding struct {
	Callables    *CallableRegistry
	Executors    *ExecutableRegistry
	Capabilities CapabilityRegistry

	mu      sync.RWMutex
	objects map[identity.CanonicalID]*ObjectBinding
}

// NewScriptBinding creates a ScriptBinding with fresh registries.
func NewScriptBinding() *ScriptBinding {
	callables := NewCallableRegistry()
	return &ScriptBinding{
		Callables:    callables,
		Executors:    NewExecutableRegistry(callables),
		Capabilities: NewMemoryCapabilityRegistry(),
		objects:      make(map[identity.CanonicalID]*ObjectBinding),
	}
}

// CallableSource exposes the callable registry as the engine-facing
// invoke.CallableSource. Returns nil when the registry is absent so
// engine-side nil checks stay meaningful.
func (sb *ScriptBinding) CallableSource() invoke.CallableSource {
	if sb == nil || sb.Callables == nil {
		return nil
	}
	return sb.Callables
}

// ExecutorSource exposes the executable registry as the engine-facing
// invoke.ExecutorSource. Returns nil when the registry is absent.
func (sb *ScriptBinding) ExecutorSource() invoke.ExecutorSource {
	if sb == nil || sb.Executors == nil {
		return nil
	}
	return sb.Executors
}

// BindFunction describes a Go function via reflection, registers it as a
// callable, creates an executable adapter, and registers the adapter.
// This is the convenience path for the common case of exposing a Go function
// as a script-callable.
func (sb *ScriptBinding) BindFunction(name string, fn any) error {
	desc, err := sb.Callables.RegisterGoFunction(name, fn)
	if err != nil {
		return err
	}
	adapter, err := NewGoFunctionAdapter(name, fn)
	if err != nil {
		return err
	}
	_ = desc // adapter's descriptor matches registration
	return sb.Executors.RegisterAdapter(adapter)
}

// Invoke dispatches an invocation request to the appropriate adapter.
func (sb *ScriptBinding) Invoke(req InvocationRequest) (InvocationOutcome, error) {
	if req.Context == nil {
		req.Context = context.Background()
	}
	return sb.Executors.Invoke(req)
}

// capabilities returns the live capability registry, initializing it
// lazily under sb.mu so concurrent first accesses (register vs.
// describe/find) cannot race on the check-then-act that plain nil guards
// would allow. Callers that only read the field must use
// capabilitiesReadOnly instead, which leaves a nil registry observable
// as nil (the documented fallback contract).
func (sb *ScriptBinding) capabilities() CapabilityRegistry {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	if sb.Capabilities == nil {
		sb.Capabilities = NewMemoryCapabilityRegistry()
	}
	return sb.Capabilities
}

// capabilitiesReadOnly snapshots the field under sb.mu for a race-free
// read; it returns nil when the registry has not been initialized.
func (sb *ScriptBinding) capabilitiesReadOnly() CapabilityRegistry {
	sb.mu.RLock()
	defer sb.mu.RUnlock()
	return sb.Capabilities
}

// RegisterCapability registers a native capability namespace.
func (sb *ScriptBinding) RegisterCapability(cap RegisteredCapability) error {
	return sb.capabilities().Register(cap)
}

// DescribeCapability returns a registered capability descriptor.
func (sb *ScriptBinding) DescribeCapability(name string) (CapabilityDesc, bool) {
	reg := sb.capabilitiesReadOnly()
	if reg == nil {
		return CapabilityDesc{}, false
	}
	return reg.Describe(name)
}

// DescribeCapabilities returns all registered capability descriptors.
func (sb *ScriptBinding) DescribeCapabilities() []CapabilityDesc {
	reg := sb.capabilitiesReadOnly()
	if reg == nil {
		return nil
	}
	return reg.DescribeAll()
}

// FindCapabilityObject looks up a native capability exported object descriptor.
func (sb *ScriptBinding) FindCapabilityObject(capabilityName, objectName string) (schema.ObjectDesc, bool) {
	reg := sb.capabilitiesReadOnly()
	if reg == nil {
		return schema.ObjectDesc{}, false
	}
	return reg.FindObject(capabilityName, objectName)
}

// FindCapabilityInterface looks up a native capability exported interface descriptor.
func (sb *ScriptBinding) FindCapabilityInterface(capabilityName, interfaceName string) (schema.InterfaceDesc, bool) {
	reg := sb.capabilitiesReadOnly()
	if reg == nil {
		return schema.InterfaceDesc{}, false
	}
	return reg.FindInterface(capabilityName, interfaceName)
}

// FindCapabilityTypeAlias looks up a native capability exported type alias.
func (sb *ScriptBinding) FindCapabilityTypeAlias(capabilityName, aliasName string) (schema.TypeDesc, bool) {
	reg := sb.capabilitiesReadOnly()
	if reg == nil {
		return schema.TypeDesc{}, false
	}
	return reg.FindTypeAlias(capabilityName, aliasName)
}

// FindCapabilityValue looks up a native capability exported value.
func (sb *ScriptBinding) FindCapabilityValue(capabilityName, valueName string) (CapabilityValueDesc, any, bool) {
	reg := sb.capabilitiesReadOnly()
	if reg == nil {
		return CapabilityValueDesc{}, nil, false
	}
	return reg.FindValue(capabilityName, valueName)
}

// InvokeCapability dispatches a native capability callable.
func (sb *ScriptBinding) InvokeCapability(ctx context.Context, capabilityName, callableName string, input any) (any, error) {
	reg := sb.capabilitiesReadOnly()
	if reg == nil {
		return nil, fmt.Errorf("capability registry is required")
	}
	return reg.Invoke(ctx, capabilityName, callableName, input)
}

// ExposeCapabilityCallables registers flattened callable descriptors and adapters for a capability.
func (sb *ScriptBinding) ExposeCapabilityCallables(capabilityName string) error {
	reg := sb.capabilitiesReadOnly()
	if reg == nil {
		return fmt.Errorf("capability registry is required")
	}
	desc, ok := reg.Describe(capabilityName)
	if !ok {
		return fmt.Errorf("capability %q is not registered", capabilityName)
	}
	entries := make([]exposedCapabilityCallable, 0, len(desc.Callables))
	for _, callableDesc := range desc.Callables {
		callable, ok := reg.FindCallable(capabilityName, callableDesc.Name)
		if !ok {
			return fmt.Errorf("capability %q callable %q is not registered", capabilityName, callableDesc.Name)
		}
		flattened := cloneCapabilityCallableDesc(capabilityName, callableDesc)
		if existing, exists := sb.Callables.Lookup(flattened.Name); exists {
			if !callableDescriptorsEqual(existing, flattened) {
				return fmt.Errorf("callable %q descriptor conflicts with existing registration", flattened.Name)
			}
			if _, adapterExists := sb.Executors.Lookup(flattened.Name); adapterExists {
				return fmt.Errorf("callable %q adapter already registered", flattened.Name)
			}
		} else {
			if _, adapterExists := sb.Executors.Lookup(flattened.Name); adapterExists {
				return fmt.Errorf("callable %q adapter already registered", flattened.Name)
			}
		}
		adapter, err := NewCapabilityExecutableAdapter(capabilityName, callable)
		if err != nil {
			return err
		}
		entries = append(entries, exposedCapabilityCallable{desc: flattened, adapter: adapter})
	}
	for _, entry := range entries {
		if _, exists := sb.Callables.Lookup(entry.desc.Name); !exists {
			if err := sb.Callables.Register(entry.desc); err != nil {
				return err
			}
		}
		if err := sb.Executors.RegisterAdapter(entry.adapter); err != nil {
			return err
		}
	}
	return nil
}

type exposedCapabilityCallable struct {
	desc    schema.CallableDesc
	adapter ExecutableAdapter
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
