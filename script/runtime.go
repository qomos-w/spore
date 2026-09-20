// Package script is Spore's host-facing public embedding surface for
// script/module execution.
//
// Canonical host binding hierarchy:
//
//   - semantic primitives: BindFunc, BindValue, BindStructDesc, BindTypeAlias
//   - convenience wrappers: BindStruct, BindStructType, BindTypeAliasName,
//     BindObject, Bind
//
// BindObject is batch callable/value registration only; it does not register
// compile-time struct types. Bind is the LLM-friendly convenience dispatcher
// that routes map entries to the appropriate primitive based on Go type.
//
// The canonical happy path is:
//
//	rt, err := script.NewRuntime()
//	rt.BindFunc / rt.BindValue / rt.BindStructDesc / rt.BindTypeAlias
//	rt.SetModuleResolver(...)                     // optional, for imports
//	rt.LoadSource(name, src)                      // seal bindings + parse + link
//	result, err := rt.Call(callable, args...)     // invoke a root callable
//	if err := result.Unwrap(); err != nil { ... } // default error path
//	result.DecodeInto(&dst)                       // unwrap the value
//
// Error/result rule:
//
//   - returned err from Call/CallNext/CallFinal = host or invocation failure
//   - Result.Error = script runtime failure
//   - Result.Unwrap() = default convenience collapse for callers that do not
//     need to distinguish the two layers
//
// Execution-error tracks (see internal/script/bytecode/doc.go for the engine
// side, which is the contract this surface mirrors):
//
//   - script-repairable failures (division by zero, failed casts, bad indices,
//     a bound Go function returning an error, exhausted streams) arrive as a
//     Result.Error whose Diagnostic.Code is the stable code for that condition.
//   - engine-internal invariant violations (VM heap budget exhausted, stale
//     handles, missing descriptors) arrive as a Result.Error too, with the
//     stable code "vm_internal_panic". Call never panics: the bytecode boundary
//     converts every escaped panic into that structured error, so a host does
//     not need its own recover() around Call. Treat vm_internal_panic as "file
//     a bug against the engine", not as something a script can fix.
//
// Sub-packages a host may need to import for deeper inspection (the script
// package re-uses their types in its public fields rather than mirroring
// them, on purpose):
//
//   - github.com/qomos-w/spore/diagnostics — CompileError.Diagnostic and
//     RuntimeError.Diagnostic carry diagnostics.Descriptor with structured
//     code/category/path/span/stack fields. Import this package when
//     branching on those fields.
//   - github.com/qomos-w/spore/schema — CallableInfo's Mode/Parameters/
//     Returns and the helper ObjectDesc are schema package types. Import
//     this package when introspecting Spore's type/argument shapes.
//   - github.com/qomos-w/spore/binding — BoundValue.Desc is a
//     binding.CapabilityValueDesc. Import this package when host code wants
//     the value-binding metadata (e.g., kind, exported name).
//
// Internal Spore packages (everything under internal/) are deliberately
// not part of this surface; the Runtime facade is the only stable entry
// point. Existing tests in package script_test exercise this entire surface
// without importing any internal/ package.
package script

import (
	"errors"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/internal/script/bytecode"
	"github.com/qomos-w/spore/internal/script/frontend"
)

// ErrRuntimeAlreadyLoaded is returned by LoadModule/LoadSource when a root
// module has already been loaded. It is not a compile error: the source was
// never parsed. Hosts that want to react to this case should test with
// errors.Is(err, script.ErrRuntimeAlreadyLoaded) and construct a fresh
// Runtime to load different source.
var ErrRuntimeAlreadyLoaded = errors.New("runtime already has root module loaded")

// Runtime is Spore's public host-facing facade for script/module execution and embedding.
type Runtime struct {
	binding           *binding.ScriptBinding
	frontend          *frontend.Frontend
	evaluator         *bytecode.VMEvaluator
	rootModule        string
	boundFuncs        map[string]BoundFunction
	boundValues       map[string]BoundValue
	boundObjects      map[string]BoundStruct
	boundInterfaces   map[string]BoundInterface
	boundTypeAliases  map[string]BoundTypeAlias
	pendingNamespaces map[string]*binding.CapabilityBuilder
	bindingsCommitted bool
	moduleResolver    ModuleResolver
	closed            bool
	hostIface         hostInterfaceLedger
	// vmHeapBytes / vmHeapSlots remember the configured VM memory budget
	// across Clone/Reset so the rebuilt evaluator keeps its memory cap.
	// Zero means "use the default budget" — the same opt-in semantics as
	// RuntimeOptions.VMHeapBytes / VMHeapSlots.
	vmHeapBytes int
	vmHeapSlots int
}

// RuntimeOptions holds optional Runtime construction knobs.
type RuntimeOptions struct {
	// ScriptBinding, if non-nil, is used as the backing binding surface instead
	// of a fresh ScriptBinding. This allows hosts to pre-register capabilities
	// (e.g. the standard library via std.NewScriptBindingWithStd) before the
	// Runtime is constructed.
	ScriptBinding *binding.ScriptBinding
	// VMHeapBytes overrides the VM heap budget (in bytes) used by the
	// underlying bytecode evaluator. A value of 0 (the default) means
	// DefaultVMHeapBytes (4 MiB); positive values scale the flat VM heap
	// to accommodate workloads that materialise deeply-nested values such
	// as ecsbind.World.View's map<string, []map<string, any>> envelope at
	// large N. Exceeding the budget after GC is reported as a structured
	// runtime error with code "vm_internal_panic" — the evaluator boundary
	// converts the VM's out-of-memory panic (see the package doc's
	// execution-error tracks) — so it is a hard failure, not a script bug:
	// budget-sensitive embedders must still size this explicitly. It is
	// preserved across Clone and Reset so a runtime rebuilt mid-session
	// keeps its budget.
	VMHeapBytes int
	// VMHeapSlots overrides the operand-stack capacity used by the
	// underlying bytecode evaluator's single execution stack. A value of
	// 0 (the default) means the historical 256-slot reserve; positive
	// values raise it. The interpreter stack grows on demand, so this is
	// the initial reserve, not a hard cap. The setting is purely opt-in
	// and behaves like VMHeapBytes with respect to defaults and
	// persistence across Clone/Reset.
	VMHeapSlots int
}

// ModuleSpec identifies a named source module to load into Runtime.
type ModuleSpec struct {
	Name   string
	Source string
}

// NewRuntime constructs a Runtime with default settings. It is the canonical
// happy-path constructor; hosts that need to pass non-default RuntimeOptions
// should use NewRuntimeWith instead. NewRuntime is defined as
// NewRuntimeWith(RuntimeOptions{}).
func NewRuntime() (*Runtime, error) {
	return NewRuntimeWith(RuntimeOptions{})
}

// NewRuntimeWith constructs a Runtime with the supplied RuntimeOptions. Most
// callers should prefer NewRuntime; NewRuntimeWith exists so future
// RuntimeOptions fields are reachable without a second constructor.
func NewRuntimeWith(opts RuntimeOptions) (*Runtime, error) {
	sb := opts.ScriptBinding
	if sb == nil {
		sb = binding.NewScriptBinding()
	}
	fe, err := frontend.New(sb)
	if err != nil {
		return nil, err
	}
	eval := bytecode.NewVMEvaluatorWith(opts.VMHeapBytes, opts.VMHeapSlots)
	eval.SetNativeBinding(sb)
	fe.SetVMCompileHook(eval)
	rt := &Runtime{
		binding:           sb,
		frontend:          fe,
		evaluator:         eval,
		boundFuncs:        make(map[string]BoundFunction),
		boundValues:       make(map[string]BoundValue),
		boundObjects:      make(map[string]BoundStruct),
		boundInterfaces:   make(map[string]BoundInterface),
		boundTypeAliases:  make(map[string]BoundTypeAlias),
		pendingNamespaces: make(map[string]*binding.CapabilityBuilder),
		hostIface:         newHostInterfaceLedger(),
		vmHeapBytes:       opts.VMHeapBytes,
		vmHeapSlots:       opts.VMHeapSlots,
	}
	rt.attachHostIfaceRoots(eval)
	eval.SetHostInterfaceResolver(rt)
	return rt, nil
}
