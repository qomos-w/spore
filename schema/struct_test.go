package schema

import "testing"

type playerProfile struct {
	DisplayName string
	Level       int
}

type inventoryItem struct {
	ID    int
	Count int
}

type playerInventory struct {
	Items []inventoryItem
	Tags  map[string]int
}

type playerWithProfile struct {
	Name    string
	Profile playerProfile
}

func TestDescribeGoStruct_ProjectsExportedFields(t *testing.T) {
	desc, err := DescribeGoStruct(playerProfile{})
	if err != nil {
		t.Fatalf("DescribeGoStruct returned error: %v", err)
	}
	if desc.Name != "playerProfile" {
		t.Fatalf("expected struct name playerProfile, got %s", desc.Name)
	}
	if len(desc.Fields) != 2 {
		t.Fatalf("expected 2 exported fields, got %d", len(desc.Fields))
	}
	if desc.Fields[0].Name != "DisplayName" || desc.Fields[0].Type.Name != "string" {
		t.Fatalf("unexpected first field: %#v", desc.Fields[0])
	}
	if desc.Fields[1].Name != "Level" || desc.Fields[1].Type.Name != "int" {
		t.Fatalf("unexpected second field: %#v", desc.Fields[1])
	}
}

func TestDescribeGoStruct_AcceptsPointerToStruct(t *testing.T) {
	desc, err := DescribeGoStruct(&playerProfile{})
	if err != nil {
		t.Fatalf("DescribeGoStruct returned error: %v", err)
	}
	if desc.Name != "playerProfile" {
		t.Fatalf("expected struct name playerProfile, got %s", desc.Name)
	}
}

func TestDescribeGoStruct_RejectsNonStruct(t *testing.T) {
	_, err := DescribeGoStruct(1)
	if err == nil {
		t.Fatal("expected error for non-struct value")
	}
}

func TestDescribeGoStruct_ProjectsSliceAndMap(t *testing.T) {
	desc, err := DescribeGoStruct(playerInventory{})
	if err != nil {
		t.Fatalf("DescribeGoStruct returned error: %v", err)
	}
	if len(desc.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(desc.Fields))
	}

	items := desc.Fields[0]
	if items.Name != "Items" || items.Type.Kind != TypeKindArray || items.Type.Element == nil {
		t.Fatalf("expected Items array descriptor, got %#v", items)
	}
	if items.Type.Element.Kind != TypeKindStruct || items.Type.Element.ClassName != "inventoryItem" {
		t.Fatalf("expected inventoryItem element, got %#v", items.Type.Element)
	}

	tags := desc.Fields[1]
	if tags.Name != "Tags" || tags.Type.Kind != TypeKindMap || tags.Type.Key == nil || tags.Type.Value == nil {
		t.Fatalf("expected Tags map descriptor, got %#v", tags)
	}
	if tags.Type.Key.Name != "string" || tags.Type.Value.Name != "int" {
		t.Fatalf("expected map[string]int descriptor, got %#v", tags.Type)
	}
}

func TestDescribeGoStruct_ProjectsNestedStruct(t *testing.T) {
	desc, err := DescribeGoStruct(playerWithProfile{})
	if err != nil {
		t.Fatalf("DescribeGoStruct returned error: %v", err)
	}
	if len(desc.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(desc.Fields))
	}
	profile := desc.Fields[1]
	if profile.Name != "Profile" || profile.Type.Kind != TypeKindStruct {
		t.Fatalf("expected Profile struct descriptor, got %#v", profile)
	}
	if profile.Type.ClassName != "playerProfile" {
		t.Fatalf("expected nested struct class name playerProfile, got %s", profile.Type.ClassName)
	}
}
