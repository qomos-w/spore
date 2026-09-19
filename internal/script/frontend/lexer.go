package frontend

import (
	"strconv"
	"unicode"
)

// lexer tokenizes a source string into a stream of tokens.
type lexer struct {
	source string
	start  int
	pos    int
	line   int
	col    int
}

func newLexer(source string) *lexer {
	return &lexer{source: source, line: 1, col: 1}
}

// nextToken scans and returns the next token from the source.
func (l *lexer) nextToken() token {
	l.skipWhitespace()
	l.start = l.pos
	if l.atEnd() {
		return token{typ: tokEOF, line: l.line, col: l.col}
	}

	ch := l.advance()

	switch {
	case isAlpha(ch):
		return l.identifier()
	case isDigit(ch):
		return l.number()
	case ch == '"':
		return l.stringLiteral()
	case ch == '/':
		if l.match('/') {
			l.lineComment()
			return l.nextToken()
		}
		if l.match('*') {
			l.blockComment()
			return l.nextToken()
		}
		return l.makeToken(tokSlash)
	case ch == '+':
		return l.makeToken(tokPlus)
	case ch == '-':
		if l.match('>') {
			return l.makeToken(tokArrow)
		}
		return l.makeToken(tokMinus)
	case ch == '*':
		return l.makeToken(tokStar)
	case ch == '%':
		return l.makeToken(tokPercent)
	case ch == '=':
		if l.match('=') {
			return l.makeToken(tokEqEq)
		}
		if l.match('>') {
			return l.makeToken(tokFatArrow)
		}
		return l.makeToken(tokEq)
	case ch == '!':
		if l.match('=') {
			return l.makeToken(tokBangEq)
		}
		return l.makeToken(tokBang)
	case ch == '<':
		if l.match('=') {
			return l.makeToken(tokLTEq)
		}
		return l.makeToken(tokLT)
	case ch == '>':
		if l.match('=') {
			return l.makeToken(tokGTEq)
		}
		return l.makeToken(tokGT)
	case ch == '&':
		if l.match('&') {
			return l.makeToken(tokAnd)
		}
		return l.errorToken("expected '&&'")
	case ch == '|':
		if l.match('|') {
			return l.makeToken(tokOr)
		}
		return l.errorToken("expected '||'")
	case ch == '?':
		if l.match('?') {
			return l.makeToken(tokQuestionQuestion)
		}
		if l.match('.') {
			return l.makeToken(tokQuestionDot)
		}
		return l.makeToken(tokQuestion)
	case ch == ':':
		return l.makeToken(tokColon)
	case ch == '.':
		if l.match('.') {
			return l.makeToken(tokDotDot)
		}
		return l.makeToken(tokDot)
	case ch == ',':
		return l.makeToken(tokComma)
	case ch == ';':
		return l.makeToken(tokSemicolon)
	case ch == '(':
		return l.makeToken(tokLParen)
	case ch == ')':
		return l.makeToken(tokRParen)
	case ch == '{':
		return l.makeToken(tokLBrace)
	case ch == '}':
		return l.makeToken(tokRBrace)
	case ch == '[':
		return l.makeToken(tokLBracket)
	case ch == ']':
		return l.makeToken(tokRBracket)
	case ch == '@':
		return l.makeToken(tokAt)
	default:
		return l.errorToken("unexpected character")
	}
}

func (l *lexer) identifier() token {
	for !l.atEnd() && isAlphaNum(l.peek()) {
		l.advance()
	}
	text := l.source[l.start:l.pos]
	typ, ok := keywords[text]
	if !ok {
		typ = tokIdent
	}
	return l.makeToken(typ)
}

func (l *lexer) number() token {
	for !l.atEnd() && isDigit(l.peek()) {
		l.advance()
	}
	if !l.atEnd() && l.peek() == '.' {
		l.advance()
		if !l.atEnd() && isDigit(l.peek()) {
			for !l.atEnd() && isDigit(l.peek()) {
				l.advance()
			}
		}
		tok := l.makeToken(tokFloatLit)
		tok.floatVal, _ = strconv.ParseFloat(tok.lexeme, 64)
		return tok
	}
	tok := l.makeToken(tokIntLit)
	tok.intVal, _ = strconv.ParseInt(tok.lexeme, 10, 64)
	return tok
}

func (l *lexer) stringLiteral() token {
	for !l.atEnd() && l.peek() != '"' {
		if l.peek() == '\\' {
			l.advance()
		}
		if l.peek() == '\n' {
			l.line++
			l.col = 0
		}
		l.advance()
	}
	if l.atEnd() {
		return l.errorToken("unterminated string")
	}
	l.advance() // closing "
	return l.makeToken(tokStringLit)
}

func (l *lexer) lineComment() {
	for !l.atEnd() && l.peek() != '\n' {
		l.advance()
	}
}

func (l *lexer) blockComment() {
	for !l.atEnd() {
		if l.peek() == '*' && l.peekNext() == '/' {
			l.advance()
			l.advance()
			return
		}
		if l.peek() == '\n' {
			l.line++
			l.col = 0
		}
		l.advance()
	}
}

func (l *lexer) skipWhitespace() {
	for !l.atEnd() {
		switch l.peek() {
		case ' ', '\t', '\r':
			l.advance()
		case '\n':
			l.advance()
			l.line++
			l.col = 0
		default:
			return
		}
	}
}

func (l *lexer) advance() byte {
	if l.atEnd() {
		return 0
	}
	ch := l.source[l.pos]
	l.pos++
	l.col++
	return ch
}

func (l *lexer) peek() byte {
	if l.atEnd() {
		return 0
	}
	return l.source[l.pos]
}

func (l *lexer) peekNext() byte {
	if l.pos+1 >= len(l.source) {
		return 0
	}
	return l.source[l.pos+1]
}

func (l *lexer) match(expected byte) bool {
	if l.atEnd() || l.source[l.pos] != expected {
		return false
	}
	l.pos++
	l.col++
	return true
}

func (l *lexer) atEnd() bool {
	return l.pos >= len(l.source)
}

func (l *lexer) makeToken(typ tokenType) token {
	return token{
		typ:    typ,
		lexeme: l.source[l.start:l.pos],
		line:   l.line,
		col:    l.col - (l.pos - l.start),
	}
}

func (l *lexer) errorToken(msg string) token {
	return token{
		typ:    tokError,
		lexeme: msg,
		line:   l.line,
		col:    l.col,
	}
}

func isAlpha(ch byte) bool {
	return ch == '_' || unicode.IsLetter(rune(ch))
}

func isDigit(ch byte) bool {
	return ch >= '0' && ch <= '9'
}

func isAlphaNum(ch byte) bool {
	return isAlpha(ch) || isDigit(ch)
}
