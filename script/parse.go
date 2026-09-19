// Package script — schema-only parsing helpers.
//
// Host code-gen tools that want to consume a .spore file purely for its
// type and callable declarations (no execution, no module linking) can use the
// helpers in this file. They are a thin re-export of the internal frontend
// parser surface; the heavyweight Runtime path is not involved.
package script

import (
	"github.com/qomos-w/spore/internal/script/frontend"
	"github.com/qomos-w/spore/schema"
)

// ParseObjects parses every top-level struct or class declaration in source
// and returns them in declaration order. Cross-references between declarations
// (e.g. one struct field whose type is another struct in the same source)
// resolve to the correct kind because all top-level names are pre-collected
// before field types are walked.
//
// Non-object top-level statements (functions, imports, package, etc.) are
// ignored.
func ParseObjects(source string) ([]schema.ObjectDesc, error) {
	return frontend.ParseAllObjectDescs(source)
}

// ParseCallables parses every top-level function declaration in source and
// returns them in declaration order. struct/class names from the same source
// are pre-collected so callable parameter/return type references resolve
// correctly.
func ParseCallables(source string) ([]schema.CallableDesc, error) {
	return frontend.ParseAllCallableDescs(source)
}

// ParseEnums parses every top-level enum declaration in source and returns
// them in declaration order with resolved member values (explicit `= N` or
// auto-increment). Non-enum statements are ignored.
func ParseEnums(source string) ([]schema.EnumDesc, error) {
	return frontend.ParseAllEnumDescs(source)
}

// ParseObject parses a single struct or class declaration. Useful for
// embedding a one-shot type spec in test or tooling code.
func ParseObject(source string) (schema.ObjectDesc, error) {
	return frontend.ParseObjectDesc(source)
}

// ParseCallable parses a single callable declaration. Useful for embedding a
// one-shot callable spec in test or tooling code.
func ParseCallable(source string) (schema.CallableDesc, error) {
	return frontend.ParseCallableDesc(source)
}

// ParseType parses a single type annotation. Useful for embedding a one-shot
// type spec in test or tooling code.
func ParseType(source string) (schema.TypeDesc, error) {
	return frontend.ParseTypeDesc(source)
}
