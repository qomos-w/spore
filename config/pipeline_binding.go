package config

import (
	"fmt"

	"github.com/qomos-w/spore/binding"
)

// PipelineDescFromAST converts a config PipelineAST into a binding PipelineDesc.
// It returns any conversion diagnostics (e.g. invalid values in input maps).
//
// The adapter lives in config (not binding) so the dependency direction stays
// config→binding: the binding layer must not import the config parsing layer.
func PipelineDescFromAST(ast PipelineAST) (binding.PipelineDesc, []Diagnostic) {
	var diags []Diagnostic
	steps := make([]binding.PipelineStepDesc, len(ast.Steps))
	for i, stepAST := range ast.Steps {
		step, stepDiags := pipelineStepFromAST(stepAST)
		diags = append(diags, stepDiags...)
		steps[i] = step
	}
	return binding.PipelineDesc{Name: ast.Name, Steps: steps}, diags
}

func pipelineStepFromAST(step PipelineStepAST) (binding.PipelineStepDesc, []Diagnostic) {
	var diags []Diagnostic

	input := make(map[string]any)
	if step.Input.Kind == ValueMap {
		for _, kv := range step.Input.Entries {
			input[kv.Key] = valueToAny(kv.Value, &diags)
		}
	} else if step.Input.Kind != ValueNull && step.Input.Kind != 0 {
		// Input is not a map — warn but still proceed.
		diags = append(diags, Diagnostic{
			Code:     "pipeline_input_mismatch",
			Category: "validate",
			Severity: "warning",
			Message:  fmt.Sprintf("step %q input is not a map value", step.Name),
			Hint:     "input should be a struct/map literal",
			Line:     step.Input.Line,
		})
	}

	var whenStr string
	if step.When != nil {
		whenStr = step.When.Raw
	}

	desc := binding.PipelineStepDesc{
		Name:      step.Name,
		Invoke:    step.Invoke,
		Input:     input,
		DependsOn: append([]string(nil), step.DependsOn...),
		Timeout:   step.Timeout,
		When:      whenStr,
	}

	if len(step.Parallel) > 0 {
		desc.Parallel = make([]binding.PipelineStepDesc, len(step.Parallel))
		for i, child := range step.Parallel {
			childDesc, childDiags := pipelineStepFromAST(child)
			diags = append(diags, childDiags...)
			desc.Parallel[i] = childDesc
		}
	}

	return desc, diags
}

func valueToAny(v Value, diags *[]Diagnostic) any {
	switch v.Kind {
	case ValueInt:
		return v.IntVal
	case ValueFloat:
		return v.FloatVal
	case ValueString:
		return v.StrVal
	case ValueBool:
		return v.BoolVal
	case ValueNull:
		return nil
	case ValueArray:
		arr := make([]any, len(v.Elements))
		for i, e := range v.Elements {
			arr[i] = valueToAny(e, diags)
		}
		return arr
	case ValueMap:
		m := make(map[string]any, len(v.Entries))
		for _, kv := range v.Entries {
			m[kv.Key] = valueToAny(kv.Value, diags)
		}
		return m
	case ValueStruct:
		m := make(map[string]any, len(v.Fields)+1)
		m["__struct__"] = v.TypeName
		for _, kv := range v.Fields {
			m[kv.Key] = valueToAny(kv.Value, diags)
		}
		return m
	case ValueRef:
		return binding.PipelineRef{Expr: v.StrVal}
	default:
		*diags = append(*diags, Diagnostic{
			Code:     "pipeline_input_mismatch",
			Category: "validate",
			Severity: "warning",
			Message:  fmt.Sprintf("unsupported value kind %d in input", v.Kind),
			Hint:     "use scalar, map, array, or reference values",
			Line:     v.Line,
		})
		return nil
	}
}
