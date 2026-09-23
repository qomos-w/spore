package gotypes

import (
	"bytes"
	"fmt"
	"go/format"
	"path"
	"sort"
	"strings"

	"github.com/qomos-w/spore/schema"
)

// This file carries the @data half of the codegen: declaration validation and
// the data-table manifest. @data structs render as plain structs (Render in
// gen.go handles that — no runtime import, no component vars, no schema IDs);
// this file adds the consumer-side handoff artifact DataTables/RefShapes, the
// same pattern ComponentShapes uses for the ecsbind helper.

// dataKeyIntegerScalars lists the scalar names allowed as @data key types
// besides string. Integer keys are the enum-table shape (a table keyed by its
// own enum or an int id); everything else (bool, float, bytes, any, array,
// map, struct) is rejected by ValidateDataTables.
var dataKeyIntegerScalars = map[string]bool{
	"byte":   true,
	"short":  true,
	"ushort": true,
	"int":    true,
	"uint":   true,
	"long":   true,
	"ulong":  true,
}

// ValidateDataTables enforces the @data declaration rules across one codegen
// run:
//
//   - key/@ref markers are only valid inside @data structs;
//   - every @data struct declares exactly one key field;
//   - a key field is not optional and is typed string, an integer scalar, or
//     a known enum;
//   - @data structs carry no schema ID (they are not transport schemas);
//   - every @ref target is a @data struct in the same run whose key type is
//     string, and the referencing field is string or array<string>;
//   - an explicit @ref(T.field) names an existing field of T.
//
// optional + @ref is allowed (a nullable reference). Errors are collected and
// reported together, sorted for stable output.
func ValidateDataTables(objs []schema.ObjectDesc, enumNames map[string]bool) error {
	dataObjs := make(map[string]schema.ObjectDesc)
	for _, obj := range objs {
		if obj.Kind == schema.TypeKindStruct && obj.IsData {
			dataObjs[obj.Name] = obj
		}
	}

	keyOf := func(obj schema.ObjectDesc) (schema.FieldDesc, bool) {
		for _, f := range obj.Fields {
			if f.IsKey {
				return f, true
			}
		}
		return schema.FieldDesc{}, false
	}
	isStringScalar := func(td schema.TypeDesc) bool {
		return td.Kind == schema.TypeKindScalar && td.Name == "string"
	}

	var errs []string
	for _, obj := range objs {
		if obj.Kind != schema.TypeKindStruct {
			continue
		}
		if !obj.IsData {
			for _, f := range obj.Fields {
				if f.IsKey {
					errs = append(errs, fmt.Sprintf("struct %q: the key modifier is only valid inside a @data struct", obj.Name))
				}
				if f.Ref != nil {
					errs = append(errs, fmt.Sprintf("struct %q field %q: @ref is only valid inside a @data struct", obj.Name, f.Name))
				}
			}
			continue
		}
		if obj.SchemaID != 0 {
			errs = append(errs, fmt.Sprintf("@data struct %q must not carry a schema ID: data tables are not transport schemas", obj.Name))
		}
		var keys []string
		for _, f := range obj.Fields {
			if f.IsKey {
				keys = append(keys, f.Name)
			}
		}
		if len(keys) == 0 {
			errs = append(errs, fmt.Sprintf("@data struct %q has no key field; declare exactly one `key <name>: <type>`", obj.Name))
			continue
		}
		if len(keys) > 1 {
			errs = append(errs, fmt.Sprintf("@data struct %q declares multiple key fields (%s); exactly one is allowed", obj.Name, strings.Join(keys, ", ")))
			continue
		}
		key, _ := keyOf(obj)
		if key.Optional {
			errs = append(errs, fmt.Sprintf("@data struct %q: key field %q cannot be optional", obj.Name, key.Name))
		}
		switch {
		case isStringScalar(key.Type):
		case key.Type.Kind == schema.TypeKindScalar && dataKeyIntegerScalars[key.Type.Name]:
		case key.Type.Kind == schema.TypeKindEnum && enumNames[key.Type.Name]:
		default:
			errs = append(errs, fmt.Sprintf("@data struct %q: key field %q must be string, an integer scalar, or an enum type", obj.Name, key.Name))
		}
	}

	for _, obj := range objs {
		if obj.Kind != schema.TypeKindStruct || !obj.IsData {
			continue
		}
		for _, f := range obj.Fields {
			if f.Ref == nil {
				continue
			}
			target, ok := dataObjs[f.Ref.Target]
			if !ok {
				errs = append(errs, fmt.Sprintf("@data struct %q field %q: @ref target %q is not a @data struct in this run", obj.Name, f.Name, f.Ref.Target))
				continue
			}
			key, hasKey := keyOf(target)
			if !hasKey || !isStringScalar(key.Type) {
				errs = append(errs, fmt.Sprintf("@data struct %q field %q: @ref target %q must be string-keyed", obj.Name, f.Name, f.Ref.Target))
				continue
			}
			elemString := f.Type.Kind == schema.TypeKindArray && f.Type.Element != nil && isStringScalar(*f.Type.Element)
			if !isStringScalar(f.Type) && !elemString {
				errs = append(errs, fmt.Sprintf("@data struct %q field %q: @ref fields must be string or array<string> (matching the string key of %q)", obj.Name, f.Name, f.Ref.Target))
				continue
			}
			if f.Ref.Field != "" {
				found := false
				for _, tf := range target.Fields {
					if tf.Name == f.Ref.Field {
						found = true
						break
					}
				}
				if !found {
					errs = append(errs, fmt.Sprintf("@data struct %q field %q: @ref target %q has no field %q", obj.Name, f.Name, f.Ref.Target, f.Ref.Field))
				}
			}
		}
	}

	if len(errs) == 0 {
		return nil
	}
	sort.Strings(errs)
	return fmt.Errorf("@data validation failed:\n  - %s", strings.Join(errs, "\n  - "))
}

// DataTableEntry is one @data struct in the package-level manifest emitted by
// RenderDataTables. KeyType is the Go type expression of the key (possibly
// qualified when the key is an enum emitted in another package).
type DataTableEntry struct {
	Name       string
	KeyField   string
	KeyType    string
	Version    uint64
	SourceFile string
}

// RefShapeEntry is one @ref(T) / @ref(T.field) field declaration in the
// manifest. TargetField is the resolved target field: the explicit field of
// @ref(T.field), else the target's key field name.
type RefShapeEntry struct {
	Table       string
	Field       string
	Target      string
	TargetField string
	IsArray     bool
	Optional    bool
}

// RenderDataTables emits the datatables.gen.go manifest: the DataTables slice
// (one row per @data struct: name, key field, key reflect.Type, row
// reflect.Type, @version) and the RefShapes slice (one row per @ref field).
// The file imports only stdlib reflect (plus external enum packages when a
// key type is a qualified enum), mirroring the dependency-free ComponentShapes
// handoff: the consumer-side loader (e.g. a moddata typed loader) iterates the
// manifest without the generator importing anything of the consumer.
func RenderDataTables(tables []DataTableEntry, refs []RefShapeEntry, opts Options) ([]byte, error) {
	if strings.TrimSpace(opts.Package) == "" {
		return nil, fmt.Errorf("gotypes: Options.Package is required")
	}
	if len(tables) == 0 {
		return nil, fmt.Errorf("gotypes: RenderDataTables requires at least one @data table")
	}

	sorted := append([]DataTableEntry(nil), tables...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	sortedRefs := append([]RefShapeEntry(nil), refs...)
	sort.SliceStable(sortedRefs, func(i, j int) bool {
		if sortedRefs[i].Table != sortedRefs[j].Table {
			return sortedRefs[i].Table < sortedRefs[j].Table
		}
		return sortedRefs[i].Field < sortedRefs[j].Field
	})

	var extImports []string
	for _, t := range sorted {
		if p, ok := opts.importPathForTypeExpr(t.KeyType); ok {
			extImports = append(extImports, p)
		}
	}
	extImports = dedupeSorted(extImports)

	var buf bytes.Buffer
	if h := strings.TrimSpace(opts.Header); h != "" {
		fmt.Fprintln(&buf, h)
	}
	fmt.Fprintf(&buf, "// Source: data tables (%d types)\n\n", len(sorted))
	fmt.Fprintf(&buf, "package %s\n\n", opts.Package)

	buf.WriteString("import (\n\t\"reflect\"\n")
	if len(extImports) > 0 {
		buf.WriteString("\n")
		for _, p := range extImports {
			fmt.Fprintf(&buf, "\t%q\n", p)
		}
	}
	buf.WriteString(")\n\n")

	buf.WriteString("// DataTable describes one @data table for consumer-side loaders (e.g. a\n")
	buf.WriteString("// moddata typed loader): the row struct type, its key field and key type,\n")
	buf.WriteString("// and the declared @version (0 = unversioned). Field names are the Spore\n")
	buf.WriteString("// source names; map them to exported Go names the same way the generated\n")
	buf.WriteString("// structs do.\n")
	buf.WriteString("type DataTable struct {\n")
	buf.WriteString("\tName     string\n")
	buf.WriteString("\tKeyField string\n")
	buf.WriteString("\tKeyType  reflect.Type\n")
	buf.WriteString("\tRowType  reflect.Type\n")
	buf.WriteString("\tVersion  uint64\n")
	buf.WriteString("}\n\n")
	buf.WriteString("// DataTables lists every @data struct in this package, sorted by name.\n")
	buf.WriteString("var DataTables = []DataTable{\n")
	for _, t := range sorted {
		fmt.Fprintf(&buf, "\t{Name: %q, KeyField: %q, KeyType: %s, RowType: reflect.TypeOf(%s{}), Version: %d},\n",
			t.Name, t.KeyField, reflectZeroExpr(t.KeyType), t.Name, t.Version)
	}
	buf.WriteString("}\n")

	if len(sortedRefs) > 0 {
		buf.WriteString("\n")
		buf.WriteString("// RefShape is one @ref(T) / @ref(T.field) declaration. TargetField is the\n")
		buf.WriteString("// referenced field of Target (the explicit field, else the target's key).\n")
		buf.WriteString("// IsArray marks array<string> fields referencing every element.\n")
		buf.WriteString("type RefShape struct {\n")
		buf.WriteString("\tTable       string\n")
		buf.WriteString("\tField       string\n")
		buf.WriteString("\tTarget      string\n")
		buf.WriteString("\tTargetField string\n")
		buf.WriteString("\tIsArray     bool\n")
		buf.WriteString("\tOptional    bool\n")
		buf.WriteString("}\n\n")
		buf.WriteString("// RefShapes lists every @ref field across the package's @data tables,\n")
		buf.WriteString("// sorted by (Table, Field). Load-time reference validation resolves each\n")
		buf.WriteString("// row's leaf string values against the target table's TargetField values.\n")
		buf.WriteString("var RefShapes = []RefShape{\n")
		for _, r := range sortedRefs {
			fmt.Fprintf(&buf, "\t{Table: %q, Field: %q, Target: %q, TargetField: %q, IsArray: %v, Optional: %v},\n",
				r.Table, r.Field, r.Target, r.TargetField, r.IsArray, r.Optional)
		}
		buf.WriteString("}\n")
	}

	out, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("gofmt rejected generated data tables: %w\n--- raw ---\n%s", err, buf.String())
	}
	return out, nil
}

// reflectZeroExpr converts a Go type expression into a reflect.TypeOf operand:
// the zero literal for string, T(0) for numeric and named (enum) types.
func reflectZeroExpr(goType string) string {
	if goType == "string" {
		return `reflect.TypeOf("")`
	}
	return fmt.Sprintf("reflect.TypeOf(%s(0))", goType)
}

// BuildDataTables derives the manifest entries from @data ObjectDescs. Run
// ValidateDataTables first: this function assumes each table has exactly one
// key field and every @ref target resolves to a @data struct in the same run.
// Returns entries in declaration order; RenderDataTables sorts for output.
func BuildDataTables(objs []schema.ObjectDesc, opts Options) ([]DataTableEntry, []RefShapeEntry, error) {
	quals := opts.enumQualifiers()
	dataObjs := make(map[string]schema.ObjectDesc)
	for _, obj := range objs {
		if obj.Kind == schema.TypeKindStruct && obj.IsData {
			dataObjs[obj.Name] = obj
		}
	}
	keyOf := func(obj schema.ObjectDesc) (schema.FieldDesc, bool) {
		for _, f := range obj.Fields {
			if f.IsKey {
				return f, true
			}
		}
		return schema.FieldDesc{}, false
	}

	var tables []DataTableEntry
	var refs []RefShapeEntry
	for _, obj := range objs {
		if obj.Kind != schema.TypeKindStruct || !obj.IsData {
			continue
		}
		key, ok := keyOf(obj)
		if !ok {
			return nil, nil, fmt.Errorf("gotypes: @data struct %q has no key field (ValidateDataTables not run?)", obj.Name)
		}
		goType, err := mapTypeQual(key.Type, quals)
		if err != nil {
			return nil, nil, fmt.Errorf("gotypes: @data struct %q key field %q: %w", obj.Name, key.Name, err)
		}
		tables = append(tables, DataTableEntry{
			Name:     obj.Name,
			KeyField: key.Name,
			KeyType:  goType,
			Version:  obj.DataVersion,
		})
		for _, f := range obj.Fields {
			if f.Ref == nil {
				continue
			}
			target, ok := dataObjs[f.Ref.Target]
			if !ok {
				return nil, nil, fmt.Errorf("gotypes: @data struct %q field %q: @ref target %q missing (ValidateDataTables not run?)", obj.Name, f.Name, f.Ref.Target)
			}
			targetField := f.Ref.Field
			if targetField == "" {
				if tKey, ok := keyOf(target); ok {
					targetField = tKey.Name
				}
			}
			refs = append(refs, RefShapeEntry{
				Table:       obj.Name,
				Field:       f.Name,
				Target:      f.Ref.Target,
				TargetField: targetField,
				IsArray:     f.Type.Kind == schema.TypeKindArray,
				Optional:    f.Optional,
			})
		}
	}
	return tables, refs, nil
}

// enumQualifiers maps external enum type names to their package qualifier
// (the last path segment of the declaring package's import path).
func (o Options) enumQualifiers() map[string]string {
	if len(o.EnumImports) == 0 {
		return nil
	}
	quals := make(map[string]string)
	for _, imp := range o.EnumImports {
		q := qualifierForImportPath(imp.ImportPath)
		for _, name := range imp.Enums {
			quals[name] = q
		}
	}
	return quals
}

// importPathForTypeExpr resolves the external import path for a (possibly
// qualified) Go type expression, reporting ok=false for local types.
func (o Options) importPathForTypeExpr(expr string) (string, bool) {
	idx := strings.Index(expr, ".")
	if idx <= 0 {
		return "", false
	}
	qualifier := expr[:idx]
	for _, imp := range o.EnumImports {
		if qualifierForImportPath(imp.ImportPath) == qualifier {
			return imp.ImportPath, true
		}
	}
	return "", false
}

// usedExternalEnumImports returns the sorted, deduped import paths of external
// enum packages whose enums are actually referenced by a field of objs.
func usedExternalEnumImports(objs []schema.ObjectDesc, opts Options) []string {
	quals := opts.enumQualifiers()
	if len(quals) == 0 {
		return nil
	}
	used := make(map[string]bool)
	var walk func(td schema.TypeDesc)
	walk = func(td schema.TypeDesc) {
		switch td.Kind {
		case schema.TypeKindStruct, schema.TypeKindClass, schema.TypeKindEnum:
			name := td.Name
			if name == "" {
				name = td.ClassName
			}
			if quals[name] != "" {
				used[quals[name]] = true
			}
		case schema.TypeKindArray:
			if td.Element != nil {
				walk(*td.Element)
			}
		case schema.TypeKindMap:
			if td.Key != nil {
				walk(*td.Key)
			}
			if td.Value != nil {
				walk(*td.Value)
			}
		}
	}
	for _, obj := range objs {
		for _, f := range obj.Fields {
			walk(f.Type)
		}
	}
	var paths []string
	for qualifier := range used {
		for _, imp := range opts.EnumImports {
			if qualifierForImportPath(imp.ImportPath) == qualifier {
				paths = append(paths, imp.ImportPath)
				break
			}
		}
	}
	return dedupeSorted(paths)
}

func qualifierForImportPath(importPath string) string {
	return path.Base(strings.TrimSuffix(importPath, "/"))
}

func dedupeSorted(in []string) []string {
	sort.Strings(in)
	out := in[:0]
	var prev string
	for i, s := range in {
		if i > 0 && s == prev {
			continue
		}
		prev = s
		out = append(out, s)
	}
	return out
}
