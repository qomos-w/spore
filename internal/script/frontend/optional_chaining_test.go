package frontend

import (
	"testing"
)

// --- Lexer ---

func TestLexer_QuestionQuestion(t *testing.T) {
	tokens := collectTokens("a ?? b")
	expected := []tokenType{tokIdent, tokQuestionQuestion, tokIdent, tokEOF}
	for i, want := range expected {
		if tokens[i].typ != want {
			t.Fatalf("token %d: expected %s, got %s (%q)", i, want, tokens[i].typ, tokens[i].lexeme)
		}
	}
}

func TestLexer_QuestionDot(t *testing.T) {
	tokens := collectTokens("a?.b")
	expected := []tokenType{tokIdent, tokQuestionDot, tokIdent, tokEOF}
	for i, want := range expected {
		if tokens[i].typ != want {
			t.Fatalf("token %d: expected %s, got %s (%q)", i, want, tokens[i].typ, tokens[i].lexeme)
		}
	}
}

func TestLexer_LoneQuestionUnchanged(t *testing.T) {
	tokens := collectTokens("a ? b")
	if tokens[1].typ != tokQuestion {
		t.Fatalf("expected lone '?' to lex as tokQuestion, got %s (%q)", tokens[1].typ, tokens[1].lexeme)
	}
}

func TestLexer_QuestionFollowedByQuestionDot(t *testing.T) {
	// `a ??? .b` lexes as ?? then ?. then ident — maximal munch on '?'.
	tokens := collectTokens("a???.b")
	expected := []tokenType{tokIdent, tokQuestionQuestion, tokQuestionDot, tokIdent, tokEOF}
	for i, want := range expected {
		if tokens[i].typ != want {
			t.Fatalf("token %d: expected %s, got %s (%q)", i, want, tokens[i].typ, tokens[i].lexeme)
		}
	}
}

// --- Parser: null coalescing ---

func TestParser_NullCoalesce(t *testing.T) {
	prog := parseProg(t, "fun f(): int = x ?? y")
	fun := prog.Stmts[0].(*funStmt)

	root, ok := fun.ExprBody.(*nullCoalesceExpr)
	if !ok {
		t.Fatalf("expected root *nullCoalesceExpr, got %T", fun.ExprBody)
	}
	if id, ok := root.Left.(*identExpr); !ok || id.Value != "x" {
		t.Fatalf("expected left ident 'x', got %T", root.Left)
	}
	if id, ok := root.Right.(*identExpr); !ok || id.Value != "y" {
		t.Fatalf("expected right ident 'y', got %T", root.Right)
	}
}

func TestParser_NullCoalesceLeftAssociative(t *testing.T) {
	prog := parseProg(t, "fun f(): int = x ?? y ?? z")
	fun := prog.Stmts[0].(*funStmt)

	root, ok := fun.ExprBody.(*nullCoalesceExpr)
	if !ok {
		t.Fatalf("expected root *nullCoalesceExpr, got %T", fun.ExprBody)
	}
	nested, ok := root.Left.(*nullCoalesceExpr)
	if !ok {
		t.Fatalf("expected left-nested *nullCoalesceExpr (left associativity), got %T", root.Left)
	}
	if id, ok := nested.Left.(*identExpr); !ok || id.Value != "x" {
		t.Fatalf("expected nested left 'x', got %T", nested.Left)
	}
	if id, ok := nested.Right.(*identExpr); !ok || id.Value != "y" {
		t.Fatalf("expected nested right 'y', got %T", nested.Right)
	}
	if id, ok := root.Right.(*identExpr); !ok || id.Value != "z" {
		t.Fatalf("expected outer right 'z', got %T", root.Right)
	}
}

func TestParser_NullCoalesceLowerPrecedenceThanOr(t *testing.T) {
	// `a ?? b || c` parses as `a ?? (b || c)` because ?? binds looser than ||.
	prog := parseProg(t, "fun f(): bool = a ?? b || c")
	fun := prog.Stmts[0].(*funStmt)

	root, ok := fun.ExprBody.(*nullCoalesceExpr)
	if !ok {
		t.Fatalf("expected root *nullCoalesceExpr, got %T", fun.ExprBody)
	}
	or, ok := root.Right.(*binaryExpr)
	if !ok || or.Operator != "||" {
		t.Fatalf("expected right *binaryExpr '||', got %T", root.Right)
	}
}

func TestParser_NullCoalesceHigherPrecedenceThanAssign(t *testing.T) {
	// `x = a ?? b`: the ?? expression is the assignment value.
	prog := parseProg(t, "fun f(): void { var x: any = a ?? b }")
	fun := prog.Stmts[0].(*funStmt)
	decl := fun.Body.Stmts[0].(*varStmt)

	_, ok := decl.Value.(*nullCoalesceExpr)
	if !ok {
		t.Fatalf("expected var initializer *nullCoalesceExpr, got %T", decl.Value)
	}
}

// --- Parser: optional chaining ---

func TestParser_OptionalMemberAccess(t *testing.T) {
	prog := parseProg(t, "fun f(): any = x?.field")
	fun := prog.Stmts[0].(*funStmt)

	chain, ok := fun.ExprBody.(*optionalChainExpr)
	if !ok {
		t.Fatalf("expected root *optionalChainExpr, got %T", fun.ExprBody)
	}
	member, ok := chain.Expr.(*memberExpr)
	if !ok {
		t.Fatalf("expected chain to hold *memberExpr, got %T", chain.Expr)
	}
	if !member.Optional {
		t.Fatal("expected memberExpr.Optional to be set")
	}
	if member.Member.Value != "field" {
		t.Fatalf("expected member 'field', got %q", member.Member.Value)
	}
}

func TestParser_OptionalMethodCall(t *testing.T) {
	prog := parseProg(t, "fun f(): any = x?.greet(1, 2)")
	fun := prog.Stmts[0].(*funStmt)

	chain, ok := fun.ExprBody.(*optionalChainExpr)
	if !ok {
		t.Fatalf("expected root *optionalChainExpr, got %T", fun.ExprBody)
	}
	call, ok := chain.Expr.(*callExpr)
	if !ok {
		t.Fatalf("expected chain to hold *callExpr, got %T", chain.Expr)
	}
	member, ok := call.Callee.(*memberExpr)
	if !ok {
		t.Fatalf("expected callee *memberExpr, got %T", call.Callee)
	}
	if !member.Optional {
		t.Fatal("expected callee memberExpr.Optional to be set")
	}
	if len(call.Arguments) != 2 {
		t.Fatalf("expected 2 arguments, got %d", len(call.Arguments))
	}
}

func TestParser_NestedOptionalChain(t *testing.T) {
	prog := parseProg(t, "fun f(): any = a?.b?.c")
	fun := prog.Stmts[0].(*funStmt)

	chain, ok := fun.ExprBody.(*optionalChainExpr)
	if !ok {
		t.Fatalf("expected root *optionalChainExpr, got %T", fun.ExprBody)
	}
	outer, ok := chain.Expr.(*memberExpr)
	if !ok || !outer.Optional {
		t.Fatalf("expected outer optional *memberExpr, got %T (optional=%v)", chain.Expr, outer.Optional)
	}
	if outer.Member.Value != "c" {
		t.Fatalf("expected outer member 'c', got %q", outer.Member.Value)
	}
	inner, ok := outer.Object.(*memberExpr)
	if !ok || !inner.Optional {
		t.Fatalf("expected inner optional *memberExpr, got %T (optional=%v)", outer.Object, inner.Optional)
	}
	if inner.Member.Value != "b" {
		t.Fatalf("expected inner member 'b', got %q", inner.Member.Value)
	}
	if id, ok := inner.Object.(*identExpr); !ok || id.Value != "a" {
		t.Fatalf("expected base ident 'a', got %T", inner.Object)
	}
}

func TestParser_OptionalChainMixedWithPlainMember(t *testing.T) {
	// `a?.b.c`: the short-circuit region covers the plain `.c` link too.
	prog := parseProg(t, "fun f(): any = a?.b.c")
	fun := prog.Stmts[0].(*funStmt)

	chain, ok := fun.ExprBody.(*optionalChainExpr)
	if !ok {
		t.Fatalf("expected root *optionalChainExpr, got %T", fun.ExprBody)
	}
	plain, ok := chain.Expr.(*memberExpr)
	if !ok {
		t.Fatalf("expected chain to hold *memberExpr, got %T", chain.Expr)
	}
	if plain.Optional {
		t.Fatal("plain `.c` link must not be marked optional")
	}
	opt, ok := plain.Object.(*memberExpr)
	if !ok || !opt.Optional {
		t.Fatalf("expected optional `?.b` link, got %T (optional=%v)", plain.Object, opt.Optional)
	}
}

func TestParser_OptionalChainCombinedWithNullCoalesce(t *testing.T) {
	// `a?.b ?? c`: the chain wrapper covers only `a?.b`; ?? applies outside.
	prog := parseProg(t, "fun f(): any = a?.b ?? c")
	fun := prog.Stmts[0].(*funStmt)

	root, ok := fun.ExprBody.(*nullCoalesceExpr)
	if !ok {
		t.Fatalf("expected root *nullCoalesceExpr, got %T", fun.ExprBody)
	}
	chain, ok := root.Left.(*optionalChainExpr)
	if !ok {
		t.Fatalf("expected ?? left to be *optionalChainExpr, got %T", root.Left)
	}
	if _, ok := chain.Expr.(*memberExpr); !ok {
		t.Fatalf("expected chain to hold *memberExpr, got %T", chain.Expr)
	}
	if id, ok := root.Right.(*identExpr); !ok || id.Value != "c" {
		t.Fatalf("expected right ident 'c', got %T", root.Right)
	}
}

func TestParser_OptionalChainInCallArguments(t *testing.T) {
	// Inner chain inside an argument list is its own short-circuit region.
	prog := parseProg(t, "fun f(): any = g(a?.b, c)")
	fun := prog.Stmts[0].(*funStmt)

	call, ok := fun.ExprBody.(*callExpr)
	if !ok {
		t.Fatalf("expected root *callExpr, got %T", fun.ExprBody)
	}
	chain, ok := call.Arguments[0].(*optionalChainExpr)
	if !ok {
		t.Fatalf("expected argument 0 *optionalChainExpr, got %T", call.Arguments[0])
	}
	if _, ok := chain.Expr.(*memberExpr); !ok {
		t.Fatalf("expected chain to hold *memberExpr, got %T", chain.Expr)
	}
	if _, ok := call.Arguments[1].(*optionalChainExpr); ok {
		t.Fatal("argument 1 must not be wrapped")
	}
}

func TestParser_PlainMemberAccessNotWrapped(t *testing.T) {
	prog := parseProg(t, "fun f(): any = a.b.c")
	fun := prog.Stmts[0].(*funStmt)

	member, ok := fun.ExprBody.(*memberExpr)
	if !ok {
		t.Fatalf("expected plain *memberExpr root without wrapper, got %T", fun.ExprBody)
	}
	if member.Optional {
		t.Fatal("plain member access must not be marked optional")
	}
}

func TestParser_OptionalMemberRequiresIdentifier(t *testing.T) {
	expectParseError(t, "fun f(): any = x?.")
	expectParseError(t, "fun f(): any = x?.1")
}
