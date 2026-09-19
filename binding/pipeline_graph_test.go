package binding_test

import (
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/config"
)

func TestBuildPipelineGraphProducesStableNodes(t *testing.T) {
	src := `
pipeline test {
    step build {
        invoke: "ci:build"
    }
    step test {
        invoke: "ci:test"
        depends_on: [build]
    }
}
`
	cfg, err := config.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	desc, _ := config.PipelineDescFromAST(cfg.Pipelines[0])
	graph := binding.BuildPipelineGraph(desc)

	if len(graph.Nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(graph.Nodes))
	}
	nodeIDs := make(map[string]string)
	for _, n := range graph.Nodes {
		nodeIDs[n.ID] = n.Kind
	}
	if nodeIDs["build"] != binding.GraphNodeKindStep {
		t.Errorf("expected build as step, got %v", nodeIDs["build"])
	}
	if nodeIDs["test"] != binding.GraphNodeKindStep {
		t.Errorf("expected test as step, got %v", nodeIDs["test"])
	}
}

func TestBuildPipelineGraphProducesDependencyEdges(t *testing.T) {
	src := `
pipeline test {
    step build {
        invoke: "ci:build"
    }
    step test {
        invoke: "ci:test"
        depends_on: [build]
    }
    step deploy {
        invoke: "ci:deploy"
        depends_on: [test]
    }
}
`
	cfg, err := config.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	desc, _ := config.PipelineDescFromAST(cfg.Pipelines[0])
	graph := binding.BuildPipelineGraph(desc)

	foundBuildToTest := false
	foundTestToDeploy := false
	for _, e := range graph.Edges {
		if e.Kind == binding.GraphEdgeKindDependsOn {
			if e.From == "build" && e.To == "test" {
				foundBuildToTest = true
			}
			if e.From == "test" && e.To == "deploy" {
				foundTestToDeploy = true
			}
		}
	}
	if !foundBuildToTest {
		t.Error("expected depends_on edge build -> test")
	}
	if !foundTestToDeploy {
		t.Error("expected depends_on edge test -> deploy")
	}
}

func TestBuildPipelineGraphProducesParallelEdges(t *testing.T) {
	src := `
pipeline test {
    step build {
        invoke: "ci:build"
    }
    parallel {
        step lint {
            invoke: "ci:lint"
        }
        step fmt {
            invoke: "ci:fmt"
        }
    }
    step deploy {
        invoke: "ci:deploy"
        depends_on: [build, lint, fmt]
    }
}
`
	cfg, err := config.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	desc, _ := config.PipelineDescFromAST(cfg.Pipelines[0])
	graph := binding.BuildPipelineGraph(desc)

	// Should have parallel node
	foundParallel := false
	for _, n := range graph.Nodes {
		if n.Kind == binding.GraphNodeKindParallel {
			foundParallel = true
			break
		}
	}
	if !foundParallel {
		t.Error("expected a parallel node")
	}

	// Should have parallel_child edges
	foundLint := false
	foundFmt := false
	for _, e := range graph.Edges {
		if e.Kind == binding.GraphEdgeKindParallelChild {
			if e.To == "lint" {
				foundLint = true
			}
			if e.To == "fmt" {
				foundFmt = true
			}
		}
	}
	if !foundLint {
		t.Error("expected parallel_child edge to lint")
	}
	if !foundFmt {
		t.Error("expected parallel_child edge to fmt")
	}
}

func TestBuildPipelineGraphProducesInputRefEdges(t *testing.T) {
	src := `
pipeline test {
    step build {
        invoke: "ci:build"
    }
    step test {
        invoke: "ci:test"
        input: {
            artifact: build.output
        }
    }
}
`
	cfg, err := config.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	desc, _ := config.PipelineDescFromAST(cfg.Pipelines[0])
	graph := binding.BuildPipelineGraph(desc)

	found := false
	for _, e := range graph.Edges {
		if e.Kind == binding.GraphEdgeKindInputRef && e.From == "build" && e.To == "test" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected input_ref edge build -> test")
	}
}

func TestBuildPipelineGraphContainsNoRuntimeOnlyFields(t *testing.T) {
	src := `
pipeline test {
    step build {
        invoke: "ci:build"
        timeout: "10m"
    }
}
`
	cfg, err := config.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	desc, _ := config.PipelineDescFromAST(cfg.Pipelines[0])
	graph := binding.BuildPipelineGraph(desc)

	for _, n := range graph.Nodes {
		if n.ID == "" {
			t.Errorf("node has empty ID: %+v", n)
		}
		if n.Kind != "step" && n.Kind != binding.GraphNodeKindParallel {
			t.Errorf("unexpected node kind %q", n.Kind)
		}
	}
	for _, e := range graph.Edges {
		if e.Kind != "depends_on" && e.Kind != binding.GraphEdgeKindParallelChild && e.Kind != binding.GraphEdgeKindInputRef {
			t.Errorf("unexpected edge kind %q", e.Kind)
		}
	}
}

func TestBuildPipelineGraphNoDuplicateEdges(t *testing.T) {
	src := `
pipeline test {
    step build {
        invoke: "ci:build"
    }
    step test {
        invoke: "ci:test"
        depends_on: [build]
        input: {
            artifact: build.output
        }
    }
}
`
	cfg, err := config.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	desc, _ := config.PipelineDescFromAST(cfg.Pipelines[0])
	graph := binding.BuildPipelineGraph(desc)

	// build -> test must retain both semantic edges (one depends_on, one input_ref),
	// and each kind must still be deduplicated individually.
	dependsCount := 0
	inputRefCount := 0
	for _, e := range graph.Edges {
		if e.From == "build" && e.To == "test" {
			switch e.Kind {
			case binding.GraphEdgeKindDependsOn:
				dependsCount++
			case binding.GraphEdgeKindInputRef:
				inputRefCount++
			}
		}
	}
	if dependsCount != 1 {
		t.Errorf("expected 1 depends_on edge from build to test, got %d", dependsCount)
	}
	if inputRefCount != 1 {
		t.Errorf("expected 1 input_ref edge from build to test, got %d", inputRefCount)
	}
}

// TestBuildPipelineGraphKeepsDependsOnAndInputRefSeparate is the regression guard for
// the edge-dedup bug: dedup must be keyed per semantic edge (kind + from + to). When the
// same node pair is connected by both a depends_on edge and an input_ref edge, dropping
// either one silently loses a hop in graph traversal.
func TestBuildPipelineGraphKeepsDependsOnAndInputRefSeparate(t *testing.T) {
	src := `
pipeline test {
    step build {
        invoke: "ci:build"
    }
    step test {
        invoke: "ci:test"
        depends_on: [build]
        input: {
            artifact: build.output
        }
    }
}
`
	cfg, err := config.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	desc, _ := config.PipelineDescFromAST(cfg.Pipelines[0])
	graph := binding.BuildPipelineGraph(desc)

	kinds := make(map[string]int)
	for _, e := range graph.Edges {
		if e.From == "build" && e.To == "test" {
			kinds[e.Kind]++
		}
	}
	if kinds[binding.GraphEdgeKindDependsOn] != 1 {
		t.Errorf("expected depends_on edge build -> test to be retained, got %d", kinds[binding.GraphEdgeKindDependsOn])
	}
	if kinds[binding.GraphEdgeKindInputRef] != 1 {
		t.Errorf("expected input_ref edge build -> test to be retained, got %d", kinds[binding.GraphEdgeKindInputRef])
	}
	if total := kinds[binding.GraphEdgeKindDependsOn] + kinds[binding.GraphEdgeKindInputRef]; total != 2 {
		t.Errorf("expected both semantic edges build -> test, got %d", total)
	}
}

// TestBuildPipelineGraphDedupesWithinSameKind guards the other half of the contract: while
// distinct kinds coexist for a pair, repeated edges of the *same* kind must still collapse.
func TestBuildPipelineGraphDedupesWithinSameKind(t *testing.T) {
	src := `
pipeline test {
    step build {
        invoke: "ci:build"
    }
    step test {
        invoke: "ci:test"
        depends_on: [build]
        input: {
            artifact: build.output
            nested: {
                again: build.output
            }
            list: [build.output, build.output]
        }
    }
}
`
	cfg, err := config.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	desc, _ := config.PipelineDescFromAST(cfg.Pipelines[0])
	graph := binding.BuildPipelineGraph(desc)

	dependsCount := 0
	inputRefCount := 0
	for _, e := range graph.Edges {
		if e.From != "build" || e.To != "test" {
			continue
		}
		switch e.Kind {
		case binding.GraphEdgeKindDependsOn:
			dependsCount++
		case binding.GraphEdgeKindInputRef:
			inputRefCount++
		}
	}
	if dependsCount != 1 {
		t.Errorf("expected repeated depends_on to collapse to 1 edge, got %d", dependsCount)
	}
	if inputRefCount != 1 {
		t.Errorf("expected repeated input_ref to collapse to 1 edge, got %d", inputRefCount)
	}
}
