package bytecode

import (
	"context"
	"fmt"
	"github.com/qomos-w/spore/invoke"
	"reflect"
	"strings"
	"time"

	"github.com/qomos-w/spore/diagnostics"
	"github.com/qomos-w/spore/internal/script/frontend"
	"github.com/qomos-w/spore/internal/script/vm"
	"github.com/qomos-w/spore/schema"
)

// VMEvaluator implements the frontend VM lowering seam and provides
// runtime evaluation for compiled source-defined callables.
type VMEvaluator struct {
	vm_                *vm.VM
	interp             *Interpreter
	chunks             map[string]*Chunk
	sessions           map[string]*streamSession
	rootProviderID     int
	nativeBinding      invoke.ScriptSurface
	hostInterfaces     HostInterfaceResolver
	nativeCapabilities map[string]map[string]struct{}
	nativeExecutables  map[string]*nativeExecutable
	allowedNamespaces  map[string]struct{}
	// registeredNativeStructs caches native struct names registered into the VM
	// during compilation, used for heuristic map→struct conversion when host
	// functions return Go structs.
	registeredNativeStructs map[string]struct{}
	importedNativeValues    []nativeImportedValue
	// vmHeapBytes / vmHeapSlots are the configured VM memory budget applied
	// both at construction and on every recompile (CompileLoweredProgram
	// rebuilds the underlying VM). A zero value means "use the
	// VMEvaluatorDefaults" (4 MiB heap / 256 slots).
	vmHeapBytes int
	vmHeapSlots int
	// onVMReplaced holds VM-lifecycle subscriptions (see OnVMReplaced).
	onVMReplaced []func(*vm.VM)
	// execState is the per-call execution-budget counter, reused across
	// calls (zeroed at the start of every EvaluateContext).
	execState invoke.ExecutionState
}

// Default VM memory budget applied when a VMEvaluator is built without an
// explicit knob. The heap default is 4 MiB: the previous 64 KiB default
// panicked real application workloads (a single ecsbind.World.View envelope
// at N≥100 or a full app-logic script blows past it), so it served nobody.
//
// Contract: exceeding the heap budget after a GC cycle is a Track 1 condition
// (vm.allocMemory → vmPanic "out of memory"). Because every bytecode entry
// point converts an escaped panic into a structured *RuntimeError (see doc.go),
// the caller observes Code "vm_internal_panic" rather than a Go panic. It is
// still a hard failure — size the budget to the workload (RuntimeOptions
// .VMHeapBytes / NewVMEvaluatorWith) instead of treating it as a catchable
// script error; the vm_internal_panic code exists so hosts can tell "the
// engine hit an invariant" apart from "the script is wrong".
const (
	DefaultVMHeapBytes = 4 << 20 // 4 MiB
	DefaultVMHeapSlots = 256
)

// resolveVMBudget applies defaults to a (bytes, slots) pair. Zero or
// negative values are treated as "use the default".
func resolveVMBudget(bytes, slots int) (int, int) {
	if bytes <= 0 {
		bytes = DefaultVMHeapBytes
	}
	if slots <= 0 {
		slots = DefaultVMHeapSlots
	}
	return bytes, slots
}

type nativeExecutable struct {
	adapter invoke.ExecutableAdapter
}

type nativeImportedValue struct {
	Path string
	Name string
}

type streamSession struct {
	args      []vm.Value
	ip        int
	stack     []vm.Value
	locals    []vm.Value
	exhausted bool
	cancelled bool
}

// NewVMEvaluator creates a new VM-backed evaluator with the default 4 MiB
// heap and a 256-slot operand-stack reserve. It is equivalent to
// NewVMEvaluatorWith(0, 0) — zero/negative values mean "use the default".
func NewVMEvaluator() *VMEvaluator {
	return NewVMEvaluatorWith(0, 0)
}

// vmWordSize:一个 memory 槽(vm.memory []uint64 的元素)的字节数。
// VMHeapBytes 契约以字节计(见 DefaultVMHeapBytes),而 vm.NewVM 的首个参数
// 是槽位数——这里做一次换算,避免"4 MiB 预算"被解释成 4M 槽(32MiB 实占,
// 每 Runtime 8 倍膨胀;游戏宿主 29 个 Runtime 曾因此常驻 ~960MB)。
const vmWordSize = 8

// NewVMEvaluatorWith creates a new VM-backed evaluator with a custom VM
// memory budget. A heapBytes of 0 (or negative) means
// DefaultVMHeapBytes (4 MiB); a slots of 0 (or negative) means
// DefaultVMHeapSlots (256). slots seeds the interpreter's single execution
// stack (which grows on demand). Exceeding the heap budget after GC is a
// Track 1 condition reported as Code "vm_internal_panic" — see
// DefaultVMHeapBytes for the contract.
func NewVMEvaluatorWith(heapBytes, slots int) *VMEvaluator {
	heapBytes, slots = resolveVMBudget(heapBytes, slots)
	v := vm.NewVM(heapBytes/vmWordSize, slots)
	eval := &VMEvaluator{
		vm_:                v,
		chunks:             make(map[string]*Chunk),
		sessions:           make(map[string]*streamSession),
		nativeCapabilities: make(map[string]map[string]struct{}),
		nativeExecutables:  make(map[string]*nativeExecutable),
		allowedNamespaces:  make(map[string]struct{}),
		vmHeapBytes:        heapBytes,
		vmHeapSlots:        slots,
	}
	eval.interp = newInterpreter(v)
	eval.interp.nativeInvoker = eval
	eval.registerRootProvider()
	return eval
}

// VMHeapBudget reports the configured VM memory budget for this evaluator
// (heap bytes, operand-stack slots). Zero means the evaluator was built with
// the default budget — useful for diagnostics and tests.
func (e *VMEvaluator) VMHeapBudget() (int, int) {
	if e == nil {
		return 0, 0
	}
	heap, slots := resolveVMBudget(e.vmHeapBytes, e.vmHeapSlots)
	return heap, slots
}

// OnVMReplaced subscribes fn to VM-instance replacement. fn is invoked
// synchronously with the current VM at subscription time, and again every
// time CompileLoweredProgram swaps in a fresh VM. Owners of GC roots that
// live outside VM memory (handle tables, session state) use this to keep
// their root providers attached — a construction-time guarantee instead
// of call-site discipline at every VM swap.
func (e *VMEvaluator) OnVMReplaced(fn func(*vm.VM)) {
	if e == nil || fn == nil {
		return
	}
	e.onVMReplaced = append(e.onVMReplaced, fn)
	fn(e.vm_)
}

// notifyVMReplaced fans out a VM swap to all subscribers.
func (e *VMEvaluator) notifyVMReplaced() {
	for _, fn := range e.onVMReplaced {
		fn(e.vm_)
	}
}

func (e *VMEvaluator) registerRootProvider() {
	e.rootProviderID = e.vm_.AddRootProvider(func(visit func(vm.Value)) {
		for _, session := range e.sessions {
			for _, val := range session.args {
				visit(val)
			}
			for _, val := range session.stack {
				visit(val)
			}
			for _, val := range session.locals {
				visit(val)
			}
		}
	})
}

// CompileLoweredProgram compiles a parsed/lowered program and registers all functions/classes/structs.
func (e *VMEvaluator) CompileLoweredProgram(compiled frontend.CompiledDeclarations, prog *frontend.Program) error {
	compiler := NewCompiler()
	compiler.nativeCapabilities = e.nativeCapabilitiesCopy()
	e.vm_ = vm.NewVM(e.vmHeapBytes/vmWordSize, e.vmHeapSlots)
	e.notifyVMReplaced()
	e.importedNativeValues = nil
	for _, imported := range compiled.ImportedSymbols() {
		switch imported.Kind {
		case "callable":
			compiler.RegisterImportedCallableAlias(imported.LocalName, imported.TargetName)
		case "variable":
			compiler.RegisterImportedGlobalAlias(imported.LocalName, imported.TargetName)
			compiler.RegisterImportedGlobalType(imported.LocalName, imported.Type)
		case "native":
			compiler.RegisterImportedNativeAlias(imported.LocalName, imported.TargetName)
		case "native_value":
			if e.nativeBinding == nil {
				return fmt.Errorf("native binding is required for imported native value %q", imported.LocalName)
			}
			desc, _, ok := e.nativeBinding.FindCapabilityValue(imported.Path, imported.Name)
			if !ok {
				return fmt.Errorf("native value %q from capability %q is not registered", imported.Name, imported.Path)
			}
			e.importedNativeValues = append(e.importedNativeValues, nativeImportedValue{Path: imported.Path, Name: imported.Name})
			compiler.RegisterImportedNativeValue(imported.LocalName, vm.EncodeHandle(vm.InvalidHandle))
			compiler.RegisterImportedGlobalType(imported.LocalName, imported.Type)
			_ = desc
		}
	}
	for _, imported := range compiled.ImportedTypes() {
		if imported.Kind == "enum" && imported.Type != "" {
			compiler.RegisterImportedEnumAlias(imported.LocalName, imported.Type)
		}
	}
	for ns := range e.allowedNamespaces {
		if _, exists := compiler.nativeCapabilities[ns]; !exists {
			compiler.nativeCapabilities[ns] = make(map[string]struct{})
		}
	}
	// Register native capability structs into the compiler so struct literals
	// and field accesses can be compiled against them.
	if e.nativeBinding != nil {
		for _, capDesc := range e.nativeBinding.DescribeCapabilities() {
			for _, obj := range capDesc.Objects {
				if obj.Kind != schema.TypeKindStruct {
					continue
				}
				fields := make([]structFieldInfo, len(obj.Fields))
				for i, f := range obj.Fields {
					fields[i] = structFieldInfo{name: f.Name, typeName: f.Type.String()}
				}
				compiler.structs[obj.Name] = structInfo{name: obj.Name, fields: fields}
			}
		}
	}
	if linked, ok := compiled.LinkedModules(); ok {
		for _, module := range linked {
			if _, err := compiler.CompileModule(module.Path, module.Program); err != nil {
				return err
			}
		}
	}
	mainChunk, err := compiler.Compile(prog)
	if err != nil {
		return err
	}
	e.chunks = compiler.GetFunctions()
	e.interp = newInterpreter(e.vm_)
	e.interp.nativeInvoker = e
	e.interp.setNativeValueResolver(e)
	e.sessions = make(map[string]*streamSession)
	e.registerRootProvider()
	compiler.RegisterFunctions(e.vm_, e.interp)
	compiler.RegisterClasses(e.vm_)
	compiler.RegisterStructs(e.vm_)
	compiler.RegisterEnums(e.vm_)
	if len(compiler.nativeValueSlots) > 0 {
		nativeValues := make([]vm.Value, len(compiler.nativeValueSlots))
		for name, slot := range compiler.nativeValueSlots {
			nativeValues[slot] = compiler.nativeValues[name]
		}
		e.interp.setNativeValues(nativeValues)
	}

	// Register native capability structs so script code can construct and
	// access them just like source-defined structs.
	e.registeredNativeStructs = make(map[string]struct{})
	if e.nativeBinding != nil {
		for _, capDesc := range e.nativeBinding.DescribeCapabilities() {
			for _, obj := range capDesc.Objects {
				if obj.Kind != schema.TypeKindStruct {
					continue
				}
				// Pass 1: register struct name so ID is allocated.
				e.vm_.StructReg().RegisterStruct(obj.Name, nil)
				e.registeredNativeStructs[obj.Name] = struct{}{}
			}
		}
	}
	// Pass 2: update native struct fields with correct TypeIDs.
	if e.nativeBinding != nil {
		for _, capDesc := range e.nativeBinding.DescribeCapabilities() {
			for _, obj := range capDesc.Objects {
				if obj.Kind != schema.TypeKindStruct {
					continue
				}
				fields := make([]vm.FieldDef, len(obj.Fields))
				for i, f := range obj.Fields {
					tid := resolveTypeID(f.Type.String(), e.vm_.StructReg(), e.vm_.EnumReg())
					fields[i] = vm.NewFieldDef(f.Name, tid, i+1)
				}
				e.vm_.StructReg().UpdateStructFields(obj.Name, fields)
			}
		}
	}

	if mainChunk != nil {
		if _, err := e.interp.Execute(mainChunk, compiler.GetFunctions()); err != nil {
			return err
		}
	}
	return nil
}

// CompileProgram compiles a parsed program and registers all functions/classes/structs.
func (e *VMEvaluator) CompileProgram(prog *frontend.Program) error {
	return e.CompileLoweredProgram(frontend.CompiledDeclarations{}, prog)
}

// AllowNativeNamespace declares a namespace as eligible for native member-call
// lowering during compilation, even if no capability has been registered yet.
func (e *VMEvaluator) AllowNativeNamespace(namespace string) {
	if e == nil || namespace == "" {
		return
	}
	if e.allowedNamespaces == nil {
		e.allowedNamespaces = make(map[string]struct{})
	}
	e.allowedNamespaces[namespace] = struct{}{}
}

// Evaluate executes a compiled function by name for the requested invocation stage,
// converting between []any and vm.Value.
func (e *VMEvaluator) Evaluate(callable string, stage invoke.InvocationStage, args []any) (any, error) {
	return e.EvaluateContext(context.Background(), invoke.ExecutionBudget{}, callable, stage, args)
}

func (e *VMEvaluator) EvaluateContext(ctx context.Context, budget invoke.ExecutionBudget, callable string, stage invoke.InvocationStage, args []any) (out any, err error) {
	// Installed first so it also covers argument conversion below: the codec
	// can panic on an exhausted VM heap, which is Track 1 (see doc.go).
	defer func() {
		if r := recover(); r != nil {
			err = bytecodePanicError(e.interpRef(), callable, "vm/evaluate", r)
			out = nil
		}
	}()
	if e == nil {
		return nil, fmt.Errorf("callable %q has no evaluator", callable)
	}

	chunk, ok := e.chunks[callable]
	if !ok {
		return nil, fmt.Errorf("callable %q not found in compiled chunks", callable)
	}

	vmArgs, err := toVMArgs(e.hostInterfaces, e.vm_, args)
	if err != nil {
		if rtErr, ok := err.(*RuntimeError); ok {
			rtErr.Callable = callable
			return nil, rtErr
		}
		return nil, fmt.Errorf("source-defined callable %q: %s", callable, err.Error())
	}

	var (
		result  vm.Value
		callErr error
	)
	// Reuse the evaluator's execution state: zeroed per call, so budget
	// accounting starts fresh exactly like a freshly allocated state.
	state := &e.execState
	*state = invoke.ExecutionState{}
	if err := invoke.CheckExecution(ctx, budget, state); err != nil {
		return nil, err
	}
	e.interp.ctx = ctx
	e.interp.budget = budget
	e.interp.execution = state
	if stage == invoke.InvocationStageNext {
		result, callErr = e.executeNext(callable, chunk, vmArgs)
	} else if stage == invoke.InvocationStageFinal {
		result, callErr = e.executeFinal(callable, chunk, vmArgs)
	} else {
		result, callErr = e.interp.ExecuteFunction(chunk, stage, vmArgs)
	}
	if callErr != nil {
		if rtErr, ok := callErr.(*RuntimeError); ok {
			rtErr.Callable = callable
			return nil, rtErr
		}
		return nil, fmt.Errorf("source-defined callable %q: %s", callable, callErr.Error())
	}

	return vmValueToAnyWithHost(e.hostInterfaces, e.vm_, result), nil
}

// interpRef returns the interpreter this evaluator owns, or nil when the
// evaluator itself is nil. Recovery helpers must tolerate a nil evaluator
// because the panic they handle can predate initialization.
func (e *VMEvaluator) interpRef() *Interpreter {
	if e == nil {
		return nil
	}
	return e.interp
}

// EvaluateUnaryInt executes a compiled unary function with an int argument and projects
// the benchmark-oriented scalar result without going through the public binding envelope.
func (e *VMEvaluator) EvaluateUnaryInt(callable string, arg int) (out int64, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = bytecodePanicError(e.interpRef(), callable, "vm/evaluate/unary", r)
			out = 0
		}
	}()
	if e == nil {
		return 0, fmt.Errorf("callable %q has no evaluator", callable)
	}
	chunk, ok := e.chunks[callable]
	if !ok {
		return 0, fmt.Errorf("callable %q not found in compiled chunks", callable)
	}
	result, err := e.interp.ExecuteFunction(chunk, invoke.InvocationStageUnary, []vm.Value{vm.EncodeInt(int32(arg))})
	if err != nil {
		if rtErr, ok := err.(*RuntimeError); ok {
			rtErr.Callable = callable
			return 0, rtErr
		}
		return 0, fmt.Errorf("source-defined callable %q: %s", callable, err.Error())
	}
	if vm.IsInt(result) {
		return int64(vm.DecodeInt(result)), nil
	}
	if vm.IsLong(result) {
		return e.vm_.DecodeLong(result), nil
	}
	if vm.IsULong(result) {
		return int64(e.vm_.DecodeULong(result)), nil
	}
	if vm.IsString(result) {
		return int64(len(e.vm_.DecodeString(result))), nil
	}
	return 0, nil
}

func (e *VMEvaluator) Reset(callable string, args []any) {
	if e == nil {
		return
	}
	delete(e.sessions, streamSessionKey(callable, mustToVMArgs(nil, e.vm_, args)))
}

// CancelCallable cancels all active stream sessions for the given callable name.
// Subsequent CallNext/CallFinal on those sessions will return a stream_cancelled error.
func (e *VMEvaluator) CancelCallable(callable string) {
	if e == nil {
		return
	}
	for key, state := range e.sessions {
		if strings.HasPrefix(key, callable+"|") {
			state.cancelled = true
		}
	}
}

// CancelAllStreams cancels all active stream sessions.
func (e *VMEvaluator) CancelAllStreams() {
	if e == nil {
		return
	}
	for _, state := range e.sessions {
		state.cancelled = true
	}
}

func newStreamCancelledError(callable string, stage invoke.InvocationStage) *RuntimeError {
	return &RuntimeError{
		Code:     "stream_cancelled",
		Category: diagnostics.CategoryStream,
		Callable: callable,
		Path:     "stream/" + string(stage),
		Message:  "stream session was cancelled",
		Stack:    []diagnostics.Frame{{Callable: callable, Stage: string(stage)}},
	}
}

func (e *VMEvaluator) SetNativeBinding(sb invoke.ScriptSurface) {
	if e == nil {
		return
	}
	e.nativeBinding = sb
	e.nativeExecutables = make(map[string]*nativeExecutable)
	if sb == nil {
		return
	}
	for _, desc := range sb.DescribeCapabilities() {
		e.registerNativeCapability(desc)
	}
	e.cacheNativeExecutables(sb)
}

func (e *VMEvaluator) SetHostInterfaceResolver(resolver HostInterfaceResolver) {
	if e == nil {
		return
	}
	e.hostInterfaces = resolver
}

func (e *VMEvaluator) cacheNativeExecutables(sb invoke.ScriptSurface) {
	if e == nil || sb == nil || sb.ExecutorSource() == nil {
		return
	}
	if e.nativeExecutables == nil {
		e.nativeExecutables = make(map[string]*nativeExecutable)
	}
	sb.ExecutorSource().ForEachAdapter(func(name string, adapter invoke.ExecutableAdapter) bool {
		e.nativeExecutables[name] = &nativeExecutable{adapter: adapter}
		return true
	})
}

func (e *VMEvaluator) lookupNativeExecutable(callable string) (*nativeExecutable, bool) {
	if e == nil || callable == "" {
		return nil, false
	}
	if e.nativeExecutables != nil {
		if entry, ok := e.nativeExecutables[callable]; ok {
			return entry, true
		}
	}
	if e.nativeBinding == nil || e.nativeBinding.ExecutorSource() == nil {
		return nil, false
	}
	adapter, ok := e.nativeBinding.ExecutorSource().Lookup(callable)
	if !ok {
		return nil, false
	}
	if e.nativeExecutables == nil {
		e.nativeExecutables = make(map[string]*nativeExecutable)
	}
	entry := &nativeExecutable{adapter: adapter}
	e.nativeExecutables[callable] = entry
	return entry, true
}

func (e *VMEvaluator) registerNativeCapability(desc invoke.CapabilityDesc) {
	if e == nil || desc.Name == "" {
		return
	}
	if e.nativeCapabilities == nil {
		e.nativeCapabilities = make(map[string]map[string]struct{})
	}
	callables := make(map[string]struct{}, len(desc.Callables))
	for _, callable := range desc.Callables {
		callables[callable.Name] = struct{}{}
	}
	e.nativeCapabilities[desc.Name] = callables
}

func (e *VMEvaluator) nativeCapabilitiesCopy() map[string]map[string]struct{} {
	copied := make(map[string]map[string]struct{})
	if e == nil {
		return copied
	}
	for namespace, callables := range e.nativeCapabilities {
		cloned := make(map[string]struct{}, len(callables))
		for callable := range callables {
			cloned[callable] = struct{}{}
		}
		copied[namespace] = cloned
	}
	return copied
}

func (e *VMEvaluator) Invoke(ctx context.Context, callable string, args []any) (any, error) {
	if e == nil || e.nativeBinding == nil {
		return nil, nil
	}
	outcome, err := e.nativeBinding.Invoke(invoke.InvocationRequest{
		Callable: callable,
		Stage:    invoke.InvocationStageUnary,
		Args:     args,
		Context:  ctx,
	})
	if err != nil {
		if strings.Contains(err.Error(), "is not registered") {
			return nil, nil
		}
		return nil, err
	}
	return projectInvocationOutcome(callable, outcome)
}

func (e *VMEvaluator) InvokeVMNative(ctx context.Context, callable string, args []vm.Value) (value vm.Value, ok bool, err error) {
	// The native bridge reaches VM allocation and codec helpers directly, so
	// it needs its own boundary (see doc.go).
	defer func() {
		if r := recover(); r != nil {
			err = bytecodePanicError(e.interpRef(), callable, "vm/call/native", r)
			value, ok = vm.EncodeInt(0), false
		}
	}()
	if e == nil || e.nativeBinding == nil {
		return vm.EncodeInt(0), false, nil
	}
	entry, ok := e.lookupNativeExecutable(callable)
	if !ok {
		return vm.EncodeInt(0), false, nil
	}
	var small [8]any
	payload := small[:0]
	if len(args) > len(small) {
		payload = make([]any, len(args))
	} else {
		payload = payload[:len(args)]
	}
	for i, arg := range args {
		payload[i] = vmValueToAnyWithHost(e.hostInterfaces, e.vm_, arg)
	}
	if err := invoke.CheckExecution(ctx, invoke.ExecutionBudget{}, nil); err != nil {
		return vm.EncodeInt(0), true, err
	}
	outcome, err := entry.adapter.Invoke(invoke.InvocationRequest{
		Callable: callable,
		Stage:    invoke.InvocationStageUnary,
		Args:     payload,
		Context:  ctx,
	})
	if err != nil {
		return vm.EncodeInt(0), true, err
	}

	// Handle error outcome first.
	if outcome.Result.Kind == invoke.InvocationResultError {
		if outcome.Result.Error == nil {
			return vm.EncodeInt(0), true, fmt.Errorf("native callable %q failed", callable)
		}
		return vm.EncodeInt(0), true, fmt.Errorf("%s", outcome.Result.Error.Message)
	}
	if outcome.Payload == nil {
		// Void-success natives (payload-less outcomes) are resolved callables;
		// returning ok=false here would make the interpreter report them as
		// undefined functions.
		return vm.EncodeInt(0), true, nil
	}

	// Fast path: if the adapter returned a Go struct, convert it directly to a
	// VM struct using the registered type. This avoids the map→struct heuristic
	// and eliminates false matches when two structs share the same field names.
	raw := outcome.Payload.Value
	if rv := reflect.ValueOf(raw); rv.IsValid() {
		for rv.Kind() == reflect.Pointer {
			if rv.IsNil() {
				return vm.EncodeInt(0), true, nil
			}
			rv = rv.Elem()
		}
		if rv.Kind() == reflect.Struct && rv.Type() != reflect.TypeOf(time.Time{}) {
			c := acquireConvCtx(e.hostInterfaces, e.vm_, "vm/call/native/result")
			vmVal, err := c.goStruct(rv)
			releaseConvCtx(c)
			if err == nil {
				return vmVal, true, nil
			}
			// Exact conversion failed (struct not registered); fall through to
			// generic conversion.
		}
	}

	// Generic path: project to map/slice/primitive, then heuristic map→struct.
	result := projectNativeResult(raw)
	if m, ok := result.(map[string]any); ok {
		if vmStruct, ok := e.mapToVMStruct(m); ok {
			return vmStruct, true, nil
		}
	}
	value, err = anyToVMValueAtPath(e.hostInterfaces, e.vm_, result, "vm/call/native/result")
	if err != nil {
		return vm.EncodeInt(0), true, err
	}
	return value, true, nil
}

func projectInvocationOutcome(callable string, outcome invoke.InvocationOutcome) (any, error) {
	if outcome.Result.Kind == invoke.InvocationResultError {
		if outcome.Result.Error == nil {
			return nil, fmt.Errorf("native callable %q failed", callable)
		}
		return nil, fmt.Errorf("%s", outcome.Result.Error.Message)
	}
	if outcome.Payload == nil {
		return nil, nil
	}
	return projectNativeResult(outcome.Payload.Value), nil
}

func (e *VMEvaluator) executeNext(callable string, chunk *Chunk, args []vm.Value) (value vm.Value, err error) {
	// A recovered panic mid-stream must not leave a resumable cursor behind.
	defer func() {
		if r := recover(); r != nil {
			e.discardStreamSession(callable, args)
			err = bytecodePanicError(e.interpRef(), callable, "vm/stream/next", r)
			value = vm.EncodeInt(0)
		}
	}()
	key := streamSessionKey(callable, args)
	state, ok := e.sessions[key]
	if ok {
		if state.cancelled {
			delete(e.sessions, key)
			return vm.EncodeInt(0), newStreamCancelledError(callable, invoke.InvocationStageNext)
		}
		if state.exhausted || state.ip >= len(chunk.code) {
			return vm.EncodeInt(0), &RuntimeError{Code: "stream_exhausted", Category: diagnostics.CategoryStream, Callable: callable, Path: "stream/next", Message: "stream exhausted before next yield", Stack: []diagnostics.Frame{{Callable: callable, Stage: string(invoke.InvocationStageNext)}}}
		}
		result, ip, stack, locals, err := e.interp.runUntilBoundary(chunk, state.ip, cloneVMValues(state.stack), cloneVMValues(state.locals), nil, true, true)
		if err != nil {
			delete(e.sessions, key)
			return vm.EncodeInt(0), err
		}
		if ip >= len(chunk.code) {
			delete(e.sessions, key)
			e.sessions[key] = &streamSession{args: cloneVMValues(state.args), ip: len(chunk.code), exhausted: true}
			return vm.EncodeInt(0), &RuntimeError{Code: "stream_exhausted", Category: diagnostics.CategoryStream, Callable: callable, Path: "stream/next", Message: "stream exhausted before next yield", Stack: []diagnostics.Frame{{Callable: callable, Stage: string(invoke.InvocationStageNext)}}}
		}
		e.sessions[key] = &streamSession{args: cloneVMValues(state.args), ip: ip, stack: cloneVMValues(stack), locals: cloneVMValues(locals)}
		return result, nil
	}
	result, ip, stack, locals, err := e.interp.ExecuteUntilYield(chunk, args)
	if err != nil {
		return vm.EncodeInt(0), err
	}
	if ip >= len(chunk.code) {
		return vm.EncodeInt(0), &RuntimeError{Code: "stream_exhausted", Category: diagnostics.CategoryStream, Callable: callable, Path: "stream/next", Message: "stream exhausted before next yield", Stack: []diagnostics.Frame{{Callable: callable, Stage: string(invoke.InvocationStageNext)}}}
	}
	e.sessions[key] = &streamSession{args: cloneVMValues(args), ip: ip, stack: cloneVMValues(stack), locals: cloneVMValues(locals)}
	return result, nil
}

func (e *VMEvaluator) executeFinal(callable string, chunk *Chunk, args []vm.Value) (value vm.Value, err error) {
	// A recovered panic mid-stream must not leave a resumable cursor behind.
	defer func() {
		if r := recover(); r != nil {
			e.discardStreamSession(callable, args)
			err = bytecodePanicError(e.interpRef(), callable, "vm/stream/final", r)
			value = vm.EncodeInt(0)
		}
	}()
	key := streamSessionKey(callable, args)
	if state, ok := e.sessions[key]; ok {
		if state.cancelled {
			delete(e.sessions, key)
			return vm.EncodeInt(0), newStreamCancelledError(callable, invoke.InvocationStageFinal)
		}
		if state.exhausted || state.ip >= len(chunk.code) {
			return vm.EncodeInt(0), &RuntimeError{Code: "stream_exhausted", Category: diagnostics.CategoryStream, Callable: callable, Path: "stream/final", Message: "stream exhausted before final result", Stack: []diagnostics.Frame{{Callable: callable, Stage: string(invoke.InvocationStageFinal)}}}
		}
		result, err := e.interp.ResumeUntilFinal(chunk, state.ip, cloneVMValues(state.stack), cloneVMValues(state.locals))
		e.sessions[key] = &streamSession{args: cloneVMValues(state.args), ip: len(chunk.code), exhausted: true}
		return result, err
	}
	result, err := e.interp.ExecuteFunction(chunk, invoke.InvocationStageFinal, args)
	if err != nil {
		return vm.EncodeInt(0), err
	}
	e.sessions[key] = &streamSession{args: cloneVMValues(args), ip: len(chunk.code), exhausted: true}
	return result, nil
}

func streamSessionKey(callable string, args []vm.Value) string {
	parts := make([]string, len(args))
	for i, arg := range args {
		parts[i] = fmt.Sprintf("%d", uint64(arg))
	}
	return callable + "|" + strings.Join(parts, ",")
}

func (e *VMEvaluator) SessionsForTest() map[string]*streamSession {
	return e.sessions
}

func (s *streamSession) ExhaustedForTest() bool {
	if s == nil {
		return false
	}
	return s.exhausted
}

func cloneVMValues(values []vm.Value) []vm.Value {
	if len(values) == 0 {
		return nil
	}
	cloned := make([]vm.Value, len(values))
	copy(cloned, values)
	return cloned
}

// VM returns the underlying VM instance.
func (e *VMEvaluator) VM() *vm.VM { return e.vm_ }

func (e *VMEvaluator) ResolveImportedNativeValue(slot int) (value vm.Value, err error) {
	// The tail call converts a host value into VM memory, which can panic on a
	// Track 1 condition (exhausted heap), so the boundary applies here too.
	defer func() {
		if r := recover(); r != nil {
			err = bytecodePanicError(e.interpRef(), "", "vm/native-value/load", r)
			value = vm.EncodeInt(0)
		}
	}()
	if e == nil {
		return vm.EncodeInt(0), fmt.Errorf("vm evaluator is not initialized")
	}
	if slot < 0 || slot >= len(e.importedNativeValues) {
		return vm.EncodeInt(0), &RuntimeError{Code: "native_value_index_out_of_bounds", Category: diagnostics.CategoryRuntime, Callable: "", Path: "vm/native-value/load", Message: fmt.Sprintf("native value index out of bounds: %d", slot)}
	}
	if e.nativeBinding == nil {
		return vm.EncodeInt(0), fmt.Errorf("native binding is required for imported native value slot %d", slot)
	}
	imported := e.importedNativeValues[slot]
	_, nativeValue, ok := e.nativeBinding.FindCapabilityValue(imported.Path, imported.Name)
	if !ok {
		return vm.EncodeInt(0), fmt.Errorf("native value %q from capability %q is not registered", imported.Name, imported.Path)
	}
	return anyToVMValueAtPath(e.hostInterfaces, e.vm_, nativeValue, "vm/evaluator/native-value/"+imported.Path+"/"+imported.Name)
}
