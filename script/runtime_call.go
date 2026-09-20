package script

import (
	"context"
	"fmt"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/schema"
)

// This file holds the Runtime call surface: callable discovery and invocation
// dispatch.

// CallableInfo is the host-facing callable discovery surface exposed by Runtime.
type CallableInfo struct {
	Name       string
	Mode       schema.CallableMode
	Parameters []schema.ParameterDesc
	Returns    []schema.TypeDesc
	ReqDesc    *schema.ObjectDesc
	RespDesc   *schema.ObjectDesc
}

// Exports returns the exported callable discovery surface for a module. The
// module argument may be the root module or any module resolved through
// imports; Call only operates on the root module, but Exports lets hosts
// inspect imported modules' surfaces too.
func (rt *Runtime) Exports(module string) ([]CallableInfo, error) {
	if rt == nil || rt.frontend == nil {
		return nil, fmt.Errorf("runtime is not initialized")
	}
	if rt.rootModule == "" {
		return nil, fmt.Errorf("runtime has no module loaded")
	}
	if module == "" {
		return nil, fmt.Errorf("module name cannot be empty")
	}
	if module == rt.rootModule {
		return callableInfosFromDescs(rt.frontend.ExportedCallables()), nil
	}
	exports, ok := rt.frontend.ModuleExports(module)
	if !ok {
		return nil, fmt.Errorf("module %q not found", module)
	}
	return callableInfosFromDescs(exports.Callables), nil
}

// LookupCallable looks up one callable by module and name. The module may be
// the root or any module reachable through imports.
func (rt *Runtime) LookupCallable(module, callable string) (CallableInfo, error) {
	if callable == "" {
		return CallableInfo{}, fmt.Errorf("callable name cannot be empty")
	}
	infos, err := rt.Exports(module)
	if err != nil {
		return CallableInfo{}, err
	}
	for _, info := range infos {
		if info.Name == callable {
			return info, nil
		}
	}
	return CallableInfo{}, fmt.Errorf("callable %q not found in module %q", callable, module)
}

// Call invokes a callable in the loaded root module and returns a public
// Result. Only callables exported by the root module are valid entry points;
// imported modules' callables are reachable transitively from root code, not
// directly through Call. A returned (Result{}, error) pair signals a
// host-side or invocation-pipeline failure; a Result with non-nil Error
// signals a structured runtime error raised inside the called code.
//
// If multi-module entry points become a first-class concern in the future, a
// CallIn(module, callable, args...) extension can be added; today the public
// surface deliberately exposes only the root path to avoid an ambiguous
// module argument that LLMs could read as a context switch.
func (rt *Runtime) Call(callable string, args ...any) (Result, error) {
	return rt.CallContext(CallContext{}, callable, args...)
}

// CallContext invokes a callable with host cancellation and execution limits.
func (rt *Runtime) CallContext(call CallContext, callable string, args ...any) (Result, error) {
	ctx, cancel := call.context()
	defer cancel()
	return rt.callStageContext(ctx, binding.ExecutionBudget(call.Budget), callable, binding.InvocationStageUnary, args)
}

// CallNext invokes the 'next' stage of a streaming callable in the loaded
// root module and returns a public Result. This is the streaming counterpart
// to Call: it delivers intermediate (delta) values from a stream fun.
//
// CallNext returns the same error/result contract as Call. If the named
// callable is not a streaming callable, the invocation is rejected with a
// structured runtime error (test with result.Ok or IsRuntimeError).
func (rt *Runtime) CallNext(callable string, args ...any) (Result, error) {
	return rt.callStage(callable, binding.InvocationStageNext, args)
}

// CallFinal invokes the 'final' stage of a streaming callable in the loaded
// root module and returns a public Result. This produces the terminal value
// that ends the stream.
//
// CallFinal returns the same error/result contract as Call. If the named
// callable is not a streaming callable, the invocation is rejected with a
// structured runtime error.
func (rt *Runtime) CallFinal(callable string, args ...any) (Result, error) {
	return rt.callStage(callable, binding.InvocationStageFinal, args)
}

func (rt *Runtime) callStage(callable string, stage binding.InvocationStage, args []any) (Result, error) {
	return rt.callStageContext(context.Background(), binding.ExecutionBudget{}, callable, stage, args)
}

func (rt *Runtime) callStageContext(ctx context.Context, budget binding.ExecutionBudget, callable string, stage binding.InvocationStage, args []any) (Result, error) {
	if rt == nil {
		return Result{err: &RuntimeError{Err: fmt.Errorf("runtime is not initialized")}}, &RuntimeError{Err: fmt.Errorf("runtime is not initialized")}
	}
	if rt.closed {
		return Result{err: &RuntimeError{Err: fmt.Errorf("runtime is closed")}}, &RuntimeError{Err: fmt.Errorf("runtime is closed")}
	}
	if rt.frontend == nil {
		return Result{err: &RuntimeError{Err: fmt.Errorf("runtime is not initialized")}}, &RuntimeError{Err: fmt.Errorf("runtime is not initialized")}
	}
	if rt.rootModule == "" {
		return Result{err: &RuntimeError{Err: fmt.Errorf("runtime has no module loaded")}}, &RuntimeError{Err: fmt.Errorf("runtime has no module loaded")}
	}
	if callable == "" {
		return Result{err: &RuntimeError{Err: fmt.Errorf("callable name cannot be empty")}}, &RuntimeError{Err: fmt.Errorf("callable name cannot be empty")}
	}
	outcome, err := rt.frontend.InvokeStageContext(ctx, budget, callable, stage, args)
	if err != nil {
		rtErr := wrapRuntimeError(err)
		return Result{err: rtErr}, rtErr
	}
	if outcome.Result.Kind == binding.InvocationResultError {
		return Result{Error: runtimeErrorFromOutcome(outcome)}, nil
	}
	if outcome.Payload == nil {
		return Result{}, nil
	}
	return Result{Value: outcome.Payload.Value}, nil
}

// CallableHandle is a host-facing handle that pre-resolves a callable lookup
// so repeated invocations skip the discovery roundtrip. It is an optional
// convenience over Call; Call remains the canonical entry point.
type CallableHandle struct {
	rt       *Runtime
	callable string
	info     CallableInfo
}

// PrepareCallable resolves the named root callable once and returns a
// CallableHandle that can invoke it repeatedly. Lookup failures are returned
// up front rather than on every call. As with Call, only callables exported
// by the root module are valid handles.
func (rt *Runtime) PrepareCallable(callable string) (CallableHandle, error) {
	if rt == nil || rt.frontend == nil {
		return CallableHandle{}, fmt.Errorf("runtime is not initialized")
	}
	if rt.rootModule == "" {
		return CallableHandle{}, fmt.Errorf("runtime has no module loaded")
	}
	info, err := rt.LookupCallable(rt.rootModule, callable)
	if err != nil {
		return CallableHandle{}, err
	}
	return CallableHandle{rt: rt, callable: callable, info: info}, nil
}

// Name returns the callable name the handle was prepared against.
func (h CallableHandle) Name() string { return h.callable }

// Info returns a copy of the callable descriptor captured when the handle was
// prepared. Use this for argument/return shape introspection without re-doing
// LookupCallable.
func (h CallableHandle) Info() CallableInfo { return h.info }

// Invoke calls the prepared callable with the given arguments. The semantics
// are identical to Runtime.Call: a non-nil error signals a host-side or
// pipeline failure; a Result with non-nil Error signals a structured runtime
// error from the called code.
func (h CallableHandle) Invoke(args ...any) (Result, error) {
	if h.rt == nil {
		return Result{}, &RuntimeError{Err: fmt.Errorf("callable handle is not initialized")}
	}
	return h.rt.Call(h.callable, args...)
}

// Next invokes the 'next' stage of a streaming callable through this handle.
// The semantics are identical to Runtime.CallNext.
func (h CallableHandle) Next(args ...any) (Result, error) {
	if h.rt == nil {
		return Result{}, &RuntimeError{Err: fmt.Errorf("callable handle is not initialized")}
	}
	return h.rt.CallNext(h.callable, args...)
}

// Final invokes the 'final' stage of a streaming callable through this handle.
// The semantics are identical to Runtime.CallFinal.
func (h CallableHandle) Final(args ...any) (Result, error) {
	if h.rt == nil {
		return Result{}, &RuntimeError{Err: fmt.Errorf("callable handle is not initialized")}
	}
	return h.rt.CallFinal(h.callable, args...)
}

// CancelCallable cancels all active stream sessions for the given callable name.
// Subsequent CallNext/CallFinal on those sessions will return a stream_cancelled error.
func (rt *Runtime) CancelCallable(callable string) {
	if rt == nil || rt.evaluator == nil {
		return
	}
	rt.evaluator.CancelCallable(callable)
}

// CancelAllStreams cancels all active stream sessions across all callables.
func (rt *Runtime) CancelAllStreams() {
	if rt == nil || rt.evaluator == nil {
		return
	}
	rt.evaluator.CancelAllStreams()
}

func callableInfosFromDescs(descs []schema.CallableDesc) []CallableInfo {
	if len(descs) == 0 {
		return nil
	}
	infos := make([]CallableInfo, 0, len(descs))
	for _, desc := range descs {
		infos = append(infos, callableInfoFromDesc(desc))
	}
	return infos
}

func callableInfoFromDesc(desc schema.CallableDesc) CallableInfo {
	clone := schema.CloneCallableDesc(desc)
	info := CallableInfo{
		Name:       clone.Name,
		Mode:       clone.Mode,
		Parameters: clone.Parameters,
		Returns:    clone.Returns,
	}
	if len(clone.Parameters) == 1 {
		if obj := objectDescFromType(clone.Parameters[0].Type); obj != nil {
			info.ReqDesc = obj
		}
	}
	if len(clone.Returns) == 1 {
		if obj := objectDescFromType(clone.Returns[0]); obj != nil {
			info.RespDesc = obj
		}
	}
	return info
}

func objectDescFromType(desc schema.TypeDesc) *schema.ObjectDesc {
	if desc.Kind != schema.TypeKindStruct && desc.Kind != schema.TypeKindClass {
		return nil
	}
	name := desc.ClassName
	if name == "" {
		name = desc.Name
	}
	if name == "" {
		return nil
	}
	return &schema.ObjectDesc{Name: name, Kind: desc.Kind}
}
