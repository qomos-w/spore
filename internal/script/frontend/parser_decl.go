package frontend

import (
	"fmt"
)

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

// parseDecoratedStructStmt parses a decorator list (@schema(N), @component,
// @data, @version(N)) followed by a struct declaration. It is the entry point
// from the top-level `@` dispatch; plain struct declarations go directly to
// parseStructStmt.
func (p *parser) parseDecoratedStructStmt(exported bool) *structStmt {
	tok := p.cur // the @ token

	schemaID := uint64(0)
	isComponent := false
	isData := false
	dataVersion := uint64(0)
	hasVersion := false
	for p.curIs(tokAt) {
		p.nextToken() // consume @
		ann := p.expectIdent()
		switch ann {
		case "schema":
			schemaID = p.parseUintAnnotationArgs("schema id")
		case "component":
			if isComponent {
				p.addError("duplicate @component annotation")
			}
			isComponent = true
		case "data":
			if isData {
				p.addError("duplicate @data annotation")
			}
			isData = true
			if p.curIs(tokLParen) {
				p.addError("@data takes no arguments")
				// Resync past the argument list so the following struct
				// declaration still parses and downstream errors stay useful.
				for !p.curIs(tokRParen) && !p.curIs(tokEOF) && !p.curIs(tokError) {
					p.nextToken()
				}
				if p.curIs(tokRParen) {
					p.nextToken()
				}
			}
		case "version":
			if hasVersion {
				p.addError("duplicate @version annotation")
			}
			hasVersion = true
			dataVersion = p.parseUintAnnotationArgs("data version")
		default:
			p.addError(fmt.Sprintf("expected schema, component, data or version annotation, got @%s", ann))
		}
	}

	// Decorators are order-independent; judge the role rules once the list is
	// complete.
	if isData && isComponent {
		p.addError("@data and @component are mutually exclusive")
	}
	if isData && schemaID != 0 {
		p.addError("@data structs must not carry @schema(N): data tables are not transport schemas")
	}
	if hasVersion && !isData {
		p.addError("@version is only valid on a @data struct")
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
		IsData:      isData,
		DataVersion: dataVersion,
	}
}

// parseUintAnnotationArgs parses `(N)` following an annotation name that takes
// a single unsigned integer argument (e.g. @schema(300), @version(2)).
func (p *parser) parseUintAnnotationArgs(what string) uint64 {
	p.expect(tokLParen)
	if !p.curIs(tokIntLit) {
		p.addError(fmt.Sprintf("expected integer %s, got %s", what, p.cur.lexeme))
		return 0
	}
	id := uint64(p.cur.intVal)
	p.nextToken()
	p.expect(tokRParen)
	return id
}

// parseRefAnnotationArgs parses `(T)` or `(T.field)` following the `ref` field
// annotation name. An empty Field means the reference targets the referenced
// struct's key field.
func (p *parser) parseRefAnnotationArgs() *fieldRef {
	p.expect(tokLParen)
	target := p.expectIdent()
	if target == "" {
		p.expect(tokRParen)
		return nil
	}
	ref := &fieldRef{Target: target}
	if p.curIs(tokDot) {
		p.nextToken()
		ref.Field = p.expectIdent()
	}
	p.expect(tokRParen)
	return ref
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
