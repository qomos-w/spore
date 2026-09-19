package binding

import (
	"fmt"
	"reflect"

	"github.com/qomos-w/spore/schema"
)

// CapabilityBuilder collects schema-described callables, objects, type aliases, and pipelines under one capability namespace.
type CapabilityBuilder struct {
	name        string
	kind        string
	metadata    map[string]string
	callables   map[string]CapabilityCallable
	objects     []schema.ObjectDesc
	interfaces  []schema.InterfaceDesc
	typeAliases map[string]schema.TypeDesc
	values      map[string]any
	valueDescs  []CapabilityValueDesc
	pipelines   []PipelineDesc
}

// NewCapability creates a builder for a native capability.
func NewCapability(name, kind string) *CapabilityBuilder {
	return &CapabilityBuilder{
		name:        name,
		kind:        kind,
		metadata:    make(map[string]string),
		callables:   make(map[string]CapabilityCallable),
		typeAliases: make(map[string]schema.TypeDesc),
		values:      make(map[string]any),
	}
}

func (b *CapabilityBuilder) WithMetadata(key, value string) *CapabilityBuilder {
	if key != "" {
		b.metadata[key] = value
	}
	return b
}

func (b *CapabilityBuilder) AddFunction(name string, fn any) error {
	if name == "" {
		return fmt.Errorf("callable name cannot be empty")
	}
	if _, exists := b.callables[name]; exists {
		return fmt.Errorf("capability %q callable %q already registered", b.name, name)
	}
	callable, err := wrapCapabilityFunction(name, fn)
	if err != nil {
		return err
	}
	b.callables[name] = callable
	return nil
}

// AddFreeFunction registers a free-form Go function as a capability callable.
// Unlike AddFunction, this does not require struct-in/struct-out shapes.
//
// Supported signatures:
//
//	func(T1, T2, ...) R
//	func(T1, T2, ...) (R, error)
//	func() R
//	func() (R, error)
//
// Rejected shapes:
//   - variadic functions (func(...T))
//   - context.Context parameters
//   - interface{} / any parameters (schema.DescribeReflectType limitation)
//   - multiple non-error returns
//   - pointer parameters or returns
//
// Parameter names in the generated schema descriptor are "arg0", "arg1", etc.
// because Go reflection does not expose function parameter names.
// Use AddFunction for struct-in/struct-out shapes that need stable field names.
func (b *CapabilityBuilder) AddFreeFunction(name string, fn any) error {
	if name == "" {
		return fmt.Errorf("callable name cannot be empty")
	}
	if _, exists := b.callables[name]; exists {
		return fmt.Errorf("capability %q callable %q already registered", b.name, name)
	}
	callable, err := wrapFreeFunction(name, fn)
	if err != nil {
		return err
	}
	b.callables[name] = callable
	return nil
}

// AddInterface registers a schema-described interface exported by this capability.
func (b *CapabilityBuilder) AddInterface(name string, iface schema.InterfaceDesc) error {
	if name == "" {
		return fmt.Errorf("interface name cannot be empty")
	}
	for _, existing := range b.interfaces {
		if existing.Name == name {
			return fmt.Errorf("capability %q interface %q already registered", b.name, name)
		}
	}
	iface.Name = name
	b.interfaces = append(b.interfaces, schema.CloneInterfaceDesc(iface))
	return nil
}

// AddObject registers a schema-described object (struct) exported by this capability.
func (b *CapabilityBuilder) AddObject(name string, obj schema.ObjectDesc) error {
	if name == "" {
		return fmt.Errorf("object name cannot be empty")
	}
	for _, existing := range b.objects {
		if existing.Name == name {
			return fmt.Errorf("capability %q object %q already registered", b.name, name)
		}
	}
	obj.Name = name
	b.objects = append(b.objects, obj)
	return nil
}

// AddTypeAlias registers a compile-time type alias exported by this capability.
func (b *CapabilityBuilder) AddTypeAlias(name string, td schema.TypeDesc) error {
	if name == "" {
		return fmt.Errorf("type alias name cannot be empty")
	}
	if _, exists := b.typeAliases[name]; exists {
		return fmt.Errorf("capability %q type alias %q already registered", b.name, name)
	}
	b.typeAliases[name] = td
	return nil
}

// AddValue registers a read-only value exported by this capability.
func (b *CapabilityBuilder) AddValue(name string, value any) error {
	if name == "" {
		return fmt.Errorf("value name cannot be empty")
	}
	if value == nil {
		return fmt.Errorf("capability %q value %q cannot be nil", b.name, name)
	}
	if _, exists := b.values[name]; exists {
		return fmt.Errorf("capability %q value %q already registered", b.name, name)
	}
	td, err := schema.DescribeReflectType(reflect.TypeOf(value))
	if err != nil {
		return err
	}
	b.values[name] = value
	b.valueDescs = append(b.valueDescs, CapabilityValueDesc{Name: name, Type: td})
	return nil
}

// AddPipeline registers a pipeline descriptor exported by this capability.
func (b *CapabilityBuilder) AddPipeline(name string, desc PipelineDesc) error {
	if name == "" {
		return fmt.Errorf("pipeline name cannot be empty")
	}
	for _, existing := range b.pipelines {
		if existing.Name == name {
			return fmt.Errorf("capability %q pipeline %q already registered", b.name, name)
		}
	}
	desc.Name = name
	b.pipelines = append(b.pipelines, desc)
	return nil
}

// Clone returns a deep copy of the builder. The cloned builder shares the
// same Go function and value references (callables/values) but has its own
// independent metadata/objects/typeAliases/pipelines maps and slices.
func (b *CapabilityBuilder) Clone() *CapabilityBuilder {
	if b == nil {
		return nil
	}
	cloned := NewCapability(b.name, b.kind)
	for k, v := range b.metadata {
		cloned.metadata[k] = v
	}
	for name, callable := range b.callables {
		cloned.callables[name] = callable
	}
	cloned.objects = make([]schema.ObjectDesc, len(b.objects))
	for i, obj := range b.objects {
		cloned.objects[i] = schema.CloneObjectDesc(obj)
	}
	cloned.interfaces = make([]schema.InterfaceDesc, len(b.interfaces))
	for i, iface := range b.interfaces {
		cloned.interfaces[i] = schema.CloneInterfaceDesc(iface)
	}
	for name, td := range b.typeAliases {
		cloned.typeAliases[name] = schema.CloneTypeDesc(td)
	}
	for name, val := range b.values {
		cloned.values[name] = val
	}
	cloned.valueDescs = make([]CapabilityValueDesc, len(b.valueDescs))
	copy(cloned.valueDescs, b.valueDescs)
	cloned.pipelines = make([]PipelineDesc, len(b.pipelines))
	for i, pl := range b.pipelines {
		cloned.pipelines[i] = clonePipelineDesc(pl)
	}
	return cloned
}

func (b *CapabilityBuilder) Build() (RegisteredCapability, error) {
	if b.name == "" {
		return RegisteredCapability{}, fmt.Errorf("capability name cannot be empty")
	}
	if len(b.callables) == 0 && len(b.objects) == 0 && len(b.interfaces) == 0 && len(b.typeAliases) == 0 && len(b.values) == 0 && len(b.pipelines) == 0 {
		return RegisteredCapability{}, fmt.Errorf("capability %q must define at least one callable, object, interface, type alias, value, or pipeline", b.name)
	}
	desc := CapabilityDesc{
		Name:        b.name,
		Kind:        b.kind,
		Callables:   make([]schema.CallableDesc, 0, len(b.callables)),
		Objects:     make([]schema.ObjectDesc, 0, len(b.objects)),
		Interfaces:  make([]schema.InterfaceDesc, 0, len(b.interfaces)),
		TypeAliases: make(map[string]schema.TypeDesc, len(b.typeAliases)),
		Values:      make([]CapabilityValueDesc, len(b.valueDescs)),
		Pipelines:   make([]PipelineDesc, len(b.pipelines)),
	}
	if len(b.metadata) > 0 {
		desc.Metadata = make(map[string]string, len(b.metadata))
		for key, value := range b.metadata {
			desc.Metadata[key] = value
		}
	}
	for _, callable := range b.callables {
		desc.Callables = append(desc.Callables, callable.Desc())
	}
	for _, obj := range b.objects {
		desc.Objects = append(desc.Objects, schema.CloneObjectDesc(obj))
	}
	for _, iface := range b.interfaces {
		desc.Interfaces = append(desc.Interfaces, schema.CloneInterfaceDesc(iface))
	}
	for name, td := range b.typeAliases {
		desc.TypeAliases[name] = td
	}
	copy(desc.Values, b.valueDescs)
	for i, pl := range b.pipelines {
		desc.Pipelines[i] = clonePipelineDesc(pl)
	}
	cap := RegisteredCapability{
		Desc:      desc,
		Callables: make(map[string]CapabilityCallable, len(b.callables)),
		Values:    make(map[string]any, len(b.values)),
	}
	for name, callable := range b.callables {
		cap.Callables[name] = callable
	}
	for name, value := range b.values {
		cap.Values[name] = value
	}
	if err := validateCapabilityRegistration(cap); err != nil {
		return RegisteredCapability{}, err
	}
	return cap, nil
}
