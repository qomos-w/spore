package script_test

import (
	"testing"

	"github.com/qomos-w/spore/script"
)

func TestAnalyze_ReturnsZeroBasedPositions(t *testing.T) {
	source := `var x: int = 1`
	tokens, err := script.Analyze(source)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tokens) == 0 {
		t.Fatal("no tokens")
	}
	if tokens[0].Line != 0 || tokens[0].Col != 0 {
		t.Fatalf("first token at (%d,%d), want (0,0)", tokens[0].Line, tokens[0].Col)
	}
}

func TestAnalyze_TokensAreSorted(t *testing.T) {
	source := `fun add(a: int, b: int): int { return a + b }`
	tokens, err := script.Analyze(source)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i := 1; i < len(tokens); i++ {
		if tokens[i].Line < tokens[i-1].Line ||
			(tokens[i].Line == tokens[i-1].Line && tokens[i].Col < tokens[i-1].Col) {
			t.Fatalf("tokens not sorted: [%d](%d,%d) after [%d](%d,%d)",
				i, tokens[i].Line, tokens[i].Col,
				i-1, tokens[i-1].Line, tokens[i-1].Col)
		}
	}
}

func TestAnalyze_CoversAllTokenTypes(t *testing.T) {
	source := `export fun test(p: Point): int {
	var items: Array<string> = []
	var n: int = 42
	for (x in items) {
		var s: string = "hello"
	}
	return p.x + p.area()
}`
	tokens, err := script.Analyze(source)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := make(map[script.SemanticTokenType]bool)
	for _, tok := range tokens {
		found[tok.Type] = true
	}
	expected := []struct {
		tt  script.SemanticTokenType
		name string
	}{
		{script.SemanticTokenTypeKeyword, "Keyword"},
		{script.SemanticTokenTypeModifier, "Modifier"},
		{script.SemanticTokenTypeFunction, "Function"},
		{script.SemanticTokenTypeParameter, "Parameter"},
		{script.SemanticTokenTypeVariable, "Variable"},
		{script.SemanticTokenTypeType, "Type"},
		{script.SemanticTokenTypeTypeParameter, "TypeParameter"},
		{script.SemanticTokenTypeProperty, "Property"},
		{script.SemanticTokenTypeMethod, "Method"},
		{script.SemanticTokenTypeString, "String"},
		{script.SemanticTokenTypeNumber, "Number"},
		{script.SemanticTokenTypeOperator, "Operator"},
	}
	for _, e := range expected {
		if !found[e.tt] {
			t.Errorf("missing token type %s (%d)", e.name, e.tt)
		}
	}
}

func TestAnalyze_ParseErrorReturnsPartialTokens(t *testing.T) {
	source := `fun hello() { return "hi" }
	this is broken syntax {{{`
	tokens, err := script.Analyze(source)
	if err == nil {
		t.Fatal("expected parse error")
	}
	if len(tokens) == 0 {
		t.Fatal("expected partial tokens even on parse error")
	}
	found := false
	for _, tok := range tokens {
		if tok.Type == script.SemanticTokenTypeFunction && tok.Length == 5 {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("missing 'hello' function token in partial parse")
	}
}

func TestAnalyze_UsesPublicTypes(t *testing.T) {
	source := `fun id(x: int): int { return x }`
	tokens, err := script.Analyze(source)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Verify we can use SemanticTokenType constants for comparison.
	hasFunction := false
	for _, tok := range tokens {
		if tok.Type == script.SemanticTokenTypeFunction {
			hasFunction = true
		}
		_ = tok.Modifiers // verify field is accessible
	}
	if !hasFunction {
		t.Fatal("no function token found")
	}
}
