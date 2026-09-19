package invoke

import (
	"github.com/qomos-w/spore/schema"
)

// CapabilityDesc is the host-facing description of a native capability.
// Contract: public semantic contract — capability surface description.
type CapabilityDesc struct {
	Name        string
	Kind        string
	Callables   []schema.CallableDesc
	Objects     []schema.ObjectDesc
	Interfaces  []schema.InterfaceDesc
	TypeAliases map[string]schema.TypeDesc
	Values      []CapabilityValueDesc
	Pipelines   []PipelineDesc
	Metadata    map[string]string
}

// CapabilityValueDesc describes a read-only native value exported by a capability.
// Contract: public semantic contract — capability value description.
type CapabilityValueDesc struct {
	Name string
	Type schema.TypeDesc
}
