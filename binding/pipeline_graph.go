package binding

import (
	"fmt"
	"strings"
)

// PipelineGraph is a declaration graph for a pipeline, not runtime state.
type PipelineGraph struct {
	Nodes []PipelineGraphNode
	Edges []PipelineGraphEdge
}

// Graph node and edge kind constants.
const (
	GraphNodeKindStep     = "step"
	GraphNodeKindParallel = "parallel"

	GraphEdgeKindDependsOn     = "depends_on"
	GraphEdgeKindParallelChild = "parallel_child"
	GraphEdgeKindInputRef      = "input_ref"
)

// PipelineGraphNode represents a node in the pipeline declaration graph.
type PipelineGraphNode struct {
	ID     string
	Label  string
	Invoke string
	Kind   string // GraphNodeKindStep | GraphNodeKindParallel
}

// PipelineGraphEdge represents a directed edge in the pipeline declaration graph.
type PipelineGraphEdge struct {
	From string
	To   string
	Kind string // GraphEdgeKindDependsOn | GraphEdgeKindParallelChild | GraphEdgeKindInputRef
}

// BuildPipelineGraph produces a declaration DAG from a PipelineDesc.
func BuildPipelineGraph(desc PipelineDesc) PipelineGraph {
	var nodes []PipelineGraphNode
	var edges []PipelineGraphEdge
	nodeIDs := make(map[string]bool)
	edgeSet := make(map[string]bool)

	var addNode func(id, label, invoke, kind string)
	addNode = func(id, label, invoke, kind string) {
		if id == "" || nodeIDs[id] {
			return
		}
		nodeIDs[id] = true
		nodes = append(nodes, PipelineGraphNode{ID: id, Label: label, Invoke: invoke, Kind: kind})
	}

	var addEdge func(from, to, kind string)
	addEdge = func(from, to, kind string) {
		if from == "" || to == "" || from == to {
			return
		}
		// Deduplicate by (from, to) regardless of kind — one edge per node pair.
		key := from + "|" + to
		if edgeSet[key] {
			return
		}
		edgeSet[key] = true
		edges = append(edges, PipelineGraphEdge{From: from, To: to, Kind: kind})
	}

	// Collect all step names first so we know which refs are internal.
	var collectNames func(steps []PipelineStepDesc, names map[string]bool)
	collectNames = func(steps []PipelineStepDesc, names map[string]bool) {
		for _, step := range steps {
			if step.Name != "" {
				names[step.Name] = true
			}
			collectNames(step.Parallel, names)
		}
	}
	stepNames := make(map[string]bool)
	collectNames(desc.Steps, stepNames)

	parallelCounter := 0
	nextParallelID := func() string {
		id := fmt.Sprintf("parallel-%d", parallelCounter)
		parallelCounter++
		return id
	}

	var processSteps func(steps []PipelineStepDesc, parentParallelID string)
	processSteps = func(steps []PipelineStepDesc, parentParallelID string) {
		for _, step := range steps {
			if len(step.Parallel) > 0 {
				// Synthetic parallel node.
				parallelID := nextParallelID()
				addNode(parallelID, parallelID, "", GraphNodeKindParallel)
				if parentParallelID != "" {
					addEdge(parentParallelID, parallelID, GraphEdgeKindParallelChild)
				}
				processSteps(step.Parallel, parallelID)
			} else {
				if step.Name == "" {
					continue
				}
				addNode(step.Name, step.Name, step.Invoke, GraphNodeKindStep)
				if parentParallelID != "" {
					addEdge(parentParallelID, step.Name, GraphEdgeKindParallelChild)
				}
				// depends_on edges.
				for _, dep := range step.DependsOn {
					if stepNames[dep] {
						addEdge(dep, step.Name, GraphEdgeKindDependsOn)
					}
				}
				// input_ref edges.
				findInputRefs(step.Input, func(refStep string) {
					if stepNames[refStep] && refStep != step.Name {
						addEdge(refStep, step.Name, GraphEdgeKindInputRef)
					}
				})
			}
		}
	}
	processSteps(desc.Steps, "")

	return PipelineGraph{Nodes: nodes, Edges: edges}
}

// findInputRefs recursively walks a pipeline input map and calls fn for each
// referenced step name found in PipelineRef values.
func findInputRefs(input map[string]any, fn func(stepName string)) {
	for _, v := range input {
		findRefInValue(v, fn)
	}
}

func findRefInValue(v any, fn func(stepName string)) {
	switch val := v.(type) {
	case PipelineRef:
		if stepName := extractStepNameFromRef(val.Expr); stepName != "" {
			fn(stepName)
		}
	case map[string]any:
		findInputRefs(val, fn)
	case []any:
		for _, item := range val {
			findRefInValue(item, fn)
		}
	}
}

// extractStepNameFromRef extracts the leading step name from a ref expression.
// e.g. "generate.output.pending" -> "generate"
func extractStepNameFromRef(expr string) string {
	parts := strings.SplitN(expr, ".", 2)
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}
