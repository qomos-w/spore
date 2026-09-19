package frontend

import "github.com/qomos-w/spore/diagnostics"

// CompiledDeclarationsFromProgramForTest exposes compiledDeclarationsFromProgram to tests in sibling packages.
func CompiledDeclarationsFromProgramForTest(prog *Program) (CompiledDeclarations, error) {
	return compiledDeclarationsFromProgram(prog)
}

// ParseModuleForTestWithDiagnostics exposes parse diagnostics to tests in sibling packages.
func ParseModuleForTestWithDiagnostics(source string) (*Program, error, []diagnostics.Descriptor) {
	l := newLexer(source)
	p := newParser(l, source)
	prog, err := p.parse()
	if err == nil {
		return prog, nil, nil
	}
	multi, ok := err.(*diagnostics.MultiError)
	if !ok {
		return prog, err, nil
	}
	return prog, err, multi.Diagnostics()
}
