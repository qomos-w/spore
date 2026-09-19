package frontend

import (
	"testing"
)

func TestWalkForSemanticTokens_FunctionAndVariables(t *testing.T) {
	source := `export fun add(a: int, b: int): int { return a + b }`
	tokens, _ := AnalyzeSemanticTokens(source)

	find := func(name string, tt int) bool {
		for _, tok := range tokens {
			if tok.Length == len(name) && tok.TokenType == tt {
				return true
			}
		}
		return false
	}

	if !find("export", stModifier) {
		t.Fatal("missing 'export' modifier")
	}
	if !find("fun", stKeyword) {
		t.Fatal("missing 'fun' keyword")
	}
	if !find("add", stFunction) {
		t.Fatal("missing 'add' function")
	}
	if !find("a", stParameter) {
		t.Fatal("missing 'a' parameter")
	}
	if !find("int", stType) {
		t.Fatal("missing 'int' type annotation")
	}
	if !find("return", stKeyword) {
		t.Fatal("missing 'return' keyword")
	}
}

func TestWalkForSemanticTokens_Struct(t *testing.T) {
	source := `struct Point { x: int, y: int }`
	tokens, _ := AnalyzeSemanticTokens(source)

	find := func(name string, tt int) bool {
		for _, tok := range tokens {
			if tok.Length == len(name) && tok.TokenType == tt {
				return true
			}
		}
		return false
	}

	if !find("Point", stStruct) {
		t.Fatal("missing 'Point' struct")
	}
	if !find("x", stProperty) {
		t.Fatal("missing 'x' property")
	}
	if !find("y", stProperty) {
		t.Fatal("missing 'y' property")
	}
}

func TestWalkForSemanticTokens_Interface(t *testing.T) {
	source := `interface Reader { fun read(): bytes; fun close() }`
	tokens, _ := AnalyzeSemanticTokens(source)

	find := func(name string, tt int) bool {
		for _, tok := range tokens {
			if tok.Length == len(name) && tok.TokenType == tt {
				return true
			}
		}
		return false
	}

	if !find("Reader", stInterface) {
		t.Fatal("missing 'Reader' interface")
	}
	if !find("read", stMethod) {
		t.Fatal("missing 'read' method")
	}
	if !find("close", stMethod) {
		t.Fatal("missing 'close' method")
	}
}

func TestWalkForSemanticTokens_InterfaceDefaultBody(t *testing.T) {
	source := "interface Greeter {\n  fun greet(): string { var m: string = \"hello\" return m }\n  fun name(): string\n}"
	tokens, err := AnalyzeSemanticTokens(source)
	if err != nil {
		t.Fatalf("semantic analysis should accept interface default method bodies, got %v", err)
	}

	find := func(name string, tt int) bool {
		for _, tok := range tokens {
			if tok.Length == len(name) && tok.TokenType == tt {
				return true
			}
		}
		return false
	}

	if !find("Greeter", stInterface) {
		t.Fatal("missing 'Greeter' interface")
	}
	if !find("greet", stMethod) {
		t.Fatal("missing 'greet' default method")
	}
	if !find("name", stMethod) {
		t.Fatal("missing 'name' abstract method")
	}
	if !find("m", stVariable) {
		t.Fatal("missing local variable token collected inside default method body")
	}
}

func TestWalkForSemanticTokens_Import(t *testing.T) {
	source := `import Foo from "bar"`
	tokens, _ := AnalyzeSemanticTokens(source)

	find := func(name string, tt int) bool {
		for _, tok := range tokens {
			if tok.Length == len(name) && tok.TokenType == tt {
				return true
			}
		}
		return false
	}

	if !find("Foo", stNamespace) {
		t.Fatal("missing 'Foo' namespace")
	}
	if !find("import", stKeyword) {
		t.Fatal("missing 'import' keyword")
	}
}

func TestWalkForSemanticTokens_MethodCallVsProperty(t *testing.T) {
	source := `fun test(p: Point): int { return p.x + p.area() }`
	tokens, _ := AnalyzeSemanticTokens(source)

	find := func(name string, tt int) bool {
		for _, tok := range tokens {
			if tok.Length == len(name) && tok.TokenType == tt {
				return true
			}
		}
		return false
	}

	if !find("p", stParameter) {
		t.Fatal("missing 'p' parameter")
	}
	if !find("x", stProperty) {
		t.Fatal("missing 'x' property")
	}
	if !find("area", stMethod) {
		t.Fatal("missing 'area' method (should be Method, not Property)")
	}
}

func TestWalkForSemanticTokens_Literals(t *testing.T) {
	source := `var s: string = "hello"
var n: int = 42
var b: bool = true
var x = null`
	tokens, _ := AnalyzeSemanticTokens(source)

	find := func(tt int) bool {
		for _, tok := range tokens {
			if tok.TokenType == tt {
				return true
			}
		}
		return false
	}

	if !find(stString) {
		t.Fatal("missing string literal")
	}
	if !find(stNumber) {
		t.Fatal("missing number literal")
	}
	if !find(stKeyword) {
		t.Fatal("missing keyword (true/null)")
	}
	if !find(stOperator) {
		t.Fatal("missing operator (=)")
	}
}

func TestWalkForSemanticTokens_TypeAliasAndGeneric(t *testing.T) {
	source := `type IntList = Array<int>`
	tokens, _ := AnalyzeSemanticTokens(source)

	find := func(name string, tt int) bool {
		for _, tok := range tokens {
			if tok.Length == len(name) && tok.TokenType == tt {
				return true
			}
		}
		return false
	}

	if !find("IntList", stType) {
		t.Fatal("missing 'IntList' type alias")
	}
	if !find("Array", stType) {
		t.Fatal("missing 'Array' type")
	}
	if !find("int", stTypeParameter) {
		t.Fatal("missing 'int' type parameter")
	}
}

func TestWalkForSemanticTokens_ForInAndWhen(t *testing.T) {
	source := `fun test(items: Array<string>): int {
	var count: int = 0
	for (item in items) {
		when item {
			is string { count = count + 1 }
		}
	}
	return count
}`
	tokens, _ := AnalyzeSemanticTokens(source)

	find := func(name string, tt int) bool {
		for _, tok := range tokens {
			if tok.Length == len(name) && tok.TokenType == tt {
				return true
			}
		}
		return false
	}

	if !find("item", stVariable) {
		t.Fatal("missing 'item' variable (for-in)")
	}
	if !find("count", stVariable) {
		t.Fatal("missing 'count' variable")
	}
	if !find("when", stKeyword) {
		t.Fatal("missing 'when' keyword")
	}
}

func TestWalkForSemanticTokens_PartialParse(t *testing.T) {
	source := `fun hello() { return "hi" }
this is broken syntax {{{`
	tokens, err := AnalyzeSemanticTokens(source)
	if err == nil {
		t.Fatal("expected parse error")
	}
	if len(tokens) == 0 {
		t.Fatal("expected partial tokens even on parse error")
	}
	found := false
	for _, tok := range tokens {
		if tok.TokenType == stFunction && tok.Length == 5 {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("missing 'hello' function token in partial parse")
	}
}

func TestWalkForSemanticTokens_ZeroBasedPositions(t *testing.T) {
	source := `var x: int = 1`
	tokens, _ := AnalyzeSemanticTokens(source)
	if len(tokens) == 0 {
		t.Fatal("no tokens")
	}
	if tokens[0].Line != 0 || tokens[0].Col != 0 {
		t.Fatalf("first token at (%d,%d), want (0,0)", tokens[0].Line, tokens[0].Col)
	}
}

func TestWalkForSemanticTokens_Sorted(t *testing.T) {
	source := `fun add(a: int, b: int): int { return a + b }`
	tokens, _ := AnalyzeSemanticTokens(source)
	for i := 1; i < len(tokens); i++ {
		if tokens[i].Line < tokens[i-1].Line ||
			(tokens[i].Line == tokens[i-1].Line && tokens[i].Col < tokens[i-1].Col) {
			t.Fatalf("tokens not sorted: [%d](%d,%d) after [%d](%d,%d)",
				i, tokens[i].Line, tokens[i].Col,
				i-1, tokens[i-1].Line, tokens[i-1].Col)
		}
	}
}

func TestWalkForSemanticTokens_NewExpr(t *testing.T) {
	source := `fun make(): Point { return new Point(1, 2) }`
	tokens, _ := AnalyzeSemanticTokens(source)

	found := false
	for _, tok := range tokens {
		if tok.TokenType == stClass && tok.Length == 5 {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("missing 'Point' class token in new expression")
	}
}

func TestWalkForSemanticTokens_StructLiteral(t *testing.T) {
	source := `fun origin(): Point { return Point{x: 1, y: 2} }`
	tokens, _ := AnalyzeSemanticTokens(source)

	found := false
	for _, tok := range tokens {
		if tok.TokenType == stStruct && tok.Length == 5 {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("missing 'Point' struct token in struct literal")
	}
}
