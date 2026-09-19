package gotypes

import (
	"strings"
	"testing"

	"github.com/qomos-w/spore/schema"
)

// TestMapScalar_SharedTableCoverage locks the scalar contract go-types now
// inherits from internal/gen/common. Before the convergence its private table
// knew only the fixed-width aliases (int32/int64/…) plus float/double, so the
// canonical Spore keywords byte/short/ushort and the narrow aliases
// int8/int16/uint8/uint16/float32/float64 failed generation with
// "unknown scalar".
func TestMapScalar_SharedTableCoverage(t *testing.T) {
	cases := map[string]string{
		"bool":    "bool",
		"string":  "string",
		"byte":    "int8",
		"int8":    "int8",
		"short":   "int16",
		"int16":   "int16",
		"ushort":  "uint16",
		"uint16":  "uint16",
		"uint8":   "uint8",
		"int":     "int32",
		"int32":   "int32",
		"uint":    "uint32",
		"uint32":  "uint32",
		"long":    "int64",
		"int64":   "int64",
		"ulong":   "uint64",
		"uint64":  "uint64",
		"float":   "float32",
		"float32": "float32",
		"double":  "float64",
		"float64": "float64",
		"bytes":   "[]byte",
		"any":     "any",
	}
	for name, want := range cases {
		got, err := mapScalar(name)
		if err != nil {
			t.Errorf("mapScalar(%q): unexpected error %v", name, err)
			continue
		}
		if got != want {
			t.Errorf("mapScalar(%q) = %q, want %q", name, got, want)
		}
	}
}

// TestMapScalar_RejectsUnsupported keeps the failure path intact: names the
// shared table has no Go rendering for (and unknown names) must still error
// rather than emitting a bogus Go type.
func TestMapScalar_RejectsUnsupported(t *testing.T) {
	for _, name := range []string{"", "null", "nope"} {
		if _, err := mapScalar(name); err == nil {
			t.Errorf("mapScalar(%q): expected error, got nil", name)
		}
	}
}

// TestRender_NewScalarCoverage is the positive end-to-end check for the drift
// repair: structs with byte/short/ushort (canonical keywords) and int8/int16
// (C-style aliases) fields used to fail Render outright.
func TestRender_NewScalarCoverage(t *testing.T) {
	objs := []schema.ObjectDesc{{
		Kind: schema.TypeKindStruct,
		Name: "NewScalars",
		Fields: []schema.FieldDesc{
			{Name: "byteField", Type: scalar("byte")},
			{Name: "int8Field", Type: scalar("int8")},
			{Name: "shortField", Type: scalar("short")},
			{Name: "int16Field", Type: scalar("int16")},
			{Name: "ushortField", Type: scalar("ushort")},
			{Name: "uint16Field", Type: scalar("uint16")},
			{Name: "uint8Field", Type: scalar("uint8")},
			{Name: "float32Field", Type: scalar("float32")},
			{Name: "float64Field", Type: scalar("float64")},
		},
	}}
	out, err := Render(objs, Options{Package: "p"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	got := normalizeWhitespace(string(out))
	wantSnippets := []string{
		`ByteField int8 ` + "`json:\"byteField\"`",
		`Int8Field int8 ` + "`json:\"int8Field\"`",
		`ShortField int16 ` + "`json:\"shortField\"`",
		`Int16Field int16 ` + "`json:\"int16Field\"`",
		`UshortField uint16 ` + "`json:\"ushortField\"`",
		`Uint16Field uint16 ` + "`json:\"uint16Field\"`",
		`Uint8Field uint8 ` + "`json:\"uint8Field\"`",
		`Float32Field float32 ` + "`json:\"float32Field\"`",
		`Float64Field float64 ` + "`json:\"float64Field\"`",
	}
	for _, w := range wantSnippets {
		if !strings.Contains(got, w) {
			t.Errorf("missing %q in normalized output:\n%s", w, got)
		}
	}
}

func scalar(name string) schema.TypeDesc {
	return schema.TypeDesc{Kind: schema.TypeKindScalar, Name: name}
}
