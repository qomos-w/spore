package frontend

import (
	"fmt"

	"github.com/qomos-w/spore/schema"
)

// ParseCallableDesc parses a Spore callable declaration string into a schema.CallableDesc.
// The input should be a callable declaration without body:
//
//	"fun add(a: int, b: int): int"
//	"stream fun chat(prompt: string): string"
func ParseCallableDesc(source string) (schema.CallableDesc, error) {
	// Wrap in a synthetic module so the existing parser can handle it
	wrapped := "export " + source + " { return 0 }"
	prog, err := parseModule(wrapped)
	if err != nil {
		// Try without export wrapper for non-export syntax
		prog, err = parseModule(source + " { return 0 }")
		if err != nil {
			return schema.CallableDesc{}, fmt.Errorf("parse callable: %w", err)
		}
	}
	if len(prog.Stmts) == 0 {
		return schema.CallableDesc{}, fmt.Errorf("no callable declaration found")
	}
	fun, ok := prog.Stmts[0].(*funStmt)
	if !ok {
		return schema.CallableDesc{}, fmt.Errorf("expected fun declaration, got %T", prog.Stmts[0])
	}

	mode := schema.CallableModeUnary
	if fun.IsStream {
		mode = schema.CallableModeStreaming
	}

	desc := schema.CallableDesc{
		Name:     fun.Name.Value,
		Mode:     mode,
		HasError: false,
	}

	// Parse parameters
	ctx := typeContext{
		structNames:       make(map[string]bool),
		classNames:        make(map[string]bool),
		typeAliases:       make(map[string]*typeAnnotation),
		importedTypeDescs: make(map[string]schema.TypeDesc),
	}
	for _, p := range fun.Params {
		paramDesc := schema.ParameterDesc{
			Name: p.Name.Value,
			Type: typeAnnotationToTypeDesc(p.Type_, ctx),
		}
		desc.Parameters = append(desc.Parameters, paramDesc)
	}

	// Parse return type
	if fun.ReturnType != nil {
		desc.Returns = append(desc.Returns, typeAnnotationToTypeDesc(fun.ReturnType, ctx))
	}

	return desc, nil
}

// ParseObjectDesc parses a Spore struct or class declaration string into a schema.ObjectDesc.
// The input should be a struct/class declaration without methods:
//
//	"struct Point { x: int, y: int }"
//	"class User { id: string, name: string }"
func ParseObjectDesc(source string) (schema.ObjectDesc, error) {
	prog, err := parseModule(source)
	if err != nil {
		return schema.ObjectDesc{}, fmt.Errorf("parse object: %w", err)
	}
	if len(prog.Stmts) == 0 {
		return schema.ObjectDesc{}, fmt.Errorf("no object declaration found")
	}

	switch stmt := prog.Stmts[0].(type) {
	case *structStmt:
		return extractClassDesc(stmt, typeContext{}), nil
	case *classStmt:
		return extractClassDesc(stmt, typeContext{}), nil
	default:
		return schema.ObjectDesc{}, fmt.Errorf("expected struct or class declaration, got %T", prog.Stmts[0])
	}
}

// ParseTypeDesc parses a Spore type annotation string into a schema.TypeDesc.
// Examples: "int", "string", "array<int>", "map<string, int>", "Point"
func ParseTypeDesc(source string) (schema.TypeDesc, error) {
	// Wrap in a synthetic variable declaration so the parser can handle it
	wrapped := "var _: " + source + " = 0"
	prog, err := parseModule(wrapped)
	if err != nil {
		return schema.TypeDesc{}, fmt.Errorf("parse type: %w", err)
	}
	if len(prog.Stmts) == 0 {
		return schema.TypeDesc{}, fmt.Errorf("no type found")
	}
	vs, ok := prog.Stmts[0].(*varStmt)
	if !ok {
		return schema.TypeDesc{}, fmt.Errorf("expected type annotation, got %T", prog.Stmts[0])
	}
	if vs.Type_ == nil {
		return schema.TypeDesc{}, fmt.Errorf("no type annotation found")
	}

	ctx := typeContext{
		structNames:       make(map[string]bool),
		classNames:        make(map[string]bool),
		typeAliases:       make(map[string]*typeAnnotation),
		importedTypeDescs: make(map[string]schema.TypeDesc),
	}
	return typeAnnotationToTypeDesc(vs.Type_, ctx), nil
}

// MustParseCallableDesc is like ParseCallableDesc but panics on error.
func MustParseCallableDesc(source string) schema.CallableDesc {
	desc, err := ParseCallableDesc(source)
	if err != nil {
		panic(err)
	}
	return desc
}

// MustParseObjectDesc is like ParseObjectDesc but panics on error.
func MustParseObjectDesc(source string) schema.ObjectDesc {
	desc, err := ParseObjectDesc(source)
	if err != nil {
		panic(err)
	}
	return desc
}

// MustParseTypeDesc is like ParseTypeDesc but panics on error.
func MustParseTypeDesc(source string) schema.TypeDesc {
	desc, err := ParseTypeDesc(source)
	if err != nil {
		panic(err)
	}
	return desc
}

// ParseAllObjectDescs parses every top-level struct or class declaration in the
// given source and returns them in declaration order. Cross-references between
// declarations (e.g. one struct field whose type is another struct in the same
// source) resolve to the correct TypeKindStruct / TypeKindClass because the
// declaration names are pre-collected into the shared typeContext.
//
// Non-object top-level statements (functions, imports, package, etc.) are
// ignored. This is the public-facing helper used by host code-gen tools that
// take a .spore file as authoring source.
func ParseAllObjectDescs(source string) ([]schema.ObjectDesc, error) {
	prog, err := parseModule(source)
	if err != nil {
		return nil, fmt.Errorf("parse objects: %w", err)
	}
	ctx := typeContext{
		structNames:       make(map[string]bool),
		classNames:        make(map[string]bool),
		enumNames:         make(map[string]bool),
		typeAliases:       make(map[string]*typeAnnotation),
		importedTypeDescs: make(map[string]schema.TypeDesc),
	}
	for _, stmt := range prog.Stmts {
		switch s := stmt.(type) {
		case *structStmt:
			ctx.structNames[s.Name.Value] = true
		case *classStmt:
			ctx.classNames[s.Name.Value] = true
		case *enumStmt:
			ctx.enumNames[s.Name.Value] = true
		}
	}
	var out []schema.ObjectDesc
	for _, stmt := range prog.Stmts {
		switch s := stmt.(type) {
		case *structStmt:
			out = append(out, extractClassDesc(s, ctx))
		case *classStmt:
			out = append(out, extractClassDesc(s, ctx))
		}
	}
	return out, nil
}

// ParseAllCallableDescs parses every top-level function declaration in the
// given source and returns them in declaration order. struct/class names from
// the same source are pre-collected so callable parameter/return type
// references resolve correctly.
func ParseAllCallableDescs(source string) ([]schema.CallableDesc, error) {
	prog, err := parseModule(source)
	if err != nil {
		return nil, fmt.Errorf("parse callables: %w", err)
	}
	ctx := typeContext{
		structNames:       make(map[string]bool),
		classNames:        make(map[string]bool),
		enumNames:         make(map[string]bool),
		typeAliases:       make(map[string]*typeAnnotation),
		importedTypeDescs: make(map[string]schema.TypeDesc),
	}
	for _, stmt := range prog.Stmts {
		switch s := stmt.(type) {
		case *structStmt:
			ctx.structNames[s.Name.Value] = true
		case *classStmt:
			ctx.classNames[s.Name.Value] = true
		case *enumStmt:
			ctx.enumNames[s.Name.Value] = true
		}
	}
	var out []schema.CallableDesc
	for _, stmt := range prog.Stmts {
		fun, ok := stmt.(*funStmt)
		if !ok {
			continue
		}
		desc, err := extractCallableDesc(fun, ctx)
		if err != nil {
			return nil, fmt.Errorf("parse callable %q: %w", fun.Name.Value, err)
		}
		out = append(out, desc)
	}
	return out, nil
}

// ParseAllEnumDescs parses every top-level enum declaration in the given
// source and returns them in declaration order with resolved member values
// (explicit `= N` or auto-increment). Non-enum statements are ignored. This is
// the host code-gen counterpart of ParseAllObjectDescs for enum types.
func ParseAllEnumDescs(source string) ([]schema.EnumDesc, error) {
	prog, err := parseModule(source)
	if err != nil {
		return nil, fmt.Errorf("parse enums: %w", err)
	}
	var out []schema.EnumDesc
	for _, stmt := range prog.Stmts {
		s, ok := stmt.(*enumStmt)
		if !ok {
			continue
		}
		out = append(out, extractEnumDesc(s))
	}
	return out, nil
}
