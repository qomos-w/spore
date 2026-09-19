package binding

// PipelineDesc is a host-facing descriptor for a pipeline declaration.
// It is line-free and serializable for transport to hosting systems like myxos.
type PipelineDesc struct {
	Name  string
	Steps []PipelineStepDesc
}

// PipelineStepDesc describes a single step in a pipeline descriptor.
type PipelineStepDesc struct {
	Name      string
	Invoke    string
	Input     map[string]any
	DependsOn []string
	Timeout   string
	When      string
	Parallel  []PipelineStepDesc
}

// PipelineRef is a host-visible marker for a reference expression value
// within a PipelineStepDesc.Input map.
type PipelineRef struct {
	Expr string
}
