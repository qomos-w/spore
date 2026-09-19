package frontend

import (
	"errors"
	"testing"

	"github.com/qomos-w/spore/diagnostics"
	"github.com/qomos-w/spore/schema"
)

func TestSchemaValidationError_NilReceiver(t *testing.T) {
	var e *schemaValidationError
	if e.Error() != "" {
		t.Fatalf("expected empty string for nil receiver, got %q", e.Error())
	}
	if e.Unwrap() != nil {
		t.Fatal("expected nil unwrap for nil receiver")
	}
	if e.DiagnosticCode() != "" {
		t.Fatalf("expected empty code for nil receiver, got %q", e.DiagnosticCode())
	}
	if e.DiagnosticCategory() != diagnostics.CategorySchema {
		t.Fatalf("expected default category for nil receiver, got %q", e.DiagnosticCategory())
	}
	if (e.DiagnosticSpan() != diagnostics.Span{}) {
		t.Fatal("expected empty span for nil receiver")
	}
	if e.DiagnosticPath() != "" {
		t.Fatalf("expected empty path for nil receiver, got %q", e.DiagnosticPath())
	}
	if e.DiagnosticCause() != nil {
		t.Fatal("expected nil cause for nil receiver")
	}
}

func TestSchemaValidationError_Unwrap(t *testing.T) {
	cause := errors.New("cause")
	e := &schemaValidationError{cause: cause}
	if e.Unwrap() != cause {
		t.Fatal("expected unwrap to return cause")
	}
}

func TestSchemaValidationError_DiagnosticCause(t *testing.T) {
	cause := errors.New("boom")
	e := &schemaValidationError{cause: cause}
	d := e.DiagnosticCause()
	if d == nil {
		t.Fatal("expected non-nil descriptor")
	}
	if d.Message != "boom" {
		t.Fatalf("expected message boom, got %q", d.Message)
	}
}

func TestSpanFromBlock_Nil(t *testing.T) {
	if (spanFromBlock(nil) != diagnostics.Span{}) {
		t.Fatal("expected empty span for nil block")
	}
}

func TestFirstReturn_Empty(t *testing.T) {
	if (firstReturn(nil) != schema.TypeDesc{}) {
		t.Fatal("expected empty TypeDesc for empty returns")
	}
}

func TestFirstReturn_NonEmpty(t *testing.T) {
	returns := []schema.TypeDesc{{Kind: schema.TypeKindScalar, Name: "int"}}
	if firstReturn(returns).Name != "int" {
		t.Fatal("expected first return")
	}
}

func TestExportedTypeName(t *testing.T) {
	tests := []struct {
		desc schema.TypeDesc
		want string
	}{
		{schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}, "int"},
		{schema.TypeDesc{Kind: schema.TypeKindArray, Name: "array"}, "array"},
		{schema.TypeDesc{Kind: schema.TypeKindArray, Name: "array", Element: &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}}, "[]int"},
		{schema.TypeDesc{Kind: schema.TypeKindMap, Name: "map"}, "map"},
		{schema.TypeDesc{Kind: schema.TypeKindMap, Name: "map", Key: &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}, Value: &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}}, "map[string]int"},
		{schema.TypeDesc{Kind: schema.TypeKindClass, Name: "class", ClassName: "Player"}, "class:Player"},
		{schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "struct", ClassName: "Point"}, "Point"},
		{schema.TypeDesc{Kind: schema.TypeKindInvalid, Name: "invalid"}, "invalid"},
	}
	for _, tc := range tests {
		got := exportedTypeName(tc.desc)
		if got != tc.want {
			t.Fatalf("exportedTypeName(%+v) = %q, want %q", tc.desc, got, tc.want)
		}
	}
}

func TestLeadingSpaceCount(t *testing.T) {
	if leadingSpaceCount("hello") != 0 {
		t.Fatal("expected 0 for no leading spaces")
	}
	if leadingSpaceCount("  hello") != 2 {
		t.Fatal("expected 2 for two leading spaces")
	}
	if leadingSpaceCount("\t\thello") != 2 {
		t.Fatal("expected 2 for two leading tabs")
	}
	if leadingSpaceCount(" \t hello") != 3 {
		t.Fatal("expected 3 for mixed leading whitespace")
	}
}

func TestValidateIdentifier(t *testing.T) {
	if err := validateIdentifier("hello", "name"); err != nil {
		t.Fatalf("expected nil for valid identifier, got %v", err)
	}
	if err := validateIdentifier("", "name"); err == nil {
		t.Fatal("expected error for empty identifier")
	}
	if err := validateIdentifier("123abc", "name"); err == nil {
		t.Fatal("expected error for identifier starting with digit")
	}
	if err := validateIdentifier("hello-world", "name"); err == nil {
		t.Fatal("expected error for identifier with hyphen")
	}
	if err := validateIdentifier("_private", "name"); err != nil {
		t.Fatalf("expected nil for underscore-start identifier, got %v", err)
	}
}

func TestIsIdentifierStart(t *testing.T) {
	if !isIdentifierStart('_') {
		t.Fatal("expected true for underscore")
	}
	if !isIdentifierStart('a') {
		t.Fatal("expected true for letter")
	}
	if isIdentifierStart('1') {
		t.Fatal("expected false for digit")
	}
}
