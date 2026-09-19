package frontend

import (
	"fmt"
	"sort"
	"strings"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/diagnostics"
	"github.com/qomos-w/spore/schema"
)

type ModuleResolver interface {
	ResolveModule(path string) (string, error)
}

type MapModuleResolver map[string]string

func (r MapModuleResolver) ResolveModule(path string) (string, error) {
	source, ok := r[path]
	if !ok {
		return "", fmt.Errorf("module %q not found", path)
	}
	return source, nil
}

type linkedProgram struct {
	program              *program
	importedSymbols      []ImportedSymbolMetadata
	importedTypes        []ImportedTypeMetadata
	modulePrograms       []linkedModule
	reExportedCallables  []schema.CallableDesc
	reExportedVariables  []ExportedVariableMetadata
	reExportedObjects    []schema.ObjectDesc
	reExportedInterfaces []schema.InterfaceDesc
	reExportedTypes      []ExportedTypeMetadata
	reExportedEnums      []schema.EnumDesc
	graph                *ModuleGraph
}

type linkedModule struct {
	path     string
	program  *program
	compiled CompiledDeclarations
}

func (f *Frontend) compiledDeclarationsForProgram(prog *program) (CompiledDeclarations, error) {
	if f == nil || len(prog.imports) == 0 {
		f.moduleGraph = newModuleGraph()
		return compiledDeclarationsFromProgram(prog)
	}
	linked, err := f.linkProgram(prog)
	if err != nil {
		f.moduleGraph = linked.graph
		return CompiledDeclarations{}, err
	}
	compiled, err := compiledDeclarationsFromProgramWithImports(linked.program, linked.importedTypes)
	if err != nil {
		return CompiledDeclarations{}, err
	}
	compiled.importedSymbols = append(compiled.importedSymbols, linked.importedSymbols...)
	compiled.importedTypes = append(compiled.importedTypes, linked.importedTypes...)
	compiled.exportedCallables = append(compiled.exportedCallables, linked.reExportedCallables...)
	compiled.exportedVariables = append(compiled.exportedVariables, linked.reExportedVariables...)
	compiled.exportedObjects = append(compiled.exportedObjects, linked.reExportedObjects...)
	compiled.exportedInterfaces = append(compiled.exportedInterfaces, linked.reExportedInterfaces...)
	compiled.exportedTypes = append(compiled.exportedTypes, linked.reExportedTypes...)
	compiled.exportedEnums = append(compiled.exportedEnums, linked.reExportedEnums...)
	for _, module := range linked.modulePrograms {
		compiled.linkedModules = appendModuleOnceLinked(compiled.linkedModules, module.path, module.program)
		// Include nested linked modules from re-export resolution.
		if nested, ok := module.compiled.LinkedModules(); ok {
			for _, n := range nested {
				compiled.linkedModules = appendModuleOnceLinked(compiled.linkedModules, n.Path, n.Program)
			}
		}
	}
	f.moduleGraph = linked.graph
	return compiled, nil
}

func (f *Frontend) linkProgram(prog *program) (linkedProgram, error) {
	if f.moduleCache == nil {
		f.moduleCache = make(map[string]compiledModule)
	}
	state := moduleLinkState{
		frontend: f,
		stack:    make(map[string]bool),
		visited:  make(map[string]compiledModule),
		cache:    f.moduleCache,
		graph:    newModuleGraph(),
	}
	return state.linkRoot(prog)
}

type compiledModule struct {
	path     string
	program  *program
	compiled CompiledDeclarations
}

type moduleLinkState struct {
	frontend *Frontend
	stack    map[string]bool
	visited  map[string]compiledModule
	cache    map[string]compiledModule // Frontend-level module cache
	graph    *ModuleGraph
}

type moduleDiagnosticError struct {
	err        error
	diagnostic ModuleDiagnostic
}

func (e *moduleDiagnosticError) Error() string { return e.err.Error() }
func (e *moduleDiagnosticError) Unwrap() error { return e.err }

func availableExports(mod compiledModule) []string {
	var names []string
	for _, desc := range mod.compiled.ExportedCallables() {
		names = append(names, desc.Name)
	}
	for _, variable := range mod.compiled.ExportedVariables() {
		names = append(names, variable.Name)
	}
	for _, obj := range mod.compiled.ExportedObjects() {
		names = append(names, obj.Name)
	}
	for _, iface := range mod.compiled.ExportedInterfaces() {
		names = append(names, iface.Name)
	}
	for _, enumDesc := range mod.compiled.ExportedEnums() {
		names = append(names, enumDesc.Name)
	}
	for _, typ := range mod.compiled.ExportedTypes() {
		names = append(names, typ.Name)
	}
	sort.Strings(names)
	return names
}

func moduleImportDiagnostic(err error, imp *importStmt, localName string, phase string, sourceKind string, targetPath string, exports []string) error {
	if err == nil {
		return nil
	}
	md := ModuleDiagnostic{Phase: phase, SourceKind: sourceKind, TargetModulePath: targetPath, AvailableExports: append([]string(nil), exports...)}
	if imp != nil {
		md.ImportName = imp.Name
		md.LocalName = localName
		md.SourceModulePath = imp.Path
		if md.TargetModulePath == "" {
			md.TargetModulePath = imp.Path
		}
	}
	return &moduleDiagnosticError{err: err, diagnostic: md}
}

func importLocalName(name, alias string) string {
	if alias != "" {
		return alias
	}
	return name
}

func importKindLabel(imp *importStmt) string {
	if imp.Name == "*" && imp.ReExport {
		return "wildcard_reexport"
	}
	if imp.ReExport {
		return "reexport"
	}
	return "symbol"
}

func callableImportResolution(path, requestedName, localName, targetPath, targetName string, reExport bool) ImportedSymbolMetadata {
	return ImportedSymbolMetadata{
		Path:             path,
		Name:             requestedName,
		LocalName:        localName,
		Kind:             "callable",
		TargetName:       targetName,
		Resolved:         true,
		SourceKind:       "module",
		ReExport:         reExport,
		TargetModulePath: targetPath,
		Explanation:      fmt.Sprintf("callable import %q from module %q resolves to %q", requestedName, path, targetName),
	}
}

func variableImportResolution(path, requestedName, localName, targetPath, targetName, typeName string, reExport bool) ImportedSymbolMetadata {
	return ImportedSymbolMetadata{
		Path:             path,
		Name:             requestedName,
		LocalName:        localName,
		Kind:             "variable",
		TargetName:       targetName,
		Type:             typeName,
		Resolved:         true,
		SourceKind:       "module",
		ReExport:         reExport,
		TargetModulePath: targetPath,
		Explanation:      fmt.Sprintf("variable import %q from module %q resolves to %q", requestedName, path, targetName),
	}
}

func nativeValueImportResolution(path, requestedName, localName string, desc binding.CapabilityValueDesc, reExport bool) ImportedSymbolMetadata {
	return ImportedSymbolMetadata{
		Path:             path,
		Name:             requestedName,
		LocalName:        localName,
		Kind:             "native_value",
		TargetName:       path + "." + desc.Name,
		Type:             exportedTypeName(desc.Type),
		Resolved:         true,
		SourceKind:       "native",
		ReExport:         reExport,
		TargetModulePath: path,
		Explanation:      fmt.Sprintf("value import %q from capability %q resolves to %q", requestedName, path, path+"."+desc.Name),
	}
}

func nativeImportResolution(path, requestedName, localName, targetName string, reExport bool) ImportedSymbolMetadata {
	return nativeImportResolutionWithTarget(path, requestedName, localName, path, targetName, reExport)
}

func nativeImportResolutionWithTarget(path, requestedName, localName, targetPath, targetName string, reExport bool) ImportedSymbolMetadata {
	return ImportedSymbolMetadata{
		Path:             path,
		Name:             requestedName,
		LocalName:        localName,
		Kind:             "native",
		TargetName:       targetName,
		Resolved:         true,
		SourceKind:       "native",
		ReExport:         reExport,
		TargetModulePath: targetPath,
		Explanation:      fmt.Sprintf("native import %q from capability %q resolves to %q", requestedName, targetPath, targetName),
	}
}

func structImportResolution(path, requestedName, localName, typeName string, obj *schema.ObjectDesc, reExport bool) ImportedTypeMetadata {
	return ImportedTypeMetadata{
		Path:             path,
		Name:             requestedName,
		LocalName:        localName,
		Kind:             "struct",
		Type:             typeName,
		ObjectDesc:       obj,
		TypeDesc:         schema.TypeDesc{Kind: schema.TypeKindStruct, Name: localName, ClassName: localName},
		Resolved:         true,
		SourceKind:       "module",
		ReExport:         reExport,
		TargetModulePath: path,
		Explanation:      fmt.Sprintf("type import %q from module %q resolves to exported struct %q", requestedName, path, typeName),
	}
}

func nativeStructImportResolution(path, requestedName, localName, typeName string, obj *schema.ObjectDesc, reExport bool) ImportedTypeMetadata {
	meta := structImportResolution(path, requestedName, localName, typeName, obj, reExport)
	meta.SourceKind = "native"
	meta.Explanation = fmt.Sprintf("type import %q from capability %q resolves to exported struct %q", requestedName, path, typeName)
	return meta
}

func interfaceImportResolution(path, requestedName, localName string, desc schema.InterfaceDesc, reExport bool) ImportedTypeMetadata {
	return ImportedTypeMetadata{
		Path:             path,
		Name:             requestedName,
		LocalName:        localName,
		Kind:             "interface",
		Type:             desc.Name,
		InterfaceDesc:    cloneInterfaceDescPtr(&desc),
		TypeDesc:         schema.TypeDesc{Kind: schema.TypeKindClass, Name: desc.Name, ClassName: desc.Name},
		Resolved:         true,
		SourceKind:       "module",
		ReExport:         reExport,
		TargetModulePath: path,
		Explanation:      fmt.Sprintf("type import %q from module %q resolves to exported interface %q", requestedName, path, desc.Name),
	}
}

func nativeInterfaceImportResolution(path, requestedName, localName string, desc schema.InterfaceDesc, reExport bool) ImportedTypeMetadata {
	meta := interfaceImportResolution(path, requestedName, localName, desc, reExport)
	meta.SourceKind = "native"
	meta.Explanation = fmt.Sprintf("type import %q from capability %q resolves to exported interface %q", requestedName, path, desc.Name)
	return meta
}

func typeAliasImportResolution(path, requestedName, localName, typeName string, td schema.TypeDesc, reExport bool) ImportedTypeMetadata {
	return ImportedTypeMetadata{
		Path:             path,
		Name:             requestedName,
		LocalName:        localName,
		Kind:             "type",
		Type:             typeName,
		TypeDesc:         td,
		Resolved:         true,
		SourceKind:       "module",
		ReExport:         reExport,
		TargetModulePath: path,
		Explanation:      fmt.Sprintf("type import %q from module %q resolves to exported type %q", requestedName, path, typeName),
	}
}

func enumImportResolution(path, requestedName, localName string, desc schema.EnumDesc, reExport bool) ImportedTypeMetadata {
	return ImportedTypeMetadata{
		Path:             path,
		Name:             requestedName,
		LocalName:        localName,
		Kind:             "enum",
		Type:             desc.Name,
		EnumDesc:         &desc,
		TypeDesc:         schema.TypeDesc{Kind: schema.TypeKindEnum, Name: desc.Name},
		Resolved:         true,
		SourceKind:       "module",
		ReExport:         reExport,
		TargetModulePath: path,
		Explanation:      fmt.Sprintf("type import %q from module %q resolves to exported enum %q", requestedName, path, desc.Name),
	}
}

func nativeTypeAliasImportResolution(path, requestedName, localName, typeName string, td schema.TypeDesc, reExport bool) ImportedTypeMetadata {
	meta := typeAliasImportResolution(path, requestedName, localName, typeName, td, reExport)
	meta.SourceKind = "native"
	meta.Explanation = fmt.Sprintf("type import %q from capability %q resolves to exported type %q", requestedName, path, typeName)
	return meta
}

func (s *moduleLinkState) linkRoot(prog *program) (linkedProgram, error) {
	linked := linkedProgram{program: prog, graph: s.graph}
	if prog == nil {
		return linked, nil
	}
	localNames := topLevelDeclarationNames(prog)
	for _, imp := range prog.imports {
		// Record dependency edge from root to imported module.
		s.graph.addEdge("", imp.Path)
		localName := importLocalName(imp.Name, imp.Alias)
		if localNames[localName] {
			return linked, moduleImportDiagnostic(moduleLinkError("duplicate_import_binding", "frontend/link/import", fmt.Sprintf("import %q collides with local declaration", localName)), imp, localName, "bind_local", "module", imp.Path, nil)
		}
		if containsImportedLocalName(linked.importedSymbols, localName) || containsImportedTypeLocalName(linked.importedTypes, localName) {
			return linked, moduleImportDiagnostic(moduleLinkError("duplicate_import_binding", "frontend/link/import", fmt.Sprintf("duplicate import binding %q", localName)), imp, localName, "bind_local", "module", imp.Path, nil)
		}
		mod, err := s.resolveCompiledModule(imp.Path)
		if err != nil {
			if _, ok := err.(*moduleDiagnosticError); !ok {
				err = moduleImportDiagnostic(err, imp, localName, "resolve_module", "module", imp.Path, nil)
			}
		}
		if err == nil {
			// Wildcard re-export: copy all exports from source module.
			if imp.Name == "*" && imp.ReExport {
				if mod.program == nil {
					return linked, moduleImportDiagnostic(moduleLinkError("cyclic_import", "frontend/link/import", fmt.Sprintf("cyclic import detected for %q", imp.Path)), imp, localName, "resolve_cycle", "module", imp.Path, availableExports(mod))
				}
				linked.modulePrograms = appendModuleOnce(linked.modulePrograms, imp.Path, mod.program, mod.compiled)
				for _, desc := range mod.compiled.ExportedCallables() {
					reDesc := schema.CloneCallableDesc(desc)
					linked.reExportedCallables = append(linked.reExportedCallables, reDesc)
				}
				for _, variable := range mod.compiled.ExportedVariables() {
					linked.reExportedVariables = append(linked.reExportedVariables, ExportedVariableMetadata{Name: variable.Name, Type: variable.Type})
				}
				for _, obj := range mod.compiled.ExportedObjects() {
					reObj := schema.CloneObjectDesc(obj)
					linked.reExportedObjects = append(linked.reExportedObjects, reObj)
				}
				for _, iface := range mod.compiled.ExportedInterfaces() {
					reIface := schema.CloneInterfaceDesc(iface)
					linked.reExportedInterfaces = append(linked.reExportedInterfaces, reIface)
				}
				for _, enumDesc := range mod.compiled.ExportedEnums() {
					reEnum := schema.CloneEnumDesc(enumDesc)
					linked.reExportedEnums = append(linked.reExportedEnums, reEnum)
				}
				for _, typeMeta := range mod.compiled.ExportedTypes() {
					linked.reExportedTypes = append(linked.reExportedTypes, ExportedTypeMetadata{Name: typeMeta.Name, Type: typeMeta.Type, TypeDesc: typeMeta.TypeDesc})
				}
				continue
			}
			if desc, ok := mod.compiled.LookupExportedCallable(imp.Name); ok {
				if mod.program == nil {
					return linked, moduleImportDiagnostic(moduleLinkError("cyclic_import", "frontend/link/import", fmt.Sprintf("cyclic runtime import detected for %q", imp.Path)), imp, localName, "resolve_cycle", "module", imp.Path, availableExports(mod))
				}
				linked.importedSymbols, ok = appendForwardedNativeImport(linked.importedSymbols, mod.compiled, imp.Name, localName, imp.ReExport)
				if ok {
					linked.modulePrograms = appendModuleOnce(linked.modulePrograms, imp.Path, mod.program, mod.compiled)
					if imp.ReExport {
						reDesc := schema.CloneCallableDesc(desc)
						reDesc.Name = localName
						linked.reExportedCallables = append(linked.reExportedCallables, reDesc)
					}
					continue
				}
				targetPath := resolveReExportTargetPath(mod, imp.Name)
				targetName := qualifiedModuleCallableName(targetPath, desc.Name)
				linked.importedSymbols = append(linked.importedSymbols, callableImportResolution(imp.Path, imp.Name, localName, targetPath, targetName, imp.ReExport))
				linked.modulePrograms = appendModuleOnce(linked.modulePrograms, imp.Path, mod.program, mod.compiled)
				if imp.ReExport {
					reDesc := schema.CloneCallableDesc(desc)
					reDesc.Name = localName
					linked.reExportedCallables = append(linked.reExportedCallables, reDesc)
				}
				continue
			}
			if variable, ok := mod.compiled.LookupExportedVariable(imp.Name); ok {
				if mod.program == nil {
					return linked, moduleImportDiagnostic(moduleLinkError("cyclic_import", "frontend/link/import", fmt.Sprintf("cyclic runtime import detected for %q", imp.Path)), imp, localName, "resolve_cycle", "module", imp.Path, availableExports(mod))
				}
				linked.importedSymbols, ok = appendForwardedNativeImport(linked.importedSymbols, mod.compiled, imp.Name, localName, imp.ReExport)
				if ok {
					linked.modulePrograms = appendModuleOnce(linked.modulePrograms, imp.Path, mod.program, mod.compiled)
					if imp.ReExport {
						linked.reExportedVariables = append(linked.reExportedVariables, ExportedVariableMetadata{Name: localName, Type: variable.Type})
					}
					continue
				}
				targetPath := resolveReExportTargetPath(mod, imp.Name)
				targetName := qualifiedModuleGlobalName(targetPath, variable.Name)
				linked.importedSymbols = append(linked.importedSymbols, variableImportResolution(imp.Path, imp.Name, localName, targetPath, targetName, variable.Type, imp.ReExport))
				linked.modulePrograms = appendModuleOnce(linked.modulePrograms, imp.Path, mod.program, mod.compiled)
				if imp.ReExport {
					linked.reExportedVariables = append(linked.reExportedVariables, ExportedVariableMetadata{Name: localName, Type: variable.Type})
				}
				continue
			}
			if obj, ok := mod.compiled.LookupExportedObject(imp.Name); ok {
				linked.importedTypes = append(linked.importedTypes, structImportResolution(imp.Path, imp.Name, localName, obj.Name, &obj, imp.ReExport))
				linked.modulePrograms = appendModuleOnce(linked.modulePrograms, imp.Path, mod.program, mod.compiled)
				if imp.ReExport {
					reObj := schema.CloneObjectDesc(obj)
					reObj.Name = localName
					linked.reExportedObjects = append(linked.reExportedObjects, reObj)
				}
				continue
			}
			if iface, ok := mod.compiled.LookupExportedInterface(imp.Name); ok {
				linked.importedTypes = append(linked.importedTypes, interfaceImportResolution(imp.Path, imp.Name, localName, iface, imp.ReExport))
				linked.modulePrograms = appendModuleOnce(linked.modulePrograms, imp.Path, mod.program, mod.compiled)
				if imp.ReExport {
					reIface := schema.CloneInterfaceDesc(iface)
					reIface.Name = localName
					linked.reExportedInterfaces = append(linked.reExportedInterfaces, reIface)
				}
				continue
			}
			if enumDesc, ok := mod.compiled.LookupExportedEnum(imp.Name); ok {
				linked.importedTypes = append(linked.importedTypes, enumImportResolution(imp.Path, imp.Name, localName, enumDesc, imp.ReExport))
				linked.modulePrograms = appendModuleOnce(linked.modulePrograms, imp.Path, mod.program, mod.compiled)
				if imp.ReExport {
					reEnum := schema.CloneEnumDesc(enumDesc)
					reEnum.Name = localName
					linked.reExportedEnums = append(linked.reExportedEnums, reEnum)
				}
				continue
			}
			if typeMeta, ok := mod.compiled.LookupExportedType(imp.Name); ok {
				linked.importedTypes = append(linked.importedTypes, typeAliasImportResolution(imp.Path, imp.Name, localName, typeMeta.Type, typeMeta.TypeDesc, imp.ReExport))
				linked.modulePrograms = appendModuleOnce(linked.modulePrograms, imp.Path, mod.program, mod.compiled)
				if imp.ReExport {
					linked.reExportedTypes = append(linked.reExportedTypes, ExportedTypeMetadata{Name: localName, Type: typeMeta.Type, TypeDesc: typeMeta.TypeDesc})
				}
				continue
			}
			return linked, moduleImportDiagnostic(moduleLinkError("missing_module_export", "frontend/link/import", fmt.Sprintf("module %q does not export %q", imp.Path, imp.Name)), imp, localName, "resolve_export", "module", imp.Path, availableExports(mod))
		}
		if s.frontend != nil && s.frontend.binding != nil {
			if capDesc, ok := findCapabilityDesc(s.frontend.binding.DescribeCapabilities(), imp.Path); ok {
				if callableName, ok := capabilityCallableName(capDesc, imp.Name); ok {
					linked.importedSymbols = append(linked.importedSymbols, nativeImportResolution(imp.Path, imp.Name, localName, imp.Path+"."+callableName, imp.ReExport))
					if imp.ReExport {
						for _, cd := range capDesc.Callables {
							if cd.Name == imp.Name {
								reDesc := schema.CloneCallableDesc(cd)
								reDesc.Name = localName
								linked.reExportedCallables = append(linked.reExportedCallables, reDesc)
								break
							}
						}
					}
					continue
				}
				// Native capability type exports: check objects then type aliases.
				if obj, ok := s.frontend.binding.FindCapabilityObject(imp.Path, imp.Name); ok {
					linked.importedTypes = append(linked.importedTypes, nativeStructImportResolution(imp.Path, imp.Name, localName, obj.Name, &obj, imp.ReExport))
					if imp.ReExport {
						reObj := schema.CloneObjectDesc(obj)
						reObj.Name = localName
						linked.reExportedObjects = append(linked.reExportedObjects, reObj)
					}
					continue
				}
				if iface, ok := s.frontend.binding.FindCapabilityInterface(imp.Path, imp.Name); ok {
					linked.importedTypes = append(linked.importedTypes, nativeInterfaceImportResolution(imp.Path, imp.Name, localName, iface, imp.ReExport))
					if desc, _, valueOK := s.frontend.binding.FindCapabilityValue(imp.Path, imp.Name); valueOK {
						linked.importedSymbols = append(linked.importedSymbols, nativeValueImportResolution(imp.Path, imp.Name, localName, desc, imp.ReExport))
					}
					if imp.ReExport {
						reIface := schema.CloneInterfaceDesc(iface)
						reIface.Name = localName
						linked.reExportedInterfaces = append(linked.reExportedInterfaces, reIface)
					}
					continue
				}
				if td, ok := s.frontend.binding.FindCapabilityTypeAlias(imp.Path, imp.Name); ok {
					linked.importedTypes = append(linked.importedTypes, nativeTypeAliasImportResolution(imp.Path, imp.Name, localName, exportedTypeName(td), td, imp.ReExport))
					if imp.ReExport {
						linked.reExportedTypes = append(linked.reExportedTypes, ExportedTypeMetadata{Name: localName, Type: exportedTypeName(td), TypeDesc: td})
					}
					continue
				}
				if desc, _, ok := s.frontend.binding.FindCapabilityValue(imp.Path, imp.Name); ok {
					linked.importedSymbols = append(linked.importedSymbols, nativeValueImportResolution(imp.Path, imp.Name, localName, desc, imp.ReExport))
					if imp.ReExport {
						linked.reExportedVariables = append(linked.reExportedVariables, ExportedVariableMetadata{Name: localName, Type: exportedTypeName(desc.Type)})
					}
					continue
				}
				return linked, moduleImportDiagnostic(moduleLinkError("missing_module_export", "frontend/link/import", fmt.Sprintf("module %q does not export %q", imp.Path, imp.Name)), imp, localName, "resolve_export", "native", imp.Path, nil)
			}
		}
		return linked, err
	}
	return linked, nil
}

func appendForwardedNativeImport(symbols []ImportedSymbolMetadata, compiled CompiledDeclarations, name, localName string, reExport bool) ([]ImportedSymbolMetadata, bool) {
	for _, imported := range compiled.ImportedSymbols() {
		if imported.SourceKind != "native" {
			continue
		}
		if imported.LocalName != name && imported.Name != name {
			continue
		}
		forwarded := imported
		forwarded.Name = name
		forwarded.LocalName = localName
		forwarded.ReExport = reExport
		return append(symbols, forwarded), true
	}
	return symbols, false
}

func (s *moduleLinkState) appendNativeReExport(path string, imp *importStmt, compiled *CompiledDeclarations) bool {
	if s == nil || s.frontend == nil || s.frontend.binding == nil || imp == nil || compiled == nil {
		return false
	}
	capDesc, ok := findCapabilityDesc(s.frontend.binding.DescribeCapabilities(), imp.Path)
	if !ok {
		return false
	}
	if imp.Name == "*" {
		for _, desc := range capDesc.Callables {
			reDesc := schema.CloneCallableDesc(desc)
			compiled.exportedCallables = append(compiled.exportedCallables, reDesc)
			compiled.importedSymbols = append(compiled.importedSymbols, nativeImportResolutionWithTarget(path, desc.Name, desc.Name, imp.Path, imp.Path+"."+desc.Name, true))
		}
		for _, obj := range capDesc.Objects {
			reObj := schema.CloneObjectDesc(obj)
			compiled.exportedObjects = append(compiled.exportedObjects, reObj)
			compiled.importedTypes = append(compiled.importedTypes, nativeStructImportResolution(imp.Path, obj.Name, obj.Name, obj.Name, &obj, true))
		}
		for _, iface := range capDesc.Interfaces {
			reIface := schema.CloneInterfaceDesc(iface)
			compiled.exportedInterfaces = append(compiled.exportedInterfaces, reIface)
			compiled.importedTypes = append(compiled.importedTypes, nativeInterfaceImportResolution(imp.Path, iface.Name, iface.Name, iface, true))
			if desc, _, valueOK := s.frontend.binding.FindCapabilityValue(imp.Path, iface.Name); valueOK {
				compiled.importedSymbols = append(compiled.importedSymbols, nativeValueImportResolution(imp.Path, iface.Name, iface.Name, desc, true))
			}
		}
		for name, td := range capDesc.TypeAliases {
			compiled.exportedTypes = append(compiled.exportedTypes, ExportedTypeMetadata{Name: name, Type: exportedTypeName(td), TypeDesc: td})
			compiled.importedTypes = append(compiled.importedTypes, nativeTypeAliasImportResolution(imp.Path, name, name, exportedTypeName(td), td, true))
		}
		for _, desc := range capDesc.Values {
			compiled.exportedVariables = append(compiled.exportedVariables, ExportedVariableMetadata{Name: desc.Name, Type: exportedTypeName(desc.Type)})
			compiled.importedSymbols = append(compiled.importedSymbols, nativeValueImportResolution(imp.Path, desc.Name, desc.Name, desc, true))
		}
		return true
	}
	if callableName, ok := capabilityCallableName(capDesc, imp.Name); ok {
		for _, cd := range capDesc.Callables {
			if cd.Name == imp.Name {
				reDesc := schema.CloneCallableDesc(cd)
				reDesc.Name = imp.Name
				compiled.exportedCallables = append(compiled.exportedCallables, reDesc)
				break
			}
		}
		compiled.importedSymbols = append(compiled.importedSymbols, nativeImportResolutionWithTarget(path, imp.Name, imp.Name, imp.Path, imp.Path+"."+callableName, true))
		return true
	}
	if obj, ok := s.frontend.binding.FindCapabilityObject(imp.Path, imp.Name); ok {
		reObj := schema.CloneObjectDesc(obj)
		reObj.Name = imp.Name
		compiled.exportedObjects = append(compiled.exportedObjects, reObj)
		compiled.importedTypes = append(compiled.importedTypes, nativeStructImportResolution(imp.Path, imp.Name, imp.Name, obj.Name, &obj, true))
		return true
	}
	if iface, ok := s.frontend.binding.FindCapabilityInterface(imp.Path, imp.Name); ok {
		reIface := schema.CloneInterfaceDesc(iface)
		reIface.Name = imp.Name
		compiled.exportedInterfaces = append(compiled.exportedInterfaces, reIface)
		compiled.importedTypes = append(compiled.importedTypes, nativeInterfaceImportResolution(imp.Path, imp.Name, imp.Name, iface, true))
		if desc, _, valueOK := s.frontend.binding.FindCapabilityValue(imp.Path, imp.Name); valueOK {
			compiled.importedSymbols = append(compiled.importedSymbols, nativeValueImportResolution(imp.Path, imp.Name, imp.Name, desc, true))
		}
		return true
	}
	if td, ok := s.frontend.binding.FindCapabilityTypeAlias(imp.Path, imp.Name); ok {
		compiled.exportedTypes = append(compiled.exportedTypes, ExportedTypeMetadata{Name: imp.Name, Type: exportedTypeName(td), TypeDesc: td})
		compiled.importedTypes = append(compiled.importedTypes, nativeTypeAliasImportResolution(imp.Path, imp.Name, imp.Name, exportedTypeName(td), td, true))
		return true
	}
	if desc, _, ok := s.frontend.binding.FindCapabilityValue(imp.Path, imp.Name); ok {
		compiled.exportedVariables = append(compiled.exportedVariables, ExportedVariableMetadata{Name: imp.Name, Type: exportedTypeName(desc.Type)})
		compiled.importedSymbols = append(compiled.importedSymbols, nativeValueImportResolution(imp.Path, imp.Name, imp.Name, desc, true))
		return true
	}
	return false
}

func (s *moduleLinkState) resolveCompiledModule(path string) (compiledModule, error) {
	if err := validateModulePath(path); err != nil {
		return compiledModule{}, err
	}
	if mod, ok := s.visited[path]; ok {
		return mod, nil
	}
	if s.stack[path] {
		// Cyclic import detected. Return a type-skeleton module containing only exported
		// struct/type names without field resolution. This allows compile-time type imports
		// across cyclic modules while still rejecting runtime symbol cyclic imports.
		mod, err := s.typeSkeletonModule(path)
		if err != nil {
			return compiledModule{}, err
		}
		s.visited[path] = mod
		return mod, nil
	}
	// Check frontend-level module cache to avoid re-parsing across LoadSource calls.
	if s.cache != nil {
		if mod, ok := s.cache[path]; ok {
			s.visited[path] = mod
			return mod, nil
		}
	}
	if s.frontend == nil || s.frontend.moduleResolver == nil {
		return compiledModule{}, moduleLinkError("missing_module_resolver", "frontend/link/import", fmt.Sprintf("no module resolver configured for %q", path))
	}
	source, err := s.frontend.moduleResolver.ResolveModule(path)
	if err != nil {
		return compiledModule{}, moduleLinkError("module_not_found", "frontend/link/import", err.Error())
	}
	s.stack[path] = true
	prog, err := parseModule(source)
	if err != nil {
		delete(s.stack, path)
		return compiledModule{}, err
	}
	// Pre-inject type information from already-resolved source modules so that
	// local type annotations referencing imported structs/types resolve correctly.
	importedTypes := s.buildImportedTypesForProgram(prog)
	compiled, err := compiledDeclarationsFromProgramWithImports(prog, importedTypes)
	if err != nil {
		delete(s.stack, path)
		return compiledModule{}, err
	}
	// Resolve re-exports: imports with ReExport=true pull exports from source modules.
	for _, imp := range prog.imports {
		if !imp.ReExport {
			continue
		}
		// Record edge before resolution so partial graph is available on error.
		s.graph.addEdge(path, imp.Path)
		if s.appendNativeReExport(path, imp, &compiled) {
			continue
		}
		sourceMod, err := s.resolveCompiledModule(imp.Path)
		if err != nil {
			delete(s.stack, path)
			return compiledModule{}, err
		}
		compiled.linkedModules = appendModuleOnceLinked(compiled.linkedModules, imp.Path, sourceMod.program)
		// Wildcard re-export: copy all exports from source module.
		if imp.Name == "*" {
			if sourceMod.program == nil {
				delete(s.stack, path)
				return compiledModule{}, moduleLinkError("cyclic_import", "frontend/link/import", fmt.Sprintf("cyclic import detected involving %q", imp.Path))
			}
			for _, desc := range sourceMod.compiled.ExportedCallables() {
				reDesc := schema.CloneCallableDesc(desc)
				compiled.exportedCallables = append(compiled.exportedCallables, reDesc)
			}
			for _, variable := range sourceMod.compiled.ExportedVariables() {
				compiled.exportedVariables = append(compiled.exportedVariables, ExportedVariableMetadata{Name: variable.Name, Type: variable.Type})
			}
			for _, obj := range sourceMod.compiled.ExportedObjects() {
				reObj := schema.CloneObjectDesc(obj)
				compiled.exportedObjects = append(compiled.exportedObjects, reObj)
			}
			for _, iface := range sourceMod.compiled.ExportedInterfaces() {
				reIface := schema.CloneInterfaceDesc(iface)
				compiled.exportedInterfaces = append(compiled.exportedInterfaces, reIface)
			}
			for _, enumDesc := range sourceMod.compiled.ExportedEnums() {
				reEnum := schema.CloneEnumDesc(enumDesc)
				compiled.exportedEnums = append(compiled.exportedEnums, reEnum)
			}
			for _, typeMeta := range sourceMod.compiled.ExportedTypes() {
				compiled.exportedTypes = append(compiled.exportedTypes, ExportedTypeMetadata{Name: typeMeta.Name, Type: typeMeta.Type, TypeDesc: typeMeta.TypeDesc})
			}
			continue
		}
		if desc, ok := sourceMod.compiled.LookupExportedCallable(imp.Name); ok {
			reDesc := schema.CloneCallableDesc(desc)
			reDesc.Name = imp.Name
			compiled.exportedCallables = append(compiled.exportedCallables, reDesc)
			compiled.importedSymbols, _ = appendForwardedNativeImport(compiled.importedSymbols, sourceMod.compiled, imp.Name, imp.Name, true)
			continue
		}
		if variable, ok := sourceMod.compiled.LookupExportedVariable(imp.Name); ok {
			compiled.exportedVariables = append(compiled.exportedVariables, ExportedVariableMetadata{Name: imp.Name, Type: variable.Type})
			continue
		}
		if obj, ok := sourceMod.compiled.LookupExportedObject(imp.Name); ok {
			reObj := schema.CloneObjectDesc(obj)
			reObj.Name = imp.Name
			compiled.exportedObjects = append(compiled.exportedObjects, reObj)
			compiled.importedTypes = append(compiled.importedTypes, structImportResolution(imp.Path, imp.Name, imp.Name, obj.Name, &obj, true))
			continue
		}
		if iface, ok := sourceMod.compiled.LookupExportedInterface(imp.Name); ok {
			reIface := schema.CloneInterfaceDesc(iface)
			reIface.Name = imp.Name
			compiled.exportedInterfaces = append(compiled.exportedInterfaces, reIface)
			compiled.importedTypes = append(compiled.importedTypes, interfaceImportResolution(imp.Path, imp.Name, imp.Name, iface, true))
			continue
		}
		if enumDesc, ok := sourceMod.compiled.LookupExportedEnum(imp.Name); ok {
			reEnum := schema.CloneEnumDesc(enumDesc)
			reEnum.Name = imp.Name
			compiled.exportedEnums = append(compiled.exportedEnums, reEnum)
			continue
		}
		if typeMeta, ok := sourceMod.compiled.LookupExportedType(imp.Name); ok {
			compiled.exportedTypes = append(compiled.exportedTypes, ExportedTypeMetadata{Name: imp.Name, Type: typeMeta.Type, TypeDesc: typeMeta.TypeDesc})
			continue
		}
		delete(s.stack, path)
		// If the source module resolved as a cyclic skeleton, the lookup failure is due to the cycle.
		if sourceMod.program == nil {
			return compiledModule{}, moduleLinkError("cyclic_import", "frontend/link/import", fmt.Sprintf("cyclic import detected involving %q", imp.Path))
		}
		return compiledModule{}, moduleLinkError("missing_module_export", "frontend/link/import", fmt.Sprintf("module %q does not export %q", imp.Path, imp.Name))
	}
	compiled.callables = renameCompiledDeclarationsForModule(path, compiled).callables
	mod := compiledModule{path: path, program: prog, compiled: compiled}
	delete(s.stack, path)
	s.visited[path] = mod
	if s.cache != nil {
		s.cache[path] = mod
	}
	return mod, nil
}

// buildImportedTypesForProgram gathers compile-time type metadata from already-resolved
// source modules so that local type annotations can reference imported structs/types.
func (s *moduleLinkState) buildImportedTypesForProgram(prog *program) []ImportedTypeMetadata {
	var importedTypes []ImportedTypeMetadata
	if prog == nil {
		return importedTypes
	}
	for _, imp := range prog.imports {
		if imp.ReExport {
			continue
		}
		var sourceMod compiledModule
		var ok bool
		if s.visited != nil {
			sourceMod, ok = s.visited[imp.Path]
		}
		if !ok && s.cache != nil {
			sourceMod, ok = s.cache[imp.Path]
		}
		if !ok {
			continue
		}
		if obj, ok := sourceMod.compiled.LookupExportedObject(imp.Name); ok {
			importedTypes = append(importedTypes, structImportResolution(imp.Path, imp.Name, imp.Name, obj.Name, &obj, false))
			continue
		}
		if typeMeta, ok := sourceMod.compiled.LookupExportedType(imp.Name); ok {
			importedTypes = append(importedTypes, typeAliasImportResolution(imp.Path, imp.Name, imp.Name, typeMeta.Type, typeMeta.TypeDesc, false))
		}
	}
	return importedTypes
}

// typeSkeletonModule creates a minimal compiledModule containing only exported struct
// and type-alias names, without field-type resolution. Used for cyclic type imports.
func (s *moduleLinkState) typeSkeletonModule(path string) (compiledModule, error) {
	if err := validateModulePath(path); err != nil {
		return compiledModule{}, err
	}
	if s.frontend == nil || s.frontend.moduleResolver == nil {
		return compiledModule{}, moduleLinkError("missing_module_resolver", "frontend/link/import", fmt.Sprintf("no module resolver configured for %q", path))
	}
	source, err := s.frontend.moduleResolver.ResolveModule(path)
	if err != nil {
		return compiledModule{}, moduleLinkError("module_not_found", "frontend/link/import", err.Error())
	}
	prog, err := parseModule(source)
	if err != nil {
		return compiledModule{}, err
	}
	compiled := CompiledDeclarations{}
	for _, stmt := range prog.Stmts {
		switch st := stmt.(type) {
		case *structStmt:
			if st.Exported {
				compiled.exportedObjects = append(compiled.exportedObjects, schema.ObjectDesc{Kind: schema.TypeKindStruct, Name: st.Name.Value})
			}
		case *interfaceStmt:
			if st.Exported {
				compiled.exportedInterfaces = append(compiled.exportedInterfaces, schema.InterfaceDesc{Name: st.Name.Value})
			}
		case *enumStmt:
			if st.Exported {
				compiled.exportedEnums = append(compiled.exportedEnums, extractEnumDesc(st))
			}
		case *typeAliasStmt:
			if st.Exported {
				compiled.exportedTypes = append(compiled.exportedTypes, ExportedTypeMetadata{Name: st.Name.Value})
			}
		}
	}
	return compiledModule{path: path, program: nil, compiled: compiled}, nil
}

func renameCompiledDeclarationsForModule(path string, compiled CompiledDeclarations) CompiledDeclarations {
	qualifiedCallables := make([]CompiledCallableDeclaration, len(compiled.callables))
	for i, callable := range compiled.callables {
		qualified := callable
		qualified.Desc.Name = qualifiedModuleCallableName(path, callable.Desc.Name)
		qualifiedCallables[i] = qualified
	}
	compiled.callables = qualifiedCallables
	return compiled
}

func resolveReExportTargetPath(mod compiledModule, name string) string {
	if mod.program == nil {
		return mod.path
	}
	var wildcardPath string
	for _, imp := range mod.program.imports {
		if !imp.ReExport {
			continue
		}
		if imp.Name == name {
			return imp.Path
		}
		if imp.Name == "*" {
			wildcardPath = imp.Path
		}
	}
	if wildcardPath != "" {
		return wildcardPath
	}
	return mod.path
}

func topLevelDeclarationNames(prog *program) map[string]bool {
	names := make(map[string]bool)
	if prog == nil {
		return names
	}
	for _, stmt := range prog.Stmts {
		switch s := stmt.(type) {
		case *funStmt:
			names[s.Name.Value] = true
		case *varStmt:
			names[s.Name.Value] = true
		case *structStmt:
			names[s.Name.Value] = true
		case *classStmt:
			names[s.Name.Value] = true
		case *interfaceStmt:
			names[s.Name.Value] = true
		case *typeAliasStmt:
			names[s.Name.Value] = true
		case *enumStmt:
			names[s.Name.Value] = true
		}
	}
	return names
}

func containsImportedLocalName(imports []ImportedSymbolMetadata, name string) bool {
	for _, imp := range imports {
		if imp.LocalName == name {
			return true
		}
	}
	return false
}

func containsImportedTypeLocalName(imports []ImportedTypeMetadata, name string) bool {
	for _, imp := range imports {
		if imp.LocalName == name {
			return true
		}
	}
	return false
}

func appendModuleOnce(modules []linkedModule, path string, prog *program, compiled CompiledDeclarations) []linkedModule {
	for _, existing := range modules {
		if existing.path == path {
			return modules
		}
	}
	return append(modules, linkedModule{path: path, program: prog, compiled: compiled})
}

func appendModuleOnceLinked(modules []LinkedModuleMetadata, path string, prog *program) []LinkedModuleMetadata {
	for _, existing := range modules {
		if existing.Path == path {
			return modules
		}
	}
	return append(modules, LinkedModuleMetadata{Path: path, Program: prog})
}

func cloneInterfaceDescPtr(desc *schema.InterfaceDesc) *schema.InterfaceDesc {
	if desc == nil {
		return nil
	}
	cloned := schema.CloneInterfaceDesc(*desc)
	return &cloned
}

func findCapabilityDesc(descs []binding.CapabilityDesc, name string) (binding.CapabilityDesc, bool) {
	for _, desc := range descs {
		if desc.Name == name {
			return desc, true
		}
	}
	return binding.CapabilityDesc{}, false
}

func capabilityCallableName(desc binding.CapabilityDesc, name string) (string, bool) {
	for _, callable := range desc.Callables {
		if callable.Name == name {
			return callable.Name, true
		}
	}
	return "", false
}

func qualifiedModuleCallableName(path, name string) string {
	return "__module_" + sanitizeModulePath(path) + "__" + name
}

func qualifiedModuleGlobalName(path, name string) string {
	return "__module_global_" + sanitizeModulePath(path) + "__" + name
}

func sanitizeModulePath(path string) string {
	replacer := strings.NewReplacer("/", "_", "\\", "_", ".", "_", "-", "_")
	return replacer.Replace(path)
}

func validateModulePath(path string) error {
	if path == "" {
		return moduleLinkError("invalid_module_path", "frontend/link/import", "module path cannot be empty")
	}
	// Reject absolute paths.
	if path[0] == '/' || path[0] == '\\' {
		return moduleLinkError("invalid_module_path", "frontend/link/import", fmt.Sprintf("module path %q must not be absolute", path))
	}
	// Reject directory traversal.
	if strings.Contains(path, "..") {
		return moduleLinkError("invalid_module_path", "frontend/link/import", fmt.Sprintf("module path %q contains invalid '..' traversal", path))
	}
	// Reject consecutive slashes and trailing slash.
	if strings.Contains(path, "//") {
		return moduleLinkError("invalid_module_path", "frontend/link/import", fmt.Sprintf("module path %q contains consecutive slashes", path))
	}
	if strings.HasSuffix(path, "/") || strings.HasSuffix(path, "\\") {
		return moduleLinkError("invalid_module_path", "frontend/link/import", fmt.Sprintf("module path %q must not end with a slash", path))
	}
	// Only allow alphanumerics, underscore, hyphen, forward slash, dot, and at-sign.
	for i, r := range path {
		if r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			continue
		}
		switch r {
		case '_', '-', '/', '.', '@':
			continue
		}
		return moduleLinkError("invalid_module_path", "frontend/link/import", fmt.Sprintf("module path %q contains invalid character %q at position %d", path, r, i))
	}
	return nil
}

func moduleLinkError(code, path, message string) error {
	return &schemaValidationError{code: code, message: message, category: diagnostics.CategoryLoad, path: path}
}
