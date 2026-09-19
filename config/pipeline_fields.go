package config

import (
	"fmt"
	"sort"
	"strings"
)

// Pipeline step DSL field names. These constants are the single source of
// truth for the step_field grammar: the parser, the parser's unknown-field
// diagnostic and the pipeline validators all read them (directly or through
// pipelineStepFields below), so renaming a field is a one-line change that
// reaches both sides at once.
//
// TestPipelineStepFields_SingleSourceTable and
// TestPipelineStepFieldNames_HaveOneDefinition pin that invariant: they fail if
// a field name is reintroduced as a literal in the parser or the validators.
const (
	PipelineStepFieldInvoke    = "invoke"
	PipelineStepFieldInput     = "input"
	PipelineStepFieldDependsOn = "depends_on"
	PipelineStepFieldTimeout   = "timeout"
	PipelineStepFieldWhen      = "when"
)

// Value forms of a step field: the grammar shape a field's value must have.
const (
	stepFormString      = "string"       // "..." literal
	stepFormValue       = "value"        // any value (map, struct literal, scalar, ref)
	stepFormStringArray = "string_array" // ["a", "b"]
	stepFormRef         = "ref"          // stepName.field
)

// pipelineStepFieldSpec describes one field accepted inside a step block.
type pipelineStepFieldSpec struct {
	Name      string // canonical field name (a PipelineStepField* constant)
	ValueForm string // a stepForm* constant
	Required  bool
	Summary   string // one-line description used in diagnostics
	Hint      string // repair hint quoted by validators; empty when self-evident
}

// pipelineStepFields is the canonical step-field table, in grammar order. It is
// the only place a step field name is written down.
var pipelineStepFields = []pipelineStepFieldSpec{
	{
		Name:      PipelineStepFieldInvoke,
		ValueForm: stepFormString,
		Required:  true,
		Summary:   `capability callable reference "<schemaID>:<callableName>"`,
		Hint:      `add invoke: "<schemaID>:<callableName>" to the step`,
	},
	{
		Name:      PipelineStepFieldInput,
		ValueForm: stepFormValue,
		Summary:   "callable input map or struct literal",
	},
	{
		Name:      PipelineStepFieldDependsOn,
		ValueForm: stepFormStringArray,
		Summary:   "names of steps that must run before this one",
	},
	{
		Name:      PipelineStepFieldTimeout,
		ValueForm: stepFormString,
		Summary:   "Go duration string limiting this step's runtime",
	},
	{
		Name:      PipelineStepFieldWhen,
		ValueForm: stepFormRef,
		Summary:   "step reference guarding whether this step runs",
	},
}

// pipelineStepField looks up a step field by name.
func pipelineStepField(name string) (pipelineStepFieldSpec, bool) {
	for _, spec := range pipelineStepFields {
		if spec.Name == name {
			return spec, true
		}
	}
	return pipelineStepFieldSpec{}, false
}

// pipelineStepFieldNames returns the canonical field names in grammar order.
func pipelineStepFieldNames() []string {
	names := make([]string, len(pipelineStepFields))
	for i, spec := range pipelineStepFields {
		names[i] = spec.Name
	}
	return names
}

// pipelineStepFieldList renders the canonical field names for diagnostics.
func pipelineStepFieldList() string {
	return strings.Join(pipelineStepFieldNames(), ", ")
}

// pipelineStepFieldHint returns the repair hint recorded for a field, falling
// back to a generic hint so validator diagnostics never lose their guidance.
func pipelineStepFieldHint(name string) string {
	if spec, ok := pipelineStepField(name); ok && spec.Hint != "" {
		return spec.Hint
	}
	return fmt.Sprintf("check the %s field", name)
}

// suggestStepFields returns canonical field names close to got, so an
// unknown-field diagnostic can offer a spelling repair instead of a lookup.
func suggestStepFields(got string) []string {
	type scored struct {
		name string
		dist int
	}
	var candidates []scored
	for _, name := range pipelineStepFieldNames() {
		if d := levenshtein(strings.ToLower(got), name); d <= 2 {
			candidates = append(candidates, scored{name: name, dist: d})
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].dist != candidates[j].dist {
			return candidates[i].dist < candidates[j].dist
		}
		return candidates[i].name < candidates[j].name
	})
	names := make([]string, 0, len(candidates))
	for _, c := range candidates {
		names = append(names, c.name)
	}
	return names
}
