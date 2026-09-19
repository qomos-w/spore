// Package gotypes renders Spore object descriptors into Go struct source.
//
// It is the schema-side sibling of the existing internal/gen/go-server
// package: where go-server emits dispatcher code that wraps typed Go handlers,
// this package emits the typed Go structs themselves so they can be authored
// from a single `.spore` file and consumed verbatim by handler code.
//
// One Go file per call to Render. Cross-namespace references are not the
// renderer's concern; the caller is expected to group related objects into
// the same call when they share a Go package.
//
// It is internal to the spore module. The public code-generation entry points
// are the `spore-gen-go-types` CLI and the re-exports `RenderGoTypes`,
// `RenderGoTypesRegistry` and `AssignSequentialSchemaIDs` in gen/render; the
// CLI itself calls those re-exports rather than this package directly.
//
// Field naming convention:
//
//   - Go field name = PascalCase of the Spore field name. Acronyms are not
//     special-cased; `agentId` becomes `AgentId`, matching gospore's reflection.
//   - json tag matches the Spore field name verbatim. This preserves wire
//     compatibility with hand-written domain types that used lowerCamelCase
//     json tags.
//
// MVP scope (matches the PoC):
//   - struct objects only; class objects are skipped silently
//   - scalar / array / map / named-type fields are supported
//   - optional fields render with `,omitempty` JSON tags. For TypeKindStruct
//     optional fields the Go type is rendered as a pointer (`*TypeName`) so
//     callers can distinguish absent from zero-valued embedded objects.
package gotypes

import (
	"bytes"
	"fmt"
	"go/format"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/qomos-w/spore/internal/gen/common"
	"github.com/qomos-w/spore/schema"
)

// Options controls Go file rendering.
//
// Package is the Go package declaration written into the output. It must be
// a valid Go identifier; Render enforces non-empty.
//
// Header is prepended verbatim above the package clause. The caller controls
// the exact wording so projects can match their own auto-gen banner format.
//
// SourcePath, if set, is recorded after the header as a `// Source: <path>`
// line for traceability.
//
// StructNames, if set, is the set of known struct type names across all input
// files. It lets Render treat optional named-type fields as pointers even when
// the struct definition lives in a different source file than the field.
type Options struct {
	Package     string
	Header      string
	SourcePath  string
	StructNames map[string]bool

	// Enums lists the enum declarations to emit alongside this file's
	// structs: a Go `type <Name> int` plus one typed constant per member.
	// Struct fields referencing an enum type render as that named type.
	Enums []schema.EnumDesc

	// NoInitialisms disables ALL_CAPS rendering of common initialisms in
	// exported field names (pawnId → PawnId instead of PawnID). Use when the
	// consuming codebase's existing Go naming predates the initialism
	// convention. Defaults to false (initialisms on).
	NoInitialisms bool

	// NoRegistryInit omits the spore/schema import and the init() blocks that
	// bind schema IDs to the runtime type registry. Use it for targets that
	// must stay dependency-free (e.g. the distributed plugin SDK, whose wire
	// format is JSON and never consults the registry): SchemaID constants and
	// the SchemaTypes map are still emitted — they need only stdlib reflect.
	NoRegistryInit bool

	// EmitComponents enables ECS component artifacts for structs declared
	// @component in the source schema:
	//
	//   - per-file Render emits a package-level runtime.Component[T] descriptor
	//     variable per component:
	//
	//	var PositionC = runtime.NewComponent[Position]("Position")
	//
	//     so the generated file gains a dependency on spore/runtime — opt-in,
	//     never for dependency-free targets.
	//   - RenderRegistry emits:
	//     - the Registry table object (componentIDs + SchemaTable accessors)
	//       consumed by World.AddRegistry, and
	//     - a ComponentShapes slice of {Name, SchemaID} pairs in schema ID
	//       order — pure stdlib data with no Go type references, intended
	//       for the consumer-side ecsbind helper to drive
	//       script.Runtime.BindStruct without codegen importing script.
	//     Both objects are local and dependency-free (stdlib method signatures
	//     and field types only).
	//
	// Variable naming: <Name>C, because the bare name collides with the
	// generated struct type in the same package.
	//
	// Component ⇒ schema: a component struct must carry a schema ID; Render
	// and RenderRegistry enforce this.
	EmitComponents bool
}

// Render produces a gofmt-clean Go source file declaring one struct per
// struct-kind ObjectDesc in objs. Output is deterministic: structs are
// emitted in lexical name order. Non-struct objects (classes, interfaces)
// are skipped without error.
func Render(objs []schema.ObjectDesc, opts Options) ([]byte, error) {
	if strings.TrimSpace(opts.Package) == "" {
		return nil, fmt.Errorf("gotypes: Options.Package is required")
	}

	// Assign sequential schema IDs from the filename base (e.g.
	// agent.chat._300.spore -> 300, 301, ...) while preserving explicit
	// @schema(N) annotations for shared structs.
	AssignSequentialSchemaIDs(objs, opts.SourcePath)

	sorted := append([]schema.ObjectDesc(nil), objs...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	var buf bytes.Buffer
	if h := strings.TrimSpace(opts.Header); h != "" {
		fmt.Fprintln(&buf, h)
	}
	if opts.SourcePath != "" {
		fmt.Fprintf(&buf, "// Source: %s\n", opts.SourcePath)
	}
	if buf.Len() > 0 {
		buf.WriteString("\n")
	}
	fmt.Fprintf(&buf, "package %s\n\n", opts.Package)

	hasIDs := hasSchemaIDs(sorted)
	hasComponents := opts.EmitComponents && hasComponentStructs(sorted)
	usesMedia := usesMediaType(sorted)

	if hasComponents {
		// Component ⇒ schema: an @component struct must carry a schema ID so it
		// can be keyed in the codegen registry table (ComponentIDs ⊆ SchemaIDs).
		if err := validateComponentSchemaIDs(sorted); err != nil {
			return nil, err
		}
	}

	if hasIDs && !opts.NoRegistryInit {
		if hasComponents {
			buf.WriteString("import (\n\t\"reflect\"\n\n\t\"github.com/qomos-w/spore/runtime\"\n\t\"github.com/qomos-w/spore/schema\"\n)\n\n")
		} else {
			buf.WriteString("import (\n\t\"reflect\"\n\n\t\"github.com/qomos-w/spore/schema\"\n)\n\n")
		}
	} else if hasComponents {
		if usesMedia {
			buf.WriteString("import (\n\t\"github.com/qomos-w/spore/runtime\"\n\n\t\"github.com/qomos-w/spore/schema\"\n)\n\n")
		} else {
			buf.WriteString("import \"github.com/qomos-w/spore/runtime\"\n\n")
		}
	} else if usesMedia {
		buf.WriteString("import \"github.com/qomos-w/spore/schema\"\n\n")
	}

	writeSchemaIDConstants(&buf, sorted)
	if !opts.NoRegistryInit {
		writeSchemaIDInit(&buf, sorted)
	}

	if hasComponents {
		writeComponentVars(&buf, sorted)
	}

	if usesMedia {
		writeMediaType(&buf, sorted)
	}

	writeEnums(&buf, opts.Enums)

	first := true
	for _, obj := range sorted {
		if obj.Kind != schema.TypeKindStruct {
			continue
		}
		if !first {
			buf.WriteString("\n")
		}
		first = false
		if err := writeStruct(&buf, obj, opts.StructNames, exportNameOptions{noInitialisms: opts.NoInitialisms}); err != nil {
			return nil, fmt.Errorf("struct %q: %w", obj.Name, err)
		}
	}

	out, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("gofmt rejected generated source: %w\n--- raw ---\n%s", err, buf.String())
	}
	return out, nil
}

func hasSchemaIDs(objs []schema.ObjectDesc) bool {
	for _, obj := range objs {
		if obj.Kind == schema.TypeKindStruct && obj.SchemaID != 0 {
			return true
		}
	}
	return false
}

// writeEnums emits Go enum projections: a named int type per enum plus one
// typed constant per member, preserving declared member values.
func writeEnums(buf *bytes.Buffer, enums []schema.EnumDesc) {
	for _, e := range enums {
		fmt.Fprintf(buf, "type %s int\n\n", e.Name)
		buf.WriteString("const (\n")
		for _, m := range e.Members {
			fmt.Fprintf(buf, "\t%s%s %s = %d\n", e.Name, m.Name, e.Name, m.Value)
		}
		buf.WriteString(")\n\n")
	}
}

// usesMediaType reports whether any struct field references the media type
// (directly or through array/map nesting).
func usesMediaType(objs []schema.ObjectDesc) bool {
	for _, obj := range objs {
		for _, f := range obj.Fields {
			if typeDescUsesMedia(f.Type) {
				return true
			}
		}
	}
	return false
}

func typeDescUsesMedia(td schema.TypeDesc) bool {
	if td.Kind == schema.TypeKindMedia {
		return true
	}
	if td.Element != nil && typeDescUsesMedia(*td.Element) {
		return true
	}
	if td.Key != nil && typeDescUsesMedia(*td.Key) {
		return true
	}
	if td.Value != nil && typeDescUsesMedia(*td.Value) {
		return true
	}
	return false
}

// writeMediaType emits the canonical Media carrier plus its contract
// validation. Skipped when a schema struct is itself named "Media" to avoid
// colliding with the user-defined type.
func writeMediaType(buf *bytes.Buffer, objs []schema.ObjectDesc) {
	for _, obj := range objs {
		if obj.Kind == schema.TypeKindStruct && obj.Name == "Media" {
			return
		}
	}
	buf.WriteString("// Media is the canonical {mime, src} reference carrier for the media\n")
	buf.WriteString("// schema type. src is a data:/file:/https: reference; inline data: URLs\n")
	buf.WriteString("// are capped at 1 MiB and file:/https: resolution is host-side.\n")
	buf.WriteString("type Media struct {\n")
	buf.WriteString("\tMime string `json:\"mime\"`\n")
	buf.WriteString("\tSrc  string `json:\"src\"`\n")
	buf.WriteString("}\n\n")
	buf.WriteString("// Validate enforces the media value contract.\n")
	buf.WriteString("func (m Media) Validate() error {\n")
	buf.WriteString("\treturn schema.ValidateMediaValue(m)\n")
	buf.WriteString("}\n\n")
}

// hasComponentStructs reports whether any struct is marked @component.
func hasComponentStructs(objs []schema.ObjectDesc) bool {
	for _, obj := range objs {
		if obj.Kind == schema.TypeKindStruct && obj.IsComponent {
			return true
		}
	}
	return false
}

// writeComponentVars emits one package-level runtime.Component[T] descriptor
// per @component struct. The variable name is <Name>C because the bare name
// collides with the generated struct type in the same package.
func writeComponentVars(buf *bytes.Buffer, objs []schema.ObjectDesc) {
	first := true
	for _, obj := range objs {
		if obj.Kind != schema.TypeKindStruct || !obj.IsComponent {
			continue
		}
		if first {
			buf.WriteString("// ECS component descriptors (declared @component in the schema).\nvar (\n")
			first = false
		}
		fmt.Fprintf(buf, "\t%sC = runtime.NewComponent[%s](%q)\n", obj.Name, obj.Name, obj.Name)
	}
	if !first {
		buf.WriteString(")\n\n")
	}
}

// validateComponentSchemaIDs enforces the component⇒schema invariant: every
// @component struct must carry a schema ID (explicit @schema(N) or a file
// base such as "agent.chat._300.spore"), because component IDs are the schema
// IDs a registry table is keyed by.
func validateComponentSchemaIDs(objs []schema.ObjectDesc) error {
	for _, obj := range objs {
		if obj.Kind != schema.TypeKindStruct || !obj.IsComponent {
			continue
		}
		if obj.SchemaID == 0 {
			return fmt.Errorf("gotypes: struct %q is declared @component but has no schema ID; declare @schema(N) or put it in a file with a numeric base (e.g. agent.chat._300.spore)", obj.Name)
		}
	}
	return nil
}

// AssignSequentialSchemaIDs fills in SchemaID for structs that do not have an
// explicit @schema(N) annotation. The filename base is extracted from
// sourcePath (e.g. "agent.chat._300.spore" -> 300); structs receive base+offset
// in declaration order. Explicit IDs are left untouched.
func AssignSequentialSchemaIDs(objs []schema.ObjectDesc, sourcePath string) {
	baseID, ok := parseSchemaBaseID(sourcePath)
	if !ok {
		return
	}
	var offset uint64
	for i := range objs {
		obj := &objs[i]
		if obj.Kind != schema.TypeKindStruct {
			continue
		}
		if obj.SchemaID != 0 {
			continue
		}
		obj.SchemaID = baseID + offset
		offset++
	}
}

// parseSchemaBaseID extracts an optional schema-id base from a filename such as
// "agent.chat._300.spore" -> 300. Paths without the "._<digits>" suffix return
// (0, false).
func parseSchemaBaseID(sourcePath string) (uint64, bool) {
	base := filepath.Base(sourcePath)
	base = strings.TrimSuffix(base, ".spore")
	idx := strings.LastIndex(base, "._")
	if idx == -1 {
		return 0, false
	}
	num := base[idx+2:]
	if num == "" {
		return 0, false
	}
	n, err := strconv.ParseUint(num, 10, 64)
	if err != nil {
		return 0, false
	}
	return uint64(n), true
}

// RegistryEntry maps a schema ID to the Go type name and source file that
// produced it. It is used to emit a single cross-file registry in
// single-package codegen modes. IsComponent marks structs declared @component;
// since component ⇒ schema, a component entry always has a nonzero ID.
type RegistryEntry struct {
	ID          uint64
	Name        string
	SourceFile  string
	IsComponent bool
}

// RenderRegistry emits a registry.go-style file that declares schema ID
// constants for every entry and an init() block that registers each schema
// ID with its corresponding Go type. Entries are sorted by ID for stable
// output.
func RenderRegistry(entries []RegistryEntry, opts Options) ([]byte, error) {
	if strings.TrimSpace(opts.Package) == "" {
		return nil, fmt.Errorf("gotypes: Options.Package is required")
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("gotypes: RenderRegistry requires at least one entry")
	}

	sorted := append([]RegistryEntry(nil), entries...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })

	var buf bytes.Buffer
	if h := strings.TrimSpace(opts.Header); h != "" {
		fmt.Fprintln(&buf, h)
	}
	fmt.Fprintf(&buf, "// Source: schema registry (%d types)\n\n", len(sorted))
	fmt.Fprintf(&buf, "package %s\n\n", opts.Package)
	if opts.NoRegistryInit {
		buf.WriteString("import (\n\t\"reflect\"\n)\n\n")
	} else {
		buf.WriteString("import (\n\t\"reflect\"\n\n\t\"github.com/qomos-w/spore/schema\"\n)\n\n")
	}

	buf.WriteString("// SchemaIDs maps schema ID to canonical struct name.\n")
	buf.WriteString("var SchemaIDs = map[uint64]string{\n")
	for _, e := range sorted {
		fmt.Fprintf(&buf, "\t%d: %q,\n", e.ID, e.Name)
	}
	buf.WriteString("}\n\n")

	buf.WriteString("// SchemaTypes maps schema ID to registered reflect.Type.\n")
	buf.WriteString("var SchemaTypes = map[uint64]reflect.Type{\n")
	for _, e := range sorted {
		fmt.Fprintf(&buf, "\t%d: reflect.TypeOf(%s{}),\n", e.ID, e.Name)
	}
	buf.WriteString("}\n\n")

	var compIDs []RegistryEntry
	if opts.EmitComponents {
		for _, e := range sorted {
			if e.IsComponent {
				if e.ID == 0 {
					return nil, fmt.Errorf("gotypes: component %q must carry a schema ID (component ⇒ schema); entries from RenderRegistry are keyed by ID", e.Name)
				}
				compIDs = append(compIDs, e)
			}
		}
	}
	if len(compIDs) > 0 {
		writeRegistryObject(&buf, compIDs)
	}

	if !opts.NoRegistryInit {
		buf.WriteString("func init() {\n")
		buf.WriteString("\tfor id, typ := range SchemaTypes {\n")
		buf.WriteString("\t\tschema.RegisterStructType(id, typ)\n")
		buf.WriteString("\t}\n")
		buf.WriteString("}\n")
	}

	out, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("gofmt rejected generated registry: %w\n--- raw ---\n%s", err, buf.String())
	}
	return out, nil
}

func writeSchemaIDInit(buf *bytes.Buffer, objs []schema.ObjectDesc) {
	var ids []schema.ObjectDesc
	for _, obj := range objs {
		if obj.Kind == schema.TypeKindStruct && obj.SchemaID != 0 {
			ids = append(ids, obj)
		}
	}
	if len(ids) == 0 {
		return
	}

	buf.WriteString("func init() {\n")
	for _, obj := range ids {
		fmt.Fprintf(buf, "\tschema.RegisterStructType(%sSchemaID, reflect.TypeOf(%s{}))\n", obj.Name, obj.Name)
	}
	buf.WriteString("}\n\n")
}

// writeRegistryObject emits the zero-runtime-dependency schema/component
// table object consumed by runtime.World.AddRegistry. The concrete type is
// local and its method signatures use only stdlib types (uint64, string,
// reflect.Type), so the generated file stays free of any spore import.
// compIDs is the @component subset of the full schema table.
func writeRegistryObject(buf *bytes.Buffer, compIDs []RegistryEntry) {
	// compIDs is already sorted by ID (RenderRegistry sorts entries before
	// splitting the @component subset), so emit ComponentShapes in the
	// same order for stable diffs.
	buf.WriteString("// ComponentShape is one @component struct available for script\n")
	buf.WriteString("// BindStruct wiring. The consumer-side helper (ecsbind) iterates\n")
	buf.WriteString("// ComponentShapes and resolves each entry's reflect.Type from\n")
	buf.WriteString("// Registry.SchemaTypes() to obtain a zero value for\n")
	buf.WriteString("// script.Runtime.BindStruct:\n")
	buf.WriteString("//\n")
	buf.WriteString("//\tfor _, c := range pkg.ComponentShapes {\n")
	buf.WriteString("//\t    rt.BindStruct(\"components\", c.Name,\n")
	buf.WriteString("//\t        reflect.New(Registry.SchemaTypes()[c.SchemaID]).Elem().Interface())\n")
	buf.WriteString("//\t}\n")
	buf.WriteString("//\n")
	buf.WriteString("// The struct holds pure data only (no Go type references) so the\n")
	buf.WriteString("// generated file stays free of any reverse-import on script or\n")
	buf.WriteString("// runtime. Codegen never emits the BindStruct call itself.\n")
	buf.WriteString("type ComponentShape struct {\n")
	buf.WriteString("\tName     string\n")
	buf.WriteString("\tSchemaID uint64\n")
	buf.WriteString("}\n\n")

	buf.WriteString("// ComponentShapes is the @component subset of the schema table, in\n")
	buf.WriteString("// schema ID order. The consumer-side helper (ecsbind) iterates this\n")
	buf.WriteString("// slice and calls script.Runtime.BindStruct for each entry; the\n")
	buf.WriteString("// codegen package itself stays out of the binding path so it can be\n")
	buf.WriteString("// imported by any dependency-free target.\n")
	buf.WriteString("var ComponentShapes = []ComponentShape{\n")
	for _, e := range compIDs {
		fmt.Fprintf(buf, "\t{Name: %q, SchemaID: %d},\n", e.Name, e.ID)
	}
	buf.WriteString("}\n\n")

	buf.WriteString("// ComponentIDs lists the schema IDs of structs declared @component.\n")
	buf.WriteString("var componentIDs = []uint64{\n")
	for _, e := range compIDs {
		fmt.Fprintf(buf, "\t%d,\n", e.ID)
	}
	buf.WriteString("}\n\n")

	buf.WriteString("// Registry is the codegen schema/component table for this package.\n")
	buf.WriteString("// It is a plain data object: pass it to a runtime World so the World can\n")
	buf.WriteString("// resolve @component Go types to their schema names:\n")
	buf.WriteString("//\n")
	buf.WriteString("//\tw := runtime.NewWorld()\n")
	buf.WriteString("//\tw.AddRegistry(Registry)\n")
	buf.WriteString("//\n")
	buf.WriteString("// The concrete type is local (stdlib method signatures only); the file\n")
	buf.WriteString("// never imports runtime.\n")
	buf.WriteString("type registryTable struct {\n")
	buf.WriteString("\tschemaTypes  map[uint64]reflect.Type\n")
	buf.WriteString("\tschemaIDs    map[uint64]string\n")
	buf.WriteString("\tcomponentIDs []uint64\n")
	buf.WriteString("}\n\n")
	buf.WriteString("func (t registryTable) SchemaTypes() map[uint64]reflect.Type {\n\treturn t.schemaTypes\n}\n\n")
	buf.WriteString("func (t registryTable) SchemaIDs() map[uint64]string {\n\treturn t.schemaIDs\n}\n\n")
	buf.WriteString("func (t registryTable) ComponentIDs() []uint64 {\n\treturn t.componentIDs\n}\n\n")
	buf.WriteString("var Registry = registryTable{\n")
	buf.WriteString("\tschemaTypes:  SchemaTypes,\n")
	buf.WriteString("\tschemaIDs:    SchemaIDs,\n")
	buf.WriteString("\tcomponentIDs: componentIDs,\n")
	buf.WriteString("}\n\n")
}

func writeSchemaIDConstants(buf *bytes.Buffer, objs []schema.ObjectDesc) {
	var ids []schema.ObjectDesc
	for _, obj := range objs {
		if obj.Kind == schema.TypeKindStruct && obj.SchemaID != 0 {
			ids = append(ids, obj)
		}
	}
	if len(ids) == 0 {
		return
	}

	buf.WriteString("// Schema IDs for value types defined in this package.\n")
	buf.WriteString("const (\n")
	for _, obj := range ids {
		fmt.Fprintf(buf, "\t%sSchemaID uint64 = %d\n", obj.Name, obj.SchemaID)
	}
	buf.WriteString(")\n\n")
}

func writeStruct(buf *bytes.Buffer, obj schema.ObjectDesc, structNames map[string]bool, exportOpts exportNameOptions) error {
	fmt.Fprintf(buf, "type %s struct {\n", obj.Name)
	for _, f := range obj.Fields {
		goType, err := mapType(f.Type)
		if err != nil {
			return fmt.Errorf("field %q: %w", f.Name, err)
		}
		// Optional + named struct → render as pointer so the field can be
		// distinguished from a zero-valued embedded struct on the wire.
		isStruct := f.Type.Kind == schema.TypeKindStruct
		if !isStruct && structNames != nil {
			name := f.Type.Name
			if name == "" {
				name = f.Type.ClassName
			}
			if structNames[name] {
				isStruct = true
			}
		}
		if f.Optional && isStruct {
			goType = "*" + goType
		}
		jsonTag := f.Name
		if f.Optional {
			jsonTag = f.Name + ",omitempty"
		}
		fmt.Fprintf(buf, "\t%s %s `json:%q`\n", exportNameWith(f.Name, exportOpts), goType, jsonTag)
	}
	fmt.Fprintln(buf, "}")
	return nil
}

func mapType(td schema.TypeDesc) (string, error) {
	switch td.Kind {
	case schema.TypeKindScalar:
		return mapScalar(td.Name)
	case schema.TypeKindMedia:
		return "Media", nil
	case schema.TypeKindArray:
		if td.Element == nil {
			return "", fmt.Errorf("array element missing")
		}
		inner, err := mapType(*td.Element)
		if err != nil {
			return "", err
		}
		return "[]" + inner, nil
	case schema.TypeKindMap:
		if td.Key == nil || td.Value == nil {
			return "", fmt.Errorf("map key/value missing")
		}
		k, err := mapType(*td.Key)
		if err != nil {
			return "", err
		}
		v, err := mapType(*td.Value)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("map[%s]%s", k, v), nil
	case schema.TypeKindStruct, schema.TypeKindClass:
		name := td.Name
		if name == "" {
			name = td.ClassName
		}
		if name == "" {
			return "", fmt.Errorf("named type missing name")
		}
		return name, nil
	case schema.TypeKindEnum:
		if td.Name == "" {
			return "", fmt.Errorf("enum type missing name")
		}
		return td.Name, nil
	}
	return "", fmt.Errorf("unsupported type kind %v", td.Kind)
}

// mapScalar maps a Spore scalar name to the Go type go-types emits for it,
// resolving through the shared scalar table in internal/gen/common. Unknown
// names are an error: emitting a bogus Go type would only surface at the
// consumer's compile time.
func mapScalar(name string) (string, error) {
	if gt, ok := common.GoTypesScalar(name); ok {
		return gt, nil
	}
	return "", fmt.Errorf("unknown scalar %q", name)
}

// exportNameOptions toggles initialism uppercasing in exported identifiers.
type exportNameOptions struct {
	// noInitialisms renders every word in plain PascalCase (pawnId → PawnId,
	// apiUrl → ApiUrl) for codebases whose existing Go naming predates the
	// initialism convention.
	noInitialisms bool
}

// exportName maps a Spore field identifier into PascalCase with the default
// initialism behavior.
func exportName(s string) string {
	return exportNameWith(s, exportNameOptions{})
}

// exportNameWith maps a Spore field identifier into PascalCase.
// Each word is capitalized on its first rune; common initialisms are fully
// uppercased unless opts.noInitialisms is set.
//
//	AggregatorActorId → AggregatorActorId
//	projectId         → ProjectID
//	agentType         → AgentType
//	html              → HTML
func exportNameWith(s string, opts exportNameOptions) string {
	if s == "" {
		return s
	}
	var b strings.Builder
	for _, w := range splitCamelWords(s) {
		if !opts.noInitialisms && isInitialism(w) {
			b.WriteString(strings.ToUpper(w))
		} else {
			r := []rune(w)
			r[0] = unicode.ToUpper(r[0])
			b.WriteString(string(r))
		}
	}
	return b.String()
}

// commonInitialisms are words rendered in ALL_CAPS in Go identifiers.
// Keep the list short and stable; adding entries is a breaking codegen change.
var commonInitialisms = map[string]struct{}{
	"id":   {},
	"url":  {},
	"json": {},
	"html": {},
	"api":  {},
	"ai":   {},
}

func isInitialism(w string) bool {
	_, ok := commonInitialisms[strings.ToLower(w)]
	return ok
}

// splitCamelWords splits a camelCase / PascalCase / mixed identifier into
// its constituent words. A word boundary occurs at:
//   - a lowercase→uppercase transition (`userId` → `user|Id`)
//   - the last uppercase of an uppercase run that is followed by a
//     lowercase (`JSONData` → `JSON|Data`)
//   - an underscore (`old_string` → `old|string`, underscore discarded)
//
// Digits stay attached to the surrounding word.
func splitCamelWords(s string) []string {
	if s == "" {
		return nil
	}
	var words []string
	for _, segment := range strings.Split(s, "_") {
		if segment == "" {
			continue
		}
		runes := []rune(segment)
		start := 0
		for i := 1; i < len(runes); i++ {
			prev := runes[i-1]
			cur := runes[i]
			lowerToUpper := unicode.IsLower(prev) && unicode.IsUpper(cur)
			upperRunEnd := unicode.IsUpper(prev) && unicode.IsUpper(cur) &&
				i+1 < len(runes) && unicode.IsLower(runes[i+1])
			if lowerToUpper || upperRunEnd {
				words = append(words, string(runes[start:i]))
				start = i
			}
		}
		words = append(words, string(runes[start:]))
	}
	return words
}
