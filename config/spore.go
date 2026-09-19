package config

import (
	"fmt"
	"strings"

	"github.com/qomos-w/spore/schema"
)

// SporeSchema renders a Config's type definitions and top-level value
// declarations as sporescript struct definitions.
func SporeSchema(cfg *Config) string {
	var b strings.Builder
	for _, td := range cfg.Types {
		writeStructDef(&b, td)
		b.WriteByte('\n')
	}
	for _, kv := range cfg.Values {
		b.WriteString("var ")
		b.WriteString(kv.Key)
		b.WriteString(": ")
		b.WriteString(typeHintFor(kv.Value, cfg))
		b.WriteString("\n")
	}
	return b.String()
}

// SporeSchemaForStruct renders a Go struct type as a Spore struct
// definition using schema introspection.
func SporeSchemaForStruct(target any) (string, error) {
	desc, err := schema.DescribeGoStruct(target)
	if err != nil {
		return "", fmt.Errorf("config: %w", err)
	}
	var b strings.Builder
	writeObjectDef(&b, desc)
	return b.String(), nil
}

func writeStructDef(b *strings.Builder, td TypeDef) {
	b.WriteString("struct ")
	b.WriteString(td.Name)
	b.WriteString(" {\n")
	for _, f := range td.Fields {
		b.WriteString("    ")
		b.WriteString(f.Name)
		b.WriteString(": ")
		b.WriteString(f.TypeText)
		b.WriteByte('\n')
	}
	b.WriteByte('}')
}

func writeObjectDef(b *strings.Builder, desc schema.ObjectDesc) {
	b.WriteString("struct ")
	b.WriteString(desc.Name)
	b.WriteString(" {\n")
	for _, f := range desc.Fields {
		b.WriteString("    ")
		b.WriteString(f.Name)
		b.WriteString(": ")
		b.WriteString(f.Type.String())
		b.WriteByte('\n')
	}
	b.WriteByte('}')
}

func typeHintFor(v Value, cfg *Config) string {
	switch v.Kind {
	case ValueInt:
		return "int"
	case ValueFloat:
		return "double"
	case ValueString:
		return "string"
	case ValueBool:
		return "bool"
	case ValueNull:
		return "any"
	case ValueArray:
		if len(v.Elements) > 0 {
			return "array<" + typeHintFor(v.Elements[0], cfg) + ">"
		}
		return "array<any>"
	case ValueMap:
		return "map<string, any>"
	case ValueStruct:
		return v.TypeName
	default:
		return "any"
	}
}
