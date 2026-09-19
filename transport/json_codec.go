package transport

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"encoding/json/jsontext"
	"encoding/json/v2"

	"github.com/qomos-w/spore/identity"
	"github.com/qomos-w/spore/schema"
)

// jsonEncodeOptions pins the v1 wire-parity invariants on top of
// encoding/json/v2 defaults: map members are emitted in sorted key order
// (Deterministic) and nil maps/slices project to null rather than {} / [].
// Everything else adopts v2 semantics: stricter duplicate-name and
// invalid-UTF-8 rejection, case-sensitive name matching, and richer
// errors that carry JSON pointer context (surfaced via jsonErrorPath).
var jsonEncodeOptions = []json.Options{
	json.Deterministic(true),
	json.FormatNilMapAsNull(true),
	json.FormatNilSliceAsNull(true),
}

// JSONCodec is a Codec implementation that uses encoding/json/v2 for
// serialization. It is the default Codec backend; alternative backends
// can implement the Codec SPI interface directly.
// Default backend for the Codec SPI — not an SPI itself.
type JSONCodec struct{}

// Encode projects a Go value into a transport view using JSON serialization.
// The value is validated against the schema descriptor before encoding.
// OrderedMap values are projected to entry sequences before serialization
// to preserve insertion order on the wire.
func (c *JSONCodec) Encode(s schema.TypeDesc, id identity.CanonicalID, value any) (View, error) {
	if err := validateEncodeType(s, value); err != nil {
		return View{}, encodeErrorf(s, id, "", err)
	}

	// Project OrderedMap to entry sequence for wire order preservation
	encodable := value
	v := reflect.ValueOf(value)
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			break
		}
		v = v.Elem()
	}
	if v.Kind() == reflect.Struct && isOrderedMapValue(v) {
		entries, err := orderedMapEntries(v)
		if err != nil {
			return View{}, encodeErrorf(s, id, "", fmt.Errorf("ordered map projection: %w", err))
		}
		encodable = entries
	}

	data, err := json.Marshal(encodable, jsonEncodeOptions...)
	if err != nil {
		return View{}, encodeErrorf(s, id, jsonErrorPath(err), fmt.Errorf("json marshal: %w", err))
	}

	return View{
		Kind:     ViewKindFull,
		Schema:   s,
		Identity: id,
		Data:     data,
	}, nil
}

// DecodeInto decodes JSON data directly into target.
func (c *JSONCodec) DecodeInto(view View, target any) error {
	if len(view.Data) == 0 {
		return &DecodeError{
			SchemaName: view.Schema.Name,
			Path:       "",
			Identity:   view.Identity,
			Err:        fmt.Errorf("empty data"),
		}
	}
	if err := json.Unmarshal(view.Data, target); err != nil {
		return decodeErrorWithPath(view, jsonErrorPath(err), "json DecodeInto: %w", err)
	}
	return nil
}

// Decode reconstructs a Go value from a transport view.
// Scalar types are decoded as their natural Go types.
// Struct types are decoded as map[string]any.
func (c *JSONCodec) Decode(view View) (any, error) {
	if len(view.Data) == 0 {
		return nil, &DecodeError{
			SchemaName: view.Schema.Name,
			Path:       "",
			Identity:   view.Identity,
			Err:        fmt.Errorf("empty data"),
		}
	}

	switch view.Schema.Kind {
	case schema.TypeKindScalar:
		return c.decodeScalar(view)
	case schema.TypeKindStruct:
		var m map[string]any
		if err := json.Unmarshal(view.Data, &m); err != nil {
			return nil, decodeErrorWithPath(view, jsonErrorPath(err), "json unmarshal: %w", err)
		}
		return m, nil
	case schema.TypeKindMedia:
		var m map[string]any
		if err := json.Unmarshal(view.Data, &m); err != nil {
			return nil, decodeErrorWithPath(view, jsonErrorPath(err), "json unmarshal: %w", err)
		}
		if err := schema.ValidateMediaValue(m); err != nil {
			return nil, decodeErrorWithPath(view, "", "media decode: %w", err)
		}
		return m, nil
	default:
		var decoded any
		if err := json.Unmarshal(view.Data, &decoded); err != nil {
			return nil, decodeErrorWithPath(view, jsonErrorPath(err), "json unmarshal: %w", err)
		}
		return decoded, nil
	}
}

func (c *JSONCodec) decodeScalar(view View) (any, error) {
	switch view.Schema.Name {
	case "int":
		var v int32
		if err := json.Unmarshal(view.Data, &v); err != nil {
			return nil, decodeErrorWithPath(view, jsonErrorPath(err), "json unmarshal: %w", err)
		}
		return v, nil
	case "long":
		var v int64
		if err := json.Unmarshal(view.Data, &v); err != nil {
			return nil, decodeErrorWithPath(view, jsonErrorPath(err), "json unmarshal: %w", err)
		}
		return v, nil
	case "uint":
		var v uint32
		if err := json.Unmarshal(view.Data, &v); err != nil {
			return nil, decodeErrorWithPath(view, jsonErrorPath(err), "json unmarshal: %w", err)
		}
		return v, nil
	case "ulong":
		var v uint64
		if err := json.Unmarshal(view.Data, &v); err != nil {
			return nil, decodeErrorWithPath(view, jsonErrorPath(err), "json unmarshal: %w", err)
		}
		return v, nil
	case "byte", "short", "ushort":
		var v int32
		if err := json.Unmarshal(view.Data, &v); err != nil {
			return nil, decodeErrorWithPath(view, jsonErrorPath(err), "json unmarshal: %w", err)
		}
		return v, nil
	case "string":
		var v string
		if err := json.Unmarshal(view.Data, &v); err != nil {
			return nil, decodeErrorWithPath(view, jsonErrorPath(err), "json unmarshal: %w", err)
		}
		return v, nil
	case "bool":
		var v bool
		if err := json.Unmarshal(view.Data, &v); err != nil {
			return nil, decodeErrorWithPath(view, jsonErrorPath(err), "json unmarshal: %w", err)
		}
		return v, nil
	case "float", "double":
		var v float64
		if err := json.Unmarshal(view.Data, &v); err != nil {
			return nil, decodeErrorWithPath(view, jsonErrorPath(err), "json unmarshal: %w", err)
		}
		return v, nil
	default:
		var v any
		if err := json.Unmarshal(view.Data, &v); err != nil {
			return nil, decodeErrorWithPath(view, jsonErrorPath(err), "json unmarshal: %w", err)
		}
		return v, nil
	}
}

// jsonErrorPath extracts deep-path context from an encoding/json/v2 error.
// v2 natively reports the failing value's location as an RFC 6901 JSON
// pointer on json.SemanticError (semantic mismatches) and
// jsontext.SyntacticError (syntax problems), so nested failures yield
// paths like ".fieldName" or ".fieldName[0]" instead of the empty
// top-level path. Errors without pointer context yield "".
func jsonErrorPath(err error) string {
	var ptr jsontext.Pointer
	var sem *json.SemanticError
	var syn *jsontext.SyntacticError
	switch {
	case errors.As(err, &sem):
		ptr = sem.JSONPointer
	case errors.As(err, &syn):
		ptr = syn.JSONPointer
	default:
		return ""
	}
	if ptr == "" {
		return ""
	}
	return jsonPointerToDottedPath(ptr)
}

// jsonPointerToDottedPath converts an RFC 6901 JSON pointer
// ("/Sub/Level", "/Items/0", "/a~1b") into the dotted transport path
// convention carried by EncodeError.Path and DecodeError.Path
// (".Sub.Level", ".Items[0]", ".a/b"). Canonical numeric tokens render
// as array indices; every other token renders as a field name with
// RFC 6901 escaping (~1 → /, ~0 → ~) reversed. The empty pointer —
// a failure at the root value itself — maps to "".
func jsonPointerToDottedPath(ptr jsontext.Pointer) string {
	if ptr == "" {
		return ""
	}
	// A non-empty RFC 6901 pointer always starts with "/".
	tokens := strings.Split(string(ptr), "/")[1:]
	var b strings.Builder
	for _, token := range tokens {
		token = strings.ReplaceAll(token, "~1", "/")
		token = strings.ReplaceAll(token, "~0", "~")
		if idx, convErr := strconv.Atoi(token); convErr == nil && strconv.Itoa(idx) == token {
			b.WriteString("[")
			b.WriteString(token)
			b.WriteString("]")
		} else {
			b.WriteString(".")
			b.WriteString(token)
		}
	}
	return b.String()
}

// validateEncodeType checks that the value's Go type is compatible with
// the schema descriptor. Schema is the projection authority.
func validateEncodeType(s schema.TypeDesc, value any) error {
	if value == nil {
		return fmt.Errorf("cannot encode nil value for schema %q", s.Name)
	}

	v := reflect.ValueOf(value)
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return fmt.Errorf("cannot encode nil pointer for schema %q", s.Name)
		}
		v = v.Elem()
	}

	switch s.Kind {
	case schema.TypeKindScalar:
		switch s.Name {
		case "int":
			switch v.Kind() {
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
				return nil
			default:
				return fmt.Errorf("type mismatch: schema %q expects int, got %s", s.Name, v.Kind())
			}
		case "long":
			switch v.Kind() {
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
				return nil
			default:
				return fmt.Errorf("type mismatch: schema %q expects long, got %s", s.Name, v.Kind())
			}
		case "uint":
			switch v.Kind() {
			case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
				return nil
			default:
				return fmt.Errorf("type mismatch: schema %q expects uint, got %s", s.Name, v.Kind())
			}
		case "ulong":
			switch v.Kind() {
			case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
				return nil
			default:
				return fmt.Errorf("type mismatch: schema %q expects ulong, got %s", s.Name, v.Kind())
			}
		case "byte", "short", "ushort":
			switch v.Kind() {
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
				reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
				return nil
			default:
				return fmt.Errorf("type mismatch: schema %q expects integer, got %s", s.Name, v.Kind())
			}
		case "string":
			if v.Kind() != reflect.String {
				return fmt.Errorf("type mismatch: schema %q expects string, got %s", s.Name, v.Kind())
			}
		case "bool":
			if v.Kind() != reflect.Bool {
				return fmt.Errorf("type mismatch: schema %q expects bool, got %s", s.Name, v.Kind())
			}
		case "float", "double":
			switch v.Kind() {
			case reflect.Float32, reflect.Float64:
				return nil
			default:
				return fmt.Errorf("type mismatch: schema %q expects float, got %s", s.Name, v.Kind())
			}
		}
	case schema.TypeKindStruct:
		// Accept both Go structs and map[string]any (the canonical
		// projection form produced by binding.ProjectView). This ensures
		// the transport layer can consume binding's output directly.
		if v.Kind() == reflect.Struct {
			return nil
		}
		if v.Kind() == reflect.Map {
			if v.Type().Key().Kind() == reflect.String && v.Type().Elem().Kind() == reflect.Interface {
				return nil
			}
			return fmt.Errorf("type mismatch: schema %q expects struct or map[string]any, got %s", s.Name, v.Type())
		}
		return fmt.Errorf("type mismatch: schema %q expects struct, got %s", s.Name, v.Kind())
	case schema.TypeKindMedia:
		// Media projects as exactly {mime, src}; the value contract (scheme
		// whitelist, data: inline cap) is enforced before any byte is written
		// so neither codec can be fed oversized inline payloads.
		if err := schema.ValidateMediaValue(value); err != nil {
			return err
		}
	case schema.TypeKindArray:
		if v.Kind() != reflect.Slice && v.Kind() != reflect.Array {
			return fmt.Errorf("type mismatch: schema %q expects array/slice, got %s", s.Name, v.Kind())
		}
	case schema.TypeKindMap:
		// Map values may arrive as a native Go map, an OrderedMap struct,
		// or a projected entry sequence ([]any of {Key,Value} maps).
		switch v.Kind() {
		case reflect.Map:
			// Native Go map — valid
		case reflect.Slice:
			// Entry sequence projection from binding layer — valid
		case reflect.Struct:
			// OrderedMap is detected by its shape, not by hardcoded import.
			if !isOrderedMapValue(v) {
				return fmt.Errorf("type mismatch: schema %q expects map, got struct %s", s.Name, v.Type().Name())
			}
		default:
			return fmt.Errorf("type mismatch: schema %q expects map, got %s", s.Name, v.Kind())
		}
	}
	return nil
}

// encodeErrorf constructs an EncodeError with explicit path context.
// Path is empty ("") for top-level validation failures; deep marshal
// failures carry paths like ".FieldName" or ".FieldName[0]", extracted
// from encoding/json/v2 JSON pointers by jsonErrorPath.
func encodeErrorf(s schema.TypeDesc, id identity.CanonicalID, path string, err error) *EncodeError {
	return &EncodeError{
		SchemaName: s.Name,
		Path:       path,
		Identity:   id,
		Err:        err,
	}
}

// decodeErrorWithPath constructs a DecodeError with explicit path context.
// Path is empty ("") for top-level decode failures; nested failures carry
// ".fieldName" / ".fieldName[0]" paths derived from json/v2 JSON pointers.
func decodeErrorWithPath(view View, path string, format string, args ...any) *DecodeError {
	return &DecodeError{
		SchemaName: view.Schema.Name,
		Path:       path,
		Identity:   view.Identity,
		Err:        fmt.Errorf(format, args...),
	}
}

// decodeErrorWithTypes constructs a DecodeError with expected/actual type context.
func decodeErrorWithTypes(view View, path string, expected, actual string, format string, args ...any) *DecodeError {
	return &DecodeError{
		SchemaName: view.Schema.Name,
		Path:       path,
		Identity:   view.Identity,
		Err:        fmt.Errorf(format, args...),
		Expected:   expected,
		Actual:     actual,
	}
}

// isOrderedMapValue detects whether a reflect.Value is an OrderedMap
// or SortedOrderedMap by checking its package path and type name prefix.
// This uses the same detection pattern as schema/describe.go's
// describeOrderedMapType without importing the schema package's concrete
// type. Both variants share the same Entries() surface.
func isOrderedMapValue(v reflect.Value) bool {
	t := v.Type()
	if t.PkgPath() != "github.com/qomos-w/spore/schema" {
		return false
	}
	name := t.Name()
	return (len(name) > 11 && name[:11] == "OrderedMap[") ||
		(len(name) > 17 && name[:17] == "SortedOrderedMap[")
}

// orderedMapEntries calls Entries() on an OrderedMap via reflection and
// converts the result to a []any of map[string]any{"Key": k, "Value": v}.
// This produces the canonical entry sequence projection that preserves
// insertion order for wire serialization.
func orderedMapEntries(v reflect.Value) ([]any, error) {
	// Get the Entries method — works on both value and pointer receiver
	methodType := v.Type()
	if methodType.Kind() != reflect.Pointer {
		methodType = reflect.PointerTo(methodType)
	}
	method, ok := methodType.MethodByName("Entries")
	if !ok {
		return nil, fmt.Errorf("OrderedMap missing Entries method")
	}

	// Call Entries() — need pointer receiver
	var receiver reflect.Value
	if v.CanAddr() {
		receiver = v.Addr()
	} else {
		// Create a new pointer and set it
		ptr := reflect.New(v.Type())
		ptr.Elem().Set(v)
		receiver = ptr
	}

	results := method.Func.Call([]reflect.Value{receiver})
	if len(results) != 1 {
		return nil, fmt.Errorf("Entries() returned %d values, expected 1", len(results))
	}

	entriesSlice := results[0]
	if entriesSlice.Kind() != reflect.Slice {
		return nil, fmt.Errorf("Entries() returned %s, expected slice", entriesSlice.Kind())
	}

	out := make([]any, entriesSlice.Len())
	for i := 0; i < entriesSlice.Len(); i++ {
		entry := entriesSlice.Index(i)
		if entry.Kind() != reflect.Struct || entry.NumField() < 2 {
			return nil, fmt.Errorf("entry %d has unexpected shape", i)
		}
		keyField := entry.Field(0)
		valField := entry.Field(1)
		out[i] = map[string]any{
			"Key":   keyField.Interface(),
			"Value": valField.Interface(),
		}
	}
	return out, nil
}
