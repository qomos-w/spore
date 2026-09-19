package config

import (
	"testing"
)

func TestParsePipelineBlock(t *testing.T) {
	src := `
pipeline code_review {
    step generate {
        invoke: "agent.coding:coding.turn"
        input: {
            intent: "implement login flow"
            model: "claude-sonnet-4"
        }
        timeout: "10m"
    }

    step review {
        invoke: "human.human-performer:human_performer.approve_tool"
        input: {
            tool_call_id: generate.output
        }
        depends_on: [generate]
        timeout: "30m"
    }
}
`
	cfg, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(cfg.Pipelines) != 1 {
		t.Fatalf("expected 1 pipeline, got %d", len(cfg.Pipelines))
	}
	p := cfg.Pipelines[0]
	if p.Name != "code_review" {
		t.Errorf("expected pipeline name code_review, got %q", p.Name)
	}
	if len(p.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(p.Steps))
	}
	if p.Steps[0].Name != "generate" {
		t.Errorf("expected step[0] name generate, got %q", p.Steps[0].Name)
	}
	if p.Steps[0].Invoke != "agent.coding:coding.turn" {
		t.Errorf("expected invoke agent.coding:coding.turn, got %q", p.Steps[0].Invoke)
	}
	if p.Steps[1].Name != "review" {
		t.Errorf("expected step[1] name review, got %q", p.Steps[1].Name)
	}
	if p.Steps[1].Invoke != "human.human-performer:human_performer.approve_tool" {
		t.Errorf("expected invoke human.human-performer:human_performer.approve_tool, got %q", p.Steps[1].Invoke)
	}
	if len(p.Steps[1].DependsOn) != 1 || p.Steps[1].DependsOn[0] != "generate" {
		t.Errorf("expected depends_on [generate], got %v", p.Steps[1].DependsOn)
	}
}

func TestParsePipelineStepInvoke(t *testing.T) {
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
	if len(cfg.Pipelines) != 1 {
		t.Fatalf("expected 1 pipeline, got %d", len(cfg.Pipelines))
	}
	if cfg.Pipelines[0].Steps[0].Invoke != "tool:my_tool" {
		t.Errorf("expected tool:my_tool, got %q", cfg.Pipelines[0].Steps[0].Invoke)
	}
}

func TestParseParallelBlock(t *testing.T) {
	src := `
pipeline test {
    step init {
        invoke: "a:b"
    }

    parallel {
        step test {
            invoke: "agent.coding:coding.turn"
            input: { intent: "run unit tests" }
        }
        step lint {
            invoke: "agent.coding:coding.turn"
            input: { intent: "run lint" }
        }
    }

    step finalize {
        invoke: "system.project:project.turn_request"
        input: { intent: "merge and tag" }
        depends_on: [init, test, lint]
    }
}
`
	cfg, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	p := cfg.Pipelines[0]
	if len(p.Steps) != 3 {
		t.Fatalf("expected 3 top-level steps, got %d", len(p.Steps))
	}
	// parallel block should be represented as a synthetic step with Parallel children
	parallelStep := p.Steps[1]
	if len(parallelStep.Parallel) != 2 {
		t.Fatalf("expected 2 parallel children, got %d", len(parallelStep.Parallel))
	}
	if parallelStep.Parallel[0].Name != "test" {
		t.Errorf("expected parallel child test, got %q", parallelStep.Parallel[0].Name)
	}
	if parallelStep.Parallel[1].Name != "lint" {
		t.Errorf("expected parallel child lint, got %q", parallelStep.Parallel[1].Name)
	}
	// finalize depends_on
	finalize := p.Steps[2]
	if len(finalize.DependsOn) != 3 {
		t.Errorf("expected 3 depends_on, got %d", len(finalize.DependsOn))
	}
}

func TestConfigRetainsValuesAndPipelines(t *testing.T) {
	src := `
host: "localhost"

pipeline deploy {
    step build {
        invoke: "ci:build"
    }
}

port: 8080
`
	cfg, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(cfg.Values) != 2 {
		t.Fatalf("expected 2 values, got %d", len(cfg.Values))
	}
	if len(cfg.Pipelines) != 1 {
		t.Fatalf("expected 1 pipeline, got %d", len(cfg.Pipelines))
	}
	host, ok := cfg.Get("host")
	if !ok || host.StrVal != "localhost" {
		t.Errorf("expected host localhost")
	}
	port, ok := cfg.Get("port")
	if !ok || port.IntVal != 8080 {
		t.Errorf("expected port 8080")
	}
}

func TestPipelineParsePreservesLineInfo(t *testing.T) {
	src := `
pipeline test {
    step s1 {
        invoke: "a:b"
    }
}
`
	cfg, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	p := cfg.Pipelines[0]
	if p.Line != 2 {
		t.Errorf("expected pipeline line 2, got %d", p.Line)
	}
	if p.Steps[0].Line != 3 {
		t.Errorf("expected step line 3, got %d", p.Steps[0].Line)
	}
}

func TestParsePipelineRefExpr(t *testing.T) {
	src := `
pipeline test {
    step s1 {
        invoke: "a:b"
        input: {
            result: s0.output
        }
    }
}
`
	cfg, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	p := cfg.Pipelines[0]
	input := p.Steps[0].Input
	if input.Kind != ValueMap {
		t.Fatalf("expected input as ValueMap, got %v", input.Kind)
	}
	result, ok := findEntry(input.Entries, "result")
	if !ok {
		t.Fatal("expected result entry")
	}
	if result.Kind != ValueRef {
		t.Fatalf("expected result as ValueRef, got %v", result.Kind)
	}
	if result.StrVal != "s0.output" {
		t.Errorf("expected ref s0.output, got %q", result.StrVal)
	}
}

func TestParsePipelineWhen(t *testing.T) {
	src := `
pipeline test {
    step s1 {
        invoke: "a:b"
        when: s0.facts.ok
    }
}
`
	cfg, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	p := cfg.Pipelines[0]
	if p.Steps[0].When == nil {
		t.Fatal("expected when clause")
	}
	if p.Steps[0].When.Raw != "s0.facts.ok" {
		t.Errorf("expected when raw s0.facts.ok, got %q", p.Steps[0].When.Raw)
	}
	if p.Steps[0].When.StepName != "s0" {
		t.Errorf("expected when step name s0, got %q", p.Steps[0].When.StepName)
	}
	if len(p.Steps[0].When.Path) != 2 || p.Steps[0].When.Path[0] != "facts" || p.Steps[0].When.Path[1] != "ok" {
		t.Errorf("expected when path [facts, ok], got %v", p.Steps[0].When.Path)
	}
}
