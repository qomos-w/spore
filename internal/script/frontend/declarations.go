package frontend

import "github.com/qomos-w/spore/schema"

// ImportMetadata records a first-phase import declaration.
type ImportMetadata struct {
	Name      string
	Alias     string
	Path      string
	LocalName string
	Kind      string
	ReExport  bool
}

// ExportedVariableMetadata is the minimal exported variable metadata shape.
type ExportedVariableMetadata struct {
	Name string
	Type string
}

// ExportedTypeMetadata is the minimal exported type alias metadata shape.
type ExportedTypeMetadata struct {
	Name     string
	Type     string
	TypeDesc schema.TypeDesc
}

// ImportedTypeMetadata records a linker-resolved compile-time imported type/object.
type ImportedTypeMetadata struct {
	Path             string
	Name             string
	LocalName        string
	Kind             string
	Type             string
	ObjectDesc       *schema.ObjectDesc
	InterfaceDesc    *schema.InterfaceDesc
	EnumDesc         *schema.EnumDesc
	TypeDesc         schema.TypeDesc
	Resolved         bool
	SourceKind       string
	ReExport         bool
	TargetModulePath string
	Explanation      string
}

// ImportedSymbolMetadata records a linker-resolved imported symbol.
type ImportedSymbolMetadata struct {
	Path             string
	Name             string
	LocalName        string
	Kind             string
	TargetName       string
	Type             string
	Resolved         bool
	SourceKind       string
	ReExport         bool
	TargetModulePath string
	Explanation      string
}

// ModuleExportCounts summarizes exported symbol counts for a module.
type ModuleExportCounts struct {
	Callables  int
	Variables  int
	Objects    int
	Interfaces int
	Types      int
	Enums      int
}

// ModuleImportResolution is the LLM-friendly explanation for one resolved import.
type ModuleImportResolution struct {
	Path             string
	RequestedName    string
	LocalName        string
	Kind             string
	TargetName       string
	Resolved         bool
	SourceKind       string
	ReExport         bool
	TargetModulePath string
	Explanation      string
}

// ModuleImportTraceHop records one hop in an import/re-export chain.
type ModuleImportTraceHop struct {
	ModulePath       string
	RequestedName    string
	LocalName        string
	Kind             string
	TargetName       string
	SourceKind       string
	ReExport         bool
	TargetModulePath string
	Explanation      string
}

// ModuleImportTrace is the structured trace for a resolved import.
type ModuleImportTrace struct {
	ModulePath       string
	RequestedName    string
	LocalName        string
	Kind             string
	TargetName       string
	SourceKind       string
	TargetModulePath string
	Resolved         bool
	Hops             []ModuleImportTraceHop
	Explanation      string
}

// ModuleImporter records one module or root script importing a symbol.
type ModuleImporter struct {
	ImporterPath     string
	RequestedName    string
	LocalName        string
	Kind             string
	SourceKind       string
	ReExport         bool
	TargetModulePath string
}

// ModuleSummary is the LLM-friendly snapshot for a resolved module.
type ModuleSummary struct {
	Path                string
	Dependencies        []string
	ReverseDependencies []string
	HasCycle            bool
	ExportCounts        ModuleExportCounts
	Imports             []ModuleImportResolution
}

// ModuleDiagnostic is the structured snapshot of the last module linking failure.
type ModuleDiagnostic struct {
	Code             string
	Category         string
	Path             string
	Message          string
	Hint             string
	Expected         string
	Actual           string
	ImportName       string
	LocalName        string
	SourceModulePath string
	TargetModulePath string
	SourceKind       string
	Phase            string
	AvailableExports []string
	CyclePath        []string
}

// DeclarationMetadata is the internal top-level declaration discovery surface.
type DeclarationMetadata struct {
	Imports            []ImportMetadata
	ImportedSymbols    []ImportedSymbolMetadata
	ImportedTypes      []ImportedTypeMetadata
	Callables          []schema.CallableDesc
	ExportedCallables  []schema.CallableDesc
	ExportedVariables  []ExportedVariableMetadata
	ExportedObjects    []schema.ObjectDesc
	ExportedInterfaces []schema.InterfaceDesc
	ExportedTypes      []ExportedTypeMetadata
	ExportedEnums      []schema.EnumDesc
	Objects            []schema.ObjectDesc
	Interfaces         []schema.InterfaceDesc
	Enums              []schema.EnumDesc
	Errors             []error
}

func (f *Frontend) Declarations() DeclarationMetadata {
	if f == nil {
		return DeclarationMetadata{}
	}
	result := DeclarationMetadata{
		Imports:           append([]ImportMetadata(nil), f.imports...),
		ImportedSymbols:   append([]ImportedSymbolMetadata(nil), f.importedSymbols...),
		ImportedTypes:     append([]ImportedTypeMetadata(nil), f.importedTypes...),
		Callables:         make([]schema.CallableDesc, 0, len(f.callables)),
		ExportedCallables: make([]schema.CallableDesc, len(f.exports)),
		Objects:           make([]schema.ObjectDesc, 0, len(f.objects)),
		Interfaces:        make([]schema.InterfaceDesc, len(f.interfaces)),
		Enums:             make([]schema.EnumDesc, len(f.enums)),
	}
	for _, entry := range f.callables {
		result.Callables = append(result.Callables, schema.CloneCallableDesc(entry.desc))
	}
	for i, desc := range f.exports {
		result.ExportedCallables[i] = schema.CloneCallableDesc(desc)
	}
	for _, entry := range f.objects {
		result.Objects = append(result.Objects, schema.CloneObjectDesc(entry.desc))
	}
	for i, entry := range f.interfaces {
		result.Interfaces[i] = schema.CloneInterfaceDesc(entry)
	}
	for i, entry := range f.enums {
		result.Enums[i] = schema.CloneEnumDesc(entry)
	}
	return result
}
