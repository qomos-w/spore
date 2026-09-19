package config

import (
	"testing"
)

func TestLexer_Scalars(t *testing.T) {
	tests := []struct {
		input string
		typ   tokenType
		val   string
	}{
		{"42", tokIntLit, "42"},
		{"3.14", tokFloatLit, "3.14"},
		{`"hello"`, tokStringLit, `"hello"`},
		{"true", tokIdent, "true"},
		{"false", tokIdent, "false"},
		{"null", tokIdent, "null"},
		{"host", tokIdent, "host"},
	}
	for _, tt := range tests {
		l := newLexer(tt.input)
		tok := l.nextToken()
		if tok.typ != tt.typ {
			t.Errorf("input %q: expected type %d, got %d", tt.input, tt.typ, tok.typ)
		}
		if tok.lexeme != tt.val {
			t.Errorf("input %q: expected lexeme %q, got %q", tt.input, tt.val, tok.lexeme)
		}
	}
}

func TestLexer_Symbols(t *testing.T) {
	tests := []struct {
		input string
		typ   tokenType
	}{
		{"{", tokLBrace},
		{"}", tokRBrace},
		{"[", tokLBracket},
		{"]", tokRBracket},
		{":", tokColon},
		{",", tokComma},
		{"<", tokLT},
		{">", tokGT},
	}
	for _, tt := range tests {
		l := newLexer(tt.input)
		tok := l.nextToken()
		if tok.typ != tt.typ {
			t.Errorf("input %q: expected type %d, got %d (%q)", tt.input, tt.typ, tok.typ, tok.lexeme)
		}
	}
}

func TestLexer_Newlines(t *testing.T) {
	l := newLexer("a\nb\nc")
	tok := l.nextToken()
	if tok.typ != tokIdent || tok.lexeme != "a" {
		t.Fatalf("expected ident a, got %d %q", tok.typ, tok.lexeme)
	}
	tok = l.nextToken()
	if tok.typ != tokNewline {
		t.Fatalf("expected newline, got %d %q", tok.typ, tok.lexeme)
	}
	tok = l.nextToken()
	if tok.typ != tokIdent || tok.lexeme != "b" {
		t.Fatalf("expected ident b, got %d %q", tok.typ, tok.lexeme)
	}
}

func TestLexer_Comments(t *testing.T) {
	l := newLexer("// comment\nx: 1")
	tok := l.nextToken()
	if tok.typ != tokNewline {
		t.Fatalf("expected newline after comment, got %d %q", tok.typ, tok.lexeme)
	}
	tok = l.nextToken()
	if tok.typ != tokIdent || tok.lexeme != "x" {
		t.Fatalf("expected ident x, got %d %q", tok.typ, tok.lexeme)
	}
}

func TestLexer_IntValue(t *testing.T) {
	l := newLexer("42")
	tok := l.nextToken()
	if tok.intVal != 42 {
		t.Errorf("expected intVal 42, got %d", tok.intVal)
	}
}

func TestLexer_FloatValue(t *testing.T) {
	l := newLexer("3.14")
	tok := l.nextToken()
	if tok.floatVal != 3.14 {
		t.Errorf("expected floatVal 3.14, got %v", tok.floatVal)
	}
}

func TestLexer_MultipleNewlines(t *testing.T) {
	l := newLexer("a\n\n\nb")
	tok := l.nextToken()
	if tok.lexeme != "a" {
		t.Fatalf("expected a, got %q", tok.lexeme)
	}
	tok = l.nextToken()
	if tok.typ != tokNewline {
		t.Fatalf("expected newline, got %d", tok.typ)
	}
	tok = l.nextToken()
	if tok.lexeme != "b" {
		t.Fatalf("expected b, got %q", tok.lexeme)
	}
}

func TestLexer_BlockComment(t *testing.T) {
	l := newLexer("a /* block */ b")
	tok := l.nextToken()
	if tok.lexeme != "a" {
		t.Fatalf("expected a, got %q", tok.lexeme)
	}
	tok = l.nextToken()
	if tok.lexeme != "b" {
		t.Fatalf("expected b after block comment, got %q", tok.lexeme)
	}
}
