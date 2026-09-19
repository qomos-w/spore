package config_test

import (
	"encoding/json"
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/config"
)

func TestPipelineASTToDescPreservesTopology(t *testing.T) {
	src := `
pipeline code_review {
    step generate {
        invoke: "agent.coding:coding.turn"
        input: {
            intent: "implement login flow"
        }
    }

    step review {
        invoke: "human.human-performer:human_performer.approve_tool"
        depends_on: [generate]
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
        depends_on: [review, test, lint]
    }
}
`
	cfg, err := config.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(cfg.Pipelines) != 1 {
		t.Fatalf("expected 1 pipeline, got %d", len(cfg.Pipelines))
	}

	desc, diags := config.PipelineDescFromAST(cfg.Pipelines[0])
	if len(diags) > 0 {
		for _, d := range diags {
			if d.Severity == "error" {
				t.Errorf("unexpected error diag: %+v", d)
			}
		}
	}

	if desc.Name != "code_review" {
		t.Errorf("expected name code_review, got %q", desc.Name)
	}
	if len(desc.Steps) != 4 {
		t.Fatalf("expected 4 steps, got %d", len(desc.Steps))
	}
	if desc.Steps[0].Name != "generate" {
		t.Errorf("expected step[0] generate, got %q", desc.Steps[0].Name)
	}
	if desc.Steps[1].Name != "review" {
		t.Errorf("expected step[1] review, got %q", desc.Steps[1].Name)
	}
	// Parallel synthetic step
	if len(desc.Steps[2].Parallel) != 2 {
		t.Fatalf("expected 2 parallel children, got %d", len(desc.Steps[2].Parallel))
	}
	if desc.Steps[2].Parallel[0].Name != "test" {
		t.Errorf("expected parallel child test, got %q", desc.Steps[2].Parallel[0].Name)
	}
	if desc.Steps[2].Parallel[1].Name != "lint" {
		t.Errorf("expected parallel child lint, got %q", desc.Steps[2].Parallel[1].Name)
	}
	if desc.Steps[3].Name != "finalize" {
		t.Errorf("expected step[3] finalize, got %q", desc.Steps[3].Name)
	}
	if len(desc.Steps[3].DependsOn) != 3 {
		t.Errorf("expected 3 depends_on, got %d", len(desc.Steps[3].DependsOn))
	}
}

func TestPipelineASTToDescPreservesInvoke(t *testing.T) {
	src := `
pipeline test {
    step s1 {
        invoke: "schema:callable"
    }
    step s2 {
        invoke: "tool:my_tool"
    }
}
`
	cfg, err := config.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	desc, _ := config.PipelineDescFromAST(cfg.Pipelines[0])
	if desc.Steps[0].Invoke != "schema:callable" {
		t.Errorf("expected schema:callable, got %q", desc.Steps[0].Invoke)
	}
	if desc.Steps[1].Invoke != "tool:my_tool" {
		t.Errorf("expected tool:my_tool, got %q", desc.Steps[1].Invoke)
	}
}

func TestPipelineASTToDescPreservesInput(t *testing.T) {
	src := `
pipeline test {
    step s1 {
        invoke: "a:b"
        input: {
            name: "hello"
            count: 42
            nested: {
                flag: true
            }
            refs: s0.output
        }
    }
}
`
	cfg, err := config.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	desc, _ := config.PipelineDescFromAST(cfg.Pipelines[0])
	input := desc.Steps[0].Input
	if input["name"] != "hello" {
		t.Errorf("expected name=hello, got %v", input["name"])
	}
	if input["count"] != int64(42) {
		t.Errorf("expected count=42, got %v (type %T)", input["count"], input["count"])
	}
	nested, ok := input["nested"].(map[string]any)
	if !ok {
		t.Fatalf("expected nested map, got %T", input["nested"])
	}
	if nested["flag"] != true {
		t.Errorf("expected nested.flag=true, got %v", nested["flag"])
	}
	ref, ok := input["refs"].(binding.PipelineRef)
	if !ok {
		t.Fatalf("expected refs as PipelineRef, got %T", input["refs"])
	}
	if ref.Expr != "s0.output" {
		t.Errorf("expected ref expr s0.output, got %q", ref.Expr)
	}
}

func TestCapabilityDescClonePreservesPipelines(t *testing.T) {
	b := binding.NewCapability("orchestration", "pipeline")
	original := binding.PipelineDesc{
		Name: "code_review",
		Steps: []binding.PipelineStepDesc{
			{Name: "generate", Invoke: "a:b"},
			{Name: "review", Invoke: "c:d", DependsOn: []string{"generate"}},
		},
	}
	if err := b.AddPipeline("code_review", original); err != nil {
		t.Fatalf("AddPipeline: %v", err)
	}
	cap, err := b.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(cap.Desc.Pipelines) != 1 {
		t.Fatalf("expected 1 pipeline, got %d", len(cap.Desc.Pipelines))
	}
	if cap.Desc.Pipelines[0].Name != "code_review" {
		t.Errorf("expected pipeline name code_review, got %q", cap.Desc.Pipelines[0].Name)
	}
	// Verify deep clone: modifying the builder's original should not affect built desc.
	original.Steps[0].Invoke = "modified"
	if cap.Desc.Pipelines[0].Steps[0].Invoke == "modified" {
		t.Error("expected deep clone to isolate built desc from original mutation")
	}
}

func TestPipelineDescSerializable(t *testing.T) {
	desc := binding.PipelineDesc{
		Name: "deploy",
		Steps: []binding.PipelineStepDesc{
			{Name: "build", Invoke: "ci:build", Input: map[string]any{"target": "app"}},
			{Name: "test", Invoke: "ci:test", DependsOn: []string{"build"}},
			{
				Parallel: []binding.PipelineStepDesc{
					{Name: "lint", Invoke: "ci:lint"},
					{Name: "fmt", Invoke: "ci:fmt"},
				},
			},
		},
	}
	data, err := json.Marshal(desc)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var decoded binding.PipelineDesc
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if decoded.Name != "deploy" {
		t.Errorf("expected name deploy, got %q", decoded.Name)
	}
	if len(decoded.Steps) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(decoded.Steps))
	}
	if decoded.Steps[1].DependsOn[0] != "build" {
		t.Errorf("expected depends_on build, got %v", decoded.Steps[1].DependsOn)
	}
	if len(decoded.Steps[2].Parallel) != 2 {
		t.Fatalf("expected 2 parallel children, got %d", len(decoded.Steps[2].Parallel))
	}
}

func TestCapabilityBuilder_AddPipeline(t *testing.T) {
	b := binding.NewCapability("orchestration", "pipeline")
	if err := b.AddPipeline("p1", binding.PipelineDesc{Name: "p1"}); err != nil {
		t.Fatalf("AddPipeline: %v", err)
	}
	if err := b.AddPipeline("p2", binding.PipelineDesc{Name: "p2"}); err != nil {
		t.Fatalf("AddPipeline p2: %v", err)
	}
	cap, err := b.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(cap.Desc.Pipelines) != 2 {
		t.Fatalf("expected 2 pipelines, got %d", len(cap.Desc.Pipelines))
	}
}

func TestCapabilityBuilder_AddPipelineRejectsDuplicate(t *testing.T) {
	b := binding.NewCapability("orchestration", "pipeline")
	if err := b.AddPipeline("p1", binding.PipelineDesc{}); err != nil {
		t.Fatalf("AddPipeline: %v", err)
	}
	if err := b.AddPipeline("p1", binding.PipelineDesc{}); err == nil {
		t.Fatal("expected duplicate pipeline error")
	}
}

func TestCapabilityBuilder_AddPipelineRejectsEmptyName(t *testing.T) {
	b := binding.NewCapability("orchestration", "pipeline")
	if err := b.AddPipeline("", binding.PipelineDesc{}); err == nil {
		t.Fatal("expected empty pipeline name error")
	}
}

func TestCapabilityBuilder_PipelineOnlyCapability(t *testing.T) {
	b := binding.NewCapability("orchestration", "pipeline")
	if err := b.AddPipeline("deploy", binding.PipelineDesc{
		Steps: []binding.PipelineStepDesc{
			{Name: "build", Invoke: "ci:build"},
		},
	}); err != nil {
		t.Fatalf("AddPipeline: %v", err)
	}
	cap, err := b.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(cap.Desc.Pipelines) != 1 {
		t.Fatalf("expected 1 pipeline, got %d", len(cap.Desc.Pipelines))
	}
	if len(cap.Desc.Callables) != 0 {
		t.Errorf("expected 0 callables, got %d", len(cap.Desc.Callables))
	}
}

func TestPipelineDescFromASTPreservesWhen(t *testing.T) {
	src := `
pipeline test {
    step s1 {
        invoke: "a:b"
        when: s0.facts.ok
    }
}
`
	cfg, err := config.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	desc, _ := config.PipelineDescFromAST(cfg.Pipelines[0])
	if desc.Steps[0].When != "s0.facts.ok" {
		t.Errorf("expected when s0.facts.ok, got %q", desc.Steps[0].When)
	}
}

func TestPipelineDescFromASTPreservesTimeout(t *testing.T) {
	src := `
pipeline test {
    step s1 {
        invoke: "a:b"
        timeout: "10m"
    }
}
`
	cfg, err := config.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	desc, _ := config.PipelineDescFromAST(cfg.Pipelines[0])
	if desc.Steps[0].Timeout != "10m" {
		t.Errorf("expected timeout 10m, got %q", desc.Steps[0].Timeout)
	}
}
