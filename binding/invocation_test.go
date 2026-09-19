package binding_test

import (
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/diagnostics"
	"github.com/qomos-w/spore/schema"
)

// ============================================================================
// DescribeInvocationResult tests
// ============================================================================

func TestDescribeInvocationResult_ForUnaryCallable(t *testing.T) {
	desc, err := schema.DescribeGoFunction("greet", greet)
	if err != nil {
		t.Fatalf("DescribeGoFunction: %v", err)
	}

	result, err := binding.DescribeInvocationResult(desc, binding.InvocationStageUnary)
	if err != nil {
		t.Fatalf("DescribeInvocationResult: %v", err)
	}

	if result.Callable != "greet" {
		t.Fatalf("expected callable greet, got %s", result.Callable)
	}
	if result.Mode != schema.CallableModeUnary {
		t.Fatalf("expected unary mode, got %s", result.Mode)
	}
	if result.Stage != binding.InvocationStageUnary {
		t.Fatalf("expected unary stage, got %s", result.Stage)
	}
	if result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value kind, got %s", result.Kind)
	}
	if result.Value == nil {
		t.Fatal("expected non-nil Value for non-void callable")
	}
	if result.Value.Name != "string" {
		t.Fatalf("expected string return type, got %s", result.Value.Name)
	}
	if result.Error != nil {
		t.Fatal("expected nil error descriptor for successful result")
	}
}

func TestDescribeInvocationResult_ForUnaryVoidCallable(t *testing.T) {
	desc, err := schema.DescribeGoFunction("touch", touch)
	if err != nil {
		t.Fatalf("DescribeGoFunction: %v", err)
	}

	result, err := binding.DescribeInvocationResult(desc, binding.InvocationStageUnary)
	if err != nil {
		t.Fatalf("DescribeInvocationResult: %v", err)
	}

	if result.Value != nil {
		t.Fatalf("expected nil Value for void callable, got %+v", result.Value)
	}
}

func TestDescribeInvocationResult_ForStreamingStages(t *testing.T) {
	desc, err := schema.NewStreamingCallableDesc(
		"stream",
		nil,
		&schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"},
		&schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"},
		false,
	)
	if err != nil {
		t.Fatalf("NewStreamingCallableDesc: %v", err)
	}

	// Next stage
	nextResult, err := binding.DescribeInvocationResult(desc, binding.InvocationStageNext)
	if err != nil {
		t.Fatalf("DescribeInvocationResult next: %v", err)
	}
	if nextResult.Mode != schema.CallableModeStreaming {
		t.Fatalf("expected streaming mode, got %s", nextResult.Mode)
	}
	if nextResult.Stage != binding.InvocationStageNext {
		t.Fatalf("expected next stage, got %s", nextResult.Stage)
	}
	if nextResult.Value == nil || nextResult.Value.Name != "string" {
		t.Fatalf("expected string return for next, got %+v", nextResult.Value)
	}

	// Final stage
	finalResult, err := binding.DescribeInvocationResult(desc, binding.InvocationStageFinal)
	if err != nil {
		t.Fatalf("DescribeInvocationResult final: %v", err)
	}
	if finalResult.Stage != binding.InvocationStageFinal {
		t.Fatalf("expected final stage, got %s", finalResult.Stage)
	}
	if finalResult.Value == nil || finalResult.Value.Name != "int" {
		t.Fatalf("expected int return for final, got %+v", finalResult.Value)
	}
}

func TestDescribeInvocationResult_RejectsInvalidStage(t *testing.T) {
	desc, err := schema.DescribeGoFunction("greet", greet)
	if err != nil {
		t.Fatalf("DescribeGoFunction: %v", err)
	}

	// Unary callable with next stage should fail
	_, err = binding.DescribeInvocationResult(desc, binding.InvocationStageNext)
	if err == nil {
		t.Fatal("expected error for next stage on unary callable, got nil")
	}

	// Unary callable with final stage should fail
	_, err = binding.DescribeInvocationResult(desc, binding.InvocationStageFinal)
	if err == nil {
		t.Fatal("expected error for final stage on unary callable, got nil")
	}
}

func TestDescribeStreamingEventResult_MapsEventKindsToStages(t *testing.T) {
	desc, err := schema.NewMessageStreamingCallableDesc(
		"chat",
		nil,
		&schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "MessageEvent"},
		&schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "MessageEnd"},
		&schema.MessageStreamingDesc{
			Start: &schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "MessageStart"},
			Delta: &schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "MessageDelta"},
			End:   &schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "MessageEnd"},
		},
		false,
	)
	if err != nil {
		t.Fatalf("NewMessageStreamingCallableDesc: %v", err)
	}

	start, err := binding.DescribeStreamingEventResult(desc, schema.StreamingEventStart)
	if err != nil {
		t.Fatalf("DescribeStreamingEventResult(start): %v", err)
	}
	if start.Stage != binding.InvocationStageNext || start.Value == nil || start.Value.Name != "MessageStart" {
		t.Fatalf("unexpected start projection: %+v", start)
	}

	delta, err := binding.DescribeStreamingEventResult(desc, schema.StreamingEventDelta)
	if err != nil {
		t.Fatalf("DescribeStreamingEventResult(delta): %v", err)
	}
	if delta.Stage != binding.InvocationStageNext || delta.Value == nil || delta.Value.Name != "MessageDelta" {
		t.Fatalf("unexpected delta projection: %+v", delta)
	}

	end, err := binding.DescribeStreamingEventResult(desc, schema.StreamingEventEnd)
	if err != nil {
		t.Fatalf("DescribeStreamingEventResult(end): %v", err)
	}
	if end.Stage != binding.InvocationStageFinal || end.Value == nil || end.Value.Name != "MessageEnd" {
		t.Fatalf("unexpected end projection: %+v", end)
	}
}

func TestDescribeStreamingEventResult_ClonesValue(t *testing.T) {
	desc, err := schema.NewMessageStreamingCallableDesc(
		"chat",
		nil,
		&schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "MessageEvent"},
		&schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "MessageEnd"},
		&schema.MessageStreamingDesc{
			Delta: &schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "MessageDelta"},
		},
		false,
	)
	if err != nil {
		t.Fatalf("NewMessageStreamingCallableDesc: %v", err)
	}

	result, err := binding.DescribeStreamingEventResult(desc, schema.StreamingEventDelta)
	if err != nil {
		t.Fatalf("DescribeStreamingEventResult: %v", err)
	}
	result.Value.Name = "Mutated"
	if desc.Streaming.Message.Delta.Name != "MessageDelta" {
		t.Fatalf("expected original descriptor to remain unchanged, got %q", desc.Streaming.Message.Delta.Name)
	}
}

func TestDescribeStreamingEventResult_RejectsMissingMessageProtocol(t *testing.T) {
	desc, err := schema.NewStreamingCallableDesc(
		"stream",
		nil,
		&schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"},
		&schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"},
		false,
	)
	if err != nil {
		t.Fatalf("NewStreamingCallableDesc: %v", err)
	}

	_, err = binding.DescribeStreamingEventResult(desc, schema.StreamingEventDelta)
	if err == nil {
		t.Fatal("expected error for streaming callable without message protocol, got nil")
	}
}

func TestDescribeStreamingEventResult_RejectsUnaryCallable(t *testing.T) {
	desc, err := schema.DescribeGoFunction("greet", greet)
	if err != nil {
		t.Fatalf("DescribeGoFunction: %v", err)
	}

	_, err = binding.DescribeStreamingEventResult(desc, schema.StreamingEventDelta)
	if err == nil {
		t.Fatal("expected error for unary callable, got nil")
	}
}

func TestDescribeStreamingEventResult_RejectsUndefinedOrUnknownEvent(t *testing.T) {
	desc, err := schema.NewMessageStreamingCallableDesc(
		"chat",
		nil,
		&schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "MessageEvent"},
		&schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "MessageEnd"},
		&schema.MessageStreamingDesc{
			Delta: &schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "MessageDelta"},
		},
		false,
	)
	if err != nil {
		t.Fatalf("NewMessageStreamingCallableDesc: %v", err)
	}

	_, err = binding.DescribeStreamingEventResult(desc, schema.StreamingEventStart)
	if err == nil {
		t.Fatal("expected error for undefined start event, got nil")
	}

	_, err = binding.DescribeStreamingEventResult(desc, schema.StreamingEventKind("bogus"))
	if err == nil {
		t.Fatal("expected error for unknown event kind, got nil")
	}
}

// ============================================================================
// NewInvocationErrorDesc tests
// ============================================================================

func TestNewInvocationErrorDesc_ProjectsInvocationContext(t *testing.T) {
	desc, err := schema.DescribeGoFunction("greet", greet)
	if err != nil {
		t.Fatalf("DescribeGoFunction: %v", err)
	}

	result, err := binding.NewInvocationErrorDesc(desc, binding.InvocationStageUnary, "something went wrong")
	if err != nil {
		t.Fatalf("NewInvocationErrorDesc: %v", err)
	}

	if result.Callable != "greet" {
		t.Fatalf("expected callable greet, got %s", result.Callable)
	}
	if result.Mode != schema.CallableModeUnary {
		t.Fatalf("expected unary mode, got %s", result.Mode)
	}
	if result.Stage != binding.InvocationStageUnary {
		t.Fatalf("expected unary stage, got %s", result.Stage)
	}
	if result.Kind != binding.InvocationResultError {
		t.Fatalf("expected error kind, got %s", result.Kind)
	}
	if result.Value != nil {
		t.Fatalf("expected nil Value for error result, got %+v", result.Value)
	}
	if result.Error == nil {
		t.Fatal("expected error descriptor")
	}
	if result.Error.Callable != "greet" {
		t.Fatalf("expected error callable greet, got %s", result.Error.Callable)
	}
	if result.Error.Stage != binding.InvocationStageUnary {
		t.Fatalf("expected error stage unary, got %s", result.Error.Stage)
	}
	if result.Error.Message != "something went wrong" {
		t.Fatalf("expected error message 'something went wrong', got %s", result.Error.Message)
	}
}

func TestNewInvocationErrorDescWithDiagnostic_ClonesEnvelope(t *testing.T) {
	desc, err := schema.DescribeGoFunction("greet", greet)
	if err != nil {
		t.Fatalf("DescribeGoFunction: %v", err)
	}
	diag := diagnostics.Descriptor{
		Category: diagnostics.CategoryRuntime,
		Code:     "division_by_zero",
		Message:  "boom",
		Path:     "script/body",
		Identity: "entity:1",
		Span:     diagnostics.Span{Start: diagnostics.Position{Line: 3, Column: 4}, End: diagnostics.Position{Line: 3, Column: 4}},
		Stack:    []diagnostics.Frame{{Callable: "greet", Stage: string(binding.InvocationStageUnary)}},
		Cause:    &diagnostics.Descriptor{Category: diagnostics.CategoryHost, Message: "inner cause"},
	}
	result, err := binding.NewInvocationErrorDescWithDiagnostic(desc, binding.InvocationStageUnary, diag)
	if err != nil {
		t.Fatalf("NewInvocationErrorDescWithDiagnostic: %v", err)
	}
	diag.Stack[0].Callable = "mutated"
	diag.Cause.Message = "changed"
	if result.Error == nil {
		t.Fatal("expected error descriptor")
	}
	if result.Error.Path != "script/body" || result.Error.Identity != "entity:1" {
		t.Fatalf("expected path/identity copied, got %+v", result.Error)
	}
	if result.Error.Span.Start.Line != 3 {
		t.Fatalf("expected span copied, got %+v", result.Error.Span)
	}
	if result.Error.Stack[0].Callable != "greet" {
		t.Fatalf("expected stack clone, got %+v", result.Error.Stack)
	}
	if result.Error.Cause == nil || result.Error.Cause.Message != "inner cause" {
		t.Fatalf("expected cause clone, got %+v", result.Error.Cause)
	}
	env := result.Error.Envelope()
	if env["code"] != "division_by_zero" || env["path"] != "script/body" || env["identity"] != "entity:1" {
		t.Fatalf("unexpected error envelope: %+v", env)
	}
	full := result.Envelope()
	errEnv, ok := full["error"].(map[string]any)
	if !ok || errEnv["code"] != "division_by_zero" {
		t.Fatalf("unexpected result envelope: %+v", full)
	}
}

func TestValidateInvocationStage_ReturnsDiagnosticPath(t *testing.T) {
	desc, err := schema.DescribeGoFunction("greet", greet)
	if err != nil {
		t.Fatalf("DescribeGoFunction: %v", err)
	}
	err = binding.ValidateInvocationStage(desc, binding.InvocationStageNext)
	if err == nil {
		t.Fatal("expected invalid stage error")
	}
	pather, ok := err.(interface{ DiagnosticPath() string })
	if !ok || pather.DiagnosticPath() != "binding/invocation/stage" {
		t.Fatalf("expected binding/invocation/stage path, got %v", err)
	}
}

func TestNewInvocationErrorDesc_RejectsInvalidStage(t *testing.T) {
	desc, err := schema.DescribeGoFunction("greet", greet)
	if err != nil {
		t.Fatalf("DescribeGoFunction: %v", err)
	}

	_, err = binding.NewInvocationErrorDesc(desc, binding.InvocationStageNext, "bad stage")
	if err == nil {
		t.Fatal("expected error for next stage on unary callable, got nil")
	}
}
