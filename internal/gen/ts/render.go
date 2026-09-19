package ts

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/qomos-w/spore/internal/gen/common"
	"github.com/qomos-w/spore/schema"
)

// renderInterface renders one ObjectDesc as a TypeScript interface body.
// The interface is exported and uses the ObjectDesc.Name verbatim. Field
// names are emitted as written in FieldDesc.Name to preserve JSON round-trip
// compatibility with the Go side; transformation to lowerCamelCase is the
// responsibility of an explicit json:"..." tag on the Go struct.
func renderInterface(obj schema.ObjectDesc) (string, error) {
	if obj.Name == "" {
		return "", fmt.Errorf("ObjectDesc has empty Name")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "export interface %s {\n", obj.Name)
	for _, f := range obj.Fields {
		if f.Private {
			continue
		}
		ts, err := renderType(f.Type)
		if err != nil {
			return "", fmt.Errorf("field %s.%s: %w", obj.Name, f.Name, err)
		}
		if f.Optional {
			fmt.Fprintf(&b, "  %s?: %s | undefined;\n", f.Name, ts)
		} else {
			fmt.Fprintf(&b, "  %s: %s;\n", f.Name, ts)
		}
	}
	b.WriteString("}\n")
	return b.String(), nil
}

// renderType returns the TypeScript expression for a TypeDesc reference,
// usable on the right-hand side of a field declaration.
func renderType(t schema.TypeDesc) (string, error) {
	switch t.Kind {
	case schema.TypeKindVoid:
		return "void", nil
	case schema.TypeKindScalar:
		return renderScalar(t.Name), nil
	case schema.TypeKindArray:
		if t.Element == nil {
			return "unknown[]", nil
		}
		inner, err := renderType(*t.Element)
		if err != nil {
			return "", err
		}
		return inner + "[]", nil
	case schema.TypeKindMap:
		if t.Key == nil || t.Value == nil {
			return "Record<string, unknown>", nil
		}
		v, err := renderType(*t.Value)
		if err != nil {
			return "", err
		}
		return "Record<string, " + v + ">", nil
	case schema.TypeKindStruct, schema.TypeKindClass:
		name := t.ClassName
		if name == "" {
			name = t.Name
		}
		if name == "" {
			return "unknown", nil
		}
		return name, nil
	case schema.TypeKindMedia:
		return "Media", nil
	case schema.TypeKindInvalid:
		return "", fmt.Errorf("invalid TypeDesc")
	default:
		return "unknown", nil
	}
}

// renderScalar maps Spore scalar names from SYNTAX.md §5.1 to their
// TypeScript equivalents, resolving through the shared scalar table in
// internal/gen/common. Names the table does not know pass through verbatim so
// hand-written manifests keep working; the empty name (an unnamed scalar)
// renders as `unknown`.
func renderScalar(name string) string {
	if name == "" {
		return "unknown"
	}
	if out, ok := common.TSScalar(name); ok {
		return out
	}
	return name
}

// renderContext carries the current namespace plus a name → owner-namespace
// map so that struct field references to types owned by another namespace
// can be rendered as qualified, imported references (e.g. systemTypes.Foo)
// instead of bare names that would not resolve in the emitted module.
type renderContext struct {
	namespace       string
	nameToNamespace map[string]string
	imports         map[string]struct{}
}

func newRenderContext(ns string, nameToNamespace map[string]string) *renderContext {
	return &renderContext{
		namespace:       ns,
		nameToNamespace: nameToNamespace,
		imports:         map[string]struct{}{},
	}
}

// importStatements returns the namespace imports accumulated while rendering
// interfaces under this context, sorted for deterministic output. Each import
// is a wildcard alias: `import * as <ns>Types from "../<ns>/types.js"`.
func (c *renderContext) importStatements() []string {
	names := make([]string, 0, len(c.imports))
	for ns := range c.imports {
		names = append(names, ns)
	}
	sort.Strings(names)
	out := make([]string, 0, len(names))
	for _, ns := range names {
		out = append(out, fmt.Sprintf("import * as %sTypes from \"../%s/types.js\";\n", ns, ns))
	}
	return out
}

// renderInterfaceWithContext renders one ObjectDesc as a TypeScript interface,
// qualifying struct references that belong to a different namespace with the
// owner's import alias and recording the import on ctx.
func renderInterfaceWithContext(obj schema.ObjectDesc, ctx *renderContext) (string, error) {
	if obj.Name == "" {
		return "", fmt.Errorf("ObjectDesc has empty Name")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "export interface %s {\n", obj.Name)
	for _, f := range obj.Fields {
		if f.Private {
			continue
		}
		ts, err := renderTypeWithContext(f.Type, ctx)
		if err != nil {
			return "", fmt.Errorf("field %s.%s: %w", obj.Name, f.Name, err)
		}
		if f.Optional {
			fmt.Fprintf(&b, "  %s?: %s | undefined;\n", f.Name, ts)
		} else {
			fmt.Fprintf(&b, "  %s: %s;\n", f.Name, ts)
		}
	}
	b.WriteString("}\n")
	return b.String(), nil
}

// renderTypeWithContext mirrors renderType but resolves struct / class names
// against ctx: a name owned by another namespace is qualified with the owner's
// alias and the cross-namespace import is recorded on ctx.
func renderTypeWithContext(t schema.TypeDesc, ctx *renderContext) (string, error) {
	switch t.Kind {
	case schema.TypeKindVoid:
		return "void", nil
	case schema.TypeKindScalar:
		return renderScalar(t.Name), nil
	case schema.TypeKindArray:
		if t.Element == nil {
			return "unknown[]", nil
		}
		inner, err := renderTypeWithContext(*t.Element, ctx)
		if err != nil {
			return "", err
		}
		return inner + "[]", nil
	case schema.TypeKindMap:
		if t.Key == nil || t.Value == nil {
			return "Record<string, unknown>", nil
		}
		v, err := renderTypeWithContext(*t.Value, ctx)
		if err != nil {
			return "", err
		}
		return "Record<string, " + v + ">", nil
	case schema.TypeKindStruct, schema.TypeKindClass:
		name := t.ClassName
		if name == "" {
			name = t.Name
		}
		if name == "" {
			return "unknown", nil
		}
		if owner, ok := ctx.nameToNamespace[name]; ok && owner != ctx.namespace {
			ctx.imports[owner] = struct{}{}
			return owner + "Types." + name, nil
		}
		return name, nil
	case schema.TypeKindMedia:
		return "Media", nil
	case schema.TypeKindInvalid:
		return "", fmt.Errorf("invalid TypeDesc")
	default:
		return "unknown", nil
	}
}

// renderRegistry builds the registry.ts content for one namespace.
func renderRegistry(namespace string, entries []NamedObjectDesc) string {
	var b strings.Builder
	sorted := append([]NamedObjectDesc(nil), entries...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].SchemaID < sorted[j].SchemaID })

	b.WriteString("import { SchemaRegistry, type SchemaEntry } from \"@qomos/spore-ts/registry\";\n\n")
	b.WriteString("export const SchemaIDs = {\n")
	for _, e := range sorted {
		fmt.Fprintf(&b, "  %s: %d,\n", e.Name, e.SchemaID)
	}
	b.WriteString("} as const;\n\n")

	b.WriteString("export const schemaEntries: SchemaEntry[] = [\n")
	for _, e := range sorted {
		typeRef := schema.TypeDesc{Kind: e.Object.Kind, Name: e.Name, ClassName: e.Object.Name, ClassID: e.SchemaID}
		entry := renderRegistryEntry(namespace, e.SchemaID, e.Name, e.Visibility.String(), typeRef, e.Object)
		b.WriteString(indentBlock(entry, "  ", true))
		b.WriteString(",\n")
	}
	b.WriteString("];\n\n")

	b.WriteString("export function buildSchemaRegistry(): SchemaRegistry {\n")
	b.WriteString("  const registry = new SchemaRegistry();\n")
	b.WriteString("  for (const entry of schemaEntries) {\n")
	b.WriteString("    registry.register(entry);\n")
	b.WriteString("  }\n")
	b.WriteString("  return registry;\n")
	b.WriteString("}\n")

	return b.String()
}

func renderRegistryEntry(namespace string, schemaID uint64, name, visibility string, typeRef schema.TypeDesc, obj schema.ObjectDesc) string {
	fields := []tsField{
		{Key: "namespace", Value: strconv.Quote(namespace)},
		{Key: "schemaId", Value: strconv.FormatUint(uint64(schemaID), 10)},
		{Key: "name", Value: strconv.Quote(name)},
		{Key: "visibility", Value: strconv.Quote(visibility)},
		{Key: "type", Value: renderTypeDescLiteral(typeRef)},
		{Key: "object", Value: renderObjectDescLiteral(obj)},
	}
	return renderTSObject(fields, "")
}

func renderTypeDescLiteral(desc schema.TypeDesc) string {
	fields := []tsField{{Key: "kind", Value: strconv.Quote(string(desc.Kind))}}
	if desc.Name != "" {
		fields = append(fields, tsField{Key: "name", Value: strconv.Quote(desc.Name)})
	}
	if desc.TypeID != 0 {
		fields = append(fields, tsField{Key: "typeId", Value: formatJSONValue(desc.TypeID)})
	}
	if desc.Element != nil {
		fields = append(fields, tsField{Key: "element", Value: renderTypeDescLiteral(*desc.Element)})
	}
	if desc.Key != nil {
		fields = append(fields, tsField{Key: "key", Value: renderTypeDescLiteral(*desc.Key)})
	}
	if desc.Value != nil {
		fields = append(fields, tsField{Key: "value", Value: renderTypeDescLiteral(*desc.Value)})
	}
	if desc.ClassName != "" {
		fields = append(fields, tsField{Key: "className", Value: strconv.Quote(desc.ClassName)})
	}
	if desc.ClassID != 0 {
		fields = append(fields, tsField{Key: "classId", Value: formatJSONValue(desc.ClassID)})
	}
	return renderTSObject(fields, "")
}

func renderObjectDescLiteral(obj schema.ObjectDesc) string {
	fields := []tsField{
		{Key: "kind", Value: strconv.Quote(string(obj.Kind))},
		{Key: "name", Value: strconv.Quote(obj.Name)},
	}
	fieldItems := make([]string, 0, len(obj.Fields))
	for _, field := range obj.Fields {
		fieldFields := []tsField{
			{Key: "name", Value: strconv.Quote(field.Name)},
			{Key: "type", Value: renderTypeDescLiteral(field.Type)},
		}
		if field.Optional {
			fieldFields = append(fieldFields, tsField{Key: "optional", Value: "true"})
		}
		if field.Private {
			fieldFields = append(fieldFields, tsField{Key: "private", Value: "true"})
		}
		fieldItems = append(fieldItems, renderTSObject(fieldFields, ""))
	}
	fields = append(fields, tsField{Key: "fields", Value: renderTSArray(fieldItems, "")})
	if obj.Parent != "" {
		fields = append(fields, tsField{Key: "parent", Value: strconv.Quote(obj.Parent)})
	}
	if obj.IsOpen {
		fields = append(fields, tsField{Key: "isOpen", Value: "true"})
	}
	if len(obj.Implements) > 0 {
		fields = append(fields, tsField{Key: "implements", Value: formatJSONValue(obj.Implements)})
	}
	if len(obj.Methods) > 0 {
		methodItems := make([]string, 0, len(obj.Methods))
		for _, method := range obj.Methods {
			methodFields := []tsField{
				{Key: "name", Value: strconv.Quote(method.Name)},
				{Key: "parameters", Value: renderTSArray(renderParameterDescLiteralItems(method.Parameters), "")},
				{Key: "returns", Value: renderTSArray(renderTypeDescLiteralItems(method.Returns), "")},
			}
			if method.IsOpen {
				methodFields = append(methodFields, tsField{Key: "isOpen", Value: "true"})
			}
			if method.IsOverride {
				methodFields = append(methodFields, tsField{Key: "isOverride", Value: "true"})
			}
			if method.Private {
				methodFields = append(methodFields, tsField{Key: "private", Value: "true"})
			}
			methodItems = append(methodItems, renderTSObject(methodFields, ""))
		}
		fields = append(fields, tsField{Key: "methods", Value: renderTSArray(methodItems, "")})
	}
	return renderTSObject(fields, "")
}

func renderParameterDescLiteralItems(params []schema.ParameterDesc) []string {
	out := make([]string, 0, len(params))
	for _, param := range params {
		out = append(out, renderTSObject([]tsField{
			{Key: "name", Value: strconv.Quote(param.Name)},
			{Key: "type", Value: renderTypeDescLiteral(param.Type)},
		}, ""))
	}
	return out
}

func renderTypeDescLiteralItems(descs []schema.TypeDesc) []string {
	out := make([]string, 0, len(descs))
	for _, desc := range descs {
		out = append(out, renderTypeDescLiteral(desc))
	}
	return out
}

type tsField struct {
	Key   string
	Value string
}

func renderTSObject(fields []tsField, baseIndent string) string {
	var b strings.Builder
	innerIndent := baseIndent + "  "
	b.WriteString("{\n")
	for i, field := range fields {
		b.WriteString(innerIndent)
		b.WriteString(field.Key)
		b.WriteString(": ")
		b.WriteString(indentBlock(field.Value, innerIndent, false))
		if i < len(fields)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString(baseIndent)
	b.WriteString("}")
	return b.String()
}

func renderTSArray(items []string, baseIndent string) string {
	if len(items) == 0 {
		return "[]"
	}
	var b strings.Builder
	innerIndent := baseIndent + "  "
	b.WriteString("[\n")
	for i, item := range items {
		b.WriteString(indentBlock(item, innerIndent, true))
		if i < len(items)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString(baseIndent)
	b.WriteString("]")
	return b.String()
}

func formatJSONValue(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(raw)
}

func indentBlock(s, indent string, indentFirstLine bool) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if i == 0 && !indentFirstLine {
			lines[i] = line
			continue
		}
		lines[i] = indent + line
	}
	return strings.Join(lines, "\n")
}

// renderIndex builds the index.ts barrel for one namespace. hasTypes /
// hasRegistry / hasCallables control which sibling re-exports are emitted so
// namespaces that only import copied types can still export a types.ts barrel
// without a registry.ts.
func renderIndex(hasTypes, hasRegistry, hasCallables bool) string {
	var b strings.Builder
	if hasTypes {
		b.WriteString("export * from \"./types.js\";\n")
	}
	if hasRegistry {
		b.WriteString("export * from \"./registry.js\";\n")
	}
	if hasCallables {
		b.WriteString("export * from \"./callables.js\";\n")
	}
	return b.String()
}

// renderCallables builds the callables.ts content for one namespace.
//
// Output shape mirrors registry.ts: a `callableEntries: CallableEntry[]`
// array sourced from the runtime types in @qomos/spore-ts/callables, plus
// a `buildCallableRegistry()` helper that pre-populates a CallableRegistry.
//
// Entries are sorted alphabetically by Name so output is deterministic
// regardless of input order. The caller is responsible for ensuring entries
// already passed the visibility filter.
func renderCallables(namespace string, entries []NamedCallableDesc) string {
	sorted := append([]NamedCallableDesc(nil), entries...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	var b strings.Builder
	b.WriteString("import { CallableRegistry, type CallableEntry } from \"@qomos/spore-ts/callables\";\n\n")
	b.WriteString("export const callableEntries: CallableEntry[] = [\n")
	for _, c := range sorted {
		entry := renderCallableEntry(namespace, c)
		b.WriteString(indentBlock(entry, "  ", true))
		b.WriteString(",\n")
	}
	b.WriteString("];\n\n")

	b.WriteString("export function buildCallableRegistry(): CallableRegistry {\n")
	b.WriteString("  const registry = new CallableRegistry();\n")
	b.WriteString("  for (const entry of callableEntries) {\n")
	b.WriteString("    registry.register(entry);\n")
	b.WriteString("  }\n")
	b.WriteString("  return registry;\n")
	b.WriteString("}\n")
	return b.String()
}

func renderCallableEntry(namespace string, c NamedCallableDesc) string {
	fields := []tsField{
		{Key: "namespace", Value: strconv.Quote(namespace)},
		{Key: "name", Value: strconv.Quote(c.Name)},
		{Key: "visibility", Value: strconv.Quote(c.Visibility.String())},
		{Key: "mode", Value: strconv.Quote(string(c.Mode))},
		{Key: "reqSchemaId", Value: strconv.FormatUint(uint64(c.ReqSchemaID), 10)},
	}
	if c.ChunkSchemaID != 0 {
		fields = append(fields, tsField{Key: "chunkSchemaId", Value: strconv.FormatUint(uint64(c.ChunkSchemaID), 10)})
	}
	fields = append(fields,
		tsField{Key: "finalSchemaId", Value: strconv.FormatUint(uint64(c.FinalSchemaID), 10)},
		tsField{Key: "req", Value: renderTypeDescLiteral(c.Req)},
	)
	if c.Chunk != nil {
		fields = append(fields, tsField{Key: "chunk", Value: renderTypeDescLiteral(*c.Chunk)})
	}
	fields = append(fields, tsField{Key: "final", Value: renderTypeDescLiteral(c.Final)})
	return renderTSObject(fields, "")
}
