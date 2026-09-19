package frontend

import (
	"context"
	"fmt"
	"github.com/qomos-w/spore/invoke"
)

// CompiledVMLoweringBackend is the frontend-owned seam for VM backends that can
// accept a precompiled frontend lowering result without exposing frontend AST
// as a wider cross-package contract.
type CompiledVMLoweringBackend interface {
	CompileLoweredProgram(compiled CompiledDeclarations, prog *Program) error
}

// VMLoweringBackend is the frontend-owned seam for VM-backed lowering. Backends
// may compile a parsed program into executable runtime state, but they do not
// own the canonical syntax/profile boundary.
type VMLoweringBackend interface {
	CompileProgram(prog *Program) error
}

// ScriptRuntimeBackend is the internal runtime capability used by
// ScriptCallableAdapter. It stays inside the frontend-owned script execution seam
// and does not restore a public evaluator API.
type ScriptRuntimeBackend interface {
	Evaluate(callable string, stage invoke.InvocationStage, args []any) (any, error)
	Reset(callable string, args []any)
}

type ContextScriptRuntimeBackend interface {
	ScriptRuntimeBackend
	EvaluateContext(ctx context.Context, budget invoke.ExecutionBudget, callable string, stage invoke.InvocationStage, args []any) (any, error)
}

func runtimeBackendFromVMLowering(backend VMLoweringBackend) ScriptRuntimeBackend {
	if backend == nil {
		return nil
	}
	runtime, _ := backend.(ScriptRuntimeBackend)
	return runtime
}

// CompileProgramForVM is the frontend-owned lowering entrypoint for VM-backed
// execution. Frontend retains ownership of the canonical parse/lower handoff;
// the concrete VM backend is injected through this seam.
func CompileProgramForVM(compiled CompiledDeclarations, prog *Program, backend VMLoweringBackend) error {
	if prog == nil {
		return nil
	}
	if backend == nil {
		return fmt.Errorf("vm lowering backend is not configured")
	}
	if compiledBackend, ok := backend.(CompiledVMLoweringBackend); ok {
		return compiledBackend.CompileLoweredProgram(compiled, prog)
	}
	return backend.CompileProgram(prog)
}
