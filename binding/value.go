package binding

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/qomos-w/spore/schema"
)

// ValueCarrierKind classifies the shape of a value carried by an invocation outcome.
// Contract: public semantic contract — value payload classification.
type ValueCarrierKind string

const (
	ValueCarrierNone   ValueCarrierKind = "none"
	ValueCarrierScalar ValueCarrierKind = "scalar"
	ValueCarrierObject ValueCarrierKind = "object"
	ValueCarrierList   ValueCarrierKind = "list"
)

// ValueCarrier wraps an invocation result payload with its kind classification.
// Contract: public semantic contract — value payload carrier for invocation outcomes.
type ValueCarrier struct {
	Kind  ValueCarrierKind
	Value any
}

// InvocationOutcome is the complete result of a callable invocation,
// combining the result descriptor with an optional value payload.
// Contract: public semantic contract — the stable output shape for callable invocations.
type InvocationOutcome struct {
	Result  InvocationResultDesc
	Payload *ValueCarrier
}

// NewValueCarrier creates a ValueCarrier from a Go value, inferring its kind.
// Contract: public semantic contract — value carrier construction.
func NewValueCarrier(value any) *ValueCarrier {
	if value == nil {
		return nil
	}
	return &ValueCarrier{Kind: inferValueCarrierKind(value), Value: value}
}

// NewInvocationOutcome constructs an InvocationOutcome from a result descriptor
// and an optional payload. Error results cannot carry payloads.
// Contract: public semantic contract — invocation outcome construction.
func NewInvocationOutcome(result InvocationResultDesc, payload any) (InvocationOutcome, error) {
	if result.Kind == InvocationResultError {
		if payload != nil {
			return InvocationOutcome{}, fmt.Errorf("invocation error result cannot carry payload")
		}
		return InvocationOutcome{Result: result, Payload: nil}, nil
	}
	if err := validateInvocationPayload(result.Value, payload); err != nil {
		return InvocationOutcome{}, err
	}
	return InvocationOutcome{Result: result, Payload: NewValueCarrier(payload)}, nil
}

func validateInvocationPayload(expected *schema.TypeDesc, payload any) error {
	if expected == nil {
		if payload != nil {
			return fmt.Errorf("void invocation result cannot carry payload")
		}
		return nil
	}
	if payload == nil {
		// A non-void callable may legitimately return null when its declared
		// return type is a nullable reference kind (map, array, struct, class,
		// media, any, object): the script VM encodes `return null` as a null
		// value that decodes to a Go nil payload, and the host distinguishes
		// "no value" from a populated one (e.g. an AI thinker returning null
		// intent). Inline value returns (scalars like int/bool/string, enums)
		// remain non-nullable: a nil payload there is a contract violation.
		if isNullableResultType(*expected) {
			return nil
		}
		return fmt.Errorf("non-void invocation result requires payload")
	}
	return validateValueAgainstType(*expected, payload)
}

// isNullableResultType reports whether a declared return type may carry a nil
// (null) value at the binding boundary. Reference kinds (map, array, struct,
// class, media) and the dynamic any/object kinds hold references the script VM
// represents as nullable; inline scalars and int-backed enums do not.
func isNullableResultType(td schema.TypeDesc) bool {
	switch td.Kind {
	case schema.TypeKindMap, schema.TypeKindArray, schema.TypeKindStruct,
		schema.TypeKindClass, schema.TypeKindMedia:
		return true
	case schema.TypeKindScalar:
		switch td.Name {
		case "any", "object", "":
			return true
		}
	}
	return false
}

// ValidateInvocationArgs checks that the invocation arguments match the
// callable descriptor's parameter list. Exported for use by ExecutableAdapter
// implementations that need to enforce the same contract as the built-in adapters.
// Contract: public semantic contract — invocation argument validation.
func ValidateInvocationArgs(desc schema.CallableDesc, args []any) error {
	if len(args) != len(desc.Parameters) {
		return newContractErrorWithTypes(CodeInvalidArgumentCount, desc.Name, InvocationStage(""), "binding/args/count",
			fmt.Sprintf("%d", len(desc.Parameters)), fmt.Sprintf("%d", len(args)),
			"callable %q expects %d args, got %d", desc.Name, len(desc.Parameters), len(args))
	}
	for i, param := range desc.Parameters {
		if args[i] == nil {
			return newContractError(CodeNilArgument, desc.Name, InvocationStage(""), "binding/args/nil", "callable %q arg %q cannot be nil", desc.Name, param.Name)
		}
		if err := validateValueAgainstType(param.Type, args[i]); err != nil {
			var oversized *schema.MediaInlineTooLargeError
			code := CodeInvalidArgumentType
			if errors.As(err, &oversized) {
				code = schema.CodeMediaInlineTooLarge
			}
			return &ContractError{
				Code:     code,
				Callable: desc.Name,
				Path:     "binding/args/type",
				Message:  fmt.Sprintf("callable %q arg %q invalid: %v", desc.Name, param.Name, err),
				Expected: typeDescName(param.Type),
				Actual:   fmt.Sprintf("%T", args[i]),
				Cause:    err,
			}
		}
	}
	return nil
}

func validateValueAgainstType(expected schema.TypeDesc, value any) error {
	if value == nil {
		return fmt.Errorf("value is nil")
	}
	if expected.Kind == schema.TypeKindMedia {
		return schema.ValidateMediaValue(value)
	}
	return validatePayloadValueAgainstType(expected, reflect.ValueOf(value))
}

func validatePayloadValueAgainstType(expected schema.TypeDesc, actual reflect.Value) error {
	for actual.Kind() == reflect.Pointer {
		if actual.IsNil() {
			return fmt.Errorf("value is nil")
		}
		actual = actual.Elem()
	}
	return validatePayloadAgainstType(expected, actual.Type())
}

func validatePayloadAgainstType(expected schema.TypeDesc, actual reflect.Type) error {
	for actual.Kind() == reflect.Pointer {
		actual = actual.Elem()
	}

	if expected.Kind == schema.TypeKindMap {
		if ordered, ok, err := schema.DescribeOrderedMapType(actual); err != nil {
			return err
		} else if ok {
			if expected.Key == nil || expected.Value == nil {
				return fmt.Errorf("map result schema %q is missing key/value descriptors", expected.Name)
			}
			if err := validateTypeCompatibility(*expected.Key, *ordered.Key); err != nil {
				return err
			}
			return validateTypeCompatibility(*expected.Value, *ordered.Value)
		}
	}

	switch expected.Kind {
	case schema.TypeKindScalar:
		if matchesScalarType(expected.Name, actual.Kind()) {
			return nil
		}
	case schema.TypeKindArray:
		if (actual.Kind() == reflect.Slice || actual.Kind() == reflect.Array) && expected.Element != nil {
			// sporescript arrays reach the binding layer as []any; the
			// element kind is interface{} regardless of the expected element
			// type. Accept this — bindGoFunctionArg performs per-element
			// conversion at invoke time.
			if actual.Elem().Kind() == reflect.Interface {
				return nil
			}
			return validatePayloadAgainstType(*expected.Element, actual.Elem())
		}
	case schema.TypeKindMap:
		if actual.Kind() == reflect.Map && expected.Key != nil && expected.Value != nil {
			if err := validatePayloadAgainstType(*expected.Key, actual.Key()); err != nil {
				return err
			}
			return validatePayloadAgainstType(*expected.Value, actual.Elem())
		}
	case schema.TypeKindStruct:
		if actual.Kind() == reflect.Struct {
			return nil
		}
		if actual.Kind() == reflect.Map && actual.Key().Kind() == reflect.String {
			return nil
		}
	case schema.TypeKindMedia:
		if schema.IsMediaStructType(actual) {
			return nil
		}
		if actual.Kind() == reflect.Map && actual.Key().Kind() == reflect.String {
			return nil
		}
	}
	return fmt.Errorf("payload type %q does not match result schema %q", actual.String(), expected.Name)
}

func validateTypeCompatibility(expected schema.TypeDesc, actual schema.TypeDesc) error {
	if expected.Kind != actual.Kind {
		return fmt.Errorf("payload type %q does not match result schema %q", actual.Name, expected.Name)
	}
	if expected.Kind == schema.TypeKindScalar {
		if expected.Name != actual.Name {
			return fmt.Errorf("payload type %q does not match result schema %q", actual.Name, expected.Name)
		}
	} else if expected.Name != actual.Name {
		return fmt.Errorf("payload type %q does not match result schema %q", actual.Name, expected.Name)
	}
	switch expected.Kind {
	case schema.TypeKindArray:
		if expected.Element == nil || actual.Element == nil {
			if expected.Element == actual.Element {
				return nil
			}
			break
		}
		return validateTypeCompatibility(*expected.Element, *actual.Element)
	case schema.TypeKindMap:
		if expected.Key == nil || expected.Value == nil || actual.Key == nil || actual.Value == nil {
			if expected.Key == actual.Key && expected.Value == actual.Value {
				return nil
			}
			break
		}
		if err := validateTypeCompatibility(*expected.Key, *actual.Key); err != nil {
			return err
		}
		return validateTypeCompatibility(*expected.Value, *actual.Value)
	}
	return nil
}

func matchesScalarType(name string, kind reflect.Kind) bool {
	switch name {
	case "bool":
		return kind == reflect.Bool
	case "byte":
		return kind == reflect.Int8 || kind == reflect.Uint8
	case "short":
		return kind == reflect.Int16
	case "ushort":
		return kind == reflect.Uint16
	case "int":
		return kind == reflect.Int || kind == reflect.Int32
	case "uint":
		return kind == reflect.Uint || kind == reflect.Uint32
	case "long":
		return kind == reflect.Int64 || kind == reflect.Int
	case "ulong":
		return kind == reflect.Uint64
	case "float":
		return kind == reflect.Float32
	case "double":
		return kind == reflect.Float64
	case "string":
		return kind == reflect.String
	case "bytes":
		return kind == reflect.Slice
	case "object", "any":
		return true
	}
	return false
}

func inferValueCarrierKind(value any) ValueCarrierKind {
	switch value.(type) {
	case []any, []string, []int, []int64, []float64, []float32, []bool:
		return ValueCarrierList
	case map[string]any, map[string]string, map[string]int:
		return ValueCarrierObject
	case string, bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64, []byte:
		return ValueCarrierScalar
	default:
		return ValueCarrierObject
	}
}

// typeDescName returns a human-readable type name for a schema.TypeDesc.
func typeDescName(td schema.TypeDesc) string {
	switch td.Kind {
	case schema.TypeKindVoid:
		return "void"
	case schema.TypeKindScalar:
		if td.Name == "" {
			return "any"
		}
		return td.Name
	case schema.TypeKindArray:
		if td.Element == nil {
			return "array"
		}
		return "[]" + typeDescName(*td.Element)
	case schema.TypeKindMap:
		if td.Key == nil || td.Value == nil {
			return "map"
		}
		return "map[" + typeDescName(*td.Key) + "]" + typeDescName(*td.Value)
	case schema.TypeKindStruct:
		if td.Name != "" {
			return td.Name
		}
		return "struct"
	case schema.TypeKindClass:
		if td.ClassName != "" {
			return td.ClassName
		}
		return "class"
	case schema.TypeKindMedia:
		return "media"
	default:
		if td.Name != "" {
			return td.Name
		}
		return "unknown"
	}
}
