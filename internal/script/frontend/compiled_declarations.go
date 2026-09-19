package frontend

import (
	"fmt"
	"strings"

	"github.com/qomos-w/spore/diagnostics"
	"github.com/qomos-w/spore/schema"
)

// ExportedParameterMetadata is the internal exported-callable parameter metadata.
type ExportedParameterMetadata struct {
	Name string
	Type string
}

// ExportedCallableMetadata is the internal exported-callable discovery metadata.
type ExportedCallableMetadata struct {
	Name       string
	Parameters []ExportedParameterMetadata
	ReturnType string
}

// ExportedParameterInfo is the minimal exported parameter metadata shape.
type ExportedParameterInfo struct {
	Name string
	Type string
}

// ExportedFunctionInfo is the minimal exported function metadata shape.
type ExportedFunctionInfo struct {
	Name       string
	Parameters []ExportedParameterInfo
	ReturnType string
}

// CompiledCallableDeclaration is the internal compiled callable declaration payload.
type CompiledCallableDeclaration struct {
	Desc     schema.CallableDesc
	Exported bool
	Source   *funStmt
}

// CompiledObjectDeclaration is the internal compiled object declaration payload.
type CompiledObjectDeclaration struct {
	Desc schema.ObjectDesc
}

type LinkedModuleMetadata struct {
	Path    string
	Program *Program
}

// CompiledDeclarations is the internal top-level declaration discovery surface.
type CompiledDeclarations struct {
	packageName        string
	imports            []ImportMetadata
	importedSymbols    []ImportedSymbolMetadata
	importedTypes      []ImportedTypeMetadata
	linkedModules      []LinkedModuleMetadata
	callables          []CompiledCallableDeclaration
	exportedCallables  []schema.CallableDesc
	exportedVariables  []ExportedVariableMetadata
	exportedObjects    []schema.ObjectDesc
	exportedInterfaces []schema.InterfaceDesc
	exportedTypes      []ExportedTypeMetadata
	exportedEnums      []schema.EnumDesc
	exportedMeta       []ExportedCallableMetadata
	objects            []CompiledObjectDeclaration
	interfaces         []schema.InterfaceDesc
	enums              []schema.EnumDesc
}

// PackageName returns the declared package name, or an empty string if no
// package declaration is present.
func (d CompiledDeclarations) PackageName() string {
	return d.packageName
}

func (d CompiledDeclarations) Imports() []ImportMetadata {
	if len(d.imports) == 0 {
		return nil
	}
	result := make([]ImportMetadata, len(d.imports))
	copy(result, d.imports)
	return result
}

func (d CompiledDeclarations) ImportedSymbols() []ImportedSymbolMetadata {
	if len(d.importedSymbols) == 0 {
		return nil
	}
	result := make([]ImportedSymbolMetadata, len(d.importedSymbols))
	copy(result, d.importedSymbols)
	return result
}

func (d CompiledDeclarations) ImportedTypes() []ImportedTypeMetadata {
	if len(d.importedTypes) == 0 {
		return nil
	}
	result := make([]ImportedTypeMetadata, len(d.importedTypes))
	copy(result, d.importedTypes)
	return result
}

func (d CompiledDeclarations) LinkedModules() ([]LinkedModuleMetadata, bool) {
	if len(d.linkedModules) == 0 {
		return nil, false
	}
	result := make([]LinkedModuleMetadata, len(d.linkedModules))
	copy(result, d.linkedModules)
	return result, true
}

func (d CompiledDeclarations) Callables() []schema.CallableDesc {
	if len(d.callables) == 0 {
		return nil
	}
	result := make([]schema.CallableDesc, len(d.callables))
	for i, decl := range d.callables {
		result[i] = schema.CloneCallableDesc(decl.Desc)
	}
	return result
}

func (d CompiledDeclarations) CallableDeclarations() []CompiledCallableDeclaration {
	if len(d.callables) == 0 {
		return nil
	}
	result := make([]CompiledCallableDeclaration, len(d.callables))
	for i, decl := range d.callables {
		result[i] = CompiledCallableDeclaration{
			Desc:     schema.CloneCallableDesc(decl.Desc),
			Exported: decl.Exported,
			Source:   decl.Source,
		}
	}
	return result
}

func (d CompiledDeclarations) ExportedCallables() []schema.CallableDesc {
	if len(d.exportedCallables) == 0 {
		return nil
	}
	result := make([]schema.CallableDesc, len(d.exportedCallables))
	for i, desc := range d.exportedCallables {
		result[i] = schema.CloneCallableDesc(desc)
	}
	return result
}

func (d CompiledDeclarations) LookupExportedCallable(name string) (schema.CallableDesc, bool) {
	for _, desc := range d.exportedCallables {
		if desc.Name == name {
			return schema.CloneCallableDesc(desc), true
		}
	}
	return schema.CallableDesc{}, false
}

func (d CompiledDeclarations) ExportedVariables() []ExportedVariableMetadata {
	if len(d.exportedVariables) == 0 {
		return nil
	}
	result := make([]ExportedVariableMetadata, len(d.exportedVariables))
	copy(result, d.exportedVariables)
	return result
}

func (d CompiledDeclarations) LookupExportedVariable(name string) (ExportedVariableMetadata, bool) {
	for _, meta := range d.exportedVariables {
		if meta.Name == name {
			return meta, true
		}
	}
	return ExportedVariableMetadata{}, false
}

func (d CompiledDeclarations) ExportedObjects() []schema.ObjectDesc {
	if len(d.exportedObjects) == 0 {
		return nil
	}
	result := make([]schema.ObjectDesc, len(d.exportedObjects))
	for i, desc := range d.exportedObjects {
		result[i] = schema.CloneObjectDesc(desc)
	}
	return result
}

func (d CompiledDeclarations) ExportedInterfaces() []schema.InterfaceDesc {
	if len(d.exportedInterfaces) == 0 {
		return nil
	}
	result := make([]schema.InterfaceDesc, len(d.exportedInterfaces))
	for i, desc := range d.exportedInterfaces {
		result[i] = schema.CloneInterfaceDesc(desc)
	}
	return result
}

func (d CompiledDeclarations) ExportedEnums() []schema.EnumDesc {
	if len(d.exportedEnums) == 0 {
		return nil
	}
	result := make([]schema.EnumDesc, len(d.exportedEnums))
	for i, desc := range d.exportedEnums {
		result[i] = schema.CloneEnumDesc(desc)
	}
	return result
}

func (d CompiledDeclarations) LookupExportedEnum(name string) (schema.EnumDesc, bool) {
	for _, desc := range d.exportedEnums {
		if desc.Name == name {
			return schema.CloneEnumDesc(desc), true
		}
	}
	return schema.EnumDesc{}, false
}

func (d CompiledDeclarations) LookupExportedObject(name string) (schema.ObjectDesc, bool) {
	for _, desc := range d.exportedObjects {
		if desc.Name == name {
			return schema.CloneObjectDesc(desc), true
		}
	}
	return schema.ObjectDesc{}, false
}

func (d CompiledDeclarations) LookupExportedInterface(name string) (schema.InterfaceDesc, bool) {
	for _, desc := range d.exportedInterfaces {
		if desc.Name == name {
			return schema.CloneInterfaceDesc(desc), true
		}
	}
	return schema.InterfaceDesc{}, false
}

func (d CompiledDeclarations) ExportedTypes() []ExportedTypeMetadata {
	if len(d.exportedTypes) == 0 {
		return nil
	}
	result := make([]ExportedTypeMetadata, len(d.exportedTypes))
	copy(result, d.exportedTypes)
	return result
}

func (d CompiledDeclarations) LookupExportedType(name string) (ExportedTypeMetadata, bool) {
	for _, meta := range d.exportedTypes {
		if meta.Name == name {
			return meta, true
		}
	}
	return ExportedTypeMetadata{}, false
}

func (d CompiledDeclarations) ExportedMetadata() []ExportedCallableMetadata {
	if len(d.exportedMeta) == 0 {
		return nil
	}
	result := make([]ExportedCallableMetadata, len(d.exportedMeta))
	for i, meta := range d.exportedMeta {
		result[i] = cloneExportedCallableMetadata(meta)
	}
	return result
}

func (d CompiledDeclarations) LookupExportedMetadata(name string) (ExportedCallableMetadata, bool) {
	for _, meta := range d.exportedMeta {
		if meta.Name == name {
			return cloneExportedCallableMetadata(meta), true
		}
	}
	return ExportedCallableMetadata{}, false
}

func (d CompiledDeclarations) Objects() []schema.ObjectDesc {
	if len(d.objects) == 0 {
		return nil
	}
	result := make([]schema.ObjectDesc, len(d.objects))
	for i, decl := range d.objects {
		result[i] = schema.CloneObjectDesc(decl.Desc)
	}
	return result
}

func (d CompiledDeclarations) ObjectDeclarations() []CompiledObjectDeclaration {
	if len(d.objects) == 0 {
		return nil
	}
	result := make([]CompiledObjectDeclaration, len(d.objects))
	for i, decl := range d.objects {
		result[i] = CompiledObjectDeclaration{Desc: schema.CloneObjectDesc(decl.Desc)}
	}
	return result
}

func (d CompiledDeclarations) InterfaceDeclarations() []schema.InterfaceDesc {
	if len(d.interfaces) == 0 {
		return nil
	}
	result := make([]schema.InterfaceDesc, len(d.interfaces))
	for i, desc := range d.interfaces {
		result[i] = schema.CloneInterfaceDesc(desc)
	}
	return result
}

func (d CompiledDeclarations) EnumDeclarations() []schema.EnumDesc {
	if len(d.enums) == 0 {
		return nil
	}
	result := make([]schema.EnumDesc, len(d.enums))
	for i, desc := range d.enums {
		result[i] = schema.CloneEnumDesc(desc)
	}
	return result
}

func (f *Frontend) CompiledDeclarations() CompiledDeclarations {
	if f == nil {
		return CompiledDeclarations{}
	}
	meta := f.Declarations()
	compiled := compiledDeclarationsFromMetadata(meta)
	compiled.packageName = f.packageName
	return compiled
}

func compileDeclarations(source string) (CompiledDeclarations, error) {
	prog, err := parseModule(source)
	if err != nil {
		return CompiledDeclarations{}, err
	}
	return compiledDeclarationsFromProgram(prog)
}

func compiledDeclarationsFromExportInfo(exports []ExportedFunctionInfo) (CompiledDeclarations, error) {
	compiled := CompiledDeclarations{
		callables:         make([]CompiledCallableDeclaration, len(exports)),
		exportedCallables: make([]schema.CallableDesc, len(exports)),
		exportedMeta:      make([]ExportedCallableMetadata, len(exports)),
	}
	for i, exported := range exports {
		desc, meta, err := exportedFunctionInfoToDesc(exported)
		if err != nil {
			return CompiledDeclarations{}, err
		}
		compiled.callables[i] = CompiledCallableDeclaration{Desc: desc, Exported: true}
		compiled.exportedCallables[i] = schema.CloneCallableDesc(desc)
		compiled.exportedMeta[i] = meta
	}
	return compiled, nil
}

func exportedFunctionInfoToDesc(info ExportedFunctionInfo) (schema.CallableDesc, ExportedCallableMetadata, error) {
	desc := schema.CallableDesc{Name: info.Name, Mode: schema.CallableModeUnary}
	meta := ExportedCallableMetadata{Name: info.Name, ReturnType: info.ReturnType, Parameters: make([]ExportedParameterMetadata, len(info.Parameters))}
	for i, param := range info.Parameters {
		typeDesc, err := typeDescFromExportedType(param.Type)
		if err != nil {
			return schema.CallableDesc{}, ExportedCallableMetadata{}, err
		}
		desc.Parameters = append(desc.Parameters, schema.ParameterDesc{Name: param.Name, Type: typeDesc})
		meta.Parameters[i] = ExportedParameterMetadata{Name: param.Name, Type: param.Type}
	}
	if info.ReturnType != "" {
		ret, err := typeDescFromExportedType(info.ReturnType)
		if err != nil {
			return schema.CallableDesc{}, ExportedCallableMetadata{}, err
		}
		desc.Returns = []schema.TypeDesc{ret}
	}
	return desc, meta, nil
}

func typeDescFromExportedType(name string) (schema.TypeDesc, error) {
	if name == "" {
		return schema.TypeDesc{}, nil
	}
	if strings.HasPrefix(name, "[]") {
		elem, err := typeDescFromExportedType(name[2:])
		if err != nil {
			return schema.TypeDesc{}, err
		}
		return schema.TypeDesc{Kind: schema.TypeKindArray, Element: &elem}, nil
	}
	if strings.HasPrefix(name, "map[") {
		close := strings.Index(name, "]")
		if close < 0 || close == 4 || close == len(name)-1 {
			return schema.TypeDesc{}, fmt.Errorf("invalid exported map type %q", name)
		}
		key, err := typeDescFromExportedType(name[4:close])
		if err != nil {
			return schema.TypeDesc{}, err
		}
		value, err := typeDescFromExportedType(name[close+1:])
		if err != nil {
			return schema.TypeDesc{}, err
		}
		return schema.TypeDesc{Kind: schema.TypeKindMap, Key: &key, Value: &value}, nil
	}
	if strings.HasPrefix(name, "class:") {
		className := strings.TrimPrefix(name, "class:")
		return schema.TypeDesc{Kind: schema.TypeKindClass, Name: className, ClassName: className}, nil
	}
	return schema.TypeDesc{Kind: schema.TypeKindScalar, Name: name}, nil
}

func compiledDeclarationsFromProgram(prog *program) (CompiledDeclarations, error) {
	return compiledDeclarationsFromProgramWithImports(prog, nil)
}

func compiledDeclarationsFromProgramWithImports(prog *program, importedTypes []ImportedTypeMetadata) (CompiledDeclarations, error) {
	meta := declarationsFromProgramWithImports(prog, importedTypes)
	if len(meta.Errors) > 0 {
		return CompiledDeclarations{}, diagnostics.NewMultiErrorWithFallback(meta.Errors, diagnostics.Descriptor{Category: diagnostics.CategorySchema, Path: "frontend/diagnostics"})
	}
	compiled := compiledDeclarationsFromMetadata(meta)
	if prog != nil && prog.pkg != nil {
		compiled.packageName = prog.pkg.Name
	}

	ci := 0
	for _, stmt := range prog.Stmts {
		fun, ok := stmt.(*funStmt)
		if !ok {
			continue
		}
		if ci < len(compiled.callables) {
			compiled.callables[ci].Source = fun
			compiled.callables[ci].Exported = fun.Exported
		}
		ci++
	}
	return compiled, nil
}

func compiledDeclarationsFromMetadata(meta DeclarationMetadata) CompiledDeclarations {
	compiled := CompiledDeclarations{
		imports:            append([]ImportMetadata(nil), meta.Imports...),
		importedSymbols:    append([]ImportedSymbolMetadata(nil), meta.ImportedSymbols...),
		importedTypes:      append([]ImportedTypeMetadata(nil), meta.ImportedTypes...),
		callables:          make([]CompiledCallableDeclaration, len(meta.Callables)),
		exportedCallables:  make([]schema.CallableDesc, len(meta.ExportedCallables)),
		exportedVariables:  make([]ExportedVariableMetadata, len(meta.ExportedVariables)),
		exportedObjects:    make([]schema.ObjectDesc, len(meta.ExportedObjects)),
		exportedInterfaces: make([]schema.InterfaceDesc, len(meta.ExportedInterfaces)),
		exportedTypes:      make([]ExportedTypeMetadata, len(meta.ExportedTypes)),
		exportedEnums:      make([]schema.EnumDesc, len(meta.ExportedEnums)),
		objects:            make([]CompiledObjectDeclaration, len(meta.Objects)),
		interfaces:         make([]schema.InterfaceDesc, len(meta.Interfaces)),
		enums:              make([]schema.EnumDesc, len(meta.Enums)),
		exportedMeta:       make([]ExportedCallableMetadata, len(meta.ExportedCallables)),
	}
	for i, desc := range meta.Callables {
		compiled.callables[i] = CompiledCallableDeclaration{Desc: schema.CloneCallableDesc(desc), Exported: false, Source: nil}
	}
	for i, desc := range meta.ExportedCallables {
		compiled.exportedCallables[i] = schema.CloneCallableDesc(desc)
		compiled.exportedMeta[i] = exportedCallableMetadataFromDesc(desc)
	}
	copy(compiled.exportedVariables, meta.ExportedVariables)
	for i, desc := range meta.ExportedObjects {
		compiled.exportedObjects[i] = schema.CloneObjectDesc(desc)
	}
	for i, desc := range meta.ExportedInterfaces {
		compiled.exportedInterfaces[i] = schema.CloneInterfaceDesc(desc)
	}
	for i, desc := range meta.ExportedEnums {
		compiled.exportedEnums[i] = schema.CloneEnumDesc(desc)
	}
	copy(compiled.exportedTypes, meta.ExportedTypes)
	for i, desc := range meta.Objects {
		compiled.objects[i] = CompiledObjectDeclaration{Desc: schema.CloneObjectDesc(desc)}
	}
	for i, desc := range meta.Interfaces {
		compiled.interfaces[i] = schema.CloneInterfaceDesc(desc)
	}
	for i, desc := range meta.Enums {
		compiled.enums[i] = schema.CloneEnumDesc(desc)
	}
	return compiled
}

func exportedCallableMetadataFromDesc(desc schema.CallableDesc) ExportedCallableMetadata {
	meta := ExportedCallableMetadata{
		Name:       desc.Name,
		ReturnType: exportedTypeName(firstReturn(desc.Returns)),
		Parameters: make([]ExportedParameterMetadata, len(desc.Parameters)),
	}
	for i, param := range desc.Parameters {
		meta.Parameters[i] = ExportedParameterMetadata{
			Name: param.Name,
			Type: exportedTypeName(param.Type),
		}
	}
	return meta
}

func firstReturn(returns []schema.TypeDesc) schema.TypeDesc {
	if len(returns) == 0 {
		return schema.TypeDesc{}
	}
	return returns[0]
}

func exportedTypeName(desc schema.TypeDesc) string {
	switch desc.Kind {
	case schema.TypeKindArray:
		if desc.Element == nil {
			return "array"
		}
		return "[]" + exportedTypeName(*desc.Element)
	case schema.TypeKindMap:
		if desc.Key == nil || desc.Value == nil {
			return "map"
		}
		return "map[" + exportedTypeName(*desc.Key) + "]" + exportedTypeName(*desc.Value)
	case schema.TypeKindClass:
		if desc.ClassName != "" {
			return "class:" + desc.ClassName
		}
		return desc.Name
	case schema.TypeKindStruct:
		if desc.ClassName != "" {
			return desc.ClassName
		}
		return desc.Name
	default:
		return desc.Name
	}
}

func cloneExportedCallableMetadata(meta ExportedCallableMetadata) ExportedCallableMetadata {
	cloned := ExportedCallableMetadata{Name: meta.Name, ReturnType: meta.ReturnType}
	if len(meta.Parameters) == 0 {
		return cloned
	}
	cloned.Parameters = make([]ExportedParameterMetadata, len(meta.Parameters))
	copy(cloned.Parameters, meta.Parameters)
	return cloned
}
