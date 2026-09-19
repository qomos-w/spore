package schema

import (
	"fmt"
	"testing"
)

// ============================================================================
// §9.1 Schema surface contract tests
//
// These tests lock the canonical schema surface contracts defined in TDD §9.1.
// Each test corresponds to a checklist item and verifies that the schema
// descriptor behavior is stable, independent, and complete.
// ============================================================================

// TestSchemaSurface_DescriptorInputOutputIsStable locks §9.1 item 1:
// "基座脚本 descriptor 有稳定输入输出语义"
//
// Verifies that DescribeType, DescribeParameter, and DescribeObject produce
// identical outputs for identical inputs (idempotent), and that their outputs
// are not affected by external state changes.
func TestSchemaSurface_DescriptorInputOutputIsStable(t *testing.T) {
	t.Run("DescribeType_idempotent", func(t *testing.T) {
		desc1, err := DescribeType(TypeInt)
		if err != nil {
			t.Fatalf("DescribeType: %v", err)
		}
		desc2, err := DescribeType(TypeInt)
		if err != nil {
			t.Fatalf("DescribeType second call: %v", err)
		}
		if desc1 != desc2 {
			t.Fatalf("DescribeType not idempotent: %+v vs %+v", desc1, desc2)
		}
	})

	t.Run("DescribeParameter_idempotent", func(t *testing.T) {
		param := ParameterDef{Name: "x", TypeID: TypeString}
		desc1, err := DescribeParameter(param)
		if err != nil {
			t.Fatalf("DescribeParameter: %v", err)
		}
		desc2, err := DescribeParameter(param)
		if err != nil {
			t.Fatalf("DescribeParameter second call: %v", err)
		}
		if desc1 != desc2 {
			t.Fatalf("DescribeParameter not idempotent: %+v vs %+v", desc1, desc2)
		}
	})

	t.Run("DescribeObject_idempotent", func(t *testing.T) {
		class := NewClass(1, "Test", nil)
		class.AddField("name", TypeString)
		class.AddField("level", TypeInt)
		class.ComputeFieldOffsets()

		desc1, err := DescribeObject(class)
		if err != nil {
			t.Fatalf("DescribeObject: %v", err)
		}
		desc2, err := DescribeObject(class)
		if err != nil {
			t.Fatalf("DescribeObject second call: %v", err)
		}
		if desc1.Kind != desc2.Kind || desc1.Name != desc2.Name || len(desc1.Fields) != len(desc2.Fields) {
			t.Fatalf("DescribeObject not idempotent: %+v vs %+v", desc1, desc2)
		}
		for i := range desc1.Fields {
			if desc1.Fields[i] != desc2.Fields[i] {
				t.Fatalf("DescribeObject field %d not idempotent: %+v vs %+v", i, desc1.Fields[i], desc2.Fields[i])
			}
		}
	})

	t.Run("DescribeType_array_composition", func(t *testing.T) {
		// Verify that composite types decompose correctly and stably
		arrayID := makeArrayType(TypeString)
		desc1, err := DescribeType(arrayID)
		if err != nil {
			t.Fatalf("DescribeType array: %v", err)
		}
		desc2, err := DescribeType(arrayID)
		if err != nil {
			t.Fatalf("DescribeType array second call: %v", err)
		}
		if desc1.Kind != TypeKindArray || desc1.Element == nil || desc1.Element.Name != "string" {
			t.Fatalf("unexpected array descriptor: %+v", desc1)
		}
		// Pointer fields prevent == comparison; compare by value semantics
		if desc1.Kind != desc2.Kind || desc1.Name != desc2.Name || desc1.TypeID != desc2.TypeID {
			t.Fatalf("DescribeType array not idempotent (top-level): %+v vs %+v", desc1, desc2)
		}
		if desc1.Element == nil || desc2.Element == nil {
			t.Fatal("expected non-nil Element for array descriptors")
		}
		if *desc1.Element != *desc2.Element {
			t.Fatalf("DescribeType array element not idempotent: %+v vs %+v", *desc1.Element, *desc2.Element)
		}
	})

	t.Run("DescribeType_output_not_affected_by_external_state", func(t *testing.T) {
		// Calling DescribeType multiple times after external state changes
		// should produce the same result — descriptor output depends only on input
		desc1, _ := DescribeType(TypeDouble)

		// Create and discard another class — this should not affect the previous result
		class := NewClass(99, "Noise", nil)
		class.AddField("x", TypeDouble)
		class.ComputeFieldOffsets()
		_, _ = DescribeObject(class)

		desc2, err := DescribeType(TypeDouble)
		if err != nil {
			t.Fatalf("DescribeType after noise: %v", err)
		}
		if desc1 != desc2 {
			t.Fatalf("DescribeType output changed after unrelated state change: %+v vs %+v", desc1, desc2)
		}
	})
}

// TestSchemaSurface_CanExpressFunctionSignature locks §9.1 item 3:
// "可表达函数签名"
//
// Verifies that CallableDesc captures a complete function signature including
// name, parameters with types, return types, error marking, and mode.
func TestSchemaSurface_CanExpressFunctionSignature(t *testing.T) {
	add := func(a int, b int) int { return a + b }

	desc, err := DescribeGoFunction("add", add)
	if err != nil {
		t.Fatalf("DescribeGoFunction: %v", err)
	}

	// Name
	if desc.Name != "add" {
		t.Fatalf("expected callable name 'add', got %q", desc.Name)
	}

	// Parameters
	if len(desc.Parameters) != 2 {
		t.Fatalf("expected 2 parameters, got %d", len(desc.Parameters))
	}
	if desc.Parameters[0].Name != "arg0" || desc.Parameters[0].Type.Kind != TypeKindScalar || desc.Parameters[0].Type.Name != "int" {
		t.Fatalf("unexpected first parameter: %+v", desc.Parameters[0])
	}
	if desc.Parameters[1].Name != "arg1" || desc.Parameters[1].Type.Kind != TypeKindScalar || desc.Parameters[1].Type.Name != "int" {
		t.Fatalf("unexpected second parameter: %+v", desc.Parameters[1])
	}

	// Return type
	if len(desc.Returns) != 1 {
		t.Fatalf("expected 1 return type, got %d", len(desc.Returns))
	}
	if desc.Returns[0].Kind != TypeKindScalar || desc.Returns[0].Name != "int" {
		t.Fatalf("expected int return, got %+v", desc.Returns[0])
	}

	// Error marking
	if desc.HasError {
		t.Fatal("expected HasError=false for pure function")
	}

	// Mode
	if desc.Mode != CallableModeUnary {
		t.Fatalf("expected unary mode, got %s", desc.Mode)
	}

	// Function with error
	load := func(id int) (string, error) { return "", fmt.Errorf("not found") }
	loadDesc, err := DescribeGoFunction("load", load)
	if err != nil {
		t.Fatalf("DescribeGoFunction load: %v", err)
	}
	if !loadDesc.HasError {
		t.Fatal("expected HasError=true for function returning error")
	}
	if len(loadDesc.Returns) != 1 || loadDesc.Returns[0].Name != "string" {
		t.Fatalf("expected string return with error, got %+v", loadDesc.Returns)
	}
}

// TestSchemaSurface_CanExpressObjectStructure locks §9.1 item 4:
// "可表达对象/类结构"
//
// Verifies that ObjectDesc captures complete object structure including name,
// fields with types, nested structs, arrays, and maps.
func TestSchemaSurface_CanExpressObjectStructure(t *testing.T) {
	t.Run("simple_struct", func(t *testing.T) {
		type simple struct {
			Name  string
			Level int
		}
		desc, err := DescribeGoStruct(simple{})
		if err != nil {
			t.Fatalf("DescribeGoStruct: %v", err)
		}
		if desc.Name != "simple" {
			t.Fatalf("expected class name 'simple', got %q", desc.Name)
		}
		if len(desc.Fields) != 2 {
			t.Fatalf("expected 2 fields, got %d", len(desc.Fields))
		}
		if desc.Fields[0].Name != "Name" || desc.Fields[0].Type.Kind != TypeKindScalar {
			t.Fatalf("unexpected Name field: %+v", desc.Fields[0])
		}
		if desc.Fields[1].Name != "Level" || desc.Fields[1].Type.Kind != TypeKindScalar {
			t.Fatalf("unexpected Level field: %+v", desc.Fields[1])
		}
	})

	t.Run("nested_struct", func(t *testing.T) {
		type inner struct{ X int }
		type outer struct {
			Pos inner
		}
		desc, err := DescribeGoStruct(outer{})
		if err != nil {
			t.Fatalf("DescribeGoStruct: %v", err)
		}
		if len(desc.Fields) != 1 {
			t.Fatalf("expected 1 field, got %d", len(desc.Fields))
		}
		posField := desc.Fields[0]
		if posField.Name != "Pos" || posField.Type.Kind != TypeKindStruct {
			t.Fatalf("expected Pos as struct, got %+v", posField)
		}
		if posField.Type.ClassName != "inner" {
			t.Fatalf("expected inner class name, got %q", posField.Type.ClassName)
		}
	})

	t.Run("array_and_map_fields", func(t *testing.T) {
		type container struct {
			Items []string
			Tags  map[string]int
		}
		desc, err := DescribeGoStruct(container{})
		if err != nil {
			t.Fatalf("DescribeGoStruct: %v", err)
		}
		items := desc.Fields[0]
		if items.Type.Kind != TypeKindArray || items.Type.Element == nil || items.Type.Element.Name != "string" {
			t.Fatalf("expected Items as []string, got %+v", items.Type)
		}
		tags := desc.Fields[1]
		if tags.Type.Kind != TypeKindMap || tags.Type.Key == nil || tags.Type.Value == nil {
			t.Fatalf("expected Tags as map, got %+v", tags.Type)
		}
		if tags.Type.Key.Name != "string" || tags.Type.Value.Name != "int" {
			t.Fatalf("expected map[string]int, got key=%s value=%s", tags.Type.Key.Name, tags.Type.Value.Name)
		}
	})

	t.Run("foundational_class", func(t *testing.T) {
		class := NewClass(10, "Player", nil)
		class.AddField("name", TypeString)
		class.AddField("level", TypeInt)
		class.ComputeFieldOffsets()

		desc, err := DescribeObject(class)
		if err != nil {
			t.Fatalf("DescribeObject: %v", err)
		}
		if desc.Name != "Player" {
			t.Fatalf("expected Player, got %q", desc.Name)
		}
		if len(desc.Fields) != 2 {
			t.Fatalf("expected 2 fields, got %d", len(desc.Fields))
		}
		if desc.Fields[0].Name != "name" || desc.Fields[0].Type.Name != "string" {
			t.Fatalf("unexpected name field: %+v", desc.Fields[0])
		}
		if desc.Fields[1].Name != "level" || desc.Fields[1].Type.Name != "int" {
			t.Fatalf("unexpected level field: %+v", desc.Fields[1])
		}
	})
}
