package schema

import "fmt"

const (
	MessageStreamEventName = "MessageStreamEvent"
	MessageStartName       = "MessageStart"
	MessageDeltaName       = "MessageDelta"
	MessageEndName         = "MessageEnd"
)

// NewStreamingCallableDesc creates a validated streaming callable descriptor.
// Contract: public semantic contract — streaming callable descriptor constructor.
func NewStreamingCallableDesc(name string, params []ParameterDesc, next *TypeDesc, final *TypeDesc, hasError bool) (CallableDesc, error) {
	desc := CallableDesc{
		Name:       name,
		Parameters: CloneParameters(params),
		HasError:   hasError,
		Mode:       CallableModeStreaming,
		Streaming: &StreamingCallableDesc{
			Next:  CloneTypeDescPtr(next),
			Final: CloneTypeDescPtr(final),
		},
	}
	if err := ValidateCallableDesc(desc); err != nil {
		return CallableDesc{}, err
	}
	return desc, nil
}

// NewMessageStreamingDesc creates a validated message-style streaming descriptor.
// Contract: public semantic contract — start/delta/end event schema descriptor constructor.
func NewMessageStreamingDesc(start *TypeDesc, delta *TypeDesc, end *TypeDesc) (*MessageStreamingDesc, error) {
	desc := &MessageStreamingDesc{
		Start: CloneTypeDescPtr(start),
		Delta: CloneTypeDescPtr(delta),
		End:   CloneTypeDescPtr(end),
	}
	if err := ValidateMessageStreamingDesc(desc); err != nil {
		return nil, err
	}
	return desc, nil
}

// NewMessageStreamingCallableDesc creates a validated streaming callable descriptor
// with optional message-style start/delta/end event schemas layered over next/final.
// Contract: public semantic contract — message streaming callable descriptor constructor.
func NewMessageStreamingCallableDesc(name string, params []ParameterDesc, next *TypeDesc, final *TypeDesc, message *MessageStreamingDesc, hasError bool) (CallableDesc, error) {
	desc := CallableDesc{
		Name:       name,
		Parameters: CloneParameters(params),
		HasError:   hasError,
		Mode:       CallableModeStreaming,
		Streaming: &StreamingCallableDesc{
			Next:    CloneTypeDescPtr(next),
			Final:   CloneTypeDescPtr(final),
			Message: CloneMessageStreamingDesc(message),
		},
	}
	if err := ValidateCallableDesc(desc); err != nil {
		return CallableDesc{}, err
	}
	return desc, nil
}

func ValidateMessageStreamingDesc(desc *MessageStreamingDesc) error {
	if desc == nil {
		return nil
	}
	if desc.Start == nil && desc.Delta == nil && desc.End == nil {
		return fmt.Errorf("message streaming protocol must define start, delta, or end schema")
	}
	return nil
}

// CanonicalMessageStartType returns the canonical schema type for a stream start event.
// Contract: public semantic contract — stable start-event type descriptor.
func CanonicalMessageStartType() TypeDesc {
	return TypeDesc{Kind: TypeKindStruct, Name: "struct", ClassName: MessageStartName}
}

// CanonicalMessageDeltaType returns the canonical schema type for a stream delta event.
// Contract: public semantic contract — stable delta-event type descriptor.
func CanonicalMessageDeltaType() TypeDesc {
	return TypeDesc{Kind: TypeKindStruct, Name: "struct", ClassName: MessageDeltaName}
}

// CanonicalMessageEndType returns the canonical schema type for a stream end event.
// Contract: public semantic contract — stable end-event type descriptor.
func CanonicalMessageEndType() TypeDesc {
	return TypeDesc{Kind: TypeKindStruct, Name: "struct", ClassName: MessageEndName}
}

// CanonicalMessageStreamEventType returns the canonical schema type for the common
// next-stage event envelope used by message-style streaming callables.
// Contract: public semantic contract — stable event-envelope type descriptor.
func CanonicalMessageStreamEventType() TypeDesc {
	return TypeDesc{Kind: TypeKindStruct, Name: "struct", ClassName: MessageStreamEventName}
}

// CanonicalMessageStartObject returns the canonical object descriptor for a stream start event.
// Contract: public semantic contract — stable start-event object shape.
func CanonicalMessageStartObject() ObjectDesc {
	return ObjectDesc{
		Kind: TypeKindStruct,
		Name: MessageStartName,
		Fields: []FieldDesc{
			{Name: "Role", Type: scalarType("string")},
			{Name: "Meta", Type: mapType(scalarType("string"), scalarType("string"))},
		},
	}
}

// CanonicalMessageDeltaObject returns the canonical object descriptor for a stream delta event.
// Contract: public semantic contract — stable delta-event object shape.
func CanonicalMessageDeltaObject() ObjectDesc {
	return ObjectDesc{
		Kind: TypeKindStruct,
		Name: MessageDeltaName,
		Fields: []FieldDesc{
			{Name: "Text", Type: scalarType("string")},
			{Name: "Meta", Type: mapType(scalarType("string"), scalarType("string"))},
		},
	}
}

// CanonicalMessageEndObject returns the canonical object descriptor for a stream end event.
// Contract: public semantic contract — stable end-event object shape.
func CanonicalMessageEndObject() ObjectDesc {
	return ObjectDesc{
		Kind: TypeKindStruct,
		Name: MessageEndName,
		Fields: []FieldDesc{
			{Name: "Reason", Type: scalarType("string")},
			{Name: "Meta", Type: mapType(scalarType("string"), scalarType("string"))},
		},
	}
}

// CanonicalMessageStreamEventObject returns the canonical object descriptor for the
// common next-stage envelope used by message-style streaming callables.
// Contract: public semantic contract — stable event-envelope object shape.
func CanonicalMessageStreamEventObject() ObjectDesc {
	return ObjectDesc{
		Kind: TypeKindStruct,
		Name: MessageStreamEventName,
		Fields: []FieldDesc{
			{Name: "Kind", Type: scalarType("string")},
			{Name: "Start", Type: CanonicalMessageStartType()},
			{Name: "Delta", Type: CanonicalMessageDeltaType()},
		},
	}
}

// CanonicalMessageStreamingDesc returns the canonical message streaming descriptor
// for start/delta/end streams layered over next/final.
// Contract: public semantic contract — stable message streaming event descriptor.
func CanonicalMessageStreamingDesc() *MessageStreamingDesc {
	return &MessageStreamingDesc{
		Start: CloneTypeDescPtr(typePtr(CanonicalMessageStartType())),
		Delta: CloneTypeDescPtr(typePtr(CanonicalMessageDeltaType())),
		End:   CloneTypeDescPtr(typePtr(CanonicalMessageEndType())),
	}
}

// NewCanonicalMessageStreamingCallableDesc creates a streaming callable descriptor
// using the canonical message start/delta/end schema vocabulary.
// Contract: public semantic contract — canonical message streaming callable constructor.
func NewCanonicalMessageStreamingCallableDesc(name string, params []ParameterDesc, hasError bool) (CallableDesc, error) {
	message := CanonicalMessageStreamingDesc()
	return NewMessageStreamingCallableDesc(name, params, typePtr(CanonicalMessageStreamEventType()), typePtr(CanonicalMessageEndType()), message, hasError)
}

func scalarType(name string) TypeDesc {
	return TypeDesc{Kind: TypeKindScalar, Name: name}
}

func mapType(key TypeDesc, value TypeDesc) TypeDesc {
	keyClone := CloneTypeDesc(key)
	valueClone := CloneTypeDesc(value)
	return TypeDesc{Kind: TypeKindMap, Name: "map", Key: &keyClone, Value: &valueClone}
}

func typePtr(desc TypeDesc) *TypeDesc {
	cloned := CloneTypeDesc(desc)
	return &cloned
}

// ValidateCallableDesc checks that a CallableDesc has consistent fields.
// It is exported for use by the binding layer's registry and adapter code.
// Contract: public semantic contract — callable descriptor validation,
// consumed across the schema/binding plane boundary.
func ValidateCallableDesc(desc CallableDesc) error {
	if desc.Name == "" {
		return fmt.Errorf("callable name cannot be empty")
	}

	mode := desc.Mode
	if mode == "" {
		mode = CallableModeUnary
	}

	switch mode {
	case CallableModeUnary:
		if desc.Streaming != nil {
			return fmt.Errorf("unary callable %q cannot define streaming protocol", desc.Name)
		}
		return nil
	case CallableModeStreaming:
		if len(desc.Returns) > 0 {
			return fmt.Errorf("streaming callable %q cannot define unary returns", desc.Name)
		}
		if desc.Streaming == nil {
			return fmt.Errorf("streaming callable %q must define streaming protocol", desc.Name)
		}
		if desc.Streaming.Next == nil && desc.Streaming.Final == nil {
			return fmt.Errorf("streaming callable %q must define next or final schema", desc.Name)
		}
		if err := ValidateMessageStreamingDesc(desc.Streaming.Message); err != nil {
			return fmt.Errorf("streaming callable %q has invalid message protocol: %w", desc.Name, err)
		}
		if desc.Streaming.Message != nil {
			if (desc.Streaming.Message.Start != nil || desc.Streaming.Message.Delta != nil) && desc.Streaming.Next == nil {
				return fmt.Errorf("streaming callable %q message start/delta requires next schema", desc.Name)
			}
			if desc.Streaming.Message.End != nil && desc.Streaming.Final == nil {
				return fmt.Errorf("streaming callable %q message end requires final schema", desc.Name)
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported callable mode %q", desc.Mode)
	}
}

// CloneParameters returns a deep copy of a parameter descriptor slice.
// Contract: public semantic contract — deep copy utility for descriptor isolation.
func CloneParameters(params []ParameterDesc) []ParameterDesc {
	if len(params) == 0 {
		return nil
	}
	cloned := make([]ParameterDesc, len(params))
	copy(cloned, params)
	return cloned
}

// CloneTypeDescPtr returns a deep copy of a TypeDesc pointer.
// Contract: public semantic contract — deep copy utility for descriptor isolation.
func CloneTypeDescPtr(desc *TypeDesc) *TypeDesc {
	if desc == nil {
		return nil
	}
	cloned := CloneTypeDesc(*desc)
	return &cloned
}

// CloneTypeDesc returns a deep copy of a TypeDesc.
// Contract: public semantic contract — deep copy utility for descriptor isolation.
func CloneTypeDesc(desc TypeDesc) TypeDesc {
	cloned := desc
	if desc.Element != nil {
		element := CloneTypeDesc(*desc.Element)
		cloned.Element = &element
	}
	if desc.Key != nil {
		key := CloneTypeDesc(*desc.Key)
		cloned.Key = &key
	}
	if desc.Value != nil {
		value := CloneTypeDesc(*desc.Value)
		cloned.Value = &value
	}
	return cloned
}

// CloneMessageStreamingDesc returns a deep copy of a MessageStreamingDesc pointer.
// Contract: public semantic contract — deep copy utility for message streaming descriptors.
func CloneMessageStreamingDesc(desc *MessageStreamingDesc) *MessageStreamingDesc {
	if desc == nil {
		return nil
	}
	return &MessageStreamingDesc{
		Start: CloneTypeDescPtr(desc.Start),
		Delta: CloneTypeDescPtr(desc.Delta),
		End:   CloneTypeDescPtr(desc.End),
	}
}

// CloneCallableDesc returns a deep copy of a CallableDesc.
// It is exported for use by the binding layer's registry code.
// Contract: public semantic contract — deep copy utility for descriptor isolation.
func CloneCallableDesc(desc CallableDesc) CallableDesc {
	cloned := desc
	cloned.Parameters = CloneParameters(desc.Parameters)
	if len(desc.Returns) > 0 {
		cloned.Returns = make([]TypeDesc, len(desc.Returns))
		for i, ret := range desc.Returns {
			cloned.Returns[i] = CloneTypeDesc(ret)
		}
	}
	if desc.Streaming != nil {
		cloned.Streaming = &StreamingCallableDesc{
			Next:    CloneTypeDescPtr(desc.Streaming.Next),
			Final:   CloneTypeDescPtr(desc.Streaming.Final),
			Message: CloneMessageStreamingDesc(desc.Streaming.Message),
		}
	}
	if cloned.Mode == "" {
		cloned.Mode = CallableModeUnary
	}
	return cloned
}

// CloneMethodDesc returns a deep copy of a MethodDesc.
func CloneMethodDesc(desc MethodDesc) MethodDesc {
	cloned := desc
	cloned.Parameters = CloneParameters(desc.Parameters)
	if len(desc.Returns) > 0 {
		cloned.Returns = make([]TypeDesc, len(desc.Returns))
		for i, ret := range desc.Returns {
			cloned.Returns[i] = CloneTypeDesc(ret)
		}
	}
	return cloned
}

// CloneObjectDesc returns a deep copy of an ObjectDesc.
func CloneObjectDesc(desc ObjectDesc) ObjectDesc {
	cloned := ObjectDesc{
		Kind:       desc.Kind,
		Name:       desc.Name,
		Parent:     desc.Parent,
		IsOpen:     desc.IsOpen,
		Implements: make([]string, len(desc.Implements)),
	}
	copy(cloned.Implements, desc.Implements)
	if len(desc.Fields) > 0 {
		cloned.Fields = make([]FieldDesc, len(desc.Fields))
		for i, field := range desc.Fields {
			cloned.Fields[i] = FieldDesc{
				Name:    field.Name,
				Type:    CloneTypeDesc(field.Type),
				Private: field.Private,
			}
		}
	}
	if len(desc.Methods) > 0 {
		cloned.Methods = make([]MethodDesc, len(desc.Methods))
		for i, method := range desc.Methods {
			cloned.Methods[i] = CloneMethodDesc(method)
		}
	}
	return cloned
}

// CloneInterfaceDesc returns a deep copy of an InterfaceDesc.
func CloneInterfaceDesc(desc InterfaceDesc) InterfaceDesc {
	cloned := InterfaceDesc{Name: desc.Name}
	if len(desc.Methods) > 0 {
		cloned.Methods = make([]MethodDesc, len(desc.Methods))
		for i, method := range desc.Methods {
			cloned.Methods[i] = CloneMethodDesc(method)
		}
	}
	return cloned
}

// CloneEnumDesc returns a deep copy of an EnumDesc.
func CloneEnumDesc(desc EnumDesc) EnumDesc {
	cloned := EnumDesc{Name: desc.Name}
	if len(desc.Members) > 0 {
		cloned.Members = make([]EnumMemberDesc, len(desc.Members))
		copy(cloned.Members, desc.Members)
	}
	return cloned
}
