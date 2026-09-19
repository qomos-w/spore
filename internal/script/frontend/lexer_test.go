package frontend

import (
	"testing"
)

func collectTokens(source string) []token {
	l := newLexer(source)
	var tokens []token
	for {
		tok := l.nextToken()
		tokens = append(tokens, tok)
		if tok.typ == tokEOF || tok.typ == tokError {
			break
		}
	}
	return tokens
}

func TestLexer_IdentifiersAndKeywords(t *testing.T) {
	tokens := collectTokens("fun export class struct var hello")
	expected := []tokenType{tokFun, tokExport, tokClass, tokStruct, tokVar, tokIdent}
	for i, tok := range tokens[:len(expected)] {
		if tok.typ != expected[i] {
			t.Fatalf("token %d: expected %s, got %s (%q)", i, expected[i], tok.typ, tok.lexeme)
		}
	}
}

func TestLexer_IntegersAndFloats(t *testing.T) {
	tokens := collectTokens("42 3.14 0 100.0")
	expected := []tokenType{tokIntLit, tokFloatLit, tokIntLit, tokFloatLit}
	for i, tok := range tokens[:len(expected)] {
		if tok.typ != expected[i] {
			t.Fatalf("token %d: expected %s, got %s (%q)", i, expected[i], tok.typ, tok.lexeme)
		}
	}
	if tokens[0].lexeme != "42" {
		t.Fatalf("expected lexeme 42, got %q", tokens[0].lexeme)
	}
	if tokens[1].lexeme != "3.14" {
		t.Fatalf("expected lexeme 3.14, got %q", tokens[1].lexeme)
	}
}

func TestLexer_Strings(t *testing.T) {
	tokens := collectTokens(`"hello" "world" "with\"escape"`)
	if tokens[0].typ != tokStringLit || tokens[0].lexeme != `"hello"` {
		t.Fatalf("expected string token, got %s %q", tokens[0].typ, tokens[0].lexeme)
	}
	if tokens[1].typ != tokStringLit || tokens[1].lexeme != `"world"` {
		t.Fatalf("expected string token, got %s %q", tokens[1].typ, tokens[1].lexeme)
	}
	if tokens[2].typ != tokStringLit {
		t.Fatalf("expected string token for escaped, got %s", tokens[2].typ)
	}
}

func TestLexer_Operators(t *testing.T) {
	tokens := collectTokens("+ - * / % = == != < <= > >= && || ! ->")
	expected := []tokenType{tokPlus, tokMinus, tokStar, tokSlash, tokPercent,
		tokEq, tokEqEq, tokBangEq, tokLT, tokLTEq, tokGT, tokGTEq,
		tokAnd, tokOr, tokBang, tokArrow}
	for i, tok := range tokens[:len(expected)] {
		if tok.typ != expected[i] {
			t.Fatalf("token %d: expected %s, got %s (%q)", i, expected[i], tok.typ, tok.lexeme)
		}
	}
}

func TestLexer_FatArrow(t *testing.T) {
	// `=>` must lex as one token and stay distinct from `==`, `=`, `>`,
	// `>=`, and the `->` arrow used by lambda return-type position.
	tokens := collectTokens("=> = => == >= -> x=>y")
	expected := []tokenType{tokFatArrow, tokEq, tokFatArrow, tokEqEq, tokGTEq, tokArrow, tokIdent, tokFatArrow, tokIdent}
	for i, tok := range tokens[:len(expected)] {
		if tok.typ != expected[i] {
			t.Fatalf("token %d: expected %s, got %s (%q)", i, expected[i], tok.typ, tok.lexeme)
		}
	}
	if tokens[0].lexeme != "=>" {
		t.Fatalf("expected lexeme \"=>\", got %q", tokens[0].lexeme)
	}
}

func TestLexer_Delimiters(t *testing.T) {
	tokens := collectTokens("( ) { } [ ] : . , ; ?")
	expected := []tokenType{tokLParen, tokRParen, tokLBrace, tokRBrace,
		tokLBracket, tokRBracket, tokColon, tokDot, tokComma, tokSemicolon, tokQuestion}
	for i, tok := range tokens[:len(expected)] {
		if tok.typ != expected[i] {
			t.Fatalf("token %d: expected %s, got %s (%q)", i, expected[i], tok.typ, tok.lexeme)
		}
	}
}

func TestLexer_WhitespaceAndComments(t *testing.T) {
	tokens := collectTokens("fun // comment\nclass /* block */ struct")
	expected := []tokenType{tokFun, tokClass, tokStruct}
	for i, tok := range tokens[:len(expected)] {
		if tok.typ != expected[i] {
			t.Fatalf("token %d: expected %s, got %s (%q)", i, expected[i], tok.typ, tok.lexeme)
		}
	}
}

func TestLexer_LineAndColumn(t *testing.T) {
	tokens := collectTokens("fun\nclass")
	if tokens[0].line != 1 {
		t.Fatalf("expected fun on line 1, got %d", tokens[0].line)
	}
	if tokens[1].line != 2 {
		t.Fatalf("expected class on line 2, got %d", tokens[1].line)
	}
	if tokens[0].col < 1 {
		t.Fatalf("expected fun col >= 1, got %d", tokens[0].col)
	}
}

func TestLexer_EOF(t *testing.T) {
	l := newLexer("")
	tok := l.nextToken()
	if tok.typ != tokEOF {
		t.Fatalf("expected EOF, got %s", tok.typ)
	}
}

func TestLexer_UnexpectedCharacter(t *testing.T) {
	l := newLexer("#")
	tok := l.nextToken()
	if tok.typ != tokError {
		t.Fatalf("expected error token, got %s", tok.typ)
	}
}

func TestLexer_CanonicalKeywordsLex(t *testing.T) {
	tokens := collectTokens("fun struct")
	if tokens[0].typ != tokFun {
		t.Fatalf("expected 'fun' to lex as tokFun, got %s", tokens[0].typ)
	}
	if tokens[1].typ != tokStruct {
		t.Fatalf("expected 'struct' to lex as tokStruct, got %s", tokens[1].typ)
	}
}

func TestLexer_AllKeptKeywords(t *testing.T) {
	source := "fun export class struct type var package import void any " +
		"return if else for while in when case break continue is as this true false null"
	tokens := collectTokens(source)
	expected := []tokenType{tokFun, tokExport, tokClass, tokStruct, tokType, tokVar,
		tokPackage, tokImport, tokVoid, tokAny, tokReturn, tokIf, tokElse, tokFor,
		tokWhile, tokIn, tokWhen, tokCase, tokBreak, tokContinue, tokIs, tokAs,
		tokThis, tokTrue, tokFalse, tokNull}
	for i, tok := range tokens[:len(expected)] {
		if tok.typ != expected[i] {
			t.Fatalf("keyword %d: expected %s, got %s (%q)", i, expected[i], tok.typ, tok.lexeme)
		}
	}
}

func TestLexer_StreamAndYieldKeywords(t *testing.T) {
	tokens := collectTokens("stream yield")
	expected := []tokenType{tokStream, tokYield}

	for i, want := range expected {
		if tokens[i].typ != want {
			t.Fatalf("token %d: expected %s, got %s (%q)", i, want, tokens[i].typ, tokens[i].lexeme)
		}
	}
}

func TestLexer_AllScalarTypeKeywords(t *testing.T) {
	keywords := []struct {
		text string
		want tokenType
	}{
		{"bool", tokBool},
		{"byte", tokByte},
		{"short", tokShort},
		{"ushort", tokUShort},
		{"int", tokInt},
		{"uint", tokUint},
		{"long", tokLong},
		{"ulong", tokUlong},
		{"float", tokFloat},
		{"double", tokDouble},
		{"string", tokString},
		{"bytes", tokBytes},
	}
	for _, kw := range keywords {
		tokens := collectTokens(kw.text)
		if tokens[0].typ != kw.want {
			t.Errorf("keyword %q: expected %s, got %s", kw.text, kw.want, tokens[0].typ)
		}
	}
}
