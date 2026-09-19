package ts

import (
	"fmt"
	"sort"
	"strings"

	"github.com/qomos-w/spore/schema"
)

// Generate renders schema descriptors and callable descriptors into a
// path → file content map. Paths are relative (e.g. "auth/types.ts") so the
// caller writes them under whatever output directory it chose.
//
// Filtering: schema and callable entries whose Visibility does not match
// Options.Visibilities are dropped silently, except that schemas referenced by
// surviving callables are retained so generated callable signatures always have
// their companion type declarations. Within a namespace, the surviving entries
// produce one or more of:
//
//	<namespace>/types.ts       — interface declarations, alphabetised by name
//	<namespace>/registry.ts    — SchemaIDs constant + SchemaRegistry meta
//	<namespace>/callables.ts   — callableEntries + buildCallableRegistry (only
//	                             when the namespace has callables surviving
//	                             the visibility filter)
//	<namespace>/index.ts       — re-export barrel
//
// All produced content is deterministic: input order does not affect output.
//
// callables may be nil — in that case Generate emits only schema artifacts
// (the legacy "schemas only" form) and never produces callables.ts.
func Generate(schemas []NamedObjectDesc, callables []NamedCallableDesc, opts Options) (map[string]string, error) {
	if err := validateSchemas(schemas); err != nil {
		return nil, err
	}
	if err := validateCallables(callables); err != nil {
		return nil, err
	}

	// Group by namespace, applying the visibility filter as we go. We collect
	// every namespace that has any surviving entry — schema or callable —
	// because both kinds need at minimum an index.ts barrel.
	schemaByNS := map[string][]NamedObjectDesc{}
	callableByNS := map[string][]NamedCallableDesc{}
	allNamespaces := map[string]struct{}{}
	schemasByNS := indexSchemasByNamespace(schemas)
	requiredByNS := referencedSchemas(schemasByNS, schemas, callables, opts)
	nameToNamespace := buildNameToNamespace(schemas)

	for _, e := range schemas {
		if !opts.shouldEmit(e.Visibility) {
			if _, ok := requiredByNS[e.Namespace][schemaKey{e.Namespace, e.SchemaID}]; !ok {
				continue
			}
		}
		schemaByNS[e.Namespace] = append(schemaByNS[e.Namespace], e)
		allNamespaces[e.Namespace] = struct{}{}
	}
	for _, c := range callables {
		if !opts.shouldEmit(c.Visibility) {
			continue
		}
		callableByNS[c.Namespace] = append(callableByNS[c.Namespace], c)
		allNamespaces[c.Namespace] = struct{}{}
	}

	files := map[string]string{}
	for ns := range allNamespaces {
		ownSchemas := schemaByNS[ns]
		callableEntries := callableByNS[ns]

		// types.ts contains only schemas that belong to this namespace.
		// References to types in other namespaces are resolved through
		// generated import statements and qualified names (e.g. systemTypes.Foo).
		typeSchemas := ownSchemas
		// Media may also be referenced by callable signatures alone; the
		// interface must exist in types.ts for the client import to resolve.
		needsMedia := namespaceUsesMedia(typeSchemas) || callablesUseMedia(callableEntries)
		if len(typeSchemas) > 0 || needsMedia {
			sort.SliceStable(typeSchemas, func(i, j int) bool { return typeSchemas[i].Name < typeSchemas[j].Name })

			ctx := newRenderContext(ns, nameToNamespace)
			var typesBuf strings.Builder
			writeHeader(&typesBuf, opts.Header)
			if needsMedia {
				typesBuf.WriteString(mediaInterfaceTS)
				typesBuf.WriteString("\n")
			}
			for _, e := range typeSchemas {
				body, err := renderInterfaceWithContext(e.Object, ctx)
				if err != nil {
					return nil, fmt.Errorf("namespace %q: %w", ns, err)
				}
				typesBuf.WriteString(body)
				typesBuf.WriteString("\n")
			}
			for _, imp := range ctx.importStatements() {
				typesBuf.WriteString(imp)
				typesBuf.WriteString("\n")
			}
			files[ns+"/types.ts"] = typesBuf.String()
		}

		// registry.ts only emits for schemas that belong to this namespace.
		if len(ownSchemas) > 0 {
			sort.SliceStable(ownSchemas, func(i, j int) bool { return ownSchemas[i].Name < ownSchemas[j].Name })

			var regBuf strings.Builder
			writeHeader(&regBuf, opts.Header)
			regBuf.WriteString(renderRegistry(ns, ownSchemas))
			files[ns+"/registry.ts"] = regBuf.String()
		}

		// callables.ts emits whenever the namespace has callables surviving
		// the filter. Sort by Name for deterministic output.
		if len(callableEntries) > 0 {
			sort.SliceStable(callableEntries, func(i, j int) bool { return callableEntries[i].Name < callableEntries[j].Name })

			var callBuf strings.Builder
			writeHeader(&callBuf, opts.Header)
			callBuf.WriteString(renderCallables(ns, callableEntries))
			files[ns+"/callables.ts"] = callBuf.String()
		}

		// index.ts always emits when the namespace appears at all. The
		// barrel re-exports whichever sibling files were produced.
		var idxBuf strings.Builder
		writeHeader(&idxBuf, opts.Header)
		idxBuf.WriteString(renderIndex(len(typeSchemas) > 0, len(ownSchemas) > 0, len(callableEntries) > 0))
		files[ns+"/index.ts"] = idxBuf.String()
	}

	return files, nil
}

// schemaKey identifies a schema entry by its (namespace, schemaID) pair —
// the authoritative identity used by validateSchemas and the wire layer.
// Names are not unique across namespaces and were retired as the retention
// key after the same-name-different-namespace bug.
type schemaKey struct {
	namespace string
	id        uint64
}

// indexSchemasByNamespace builds a (namespace, name) → entry lookup so
// nested struct references inside a callable payload TypeDesc can be
// resolved without a global name map. We index by both NamedObjectDesc.Name
// and ObjectDesc.Name because nested TypeDesc.ClassName references can
// match either; in practice they are usually equal, but the data model
// permits them to diverge.
func indexSchemasByNamespace(schemas []NamedObjectDesc) map[string]map[string]NamedObjectDesc {
	out := make(map[string]map[string]NamedObjectDesc, len(schemas))
	for _, s := range schemas {
		nsTable, ok := out[s.Namespace]
		if !ok {
			nsTable = make(map[string]NamedObjectDesc)
			out[s.Namespace] = nsTable
		}
		if s.Name != "" {
			nsTable[s.Name] = s
		}
		if s.Object.Name != "" && s.Object.Name != s.Name {
			if _, dup := nsTable[s.Object.Name]; !dup {
				nsTable[s.Object.Name] = s
			}
		}
	}
	return out
}

// buildGlobalByName maps every schema name (and ObjectDesc.Name) to its
// canonical entry so that callable TypeDesc references can be resolved across
// namespaces.
func buildGlobalByName(schemas []NamedObjectDesc) map[string]NamedObjectDesc {
	out := make(map[string]NamedObjectDesc, len(schemas))
	for _, s := range schemas {
		if s.Name != "" {
			if _, ok := out[s.Name]; !ok {
				out[s.Name] = s
			}
		}
		if s.Object.Name != "" && s.Object.Name != s.Name {
			if _, ok := out[s.Object.Name]; !ok {
				out[s.Object.Name] = s
			}
		}
	}
	return out
}

// buildNameToNamespace maps every schema name (and ObjectDesc.Name) to the
// namespace that owns it. The map is used when rendering a namespace's
// types.ts so that field references to types in other namespaces can be
// qualified with an import alias.
func buildNameToNamespace(schemas []NamedObjectDesc) map[string]string {
	out := make(map[string]string, len(schemas))
	for _, s := range schemas {
		if s.Name != "" {
			if _, ok := out[s.Name]; !ok {
				out[s.Name] = s.Namespace
			}
		}
		if s.Object.Name != "" && s.Object.Name != s.Name {
			if _, ok := out[s.Object.Name]; !ok {
				out[s.Object.Name] = s.Namespace
			}
		}
	}
	return out
}

// referencedSchemas walks every surviving callable's request / chunk /
// final TypeDesc and every own schema's fields, returning for each namespace
// the set of (namespace, schemaID) keys that must be available in that
// namespace's types.ts. References are resolved first in the namespace's own
// schemas, then globally, so schemas that have been moved to a shared
// namespace (e.g. "system") are still found.
func referencedSchemas(schemasByNS map[string]map[string]NamedObjectDesc, schemas []NamedObjectDesc, callables []NamedCallableDesc, opts Options) map[string]map[schemaKey]struct{} {
	globalByName := buildGlobalByName(schemas)
	requiredByNS := make(map[string]map[schemaKey]struct{})

	for _, c := range callables {
		if !opts.shouldEmit(c.Visibility) {
			continue
		}
		addReferencedSchema(requiredByNS, schemasByNS, c.Namespace, c.Req, globalByName)
		addReferencedSchema(requiredByNS, schemasByNS, c.Namespace, typeDescOrZero(c.Chunk), globalByName)
		addReferencedSchema(requiredByNS, schemasByNS, c.Namespace, c.Final, globalByName)
	}

	for _, s := range schemas {
		if !opts.shouldEmit(s.Visibility) {
			continue
		}
		selfRef := schema.TypeDesc{
			Kind:      s.Object.Kind,
			Name:      s.Object.Name,
			ClassName: s.Object.Name,
		}
		addReferencedSchema(requiredByNS, schemasByNS, s.Namespace, selfRef, globalByName)
	}

	return requiredByNS
}

func addReferencedSchema(requiredByNS map[string]map[schemaKey]struct{}, schemasByNS map[string]map[string]NamedObjectDesc, namespace string, desc schema.TypeDesc, globalByName map[string]NamedObjectDesc) {
	ensure := func(ns string) map[schemaKey]struct{} {
		if requiredByNS[ns] == nil {
			requiredByNS[ns] = make(map[schemaKey]struct{})
		}
		return requiredByNS[ns]
	}

	switch desc.Kind {
	case schema.TypeKindStruct, schema.TypeKindClass:
		name := desc.ClassName
		if name == "" {
			name = desc.Name
		}
		if name == "" {
			return
		}
		var entry NamedObjectDesc
		var ok bool
		if nsTable, hasNS := schemasByNS[namespace]; hasNS {
			entry, ok = nsTable[name]
		}
		if !ok {
			entry, ok = globalByName[name]
		}
		if !ok {
			return
		}
		required := ensure(entry.Namespace)
		key := schemaKey{entry.Namespace, entry.SchemaID}
		if _, dup := required[key]; dup {
			return
		}
		required[key] = struct{}{}
		for _, field := range entry.Object.Fields {
			addReferencedSchema(requiredByNS, schemasByNS, entry.Namespace, field.Type, globalByName)
		}
	case schema.TypeKindArray:
		if desc.Element != nil {
			addReferencedSchema(requiredByNS, schemasByNS, namespace, *desc.Element, globalByName)
		}
	case schema.TypeKindMap:
		if desc.Key != nil {
			addReferencedSchema(requiredByNS, schemasByNS, namespace, *desc.Key, globalByName)
		}
		if desc.Value != nil {
			addReferencedSchema(requiredByNS, schemasByNS, namespace, *desc.Value, globalByName)
		}
	}
}

func typeDescOrZero(desc *schema.TypeDesc) schema.TypeDesc {
	if desc == nil {
		return schema.TypeDesc{}
	}
	return *desc
}

// validateSchemas rejects schema entries with empty Namespace / Name or
// duplicate (Namespace, SchemaID) pairs. Duplicates would either silently
// overwrite or render conflicting registry entries; better to fail loudly.
func validateSchemas(input []NamedObjectDesc) error {
	seen := map[string]map[uint64]string{}
	for i, e := range input {
		if e.Namespace == "" {
			return fmt.Errorf("schema entry[%d]: empty Namespace", i)
		}
		if e.Name == "" {
			return fmt.Errorf("schema entry[%d]: empty Name", i)
		}
		if e.Object.Name == "" {
			return fmt.Errorf("schema entry[%d] %s/%s: ObjectDesc.Name is empty", i, e.Namespace, e.Name)
		}
		nsTable, ok := seen[e.Namespace]
		if !ok {
			nsTable = map[uint64]string{}
			seen[e.Namespace] = nsTable
		}
		if existing, dup := nsTable[e.SchemaID]; dup {
			return fmt.Errorf("namespace %q: SchemaID %d collision between %q and %q",
				e.Namespace, e.SchemaID, existing, e.Name)
		}
		nsTable[e.SchemaID] = e.Name
	}
	return nil
}

// validateCallables rejects callable entries with empty Namespace / Name,
// duplicate (Namespace, Name) pairs, or stream callables that lack chunk
// schema. Mode must be one of "unary" / "streaming".
func validateCallables(input []NamedCallableDesc) error {
	seen := map[string]map[string]struct{}{}
	for i, c := range input {
		if c.Namespace == "" {
			return fmt.Errorf("callable entry[%d]: empty Namespace", i)
		}
		if c.Name == "" {
			return fmt.Errorf("callable entry[%d]: empty Name", i)
		}
		switch string(c.Mode) {
		case "unary":
			if c.ChunkSchemaID != 0 || c.Chunk != nil {
				return fmt.Errorf("callable %s/%s: unary mode must not carry chunk schema", c.Namespace, c.Name)
			}
		case "streaming":
			if c.ChunkSchemaID == 0 {
				return fmt.Errorf("callable %s/%s: streaming mode requires non-zero ChunkSchemaID", c.Namespace, c.Name)
			}
			if c.Chunk == nil {
				return fmt.Errorf("callable %s/%s: streaming mode requires Chunk TypeDesc", c.Namespace, c.Name)
			}
		default:
			return fmt.Errorf("callable %s/%s: unknown Mode %q (want unary or streaming)", c.Namespace, c.Name, c.Mode)
		}
		nsTable, ok := seen[c.Namespace]
		if !ok {
			nsTable = map[string]struct{}{}
			seen[c.Namespace] = nsTable
		}
		if _, dup := nsTable[c.Name]; dup {
			return fmt.Errorf("namespace %q: callable name %q declared twice", c.Namespace, c.Name)
		}
		nsTable[c.Name] = struct{}{}
	}
	return nil
}

// writeHeader prepends the optional comment header to a file buffer.
// A single trailing blank line separates the header from generated content
// so editors render the boundary clearly.
func writeHeader(b *strings.Builder, header string) {
	if header == "" {
		return
	}
	b.WriteString(header)
	if !strings.HasSuffix(header, "\n") {
		b.WriteString("\n")
	}
	b.WriteString("\n")
}

// mediaInterfaceTS is the canonical TypeScript carrier for the media schema
// type, emitted into a namespace's types.ts when any field references media.
const mediaInterfaceTS = `// Media is the canonical {mime, src} reference carrier for the media schema
// type. src is a data:/file:/https: reference; inline data: URLs are capped
// at 1 MiB and file:/https: resolution is host-side.
export interface Media {
  mime: string;
  src: string;
}
`

// namespaceUsesMedia reports whether any schema field references the media
// type (directly or through array/map nesting).
func namespaceUsesMedia(entries []NamedObjectDesc) bool {
	for _, e := range entries {
		for _, f := range e.Object.Fields {
			if typeDescUsesMediaTS(f.Type) {
				return true
			}
		}
	}
	return false
}

func typeDescUsesMediaTS(td schema.TypeDesc) bool {
	if td.Kind == schema.TypeKindMedia {
		return true
	}
	if td.Element != nil && typeDescUsesMediaTS(*td.Element) {
		return true
	}
	if td.Key != nil && typeDescUsesMediaTS(*td.Key) {
		return true
	}
	if td.Value != nil && typeDescUsesMediaTS(*td.Value) {
		return true
	}
	return false
}

// callablesUseMedia reports whether any callable request/chunk/final type
// references the media type.
func callablesUseMedia(entries []NamedCallableDesc) bool {
	for _, c := range entries {
		if typeDescUsesMediaTS(c.Req) || typeDescUsesMediaTS(c.Final) {
			return true
		}
		if c.Chunk != nil && typeDescUsesMediaTS(*c.Chunk) {
			return true
		}
	}
	return false
}
