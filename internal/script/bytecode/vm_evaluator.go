package bytecode

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/qomos-w/spore/binding"
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
	nativeBinding      *binding.ScriptBinding
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
	execState binding.ExecutionState
}

// Default VM memory budget applied when a VMEvaluator is built without an
// explicit knob. The heap default is 4 MiB: the previous 64 KiB default
// panicked real application workloads (a single ecsbind.World.View envelope
// at N≥100 or a full app-logic script blows past it), so it served nobody.
//
// Contract: exceeding the heap budget after a GC cycle is a panic
// (vm.allocMemory → vmPanic "out of memory"), not an error return.
// Embedders that run untrusted or budget-sensitive scripts must set an
// explicit budget (RuntimeOptions.VMHeapBytes / NewVMEvaluatorWith) and
// either size it to their workload or recover the panic at their
// invocation boundary.
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
	adapter binding.ExecutableAdapter
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
// heap and 256 call-stack slots. It is equivalent to
// NewVMEvaluatorWith(0, 0) — zero/negative values mean "use the default".
func NewVMEvaluator() *VMEvaluator {
	return NewVMEvaluatorWith(0, 0)
}

// NewVMEvaluatorWith creates a new VM-backed evaluator with a custom VM
// memory budget. A heapBytes of 0 (or negative) means
// DefaultVMHeapBytes (4 MiB); a slots of 0 (or negative) means
// DefaultVMHeapSlots (256). Exceeding the budget after GC panics
// ("out of memory") — see DefaultVMHeapBytes for the contract.
func NewVMEvaluatorWith(heapBytes, slots int) *VMEvaluator {
	heapBytes, slots = resolveVMBudget(heapBytes, slots)
	v := vm.NewVM(heapBytes, slots)
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
// (heap bytes, call-stack slots). Zero means the evaluator was built with
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
	e.vm_ = vm.NewVM(e.vmHeapBytes, e.vmHeapSlots)
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
func (e *VMEvaluator) Evaluate(callable string, stage binding.InvocationStage, args []any) (any, error) {
	return e.EvaluateContext(context.Background(), binding.ExecutionBudget{}, callable, stage, args)
}

func (e *VMEvaluator) EvaluateContext(ctx context.Context, budget binding.ExecutionBudget, callable string, stage binding.InvocationStage, args []any) (any, error) {
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
	*state = binding.ExecutionState{}
	if err := binding.CheckExecution(ctx, budget, state); err != nil {
		return nil, err
	}
	e.interp.ctx = ctx
	e.interp.budget = budget
	e.interp.execution = state
	if stage == binding.InvocationStageNext {
		result, callErr = e.executeNext(callable, chunk, vmArgs)
	} else if stage == binding.InvocationStageFinal {
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

// EvaluateUnaryInt executes a compiled unary function with an int argument and projects
// the benchmark-oriented scalar result without going through the public binding envelope.
func (e *VMEvaluator) EvaluateUnaryInt(callable string, arg int) (int64, error) {
	if e == nil {
		return 0, fmt.Errorf("callable %q has no evaluator", callable)
	}
	chunk, ok := e.chunks[callable]
	if !ok {
		return 0, fmt.Errorf("callable %q not found in compiled chunks", callable)
	}
	result, err := e.interp.ExecuteFunction(chunk, binding.InvocationStageUnary, []vm.Value{vm.EncodeInt(int32(arg))})
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

func newStreamCancelledError(callable string, stage binding.InvocationStage) *RuntimeError {
	return &RuntimeError{
		Code:     "stream_cancelled",
		Category: diagnostics.CategoryStream,
		Callable: callable,
		Path:     "stream/" + string(stage),
		Message:  "stream session was cancelled",
		Stack:    []diagnostics.Frame{{Callable: callable, Stage: string(stage)}},
	}
}

func (e *VMEvaluator) SetNativeBinding(sb *binding.ScriptBinding) {
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

func (e *VMEvaluator) cacheNativeExecutables(sb *binding.ScriptBinding) {
	if e == nil || sb == nil || sb.Executors == nil {
		return
	}
	if e.nativeExecutables == nil {
		e.nativeExecutables = make(map[string]*nativeExecutable)
	}
	sb.Executors.ForEachAdapter(func(name string, adapter binding.ExecutableAdapter) bool {
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
	if e.nativeBinding == nil || e.nativeBinding.Executors == nil {
		return nil, false
	}
	adapter, ok := e.nativeBinding.Executors.Lookup(callable)
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

func (e *VMEvaluator) registerNativeCapability(desc binding.CapabilityDesc) {
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
	outcome, err := e.nativeBinding.Invoke(binding.InvocationRequest{
		Callable: callable,
		Stage:    binding.InvocationStageUnary,
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

func (e *VMEvaluator) InvokeVMNative(ctx context.Context, callable string, args []vm.Value) (vm.Value, bool, error) {
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
	if err := binding.CheckExecution(ctx, binding.ExecutionBudget{}, nil); err != nil {
		return vm.EncodeInt(0), true, err
	}
	outcome, err := entry.adapter.Invoke(binding.InvocationRequest{
		Callable: callable,
		Stage:    binding.InvocationStageUnary,
		Args:     payload,
		Context:  ctx,
	})
	if err != nil {
		return vm.EncodeInt(0), true, err
	}

	// Handle error outcome first.
	if outcome.Result.Kind == binding.InvocationResultError {
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
			vmVal, err := goStructToVMStruct(e.hostInterfaces, e.vm_, rv, "vm/call/native/result")
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
	value, err := anyToVMValueAtPath(e.hostInterfaces, e.vm_, result, "vm/call/native/result")
	if err != nil {
		return vm.EncodeInt(0), true, err
	}
	return value, true, nil
}

// mapToVMStruct attempts to convert a map[string]any into a VM struct
// by matching its keys against registered struct field names.
func (e *VMEvaluator) mapToVMStruct(m map[string]any) (vm.Value, bool) {
	if e.vm_ == nil || len(m) == 0 || len(e.registeredNativeStructs) == 0 {
		return vm.EncodeInt(0), false
	}
	for name := range e.registeredNativeStructs {
		sd := e.vm_.StructReg().GetStruct(name)
		if sd == nil {
			continue
		}
		fields := sd.Fields()
		if len(fields) != len(m) {
			continue
		}
		match := true
		for _, f := range fields {
			if _, ok := m[f.Name()]; !ok {
				match = false
				break
			}
		}
		if match {
			fieldValues := make([]vm.Value, len(fields))
			for i, f := range fields {
				val, err := anyToVMValueAtPath(nil, e.vm_, m[f.Name()], "vm/struct/"+name+"/"+f.Name())
				if err != nil {
					return vm.EncodeInt(0), false
				}
				fieldValues[i] = val
			}
			handle := e.vm_.NewStructInstance(name, fieldValues)
			if handle != vm.InvalidHandle {
				return vm.EncodeHandle(handle), true
			}
		}
	}
	return vm.EncodeInt(0), false
}

func projectInvocationOutcome(callable string, outcome binding.InvocationOutcome) (any, error) {
	if outcome.Result.Kind == binding.InvocationResultError {
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

func (e *VMEvaluator) executeNext(callable string, chunk *Chunk, args []vm.Value) (vm.Value, error) {
	key := streamSessionKey(callable, args)
	state, ok := e.sessions[key]
	if ok {
		if state.cancelled {
			delete(e.sessions, key)
			return vm.EncodeInt(0), newStreamCancelledError(callable, binding.InvocationStageNext)
		}
		if state.exhausted || state.ip >= len(chunk.code) {
			return vm.EncodeInt(0), &RuntimeError{Code: "stream_exhausted", Category: diagnostics.CategoryStream, Callable: callable, Path: "stream/next", Message: "stream exhausted before next yield", Stack: []diagnostics.Frame{{Callable: callable, Stage: string(binding.InvocationStageNext)}}}
		}
		result, ip, stack, locals, err := e.interp.runUntilBoundary(chunk, state.ip, cloneVMValues(state.stack), cloneVMValues(state.locals), nil, true, true)
		if err != nil {
			delete(e.sessions, key)
			return vm.EncodeInt(0), err
		}
		if ip >= len(chunk.code) {
			delete(e.sessions, key)
			e.sessions[key] = &streamSession{args: cloneVMValues(state.args), ip: len(chunk.code), exhausted: true}
			return vm.EncodeInt(0), &RuntimeError{Code: "stream_exhausted", Category: diagnostics.CategoryStream, Callable: callable, Path: "stream/next", Message: "stream exhausted before next yield", Stack: []diagnostics.Frame{{Callable: callable, Stage: string(binding.InvocationStageNext)}}}
		}
		e.sessions[key] = &streamSession{args: cloneVMValues(state.args), ip: ip, stack: cloneVMValues(stack), locals: cloneVMValues(locals)}
		return result, nil
	}
	result, ip, stack, locals, err := e.interp.ExecuteUntilYield(chunk, args)
	if err != nil {
		return vm.EncodeInt(0), err
	}
	if ip >= len(chunk.code) {
		return vm.EncodeInt(0), &RuntimeError{Code: "stream_exhausted", Category: diagnostics.CategoryStream, Callable: callable, Path: "stream/next", Message: "stream exhausted before next yield", Stack: []diagnostics.Frame{{Callable: callable, Stage: string(binding.InvocationStageNext)}}}
	}
	e.sessions[key] = &streamSession{args: cloneVMValues(args), ip: ip, stack: cloneVMValues(stack), locals: cloneVMValues(locals)}
	return result, nil
}

func (e *VMEvaluator) executeFinal(callable string, chunk *Chunk, args []vm.Value) (vm.Value, error) {
	key := streamSessionKey(callable, args)
	if state, ok := e.sessions[key]; ok {
		if state.cancelled {
			delete(e.sessions, key)
			return vm.EncodeInt(0), newStreamCancelledError(callable, binding.InvocationStageFinal)
		}
		if state.exhausted || state.ip >= len(chunk.code) {
			return vm.EncodeInt(0), &RuntimeError{Code: "stream_exhausted", Category: diagnostics.CategoryStream, Callable: callable, Path: "stream/final", Message: "stream exhausted before final result", Stack: []diagnostics.Frame{{Callable: callable, Stage: string(binding.InvocationStageFinal)}}}
		}
		result, err := e.interp.ResumeUntilFinal(chunk, state.ip, cloneVMValues(state.stack), cloneVMValues(state.locals))
		e.sessions[key] = &streamSession{args: cloneVMValues(state.args), ip: len(chunk.code), exhausted: true}
		return result, err
	}
	result, err := e.interp.ExecuteFunction(chunk, binding.InvocationStageFinal, args)
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

// vmArgPaths pre-builds the per-index argument diagnostic paths; argument
// lists are short, so the common indices avoid fmt.Sprintf entirely.
var vmArgPaths = [...]string{
	"vm/evaluator/argument/0", "vm/evaluator/argument/1",
	"vm/evaluator/argument/2", "vm/evaluator/argument/3",
	"vm/evaluator/argument/4", "vm/evaluator/argument/5",
	"vm/evaluator/argument/6", "vm/evaluator/argument/7",
}

func vmArgPath(i int) string {
	if i < len(vmArgPaths) {
		return vmArgPaths[i]
	}
	return "vm/evaluator/argument/" + strconv.Itoa(i)
}

func toVMArgs(host HostInterfaceResolver, vm_ *vm.VM, args []any) ([]vm.Value, error) {
	vmArgs := make([]vm.Value, len(args))
	for i, arg := range args {
		value, err := anyToVMValueAtPath(host, vm_, arg, vmArgPath(i))
		if err != nil {
			return nil, err
		}
		vmArgs[i] = value
	}
	return vmArgs, nil
}

func mustToVMArgs(host HostInterfaceResolver, vm_ *vm.VM, args []any) []vm.Value {
	vmArgs, err := toVMArgs(host, vm_, args)
	if err != nil {
		return nil
	}
	return vmArgs
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

// --- Value conversion helpers ---

// HostAnyToVMValue converts a host value into a VM value using the evaluator bridge.
func HostAnyToVMValue(host HostInterfaceResolver, vm_ *vm.VM, v any, path string) (vm.Value, error) {
	return anyToVMValueAtPath(host, vm_, v, path)
}

// HostVMValueToAny converts a VM value back into a host value using the evaluator bridge.
func HostVMValueToAny(host HostInterfaceResolver, vm_ *vm.VM, v vm.Value) any {
	return vmValueToAnyWithHost(host, vm_, v)
}

// HostInterfaceResolver lets script.Runtime surface host-backed proxy objects through the VM bridge.
type HostInterfaceResolver interface {
	HostInterfaceHandleForValue(value any) (vm.Handle, bool)
	HostInterfaceObjectForHandle(handle vm.Handle) (any, bool)
}

func (e *VMEvaluator) ResolveImportedNativeValue(slot int) (vm.Value, error) {
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
	_, value, ok := e.nativeBinding.FindCapabilityValue(imported.Path, imported.Name)
	if !ok {
		return vm.EncodeInt(0), fmt.Errorf("native value %q from capability %q is not registered", imported.Name, imported.Path)
	}
	return anyToVMValueAtPath(e.hostInterfaces, e.vm_, value, "vm/evaluator/native-value/"+imported.Path+"/"+imported.Name)
}

func anyToVMValue(vm_ *vm.VM, v any) (vm.Value, error) {
	return anyToVMValueAtPath(nil, vm_, v, "vm/evaluator/argument")
}

func anyToVMValueAtPath(host HostInterfaceResolver, vm_ *vm.VM, v any, path string) (vm.Value, error) {
	if host != nil {
		if handle, ok := host.HostInterfaceHandleForValue(v); ok {
			return vm.EncodeHandle(handle), nil
		}
	}
	switch val := v.(type) {
	case int:
		if val < -2147483648 || val > 2147483647 {
			return vm.EncodeInt(0), &RuntimeError{
				Code:     "value_out_of_range",
				Category: diagnostics.CategoryRuntime,
				Path:     path,
				Message:  fmt.Sprintf("Go int value %d exceeds script int32 range", val),
			}
		}
		return vm.EncodeInt(int32(val)), nil
	case int32:
		return vm.EncodeInt(val), nil
	case int64:
		return vm.EncodeLong(val, vm_), nil
	case uint64:
		return vm.EncodeULong(val, vm_), nil
	case float32:
		return vm.EncodeFloat(val), nil
	case float64:
		return vm.EncodeDouble(val, vm_), nil
	case bool:
		return vm.EncodeBool(val), nil
	case string:
		return vm_.EncodeString(val), nil
	case time.Time:
		return vm_.EncodeString(val.Format(time.RFC3339Nano)), nil
	case []byte:
		return vm_.EncodeBytes(val), nil
	case []any:
		return sliceToVMArray(host, vm_, reflect.ValueOf(val), path)
	case []map[string]any:
		return sliceToVMArray(host, vm_, reflect.ValueOf(val), path)
	case map[string]any:
		return stringMapToVMMap(host, vm_, reflect.ValueOf(val), path)
	case nil:
		return vm.EncodeHandle(vm.InvalidHandle), nil
	default:
		rv := reflect.ValueOf(v)
		if rv.IsValid() {
			switch rv.Kind() {
			case reflect.Slice, reflect.Array:
				return sliceToVMArray(host, vm_, rv, path)
			case reflect.Map:
				if rv.Type().Key().Kind() == reflect.String {
					return stringMapToVMMap(host, vm_, rv, path)
				}
			case reflect.Struct:
				return goStructToVMStruct(host, vm_, rv, path)
			}
		}
		return unsupportedVMArgument(v, path)
	}
}

func sliceToVMArray(host HostInterfaceResolver, vm_ *vm.VM, rv reflect.Value, path string) (vm.Value, error) {
	arr := vm_.NewArray(vm.TypeInvalid, 0)
	releaseArrRoot := vm_.AddTemporaryRoot(vm.EncodeHandle(arr))
	defer releaseArrRoot()
	for i := 0; i < rv.Len(); i++ {
		elemVal, err := anyToVMValueAtPath(host, vm_, rv.Index(i).Interface(), path+"/"+strconv.Itoa(i))
		if err != nil {
			return vm.EncodeInt(0), err
		}
		releaseElemRoot := vm_.AddTemporaryRoot(elemVal)
		vm_.ArrayPush(arr, elemVal)
		releaseElemRoot()
	}
	return vm.EncodeHandle(arr), nil
}

func stringMapToVMMap(host HostInterfaceResolver, vm_ *vm.VM, rv reflect.Value, path string) (vm.Value, error) {
	m := vm_.NewMap(vm.TypeInvalid, vm.TypeInvalid, rv.Len())
	// Root the outer map: converting nested values allocates, and an
	// unrooted map gets swept once the heap crosses its GC threshold —
	// the reused memory then corrupts the next MapSet ("map is full").
	releaseRoot := vm_.AddTemporaryRoot(vm.EncodeHandle(m))
	defer releaseRoot()
	iter := rv.MapRange()
	for iter.Next() {
		keyString := iter.Key().String()
		elemVal, err := anyToVMValueAtPath(host, vm_, iter.Value().Interface(), path+"/"+keyString)
		if err != nil {
			return vm.EncodeInt(0), err
		}
		encodedKey := vm_.EncodeString(keyString)
		// EncodeString may allocate a heap string; root the pending pair so a
		// GC triggered by that allocation cannot sweep the unstored elemVal.
		releaseElemRoot := vm_.AddTemporaryRoot(elemVal, encodedKey)
		vm_.MapSet(m, encodedKey, elemVal)
		releaseElemRoot()
	}
	return vm.EncodeHandle(m), nil
}

func goStructToVMStruct(host HostInterfaceResolver, vm_ *vm.VM, rv reflect.Value, path string) (vm.Value, error) {
	if rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return unsupportedVMArgument(rv.Interface(), path)
	}
	structName := rv.Type().Name()
	sd := vm_.StructReg().GetStruct(structName)
	if sd == nil {
		return unsupportedVMArgument(rv.Interface(), path+"/struct-not-registered:"+structName)
	}
	fields := sd.Fields()
	fieldValues := make([]vm.Value, len(fields))
	// Root each converted field as it is produced: later field conversions
	// allocate, and unrooted handles (nested maps/arrays/structs) would be
	// swept by a mid-conversion GC pass before NewStructInstance stores them.
	var releases []func()
	defer func() {
		for i := len(releases) - 1; i >= 0; i-- {
			releases[i]()
		}
	}()
	for i, f := range fields {
		fieldRv := rv.FieldByName(f.Name())
		if !fieldRv.IsValid() {
			fieldRv = findFieldByJSONTag(rv, f.Name())
		}
		if !fieldRv.IsValid() {
			return unsupportedVMArgument(rv.Interface(), fmt.Sprintf("%s/field-%s-not-found", path, f.Name()))
		}
		val, err := anyToVMValueAtPath(host, vm_, fieldRv.Interface(), path+"/"+f.Name())
		if err != nil {
			return vm.EncodeInt(0), err
		}
		fieldValues[i] = val
		releases = append(releases, vm_.AddTemporaryRoot(val))
	}
	handle := vm_.NewStructInstance(structName, fieldValues)
	if handle == vm.InvalidHandle {
		return unsupportedVMArgument(rv.Interface(), path+"/new-struct-failed")
	}
	return vm.EncodeHandle(handle), nil
}

func findFieldByJSONTag(rv reflect.Value, name string) reflect.Value {
	typ := rv.Type()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}
		tag := field.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		if idx := strings.Index(tag, ","); idx >= 0 {
			tag = tag[:idx]
		}
		if tag == name {
			return rv.Field(i)
		}
	}
	return reflect.Value{}
}

func projectNativeResult(v any) any {
	if v == nil {
		return nil
	}
	rv := reflect.ValueOf(v)
	if !rv.IsValid() {
		return nil
	}
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	switch rv.Kind() {
	case reflect.Slice:
		if rv.Type().Elem().Kind() == reflect.Uint8 {
			bytes := make([]byte, rv.Len())
			reflect.Copy(reflect.ValueOf(bytes), rv)
			return bytes
		}
		items := make([]any, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			items[i] = projectNativeResult(rv.Index(i).Interface())
		}
		return items
	case reflect.Struct:
		if rv.Type() == reflect.TypeOf(time.Time{}) {
			return rv.Interface().(time.Time).Format(time.RFC3339Nano)
		}
		result := make(map[string]any, rv.NumField())
		rt := rv.Type()
		for i := 0; i < rv.NumField(); i++ {
			field := rt.Field(i)
			if !field.IsExported() {
				continue
			}
			name := binding.JSONTagName(field)
			if name == "-" {
				continue
			}
			result[name] = projectNativeResult(rv.Field(i).Interface())
		}
		return result
	case reflect.Array:
		items := make([]any, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			items[i] = projectNativeResult(rv.Index(i).Interface())
		}
		return items
	case reflect.Map:
		if rv.Type().Key().Kind() == reflect.String {
			result := make(map[string]any, rv.Len())
			iter := rv.MapRange()
			for iter.Next() {
				result[iter.Key().String()] = projectNativeResult(iter.Value().Interface())
			}
			return result
		}
	}
	return v
}

func unsupportedVMArgument(v any, path string) (vm.Value, error) {
	return vm.EncodeInt(0), &RuntimeError{
		Code:     "unsupported_vm_argument_type",
		Category: diagnostics.CategoryRuntime,
		Path:     path,
		Message:  fmt.Sprintf("unsupported VM argument type %T", v),
	}
}

func vmValueToAny(v_ *vm.VM, v vm.Value) any {
	return vmValueToAnyWithHost(nil, v_, v)
}

func vmValueToAnyWithHost(host HostInterfaceResolver, v_ *vm.VM, v vm.Value) any {
	if vm.IsDouble(v) {
		return v_.DecodeDouble(v)
	}
	if vm.IsNull(v) {
		return nil
	}
	if vm.IsBool(v) {
		return vm.DecodeBool(v)
	}
	if vm.IsLong(v) {
		return v_.DecodeLong(v)
	}
	if vm.IsULong(v) {
		return v_.DecodeULong(v)
	}
	if vm.IsInt(v) {
		return int(vm.DecodeInt(v))
	}
	if vm.IsFloat(v) {
		return float64(vm.DecodeFloat(v))
	}
	if vm.IsBytes(v) {
		return v_.DecodeBytes(v)
	}
	if vm.IsString(v) {
		return v_.DecodeString(v)
	}
	if vm.IsHandle(v) {
		h := vm.DecodeHandle(v)
		if host != nil {
			if target, ok := host.HostInterfaceObjectForHandle(h); ok {
				return target
			}
		}
		idx := v_.ResolveHandle(h)
		if idx >= 0 && idx < v_.MemTop() {
			header := v_.MemoryAt(idx)
			if v_.IsLongHeapHeader(header) {
				return v_.DecodeLong(v)
			}
			if v_.IsULongHeapHeader(header) {
				return v_.DecodeULong(v)
			}
			if v_.IsLargeStringHeader(header) {
				return v_.DecodeString(v)
			}
			if v_.IsLargeBytesHeader(header) {
				return v_.DecodeBytes(v)
			}
			if v_.IsMap(h) {
				result := make(map[string]any)
				v_.MapIterate(h, func(key, val vm.Value) bool {
					result[v_.DecodeString(key)] = vmValueToAnyWithHost(host, v_, val)
					return true
				})
				return result
			}
			if v_.IsStruct(h) {
				sd := v_.StructReg().GetStructByID(uint32(v_.MemoryAt(idx)>>32) & 0x3FFFFFFF)
				if sd != nil {
					result := make(map[string]any, sd.FieldCount())
					fields := sd.Fields()
					for i := 0; i < sd.FieldCount(); i++ {
						result[fields[i].Name()] = vmValueToAnyWithHost(host, v_, v_.GetStructFieldByIndex(h, i))
					}
					return result
				}
			}
			length := v_.ArrayLength(h)
			items := make([]any, length)
			for i := 0; i < length; i++ {
				items[i] = vmValueToAnyWithHost(host, v_, v_.GetArrayElement(h, i))
			}
			return items
		}
	}
	return nil
}
