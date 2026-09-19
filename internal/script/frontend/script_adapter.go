package frontend

import (
	"fmt"
	"sync"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/diagnostics"
	"github.com/qomos-w/spore/schema"
)

// ScriptCallableAdapter implements binding.ExecutableAdapter for a
// script-defined callable. It holds the compiled callable declaration
// and delegates execution to the internal runtime backend, enriching error
// diagnostics with script-specific context (diagnostic code, body
// location, operand information).
//
// ScriptCallableAdapter is an internal implementation of the
// ExecutableAdapter SPI — external consumers interact with it only
// through the ExecutableAdapter interface.
type ScriptCallableAdapter struct {
	desc    schema.CallableDesc
	runtime ScriptRuntimeBackend
	// results caches the (immutable) success InvocationResultDesc per
	// stage: it is derived only from desc, so it is computed once instead
	// of being re-validated and re-cloned on every invoke.
	results sync.Map // InvocationStage → InvocationResultDesc
}

// NewScriptCallableAdapter creates a ScriptCallableAdapter for a
// compiled script-defined callable. The runtime backend may be nil,
// in which case invocation will produce an error result with the
// no_evaluator diagnostic code.
func NewScriptCallableAdapter(desc schema.CallableDesc, runtime ScriptRuntimeBackend) (*ScriptCallableAdapter, error) {
	if err := schema.ValidateCallableDesc(desc); err != nil {
		return nil, err
	}
	return &ScriptCallableAdapter{
		desc:    schema.CloneCallableDesc(desc),
		runtime: runtime,
	}, nil
}

func (a *ScriptCallableAdapter) Callable() schema.CallableDesc {
	if a == nil {
		return schema.CallableDesc{}
	}
	return schema.CloneCallableDesc(a.desc)
}

func (a *ScriptCallableAdapter) Invoke(req binding.InvocationRequest) (binding.InvocationOutcome, error) {
	if a == nil {
		return binding.InvocationOutcome{}, fmt.Errorf("script callable adapter is nil")
	}
	if req.Callable != a.desc.Name {
		return binding.InvocationOutcome{}, fmt.Errorf("adapter for %q cannot handle %q", a.desc.Name, req.Callable)
	}
	if err := binding.ValidateInvocationStage(a.desc, req.Stage); err != nil {
		return binding.InvocationOutcome{}, err
	}
	if err := binding.ValidateInvocationArgs(a.desc, req.Args); err != nil {
		return binding.InvocationOutcome{}, err
	}

	var (
		payload any
		callErr error
	)
	if a.runtime == nil {
		callErr = noEvaluatorError(a.desc.Name)
	} else if runtime, ok := a.runtime.(ContextScriptRuntimeBackend); ok {
		payload, callErr = runtime.EvaluateContext(req.Context, req.Budget, a.desc.Name, req.Stage, req.Args)
	} else {
		payload, callErr = a.runtime.Evaluate(a.desc.Name, req.Stage, req.Args)
	}

	if callErr != nil {
		base := diagnostics.FromError(callErr, diagnostics.Descriptor{
			Category: diagnostics.CategoryRuntime,
			Callable: a.desc.Name,
			Stage:    string(req.Stage),
		})
		if len(base.Stack) == 0 {
			base.Stack = []diagnostics.Frame{{Callable: a.desc.Name, Stage: string(req.Stage)}}
		}
		result, err := binding.NewInvocationErrorDescWithDiagnostic(a.desc, req.Stage, base)
		if err != nil {
			return binding.InvocationOutcome{}, err
		}
		return binding.NewInvocationOutcome(result, nil)
	}
	if cached, ok := a.results.Load(req.Stage); ok {
		return binding.NewInvocationOutcome(cached.(binding.InvocationResultDesc), payload)
	}
	result, err := binding.DescribeInvocationResult(a.desc, req.Stage)
	if err != nil {
		return binding.InvocationOutcome{}, err
	}
	a.results.Store(req.Stage, result)
	return binding.NewInvocationOutcome(result, payload)
}
