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
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/diagnostics"
	"github.com/qomos-w/spore/internal/script/bytecode"
	"github.com/qomos-w/spore/internal/script/frontend"
	"github.com/qomos-w/spore/internal/script/vm"
	"github.com/qomos-w/spore/schema"
)

// ErrRuntimeAlreadyLoaded is returned by LoadModule/LoadSource when a root
// module has already been loaded. It is not a compile error: the source was
// never parsed. Hosts that want to react to this case should test with
// errors.Is(err, script.ErrRuntimeAlreadyLoaded) and construct a fresh
// Runtime to load different source.
var ErrRuntimeAlreadyLoaded = errors.New("runtime already has root module loaded")

// Runtime is Spore's public host-facing facade for script/module execution and embedding.
type Runtime struct {
	binding               *binding.ScriptBinding
	frontend              *frontend.Frontend
	evaluator             *bytecode.VMEvaluator
	rootModule            string
	boundFuncs            map[string]BoundFunction
	boundValues           map[string]BoundValue
	boundObjects          map[string]BoundStruct
	boundInterfaces       map[string]BoundInterface
	boundTypeAliases      map[string]BoundTypeAlias
	pendingNamespaces     map[string]*binding.CapabilityBuilder
	bindingsCommitted     bool
	moduleResolver        ModuleResolver
	closed                bool
	hostInterfaceClasses  map[string]hostInterfaceClass
	hostInterfaceObjects  map[uint64]hostInterfaceObject
	hostInterfaceHandles  map[vm.Handle]uint64
	hostInterfaceBindings map[hostInterfaceBindingKey]uint64
	nextHostInterfaceID   uint64
	// hostIfaceRootProviderVM is the VM on which hostIfaceRootProviderID is
	// registered. Tracked by pointer so the provider is re-registered when the
	// execution VM is (re)created — the proxy objects allocated via CreateObject
	// are not otherwise GC-rooted and are reclaimed under sustained allocation.
	hostIfaceRootProviderVM  *vm.VM
	hostIfaceRootProviderID  int
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
	// large N. Exceeding the budget after GC panics ("out of memory") —
	// budget-sensitive embedders must set this explicitly and recover at
	// their invocation boundary. It is preserved across Clone and Reset so
	// a runtime rebuilt mid-session keeps its budget.
	VMHeapBytes int
	// VMHeapSlots overrides the VM call-stack slot count used by the
	// underlying bytecode evaluator. A value of 0 (the default) means the
	// historical 256-slot budget; positive values raise the cap. The
	// setting is purely opt-in and behaves like VMHeapBytes with respect
	// to defaults and persistence across Clone/Reset.
	VMHeapSlots int
}

// ModuleSpec identifies a named source module to load into Runtime.
type ModuleSpec struct {
	Name   string
	Source string
}

// Result is the public value surface returned from Runtime calls.
//
// Default recommendation: first call Unwrap when you only care whether the
// invocation succeeded, then DecodeInto the value. Use the tri-state helpers
// (Ok/Void/Error) only when you explicitly need to distinguish void success
// from runtime failure.
//
// Observable states:
//
//   - Success with a value: Error is nil, Value is non-nil. The script
//     returned a regular result.
//   - Success without a value: Error is nil, Value is nil. The script
//     returned void (or returned an explicit nil/null — these are
//     indistinguishable at this layer).
//   - Failure: Error is non-nil. The script raised a runtime error; Value
//     should be ignored.
//
// Returned err from Call/CallNext/CallFinal is a different channel: it signals
// host-side or invocation-pipeline failure before a successful script result
// was produced.
type Result struct {
	Value any
	Error *RuntimeError
	err   error // host-side error captured for Unwrap
}

// Ok reports whether the call succeeded (Error is nil). It is true for both
// the "value returned" and "void return" cases. Hosts that need to
// distinguish those two cases additionally check Void or Value != nil.
func (r Result) Ok() bool { return r.Error == nil }

// Void reports whether the call succeeded but produced no value. It is true
// only when Error is nil and Value is nil. Returning false does NOT prove a
// runtime error — also check Ok.
func (r Result) Void() bool { return r.Error == nil && r.Value == nil }

// DecodeInto copies the result value into dst when the types are compatible.
// Supported destinations: *any, *string, *bool, *int (and its sized variants),
// *uint (and its sized variants), *float32, *float64, *[]any, *map[string]any.
// Numeric values are converted with Go's standard cast rules; mismatches and
// unsupported targets return a non-nil error. If the result already carries a
// runtime error, that error is surfaced and dst is left untouched.
func (r Result) DecodeInto(dst any) error {
	if r.Error != nil {
		return r.Error
	}
	if dst == nil {
		return fmt.Errorf("decode target cannot be nil")
	}
	return decodeInto(dst, r.Value)
}

// Unwrap returns a single error that merges both host-side failure and script
// runtime failure (Result.Error). This is the default convenience path for
// callers that only care whether the invocation succeeded, not which layer
// failed.
//
// Usage:
//
//	result, err := rt.Call("greet")
//	if err != nil {
//	    return err
//	}
//	if err := result.Unwrap(); err != nil {
//	    return err
//	}
//	var s string
//	result.DecodeInto(&s)
//
// Unwrap returns nil when the call succeeded (Ok is true). It never loses
// structured diagnostic information: if the error came from script code, it is
// still a *RuntimeError with Code/Category/Stack fields.
func (r Result) Unwrap() error {
	if r.Error != nil {
		return r.Error
	}
	if r.err != nil {
		return r.err
	}
	return nil
}

// AsString returns the result value as a string. It is a convenience over
// DecodeInto for callers that only need a string and don't want to declare a
// variable and pass its address.
func (r Result) AsString() (string, error) {
	var s string
	if err := r.DecodeInto(&s); err != nil {
		return "", err
	}
	return s, nil
}

// AsInt returns the result value as an int. Numeric values are converted with
// Go's standard int64→int cast rules.
func (r Result) AsInt() (int, error) {
	var n int
	if err := r.DecodeInto(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// AsSlice returns the result value as a []any slice.
func (r Result) AsSlice() ([]any, error) {
	var s []any
	if err := r.DecodeInto(&s); err != nil {
		return nil, err
	}
	return s, nil
}

// AsMap returns the result value as a map[string]any.
func (r Result) AsMap() (map[string]any, error) {
	var m map[string]any
	if err := r.DecodeInto(&m); err != nil {
		return nil, err
	}
	return m, nil
}

// CallableInfo is the host-facing callable discovery surface exposed by Runtime.
type CallableInfo struct {
	Name       string
	Mode       schema.CallableMode
	Parameters []schema.ParameterDesc
	Returns    []schema.TypeDesc
	ReqDesc    *schema.ObjectDesc
	RespDesc   *schema.ObjectDesc
}

// BoundFunction describes a host-bound callable exposed through Runtime.
// Info captures the same host-facing shape as Exports/LookupCallable.
type BoundFunction struct {
	Namespace string
	Name      string
	Callable  string
	Info      CallableInfo
}

// BoundValue describes a host-bound read-only value exposed through Runtime.
type BoundValue struct {
	Namespace string
	Name      string
	Desc      binding.CapabilityValueDesc
}

// BoundStruct describes a host-bound struct/object type exposed through Runtime.
// It is registered via BindStructDesc or BindStruct and becomes importable by
// script code as a compile-time type (e.g. `import Point from "host"`).
type BoundStruct struct {
	Namespace  string
	Name       string
	ObjectDesc schema.ObjectDesc
}

// BoundInterface describes a host-bound interface type exposed through Runtime.
// It is registered via BindInterfaceDesc or BindInterfaceObject and becomes
// importable by script code as a compile-time interface type.
type BoundInterface struct {
	Namespace     string
	Name          string
	InterfaceDesc schema.InterfaceDesc
}

type hostInterfaceBindingKey struct {
	Namespace string
	Name      string
}

type hostInterfaceClass struct {
	Namespace      string
	Name           string
	InterfaceDesc  schema.InterfaceDesc
	ProxyClassName string
	ClassID        uint32
}

type hostInterfaceObject struct {
	ID             uint64
	Namespace      string
	Name           string
	InterfaceDesc  schema.InterfaceDesc
	Target         any
	ProxyClassName string
	Handle         vm.Handle
	Pending        bool
}

// BoundTypeAlias describes a host-bound type alias exposed through Runtime.
// It is registered via BindTypeAlias and becomes importable by script code as a
// compile-time type (e.g. `import UserId from "host"`).
type BoundTypeAlias struct {
	Namespace string
	Name      string
	TypeDesc  schema.TypeDesc
}

// ModuleResolver resolves an import path into module source. The path is the
// canonical module identity (it appears verbatim in the script's `import …
// from "<path>"` form), so the resolver only needs to supply the source. The
// resolver is consulted when the root source is loaded; missing modules
// surface as compile errors.
type ModuleResolver interface {
	ResolveModule(path string) (string, error)
}

// ModuleResolverFunc is an adapter to allow the use of ordinary functions as
// ModuleResolver implementations.
type ModuleResolverFunc func(path string) (string, error)

// ResolveModule calls f(path).
func (f ModuleResolverFunc) ResolveModule(path string) (string, error) { return f(path) }

// CompileError marks a compile/load/link failure surfaced by Runtime. The
// embedded Diagnostic preserves structured context (Code, Category, Path,
// Span) so hosts can branch on stable identifiers and surface precise
// locations rather than parsing opaque text.
type CompileError struct {
	Err        error
	Diagnostic diagnostics.Descriptor
}

func (e *CompileError) Error() string {
	if e == nil || e.Err == nil {
		return "compile error"
	}
	return e.Err.Error()
}

// Unwrap returns the wrapped underlying error so errors.Is and errors.As can
// reach sentinels and structured types beneath the public CompileError shell.
func (e *CompileError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// RuntimeError marks an invocation/runtime failure surfaced by Runtime. The
// embedded Diagnostic preserves structured context (Code, Category, Stack,
// Cause, etc.) so hosts can branch on stable identifiers, walk the script
// stack, and surface precise repair information without parsing text.
type RuntimeError struct {
	Err        error
	Diagnostic diagnostics.Descriptor
}

func (e *RuntimeError) Error() string {
	if e == nil || e.Err == nil {
		return "runtime error"
	}
	return e.Err.Error()
}

// Unwrap returns the wrapped underlying error so errors.Is and errors.As can
// reach sentinels and structured types beneath the public RuntimeError shell.
func (e *RuntimeError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
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
		binding:               sb,
		frontend:              fe,
		evaluator:             eval,
		boundFuncs:            make(map[string]BoundFunction),
		boundValues:           make(map[string]BoundValue),
		boundObjects:          make(map[string]BoundStruct),
		boundInterfaces:       make(map[string]BoundInterface),
		boundTypeAliases:      make(map[string]BoundTypeAlias),
		pendingNamespaces:     make(map[string]*binding.CapabilityBuilder),
		hostInterfaceClasses:  make(map[string]hostInterfaceClass),
		hostInterfaceObjects:  make(map[uint64]hostInterfaceObject),
		hostInterfaceHandles:  make(map[vm.Handle]uint64),
		hostInterfaceBindings: make(map[hostInterfaceBindingKey]uint64),
		vmHeapBytes:           opts.VMHeapBytes,
		vmHeapSlots:           opts.VMHeapSlots,
	}
	rt.attachHostIfaceRoots(eval)
	eval.SetHostInterfaceResolver(rt)
	return rt, nil
}

// LoadModule loads a named module source into the Runtime as the root module.
// LoadSource is the canonical happy-path entry point; LoadModule exists so
// callers that already construct a ModuleSpec value (or that need to evolve
// their setup as ModuleSpec gains fields) can pass it through unchanged.
//
// Each Runtime instance can only load a root module once: subsequent calls
// return ErrRuntimeAlreadyLoaded (testable via errors.Is) — a state error,
// not a compile error. Argument-validation failures (empty name/source,
// uninitialized runtime) are also plain errors. Only genuine
// compile/load/link failures returned by the parser/linker are wrapped as
// *CompileError. Imports declared by the root source are resolved through
// the configured ModuleResolver; resolved imports become loaded modules
// visible to Exports and LookupCallable but are not themselves callable
// entry points.
func (rt *Runtime) LoadModule(spec ModuleSpec) error {
	if rt == nil || rt.frontend == nil {
		return fmt.Errorf("runtime is not initialized")
	}
	if rt.closed {
		return fmt.Errorf("runtime is closed")
	}
	if spec.Name == "" {
		return fmt.Errorf("module name cannot be empty")
	}
	if spec.Source == "" {
		return fmt.Errorf("module source cannot be empty")
	}
	if rt.rootModule != "" {
		return fmt.Errorf("%w: %q", ErrRuntimeAlreadyLoaded, rt.rootModule)
	}
	if err := rt.commitPendingBindings(); err != nil {
		return wrapCompileError(err)
	}
	if err := rt.frontend.LoadSource(spec.Source); err != nil {
		return wrapCompileError(err)
	}
	if err := rt.finalizePendingHostInterfaces(); err != nil {
		return wrapCompileError(err)
	}
	rt.rootModule = spec.Name
	return nil
}

// LoadSource is the canonical entry point for loading the root module: it
// takes the module name and source text directly, with no spec wrapper. Hosts
// should reach for LoadSource by default and only fall back to LoadModule
// when they already have a ModuleSpec (or need ModuleSpec's future fields).
// LoadSource is defined as LoadModule(ModuleSpec{Name: name, Source: source})
// and shares all of LoadModule's error semantics.
func (rt *Runtime) LoadSource(name string, source string) error {
	return rt.LoadModule(ModuleSpec{Name: name, Source: source})
}

// LoadPackage loads a validated multi-module package. The package entry module
// is loaded as the runtime root and imports are resolved from package.Modules.
func (rt *Runtime) LoadPackage(pkg Package) error {
	if err := pkg.Validate(); err != nil {
		return err
	}
	if pkg.Hash != "" && pkg.Hash != pkg.ComputeHash() {
		return fmt.Errorf("package hash mismatch")
	}
	existing := rt.moduleResolver
	rt.SetModuleResolver(ModuleResolverFunc(func(path string) (string, error) {
		if source, ok := pkg.Modules[path]; ok {
			return source, nil
		}
		if existing != nil {
			return existing.ResolveModule(path)
		}
		return "", fmt.Errorf("module %q not found in package", path)
	}))
	if err := rt.LoadSource(pkg.EntryModule, pkg.Modules[pkg.EntryModule]); err != nil {
		rt.SetModuleResolver(existing)
		return err
	}
	return nil
}

// ReloadPackage performs a transactional package reload. The current runtime
// remains intact if validation or loading of the replacement fails.
func (rt *Runtime) ReloadPackage(pkg Package) error {
	if rt == nil {
		return fmt.Errorf("runtime is not initialized")
	}
	if err := pkg.Validate(); err != nil {
		return err
	}
	if pkg.Hash != "" && pkg.Hash != pkg.ComputeHash() {
		return fmt.Errorf("package hash mismatch")
	}
	candidate, err := rt.Clone()
	if err != nil {
		return err
	}
	if err := candidate.LoadPackage(pkg); err != nil {
		candidate.Close()
		return err
	}
	old := *rt
	*rt = *candidate
	candidate = nil
	old.Close()
	return nil
}

// LoadSourceWithDeps is a convenience that loads the root module together with
// inline dependency sources, without requiring a ModuleResolver. The deps map
// keys are import paths and values are module source strings. This is useful
// for simple scripts with a few inline imports where setting up a full
// ModuleResolver would be overkill.
//
// Example:
//
//	rt.LoadSourceWithDeps("app", `import { add } from "math"
//	export fun main(): int { return add(1, 2) }`, map[string]string{
//	    "math": `export fun add(a: int, b: int): int { return a + b }`,
//	})
//
// LoadSourceWithDeps composes freely with SetModuleResolver: inline deps take
// precedence over the resolver, and if a path is not found in deps, the
// resolver is consulted as a fallback.
func (rt *Runtime) LoadSourceWithDeps(name string, source string, deps map[string]string) error {
	if rt == nil || rt.frontend == nil {
		return fmt.Errorf("runtime is not initialized")
	}
	if rt.closed {
		return fmt.Errorf("runtime is closed")
	}
	if rt.rootModule != "" {
		return fmt.Errorf("%w: %q", ErrRuntimeAlreadyLoaded, rt.rootModule)
	}

	// Save existing resolver.
	existingResolver := rt.moduleResolver

	// Build a composite resolver: deps first, then existing resolver.
	composite := ModuleResolverFunc(func(path string) (string, error) {
		if src, ok := deps[path]; ok {
			return src, nil
		}
		if existingResolver != nil {
			return existingResolver.ResolveModule(path)
		}
		return "", fmt.Errorf("module %q not found", path)
	})

	rt.SetModuleResolver(composite)
	defer rt.SetModuleResolver(existingResolver)

	return rt.LoadSource(name, source)
}

// RootModule returns the name of the active root module, or "" if no module
// has been loaded yet. The root module is the only module whose callables can
// be invoked through Call as host-facing entry points.
func (rt *Runtime) RootModule() string {
	if rt == nil {
		return ""
	}
	return rt.rootModule
}

// LoadedModules returns the names of all modules currently loaded in the
// Runtime. The first entry is always the root module (the name passed to
// LoadModule/LoadSource), followed by any modules transitively resolved
// through imports in sorted order. If no module has been loaded yet, the
// result is nil.
func (rt *Runtime) LoadedModules() []string {
	if rt == nil || rt.frontend == nil || rt.rootModule == "" {
		return nil
	}
	seen := map[string]struct{}{rt.rootModule: {}}
	result := []string{rt.rootModule}
	for _, summary := range rt.frontend.AllModuleSummaries() {
		if summary.Path == "" {
			continue
		}
		if _, exists := seen[summary.Path]; exists {
			continue
		}
		seen[summary.Path] = struct{}{}
		result = append(result, summary.Path)
	}
	return result
}

// SetModuleResolver installs the module resolver used for import linking. The
// resolver is consulted whenever the loaded source contains an `import …
// from "<path>"` declaration. Passing nil clears any previously installed
// resolver. SetModuleResolver does not return an error: it is a pure store.
func (rt *Runtime) SetModuleResolver(r ModuleResolver) {
	if rt == nil || rt.frontend == nil {
		return
	}
	rt.moduleResolver = r
	if r == nil {
		rt.frontend.SetModuleResolver(nil)
		return
	}
	rt.frontend.SetModuleResolver(r)
}

// BindFunc exposes a host function under a namespace and name. Multiple
// BindFunc / BindValue / BindObject calls accumulate into the same namespace
// until the bindings are sealed by the first LoadModule/LoadSource call.
// After that point, further Bind* calls on any namespace return an error.
func (rt *Runtime) BindFunc(namespace, name string, fn any) error {
	if rt == nil || rt.binding == nil {
		return fmt.Errorf("runtime is not initialized")
	}
	if rt.bindingsCommitted {
		return fmt.Errorf("bindings are sealed: a module has already been loaded")
	}
	if namespace == "" {
		return fmt.Errorf("namespace cannot be empty")
	}
	builder := rt.builderForNamespace(namespace)
	if err := builder.AddFreeFunction(name, fn); err != nil {
		return err
	}
	return rt.refreshNamespaceSnapshot(namespace, builder)
}

// BindValue exposes a read-only host value under a namespace and name.
// Like BindFunc, multiple BindValue / BindFunc / BindObject calls accumulate
// into the same namespace until LoadModule/LoadSource seals the surface.
func (rt *Runtime) BindValue(namespace, name string, v any) error {
	if rt == nil || rt.binding == nil {
		return fmt.Errorf("runtime is not initialized")
	}
	if rt.bindingsCommitted {
		return fmt.Errorf("bindings are sealed: a module has already been loaded")
	}
	if namespace == "" {
		return fmt.Errorf("namespace cannot be empty")
	}
	return rt.bindPendingValue(namespace, name, v)
}

func (rt *Runtime) bindPendingValue(namespace, name string, v any) error {
	builder := rt.builderForNamespace(namespace)
	if err := builder.AddValue(name, v); err != nil {
		return err
	}
	return rt.refreshNamespaceSnapshot(namespace, builder)
}

// BindStructDesc registers a schema-described struct type under a namespace and
// name, making it importable by script code as a compile-time struct type.
// This is the canonical descriptor-based registration API for host-defined
// struct shapes.
//
// After BindStructDesc, script code can write:
//
//	import Point from "geom"
//	fun dist(p: Point): int { return p.x + p.y }
//
// Field naming convention:
//
// Script code references fields by the exact Name string in
// schema.FieldDesc. BindStruct (the reflection convenience wrapper) derives
// field names from Go json tags and falls back to the Go field name, which is
// typically lowerCamelCase or PascalCase depending on tags. To keep
// descriptor-first registrations consistent with reflection-derived ones,
// prefer the same naming convention you would expect from BindStruct on the
// matching Go type. Mixing PascalCase here with lowerCamelCase elsewhere will
// produce inconsistent script-visible names without any error from
// BindStructDesc itself.
//
// BindStructDesc only defines compile-time type visibility. It does not bind a
// runtime Go object instance; hosts that need runtime object binding should use
// the binding package directly.
func (rt *Runtime) BindStructDesc(namespace, name string, desc schema.ObjectDesc) error {
	if rt == nil || rt.binding == nil {
		return fmt.Errorf("runtime is not initialized")
	}
	if rt.bindingsCommitted {
		return fmt.Errorf("bindings are sealed: a module has already been loaded")
	}
	if namespace == "" {
		return fmt.Errorf("namespace cannot be empty")
	}
	if name == "" {
		return fmt.Errorf("struct name cannot be empty")
	}
	if err := validateStructDescriptor(name, desc); err != nil {
		return err
	}
	builder := rt.builderForNamespace(namespace)
	if err := builder.AddObject(name, desc); err != nil {
		return err
	}
	rt.registerBoundStructInVM(name, desc)
	return rt.refreshNamespaceSnapshot(namespace, builder)
}

// BindStruct registers a Go struct type under a namespace and name, making it
// importable by script code as a compile-time struct type. This is a
// reflection-based convenience wrapper over BindStructDesc. The value argument
// should be a zero value of the struct type (e.g. `MyStruct{}`); its type is
// reflected to produce the schema.ObjectDesc that the linker uses for type
// checking and script-visible field names.
//
// After BindStruct, script code can write:
//
//	import Point from "geom"
//	fun dist(p: Point): int { return p.x + p.y }
//
// BindStruct composes freely with BindFunc/BindValue/BindObject/BindTypeAlias
// on the same namespace until LoadModule/LoadSource seals the surface.
func (rt *Runtime) BindStruct(namespace, name string, value any) error {
	if value == nil {
		return fmt.Errorf("struct value cannot be nil")
	}
	obj, err := schema.DescribeGoStruct(value)
	if err != nil {
		return fmt.Errorf("cannot describe struct %q: %w", name, err)
	}
	return rt.BindStructDesc(namespace, name, obj)
}

// BindInterfaceDesc registers a schema-described interface type under a namespace and
// name, making it importable by script code as a compile-time interface type.
// The descriptor is method-only: fields, inheritance metadata, and runtime object
// state are not part of this surface.
func (rt *Runtime) BindInterfaceDesc(namespace, name string, desc schema.InterfaceDesc) error {
	if rt == nil || rt.binding == nil {
		return fmt.Errorf("runtime is not initialized")
	}
	if rt.bindingsCommitted {
		return fmt.Errorf("bindings are sealed: a module has already been loaded")
	}
	if namespace == "" {
		return fmt.Errorf("namespace cannot be empty")
	}
	if name == "" {
		return fmt.Errorf("interface name cannot be empty")
	}
	if err := validateInterfaceDescriptor(name, desc); err != nil {
		return err
	}
	builder := rt.builderForNamespace(namespace)
	if err := builder.AddInterface(name, desc); err != nil {
		return err
	}
	return rt.refreshNamespaceSnapshot(namespace, builder)
}

// BindInterfaceObject registers a host-backed interface object under a namespace and
// name. The bound symbol is importable as both a compile-time interface type and
// a runtime value whose surface exposes only the descriptor's methods.
func (rt *Runtime) BindInterfaceObject(namespace, name string, desc schema.InterfaceDesc, target any) error {
	if target == nil {
		return fmt.Errorf("interface target cannot be nil")
	}
	if err := rt.BindInterfaceDesc(namespace, name, desc); err != nil {
		return err
	}
	key := hostInterfaceBindingKey{Namespace: namespace, Name: name}
	if existingID, ok := rt.hostInterfaceBindings[key]; ok {
		delete(rt.hostInterfaceObjects, existingID)
	}
	objID := atomic.AddUint64(&rt.nextHostInterfaceID, 1)
	rt.hostInterfaceBindings[key] = objID
	rt.hostInterfaceObjects[objID] = hostInterfaceObject{
		ID:             objID,
		Namespace:      namespace,
		Name:           name,
		InterfaceDesc:  schema.CloneInterfaceDesc(desc),
		Target:         target,
		ProxyClassName: hostInterfaceProxyClassName(namespace, name),
		Handle:         vm.InvalidHandle,
		Pending:        true,
	}
	return rt.bindPendingValue(namespace, name, target)
}

// BindStructType is a generic convenience over BindStruct that does not require
// a zero value instance. The type parameter T is the Go struct type to register.
//
// Example:
//
//	rt.BindStructType[Point]("geom", "Point")
//
// BindStructType delegates to BindStruct internally and shares all of its
// validation and sealing semantics.
func BindStructType[T any](rt *Runtime, namespace, name string) error {
	var zero T
	return rt.BindStruct(namespace, name, zero)
}

// BindTypeAlias registers a compile-time type alias under a namespace and name,
// making it importable by script code. This is useful for exposing canonical
// type names (e.g. `UserId`, `Timestamp`) that script code can use in type
// annotations without re-declaring the underlying shape.
//
// After BindTypeAlias, script code can write:
//
//	import UserId from "types"
//	fun greet(id: UserId): string { return "hello " + id }
//
// BindTypeAlias composes freely with BindFunc/BindValue/BindObject/BindStruct
// on the same namespace until LoadModule/LoadSource seals the surface.
func (rt *Runtime) BindTypeAlias(namespace, name string, td schema.TypeDesc) error {
	if rt == nil || rt.binding == nil {
		return fmt.Errorf("runtime is not initialized")
	}
	if rt.bindingsCommitted {
		return fmt.Errorf("bindings are sealed: a module has already been loaded")
	}
	if namespace == "" {
		return fmt.Errorf("namespace cannot be empty")
	}
	if name == "" {
		return fmt.Errorf("type alias name cannot be empty")
	}
	builder := rt.builderForNamespace(namespace)
	if err := builder.AddTypeAlias(name, td); err != nil {
		return err
	}
	return rt.refreshNamespaceSnapshot(namespace, builder)
}

// BindTypeAliasName is a convenience shorthand for BindTypeAlias that accepts a
// primitive type name string instead of a full schema.TypeDesc. Supported names
// are the canonical Spore scalar types: "string", "int", "long", "float",
// "double", "bool", and "any". For complex or custom types, use BindTypeAlias
// with an explicit schema.TypeDesc.
func (rt *Runtime) BindTypeAliasName(namespace, name string, typeName string) error {
	td := schema.TypeDesc{Kind: schema.TypeKindScalar, Name: typeName}
	return rt.BindTypeAlias(namespace, name, td)
}

// BindObject exposes a group of host-visible surfaces under a single
// namespace in one call. Map entries whose values are Go functions become
// callables; other entries become read-only values. BindObject does NOT
// register struct/object types — use BindStructDesc for the canonical
// descriptor-based API, or BindStruct as the reflection convenience form.
//
// BindObject is a convenience shape for simple hosts; the underlying
// binding/capability model remains the canonical authority. Hosts that need
// stricter typing or richer schema should reach for the binding package
// directly rather than treating BindObject as a second module system.
func (rt *Runtime) BindObject(namespace string, values map[string]any) error {
	if rt == nil || rt.binding == nil {
		return fmt.Errorf("runtime is not initialized")
	}
	if rt.bindingsCommitted {
		return fmt.Errorf("bindings are sealed: a module has already been loaded")
	}
	if namespace == "" {
		return fmt.Errorf("namespace cannot be empty")
	}
	if len(values) == 0 {
		return nil
	}
	builder := rt.builderForNamespace(namespace)
	for name, v := range values {
		if name == "" {
			return fmt.Errorf("BindObject %q: entry name cannot be empty", namespace)
		}
		if v != nil && reflect.TypeOf(v).Kind() == reflect.Func {
			if err := builder.AddFreeFunction(name, v); err != nil {
				return err
			}
			continue
		}
		if err := builder.AddValue(name, v); err != nil {
			return err
		}
	}
	return rt.refreshNamespaceSnapshot(namespace, builder)
}

// Bind is a unified convenience dispatcher that automatically routes each map
// entry to the appropriate binding primitive based on its Go type:
//
//   - function values → BindFunc (callable)
//   - struct values   → BindStruct (reflection convenience over BindStructDesc)
//   - everything else → BindValue (read-only value)
//
// Bind is the LLM-friendly convenience entry point when callers want one map to
// contain both runtime values and compile-time type definitions. It is not a
// new semantic category; it is derived from the canonical binding primitives.
//
// Example:
//
//	rt.Bind("env", map[string]any{
//	    "echo":   func(s string) string { return s },
//	    "name":   "spore",
//	    "Config": Config{},  // struct zero value → type definition
//	})
//
// Bind composes freely with itself and with explicit BindFunc/BindValue/BindStruct
// calls on the same namespace. Entries are processed in map iteration order.
func (rt *Runtime) Bind(namespace string, values map[string]any) error {
	if rt == nil || rt.binding == nil {
		return fmt.Errorf("runtime is not initialized")
	}
	if rt.bindingsCommitted {
		return fmt.Errorf("bindings are sealed: a module has already been loaded")
	}
	if namespace == "" {
		return fmt.Errorf("namespace cannot be empty")
	}
	if len(values) == 0 {
		return nil
	}
	for name, v := range values {
		if name == "" {
			return fmt.Errorf("Bind %q: entry name cannot be empty", namespace)
		}
		if v == nil {
			if err := rt.BindValue(namespace, name, nil); err != nil {
				return err
			}
			continue
		}
		rv := reflect.ValueOf(v)
		typ := rv.Type()
		switch typ.Kind() {
		case reflect.Func:
			if err := rt.BindFunc(namespace, name, v); err != nil {
				return err
			}
		case reflect.Struct:
			if typ == reflect.TypeOf(time.Time{}) {
				// time.Time is a value, not a type definition.
				if err := rt.BindValue(namespace, name, v); err != nil {
					return err
				}
			} else {
				if err := rt.BindStruct(namespace, name, v); err != nil {
					return err
				}
			}
		default:
			if err := rt.BindValue(namespace, name, v); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateStructDescriptor(name string, desc schema.ObjectDesc) error {
	if desc.Kind != schema.TypeKindStruct {
		return fmt.Errorf("struct descriptor kind must be struct")
	}
	if desc.Name != "" && desc.Name != name {
		return fmt.Errorf("struct descriptor name %q does not match binding name %q", desc.Name, name)
	}
	if desc.Parent != "" || desc.IsOpen || len(desc.Implements) > 0 || len(desc.Methods) > 0 {
		return fmt.Errorf("struct descriptor must not declare parent/open/interfaces/methods")
	}
	seen := make(map[string]struct{}, len(desc.Fields))
	for _, field := range desc.Fields {
		if field.Name == "" {
			return fmt.Errorf("struct descriptor field name cannot be empty")
		}
		if _, ok := seen[field.Name]; ok {
			return fmt.Errorf("duplicate field %q in struct descriptor", field.Name)
		}
		seen[field.Name] = struct{}{}
		if err := validateTypeDesc(field.Type); err != nil {
			return fmt.Errorf("field %q: %w", field.Name, err)
		}
	}
	return nil
}

func validateInterfaceDescriptor(name string, desc schema.InterfaceDesc) error {
	if desc.Name != "" && desc.Name != name {
		return fmt.Errorf("interface descriptor name %q does not match binding name %q", desc.Name, name)
	}
	seen := make(map[string]struct{}, len(desc.Methods))
	for _, method := range desc.Methods {
		if method.Name == "" {
			return fmt.Errorf("interface descriptor method name cannot be empty")
		}
		if _, ok := seen[method.Name]; ok {
			return fmt.Errorf("duplicate method %q in interface descriptor", method.Name)
		}
		seen[method.Name] = struct{}{}
		for _, param := range method.Parameters {
			if param.Name == "" {
				return fmt.Errorf("method %q parameter name cannot be empty", method.Name)
			}
			if err := validateTypeDesc(param.Type); err != nil {
				return fmt.Errorf("method %q parameter %q: %w", method.Name, param.Name, err)
			}
		}
		for i, ret := range method.Returns {
			if err := validateTypeDesc(ret); err != nil {
				return fmt.Errorf("method %q return %d: %w", method.Name, i, err)
			}
		}
	}
	return nil
}

func validateTypeDesc(desc schema.TypeDesc) error {
	if desc.Kind == schema.TypeKindInvalid {
		return fmt.Errorf("type descriptor kind cannot be invalid")
	}
	switch desc.Kind {
	case schema.TypeKindArray:
		if desc.Element == nil {
			return fmt.Errorf("array type descriptor must declare element")
		}
		return validateTypeDesc(*desc.Element)
	case schema.TypeKindMap:
		if desc.Key == nil {
			return fmt.Errorf("map type descriptor must declare key")
		}
		if desc.Value == nil {
			return fmt.Errorf("map type descriptor must declare value")
		}
		if err := validateTypeDesc(*desc.Key); err != nil {
			return err
		}
		return validateTypeDesc(*desc.Value)
	case schema.TypeKindStruct, schema.TypeKindClass:
		if desc.Name == "" && desc.ClassName == "" {
			return fmt.Errorf("object type descriptor must declare name or className")
		}
	}
	return nil
}

// registerBoundStructInVM mirrors a host-bound ObjectDesc into the underlying
// VM struct registry so that goStructToVMStruct can encode Go values of the
// matching type when they are passed as Call arguments. Without this bridge,
// only structs declared inside script source are reachable from the VM, and
// host-bound structs would fail with unsupported_vm_argument_type.
//
// Field types are recorded as TypeInvalid (matching compiler.registerStructs);
// the VM accesses fields by name and slot, not by type. Field offset i+1
// matches the layout used by NewStructInstance and getStructFieldByName.
func (rt *Runtime) registerBoundStructInVM(name string, desc schema.ObjectDesc) {
	if rt == nil || rt.evaluator == nil {
		return
	}
	v := rt.evaluator.VM()
	if v == nil {
		return
	}
	fields := make([]vm.FieldDef, len(desc.Fields))
	for i, f := range desc.Fields {
		fields[i] = vm.NewFieldDef(f.Name, vm.TypeInvalid, i+1)
	}
	v.StructReg().RegisterStruct(name, fields)
}

func (rt *Runtime) ensureHostInterfaceClass(namespace, name string, desc schema.InterfaceDesc) error {
	if rt == nil || rt.evaluator == nil {
		return fmt.Errorf("runtime is not initialized")
	}
	key := namespace + "/" + name
	if _, ok := rt.hostInterfaceClasses[key]; ok {
		return nil
	}
	v := rt.evaluator.VM()
	if v == nil {
		return fmt.Errorf("runtime VM is not initialized")
	}
	if v.IfaceReg().GetInterfaceByName(name) == nil {
		methods := make([]vm.InterfaceMethodSig, len(desc.Methods))
		for i, method := range desc.Methods {
			methods[i] = vm.NewInterfaceMethodSig(method.Name, len(method.Parameters))
		}
		v.IfaceReg().RegisterInterface(vm.NewInterfaceDef(name, methods))
	}
	proxyClassName := hostInterfaceProxyClassName(namespace, name)
	class := v.ClassReg().GetClassByName(proxyClassName)
	if class == nil {
		class = vm.NewClass(0, proxyClassName, nil)
		for _, method := range desc.Methods {
			methodName := method.Name
			class.AddMethod(methodName, func(v *vm.VM, receiver vm.Handle, args []vm.Value) vm.Value {
				return rt.invokeHostInterfaceMethod(receiver, methodName, args)
			})
		}
		classID := v.ClassReg().RegisterClass(class)
		class = v.ClassReg().GetClassByName(proxyClassName)
		if class == nil {
			return fmt.Errorf("proxy class %q registration failed", proxyClassName)
		}
		v.IfaceReg().RegisterImplementation(proxyClassName, name)
		rt.hostInterfaceClasses[key] = hostInterfaceClass{
			Namespace:      namespace,
			Name:           name,
			InterfaceDesc:  schema.CloneInterfaceDesc(desc),
			ProxyClassName: proxyClassName,
			ClassID:        classID,
		}
		return nil
	}
	v.IfaceReg().RegisterImplementation(proxyClassName, name)
	rt.hostInterfaceClasses[key] = hostInterfaceClass{
		Namespace:      namespace,
		Name:           name,
		InterfaceDesc:  schema.CloneInterfaceDesc(desc),
		ProxyClassName: proxyClassName,
		ClassID:        class.ID(),
	}
	return nil
}

func (rt *Runtime) registerHostInterfaceObject(namespace, name string, desc schema.InterfaceDesc, target any) (hostInterfaceObject, error) {
	if err := rt.ensureHostInterfaceClass(namespace, name, desc); err != nil {
		return hostInterfaceObject{}, err
	}
	classInfo := rt.hostInterfaceClasses[namespace+"/"+name]
	v := rt.evaluator.VM()
	if v == nil {
		return hostInterfaceObject{}, fmt.Errorf("runtime VM is not initialized")
	}
	key := hostInterfaceBindingKey{Namespace: namespace, Name: name}
	if id, ok := rt.hostInterfaceBindings[key]; ok {
		if existing, exists := rt.hostInterfaceObjects[id]; exists {
			if !existing.Pending && existing.Handle != vm.InvalidHandle && reflect.DeepEqual(existing.Target, target) {
				return existing, nil
			}
			// Target differs — create a fresh object instead of overwriting.
			// The old object remains valid via its existing handle.
		}
	}
	if existing, ok := rt.lookupHostInterfaceObjectByTarget(namespace, name, target); ok && !existing.Pending {
		return existing, nil
	}
	handle := v.CreateObject(classInfo.ClassID)
	if handle == vm.InvalidHandle {
		return hostInterfaceObject{}, fmt.Errorf("proxy object allocation failed for %s.%s", namespace, name)
	}
	id := atomic.AddUint64(&rt.nextHostInterfaceID, 1)
	obj := hostInterfaceObject{
		ID:             id,
		Namespace:      namespace,
		Name:           name,
		InterfaceDesc:  schema.CloneInterfaceDesc(desc),
		Target:         target,
		ProxyClassName: classInfo.ProxyClassName,
		Handle:         handle,
		Pending:        false,
	}
	rt.hostInterfaceObjects[id] = obj
	rt.hostInterfaceHandles[handle] = id
	rt.hostInterfaceBindings[hostInterfaceBindingKey{Namespace: namespace, Name: name}] = id
	return obj, nil
}

// RegisterHostInterfaceInstance registers an additional runtime instance of a
// previously-bound host interface. The interface class must already exist
// (created by BindInterfaceObject); this call only allocates a new proxy
// object handle for the given target.
func (rt *Runtime) RegisterHostInterfaceInstance(namespace, name string, target any) error {
	if target == nil {
		return fmt.Errorf("interface target cannot be nil")
	}
	classInfo, ok := rt.hostInterfaceClasses[namespace+"/"+name]
	if !ok {
		return fmt.Errorf("interface %s.%s has not been registered", namespace, name)
	}
	_, err := rt.registerHostInterfaceObject(namespace, name, classInfo.InterfaceDesc, target)
	return err
}

func (rt *Runtime) lookupHostInterfaceObjectByTarget(namespace, name string, target any) (hostInterfaceObject, bool) {
	for _, obj := range rt.hostInterfaceObjects {
		if obj.Namespace == namespace && obj.Name == name && reflect.DeepEqual(obj.Target, target) {
			return obj, true
		}
	}
	return hostInterfaceObject{}, false
}

func (rt *Runtime) hostInterfaceObjectForHandle(handle vm.Handle) (hostInterfaceObject, bool) {
	if rt == nil {
		return hostInterfaceObject{}, false
	}
	id, ok := rt.hostInterfaceHandles[handle]
	if !ok {
		return hostInterfaceObject{}, false
	}
	obj, ok := rt.hostInterfaceObjects[id]
	return obj, ok
}

func (rt *Runtime) lookupBoundHostInterfaceObject(namespace, name string) (hostInterfaceObject, bool) {
	if rt == nil {
		return hostInterfaceObject{}, false
	}
	id, ok := rt.hostInterfaceBindings[hostInterfaceBindingKey{Namespace: namespace, Name: name}]
	if !ok {
		return hostInterfaceObject{}, false
	}
	obj, ok := rt.hostInterfaceObjects[id]
	return obj, ok
}

// markHostInterfaceRoots is the GC root provider that keeps host interface
// proxy objects live. Proxy objects are allocated in the VM heap via
// CreateObject but are only referenced from the Runtime's handle table, which
// the VM GC cannot see — so without this provider sustained allocation
// reclaims a proxy, the handle is reused, and method dispatch panics with
// "object class not found".
func (rt *Runtime) markHostInterfaceRoots(visit func(vm.Value)) {
	if rt == nil {
		return
	}
	for h := range rt.hostInterfaceHandles {
		visit(vm.EncodeHandle(h))
	}
}

// attachHostIfaceRoots subscribes the host-interface root provider to the
// evaluator's VM lifecycle: it registers on the current VM immediately and
// re-registers on every VM swap (each CompileLoweredProgram builds a fresh
// VM, discarding providers attached to the old one). Wiring this at every
// evaluator construction path (NewRuntimeWith, Clone, Reset) makes
// "host-bound objects stay rooted for the RT lifetime" a construction-time
// guarantee instead of call-site discipline at bind/finalize time.
func (rt *Runtime) attachHostIfaceRoots(eval *bytecode.VMEvaluator) {
	eval.OnVMReplaced(func(v *vm.VM) {
		rt.hostIfaceRootProviderID = v.AddRootProvider(rt.markHostInterfaceRoots)
		rt.hostIfaceRootProviderVM = v
	})
}

func (rt *Runtime) lookupHostInterfaceObjectForValue(value any) (hostInterfaceObject, bool) {
	for _, obj := range rt.hostInterfaceObjects {
		if obj.Pending || obj.Handle == vm.InvalidHandle {
			continue
		}
		if reflect.DeepEqual(obj.Target, value) {
			return obj, true
		}
	}
	return hostInterfaceObject{}, false
}

func (rt *Runtime) hostInterfaceHandleForValue(value any) (vm.Handle, bool) {
	obj, ok := rt.lookupHostInterfaceObjectForValue(value)
	if !ok || obj.Pending || obj.Handle == vm.InvalidHandle {
		return vm.InvalidHandle, false
	}
	return obj.Handle, true
}

func (rt *Runtime) HostInterfaceHandleForValue(value any) (vm.Handle, bool) {
	return rt.hostInterfaceHandleForValue(value)
}

func (rt *Runtime) HostInterfaceObjectForHandle(handle vm.Handle) (any, bool) {
	obj, ok := rt.hostInterfaceObjectForHandle(handle)
	if !ok {
		return nil, false
	}
	return obj.Target, true
}

func hostInterfaceProxyClassName(namespace, name string) string {
	namespace = strings.ReplaceAll(namespace, "/", "__")
	namespace = strings.ReplaceAll(namespace, "\\", "__")
	return "__host_iface__" + namespace + "__" + name
}

func (rt *Runtime) invokeHostInterfaceMethod(receiver vm.Handle, methodName string, args []vm.Value) vm.Value {
	obj, ok := rt.hostInterfaceObjectForHandle(receiver)
	if !ok {
		panic(fmt.Sprintf("host interface receiver not found for handle %v", receiver))
	}
	method, ok := lookupHostMethod(obj.Target, methodName)
	if !ok {
		panic(fmt.Sprintf("host interface method %q not found on %T", methodName, obj.Target))
	}
	goArgs := make([]any, len(args))
	for i, arg := range args {
		goArgs[i] = bytecode.HostVMValueToAny(rt, rt.evaluator.VM(), arg)
	}
	results, err := binding.InvokeGoFunctionForHostProxy(method, goArgs)
	if err != nil {
		panic(err)
	}
	if len(results) == 0 {
		return vm.EncodeHandle(vm.InvalidHandle)
	}
	out, err := bytecode.HostAnyToVMValue(rt, rt.evaluator.VM(), results[0], "vm/evaluator/host-interface-method/"+methodName)
	if err != nil {
		panic(err)
	}
	return out
}

func exportedMethodName(name string) string {
	if name == "" {
		return ""
	}
	parts := strings.Split(name, "_")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, "")
}

// builderForNamespace returns the pending builder for namespace, creating one
// on first access. Callers must check bindingsCommitted before invoking.
func (rt *Runtime) builderForNamespace(namespace string) *binding.CapabilityBuilder {
	if b, ok := rt.pendingNamespaces[namespace]; ok {
		return b
	}
	b := binding.NewCapability(namespace, "host")
	rt.pendingNamespaces[namespace] = b
	return b
}

// refreshNamespaceSnapshot rebuilds the BoundFunctions / BoundValues snapshot
// for namespace from the current builder state. Call after each successful
// Add* on the pending builder so introspection accessors remain in sync.
//
// Order matters: Build is called first so a builder error short-circuits
// without touching the existing snapshot. Old entries for this namespace are
// then evicted, and the fresh descriptors take their place.
func (rt *Runtime) refreshNamespaceSnapshot(namespace string, builder *binding.CapabilityBuilder) error {
	cap, err := builder.Build()
	if err != nil {
		return err
	}
	prefix := namespace + "/"
	for k := range rt.boundFuncs {
		if strings.HasPrefix(k, prefix) {
			delete(rt.boundFuncs, k)
		}
	}
	for k := range rt.boundValues {
		if strings.HasPrefix(k, prefix) {
			delete(rt.boundValues, k)
		}
	}
	for _, desc := range cap.Desc.Callables {
		info := callableInfoFromDesc(desc)
		rt.boundFuncs[namespace+"/"+desc.Name] = BoundFunction{
			Namespace: namespace,
			Name:      desc.Name,
			Callable:  namespace + "." + desc.Name,
			Info:      info,
		}
	}
	for _, desc := range cap.Desc.Values {
		rt.boundValues[namespace+"/"+desc.Name] = BoundValue{
			Namespace: namespace,
			Name:      desc.Name,
			Desc:      desc,
		}
	}
	for k := range rt.boundObjects {
		if strings.HasPrefix(k, prefix) {
			delete(rt.boundObjects, k)
		}
	}
	for _, obj := range cap.Desc.Objects {
		rt.boundObjects[namespace+"/"+obj.Name] = BoundStruct{
			Namespace:  namespace,
			Name:       obj.Name,
			ObjectDesc: schema.CloneObjectDesc(obj),
		}
	}
	for k := range rt.boundInterfaces {
		if strings.HasPrefix(k, prefix) {
			delete(rt.boundInterfaces, k)
		}
	}
	for _, iface := range cap.Desc.Interfaces {
		rt.boundInterfaces[namespace+"/"+iface.Name] = BoundInterface{
			Namespace:     namespace,
			Name:          iface.Name,
			InterfaceDesc: schema.CloneInterfaceDesc(iface),
		}
	}
	for k := range rt.boundTypeAliases {
		if strings.HasPrefix(k, prefix) {
			delete(rt.boundTypeAliases, k)
		}
	}
	for name, td := range cap.Desc.TypeAliases {
		rt.boundTypeAliases[namespace+"/"+name] = BoundTypeAlias{
			Namespace: namespace,
			Name:      name,
			TypeDesc:  schema.CloneTypeDesc(td),
		}
	}
	return nil
}

// commitPendingBindings registers all pending namespace builders into the
// underlying ScriptBinding and exposes their callables. Namespaces are
// registered in lexical order so the same binding setup commits in the same
// order across runs (important for reproducible diagnostic output on
// commit-time failures). After commit the binding surface is sealed:
// subsequent Bind* calls return an error.
func (rt *Runtime) commitPendingBindings() error {
	if rt.bindingsCommitted {
		return nil
	}
	rt.bindingsCommitted = true
	namespaces := make([]string, 0, len(rt.pendingNamespaces))
	for ns := range rt.pendingNamespaces {
		namespaces = append(namespaces, ns)
	}
	sort.Strings(namespaces)
	for _, namespace := range namespaces {
		builder := rt.pendingNamespaces[namespace]
		cap, err := builder.Build()
		if err != nil {
			return err
		}
		if err := rt.binding.RegisterCapability(cap); err != nil {
			return err
		}
		if len(cap.Desc.Callables) > 0 {
			if err := rt.binding.ExposeCapabilityCallables(namespace); err != nil {
				return err
			}
		}
	}
	return nil
}

// finalizePendingHostInterfaces registers proxy classes and allocates object
// handles for all pending host interface bindings on the current execution VM.
// Must be called after CompileLoweredProgram so that rt.evaluator.VM() is the
// execution VM, not a stale pre-compile instance.
func (rt *Runtime) finalizePendingHostInterfaces() error {
	for _, obj := range rt.hostInterfaceObjects {
		if !obj.Pending {
			continue
		}
		if _, err := rt.registerHostInterfaceObject(obj.Namespace, obj.Name, obj.InterfaceDesc, obj.Target); err != nil {
			return err
		}
	}
	return nil
}

func (rt *Runtime) boundInterfacesInNamespace(namespace string) []BoundInterface {
	if rt == nil || len(rt.boundInterfaces) == 0 {
		return nil
	}
	result := make([]BoundInterface, 0)
	for _, info := range rt.boundInterfaces {
		if info.Namespace == namespace {
			result = append(result, info)
		}
	}
	return result
}

// BoundFunctions returns host functions bound through the Runtime facade.
func (rt *Runtime) BoundFunctions() []BoundFunction {
	if rt == nil || len(rt.boundFuncs) == 0 {
		return nil
	}
	result := make([]BoundFunction, 0, len(rt.boundFuncs))
	for _, info := range rt.boundFuncs {
		result = append(result, info)
	}
	return result
}

// BoundValues returns host values bound through the Runtime facade.
func (rt *Runtime) BoundValues() []BoundValue {
	if rt == nil || len(rt.boundValues) == 0 {
		return nil
	}
	result := make([]BoundValue, 0, len(rt.boundValues))
	for _, info := range rt.boundValues {
		result = append(result, info)
	}
	return result
}

// BoundObjects returns host struct/object types bound through the Runtime
// facade via BindStructDesc or BindStruct. These are compile-time types visible
// to script code through import, not runtime values.
func (rt *Runtime) BoundObjects() []BoundStruct {
	if rt == nil || len(rt.boundObjects) == 0 {
		return nil
	}
	result := make([]BoundStruct, 0, len(rt.boundObjects))
	for _, info := range rt.boundObjects {
		result = append(result, info)
	}
	return result
}

// BoundInterfaces returns host interface types bound through the Runtime facade
// via BindInterfaceDesc. These are compile-time interface types visible to
// script code through import, not runtime values.
func (rt *Runtime) BoundInterfaces() []BoundInterface {
	if rt == nil || len(rt.boundInterfaces) == 0 {
		return nil
	}
	result := make([]BoundInterface, 0, len(rt.boundInterfaces))
	for _, info := range rt.boundInterfaces {
		result = append(result, info)
	}
	return result
}

// BoundTypeAliases returns host type aliases bound through the Runtime facade
// via BindTypeAlias. These are compile-time types visible to script code
// through import, not runtime values.
func (rt *Runtime) BoundTypeAliases() []BoundTypeAlias {
	if rt == nil || len(rt.boundTypeAliases) == 0 {
		return nil
	}
	result := make([]BoundTypeAlias, 0, len(rt.boundTypeAliases))
	for _, info := range rt.boundTypeAliases {
		result = append(result, info)
	}
	return result
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

// Close releases all resources held by the Runtime. After Close, the Runtime
// is no longer usable; any method call returns an error. Close is idempotent.
func (rt *Runtime) Close() error {
	if rt == nil {
		return nil
	}
	if rt.evaluator != nil {
		rt.evaluator.CancelAllStreams()
	}
	rt.closed = true
	rt.binding = nil
	rt.frontend = nil
	rt.evaluator = nil
	rt.moduleResolver = nil
	rt.rootModule = ""
	rt.boundFuncs = nil
	rt.boundValues = nil
	rt.boundObjects = nil
	rt.boundInterfaces = nil
	rt.boundTypeAliases = nil
	rt.pendingNamespaces = nil
	rt.hostInterfaceClasses = nil
	rt.hostInterfaceObjects = nil
	rt.hostInterfaceHandles = nil
	rt.hostInterfaceBindings = nil
	rt.bindingsCommitted = false
	return nil
}

// Reload is a convenience that performs Reset followed by LoadSource in one
// call. It discards the current module, compiled code, and stream sessions,
// then loads the given source as the new root module. Existing bindings are
// preserved. This is the canonical way to swap out script source without
// reconstructing the Runtime.
func (rt *Runtime) Reload(name, source string) error {
	if err := rt.Reset(); err != nil {
		return err
	}
	return rt.LoadSource(name, source)
}

// Clone returns a new Runtime with the same bindings and module resolver as
// the original, but with a fresh VM/Frontend/Evaluator. The cloned Runtime
// has no loaded module and can independently LoadSource. This is the canonical
// way to create concurrent Runtime instances that share the same host
// capability surface.
func (rt *Runtime) Clone() (*Runtime, error) {
	if rt == nil || rt.frontend == nil {
		return nil, fmt.Errorf("runtime is not initialized")
	}
	if rt.closed {
		return nil, fmt.Errorf("runtime is closed")
	}

	sb := binding.NewScriptBinding()
	fe, err := frontend.New(sb)
	if err != nil {
		return nil, err
	}
	eval := bytecode.NewVMEvaluatorWith(rt.vmHeapBytes, rt.vmHeapSlots)
	fe.SetVMCompileHook(eval)
	if rt.moduleResolver != nil {
		fe.SetModuleResolver(rt.moduleResolver)
	}

	cloned := &Runtime{
		binding:               sb,
		frontend:              fe,
		evaluator:             eval,
		boundFuncs:            make(map[string]BoundFunction),
		boundValues:           make(map[string]BoundValue),
		boundObjects:          make(map[string]BoundStruct),
		boundInterfaces:       make(map[string]BoundInterface),
		boundTypeAliases:      make(map[string]BoundTypeAlias),
		pendingNamespaces:     make(map[string]*binding.CapabilityBuilder),
		moduleResolver:        rt.moduleResolver,
		hostInterfaceClasses:  make(map[string]hostInterfaceClass),
		hostInterfaceObjects:  make(map[uint64]hostInterfaceObject),
		hostInterfaceHandles:  make(map[vm.Handle]uint64),
		hostInterfaceBindings: make(map[hostInterfaceBindingKey]uint64),
		vmHeapBytes:           rt.vmHeapBytes,
		vmHeapSlots:           rt.vmHeapSlots,
	}

	// Copy pending namespace builders so the clone gets the same bindings.
	for ns, builder := range rt.pendingNamespaces {
		cloned.pendingNamespaces[ns] = builder.Clone()
	}

	// Copy bound snapshots so introspection accessors are in sync.
	for k, v := range rt.boundFuncs {
		cloned.boundFuncs[k] = v
	}
	for k, v := range rt.boundValues {
		cloned.boundValues[k] = v
	}
	for k, v := range rt.boundObjects {
		cloned.boundObjects[k] = v
	}
	for k, v := range rt.boundInterfaces {
		cloned.boundInterfaces[k] = v
	}
	for k, v := range rt.boundTypeAliases {
		cloned.boundTypeAliases[k] = v
	}
	for id, obj := range rt.hostInterfaceObjects {
		cloned.hostInterfaceObjects[id] = hostInterfaceObject{
			ID:             obj.ID,
			Namespace:      obj.Namespace,
			Name:           obj.Name,
			InterfaceDesc:  schema.CloneInterfaceDesc(obj.InterfaceDesc),
			Target:         obj.Target,
			ProxyClassName: obj.ProxyClassName,
			Handle:         vm.InvalidHandle,
			Pending:        true,
		}
	}
	for k, v := range rt.hostInterfaceBindings {
		cloned.hostInterfaceBindings[k] = v
	}

	// Re-commit bindings into the fresh backing.
	if err := cloned.commitPendingBindings(); err != nil {
		return nil, fmt.Errorf("clone: re-commit bindings failed: %w", err)
	}

	// Wire up native binding after capabilities are registered.
	eval.SetNativeBinding(sb)
	eval.SetHostInterfaceResolver(cloned)
	cloned.attachHostIfaceRoots(eval)

	return cloned, nil
}

// Reset returns the Runtime to its initial post-construction state.
// All loaded modules, compiled code, and stream sessions are discarded.
// Existing bindings (BindFunc/BindValue/BindStruct/etc.) are preserved and
// re-committed to a fresh underlying VM. The ModuleResolver is preserved.
// After Reset, LoadSource can be called again.
func (rt *Runtime) Reset() error {
	if rt == nil {
		return fmt.Errorf("runtime is not initialized")
	}
	if rt.closed {
		return fmt.Errorf("runtime is closed")
	}
	if rt.evaluator != nil {
		rt.evaluator.CancelAllStreams()
	}

	// Rebuild the backing stack with fresh state.
	sb := binding.NewScriptBinding()
	fe, err := frontend.New(sb)
	if err != nil {
		return err
	}
	eval := bytecode.NewVMEvaluatorWith(rt.vmHeapBytes, rt.vmHeapSlots)
	fe.SetVMCompileHook(eval)
	if rt.moduleResolver != nil {
		fe.SetModuleResolver(rt.moduleResolver)
	}

	rt.binding = sb
	rt.frontend = fe
	rt.evaluator = eval
	rt.rootModule = ""
	rt.bindingsCommitted = false

	// Reset host interface proxy state so that proxy classes and handles
	// are re-registered against the fresh VM when a module is next loaded.
	for id, obj := range rt.hostInterfaceObjects {
		obj.Handle = vm.InvalidHandle
		obj.Pending = true
		rt.hostInterfaceObjects[id] = obj
	}
	rt.hostInterfaceHandles = make(map[vm.Handle]uint64)
	rt.hostInterfaceClasses = make(map[string]hostInterfaceClass)

	// Re-commit existing bindings into the fresh backing.
	if err := rt.commitPendingBindings(); err != nil {
		return fmt.Errorf("reset: re-commit bindings failed: %w", err)
	}

	// Re-mirror host-bound structs into the fresh VM struct registry so that
	// Call arguments matching those types continue to encode after reset.
	for _, bs := range rt.boundObjects {
		rt.registerBoundStructInVM(bs.Name, bs.ObjectDesc)
	}

	// Wire up native binding after capabilities are registered so the
	// evaluator sees the full capability surface.
	eval.SetNativeBinding(sb)
	eval.SetHostInterfaceResolver(rt)
	rt.attachHostIfaceRoots(eval)
	return nil
}

// IsCompileError reports whether err is a Runtime compile/load/link error.
func IsCompileError(err error) bool {
	_, ok := err.(*CompileError)
	return ok
}

// IsRuntimeError reports whether err is a Runtime invocation/runtime error.
func IsRuntimeError(err error) bool {
	_, ok := err.(*RuntimeError)
	return ok
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

func wrapCompileError(err error) error {
	if err == nil {
		return nil
	}
	return &CompileError{Err: err, Diagnostic: diagnostics.FromError(err, diagnostics.Descriptor{})}
}

func wrapRuntimeError(err error) error {
	if err == nil {
		return nil
	}
	return &RuntimeError{Err: err, Diagnostic: diagnostics.FromError(err, diagnostics.Descriptor{})}
}

func runtimeErrorFromOutcome(outcome binding.InvocationOutcome) *RuntimeError {
	if outcome.Result.Error == nil {
		return &RuntimeError{Err: fmt.Errorf("runtime call failed")}
	}
	diag := diagnostics.Descriptor{
		Code:     outcome.Result.Error.DiagnosticCode,
		Category: diagnostics.Category(outcome.Result.Error.Category),
		Message:  outcome.Result.Error.Message,
		Span:     outcome.Result.Error.Span,
		Path:     outcome.Result.Error.Path,
		Identity: outcome.Result.Error.Identity,
		Stack:    append([]diagnostics.Frame(nil), outcome.Result.Error.Stack...),
		Cause:    diagnostics.ClonePtr(outcome.Result.Error.Cause),
	}
	diag = diagnostics.Normalize(diag)
	msg := diag.Message
	if msg == "" {
		msg = "runtime call failed"
	}
	return &RuntimeError{Err: errors.New(msg), Diagnostic: diag}
}

func decodeInto(dst any, value any) error {
	switch p := dst.(type) {
	case *any:
		*p = value
		return nil
	case *string:
		s, ok := value.(string)
		if !ok {
			return decodeTypeMismatch("string", value)
		}
		*p = s
		return nil
	case *bool:
		b, ok := value.(bool)
		if !ok {
			return decodeTypeMismatch("bool", value)
		}
		*p = b
		return nil
	case *int:
		n, err := decodeInt64(value)
		if err != nil {
			return err
		}
		*p = int(n)
		return nil
	case *int8:
		n, err := decodeInt64(value)
		if err != nil {
			return err
		}
		*p = int8(n)
		return nil
	case *int16:
		n, err := decodeInt64(value)
		if err != nil {
			return err
		}
		*p = int16(n)
		return nil
	case *int32:
		n, err := decodeInt64(value)
		if err != nil {
			return err
		}
		*p = int32(n)
		return nil
	case *int64:
		n, err := decodeInt64(value)
		if err != nil {
			return err
		}
		*p = n
		return nil
	case *uint:
		n, err := decodeUint64(value)
		if err != nil {
			return err
		}
		*p = uint(n)
		return nil
	case *uint8:
		n, err := decodeUint64(value)
		if err != nil {
			return err
		}
		*p = uint8(n)
		return nil
	case *uint16:
		n, err := decodeUint64(value)
		if err != nil {
			return err
		}
		*p = uint16(n)
		return nil
	case *uint32:
		n, err := decodeUint64(value)
		if err != nil {
			return err
		}
		*p = uint32(n)
		return nil
	case *uint64:
		n, err := decodeUint64(value)
		if err != nil {
			return err
		}
		*p = n
		return nil
	case *float32:
		f, err := decodeFloat64(value)
		if err != nil {
			return err
		}
		*p = float32(f)
		return nil
	case *float64:
		f, err := decodeFloat64(value)
		if err != nil {
			return err
		}
		*p = f
		return nil
	case *[]any:
		s, ok := value.([]any)
		if !ok {
			return decodeTypeMismatch("[]any", value)
		}
		*p = s
		return nil
	case *map[string]any:
		m, ok := value.(map[string]any)
		if !ok {
			return decodeTypeMismatch("map[string]any", value)
		}
		*p = m
		return nil
	}
	return fmt.Errorf("DecodeInto does not support target type %T", dst)
}

func decodeInt64(value any) (int64, error) {
	switch v := value.(type) {
	case int:
		return int64(v), nil
	case int8:
		return int64(v), nil
	case int16:
		return int64(v), nil
	case int32:
		return int64(v), nil
	case int64:
		return v, nil
	case uint:
		return int64(v), nil
	case uint8:
		return int64(v), nil
	case uint16:
		return int64(v), nil
	case uint32:
		return int64(v), nil
	case uint64:
		return int64(v), nil
	case float32:
		return int64(v), nil
	case float64:
		return int64(v), nil
	}
	return 0, decodeTypeMismatch("integer", value)
}

func decodeUint64(value any) (uint64, error) {
	switch v := value.(type) {
	case int:
		return uint64(v), nil
	case int8:
		return uint64(v), nil
	case int16:
		return uint64(v), nil
	case int32:
		return uint64(v), nil
	case int64:
		return uint64(v), nil
	case uint:
		return uint64(v), nil
	case uint8:
		return uint64(v), nil
	case uint16:
		return uint64(v), nil
	case uint32:
		return uint64(v), nil
	case uint64:
		return v, nil
	case float32:
		return uint64(v), nil
	case float64:
		return uint64(v), nil
	}
	return 0, decodeTypeMismatch("unsigned integer", value)
}

func decodeFloat64(value any) (float64, error) {
	switch v := value.(type) {
	case float32:
		return float64(v), nil
	case float64:
		return v, nil
	case int:
		return float64(v), nil
	case int8:
		return float64(v), nil
	case int16:
		return float64(v), nil
	case int32:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case uint:
		return float64(v), nil
	case uint8:
		return float64(v), nil
	case uint16:
		return float64(v), nil
	case uint32:
		return float64(v), nil
	case uint64:
		return float64(v), nil
	}
	return 0, decodeTypeMismatch("number", value)
}

func decodeTypeMismatch(target string, value any) error {
	if value == nil {
		return fmt.Errorf("DecodeInto: cannot decode nil into %s target", target)
	}
	return fmt.Errorf("DecodeInto: cannot decode %T into %s target", value, target)
}
