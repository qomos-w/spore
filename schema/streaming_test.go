package schema_test

import (
	"testing"

	"github.com/qomos-w/spore/schema"
)

func TestCanonicalMessageStreamingShapes(t *testing.T) {
	start := schema.CanonicalMessageStartObject()
	if start.Name != schema.MessageStartName || len(start.Fields) != 2 {
		t.Fatalf("unexpected canonical start object: %+v", start)
	}
	if start.Fields[0].Name != "Role" || start.Fields[0].Type.Name != "string" {
		t.Fatalf("unexpected start Role field: %+v", start.Fields[0])
	}
	if start.Fields[1].Name != "Meta" || start.Fields[1].Type.Kind != schema.TypeKindMap {
		t.Fatalf("unexpected start Meta field: %+v", start.Fields[1])
	}

	delta := schema.CanonicalMessageDeltaObject()
	if delta.Name != schema.MessageDeltaName || len(delta.Fields) != 2 {
		t.Fatalf("unexpected canonical delta object: %+v", delta)
	}
	if delta.Fields[0].Name != "Text" || delta.Fields[0].Type.Name != "string" {
		t.Fatalf("unexpected delta Text field: %+v", delta.Fields[0])
	}

	end := schema.CanonicalMessageEndObject()
	if end.Name != schema.MessageEndName || len(end.Fields) != 2 {
		t.Fatalf("unexpected canonical end object: %+v", end)
	}
	if end.Fields[0].Name != "Reason" || end.Fields[0].Type.Name != "string" {
		t.Fatalf("unexpected end Reason field: %+v", end.Fields[0])
	}

	event := schema.CanonicalMessageStreamEventObject()
	if event.Name != schema.MessageStreamEventName || len(event.Fields) != 3 {
		t.Fatalf("unexpected canonical event object: %+v", event)
	}
	if event.Fields[0].Name != "Kind" || event.Fields[0].Type.Name != "string" {
		t.Fatalf("unexpected event Kind field: %+v", event.Fields[0])
	}
	if event.Fields[1].Type.ClassName != schema.MessageStartName {
		t.Fatalf("unexpected event Start field: %+v", event.Fields[1])
	}
	if event.Fields[2].Type.ClassName != schema.MessageDeltaName {
		t.Fatalf("unexpected event Delta field: %+v", event.Fields[2])
	}
}

func TestNewStreamingCallableDesc_CreatesDescriptor(t *testing.T) {
	desc, err := schema.NewStreamingCallableDesc(
		"stream",
		[]schema.ParameterDesc{{Name: "x", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}}},
		&schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"},
		&schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "bool"},
		true,
	)
	if err != nil {
		t.Fatalf("NewStreamingCallableDesc: %v", err)
	}
	if desc.Name != "stream" {
		t.Fatalf("expected name stream, got %q", desc.Name)
	}
	if desc.Mode != schema.CallableModeStreaming {
		t.Fatalf("expected streaming mode, got %s", desc.Mode)
	}
	if desc.Streaming == nil || desc.Streaming.Next == nil || desc.Streaming.Final == nil {
		t.Fatal("expected streaming protocol with next and final")
	}
	if !desc.HasError {
		t.Fatal("expected HasError true")
	}
}

func TestNewStreamingCallableDesc_RejectsInvalid(t *testing.T) {
	// Empty name should fail validation
	_, err := schema.NewStreamingCallableDesc("", nil, nil, nil, false)
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestNewMessageStreamingDesc_CreatesDescriptor(t *testing.T) {
	desc, err := schema.NewMessageStreamingDesc(
		&schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "Start"},
		&schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "Delta"},
		&schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "End"},
	)
	if err != nil {
		t.Fatalf("NewMessageStreamingDesc: %v", err)
	}
	if desc == nil {
		t.Fatal("expected non-nil descriptor")
	}
	if desc.Start == nil || desc.Start.Name != "Start" {
		t.Fatalf("unexpected start: %+v", desc.Start)
	}
	if desc.Delta == nil || desc.Delta.Name != "Delta" {
		t.Fatalf("unexpected delta: %+v", desc.Delta)
	}
	if desc.End == nil || desc.End.Name != "End" {
		t.Fatalf("unexpected end: %+v", desc.End)
	}
}

func TestNewMessageStreamingDesc_RejectsAllNil(t *testing.T) {
	_, err := schema.NewMessageStreamingDesc(nil, nil, nil)
	if err == nil {
		t.Fatal("expected error when all fields are nil")
	}
}

func TestNewCanonicalMessageStreamingCallableDesc(t *testing.T) {
	desc, err := schema.NewCanonicalMessageStreamingCallableDesc(
		"chat",
		[]schema.ParameterDesc{{Name: "prompt", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}}},
		true,
	)
	if err != nil {
		t.Fatalf("NewCanonicalMessageStreamingCallableDesc: %v", err)
	}
	if desc.Mode != schema.CallableModeStreaming {
		t.Fatalf("expected streaming mode, got %s", desc.Mode)
	}
	if desc.Streaming == nil || desc.Streaming.Message == nil {
		t.Fatal("expected canonical message streaming descriptor")
	}
	if desc.Streaming.Next == nil || desc.Streaming.Next.ClassName != schema.MessageStreamEventName {
		t.Fatalf("unexpected next type: %+v", desc.Streaming.Next)
	}
	if desc.Streaming.Final == nil || desc.Streaming.Final.ClassName != schema.MessageEndName {
		t.Fatalf("unexpected final type: %+v", desc.Streaming.Final)
	}
	if desc.Streaming.Message.Start == nil || desc.Streaming.Message.Start.ClassName != schema.MessageStartName {
		t.Fatalf("unexpected start type: %+v", desc.Streaming.Message.Start)
	}
	if desc.Streaming.Message.Delta == nil || desc.Streaming.Message.Delta.ClassName != schema.MessageDeltaName {
		t.Fatalf("unexpected delta type: %+v", desc.Streaming.Message.Delta)
	}
	if desc.Streaming.Message.End == nil || desc.Streaming.Message.End.ClassName != schema.MessageEndName {
		t.Fatalf("unexpected end type: %+v", desc.Streaming.Message.End)
	}
}

func TestCanonicalMessageStreamingDesc_ClonesReturnedTypes(t *testing.T) {
	message := schema.CanonicalMessageStreamingDesc()
	message.Start.ClassName = "Mutated"

	fresh := schema.CanonicalMessageStreamingDesc()
	if fresh.Start.ClassName != schema.MessageStartName {
		t.Fatalf("expected canonical descriptor to remain isolated, got %q", fresh.Start.ClassName)
	}
}

func TestNewMessageStreamingCallableDesc_CreatesDescriptor(t *testing.T) {
	desc, err := schema.NewMessageStreamingCallableDesc(
		"chat",
		[]schema.ParameterDesc{{Name: "prompt", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}}},
		&schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "MessageEvent"},
		&schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "MessageEnd"},
		&schema.MessageStreamingDesc{
			Start: &schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "MessageStart"},
			Delta: &schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "MessageDelta"},
			End:   &schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "MessageEnd"},
		},
		true,
	)
	if err != nil {
		t.Fatalf("NewMessageStreamingCallableDesc: %v", err)
	}
	if desc.Mode != schema.CallableModeStreaming {
		t.Fatalf("expected streaming mode, got %s", desc.Mode)
	}
	if desc.Streaming == nil || desc.Streaming.Message == nil {
		t.Fatal("expected message streaming descriptor")
	}
	if desc.Streaming.Message.Start == nil || desc.Streaming.Message.Start.Name != "MessageStart" {
		t.Fatalf("unexpected start schema: %+v", desc.Streaming.Message.Start)
	}
}

func TestValidateCallableDesc_RejectsEmptyMessageStreamingDesc(t *testing.T) {
	desc := schema.CallableDesc{
		Name: "chat",
		Mode: schema.CallableModeStreaming,
		Streaming: &schema.StreamingCallableDesc{
			Next:    &schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "MessageEvent"},
			Message: &schema.MessageStreamingDesc{},
		},
	}
	if err := schema.ValidateCallableDesc(desc); err == nil {
		t.Fatal("expected error for empty message streaming descriptor, got nil")
	}
}

func TestValidateCallableDesc_RejectsStartOrDeltaWithoutNext(t *testing.T) {
	desc := schema.CallableDesc{
		Name: "chat",
		Mode: schema.CallableModeStreaming,
		Streaming: &schema.StreamingCallableDesc{
			Final: &schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "MessageEnd"},
			Message: &schema.MessageStreamingDesc{
				Delta: &schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "MessageDelta"},
			},
		},
	}
	if err := schema.ValidateCallableDesc(desc); err == nil {
		t.Fatal("expected error for delta without next schema, got nil")
	}
}

func TestValidateCallableDesc_RejectsEndWithoutFinal(t *testing.T) {
	desc := schema.CallableDesc{
		Name: "chat",
		Mode: schema.CallableModeStreaming,
		Streaming: &schema.StreamingCallableDesc{
			Next: &schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "MessageEvent"},
			Message: &schema.MessageStreamingDesc{
				End: &schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "MessageEnd"},
			},
		},
	}
	if err := schema.ValidateCallableDesc(desc); err == nil {
		t.Fatal("expected error for end without final schema, got nil")
	}
}

func TestValidateCallableDesc_RejectsEmptyName(t *testing.T) {
	desc := schema.CallableDesc{Name: ""}
	if err := schema.ValidateCallableDesc(desc); err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestValidateCallableDesc_RejectsStreamingWithReturns(t *testing.T) {
	desc := schema.CallableDesc{
		Name:    "stream",
		Mode:    schema.CallableModeStreaming,
		Returns: []schema.TypeDesc{{Kind: schema.TypeKindScalar, Name: "int"}},
		Streaming: &schema.StreamingCallableDesc{
			Next: &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"},
		},
	}
	if err := schema.ValidateCallableDesc(desc); err == nil {
		t.Fatal("expected error for streaming with returns")
	}
}

func TestValidateCallableDesc_RejectsStreamingWithoutNextOrFinal(t *testing.T) {
	desc := schema.CallableDesc{
		Name: "stream",
		Mode: schema.CallableModeStreaming,
		Streaming: &schema.StreamingCallableDesc{
			Message: &schema.MessageStreamingDesc{
				Start: &schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "Start"},
			},
		},
	}
	if err := schema.ValidateCallableDesc(desc); err == nil {
		t.Fatal("expected error for streaming without next or final")
	}
}

func TestValidateCallableDesc_RejectsInvalidMode(t *testing.T) {
	desc := schema.CallableDesc{
		Name: "bad",
		Mode: schema.CallableMode("unknown"),
	}
	if err := schema.ValidateCallableDesc(desc); err == nil {
		t.Fatal("expected error for invalid mode")
	}
}

func TestNewMessageStreamingCallableDesc_RejectsInvalid(t *testing.T) {
	_, err := schema.NewMessageStreamingCallableDesc(
		"",
		nil,
		nil,
		nil,
		nil,
		false,
	)
	if err == nil {
		t.Fatal("expected error for invalid descriptor")
	}
}

func TestCloneCallableDesc_ClonesMessageStreamingDescriptor(t *testing.T) {
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

	cloned := schema.CloneCallableDesc(desc)
	cloned.Streaming.Message.Delta.Name = "Mutated"
	if desc.Streaming.Message.Delta.Name != "MessageDelta" {
		t.Fatalf("expected original delta schema to remain unchanged, got %q", desc.Streaming.Message.Delta.Name)
	}
}

func TestNewMessageStreamingCallableDesc_ClonesInputs(t *testing.T) {
	params := []schema.ParameterDesc{{Name: "prompt", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}}}
	delta := &schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "MessageDelta"}
	message := &schema.MessageStreamingDesc{Delta: delta}

	desc, err := schema.NewMessageStreamingCallableDesc(
		"chat",
		params,
		&schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "MessageEvent"},
		nil,
		message,
		false,
	)
	if err != nil {
		t.Fatalf("NewMessageStreamingCallableDesc: %v", err)
	}

	params[0].Name = "mutated"
	delta.Name = "Mutated"
	message.Delta.Name = "MutatedAgain"
	if desc.Parameters[0].Name != "prompt" {
		t.Fatalf("expected params to be cloned, got %q", desc.Parameters[0].Name)
	}
	if desc.Streaming.Message.Delta.Name != "MessageDelta" {
		t.Fatalf("expected message descriptor to be cloned, got %q", desc.Streaming.Message.Delta.Name)
	}
}
