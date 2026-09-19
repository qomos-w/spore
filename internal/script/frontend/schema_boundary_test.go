package frontend

import (
	"strings"
	"testing"
)

func TestSchemaBoundary_ExportFunClassLeakageMatrix(t *testing.T) {
	cases := []struct {
		name      string
		source    string
		wantError bool
	}{
		{
			name: "class_param",
			source: `class Model { id: int }
export fun api(m: Model): void {}`,
			wantError: true,
		},
		{
			name: "class_return",
			source: `class Model { id: int }
export fun api(): Model {}`,
			wantError: true,
		},
		{
			name: "array_of_class",
			source: `class Model { id: int }
export fun api(): array<Model> {}`,
			wantError: true,
		},
		{
			name: "map_with_class_value",
			source: `class Model { id: int }
export fun api(): map<string, Model> {}`,
			wantError: true,
		},
		{
			name: "nested_class_field_rejected",
			source: `
class Model { id: int }
struct Envelope { model: Model }
export fun api(): Envelope {}`,
			wantError: true,
		},
		{
			name: "nested_class_field_param_rejected",
			source: `
class Model { id: int }
struct Envelope { model: Model }
export fun api(req: Envelope): void {}`,
			wantError: true,
		},
		{
			name: "nested_struct_chain_class_rejected",
			source: `
class Model { id: int }
struct Inner { model: Model }
struct Outer { inner: Inner }
export fun api(): Outer {}`,
			wantError: true,
		},
		{
			name: "struct_array_of_class_rejected",
			source: `
class Model { id: int }
struct Envelope { items: array<Model> }
export fun api(): Envelope {}`,
			wantError: true,
		},
		{
			name: "struct_map_of_class_rejected",
			source: `
class Model { id: int }
struct Envelope { items: map<string, Model> }
export fun api(): Envelope {}`,
			wantError: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := compileDeclarations(tc.source)
			if tc.wantError {
				if err == nil {
					t.Fatal("expected schema boundary error for class leakage")
				}
				if !strings.Contains(err.Error(), "class type") {
					t.Fatalf("expected 'class type' in error, got %q", err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("expected schema boundary case to succeed, got %v", err)
			}
		})
	}
}

func TestSchemaBoundary_ExportFunNestedValueContractMatrix(t *testing.T) {
	cases := []struct {
		name   string
		source string
	}{
		{
			name: "nested_input_output",
			source: `
struct Entry {
  key: string
  value: int
}
struct Request {
  items: array<Entry>
}
struct Response {
  totals: map<string, Entry>
}
export fun api(req: Request): Response {}`,
		},
		{
			name: "multiple_params_and_nested_return",
			source: `
struct Entry {
  key: string
  value: int
}
struct Query {
  items: array<Entry>
}
struct Meta {
  count: int
}
struct Response {
  values: map<string, Entry>
  meta: Meta
}
export fun api(q: Query, limit: int, verbose: bool): Response {}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prog, err := parseModule(tc.source)
			if err != nil {
				t.Fatal(err)
			}
			meta := declarationsFromProgram(prog)
			if len(meta.Errors) != 0 {
				t.Fatalf("nested exported value contract should be allowed, got errors: %v", meta.Errors)
			}
			if len(meta.ExportedCallables) != 1 {
				t.Fatalf("expected 1 exported callable, got %d", len(meta.ExportedCallables))
			}
		})
	}
}

func TestSchemaBoundary_ExportFunComplexValueShapeWithNestedIOAllowed(t *testing.T) {
	source := `
struct Entry {
  key: string
  value: int
}
struct Request {
  items: array<Entry>
}
struct Response {
  totals: map<string, Entry>
}
export fun api(req: Request): Response {}`
	prog, err := parseModule(source)
	if err != nil {
		t.Fatal(err)
	}
	meta := declarationsFromProgram(prog)
	if len(meta.Errors) != 0 {
		t.Fatalf("nested exported input/output value shape should be allowed, got errors: %v", meta.Errors)
	}
	if len(meta.ExportedCallables) != 1 {
		t.Fatalf("expected 1 exported callable, got %d", len(meta.ExportedCallables))
	}
}

func TestSchemaBoundary_ExportFunComplexValueShapeAllowed(t *testing.T) {
	source := `
struct Entry {
  key: string
  value: int
}
struct Payload {
  items: array<Entry>
  totals: map<string, Entry>
}
export fun api(): Payload {}`
	prog, err := parseModule(source)
	if err != nil {
		t.Fatal(err)
	}
	meta := declarationsFromProgram(prog)
	if len(meta.Errors) != 0 {
		t.Fatalf("complex exported value shape should be allowed, got errors: %v", meta.Errors)
	}
	if len(meta.ExportedCallables) != 1 {
		t.Fatalf("expected 1 exported callable, got %d", len(meta.ExportedCallables))
	}
}

func TestSchemaBoundary_ExportFunClassLeakageRejectedInScenario(t *testing.T) {
	source := `
class InternalModel {
  value: int
  constructor(v: int) { this.value = v }
  fun score(): int { return this.value + 1 }
}
fun build(): InternalModel {
  return new InternalModel(41)
}
export fun api(): InternalModel {
  return build()
}`
	_, err := compileDeclarations(source)
	if err == nil {
		t.Fatal("expected schema boundary error for class leakage in exported scenario")
	}
	if !strings.Contains(err.Error(), "class type") {
		t.Fatalf("expected 'class type' in error, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "schema boundary") {
		t.Fatalf("expected 'schema boundary' in error, got %q", err.Error())
	}
}

// --- Schema boundary: export fun must use struct types, not class types ---

func TestSchemaBoundary_ExportFunClassParamRejected(t *testing.T) {
	source := "class Model {\n  id: int\n}\nexport fun api(m: Model): void {}"
	prog, err := parseModule(source)
	if err != nil {
		t.Fatal(err)
	}
	meta := declarationsFromProgram(prog)
	if len(meta.Errors) == 0 {
		t.Fatal("expected schema boundary error for class-typed export param")
	}
	if !strings.Contains(meta.Errors[0].Error(), "class type") {
		t.Fatalf("expected 'class type' in error, got %q", meta.Errors[0].Error())
	}
	if !strings.Contains(meta.Errors[0].Error(), "schema boundary") {
		t.Fatalf("expected 'schema boundary' in error, got %q", meta.Errors[0].Error())
	}
}

func TestSchemaBoundary_ExportFunClassReturnRejected(t *testing.T) {
	source := "class Model {\n  id: int\n}\nexport fun api(): Model {}"
	prog, err := parseModule(source)
	if err != nil {
		t.Fatal(err)
	}
	meta := declarationsFromProgram(prog)
	if len(meta.Errors) == 0 {
		t.Fatal("expected schema boundary error for class-typed export return")
	}
	if !strings.Contains(meta.Errors[0].Error(), "class type") {
		t.Fatalf("expected 'class type' in error, got %q", meta.Errors[0].Error())
	}
}

func TestSchemaBoundary_ExportFunStructParamAllowed(t *testing.T) {
	source := "struct Data {\n  value: string\n}\nexport fun api(d: Data): void {}"
	prog, err := parseModule(source)
	if err != nil {
		t.Fatal(err)
	}
	meta := declarationsFromProgram(prog)
	if len(meta.Errors) != 0 {
		t.Fatalf("struct-typed export param should be allowed, got errors: %v", meta.Errors)
	}
	if len(meta.ExportedCallables) != 1 {
		t.Fatalf("expected 1 exported callable, got %d", len(meta.ExportedCallables))
	}
}

func TestSchemaBoundary_ExportFunStructReturnAllowed(t *testing.T) {
	source := "struct Data {\n  value: string\n}\nexport fun api(): Data {}"
	prog, err := parseModule(source)
	if err != nil {
		t.Fatal(err)
	}
	meta := declarationsFromProgram(prog)
	if len(meta.Errors) != 0 {
		t.Fatalf("struct-typed export return should be allowed, got errors: %v", meta.Errors)
	}
}

func TestSchemaBoundary_ExportFunArrayOfClassRejected(t *testing.T) {
	source := "class Model {\n  id: int\n}\nexport fun api(): array<Model> {}"
	prog, err := parseModule(source)
	if err != nil {
		t.Fatal(err)
	}
	meta := declarationsFromProgram(prog)
	if len(meta.Errors) == 0 {
		t.Fatal("expected schema boundary error for array-of-class export return")
	}
	if !strings.Contains(meta.Errors[0].Error(), "class type") {
		t.Fatalf("expected 'class type' in error, got %q", meta.Errors[0].Error())
	}
}

func TestSchemaBoundary_ExportFunMapWithClassValueRejected(t *testing.T) {
	source := "class Model {\n  id: int\n}\nexport fun api(): map<string, Model> {}"
	prog, err := parseModule(source)
	if err != nil {
		t.Fatal(err)
	}
	meta := declarationsFromProgram(prog)
	if len(meta.Errors) == 0 {
		t.Fatal("expected schema boundary error for map-with-class-value export return")
	}
	if !strings.Contains(meta.Errors[0].Error(), "class type") {
		t.Fatalf("expected 'class type' in error, got %q", meta.Errors[0].Error())
	}
}

func TestSchemaBoundary_ExportFunArrayOfClassParamRejected(t *testing.T) {
	source := "class Model {\n  id: int\n}\nexport fun api(items: array<Model>): void {}"
	prog, err := parseModule(source)
	if err != nil {
		t.Fatal(err)
	}
	meta := declarationsFromProgram(prog)
	if len(meta.Errors) == 0 {
		t.Fatal("expected schema boundary error for array-of-class export param")
	}
}

func TestSchemaBoundary_ExportFunScalarParamAllowed(t *testing.T) {
	source := "export fun api(x: int, s: string): bool {}"
	prog, err := parseModule(source)
	if err != nil {
		t.Fatal(err)
	}
	meta := declarationsFromProgram(prog)
	if len(meta.Errors) != 0 {
		t.Fatalf("scalar-typed export should be allowed, got errors: %v", meta.Errors)
	}
}

func TestSchemaBoundary_ErrorPropagatesThroughCompiledDeclarations(t *testing.T) {
	source := "class Model {\n  id: int\n}\nexport fun api(m: Model): void {}"
	prog, err := parseModule(source)
	if err != nil {
		t.Fatal(err)
	}
	_, err = compiledDeclarationsFromProgram(prog)
	if err == nil {
		t.Fatal("expected error from compiledDeclarationsFromProgram for class-typed export")
	}
	if !strings.Contains(err.Error(), "class type") {
		t.Fatalf("expected 'class type' in propagated error, got %q", err.Error())
	}
}

func TestSchemaBoundary_ErrorPropagatesThroughLoadSource(t *testing.T) {
	source := "class Model {\n  id: int\n}\nexport fun api(m: Model): void {}"
	_, err := compileDeclarations(source)
	if err == nil {
		t.Fatal("expected error from compileDeclarations for class-typed export")
	}
	if !strings.Contains(err.Error(), "class type") {
		t.Fatalf("expected 'class type' in propagated error, got %q", err.Error())
	}
}

func TestSchemaBoundary_ExportFunArrayOfStructAllowed(t *testing.T) {
	source := "struct Data {\n  value: string\n}\nexport fun api(): array<Data> {}"
	prog, err := parseModule(source)
	if err != nil {
		t.Fatal(err)
	}
	meta := declarationsFromProgram(prog)
	if len(meta.Errors) != 0 {
		t.Fatalf("array-of-struct export return should be allowed, got errors: %v", meta.Errors)
	}
}

func TestSchemaBoundary_ExportFunMapWithStructValueAllowed(t *testing.T) {
	source := "struct Data {\n  value: string\n}\nexport fun api(): map<string, Data> {}"
	prog, err := parseModule(source)
	if err != nil {
		t.Fatal(err)
	}
	meta := declarationsFromProgram(prog)
	if len(meta.Errors) != 0 {
		t.Fatalf("map-with-struct-value export return should be allowed, got errors: %v", meta.Errors)
	}
}

func TestSchemaBoundary_UndeclaredUppercaseTypeAllowedInExport(t *testing.T) {
	// An uppercase ident that is NOT declared as struct or class defaults to
	// TypeKindClass in the type system. Since no declaration exists, the
	// boundary check sees TypeKindClass and rejects it.
	source := "export fun api(result: Result): void {}"
	prog, err := parseModule(source)
	if err != nil {
		t.Fatal(err)
	}
	meta := declarationsFromProgram(prog)
	// Undeclared uppercase types now resolve to TypeKindClass, which is
	// rejected at the schema boundary. This is correct: if you want to export
	// a type, it must be declared as a struct.
	if len(meta.Errors) == 0 {
		t.Fatal("expected schema boundary error for undeclared class-typed export param")
	}
	if !strings.Contains(meta.Errors[0].Error(), "class type") {
		t.Fatalf("expected 'class type' in error, got %q", meta.Errors[0].Error())
	}
}
