package binding

import (
	"fmt"

	"github.com/qomos-w/spore/config"
)

// PipelineDescFromAST converts a config PipelineAST into a binding PipelineDesc.
// It returns any conversion diagnostics (e.g. invalid values in input maps).
func PipelineDescFromAST(ast config.PipelineAST) (PipelineDesc, []config.Diagnostic) {
	var diags []config.Diagnostic
	steps := make([]PipelineStepDesc, len(ast.Steps))
	for i, stepAST := range ast.Steps {
		step, stepDiags := pipelineStepFromAST(stepAST)
		diags = append(diags, stepDiags...)
		steps[i] = step
	}
	return PipelineDesc{Name: ast.Name, Steps: steps}, diags
}

func pipelineStepFromAST(step config.PipelineStepAST) (PipelineStepDesc, []config.Diagnostic) {
	var diags []config.Diagnostic

	input := make(map[string]any)
	if step.Input.Kind == config.ValueMap {
		for _, kv := range step.Input.Entries {
			input[kv.Key] = valueToAny(kv.Value, &diags)
		}
	} else if step.Input.Kind != config.ValueNull && step.Input.Kind != 0 {
		// Input is not a map — warn but still proceed.
		diags = append(diags, config.Diagnostic{
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

	desc := PipelineStepDesc{
		Name:      step.Name,
		Invoke:    step.Invoke,
		Input:     input,
		DependsOn: append([]string(nil), step.DependsOn...),
		Timeout:   step.Timeout,
		When:      whenStr,
	}

	if len(step.Parallel) > 0 {
		desc.Parallel = make([]PipelineStepDesc, len(step.Parallel))
		for i, child := range step.Parallel {
			childDesc, childDiags := pipelineStepFromAST(child)
			diags = append(diags, childDiags...)
			desc.Parallel[i] = childDesc
		}
	}

	return desc, diags
}

func valueToAny(v config.Value, diags *[]config.Diagnostic) any {
	switch v.Kind {
	case config.ValueInt:
		return v.IntVal
	case config.ValueFloat:
		return v.FloatVal
	case config.ValueString:
		return v.StrVal
	case config.ValueBool:
		return v.BoolVal
	case config.ValueNull:
		return nil
	case config.ValueArray:
		arr := make([]any, len(v.Elements))
		for i, e := range v.Elements {
			arr[i] = valueToAny(e, diags)
		}
		return arr
	case config.ValueMap:
		m := make(map[string]any, len(v.Entries))
		for _, kv := range v.Entries {
			m[kv.Key] = valueToAny(kv.Value, diags)
		}
		return m
	case config.ValueStruct:
		m := make(map[string]any, len(v.Fields)+1)
		m["__struct__"] = v.TypeName
		for _, kv := range v.Fields {
			m[kv.Key] = valueToAny(kv.Value, diags)
		}
		return m
	case config.ValueRef:
		return PipelineRef{Expr: v.StrVal}
	default:
		*diags = append(*diags, config.Diagnostic{
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
