package script

import (
	"fmt"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/internal/script/bytecode"
	"github.com/qomos-w/spore/internal/script/frontend"
)

// This file holds the Runtime lifecycle: loading, reloading, cloning, resetting
// and closing, plus the module resolver surface.

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
	rt.hostIface.clear()
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
		binding:           sb,
		frontend:          fe,
		evaluator:         eval,
		boundFuncs:        make(map[string]BoundFunction),
		boundValues:       make(map[string]BoundValue),
		boundObjects:      make(map[string]BoundStruct),
		boundInterfaces:   make(map[string]BoundInterface),
		boundTypeAliases:  make(map[string]BoundTypeAlias),
		pendingNamespaces: make(map[string]*binding.CapabilityBuilder),
		moduleResolver:    rt.moduleResolver,
		hostIface:         rt.hostIface.clone(),
		vmHeapBytes:       rt.vmHeapBytes,
		vmHeapSlots:       rt.vmHeapSlots,
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

	// The ledger's durable index (objects + bindings) is preserved; only the
	// VM-scoped projection is stale. It is invalidated in one step when the
	// fresh evaluator's VM is bound below — no per-object rewrite is needed.

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
