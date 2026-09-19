package binding

import (
	"context"

	"github.com/qomos-w/spore/schema"
)

// CapabilityCallable is a schema-described native callable inside a capability.
type CapabilityCallable interface {
	Desc() schema.CallableDesc
	Invoke(ctx context.Context, input any) (any, error)
}

// RegisteredCapability is the registration unit for a native capability.
type RegisteredCapability struct {
	Desc      CapabilityDesc
	Policy    CapabilityPolicy
	Callables map[string]CapabilityCallable
	Values    map[string]any
}

// The capability plane is stored by the unified Registry (registry.go); the
// capability descriptors and callable contract below remain the registration
// units that plane consumes.

func cloneCapabilityDesc(desc CapabilityDesc) CapabilityDesc {
	cloned := CapabilityDesc{
		Name:       desc.Name,
		Kind:       desc.Kind,
		Callables:  make([]schema.CallableDesc, 0, len(desc.Callables)),
		Objects:    make([]schema.ObjectDesc, 0, len(desc.Objects)),
		Interfaces: make([]schema.InterfaceDesc, 0, len(desc.Interfaces)),
		Values:     make([]CapabilityValueDesc, len(desc.Values)),
		Pipelines:  make([]PipelineDesc, len(desc.Pipelines)),
	}
	for _, callable := range desc.Callables {
		cloned.Callables = append(cloned.Callables, schema.CloneCallableDesc(callable))
	}
	for _, obj := range desc.Objects {
		cloned.Objects = append(cloned.Objects, schema.CloneObjectDesc(obj))
	}
	for _, iface := range desc.Interfaces {
		cloned.Interfaces = append(cloned.Interfaces, schema.CloneInterfaceDesc(iface))
	}
	copy(cloned.Values, desc.Values)
	if len(desc.TypeAliases) > 0 {
		cloned.TypeAliases = make(map[string]schema.TypeDesc, len(desc.TypeAliases))
		for key, value := range desc.TypeAliases {
			cloned.TypeAliases[key] = value
		}
	}
	if len(desc.Metadata) > 0 {
		cloned.Metadata = make(map[string]string, len(desc.Metadata))
		for key, value := range desc.Metadata {
			cloned.Metadata[key] = value
		}
	}
	for i, pl := range desc.Pipelines {
		cloned.Pipelines[i] = clonePipelineDesc(pl)
	}
	return cloned
}

func clonePipelineDesc(desc PipelineDesc) PipelineDesc {
	cloned := PipelineDesc{
		Name:  desc.Name,
		Steps: make([]PipelineStepDesc, len(desc.Steps)),
	}
	for i, step := range desc.Steps {
		cloned.Steps[i] = clonePipelineStepDesc(step)
	}
	return cloned
}

func clonePipelineStepDesc(step PipelineStepDesc) PipelineStepDesc {
	cloned := PipelineStepDesc{
		Name:      step.Name,
		Invoke:    step.Invoke,
		DependsOn: make([]string, len(step.DependsOn)),
		Timeout:   step.Timeout,
		When:      step.When,
	}
	copy(cloned.DependsOn, step.DependsOn)
	if len(step.Input) > 0 {
		cloned.Input = make(map[string]any, len(step.Input))
		for k, v := range step.Input {
			cloned.Input[k] = v
		}
	}
	if len(step.Parallel) > 0 {
		cloned.Parallel = make([]PipelineStepDesc, len(step.Parallel))
		for i, child := range step.Parallel {
			cloned.Parallel[i] = clonePipelineStepDesc(child)
		}
	}
	return cloned
}

func cloneRegisteredCapability(cap RegisteredCapability) RegisteredCapability {
	cloned := RegisteredCapability{
		Desc:      cloneCapabilityDesc(cap.Desc),
		Policy:    cap.Policy,
		Callables: make(map[string]CapabilityCallable, len(cap.Callables)),
		Values:    make(map[string]any, len(cap.Values)),
	}
	for name, callable := range cap.Callables {
		cloned.Callables[name] = callable
	}
	for name, value := range cap.Values {
		cloned.Values[name] = value
	}
	return cloned
}
