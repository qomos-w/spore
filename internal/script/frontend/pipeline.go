package frontend

import (
	"fmt"

	"github.com/qomos-w/spore/schema"
)

// parseModule runs the full lexer → parser → AST pipeline on source text.
func parseModule(source string) (*program, error) {
	l := newLexer(source)
	p := newParser(l, source)
	return p.parse()
}

func importsFromProgram(prog *program) []ImportMetadata {
	if prog == nil || len(prog.imports) == 0 {
		return nil
	}
	imports := make([]ImportMetadata, len(prog.imports))
	for i, imp := range prog.imports {
		imports[i] = ImportMetadata{Name: imp.Name, Alias: imp.Alias, Path: imp.Path, LocalName: importLocalName(imp.Name, imp.Alias), Kind: importKindLabel(imp), ReExport: imp.ReExport}
	}
	return imports
}

// ParseModuleForTest is the exported version of parseModule for use by tests.
func ParseModuleForTest(source string) (*Program, error) {
	return parseModule(source)
}

// AnalyzeSemanticTokens returns semantic highlighting tokens for Spore source.
// It is the internal implementation behind script.Analyze().
func AnalyzeSemanticTokens(source string) ([]semanticTokenResult, error) {
	prog, err := parseModule(source)
	if prog == nil {
		// Complete parse failure — return lexer-only tokens.
		return walkForSemanticTokens(source, nil), err
	}
	return walkForSemanticTokens(source, prog), err
}

// declarationsFromProgram extracts schema descriptors from a parsed AST program.
func declarationsFromProgram(prog *program) DeclarationMetadata {
	return declarationsFromProgramWithImports(prog, nil)
}

// declarationsFromProgramWithImports extracts schema descriptors with compile-time imported types injected into type resolution.
func declarationsFromProgramWithImports(prog *program, importedTypes []ImportedTypeMetadata) DeclarationMetadata {
	meta := DeclarationMetadata{
		Imports:            importsFromProgram(prog),
		Callables:          make([]schema.CallableDesc, 0, len(prog.Stmts)),
		ExportedCallables:  make([]schema.CallableDesc, 0),
		ExportedVariables:  make([]ExportedVariableMetadata, 0),
		ExportedObjects:    make([]schema.ObjectDesc, 0),
		ExportedInterfaces: make([]schema.InterfaceDesc, 0),
		ExportedTypes:      make([]ExportedTypeMetadata, 0),
		ExportedEnums:      make([]schema.EnumDesc, 0),
		Objects:            make([]schema.ObjectDesc, 0),
		Interfaces:         make([]schema.InterfaceDesc, 0),
		Enums:              make([]schema.EnumDesc, 0),
	}
	// Build type context: struct names and class names for type resolution.
	ctx := typeContext{
		structNames:       make(map[string]bool),
		classNames:        make(map[string]bool),
		enumNames:         make(map[string]bool),
		typeAliases:       make(map[string]*typeAnnotation),
		importedTypeDescs: make(map[string]schema.TypeDesc),
	}
	// Inject compile-time imported types so local annotations can reference them.
	for _, imp := range importedTypes {
		if imp.Kind == "struct" && imp.ObjectDesc != nil {
			ctx.structNames[imp.LocalName] = true
			ctx.importedTypeDescs[imp.LocalName] = schema.TypeDesc{
				Kind:      schema.TypeKindStruct,
				Name:      imp.LocalName,
				ClassName: imp.LocalName,
			}
		} else if imp.Kind == "enum" && imp.Type != "" {
			ctx.enumNames[imp.LocalName] = true
			ctx.importedTypeDescs[imp.LocalName] = schema.TypeDesc{Kind: schema.TypeKindEnum, Name: imp.Type}
		} else if imp.Kind == "type" && imp.TypeDesc.Kind != "" {
			ctx.importedTypeDescs[imp.LocalName] = imp.TypeDesc
		}
	}
	// Best-effort: mark uppercase import names as potential structs so that
	// cross-module type annotations resolve correctly even before the source
	// module is fully resolved (e.g. in cyclic type-import scenarios).
	for _, imp := range prog.imports {
		if !imp.ReExport && len(imp.Name) > 0 && imp.Name[0] >= 'A' && imp.Name[0] <= 'Z' {
			ctx.structNames[imp.Name] = true
		}
	}
	for _, stmt := range prog.Stmts {
		switch s := stmt.(type) {
		case *structStmt:
			ctx.structNames[s.Name.Value] = true
		case *classStmt:
			ctx.classNames[s.Name.Value] = true
		case *enumStmt:
			ctx.enumNames[s.Name.Value] = true
		case *typeAliasStmt:
			if s.Alias != nil {
				ctx.typeAliases[s.Name.Value] = s.Alias
			}
		}
	}
	for _, stmt := range prog.Stmts {
		switch s := stmt.(type) {
		case *structStmt:
			meta.Objects = append(meta.Objects, extractClassDesc(s, ctx))
		case *classStmt:
			meta.Objects = append(meta.Objects, extractClassDesc(s, ctx))
		case *interfaceStmt:
			meta.Interfaces = append(meta.Interfaces, extractInterfaceDesc(s, ctx))
		case *enumStmt:
			meta.Enums = append(meta.Enums, extractEnumDesc(s))
		}
	}
	objectsByName := make(map[string]schema.ObjectDesc, len(meta.Objects))
	for _, obj := range meta.Objects {
		objectsByName[obj.Name] = obj
	}
	for _, stmt := range prog.Stmts {
		switch s := stmt.(type) {
		case *structStmt:
			if s.Exported {
				desc := objectsByName[s.Name.Value]
				if err := validateExportObjectTypes(desc, objectsByName); err != nil {
					meta.Errors = append(meta.Errors, err)
				} else {
					meta.ExportedObjects = append(meta.ExportedObjects, desc)
				}
			}
		case *funStmt:
			desc, err := extractCallableDesc(s, ctx)
			if err != nil {
				meta.Errors = append(meta.Errors, err)
				continue
			}
			if s.Exported {
				if err := validateExportCallableTypes(s, desc, objectsByName); err != nil {
					meta.Errors = append(meta.Errors, err)
				}
				meta.ExportedCallables = append(meta.ExportedCallables, desc)
			}
			meta.Callables = append(meta.Callables, desc)
		case *varStmt:
			if s.Exported {
				meta.ExportedVariables = append(meta.ExportedVariables, ExportedVariableMetadata{Name: s.Name.Value, Type: exportedTypeName(typeAnnotationToTypeDesc(s.Type_, ctx))})
			}
		case *interfaceStmt:
			if s.Exported {
				meta.ExportedInterfaces = append(meta.ExportedInterfaces, extractInterfaceDesc(s, ctx))
			}
		case *enumStmt:
			if s.Exported {
				meta.ExportedEnums = append(meta.ExportedEnums, extractEnumDesc(s))
			}
		case *typeAliasStmt:
			if s.Exported {
				resolved := typeAnnotationToTypeDesc(s.Alias, ctx)
				if err := validateExportTypeDesc(resolved, objectsByName); err != nil {
					meta.Errors = append(meta.Errors, err)
				} else {
					meta.ExportedTypes = append(meta.ExportedTypes, ExportedTypeMetadata{Name: s.Name.Value, Type: exportedTypeName(resolved), TypeDesc: resolved})
				}
			}
		}
	}
	return meta
}

func validateExportObjectTypes(desc schema.ObjectDesc, objectsByName map[string]schema.ObjectDesc) error {
	for _, field := range desc.Fields {
		if err := validateExportTypeDesc(field.Type, objectsByName); err != nil {
			return err
		}
	}
	return nil
}

func validateExportTypeDesc(td schema.TypeDesc, objectsByName map[string]schema.ObjectDesc) error {
	return validateTypeDescForExport(nil, td, "export", "", objectsByName, make(map[string]bool))
}

// validateExportCallableTypes enforces that export fun parameters and return types
// use struct types (data contracts), not class types (internal implementation).
func validateExportCallableTypes(stmt *funStmt, desc schema.CallableDesc, objectsByName map[string]schema.ObjectDesc) error {
	for _, param := range desc.Parameters {
		if err := validateTypeDescForExport(stmt, param.Type, "parameter", param.Name, objectsByName, make(map[string]bool)); err != nil {
			return err
		}
	}
	if desc.Mode == schema.CallableModeStreaming && desc.Streaming != nil {
		if desc.Streaming.Next != nil {
			if err := validateTypeDescForExport(stmt, *desc.Streaming.Next, "stream next", "", objectsByName, make(map[string]bool)); err != nil {
				return err
			}
		}
		if desc.Streaming.Final != nil {
			if err := validateTypeDescForExport(stmt, *desc.Streaming.Final, "stream final", "", objectsByName, make(map[string]bool)); err != nil {
				return err
			}
		}
		return nil
	}
	for _, ret := range desc.Returns {
		if err := validateTypeDescForExport(stmt, ret, "return", "", objectsByName, make(map[string]bool)); err != nil {
			return err
		}
	}
	return nil
}

// validateTypeDescForExport checks a single TypeDesc for class-type references
// in an export fun signature and any reachable struct field graph.
func validateTypeDescForExport(stmt *funStmt, td schema.TypeDesc, role string, paramName string, objectsByName map[string]schema.ObjectDesc, visited map[string]bool) error {
	switch td.Kind {
	case schema.TypeKindClass:
		detail := td.ClassName
		if paramName != "" {
			detail = fmt.Sprintf("%s (%s)", paramName, td.ClassName)
		}
		return newExportBoundaryError(stmt, role, detail)
	case schema.TypeKindArray:
		if td.Element != nil {
			return validateTypeDescForExport(stmt, *td.Element, role, paramName, objectsByName, visited)
		}
	case schema.TypeKindMap:
		if td.Key != nil {
			if err := validateTypeDescForExport(stmt, *td.Key, role, paramName, objectsByName, visited); err != nil {
				return err
			}
		}
		if td.Value != nil {
			return validateTypeDescForExport(stmt, *td.Value, role, paramName, objectsByName, visited)
		}
	case schema.TypeKindStruct:
		name := td.ClassName
		if name == "" {
			name = td.Name
		}
		if name == "" || visited[name] {
			return nil
		}
		visited[name] = true
		obj, ok := objectsByName[name]
		if !ok {
			return nil
		}
		if obj.Kind == schema.TypeKindClass {
			detail := obj.Name
			if paramName != "" {
				detail = fmt.Sprintf("%s (%s)", paramName, obj.Name)
			}
			return newExportBoundaryError(stmt, role, detail)
		}
		for _, field := range obj.Fields {
			if err := validateTypeDescForExport(stmt, field.Type, role, paramName, objectsByName, visited); err != nil {
				return err
			}
		}
	}
	return nil
}
