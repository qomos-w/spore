package frontend

import (
	"fmt"
	"sort"
	"strings"

	"github.com/qomos-w/spore/schema"
)

// ModuleGraph records dependency edges between resolved modules.
// An edge A -> B means module A imports (depends on) module B.
type ModuleGraph struct {
	edges map[string]map[string]bool // from -> set of to
}

// newModuleGraph creates an empty module graph.
func newModuleGraph() *ModuleGraph {
	return &ModuleGraph{edges: make(map[string]map[string]bool)}
}

// addEdge records a dependency from -> to. Duplicate edges are ignored.
func (g *ModuleGraph) addEdge(from, to string) {
	if g.edges[from] == nil {
		g.edges[from] = make(map[string]bool)
	}
	g.edges[from][to] = true
}

// Modules returns all module paths present in the graph, sorted.
func (g *ModuleGraph) Modules() []string {
	set := make(map[string]bool)
	for from, tos := range g.edges {
		set[from] = true
		for to := range tos {
			set[to] = true
		}
	}
	result := make([]string, 0, len(set))
	for path := range set {
		result = append(result, path)
	}
	sort.Strings(result)
	return result
}

// Dependencies returns the direct dependencies of a module, sorted.
// If the module has no recorded dependencies, returns nil.
func (g *ModuleGraph) Dependencies(path string) []string {
	tos, ok := g.edges[path]
	if !ok || len(tos) == 0 {
		return nil
	}
	result := make([]string, 0, len(tos))
	for to := range tos {
		result = append(result, to)
	}
	sort.Strings(result)
	return result
}

// ReverseDependencies returns modules that directly depend on the given path, sorted.
func (g *ModuleGraph) ReverseDependencies(path string) []string {
	var result []string
	for from, tos := range g.edges {
		if tos[path] {
			result = append(result, from)
		}
	}
	sort.Strings(result)
	return result
}

// HasCycle reports whether the dependency graph contains any cycle.
func (g *ModuleGraph) HasCycle() bool {
	visited := make(map[string]bool)
	recStack := make(map[string]bool)

	var dfs func(string) bool
	dfs = func(node string) bool {
		visited[node] = true
		recStack[node] = true
		for to := range g.edges[node] {
			if !visited[to] {
				if dfs(to) {
					return true
				}
			} else if recStack[to] {
				return true
			}
		}
		recStack[node] = false
		return false
	}

	for from := range g.edges {
		if !visited[from] {
			if dfs(from) {
				return true
			}
		}
	}
	return false
}

// CyclePath returns one cyclic path in the graph as a slice of module paths,
// or nil if the graph is acyclic.
func (g *ModuleGraph) CyclePath() []string {
	visited := make(map[string]bool)
	recStack := make(map[string]bool)
	parent := make(map[string]string)

	var dfs func(string) []string
	dfs = func(node string) []string {
		visited[node] = true
		recStack[node] = true
		for to := range g.edges[node] {
			if !visited[to] {
				parent[to] = node
				if cycle := dfs(to); cycle != nil {
					return cycle
				}
			} else if recStack[to] {
				// Found cycle: reconstruct path from to back to to
				path := []string{to}
				cur := node
				for cur != to {
					path = append(path, cur)
					cur = parent[cur]
					if cur == "" {
						break
					}
				}
				path = append(path, to)
				// Reverse to get forward direction
				for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
					path[i], path[j] = path[j], path[i]
				}
				return path
			}
		}
		recStack[node] = false
		return nil
	}

	for from := range g.edges {
		if !visited[from] {
			if cycle := dfs(from); cycle != nil {
				return cycle
			}
		}
	}
	return nil
}

// TopologicalOrder returns modules in dependency order (dependencies first),
// or an error if the graph contains a cycle.
func (g *ModuleGraph) TopologicalOrder() ([]string, error) {
	if g.HasCycle() {
		return nil, fmt.Errorf("cannot compute topological order: graph contains a cycle")
	}

	// Kahn's algorithm
	inDegree := make(map[string]int)
	modules := g.Modules()
	for _, m := range modules {
		if inDegree[m] == 0 {
			inDegree[m] = 0
		}
	}
	for _, tos := range g.edges {
		for to := range tos {
			inDegree[to]++
		}
	}

	var queue []string
	for m, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, m)
		}
	}
	sort.Strings(queue)

	var result []string
	for len(queue) > 0 {
		// Pop front
		node := queue[0]
		queue = queue[1:]
		result = append(result, node)

		for to := range g.edges[node] {
			inDegree[to]--
			if inDegree[to] == 0 {
				queue = append(queue, to)
			}
		}
		sort.Strings(queue)
	}
	return result, nil
}

// ModuleExports describes all exported symbols from a single resolved module.
type ModuleExports struct {
	Path       string
	Callables  []schema.CallableDesc
	Variables  []ExportedVariableMetadata
	Objects    []schema.ObjectDesc
	Interfaces []schema.InterfaceDesc
	Types      []ExportedTypeMetadata
	Enums      []schema.EnumDesc
}

// SporeSyntax returns all exports in sporescript-style, one per line.
func (m ModuleExports) SporeSyntax() string {
	return ModuleExportsSporeSyntax(m)
}

// String returns a human-readable summary of the graph.
func (g *ModuleGraph) String() string {
	mods := g.Modules()
	if len(mods) == 0 {
		return "(empty graph)"
	}
	var b strings.Builder
	for _, from := range mods {
		deps := g.Dependencies(from)
		if len(deps) == 0 {
			continue
		}
		b.WriteString(from)
		b.WriteString(" -> ")
		for i, d := range deps {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(d)
		}
		b.WriteString("\n")
	}
	return b.String()
}
