package frontend

import (
	"fmt"

	"github.com/qomos-w/spore/diagnostics"
)

// parser converts a token stream into an AST using recursive descent
// with a Pratt-style precedence parser for expressions.
type parser struct {
	lex       *lexer
	cur       token
	pk        token
	errors    []parseError
	source    string
	reExports []*importStmt
}

func newParser(l *lexer, source string) *parser {
	p := &parser{lex: l, source: source, errors: nil}
	p.nextToken() // prime cur
	p.nextToken() // prime pk
	return p
}

// --- Precedence levels (Pratt parser) ---

const (
	_ int = iota
	lowestPrec
	assignPrec   // =
	coalescePrec // ?? (below ||, above assignment)
	orPrec       // ||
	andPrec      // &&
	equalPrec    // == !=
	comparePrec  // < <= > >=
	sumPrec      // + -
	productPrec  // * / %
	prefixPrec   // unary ! -
	callPrec     // function call ()
	indexPrec    // array index []
	memberPrec   // member access .
)

var precedences = map[tokenType]int{
	tokEq:       assignPrec,
	tokQuestionQuestion: coalescePrec,
	tokOr:       orPrec,
	tokAnd:      andPrec,
	tokEqEq:     equalPrec,
	tokBangEq:   equalPrec,
	tokLT:       comparePrec,
	tokLTEq:     comparePrec,
	tokGT:       comparePrec,
	tokGTEq:     comparePrec,
	tokPlus:     sumPrec,
	tokMinus:    sumPrec,
	tokStar:     productPrec,
	tokSlash:    productPrec,
	tokPercent:  productPrec,
	tokLParen:   callPrec,
	tokDot:      memberPrec,
	tokQuestionDot: memberPrec,
	tokLBracket: indexPrec,
	tokIs:       comparePrec,
	tokAs:       comparePrec,
}

func (p *parser) curPrecedence() int {
	if prec, ok := precedences[p.cur.typ]; ok {
		return prec
	}
	return lowestPrec
}

// peekIs reports whether the lookahead token has the given type.
func (p *parser) peekIs(typ tokenType) bool {
	return p.pk.typ == typ
}

// --- Top-level parse ---

func (p *parser) parse() (*program, error) {
	prog := &program{}

	if p.curIs(tokPackage) {
		prog.pkg = p.parsePackageStmt()
	}

	for p.curIs(tokImport) {
		prog.imports = append(prog.imports, p.parseImportStmts()...)
	}

	for !p.curIs(tokEOF) && !p.curIs(tokError) {
		beforeErrors := len(p.errors)
		stmt := p.parseTopLevel()
		if stmt != nil {
			prog.Stmts = append(prog.Stmts, stmt)
		}
		if len(p.errors) > beforeErrors {
			p.synchronizeTopLevel()
		}
	}

	// Collect re-exports parsed during top-level parsing.
	for _, re := range p.reExports {
		prog.imports = append(prog.imports, re)
	}

	if len(p.errors) > 0 {
		return prog, diagnostics.NewMultiErrorWithFallback(convertParseErrors(p.errors), diagnostics.Descriptor{Category: diagnostics.CategoryLoad, Path: "frontend/diagnostics"})
	}
	if len(prog.Stmts) == 0 && len(prog.imports) == 0 {
		return nil, newParseError(1, 1, "source does not declare any callables or objects")
	}
	return prog, nil
}

func (p *parser) parseTopLevel() statement {
	exportTok := token{}
	exported := false
	if p.curIs(tokExport) {
		exported = true
		exportTok = p.cur
		p.nextToken()
	}

	// Open modifier for classes/functions
	isOpen := false
	if p.curIs(tokOpen) {
		isOpen = true
		p.nextToken()
	}

	// Stream modifier for fun declarations
	isStream := false
	if p.curIs(tokStream) {
		isStream = true
		p.nextToken()
	}

	switch p.cur.typ {
	case tokFun:
		if isOpen {
			p.addError("open cannot be applied to top-level fun")
			return nil
		}
		return p.parseFunStmt(exported, isStream)
	case tokAt:
		// Decorators: @schema(N) / @component are only valid on a struct
		// declaration. parseDecoratedStructStmt consumes the annotation
		// list and reports when the declaration that follows is not a struct.
		if isStream {
			p.addError("stream must be followed by fun")
			return nil
		}
		return p.parseDecoratedStructStmt(exported)
	case tokStruct:
		if isStream {
			p.addError("stream must be followed by fun")
			return nil
		}
		return p.parseStructStmt(exported)
	case tokEnum:
		if isOpen {
			p.addError("open cannot be applied to enum")
			return nil
		}
		if isStream {
			p.addError("stream must be followed by fun")
			return nil
		}
		return p.parseEnumStmt(exported)
	case tokClass:
		if exported {
			p.addError("export cannot be applied to class")
			return nil
		}
		if isStream {
			p.addError("stream must be followed by fun")
			return nil
		}
		stmt := p.parseClassStmt()
		stmt.IsOpen = isOpen
		return stmt
	case tokInterface:
		if isStream {
			p.addError("stream must be followed by fun")
			return nil
		}
		stmt := p.parseInterfaceStmt()
		stmt.Exported = exported
		return stmt
	case tokVar:
		if isStream {
			p.addError("stream must be followed by fun")
			return nil
		}
		return p.parseVarStmt(exported)
	case tokType:
		if isStream {
			p.addError("stream must be followed by fun")
			return nil
		}
		return p.parseTypeAliasStmt(exported)
	case tokStar:
		// Wildcard re-export: export * from "module"
		if exported && p.pkIs(tokFrom) {
			p.nextToken() // consume *
			p.expect(tokFrom)
			path := p.expectStringLiteral()
			p.reExports = append(p.reExports, &importStmt{tok: exportTok, Name: "*", Path: path, ReExport: true})
			return nil
		}
		p.addError(fmt.Sprintf("unexpected '*' at top level"))
		return nil
	case tokIdent:
		// Re-export: export X from "module"
		if exported && p.pkIs(tokFrom) {
			name := p.cur.lexeme
			p.nextToken() // consume ident
			p.expect(tokFrom)
			path := p.expectStringLiteral()
			p.reExports = append(p.reExports, &importStmt{tok: exportTok, Name: name, Path: path, ReExport: true})
			return nil
		}
		// Global assignment: ident = expr (only to declared globals)
		if p.pkIs(tokEq) {
			return p.parseTopLevelAssignStmt()
		}
		if exported {
			p.addError(fmt.Sprintf("expected declaration after export, got %s", p.cur.lexeme))
		} else {
			p.addError(fmt.Sprintf("expected declaration, got %s", p.cur.lexeme))
		}
		return nil
	default:
		p.addError(fmt.Sprintf("expected declaration, got %s", p.cur.lexeme))
		return nil
	}
}

// --- Package ---

func (p *parser) parsePackageStmt() *packageStmt {
	tok := p.cur
	p.nextToken()
	name := p.expectIdent()
	// Package names may be dotted, e.g. `package demo.net`.
	for p.curIs(tokDot) && p.pkIs(tokIdent) {
		p.nextToken() // consume '.'
		segment := p.expectIdent()
		name += "." + segment
	}
	return &packageStmt{tok: tok, Name: name}
}

// --- Import ---

func (p *parser) parseImportStmts() []*importStmt {
	tok := p.cur
	p.nextToken()

	// Batch import: import { A, B as Y } from "path"
	if p.curIs(tokLBrace) {
		p.nextToken() // consume {
		var bindings []importBinding
		for {
			if p.curIs(tokRBrace) {
				p.nextToken() // consume }
				break
			}
			name := p.expectIdent()
			alias := ""
			if p.curIs(tokAs) {
				p.nextToken()
				alias = p.expectIdent()
			}
			bindings = append(bindings, importBinding{Name: name, Alias: alias})
			if p.curIs(tokComma) {
				p.nextToken()
				continue
			}
			if p.curIs(tokRBrace) {
				p.nextToken() // consume }
				break
			}
			p.addError(fmt.Sprintf("expected ',' or '}' in import list, got %s", p.cur.typ))
			break
		}
		p.expect(tokFrom)
		path := p.expectStringLiteral()
		stmts := make([]*importStmt, 0, len(bindings))
		for _, b := range bindings {
			stmts = append(stmts, &importStmt{tok: tok, Name: b.Name, Path: path, Alias: b.Alias})
		}
		return stmts
	}

	// Single import: import Name [as Alias] from "path"
	name := p.expectIdent()
	alias := ""
	if p.curIs(tokAs) {
		p.nextToken()
		alias = p.expectIdent()
	}
	p.expect(tokFrom)
	path := p.expectStringLiteral()
	return []*importStmt{{tok: tok, Name: name, Path: path, Alias: alias}}
}

type importBinding struct {
	Name  string
	Alias string
}

// --- Fun ---

func (p *parser) parseFunStmt(exported bool, isStream bool) *funStmt {
	tok := p.cur
	p.nextToken()

	name := p.expectIdent()

	p.expect(tokLParen)
	params := p.parseParamList()
	p.expect(tokRParen)

	var returnType *typeAnnotation
	if p.curIs(tokColon) {
		p.nextToken()
		returnType = p.parseTypeAnnotation()
	} else if p.curIs(tokArrow) {
		p.nextToken()
		returnType = p.parseTypeAnnotation()
	}

	stmt := &funStmt{
		tok:        tok,
		Name:       &ident{tok: p.cur, Value: name},
		Params:     params,
		ReturnType: returnType,
		Exported:   exported,
		IsStream:   isStream,
	}

	if p.curIs(tokLBrace) {
		body := p.parseBlockStmt()
		stmt.Body = body
		stmt.BodyLine = body.tok.line
		stmt.BodyCol = body.tok.col
	} else if p.curIs(tokEq) {
		if isStream {
			p.addError("stream fun cannot use expression body")
			return nil
		}
		p.nextToken()
		bodyStart := p.cur
		stmt.ExprBody = p.parseExpression()
		stmt.BodyLine = bodyStart.line
		stmt.BodyCol = bodyStart.col
	}

	return stmt
}

func (p *parser) isTopLevelStart() bool {
	switch p.cur.typ {
	case tokFun, tokExport, tokOpen, tokStream, tokStruct, tokClass, tokInterface, tokVar, tokType, tokEnum:
		return true
	}
	return false
}

func (p *parser) synchronizeTopLevel() {
	if p.curIs(tokEOF) || p.curIs(tokError) {
		return
	}
	if p.isTopLevelStart() {
		return
	}
	p.nextToken()
	for !p.curIs(tokEOF) && !p.curIs(tokError) && !p.isTopLevelStart() {
		p.nextToken()
	}
}

func (p *parser) isClassMemberStart() bool {
	switch p.cur.typ {
	case tokIdent, tokConstructor, tokFun, tokPublic, tokPrivate, tokOverride, tokOpen, tokStatic:
		return true
	}
	return false
}

func (p *parser) synchronizeClassMember() {
	if p.isClassMemberStart() || p.curIs(tokRBrace) || p.curIs(tokEOF) || p.curIs(tokError) {
		return
	}
	p.nextToken()
	for !p.curIs(tokRBrace) && !p.curIs(tokEOF) && !p.curIs(tokError) && !p.isClassMemberStart() {
		p.nextToken()
	}
}

// --- Struct ---

// parseStructStmt parses `struct Name { ... }` plus the optional decorator
// list that may precede it: @schema(N) declares the transport schema id,
// @component marks the struct as an ECS component for runtime codegen.
// Decorators are accepted in any order and only on struct declarations.
func (p *parser) parseStructStmt(exported bool) *structStmt {
	tok := p.cur
	p.nextToken() // consume struct keyword (annotations already consumed by the caller)

	schemaID := uint64(0)
	isComponent := false
	name := p.expectIdent()
	p.expect(tokLBrace)
	fields := p.parseFieldList()
	p.expect(tokRBrace)

	return &structStmt{
		tok:         tok,
		Name:        &ident{tok: tok, Value: name},
		Fields:      fields,
		Exported:    exported,
		SchemaID:    schemaID,
		IsComponent: isComponent,
	}
}

// parseDecoratedStructStmt parses a decorator list (@schema(N), @component)
// followed by a struct declaration. It is the entry point from the top-level
// `@` dispatch; plain struct declarations go directly to parseStructStmt.
func (p *parser) parseDecoratedStructStmt(exported bool) *structStmt {
	tok := p.cur // the @ token

	schemaID := uint64(0)
	isComponent := false
	for p.curIs(tokAt) {
		p.nextToken() // consume @
		ann := p.expectIdent()
		switch ann {
		case "schema":
			schemaID = p.parseSchemaAnnotationArgs()
		case "component":
			if isComponent {
				p.addError("duplicate @component annotation")
			}
			isComponent = true
		default:
			p.addError(fmt.Sprintf("expected schema or component annotation, got @%s", ann))
		}
	}

	if !p.curIs(tokStruct) {
		p.addError(fmt.Sprintf("@schema/@component annotation must precede a struct declaration, got %s", p.cur.lexeme))
		return nil
	}
	p.nextToken() // consume struct keyword

	name := p.expectIdent()
	p.expect(tokLBrace)
	fields := p.parseFieldList()
	p.expect(tokRBrace)

	return &structStmt{
		tok:         tok,
		Name:        &ident{tok: tok, Value: name},
		Fields:      fields,
		Exported:    exported,
		SchemaID:    schemaID,
		IsComponent: isComponent,
	}
}

// parseSchemaAnnotationArgs parses `(N)` following the schema annotation name.
func (p *parser) parseSchemaAnnotationArgs() uint64 {
	p.expect(tokLParen)
	if !p.curIs(tokIntLit) {
		p.addError(fmt.Sprintf("expected integer schema id, got %s", p.cur.lexeme))
		return 0
	}
	id := uint64(p.cur.intVal)
	p.nextToken()
	p.expect(tokRParen)
	return id
}

// --- Enum ---

// parseEnumStmt parses `enum Name { A, B = 3, C }`. Members default to
// auto-increment values starting at 0; explicit values use `= int`. The
// raw member values are recorded here and resolved (auto-increment,
// duplicate detection) at compilation.
func (p *parser) parseEnumStmt(exported bool) *enumStmt {
	tok := p.cur
	p.nextToken()

	name := p.expectIdent()
	p.expect(tokLBrace)

	stmt := &enumStmt{
		tok:      tok,
		Name:     &ident{tok: tok, Value: name},
		Exported: exported,
	}

	seen := map[string]bool{}
	for !p.curIs(tokRBrace) && !p.curIs(tokEOF) && !p.curIs(tokError) {
		memberTok := p.cur
		memberName := p.expectIdent()
		if memberName == "" {
			break
		}
		member := &enumMember{Name: &ident{tok: memberTok, Value: memberName}}
		if p.curIs(tokEq) {
			p.nextToken()
			// Allow explicit negative values (`None = -1`); auto-increment
			// continues from the signed value.
			negative := false
			if p.curIs(tokMinus) {
				negative = true
				p.nextToken()
			}
			if !p.curIs(tokIntLit) {
				p.addError(fmt.Sprintf("expected integer enum value after '=', got %s", p.cur.lexeme))
				break
			}
			member.Value = p.cur.intVal
			if negative {
				member.Value = -member.Value
			}
			member.HasValue = true
			p.nextToken()
		}
		if seen[memberName] {
			p.addError(fmt.Sprintf("duplicate enum member %q in enum %s", memberName, name))
		}
		seen[memberName] = true
		stmt.Members = append(stmt.Members, member)

		if p.curIs(tokComma) {
			p.nextToken()
			continue
		}
		if p.curIs(tokRBrace) {
			break
		}
		p.addError(fmt.Sprintf("expected ',' or '}' after enum member, got %s", p.cur.lexeme))
		break
	}
	p.expect(tokRBrace)
	return stmt
}

// --- Class ---

func (p *parser) parseClassStmt() *classStmt {
	tok := p.cur
	p.nextToken()

	name := p.expectIdent()

	// Inheritance: class Dog : Animal or class Dog : Animal, IDrawable
	var parent *ident
	var implements []*ident
	if p.curIs(tokColon) {
		p.nextToken()
		parentName := p.expectIdent()
		parent = &ident{tok: p.cur, Value: parentName}

		// Optional comma-separated interface list
		for p.curIs(tokComma) {
			p.nextToken()
			ifaceName := p.expectIdent()
			implements = append(implements, &ident{tok: p.cur, Value: ifaceName})
		}
	}

	p.expect(tokLBrace)

	var fields []*astFieldDecl
	var methods []*funStmt

	for !p.curIs(tokRBrace) && !p.curIs(tokEOF) && !p.curIs(tokError) {
		if p.curIs(tokStatic) {
			p.addError("static is not supported in Spore")
			p.nextToken()
			continue
		}

		// Access modifiers
		access := accessPublic
		if p.curIs(tokPublic) {
			p.nextToken()
		} else if p.curIs(tokPrivate) {
			access = accessPrivate
			p.nextToken()
		}

		// Override modifier
		isOverride := false
		if p.curIs(tokOverride) {
			isOverride = true
			p.nextToken()
		}

		// Open modifier on method
		methodIsOpen := false
		if p.curIs(tokOpen) {
			methodIsOpen = true
			p.nextToken()
		}

		if p.curIs(tokConstructor) {
			// constructor(params) { body } — parsed as method named after the class
			ctor := p.parseConstructorStmt(name)
			ctor.IsOverride = isOverride
			ctor.IsOpen = methodIsOpen
			ctor.Access = access
			methods = append(methods, ctor)
		} else if p.curIs(tokFun) {
			method := p.parseFunStmt(false, false)
			method.IsOverride = isOverride
			method.IsOpen = methodIsOpen
			method.Access = access
			methods = append(methods, method)
		} else {
			optional := false
			// `optional` is a field-prefix modifier. Treat as modifier only when
			// followed by something that isn't `:`, so `optional: T` (a field
			// literally named "optional") still parses as a field name.
			if p.curIs(tokOptional) && !p.pkIs(tokColon) {
				optional = true
				p.nextToken()
			}
			fieldNameTok := p.cur
			fieldName := p.expectIdent()
			beforeErrors := len(p.errors)
			p.expect(tokColon)
			missingTypeBoundary := (p.curIs(tokIdent) && p.pkIs(tokColon)) || p.curIs(tokFun) || p.curIs(tokConstructor) || p.curIs(tokRBrace) || p.curIs(tokEOF) || p.curIs(tokError)
			if len(p.errors) == beforeErrors && missingTypeBoundary {
				p.addError(fmt.Sprintf("expected type, got %s", p.cur.lexeme))
				p.synchronizeClassMember()
				continue
			}
			fieldType := p.parseTypeAnnotation()
			if len(p.errors) > beforeErrors {
				p.synchronizeClassMember()
				continue
			}
			if fieldName == "" || fieldType == nil || fieldType.Name == "" {
				continue
			}
			fields = append(fields, &astFieldDecl{
				Name:     &ident{tok: fieldNameTok, Value: fieldName},
				Type_:    fieldType,
				Access:   access,
				Optional: optional,
			})
		}
		if p.curIs(tokComma) {
			p.nextToken()
		}
	}
	p.expect(tokRBrace)

	return &classStmt{
		tok:        tok,
		Name:       &ident{tok: tok, Value: name},
		Parent:     parent,
		Implements: implements,
		Fields:     fields,
		Methods:    methods,
	}
}

// --- Constructor ---

func (p *parser) parseConstructorStmt(className string) *funStmt {
	tok := p.cur
	p.nextToken() // consume 'constructor'

	p.expect(tokLParen)
	params := p.parseParamList()
	p.expect(tokRParen)

	stmt := &funStmt{
		tok:      tok,
		Name:     &ident{tok: tok, Value: className},
		Params:   params,
		Exported: false,
	}

	if p.curIs(tokLBrace) {
		body := p.parseBlockStmt()
		stmt.Body = body
		stmt.BodyLine = body.tok.line
		stmt.BodyCol = body.tok.col
	}

	return stmt
}

// --- Interface ---

func (p *parser) parseInterfaceStmt() *interfaceStmt {
	tok := p.cur
	p.nextToken() // consume 'interface'

	name := p.expectIdent()
	p.expect(tokLBrace)

	var methods []*methodSignature
	for !p.curIs(tokRBrace) && !p.curIs(tokEOF) && !p.curIs(tokError) {
		if !p.curIs(tokFun) {
			p.addError(fmt.Sprintf("expected interface method, got %s", p.cur.lexeme))
			p.synchronizeInterfaceMember()
			continue
		}

		beforeErrors := len(p.errors)
		p.nextToken() // consume 'fun'
		methodNameTok := p.cur
		methodName := p.expectIdent()

		p.expect(tokLParen)
		params := p.parseParamList()
		p.expect(tokRParen)

		var returnType *typeAnnotation
		if p.curIs(tokColon) {
			p.nextToken()
			returnType = p.parseTypeAnnotation()
		} else if p.curIs(tokArrow) {
			p.nextToken()
			returnType = p.parseTypeAnnotation()
		}

		// Optional default method body: `fun name(params): T { ... }`.
		var body *blockStmt
		if p.curIs(tokLBrace) {
			body = p.parseBlockStmt()
		}

		if len(p.errors) > beforeErrors {
			p.synchronizeInterfaceMember()
			continue
		}

		methods = append(methods, &methodSignature{
			Name:       &ident{tok: methodNameTok, Value: methodName},
			Params:     params,
			ReturnType: returnType,
			Body:       body,
		})
		if p.curIs(tokComma) {
			p.nextToken()
		}
	}
	p.expect(tokRBrace)

	return &interfaceStmt{
		tok:     tok,
		Name:    &ident{tok: tok, Value: name},
		Methods: methods,
	}
}

func (p *parser) synchronizeInterfaceMember() {
	if p.curIs(tokFun) || p.curIs(tokRBrace) || p.curIs(tokEOF) || p.curIs(tokError) {
		return
	}
	for !p.curIs(tokFun) && !p.curIs(tokRBrace) && !p.curIs(tokEOF) && !p.curIs(tokError) {
		p.nextToken()
	}
}

// --- Top-level assignment ---

func (p *parser) parseTopLevelAssignStmt() statement {
	tok := p.cur
	name := p.cur.lexeme
	p.nextToken() // consume ident
	p.expect(tokEq)
	val := p.parseExpression()

	// Represented as assignExpr wrapped in exprStatement,
	// so the compiler can handle it the same as body-level assignment.
	return &exprStatement{
		tok: tok,
		Expr: &assignExpr{
			tok:    tok,
			Target: &identExpr{tok: tok, Value: name},
			Value:  val,
		},
	}
}

// --- Var ---

func (p *parser) parseVarStmt(exported bool) *varStmt {
	tok := p.cur
	p.nextToken()

	name := p.expectIdent()
	p.expect(tokColon)
	typeAnno := p.parseTypeAnnotation()

	var val expression
	if p.curIs(tokEq) {
		p.nextToken()
		val = p.parseExpression()
	}

	return &varStmt{
		tok:      tok,
		Name:     &ident{tok: tok, Value: name},
		Type_:    typeAnno,
		Value:    val,
		InitExpr: val,
		Exported: exported,
	}
}

// --- Type Alias ---

func (p *parser) parseTypeAliasStmt(exported bool) *typeAliasStmt {
	tok := p.cur
	p.nextToken()

	name := p.expectIdent()
	p.expect(tokEq)
	alias := p.parseTypeAnnotation()

	return &typeAliasStmt{
		tok:      tok,
		Name:     &ident{tok: tok, Value: name},
		Alias:    alias,
		Exported: exported,
	}
}

// --- Parameter list ---

func (p *parser) parseParamList() []*param {
	var params []*param
	seen := make(map[string]bool)

	for !p.curIs(tokRParen) && !p.curIs(tokEOF) && !p.curIs(tokError) {
		paramName := p.expectIdent()
		if seen[paramName] {
			p.addError(fmt.Sprintf("duplicate parameter %q", paramName))
			return nil
		}
		seen[paramName] = true

		var paramType *typeAnnotation
		if p.curIs(tokColon) {
			p.nextToken()
			paramType = p.parseTypeAnnotation()
		} else {
			paramType = p.parseTypeAnnotation()
		}

		params = append(params, &param{
			Name:  &ident{tok: p.cur, Value: paramName},
			Type_: paramType,
		})

		if p.curIs(tokComma) {
			p.nextToken()
		}
	}
	return params
}

// --- Field list ---

func (p *parser) parseFieldList() []*astFieldDecl {
	var fields []*astFieldDecl
	seen := make(map[string]bool)

	for !p.curIs(tokRBrace) && !p.curIs(tokEOF) && !p.curIs(tokError) {
		optional := false
		// `optional` is a field-prefix modifier. Treat as modifier only when
		// followed by something that isn't `:`, so `optional: T` (a field
		// literally named "optional") still parses as a field name.
		if p.curIs(tokOptional) && !p.pkIs(tokColon) {
			optional = true
			p.nextToken()
		}
		fieldName := p.expectFieldName()
		if seen[fieldName] {
			p.addError(fmt.Sprintf("duplicate field %q", fieldName))
			return nil
		}
		seen[fieldName] = true

		var fieldType *typeAnnotation
		if p.curIs(tokColon) {
			p.nextToken()
			fieldType = p.parseTypeAnnotation()
		} else {
			fieldType = p.parseTypeAnnotation()
		}

		fields = append(fields, &astFieldDecl{
			Name:     &ident{tok: p.cur, Value: fieldName},
			Type_:    fieldType,
			Optional: optional,
		})

		if p.curIs(tokComma) {
			p.nextToken()
		}
	}
	return fields
}

// --- Type annotation ---

// scalarTypeName checks if the current token is a scalar type keyword.
func scalarTypeName(t token) (string, bool) {
	switch t.typ {
	case tokBool:
		return "bool", true
	case tokByte:
		return "byte", true
	case tokShort:
		return "short", true
	case tokUShort:
		return "ushort", true
	case tokUint:
		return "uint", true
	case tokLong:
		return "long", true
	case tokUlong:
		return "ulong", true
	case tokDouble:
		return "double", true
	case tokBytes:
		return "bytes", true
	case tokInt:
		return "int", true
	case tokFloat:
		return "float", true
	case tokString:
		return "string", true
	case tokMedia:
		return "media", true
	default:
		return "", false
	}
}
func (p *parser) parseTypeAnnotation() *typeAnnotation {
	tok := p.cur

	if p.curIs(tokVoid) {
		p.nextToken()
		return &typeAnnotation{tok: tok, Name: "void"}
	}

	if p.curIs(tokAny) {
		p.nextToken()
		return &typeAnnotation{tok: tok, Name: "any"}
	}
	if name, ok := scalarTypeName(p.cur); ok {
		p.nextToken()
		return &typeAnnotation{tok: tok, Name: name}
	}

	if p.curIs(tokMap) {
		mapTok := p.cur
		p.nextToken()
		return p.parseMapTypeAnnotation(mapTok)
	}

	if p.curIs(tokArray) {
		arrTok := p.cur
		p.nextToken()
		if p.curIs(tokLT) {
			p.nextToken()
			element := p.parseTypeAnnotation()
			p.expect(tokGT)
			return &typeAnnotation{
				tok:    arrTok,
				Name:   "array",
				Params: []*typeAnnotation{element},
			}
		}
		return &typeAnnotation{tok: arrTok, Name: "array"}
	}

	// `fun` starts a function type only when followed by `(`; a bare `fun`
	// falls through to the generic error below so malformed-input recovery
	// (which relies on parseTypeAnnotation never consuming tokens it cannot
	// parse) keeps working.
	if p.curIs(tokFun) && p.peekIs(tokLParen) {
		return p.parseFunTypeAnnotation()
	}

	if p.curIs(tokIdent) {
		name := p.cur.lexeme
		p.nextToken()
		if p.curIs(tokLT) {
			p.nextToken()
			var params []*typeAnnotation
			params = append(params, p.parseTypeAnnotation())
			for p.curIs(tokComma) {
				p.nextToken()
				params = append(params, p.parseTypeAnnotation())
			}
			p.expect(tokGT)
			return &typeAnnotation{
				tok:    tok,
				Name:   name,
				Params: params,
			}
		}
		return &typeAnnotation{tok: tok, Name: name}
	}

	p.addError(fmt.Sprintf("expected type, got %s", p.cur.lexeme))
	return &typeAnnotation{tok: tok, Name: ""}
}

// parseFunTypeAnnotation parses a function type in type position:
//
//	fun(T1, T2): R
//	fun(T1, T2) -> R
//
// Parameter entries are types only (no parameter names). The optional return
// type is introduced by `:` or `->`; omitting it means `void`. The return
// type binds greedily to the function type, so `fun(cb: fun(int): int)`
// parses `fun(int): int` as cb's type and leaves the enclosing callable
// without a return annotation.
func (p *parser) parseFunTypeAnnotation() *typeAnnotation {
	funTok := p.cur
	p.nextToken() // consume 'fun'

	p.expect(tokLParen)
	var paramTypes []*typeAnnotation
	for !p.curIs(tokRParen) && !p.curIs(tokEOF) && !p.curIs(tokError) {
		before := p.cur
		paramTypes = append(paramTypes, p.parseTypeAnnotation())
		if p.curIs(tokComma) {
			p.nextToken()
			continue
		}
		if p.cur == before && !p.curIs(tokRParen) {
			// No progress on a malformed parameter type — stop instead of
			// looping; expect(tokRParen) reports the position.
			break
		}
	}
	p.expect(tokRParen)

	var returnType *typeAnnotation
	if p.curIs(tokColon) || p.curIs(tokArrow) {
		p.nextToken()
		returnType = p.parseTypeAnnotation()
	}

	return &typeAnnotation{
		tok:       funTok,
		Name:      "fun",
		IsFun:     true,
		FunParams: paramTypes,
		FunReturn: returnType,
	}
}

func (p *parser) parseMapTypeAnnotation(mapTok token) *typeAnnotation {
	if p.curIs(tokLT) {
		p.nextToken()
		key := p.parseTypeAnnotation()
		p.expect(tokComma)
		value := p.parseTypeAnnotation()
		p.expect(tokGT)
		return &typeAnnotation{
			tok:    mapTok,
			Name:   "map",
			Params: []*typeAnnotation{key, value},
		}
	}
	return &typeAnnotation{tok: mapTok, Name: "map"}
}

// --- Block statement ---

func (p *parser) parseBlockStmt() *blockStmt {
	tok := p.cur
	p.expect(tokLBrace)

	var stmts []statement
	for !p.curIs(tokRBrace) && !p.curIs(tokEOF) && !p.curIs(tokError) {
		stmt := p.parseStatement()
		if stmt != nil {
			stmts = append(stmts, stmt)
		}
		if len(p.errors) > 0 {
			return &blockStmt{tok: tok, Stmts: stmts}
		}
	}
	p.expect(tokRBrace)
	return &blockStmt{tok: tok, Stmts: stmts}
}

// --- Statement dispatch ---

func (p *parser) parseStatement() statement {
	switch p.cur.typ {
	case tokYield:
		return p.parseYieldStmt()
	case tokAsync:
		p.addError("async is not supported in Spore")
		p.nextToken()
		return nil
	case tokAwait:
		p.addError("await is not supported in Spore")
		p.nextToken()
		return nil
	case tokStatic:
		p.addError("static is not supported in Spore")
		p.nextToken()
		return nil
	case tokVar:
		return p.parseVarStmt(false)
	case tokReturn:
		return p.parseReturnStmt()
	case tokIf:
		return p.parseIfStmt()
	case tokWhile:
		return p.parseWhileStmt()
	case tokFor:
		return p.parseForStmt()
	case tokWhen:
		return p.parseWhenStmt()
	case tokBreak:
		return p.parseBreakStmt()
	case tokContinue:
		return p.parseContinueStmt()
	case tokTry:
		return p.parseTryStmt()
	case tokDefer:
		return p.parseDeferStmt()
	case tokLBrace:
		return p.parseBlockStmt()
	default:
		return p.parseExprStatement()
	}
}

func (p *parser) parseReturnStmt() statement {
	tok := p.cur
	p.nextToken()

	var val expression
	if !p.curIs(tokSemicolon) && !p.curIs(tokRBrace) && !p.curIs(tokEOF) {
		val = p.parseExpression()
	}

	if p.curIs(tokSemicolon) {
		p.nextToken()
	}
	return &returnStmt{tok: tok, Value: val}
}

func (p *parser) parseYieldStmt() statement {
	tok := p.cur
	p.nextToken()

	var val expression
	if !p.curIs(tokSemicolon) && !p.curIs(tokRBrace) && !p.curIs(tokEOF) {
		val = p.parseExpression()
	}

	if p.curIs(tokSemicolon) {
		p.nextToken()
	}
	return &yieldStmt{tok: tok, Value: val}
}

func (p *parser) parseExprStatement() statement {
	tok := p.cur
	expr := p.parseExpression()
	if p.curIs(tokSemicolon) {
		p.nextToken()
	}
	return &exprStatement{tok: tok, Expr: expr}
}

// --- Control flow statements ---

func (p *parser) parseIfStmt() statement {
	tok := p.cur
	p.nextToken() // consume 'if'

	condition := p.parseExpression()
	consequence := p.parseBlockStmt()

	var alternative statement
	if p.curIs(tokElse) {
		p.nextToken()
		if p.curIs(tokIf) {
			alternative = p.parseIfStmt()
		} else {
			alternative = p.parseBlockStmt()
		}
	}

	return &ifStmt{
		tok:         tok,
		Condition:   condition,
		Consequence: consequence,
		Alternative: alternative,
	}
}

func (p *parser) parseWhileStmt() statement {
	tok := p.cur
	p.nextToken() // consume 'while'

	condition := p.parseExpression()
	body := p.parseBlockStmt()

	return &whileStmt{
		tok:       tok,
		Condition: condition,
		Body:      body,
	}
}

func (p *parser) parseForStmt() statement {
	tok := p.cur
	p.nextToken() // consume 'for'

	// Check for for-in: for (name in iterable) { ... }
	if p.curIs(tokLParen) {
		p.nextToken() // consume '('
		if p.pkIs(tokIn) {
			// for-in loop: for (name in iterable) { body }
			varName := p.cur.lexeme
			p.nextToken() // consume variable name
			p.nextToken() // consume 'in'
			iterable := p.parseExpression()
			p.expect(tokRParen)
			body := p.parseBlockStmt()
			return &forStmt{
				tok:      tok,
				IsForIn:  true,
				Variable: varName,
				Iterable: iterable,
				Body:     body,
			}
		}
		// Classic C-style for: for (init; cond; update) { body }
		init := p.parseForClauseInit()
		p.expect(tokSemicolon)
		condition := p.parseExpression()
		p.expect(tokSemicolon)
		update := p.parseExpression()
		p.expect(tokRParen)
		body := p.parseBlockStmt()
		return &forStmt{
			tok:       tok,
			Init:      init,
			Condition: condition,
			Update:    update,
			Body:      body,
		}
	}

	p.addError("expected for-in or for loop")
	return nil
}

func (p *parser) parseForClauseInit() statement {
	if p.curIs(tokVar) {
		return p.parseVarStmt(false)
	}
	if p.curIs(tokSemicolon) {
		return nil
	}
	tok := p.cur
	expr := p.parseExpression()
	return &exprStatement{tok: tok, Expr: expr}
}

func (p *parser) parseWhenStmt() statement {
	tok := p.cur
	p.nextToken() // consume 'when'

	if !p.curIs(tokLParen) {
		p.addError("when requires a subject expression")
		return nil
	}

	p.nextToken()
	expr := p.parseExpression()
	p.expect(tokRParen)

	p.expect(tokLBrace)

	var cases []*caseClause
	var defaultCase *blockStmt
	suppressedErrors := false
	reportedCaseError := false

	for !p.curIs(tokRBrace) && !p.curIs(tokEOF) && !p.curIs(tokError) {
		if p.curIs(tokCase) {
			beforeErrors := len(p.errors)
			cc := p.parseCaseClause()
			if cc != nil && len(p.errors) == beforeErrors {
				cases = append(cases, cc)
			}
			if len(p.errors) > beforeErrors {
				suppressedErrors = true
				if p.errors[len(p.errors)-1].msg == "expected case expression before block" {
					reportedCaseError = true
				}
				p.errors = p.errors[:beforeErrors]
				p.synchronizeWhenCase()
			}
			continue
		}
		if p.curIs(tokElse) {
			p.nextToken()
			beforeErrors := len(p.errors)
			defaultCase = p.parseBlockStmt()
			if len(p.errors) > beforeErrors {
				suppressedErrors = true
				p.errors = p.errors[:beforeErrors]
				p.synchronizeWhenCase()
			} else {
				break
			}
			continue
		}
		p.addError(fmt.Sprintf("expected 'case' or 'else' in when, got %s", p.cur.lexeme))
		suppressedErrors = true
		p.errors = p.errors[:len(p.errors)-1]
		p.synchronizeWhenCase()
	}

	p.expect(tokRBrace)
	if reportedCaseError {
		p.addError("expected case expression before block")
	} else if suppressedErrors {
		p.addError("invalid when case")
	}

	return &whenStmt{
		tok:         tok,
		Expr:        expr,
		Cases:       cases,
		DefaultCase: defaultCase,
	}
}

func (p *parser) parseCaseClause() *caseClause {
	tok := p.cur
	p.nextToken() // consume 'case'

	beforeErrors := len(p.errors)
	var values []expression
	var variable string
	var typeAnno *typeAnnotation

	// Try to parse value matches or type matches
	// Type match: case Name: Type
	// Value match: case 1, 2, 3
	if p.curIs(tokIdent) && p.pkIs(tokColon) {
		// Type match: case Name: Type
		variable = p.cur.lexeme
		p.nextToken() // consume variable name
		p.nextToken() // consume ':'
		typeAnno = p.parseTypeAnnotation()
	} else {
		// Value match: case expr, expr, ...
		if p.curIs(tokLBrace) {
			p.addError("expected case expression before block")
			p.skipBraceBlock()
			return nil
		}
		values = append(values, p.parseExpression())
		for p.curIs(tokComma) {
			p.nextToken()
			values = append(values, p.parseExpression())
		}
	}
	if len(p.errors) > beforeErrors {
		return nil
	}

	var guard expression
	if p.curIs(tokWhen) {
		p.nextToken()
		guard = p.parseExpression()
		if len(p.errors) > beforeErrors {
			return nil
		}
	}

	body := p.parseBlockStmt()
	if len(p.errors) > beforeErrors {
		return nil
	}

	return &caseClause{
		tok:            tok,
		Values:         values,
		Variable:       variable,
		TypeAnnotation: typeAnno,
		Guard:          guard,
		Body:           body,
	}
}

func (p *parser) synchronizeWhenCase() {
	if p.curIs(tokCase) || p.curIs(tokElse) || p.curIs(tokRBrace) || p.curIs(tokEOF) || p.curIs(tokError) {
		return
	}

	depth := 0
	for !p.curIs(tokEOF) && !p.curIs(tokError) {
		if depth == 0 && (p.curIs(tokCase) || p.curIs(tokElse) || p.curIs(tokRBrace)) {
			return
		}
		if p.curIs(tokLBrace) {
			depth++
		} else if p.curIs(tokRBrace) {
			if depth == 0 {
				return
			}
			depth--
		}
		p.nextToken()
	}
}

func (p *parser) skipBraceBlock() {
	if !p.curIs(tokLBrace) {
		return
	}
	depth := 0
	for !p.curIs(tokEOF) && !p.curIs(tokError) {
		if p.curIs(tokLBrace) {
			depth++
		} else if p.curIs(tokRBrace) {
			depth--
			p.nextToken()
			if depth == 0 {
				return
			}
			continue
		}
		p.nextToken()
	}
}

func (p *parser) parseBreakStmt() statement {
	tok := p.cur
	p.nextToken() // consume 'break'
	if p.curIs(tokSemicolon) {
		p.nextToken()
	}
	return &breakStmt{tok: tok}
}

func (p *parser) parseContinueStmt() statement {
	tok := p.cur
	p.nextToken() // consume 'continue'
	if p.curIs(tokSemicolon) {
		p.nextToken()
	}
	return &continueStmt{tok: tok}
}

// parseTryStmt parses `try { ... } catch (e) { ... }`. The catch clause is
// required: every try must declare how the captured error is bound.
func (p *parser) parseTryStmt() statement {
	tok := p.cur
	p.nextToken() // consume 'try'

	body := p.parseBlockStmt()

	var catchVar *ident
	var catchBody *blockStmt
	if p.curIs(tokCatch) {
		p.nextToken() // consume 'catch'
		p.expect(tokLParen)
		catchVar = &ident{tok: p.cur, Value: p.expectIdent()}
		p.expect(tokRParen)
		catchBody = p.parseBlockStmt()
	}

	if catchBody == nil {
		p.addError("try requires a catch clause: try { ... } catch (e) { ... }")
		return nil
	}

	return &tryStmt{
		tok:       tok,
		Body:      body,
		CatchVar:  catchVar,
		CatchBody: catchBody,
	}
}

// parseDeferStmt parses `defer { ... }`. The deferred body must be a block.
func (p *parser) parseDeferStmt() statement {
	tok := p.cur
	p.nextToken() // consume 'defer'

	if !p.curIs(tokLBrace) {
		p.addError("defer requires a block body: defer { ... }")
		return nil
	}

	body := p.parseBlockStmt()
	return &deferStmt{tok: tok, Body: body}
}

// --- Pratt expression parser ---

func (p *parser) parseExpression() expression {
	return p.parseExpressionWithPrecedence(lowestPrec)
}

// parseExpressionChecked parses an expression and reports whether the parser
// consumed any input doing so. Container-literal loops must break when a
// nested expression fails without progress; addError does not advance the
// cursor, so retrying the same token would loop forever.
func (p *parser) parseExpressionChecked() (expression, bool) {
	before := p.cur
	expr := p.parseExpression()
	if expr == nil && p.cur == before {
		return nil, false
	}
	return expr, true
}

func (p *parser) parseExpressionWithPrecedence(prec int) expression {
	// Prefix parsing
	left := p.parsePrefix()
	if left == nil {
		return nil
	}

	// Optional-chaining bookkeeping: once a postfix chain contains a `?.`
	// link, the entire chain (up to the first non-postfix operator) is
	// wrapped in an optionalChainExpr so the compiler can short-circuit
	// every remaining link when an optional receiver is null.
	optionalChain := false
	var optTok token
	finalizeOptional := func(expr expression) expression {
		if !optionalChain {
			return expr
		}
		optionalChain = false
		return &optionalChainExpr{tok: optTok, Expr: expr}
	}

	for {
		if prec < p.curPrecedence() {
			// Postfix operators extend the current chain; any other
			// (binary/assignment) operator terminates it, so the chain
			// wrapper must be applied to `left` before descending.
			if isPostfixOperator(p.cur.typ) {
				if p.curIs(tokQuestionDot) {
					optionalChain = true
					optTok = p.cur
				}
			} else {
				left = finalizeOptional(left)
			}
			left = p.parseInfix(left)
			if left == nil {
				return nil
			}
			continue
		}

		// Struct literal: uppercase identifier followed by { is parsed as TypeName{field: value, ...}.
		// This cannot go in the precedence table because { also starts block statements.
		// Convention: type names are uppercase; variable names are lowercase.
		if p.curIs(tokLBrace) {
			if ident, ok := left.(*identExpr); ok && len(ident.Value) > 0 && ident.Value[0] >= 'A' && ident.Value[0] <= 'Z' {
				left = p.parseInfix(left)
				if left == nil {
					return nil
				}
				continue
			}
		}

		break
	}

	return finalizeOptional(left)
}

// isPostfixOperator reports whether the given operator token extends the
// current postfix chain (member access, optional member access, call,
// index) rather than starting a new binary/assignment operand relation.
func isPostfixOperator(typ tokenType) bool {
	switch typ {
	case tokDot, tokQuestionDot, tokLParen, tokLBracket:
		return true
	}
	return false
}

func (p *parser) parsePrefix() expression {
	switch p.cur.typ {
	case tokIntLit:
		return p.parseIntLiteral()
	case tokFloatLit:
		return p.parseFloatLiteral()
	case tokStringLit:
		return p.parseStringLiteral()
	case tokTrue:
		return p.parseBoolLiteral(true)
	case tokFalse:
		return p.parseBoolLiteral(false)
	case tokNull:
		return p.parseNullLiteral()
	case tokIdent:
		// `name => expr` — single-parameter short form. '=>' cannot follow an
		// identifier in any other production, so the lookahead is decisive.
		if p.pkIs(tokFatArrow) {
			return p.parseSingleParamArrowLambda()
		}
		return p.parseIdentifier()
	case tokBang:
		return p.parseUnaryExpr()
	case tokMinus:
		return p.parseUnaryExpr()
	case tokLParen:
		// `(params): R => expr` — speculative; rewind to the grouped
		// expression when the tokens are not an arrow lambda.
		if lam := p.tryParseArrowLambda(); lam != nil {
			return lam
		}
		return p.parseGroupedExpr()
	case tokThis:
		return p.parseThisExpr()
	case tokLBracket:
		return p.parseArrayLiteral()
	case tokLBrace:
		return p.parseMapLiteral()
	case tokNew:
		return p.parseNewExpr()
	case tokSuper:
		return p.parseSuperExpr()
	case tokFun:
		// Only the anonymous form `fun(params)...` is a lambda expression; a
		// named `fun name(...)` declaration never starts an expression. Guard
		// on the lookahead so statement-level recovery keeps working.
		if p.peekIs(tokLParen) {
			return p.parseLambdaExpr()
		}
		p.addError(fmt.Sprintf("unexpected token in expression: %s", p.cur.lexeme))
		return nil
	default:
		p.addError(fmt.Sprintf("unexpected token in expression: %s", p.cur.lexeme))
		return nil
	}
}

func (p *parser) parseInfix(left expression) expression {
	switch p.cur.typ {
	case tokPlus, tokMinus, tokStar, tokSlash, tokPercent,
		tokEqEq, tokBangEq, tokLT, tokLTEq, tokGT, tokGTEq,
		tokAnd, tokOr:
		return p.parseBinaryExpr(left)
	case tokIs:
		return p.parseTypeCheckExpr(left)
	case tokAs:
		return p.parseTypeCastExpr(left)
	case tokEq:
		return p.parseAssignExpr(left)
	case tokQuestionQuestion:
		return p.parseNullCoalesceExpr(left)
	case tokQuestionDot:
		return p.parseOptionalMemberExpr(left)
	case tokLParen:
		return p.parseCallExpr(left)
	case tokDot:
		return p.parseMemberExpr(left)
	case tokLBracket:
		return p.parseIndexExpr(left)
	case tokLBrace:
		return p.parseStructLiteralExpr(left)
	default:
		p.addError(fmt.Sprintf("unexpected infix Operator: %s", p.cur.lexeme))
		return nil
	}
}

// --- Prefix expression parsers ---

func (p *parser) parseIntLiteral() expression {
	tok := p.cur
	val := p.cur.intVal
	p.nextToken()
	return &intLiteral{tok: tok, Value: val}
}

func (p *parser) parseFloatLiteral() expression {
	tok := p.cur
	val := p.cur.floatVal
	p.nextToken()
	return &floatLiteral{tok: tok, Value: val}
}

func (p *parser) parseStringLiteral() expression {
	tok := p.cur
	// Strip surrounding quotes from the lexeme.
	val := tok.lexeme
	if len(val) >= 2 && val[0] == '"' && val[len(val)-1] == '"' {
		val = val[1 : len(val)-1]
	}
	p.nextToken()
	return &stringLiteral{tok: tok, Value: val}
}

func (p *parser) parseBoolLiteral(val bool) expression {
	tok := p.cur
	p.nextToken()
	return &boolLiteral{tok: tok, Value: val}
}

func (p *parser) parseNullLiteral() expression {
	tok := p.cur
	p.nextToken()
	return &nullLiteral{tok: tok}
}

func (p *parser) parseIdentifier() expression {
	tok := p.cur
	val := p.cur.lexeme
	p.nextToken()
	return &identExpr{tok: tok, Value: val}
}

func (p *parser) parseUnaryExpr() expression {
	tok := p.cur
	op := p.cur.lexeme
	p.nextToken()
	right := p.parseExpressionWithPrecedence(prefixPrec)
	return &unaryExpr{tok: tok, Operator: op, Right: right}
}

func (p *parser) parseGroupedExpr() expression {
	p.nextToken() // consume '('
	expr := p.parseExpression()
	p.expect(tokRParen)
	return expr
}

// parseLambdaExpr parses an anonymous function expression:
//
//	fun(params): returnType { ... }   (block body)
//	fun(params): returnType = expr    (expression body)
//
// The lambda form is only reachable from expression context; named `fun`
// declarations are parsed at top level / class-member level, so the two
// productions never conflict.
func (p *parser) parseLambdaExpr() expression {
	tok := p.cur
	p.nextToken() // consume 'fun'

	p.expect(tokLParen)
	params := p.parseParamList()
	p.expect(tokRParen)

	var returnType *typeAnnotation
	if p.curIs(tokColon) {
		p.nextToken()
		returnType = p.parseTypeAnnotation()
	} else if p.curIs(tokArrow) {
		p.nextToken()
		returnType = p.parseTypeAnnotation()
	}

	lambda := &lambdaExpr{
		tok:        tok,
		Params:     params,
		ReturnType: returnType,
	}

	if p.curIs(tokLBrace) {
		lambda.Body = p.parseBlockStmt()
	} else if p.curIs(tokEq) {
		p.nextToken()
		lambda.ExprBody = p.parseExpression()
	} else {
		p.addError(fmt.Sprintf("expected lambda body '{' or '=', got %s", p.cur.lexeme))
		return nil
	}
	return lambda
}

// tryParseArrowLambda speculatively parses the short-form arrow lambda
//
//	(params): R => expr
//	(params) => expr
//	() => expr
//
// starting at the current '(' token. The parameter list, the optional return
// annotation, and the '=>' introducer are scanned first; if the tokens do not
// have that shape, the parser rewinds and returns nil so the caller falls
// back to the grouped-expression production. The body is always a single
// expression — block bodies stay on the `fun` form.
func (p *parser) tryParseArrowLambda() expression {
	state := p.saveState()
	if lam := p.parseArrowLambdaAtParen(); lam != nil {
		return lam
	}
	p.restoreState(state)
	return nil
}

// parseArrowLambdaAtParen parses an arrow lambda assuming cur is '('.
// It returns nil (without rewinding; the caller owns the snapshot) whenever
// the tokens are not `( paramList ) (: R)? =>`.
func (p *parser) parseArrowLambdaAtParen() expression {
	tok := p.cur
	p.nextToken() // consume '('

	params, ok := p.parseArrowParamList()
	if !ok || !p.curIs(tokRParen) {
		return nil
	}
	p.nextToken() // consume ')'

	var returnType *typeAnnotation
	if p.curIs(tokColon) {
		p.nextToken()
		before := len(p.errors)
		returnType = p.parseTypeAnnotation()
		if len(p.errors) > before {
			return nil
		}
	}

	if !p.curIs(tokFatArrow) {
		return nil
	}
	p.nextToken() // consume '=>'

	body := p.parseExpression()
	if body == nil {
		return nil
	}
	return &lambdaExpr{
		tok:        tok,
		Params:     params,
		ReturnType: returnType,
		ExprBody:   body,
	}
}

// parseArrowParamList scans `name` or `name: Type` entries separated by
// commas, stopping at ')'. Unlike parseParamList it accepts an un-annotated
// parameter without consuming a type for it, and reports failure (false)
// instead of recording errors, because it runs inside a speculative parse
// that rewinds on failure.
func (p *parser) parseArrowParamList() ([]*param, bool) {
	var params []*param
	seen := make(map[string]bool)
	for !p.curIs(tokRParen) {
		if p.curIs(tokEOF) || p.curIs(tokError) || !p.curIs(tokIdent) {
			return nil, false
		}
		nameTok := p.cur
		if seen[nameTok.lexeme] {
			return nil, false
		}
		seen[nameTok.lexeme] = true
		p.nextToken()

		var paramType *typeAnnotation
		if p.curIs(tokColon) {
			p.nextToken()
			before := len(p.errors)
			paramType = p.parseTypeAnnotation()
			if len(p.errors) > before {
				return nil, false
			}
		}

		params = append(params, &param{
			Name:  &ident{tok: nameTok, Value: nameTok.lexeme},
			Type_: paramType,
		})

		if p.curIs(tokComma) {
			p.nextToken()
			continue
		}
		if !p.curIs(tokRParen) {
			return nil, false
		}
	}
	return params, true
}

// parseSingleParamArrowLambda parses the one-parameter short form
//
//	name => expr
//
// The caller guarantees that the lookahead after the identifier is '=>',
// which cannot occur after an identifier in any other production, so no
// speculation is needed. The parameter is untyped; use the parenthesized
// form for annotations.
func (p *parser) parseSingleParamArrowLambda() expression {
	tok := p.cur
	name := tok.lexeme
	p.nextToken() // consume name; cur is now '=>'
	p.nextToken() // consume '=>'

	body := p.parseExpression()
	if body == nil {
		return nil
	}
	return &lambdaExpr{
		tok:      tok,
		Params:   []*param{{Name: &ident{tok: tok, Value: name}}},
		ExprBody: body,
	}
}

func (p *parser) parseThisExpr() expression {
	tok := p.cur
	p.nextToken()
	return &thisExpr{tok: tok}
}

func (p *parser) parseArrayLiteral() expression {
	tok := p.cur
	p.nextToken() // consume '['

	var elements []expression
	for !p.curIs(tokRBracket) && !p.curIs(tokEOF) && !p.curIs(tokError) {
		elem, ok := p.parseExpressionChecked()
		if !ok {
			break
		}
		elements = append(elements, elem)
		if p.curIs(tokComma) {
			p.nextToken()
		}
	}
	p.expect(tokRBracket)
	return &arrayLiteral{tok: tok, Elements: elements}
}

func (p *parser) parseMapLiteral() expression {
	tok := p.cur
	p.nextToken() // consume '{'

	var pairs []mapPair
	for !p.curIs(tokRBrace) && !p.curIs(tokEOF) && !p.curIs(tokError) {
		// Key must be a string literal.
		if p.cur.typ != tokStringLit {
			p.addError(fmt.Sprintf("expected string key in map literal, got %s", p.cur.lexeme))
			return nil
		}
		key := p.parseStringLiteral()

		p.expect(tokColon)

		value, ok := p.parseExpressionChecked()
		if !ok {
			break
		}
		pairs = append(pairs, mapPair{Key: key, Value: value})

		if p.curIs(tokComma) {
			p.nextToken()
		}
	}
	p.expect(tokRBrace)
	return &mapLiteral{tok: tok, Pairs: pairs}
}

func (p *parser) parseStructLiteralExpr(left expression) expression {
	// left must be an identifier (the type name).
	ident, ok := left.(*identExpr)
	if !ok {
		p.addError("struct literal requires type name before {")
		return nil
	}
	typeName := ident.Value
	p.nextToken() // consume {

	var fields []structFieldInit
	hasZeroFill := false
	for !p.curIs(tokRBrace) && !p.curIs(tokEOF) && !p.curIs(tokError) {
		if p.curIs(tokDotDot) {
			if hasZeroFill {
				p.addError("spread '..' may appear at most once in a struct literal")
				return nil
			}
			hasZeroFill = true
			p.nextToken() // consume ..
			if p.curIs(tokComma) {
				p.nextToken()
			}
			if !p.curIs(tokRBrace) {
				p.addError("spread '..' must be the last element in a struct literal")
				return nil
			}
			break
		}
		fieldName := p.expectIdent()
		p.expect(tokColon)
		value, ok := p.parseExpressionChecked()
		if !ok {
			break
		}
		fields = append(fields, structFieldInit{Name: fieldName, Value: value})
		if p.curIs(tokComma) {
			p.nextToken()
		}
	}
	p.expect(tokRBrace)
	return &structLiteral{tok: ident.tok, TypeName: typeName, Fields: fields, HasZeroFill: hasZeroFill}
}
func (p *parser) parseNewExpr() expression {
	tok := p.cur
	p.nextToken() // consume 'new'

	className := p.expectIdent()

	var args []expression
	if p.curIs(tokLParen) {
		p.nextToken()
		for !p.curIs(tokRParen) && !p.curIs(tokEOF) && !p.curIs(tokError) {
			arg, ok := p.parseExpressionChecked()
			if !ok {
				break
			}
			args = append(args, arg)
			if p.curIs(tokComma) {
				p.nextToken()
			}
		}
		p.expect(tokRParen)
	}

	return &newExpr{tok: tok, ClassName: className, Arguments: args}
}

func (p *parser) parseSuperExpr() expression {
	tok := p.cur
	p.nextToken() // consume 'super'

	// super.method(args) or super()
	var method *ident
	var args []expression

	if p.curIs(tokDot) {
		p.nextToken() // consume '.'
		methodName := p.expectIdent()
		method = &ident{tok: p.cur, Value: methodName}

		if p.curIs(tokLParen) {
			p.nextToken()
			for !p.curIs(tokRParen) && !p.curIs(tokEOF) && !p.curIs(tokError) {
				arg, ok := p.parseExpressionChecked()
				if !ok {
					break
				}
				args = append(args, arg)
				if p.curIs(tokComma) {
					p.nextToken()
				}
			}
			p.expect(tokRParen)
		}
	} else if p.curIs(tokLParen) {
		p.nextToken()
		for !p.curIs(tokRParen) && !p.curIs(tokEOF) && !p.curIs(tokError) {
			arg, ok := p.parseExpressionChecked()
			if !ok {
				break
			}
			args = append(args, arg)
			if p.curIs(tokComma) {
				p.nextToken()
			}
		}
		p.expect(tokRParen)
	}

	return &superExpr{tok: tok, Method: method, Arguments: args}
}

// --- Infix expression parsers ---

func (p *parser) parseBinaryExpr(left expression) expression {
	tok := p.cur
	op := p.cur.lexeme
	prec := p.curPrecedence()
	p.nextToken()
	right := p.parseExpressionWithPrecedence(prec)
	return &binaryExpr{tok: tok, Left: left, Operator: op, Right: right}
}

func (p *parser) parseTypeCheckExpr(left expression) expression {
	tok := p.cur
	p.nextToken() // consume 'is'
	typeName := p.parseTypeAnnotation()
	return &typeCheckExpr{tok: tok, Left: left, TypeName: typeName.Name}
}

func (p *parser) parseTypeCastExpr(left expression) expression {
	tok := p.cur
	p.nextToken() // consume 'as'
	target := p.parseTypeAnnotation()
	return &typeCastExpr{tok: tok, Left: left, Target: target}
}

func (p *parser) parseAssignExpr(left expression) expression {
	tok := p.cur
	p.nextToken() // consume '='
	val := p.parseExpressionWithPrecedence(assignPrec - 1)
	return &assignExpr{tok: tok, Target: left, Value: val}
}

func (p *parser) parseCallExpr(callee expression) expression {
	tok := p.cur
	p.nextToken() // consume '('

	var args []expression
	for !p.curIs(tokRParen) && !p.curIs(tokEOF) && !p.curIs(tokError) {
		arg, ok := p.parseExpressionChecked()
		if !ok {
			break
		}
		args = append(args, arg)
		if p.curIs(tokComma) {
			p.nextToken()
		}
	}
	p.expect(tokRParen)
	return &callExpr{tok: tok, Callee: callee, Arguments: args}
}

func (p *parser) parseMemberExpr(object expression) expression {
	tok := p.cur
	p.nextToken() // consume '.'

	memberName := p.expectIdent()
	return &memberExpr{tok: tok, Object: object, Member: &ident{tok: p.cur, Value: memberName}}
}

// parseOptionalMemberExpr parses `object?.member`. The memberExpr is marked
// Optional; the enclosing parseExpressionWithPrecedence wraps the whole
// postfix chain in an optionalChainExpr so the compiler can short-circuit
// the remainder of the chain when object is null.
func (p *parser) parseOptionalMemberExpr(object expression) expression {
	tok := p.cur
	p.nextToken() // consume '?.'

	memberName := p.expectIdent()
	return &memberExpr{tok: tok, Object: object, Member: &ident{tok: p.cur, Value: memberName}, Optional: true}
}

// parseNullCoalesceExpr parses `left ?? right`. Left-associative (the right
// side is parsed at the operator's own precedence, so equal-precedence `??`
// on the right belongs to the outer node) and lower precedence than `||`.
func (p *parser) parseNullCoalesceExpr(left expression) expression {
	tok := p.cur
	p.nextToken() // consume '??'
	right := p.parseExpressionWithPrecedence(coalescePrec)
	return &nullCoalesceExpr{tok: tok, Left: left, Right: right}
}

func (p *parser) parseIndexExpr(left expression) expression {
	tok := p.cur
	p.nextToken() // consume '['

	index := p.parseExpression()
	p.expect(tokRBracket)
	return &indexExpr{tok: tok, Left: left, Index: index}
}

// --- Helpers ---

func (p *parser) nextToken() {
	p.cur = p.pk
	p.pk = p.lex.nextToken()
}

// --- Speculative parsing ---

// parserState snapshots the parser position (lexer offset plus the cur/pk
// lookahead window) so a failed speculative production can rewind cleanly.
// lexer is a plain value (source string plus offsets), so copying the struct
// is a complete snapshot.
type parserState struct {
	lexState lexer
	cur      token
	pk       token
	nErrors  int
}

func (p *parser) saveState() parserState {
	return parserState{lexState: *p.lex, cur: p.cur, pk: p.pk, nErrors: len(p.errors)}
}

func (p *parser) restoreState(s parserState) {
	*p.lex = s.lexState
	p.cur = s.cur
	p.pk = s.pk
	p.errors = p.errors[:s.nErrors]
}

func (p *parser) curIs(t tokenType) bool {
	return p.cur.typ == t
}

func (p *parser) pkIs(t tokenType) bool {
	return p.pk.typ == t
}

func (p *parser) expect(t tokenType) {
	if p.curIs(t) {
		p.nextToken()
		return
	}
	p.addError(fmt.Sprintf("expected %s, got %s", t, p.cur.lexeme))
}

func (p *parser) expectIdent() string {
	if p.curIs(tokIdent) {
		name := p.cur.lexeme
		p.nextToken()
		return name
	}
	p.addError(fmt.Sprintf("expected identifier, got %s", p.cur.lexeme))
	return ""
}

// expectFieldName accepts an identifier OR any keyword as a struct/class
// field name. This lets schema authors use `type`, `from`, `case`, etc. as
// JSON tag names (a common need when modeling existing wire shapes) without
// having to invent a separate rename annotation. Only valid inside
// parseFieldList; control-flow contexts continue to use expectIdent.
func (p *parser) expectFieldName() string {
	if p.cur.typ == tokIdent || p.cur.lexeme != "" && isReservedLexeme(p.cur.lexeme) {
		name := p.cur.lexeme
		p.nextToken()
		return name
	}
	p.addError(fmt.Sprintf("expected field name, got %s", p.cur.lexeme))
	return ""
}

// isReservedLexeme reports whether s would otherwise lex as a keyword. Used
// by expectFieldName to allow keywords in struct-field position.
func isReservedLexeme(s string) bool {
	_, ok := keywords[s]
	return ok
}

func (p *parser) expectStringLiteral() string {
	if p.curIs(tokStringLit) {
		value := p.cur.lexeme
		if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
			value = value[1 : len(value)-1]
		}
		p.nextToken()
		return value
	}
	p.addError(fmt.Sprintf("expected string literal, got %s", p.cur.lexeme))
	return ""
}

func (p *parser) addError(msg string) {
	p.errors = append(p.errors, parseError{
		line:   p.cur.line,
		column: p.cur.col,
		msg:    msg,
		code:   diagParseError,
		path:   "frontend/parse/token",
	})
}
