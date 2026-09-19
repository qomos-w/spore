package frontend

import (
	"testing"

	"github.com/qomos-w/spore/schema"
)

// --- Pipeline end-to-end ---

func TestPipeline_ExpressionBodyCallablePreservesReturnContract(t *testing.T) {
	prog, err := parseModule("fun greet(name: string): string = name")
	if err != nil {
		t.Fatal(err)
	}

	meta := declarationsFromProgram(prog)
	if len(meta.Callables) != 1 {
		t.Fatalf("expected 1 callable, got %d", len(meta.Callables))
	}

	desc := meta.Callables[0]
	if desc.Name != "greet" {
		t.Fatalf("expected greet, got %q", desc.Name)
	}
	if len(desc.Parameters) != 1 || desc.Parameters[0].Type.Name != "string" {
		t.Fatalf("expected one string param, got %+v", desc.Parameters)
	}
	if len(desc.Returns) != 1 || desc.Returns[0].Name != "string" {
		t.Fatalf("expected string return, got %+v", desc.Returns)
	}
}

func TestPipeline_BlockBodyCallablePreservesReturnContract(t *testing.T) {
	prog, err := parseModule("fun wrap(x: int): int { return x }")
	if err != nil {
		t.Fatal(err)
	}

	meta := declarationsFromProgram(prog)
	if len(meta.Callables) != 1 {
		t.Fatalf("expected 1 callable, got %d", len(meta.Callables))
	}

	desc := meta.Callables[0]
	if desc.Name != "wrap" {
		t.Fatalf("expected wrap, got %q", desc.Name)
	}
	if len(desc.Parameters) != 1 || desc.Parameters[0].Type.Name != "int" {
		t.Fatalf("expected one int param, got %+v", desc.Parameters)
	}
	if len(desc.Returns) != 1 || desc.Returns[0].Name != "int" {
		t.Fatalf("expected int return, got %+v", desc.Returns)
	}
}

func TestPipeline_ExportedFunOnlyAppearsInExportedCallables(t *testing.T) {
	prog, err := parseModule("fun local(): int = 1\nexport fun api(x: int): int = x")
	if err != nil {
		t.Fatal(err)
	}

	meta := declarationsFromProgram(prog)
	if len(meta.Callables) != 2 {
		t.Fatalf("expected 2 callables, got %d", len(meta.Callables))
	}
	if len(meta.ExportedCallables) != 1 {
		t.Fatalf("expected 1 exported callable, got %d", len(meta.ExportedCallables))
	}
	if meta.ExportedCallables[0].Name != "api" {
		t.Fatalf("expected exported callable api, got %q", meta.ExportedCallables[0].Name)
	}
}

func TestPipeline_SourceToCallableDescriptors(t *testing.T) {
	prog, err := parseModule("fun add(a: int, b: int): int {}")
	if err != nil {
		t.Fatal(err)
	}
	meta := declarationsFromProgram(prog)
	if len(meta.Callables) != 1 {
		t.Fatalf("expected 1 callable, got %d", len(meta.Callables))
	}
	if meta.Callables[0].Name != "add" {
		t.Fatalf("expected 'add', got %q", meta.Callables[0].Name)
	}
	if len(meta.Callables[0].Parameters) != 2 {
		t.Fatalf("expected 2 params, got %d", len(meta.Callables[0].Parameters))
	}
}

func TestPipeline_SourceToClassDescriptors(t *testing.T) {
	prog, err := parseModule("struct Point {\n  x: int\n  y: int\n}")
	if err != nil {
		t.Fatal(err)
	}
	meta := declarationsFromProgram(prog)
	if len(meta.Objects) != 1 {
		t.Fatalf("expected 1 object, got %d", len(meta.Objects))
	}
	if meta.Objects[0].Name != "Point" {
		t.Fatalf("expected 'Point', got %q", meta.Objects[0].Name)
	}
}

func TestPipeline_ExportedCallableDiscovery(t *testing.T) {
	source := "fun internal(): void {}\nexport fun api(x: int): int {}"
	prog, err := parseModule(source)
	if err != nil {
		t.Fatal(err)
	}
	meta := declarationsFromProgram(prog)
	if len(meta.Callables) != 2 {
		t.Fatalf("expected 2 callables, got %d", len(meta.Callables))
	}
	if len(meta.ExportedCallables) != 1 {
		t.Fatalf("expected 1 exported, got %d", len(meta.ExportedCallables))
	}
	if meta.ExportedCallables[0].Name != "api" {
		t.Fatalf("expected exported 'api', got %q", meta.ExportedCallables[0].Name)
	}
}

func TestPipeline_SyntaxMDStyleDeclarations(t *testing.T) {
	source := `fun compute(x: int): int {}
export fun serve(): void {}
struct Data {
  value: string
}
class Model {
  id: int
  fun validate(): bool {}
}`
	prog, err := parseModule(source)
	if err != nil {
		t.Fatal(err)
	}
	meta := declarationsFromProgram(prog)
	if len(meta.Callables) != 2 {
		t.Fatalf("expected 2 callables, got %d", len(meta.Callables))
	}
	if len(meta.ExportedCallables) != 1 {
		t.Fatalf("expected 1 exported, got %d", len(meta.ExportedCallables))
	}
	if len(meta.Objects) != 2 {
		t.Fatalf("expected 2 objects (struct+class), got %d", len(meta.Objects))
	}
}

func TestPipeline_CanonicalSyntaxDeclarations(t *testing.T) {
	source := "fun greet(name: string): string = name\nstruct Point {\n  x: int\n  y: int\n}"
	prog, err := parseModule(source)
	if err != nil {
		t.Fatal(err)
	}
	meta := declarationsFromProgram(prog)
	if len(meta.Callables) != 1 {
		t.Fatalf("expected 1 callable, got %d", len(meta.Callables))
	}
	if meta.Callables[0].Name != "greet" {
		t.Fatalf("expected 'greet', got %q", meta.Callables[0].Name)
	}
	if len(meta.Objects) != 1 {
		t.Fatalf("expected 1 object, got %d", len(meta.Objects))
	}
	if meta.Objects[0].Name != "Point" {
		t.Fatalf("expected 'Point', got %q", meta.Objects[0].Name)
	}
}

func TestPipeline_ErrorOnInvalidSource(t *testing.T) {
	_, err := parseModule("this is not valid")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestPipeline_SameSourceSameDescriptors(t *testing.T) {
	source := "fun f(x: int): string {}"
	prog1, _ := parseModule(source)
	prog2, _ := parseModule(source)
	meta1 := declarationsFromProgram(prog1)
	meta2 := declarationsFromProgram(prog2)
	if len(meta1.Callables) != len(meta2.Callables) {
		t.Fatal("descriptor count mismatch")
	}
	if meta1.Callables[0].Name != meta2.Callables[0].Name {
		t.Fatal("descriptor name mismatch")
	}
}

func TestPipeline_MixedCanonicalDeclarations(t *testing.T) {
	source := "fun old(x: int): int = x\nfun fresh(x: int): int = x + 1"
	prog, err := parseModule(source)
	if err != nil {
		t.Fatal(err)
	}
	meta := declarationsFromProgram(prog)
	if len(meta.Callables) != 2 {
		t.Fatalf("expected 2 callables, got %d", len(meta.Callables))
	}
	names := []string{"old", "fresh"}
	for i, desc := range meta.Callables {
		if desc.Name != names[i] {
			t.Fatalf("callable %d: expected %q, got %q", i, names[i], desc.Name)
		}
	}
}

func TestPipeline_ClassMethodsDoNotBecomeCallableDescriptors(t *testing.T) {
	source := "class Service {\n  name: string\n  fun run(): void {}\n  fun stop(): void {}\n}\nexport fun create(): Service {}"
	prog, err := parseModule(source)
	if err != nil {
		t.Fatal(err)
	}
	meta := declarationsFromProgram(prog)
	if len(meta.Objects) != 1 {
		t.Fatalf("expected 1 object, got %d", len(meta.Objects))
	}
	if len(meta.Objects[0].Fields) != 1 || meta.Objects[0].Fields[0].Name != "name" {
		t.Fatalf("class methods must not change field-only object metadata, got %+v", meta.Objects[0].Fields)
	}
	if len(meta.Callables) != 1 {
		t.Fatalf("class methods must not be promoted to top-level callable descriptors, got %d callables", len(meta.Callables))
	}
	if meta.Callables[0].Name != "create" {
		t.Fatalf("expected only top-level callable 'create', got %q", meta.Callables[0].Name)
	}
	if len(meta.ExportedCallables) != 1 || meta.ExportedCallables[0].Name != "create" {
		t.Fatalf("expected exported top-level callable create, got %+v", meta.ExportedCallables)
	}
}

func TestPipeline_ClassMethodsRemainInternalAfterBytecodeCompilation(t *testing.T) {
	source := "class Service {\n  fun run(): void {}\n}\nexport fun create(): void {}"
	prog, err := ParseModuleForTest(source)
	if err != nil {
		t.Fatal(err)
	}

	meta := declarationsFromProgram(prog)
	if len(meta.ExportedCallables) != 1 {
		t.Fatalf("expected 1 exported callable, got %d", len(meta.ExportedCallables))
	}
	if meta.ExportedCallables[0].Name != "create" {
		t.Fatalf("expected only top-level export create, got %q", meta.ExportedCallables[0].Name)
	}
	for _, desc := range meta.Callables {
		if desc.Name == "run" || desc.Name == "Service.run" {
			t.Fatalf("class method leaked into callable metadata: %+v", desc)
		}
	}
}

func TestPipeline_NewAndMethodDispatchDoNotLeakIntoCallableSurface(t *testing.T) {
	source := "class Counter {\n  value: int\n  fun inc(): void {}\n}\nfun make(): Counter { var c: Counter = new Counter() c.value = 1 return c }\nexport fun start(): void {}"
	prog, err := ParseModuleForTest(source)
	if err != nil {
		t.Fatal(err)
	}
	meta := declarationsFromProgram(prog)

	// Only top-level funs should appear as callables, not new/field/method internals.
	if len(meta.Callables) != 2 {
		t.Fatalf("expected 2 callables (make, start), got %d: %+v", len(meta.Callables), meta.Callables)
	}
	for _, c := range meta.Callables {
		if c.Name == "inc" || c.Name == "Counter.inc" {
			t.Fatalf("method dispatch leaked into callable surface: %q", c.Name)
		}
	}
	if len(meta.ExportedCallables) != 1 || meta.ExportedCallables[0].Name != "start" {
		t.Fatalf("expected exported callable 'start', got %+v", meta.ExportedCallables)
	}
}

func TestValidateExportCallableTypes_ClassParameterRejected(t *testing.T) {
	stmt := &funStmt{Name: &ident{Value: "api"}}
	desc := schema.CallableDesc{
		Name:       "api",
		Parameters: []schema.ParameterDesc{{Name: "p", Type: schema.TypeDesc{Kind: schema.TypeKindClass, Name: "class", ClassName: "Player"}}},
	}
	if err := validateExportCallableTypes(stmt, desc, nil); err == nil {
		t.Fatal("expected error for class parameter")
	}
}

func TestValidateExportCallableTypes_ClassReturnRejected(t *testing.T) {
	stmt := &funStmt{Name: &ident{Value: "api"}}
	desc := schema.CallableDesc{
		Name:    "api",
		Returns: []schema.TypeDesc{{Kind: schema.TypeKindClass, Name: "class", ClassName: "Player"}},
	}
	if err := validateExportCallableTypes(stmt, desc, nil); err == nil {
		t.Fatal("expected error for class return")
	}
}

func TestValidateExportCallableTypes_ClassStreamNextRejected(t *testing.T) {
	stmt := &funStmt{Name: &ident{Value: "api"}}
	desc := schema.CallableDesc{
		Name: "api",
		Mode: schema.CallableModeStreaming,
		Streaming: &schema.StreamingCallableDesc{
			Next: &schema.TypeDesc{Kind: schema.TypeKindClass, Name: "class", ClassName: "Player"},
		},
	}
	if err := validateExportCallableTypes(stmt, desc, nil); err == nil {
		t.Fatal("expected error for class stream next")
	}
}

func TestValidateExportCallableTypes_ClassStreamFinalRejected(t *testing.T) {
	stmt := &funStmt{Name: &ident{Value: "api"}}
	desc := schema.CallableDesc{
		Name: "api",
		Mode: schema.CallableModeStreaming,
		Streaming: &schema.StreamingCallableDesc{
			Final: &schema.TypeDesc{Kind: schema.TypeKindClass, Name: "class", ClassName: "Player"},
		},
	}
	if err := validateExportCallableTypes(stmt, desc, nil); err == nil {
		t.Fatal("expected error for class stream final")
	}
}

func TestValidateExportCallableTypes_StructWithClassFieldRejected(t *testing.T) {
	stmt := &funStmt{Name: &ident{Value: "api"}}
	desc := schema.CallableDesc{
		Name: "api",
		Parameters: []schema.ParameterDesc{{
			Name: "p",
			Type: schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "struct", ClassName: "Config"},
		}},
	}
	objects := map[string]schema.ObjectDesc{
		"Config": {
			Kind:   schema.TypeKindClass,
			Name:   "Config",
			Fields: []schema.FieldDesc{{Name: "x", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}}},
		},
	}
	if err := validateExportCallableTypes(stmt, desc, objects); err == nil {
		t.Fatal("expected error for struct parameter backed by class object")
	}
}

func TestValidateExportCallableTypes_ArrayOfClassRejected(t *testing.T) {
	stmt := &funStmt{Name: &ident{Value: "api"}}
	desc := schema.CallableDesc{
		Name: "api",
		Parameters: []schema.ParameterDesc{{
			Name: "p",
			Type: schema.TypeDesc{Kind: schema.TypeKindArray, Name: "array", Element: &schema.TypeDesc{Kind: schema.TypeKindClass, Name: "class", ClassName: "Player"}},
		}},
	}
	if err := validateExportCallableTypes(stmt, desc, nil); err == nil {
		t.Fatal("expected error for array of class")
	}
}

func TestValidateExportCallableTypes_MapWithClassValueRejected(t *testing.T) {
	stmt := &funStmt{Name: &ident{Value: "api"}}
	desc := schema.CallableDesc{
		Name: "api",
		Parameters: []schema.ParameterDesc{{
			Name: "p",
			Type: schema.TypeDesc{
				Kind:  schema.TypeKindMap,
				Name:  "map",
				Key:   &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"},
				Value: &schema.TypeDesc{Kind: schema.TypeKindClass, Name: "class", ClassName: "Player"},
			},
		}},
	}
	if err := validateExportCallableTypes(stmt, desc, nil); err == nil {
		t.Fatal("expected error for map with class value")
	}
}

func TestValidateExportCallableTypes_ValidStructAllowed(t *testing.T) {
	stmt := &funStmt{Name: &ident{Value: "api"}}
	desc := schema.CallableDesc{
		Name: "api",
		Parameters: []schema.ParameterDesc{{
			Name: "p",
			Type: schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "struct", ClassName: "Config"},
		}},
	}
	objects := map[string]schema.ObjectDesc{
		"Config": {
			Kind:   schema.TypeKindStruct,
			Name:   "Config",
			Fields: []schema.FieldDesc{{Name: "x", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}}},
		},
	}
	if err := validateExportCallableTypes(stmt, desc, objects); err != nil {
		t.Fatalf("expected nil for valid struct parameter, got %v", err)
	}
}

func TestValidateExportCallableTypes_ValidScalarAllowed(t *testing.T) {
	stmt := &funStmt{Name: &ident{Value: "api"}}
	desc := schema.CallableDesc{
		Name:       "api",
		Parameters: []schema.ParameterDesc{{Name: "p", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}}},
		Returns:    []schema.TypeDesc{{Kind: schema.TypeKindScalar, Name: "string"}},
	}
	if err := validateExportCallableTypes(stmt, desc, nil); err != nil {
		t.Fatalf("expected nil for valid scalar signature, got %v", err)
	}
}

func TestPipeline_ExecutionSurfaceExpansionPreservesDeclarationOrder(t *testing.T) {
	source := "struct Config {\n  name: string\n  timeout: int\n}\nclass Service {\n  host: string\n}\nfun init(): void {}\nexport fun run(): void {}\nfun stop(): void {}"
	prog, err := ParseModuleForTest(source)
	if err != nil {
		t.Fatal(err)
	}
	meta := declarationsFromProgram(prog)

	// Objects must be declaration-first order: Config then Service.
	if len(meta.Objects) != 2 {
		t.Fatalf("expected 2 objects, got %d", len(meta.Objects))
	}
	if meta.Objects[0].Name != "Config" || meta.Objects[1].Name != "Service" {
		t.Fatalf("declaration order broken: got %q, %q", meta.Objects[0].Name, meta.Objects[1].Name)
	}

	// Callables must be declaration-first order: init, run, stop.
	if len(meta.Callables) != 3 {
		t.Fatalf("expected 3 callables, got %d", len(meta.Callables))
	}
	names := []string{meta.Callables[0].Name, meta.Callables[1].Name, meta.Callables[2].Name}
	if names[0] != "init" || names[1] != "run" || names[2] != "stop" {
		t.Fatalf("callable declaration order broken: got %v", names)
	}

	// Only export fun should be exported.
	if len(meta.ExportedCallables) != 1 || meta.ExportedCallables[0].Name != "run" {
		t.Fatalf("expected only exported 'run', got %+v", meta.ExportedCallables)
	}
}
