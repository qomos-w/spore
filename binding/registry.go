package binding

import (
	"context"
	"fmt"
	"sync"

	"github.com/qomos-w/spore/invoke"
	"github.com/qomos-w/spore/schema"
)

// Registry is the single registration store of the Script Binding plane.
//
// It replaces the three previously independent registries — the callable
// registry, the executable registry, and the capability registry — with one
// store guarded by one lock, so name-collision rules, descriptor validation,
// and deep-clone boundaries are defined once and cannot drift between planes.
// The previous design had two registries that each owned a descriptor and a
// third that owned a namespace; registration state and validation lived in
// three places and the capability exposure path had to re-clone and re-compare
// descriptors across them. Here every plane shares the same lock and the same
// descriptor-comparison routine.
//
// The three planes are exposed as explicitly separated surfaces:
//
//   - callables:      RegisterCallable / RegisterGoFunction / LookupCallable /
//                     LookupCallableRef / ListCallables
//   - executables:    RegisterAdapter / LookupAdapter /
//                     ForEachAdapter / Invoke / ExposeCapabilityCallables
//   - capabilities:   RegisterCapability / DescribeCapability / DescribeCapabilities /
//                     Find* / InvokeCapability / InvokeCapabilityAuthorized
//
// Registry is deliberately not itself the engine-facing contract: the engine's
// invoke.CallableSource and invoke.ExecutorSource both declare a method named
// Lookup with incompatible result types. CallableSource and ExecutorSource
// return the two typed views the engine consumes, so the invoke package
// contract is the common interface the whole plane is described by.
type Registry struct {
	mu sync.RWMutex

	callables     map[string]schema.CallableDesc
	callableOrder []string

	adapters map[string]ExecutableAdapter

	capabilities    map[string]RegisteredCapability
	capabilityOrder []string
}

// NewRegistry creates an empty unified registry.
func NewRegistry() *Registry {
	return &Registry{
		callables:       make(map[string]schema.CallableDesc),
		callableOrder:   make([]string, 0),
		adapters:        make(map[string]ExecutableAdapter),
		capabilities:    make(map[string]RegisteredCapability),
		capabilityOrder: make([]string, 0),
	}
}

// --- Callable plane ---

// RegisterCallable adds a callable descriptor to the registry. Returns an
// error if the descriptor is invalid or the name is already registered.
func (r *Registry) RegisterCallable(desc schema.CallableDesc) error {
	if err := schema.ValidateCallableDesc(desc); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.callables[desc.Name]; exists {
		return fmt.Errorf("callable %q already registered", desc.Name)
	}
	copied := schema.CloneCallableDesc(desc)
	r.callables[desc.Name] = copied
	r.callableOrder = append(r.callableOrder, copied.Name)
	return nil
}

// RegisterGoFunction describes a Go function via reflection and registers it
// as a callable descriptor.
func (r *Registry) RegisterGoFunction(name string, fn any) (schema.CallableDesc, error) {
	desc, err := schema.DescribeGoFunction(name, fn)
	if err != nil {
		return schema.CallableDesc{}, err
	}
	if err := r.RegisterCallable(desc); err != nil {
		return schema.CallableDesc{}, err
	}
	return schema.CloneCallableDesc(desc), nil
}

// LookupCallable returns a deep-cloned callable descriptor for the given name.
func (r *Registry) LookupCallable(name string) (schema.CallableDesc, bool) {
	r.mu.RLock()
	desc, ok := r.callables[name]
	r.mu.RUnlock()
	if !ok {
		return schema.CallableDesc{}, false
	}
	return schema.CloneCallableDesc(desc), true
}

// LookupCallableRef returns the stored descriptor without cloning. The returned
// value must be treated as immutable — its slices are shared with the registry.
// Hot dispatch paths use this to avoid per-invoke deep clones.
func (r *Registry) LookupCallableRef(name string) (schema.CallableDesc, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	desc, ok := r.callables[name]
	return desc, ok
}

// ListCallables returns all registered callable descriptors in registration order.
func (r *Registry) ListCallables() []schema.CallableDesc {
	r.mu.RLock()
	defer r.mu.RUnlock()
	list := make([]schema.CallableDesc, 0, len(r.callableOrder))
	for _, name := range r.callableOrder {
		list = append(list, schema.CloneCallableDesc(r.callables[name]))
	}
	return list
}

// --- Executable plane ---

// RegisterAdapter registers an executable adapter for a callable that is
// already in the registry. The adapter's descriptor must match the registered
// descriptor.
func (r *Registry) RegisterAdapter(adapter ExecutableAdapter) error {
	if adapter == nil {
		return fmt.Errorf("adapter is required")
	}
	desc := adapter.Callable()
	r.mu.Lock()
	defer r.mu.Unlock()
	registered, ok := r.callables[desc.Name]
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

// ExposeCapabilityCallables registers the flattened callable descriptors and
// executable adapters for a registered capability.
//
// The whole exposure is one transaction under the registry lock: a conflict on
// any callable leaves no partial registration behind. Because the callable and
// executable planes now share one store, the descriptor-consistency check and
// the adapter registration happen together instead of being split across two
// registries with a clone-and-compare round trip at the call site.
func (r *Registry) ExposeCapabilityCallables(capabilityName string) error {
	r.mu.RLock()
	cap, ok := r.capabilities[capabilityName]
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("capability %q is not registered", capabilityName)
	}

	type exposure struct {
		desc    schema.CallableDesc
		adapter ExecutableAdapter
	}
	exposures := make([]exposure, 0, len(cap.Desc.Callables))
	for _, callableDesc := range cap.Desc.Callables {
		callable, ok := cap.Callables[callableDesc.Name]
		if !ok {
			return fmt.Errorf("capability %q callable %q is not registered", capabilityName, callableDesc.Name)
		}
		adapter, err := NewCapabilityExecutableAdapter(capabilityName, callable)
		if err != nil {
			return err
		}
		exposures = append(exposures, exposure{desc: adapter.Callable(), adapter: adapter})
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	// Validate every entry before mutating anything, so a conflict cannot
	// leave a partially exposed capability.
	for _, e := range exposures {
		if _, exists := r.adapters[e.desc.Name]; exists {
			return fmt.Errorf("callable %q adapter already registered", e.desc.Name)
		}
		if registered, exists := r.callables[e.desc.Name]; exists {
			if !callableDescriptorsEqual(registered, e.desc) {
				return fmt.Errorf("callable %q descriptor conflicts with existing registration", e.desc.Name)
			}
		}
	}
	for _, e := range exposures {
		if _, exists := r.callables[e.desc.Name]; !exists {
			copied := schema.CloneCallableDesc(e.desc)
			r.callables[e.desc.Name] = copied
			r.callableOrder = append(r.callableOrder, copied.Name)
		}
		r.adapters[e.desc.Name] = e.adapter
	}
	return nil
}

// LookupAdapter returns the executable adapter for the given callable name.
func (r *Registry) LookupAdapter(name string) (ExecutableAdapter, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	adapter, ok := r.adapters[name]
	return adapter, ok
}

// ForEachAdapter visits registered executable adapters until fn returns false.
// The adapter set is snapshotted under the read lock so a callback that touches
// the registry cannot deadlock against the iteration.
func (r *Registry) ForEachAdapter(fn func(name string, adapter ExecutableAdapter) bool) {
	if r == nil || fn == nil {
		return
	}
	r.mu.RLock()
	snapshot := make(map[string]ExecutableAdapter, len(r.adapters))
	for name, adapter := range r.adapters {
		snapshot[name] = adapter
	}
	r.mu.RUnlock()
	for name, adapter := range snapshot {
		if !fn(name, adapter) {
			return
		}
	}
}

// Invoke dispatches an invocation request to the appropriate adapter.
//
// A declared MaxDuration budget is applied here, once, as a context deadline:
// an earlier caller deadline is preserved, otherwise the adapter receives a
// context whose deadline is now+MaxDuration. This mirrors
// script.CallContext.context() so the duration budget flows through ctx
// instead of being enforced twice.
func (r *Registry) Invoke(req InvocationRequest) (InvocationOutcome, error) {
	if req.Budget.MaxDuration > 0 {
		ctx := req.Context
		if ctx == nil {
			ctx = context.Background()
		}
		child, cancel := context.WithTimeout(ctx, req.Budget.MaxDuration)
		defer cancel()
		req.Context = child
	}
	desc, ok := r.LookupCallableRef(req.Callable)
	if !ok {
		return InvocationOutcome{}, fmt.Errorf("callable %q is not registered", req.Callable)
	}
	if err := ValidateInvocationStage(desc, req.Stage); err != nil {
		return InvocationOutcome{}, err
	}
	adapter, ok := r.LookupAdapter(req.Callable)
	if !ok {
		return InvocationOutcome{}, fmt.Errorf("callable %q has no executable adapter", req.Callable)
	}
	return adapter.Invoke(req)
}

// --- Capability plane ---

// RegisterCapability registers a native capability namespace.
func (r *Registry) RegisterCapability(cap RegisteredCapability) error {
	if cap.Desc.Name == "" {
		return fmt.Errorf("capability name cannot be empty")
	}
	if len(cap.Callables) == 0 && len(cap.Desc.Objects) == 0 && len(cap.Desc.Interfaces) == 0 && len(cap.Desc.TypeAliases) == 0 && len(cap.Desc.Values) == 0 {
		return fmt.Errorf("capability %q must define at least one callable, object, interface, type alias, or value", cap.Desc.Name)
	}
	if err := validateCapabilityRegistration(cap); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.capabilities[cap.Desc.Name]; exists {
		return fmt.Errorf("capability %q already registered", cap.Desc.Name)
	}
	cloned := cloneRegisteredCapability(cap)
	r.capabilities[cap.Desc.Name] = cloned
	r.capabilityOrder = append(r.capabilityOrder, cap.Desc.Name)
	return nil
}

// DescribeCapability returns a registered capability descriptor.
func (r *Registry) DescribeCapability(name string) (CapabilityDesc, bool) {
	r.mu.RLock()
	cap, ok := r.capabilities[name]
	r.mu.RUnlock()
	if !ok {
		return CapabilityDesc{}, false
	}
	return cloneCapabilityDesc(cap.Desc), true
}

// DescribeCapabilities returns all registered capability descriptors.
func (r *Registry) DescribeCapabilities() []CapabilityDesc {
	r.mu.RLock()
	defer r.mu.RUnlock()
	list := make([]CapabilityDesc, 0, len(r.capabilityOrder))
	for _, name := range r.capabilityOrder {
		list = append(list, cloneCapabilityDesc(r.capabilities[name].Desc))
	}
	return list
}

// FindCapabilityCallable looks up a capability callable by capability and local name.
func (r *Registry) FindCapabilityCallable(capabilityName, callableName string) (CapabilityCallable, bool) {
	r.mu.RLock()
	cap, ok := r.capabilities[capabilityName]
	r.mu.RUnlock()
	if !ok {
		return nil, false
	}
	callable, ok := cap.Callables[callableName]
	return callable, ok
}

// FindCapabilityObject looks up a capability exported object descriptor.
func (r *Registry) FindCapabilityObject(capabilityName, objectName string) (schema.ObjectDesc, bool) {
	r.mu.RLock()
	cap, ok := r.capabilities[capabilityName]
	r.mu.RUnlock()
	if !ok {
		return schema.ObjectDesc{}, false
	}
	for _, obj := range cap.Desc.Objects {
		if obj.Name == objectName {
			return schema.CloneObjectDesc(obj), true
		}
	}
	return schema.ObjectDesc{}, false
}

// FindCapabilityInterface looks up a capability exported interface descriptor.
func (r *Registry) FindCapabilityInterface(capabilityName, interfaceName string) (schema.InterfaceDesc, bool) {
	r.mu.RLock()
	cap, ok := r.capabilities[capabilityName]
	r.mu.RUnlock()
	if !ok {
		return schema.InterfaceDesc{}, false
	}
	for _, iface := range cap.Desc.Interfaces {
		if iface.Name == interfaceName {
			return schema.CloneInterfaceDesc(iface), true
		}
	}
	return schema.InterfaceDesc{}, false
}

// FindCapabilityTypeAlias looks up a capability exported type alias.
func (r *Registry) FindCapabilityTypeAlias(capabilityName, aliasName string) (schema.TypeDesc, bool) {
	r.mu.RLock()
	cap, ok := r.capabilities[capabilityName]
	r.mu.RUnlock()
	if !ok {
		return schema.TypeDesc{}, false
	}
	if td, ok := cap.Desc.TypeAliases[aliasName]; ok {
		return td, true
	}
	return schema.TypeDesc{}, false
}

// FindCapabilityValue looks up a capability exported value.
func (r *Registry) FindCapabilityValue(capabilityName, valueName string) (CapabilityValueDesc, any, bool) {
	r.mu.RLock()
	cap, ok := r.capabilities[capabilityName]
	r.mu.RUnlock()
	if !ok {
		return CapabilityValueDesc{}, nil, false
	}
	value, ok := cap.Values[valueName]
	if !ok {
		return CapabilityValueDesc{}, nil, false
	}
	for _, desc := range cap.Desc.Values {
		if desc.Name == valueName {
			return desc, value, true
		}
	}
	return CapabilityValueDesc{}, nil, false
}

// InvokeCapability dispatches a native capability callable.
func (r *Registry) InvokeCapability(ctx context.Context, capabilityName, callableName string, input any) (any, error) {
	return r.InvokeCapabilityAuthorized(AuthorizedInvocation{Context: ctx}, capabilityName, callableName, input)
}

// InvokeCapabilityAuthorized applies the registered capability policy before invocation.
func (r *Registry) InvokeCapabilityAuthorized(inv AuthorizedInvocation, capabilityName, callableName string, input any) (any, error) {
	callable, ok := r.FindCapabilityCallable(capabilityName, callableName)
	if !ok {
		if _, capOK := r.DescribeCapability(capabilityName); !capOK {
			return nil, fmt.Errorf("capability %q is not registered", capabilityName)
		}
		return nil, fmt.Errorf("capability %q callable %q is not registered", capabilityName, callableName)
	}
	r.mu.RLock()
	cap := r.capabilities[capabilityName]
	r.mu.RUnlock()
	ctx, cancel, err := authorizedContext(inv, cap.Policy)
	if err != nil {
		return nil, err
	}
	defer cancel()
	if err := CheckExecution(ctx, inv.Budget, nil); err != nil {
		return nil, err
	}
	return callable.Invoke(ctx, input)
}

// validateCapabilityRegistration checks a RegisteredCapability for internal
// consistency before it is admitted into the registry: every advertised
// callable and value must have a matching runtime entry, and each runtime
// callable's descriptor must match its declaration.
func validateCapabilityRegistration(cap RegisteredCapability) error {
	if len(cap.Desc.Callables) != len(cap.Callables) {
		return fmt.Errorf("capability %q descriptor/callable count mismatch", cap.Desc.Name)
	}
	descByName := make(map[string]schema.CallableDesc, len(cap.Desc.Callables))
	for _, desc := range cap.Desc.Callables {
		if desc.Name == "" {
			return fmt.Errorf("capability %q has callable with empty name", cap.Desc.Name)
		}
		if err := schema.ValidateCallableDesc(desc); err != nil {
			return fmt.Errorf("capability %q callable %q invalid: %w", cap.Desc.Name, desc.Name, err)
		}
		if _, exists := descByName[desc.Name]; exists {
			return fmt.Errorf("capability %q callable %q already declared", cap.Desc.Name, desc.Name)
		}
		descByName[desc.Name] = schema.CloneCallableDesc(desc)
	}
	for name, callable := range cap.Callables {
		if name == "" {
			return fmt.Errorf("capability %q has callable with empty runtime name", cap.Desc.Name)
		}
		desc, ok := descByName[name]
		if !ok {
			return fmt.Errorf("capability %q callable %q missing descriptor", cap.Desc.Name, name)
		}
		if !callableDescriptorsEqual(desc, callable.Desc()) {
			return fmt.Errorf("capability %q callable %q descriptor mismatch", cap.Desc.Name, name)
		}
	}
	valueNames := make(map[string]struct{}, len(cap.Desc.Values))
	for _, desc := range cap.Desc.Values {
		if desc.Name == "" {
			return fmt.Errorf("capability %q has value with empty name", cap.Desc.Name)
		}
		if _, exists := valueNames[desc.Name]; exists {
			return fmt.Errorf("capability %q value %q already declared", cap.Desc.Name, desc.Name)
		}
		valueNames[desc.Name] = struct{}{}
		if _, ok := cap.Values[desc.Name]; !ok {
			return fmt.Errorf("capability %q value %q missing runtime value", cap.Desc.Name, desc.Name)
		}
	}
	for name := range cap.Values {
		if _, ok := valueNames[name]; !ok {
			return fmt.Errorf("capability %q value %q missing descriptor", cap.Desc.Name, name)
		}
	}
	return nil
}

// --- Engine-facing views ---

// CallableSource returns the engine-facing read/write view of the callable plane.
func (r *Registry) CallableSource() invoke.CallableSource {
	if r == nil {
		return nil
	}
	return callableView{reg: r}
}

// ExecutorSource returns the engine-facing view of the executable plane.
func (r *Registry) ExecutorSource() invoke.ExecutorSource {
	if r == nil {
		return nil
	}
	return executorView{reg: r}
}

// callableView satisfies invoke.CallableSource over the unified registry.
type callableView struct{ reg *Registry }

func (v callableView) Register(desc schema.CallableDesc) error { return v.reg.RegisterCallable(desc) }

func (v callableView) Lookup(name string) (schema.CallableDesc, bool) { return v.reg.LookupCallable(name) }

func (v callableView) List() []schema.CallableDesc { return v.reg.ListCallables() }

// executorView satisfies invoke.ExecutorSource over the unified registry.
type executorView struct{ reg *Registry }

func (v executorView) Lookup(name string) (ExecutableAdapter, bool) { return v.reg.LookupAdapter(name) }

func (v executorView) RegisterAdapter(adapter ExecutableAdapter) error {
	return v.reg.RegisterAdapter(adapter)
}

func (v executorView) ForEachAdapter(fn func(name string, adapter ExecutableAdapter) bool) {
	v.reg.ForEachAdapter(fn)
}

var (
	_ invoke.CallableSource = callableView{}
	_ invoke.ExecutorSource = executorView{}
)

// --- Descriptor comparison ---

// callableDescriptorsEqual reports whether two callable descriptors describe
// the same callable. It reads fields directly and does not clone: comparison
// is read-only, so the deep clones the previous implementation performed here
// were pure overhead on every registration.
func callableDescriptorsEqual(left, right schema.CallableDesc) bool {
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
