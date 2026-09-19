package common

// This file is the single authoritative Spore scalar table. Every generator
// resolves scalar names through it, so a scalar (or a new target column) is
// declared exactly once and cannot drift between generators.
//
// Canonical Spore scalar names come from the language keyword set (bool, byte,
// short, ushort, int, uint, long, ulong, float, double, string, bytes, any).
// C-style spellings (int8, int64, float32, …) are accepted as aliases of the
// canonical name they denote. `null` and the empty name are the two TypeScript
// special cases; neither has a Go rendering, so the Go targets reject them.
//
// Column semantics:
//
//	TS        TypeScript type used by internal/gen/ts and internal/gen/ts-client
//	GoTypes   Go type used by internal/gen/go-types  (fixed-width mapping:
//	          int→int32, uint→uint32, float→float32, double→float64)
//	GoServer  Go type used by internal/gen/go-server (Go-native mapping:
//	          int→int, uint→uint, but fixed-width for the wider types)
//
// An empty column means "this target does not render that scalar"; the target
// reports its own error for it. The GoTypes/GoServer divergence on int/uint is
// intentional and predates the convergence — go-types emits wire-stable
// fixed-width types, go-server matches the handler author's idiomatic Go.
var scalarTable = []scalarSpec{
	{Name: "bool", TS: "boolean", GoTypes: "bool", GoServer: "bool"},
	{Name: "byte", Aliases: []string{"int8"}, TS: "number", GoTypes: "int8", GoServer: "int8"},
	{Name: "uint8", TS: "number", GoTypes: "uint8", GoServer: "uint8"},
	{Name: "short", Aliases: []string{"int16"}, TS: "number", GoTypes: "int16", GoServer: "int16"},
	{Name: "ushort", Aliases: []string{"uint16"}, TS: "number", GoTypes: "uint16", GoServer: "uint16"},
	{Name: "int", TS: "number", GoTypes: "int32", GoServer: "int"},
	{Name: "int32", TS: "number", GoTypes: "int32", GoServer: "int32"},
	{Name: "uint", TS: "number", GoTypes: "uint32", GoServer: "uint"},
	{Name: "uint32", TS: "number", GoTypes: "uint32", GoServer: "uint32"},
	{Name: "long", Aliases: []string{"int64"}, TS: "number", GoTypes: "int64", GoServer: "int64"},
	{Name: "ulong", Aliases: []string{"uint64"}, TS: "number", GoTypes: "uint64", GoServer: "uint64"},
	{Name: "float", Aliases: []string{"float32"}, TS: "number", GoTypes: "float32", GoServer: "float32"},
	{Name: "double", Aliases: []string{"float64"}, TS: "number", GoTypes: "float64", GoServer: "float64"},
	{Name: "string", TS: "string", GoTypes: "string", GoServer: "string"},
	{Name: "bytes", TS: "Uint8Array", GoTypes: "[]byte", GoServer: "[]byte"},
	{Name: "any", TS: "unknown", GoTypes: "any", GoServer: "any"},
	{Name: "null", TS: "null"},
}

// scalarSpec is one row of the scalar table. Aliases resolve to the row that
// declares them, so LookupScalar reports the canonical Name for every spelling.
type scalarSpec struct {
	Name     string   // canonical Spore scalar name
	Aliases  []string // alternate spellings resolving to this row
	TS       string   // TypeScript type, "" when unsupported
	GoTypes  string   // Go type for go-types, "" when unsupported
	GoServer string   // Go type for go-server, "" when unsupported
}

// scalarIndex maps every canonical name and alias to its row. It is built once
// at package initialisation; duplicate keys panic here rather than silently
// dropping a scalar.
var scalarIndex = buildScalarIndex()

func buildScalarIndex() map[string]scalarSpec {
	index := make(map[string]scalarSpec, len(scalarTable)*2)
	for _, s := range scalarTable {
		index[s.Name] = s
		for _, a := range s.Aliases {
			index[a] = s
		}
	}
	return index
}

// LookupScalar resolves a Spore scalar name (canonical or alias) to its table
// row. The returned row's Name is always the canonical spelling.
func LookupScalar(name string) (scalarSpec, bool) {
	s, ok := scalarIndex[name]
	return s, ok
}

// TSScalar returns the TypeScript type for a Spore scalar name. ok is false for
// unknown names and for scalars no TypeScript target renders; callers decide
// the fallback (internal/gen/ts passes unknown names through verbatim).
func TSScalar(name string) (string, bool) {
	s, ok := scalarIndex[name]
	if !ok || s.TS == "" {
		return "", false
	}
	return s.TS, true
}

// GoTypesScalar returns the Go type emitted by internal/gen/go-types for a
// Spore scalar name. ok is false for scalar names go-types cannot render.
func GoTypesScalar(name string) (string, bool) {
	s, ok := scalarIndex[name]
	if !ok || s.GoTypes == "" {
		return "", false
	}
	return s.GoTypes, true
}

// GoServerScalar returns the Go type emitted by internal/gen/go-server for a
// Spore scalar name. ok is false for scalar names go-server cannot render.
func GoServerScalar(name string) (string, bool) {
	s, ok := scalarIndex[name]
	if !ok || s.GoServer == "" {
		return "", false
	}
	return s.GoServer, true
}
