package frontend

// DeclarationCollector collects top-level declaration information from supported sources.
type DeclarationCollector struct{}

func (DeclarationCollector) CollectSource(source string) (CompiledDeclarations, error) {
	return compileDeclarations(source)
}

func (DeclarationCollector) CollectExportInfo(exports []ExportedFunctionInfo) (CompiledDeclarations, error) {
	return compiledDeclarationsFromExportInfo(exports)
}

func collectDeclarations(source string) (CompiledDeclarations, error) {
	return DeclarationCollector{}.CollectSource(source)
}
