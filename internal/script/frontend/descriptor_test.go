package frontend

import (
	"strings"
	"testing"

	"github.com/qomos-w/spore/diagnostics"
	"github.com/qomos-w/spore/schema"
)

// --- Descriptor extraction ---

func TestDescriptor_FunToCallableDesc(t *testing.T) {
	prog := parseProg(t, "fun greet(name: string): void {}")
	fun := prog.Stmts[0].(*funStmt)
	ctx := typeContextFromProg(prog)
	desc, err := extractCallableDesc(fun, ctx)
	if err != nil {
		t.Fatalf("extractCallableDesc: %v", err)
	}
	if desc.Name != "greet" {
		t.Fatalf("expected name 'greet', got %q", desc.Name)
	}
	if desc.Mode != schema.CallableModeUnary {
		t.Fatalf("expected unary mode, got %q", desc.Mode)
	}
	if len(desc.Parameters) != 1 {
		t.Fatalf("expected 1 param, got %d", len(desc.Parameters))
	}
	if desc.Parameters[0].Name != "name" {
		t.Fatalf("expected param 'name', got %q", desc.Parameters[0].Name)
	}
	if desc.Parameters[0].Type.Kind != schema.TypeKindScalar || desc.Parameters[0].Type.Name != "string" {
		t.Fatalf("expected scalar string, got %+v", desc.Parameters[0].Type)
	}
	if len(desc.Returns) != 1 || desc.Returns[0].Kind != schema.TypeKindVoid {
		t.Fatalf("expected void return, got %+v", desc.Returns)
	}
}

func TestDescriptor_ExportFunToCallableDesc(t *testing.T) {
	prog := parseProg(t, "export fun compute(x: int): int {}")
	fun := prog.Stmts[0].(*funStmt)
	ctx := typeContextFromProg(prog)
	desc, err := extractCallableDesc(fun, ctx)
	if err != nil {
		t.Fatalf("extractCallableDesc: %v", err)
	}
	if desc.Name != "compute" {
		t.Fatalf("expected name 'compute', got %q", desc.Name)
	}
	if len(desc.Returns) != 1 || desc.Returns[0].Kind != schema.TypeKindScalar || desc.Returns[0].Name != "int" {
		t.Fatalf("expected int return, got %+v", desc.Returns)
	}
}

func TestDescriptor_FunWithArrayParams(t *testing.T) {
	prog := parseProg(t, "fun f(data: array<string>): void {}")
	fun := prog.Stmts[0].(*funStmt)
	ctx := typeContextFromProg(prog)
	desc, err := extractCallableDesc(fun, ctx)
	if err != nil {
		t.Fatalf("extractCallableDesc: %v", err)
	}
	pt := desc.Parameters[0].Type
	if pt.Kind != schema.TypeKindArray {
		t.Fatalf("expected array kind, got %q", pt.Kind)
	}
	if pt.Element == nil || pt.Element.Kind != schema.TypeKindScalar || pt.Element.Name != "string" {
		t.Fatalf("expected array<string> element, got %+v", pt)
	}
}

func TestDescriptor_FunWithMapParams(t *testing.T) {
	prog := parseProg(t, "fun f(m: map<string, int>): void {}")
	fun := prog.Stmts[0].(*funStmt)
	ctx := typeContextFromProg(prog)
	desc, err := extractCallableDesc(fun, ctx)
	if err != nil {
		t.Fatalf("extractCallableDesc: %v", err)
	}
	pt := desc.Parameters[0].Type
	if pt.Kind != schema.TypeKindMap {
		t.Fatalf("expected map kind, got %q", pt.Kind)
	}
	if pt.Key == nil || pt.Key.Name != "string" {
		t.Fatalf("expected string key, got %+v", pt.Key)
	}
	if pt.Value == nil || pt.Value.Name != "int" {
		t.Fatalf("expected int value, got %+v", pt.Value)
	}
}

func TestDescriptor_FunWithClassRefType(t *testing.T) {
	prog := parseProg(t, "fun f(p: Point): void {}")
	fun := prog.Stmts[0].(*funStmt)
	ctx := typeContextFromProg(prog)
	desc, err := extractCallableDesc(fun, ctx)
	if err != nil {
		t.Fatalf("extractCallableDesc: %v", err)
	}
	pt := desc.Parameters[0].Type
	if pt.Kind != schema.TypeKindClass {
		t.Fatalf("expected class kind, got %q", pt.Kind)
	}
	if pt.ClassName != "Point" {
		t.Fatalf("expected class 'Point', got %q", pt.ClassName)
	}
}

func TestDescriptor_FunWithFunTypeParamSurfacesAsAny(t *testing.T) {
	// Function types are compile-time-only; the descriptor surface keeps
	// closures as scalar `any`.
	prog := parseProg(t, "fun apply(f: fun(int): int): int { return f(1) }")
	fun := prog.Stmts[0].(*funStmt)
	ctx := typeContextFromProg(prog)
	desc, err := extractCallableDesc(fun, ctx)
	if err != nil {
		t.Fatalf("extractCallableDesc: %v", err)
	}
	pt := desc.Parameters[0].Type
	if pt.Kind != schema.TypeKindScalar || pt.Name != "any" {
		t.Fatalf("expected scalar any for fun-typed param, got %+v", pt)
	}
}

func TestDescriptor_TypeAliasChainResolvesInCallableReturn(t *testing.T) {
	prog := parseProg(t, `
type Id = long
type UserId = Id
fun value(): UserId = 42`)
	fun := prog.Stmts[2].(*funStmt)
	ctx := typeContextFromProg(prog)

	desc, err := extractCallableDesc(fun, ctx)
	if err != nil {
		t.Fatalf("extractCallableDesc: %v", err)
	}
	if len(desc.Returns) != 1 {
		t.Fatalf("expected 1 return, got %d", len(desc.Returns))
	}
}

func TestDescriptor_StructDeclarationProducesStructObjectDesc(t *testing.T) {
	prog := parseProg(t, `
struct Point {
  x: int
  y: int
}`)
	ctx := typeContextFromProg(prog)

	desc := extractClassDesc(prog.Stmts[0], ctx)
	if desc.Name != "Point" {
		t.Fatalf("expected Point, got %q", desc.Name)
	}
	if desc.Kind != schema.TypeKindStruct {
		t.Fatalf("expected TypeKindStruct, got %q", desc.Kind)
	}
	if len(desc.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(desc.Fields))
	}
	if desc.Fields[0].Name != "x" || desc.Fields[0].Type.Name != "int" {
		t.Fatalf("expected field x:int, got %+v", desc.Fields[0])
	}
	if desc.Fields[1].Name != "y" || desc.Fields[1].Type.Name != "int" {
		t.Fatalf("expected field y:int, got %+v", desc.Fields[1])
	}
}

func TestDescriptor_SchemaAnnotationPropagates(t *testing.T) {
	prog := parseProg(t, `
@schema(3)
struct Event {
  id: string
  optional payload: string
}`)
	ctx := typeContextFromProg(prog)

	desc := extractClassDesc(prog.Stmts[0], ctx)
	if desc.SchemaID != 3 {
		t.Fatalf("expected SchemaID=3, got %d", desc.SchemaID)
	}
	if len(desc.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(desc.Fields))
	}
	if !desc.Fields[1].Optional {
		t.Error("expected payload to have Optional=true")
	}
}

func TestParser_SchemaAnnotationOnNonStructIsError(t *testing.T) {
	l := newLexer("@schema(3)\nclass NotAllowed { id: string }")
	p := newParser(l, "@schema(3)\nclass NotAllowed { id: string }")
	_, err := p.parse()
	if err == nil {
		t.Fatal("expected error for @schema on non-struct declaration")
	}
	if !strings.Contains(err.Error(), "must precede a struct declaration") {
		t.Fatalf("expected 'must precede a struct declaration' error, got %q", err.Error())
	}
}

func TestDescriptor_ComponentAnnotationPropagates(t *testing.T) {
	prog := parseProg(t, `
@component
struct Position {
  x: float
  y: float
}`)
	ctx := typeContextFromProg(prog)

	desc := extractClassDesc(prog.Stmts[0], ctx)
	if !desc.IsComponent {
		t.Fatal("expected IsComponent=true for @component struct")
	}
}

func TestDescriptor_SchemaAndComponentCombine(t *testing.T) {
	// Either order must work and both markers must propagate.
	for _, src := range []string{
		"@schema(7)\n@component\nstruct P { v: int }",
		"@component\n@schema(7)\nstruct P { v: int }",
	} {
		prog := parseProg(t, src)
		ctx := typeContextFromProg(prog)
		desc := extractClassDesc(prog.Stmts[0], ctx)
		if !desc.IsComponent {
			t.Fatalf("expected IsComponent=true for source %q", src)
		}
		if desc.SchemaID != 7 {
			t.Fatalf("expected SchemaID=7 for source %q, got %d", src, desc.SchemaID)
		}
	}
}

func TestDescriptor_PlainStructIsNotComponent(t *testing.T) {
	prog := parseProg(t, `
struct Plain {
  id: string
}`)
	ctx := typeContextFromProg(prog)

	desc := extractClassDesc(prog.Stmts[0], ctx)
	if desc.IsComponent {
		t.Fatal("plain struct must not be marked IsComponent")
	}
}

func TestParser_ComponentAnnotationOnNonStructIsError(t *testing.T) {
	l := newLexer("@component\nclass NotAllowed { id: string }")
	p := newParser(l, "@component\nclass NotAllowed { id: string }")
	_, err := p.parse()
	if err == nil {
		t.Fatal("expected error for @component on non-struct declaration")
	}
	if !strings.Contains(err.Error(), "must precede a struct declaration") {
		t.Fatalf("expected 'must precede a struct declaration' error, got %q", err.Error())
	}
}

func TestDescriptor_OptionalFieldPropagates(t *testing.T) {
	prog := parseProg(t, `
struct Person {
  name: string
  optional nickname: string
  optional age: int
}`)
	ctx := typeContextFromProg(prog)

	desc := extractClassDesc(prog.Stmts[0], ctx)
	if len(desc.Fields) != 3 {
		t.Fatalf("expected 3 fields, got %d", len(desc.Fields))
	}
	if desc.Fields[0].Optional {
		t.Error("expected name to have Optional=false")
	}
	if !desc.Fields[1].Optional {
		t.Error("expected nickname to have Optional=true")
	}
	if !desc.Fields[2].Optional {
		t.Error("expected age to have Optional=true")
	}
}

func TestDescriptor_OptionalClassFieldPropagates(t *testing.T) {
	prog := parseProg(t, `
class Animal {
  optional nickname: string
  age: int
}`)
	ctx := typeContextFromProg(prog)

	desc := extractClassDesc(prog.Stmts[0], ctx)
	if len(desc.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(desc.Fields))
	}
	if !desc.Fields[0].Optional {
		t.Error("expected nickname to have Optional=true")
	}
	if desc.Fields[1].Optional {
		t.Error("expected age to have Optional=false")
	}
}

func TestDescriptor_FunReturningStructRefPreservesTypeKind(t *testing.T) {
	prog := parseProg(t, `
struct Point {
  x: int
  y: int
}
fun make(): Point = Point{x: 1, y: 2}`)
	fun := prog.Stmts[1].(*funStmt)
	ctx := typeContextFromProg(prog)

	desc, err := extractCallableDesc(fun, ctx)
	if err != nil {
		t.Fatalf("extractCallableDesc: %v", err)
	}
	if len(desc.Returns) != 1 {
		t.Fatalf("expected 1 return type, got %d", len(desc.Returns))
	}
	if desc.Returns[0].Kind != schema.TypeKindStruct {
		t.Fatalf("expected return kind TypeKindStruct, got %q", desc.Returns[0].Kind)
	}
	if desc.Returns[0].ClassName != "Point" {
		t.Fatalf("expected return ClassName Point, got %q", desc.Returns[0].ClassName)
	}
}

func TestDescriptor_ClassToObjectDesc(t *testing.T) {
	prog := parseProg(t, "class Animal {\n  name: string\n  fun speak(): string {}\n}")
	ctx := typeContextFromProg(prog)
	desc := extractClassDesc(prog.Stmts[0], ctx)
	if desc.Name != "Animal" {
		t.Fatalf("expected name 'Animal', got %q", desc.Name)
	}
	if desc.Kind != schema.TypeKindClass {
		t.Fatalf("expected Kind=TypeKindClass, got %q", desc.Kind)
	}
	if len(desc.Fields) != 1 {
		t.Fatalf("expected 1 field, got %d", len(desc.Fields))
	}
}

func TestDescriptor_ClassMethodsAreASTOnly(t *testing.T) {
	prog := parseProg(t, "class Animal {\n  name: string\n  fun speak(): string {}\n  fun move(speed: int): void {}\n}")
	class := prog.Stmts[0].(*classStmt)
	if len(class.Methods) != 2 {
		t.Fatalf("expected parser to retain 2 AST methods, got %d", len(class.Methods))
	}

	ctx := typeContextFromProg(prog)
	desc := extractClassDesc(class, ctx)
	if desc.Name != "Animal" {
		t.Fatalf("expected name 'Animal', got %q", desc.Name)
	}
	if len(desc.Fields) != 1 {
		t.Fatalf("class method declarations must not enter current ObjectDesc contract; expected 1 field, got %d", len(desc.Fields))
	}
	if desc.Fields[0].Name != "name" {
		t.Fatalf("expected field 'name', got %+v", desc.Fields[0])
	}
}

func TestDescriptor_FunWithYieldToStreamingCallableDesc(t *testing.T) {
	prog := parseProg(t, "stream fun emit(): int { yield 1 return 2 }")
	fun := prog.Stmts[0].(*funStmt)
	ctx := typeContextFromProg(prog)
	desc, err := extractCallableDesc(fun, ctx)
	if err != nil {
		t.Fatalf("extractCallableDesc: %v", err)
	}
	if desc.Mode != schema.CallableModeStreaming {
		t.Fatalf("expected streaming mode, got %q", desc.Mode)
	}
	if desc.Streaming == nil || desc.Streaming.Next == nil || desc.Streaming.Final == nil {
		t.Fatalf("expected next/final streaming schemas, got %+v", desc.Streaming)
	}
	if desc.Streaming.Next.Kind != schema.TypeKindScalar || desc.Streaming.Next.Name != "int" {
		t.Fatalf("expected next int schema, got %+v", desc.Streaming.Next)
	}
	if desc.Streaming.Final.Kind != schema.TypeKindScalar || desc.Streaming.Final.Name != "int" {
		t.Fatalf("expected final int schema, got %+v", desc.Streaming.Final)
	}
}

func TestDescriptor_FunWithYieldNestedInIfToStreamingCallableDesc(t *testing.T) {
	prog := parseProg(t, "stream fun emit(flag: bool): int { if flag { yield 1 } return 2 }")
	fun := prog.Stmts[0].(*funStmt)
	ctx := typeContextFromProg(prog)
	desc, err := extractCallableDesc(fun, ctx)
	if err != nil {
		t.Fatalf("extractCallableDesc: %v", err)
	}
	if desc.Mode != schema.CallableModeStreaming {
		t.Fatalf("expected streaming mode, got %q", desc.Mode)
	}
	if desc.Streaming == nil || desc.Streaming.Next == nil || desc.Streaming.Next.Name != "int" {
		t.Fatalf("expected nested yield to infer int next schema, got %+v", desc.Streaming)
	}
}

func TestDescriptor_YieldTypeMismatchHasDiagnosticPath(t *testing.T) {
	prog := parseProg(t, "stream fun emit(): int { yield 1 yield \"x\" return 2 }")
	fun := prog.Stmts[0].(*funStmt)
	ctx := typeContextFromProg(prog)
	_, err := extractCallableDesc(fun, ctx)
	if err == nil {
		t.Fatal("expected lowering diagnostic error")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "yield_type_mismatch" {
		t.Fatalf("expected yield_type_mismatch code, got %v", err)
	}
	pather, ok := err.(interface{ DiagnosticPath() string })
	if !ok || pather.DiagnosticPath() != "frontend/type/inference" {
		t.Fatalf("expected frontend/type/inference path, got %v", err)
	}
	category, ok := err.(interface{ DiagnosticCategory() diagnostics.Category })
	if !ok || category.DiagnosticCategory() != diagnostics.CategorySchema {
		t.Fatalf("expected schema category, got %v", err)
	}
}

func TestDescriptor_DeclarationOrderStable(t *testing.T) {
	source := "fun a(): void {}\nfun b(): void {}\nfun c(): void {}"
	prog := parseProg(t, source)
	ctx := typeContextFromProg(prog)
	names := []string{"a", "b", "c"}
	for i, stmt := range prog.Stmts {
		fun := stmt.(*funStmt)
		desc, err := extractCallableDesc(fun, ctx)
		if err != nil {
			t.Fatalf("extractCallableDesc: %v", err)
		}
		if desc.Name != names[i] {
			t.Fatalf("stmt %d: expected %q, got %q", i, names[i], desc.Name)
		}
	}
}

func TestDescriptor_StreamFunRequiresYieldDiagnosticShape(t *testing.T) {
	prog := parseProg(t, "stream fun empty(): int { return 1 }")
	fun := prog.Stmts[0].(*funStmt)
	ctx := typeContextFromProg(prog)

	_, err := extractCallableDesc(fun, ctx)
	if err == nil {
		t.Fatal("expected stream_fun_requires_yield error")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "stream_fun_requires_yield" {
		t.Fatalf("expected stream_fun_requires_yield code, got %v", err)
	}
	pather, ok := err.(interface{ DiagnosticPath() string })
	if !ok || pather.DiagnosticPath() != "frontend/callable/stream" {
		t.Fatalf("expected frontend/callable/stream path, got %v", err)
	}
	category, ok := err.(interface{ DiagnosticCategory() diagnostics.Category })
	if !ok || category.DiagnosticCategory() != diagnostics.CategorySchema {
		t.Fatalf("expected schema category, got %v", err)
	}
}

func TestDescriptor_YieldOutsideStreamFunDiagnosticShape(t *testing.T) {
	prog := parseProg(t, "fun bad(): int { yield 1 return 2 }")
	fun := prog.Stmts[0].(*funStmt)
	ctx := typeContextFromProg(prog)

	_, err := extractCallableDesc(fun, ctx)
	if err == nil {
		t.Fatal("expected yield_requires_stream_fun error")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "yield_requires_stream_fun" {
		t.Fatalf("expected yield_requires_stream_fun code, got %v", err)
	}
	pather, ok := err.(interface{ DiagnosticPath() string })
	if !ok || pather.DiagnosticPath() != "frontend/callable/stream" {
		t.Fatalf("expected frontend/callable/stream path, got %v", err)
	}
	category, ok := err.(interface{ DiagnosticCategory() diagnostics.Category })
	if !ok || category.DiagnosticCategory() != diagnostics.CategorySchema {
		t.Fatalf("expected schema category, got %v", err)
	}
}

func TestDescriptor_StreamFunMissingFinalTypeDiagnosticShape(t *testing.T) {
	prog := parseProg(t, "stream fun emit() { yield 1 return 2 }")
	fun := prog.Stmts[0].(*funStmt)
	ctx := typeContextFromProg(prog)

	_, err := extractCallableDesc(fun, ctx)
	if err == nil {
		t.Fatal("expected stream_final_type_required error")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "stream_final_type_required" {
		t.Fatalf("expected stream_final_type_required code, got %v", err)
	}
	pather, ok := err.(interface{ DiagnosticPath() string })
	if !ok || pather.DiagnosticPath() != "frontend/callable/stream" {
		t.Fatalf("expected frontend/callable/stream path, got %v", err)
	}
	category, ok := err.(interface{ DiagnosticCategory() diagnostics.Category })
	if !ok || category.DiagnosticCategory() != diagnostics.CategorySchema {
		t.Fatalf("expected schema category, got %v", err)
	}
}

func TestDescriptor_StreamFunNestedYieldTypeMismatchDiagnosticShape(t *testing.T) {
	prog := parseProg(t, `stream fun emit(flag: bool): int { if flag { yield 1 } else { yield "x" } return 2 }`)
	fun := prog.Stmts[0].(*funStmt)
	ctx := typeContextFromProg(prog)

	_, err := extractCallableDesc(fun, ctx)
	if err == nil {
		t.Fatal("expected yield_type_mismatch error")
	}
	coder, ok := err.(interface{ DiagnosticCode() string })
	if !ok || coder.DiagnosticCode() != "yield_type_mismatch" {
		t.Fatalf("expected yield_type_mismatch code, got %v", err)
	}
	pather, ok := err.(interface{ DiagnosticPath() string })
	if !ok || pather.DiagnosticPath() != "frontend/type/inference" {
		t.Fatalf("expected frontend/type/inference path, got %v", err)
	}
	category, ok := err.(interface{ DiagnosticCategory() diagnostics.Category })
	if !ok || category.DiagnosticCategory() != diagnostics.CategorySchema {
		t.Fatalf("expected schema category, got %v", err)
	}
}
