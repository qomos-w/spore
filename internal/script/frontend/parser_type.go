package frontend

import (
	"fmt"
)

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
		// Field decorators: only @ref(T) / @ref(T.field) is valid in field
		// position. Other annotation names are rejected here rather than
		// silently ignored.
		var ref *fieldRef
		for p.curIs(tokAt) {
			p.nextToken() // consume @
			ann := p.expectIdent()
			switch ann {
			case "ref":
				parsed := p.parseRefAnnotationArgs()
				if ref != nil {
					p.addError("duplicate @ref annotation on field")
				} else {
					ref = parsed
				}
			default:
				p.addError(fmt.Sprintf("expected ref annotation, got @%s", ann))
			}
		}
		optional := false
		// `optional` is a field-prefix modifier. Treat as modifier only when
		// followed by something that isn't `:`, so `optional: T` (a field
		// literally named "optional") still parses as a field name.
		if p.curIs(tokOptional) && !p.pkIs(tokColon) {
			optional = true
			p.nextToken()
		}
		// `key` marks the @data table key field, with the same disambiguation
		// as `optional` so `key: T` remains a valid field name.
		key := false
		if p.curIs(tokKey) && !p.pkIs(tokColon) {
			key = true
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
			Key:      key,
			Ref:      ref,
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
