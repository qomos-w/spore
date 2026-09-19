package schema

import (
	"reflect"
	"testing"
	"time"
)

func makeArrayType(elemType TypeID) TypeID {
	elemCategory := uint64(typeCategory(elemType))
	elemData := uint64(elemType) & 0x0FFFFFFFFFFFFFFF
	return TypeID(uint64(typeCategoryArray)<<60 | elemCategory<<56 | elemData)
}

func makeMapType(keyTypeID, valueTypeID TypeID) TypeID {
	keyBits := uint64(keyTypeID) & 0x3FFFFFFF
	valueBits := uint64(valueTypeID) & 0x3FFFFFFF
	return TypeID(uint64(typeCategoryMap)<<60 | keyBits<<30 | valueBits)
}

func makeStructType() TypeID {
	return TypeID(uint64(typeCategoryStruct) << 60)
}

func makeClassType(classID uint32) TypeID {
	return TypeID(uint64(typeCategoryClass)<<60 | uint64(classID))
}

func TestDescribeType_BasicScalar(t *testing.T) {
	tests := []struct {
		name     string
		typeID   TypeID
		expected string
	}{
		{name: "int", typeID: TypeInt, expected: "int"},
		{name: "string", typeID: TypeString, expected: "string"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			desc, err := DescribeType(tt.typeID)
			if err != nil {
				t.Fatalf("DescribeType returned error: %v", err)
			}
			if desc.Kind != TypeKindScalar {
				t.Fatalf("expected scalar kind, got %s", desc.Kind)
			}
			if desc.Name != tt.expected {
				t.Fatalf("expected %s, got %s", tt.expected, desc.Name)
			}
		})
	}
}

func TestDescribeType_VoidAndInvalid(t *testing.T) {
	voidDesc, err := DescribeType(TypeVoid)
	if err != nil {
		t.Fatalf("DescribeType(TypeVoid) returned error: %v", err)
	}
	if voidDesc.Kind != TypeKindVoid {
		t.Fatalf("expected void kind, got %s", voidDesc.Kind)
	}

	invalidDesc, err := DescribeType(TypeInvalid)
	if err != nil {
		t.Fatalf("DescribeType(TypeInvalid) returned error: %v", err)
	}
	if invalidDesc.Kind != TypeKindInvalid {
		t.Fatalf("expected invalid kind, got %s", invalidDesc.Kind)
	}
}

func TestDescribeType_Array(t *testing.T) {
	typeID := makeArrayType(TypeInt)
	desc, err := DescribeType(typeID)
	if err != nil {
		t.Fatalf("DescribeType returned error: %v", err)
	}
	if desc.Kind != TypeKindArray {
		t.Fatalf("expected array kind, got %s", desc.Kind)
	}
	if desc.Element == nil {
		t.Fatal("expected element descriptor")
	}
	if desc.Element.Kind != TypeKindScalar || desc.Element.Name != "int" {
		t.Fatalf("expected int element, got %#v", desc.Element)
	}
}

func TestDescribeType_Map(t *testing.T) {
	typeID := makeMapType(TypeString, TypeInt)
	desc, err := DescribeType(typeID)
	if err != nil {
		t.Fatalf("DescribeType returned error: %v", err)
	}
	if desc.Kind != TypeKindMap {
		t.Fatalf("expected map kind, got %s", desc.Kind)
	}
	if desc.Key == nil || desc.Value == nil {
		t.Fatal("expected key/value descriptors")
	}
	if desc.Key.Name != "string" {
		t.Fatalf("expected string key, got %#v", desc.Key)
	}
	if desc.Value.Name != "int" {
		t.Fatalf("expected int value, got %#v", desc.Value)
	}
}

func TestDescribeType_Struct(t *testing.T) {
	typeID := makeStructType()
	desc, err := DescribeType(typeID)
	if err != nil {
		t.Fatalf("DescribeType returned error: %v", err)
	}
	if desc.Kind != TypeKindStruct {
		t.Fatalf("expected struct kind, got %s", desc.Kind)
	}
	if desc.Name != "struct" {
		t.Fatalf("expected struct name, got %s", desc.Name)
	}
}

func TestDescribeType_Class(t *testing.T) {
	typeID := makeClassType(42)
	desc, err := DescribeType(typeID)
	if err != nil {
		t.Fatalf("DescribeType returned error: %v", err)
	}
	if desc.Kind != TypeKindClass {
		t.Fatalf("expected class kind, got %s", desc.Kind)
	}
	if desc.ClassID != 42 {
		t.Fatalf("expected class id 42, got %d", desc.ClassID)
	}
}

func TestDescribeParameter_PreservesNameAndType(t *testing.T) {
	desc, err := DescribeParameter(ParameterDef{Name: "a", TypeID: TypeInt})
	if err != nil {
		t.Fatalf("DescribeParameter returned error: %v", err)
	}
	if desc.Name != "a" {
		t.Fatalf("expected name a, got %s", desc.Name)
	}
	if desc.Type.Kind != TypeKindScalar || desc.Type.Name != "int" {
		t.Fatalf("expected int scalar, got %#v", desc.Type)
	}
}

func TestDescribeParameter_InvalidType(t *testing.T) {
	desc, err := DescribeParameter(ParameterDef{Name: "bad", TypeID: TypeInvalid})
	if err != nil {
		t.Fatalf("DescribeParameter returned error: %v", err)
	}
	if desc.Type.Kind != TypeKindInvalid {
		t.Fatalf("expected invalid kind, got %s", desc.Type.Kind)
	}
}

func TestDescribeObject_FieldsOnlyContract(t *testing.T) {
	class := NewClass(1, "Player", nil)
	class.AddField("name", TypeString)
	class.AddField("level", TypeInt)
	class.ComputeFieldOffsets()

	desc, err := DescribeObject(class)
	if err != nil {
		t.Fatalf("DescribeObject returned error: %v", err)
	}
	if desc.Kind != TypeKindClass {
		t.Fatalf("expected Kind=TypeKindClass for foundational Class, got %q", desc.Kind)
	}
	if desc.Name != "Player" {
		t.Fatalf("expected class name Player, got %s", desc.Name)
	}
	if len(desc.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(desc.Fields))
	}
	if desc.Fields[0].Name != "name" || desc.Fields[0].Type.Name != "string" {
		t.Fatalf("unexpected first field: %#v", desc.Fields[0])
	}
	if desc.Fields[1].Name != "level" || desc.Fields[1].Type.Name != "int" {
		t.Fatalf("unexpected second field: %#v", desc.Fields[1])
	}
}

func TestDescribeObject_DoesNotDependOnVMExecution(t *testing.T) {
	class := NewClass(2, "Config", nil)
	class.AddField("enabled", TypeBool)
	class.ComputeFieldOffsets()

	desc, err := DescribeObject(class)
	if err != nil {
		t.Fatalf("DescribeObject returned error: %v", err)
	}
	if len(desc.Fields) != 1 {
		t.Fatalf("expected 1 field, got %d", len(desc.Fields))
	}
	if desc.Fields[0].Type.Name != "bool" {
		t.Fatalf("expected bool field, got %#v", desc.Fields[0])
	}
}

func TestDescribeGoStruct_TreatsOrderedMapAsMapSemanticFamily(t *testing.T) {
	typ := reflect.TypeOf((*OrderedMap[string, int])(nil))
	desc, err := DescribeReflectType(typ)
	if probe, ok, probeErr := DescribeOrderedMapType(typ); probeErr != nil {
		t.Fatalf("DescribeOrderedMapType returned error: %v", probeErr)
	} else if !ok {
		t.Fatal("expected DescribeOrderedMapType to recognize OrderedMap")
	} else if probe.Kind != TypeKindMap {
		t.Fatalf("expected ordered map probe to be map, got %#v", probe)
	}
	if err != nil {
		t.Fatalf("DescribeReflectType returned error: %v", err)
	}
	if desc.Kind != TypeKindMap || desc.Key == nil || desc.Value == nil {
		t.Fatalf("expected map type for ordered map, got %#v", desc)
	}
	if desc.Key.Name != "string" || desc.Value.Name != "int" {
		t.Fatalf("unexpected ordered map descriptors: %#v", desc)
	}
}

func TestDescribeReflectType_ScalarKinds(t *testing.T) {
	tests := []struct {
		name     string
		value    any
		wantName string
	}{
		{"bool", true, "bool"},
		{"int", int(0), "int"},
		{"int8", int8(0), "byte"},
		{"int16", int16(0), "short"},
		{"int32", int32(0), "int"},
		{"int64", int64(0), "long"},
		{"uint", uint(0), "uint"},
		{"uint8", uint8(0), "byte"},
		{"uint16", uint16(0), "ushort"},
		{"uint32", uint32(0), "uint"},
		{"uint64", uint64(0), "ulong"},
		{"float32", float32(0), "float"},
		{"float64", float64(0), "double"},
		{"string", "", "string"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			desc, err := DescribeReflectType(reflect.TypeOf(tc.value))
			if err != nil {
				t.Fatalf("DescribeReflectType: %v", err)
			}
			if desc.Kind != TypeKindScalar || desc.Name != tc.wantName {
				t.Fatalf("expected scalar %q, got %#v", tc.wantName, desc)
			}
		})
	}
}

func TestDescribeReflectType_Slice(t *testing.T) {
	// []byte maps to bytes scalar
	desc, err := DescribeReflectType(reflect.TypeOf([]byte(nil)))
	if err != nil {
		t.Fatalf("DescribeReflectType: %v", err)
	}
	if desc.Kind != TypeKindScalar || desc.Name != "bytes" {
		t.Fatalf("expected bytes scalar, got %#v", desc)
	}

	// []int maps to array of int
	desc, err = DescribeReflectType(reflect.TypeOf([]int(nil)))
	if err != nil {
		t.Fatalf("DescribeReflectType: %v", err)
	}
	if desc.Kind != TypeKindArray || desc.Element == nil || desc.Element.Name != "int" {
		t.Fatalf("expected array of int, got %#v", desc)
	}
}

func TestDescribeReflectType_Array(t *testing.T) {
	desc, err := DescribeReflectType(reflect.TypeOf([3]int{}))
	if err != nil {
		t.Fatalf("DescribeReflectType: %v", err)
	}
	if desc.Kind != TypeKindArray || desc.Element == nil || desc.Element.Name != "int" {
		t.Fatalf("expected array of int, got %#v", desc)
	}
}

func TestDescribeReflectType_Map(t *testing.T) {
	desc, err := DescribeReflectType(reflect.TypeOf(map[string]int(nil)))
	if err != nil {
		t.Fatalf("DescribeReflectType: %v", err)
	}
	if desc.Kind != TypeKindMap || desc.Key == nil || desc.Value == nil {
		t.Fatalf("expected map kind with key/value, got %#v", desc)
	}
	if desc.Key.Name != "string" || desc.Value.Name != "int" {
		t.Fatalf("unexpected map descriptors: key=%+v value=%+v", desc.Key, desc.Value)
	}
}

func TestDescribeReflectType_Struct(t *testing.T) {
	type Point struct{ X, Y int }
	desc, err := DescribeReflectType(reflect.TypeOf(Point{}))
	if err != nil {
		t.Fatalf("DescribeReflectType: %v", err)
	}
	if desc.Kind != TypeKindStruct || desc.ClassName != "Point" {
		t.Fatalf("expected struct Point, got %#v", desc)
	}
}

func TestDescribeReflectType_Time(t *testing.T) {
	desc, err := DescribeReflectType(reflect.TypeOf(struct{ When time.Time }{}).Field(0).Type)
	if err != nil {
		t.Fatalf("DescribeReflectType: %v", err)
	}
	if desc.Kind != TypeKindScalar || desc.Name != "string" {
		t.Fatalf("expected time.Time to be described as string scalar, got %#v", desc)
	}
}

func TestDescribeReflectType_PointerDeref(t *testing.T) {
	desc, err := DescribeReflectType(reflect.TypeOf((*int)(nil)))
	if err != nil {
		t.Fatalf("DescribeReflectType: %v", err)
	}
	if desc.Kind != TypeKindScalar || desc.Name != "int" {
		t.Fatalf("expected int scalar after pointer deref, got %#v", desc)
	}
}

func TestDescribeReflectType_Unsupported(t *testing.T) {
	// channel
	if _, err := DescribeReflectType(reflect.TypeOf(make(chan int))); err == nil {
		t.Fatal("expected error for channel type")
	}
	// func
	if _, err := DescribeReflectType(reflect.TypeOf(func() {})); err == nil {
		t.Fatal("expected error for func type")
	}
	// complex
	if _, err := DescribeReflectType(reflect.TypeOf(complex(1, 2))); err == nil {
		t.Fatal("expected error for complex type")
	}
}

func TestDescribeType_UnsupportedTypeID(t *testing.T) {
	// A random type ID that does not match any known type
	_, err := DescribeType(0xFFFFFFFF)
	if err == nil {
		t.Fatal("expected error for unsupported type id")
	}
}

func TestDescribeParameter_InvalidTypeID(t *testing.T) {
	_, err := DescribeParameter(ParameterDef{Name: "bad", TypeID: 0xFFFFFFFF})
	if err == nil {
		t.Fatal("expected error for invalid type id")
	}
}

type orderedMapStructProjection struct {
	Items *OrderedMap[string, int]
}

type badFieldStruct struct {
	X int
	Y chan int
}

func TestDescribeGoStruct_RejectsUnsupportedFieldType(t *testing.T) {
	_, err := DescribeGoStruct(badFieldStruct{})
	if err == nil {
		t.Fatal("expected error for unsupported field type")
	}
}

func TestDescribeGoStruct_ProjectsOrderedMapFieldAsMap(t *testing.T) {
	desc, err := DescribeGoStruct(orderedMapStructProjection{})
	if err != nil {
		t.Fatalf("DescribeGoStruct returned error: %v", err)
	}
	if desc.Kind != TypeKindStruct {
		t.Fatalf("expected Kind=TypeKindStruct for Go struct, got %q", desc.Kind)
	}
	if desc.Name != "orderedMapStructProjection" {
		t.Fatalf("expected struct name orderedMapStructProjection, got %s", desc.Name)
	}
	if len(desc.Fields) != 1 {
		t.Fatalf("expected 1 field, got %d", len(desc.Fields))
	}
	field := desc.Fields[0]
	if field.Name != "Items" {
		t.Fatalf("expected field Items, got %#v", field)
	}
	if field.Type.Kind != TypeKindMap || field.Type.Key == nil || field.Type.Value == nil {
		t.Fatalf("expected OrderedMap field to project as map, got %#v", field.Type)
	}
	if field.Type.Key.Name != "string" || field.Type.Value.Name != "int" {
		t.Fatalf("unexpected OrderedMap field descriptors: %#v", field.Type)
	}
}

// TestDescribeGoStruct_CapturesDescriptionTag locks in that the
// `description:"..."` struct tag flows through to FieldDesc.Description,
// which downstream codegen renders as JSDoc on the TypeScript surface.
func TestDescribeGoStruct_CapturesDescriptionTag(t *testing.T) {
	type Account struct {
		ID    string `json:"id" description:"opaque account identifier"`
		Email string `json:"email"`
	}

	desc, err := DescribeGoStruct(Account{})
	if err != nil {
		t.Fatalf("DescribeGoStruct: %v", err)
	}
	if len(desc.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(desc.Fields))
	}
	if got, want := desc.Fields[0].Description, "opaque account identifier"; got != want {
		t.Errorf("Fields[0].Description = %q, want %q", got, want)
	}
	if desc.Fields[1].Description != "" {
		t.Errorf("Fields[1].Description should be empty without tag, got %q", desc.Fields[1].Description)
	}
}
