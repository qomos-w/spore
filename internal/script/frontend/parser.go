package frontend

import (
	"fmt"

	"github.com/qomos-w/spore/diagnostics"
)

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
	tokEq:               assignPrec,
	tokQuestionQuestion: coalescePrec,
	tokOr:               orPrec,
	tokAnd:              andPrec,
	tokEqEq:             equalPrec,
	tokBangEq:           equalPrec,
	tokLT:               comparePrec,
	tokLTEq:             comparePrec,
	tokGT:               comparePrec,
	tokGTEq:             comparePrec,
	tokPlus:             sumPrec,
	tokMinus:            sumPrec,
	tokStar:             productPrec,
	tokSlash:            productPrec,
	tokPercent:          productPrec,
	tokLParen:           callPrec,
	tokDot:              memberPrec,
	tokQuestionDot:      memberPrec,
	tokLBracket:         indexPrec,
	tokIs:               comparePrec,
	tokAs:               comparePrec,
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
