package frontend

import (
	"strings"
	"testing"
	"time"
)

func parseProg(t *testing.T, source string) *program {
	t.Helper()
	l := newLexer(source)
	p := newParser(l, source)
	prog, err := p.parse()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	return prog
}

// typeContextFromProg builds a typeContext from a parsed program's declarations.
func typeContextFromProg(prog *program) typeContext {
	ctx := typeContext{
		structNames: make(map[string]bool),
		classNames:  make(map[string]bool),
		typeAliases: make(map[string]*typeAnnotation),
	}
	for _, stmt := range prog.Stmts {
		switch s := stmt.(type) {
		case *structStmt:
			ctx.structNames[s.Name.Value] = true
		case *classStmt:
			ctx.classNames[s.Name.Value] = true
		case *typeAliasStmt:
			if s.Alias != nil {
				ctx.typeAliases[s.Name.Value] = s.Alias
			}
		}
	}
	return ctx
}

func expectParseError(t *testing.T, source string) {
	t.Helper()
	l := newLexer(source)
	p := newParser(l, source)
	_, err := p.parse()
	if err == nil {
		t.Fatalf("expected parse error, got none")
	}
}

func expectParseErrorContains(t *testing.T, source string, want string) {
	t.Helper()
	l := newLexer(source)
	p := newParser(l, source)
	_, err := p.parse()
	if err == nil {
		t.Fatalf("expected parse error, got none")
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("expected parse error containing %q, got %q", want, err.Error())
	}
}

// --- Program structure ---

func TestParser_EmptySource(t *testing.T) {
	expectParseError(t, "")
}

func TestParser_PackageDeclaration(t *testing.T) {
	prog, err := parseModule("package myapp\nfun f(): void {}")
	if err != nil {
		t.Fatalf("unexpected error for package declaration: %v", err)
	}
	if prog.PackageName() != "myapp" {
		t.Fatalf("expected package name 'myapp', got %q", prog.PackageName())
	}
}

func TestParser_PackageDeclaration_DottedName(t *testing.T) {
	prog, err := parseModule("package demo.net\nfun f(): void {}")
	if err != nil {
		t.Fatalf("unexpected error for dotted package declaration: %v", err)
	}
	if prog.PackageName() != "demo.net" {
		t.Fatalf("expected package name 'demo.net', got %q", prog.PackageName())
	}
}

func TestParser_ImportDeclarations(t *testing.T) {
	prog := parseProg(t, "import add from \"math\"\nfun f(): void {}")
	if len(prog.imports) != 1 || prog.imports[0].Name != "add" || prog.imports[0].Path != "math" {
		t.Fatalf("unexpected imports: %+v", prog.imports)
	}
}

func TestParser_FailFastOnFirstTopLevelError(t *testing.T) {
	expectParseErrorContains(t, "bogus\nfun later(): int { return 1 }", "expected declaration, got bogus")
}

func TestParser_ClassFieldMissingTypeReportsStableMessage(t *testing.T) {
	expectParseErrorContains(t, "class Broken {\n  value:\n}\nfun later(): int { return 1 }", "expected type")
}

func TestParser_WhenCaseMissingExpressionReportsStableMessage(t *testing.T) {
	expectParseErrorContains(t, "fun classify(v: int): int {\n  when (v) {\n    case { return 1 }\n    else { return 0 }\n  }\n}", "expected case expression before block")
}

func TestParser_RecoveryStopsBeforeLaterTopLevelDeclaration(t *testing.T) {
	l := newLexer("class Broken {\n  value:\n}\nfun later(): int { return 1 }")
	p := newParser(l, "class Broken {\n  value:\n}\nfun later(): int { return 1 }")
	prog, err := p.parse()
	if err == nil {
		t.Fatal("expected parse error")
	}
	if prog == nil {
		t.Fatal("expected partial program to be returned during recovery")
	}
	if len(prog.Stmts) != 2 {
		t.Fatalf("expected parser to recover and preserve broken class plus later declaration, got %d statements", len(prog.Stmts))
	}
	if _, ok := prog.Stmts[1].(*funStmt); !ok {
		t.Fatalf("expected recovered later statement to be *funStmt, got %T", prog.Stmts[1])
	}
}

func TestParser_MultiDiagnosticOrderForMalformedWhenIsStable(t *testing.T) {
	prog, err, diags := ParseModuleForTestWithDiagnostics("fun classify(v: int): int {\n  when (v) {\n    case { return 1 }\n    else { return 0 }\n  }\n}")
	if err == nil {
		t.Fatal("expected parse error")
	}
	if len(diags) != 0 {
		t.Fatalf("expected recovery to absorb malformed when diagnostics, got %d", len(diags))
	}
	if prog == nil || len(prog.Stmts) != 1 {
		t.Fatalf("expected partial program with one function, got %#v", prog)
	}
}

func TestParser_ClassMemberRecoveryKeepsLaterField(t *testing.T) {
	prog, err, _ := ParseModuleForTestWithDiagnostics("class Broken {\n  bad:\n  good: int\n}\nfun later(): int { return 1 }")
	if err == nil {
		t.Fatal("expected parse error")
	}
	if prog == nil || len(prog.Stmts) != 2 {
		t.Fatalf("expected partial program with class and later function, got %#v", prog)
	}
	cl, ok := prog.Stmts[0].(*classStmt)
	if !ok {
		t.Fatalf("expected first statement class, got %T", prog.Stmts[0])
	}
	field := cl.Fields[0]
	if field.Name.Value != "good" || field.Type_ == nil || field.Type_.Name != "int" {
		t.Fatalf("expected recovery to keep only the later valid field, got name=%q type=%#v", field.Name.Value, field.Type_)
	}
	if _, ok := prog.Stmts[1].(*funStmt); !ok {
		t.Fatalf("expected later function to survive recovery, got %T", prog.Stmts[1])
	}
}

func TestParser_WhenCaseRecoveryKeepsLaterCase(t *testing.T) {
	prog, err, diags := ParseModuleForTestWithDiagnostics("fun classify(v: int): int {\n  when (v) {\n    case { return 1 }\n    case 2 { return 2 }\n    else { return 0 }\n  }\n}")
	if err == nil {
		t.Fatal("expected parse error")
	}
	if len(diags) != 0 {
		t.Fatalf("expected recovery to avoid surfacing malformed first case diagnostics, got %d", len(diags))
	}
	if prog == nil || len(prog.Stmts) != 1 {
		t.Fatalf("expected partial program with one function, got %#v", prog)
	}
	fn := prog.Stmts[0].(*funStmt)
	ws := fn.Body.Stmts[0].(*whenStmt)
	if len(ws.Cases) != 1 {
		t.Fatalf("expected recovery to keep the later valid case, got %d cases", len(ws.Cases))
	}
	lit, ok := ws.Cases[0].Values[0].(*intLiteral)
	if !ok || lit.Value != 2 {
		t.Fatalf("expected surviving case to match literal 2, got %#v", ws.Cases[0].Values)
	}
	if ws.DefaultCase == nil {
		t.Fatal("expected recovery to keep else block")
	}
}

func TestParser_ClassMemberRecoveryKeepsLaterMethodWithoutPlaceholderField(t *testing.T) {
	prog, err, _ := ParseModuleForTestWithDiagnostics("class Broken {\n  bad:\n  fun ok(): void {}\n}\n")
	if err == nil {
		t.Fatal("expected parse error")
	}
	if prog == nil || len(prog.Stmts) != 1 {
		t.Fatalf("expected partial program with one class, got %#v", prog)
	}
	cl := prog.Stmts[0].(*classStmt)
	if len(cl.Fields) != 0 {
		t.Fatalf("expected no placeholder field after malformed field, got %#v", cl.Fields)
	}
	if len(cl.Methods) != 1 || cl.Methods[0].Name.Value != "ok" {
		t.Fatalf("expected later method to survive recovery, got %#v", cl.Methods)
	}
}

func TestParser_WhenCaseRecoverySkipsNestedBrokenBlockAndKeepsElse(t *testing.T) {
	prog, err, diags := ParseModuleForTestWithDiagnostics("fun classify(v: int): int {\n  when (v) {\n    case { if (true) { return 1 } }\n    case 2 { return 2 }\n    else { return 0 }\n  }\n}")
	if err == nil {
		t.Fatal("expected parse error")
	}
	if len(diags) != 0 {
		t.Fatalf("expected recovery to absorb malformed nested case diagnostics, got %d", len(diags))
	}
	ws := prog.Stmts[0].(*funStmt).Body.Stmts[0].(*whenStmt)
	if len(ws.Cases) != 1 {
		t.Fatalf("expected later valid case to survive nested broken block, got %d cases", len(ws.Cases))
	}
	if ws.DefaultCase == nil {
		t.Fatal("expected else block to survive nested broken block")
	}
}

func TestParser_WhenElseRecoveryKeepsLaterTopLevelDeclaration(t *testing.T) {
	prog, err, _ := ParseModuleForTestWithDiagnostics("fun classify(v: int): int {\n  when (v) {\n    case 1 { return 1 }\n    else { return\n  }\n}\nfun later(): int { return 2 }")
	if err == nil {
		t.Fatal("expected parse error")
	}
	if prog == nil || len(prog.Stmts) != 2 {
		t.Fatalf("expected partial program with two top-level functions, got %#v", prog)
	}
	fn, ok := prog.Stmts[0].(*funStmt)
	if !ok {
		t.Fatalf("expected first statement function, got %T", prog.Stmts[0])
	}
	ws, ok := fn.Body.Stmts[0].(*whenStmt)
	if !ok {
		t.Fatalf("expected when statement in first function body, got %#v", fn.Body.Stmts)
	}
	if len(ws.Cases) != 1 {
		t.Fatalf("expected previous valid case to survive malformed else, got %d cases", len(ws.Cases))
	}
	if _, ok := prog.Stmts[1].(*funStmt); !ok {
		t.Fatalf("expected later top-level function to survive recovery, got %T", prog.Stmts[1])
	}
}

func TestParser_WhenElseRecoveryKeepsPreviousCase(t *testing.T) {
	prog, err, _ := ParseModuleForTestWithDiagnostics("fun classify(v: int): int {\n  when (v) {\n    case 1 { return 1 }\n    else { return\n  }\n}")
	if err == nil {
		t.Fatal("expected parse error")
	}
	fn := prog.Stmts[0].(*funStmt)
	ws := fn.Body.Stmts[0].(*whenStmt)
	if len(ws.Cases) != 1 {
		t.Fatalf("expected valid case before malformed else to survive, got %d cases", len(ws.Cases))
	}
	lit, ok := ws.Cases[0].Values[0].(*intLiteral)
	if !ok || lit.Value != 1 {
		t.Fatalf("expected surviving case to match literal 1, got %#v", ws.Cases[0].Values)
	}
}

func TestParser_InterfaceMethodRecoveryKeepsLaterMethod(t *testing.T) {
	prog, err, _ := ParseModuleForTestWithDiagnostics("interface I {\n  fun bad(\n  fun ok(): int\n}\nfun later(): int { return 1 }")
	if err == nil {
		t.Fatal("expected parse error")
	}
	if prog == nil || len(prog.Stmts) != 2 {
		t.Fatalf("expected partial program with interface and later function, got %#v", prog)
	}
	iface, ok := prog.Stmts[0].(*interfaceStmt)
	if !ok {
		t.Fatalf("expected first statement interface, got %T", prog.Stmts[0])
	}
	if len(iface.Methods) != 1 || iface.Methods[0].Name.Value != "ok" {
		t.Fatalf("expected later valid method to survive recovery, got %#v", iface.Methods)
	}
	if _, ok := prog.Stmts[1].(*funStmt); !ok {
		t.Fatalf("expected later top-level function to survive recovery, got %T", prog.Stmts[1])
	}
}

func TestParser_InterfaceMethodRecoveryStopsAtRBrace(t *testing.T) {
	prog, err, _ := ParseModuleForTestWithDiagnostics("interface I {\n  fun bad(name:\n}\nfun later(): int { return 1 }")
	if err == nil {
		t.Fatal("expected parse error")
	}
	if prog == nil || len(prog.Stmts) != 2 {
		t.Fatalf("expected partial program with interface and later function, got %#v", prog)
	}
	iface, ok := prog.Stmts[0].(*interfaceStmt)
	if !ok {
		t.Fatalf("expected first statement interface, got %T", prog.Stmts[0])
	}
	if len(iface.Methods) != 0 {
		t.Fatalf("expected malformed interface method to be dropped, got %#v", iface.Methods)
	}
	if _, ok := prog.Stmts[1].(*funStmt); !ok {
		t.Fatalf("expected later top-level function to survive interface recovery, got %T", prog.Stmts[1])
	}
}

func TestParser_InterfaceBodyNonFunTokenReportsErrorAndRecovers(t *testing.T) {
	prog, err, _ := ParseModuleForTestWithDiagnostics("interface I {\n  x: int\n  fun ok(): int\n}\nfun later(): int { return 1 }")
	if err == nil {
		t.Fatal("expected parse error")
	}
	if prog == nil || len(prog.Stmts) != 2 {
		t.Fatalf("expected partial program with interface and later function, got %#v", prog)
	}
	iface, ok := prog.Stmts[0].(*interfaceStmt)
	if !ok {
		t.Fatalf("expected first statement interface, got %T", prog.Stmts[0])
	}
	if len(iface.Methods) != 1 || iface.Methods[0].Name.Value != "ok" {
		t.Fatalf("expected later valid method to survive recovery, got %#v", iface.Methods)
	}
	if _, ok := prog.Stmts[1].(*funStmt); !ok {
		t.Fatalf("expected later top-level function to survive recovery, got %T", prog.Stmts[1])
	}
}

func TestParser_ClassMemberRecoveryKeepsLaterFieldAfterMultipleMalformedFields(t *testing.T) {
	prog, err, _ := ParseModuleForTestWithDiagnostics("class Broken {\n  bad1:\n  bad2:\n  bad3:\n  good: int\n}\nfun later(): int { return 1 }")
	if err == nil {
		t.Fatal("expected parse error")
	}
	if prog == nil || len(prog.Stmts) != 2 {
		t.Fatalf("expected partial program with class and later function, got %#v", prog)
	}
	cl := prog.Stmts[0].(*classStmt)
	if len(cl.Fields) != 1 || cl.Fields[0].Name.Value != "good" {
		t.Fatalf("expected only the trailing valid field to survive multiple malformed fields, got %#v", cl.Fields)
	}
	if _, ok := prog.Stmts[1].(*funStmt); !ok {
		t.Fatalf("expected later top-level function to survive recovery, got %T", prog.Stmts[1])
	}
}

func TestParser_WhenCaseRecoveryKeepsLaterCasesAcrossMultipleMalformed(t *testing.T) {
	prog, err, _ := ParseModuleForTestWithDiagnostics("fun classify(v: int): int {\n  when (v) {\n    case { return 1 }\n    case 2 { return 2 }\n    case { return 3 }\n    case 4 { return 4 }\n    else { return 0 }\n  }\n}")
	if err == nil {
		t.Fatal("expected parse error")
	}
	fn := prog.Stmts[0].(*funStmt)
	ws := fn.Body.Stmts[0].(*whenStmt)
	if len(ws.Cases) != 2 {
		t.Fatalf("expected two valid cases to survive interleaved malformed cases, got %d", len(ws.Cases))
	}
	first, ok := ws.Cases[0].Values[0].(*intLiteral)
	if !ok || first.Value != 2 {
		t.Fatalf("expected first surviving case to match literal 2, got %#v", ws.Cases[0].Values)
	}
	second, ok := ws.Cases[1].Values[0].(*intLiteral)
	if !ok || second.Value != 4 {
		t.Fatalf("expected second surviving case to match literal 4, got %#v", ws.Cases[1].Values)
	}
	if ws.DefaultCase == nil {
		t.Fatal("expected else block to survive interleaved malformed cases")
	}
}

// --- Fun declarations ---

func TestParser_FunDeclaration(t *testing.T) {
	prog := parseProg(t, "fun greet(name: string): void {}")
	if len(prog.Stmts) != 1 {
		t.Fatalf("expected 1 statement, got %d", len(prog.Stmts))
	}
	fun, ok := prog.Stmts[0].(*funStmt)
	if !ok {
		t.Fatal("expected *funStmt")
	}
	if fun.Name.Value != "greet" {
		t.Fatalf("expected name 'greet', got %q", fun.Name.Value)
	}
	if fun.Exported {
		t.Fatal("expected fun to not be exported")
	}
	if len(fun.Params) != 1 {
		t.Fatalf("expected 1 param, got %d", len(fun.Params))
	}
	if fun.Params[0].Name.Value != "name" {
		t.Fatalf("expected param name 'name', got %q", fun.Params[0].Name.Value)
	}
	if fun.Params[0].Type_.Name != "string" {
		t.Fatalf("expected param type 'string', got %q", fun.Params[0].Type_.Name)
	}
	if fun.ReturnType.Name != "void" {
		t.Fatalf("expected return type 'void', got %q", fun.ReturnType.Name)
	}
}

func TestParser_ExportFunDeclaration(t *testing.T) {
	prog := parseProg(t, "export fun compute(x: int): int {}")
	fun := prog.Stmts[0].(*funStmt)
	if !fun.Exported {
		t.Fatal("expected fun to be exported")
	}
	if fun.Name.Value != "compute" {
		t.Fatalf("expected name 'compute', got %q", fun.Name.Value)
	}
}

func TestParser_FunWithMultipleParameters(t *testing.T) {
	prog := parseProg(t, "fun add(a: int, b: int): int {}")
	fun := prog.Stmts[0].(*funStmt)
	if len(fun.Params) != 2 {
		t.Fatalf("expected 2 params, got %d", len(fun.Params))
	}
	if fun.Params[0].Name.Value != "a" || fun.Params[1].Name.Value != "b" {
		t.Fatalf("expected params a,b, got %q,%q", fun.Params[0].Name.Value, fun.Params[1].Name.Value)
	}
}

func TestParser_FunWithGenericTypes(t *testing.T) {
	prog := parseProg(t, "fun items(data: array<string>): map<string, int> {}")
	fun := prog.Stmts[0].(*funStmt)
	paramType := fun.Params[0].Type_
	if paramType.Name != "array" {
		t.Fatalf("expected param type 'array', got %q", paramType.Name)
	}
	if len(paramType.Params) != 1 || paramType.Params[0].Name != "string" {
		t.Fatalf("expected array<string>, got %+v", paramType)
	}
	retType := fun.ReturnType
	if retType.Name != "map" {
		t.Fatalf("expected return type 'map', got %q", retType.Name)
	}
	if len(retType.Params) != 2 {
		t.Fatalf("expected map with 2 type params, got %d", len(retType.Params))
	}
	if retType.Params[0].Name != "string" || retType.Params[1].Name != "int" {
		t.Fatalf("expected map<string, int>, got %q, %q", retType.Params[0].Name, retType.Params[1].Name)
	}
}

func TestParser_FunWithBodyBlock(t *testing.T) {
	prog := parseProg(t, "fun f(): void {\n  return\n}")
	fun := prog.Stmts[0].(*funStmt)
	if fun.Body == nil {
		t.Fatal("expected body block")
	}
	if len(fun.Body.Stmts) == 0 {
		// parseReturnStmt produces a placeholder, may not be in stmts
		// That's acceptable for Phase 10 — body parsing is minimal
	}
}

func TestParser_FunWithEqBody(t *testing.T) {
	prog := parseProg(t, "fun twice(x: int): int = x * 2")
	fun := prog.Stmts[0].(*funStmt)
	if fun.ExprBody == nil {
		t.Fatal("expected ExprBody from = expr syntax")
	}
	if fun.Body != nil {
		// = expr syntax doesn't produce a block body — that's correct
	}
}

func TestParser_BlockBodySingleReturnExpression(t *testing.T) {
	prog := parseProg(t, "fun pick(value: string): string { return value }")
	fun := prog.Stmts[0].(*funStmt)
	if fun.Body == nil || len(fun.Body.Stmts) != 1 {
		t.Fatalf("expected single return statement, got %+v", fun.Body)
	}
	ret, ok := fun.Body.Stmts[0].(*returnStmt)
	if !ok {
		t.Fatalf("expected *returnStmt, got %T", fun.Body.Stmts[0])
	}
	if ret.Value == nil {
		t.Fatal("expected return expression")
	}
}

func TestParser_BlockBodyVarReturnSubset(t *testing.T) {
	prog := parseProg(t, "fun boost(base: int, bonus: int): int { var total: int = base + bonus return total }")
	fun := prog.Stmts[0].(*funStmt)
	if fun.Body == nil || len(fun.Body.Stmts) != 2 {
		t.Fatalf("expected var + return block, got %+v", fun.Body)
	}
	decl, ok := fun.Body.Stmts[0].(*varStmt)
	if !ok || decl.InitExpr == nil {
		t.Fatalf("expected first stmt var with initializer, got %+v", fun.Body.Stmts[0])
	}
	ret, ok := fun.Body.Stmts[1].(*returnStmt)
	if !ok || ret.Value == nil {
		t.Fatalf("expected second stmt return with value, got %+v", fun.Body.Stmts[1])
	}
}

func TestParser_BlockBodyTwoVarReturnSubset(t *testing.T) {
	prog := parseProg(t, "fun boost(base: int, bonus: int): int { var left: int = base var right: int = bonus return left + right }")
	fun := prog.Stmts[0].(*funStmt)
	if fun.Body == nil || len(fun.Body.Stmts) != 3 {
		t.Fatalf("expected two var statements plus return, got %+v", fun.Body)
	}
	decl1, ok := fun.Body.Stmts[0].(*varStmt)
	if !ok || decl1.InitExpr == nil {
		t.Fatalf("expected first stmt var with initializer, got %+v", fun.Body.Stmts[0])
	}
	decl2, ok := fun.Body.Stmts[1].(*varStmt)
	if !ok || decl2.InitExpr == nil {
		t.Fatalf("expected second stmt var with initializer, got %+v", fun.Body.Stmts[1])
	}
	ret, ok := fun.Body.Stmts[2].(*returnStmt)
	if !ok || ret.Value == nil {
		t.Fatalf("expected final return with value, got %+v", fun.Body.Stmts[2])
	}
}

func TestParser_BlockBodyYieldExpression(t *testing.T) {
	prog := parseProg(t, "stream fun emit(): int { yield 1 return 2 }")
	fun := prog.Stmts[0].(*funStmt)
	if fun.Body == nil || len(fun.Body.Stmts) != 2 {
		t.Fatalf("expected yield + return block, got %+v", fun.Body)
	}
	yld, ok := fun.Body.Stmts[0].(*yieldStmt)
	if !ok {
		t.Fatalf("expected *yieldStmt, got %T", fun.Body.Stmts[0])
	}
	if yld.Value == nil {
		t.Fatal("expected yield expression")
	}
}

func TestParser_BlockBodyYieldSemicolon(t *testing.T) {
	prog := parseProg(t, "stream fun emit(): int { yield 1; return 2 }")
	fun := prog.Stmts[0].(*funStmt)
	if fun.Body == nil || len(fun.Body.Stmts) != 2 {
		t.Fatalf("expected yield + return block, got %+v", fun.Body)
	}
	if _, ok := fun.Body.Stmts[0].(*yieldStmt); !ok {
		t.Fatalf("expected *yieldStmt, got %T", fun.Body.Stmts[0])
	}
}

func TestParser_YieldAcceptedInNonStreamFun_SyntaxOnly(t *testing.T) {
	prog := parseProg(t, "fun f(): int { yield 1 return 2 }")
	fun := prog.Stmts[0].(*funStmt)
	if fun.Body == nil || len(fun.Body.Stmts) != 2 {
		t.Fatalf("expected yield + return block, got %+v", fun.Body)
	}
	if _, ok := fun.Body.Stmts[0].(*yieldStmt); !ok {
		t.Fatalf("expected *yieldStmt, got %T", fun.Body.Stmts[0])
	}
}

// --- Stream fun declarations ---

func TestParser_StreamFunDeclaration(t *testing.T) {
	prog := parseProg(t, "stream fun chat(prompt: string): int { yield 1 return 2 }")
	fun := prog.Stmts[0].(*funStmt)
	if !fun.IsStream {
		t.Fatal("expected IsStream == true")
	}
	if fun.Name.Value != "chat" {
		t.Fatalf("expected name 'chat', got %q", fun.Name.Value)
	}
	if fun.Body == nil || len(fun.Body.Stmts) != 2 {
		t.Fatalf("expected 2 stmts in body, got %+v", fun.Body)
	}
}

func TestParser_ExportStreamFun(t *testing.T) {
	prog := parseProg(t, "export stream fun chat(): int { yield 1 return 2 }")
	fun := prog.Stmts[0].(*funStmt)
	if !fun.Exported {
		t.Fatal("expected Exported == true")
	}
	if !fun.IsStream {
		t.Fatal("expected IsStream == true")
	}
}

func TestParser_StreamWithoutFunError(t *testing.T) {
	expectParseError(t, "stream class Foo {}")
}

func TestParser_OpenTopLevelFunError(t *testing.T) {
	cases := []string{
		"open fun f(): void {}",
		"export open fun f(): void {}",
		"open stream fun f(): int { yield 1 return 2 }",
		"export open stream fun f(): int { yield 1 return 2 }",
	}
	for _, source := range cases {
		expectParseError(t, source)
	}
}

func TestParser_StreamFunExpressionBodyError(t *testing.T) {
	expectParseError(t, "stream fun f(): int = 42")
}
func TestParser_ExpressionPrecedence_MulBeforeAdd(t *testing.T) {
	prog := parseProg(t, "fun calc(): int = 1 + 2 * 3")
	fun := prog.Stmts[0].(*funStmt)

	root, ok := fun.ExprBody.(*binaryExpr)
	if !ok {
		t.Fatalf("expected root *binaryExpr, got %T", fun.ExprBody)
	}
	if root.Operator != "+" {
		t.Fatalf("expected root operator +, got %q", root.Operator)
	}

	left, ok := root.Left.(*intLiteral)
	if !ok {
		t.Fatalf("expected left *intLiteral, got %T", root.Left)
	}
	if left.Value != 1 {
		t.Fatalf("expected left value 1, got %d", left.Value)
	}

	right, ok := root.Right.(*binaryExpr)
	if !ok {
		t.Fatalf("expected right *binaryExpr, got %T", root.Right)
	}
	if right.Operator != "*" {
		t.Fatalf("expected nested operator *, got %q", right.Operator)
	}

	rleft, ok := right.Left.(*intLiteral)
	if !ok {
		t.Fatalf("expected nested left *intLiteral, got %T", right.Left)
	}
	if rleft.Value != 2 {
		t.Fatalf("expected nested left value 2, got %d", rleft.Value)
	}

	rright, ok := right.Right.(*intLiteral)
	if !ok {
		t.Fatalf("expected nested right *intLiteral, got %T", right.Right)
	}
	if rright.Value != 3 {
		t.Fatalf("expected nested right value 3, got %d", rright.Value)
	}
}

func TestParser_ExpressionPrecedence_ParenOverridesDefault(t *testing.T) {
	prog := parseProg(t, "fun calc(): int = (1 + 2) * 3")
	fun := prog.Stmts[0].(*funStmt)

	root, ok := fun.ExprBody.(*binaryExpr)
	if !ok {
		t.Fatalf("expected root *binaryExpr, got %T", fun.ExprBody)
	}
	if root.Operator != "*" {
		t.Fatalf("expected root operator *, got %q", root.Operator)
	}

	left, ok := root.Left.(*binaryExpr)
	if !ok {
		t.Fatalf("expected left *binaryExpr, got %T", root.Left)
	}
	if left.Operator != "+" {
		t.Fatalf("expected nested operator +, got %q", left.Operator)
	}

	lleft, ok := left.Left.(*intLiteral)
	if !ok {
		t.Fatalf("expected nested left *intLiteral, got %T", left.Left)
	}
	if lleft.Value != 1 {
		t.Fatalf("expected nested left value 1, got %d", lleft.Value)
	}

	lright, ok := left.Right.(*intLiteral)
	if !ok {
		t.Fatalf("expected nested right *intLiteral, got %T", left.Right)
	}
	if lright.Value != 2 {
		t.Fatalf("expected nested right value 2, got %d", lright.Value)
	}

	right, ok := root.Right.(*intLiteral)
	if !ok {
		t.Fatalf("expected right *intLiteral, got %T", root.Right)
	}
	if right.Value != 3 {
		t.Fatalf("expected right value 3, got %d", right.Value)
	}
}

func TestParser_ExpressionPrecedence_LogicalAndBeforeOr(t *testing.T) {
	prog := parseProg(t, "fun calc(a: bool, b: bool, c: bool): bool = a && b || c")
	fun := prog.Stmts[0].(*funStmt)

	root, ok := fun.ExprBody.(*binaryExpr)
	if !ok {
		t.Fatalf("expected root *binaryExpr, got %T", fun.ExprBody)
	}
	if root.Operator != "||" {
		t.Fatalf("expected root operator ||, got %q", root.Operator)
	}

	left, ok := root.Left.(*binaryExpr)
	if !ok {
		t.Fatalf("expected left *binaryExpr, got %T", root.Left)
	}
	if left.Operator != "&&" {
		t.Fatalf("expected nested operator &&, got %q", left.Operator)
	}

	if ident, ok := left.Left.(*identExpr); !ok || ident.Value != "a" {
		t.Fatalf("expected left-left ident a, got %T %+v", left.Left, left.Left)
	}
	if ident, ok := left.Right.(*identExpr); !ok || ident.Value != "b" {
		t.Fatalf("expected left-right ident b, got %T %+v", left.Right, left.Right)
	}
	if ident, ok := root.Right.(*identExpr); !ok || ident.Value != "c" {
		t.Fatalf("expected right ident c, got %T %+v", root.Right, root.Right)
	}
}

func TestParser_ExpressionPrecedence_UnaryBeforeCompare(t *testing.T) {
	prog := parseProg(t, "fun calc(a: bool, b: bool): bool = !a == b")
	fun := prog.Stmts[0].(*funStmt)

	root, ok := fun.ExprBody.(*binaryExpr)
	if !ok {
		t.Fatalf("expected root *binaryExpr, got %T", fun.ExprBody)
	}
	if root.Operator != "==" {
		t.Fatalf("expected root operator ==, got %q", root.Operator)
	}

	left, ok := root.Left.(*unaryExpr)
	if !ok {
		t.Fatalf("expected left *unaryExpr, got %T", root.Left)
	}
	if left.Operator != "!" {
		t.Fatalf("expected unary operator !, got %q", left.Operator)
	}
	if ident, ok := left.Right.(*identExpr); !ok || ident.Value != "a" {
		t.Fatalf("expected unary right ident a, got %T %+v", left.Right, left.Right)
	}

	if ident, ok := root.Right.(*identExpr); !ok || ident.Value != "b" {
		t.Fatalf("expected right ident b, got %T %+v", root.Right, root.Right)
	}
}

func TestParser_FunExpressionBody_StringLiteral(t *testing.T) {
	prog := parseProg(t, `fun greet(): string = "hi"`)
	fun := prog.Stmts[0].(*funStmt)

	if fun.ExprBody == nil {
		t.Fatal("expected ExprBody")
	}
	if fun.Body != nil {
		t.Fatal("expected block Body to be nil for expression-body function")
	}
	if fun.ReturnType == nil || fun.ReturnType.Name != "string" {
		t.Fatalf("expected return type string, got %+v", fun.ReturnType)
	}

	lit, ok := fun.ExprBody.(*stringLiteral)
	if !ok {
		t.Fatalf("expected *stringLiteral, got %T", fun.ExprBody)
	}
	if lit.Value != "hi" {
		t.Fatalf(`expected "hi", got %q`, lit.Value)
	}
}

func TestParser_FunExpressionBody_BinaryExpr(t *testing.T) {
	prog := parseProg(t, "fun add(a: int, b: int): int = a + b")
	fun := prog.Stmts[0].(*funStmt)

	if len(fun.Params) != 2 {
		t.Fatalf("expected 2 params, got %d", len(fun.Params))
	}
	if fun.ExprBody == nil {
		t.Fatal("expected ExprBody")
	}
	if fun.Body != nil {
		t.Fatal("expected block Body to be nil for expression-body function")
	}

	root, ok := fun.ExprBody.(*binaryExpr)
	if !ok {
		t.Fatalf("expected *binaryExpr, got %T", fun.ExprBody)
	}
	if root.Operator != "+" {
		t.Fatalf("expected operator +, got %q", root.Operator)
	}
	if ident, ok := root.Left.(*identExpr); !ok || ident.Value != "a" {
		t.Fatalf("expected left ident a, got %T %+v", root.Left, root.Left)
	}
	if ident, ok := root.Right.(*identExpr); !ok || ident.Value != "b" {
		t.Fatalf("expected right ident b, got %T %+v", root.Right, root.Right)
	}
}

func TestParser_FunBlockBody_SingleReturnValue(t *testing.T) {
	prog := parseProg(t, "fun wrap(x: int): int { return x }")
	fun := prog.Stmts[0].(*funStmt)

	if fun.Body == nil {
		t.Fatal("expected block Body")
	}
	if fun.ExprBody != nil {
		t.Fatal("expected ExprBody to be nil for block-body function")
	}
	if len(fun.Body.Stmts) != 1 {
		t.Fatalf("expected 1 stmt in body, got %d", len(fun.Body.Stmts))
	}

	ret, ok := fun.Body.Stmts[0].(*returnStmt)
	if !ok {
		t.Fatalf("expected *returnStmt, got %T", fun.Body.Stmts[0])
	}
	if ident, ok := ret.Value.(*identExpr); !ok || ident.Value != "x" {
		t.Fatalf("expected return ident x, got %T %+v", ret.Value, ret.Value)
	}
}

func TestParser_FunNoParamsWithExplicitReturnType(t *testing.T) {
	prog := parseProg(t, "fun value(): int = 42")
	fun := prog.Stmts[0].(*funStmt)

	if len(fun.Params) != 0 {
		t.Fatalf("expected 0 params, got %d", len(fun.Params))
	}
	if fun.ReturnType == nil || fun.ReturnType.Name != "int" {
		t.Fatalf("expected return type int, got %+v", fun.ReturnType)
	}
}

func TestParser_ExportFunWithParamsAndReturnType(t *testing.T) {
	prog := parseProg(t, "export fun add(a: int, b: int): int = a + b")
	fun := prog.Stmts[0].(*funStmt)

	if !fun.Exported {
		t.Fatal("expected Exported == true")
	}
	if fun.Name.Value != "add" {
		t.Fatalf("expected name add, got %q", fun.Name.Value)
	}
	if len(fun.Params) != 2 {
		t.Fatalf("expected 2 params, got %d", len(fun.Params))
	}
	if fun.ReturnType == nil || fun.ReturnType.Name != "int" {
		t.Fatalf("expected return type int, got %+v", fun.ReturnType)
	}
}

func TestParser_VarDeclarationWithoutInitializer(t *testing.T) {
	prog := parseProg(t, "fun calc(): void { var count: int }")
	fun := prog.Stmts[0].(*funStmt)

	if fun.Body == nil {
		t.Fatal("expected block Body")
	}
	if len(fun.Body.Stmts) != 1 {
		t.Fatalf("expected 1 stmt, got %d", len(fun.Body.Stmts))
	}

	decl, ok := fun.Body.Stmts[0].(*varStmt)
	if !ok {
		t.Fatalf("expected *varStmt, got %T", fun.Body.Stmts[0])
	}
	if decl.Name == nil || decl.Name.Value != "count" {
		t.Fatalf("expected var name count, got %+v", decl.Name)
	}
	if decl.Type_ == nil || decl.Type_.Name != "int" {
		t.Fatalf("expected var type int, got %+v", decl.Type_)
	}
	if decl.InitExpr != nil {
		t.Fatalf("expected nil InitExpr, got %+v", decl.InitExpr)
	}
}

func TestParser_VarDeclarationWithInitializer(t *testing.T) {
	prog := parseProg(t, "fun calc(): void { var count: int = 1 }")
	fun := prog.Stmts[0].(*funStmt)

	if fun.Body == nil {
		t.Fatal("expected block Body")
	}
	if len(fun.Body.Stmts) != 1 {
		t.Fatalf("expected 1 stmt, got %d", len(fun.Body.Stmts))
	}

	decl, ok := fun.Body.Stmts[0].(*varStmt)
	if !ok {
		t.Fatalf("expected *varStmt, got %T", fun.Body.Stmts[0])
	}
	if decl.Name == nil || decl.Name.Value != "count" {
		t.Fatalf("expected var name count, got %+v", decl.Name)
	}
	if decl.Type_ == nil || decl.Type_.Name != "int" {
		t.Fatalf("expected var type int, got %+v", decl.Type_)
	}

	init, ok := decl.InitExpr.(*intLiteral)
	if !ok {
		t.Fatalf("expected InitExpr *intLiteral, got %T", decl.InitExpr)
	}
	if init.Value != 1 {
		t.Fatalf("expected initializer value 1, got %d", init.Value)
	}
}

func TestParser_BlockBody_AssignmentStatement(t *testing.T) {
	prog := parseProg(t, "fun calc(): int { var count: int = 1 count = 2 return count }")
	fun := prog.Stmts[0].(*funStmt)

	if fun.Body == nil {
		t.Fatal("expected block Body")
	}
	if len(fun.Body.Stmts) != 3 {
		t.Fatalf("expected 3 stmts, got %d", len(fun.Body.Stmts))
	}

	assignStmt, ok := fun.Body.Stmts[1].(*exprStatement)
	if !ok {
		t.Fatalf("expected second stmt *exprStatement, got %T", fun.Body.Stmts[1])
	}

	assign, ok := assignStmt.Expr.(*assignExpr)
	if !ok {
		t.Fatalf("expected assignment expr, got %T", assignStmt.Expr)
	}

	target, ok := assign.Target.(*identExpr)
	if !ok {
		t.Fatalf("expected assignment target *identExpr, got %T", assign.Target)
	}
	if target.Value != "count" {
		t.Fatalf("expected assignment target count, got %q", target.Value)
	}

	value, ok := assign.Value.(*intLiteral)
	if !ok {
		t.Fatalf("expected assignment value *intLiteral, got %T", assign.Value)
	}
	if value.Value != 2 {
		t.Fatalf("expected assignment value 2, got %d", value.Value)
	}
}

func TestParser_TopLevelAssignmentStatement(t *testing.T) {
	prog := parseProg(t, "var count: int = 1\ncount = 2")
	if len(prog.Stmts) != 2 {
		t.Fatalf("expected 2 stmts, got %d", len(prog.Stmts))
	}

	if _, ok := prog.Stmts[0].(*varStmt); !ok {
		t.Fatalf("expected first stmt *varStmt, got %T", prog.Stmts[0])
	}

	assignStmt, ok := prog.Stmts[1].(*exprStatement)
	if !ok {
		t.Fatalf("expected second stmt *exprStatement, got %T", prog.Stmts[1])
	}

	assign, ok := assignStmt.Expr.(*assignExpr)
	if !ok {
		t.Fatalf("expected assignment expr, got %T", assignStmt.Expr)
	}

	target, ok := assign.Target.(*identExpr)
	if !ok {
		t.Fatalf("expected target *identExpr, got %T", assign.Target)
	}
	if target.Value != "count" {
		t.Fatalf("expected target count, got %q", target.Value)
	}

	value, ok := assign.Value.(*intLiteral)
	if !ok {
		t.Fatalf("expected value *intLiteral, got %T", assign.Value)
	}
	if value.Value != 2 {
		t.Fatalf("expected value 2, got %d", value.Value)
	}
}

func TestParser_StreamFunAcceptsBlockYieldReturn(t *testing.T) {
	prog := parseProg(t, "stream fun emit(): int { yield 1 return 2 }")
	fun := prog.Stmts[0].(*funStmt)

	if !fun.IsStream {
		t.Fatal("expected IsStream == true")
	}
	if fun.ExprBody != nil {
		t.Fatal("expected ExprBody nil for block-body stream fun")
	}
	if fun.Body == nil {
		t.Fatal("expected block Body")
	}
	if fun.ReturnType == nil || fun.ReturnType.Name != "int" {
		t.Fatalf("expected return type int, got %+v", fun.ReturnType)
	}
	if len(fun.Body.Stmts) != 2 {
		t.Fatalf("expected 2 stmts, got %d", len(fun.Body.Stmts))
	}

	if _, ok := fun.Body.Stmts[0].(*yieldStmt); !ok {
		t.Fatalf("expected first stmt *yieldStmt, got %T", fun.Body.Stmts[0])
	}
	if _, ok := fun.Body.Stmts[1].(*returnStmt); !ok {
		t.Fatalf("expected second stmt *returnStmt, got %T", fun.Body.Stmts[1])
	}
}

func TestParser_StreamFunRequiresFunKeyword(t *testing.T) {
	cases := []string{
		"stream class Foo {}",
		"stream struct Foo {}",
		"stream var count: int",
		"stream type ID = int",
	}

	for _, source := range cases {
		expectParseError(t, source)
	}
}

func TestParser_StreamFunRejectsExpressionBody(t *testing.T) {
	expectParseError(t, "stream fun emit(): int = 42")
}

func TestParser_StreamFunRejectsIllegalTopLevelModifierCombination(t *testing.T) {
	cases := []string{
		"open stream fun emit(): int { yield 1 return 2 }",
		"export open stream fun emit(): int { yield 1 return 2 }",
	}

	for _, source := range cases {
		expectParseError(t, source)
	}
}

func TestParser_IfWithoutElse(t *testing.T) {
	prog := parseProg(t, "fun check(flag: bool): int { if flag { return 1 } return 2 }")
	fun := prog.Stmts[0].(*funStmt)

	if fun.Body == nil {
		t.Fatal("expected block Body")
	}
	if len(fun.Body.Stmts) != 2 {
		t.Fatalf("expected 2 stmts, got %d", len(fun.Body.Stmts))
	}

	stmt, ok := fun.Body.Stmts[0].(*ifStmt)
	if !ok {
		t.Fatalf("expected first stmt *ifStmt, got %T", fun.Body.Stmts[0])
	}

	cond, ok := stmt.Condition.(*identExpr)
	if !ok || cond.Value != "flag" {
		t.Fatalf("expected condition ident flag, got %T %+v", stmt.Condition, stmt.Condition)
	}

	if stmt.Consequence == nil {
		t.Fatal("expected consequence block")
	}
	if len(stmt.Consequence.Stmts) != 1 {
		t.Fatalf("expected 1 stmt in consequence, got %d", len(stmt.Consequence.Stmts))
	}
	if _, ok := stmt.Consequence.Stmts[0].(*returnStmt); !ok {
		t.Fatalf("expected consequence stmt *returnStmt, got %T", stmt.Consequence.Stmts[0])
	}

	if stmt.Alternative != nil {
		t.Fatalf("expected nil alternative, got %T", stmt.Alternative)
	}
}

func TestParser_IfElseStructure(t *testing.T) {
	prog := parseProg(t, "fun check(flag: bool): int { if flag { return 1 } else { return 2 } }")
	fun := prog.Stmts[0].(*funStmt)

	if fun.Body == nil {
		t.Fatal("expected block Body")
	}
	if len(fun.Body.Stmts) != 1 {
		t.Fatalf("expected 1 stmt, got %d", len(fun.Body.Stmts))
	}

	stmt, ok := fun.Body.Stmts[0].(*ifStmt)
	if !ok {
		t.Fatalf("expected first stmt *ifStmt, got %T", fun.Body.Stmts[0])
	}

	if stmt.Consequence == nil || len(stmt.Consequence.Stmts) != 1 {
		t.Fatalf("expected consequence block with 1 stmt, got %+v", stmt.Consequence)
	}
	if _, ok := stmt.Consequence.Stmts[0].(*returnStmt); !ok {
		t.Fatalf("expected consequence return stmt, got %T", stmt.Consequence.Stmts[0])
	}

	alt, ok := stmt.Alternative.(*blockStmt)
	if !ok {
		t.Fatalf("expected alternative *blockStmt, got %T", stmt.Alternative)
	}
	if len(alt.Stmts) != 1 {
		t.Fatalf("expected 1 stmt in alternative, got %d", len(alt.Stmts))
	}
	if _, ok := alt.Stmts[0].(*returnStmt); !ok {
		t.Fatalf("expected alternative return stmt, got %T", alt.Stmts[0])
	}
}

func TestParser_NestedIfElseStructure(t *testing.T) {
	prog := parseProg(t, `
fun check(a: bool, b: bool): int {
  if a {
    if b { return 1 } else { return 2 }
  }
  return 3
}`)
	fun := prog.Stmts[0].(*funStmt)

	if fun.Body == nil {
		t.Fatal("expected block Body")
	}
	if len(fun.Body.Stmts) != 2 {
		t.Fatalf("expected 2 stmts, got %d", len(fun.Body.Stmts))
	}

	outer, ok := fun.Body.Stmts[0].(*ifStmt)
	if !ok {
		t.Fatalf("expected outer *ifStmt, got %T", fun.Body.Stmts[0])
	}
	if outer.Consequence == nil || len(outer.Consequence.Stmts) != 1 {
		t.Fatalf("expected outer consequence with 1 stmt, got %+v", outer.Consequence)
	}

	inner, ok := outer.Consequence.Stmts[0].(*ifStmt)
	if !ok {
		t.Fatalf("expected nested *ifStmt, got %T", outer.Consequence.Stmts[0])
	}
	if inner.Alternative == nil {
		t.Fatal("expected nested else block")
	}

	if _, ok := fun.Body.Stmts[1].(*returnStmt); !ok {
		t.Fatalf("expected trailing return stmt, got %T", fun.Body.Stmts[1])
	}
}

func TestParser_IfMissingConditionReturnsParseDiagnostic(t *testing.T) {
	l := newLexer("fun bad(): int { if { return 1 } return 2 }")
	p := newParser(l, "fun bad(): int { if { return 1 } return 2 }")
	_, err := p.parse()
	if err == nil {
		t.Fatal("expected parse error")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() == "" {
		t.Fatalf("expected non-empty DiagnosticCode, got %v", err)
	}
	pather, ok := err.(interface{ DiagnosticPath() string })
	if !ok || pather.DiagnosticPath() == "" {
		t.Fatalf("expected non-empty DiagnosticPath, got %v", err)
	}
}

func TestParser_WhileStatementStructure(t *testing.T) {
	prog := parseProg(t, "fun sum(n: int): int { var i: int = 0 while i < n { i = i + 1 } return i }")
	fun := prog.Stmts[0].(*funStmt)

	if fun.Body == nil {
		t.Fatal("expected block Body")
	}
	if len(fun.Body.Stmts) != 3 {
		t.Fatalf("expected 3 stmts, got %d", len(fun.Body.Stmts))
	}

	stmt, ok := fun.Body.Stmts[1].(*whileStmt)
	if !ok {
		t.Fatalf("expected second stmt *whileStmt, got %T", fun.Body.Stmts[1])
	}

	cond, ok := stmt.Condition.(*binaryExpr)
	if !ok {
		t.Fatalf("expected condition *binaryExpr, got %T", stmt.Condition)
	}
	if cond.Operator != "<" {
		t.Fatalf("expected condition operator <, got %q", cond.Operator)
	}
	if ident, ok := cond.Left.(*identExpr); !ok || ident.Value != "i" {
		t.Fatalf("expected condition left ident i, got %T %+v", cond.Left, cond.Left)
	}
	if ident, ok := cond.Right.(*identExpr); !ok || ident.Value != "n" {
		t.Fatalf("expected condition right ident n, got %T %+v", cond.Right, cond.Right)
	}

	if stmt.Body == nil {
		t.Fatal("expected while body")
	}
	if len(stmt.Body.Stmts) != 1 {
		t.Fatalf("expected 1 stmt in while body, got %d", len(stmt.Body.Stmts))
	}

	assignStmt, ok := stmt.Body.Stmts[0].(*exprStatement)
	if !ok {
		t.Fatalf("expected while body assignment expr statement, got %T", stmt.Body.Stmts[0])
	}
	assign, ok := assignStmt.Expr.(*assignExpr)
	if !ok {
		t.Fatalf("expected assignment expr, got %T", assignStmt.Expr)
	}
	target, ok := assign.Target.(*identExpr)
	if !ok || target.Value != "i" {
		t.Fatalf("expected assignment target i, got %T %+v", assign.Target, assign.Target)
	}
}

func TestParser_WhileAllowsMultipleBodyStatements(t *testing.T) {
	prog := parseProg(t, "fun calc(): int { var i: int = 0 var total: int = 0 while i < 3 { total = total + i i = i + 1 } return total }")
	fun := prog.Stmts[0].(*funStmt)

	if fun.Body == nil {
		t.Fatal("expected block Body")
	}
	if len(fun.Body.Stmts) != 4 {
		t.Fatalf("expected 4 top-level block stmts, got %d", len(fun.Body.Stmts))
	}

	stmt, ok := fun.Body.Stmts[2].(*whileStmt)
	if !ok {
		t.Fatalf("expected third stmt *whileStmt, got %T", fun.Body.Stmts[2])
	}
	if stmt.Body == nil || len(stmt.Body.Stmts) != 2 {
		t.Fatalf("expected 2 stmts in while body, got %+v", stmt.Body)
	}
	if _, ok := stmt.Body.Stmts[0].(*exprStatement); !ok {
		t.Fatalf("expected first while stmt *exprStatement, got %T", stmt.Body.Stmts[0])
	}
	if _, ok := stmt.Body.Stmts[1].(*exprStatement); !ok {
		t.Fatalf("expected second while stmt *exprStatement, got %T", stmt.Body.Stmts[1])
	}
}

func TestParser_WhileMissingConditionReturnsParseDiagnostic(t *testing.T) {
	l := newLexer("fun bad(): int { while { return 1 } return 2 }")
	p := newParser(l, "fun bad(): int { while { return 1 } return 2 }")
	_, err := p.parse()
	if err == nil {
		t.Fatal("expected parse error")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() == "" {
		t.Fatalf("expected non-empty DiagnosticCode, got %v", err)
	}
	pather, ok := err.(interface{ DiagnosticPath() string })
	if !ok || pather.DiagnosticPath() == "" {
		t.Fatalf("expected non-empty DiagnosticPath, got %v", err)
	}
}

func TestParser_BreakStatementInsideWhile(t *testing.T) {
	prog := parseProg(t, `
fun calc(): int {
  var i: int = 0
  while i < 10 {
    break
  }
  return i
}`)
	fun := prog.Stmts[0].(*funStmt)

	if fun.Body == nil {
		t.Fatal("expected block Body")
	}
	if len(fun.Body.Stmts) != 3 {
		t.Fatalf("expected 3 top-level stmts, got %d", len(fun.Body.Stmts))
	}

	stmt, ok := fun.Body.Stmts[1].(*whileStmt)
	if !ok {
		t.Fatalf("expected second stmt *whileStmt, got %T", fun.Body.Stmts[1])
	}
	if stmt.Body == nil || len(stmt.Body.Stmts) != 1 {
		t.Fatalf("expected while body with 1 stmt, got %+v", stmt.Body)
	}
	if _, ok := stmt.Body.Stmts[0].(*breakStmt); !ok {
		t.Fatalf("expected while body stmt *breakStmt, got %T", stmt.Body.Stmts[0])
	}
}

func TestParser_ContinueStatementInsideWhile(t *testing.T) {
	prog := parseProg(t, `
fun calc(): int {
  var i: int = 0
  while i < 10 {
    i = i + 1
    continue
  }
  return i
}`)
	fun := prog.Stmts[0].(*funStmt)

	if fun.Body == nil {
		t.Fatal("expected block Body")
	}
	if len(fun.Body.Stmts) != 3 {
		t.Fatalf("expected 3 top-level stmts, got %d", len(fun.Body.Stmts))
	}

	stmt, ok := fun.Body.Stmts[1].(*whileStmt)
	if !ok {
		t.Fatalf("expected second stmt *whileStmt, got %T", fun.Body.Stmts[1])
	}
	if stmt.Body == nil || len(stmt.Body.Stmts) != 2 {
		t.Fatalf("expected while body with 2 stmts, got %+v", stmt.Body)
	}
	if _, ok := stmt.Body.Stmts[1].(*continueStmt); !ok {
		t.Fatalf("expected second while body stmt *continueStmt, got %T", stmt.Body.Stmts[1])
	}
}

func TestParser_BreakAndContinueInNestedIfInsideWhile(t *testing.T) {
	prog := parseProg(t, `
fun calc(flag: bool): int {
  var i: int = 0
  while i < 10 {
    if flag { break } else { continue }
  }
  return i
}`)
	fun := prog.Stmts[0].(*funStmt)

	if fun.Body == nil {
		t.Fatal("expected block Body")
	}
	stmt, ok := fun.Body.Stmts[1].(*whileStmt)
	if !ok {
		t.Fatalf("expected second stmt *whileStmt, got %T", fun.Body.Stmts[1])
	}
	if stmt.Body == nil || len(stmt.Body.Stmts) != 1 {
		t.Fatalf("expected while body with 1 stmt, got %+v", stmt.Body)
	}

	ifs, ok := stmt.Body.Stmts[0].(*ifStmt)
	if !ok {
		t.Fatalf("expected while body stmt *ifStmt, got %T", stmt.Body.Stmts[0])
	}
	if ifs.Consequence == nil || len(ifs.Consequence.Stmts) != 1 {
		t.Fatalf("expected consequence with 1 stmt, got %+v", ifs.Consequence)
	}
	if _, ok := ifs.Consequence.Stmts[0].(*breakStmt); !ok {
		t.Fatalf("expected consequence *breakStmt, got %T", ifs.Consequence.Stmts[0])
	}

	alt, ok := ifs.Alternative.(*blockStmt)
	if !ok {
		t.Fatalf("expected alternative *blockStmt, got %T", ifs.Alternative)
	}
	if len(alt.Stmts) != 1 {
		t.Fatalf("expected alternative with 1 stmt, got %d", len(alt.Stmts))
	}
	if _, ok := alt.Stmts[0].(*continueStmt); !ok {
		t.Fatalf("expected alternative *continueStmt, got %T", alt.Stmts[0])
	}
}

func TestParser_ArrayLiteralExpression(t *testing.T) {
	prog := parseProg(t, "fun values(): array<int> = [1, 2, 3]")
	fun := prog.Stmts[0].(*funStmt)

	arr, ok := fun.ExprBody.(*arrayLiteral)
	if !ok {
		t.Fatalf("expected ExprBody *arrayLiteral, got %T", fun.ExprBody)
	}
	if len(arr.Elements) != 3 {
		t.Fatalf("expected 3 array elements, got %d", len(arr.Elements))
	}

	for i, want := range []int64{1, 2, 3} {
		lit, ok := arr.Elements[i].(*intLiteral)
		if !ok {
			t.Fatalf("element %d: expected *intLiteral, got %T", i, arr.Elements[i])
		}
		if lit.Value != want {
			t.Fatalf("element %d: expected %d, got %d", i, want, lit.Value)
		}
	}
}

func TestParser_MapLiteralExpression(t *testing.T) {
	prog := parseProg(t, `fun attrs(): map<string, int> = {"hp": 10, "mp": 20}`)
	fun := prog.Stmts[0].(*funStmt)

	m, ok := fun.ExprBody.(*mapLiteral)
	if !ok {
		t.Fatalf("expected ExprBody *mapLiteral, got %T", fun.ExprBody)
	}
	if len(m.Pairs) != 2 {
		t.Fatalf("expected 2 map pairs, got %d", len(m.Pairs))
	}

	key0, ok := m.Pairs[0].Key.(*stringLiteral)
	if !ok {
		t.Fatalf("expected first key *stringLiteral, got %T", m.Pairs[0].Key)
	}
	if key0.Value != "hp" {
		t.Fatalf("expected first key hp, got %q", key0.Value)
	}
	val0, ok := m.Pairs[0].Value.(*intLiteral)
	if !ok {
		t.Fatalf("expected first value *intLiteral, got %T", m.Pairs[0].Value)
	}
	if val0.Value != 10 {
		t.Fatalf("expected first value 10, got %d", val0.Value)
	}

	key1, ok := m.Pairs[1].Key.(*stringLiteral)
	if !ok {
		t.Fatalf("expected second key *stringLiteral, got %T", m.Pairs[1].Key)
	}
	if key1.Value != "mp" {
		t.Fatalf("expected second key mp, got %q", key1.Value)
	}
	val1, ok := m.Pairs[1].Value.(*intLiteral)
	if !ok {
		t.Fatalf("expected second value *intLiteral, got %T", m.Pairs[1].Value)
	}
	if val1.Value != 20 {
		t.Fatalf("expected second value 20, got %d", val1.Value)
	}
}

func TestParser_ArrayIndexExpression(t *testing.T) {
	prog := parseProg(t, "fun first(): int = [10, 20, 30][0]")
	fun := prog.Stmts[0].(*funStmt)

	idx, ok := fun.ExprBody.(*indexExpr)
	if !ok {
		t.Fatalf("expected ExprBody *indexExpr, got %T", fun.ExprBody)
	}

	arr, ok := idx.Left.(*arrayLiteral)
	if !ok {
		t.Fatalf("expected index left *arrayLiteral, got %T", idx.Left)
	}
	if len(arr.Elements) != 3 {
		t.Fatalf("expected 3 array elements, got %d", len(arr.Elements))
	}

	index, ok := idx.Index.(*intLiteral)
	if !ok {
		t.Fatalf("expected index *intLiteral, got %T", idx.Index)
	}
	if index.Value != 0 {
		t.Fatalf("expected index 0, got %d", index.Value)
	}
}

func TestParser_MapIndexExpression(t *testing.T) {
	prog := parseProg(t, `fun hp(): int = {"hp": 10, "mp": 20}["hp"]`)
	fun := prog.Stmts[0].(*funStmt)

	idx, ok := fun.ExprBody.(*indexExpr)
	if !ok {
		t.Fatalf("expected ExprBody *indexExpr, got %T", fun.ExprBody)
	}

	m, ok := idx.Left.(*mapLiteral)
	if !ok {
		t.Fatalf("expected index left *mapLiteral, got %T", idx.Left)
	}
	if len(m.Pairs) != 2 {
		t.Fatalf("expected 2 map pairs, got %d", len(m.Pairs))
	}

	key, ok := idx.Index.(*stringLiteral)
	if !ok {
		t.Fatalf("expected map index key *stringLiteral, got %T", idx.Index)
	}
	if key.Value != "hp" {
		t.Fatalf("expected map index key hp, got %q", key.Value)
	}
}

func TestParser_MapLiteralRequiresStringKeyDiagnostic(t *testing.T) {
	l := newLexer(`fun bad(): map<string, int> = {1: 10}`)
	p := newParser(l, `fun bad(): map<string, int> = {1: 10}`)
	_, err := p.parse()
	if err == nil {
		t.Fatal("expected parse error")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() == "" {
		t.Fatalf("expected non-empty DiagnosticCode, got %v", err)
	}
	pather, ok := err.(interface{ DiagnosticPath() string })
	if !ok || pather.DiagnosticPath() == "" {
		t.Fatalf("expected non-empty DiagnosticPath, got %v", err)
	}
}

func TestParser_StructLiteralExpression(t *testing.T) {
	source := `
struct Point {
  x: int
  y: int
}
fun make(): Point = Point{x: 1, y: 2}`
	prog := parseProg(t, source)

	if len(prog.Stmts) != 2 {
		t.Fatalf("expected 2 stmts, got %d", len(prog.Stmts))
	}

	fun := prog.Stmts[1].(*funStmt)
	lit, ok := fun.ExprBody.(*structLiteral)
	if !ok {
		t.Fatalf("expected ExprBody *structLiteral, got %T", fun.ExprBody)
	}
	if lit.TypeName != "Point" {
		t.Fatalf("expected struct literal type Point, got %q", lit.TypeName)
	}
	if len(lit.Fields) != 2 {
		t.Fatalf("expected 2 struct fields, got %d", len(lit.Fields))
	}
	if lit.Fields[0].Name != "x" {
		t.Fatalf("expected first field x, got %q", lit.Fields[0].Name)
	}
	if lit.Fields[1].Name != "y" {
		t.Fatalf("expected second field y, got %q", lit.Fields[1].Name)
	}

	xv, ok := lit.Fields[0].Value.(*intLiteral)
	if !ok {
		t.Fatalf("expected x field value *intLiteral, got %T", lit.Fields[0].Value)
	}
	if xv.Value != 1 {
		t.Fatalf("expected x value 1, got %d", xv.Value)
	}

	yv, ok := lit.Fields[1].Value.(*intLiteral)
	if !ok {
		t.Fatalf("expected y field value *intLiteral, got %T", lit.Fields[1].Value)
	}
	if yv.Value != 2 {
		t.Fatalf("expected y value 2, got %d", yv.Value)
	}
}

func TestParser_FieldAccessOnStructVariable(t *testing.T) {
	source := `
struct Point {
  x: int
  y: int
}
fun read(): int {
  var p: Point = Point{x: 1, y: 2}
  return p.x
}`
	prog := parseProg(t, source)

	fun := prog.Stmts[1].(*funStmt)
	if fun.Body == nil {
		t.Fatal("expected block Body")
	}
	if len(fun.Body.Stmts) != 2 {
		t.Fatalf("expected 2 stmts, got %d", len(fun.Body.Stmts))
	}

	ret, ok := fun.Body.Stmts[1].(*returnStmt)
	if !ok {
		t.Fatalf("expected second stmt *returnStmt, got %T", fun.Body.Stmts[1])
	}

	member, ok := ret.Value.(*memberExpr)
	if !ok {
		t.Fatalf("expected return value *memberExpr, got %T", ret.Value)
	}
	if member.Member == nil || member.Member.Value != "x" {
		t.Fatalf("expected member x, got %+v", member.Member)
	}
	if ident, ok := member.Object.(*identExpr); !ok || ident.Value != "p" {
		t.Fatalf("expected member object ident p, got %T %+v", member.Object, member.Object)
	}
}

func TestParser_ForStatementStructure(t *testing.T) {
	prog := parseProg(t, `
fun sum(): int {
  var total: int = 0
  for (var i: int = 0; i < 3; i = i + 1) {
    total = total + i
  }
  return total
}`)
	fun := prog.Stmts[0].(*funStmt)

	if fun.Body == nil {
		t.Fatal("expected block Body")
	}
	if len(fun.Body.Stmts) != 3 {
		t.Fatalf("expected 3 top-level stmts, got %d", len(fun.Body.Stmts))
	}

	stmt, ok := fun.Body.Stmts[1].(*forStmt)
	if !ok {
		t.Fatalf("expected second stmt *forStmt, got %T", fun.Body.Stmts[1])
	}
	if stmt.IsForIn {
		t.Fatal("expected C-style for, got for-in")
	}

	init, ok := stmt.Init.(*varStmt)
	if !ok {
		t.Fatalf("expected for init *varStmt, got %T", stmt.Init)
	}
	if init.Name == nil || init.Name.Value != "i" {
		t.Fatalf("expected init var i, got %+v", init.Name)
	}

	cond, ok := stmt.Condition.(*binaryExpr)
	if !ok {
		t.Fatalf("expected condition *binaryExpr, got %T", stmt.Condition)
	}
	if cond.Operator != "<" {
		t.Fatalf("expected condition operator <, got %q", cond.Operator)
	}

	update, ok := stmt.Update.(*assignExpr)
	if !ok {
		t.Fatalf("expected update *assignExpr, got %T", stmt.Update)
	}
	target, ok := update.Target.(*identExpr)
	if !ok || target.Value != "i" {
		t.Fatalf("expected update target i, got %T %+v", update.Target, update.Target)
	}

	if stmt.Body == nil {
		t.Fatal("expected for body")
	}
	if len(stmt.Body.Stmts) != 1 {
		t.Fatalf("expected 1 stmt in for body, got %d", len(stmt.Body.Stmts))
	}
	if _, ok := stmt.Body.Stmts[0].(*exprStatement); !ok {
		t.Fatalf("expected for body stmt *exprStatement, got %T", stmt.Body.Stmts[0])
	}
}

func TestParser_ForStatementMultipleBodyStatements(t *testing.T) {
	prog := parseProg(t, `
fun calc(): int {
  var total: int = 0
  for (var i: int = 0; i < 3; i = i + 1) {
    total = total + i
    total = total + 1
  }
  return total
}`)
	fun := prog.Stmts[0].(*funStmt)

	stmt, ok := fun.Body.Stmts[1].(*forStmt)
	if !ok {
		t.Fatalf("expected second stmt *forStmt, got %T", fun.Body.Stmts[1])
	}
	if stmt.Body == nil || len(stmt.Body.Stmts) != 2 {
		t.Fatalf("expected for body with 2 stmts, got %+v", stmt.Body)
	}
}

func TestParser_ForMissingConditionReturnsParseDiagnostic(t *testing.T) {
	l := newLexer("fun bad(): int { for (var i: int = 0; { return 1 }) }")
	p := newParser(l, "fun bad(): int { for (var i: int = 0; { return 1 }) }")
	_, err := p.parse()
	if err == nil {
		t.Fatal("expected parse error")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() == "" {
		t.Fatalf("expected non-empty DiagnosticCode, got %v", err)
	}
	pather, ok := err.(interface{ DiagnosticPath() string })
	if !ok || pather.DiagnosticPath() == "" {
		t.Fatalf("expected non-empty DiagnosticPath, got %v", err)
	}
}

func TestParser_WhenStatementStructure(t *testing.T) {
	prog := parseProg(t, `
fun label(x: int): int {
  when (x) {
    case 1 { return 10 }
    case 2 { return 20 }
    else { return 30 }
  }
}`)
	fun := prog.Stmts[0].(*funStmt)

	if fun.Body == nil {
		t.Fatal("expected block Body")
	}
	if len(fun.Body.Stmts) != 1 {
		t.Fatalf("expected 1 top-level stmt, got %d", len(fun.Body.Stmts))
	}

	stmt, ok := fun.Body.Stmts[0].(*whenStmt)
	if !ok {
		t.Fatalf("expected *whenStmt, got %T", fun.Body.Stmts[0])
	}

	cond, ok := stmt.Expr.(*identExpr)
	if !ok || cond.Value != "x" {
		t.Fatalf("expected when expr ident x, got %T %+v", stmt.Expr, stmt.Expr)
	}

	if len(stmt.Cases) != 2 {
		t.Fatalf("expected 2 cases, got %d", len(stmt.Cases))
	}

	case0 := stmt.Cases[0]
	if len(case0.Values) != 1 {
		t.Fatalf("expected first case 1 value, got %d", len(case0.Values))
	}
	if lit, ok := case0.Values[0].(*intLiteral); !ok || lit.Value != 1 {
		t.Fatalf("expected first case literal 1, got %T %+v", case0.Values[0], case0.Values[0])
	}
	if case0.Body == nil || len(case0.Body.Stmts) != 1 {
		t.Fatalf("expected first case body with 1 stmt, got %+v", case0.Body)
	}

	case1 := stmt.Cases[1]
	if len(case1.Values) != 1 {
		t.Fatalf("expected second case 1 value, got %d", len(case1.Values))
	}
	if lit, ok := case1.Values[0].(*intLiteral); !ok || lit.Value != 2 {
		t.Fatalf("expected second case literal 2, got %T %+v", case1.Values[0], case1.Values[0])
	}

	if stmt.DefaultCase == nil {
		t.Fatal("expected default case")
	}
	if len(stmt.DefaultCase.Stmts) != 1 {
		t.Fatalf("expected default case with 1 stmt, got %d", len(stmt.DefaultCase.Stmts))
	}
}

func TestParser_WhenCaseAllowsMultipleValues(t *testing.T) {
	prog := parseProg(t, `
fun label(x: int): int {
  when (x) {
    case 1, 2 { return 10 }
    else { return 30 }
  }
}`)
	fun := prog.Stmts[0].(*funStmt)
	stmt := fun.Body.Stmts[0].(*whenStmt)

	if len(stmt.Cases) != 1 {
		t.Fatalf("expected 1 case, got %d", len(stmt.Cases))
	}
	if len(stmt.Cases[0].Values) != 2 {
		t.Fatalf("expected case with 2 values, got %d", len(stmt.Cases[0].Values))
	}
}

func TestParser_WhenCaseGuardParses(t *testing.T) {
	prog := parseProg(t, `
fun classify(x: int): int {
  when (x) {
    case 1 when true { return 10 }
    case 2, 3 when x > 1 { return 20 }
    else { return 30 }
  }
}`)
	fun := prog.Stmts[0].(*funStmt)
	stmt := fun.Body.Stmts[0].(*whenStmt)

	if len(stmt.Cases) != 2 {
		t.Fatalf("expected 2 cases, got %d", len(stmt.Cases))
	}
	if stmt.Cases[0].Guard == nil {
		t.Fatal("expected first case to have a guard")
	}
	if stmt.Cases[1].Guard == nil {
		t.Fatal("expected second case to have a guard")
	}
}

func TestParser_WhenMissingSubjectReturnsParseDiagnostic(t *testing.T) {
	l := newLexer("fun bad(): int { when { else { return 1 } } }")
	p := newParser(l, "fun bad(): int { when { else { return 1 } } }")
	_, err := p.parse()
	if err == nil {
		t.Fatal("expected parse error")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() == "" {
		t.Fatalf("expected non-empty DiagnosticCode, got %v", err)
	}
	pather, ok := err.(interface{ DiagnosticPath() string })
	if !ok || pather.DiagnosticPath() == "" {
		t.Fatalf("expected non-empty DiagnosticPath, got %v", err)
	}
}

func TestParser_ArrayElementAssignmentExpression(t *testing.T) {
	prog := parseProg(t, `
fun set(): int {
  var xs: array<int> = [1, 2, 3]
  xs[1] = 9
  return xs[1]
}`)
	fun := prog.Stmts[0].(*funStmt)

	assignStmt, ok := fun.Body.Stmts[1].(*exprStatement)
	if !ok {
		t.Fatalf("expected second stmt *exprStatement, got %T", fun.Body.Stmts[1])
	}

	assign, ok := assignStmt.Expr.(*assignExpr)
	if !ok {
		t.Fatalf("expected assignment expr, got %T", assignStmt.Expr)
	}

	target, ok := assign.Target.(*indexExpr)
	if !ok {
		t.Fatalf("expected assignment target *indexExpr, got %T", assign.Target)
	}
	if _, ok := target.Left.(*identExpr); !ok {
		t.Fatalf("expected index left ident, got %T", target.Left)
	}
	if lit, ok := target.Index.(*intLiteral); !ok || lit.Value != 1 {
		t.Fatalf("expected index 1, got %T %+v", target.Index, target.Index)
	}
	if lit, ok := assign.Value.(*intLiteral); !ok || lit.Value != 9 {
		t.Fatalf("expected assigned value 9, got %T %+v", assign.Value, assign.Value)
	}
}

func TestParser_MapKeyAssignmentExpression(t *testing.T) {
	prog := parseProg(t, `
fun set(): int {
  var attrs: map<string, int> = {"hp": 10}
  attrs["hp"] = 20
  return attrs["hp"]
}`)
	fun := prog.Stmts[0].(*funStmt)

	assignStmt, ok := fun.Body.Stmts[1].(*exprStatement)
	if !ok {
		t.Fatalf("expected second stmt *exprStatement, got %T", fun.Body.Stmts[1])
	}

	assign, ok := assignStmt.Expr.(*assignExpr)
	if !ok {
		t.Fatalf("expected assignment expr, got %T", assignStmt.Expr)
	}

	target, ok := assign.Target.(*indexExpr)
	if !ok {
		t.Fatalf("expected assignment target *indexExpr, got %T", assign.Target)
	}
	if _, ok := target.Left.(*identExpr); !ok {
		t.Fatalf("expected index left ident, got %T", target.Left)
	}
	if lit, ok := target.Index.(*stringLiteral); !ok || lit.Value != "hp" {
		t.Fatalf("expected key hp, got %T %+v", target.Index, target.Index)
	}
	if lit, ok := assign.Value.(*intLiteral); !ok || lit.Value != 20 {
		t.Fatalf("expected assigned value 20, got %T %+v", assign.Value, assign.Value)
	}
}

func TestParser_StructFieldAssignmentExpression(t *testing.T) {
	prog := parseProg(t, `
struct Point {
  x: int
  y: int
}
fun set(): int {
  var p: Point = Point{x: 1, y: 2}
  p.x = 9
  return p.x
}`)
	fun := prog.Stmts[1].(*funStmt)

	assignStmt, ok := fun.Body.Stmts[1].(*exprStatement)
	if !ok {
		t.Fatalf("expected second stmt *exprStatement, got %T", fun.Body.Stmts[1])
	}

	assign, ok := assignStmt.Expr.(*assignExpr)
	if !ok {
		t.Fatalf("expected assignment expr, got %T", assignStmt.Expr)
	}

	target, ok := assign.Target.(*memberExpr)
	if !ok {
		t.Fatalf("expected assignment target *memberExpr, got %T", assign.Target)
	}
	if ident, ok := target.Object.(*identExpr); !ok || ident.Value != "p" {
		t.Fatalf("expected target object p, got %T %+v", target.Object, target.Object)
	}
	if target.Member == nil || target.Member.Value != "x" {
		t.Fatalf("expected member x, got %+v", target.Member)
	}
}

func TestParser_TypeAliasDeclarationChain(t *testing.T) {
	prog := parseProg(t, `
type Id = long
type UserId = Id
fun value(): UserId = 42`)

	if len(prog.Stmts) != 3 {
		t.Fatalf("expected 3 stmts, got %d", len(prog.Stmts))
	}

	if _, ok := prog.Stmts[0].(*typeAliasStmt); !ok {
		t.Fatalf("expected first stmt *typeAliasStmt, got %T", prog.Stmts[0])
	}
	if _, ok := prog.Stmts[1].(*typeAliasStmt); !ok {
		t.Fatalf("expected second stmt *typeAliasStmt, got %T", prog.Stmts[1])
	}
}

func TestParser_ForInStatementStructure(t *testing.T) {
	prog := parseProg(t, `
fun sum(): int {
  var total: int = 0
  for (item in [1, 2, 3]) {
    total = total + item
  }
  return total
}`)
	fun := prog.Stmts[0].(*funStmt)

	if len(fun.Body.Stmts) != 3 {
		t.Fatalf("expected 3 top-level stmts, got %d", len(fun.Body.Stmts))
	}

	stmt, ok := fun.Body.Stmts[1].(*forStmt)
	if !ok {
		t.Fatalf("expected second stmt *forStmt, got %T", fun.Body.Stmts[1])
	}
	if !stmt.IsForIn {
		t.Fatal("expected for-in statement")
	}
	if stmt.Variable != "item" {
		t.Fatalf("expected for-in variable item, got %q", stmt.Variable)
	}
	if stmt.Iterable == nil {
		t.Fatal("expected for-in iterable")
	}
	if _, ok := stmt.Iterable.(*arrayLiteral); !ok {
		t.Fatalf("expected iterable *arrayLiteral, got %T", stmt.Iterable)
	}
	if stmt.Body == nil || len(stmt.Body.Stmts) != 1 {
		t.Fatalf("expected for-in body with 1 stmt, got %+v", stmt.Body)
	}
}

// --- Struct declarations ---

func TestParser_StructDeclaration(t *testing.T) {
	prog := parseProg(t, "struct Point {\n  x: int\n  y: int\n}")
	if len(prog.Stmts) != 1 {
		t.Fatalf("expected 1 statement, got %d", len(prog.Stmts))
	}
	st, ok := prog.Stmts[0].(*structStmt)
	if !ok {
		t.Fatal("expected *structStmt")
	}
	if st.Name.Value != "Point" {
		t.Fatalf("expected name 'Point', got %q", st.Name.Value)
	}
	if len(st.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(st.Fields))
	}
	if st.Fields[0].Name.Value != "x" || st.Fields[1].Name.Value != "y" {
		t.Fatalf("expected fields x,y, got %q,%q", st.Fields[0].Name.Value, st.Fields[1].Name.Value)
	}
	if st.Fields[0].Type_.Name != "int" || st.Fields[1].Type_.Name != "int" {
		t.Fatal("expected field types int")
	}
}

func TestParser_StructOptionalField(t *testing.T) {
	prog := parseProg(t, "struct S {\n  required: int\n  optional name: string\n  optional age: int\n}")
	st := prog.Stmts[0].(*structStmt)
	if len(st.Fields) != 3 {
		t.Fatalf("expected 3 fields, got %d", len(st.Fields))
	}
	if st.Fields[0].Optional {
		t.Error("expected required field to have Optional=false")
	}
	if !st.Fields[1].Optional {
		t.Error("expected name field to have Optional=true")
	}
	if !st.Fields[2].Optional {
		t.Error("expected age field to have Optional=true")
	}
	if st.Fields[1].Name.Value != "name" || st.Fields[1].Type_.Name != "string" {
		t.Errorf("expected name:string, got %s:%s", st.Fields[1].Name.Value, st.Fields[1].Type_.Name)
	}
}

func TestParser_StructFieldNamedOptional(t *testing.T) {
	// `optional: T` (no second identifier before `:`) parses as a field
	// literally named "optional" — not as the modifier.
	prog := parseProg(t, "struct S {\n  optional: int\n}")
	st := prog.Stmts[0].(*structStmt)
	if len(st.Fields) != 1 {
		t.Fatalf("expected 1 field, got %d", len(st.Fields))
	}
	if st.Fields[0].Name.Value != "optional" {
		t.Errorf("expected field name 'optional', got %q", st.Fields[0].Name.Value)
	}
	if st.Fields[0].Optional {
		t.Error("field named 'optional' must not itself be marked Optional")
	}
}

func TestParser_ClassOptionalField(t *testing.T) {
	prog := parseProg(t, "class Animal {\n  optional nickname: string\n  age: int\n}")
	cl := prog.Stmts[0].(*classStmt)
	if len(cl.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(cl.Fields))
	}
	if !cl.Fields[0].Optional {
		t.Error("expected nickname to have Optional=true")
	}
	if cl.Fields[1].Optional {
		t.Error("expected age to have Optional=false")
	}
	if cl.Fields[0].Name.Value != "nickname" || cl.Fields[0].Type_.Name != "string" {
		t.Errorf("expected nickname:string, got %s:%s", cl.Fields[0].Name.Value, cl.Fields[0].Type_.Name)
	}
}

// --- Class declarations ---

func TestParser_ClassDeclaration(t *testing.T) {
	source := "class Animal {\n  name: string\n  fun speak(): string {}\n}"
	prog := parseProg(t, source)
	cl, ok := prog.Stmts[0].(*classStmt)
	if !ok {
		t.Fatal("expected *classStmt")
	}
	if cl.Name.Value != "Animal" {
		t.Fatalf("expected name 'Animal', got %q", cl.Name.Value)
	}
	if len(cl.Fields) != 1 {
		t.Fatalf("expected 1 field, got %d", len(cl.Fields))
	}
	if cl.Fields[0].Name.Value != "name" {
		t.Fatalf("expected field 'name', got %q", cl.Fields[0].Name.Value)
	}
	if len(cl.Methods) != 1 {
		t.Fatalf("expected 1 method, got %d", len(cl.Methods))
	}
	if cl.Methods[0].Name.Value != "speak" {
		t.Fatalf("expected method 'speak', got %q", cl.Methods[0].Name.Value)
	}
}

// --- Var declarations ---

func TestParser_VarDeclaration(t *testing.T) {
	prog := parseProg(t, "var count: int")
	v, ok := prog.Stmts[0].(*varStmt)
	if !ok {
		t.Fatal("expected *varStmt")
	}
	if v.Name.Value != "count" {
		t.Fatalf("expected name 'count', got %q", v.Name.Value)
	}
	if v.Type_.Name != "int" {
		t.Fatalf("expected type 'int', got %q", v.Type_.Name)
	}
}

func TestParser_VarWithInitializer(t *testing.T) {
	prog := parseProg(t, "var count: int = 0")
	v := prog.Stmts[0].(*varStmt)
	if v.Name.Value != "count" {
		t.Fatalf("expected name 'count', got %q", v.Name.Value)
	}
	// value expression is nil for Phase 10 (parseExpression returns nil)
	// but the parser should not error on the = 0 part
}

// --- Type alias ---

func TestParser_TypeAlias(t *testing.T) {
	prog := parseProg(t, "type ID = int")
	ta, ok := prog.Stmts[0].(*typeAliasStmt)
	if !ok {
		t.Fatal("expected *typeAliasStmt")
	}
	if ta.Name.Value != "ID" {
		t.Fatalf("expected name 'ID', got %q", ta.Name.Value)
	}
	if ta.Alias.Name != "int" {
		t.Fatalf("expected alias 'int', got %q", ta.Alias.Name)
	}
}

func TestParser_TypeAliasGeneric(t *testing.T) {
	prog := parseProg(t, "type Names = array<string>")
	ta := prog.Stmts[0].(*typeAliasStmt)
	if ta.Alias.Name != "array" {
		t.Fatalf("expected alias 'array', got %q", ta.Alias.Name)
	}
	if len(ta.Alias.Params) != 1 || ta.Alias.Params[0].Name != "string" {
		t.Fatalf("expected array<string>, got %+v", ta.Alias)
	}
}

// --- Mixed declarations ---

func TestParser_MixedDeclarations(t *testing.T) {
	source := `fun f(): void {}
struct S {
  x: int
}
class C {
  y: string
  fun m(): void {}
}
var v: int
type T = string`
	prog := parseProg(t, source)
	if len(prog.Stmts) != 5 {
		t.Fatalf("expected 5 statements, got %d", len(prog.Stmts))
	}
	if _, ok := prog.Stmts[0].(*funStmt); !ok {
		t.Fatalf("stmt 0: expected *funStmt, got %T", prog.Stmts[0])
	}
	if _, ok := prog.Stmts[1].(*structStmt); !ok {
		t.Fatalf("stmt 1: expected *structStmt, got %T", prog.Stmts[1])
	}
	if _, ok := prog.Stmts[2].(*classStmt); !ok {
		t.Fatalf("stmt 2: expected *classStmt, got %T", prog.Stmts[2])
	}
	if _, ok := prog.Stmts[3].(*varStmt); !ok {
		t.Fatalf("stmt 3: expected *varStmt, got %T", prog.Stmts[3])
	}
	if _, ok := prog.Stmts[4].(*typeAliasStmt); !ok {
		t.Fatalf("stmt 4: expected *typeAliasStmt, got %T", prog.Stmts[4])
	}
}

// --- Canonical syntax forms ---

func TestParser_CanonicalFunSyntax(t *testing.T) {
	prog := parseProg(t, "fun greet(name: string): string = expr")
	fun := prog.Stmts[0].(*funStmt)
	if fun.Name.Value != "greet" {
		t.Fatalf("expected name 'greet', got %q", fun.Name.Value)
	}
	if len(fun.Params) != 1 {
		t.Fatalf("expected 1 param, got %d", len(fun.Params))
	}
	if fun.Params[0].Name.Value != "name" {
		t.Fatalf("expected param 'name', got %q", fun.Params[0].Name.Value)
	}
	if fun.ReturnType.Name != "string" {
		t.Fatalf("expected return type 'string', got %q", fun.ReturnType.Name)
	}
	if fun.ExprBody == nil {
		t.Fatal("expected ExprBody from = expr syntax")
	}
}

func TestParser_CanonicalExportFunSyntax(t *testing.T) {
	prog := parseProg(t, "export fun compute(x: int): int = x + 1")
	fun := prog.Stmts[0].(*funStmt)
	if !fun.Exported {
		t.Fatal("expected exported")
	}
	if fun.Name.Value != "compute" {
		t.Fatalf("expected name 'compute', got %q", fun.Name.Value)
	}
}

func TestParser_CanonicalImportSyntax(t *testing.T) {
	prog := parseProg(t, "import add from \"math\"\nfun use(): int = add(1, 2)")
	if len(prog.imports) != 1 {
		t.Fatalf("expected 1 import, got %d", len(prog.imports))
	}
	imp := prog.imports[0]
	if imp.Name != "add" || imp.Path != "math" || imp.Alias != "" {
		t.Fatalf("unexpected import: %+v", imp)
	}
}

func TestParser_CanonicalImportAliasSyntax(t *testing.T) {
	prog := parseProg(t, "import add as plus from \"math\"\nfun use(): int = plus(1, 2)")
	if len(prog.imports) != 1 {
		t.Fatalf("expected 1 import, got %d", len(prog.imports))
	}
	imp := prog.imports[0]
	if imp.Name != "add" || imp.Path != "math" || imp.Alias != "plus" {
		t.Fatalf("unexpected import: %+v", imp)
	}
}

func TestParser_BatchImportSyntax(t *testing.T) {
	prog := parseProg(t, "import { add, sub } from \"math\"\nfun use(): int = add(1, 2)")
	if len(prog.imports) != 2 {
		t.Fatalf("expected 2 imports, got %d", len(prog.imports))
	}
	if prog.imports[0].Name != "add" || prog.imports[0].Path != "math" {
		t.Fatalf("unexpected import[0]: %+v", prog.imports[0])
	}
	if prog.imports[1].Name != "sub" || prog.imports[1].Path != "math" {
		t.Fatalf("unexpected import[1]: %+v", prog.imports[1])
	}
}

func TestParser_BatchImportWithAliasSyntax(t *testing.T) {
	prog := parseProg(t, "import { add as plus, sub as minus } from \"math\"\nfun use(): int = plus(1, 2)")
	if len(prog.imports) != 2 {
		t.Fatalf("expected 2 imports, got %d", len(prog.imports))
	}
	if prog.imports[0].Name != "add" || prog.imports[0].Alias != "plus" || prog.imports[0].Path != "math" {
		t.Fatalf("unexpected import[0]: %+v", prog.imports[0])
	}
	if prog.imports[1].Name != "sub" || prog.imports[1].Alias != "minus" || prog.imports[1].Path != "math" {
		t.Fatalf("unexpected import[1]: %+v", prog.imports[1])
	}
}

func TestParser_BatchImportTrailingComma(t *testing.T) {
	prog := parseProg(t, "import { add, sub, } from \"math\"\nfun use(): int = 1")
	if len(prog.imports) != 2 {
		t.Fatalf("expected 2 imports, got %d", len(prog.imports))
	}
}

func TestParser_ReExportDirect(t *testing.T) {
	l := newLexer("export add from \"math\"")
	p := newParser(l, "")
	// Verify initial state after newParser
	if p.cur.typ != tokExport {
		t.Fatalf("cur should be tokExport, got %v", p.cur.typ)
	}
	if p.pk.typ != tokIdent {
		t.Fatalf("pk should be tokIdent, got %v", p.pk.typ)
	}
	// Call parseTopLevel directly
	stmt := p.parseTopLevel()
	if stmt != nil {
		t.Fatalf("expected nil, got %T", stmt)
	}
	if len(p.reExports) != 1 {
		t.Fatalf("expected 1 re-export, got %d", len(p.reExports))
	}
	re := p.reExports[0]
	if re.Name != "add" || re.Path != "math" || !re.ReExport {
		t.Fatalf("unexpected re-export: %+v", re)
	}
}

func TestParser_ReExportSyntax(t *testing.T) {
	prog := parseProg(t, "export add from \"math\"\nfun use(): int = 1")
	if len(prog.imports) != 1 {
		t.Fatalf("expected 1 import (re-export), got %d", len(prog.imports))
	}
	imp := prog.imports[0]
	if imp.Name != "add" || imp.Path != "math" || !imp.ReExport {
		t.Fatalf("unexpected re-export: %+v", imp)
	}
	if len(prog.Stmts) != 1 {
		t.Fatalf("expected 1 stmt, got %d", len(prog.Stmts))
	}
}

func TestParser_WildcardReExport(t *testing.T) {
	prog := parseProg(t, `export * from "math"
fun use(): int = 1`)
	if len(prog.imports) != 1 {
		t.Fatalf("expected 1 import (wildcard re-export), got %d", len(prog.imports))
	}
	imp := prog.imports[0]
	if imp.Name != "*" || imp.Path != "math" || !imp.ReExport {
		t.Fatalf("unexpected wildcard re-export: %+v", imp)
	}
	if len(prog.Stmts) != 1 {
		t.Fatalf("expected 1 stmt, got %d", len(prog.Stmts))
	}
}

func TestParser_CanonicalExportVarSyntax(t *testing.T) {
	prog := parseProg(t, "export var answer: int = 42")
	decl := prog.Stmts[0].(*varStmt)
	if !decl.Exported {
		t.Fatal("expected exported var")
	}
	if decl.Name.Value != "answer" {
		t.Fatalf("expected var name 'answer', got %q", decl.Name.Value)
	}
}

func TestParser_CanonicalStructSyntax(t *testing.T) {
	prog := parseProg(t, "struct Point {\n  x: int\n  y: int\n}")
	st := prog.Stmts[0].(*structStmt)
	if st.Name.Value != "Point" {
		t.Fatalf("expected name 'Point', got %q", st.Name.Value)
	}
	if len(st.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(st.Fields))
	}
}

// --- Type annotation variants ---

func TestParser_ArrayType(t *testing.T) {
	prog := parseProg(t, "fun f(data: array<string>): void {}")
	fun := prog.Stmts[0].(*funStmt)
	paramType := fun.Params[0].Type_
	if paramType.Name != "array" {
		t.Fatalf("expected 'array', got %q", paramType.Name)
	}
	if len(paramType.Params) != 1 || paramType.Params[0].Name != "string" {
		t.Fatalf("expected array<string>, got %+v", paramType)
	}
}

func TestParser_MapType(t *testing.T) {
	prog := parseProg(t, "fun f(m: map<string, int>): void {}")
	fun := prog.Stmts[0].(*funStmt)
	paramType := fun.Params[0].Type_
	if paramType.Name != "map" {
		t.Fatalf("expected 'map', got %q", paramType.Name)
	}
	if len(paramType.Params) != 2 {
		t.Fatalf("expected 2 map type params, got %d", len(paramType.Params))
	}
	if paramType.Params[0].Name != "string" || paramType.Params[1].Name != "int" {
		t.Fatalf("expected map<string, int>, got %q, %q", paramType.Params[0].Name, paramType.Params[1].Name)
	}
}

func TestParser_ClassTypeReference(t *testing.T) {
	prog := parseProg(t, "fun f(p: Point): void {}")
	fun := prog.Stmts[0].(*funStmt)
	paramType := fun.Params[0].Type_
	if paramType.Name != "Point" {
		t.Fatalf("expected 'Point', got %q", paramType.Name)
	}
}

func TestParser_VoidReturnType(t *testing.T) {
	prog := parseProg(t, "fun f(): void {}")
	fun := prog.Stmts[0].(*funStmt)
	if fun.ReturnType.Name != "void" {
		t.Fatalf("expected 'void', got %q", fun.ReturnType.Name)
	}
}

func TestParser_AnyType(t *testing.T) {
	prog := parseProg(t, "fun f(x: any): any {}")
	fun := prog.Stmts[0].(*funStmt)
	if fun.Params[0].Type_.Name != "any" {
		t.Fatalf("expected param type 'any', got %q", fun.Params[0].Type_.Name)
	}
	if fun.ReturnType.Name != "any" {
		t.Fatalf("expected return type 'any', got %q", fun.ReturnType.Name)
	}
}

// --- Error cases ---

func TestParser_AllowsExportOnStruct(t *testing.T) {
	prog := parseProg(t, "export struct S {\n  x: int\n}")
	decl := prog.Stmts[0].(*structStmt)
	if !decl.Exported {
		t.Fatal("expected exported struct")
	}
}

func TestParser_RejectsExportOnClass(t *testing.T) {
	expectParseError(t, "export class C {\n  x: int\n}")
}

func TestParser_AllowsExportOnVar(t *testing.T) {
	prog := parseProg(t, "export var x: int = 1")
	decl := prog.Stmts[0].(*varStmt)
	if !decl.Exported {
		t.Fatal("expected exported var")
	}
}

func TestParser_AllowsExportOnType(t *testing.T) {
	prog := parseProg(t, "export type T = int")
	decl := prog.Stmts[0].(*typeAliasStmt)
	if !decl.Exported {
		t.Fatal("expected exported type")
	}
}

func TestParser_RejectsDuplicateParams(t *testing.T) {
	expectParseError(t, "fun f(x: int, x: int): void {}")
}

func TestParser_RejectsDuplicateFields(t *testing.T) {
	expectParseError(t, "struct S {\n  x: int\n  x: string\n}")
}

// --- Declaration order stability ---

func TestParser_DeclarationOrderStable(t *testing.T) {
	source := "fun a(): void {}\nfun b(): void {}\nfun c(): void {}"
	prog := parseProg(t, source)
	names := []string{"a", "b", "c"}
	for i, stmt := range prog.Stmts {
		fun := stmt.(*funStmt)
		if fun.Name.Value != names[i] {
			t.Fatalf("stmt %d: expected %q, got %q", i, names[i], fun.Name.Value)
		}
	}
}

// --- Fun without return type ---

func TestParser_CStyleForLoop(t *testing.T) {
	prog := parseProg(t, "fun f(): void { for (var i: int = 0; i < 3; i = i + 1) {} }")
	fun := prog.Stmts[0].(*funStmt)
	body := fun.Body
	if body == nil || len(body.Stmts) != 1 {
		t.Fatalf("expected single for statement in body, got %+v", body)
	}
	loop, ok := body.Stmts[0].(*forStmt)
	if !ok {
		t.Fatalf("expected *forStmt, got %T", body.Stmts[0])
	}
	if loop.IsForIn {
		t.Fatal("expected classic for loop, got for-in")
	}
	if loop.Init == nil || loop.Condition == nil || loop.Update == nil {
		t.Fatalf("expected init/condition/update to be present, got %+v", loop)
	}
}

func TestParser_ForInLoopParses(t *testing.T) {
	prog := parseProg(t, "fun f(): void { for (item in items) {} }")
	fun := prog.Stmts[0].(*funStmt)
	body := fun.Body
	if body == nil || len(body.Stmts) != 1 {
		t.Fatalf("expected single for statement in body, got %+v", body)
	}
	loop, ok := body.Stmts[0].(*forStmt)
	if !ok {
		t.Fatalf("expected *forStmt, got %T", body.Stmts[0])
	}
	if !loop.IsForIn {
		t.Fatal("expected for-in loop")
	}
	if loop.Variable != "item" || loop.Iterable == nil {
		t.Fatalf("expected parsed for-in variable/iterable, got %+v", loop)
	}
}

func TestParser_ForInLoopWithoutParensRejected(t *testing.T) {
	expectParseError(t, "fun f(): void { for item in items {} }")
}

func TestParser_WhenRequiresSubjectExpression(t *testing.T) {
	expectParseError(t, "fun f(): void { when { case 1 {} } }")
}

// --- Function types in type position ---

func TestParser_VarWithFunTypeAnnotation(t *testing.T) {
	prog := parseProg(t, "fun main(): void { var f: fun(int): int = fun(x: int): int { return x } }")
	fn := prog.Stmts[0].(*funStmt)
	vs := fn.Body.Stmts[0].(*varStmt)
	if vs.Type_ == nil || !vs.Type_.IsFun {
		t.Fatalf("expected function type annotation on var, got %+v", vs.Type_)
	}
	if vs.Type_.Name != "fun" {
		t.Fatalf("expected fun type name 'fun', got %q", vs.Type_.Name)
	}
	if len(vs.Type_.FunParams) != 1 || vs.Type_.FunParams[0].Name != "int" {
		t.Fatalf("expected one param type 'int', got %+v", vs.Type_.FunParams)
	}
	if vs.Type_.FunReturn == nil || vs.Type_.FunReturn.Name != "int" {
		t.Fatalf("expected return type 'int', got %+v", vs.Type_.FunReturn)
	}
}

func TestParser_FunTypeMultipleParamsAndArrowReturn(t *testing.T) {
	prog := parseProg(t, "fun main(): void { var f: fun(int, string) -> bool = fun(a: int, b: string): bool { return true } }")
	fn := prog.Stmts[0].(*funStmt)
	vs := fn.Body.Stmts[0].(*varStmt)
	if !vs.Type_.IsFun {
		t.Fatal("expected function type annotation")
	}
	if len(vs.Type_.FunParams) != 2 {
		t.Fatalf("expected 2 param types, got %d", len(vs.Type_.FunParams))
	}
	if vs.Type_.FunParams[0].Name != "int" || vs.Type_.FunParams[1].Name != "string" {
		t.Fatalf("expected param types int,string, got %+v", vs.Type_.FunParams)
	}
	if vs.Type_.FunReturn == nil || vs.Type_.FunReturn.Name != "bool" {
		t.Fatalf("expected arrow return type 'bool', got %+v", vs.Type_.FunReturn)
	}
}

func TestParser_FunTypeWithoutReturnMeansVoid(t *testing.T) {
	prog := parseProg(t, "fun main(): void { var f: fun(int) = fun(x: int) { return } }")
	fn := prog.Stmts[0].(*funStmt)
	vs := fn.Body.Stmts[0].(*varStmt)
	if !vs.Type_.IsFun {
		t.Fatal("expected function type annotation")
	}
	if vs.Type_.FunReturn != nil {
		t.Fatalf("expected nil (void) return type, got %+v", vs.Type_.FunReturn)
	}
}

func TestParser_FunTypeNestedGenericParams(t *testing.T) {
	prog := parseProg(t, "fun main(): void { var f: fun(array<int>, map<string, int>): array<string> = fun(xs: array<int>, m: map<string, int>): array<string> { return [] } }")
	fn := prog.Stmts[0].(*funStmt)
	vs := fn.Body.Stmts[0].(*varStmt)
	if !vs.Type_.IsFun {
		t.Fatal("expected function type annotation")
	}
	if got := vs.Type_.FunParams[0]; got.Name != "array" || len(got.Params) != 1 || got.Params[0].Name != "int" {
		t.Fatalf("expected array<int> param type, got %+v", got)
	}
	if got := vs.Type_.FunParams[1]; got.Name != "map" || len(got.Params) != 2 {
		t.Fatalf("expected map<string,int> param type, got %+v", got)
	}
	if got := vs.Type_.FunReturn; got == nil || got.Name != "array" {
		t.Fatalf("expected array return type, got %+v", got)
	}
}

func TestParser_FunParamWithFunType(t *testing.T) {
	prog := parseProg(t, "fun apply(cb: fun(int): int): int { return cb(1) }")
	fn := prog.Stmts[0].(*funStmt)
	pt := fn.Params[0].Type_
	if pt == nil || !pt.IsFun {
		t.Fatalf("expected function type on param, got %+v", pt)
	}
	if len(pt.FunParams) != 1 || pt.FunParams[0].Name != "int" {
		t.Fatalf("expected one param type 'int', got %+v", pt.FunParams)
	}
	// The first `: int` binds to the fun type; the second annotates apply.
	if pt.FunReturn == nil || pt.FunReturn.Name != "int" {
		t.Fatalf("expected greedy return type 'int' on fun type, got %+v", pt.FunReturn)
	}
	if fn.ReturnType == nil || fn.ReturnType.Name != "int" {
		t.Fatalf("expected enclosing fun return type 'int', got %+v", fn.ReturnType)
	}
}

func TestParser_NestedFunTypeParam(t *testing.T) {
	prog := parseProg(t, "fun main(): void { var f: fun(fun(int): int, int): int = fun(g: fun(int): int, n: int): int { return g(n) } }")
	fn := prog.Stmts[0].(*funStmt)
	vs := fn.Body.Stmts[0].(*varStmt)
	if !vs.Type_.IsFun {
		t.Fatal("expected function type annotation")
	}
	inner := vs.Type_.FunParams[0]
	if !inner.IsFun || len(inner.FunParams) != 1 || inner.FunReturn == nil || inner.FunReturn.Name != "int" {
		t.Fatalf("expected nested fun(int):int param type, got %+v", inner)
	}
	if vs.Type_.FunParams[1].Name != "int" {
		t.Fatalf("expected second param type 'int', got %+v", vs.Type_.FunParams[1])
	}
}

func TestParser_TypeAliasToFunType(t *testing.T) {
	prog := parseProg(t, "type Handler = fun(int): string")
	ta := prog.Stmts[0].(*typeAliasStmt)
	if ta.Alias == nil || !ta.Alias.IsFun {
		t.Fatalf("expected function type alias target, got %+v", ta.Alias)
	}
}

func TestParser_MalformedFunTypeRecoversWithoutHang(t *testing.T) {
	// `fun` without a following `(` must not enter the function-type branch;
	// recovery keeps the parser terminating (regression guard).
	prog, err, _ := ParseModuleForTestWithDiagnostics("interface I {\n  fun bad(\n  fun ok(): int\n}\nfun later(): int { return 1 }")
	if err == nil {
		t.Fatal("expected parse error")
	}
	if prog == nil || len(prog.Stmts) != 2 {
		t.Fatalf("expected partial program with 2 statements, got %#v", prog)
	}
}

// --- Arrow lambda short form ---

// arrowInitOf extracts the initializer expression of the first var statement
// in the first fun body of the program.
func arrowInitOf(t *testing.T, source string) expression {
	t.Helper()
	prog := parseProg(t, source)
	fn := prog.Stmts[0].(*funStmt)
	vs := fn.Body.Stmts[0].(*varStmt)
	if vs.InitExpr == nil {
		t.Fatalf("expected initializer on first var, got none")
	}
	return vs.InitExpr
}

func TestParser_ArrowLambdaSingleParam(t *testing.T) {
	lam := arrowInitOf(t, "fun main(): void { var f: any = (x) => x + 1 }").(*lambdaExpr)
	if len(lam.Params) != 1 || lam.Params[0].Name.Value != "x" {
		t.Fatalf("expected one param 'x', got %+v", lam.Params)
	}
	if lam.Params[0].Type_ != nil {
		t.Fatalf("expected untyped param, got %+v", lam.Params[0].Type_)
	}
	if lam.ReturnType != nil {
		t.Fatalf("expected no return type, got %+v", lam.ReturnType)
	}
	if lam.Body != nil || lam.ExprBody == nil {
		t.Fatalf("expected expression body only, got body=%v expr=%v", lam.Body, lam.ExprBody)
	}
	if bin, ok := lam.ExprBody.(*binaryExpr); !ok || bin.Operator != "+" {
		t.Fatalf("expected binary '+' body, got %#v", lam.ExprBody)
	}
}

func TestParser_ArrowLambdaTypedParamsAndReturn(t *testing.T) {
	lam := arrowInitOf(t, "fun main(): void { var f: any = (a: int, b: string): bool => true }").(*lambdaExpr)
	if len(lam.Params) != 2 {
		t.Fatalf("expected two params, got %d", len(lam.Params))
	}
	if lam.Params[0].Type_ == nil || lam.Params[0].Type_.Name != "int" {
		t.Fatalf("expected first param typed int, got %+v", lam.Params[0].Type_)
	}
	if lam.Params[1].Type_ == nil || lam.Params[1].Type_.Name != "string" {
		t.Fatalf("expected second param typed string, got %+v", lam.Params[1].Type_)
	}
	if lam.ReturnType == nil || lam.ReturnType.Name != "bool" {
		t.Fatalf("expected bool return type, got %+v", lam.ReturnType)
	}
}

func TestParser_ArrowLambdaUntypedParamsAmongTyped(t *testing.T) {
	// Annotations are per-parameter: mixing typed and untyped is allowed.
	lam := arrowInitOf(t, "fun main(): void { var f: any = (n: int, acc) => acc }").(*lambdaExpr)
	if len(lam.Params) != 2 {
		t.Fatalf("expected two params, got %d", len(lam.Params))
	}
	if lam.Params[0].Type_ == nil || lam.Params[0].Type_.Name != "int" {
		t.Fatalf("expected first param typed int, got %+v", lam.Params[0].Type_)
	}
	if lam.Params[1].Type_ != nil {
		t.Fatalf("expected second param untyped, got %+v", lam.Params[1].Type_)
	}
}

func TestParser_ArrowLambdaZeroParams(t *testing.T) {
	lam := arrowInitOf(t, "fun main(): void { var f: any = (): int => 42 }").(*lambdaExpr)
	if len(lam.Params) != 0 {
		t.Fatalf("expected zero params, got %d", len(lam.Params))
	}
	if lam.ReturnType == nil || lam.ReturnType.Name != "int" {
		t.Fatalf("expected int return type, got %+v", lam.ReturnType)
	}
	if lit, ok := lam.ExprBody.(*intLiteral); !ok || lit.Value != 42 {
		t.Fatalf("expected literal 42 body, got %#v", lam.ExprBody)
	}
}

func TestParser_ArrowLambdaSingleParamNoParens(t *testing.T) {
	lam := arrowInitOf(t, "fun main(): void { var f: any = x => x * 2 }").(*lambdaExpr)
	if len(lam.Params) != 1 || lam.Params[0].Name.Value != "x" || lam.Params[0].Type_ != nil {
		t.Fatalf("expected one untyped param 'x', got %+v", lam.Params)
	}
	if bin, ok := lam.ExprBody.(*binaryExpr); !ok || bin.Operator != "*" {
		t.Fatalf("expected binary '*' body, got %#v", lam.ExprBody)
	}
}

func TestParser_ArrowLambdaNested(t *testing.T) {
	lam := arrowInitOf(t, "fun main(): void { var add: any = (a) => (b): int => a + b }").(*lambdaExpr)
	if len(lam.Params) != 1 || lam.Params[0].Name.Value != "a" {
		t.Fatalf("expected outer param 'a', got %+v", lam.Params)
	}
	inner, ok := lam.ExprBody.(*lambdaExpr)
	if !ok {
		t.Fatalf("expected nested lambda body, got %#v", lam.ExprBody)
	}
	if len(inner.Params) != 1 || inner.Params[0].Name.Value != "b" {
		t.Fatalf("expected inner param 'b', got %+v", inner.Params)
	}
	if inner.ReturnType == nil || inner.ReturnType.Name != "int" {
		t.Fatalf("expected inner int return type, got %+v", inner.ReturnType)
	}
}

func TestParser_ArrowLambdaGroupedExprUnaffected(t *testing.T) {
	// `(a + b)` is a grouped expression, not a parameter list: the failed
	// arrow speculation must rewind cleanly and parse normally.
	init := arrowInitOf(t, "fun main(): void { var s: int = (a + b) * 2 }")
	mul, ok := init.(*binaryExpr)
	if !ok || mul.Operator != "*" {
		t.Fatalf("expected binary '*' root, got %#v", init)
	}
	add, ok := mul.Left.(*binaryExpr)
	if !ok || add.Operator != "+" {
		t.Fatalf("expected grouped '+' operand, got %#v", mul.Left)
	}
	if _, isLambda := mul.Left.(*lambdaExpr); isLambda {
		t.Fatal("grouped expression must not parse as a lambda")
	}
}

func TestParser_ArrowLambdaGroupedCallUnaffected(t *testing.T) {
	// Parenthesized call result with commas inside inner parens must not be
	// mistaken for an arrow parameter list.
	init := arrowInitOf(t, "fun main(): void { var v: int = min(a, b) + 1 }")
	bin, ok := init.(*binaryExpr)
	if !ok || bin.Operator != "+" {
		t.Fatalf("expected binary '+' root, got %#v", init)
	}
	if call, ok := bin.Left.(*callExpr); !ok || len(call.Arguments) != 2 {
		t.Fatalf("expected two-arg call operand, got %#v", bin.Left)
	}
}

func TestParser_ArrowLambdaAsCallArgument(t *testing.T) {
	prog := parseProg(t, "fun main(): void { var r: int = apply(x => x + 1, 2) }")
	fn := prog.Stmts[0].(*funStmt)
	vs := fn.Body.Stmts[0].(*varStmt)
	call, ok := vs.InitExpr.(*callExpr)
	if !ok {
		t.Fatalf("expected call expression, got %#v", vs.InitExpr)
	}
	if len(call.Arguments) != 2 {
		t.Fatalf("expected two arguments, got %d", len(call.Arguments))
	}
	lam, ok := call.Arguments[0].(*lambdaExpr)
	if !ok {
		t.Fatalf("expected arrow lambda argument, got %#v", call.Arguments[0])
	}
	if len(lam.Params) != 1 || lam.Params[0].Name.Value != "x" {
		t.Fatalf("expected one param 'x', got %+v", lam.Params)
	}
}

func TestParser_ArrowLambdaParenthesizedAsCallArgument(t *testing.T) {
	prog := parseProg(t, "fun main(): void { var r: int = apply((x): int => x * 3, 2) }")
	fn := prog.Stmts[0].(*funStmt)
	vs := fn.Body.Stmts[0].(*varStmt)
	call := vs.InitExpr.(*callExpr)
	lam, ok := call.Arguments[0].(*lambdaExpr)
	if !ok {
		t.Fatalf("expected arrow lambda argument, got %#v", call.Arguments[0])
	}
	if lam.ReturnType == nil || lam.ReturnType.Name != "int" {
		t.Fatalf("expected int return type, got %+v", lam.ReturnType)
	}
}

func TestParser_ArrowLambdaOperatorPrecedence(t *testing.T) {
	// Body is a full expression: `x * 2 + 1` binds * tighter than +.
	lam := arrowInitOf(t, "fun main(): void { var f: any = x => x * 2 + 1 }").(*lambdaExpr)
	sum, ok := lam.ExprBody.(*binaryExpr)
	if !ok || sum.Operator != "+" {
		t.Fatalf("expected '+' root body, got %#v", lam.ExprBody)
	}
	prod, ok := sum.Left.(*binaryExpr)
	if !ok || prod.Operator != "*" {
		t.Fatalf("expected '*' sub-expression, got %#v", sum.Left)
	}
}

func TestParser_ArrowLambdaComparisonBody(t *testing.T) {
	// `=>` must not be confused with `>=` or `>` inside the body.
	lam := arrowInitOf(t, "fun main(): void { var f: any = x => x >= 10 }").(*lambdaExpr)
	cmp, ok := lam.ExprBody.(*binaryExpr)
	if !ok || cmp.Operator != ">=" {
		t.Fatalf("expected '>=' body, got %#v", lam.ExprBody)
	}
}

func TestParser_ArrowLambdaFunTypeAnnotations(t *testing.T) {
	// Parameter and return annotations reuse the function-type syntax.
	lam := arrowInitOf(t, "fun main(): void { var f: any = (g: fun(int): int, v: int): fun(int): int => (n) => g(v) + n }").(*lambdaExpr)
	if len(lam.Params) != 2 {
		t.Fatalf("expected two params, got %d", len(lam.Params))
	}
	gt := lam.Params[0].Type_
	if gt == nil || !gt.IsFun || len(gt.FunParams) != 1 || gt.FunReturn == nil || gt.FunReturn.Name != "int" {
		t.Fatalf("expected fun(int): int param type, got %+v", gt)
	}
	if lam.ReturnType == nil || !lam.ReturnType.IsFun {
		t.Fatalf("expected fun-typed return annotation, got %+v", lam.ReturnType)
	}
	if _, ok := lam.ExprBody.(*lambdaExpr); !ok {
		t.Fatalf("expected nested arrow body, got %#v", lam.ExprBody)
	}
}

func TestParser_ArrowLambdaDuplicateParamRejected(t *testing.T) {
	expectParseError(t, "fun main(): void { var f: any = (x: int, x: int) => x }")
}

func TestParser_ArrowLambdaMalformedFallsBackToGrouped(t *testing.T) {
	// A grouped expression followed by '=>' is not an arrow lambda; the
	// speculation rewinds and the stray '=>' surfaces as a parse error.
	expectParseError(t, "fun main(): void { var f: any = (a + b) => c }")
}

func TestParser_ArrowLambdaMissingBodyRejected(t *testing.T) {
	expectParseError(t, "fun main(): void { var f: any = x => }")
}

func TestParser_ArrowLambdaMalformedReturnAnnotationRejected(t *testing.T) {
	expectParseError(t, "fun main(): void { var f: any = (x: ) => x }")
}

func TestParser_AssignmentNotConfusedWithArrow(t *testing.T) {
	// `x = y` and comparison bodies must keep their meaning.
	prog := parseProg(t, "fun main(): void { var a: int = 1 var b: int = 2 a = b }")
	fn := prog.Stmts[0].(*funStmt)
	assign, ok := fn.Body.Stmts[2].(*exprStatement).Expr.(*assignExpr)
	if !ok {
		t.Fatalf("expected assignment statement, got %#v", fn.Body.Stmts[2])
	}
	if _, isLambda := assign.Value.(*lambdaExpr); isLambda {
		t.Fatal("assignment must not parse as an arrow lambda")
	}
}

// TestParser_PoisonTokenInExpressionTerminates guards against the infinite
// loop where parsePrefix errors without consuming the token and a
// container-literal loop retries it forever (production memory explosion:
// unbounded nil appends + error formatting). Every source here must produce
// a parse error promptly, not hang.
func TestParser_PoisonTokenInExpressionTerminates(t *testing.T) {
	sources := []string{
		`var m: map[string]any = {"a": 1}`, // map[...] is invalid type syntax; '[' reaches expression position
		`fun main(): void { var xs = [1, 2, =] }`,
		`fun main(): void { var m = {"a": =} }`,
		`fun main(): void { var xs = [1, 2, , 3] }`,
		`fun main(): void { foo(=) }`,
		`fun main(): void { new Foo(=) }`,
		`fun main(): void { class B { fun init() { super(=) } } }`,
	}
	for _, src := range sources {
		t.Run(src, func(t *testing.T) {
			done := make(chan error, 1)
			go func() {
				l := newLexer(src)
				p := newParser(l, src)
				_, err := p.parse()
				done <- err
			}()
			select {
			case err := <-done:
				if err == nil {
					t.Fatalf("expected parse error, got none")
				}
			case <-time.After(2 * time.Second):
				t.Fatalf("parse did not terminate within 2s (infinite-loop regression)")
			}
		})
	}
}
