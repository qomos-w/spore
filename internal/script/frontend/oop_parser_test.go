package frontend

import (
	"strings"
	"testing"
)

// --- OOP Parser Tests: Constructor, Super, Override, Open, Interface, Inheritance ---

func TestParser_PrivateFieldModifierShape(t *testing.T) {
	source := "class Foo {\n  private name: string\n  id: int\n}"
	prog := parseProg(t, source)
	cl := prog.Stmts[0].(*classStmt)
	if len(cl.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(cl.Fields))
	}
	if cl.Fields[0].Access != accessPrivate {
		t.Fatal("expected first field to be private")
	}
	if cl.Fields[1].Access != accessPublic {
		t.Fatal("expected second field to default to public")
	}
}

func TestParser_PrivateMethodModifierShape(t *testing.T) {
	source := "class Foo {\n  private fun helper(): void {}\n  fun visible(): void {}\n}"
	prog := parseProg(t, source)
	cl := prog.Stmts[0].(*classStmt)
	if len(cl.Methods) != 2 {
		t.Fatalf("expected 2 methods, got %d", len(cl.Methods))
	}
	if cl.Methods[0].Access != accessPrivate {
		t.Fatal("expected helper to be private")
	}
	if cl.Methods[1].Access != accessPublic {
		t.Fatal("expected visible to default to public")
	}
}

func TestParser_InterfaceDeclarationShape(t *testing.T) {
	source := "interface ISerializable {\n  fun serialize(): string\n  fun deserialize(data: string): void\n}"
	prog := parseProg(t, source)
	iface, ok := prog.Stmts[0].(*interfaceStmt)
	if !ok {
		t.Fatal("expected *interfaceStmt")
	}
	if iface.Name.Value != "ISerializable" {
		t.Fatalf("expected name 'ISerializable', got %q", iface.Name.Value)
	}
	if len(iface.Methods) != 2 {
		t.Fatalf("expected 2 methods, got %d", len(iface.Methods))
	}
	if iface.Methods[0].Name.Value != "serialize" {
		t.Fatalf("expected first method 'serialize', got %q", iface.Methods[0].Name.Value)
	}
	if iface.Methods[1].Name.Value != "deserialize" {
		t.Fatalf("expected second method 'deserialize', got %q", iface.Methods[1].Name.Value)
	}
}

func TestParser_ClassImplementsInterfaceShape(t *testing.T) {
	source := "class Dog : Animal, IDrawable, ISerializable {\n  name: string\n}"
	prog := parseProg(t, source)
	cl, ok := prog.Stmts[0].(*classStmt)
	if !ok {
		t.Fatal("expected *classStmt")
	}
	if cl.Parent == nil || cl.Parent.Value != "Animal" {
		t.Fatalf("expected parent 'Animal', got %v", cl.Parent)
	}
	if len(cl.Implements) != 2 {
		t.Fatalf("expected 2 implements, got %d", len(cl.Implements))
	}
	if cl.Implements[0].Value != "IDrawable" {
		t.Fatalf("expected first implement 'IDrawable', got %q", cl.Implements[0].Value)
	}
	if cl.Implements[1].Value != "ISerializable" {
		t.Fatalf("expected second implement 'ISerializable', got %q", cl.Implements[1].Value)
	}
}

func TestParser_SuperMethodExpression(t *testing.T) {
	source := "class Dog : Animal {\n  override fun speak(): string { return super.speak() }\n}"
	prog := parseProg(t, source)
	cl := prog.Stmts[0].(*classStmt)
	if len(cl.Methods) != 1 {
		t.Fatalf("expected 1 method, got %d", len(cl.Methods))
	}
	ret, ok := cl.Methods[0].Body.Stmts[0].(*returnStmt)
	if !ok {
		t.Fatalf("expected *returnStmt, got %T", cl.Methods[0].Body.Stmts[0])
	}
	super, ok := ret.Value.(*superExpr)
	if !ok {
		t.Fatalf("expected *superExpr for super.speak(), got %T", ret.Value)
	}
	if super.Method == nil || super.Method.Value != "speak" {
		t.Fatalf("expected super method 'speak', got %v", super.Method)
	}
}

func TestParser_OpenClassModifier(t *testing.T) {
	source := "open class Animal {\n  name: string\n  fun speak(): string {}\n}"
	prog := parseProg(t, source)
	cl, ok := prog.Stmts[0].(*classStmt)
	if !ok {
		t.Fatal("expected *classStmt")
	}
	if !cl.IsOpen {
		t.Fatal("expected IsOpen=true")
	}
}

func TestParser_OverrideMethodModifier(t *testing.T) {
	source := "class Dog : Animal {\n  override fun speak(): string {}\n}"
	prog := parseProg(t, source)
	cl := prog.Stmts[0].(*classStmt)
	if len(cl.Methods) != 1 {
		t.Fatalf("expected 1 method, got %d", len(cl.Methods))
	}
	if !cl.Methods[0].IsOverride {
		t.Fatal("expected IsOverride=true")
	}
}

func TestParser_OpenMethodModifier(t *testing.T) {
	source := "class Animal {\n  open fun speak(): string {}\n}"
	prog := parseProg(t, source)
	cl := prog.Stmts[0].(*classStmt)
	if len(cl.Methods) != 1 {
		t.Fatalf("expected 1 method, got %d", len(cl.Methods))
	}
	if !cl.Methods[0].IsOpen {
		t.Fatal("expected IsOpen=true on method")
	}
}

func TestParser_ClassConstructor(t *testing.T) {
	source := "class Dog {\n  name: string\n  constructor(n: string) {}\n}"
	prog := parseProg(t, source)
	cl, ok := prog.Stmts[0].(*classStmt)
	if !ok {
		t.Fatal("expected *classStmt")
	}
	if len(cl.Methods) != 1 {
		t.Fatalf("expected 1 method (constructor), got %d", len(cl.Methods))
	}
	if cl.Methods[0].Name.Value != "Dog" {
		t.Fatalf("expected constructor named 'Dog', got %q", cl.Methods[0].Name.Value)
	}
}

func TestParser_ClassConstructorWithBody(t *testing.T) {
	source := "class Dog {\n  name: string\n  constructor(n: string) { this.name = n }\n}"
	prog := parseProg(t, source)
	cl := prog.Stmts[0].(*classStmt)
	if len(cl.Methods) != 1 {
		t.Fatalf("expected 1 method, got %d", len(cl.Methods))
	}
	ctor := cl.Methods[0]
	if ctor.Name.Value != "Dog" {
		t.Fatalf("constructor should be named after class, got %q", ctor.Name.Value)
	}
	if len(ctor.Params) != 1 {
		t.Fatalf("expected 1 param, got %d", len(ctor.Params))
	}
	if ctor.Params[0].Name.Value != "n" {
		t.Fatalf("expected param 'n', got %q", ctor.Params[0].Name.Value)
	}
}

func TestParser_ClassInheritance(t *testing.T) {
	source := "class Dog : Animal {\n  name: string\n  fun speak(): string {}\n}"
	prog := parseProg(t, source)
	cl, ok := prog.Stmts[0].(*classStmt)
	if !ok {
		t.Fatal("expected *classStmt")
	}
	if cl.Parent == nil || cl.Parent.Value != "Animal" {
		t.Fatalf("expected parent 'Animal', got %v", cl.Parent)
	}
	if cl.Name.Value != "Dog" {
		t.Fatalf("expected name 'Dog', got %q", cl.Name.Value)
	}
}

func TestParser_ClassInheritanceWithImplements(t *testing.T) {
	source := "class Dog : Animal, IDrawable, ISerializable {\n  name: string\n}"
	prog := parseProg(t, source)
	cl, ok := prog.Stmts[0].(*classStmt)
	if !ok {
		t.Fatal("expected *classStmt")
	}
	if cl.Parent == nil || cl.Parent.Value != "Animal" {
		t.Fatalf("expected parent 'Animal', got %v", cl.Parent)
	}
	if len(cl.Implements) != 2 {
		t.Fatalf("expected 2 implements, got %d", len(cl.Implements))
	}
	if cl.Implements[0].Value != "IDrawable" {
		t.Fatalf("expected first implement 'IDrawable', got %q", cl.Implements[0].Value)
	}
	if cl.Implements[1].Value != "ISerializable" {
		t.Fatalf("expected second implement 'ISerializable', got %q", cl.Implements[1].Value)
	}
}

func TestParser_ClassNoInheritance(t *testing.T) {
	source := "class Animal {\n  name: string\n}"
	prog := parseProg(t, source)
	cl, ok := prog.Stmts[0].(*classStmt)
	if !ok {
		t.Fatal("expected *classStmt")
	}
	if cl.Parent != nil {
		t.Fatalf("expected no parent, got %q", cl.Parent.Value)
	}
	if len(cl.Implements) != 0 {
		t.Fatalf("expected no implements, got %d", len(cl.Implements))
	}
}

func TestParser_OpenClass(t *testing.T) {
	source := "open class Animal {\n  name: string\n  fun speak(): string {}\n}"
	prog := parseProg(t, source)
	cl, ok := prog.Stmts[0].(*classStmt)
	if !ok {
		t.Fatal("expected *classStmt")
	}
	if !cl.IsOpen {
		t.Fatal("expected IsOpen=true")
	}
}

func TestParser_Constructor(t *testing.T) {
	source := "class Dog {\n  name: string\n  constructor(n: string) {}\n}"
	prog := parseProg(t, source)
	cl, ok := prog.Stmts[0].(*classStmt)
	if !ok {
		t.Fatal("expected *classStmt")
	}
	if len(cl.Methods) != 1 {
		t.Fatalf("expected 1 method (constructor), got %d", len(cl.Methods))
	}
	if cl.Methods[0].Name.Value != "Dog" {
		t.Fatalf("expected constructor named 'Dog', got %q", cl.Methods[0].Name.Value)
	}
}

func TestParser_ConstructorWithBody(t *testing.T) {
	source := "class Dog {\n  name: string\n  constructor(n: string) { this.name = n }\n}"
	prog := parseProg(t, source)
	cl := prog.Stmts[0].(*classStmt)
	if len(cl.Methods) != 1 {
		t.Fatalf("expected 1 method, got %d", len(cl.Methods))
	}
	ctor := cl.Methods[0]
	if ctor.Name.Value != "Dog" {
		t.Fatalf("constructor should be named after class, got %q", ctor.Name.Value)
	}
	if len(ctor.Params) != 1 {
		t.Fatalf("expected 1 param, got %d", len(ctor.Params))
	}
	if ctor.Params[0].Name.Value != "n" {
		t.Fatalf("expected param 'n', got %q", ctor.Params[0].Name.Value)
	}
}

func TestParser_MethodAccessModifiers(t *testing.T) {
	source := "class Foo {\n  public fun a(): void {}\n  private fun b(): void {}\n  fun c(): void {}\n}"
	prog := parseProg(t, source)
	cl := prog.Stmts[0].(*classStmt)
	if len(cl.Methods) != 3 {
		t.Fatalf("expected 3 methods, got %d", len(cl.Methods))
	}
	if cl.Methods[0].Access != accessPublic {
		t.Fatal("expected method 'a' to be public")
	}
	if cl.Methods[1].Access != accessPrivate {
		t.Fatal("expected method 'b' to be private")
	}
	if cl.Methods[2].Access != accessPublic {
		t.Fatal("expected method 'c' to default to public")
	}
}

func TestParser_OverrideMethod(t *testing.T) {
	source := "class Dog : Animal {\n  override fun speak(): string {}\n}"
	prog := parseProg(t, source)
	cl := prog.Stmts[0].(*classStmt)
	if len(cl.Methods) != 1 {
		t.Fatalf("expected 1 method, got %d", len(cl.Methods))
	}
	if !cl.Methods[0].IsOverride {
		t.Fatal("expected IsOverride=true")
	}
}

func TestParser_OpenMethod(t *testing.T) {
	source := "class Animal {\n  open fun speak(): string {}\n}"
	prog := parseProg(t, source)
	cl := prog.Stmts[0].(*classStmt)
	if len(cl.Methods) != 1 {
		t.Fatalf("expected 1 method, got %d", len(cl.Methods))
	}
	if !cl.Methods[0].IsOpen {
		t.Fatal("expected IsOpen=true on method")
	}
}

func TestParser_PrivateField(t *testing.T) {
	source := "class Foo {\n  private name: string\n  id: int\n}"
	prog := parseProg(t, source)
	cl := prog.Stmts[0].(*classStmt)
	if len(cl.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(cl.Fields))
	}
	if cl.Fields[0].Access != accessPrivate {
		t.Fatal("expected first field to be private")
	}
	if cl.Fields[1].Access != accessPublic {
		t.Fatal("expected second field to default to public")
	}
}

func TestParser_InterfaceDeclaration(t *testing.T) {
	source := "interface ISerializable {\n  fun serialize(): string\n  fun deserialize(data: string): void\n}"
	prog := parseProg(t, source)
	iface, ok := prog.Stmts[0].(*interfaceStmt)
	if !ok {
		t.Fatal("expected *interfaceStmt")
	}
	if iface.Name.Value != "ISerializable" {
		t.Fatalf("expected name 'ISerializable', got %q", iface.Name.Value)
	}
	if len(iface.Methods) != 2 {
		t.Fatalf("expected 2 methods, got %d", len(iface.Methods))
	}
	if iface.Methods[0].Name.Value != "serialize" {
		t.Fatalf("expected first method 'serialize', got %q", iface.Methods[0].Name.Value)
	}
	if iface.Methods[1].Name.Value != "deserialize" {
		t.Fatalf("expected second method 'deserialize', got %q", iface.Methods[1].Name.Value)
	}
}

func TestParser_InterfaceMethodReturnType(t *testing.T) {
	source := "interface IDrawable {\n  fun draw(): void\n}"
	prog := parseProg(t, source)
	iface := prog.Stmts[0].(*interfaceStmt)
	if iface.Methods[0].ReturnType == nil {
		t.Fatal("expected return type on interface method")
	}
	if iface.Methods[0].ReturnType.Name != "void" {
		t.Fatalf("expected return type 'void', got %q", iface.Methods[0].ReturnType.Name)
	}
}

func TestParser_SuperExpression(t *testing.T) {
	source := "class Dog : Animal {\n  override fun speak(): string { return super.speak() }\n}"
	prog := parseProg(t, source)
	cl := prog.Stmts[0].(*classStmt)
	if len(cl.Methods) != 1 {
		t.Fatalf("expected 1 method, got %d", len(cl.Methods))
	}
	ret, ok := cl.Methods[0].Body.Stmts[0].(*returnStmt)
	if !ok {
		t.Fatalf("expected *returnStmt, got %T", cl.Methods[0].Body.Stmts[0])
	}
	super, ok := ret.Value.(*superExpr)
	if !ok {
		t.Fatalf("expected *superExpr for super.speak(), got %T", ret.Value)
	}
	if super.Method == nil || super.Method.Value != "speak" {
		t.Fatalf("expected super method 'speak', got %v", super.Method)
	}
}

// --- Reserved token rejection tests ---

func TestParser_StaticInClassBody(t *testing.T) {
	_, err := parseModule("class Foo {\n  static fun bar(): void {}\n  name: string\n}")
	if err == nil {
		t.Fatal("expected error for static in class body")
	}
	if !strings.Contains(err.Error(), "static is not supported") {
		t.Fatalf("expected 'not supported' error, got %q", err.Error())
	}
}

func TestParser_StaticInFunctionBody(t *testing.T) {
	_, err := parseModule("fun f(): void {\n  static\n}")
	if err == nil {
		t.Fatal("expected error for static in function body")
	}
	if !strings.Contains(err.Error(), "static is not supported") {
		t.Fatalf("expected 'not supported' error, got %q", err.Error())
	}
}

func TestParser_AsyncRejected(t *testing.T) {
	_, err := parseModule("fun f(): void {\n  async\n}")
	if err == nil {
		t.Fatal("expected error for async")
	}
	if !strings.Contains(err.Error(), "async is not supported") {
		t.Fatalf("expected 'not supported' error, got %q", err.Error())
	}
}

func TestParser_AwaitRejected(t *testing.T) {
	_, err := parseModule("fun f(): void {\n  await\n}")
	if err == nil {
		t.Fatal("expected error for await")
	}
	if !strings.Contains(err.Error(), "await is not supported") {
		t.Fatalf("expected 'not supported' error, got %q", err.Error())
	}
}

func TestParser_PackageAccepted(t *testing.T) {
	prog, err := parseModule("package myapp\nfun f(): void {}")
	if err != nil {
		t.Fatalf("unexpected error for package: %v", err)
	}
	if prog.PackageName() != "myapp" {
		t.Fatalf("expected package name 'myapp', got %q", prog.PackageName())
	}
}

func TestParser_ImportSyntaxAccepted(t *testing.T) {
	prog, err := parseModule("import add from \"math\"\nfun f(): int = add(1, 2)")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(prog.imports) != 1 {
		t.Fatalf("expected 1 import, got %d", len(prog.imports))
	}
}

func TestParser_ImportWithAliasSyntaxAccepted(t *testing.T) {
	prog, err := parseModule("import add as plus from \"math\"\nfun f(): int = plus(1, 2)")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(prog.imports) != 1 || prog.imports[0].Alias != "plus" {
		t.Fatalf("unexpected imports: %+v", prog.imports)
	}
}

func TestParser_InterfaceDefaultMethodBody(t *testing.T) {
	source := `interface IGreet {
  fun greet(): string { return "hello" }
  fun name(): string
  fun loud(): string { return this.greet() + "!" }
}`
	prog := parseProg(t, source)
	iface, ok := prog.Stmts[0].(*interfaceStmt)
	if !ok {
		t.Fatal("expected *interfaceStmt")
	}
	if len(iface.Methods) != 3 {
		t.Fatalf("expected 3 methods, got %d", len(iface.Methods))
	}
	if iface.Methods[0].Body == nil {
		t.Fatal("expected default method body on greet")
	}
	if len(iface.Methods[0].Body.Stmts) != 1 {
		t.Fatalf("expected 1 statement in greet body, got %d", len(iface.Methods[0].Body.Stmts))
	}
	if iface.Methods[1].Body != nil {
		t.Fatal("expected nil body for abstract method name")
	}
	if iface.Methods[2].Body == nil || len(iface.Methods[2].Body.Stmts) != 1 {
		t.Fatalf("expected default body on loud, got %+v", iface.Methods[2].Body)
	}
	if iface.Methods[2].ReturnType == nil || iface.Methods[2].ReturnType.Name != "string" {
		t.Fatalf("expected return type string on loud, got %+v", iface.Methods[2].ReturnType)
	}
}
