package common

import "testing"

// TestScalarTable_Mappings locks every row of the shared scalar table. This is
// the drift guard: the four generators now read one table, so a change here
// (accidental or intentional) must be reflected in this test before it can
// reach the TypeScript or Go output.
func TestScalarTable_Mappings(t *testing.T) {
	type want struct {
		ts       string
		goTypes  string
		goServer string
	}
	cases := map[string]want{
		// Canonical Spore scalar names.
		"bool":   {ts: "boolean", goTypes: "bool", goServer: "bool"},
		"byte":   {ts: "number", goTypes: "int8", goServer: "int8"},
		"short":  {ts: "number", goTypes: "int16", goServer: "int16"},
		"ushort": {ts: "number", goTypes: "uint16", goServer: "uint16"},
		"int":    {ts: "number", goTypes: "int32", goServer: "int"},
		"uint":   {ts: "number", goTypes: "uint32", goServer: "uint"},
		"long":   {ts: "number", goTypes: "int64", goServer: "int64"},
		"ulong":  {ts: "number", goTypes: "uint64", goServer: "uint64"},
		"float":  {ts: "number", goTypes: "float32", goServer: "float32"},
		"double": {ts: "number", goTypes: "float64", goServer: "float64"},
		"string": {ts: "string", goTypes: "string", goServer: "string"},
		"bytes":  {ts: "Uint8Array", goTypes: "[]byte", goServer: "[]byte"},
		"any":    {ts: "unknown", goTypes: "any", goServer: "any"},

		// C-style aliases, resolved to the row they alias.
		"int8":    {ts: "number", goTypes: "int8", goServer: "int8"},
		"int16":   {ts: "number", goTypes: "int16", goServer: "int16"},
		"int32":   {ts: "number", goTypes: "int32", goServer: "int32"},
		"int64":   {ts: "number", goTypes: "int64", goServer: "int64"},
		"uint16":  {ts: "number", goTypes: "uint16", goServer: "uint16"},
		"uint32":  {ts: "number", goTypes: "uint32", goServer: "uint32"},
		"uint64":  {ts: "number", goTypes: "uint64", goServer: "uint64"},
		"float32": {ts: "number", goTypes: "float32", goServer: "float32"},
		"float64": {ts: "number", goTypes: "float64", goServer: "float64"},

		// Unsigned 8-bit has no canonical Spore keyword; it is its own row.
		"uint8": {ts: "number", goTypes: "uint8", goServer: "uint8"},

		// TypeScript-only scalar: no Go target renders it.
		"null": {ts: "null"},
	}
	for name, w := range cases {
		if got, ok := TSScalar(name); !ok || got != w.ts {
			t.Errorf("TSScalar(%q) = (%q, %v), want (%q, true)", name, got, ok, w.ts)
		}
		assertGoLookup(t, "GoTypesScalar", name, w.goTypes, GoTypesScalar)
		assertGoLookup(t, "GoServerScalar", name, w.goServer, GoServerScalar)
	}
}

func assertGoLookup(t *testing.T, fn, name, want string, lookup func(string) (string, bool)) {
	t.Helper()
	got, ok := lookup(name)
	if want == "" {
		if ok {
			t.Errorf("%s(%q) = (%q, true), want unsupported", fn, name, got)
		}
		return
	}
	if !ok || got != want {
		t.Errorf("%s(%q) = (%q, %v), want (%q, true)", fn, name, got, ok, want)
	}
}

// TestLookupScalar_AliasResolvesToCanonical pins that aliases report the
// canonical row name, so callers can reason about the alias set.
func TestLookupScalar_AliasResolvesToCanonical(t *testing.T) {
	aliases := map[string]string{
		"int8":    "byte",
		"int16":   "short",
		"uint16":  "ushort",
		"int64":   "long",
		"uint64":  "ulong",
		"float32": "float",
		"float64": "double",
	}
	for alias, canonical := range aliases {
		spec, ok := LookupScalar(alias)
		if !ok {
			t.Errorf("LookupScalar(%q): not found", alias)
			continue
		}
		if spec.Name != canonical {
			t.Errorf("LookupScalar(%q).Name = %q, want %q", alias, spec.Name, canonical)
		}
	}
	canonical, ok := LookupScalar("int32")
	if !ok || canonical.Name != "int32" {
		t.Errorf("LookupScalar(\"int32\") = (%q, %v), want its own row (go-server renders it differently from int)", canonical.Name, ok)
	}
}

func TestLookupScalar_UnknownAndEmpty(t *testing.T) {
	for _, name := range []string{"", "nope", "int128", "Media", "void"} {
		if _, ok := LookupScalar(name); ok {
			t.Errorf("LookupScalar(%q): expected not found", name)
		}
	}
}

// TestScalarTable_NoAmbiguousKeys guards the index build: a name listed twice
// (as a canonical name or an alias) would silently shadow a row.
func TestScalarTable_NoAmbiguousKeys(t *testing.T) {
	seen := map[string]string{}
	for _, s := range scalarTable {
		if s.Name == "" {
			t.Errorf("scalar row with empty canonical name")
			continue
		}
		for _, key := range append([]string{s.Name}, s.Aliases...) {
			if prev, dup := seen[key]; dup {
				t.Errorf("scalar name %q declared by both %q and %q", key, prev, s.Name)
			}
			seen[key] = s.Name
		}
	}
	if len(scalarIndex) != len(seen) {
		t.Errorf("index has %d keys, table declares %d", len(scalarIndex), len(seen))
	}
}

// TestScalarTable_CanonicalSporeKeywordsAreSupported pins that every scalar
// keyword the Spore language frontend can emit is renderable by every target.
// byte/short/ushort are keywords (internal/script/frontend/token.go) and used
// to be rejected by go-types; that drift must not come back.
func TestScalarTable_CanonicalSporeKeywordsAreSupported(t *testing.T) {
	keywords := []string{
		"bool", "byte", "short", "ushort", "int", "uint", "long", "ulong",
		"float", "double", "string", "bytes", "any",
	}
	for _, name := range keywords {
		if _, ok := TSScalar(name); !ok {
			t.Errorf("TSScalar(%q): canonical keyword must be renderable", name)
		}
		if _, ok := GoTypesScalar(name); !ok {
			t.Errorf("GoTypesScalar(%q): canonical keyword must be renderable", name)
		}
		if _, ok := GoServerScalar(name); !ok {
			t.Errorf("GoServerScalar(%q): canonical keyword must be renderable", name)
		}
	}
}
