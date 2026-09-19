package config

import (
	"testing"

	"github.com/qomos-w/spore/schema"
)

type mockSchemaResolver struct {
	schemas   map[string]bool
	callables map[string]schema.CallableDesc
}

func (m *mockSchemaResolver) ResolveSchema(schemaID string) bool {
	return m.schemas[schemaID]
}

func (m *mockSchemaResolver) ResolveCallable(schemaID, callableName string) (schema.CallableDesc, bool) {
	key := schemaID + ":" + callableName
	desc, ok := m.callables[key]
	return desc, ok
}

func newMockResolver() *mockSchemaResolver {
	return &mockSchemaResolver{
		schemas: map[string]bool{
			"agent.coding":  true,
			"human.approver": true,
		},
		callables: map[string]schema.CallableDesc{
			"agent.coding:coding.turn": {
				Name: "coding.turn",
				Parameters: []schema.ParameterDesc{
					{Name: "intent", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
					{Name: "model", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
				},
				Mode: schema.CallableModeUnary,
			},
			"human.approver:human_performer.approve_tool": {
				Name: "human_performer.approve_tool",
				Parameters: []schema.ParameterDesc{
					{Name: "tool_call_id", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
				},
				Mode: schema.CallableModeUnary,
			},
		},
	}
}

func TestPipelineValidatorRejectsUnknownSchemaWhenResolverPresent(t *testing.T) {
	src := `
pipeline test {
    step s1 {
        invoke: "unknown.schema:do_something"
    }
}
`
	cfg, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	resolver := newMockResolver()
	diags := ValidatePipelinesWithResolver(resolver)(cfg, nil)
	found := false
	for _, d := range diags {
		if d.Code == "pipeline_unknown_schema" {
			found = true
			if d.Actual != "unknown.schema" {
				t.Errorf("expected actual unknown.schema, got %q", d.Actual)
			}
		}
	}
	if !found {
		t.Errorf("expected pipeline_unknown_schema diagnostic, got %v", diagCodes(diags))
	}
}

func TestPipelineValidatorRejectsUnknownCallableWhenResolverPresent(t *testing.T) {
	src := `
pipeline test {
    step s1 {
        invoke: "agent.coding:nonexistent_callable"
    }
}
`
	cfg, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	resolver := newMockResolver()
	diags := ValidatePipelinesWithResolver(resolver)(cfg, nil)
	found := false
	for _, d := range diags {
		if d.Code == "pipeline_unknown_callable" {
			found = true
			if d.Actual != "nonexistent_callable" {
				t.Errorf("expected actual nonexistent_callable, got %q", d.Actual)
			}
		}
	}
	if !found {
		t.Errorf("expected pipeline_unknown_callable diagnostic, got %v", diagCodes(diags))
	}
}

func TestPipelineValidatorReportsInputMismatchWhenResolverPresent(t *testing.T) {
	src := `
pipeline test {
    step s1 {
        invoke: "agent.coding:coding.turn"
        input: {
            intent: "hello"
            unknown_param: 42
        }
    }
}
`
	cfg, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	resolver := newMockResolver()
	diags := ValidatePipelinesWithResolver(resolver)(cfg, nil)
	found := false
	for _, d := range diags {
		if d.Code == "pipeline_input_mismatch" {
			found = true
			if d.Actual != "unknown_param" {
				t.Errorf("expected actual unknown_param, got %q", d.Actual)
			}
			if d.Severity != "warning" {
				t.Errorf("expected warning severity, got %q", d.Severity)
			}
		}
	}
	if !found {
		t.Errorf("expected pipeline_input_mismatch diagnostic, got %v", diagCodes(diags))
	}
}

func TestPipelineValidatorAcceptsKnownCallableWithMatchingInput(t *testing.T) {
	src := `
pipeline test {
    step s1 {
        invoke: "agent.coding:coding.turn"
        input: {
            intent: "hello"
            model: "claude"
        }
    }
}
`
	cfg, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	resolver := newMockResolver()
	diags := ValidatePipelinesWithResolver(resolver)(cfg, nil)
	for _, d := range diags {
		if d.Code == "pipeline_unknown_schema" || d.Code == "pipeline_unknown_callable" || d.Code == "pipeline_input_mismatch" {
			t.Errorf("unexpected registry-aware diagnostic: %+v", d)
		}
	}
}

func TestPipelineValidatorToolRefBypassesRegistry(t *testing.T) {
	src := `
pipeline test {
    step s1 {
        invoke: "tool:my_tool"
    }
}
`
	cfg, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	resolver := newMockResolver()
	diags := ValidatePipelinesWithResolver(resolver)(cfg, nil)
	for _, d := range diags {
		if d.Code == "pipeline_unknown_schema" || d.Code == "pipeline_unknown_callable" {
			t.Errorf("tool: ref should not trigger registry lookup: %+v", d)
		}
	}
}

func TestPipelineValidatorNoResolverOnlyShapeChecks(t *testing.T) {
	src := `
pipeline test {
    step s1 {
        invoke: "agent.coding:coding.turn"
        input: {
            intent: "hello"
        }
    }
}
`
	cfg, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// Without resolver, shape validation should pass for valid invoke format
	diags := ValidatePipelines()(cfg, nil)
	for _, d := range diags {
		if d.Code == "pipeline_unknown_schema" || d.Code == "pipeline_unknown_callable" || d.Code == "pipeline_input_mismatch" {
			t.Errorf("without resolver, should not produce registry-aware diags: %+v", d)
		}
	}
}
