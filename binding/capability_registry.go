package binding

import (
	"context"
	"fmt"
	"sync"

	"github.com/qomos-w/spore/schema"
)

// MemoryCapabilityRegistry stores capabilities in memory.
type MemoryCapabilityRegistry struct {
	mu      sync.RWMutex
	caps    map[string]RegisteredCapability
	ordered []string
}

// NewMemoryCapabilityRegistry creates an empty capability registry.
func NewMemoryCapabilityRegistry() *MemoryCapabilityRegistry {
	return &MemoryCapabilityRegistry{
		caps:    make(map[string]RegisteredCapability),
		ordered: make([]string, 0),
	}
}

func (r *MemoryCapabilityRegistry) Register(cap RegisteredCapability) error {
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
	if _, exists := r.caps[cap.Desc.Name]; exists {
		return fmt.Errorf("capability %q already registered", cap.Desc.Name)
	}
	cloned := cloneRegisteredCapability(cap)
	r.caps[cap.Desc.Name] = cloned
	r.ordered = append(r.ordered, cap.Desc.Name)
	return nil
}

func (r *MemoryCapabilityRegistry) Describe(name string) (CapabilityDesc, bool) {
	r.mu.RLock()
	cap, ok := r.caps[name]
	r.mu.RUnlock()
	if !ok {
		return CapabilityDesc{}, false
	}
	return cloneCapabilityDesc(cap.Desc), true
}

func (r *MemoryCapabilityRegistry) DescribeAll() []CapabilityDesc {
	r.mu.RLock()
	defer r.mu.RUnlock()
	list := make([]CapabilityDesc, 0, len(r.ordered))
	for _, name := range r.ordered {
		list = append(list, cloneCapabilityDesc(r.caps[name].Desc))
	}
	return list
}

func (r *MemoryCapabilityRegistry) FindCallable(capabilityName, callableName string) (CapabilityCallable, bool) {
	r.mu.RLock()
	cap, ok := r.caps[capabilityName]
	r.mu.RUnlock()
	if !ok {
		return nil, false
	}
	callable, ok := cap.Callables[callableName]
	return callable, ok
}

func (r *MemoryCapabilityRegistry) FindObject(capabilityName, objectName string) (schema.ObjectDesc, bool) {
	r.mu.RLock()
	cap, ok := r.caps[capabilityName]
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

func (r *MemoryCapabilityRegistry) FindInterface(capabilityName, interfaceName string) (schema.InterfaceDesc, bool) {
	r.mu.RLock()
	cap, ok := r.caps[capabilityName]
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

func (r *MemoryCapabilityRegistry) FindTypeAlias(capabilityName, aliasName string) (schema.TypeDesc, bool) {
	r.mu.RLock()
	cap, ok := r.caps[capabilityName]
	r.mu.RUnlock()
	if !ok {
		return schema.TypeDesc{}, false
	}
	if td, ok := cap.Desc.TypeAliases[aliasName]; ok {
		return td, true
	}
	return schema.TypeDesc{}, false
}

func (r *MemoryCapabilityRegistry) FindValue(capabilityName, valueName string) (CapabilityValueDesc, any, bool) {
	r.mu.RLock()
	cap, ok := r.caps[capabilityName]
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

func (r *MemoryCapabilityRegistry) Invoke(ctx context.Context, capabilityName, callableName string, input any) (any, error) {
	return r.InvokeAuthorized(AuthorizedInvocation{Context: ctx}, capabilityName, callableName, input)
}

// InvokeAuthorized applies the registered capability policy before invocation.
func (r *MemoryCapabilityRegistry) InvokeAuthorized(inv AuthorizedInvocation, capabilityName, callableName string, input any) (any, error) {
	callable, ok := r.FindCallable(capabilityName, callableName)
	if !ok {
		if _, capOK := r.Describe(capabilityName); !capOK {
			return nil, fmt.Errorf("capability %q is not registered", capabilityName)
		}
		return nil, fmt.Errorf("capability %q callable %q is not registered", capabilityName, callableName)
	}
	r.mu.RLock()
	cap := r.caps[capabilityName]
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
