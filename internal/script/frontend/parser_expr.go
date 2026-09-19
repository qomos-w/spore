package frontend

import (
	"fmt"
)

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
