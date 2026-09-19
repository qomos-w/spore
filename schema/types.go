package schema

// TypeKind classifies the kind of a type descriptor.
// Contract: public semantic contract — consumers rely on stable type classification.
type TypeKind string

// CallableMode classifies the execution mode of a callable.
// Contract: public semantic contract — determines invocation protocol.
type CallableMode string

// StreamingEventKind classifies message-style events layered over a streaming callable.
// Contract: public semantic contract — event vocabulary for message-style streaming payloads.
type StreamingEventKind string

const (
	TypeKindInvalid TypeKind = "invalid"
	TypeKindVoid    TypeKind = "void"
	TypeKindScalar  TypeKind = "scalar"
	TypeKindArray   TypeKind = "array"
	TypeKindMap     TypeKind = "map"
	TypeKindStruct  TypeKind = "struct"
	TypeKindClass   TypeKind = "class"
	TypeKindEnum    TypeKind = "enum"
	TypeKindMedia   TypeKind = "media"

	CallableModeUnary     CallableMode = "unary"
	CallableModeStreaming CallableMode = "streaming"

	StreamingEventStart StreamingEventKind = "start"
	StreamingEventDelta StreamingEventKind = "delta"
	StreamingEventEnd   StreamingEventKind = "end"
)

// TypeDesc is the canonical schema type descriptor.
// Contract: public semantic contract — the stable vocabulary for describing types
// across all four planes (schema, transport, runtime carrier, script binding).
//
// JSON tags use lowerCamelCase to match the manifest format documented in
// cmd/spore-gen-ts/README.md and the TypeScript mirror in ts/src/schema.ts.
// `omitempty` is applied only to optional fields (pointers, optional strings,
// nullable IDs); required fields like Kind always emit on the wire.
type TypeDesc struct {
	Kind      TypeKind  `json:"kind"`
	Name      string    `json:"name,omitempty"`
	TypeID    TypeID    `json:"typeId,omitempty"`
	Element   *TypeDesc `json:"element,omitempty"`
	Key       *TypeDesc `json:"key,omitempty"`
	Value     *TypeDesc `json:"value,omitempty"`
	ClassName string    `json:"className,omitempty"`
	ClassID   uint64    `json:"classId,omitempty"`
}

func (td TypeDesc) String() string {
	switch td.Kind {
	case TypeKindVoid:
		return "void"
	case TypeKindScalar:
		if td.Name == "" {
			return "any"
		}
		return td.Name
	case TypeKindArray:
		if td.Element == nil {
			return "array"
		}
		return "array<" + td.Element.String() + ">"
	case TypeKindMap:
		if td.Key == nil || td.Value == nil {
			return "map"
		}
		return "map<" + td.Key.String() + ", " + td.Value.String() + ">"
	case TypeKindStruct:
		name := td.ClassName
		if name == "" || name == "struct" {
			name = td.Name
		}
		if name == "" {
			return "struct"
		}
		return name
	case TypeKindClass:
		name := td.ClassName
		if name == "" {
			name = td.Name
		}
		if name == "" {
			return "class"
		}
		return name
	case TypeKindEnum:
		if td.Name != "" {
			return td.Name
		}
		return "enum"
	case TypeKindMedia:
		return "media"
	default:
		if td.Name != "" {
			return td.Name
		}
		return "unknown"
	}
}

// ParameterDesc describes a named parameter in a callable signature.
// Contract: public semantic contract — stable parameter description for callable descriptors.
type ParameterDesc struct {
	Name string   `json:"name"`
	Type TypeDesc `json:"type"`
}

// StreamingCallableDesc describes the streaming protocol of a callable.
// Contract: public semantic contract — defines next/final schema for streaming callables.
type StreamingCallableDesc struct {
	Next    *TypeDesc             `json:"next,omitempty"`
	Final   *TypeDesc             `json:"final,omitempty"`
	Message *MessageStreamingDesc `json:"message,omitempty"`
}

// MessageStreamingDesc describes message-style events layered over a streaming callable.
// Contract: public semantic contract — defines optional start/delta/end payload schemas.
type MessageStreamingDesc struct {
	Start *TypeDesc `json:"start,omitempty"`
	Delta *TypeDesc `json:"delta,omitempty"`
	End   *TypeDesc `json:"end,omitempty"`
}

// CallableDesc is the canonical schema descriptor for a callable function.
// Contract: public semantic contract — the stable vocabulary for describing callable signatures
// across all four planes.
//
// HasError emits on the wire even when false because the TypeScript mirror
// declares it as a required field. Parameters and Returns are also required
// on the wire; producers must initialize them as non-nil slices (callers
// inside spore already do — see schema/callable.go).
type CallableDesc struct {
	Name       string                 `json:"name"`
	Parameters []ParameterDesc        `json:"parameters"`
	Returns    []TypeDesc             `json:"returns"`
	HasError   bool                   `json:"hasError"`
	Mode       CallableMode           `json:"mode"`
	Streaming  *StreamingCallableDesc `json:"streaming,omitempty"`
}

// FieldDesc describes a named field in an object shape.
// Contract: public semantic contract — stable field description for object descriptors.
//
// Description carries human-readable documentation for the field (used for
// generated JSDoc / IDE hover). For reflect-described Go structs it is sourced
// from the `description:"..."` struct tag; the foundational-Class path leaves
// it empty until that vocabulary grows a doc channel.
type FieldDesc struct {
	Name        string   `json:"name"`
	Type        TypeDesc `json:"type"`
	Description string   `json:"description,omitempty"`
	Private     bool     `json:"private,omitempty"`  // true if field has private access modifier
	Optional    bool     `json:"optional,omitempty"` // true if field is declared with the optional modifier
}

// MethodDesc describes a method in an object shape.
// Contract: public semantic contract — stable method description for object descriptors.
type MethodDesc struct {
	Name       string          `json:"name"`
	Parameters []ParameterDesc `json:"parameters"`
	Returns    []TypeDesc      `json:"returns"`
	IsOpen     bool            `json:"isOpen,omitempty"`
	IsOverride bool            `json:"isOverride,omitempty"`
	Private    bool            `json:"private,omitempty"`
}

// ObjectDesc is the canonical schema-visible object shape and the primary
// protocol-layer definition consumed by hosting environments (gospore).
// Contract: public semantic contract — the stable vocabulary for describing object shapes
// across all four planes (schema, transport, runtime carrier, script binding) and
// across process boundaries to external hosts.
// It intentionally excludes runtime layout metadata such as field offsets,
// reflection index paths, or native binding handles.
// The Kind field distinguishes data-contract shapes (TypeKindStruct, safe for
// schema boundary / export fun / cross-process transport) from internal shapes
// (TypeKindClass, confined to script-internal use).
type ObjectDesc struct {
	Kind        TypeKind     `json:"kind"` // TypeKindStruct for struct, TypeKindClass for class
	Name        string       `json:"name"`
	Fields      []FieldDesc  `json:"fields"`
	Parent      string       `json:"parent,omitempty"`     // parent class name (empty if none)
	IsOpen      bool         `json:"isOpen,omitempty"`     // true if class can be inherited
	Implements  []string     `json:"implements,omitempty"` // interface names
	Methods     []MethodDesc `json:"methods,omitempty"`    // class methods (including constructor)
	SchemaID    uint64       `json:"schemaId,omitempty"`    // optional stable schema id declared in source
	IsComponent bool         `json:"isComponent,omitempty"` // true if declared @component (ECS component)
}

// InterfaceDesc describes an interface declaration.
// Contract: public semantic contract — stable interface description for schema consumers.
type InterfaceDesc struct {
	Name    string       `json:"name"`
	Methods []MethodDesc `json:"methods"`
}

// EnumMemberDesc describes one member of an enum declaration.
// Contract: public semantic contract — stable enum member description for schema consumers.
type EnumMemberDesc struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
}

// EnumDesc describes an enum declaration: a named closed set of int-valued
// members. Enums are data contracts (safe for schema boundary / export) whose
// runtime representation is an int tagged with the declaring enum's ID.
// Contract: public semantic contract — stable enum description for schema consumers.
type EnumDesc struct {
	Name    string           `json:"name"`
	Members []EnumMemberDesc `json:"members"`
}
