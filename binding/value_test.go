package binding_test

import (
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/schema"
)

// ============================================================================
// NewInvocationOutcome tests
// ============================================================================

func TestNewInvocationOutcome_ValuePayload(t *testing.T) {
	desc, err := schema.DescribeGoFunction("greet", greet)
	if err != nil {
		t.Fatalf("DescribeGoFunction: %v", err)
	}

	result, err := binding.DescribeInvocationResult(desc, binding.InvocationStageUnary)
	if err != nil {
		t.Fatalf("DescribeInvocationResult: %v", err)
	}

	outcome, err := binding.NewInvocationOutcome(result, "Alice")
	if err != nil {
		t.Fatalf("NewInvocationOutcome: %v", err)
	}

	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value kind, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil {
		t.Fatal("expected non-nil payload")
	}
	if outcome.Payload.Value != "Alice" {
		t.Fatalf("expected payload Alice, got %v", outcome.Payload.Value)
	}
	if outcome.Payload.Kind != binding.ValueCarrierScalar {
		t.Fatalf("expected scalar carrier, got %s", outcome.Payload.Kind)
	}
}

func TestNewInvocationOutcome_ProjectsListAndObjectPayloadKinds(t *testing.T) {
	// List payload
	listResult := binding.InvocationResultDesc{
		Callable: "items",
		Mode:     schema.CallableModeUnary,
		Stage:    binding.InvocationStageUnary,
		Kind:     binding.InvocationResultValue,
		Value:    &schema.TypeDesc{Kind: schema.TypeKindArray, Name: "array", Element: &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
	}

	listOutcome, err := binding.NewInvocationOutcome(listResult, []string{"a", "b"})
	if err != nil {
		t.Fatalf("NewInvocationOutcome list: %v", err)
	}
	if listOutcome.Payload.Kind != binding.ValueCarrierList {
		t.Fatalf("expected list carrier, got %s", listOutcome.Payload.Kind)
	}

	// Object payload
	objResult := binding.InvocationResultDesc{
		Callable: "obj",
		Mode:     schema.CallableModeUnary,
		Stage:    binding.InvocationStageUnary,
		Kind:     binding.InvocationResultValue,
		Value:    &schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "struct"},
	}

	objOutcome, err := binding.NewInvocationOutcome(objResult, map[string]any{"key": "val"})
	if err != nil {
		t.Fatalf("NewInvocationOutcome object: %v", err)
	}
	if objOutcome.Payload.Kind != binding.ValueCarrierObject {
		t.Fatalf("expected object carrier, got %s", objOutcome.Payload.Kind)
	}
}

func TestNewInvocationOutcome_AcceptsOrderedMapPayloadForMapSchema(t *testing.T) {
	om := schema.NewOrderedMap[string, int]()
	om.Set("a", 1)
	om.Set("b", 2)

	mapResult := binding.InvocationResultDesc{
		Callable: "mapResult",
		Mode:     schema.CallableModeUnary,
		Stage:    binding.InvocationStageUnary,
		Kind:     binding.InvocationResultValue,
		Value: &schema.TypeDesc{
			Kind:  schema.TypeKindMap,
			Name:  "map",
			Key:   &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"},
			Value: &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"},
		},
	}

	outcome, err := binding.NewInvocationOutcome(mapResult, om)
	if err != nil {
		t.Fatalf("NewInvocationOutcome OrderedMap: %v", err)
	}
	if outcome.Payload == nil {
		t.Fatal("expected non-nil payload for OrderedMap")
	}
}

func TestNewInvocationOutcome_RejectsOrderedMapPayloadWithMismatchedValueSchema(t *testing.T) {
	om := schema.NewOrderedMap[string, int]()
	om.Set("a", 1)

	mapResult := binding.InvocationResultDesc{
		Callable: "badMap",
		Mode:     schema.CallableModeUnary,
		Stage:    binding.InvocationStageUnary,
		Kind:     binding.InvocationResultValue,
		Value: &schema.TypeDesc{
			Kind:  schema.TypeKindMap,
			Name:  "map",
			Key:   &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"},
			Value: &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}, // expects string values, but OrderedMap has int values
		},
	}

	_, err := binding.NewInvocationOutcome(mapResult, om)
	if err == nil {
		t.Fatal("expected error for mismatched OrderedMap value schema, got nil")
	}
}

func TestNewInvocationOutcome_RejectsPayloadKindMismatch(t *testing.T) {
	// Schema says string return, but payload is an int
	stringResult := binding.InvocationResultDesc{
		Callable: "greet",
		Mode:     schema.CallableModeUnary,
		Stage:    binding.InvocationStageUnary,
		Kind:     binding.InvocationResultValue,
		Value:    &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"},
	}

	_, err := binding.NewInvocationOutcome(stringResult, 42)
	if err == nil {
		t.Fatal("expected error for payload kind mismatch, got nil")
	}
}

func TestNewInvocationOutcome_RejectsMissingPayloadForNonVoidResult(t *testing.T) {
	stringResult := binding.InvocationResultDesc{
		Callable: "greet",
		Mode:     schema.CallableModeUnary,
		Stage:    binding.InvocationStageUnary,
		Kind:     binding.InvocationResultValue,
		Value:    &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"},
	}

	_, err := binding.NewInvocationOutcome(stringResult, nil)
	if err == nil {
		t.Fatal("expected error for missing payload on non-void result, got nil")
	}
}

func TestNewInvocationOutcome_RejectsPayloadForVoidResult(t *testing.T) {
	voidResult := binding.InvocationResultDesc{
		Callable: "touch",
		Mode:     schema.CallableModeUnary,
		Stage:    binding.InvocationStageUnary,
		Kind:     binding.InvocationResultValue,
		Value:    nil, // void
	}

	_, err := binding.NewInvocationOutcome(voidResult, "unexpected")
	if err == nil {
		t.Fatal("expected error for payload on void result, got nil")
	}
}

func TestNewInvocationOutcome_RejectsPayloadForErrorResult(t *testing.T) {
	errorResult := binding.InvocationResultDesc{
		Callable: "fail",
		Mode:     schema.CallableModeUnary,
		Stage:    binding.InvocationStageUnary,
		Kind:     binding.InvocationResultError,
		Value:    nil,
		Error:    &binding.InvocationErrorDesc{Callable: "fail", Stage: binding.InvocationStageUnary, Message: "boom"},
	}

	_, err := binding.NewInvocationOutcome(errorResult, "unexpected")
	if err == nil {
		t.Fatal("expected error for payload on error result, got nil")
	}
}

func TestNewInvocationOutcome_AllowsNilPayloadForVoidResult(t *testing.T) {
	voidResult := binding.InvocationResultDesc{
		Callable: "touch",
		Mode:     schema.CallableModeUnary,
		Stage:    binding.InvocationStageUnary,
		Kind:     binding.InvocationResultValue,
		Value:    nil, // void
	}

	outcome, err := binding.NewInvocationOutcome(voidResult, nil)
	if err != nil {
		t.Fatalf("NewInvocationOutcome void: %v", err)
	}
	if outcome.Payload != nil {
		t.Fatalf("expected nil payload for void result, got %+v", outcome.Payload)
	}
}

func TestNewInvocationOutcome_AllowsNilPayloadForNullableReferenceResult(t *testing.T) {
	cases := []struct {
		name string
		td   schema.TypeDesc
	}{
		{"map", schema.TypeDesc{Kind: schema.TypeKindMap, Key: &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}, Value: &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "any"}}},
		{"array", schema.TypeDesc{Kind: schema.TypeKindArray, Element: &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}}},
		{"struct", schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "Intent"}},
		{"media", schema.TypeDesc{Kind: schema.TypeKindMedia}},
		{"any", schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "any"}},
		{"object", schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "object"}},
	}
	for _, tc := range cases {
		result := binding.InvocationResultDesc{
			Callable: "think",
			Mode:     schema.CallableModeUnary,
			Stage:    binding.InvocationStageUnary,
			Kind:     binding.InvocationResultValue,
			Value:    &tc.td,
		}
		outcome, err := binding.NewInvocationOutcome(result, nil)
		if err != nil {
			t.Errorf("%s: expected nil payload accepted for nullable return, got error: %v", tc.name, err)
		}
		if outcome.Payload != nil {
			t.Errorf("%s: expected nil payload carrier, got %+v", tc.name, outcome.Payload)
		}
	}
}

func TestNewInvocationOutcome_RejectsNilPayloadForValueScalarResult(t *testing.T) {
	cases := []struct {
		name string
		td   schema.TypeDesc
	}{
		{"int", schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}},
		{"bool", schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "bool"}},
		{"string", schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
		{"enum", schema.TypeDesc{Kind: schema.TypeKindEnum, Name: "Color"}},
	}
	for _, tc := range cases {
		result := binding.InvocationResultDesc{
			Callable: "v",
			Mode:     schema.CallableModeUnary,
			Stage:    binding.InvocationStageUnary,
			Kind:     binding.InvocationResultValue,
			Value:    &tc.td,
		}
		_, err := binding.NewInvocationOutcome(result, nil)
		if err == nil {
			t.Errorf("%s: expected rejection of nil payload for non-nullable return", tc.name)
		}
	}
}
