package frontend

import (
	"fmt"
)

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
