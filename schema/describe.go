package schema

import (
	"fmt"
	"reflect"
	"strings"
)

const (
	typeCategoryBasic  = 0x0
	typeCategoryArray  = 0x1
	typeCategoryMap    = 0x2
	typeCategoryStruct = 0x3
	typeCategoryClass  = 0x4
	typeCategoryEnum   = 0x5
)

var errorType = reflect.TypeOf((*error)(nil)).Elem()

func typeCategory(typeID TypeID) uint8 {
	return uint8((uint64(typeID) >> 60) & 0xF)
}

func elementType(typeID TypeID) TypeID {
	elemCategory := uint8((uint64(typeID) >> 56) & 0xF)
	elemData := uint64(typeID) & 0x00FFFFFFFFFFFFFF
	return TypeID(uint64(elemCategory)<<60 | elemData)
}

func keyType(typeID TypeID) TypeID {
	return TypeID((uint64(typeID) >> 30) & 0x3FFFFFFF)
}

func valueType(typeID TypeID) TypeID {
	return TypeID(uint64(typeID) & 0x3FFFFFFF)
}

func classIDFromType(typeID TypeID) uint64 {
	return uint64(typeID) & ((1 << 53) - 1)
}

// MakeEnumTypeID builds an enum-category TypeID carrying a runtime enum ID
// (the 1-based registry index assigned when the enum is compiled/registered).
func MakeEnumTypeID(enumID uint64) TypeID {
	return TypeID(uint64(typeCategoryEnum)<<60 | enumID)
}

// DescribeType maps a foundational TypeID to its canonical TypeDesc.
// Contract: public semantic contract — foundational type→descriptor mapping.
func DescribeType(typeID TypeID) (TypeDesc, error) {
	switch typeID {
	case TypeInvalid:
		return TypeDesc{Kind: TypeKindInvalid, Name: "invalid", TypeID: typeID}, nil
	case TypeVoid:
		return TypeDesc{Kind: TypeKindVoid, Name: "void", TypeID: typeID}, nil
	case TypeBool:
		return TypeDesc{Kind: TypeKindScalar, Name: "bool", TypeID: typeID}, nil
	case TypeByte:
		return TypeDesc{Kind: TypeKindScalar, Name: "byte", TypeID: typeID}, nil
	case TypeShort:
		return TypeDesc{Kind: TypeKindScalar, Name: "short", TypeID: typeID}, nil
	case TypeUShort:
		return TypeDesc{Kind: TypeKindScalar, Name: "ushort", TypeID: typeID}, nil
	case TypeInt:
		return TypeDesc{Kind: TypeKindScalar, Name: "int", TypeID: typeID}, nil
	case TypeUInt:
		return TypeDesc{Kind: TypeKindScalar, Name: "uint", TypeID: typeID}, nil
	case TypeLong:
		return TypeDesc{Kind: TypeKindScalar, Name: "long", TypeID: typeID}, nil
	case TypeULong:
		return TypeDesc{Kind: TypeKindScalar, Name: "ulong", TypeID: typeID}, nil
	case TypeFloat:
		return TypeDesc{Kind: TypeKindScalar, Name: "float", TypeID: typeID}, nil
	case TypeDouble:
		return TypeDesc{Kind: TypeKindScalar, Name: "double", TypeID: typeID}, nil
	case TypeString:
		return TypeDesc{Kind: TypeKindScalar, Name: "string", TypeID: typeID}, nil
	case TypeBytes:
		return TypeDesc{Kind: TypeKindScalar, Name: "bytes", TypeID: typeID}, nil
	case TypeObject:
		return TypeDesc{Kind: TypeKindScalar, Name: "object", TypeID: typeID}, nil
	case TypeAny:
		return TypeDesc{Kind: TypeKindScalar, Name: "any", TypeID: typeID}, nil
	}

	switch typeCategory(typeID) {
	case typeCategoryArray:
		elem, err := DescribeType(elementType(typeID))
		if err != nil {
			return TypeDesc{}, err
		}
		return TypeDesc{Kind: TypeKindArray, Name: "array", TypeID: typeID, Element: &elem}, nil
	case typeCategoryMap:
		key, err := DescribeType(keyType(typeID))
		if err != nil {
			return TypeDesc{}, err
		}
		value, err := DescribeType(valueType(typeID))
		if err != nil {
			return TypeDesc{}, err
		}
		return TypeDesc{Kind: TypeKindMap, Name: "map", TypeID: typeID, Key: &key, Value: &value}, nil
	case typeCategoryStruct:
		return TypeDesc{Kind: TypeKindStruct, Name: "struct", TypeID: typeID}, nil
	case typeCategoryClass:
		return TypeDesc{Kind: TypeKindClass, Name: "class", TypeID: typeID, ClassID: classIDFromType(typeID)}, nil
	case typeCategoryEnum:
		return TypeDesc{Kind: TypeKindEnum, Name: "enum", TypeID: typeID}, nil
	default:
		return TypeDesc{}, fmt.Errorf("unsupported type id: %d", typeID)
	}
}

// DescribeParameter maps a foundational ParameterDef to its canonical ParameterDesc.
// Contract: public semantic contract — foundational parameter→descriptor mapping.
func DescribeParameter(param ParameterDef) (ParameterDesc, error) {
	typeDesc, err := DescribeType(param.TypeID)
	if err != nil {
		return ParameterDesc{}, err
	}
	return ParameterDesc{Name: param.Name, Type: typeDesc}, nil
}

// DescribeObject maps a foundational Class to its canonical ObjectDesc.
// Contract: public semantic contract — foundational class→descriptor mapping.
// Foundational Class always represents a VM class, so Kind is set to TypeKindClass.
func DescribeObject(class *Class) (ObjectDesc, error) {
	fields := class.Fields
	if len(class.AllFields) > 0 {
		fields = class.AllFields
	}

	desc := ObjectDesc{
		Kind:   TypeKindClass,
		Name:   class.Name,
		Fields: make([]FieldDesc, 0, len(fields)),
	}
	for _, field := range fields {
		typeDesc, err := DescribeType(field.TypeID)
		if err != nil {
			return ObjectDesc{}, err
		}
		desc.Fields = append(desc.Fields, FieldDesc{Name: field.Name, Type: typeDesc})
	}
	return desc, nil
}

// DescribeGoFunction describes a Go function via reflection, producing a
// canonical CallableDesc. This is the primary entry point for Go function
// embedding into the schema-described callable surface.
// Contract: public semantic contract — Go function→callable descriptor generation.
func DescribeGoFunction(name string, fn any) (CallableDesc, error) {
	typ := reflect.TypeOf(fn)
	if typ == nil {
		return CallableDesc{}, fmt.Errorf("expected function, got nil")
	}
	if typ.Kind() != reflect.Func {
		return CallableDesc{}, fmt.Errorf("expected function, got %s", typ.Kind())
	}
	if typ.IsVariadic() {
		return CallableDesc{}, fmt.Errorf("unsupported Go function signature: variadic")
	}

	desc := CallableDesc{
		Name:       name,
		Parameters: make([]ParameterDesc, 0, typ.NumIn()),
		Returns:    make([]TypeDesc, 0, typ.NumOut()),
		Mode:       CallableModeUnary,
	}

	for i := 0; i < typ.NumIn(); i++ {
		typeDesc, err := DescribeReflectType(typ.In(i))
		if err != nil {
			return CallableDesc{}, err
		}
		desc.Parameters = append(desc.Parameters, ParameterDesc{
			Name: fmt.Sprintf("arg%d", i),
			Type: typeDesc,
		})
	}

	hasError, returns, err := describeFunctionReturns(typ)
	if err != nil {
		return CallableDesc{}, err
	}
	desc.HasError = hasError
	desc.Returns = returns
	return desc, nil
}

func describeFunctionReturns(typ reflect.Type) (bool, []TypeDesc, error) {
	if typ.NumOut() == 0 {
		return false, nil, nil
	}

	returns := make([]TypeDesc, 0, typ.NumOut())
	hasError := false
	for i := 0; i < typ.NumOut(); i++ {
		out := typ.Out(i)
		if out == errorType {
			if i != typ.NumOut()-1 {
				return false, nil, fmt.Errorf("unsupported Go function signature: error must be last return")
			}
			hasError = true
			continue
		}
		if len(returns) > 0 {
			return false, nil, fmt.Errorf("unsupported Go function signature: multiple non-error returns")
		}
		typeDesc, err := DescribeReflectType(out)
		if err != nil {
			return false, nil, err
		}
		returns = append(returns, typeDesc)
	}
	return hasError, returns, nil
}

// DescribeGoStruct describes a Go struct via reflection, producing a canonical
// ObjectDesc. This is the primary entry point for Go struct embedding into the
// schema-described object surface.
// Contract: public semantic contract — Go struct→object descriptor generation.
func DescribeGoStruct(value any) (ObjectDesc, error) {
	typ := reflect.TypeOf(value)
	if typ == nil {
		return ObjectDesc{}, fmt.Errorf("expected struct, got nil")
	}
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return ObjectDesc{}, fmt.Errorf("expected struct, got %s", typ.Kind())
	}
	return describeReflectStruct(typ)
}

// DescribeReflectType produces a TypeDesc from a Go reflect.Type.
// It is exported for use by the binding layer's adapter code, which
// needs to generate schema descriptors for struct field binding.
// Contract: public semantic contract — reflect.Type→TypeDesc mapping,
// consumed by the binding layer across the schema/binding plane boundary.
func DescribeReflectType(typ reflect.Type) (TypeDesc, error) {
	original := typ
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}

	if ordered, ok, err := DescribeOrderedMapType(original); ok || err != nil {
		return ordered, err
	}
	if ordered, ok, err := DescribeOrderedMapType(typ); ok || err != nil {
		return ordered, err
	}

	// schema.Media is the canonical host carrier of the media schema type.
	if typ.PkgPath() == "github.com/qomos-w/spore/schema" && typ.Name() == "Media" {
		return TypeDesc{Kind: TypeKindMedia, Name: "media"}, nil
	}

	switch typ.Kind() {
	case reflect.Bool:
		return DescribeType(TypeBool)
	case reflect.Int:
		return DescribeType(TypeInt)
	case reflect.Int8:
		return DescribeType(TypeByte)
	case reflect.Int16:
		return DescribeType(TypeShort)
	case reflect.Int32:
		return DescribeType(TypeInt)
	case reflect.Int64:
		return DescribeType(TypeLong)
	case reflect.Uint:
		return DescribeType(TypeUInt)
	case reflect.Uint8:
		return DescribeType(TypeByte)
	case reflect.Uint16:
		return DescribeType(TypeUShort)
	case reflect.Uint32:
		return DescribeType(TypeUInt)
	case reflect.Uint64:
		return DescribeType(TypeULong)
	case reflect.Float32:
		return DescribeType(TypeFloat)
	case reflect.Float64:
		return DescribeType(TypeDouble)
	case reflect.String:
		return DescribeType(TypeString)
	case reflect.Slice:
		if typ.Elem().Kind() == reflect.Uint8 {
			return DescribeType(TypeBytes)
		}
		elem, err := DescribeReflectType(typ.Elem())
		if err != nil {
			return TypeDesc{}, err
		}
		return TypeDesc{Kind: TypeKindArray, Name: "array", Element: &elem}, nil
	case reflect.Array:
		elem, err := DescribeReflectType(typ.Elem())
		if err != nil {
			return TypeDesc{}, err
		}
		return TypeDesc{Kind: TypeKindArray, Name: "array", Element: &elem}, nil
	case reflect.Map:
		key, err := DescribeReflectType(typ.Key())
		if err != nil {
			return TypeDesc{}, err
		}
		value, err := DescribeReflectType(typ.Elem())
		if err != nil {
			return TypeDesc{}, err
		}
		return TypeDesc{Kind: TypeKindMap, Name: "map", Key: &key, Value: &value}, nil
	case reflect.Struct:
		// time.Time is surfaced as an RFC3339 string across the script boundary.
		if typ.PkgPath() == "time" && typ.Name() == "Time" {
			return DescribeType(TypeString)
		}
		return TypeDesc{Kind: TypeKindStruct, Name: "struct", ClassName: typ.Name()}, nil
	case reflect.Interface:
		if typ.NumMethod() == 0 { // empty interface = any
			return DescribeType(TypeAny)
		}
		return TypeDesc{}, fmt.Errorf("unsupported Go type: %s", typ.String())
	default:
		return TypeDesc{}, fmt.Errorf("unsupported Go type: %s", typ.String())
	}
}

// DescribeOrderedMapType checks whether a Go type is an OrderedMap or
// SortedOrderedMap and, if so, returns its map schema descriptor.
// It is exported for use by the binding layer's value validation code.
// Contract: public semantic contract — OrderedMap type detection,
// consumed by the binding layer across the schema/binding plane boundary.
func DescribeOrderedMapType(typ reflect.Type) (TypeDesc, bool, error) {
	orderedType := typ
	if orderedType.Kind() == reflect.Pointer {
		orderedType = orderedType.Elem()
	}
	if !IsOrderedMapType(orderedType) {
		return TypeDesc{}, false, nil
	}

	methodType := orderedType
	if methodType.Kind() != reflect.Pointer {
		methodType = reflect.PointerTo(methodType)
	}
	entriesMethod, ok := methodType.MethodByName("Entries")
	if !ok {
		return TypeDesc{}, false, fmt.Errorf("unsupported OrderedMap shape: missing Entries method")
	}
	entriesType := entriesMethod.Type
	if entriesType.NumIn() != 1 || entriesType.NumOut() != 1 {
		return TypeDesc{}, false, fmt.Errorf("unsupported OrderedMap shape: Entries signature %s", entriesType.String())
	}
	resultType := entriesType.Out(0)
	if resultType.Kind() != reflect.Slice {
		return TypeDesc{}, false, fmt.Errorf("unsupported OrderedMap shape: Entries returns %s", resultType.Kind())
	}
	entryType := resultType.Elem()
	if entryType.PkgPath() != "github.com/qomos-w/spore/schema" || !strings.HasPrefix(entryType.Name(), "Entry[") {
		return TypeDesc{}, false, fmt.Errorf("unsupported OrderedMap shape: Entries element is %s", entryType.String())
	}
	if entryType.Kind() != reflect.Struct || entryType.NumField() != 2 {
		return TypeDesc{}, false, fmt.Errorf("unsupported OrderedMap shape: entry layout is %s", entryType.String())
	}

	keyField := entryType.Field(0)
	valueField := entryType.Field(1)
	if keyField.Name != "Key" || valueField.Name != "Value" || !keyField.IsExported() || !valueField.IsExported() {
		return TypeDesc{}, false, fmt.Errorf("unsupported OrderedMap shape: entry fields are %s and %s", keyField.Name, valueField.Name)
	}

	key, err := DescribeReflectType(keyField.Type)
	if err != nil {
		return TypeDesc{}, false, err
	}
	value, err := DescribeReflectType(valueField.Type)
	if err != nil {
		return TypeDesc{}, false, err
	}
	return TypeDesc{Kind: TypeKindMap, Name: "map", Key: &key, Value: &value}, true, nil
}

func describeReflectStruct(typ reflect.Type) (ObjectDesc, error) {
	desc := ObjectDesc{
		Kind:   TypeKindStruct,
		Name:   typ.Name(),
		Fields: make([]FieldDesc, 0, typ.NumField()),
	}
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}
		name := jsonTagName(field)
		if name == "-" {
			continue
		}
		typeDesc, err := DescribeReflectType(field.Type)
		if err != nil {
			return ObjectDesc{}, err
		}
		desc.Fields = append(desc.Fields, FieldDesc{
			Name:        name,
			Type:        typeDesc,
			Description: field.Tag.Get("description"),
			Optional:    jsonTagOmitEmpty(field),
		})
	}
	return desc, nil
}

// jsonTagName extracts the JSON field name from a struct field tag.
// Returns "-" if the field should be ignored, or the field name if no tag.
// This mirrors binding.JSONTagName without introducing a package dependency.
func jsonTagName(field reflect.StructField) string {
	tag := field.Tag.Get("json")
	if tag == "" {
		return field.Name
	}
	if tag == "-" {
		return "-"
	}
	if idx := strings.Index(tag, ","); idx >= 0 {
		tag = tag[:idx]
	}
	if tag == "" {
		return field.Name
	}
	return tag
}

// jsonTagOmitEmpty reports whether a struct field's json tag includes
// omitempty, indicating the field is semantically optional.
func jsonTagOmitEmpty(field reflect.StructField) bool {
	tag := field.Tag.Get("json")
	if tag == "" {
		return false
	}
	parts := strings.Split(tag, ",")
	for _, p := range parts[1:] {
		if strings.TrimSpace(p) == "omitempty" {
			return true
		}
	}
	return false
}
