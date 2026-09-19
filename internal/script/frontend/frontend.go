package frontend

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/diagnostics"
	"github.com/qomos-w/spore/schema"
)

// Frontend is an internal script frontend facade over the existing Script Binding
// plane. It snapshots canonical callable descriptors and dispatches invocation
// through ScriptBinding without introducing a parallel descriptor or result model.
type Frontend struct {
	binding           *binding.ScriptBinding
	vmCompileHook     VMLoweringBackend
	moduleResolver    ModuleResolver
	callables         map[string]callableEntry
	objects           map[string]objectEntry
	interfaces        []schema.InterfaceDesc
	enums             []schema.EnumDesc
	exports           []schema.CallableDesc
	imports           []ImportMetadata
	importedSymbols   []ImportedSymbolMetadata
	importedTypes     []ImportedTypeMetadata
	linkedModules     []LinkedModuleMetadata
	moduleCache       map[string]compiledModule
	moduleGraph       *ModuleGraph
	moduleDiagnostics []ModuleDiagnostic
	packageName       string
}

type callableEntry struct {
	desc     schema.CallableDesc
	exported bool
}

type objectEntry struct {
	desc schema.ObjectDesc
}

// New constructs an internal frontend from an existing ScriptBinding and
// snapshots its registered callable descriptors.
func New(sb *binding.ScriptBinding) (*Frontend, error) {
	if sb == nil {
		return nil, fmt.Errorf("script binding is required")
	}
	f := &Frontend{binding: sb}

	f.Refresh()
	return f, nil
}

// Refresh rebuilds the internal callable snapshot from the backing ScriptBinding.
func (f *Frontend) Refresh() {
	if f == nil {
		return
	}
	f.callables = make(map[string]callableEntry)
	f.objects = make(map[string]objectEntry)
	f.interfaces = nil
	if f.binding == nil || f.binding.Callables == nil {
		return
	}
	for _, desc := range f.binding.Callables.List() {
		entry := callableEntry{desc: schema.CloneCallableDesc(desc)}
		for _, exported := range f.exports {
			if exported.Name == desc.Name {
				entry.exported = true
				break
			}
		}
		f.callables[desc.Name] = entry
	}
}

// LoadSource parses a minimal internal script frontend source and registers the
// resulting canonical descriptors into the frontend and backing ScriptBinding.
func (f *Frontend) LoadSource(source string) error {
	if f == nil || f.binding == nil || f.binding.Callables == nil || f.binding.Executors == nil {
		return fmt.Errorf("script binding is required")
	}
	prog, err := parseModule(source)
	if err != nil {
		f.moduleDiagnostics = nil
		return err
	}
	f.moduleDiagnostics = nil
	compiled, err := f.compiledDeclarationsForProgram(prog)
	if err != nil {
		f.captureModuleDiagnostic(err)
		return err
	}

	// If a VM compile hook is registered, lower the parsed program through the
	// frontend-owned VM seam before registering descriptors and adapters.
	if f.vmCompileHook != nil {
		if err := CompileProgramForVM(compiled, prog, f.vmCompileHook); err != nil {
			return err
		}
	}

	if err := f.registerCompiledDeclarations(compiled); err != nil {
		return err
	}
	return nil
}

// SetVMCompileHook sets the backend used by the frontend-owned VM lowering seam.
func (f *Frontend) SetVMCompileHook(hook VMLoweringBackend) {
	f.vmCompileHook = hook
}

// SetModuleResolver sets the module resolver used for first-phase import linking.
func (f *Frontend) SetModuleResolver(resolver ModuleResolver) {
	if f == nil {
		return
	}
	f.moduleResolver = resolver
}

// ClearModuleCache discards the frontend-level parsed module cache.
// Call this when the backing ModuleResolver may return different source for the same path.
func (f *Frontend) ClearModuleCache() {
	if f == nil {
		return
	}
	f.moduleCache = nil
}

// ModuleGraph returns the dependency graph of the last successfully loaded source,
// or nil if no source has been loaded.
func (f *Frontend) ModuleGraph() *ModuleGraph {
	if f == nil {
		return nil
	}
	return f.moduleGraph
}

func (f *Frontend) LastModuleDiagnostics() []ModuleDiagnostic {
	if f == nil || len(f.moduleDiagnostics) == 0 {
		return nil
	}
	result := make([]ModuleDiagnostic, len(f.moduleDiagnostics))
	copy(result, f.moduleDiagnostics)
	for i := range result {
		result[i].AvailableExports = append([]string(nil), result[i].AvailableExports...)
		result[i].CyclePath = append([]string(nil), result[i].CyclePath...)
	}
	return result
}

func (f *Frontend) captureModuleDiagnostic(err error) {
	if f == nil || err == nil {
		return
	}
	var detail *moduleDiagnosticError
	if errors.As(err, &detail) {
		md := detail.diagnostic
		d := diagnostics.FromError(detail.err, diagnostics.Descriptor{})
		if d.Category != diagnostics.CategoryLoad || d.Code == "" {
			return
		}
		md.Code = d.Code
		md.Category = string(d.Category)
		md.Path = d.Path
		md.Message = d.Message
		md.Hint = d.Hint
		md.Expected = d.Expected
		md.Actual = d.Actual
		if d.Code == "cyclic_import" && f.moduleGraph != nil {
			md.CyclePath = f.moduleGraph.CyclePath()
		}
		f.moduleDiagnostics = []ModuleDiagnostic{md}
		return
	}
	d := diagnostics.FromError(err, diagnostics.Descriptor{})
	if d.Category != diagnostics.CategoryLoad || d.Code == "" {
		return
	}
	md := ModuleDiagnostic{Code: d.Code, Category: string(d.Category), Path: d.Path, Message: d.Message, Hint: d.Hint, Expected: d.Expected, Actual: d.Actual}
	if d.Code == "cyclic_import" && f.moduleGraph != nil {
		md.CyclePath = f.moduleGraph.CyclePath()
	}
	f.moduleDiagnostics = []ModuleDiagnostic{md}
}

func (f *Frontend) ModuleSummary(path string) (ModuleSummary, bool) {
	if f == nil {
		return ModuleSummary{}, false
	}
	exports, ok := f.ModuleExports(path)
	if !ok {
		return ModuleSummary{}, false
	}
	summary := ModuleSummary{
		Path:         path,
		ExportCounts: ModuleExportCounts{Callables: len(exports.Callables), Variables: len(exports.Variables), Objects: len(exports.Objects), Interfaces: len(exports.Interfaces), Types: len(exports.Types), Enums: len(exports.Enums)},
	}
	if f.moduleGraph != nil {
		summary.Dependencies = f.moduleGraph.Dependencies(path)
		summary.ReverseDependencies = f.moduleGraph.ReverseDependencies(path)
		summary.HasCycle = f.moduleGraph.HasCycle()
	}
	if mod, ok := f.moduleCache[path]; ok {
		summary.Imports = moduleImportResolutions(mod.compiled)
	}
	return summary, true
}

func (f *Frontend) AllModuleSummaries() []ModuleSummary {
	if f == nil || f.moduleCache == nil {
		return nil
	}
	paths := make([]string, 0, len(f.moduleCache))
	for path := range f.moduleCache {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	result := make([]ModuleSummary, 0, len(paths))
	for _, path := range paths {
		if summary, ok := f.ModuleSummary(path); ok {
			result = append(result, summary)
		}
	}
	return result
}

func (f *Frontend) ExplainImport(modulePath string, name string) (ModuleImportResolution, bool) {
	if f == nil || f.moduleCache == nil {
		return ModuleImportResolution{}, false
	}
	mod, ok := f.moduleCache[modulePath]
	if !ok {
		return ModuleImportResolution{}, false
	}
	return findImportResolution(moduleImportResolutions(mod.compiled), name)
}

func (f *Frontend) RootImportResolutions() []ModuleImportResolution {
	if f == nil {
		return nil
	}
	return importResolutionsFromMetadata(f.importedSymbols, f.importedTypes)
}

func (f *Frontend) ExplainRootImport(name string) (ModuleImportResolution, bool) {
	if f == nil {
		return ModuleImportResolution{}, false
	}
	return findImportResolution(importResolutionsFromMetadata(f.importedSymbols, f.importedTypes), name)
}

func (f *Frontend) ExplainImportTrace(modulePath string, name string) (ModuleImportTrace, bool) {
	if f == nil || f.moduleCache == nil {
		return ModuleImportTrace{}, false
	}
	mod, ok := f.moduleCache[modulePath]
	if !ok {
		return ModuleImportTrace{}, false
	}
	return f.buildImportTrace(modulePath, findTraceSeed(mod.compiled, name))
}

func (f *Frontend) ExplainRootImportTrace(name string) (ModuleImportTrace, bool) {
	if f == nil {
		return ModuleImportTrace{}, false
	}
	return f.buildImportTrace(rootModulePath, findTraceSeedFromMetadata(f.importedSymbols, f.importedTypes, name))
}

func (f *Frontend) ExplainImportText(modulePath string, name string) (string, bool) {
	trace, ok := f.ExplainImportTrace(modulePath, name)
	if !ok {
		return "", false
	}
	return formatImportTraceText(trace), true
}

func (f *Frontend) ExplainRootImportText(name string) (string, bool) {
	trace, ok := f.ExplainRootImportTrace(name)
	if !ok {
		return "", false
	}
	return formatImportTraceText(trace), true
}

func (f *Frontend) ExplainModuleText(path string) (string, bool) {
	if f == nil {
		return "", false
	}
	if path == rootModulePath {
		return f.formatRootModuleText(), true
	}
	summary, ok := f.ModuleSummary(path)
	if !ok {
		return "", false
	}
	var lines []string
	lines = append(lines, fmt.Sprintf("module %q", path))
	lines = append(lines, fmt.Sprintf("exports: callables=%d variables=%d objects=%d interfaces=%d types=%d enums=%d", summary.ExportCounts.Callables, summary.ExportCounts.Variables, summary.ExportCounts.Objects, summary.ExportCounts.Interfaces, summary.ExportCounts.Types, summary.ExportCounts.Enums))
	if len(summary.Dependencies) > 0 {
		lines = append(lines, "dependencies: "+strings.Join(summary.Dependencies, ", "))
	}
	if len(summary.Imports) > 0 {
		lines = append(lines, "imports:")
		for _, resolution := range summary.Imports {
			lines = append(lines, "- "+formatImportResolutionLine(resolution))
		}
	}
	if exports, ok := f.ModuleExports(path); ok {
		text := strings.TrimSpace(exports.SporeSyntax())
		if text != "" {
			lines = append(lines, "exports syntax:")
			lines = append(lines, text)
		}
	}
	return strings.Join(lines, "\n"), true
}

func (f *Frontend) FindSymbolImporters(modulePath, exportName string) []ModuleImporter {
	if f == nil || exportName == "" {
		return nil
	}
	var result []ModuleImporter
	for _, importer := range importersFromMetadata(rootModulePath, f.importedSymbols, f.importedTypes) {
		if importer.TargetModulePath == modulePath && matchesImporterName(importer, exportName) {
			result = append(result, importer)
		}
	}
	for _, path := range f.sortedModulePaths() {
		mod, ok := f.moduleCache[path]
		if !ok {
			continue
		}
		for _, importer := range importersFromMetadata(path, mod.compiled.ImportedSymbols(), mod.compiled.ImportedTypes()) {
			if importer.TargetModulePath == modulePath && matchesImporterName(importer, exportName) {
				result = append(result, importer)
			}
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].ImporterPath != result[j].ImporterPath {
			return result[i].ImporterPath < result[j].ImporterPath
		}
		if result[i].LocalName != result[j].LocalName {
			return result[i].LocalName < result[j].LocalName
		}
		return result[i].RequestedName < result[j].RequestedName
	})
	return result
}

func findImportResolution(resolutions []ModuleImportResolution, name string) (ModuleImportResolution, bool) {
	for _, resolution := range resolutions {
		if resolution.LocalName == name {
			return resolution, true
		}
	}
	for _, resolution := range resolutions {
		if resolution.RequestedName == name {
			return resolution, true
		}
	}
	return ModuleImportResolution{}, false
}

func importResolutionsFromMetadata(symbols []ImportedSymbolMetadata, types []ImportedTypeMetadata) []ModuleImportResolution {
	var result []ModuleImportResolution
	for _, imp := range symbols {
		result = append(result, ModuleImportResolution{Path: imp.Path, RequestedName: imp.Name, LocalName: imp.LocalName, Kind: imp.Kind, TargetName: imp.TargetName, Resolved: imp.Resolved, SourceKind: imp.SourceKind, ReExport: imp.ReExport, TargetModulePath: imp.TargetModulePath, Explanation: imp.Explanation})
	}
	for _, imp := range types {
		result = append(result, ModuleImportResolution{Path: imp.Path, RequestedName: imp.Name, LocalName: imp.LocalName, Kind: imp.Kind, TargetName: imp.Type, Resolved: imp.Resolved, SourceKind: imp.SourceKind, ReExport: imp.ReExport, TargetModulePath: imp.TargetModulePath, Explanation: imp.Explanation})
	}
	return result
}

func moduleImportResolutions(compiled CompiledDeclarations) []ModuleImportResolution {
	return importResolutionsFromMetadata(compiled.ImportedSymbols(), compiled.ImportedTypes())
}

const rootModulePath = "<root>"

type importTraceSeed struct {
	resolution ModuleImportResolution
	ok         bool
}

func findTraceSeed(compiled CompiledDeclarations, name string) importTraceSeed {
	return findTraceSeedFromMetadata(compiled.ImportedSymbols(), compiled.ImportedTypes(), name)
}

func findTraceSeedFromMetadata(symbols []ImportedSymbolMetadata, types []ImportedTypeMetadata, name string) importTraceSeed {
	resolution, ok := findImportResolution(importResolutionsFromMetadata(symbols, types), name)
	return importTraceSeed{resolution: resolution, ok: ok}
}

func (f *Frontend) buildImportTrace(modulePath string, seed importTraceSeed) (ModuleImportTrace, bool) {
	if !seed.ok {
		return ModuleImportTrace{}, false
	}
	trace := ModuleImportTrace{
		ModulePath:       modulePath,
		RequestedName:    seed.resolution.RequestedName,
		LocalName:        seed.resolution.LocalName,
		Kind:             seed.resolution.Kind,
		TargetName:       seed.resolution.TargetName,
		SourceKind:       seed.resolution.SourceKind,
		TargetModulePath: seed.resolution.TargetModulePath,
		Resolved:         seed.resolution.Resolved,
		Explanation:      seed.resolution.Explanation,
	}
	visited := make(map[string]bool)
	currentModule := modulePath
	current := seed.resolution
	for current.Resolved {
		trace.Hops = append(trace.Hops, traceHopFromResolution(currentModule, current))
		nextPath, nextName, follow := f.nextTraceStep(currentModule, current)
		if !follow {
			break
		}
		key := nextPath + "\x00" + nextName
		if visited[key] {
			break
		}
		visited[key] = true
		mod, ok := f.moduleCache[nextPath]
		if !ok {
			break
		}
		next, ok := findImportResolution(moduleImportResolutions(mod.compiled), nextName)
		if !ok {
			break
		}
		currentModule = nextPath
		current = next
	}
	return trace, true
}

func traceHopFromResolution(modulePath string, resolution ModuleImportResolution) ModuleImportTraceHop {
	return ModuleImportTraceHop{ModulePath: modulePath, RequestedName: resolution.RequestedName, LocalName: resolution.LocalName, Kind: resolution.Kind, TargetName: resolution.TargetName, SourceKind: resolution.SourceKind, ReExport: resolution.ReExport, TargetModulePath: resolution.TargetModulePath, Explanation: resolution.Explanation}
}

func (f *Frontend) nextTraceStep(modulePath string, resolution ModuleImportResolution) (string, string, bool) {
	if f == nil || f.moduleCache == nil || resolution.SourceKind == "native" || resolution.TargetModulePath == "" {
		return "", "", false
	}
	nextPath := resolution.TargetModulePath
	nextName := resolution.TargetName
	if strings.HasPrefix(nextName, "__module_") {
		nextName = requestedNameFromQualifiedCallable(nextPath, nextName)
	}
	mod, ok := f.moduleCache[nextPath]
	if !ok {
		return "", "", false
	}
	if nestedPath := resolveReExportTargetPath(mod, nextName); nestedPath != mod.path {
		return nestedPath, nextName, true
	}
	next, ok := findImportResolution(moduleImportResolutions(mod.compiled), nextName)
	if !ok || !next.ReExport {
		return "", "", false
	}
	return next.TargetModulePath, next.RequestedName, true
}

func requestedNameFromQualifiedCallable(modulePath, targetName string) string {
	prefix := qualifiedModuleCallableName(modulePath, "")
	if strings.HasPrefix(targetName, prefix) {
		return strings.TrimPrefix(targetName, prefix)
	}
	return targetName
}

func (f *Frontend) formatRootModuleText() string {
	var lines []string
	lines = append(lines, "module \"<root>\"")
	resolutions := f.RootImportResolutions()
	if len(resolutions) > 0 {
		lines = append(lines, "imports:")
		for _, resolution := range resolutions {
			lines = append(lines, "- "+formatImportResolutionLine(resolution))
		}
	}
	if len(f.exports) > 0 {
		lines = append(lines, fmt.Sprintf("exports: callables=%d", len(f.exports)))
	}
	return strings.Join(lines, "\n")
}

func formatImportTraceText(trace ModuleImportTrace) string {
	var lines []string
	lines = append(lines, fmt.Sprintf("import %q in module %q", trace.LocalName, trace.ModulePath))
	lines = append(lines, fmt.Sprintf("resolved: %t kind=%s target=%s.%s source=%s", trace.Resolved, trace.Kind, trace.TargetModulePath, trace.TargetName, trace.SourceKind))
	if trace.Explanation != "" {
		lines = append(lines, "explanation: "+trace.Explanation)
	}
	if len(trace.Hops) > 0 {
		lines = append(lines, "trace:")
		for i, hop := range trace.Hops {
			lines = append(lines, fmt.Sprintf("%d. %s", i+1, formatTraceHopLine(hop)))
		}
	}
	return strings.Join(lines, "\n")
}

func formatTraceHopLine(hop ModuleImportTraceHop) string {
	parts := []string{fmt.Sprintf("%s imports %s as %s", hop.ModulePath, hop.RequestedName, hop.LocalName)}
	parts = append(parts, fmt.Sprintf("kind=%s", hop.Kind))
	parts = append(parts, fmt.Sprintf("target=%s.%s", hop.TargetModulePath, hop.TargetName))
	parts = append(parts, fmt.Sprintf("source=%s", hop.SourceKind))
	if hop.ReExport {
		parts = append(parts, "re-export")
	}
	return strings.Join(parts, " ")
}

func formatImportResolutionLine(resolution ModuleImportResolution) string {
	parts := []string{fmt.Sprintf("%s as %s", resolution.RequestedName, resolution.LocalName)}
	parts = append(parts, fmt.Sprintf("kind=%s", resolution.Kind))
	parts = append(parts, fmt.Sprintf("target=%s.%s", resolution.TargetModulePath, resolution.TargetName))
	parts = append(parts, fmt.Sprintf("source=%s", resolution.SourceKind))
	if resolution.ReExport {
		parts = append(parts, "re-export")
	}
	if resolution.Explanation != "" {
		parts = append(parts, "-- "+resolution.Explanation)
	}
	return strings.Join(parts, " ")
}

func (f *Frontend) sortedModulePaths() []string {
	if f == nil || f.moduleCache == nil {
		return nil
	}
	paths := make([]string, 0, len(f.moduleCache))
	for path := range f.moduleCache {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func importersFromMetadata(importerPath string, symbols []ImportedSymbolMetadata, types []ImportedTypeMetadata) []ModuleImporter {
	var result []ModuleImporter
	for _, imp := range symbols {
		result = append(result, ModuleImporter{ImporterPath: importerPath, RequestedName: imp.Name, LocalName: imp.LocalName, Kind: imp.Kind, SourceKind: imp.SourceKind, ReExport: imp.ReExport, TargetModulePath: imp.TargetModulePath})
	}
	for _, imp := range types {
		result = append(result, ModuleImporter{ImporterPath: importerPath, RequestedName: imp.Name, LocalName: imp.LocalName, Kind: imp.Kind, SourceKind: imp.SourceKind, ReExport: imp.ReExport, TargetModulePath: imp.TargetModulePath})
	}
	return result
}

func matchesImporterName(importer ModuleImporter, exportName string) bool {
	if importer.RequestedName == exportName || importer.LocalName == exportName {
		return true
	}
	if strings.HasSuffix(importer.LocalName, "__"+exportName) || strings.HasSuffix(importer.RequestedName, "__"+exportName) {
		return true
	}
	return false
}

func (f *Frontend) registerCompiledDeclarations(compiled CompiledDeclarations) error {
	for _, callable := range compiled.CallableDeclarations() {
		desc := callable.Desc
		if err := f.binding.Callables.Register(desc); err != nil {
			return err
		}
		adapter, err := NewScriptCallableAdapter(desc, runtimeBackendFromVMLowering(f.vmCompileHook))
		if err != nil {
			return err
		}
		if err := f.binding.Executors.RegisterAdapter(adapter); err != nil {
			return err
		}
	}
	f.exports = compiled.ExportedCallables()
	objects := compiled.ObjectDeclarations()
	for _, object := range objects {
		if _, exists := f.objects[object.Desc.Name]; exists {
			return fmt.Errorf("object %q already registered", object.Desc.Name)
		}
		f.objects[object.Desc.Name] = objectEntry{desc: object.Desc}
	}
	f.interfaces = compiled.InterfaceDeclarations()
	f.imports = compiled.Imports()
	f.importedSymbols = compiled.ImportedSymbols()
	f.importedTypes = compiled.ImportedTypes()
	if modules, ok := compiled.LinkedModules(); ok {
		f.linkedModules = modules
	}
	f.packageName = compiled.PackageName()
	f.Refresh()
	for _, object := range objects {
		f.objects[object.Desc.Name] = objectEntry{desc: object.Desc}
	}
	// Re-apply interfaces after Refresh clears them.
	f.interfaces = compiled.InterfaceDeclarations()
	// Enums are compile-time type declarations; Refresh does not manage them.
	f.enums = compiled.EnumDeclarations()
	return nil
}

// Callable looks up the canonical callable descriptor by name.
func (f *Frontend) Callable(name string) (schema.CallableDesc, bool) {
	if f == nil {
		return schema.CallableDesc{}, false
	}
	entry, ok := f.callables[name]
	if !ok {
		return schema.CallableDesc{}, false
	}
	return schema.CloneCallableDesc(entry.desc), true
}

// Callables returns the current callable snapshot indexed by name.
func (f *Frontend) Callables() map[string]schema.CallableDesc {
	result := make(map[string]schema.CallableDesc, len(f.callables))
	for name, entry := range f.callables {
		result[name] = schema.CloneCallableDesc(entry.desc)
	}
	return result
}

// ExportedCallables returns top-level exported callable descriptors in declaration order.
func (f *Frontend) ExportedCallables() []schema.CallableDesc {
	if f == nil || len(f.exports) == 0 {
		return nil
	}
	result := make([]schema.CallableDesc, len(f.exports))
	for i, desc := range f.exports {
		result[i] = schema.CloneCallableDesc(desc)
	}
	return result
}

// LookupExportedCallable finds a top-level exported callable descriptor by name.
func (f *Frontend) LookupExportedCallable(name string) (schema.CallableDesc, bool) {
	if f == nil {
		return schema.CallableDesc{}, false
	}
	for _, desc := range f.exports {
		if desc.Name == name {
			return schema.CloneCallableDesc(desc), true
		}
	}
	return schema.CallableDesc{}, false
}

// Object looks up the canonical object descriptor by name.
func (f *Frontend) Object(name string) (schema.ObjectDesc, bool) {
	if f == nil {
		return schema.ObjectDesc{}, false
	}
	entry, ok := f.objects[name]
	if !ok {
		return schema.ObjectDesc{}, false
	}
	return schema.CloneObjectDesc(entry.desc), true
}

// Objects returns the current object descriptor snapshot indexed by name.
func (f *Frontend) Objects() map[string]schema.ObjectDesc {
	result := make(map[string]schema.ObjectDesc, len(f.objects))
	for name, entry := range f.objects {
		result[name] = schema.CloneObjectDesc(entry.desc)
	}
	return result
}

// Invoke dispatches through the existing ScriptBinding invocation path.
func (f *Frontend) Invoke(callable string, args []any) (binding.InvocationOutcome, error) {
	return f.InvokeStage(callable, binding.InvocationStageUnary, args)
}

func (f *Frontend) InvokeStage(callable string, stage binding.InvocationStage, args []any) (binding.InvocationOutcome, error) {
	return f.InvokeStageContext(context.Background(), binding.ExecutionBudget{}, callable, stage, args)
}

func (f *Frontend) InvokeStageContext(ctx context.Context, budget binding.ExecutionBudget, callable string, stage binding.InvocationStage, args []any) (binding.InvocationOutcome, error) {
	if f == nil || f.binding == nil {
		return binding.InvocationOutcome{}, fmt.Errorf("script binding is required")
	}
	return f.binding.Invoke(binding.InvocationRequest{
		Callable: callable,
		Stage:    stage,
		Args:     args,
		Context:  ctx,
		Budget:   budget,
	})
}

// ModuleExports returns the exported symbols for a resolved module by path.
// If the module has not been resolved, returns false.
func (f *Frontend) ModuleExports(path string) (ModuleExports, bool) {
	if f == nil || f.moduleCache == nil {
		return ModuleExports{}, false
	}
	mod, ok := f.moduleCache[path]
	if !ok {
		return ModuleExports{}, false
	}
	result := ModuleExports{Path: path}
	for _, desc := range mod.compiled.ExportedCallables() {
		result.Callables = append(result.Callables, schema.CloneCallableDesc(desc))
	}
	for _, v := range mod.compiled.ExportedVariables() {
		result.Variables = append(result.Variables, ExportedVariableMetadata{Name: v.Name, Type: v.Type})
	}
	for _, obj := range mod.compiled.ExportedObjects() {
		result.Objects = append(result.Objects, schema.CloneObjectDesc(obj))
	}
	for _, iface := range mod.compiled.ExportedInterfaces() {
		result.Interfaces = append(result.Interfaces, schema.CloneInterfaceDesc(iface))
	}
	for _, enumDesc := range mod.compiled.ExportedEnums() {
		result.Enums = append(result.Enums, schema.CloneEnumDesc(enumDesc))
	}
	for _, typ := range mod.compiled.ExportedTypes() {
		result.Types = append(result.Types, ExportedTypeMetadata{Name: typ.Name, Type: typ.Type, TypeDesc: typ.TypeDesc})
	}
	return result, true
}

// AllModuleExports returns exported symbols for all resolved modules.
func (f *Frontend) AllModuleExports() []ModuleExports {
	if f == nil || f.moduleCache == nil {
		return nil
	}
	result := make([]ModuleExports, 0, len(f.moduleCache))
	for path := range f.moduleCache {
		if exports, ok := f.ModuleExports(path); ok {
			result = append(result, exports)
		}
	}
	return result
}

type parseError struct {
	line     int
	column   int
	msg      string
	code     sourceDiagnosticCode
	path     string
	expected string
	actual   string
}

type sourceDiagnosticCode string

const diagNoEvaluator sourceDiagnosticCode = "no_evaluator"
const diagParseError sourceDiagnosticCode = "parse_error"

func init() {
	diagnostics.RegisterCode(diagnostics.CodeInfo{Code: string(diagNoEvaluator), Category: diagnostics.CategoryHost, Description: "Source-defined callable has no evaluator", Hint: "为该 callable 提供 evaluator，或改为绑定到可执行实现"})
	diagnostics.RegisterCode(diagnostics.CodeInfo{Code: string(diagParseError), Category: diagnostics.CategoryLoad, Description: "Frontend parser rejected the source", Hint: "检查报错位置附近的语法，并按 expected/actual 修正 token"})
}

type sourceEvalError struct {
	code        sourceDiagnosticCode
	category    diagnostics.Category
	callable    string
	stage       string
	span        diagnostics.Span
	stack       []diagnostics.Frame
	cause       *diagnostics.Descriptor
	baseMessage string
	expected    string
	actual      string
}

func (e *sourceEvalError) Error() string {
	if e == nil {
		return ""
	}
	if e.baseMessage != "" {
		return e.baseMessage
	}
	return fmt.Sprintf("source-defined callable %q has no evaluator", e.callable)
}

func (e *sourceEvalError) DiagnosticCode() string { return string(e.code) }
func (e *sourceEvalError) DiagnosticCategory() diagnostics.Category {
	if e == nil || e.category == "" {
		return diagnostics.CategoryHost
	}
	return e.category
}
func (e *sourceEvalError) DiagnosticSpan() diagnostics.Span {
	if e == nil {
		return diagnostics.Span{}
	}
	return e.span
}
func (e *sourceEvalError) DiagnosticStack() []diagnostics.Frame {
	if e == nil || len(e.stack) == 0 {
		return nil
	}
	return append([]diagnostics.Frame(nil), e.stack...)
}
func (e *sourceEvalError) DiagnosticCause() *diagnostics.Descriptor {
	if e == nil {
		return nil
	}
	return diagnostics.ClonePtr(e.cause)
}
func (e *sourceEvalError) DiagnosticExpected() string {
	if e == nil {
		return ""
	}
	return e.expected
}
func (e *sourceEvalError) DiagnosticActual() string {
	if e == nil {
		return ""
	}
	return e.actual
}

func (e parseError) Error() string {
	if e.column > 0 {
		return fmt.Sprintf("line %d, column %d: %s", e.line, e.column, e.msg)
	}
	return fmt.Sprintf("line %d: %s", e.line, e.msg)
}

func (e parseError) DiagnosticCode() string                   { return string(e.code) }
func (e parseError) DiagnosticCategory() diagnostics.Category { return diagnostics.CategoryLoad }
func (e parseError) DiagnosticPath() string {
	if e.path == "" {
		return "frontend/parse"
	}
	return e.path
}
func (e parseError) DiagnosticSpan() diagnostics.Span {
	return diagnostics.Span{Start: diagnostics.Position{Line: e.line, Column: e.column}, End: diagnostics.Position{Line: e.line, Column: e.column}}
}
func (e parseError) DiagnosticExpected() string { return e.expected }
func (e parseError) DiagnosticActual() string   { return e.actual }

func noEvaluatorError(callable string) error {
	frame := diagnostics.Frame{Callable: callable, Stage: string(binding.InvocationStageUnary)}
	return &sourceEvalError{
		code:        diagNoEvaluator,
		category:    diagnostics.CategoryHost,
		callable:    callable,
		stage:       string(binding.InvocationStageUnary),
		stack:       []diagnostics.Frame{frame},
		baseMessage: fmt.Sprintf("source-defined callable %q has no evaluator", callable),
	}
}

func leadingSpaceCount(s string) int {
	count := 0
	for _, r := range s {
		if r != ' ' && r != '\t' {
			break
		}
		count++
	}
	return count
}

func validateIdentifier(name string, label string) error {
	if name == "" {
		return fmt.Errorf("%s cannot be empty", label)
	}
	for i, r := range name {
		if i == 0 {
			if !isIdentifierStart(r) {
				return fmt.Errorf("%s %q is invalid", label, name)
			}
			continue
		}
		if !isIdentifierPart(r) {
			return fmt.Errorf("%s %q is invalid", label, name)
		}
	}
	return nil
}

func isIdentifierStart(r rune) bool {
	return r == '_' || unicode.IsLetter(r)
}

func isIdentifierPart(r rune) bool {
	return isIdentifierStart(r) || unicode.IsDigit(r)
}

func newParseError(line int, column int, msg string) error {
	return parseError{line: line, column: column, msg: msg, code: diagParseError, path: "frontend/parse"}
}
