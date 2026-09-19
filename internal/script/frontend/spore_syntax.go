package frontend

import (
	"fmt"
	"strings"

	"github.com/qomos-w/spore/schema"
)

// SporeSyntax returns a sporescript-style type annotation for a TypeDesc.
// Examples: int, array<int>, map<string, int>, Point
func SporeSyntax(td schema.TypeDesc) string {
	return td.String()
}

// CallableSporeSyntax returns a sporescript-style callable declaration.
// Examples:
//
//	fun add(a: int, b: int): int
//	stream fun chat(prompt: string): MessageEvent
func CallableSporeSyntax(desc schema.CallableDesc) string {
	var b strings.Builder
	if desc.Mode == schema.CallableModeStreaming {
		b.WriteString("stream ")
	}
	b.WriteString("fun ")
	b.WriteString(desc.Name)
	b.WriteString("(")
	for i, p := range desc.Parameters {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(p.Name)
		b.WriteString(": ")
		b.WriteString(SporeSyntax(p.Type))
	}
	b.WriteString(")")
	if len(desc.Returns) > 0 {
		b.WriteString(": ")
		if len(desc.Returns) == 1 {
			b.WriteString(SporeSyntax(desc.Returns[0]))
		} else {
			for i, r := range desc.Returns {
				if i > 0 {
					b.WriteString(", ")
				}
				b.WriteString(SporeSyntax(r))
			}
		}
	}
	return b.String()
}

// ObjectSporeSyntax returns a sporescript-style object declaration.
// Examples:
//
//	struct Point { x: int, y: int }
//	class User { id: string, name: string }
func ObjectSporeSyntax(desc schema.ObjectDesc) string {
	var b strings.Builder
	if desc.Kind == schema.TypeKindStruct {
		b.WriteString("struct ")
	} else {
		b.WriteString("class ")
	}
	b.WriteString(desc.Name)
	b.WriteString(" {")
	for i, f := range desc.Fields {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(f.Name)
		b.WriteString(": ")
		b.WriteString(SporeSyntax(f.Type))
	}
	b.WriteString("}")
	return b.String()
}

// TypeAliasSporeSyntax returns a sporescript-style type alias declaration.
// Example: type IntPair = array<int>
func TypeAliasSporeSyntax(name string, td schema.TypeDesc) string {
	return fmt.Sprintf("type %s = %s", name, SporeSyntax(td))
}

// VariableSporeSyntax returns a sporescript-style variable declaration.
// Example: var PI: double = ?
func VariableSporeSyntax(name string, td schema.TypeDesc) string {
	return fmt.Sprintf("var %s: %s", name, SporeSyntax(td))
}

// EnumSporeSyntax returns a sporescript-style enum declaration.
// Example: enum Color { Red = 0, Green = 1, Blue = 2 }
func EnumSporeSyntax(desc schema.EnumDesc) string {
	var b strings.Builder
	b.WriteString("enum ")
	b.WriteString(desc.Name)
	b.WriteString(" { ")
	for i, m := range desc.Members {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(m.Name)
		b.WriteString(" = ")
		b.WriteString(fmt.Sprintf("%d", m.Value))
	}
	b.WriteString(" }")
	return b.String()
}

// ModuleExportsSporeSyntax returns all exports in sporescript-style,
// one per line.
func ModuleExportsSporeSyntax(exports ModuleExports) string {
	var b strings.Builder
	for _, c := range exports.Callables {
		b.WriteString(CallableSporeSyntax(c))
		b.WriteString("\n")
	}
	for _, v := range exports.Variables {
		b.WriteString(VariableSporeSyntax(v.Name, schema.TypeDesc{Kind: schema.TypeKindScalar, Name: v.Type}))
		b.WriteString("\n")
	}
	for _, o := range exports.Objects {
		b.WriteString(ObjectSporeSyntax(o))
		b.WriteString("\n")
	}
	for _, e := range exports.Enums {
		b.WriteString(EnumSporeSyntax(e))
		b.WriteString("\n")
	}
	for _, t := range exports.Types {
		b.WriteString(TypeAliasSporeSyntax(t.Name, t.TypeDesc))
		b.WriteString("\n")
	}
	return b.String()
}
