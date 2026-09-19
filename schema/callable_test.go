package schema

import "testing"

func TestCloneMethodDesc_DeepCopiesParametersAndReturns(t *testing.T) {
	original := MethodDesc{
		Name:       "add",
		Parameters: []ParameterDesc{{Name: "a", Type: TypeDesc{Kind: TypeKindScalar, Name: "int"}}},
		Returns:    []TypeDesc{{Kind: TypeKindScalar, Name: "int"}},
	}
	cloned := CloneMethodDesc(original)

	if cloned.Name != "add" {
		t.Fatalf("expected name preserved, got %q", cloned.Name)
	}
	if len(cloned.Parameters) != 1 || cloned.Parameters[0].Name != "a" {
		t.Fatal("parameters not cloned")
	}
	if len(cloned.Returns) != 1 || cloned.Returns[0].Name != "int" {
		t.Fatal("returns not cloned")
	}

	// Mutation to clone must not affect original
	cloned.Parameters[0].Name = "mutated"
	if original.Parameters[0].Name != "a" {
		t.Fatal("clone mutation leaked to original")
	}
}

func TestCloneObjectDesc_DeepCopiesFieldsAndMethods(t *testing.T) {
	original := ObjectDesc{
		Name:       "Player",
		Kind:       TypeKindClass,
		Parent:     "Entity",
		IsOpen:     true,
		Implements: []string{"Movable", "Damageable"},
		Fields: []FieldDesc{
			{Name: "Name", Type: TypeDesc{Kind: TypeKindScalar, Name: "string"}},
			{Name: "Level", Type: TypeDesc{Kind: TypeKindScalar, Name: "int"}},
		},
		Methods: []MethodDesc{
			{Name: "Attack", Parameters: []ParameterDesc{{Name: "target", Type: TypeDesc{Kind: TypeKindScalar, Name: "string"}}}},
		},
	}
	cloned := CloneObjectDesc(original)

	if cloned.Name != "Player" || cloned.Kind != TypeKindClass || cloned.Parent != "Entity" || !cloned.IsOpen {
		t.Fatal("basic fields not preserved")
	}
	if len(cloned.Implements) != 2 || cloned.Implements[0] != "Movable" {
		t.Fatal("implements not cloned")
	}
	if len(cloned.Fields) != 2 || cloned.Fields[0].Name != "Name" {
		t.Fatal("fields not cloned")
	}
	if len(cloned.Methods) != 1 || cloned.Methods[0].Name != "Attack" {
		t.Fatal("methods not cloned")
	}

	// Deep copy verification
	cloned.Implements[0] = "mutated"
	if original.Implements[0] != "Movable" {
		t.Fatal("implements mutation leaked")
	}
	cloned.Fields[0].Name = "mutated"
	if original.Fields[0].Name != "Name" {
		t.Fatal("fields mutation leaked")
	}
	cloned.Methods[0].Name = "mutated"
	if original.Methods[0].Name != "Attack" {
		t.Fatal("methods mutation leaked")
	}
}

func TestCloneInterfaceDesc_DeepCopiesMethods(t *testing.T) {
	original := InterfaceDesc{
		Name: "Movable",
		Methods: []MethodDesc{
			{Name: "Move", Parameters: []ParameterDesc{{Name: "dx", Type: TypeDesc{Kind: TypeKindScalar, Name: "int"}}}},
		},
	}
	cloned := CloneInterfaceDesc(original)

	if cloned.Name != "Movable" {
		t.Fatalf("expected name preserved, got %q", cloned.Name)
	}
	if len(cloned.Methods) != 1 || cloned.Methods[0].Name != "Move" {
		t.Fatal("methods not cloned")
	}

	cloned.Methods[0].Name = "mutated"
	if original.Methods[0].Name != "Move" {
		t.Fatal("methods mutation leaked to original")
	}
}

func TestCloneMethodDesc_EmptyIsSafe(t *testing.T) {
	original := MethodDesc{Name: "empty"}
	cloned := CloneMethodDesc(original)
	if cloned.Name != "empty" || len(cloned.Parameters) != 0 || len(cloned.Returns) != 0 {
		t.Fatal("empty method desc not cloned correctly")
	}
}

func TestCloneObjectDesc_EmptyIsSafe(t *testing.T) {
	original := ObjectDesc{Name: "empty"}
	cloned := CloneObjectDesc(original)
	if cloned.Name != "empty" || len(cloned.Fields) != 0 || len(cloned.Methods) != 0 {
		t.Fatal("empty object desc not cloned correctly")
	}
}

func TestCloneInterfaceDesc_EmptyIsSafe(t *testing.T) {
	original := InterfaceDesc{Name: "empty"}
	cloned := CloneInterfaceDesc(original)
	if cloned.Name != "empty" || len(cloned.Methods) != 0 {
		t.Fatal("empty interface desc not cloned correctly")
	}
}

func TestCloneTypeDesc_DeepCopiesNestedPointers(t *testing.T) {
	original := TypeDesc{
		Kind:  TypeKindArray,
		Name:  "array",
		Element: &TypeDesc{Kind: TypeKindScalar, Name: "int"},
		Key:     &TypeDesc{Kind: TypeKindScalar, Name: "string"},
		Value:   &TypeDesc{Kind: TypeKindScalar, Name: "bool"},
	}
	cloned := CloneTypeDesc(original)

	if cloned.Kind != TypeKindArray || cloned.Name != "array" {
		t.Fatal("basic fields not preserved")
	}
	if cloned.Element == nil || cloned.Element.Name != "int" {
		t.Fatal("element not cloned")
	}
	if cloned.Key == nil || cloned.Key.Name != "string" {
		t.Fatal("key not cloned")
	}
	if cloned.Value == nil || cloned.Value.Name != "bool" {
		t.Fatal("value not cloned")
	}

	// Pointers must be distinct
	if cloned.Element == original.Element {
		t.Fatal("element pointer not deep copied")
	}
	if cloned.Key == original.Key {
		t.Fatal("key pointer not deep copied")
	}
	if cloned.Value == original.Value {
		t.Fatal("value pointer not deep copied")
	}

	// Mutation isolation
	cloned.Element.Name = "mutated"
	if original.Element.Name != "int" {
		t.Fatal("element mutation leaked")
	}
}

func TestCloneTypeDesc_NestedRecursion(t *testing.T) {
	inner := &TypeDesc{Kind: TypeKindScalar, Name: "int"}
	original := TypeDesc{
		Kind:    TypeKindArray,
		Name:    "array",
		Element: inner,
	}
	cloned := CloneTypeDesc(original)
	if cloned.Element == inner {
		t.Fatal("nested element not deep copied")
	}
}

func TestCloneTypeDescPtr_NilReturnsNil(t *testing.T) {
	if got := CloneTypeDescPtr(nil); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestCloneTypeDescPtr_NonNilDeepCopies(t *testing.T) {
	original := &TypeDesc{Kind: TypeKindScalar, Name: "string"}
	cloned := CloneTypeDescPtr(original)
	if cloned == nil {
		t.Fatal("expected non-nil clone")
	}
	if cloned == original {
		t.Fatal("clone must be a distinct pointer")
	}
	if cloned.Name != "string" {
		t.Fatalf("expected name preserved, got %q", cloned.Name)
	}
}

func TestCloneMessageStreamingDesc_NilReturnsNil(t *testing.T) {
	if got := CloneMessageStreamingDesc(nil); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestCloneMessageStreamingDesc_DeepCopies(t *testing.T) {
	original := &MessageStreamingDesc{
		Start: &TypeDesc{Kind: TypeKindScalar, Name: "string"},
		Delta: &TypeDesc{Kind: TypeKindScalar, Name: "int"},
		End:   &TypeDesc{Kind: TypeKindScalar, Name: "bool"},
	}
	cloned := CloneMessageStreamingDesc(original)
	if cloned == nil {
		t.Fatal("expected non-nil clone")
	}
	if cloned.Start == original.Start || cloned.Start.Name != "string" {
		t.Fatal("start not deep copied")
	}
	if cloned.Delta == original.Delta || cloned.Delta.Name != "int" {
		t.Fatal("delta not deep copied")
	}
	if cloned.End == original.End || cloned.End.Name != "bool" {
		t.Fatal("end not deep copied")
	}
}

func TestCloneCallableDesc_EmptyReturnsAndNoStreaming(t *testing.T) {
	original := CallableDesc{
		Name:       "touch",
		Parameters: []ParameterDesc{{Name: "x", Type: TypeDesc{Kind: TypeKindScalar, Name: "int"}}},
		Mode:       CallableModeUnary,
	}
	cloned := CloneCallableDesc(original)
	if cloned.Name != "touch" {
		t.Fatalf("expected name preserved, got %q", cloned.Name)
	}
	if len(cloned.Returns) != 0 {
		t.Fatalf("expected empty returns, got %d", len(cloned.Returns))
	}
	if cloned.Streaming != nil {
		t.Fatal("expected no streaming")
	}
	if cloned.Mode != CallableModeUnary {
		t.Fatalf("expected mode unary, got %q", cloned.Mode)
	}
}

func TestCloneCallableDesc_WithStreamingAndReturns(t *testing.T) {
	original := CallableDesc{
		Name:       "stream",
		Parameters: []ParameterDesc{{Name: "x", Type: TypeDesc{Kind: TypeKindScalar, Name: "int"}}},
		Returns:    []TypeDesc{{Kind: TypeKindScalar, Name: "string"}},
		Mode:       CallableModeStreaming,
		Streaming: &StreamingCallableDesc{
			Next:  &TypeDesc{Kind: TypeKindScalar, Name: "int"},
			Final: &TypeDesc{Kind: TypeKindScalar, Name: "bool"},
			Message: &MessageStreamingDesc{
				Start: &TypeDesc{Kind: TypeKindScalar, Name: "string"},
			},
		},
	}
	cloned := CloneCallableDesc(original)
	if cloned.Name != "stream" {
		t.Fatalf("expected name preserved, got %q", cloned.Name)
	}
	if len(cloned.Returns) != 1 || cloned.Returns[0].Name != "string" {
		t.Fatal("returns not cloned")
	}
	if cloned.Streaming == nil {
		t.Fatal("expected streaming")
	}
	if cloned.Streaming.Next == original.Streaming.Next || cloned.Streaming.Next.Name != "int" {
		t.Fatal("next not deep copied")
	}
	if cloned.Streaming.Final == original.Streaming.Final || cloned.Streaming.Final.Name != "bool" {
		t.Fatal("final not deep copied")
	}
	if cloned.Streaming.Message == original.Streaming.Message || cloned.Streaming.Message.Start == original.Streaming.Message.Start {
		t.Fatal("message not deep copied")
	}
	if cloned.Mode != CallableModeStreaming {
		t.Fatalf("expected mode streaming, got %q", cloned.Mode)
	}
}

func TestCloneCallableDesc_EmptyModeDefaultsToUnary(t *testing.T) {
	original := CallableDesc{Name: "default"}
	cloned := CloneCallableDesc(original)
	if cloned.Mode != CallableModeUnary {
		t.Fatalf("expected default unary mode, got %q", cloned.Mode)
	}
}
