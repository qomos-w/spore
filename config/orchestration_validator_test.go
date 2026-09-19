package config

import (
	"strings"
	"testing"
)

func TestPipelineValidatorRejectsDuplicateStepNames(t *testing.T) {
	src := `
pipeline test {
    step a {
        invoke: "x:y"
    }
    step a {
        invoke: "x:z"
    }
}
`
	cfg, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	diags := ValidatePipelines()(cfg, nil)
	found := false
	for _, d := range diags {
		if d.Code == "pipeline_duplicate_step" {
			found = true
			if !strings.Contains(d.Message, "a") {
				t.Errorf("expected message to mention step 'a', got %q", d.Message)
			}
		}
	}
	if !found {
		t.Errorf("expected pipeline_duplicate_step diagnostic, got %v", diagCodes(diags))
	}
}

func TestPipelineValidatorRejectsMissingInvoke(t *testing.T) {
	src := `
pipeline test {
    step a {
        input: {}
    }
}
`
	cfg, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	diags := ValidatePipelines()(cfg, nil)
	found := false
	for _, d := range diags {
		if d.Code == "pipeline_missing_invoke" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected pipeline_missing_invoke diagnostic, got %v", diagCodes(diags))
	}
}

func TestPipelineValidatorRejectsInvalidInvokeRef(t *testing.T) {
	cases := []string{
		`invoke: "no_colon"`,
		`invoke: "too:many:colons"`,
	}
	for _, invokeField := range cases {
		src := "pipeline test {\n    step a {\n        " + invokeField + "\n    }\n}\n"
		cfg, err := Parse(src)
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		diags := ValidatePipelines()(cfg, nil)
		found := false
		for _, d := range diags {
			if d.Code == "pipeline_invalid_invoke_ref" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected pipeline_invalid_invoke_ref for %s, got %v", invokeField, diagCodes(diags))
		}
	}
}

func TestPipelineValidatorAcceptsValidInvokeRef(t *testing.T) {
	cases := []string{
		`invoke: "a:b"`,
		`invoke: "tool:my_tool"`,
		`invoke: "agent.coding:coding.turn"`,
	}
	for _, invokeField := range cases {
		src := "pipeline test {\n    step a {\n        " + invokeField + "\n    }\n}\n"
		cfg, err := Parse(src)
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		diags := ValidatePipelines()(cfg, nil)
		for _, d := range diags {
			if d.Code == "pipeline_invalid_invoke_ref" {
				t.Errorf("unexpected invalid_invoke_ref for %s", invokeField)
			}
		}
	}
}

func TestPipelineValidatorRejectsUnknownDependency(t *testing.T) {
	src := `
pipeline test {
    step a {
        invoke: "x:y"
    }
    step b {
        invoke: "x:z"
        depends_on: [a, nonexistent]
    }
}
`
	cfg, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	diags := ValidatePipelines()(cfg, nil)
	found := false
	for _, d := range diags {
		if d.Code == "pipeline_unknown_dependency" {
			found = true
			if !strings.Contains(d.Message, "nonexistent") {
				t.Errorf("expected message to mention 'nonexistent', got %q", d.Message)
			}
		}
	}
	if !found {
		t.Errorf("expected pipeline_unknown_dependency diagnostic, got %v", diagCodes(diags))
	}
}

func TestPipelineValidatorRejectsDependencyCycle(t *testing.T) {
	src := `
pipeline test {
    step a {
        invoke: "x:y"
        depends_on: [c]
    }
    step b {
        invoke: "x:z"
        depends_on: [a]
    }
    step c {
        invoke: "x:w"
        depends_on: [b]
    }
}
`
	cfg, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	diags := ValidatePipelines()(cfg, nil)
	found := false
	for _, d := range diags {
		if d.Code == "pipeline_dependency_cycle" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected pipeline_dependency_cycle diagnostic, got %v", diagCodes(diags))
	}
}

func TestPipelineValidatorRejectsSelfDependency(t *testing.T) {
	src := `
pipeline test {
    step a {
        invoke: "x:y"
        depends_on: [a]
    }
}
`
	cfg, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	diags := ValidatePipelines()(cfg, nil)
	found := false
	for _, d := range diags {
		if d.Code == "pipeline_dependency_cycle" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected pipeline_dependency_cycle for self-dependency, got %v", diagCodes(diags))
	}
}

func TestPipelineValidatorUnknownDependencyDoesNotTriggerCycle(t *testing.T) {
	src := `
pipeline test {
    step a {
        invoke: "x:y"
        depends_on: [unknown]
    }
}
`
	cfg, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	diags := ValidatePipelines()(cfg, nil)
	// Should report unknown_dependency, but NOT dependency_cycle.
	foundUnknown := false
	for _, d := range diags {
		if d.Code == "pipeline_dependency_cycle" {
			t.Errorf("unexpected pipeline_dependency_cycle for unknown dependency: %+v", d)
		}
		if d.Code == "pipeline_unknown_dependency" {
			foundUnknown = true
		}
	}
	if !foundUnknown {
		t.Errorf("expected pipeline_unknown_dependency, got %v", diagCodes(diags))
	}
}

func TestPipelineValidatorRejectsParallelChildNameCollision(t *testing.T) {
	src := `
pipeline test {
    step a {
        invoke: "x:y"
    }
    parallel {
        step a {
            invoke: "x:z"
        }
    }
}
`
	cfg, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	diags := ValidatePipelines()(cfg, nil)
	found := false
	for _, d := range diags {
		if d.Code == "pipeline_duplicate_step" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected pipeline_duplicate_step for parallel child collision, got %v", diagCodes(diags))
	}
}

func TestPipelineValidatorRejectsInvalidTimeout(t *testing.T) {
	src := `
pipeline test {
    step a {
        invoke: "x:y"
        timeout: "not_a_duration"
    }
}
`
	cfg, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	diags := ValidatePipelines()(cfg, nil)
	found := false
	for _, d := range diags {
		if d.Code == "pipeline_invalid_timeout" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected pipeline_invalid_timeout diagnostic, got %v", diagCodes(diags))
	}
}

func TestPipelineValidatorAcceptsValidTimeout(t *testing.T) {
	src := `
pipeline test {
    step a {
        invoke: "x:y"
        timeout: "10m"
    }
    step b {
        invoke: "x:z"
        timeout: "30s"
    }
    step c {
        invoke: "x:w"
        timeout: "1h30m"
    }
}
`
	cfg, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	diags := ValidatePipelines()(cfg, nil)
	for _, d := range diags {
		if d.Code == "pipeline_invalid_timeout" {
			t.Errorf("unexpected invalid_timeout: %v", d)
		}
	}
}

func TestPipelineValidatorAllDiagnosticsHaveContext(t *testing.T) {
	src := `
pipeline test {
    step a {
        invoke: "bad"
        depends_on: [missing]
    }
    step a {
        invoke: "x:y"
    }
}
`
	cfg, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	diags := ValidatePipelines()(cfg, nil)
	for _, d := range diags {
		if d.Code == "" {
			t.Errorf("diagnostic missing code: %+v", d)
		}
		if d.Severity == "" {
			t.Errorf("diagnostic missing severity: %+v", d)
		}
		if d.Message == "" {
			t.Errorf("diagnostic missing message: %+v", d)
		}
		if d.Hint == "" {
			t.Errorf("diagnostic missing hint: %+v", d)
		}
	}
}
