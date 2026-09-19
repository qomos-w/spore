package binding

import (
	"reflect"
	"testing"

	"github.com/qomos-w/spore/schema"
)

func TestValidateTypeCompatibility_DifferentKind(t *testing.T) {
	a := schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}
	b := schema.TypeDesc{Kind: schema.TypeKindArray, Name: "array"}
	if err := validateTypeCompatibility(a, b); err == nil {
		t.Fatal("expected error for different kinds")
	}
}

func TestValidateTypeCompatibility_ScalarNameMismatch(t *testing.T) {
	a := schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}
	b := schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}
	if err := validateTypeCompatibility(a, b); err == nil {
		t.Fatal("expected error for scalar name mismatch")
	}
}

func TestValidateTypeCompatibility_NonScalarNameMismatch(t *testing.T) {
	a := schema.TypeDesc{Kind: schema.TypeKindArray, Name: "list"}
	b := schema.TypeDesc{Kind: schema.TypeKindArray, Name: "array"}
	if err := validateTypeCompatibility(a, b); err == nil {
		t.Fatal("expected error for non-scalar name mismatch")
	}
}

func TestValidateTypeCompatibility_ArrayBothNilElements(t *testing.T) {
	a := schema.TypeDesc{Kind: schema.TypeKindArray, Name: "array"}
	b := schema.TypeDesc{Kind: schema.TypeKindArray, Name: "array"}
	if err := validateTypeCompatibility(a, b); err != nil {
		t.Fatalf("expected nil for both nil elements, got %v", err)
	}
}

func TestValidateTypeCompatibility_ArrayOneNilElementAllowed(t *testing.T) {
	// One nil element is tolerated; only non-nil mismatches are errors
	a := schema.TypeDesc{Kind: schema.TypeKindArray, Name: "array", Element: &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}}
	b := schema.TypeDesc{Kind: schema.TypeKindArray, Name: "array"}
	if err := validateTypeCompatibility(a, b); err != nil {
		t.Fatalf("expected nil for one nil element, got %v", err)
	}
}

func TestValidateTypeCompatibility_ArrayRecursive(t *testing.T) {
	elem := schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}
	a := schema.TypeDesc{Kind: schema.TypeKindArray, Name: "array", Element: &elem}
	b := schema.TypeDesc{Kind: schema.TypeKindArray, Name: "array", Element: &elem}
	if err := validateTypeCompatibility(a, b); err != nil {
		t.Fatalf("expected nil for matching array elements, got %v", err)
	}
}

func TestValidateTypeCompatibility_MapBothNilKeyValue(t *testing.T) {
	a := schema.TypeDesc{Kind: schema.TypeKindMap, Name: "map"}
	b := schema.TypeDesc{Kind: schema.TypeKindMap, Name: "map"}
	if err := validateTypeCompatibility(a, b); err != nil {
		t.Fatalf("expected nil for both nil key/value, got %v", err)
	}
}

func TestValidateTypeCompatibility_MapOneNilKeyValueAllowed(t *testing.T) {
	// One nil key/value is tolerated; only non-nil mismatches are errors
	k := schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}
	a := schema.TypeDesc{Kind: schema.TypeKindMap, Name: "map", Key: &k, Value: &k}
	b := schema.TypeDesc{Kind: schema.TypeKindMap, Name: "map"}
	if err := validateTypeCompatibility(a, b); err != nil {
		t.Fatalf("expected nil for one nil key/value, got %v", err)
	}
}

func TestValidateTypeCompatibility_MapRecursive(t *testing.T) {
	k := schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}
	v := schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}
	a := schema.TypeDesc{Kind: schema.TypeKindMap, Name: "map", Key: &k, Value: &v}
	b := schema.TypeDesc{Kind: schema.TypeKindMap, Name: "map", Key: &k, Value: &v}
	if err := validateTypeCompatibility(a, b); err != nil {
		t.Fatalf("expected nil for matching map key/value, got %v", err)
	}
}

func TestMatchesScalarType(t *testing.T) {
	tests := []struct {
		name string
		kind reflect.Kind
		want bool
	}{
		{"bool", reflect.Bool, true},
		{"bool", reflect.Int, false},
		{"byte", reflect.Int8, true},
		{"byte", reflect.Uint8, true},
		{"byte", reflect.Int, false},
		{"short", reflect.Int16, true},
		{"short", reflect.Int32, false},
		{"ushort", reflect.Uint16, true},
		{"ushort", reflect.Uint32, false},
		{"int", reflect.Int, true},
		{"int", reflect.Int32, true},
		{"int", reflect.Int64, false},
		{"uint", reflect.Uint, true},
		{"uint", reflect.Uint32, true},
		{"uint", reflect.Uint64, false},
		{"long", reflect.Int64, true},
		{"long", reflect.Int, true},
		{"ulong", reflect.Uint64, true},
		{"ulong", reflect.Uint, false},
		{"float", reflect.Float32, true},
		{"float", reflect.Float64, false},
		{"double", reflect.Float64, true},
		{"double", reflect.Float32, false},
		{"string", reflect.String, true},
		{"string", reflect.Int, false},
		{"bytes", reflect.Slice, true},
		{"bytes", reflect.String, false},
		{"object", reflect.Interface, true},
		{"any", reflect.Map, true},
		{"unknown", reflect.Int, false},
	}
	for _, tc := range tests {
		got := matchesScalarType(tc.name, tc.kind)
		if got != tc.want {
			t.Fatalf("matchesScalarType(%q, %v) = %v, want %v", tc.name, tc.kind, got, tc.want)
		}
	}
}
