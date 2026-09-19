package frontend

import (
	"testing"

	"github.com/qomos-w/spore/schema"
)

// --- OOP Descriptor Extraction Tests ---

func TestDescriptor_PrivateFieldMetadata(t *testing.T) {
	prog := parseProg(t, "class Foo {\n  private name: string\n  id: int\n}")
	ctx := typeContextFromProg(prog)
	desc := extractClassDesc(prog.Stmts[0], ctx)
	if len(desc.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(desc.Fields))
	}
	if !desc.Fields[0].Private {
		t.Fatal("expected first field Private=true")
	}
	if desc.Fields[1].Private {
		t.Fatal("expected second field Private=false")
	}
}

func TestDescriptor_PrivateMethodMetadata(t *testing.T) {
	prog := parseProg(t, "class Foo {\n  private fun helper(): void {}\n}")
	ctx := typeContextFromProg(prog)
	desc := extractClassDesc(prog.Stmts[0], ctx)
	if len(desc.Methods) != 1 {
		t.Fatalf("expected 1 method, got %d", len(desc.Methods))
	}
	if !desc.Methods[0].Private {
		t.Fatal("expected method Private=true")
	}
}

func TestDescriptor_ClassImplementsMetadata(t *testing.T) {
	prog := parseProg(t, "class Dog : Animal, IDrawable, ISerializable {\n  name: string\n}")
	ctx := typeContextFromProg(prog)
	desc := extractClassDesc(prog.Stmts[0], ctx)
	if len(desc.Implements) != 2 {
		t.Fatalf("expected 2 implements, got %d", len(desc.Implements))
	}
	if desc.Implements[0] != "IDrawable" {
		t.Fatalf("expected 'IDrawable', got %q", desc.Implements[0])
	}
	if desc.Implements[1] != "ISerializable" {
		t.Fatalf("expected 'ISerializable', got %q", desc.Implements[1])
	}
}

func TestDescriptor_InterfaceExtractionMetadata(t *testing.T) {
	prog := parseProg(t, "interface ISerializable {\n  fun serialize(): string\n  fun deserialize(data: string): void\n}")
	iface := prog.Stmts[0].(*interfaceStmt)
	ctx := typeContextFromProg(prog)
	desc := extractInterfaceDesc(iface, ctx)
	if desc.Name != "ISerializable" {
		t.Fatalf("expected name 'ISerializable', got %q", desc.Name)
	}
	if len(desc.Methods) != 2 {
		t.Fatalf("expected 2 methods, got %d", len(desc.Methods))
	}
}

func TestDescriptor_OpenClassMetadata(t *testing.T) {
	prog := parseProg(t, "open class Base {\n  fun template(): void {}\n}")
	ctx := typeContextFromProg(prog)
	desc := extractClassDesc(prog.Stmts[0], ctx)
	if !desc.IsOpen {
		t.Fatal("expected IsOpen=true")
	}
}

func TestDescriptor_OpenMethodMetadata(t *testing.T) {
	prog := parseProg(t, "class Animal {\n  open fun speak(): string {}\n}")
	ctx := typeContextFromProg(prog)
	desc := extractClassDesc(prog.Stmts[0], ctx)
	if len(desc.Methods) != 1 {
		t.Fatalf("expected 1 method, got %d", len(desc.Methods))
	}
	if !desc.Methods[0].IsOpen {
		t.Fatal("expected method IsOpen=true")
	}
}

func TestDescriptor_OverrideMethodMetadata(t *testing.T) {
	prog := parseProg(t, "class Dog : Animal {\n  override fun speak(): string {}\n}")
	ctx := typeContextFromProg(prog)
	desc := extractClassDesc(prog.Stmts[0], ctx)
	if len(desc.Methods) != 1 {
		t.Fatalf("expected 1 method, got %d", len(desc.Methods))
	}
	if !desc.Methods[0].IsOverride {
		t.Fatal("expected method IsOverride=true")
	}
}

func TestDescriptor_ClassConstructorRetainedInMethodMetadata(t *testing.T) {
	prog := parseProg(t, "class Dog {\n  name: string\n  constructor(n: string) {}\n}")
	ctx := typeContextFromProg(prog)
	desc := extractClassDesc(prog.Stmts[0], ctx)
	if len(desc.Methods) != 1 {
		t.Fatalf("expected 1 method, got %d", len(desc.Methods))
	}
	if desc.Methods[0].Name != "Dog" {
		t.Fatalf("expected constructor metadata name Dog, got %q", desc.Methods[0].Name)
	}
	if len(desc.Methods[0].Parameters) != 1 || desc.Methods[0].Parameters[0].Name != "n" {
		t.Fatalf("expected constructor param n, got %+v", desc.Methods[0].Parameters)
	}
}

func TestDescriptor_ClassWithParent(t *testing.T) {
	prog := parseProg(t, "class Dog : Animal {\n  name: string\n}")
	ctx := typeContextFromProg(prog)
	desc := extractClassDesc(prog.Stmts[0], ctx)
	if desc.Parent != "Animal" {
		t.Fatalf("expected parent 'Animal', got %q", desc.Parent)
	}
}

func TestDescriptor_ClassWithNoParent(t *testing.T) {
	prog := parseProg(t, "class Animal {\n  name: string\n}")
	ctx := typeContextFromProg(prog)
	desc := extractClassDesc(prog.Stmts[0], ctx)
	if desc.Parent != "" {
		t.Fatalf("expected empty parent, got %q", desc.Parent)
	}
}

func TestDescriptor_OpenClass(t *testing.T) {
	prog := parseProg(t, "open class Base {\n  fun template(): void {}\n}")
	ctx := typeContextFromProg(prog)
	desc := extractClassDesc(prog.Stmts[0], ctx)
	if !desc.IsOpen {
		t.Fatal("expected IsOpen=true")
	}
}

func TestDescriptor_ClosedClass(t *testing.T) {
	prog := parseProg(t, "class Final {\n  value: int\n}")
	ctx := typeContextFromProg(prog)
	desc := extractClassDesc(prog.Stmts[0], ctx)
	if desc.IsOpen {
		t.Fatal("expected IsOpen=false")
	}
}

func TestDescriptor_ClassImplements(t *testing.T) {
	prog := parseProg(t, "class Dog : Animal, IDrawable, ISerializable {\n  name: string\n}")
	ctx := typeContextFromProg(prog)
	desc := extractClassDesc(prog.Stmts[0], ctx)
	if len(desc.Implements) != 2 {
		t.Fatalf("expected 2 implements, got %d", len(desc.Implements))
	}
	if desc.Implements[0] != "IDrawable" {
		t.Fatalf("expected 'IDrawable', got %q", desc.Implements[0])
	}
	if desc.Implements[1] != "ISerializable" {
		t.Fatalf("expected 'ISerializable', got %q", desc.Implements[1])
	}
}

func TestDescriptor_ClassMethods(t *testing.T) {
	prog := parseProg(t, "class Animal {\n  name: string\n  fun speak(): string {}\n  fun move(speed: int): void {}\n}")
	ctx := typeContextFromProg(prog)
	desc := extractClassDesc(prog.Stmts[0], ctx)
	if len(desc.Methods) != 2 {
		t.Fatalf("expected 2 methods, got %d", len(desc.Methods))
	}
	if desc.Methods[0].Name != "speak" {
		t.Fatalf("expected method 'speak', got %q", desc.Methods[0].Name)
	}
	if desc.Methods[1].Name != "move" {
		t.Fatalf("expected method 'move', got %q", desc.Methods[1].Name)
	}
}

func TestDescriptor_MethodIsOpen(t *testing.T) {
	prog := parseProg(t, "class Animal {\n  open fun speak(): string {}\n}")
	ctx := typeContextFromProg(prog)
	desc := extractClassDesc(prog.Stmts[0], ctx)
	if len(desc.Methods) != 1 {
		t.Fatalf("expected 1 method, got %d", len(desc.Methods))
	}
	if !desc.Methods[0].IsOpen {
		t.Fatal("expected method IsOpen=true")
	}
}

func TestDescriptor_MethodIsOverride(t *testing.T) {
	prog := parseProg(t, "class Dog : Animal {\n  override fun speak(): string {}\n}")
	ctx := typeContextFromProg(prog)
	desc := extractClassDesc(prog.Stmts[0], ctx)
	if len(desc.Methods) != 1 {
		t.Fatalf("expected 1 method, got %d", len(desc.Methods))
	}
	if !desc.Methods[0].IsOverride {
		t.Fatal("expected method IsOverride=true")
	}
}

func TestDescriptor_MethodPrivate(t *testing.T) {
	prog := parseProg(t, "class Foo {\n  private fun helper(): void {}\n}")
	ctx := typeContextFromProg(prog)
	desc := extractClassDesc(prog.Stmts[0], ctx)
	if len(desc.Methods) != 1 {
		t.Fatalf("expected 1 method, got %d", len(desc.Methods))
	}
	if !desc.Methods[0].Private {
		t.Fatal("expected method Private=true")
	}
}

func TestDescriptor_FieldPrivate(t *testing.T) {
	prog := parseProg(t, "class Foo {\n  private name: string\n  id: int\n}")
	ctx := typeContextFromProg(prog)
	desc := extractClassDesc(prog.Stmts[0], ctx)
	if len(desc.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(desc.Fields))
	}
	if !desc.Fields[0].Private {
		t.Fatal("expected first field Private=true")
	}
	if desc.Fields[1].Private {
		t.Fatal("expected second field Private=false")
	}
}

func TestDescriptor_MethodParameters(t *testing.T) {
	prog := parseProg(t, "class Foo {\n  fun calc(x: int, y: string): bool {}\n}")
	ctx := typeContextFromProg(prog)
	desc := extractClassDesc(prog.Stmts[0], ctx)
	m := desc.Methods[0]
	if len(m.Parameters) != 2 {
		t.Fatalf("expected 2 params, got %d", len(m.Parameters))
	}
	if m.Parameters[0].Name != "x" || m.Parameters[0].Type.Kind != schema.TypeKindScalar {
		t.Fatalf("expected param x scalar, got %+v", m.Parameters[0])
	}
	if m.Parameters[1].Name != "y" || m.Parameters[1].Type.Kind != schema.TypeKindScalar {
		t.Fatalf("expected param y scalar, got %+v", m.Parameters[1])
	}
}

func TestDescriptor_MethodReturnType(t *testing.T) {
	prog := parseProg(t, "class Foo {\n  fun validate(): bool {}\n}")
	ctx := typeContextFromProg(prog)
	desc := extractClassDesc(prog.Stmts[0], ctx)
	m := desc.Methods[0]
	if len(m.Returns) != 1 {
		t.Fatalf("expected 1 return, got %d", len(m.Returns))
	}
	if m.Returns[0].Kind != schema.TypeKindScalar || m.Returns[0].Name != "bool" {
		t.Fatalf("expected bool return, got %+v", m.Returns[0])
	}
}

func TestDescriptor_InterfaceExtraction(t *testing.T) {
	prog := parseProg(t, "interface ISerializable {\n  fun serialize(): string\n  fun deserialize(data: string): void\n}")
	iface := prog.Stmts[0].(*interfaceStmt)
	ctx := typeContextFromProg(prog)
	desc := extractInterfaceDesc(iface, ctx)
	if desc.Name != "ISerializable" {
		t.Fatalf("expected name 'ISerializable', got %q", desc.Name)
	}
	if len(desc.Methods) != 2 {
		t.Fatalf("expected 2 methods, got %d", len(desc.Methods))
	}
	if desc.Methods[0].Name != "serialize" {
		t.Fatalf("expected first method 'serialize', got %q", desc.Methods[0].Name)
	}
	if desc.Methods[1].Name != "deserialize" {
		t.Fatalf("expected second method 'deserialize', got %q", desc.Methods[1].Name)
	}
}

func TestDescriptor_InterfaceMethodSignature(t *testing.T) {
	prog := parseProg(t, "interface IDrawable {\n  fun draw(color: string, opacity: float): void\n}")
	iface := prog.Stmts[0].(*interfaceStmt)
	ctx := typeContextFromProg(prog)
	desc := extractInterfaceDesc(iface, ctx)
	m := desc.Methods[0]
	if len(m.Parameters) != 2 {
		t.Fatalf("expected 2 params, got %d", len(m.Parameters))
	}
	if m.Parameters[0].Name != "color" {
		t.Fatalf("expected param 'color', got %q", m.Parameters[0].Name)
	}
	if m.Parameters[1].Name != "opacity" {
		t.Fatalf("expected param 'opacity', got %q", m.Parameters[1].Name)
	}
}

func TestDescriptor_ArrayOfScalarSyntax(t *testing.T) {
	prog := parseProg(t, "fun f(items: array<int>): void {}")
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
	if pt.Element == nil || pt.Element.Kind != schema.TypeKindScalar || pt.Element.Name != "int" {
		t.Fatalf("expected array<int> element, got %+v", pt)
	}
}

func TestDescriptor_ArrayOfClassSyntax(t *testing.T) {
	prog := parseProg(t, "fun f(items: array<Model>): void {}")
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
	if pt.Element == nil || pt.Element.Kind != schema.TypeKindClass || pt.Element.ClassName != "Model" {
		t.Fatalf("expected array<Model> element class, got %+v", pt)
	}
}

func TestDescriptor_MapWithClassValueSyntax(t *testing.T) {
	prog := parseProg(t, "fun f(m: map<string, Model>): void {}")
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
	if pt.Value == nil || pt.Value.Kind != schema.TypeKindClass || pt.Value.ClassName != "Model" {
		t.Fatalf("expected map<string, Model> value class, got %+v", pt)
	}
}

// --- TypeKindStruct resolution tests ---

func TestDescriptor_StructTypeResolvesToStructKind(t *testing.T) {
	prog := parseProg(t, "struct Data {\n  value: string\n}\nfun f(d: Data): void {}")
	ctx := typeContextFromProg(prog)
	fun := prog.Stmts[1].(*funStmt)
	desc, err := extractCallableDesc(fun, ctx)
	if err != nil {
		t.Fatalf("extractCallableDesc: %v", err)
	}
	pt := desc.Parameters[0].Type
	if pt.Kind != schema.TypeKindStruct {
		t.Fatalf("expected TypeKindStruct for struct-typed param, got %q", pt.Kind)
	}
	if pt.ClassName != "Data" {
		t.Fatalf("expected ClassName 'Data', got %q", pt.ClassName)
	}
}

func TestDescriptor_ClassTypeResolvesToClassKind(t *testing.T) {
	prog := parseProg(t, "class Model {\n  id: int\n}\nfun f(m: Model): void {}")
	ctx := typeContextFromProg(prog)
	fun := prog.Stmts[1].(*funStmt)
	desc, err := extractCallableDesc(fun, ctx)
	if err != nil {
		t.Fatalf("extractCallableDesc: %v", err)
	}
	pt := desc.Parameters[0].Type
	if pt.Kind != schema.TypeKindClass {
		t.Fatalf("expected TypeKindClass for class-typed param, got %q", pt.Kind)
	}
	if pt.ClassName != "Model" {
		t.Fatalf("expected ClassName 'Model', got %q", pt.ClassName)
	}
}

func TestDescriptor_StructReturnResolvesToStructKind(t *testing.T) {
	prog := parseProg(t, "struct Data {\n  value: string\n}\nfun f(): Data {}")
	ctx := typeContextFromProg(prog)
	fun := prog.Stmts[1].(*funStmt)
	desc, err := extractCallableDesc(fun, ctx)
	if err != nil {
		t.Fatalf("extractCallableDesc: %v", err)
	}
	if len(desc.Returns) != 1 {
		t.Fatalf("expected 1 return, got %d", len(desc.Returns))
	}
	if desc.Returns[0].Kind != schema.TypeKindStruct {
		t.Fatalf("expected TypeKindStruct for struct return, got %q", desc.Returns[0].Kind)
	}
}

func TestDescriptor_ArrayOfStructResolvesToStructElement(t *testing.T) {
	prog := parseProg(t, "struct Data {\n  value: string\n}\nfun f(items: array<Data>): void {}")
	ctx := typeContextFromProg(prog)
	fun := prog.Stmts[1].(*funStmt)
	desc, err := extractCallableDesc(fun, ctx)
	if err != nil {
		t.Fatalf("extractCallableDesc: %v", err)
	}
	pt := desc.Parameters[0].Type
	if pt.Kind != schema.TypeKindArray {
		t.Fatalf("expected array kind, got %q", pt.Kind)
	}
	if pt.Element == nil || pt.Element.Kind != schema.TypeKindStruct {
		t.Fatalf("expected TypeKindStruct element, got %+v", pt.Element)
	}
	if pt.Element.ClassName != "Data" {
		t.Fatalf("expected ClassName 'Data', got %q", pt.Element.ClassName)
	}
}

func TestDescriptor_MapWithStructValueResolvesToStructValue(t *testing.T) {
	prog := parseProg(t, "struct Data {\n  value: string\n}\nfun f(m: map<string, Data>): void {}")
	ctx := typeContextFromProg(prog)
	fun := prog.Stmts[1].(*funStmt)
	desc, err := extractCallableDesc(fun, ctx)
	if err != nil {
		t.Fatalf("extractCallableDesc: %v", err)
	}
	pt := desc.Parameters[0].Type
	if pt.Kind != schema.TypeKindMap {
		t.Fatalf("expected map kind, got %q", pt.Kind)
	}
	if pt.Value == nil || pt.Value.Kind != schema.TypeKindStruct {
		t.Fatalf("expected TypeKindStruct value, got %+v", pt.Value)
	}
	if pt.Value.ClassName != "Data" {
		t.Fatalf("expected ClassName 'Data', got %q", pt.Value.ClassName)
	}
}

func TestDescriptor_StructDescKindIsStruct(t *testing.T) {
	prog := parseProg(t, "struct Point {\n  x: int\n  y: int\n}")
	ctx := typeContextFromProg(prog)
	desc := extractClassDesc(prog.Stmts[0], ctx)
	if desc.Kind != schema.TypeKindStruct {
		t.Fatalf("expected struct ObjectDesc.Kind=TypeKindStruct, got %q", desc.Kind)
	}
}

func TestDescriptor_ObjectDescKindIsClass(t *testing.T) {
	prog := parseProg(t, "class Model {\n  id: int\n}")
	ctx := typeContextFromProg(prog)
	desc := extractClassDesc(prog.Stmts[0], ctx)
	if desc.Kind != schema.TypeKindClass {
		t.Fatalf("expected class ObjectDesc.Kind=TypeKindClass, got %q", desc.Kind)
	}
}
