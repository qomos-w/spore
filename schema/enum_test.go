package schema

import "testing"

func TestEnumDesc_CloneDeepCopiesMembers(t *testing.T) {
	desc := EnumDesc{Name: "Color", Members: []EnumMemberDesc{{Name: "Red", Value: 1}, {Name: "Green", Value: 2}}}
	cloned := CloneEnumDesc(desc)
	cloned.Members[0].Value = 99
	if desc.Members[0].Value != 1 {
		t.Fatalf("CloneEnumDesc shared member slice: %+v", desc.Members)
	}
}

func TestTypeDesc_StringForEnumKind(t *testing.T) {
	td := TypeDesc{Kind: TypeKindEnum, Name: "Color"}
	if got := td.String(); got != "Color" {
		t.Fatalf("expected Color, got %q", got)
	}
	td = TypeDesc{Kind: TypeKindEnum}
	if got := td.String(); got != "enum" {
		t.Fatalf("expected enum, got %q", got)
	}
}

func TestMakeEnumTypeID_DescribeRoundTrip(t *testing.T) {
	tid := MakeEnumTypeID(7)
	desc, err := DescribeType(tid)
	if err != nil {
		t.Fatalf("DescribeType: %v", err)
	}
	if desc.Kind != TypeKindEnum {
		t.Fatalf("expected enum kind, got %q", desc.Kind)
	}
	if desc.TypeID != tid {
		t.Fatalf("expected TypeID %d preserved, got %d", tid, desc.TypeID)
	}
}
