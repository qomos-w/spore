package tsclient

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/qomos-w/spore/internal/gen/common"
	"github.com/qomos-w/spore/internal/gen/ts"
	"github.com/qomos-w/spore/schema"
)

// renderClient renders the body of `<namespace>/client.ts` for one
// namespace. The caller (Generate) is responsible for sorting entries
// alphabetically by Name and applying the visibility filter.
//
// Output shape:
//
//	import type { ClientTransport, [StreamCall,] [UnaryCall,] } from "@qomos/spore-ts/client";
//	[import type { TypeA, TypeB, ... } from "./types.js";]
//
//	export class <NS>Client {
//	  constructor(private readonly transport: ClientTransport) {}
//
//	  unaryMethod(req: ReqType): Promise<FinalType> { ... }
//	  streamMethod(req: ReqType): StreamCall<ChunkType, FinalType> { ... }
//	}
//
// The runtime imports are conditional: StreamCall is only pulled in when
// the namespace declares at least one streaming callable. UnaryCall is
// never imported because unary methods unwrap to `Promise<Final>` —
// callers never see the UnaryCall handle directly.
func renderClient(namespace string, entries []ts.NamedCallableDesc) string {
	typeImports := gatherTypeImports(entries)
	streaming := hasStreaming(entries)

	var b strings.Builder

	// Runtime import — single line; small enough that a multi-line block
	// would be needlessly verbose.
	b.WriteString("import type { ClientTransport")
	if streaming {
		b.WriteString(", StreamCall")
	}
	b.WriteString(" } from \"@qomos/spore-ts/client\";\n")

	// User-defined types pulled from the same namespace's types.ts. Empty
	// when every callable is void-typed.
	if len(typeImports) > 0 {
		b.WriteString("import type { ")
		b.WriteString(strings.Join(typeImports, ", "))
		b.WriteString(" } from \"./types.js\";\n")
	}
	b.WriteString("\n")

	// Class declaration.
	className := classNameFor(namespace)
	fmt.Fprintf(&b, "export class %s {\n", className)
	b.WriteString("  constructor(private readonly transport: ClientTransport) {}\n")
	for _, c := range entries {
		b.WriteString("\n")
		b.WriteString(renderMethod(c))
	}
	b.WriteString("}\n")
	return b.String()
}

// gatherTypeImports returns the sorted, de-duplicated set of TypeScript
// type names that callable signatures reference. Only struct/class
// TypeDescs contribute imports; void / scalar / array / map are inlined.
func gatherTypeImports(entries []ts.NamedCallableDesc) []string {
	set := map[string]struct{}{}
	add := func(t schema.TypeDesc) {
		if name := importName(t); name != "" {
			set[name] = struct{}{}
		}
	}
	for _, c := range entries {
		add(c.Req)
		if c.Chunk != nil {
			add(*c.Chunk)
		}
		add(c.Final)
	}
	out := make([]string, 0, len(set))
	for name := range set {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// importName returns the TS class name to import for this type, or ""
// when the type renders inline (void, scalar, array, map).
//
// Note arrays and maps of struct elements are rendered as `T[]` /
// `Record<string, T>` where T's class name does need importing — the
// recursion is handled by walking the element/value descriptors.
func importName(t schema.TypeDesc) string {
	switch t.Kind {
	case schema.TypeKindStruct, schema.TypeKindClass:
		if t.ClassName != "" {
			return t.ClassName
		}
		return t.Name
	case schema.TypeKindMedia:
		return "Media"
	case schema.TypeKindArray:
		if t.Element != nil {
			return importName(*t.Element)
		}
	case schema.TypeKindMap:
		if t.Value != nil {
			return importName(*t.Value)
		}
	}
	return ""
}

// hasStreaming reports whether at least one callable uses streaming mode.
func hasStreaming(entries []ts.NamedCallableDesc) bool {
	for _, c := range entries {
		if c.Mode == schema.CallableModeStreaming {
			return true
		}
	}
	return false
}

// classNameFor maps a namespace into a TypeScript class name. Conventions:
// the namespace's first letter is upper-cased and "Client" is suffixed
// (auth → AuthClient, signals → SignalsClient).
func classNameFor(namespace string) string {
	if namespace == "" {
		return "Client"
	}
	runes := []rune(namespace)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes) + "Client"
}

// renderMethod renders one callable as a class method. The chosen shape:
//
//   - unary:   `name(req: Req): Promise<Final> { return this.transport.unary<...>(spec, req).final(); }`
//   - stream:  `name(req: Req): StreamCall<Chunk, Final> { return this.transport.stream<...>(spec, req); }`
//
// When the request type is void the method takes no `req` parameter and
// the underlying transport call receives `undefined` — `req: void` would
// be a confusing public surface even if TypeScript allows it.
//
// Method names preserve the on-wire callable name (typically snake_case).
// Method-name transformation to camelCase is intentionally NOT done — it
// would create an asymmetry between TS and Go and complicate manual
// inspection of network traffic.
func renderMethod(c ts.NamedCallableDesc) string {
	reqType := renderType(c.Req)
	finalType := renderType(c.Final)
	voidReq := c.Req.Kind == schema.TypeKindVoid

	var b strings.Builder

	// Method signature — drop the req param when the request is void.
	param := "req: " + reqType
	if voidReq {
		param = ""
	}
	// Argument passed to the transport call — `undefined` for void.
	arg := "req"
	if voidReq {
		arg = "undefined"
	}

	if c.Mode == schema.CallableModeStreaming {
		chunkType := renderType(*c.Chunk)
		fmt.Fprintf(&b, "  %s(%s): StreamCall<%s, %s> {\n", c.Name, param, chunkType, finalType)
		fmt.Fprintf(&b, "    return this.transport.stream<%s, %s, %s>({\n", reqType, chunkType, finalType)
		fmt.Fprintf(&b, "      namespace: %q,\n", c.Namespace)
		fmt.Fprintf(&b, "      name: %q,\n", c.Name)
		fmt.Fprintf(&b, "      reqSchemaId: %d,\n", c.ReqSchemaID)
		fmt.Fprintf(&b, "      chunkSchemaId: %d,\n", c.ChunkSchemaID)
		fmt.Fprintf(&b, "      finalSchemaId: %d,\n", c.FinalSchemaID)
		fmt.Fprintf(&b, "    }, %s);\n", arg)
		b.WriteString("  }\n")
	} else {
		fmt.Fprintf(&b, "  %s(%s): Promise<%s> {\n", c.Name, param, finalType)
		fmt.Fprintf(&b, "    return this.transport.unary<%s, %s>({\n", reqType, finalType)
		fmt.Fprintf(&b, "      namespace: %q,\n", c.Namespace)
		fmt.Fprintf(&b, "      name: %q,\n", c.Name)
		fmt.Fprintf(&b, "      reqSchemaId: %d,\n", c.ReqSchemaID)
		fmt.Fprintf(&b, "      finalSchemaId: %d,\n", c.FinalSchemaID)
		fmt.Fprintf(&b, "    }, %s).final();\n", arg)
		b.WriteString("  }\n")
	}
	return b.String()
}

// renderType returns the TypeScript expression for a TypeDesc reference,
// usable on the right-hand side of a parameter or return type. Mirrors
// `internal/gen/ts/render.go::renderType` — duplicated here to keep the
// two generators free of cross-package coupling on private helpers.
func renderType(t schema.TypeDesc) string {
	switch t.Kind {
	case schema.TypeKindVoid:
		return "void"
	case schema.TypeKindScalar:
		return renderScalar(t.Name)
	case schema.TypeKindArray:
		if t.Element == nil {
			return "unknown[]"
		}
		return renderType(*t.Element) + "[]"
	case schema.TypeKindMap:
		if t.Key == nil || t.Value == nil {
			return "Record<string, unknown>"
		}
		return "Record<string, " + renderType(*t.Value) + ">"
	case schema.TypeKindStruct, schema.TypeKindClass:
		if t.ClassName != "" {
			return t.ClassName
		}
		if t.Name != "" {
			return t.Name
		}
		return "unknown"
	case schema.TypeKindMedia:
		return "Media"
	default:
		return "unknown"
	}
}

// renderScalar maps Spore scalar names to TypeScript equivalents, resolving
// through the shared scalar table in internal/gen/common (the same table
// internal/gen/ts uses, so the two TypeScript surfaces cannot drift). Unknown
// names pass through verbatim; the empty name renders as `unknown`.
func renderScalar(name string) string {
	if name == "" {
		return "unknown"
	}
	if out, ok := common.TSScalar(name); ok {
		return out
	}
	return name
}
