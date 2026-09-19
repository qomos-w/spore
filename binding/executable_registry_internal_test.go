package binding

import (
	"testing"

	"github.com/qomos-w/spore/schema"
)

func TestCallableDescriptorsEqual(t *testing.T) {
	base := schema.CallableDesc{
		Name:       "test",
		Mode:       schema.CallableModeUnary,
		Parameters: []schema.ParameterDesc{{Name: "x", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}}},
		Returns:    []schema.TypeDesc{{Kind: schema.TypeKindScalar, Name: "string"}},
	}

	if !callableDescriptorsEqual(base, base) {
		t.Fatal("expected descriptor to equal itself")
	}

	// different mode
	streaming := base
	streaming.Mode = schema.CallableModeStreaming
	streaming.Streaming = &schema.StreamingCallableDesc{
		Next: &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"},
	}
	if callableDescriptorsEqual(base, streaming) {
		t.Fatal("expected different modes to differ")
	}

	// different name
	other := base
	other.Name = "other"
	if callableDescriptorsEqual(base, other) {
		t.Fatal("expected different names to differ")
	}

	// different hasError
	other = base
	other.HasError = true
	if callableDescriptorsEqual(base, other) {
		t.Fatal("expected different HasError to differ")
	}

	// different parameter count
	other = base
	other.Parameters = []schema.ParameterDesc{}
	if callableDescriptorsEqual(base, other) {
		t.Fatal("expected different param count to differ")
	}

	// different return count
	other = base
	other.Returns = []schema.TypeDesc{}
	if callableDescriptorsEqual(base, other) {
		t.Fatal("expected different return count to differ")
	}

	// different parameter name
	other = base
	other.Parameters = []schema.ParameterDesc{{Name: "y", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}}}
	if callableDescriptorsEqual(base, other) {
		t.Fatal("expected different param name to differ")
	}

	// different parameter type
	other = base
	other.Parameters = []schema.ParameterDesc{{Name: "x", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}}}
	if callableDescriptorsEqual(base, other) {
		t.Fatal("expected different param type to differ")
	}

	// different return type
	other = base
	other.Returns = []schema.TypeDesc{{Kind: schema.TypeKindScalar, Name: "int"}}
	if callableDescriptorsEqual(base, other) {
		t.Fatal("expected different return type to differ")
	}

	// one nil streaming
	streaming2 := streaming
	streaming2.Streaming = nil
	if callableDescriptorsEqual(streaming, streaming2) {
		t.Fatal("expected one nil streaming to differ")
	}

	// both nil streaming
	if !callableDescriptorsEqual(base, base) {
		t.Fatal("expected both nil streaming to match")
	}

	// streaming with message protocol — base for message comparison.
	// Each variant gets its own *StreamingCallableDesc so the comparison
	// under test isolates the Message field.
	mkStreaming := func(msg *schema.MessageStreamingDesc) schema.CallableDesc {
		d := base
		d.Mode = schema.CallableModeStreaming
		d.Streaming = &schema.StreamingCallableDesc{
			Next:    &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"},
			Message: msg,
		}
		return d
	}
	noMsg := mkStreaming(nil)
	withMsg := mkStreaming(&schema.MessageStreamingDesc{
		Start: &schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "struct", ClassName: schema.MessageStartName},
		Delta: &schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "struct", ClassName: schema.MessageDeltaName},
	})
	if callableDescriptorsEqual(noMsg, withMsg) {
		t.Fatal("expected message present vs absent to differ")
	}

	// identical message protocol must match
	withMsg2 := mkStreaming(&schema.MessageStreamingDesc{
		Start: &schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "struct", ClassName: schema.MessageStartName},
		Delta: &schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "struct", ClassName: schema.MessageDeltaName},
	})
	if !callableDescriptorsEqual(withMsg, withMsg2) {
		t.Fatal("expected identical message protocols to match")
	}

	// differing only in the message Delta must differ
	withMsg3 := mkStreaming(&schema.MessageStreamingDesc{
		Start: &schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "struct", ClassName: schema.MessageStartName},
		Delta: &schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "struct", ClassName: schema.MessageEndName}, // changed delta
	})
	if callableDescriptorsEqual(withMsg, withMsg3) {
		t.Fatal("expected different message Delta to differ")
	}
}

func TestNormalizedCallableMode(t *testing.T) {
	if normalizedCallableMode(schema.CallableDesc{}) != schema.CallableModeUnary {
		t.Fatal("expected empty mode to normalize to unary")
	}
	if normalizedCallableMode(schema.CallableDesc{Mode: schema.CallableModeStreaming}) != schema.CallableModeStreaming {
		t.Fatal("expected streaming mode to remain streaming")
	}
}

func TestTypeDescPtrsEqual(t *testing.T) {
	if !typeDescPtrsEqual(nil, nil) {
		t.Fatal("expected nil == nil")
	}
	if typeDescPtrsEqual(&schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}, nil) {
		t.Fatal("expected non-nil != nil")
	}
	if !typeDescPtrsEqual(&schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}, &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}) {
		t.Fatal("expected equal values to match")
	}
}

func TestTypeDescsEqual(t *testing.T) {
	base := schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}
	if !typeDescsEqual(base, base) {
		t.Fatal("expected same desc to equal")
	}

	other := base
	other.Kind = schema.TypeKindArray
	if typeDescsEqual(base, other) {
		t.Fatal("expected different kind to differ")
	}

	other = base
	other.Name = "string"
	if typeDescsEqual(base, other) {
		t.Fatal("expected different name to differ")
	}

	other = base
	other.Element = &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}
	if typeDescsEqual(base, other) {
		t.Fatal("expected different element to differ")
	}

	other = base
	other.Key = &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}
	if typeDescsEqual(base, other) {
		t.Fatal("expected different key to differ")
	}

	other = base
	other.Value = &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "bool"}
	if typeDescsEqual(base, other) {
		t.Fatal("expected different value to differ")
	}
}
