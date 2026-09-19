package schema

import "testing"

func TestClassAddFieldMaintainsDeclaredAndAllFields(t *testing.T) {
	cls := NewClass(7, "Player", nil)
	cls.AddField("hp", TypeInt)
	cls.AddField("name", TypeString)

	if cls.ID != 7 {
		t.Fatalf("expected class id 7, got %d", cls.ID)
	}
	if cls.Name != "Player" {
		t.Fatalf("expected class name Player, got %q", cls.Name)
	}
	if len(cls.Fields) != 2 {
		t.Fatalf("expected 2 declared fields, got %d", len(cls.Fields))
	}
	if len(cls.AllFields) != 2 {
		t.Fatalf("expected 2 all fields, got %d", len(cls.AllFields))
	}
	if cls.Fields[0].Name != "hp" || cls.Fields[0].TypeID != TypeInt {
		t.Fatalf("unexpected first field: %+v", cls.Fields[0])
	}
	if cls.AllFields[1].Name != "name" || cls.AllFields[1].TypeID != TypeString {
		t.Fatalf("unexpected second all field: %+v", cls.AllFields[1])
	}
}

func TestClassComputeFieldOffsetsIsSafeNoOp(t *testing.T) {
	cls := NewClass(1, "Safe", nil)
	cls.AddField("value", TypeLong)
	cls.ComputeFieldOffsets()

	if len(cls.Fields) != 1 || len(cls.AllFields) != 1 {
		t.Fatalf("expected fields to remain unchanged after ComputeFieldOffsets, got fields=%d allFields=%d", len(cls.Fields), len(cls.AllFields))
	}
}

func TestFoundationalTypeIDsAreStable(t *testing.T) {
	cases := []struct {
		name string
		got  TypeID
		want TypeID
	}{
		{"invalid", TypeInvalid, 0},
		{"void", TypeVoid, 1},
		{"bool", TypeBool, 2},
		{"int", TypeInt, 6},
		{"long", TypeLong, 8},
		{"double", TypeDouble, 11},
		{"string", TypeString, 12},
		{"any", TypeAny, 15},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Fatalf("expected %s TypeID %d, got %d", tc.name, tc.want, tc.got)
			}
		})
	}
}
