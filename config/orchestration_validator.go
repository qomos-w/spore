package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/qomos-w/spore/schema"
)

// SchemaResolver resolves capability schemas and callables for registry-aware
// pipeline validation. Host systems provide an implementation.
type SchemaResolver interface {
	ResolveSchema(schemaID string) bool
	ResolveCallable(schemaID, callableName string) (schema.CallableDesc, bool)
}

// ValidatePipelines returns a validator that checks pipeline declarations for
// topology errors, invoke format, and dependency correctness.
// It performs shape validation only; no registry lookup is attempted.
func ValidatePipelines() Validator {
	return func(cfg *Config, target any) []Diagnostic {
		var diags []Diagnostic
		for _, pl := range cfg.Pipelines {
			diags = append(diags, validatePipelineWithResolver(pl, nil)...)
		}
		return diags
	}
}

// ValidatePipelinesWithResolver returns a validator that checks pipeline
// declarations using a schema resolver for capability/callable existence and
// input shape validation.
func ValidatePipelinesWithResolver(resolver SchemaResolver) Validator {
	return func(cfg *Config, target any) []Diagnostic {
		var diags []Diagnostic
		for _, pl := range cfg.Pipelines {
			diags = append(diags, validatePipelineWithResolver(pl, resolver)...)
		}
		return diags
	}
}

func validatePipelineWithResolver(pl PipelineAST, resolver SchemaResolver) []Diagnostic {
	var diags []Diagnostic

	// Collect all step names (including parallel children) for uniqueness checks.
	stepNames := make(map[string]int) // name -> line
	// Collect top-level step names for dependency checks.
	topLevelNames := make(map[string]bool)
	// Build dependency graph: step name -> depends_on
	deps := make(map[string][]string)

	var collectSteps func(steps []PipelineStepAST, isTopLevel bool)
	collectSteps = func(steps []PipelineStepAST, isTopLevel bool) {
		for _, step := range steps {
			if step.Name != "" {
				if prevLine, exists := stepNames[step.Name]; exists {
					diags = append(diags, Diagnostic{
						Code:     "pipeline_duplicate_step",
						Category: "validate",
						Severity: "error",
						Message:  fmt.Sprintf("duplicate step name %q in pipeline %q", step.Name, pl.Name),
						Hint:     "rename the step or remove the duplicate",
						Line:     step.Line,
						Field:    step.Name,
						Actual:   fmt.Sprintf("duplicate at line %d (first at line %d)", step.Line, prevLine),
					})
				} else {
					stepNames[step.Name] = step.Line
				}
				if isTopLevel {
					topLevelNames[step.Name] = true
					if len(step.DependsOn) > 0 {
						deps[step.Name] = append([]string(nil), step.DependsOn...)
					}
				}
			}
			// Recurse into parallel children.
			if len(step.Parallel) > 0 {
				collectSteps(step.Parallel, false)
			}
		}
	}
	collectSteps(pl.Steps, true)

	// Validate each step.
	var validateStep func(step PipelineStepAST)
	validateStep = func(step PipelineStepAST) {
		if step.Name == "" {
			return // synthetic parallel parent
		}

		// invoke is required.
		if step.Invoke == "" {
			diags = append(diags, Diagnostic{
				Code:     "pipeline_missing_invoke",
				Category: "validate",
				Severity: "error",
				Message:  fmt.Sprintf("step %q in pipeline %q is missing required field %q", step.Name, pl.Name, PipelineStepFieldInvoke),
				Hint:     pipelineStepFieldHint(PipelineStepFieldInvoke),
				Line:     step.Line,
				Field:    step.Name,
			})
			return
		}

		// Validate invoke format.
		if !isValidInvokeRef(step.Invoke) {
			diags = append(diags, Diagnostic{
				Code:     "pipeline_invalid_invoke_ref",
				Category: "validate",
				Severity: "error",
				Message:  fmt.Sprintf("step %q in pipeline %q has invalid %s ref %q", step.Name, pl.Name, PipelineStepFieldInvoke, step.Invoke),
				Hint:     fmt.Sprintf("use format \"<schemaID>:<callableName>\" or \"tool:<toolName>\" for %s", PipelineStepFieldInvoke),
				Line:     step.Line,
				Field:    step.Name,
				Actual:   step.Invoke,
			})
			return
		}

		// Registry-aware validation.
		if resolver != nil {
			schemaID, callableName := splitInvokeRef(step.Invoke)
			if schemaID == "tool" {
				// tool: refs are validated only by format; no schema lookup.
			} else {
				if !resolver.ResolveSchema(schemaID) {
					diags = append(diags, Diagnostic{
						Code:     "pipeline_unknown_schema",
						Category: "validate",
						Severity: "error",
						Message:  fmt.Sprintf("step %q in pipeline %q references unknown schema %q", step.Name, pl.Name, schemaID),
						Hint:     "ensure the capability schema is registered",
						Line:     step.Line,
						Field:    step.Name,
						Actual:   schemaID,
					})
				} else {
					callableDesc, found := resolver.ResolveCallable(schemaID, callableName)
					if !found {
						diags = append(diags, Diagnostic{
							Code:     "pipeline_unknown_callable",
							Category: "validate",
							Severity: "error",
							Message:  fmt.Sprintf("step %q in pipeline %q references unknown callable %q in schema %q", step.Name, pl.Name, callableName, schemaID),
							Hint:     "ensure the callable is declared in the capability schema",
							Line:     step.Line,
							Field:    step.Name,
							Actual:   callableName,
						})
					} else {
						// Validate input keys match callable parameters.
						if step.Input.Kind == ValueMap {
							paramNames := make(map[string]bool)
							for _, p := range callableDesc.Parameters {
								paramNames[p.Name] = true
							}
							for _, kv := range step.Input.Entries {
								if !paramNames[kv.Key] {
									diags = append(diags, Diagnostic{
										Code:     "pipeline_input_mismatch",
										Category: "validate",
										Severity: "warning",
										Message:  fmt.Sprintf("step %q input key %q does not match any parameter of callable %q", step.Name, kv.Key, callableName),
										Hint:     fmt.Sprintf("valid parameters: %s", paramList(callableDesc.Parameters)),
										Line:     kv.Line,
										Field:    step.Name,
										Actual:   kv.Key,
									})
								}
							}
						}
					}
				}
			}
		}

		// Validate timeout format if present.
		if step.Timeout != "" {
			if _, err := time.ParseDuration(step.Timeout); err != nil {
				diags = append(diags, Diagnostic{
					Code:     "pipeline_invalid_timeout",
					Category: "validate",
					Severity: "error",
					Message:  fmt.Sprintf("step %q in pipeline %q has invalid timeout %q", step.Name, pl.Name, step.Timeout),
					Hint:     "use a valid Go duration string like \"10m\", \"30s\", \"1h30m\"",
					Line:     step.Line,
					Field:    step.Name,
					Actual:   step.Timeout,
				})
			}
		}

		// Validate depends_on references.
		for _, dep := range step.DependsOn {
			if !topLevelNames[dep] {
				diags = append(diags, Diagnostic{
					Code:     "pipeline_unknown_dependency",
					Category: "validate",
					Severity: "error",
					Message:  fmt.Sprintf("step %q in pipeline %q depends on unknown step %q", step.Name, pl.Name, dep),
					Hint:     "ensure the referenced step exists in the pipeline",
					Line:     step.Line,
					Field:    step.Name,
					Actual:   dep,
				})
			}
		}
	}

	var validateSteps func(steps []PipelineStepAST)
	validateSteps = func(steps []PipelineStepAST) {
		for _, step := range steps {
			validateStep(step)
			if len(step.Parallel) > 0 {
				validateSteps(step.Parallel)
			}
		}
	}
	validateSteps(pl.Steps)

	// Check for dependency cycles.
	if cycle := findDependencyCycle(topLevelNames, deps); cycle != "" {
		diags = append(diags, Diagnostic{
			Code:     "pipeline_dependency_cycle",
			Category: "validate",
			Severity: "error",
			Message:  fmt.Sprintf("pipeline %q contains a dependency cycle: %s", pl.Name, cycle),
			Hint:     "remove circular dependencies so steps form a DAG",
			Field:    pl.Name,
		})
	}

	return diags
}

// isValidInvokeRef checks that invoke matches "schemaID:callableName" or "tool:toolName".
func isValidInvokeRef(invoke string) bool {
	if invoke == "" {
		return false
	}
	parts := strings.Split(invoke, ":")
	if len(parts) != 2 {
		return false
	}
	return parts[0] != "" && parts[1] != ""
}

// splitInvokeRef splits an invoke ref into schemaID and callableName.
func splitInvokeRef(invoke string) (string, string) {
	parts := strings.SplitN(invoke, ":", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return parts[0], parts[1]
}

// paramList formats parameter names as a comma-separated string.
func paramList(params []schema.ParameterDesc) string {
	names := make([]string, len(params))
	for i, p := range params {
		names[i] = p.Name
	}
	return strings.Join(names, ", ")
}

// findDependencyCycle detects cycles in the dependency graph using DFS.
func findDependencyCycle(nodes map[string]bool, deps map[string][]string) string {
	visited := make(map[string]bool)
	recStack := make(map[string]bool)

	var dfs func(node string, path []string) (string, bool)
	dfs = func(node string, path []string) (string, bool) {
		visited[node] = true
		recStack[node] = true
		path = append(path, node)

		for _, dep := range deps[node] {
			if dep == node {
				// Self-dependency
				return node + " -> " + node, true
			}
			if !nodes[dep] {
				// Skip dependencies to steps outside the pipeline; these are
				// reported as pipeline_unknown_dependency, not as a cycle.
				continue
			}
			if recStack[dep] {
				// Found cycle
				cycle := strings.Join(path, " -> ") + " -> " + dep
				return cycle, true
			}
			if !visited[dep] {
				if cycle, found := dfs(dep, path); found {
					return cycle, true
				}
			}
		}

		recStack[node] = false
		return "", false
	}

	for node := range nodes {
		if !visited[node] {
			if cycle, found := dfs(node, nil); found {
				return cycle
			}
		}
	}
	return ""
}
