// Package goserver generates Go server-side dispatcher code from spore
// callable + schema descriptors. It is the symmetric sibling of
// `internal/gen/ts-client`: where ts-client produces TypeScript client
// classes that wrap a `ClientTransport`, this package produces Go code
// that wraps a typed handler interface and translates between map-based
// wire payloads and the handler's typed Go structs.
//
// Per namespace, one `<ns>_dispatcher_gen.go` file is emitted under the
// configured Go package. Each file declares:
//
//   - `<NS>Handler` interface — every public callable in the namespace
//     becomes one method whose signature reflects the callable's typed
//     request/final shapes.
//   - `Dispatch<NS>` function — accepts (handler, callID, payload), looks
//     up the callable, marshals the wire-form payload into the typed
//     request struct, invokes the handler method, then converts the typed
//     response back into a wire-form `map[string]any`.
//
// MVP scope: only struct request and struct final types are supported;
// void / scalar / array / map / nested-struct fields are rejected at
// generation time. Streaming callables are also rejected — the streaming
// server side has its own dispatch shape (channel/iterator) that this
// package does not yet target. exp09 (the primary consumer today) hits
// only the supported subset.
//
// Like ts and ts-client, this package is internal to the spore module:
// external users invoke the wrapping CLI in `cmd/spore-gen-go-server`.
package goserver

import (
	"fmt"
	"go/format"
	"sort"
	"strings"

	"github.com/qomos-w/spore/internal/gen/ts"
	"github.com/qomos-w/spore/schema"
)

// Options controls the rendered output.
//
// Package is the Go package declaration written into every generated file
// (e.g., "exp09"). It must be a valid Go identifier; Generate enforces
// non-empty.
//
// Visibilities and Header mirror `ts.Options`: empty Visibilities defaults
// to {VisibilityPublic}; Header is prepended verbatim to each file.
type Options struct {
	Package      string
	Visibilities []ts.Visibility
	Header       string
}

// Generate renders dispatcher source files keyed by relative path. Each
// surviving namespace gets one `<ns>_dispatcher_gen.go` file in the root
// of the output directory; consumers MkdirAll on the parent and write
// content directly.
//
// schemas is consulted to look up field shapes for each callable's
// request and final TypeDescs — the codegen must walk fields to emit
// `payload["x"].(string)` and `map[string]any{"y": resp.Y}` literals,
// so callables-only input (like ts-client receives) is insufficient.
func Generate(
	schemas []ts.NamedObjectDesc,
	callables []ts.NamedCallableDesc,
	opts Options,
) (map[string]string, error) {
	if strings.TrimSpace(opts.Package) == "" {
		return nil, fmt.Errorf("goserver: Options.Package is required")
	}
	if err := validateCallables(callables); err != nil {
		return nil, err
	}

	schemaIndex := indexSchemas(schemas)

	byNS := map[string][]ts.NamedCallableDesc{}
	for _, c := range callables {
		if !shouldEmit(opts, c.Visibility) {
			continue
		}
		byNS[c.Namespace] = append(byNS[c.Namespace], c)
	}

	files := map[string]string{}
	for ns, entries := range byNS {
		sort.SliceStable(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })

		body, err := renderDispatcher(opts.Package, ns, entries, schemaIndex)
		if err != nil {
			return nil, fmt.Errorf("namespace %s: %w", ns, err)
		}

		var b strings.Builder
		writeHeader(&b, opts.Header)
		b.WriteString(body)
		formatted, err := format.Source([]byte(b.String()))
		if err != nil {
			return nil, fmt.Errorf("namespace %s: gofmt rejected generated source: %w\n--- raw ---\n%s", ns, err, b.String())
		}
		files[ns+"_dispatcher_gen.go"] = string(formatted)
	}
	return files, nil
}

// validateCallables enforces the same baseline contract as ts-client's
// validator (non-empty namespace/name, mode-consistent fields, unique
// (ns, name) pairs) plus an extra goserver-specific rule: streaming
// callables are NOT yet supported by the server-side dispatcher
// generator.
func validateCallables(input []ts.NamedCallableDesc) error {
	seen := map[string]map[string]struct{}{}
	for i, c := range input {
		if c.Namespace == "" {
			return fmt.Errorf("callable entry[%d]: empty Namespace", i)
		}
		if c.Name == "" {
			return fmt.Errorf("callable entry[%d]: empty Name", i)
		}
		switch c.Mode {
		case schema.CallableModeUnary:
			if c.ChunkSchemaID != 0 || c.Chunk != nil {
				return fmt.Errorf("callable %s/%s: unary mode must not carry chunk schema", c.Namespace, c.Name)
			}
		case schema.CallableModeStreaming:
			return fmt.Errorf("callable %s/%s: streaming callables are not yet supported by goserver", c.Namespace, c.Name)
		default:
			return fmt.Errorf("callable %s/%s: unknown Mode %q (want unary)", c.Namespace, c.Name, c.Mode)
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

// shouldEmit reports whether the entry passes the visibility filter.
// Empty Options.Visibilities defaults to {VisibilityPublic}, matching
// ts-client.
func shouldEmit(opts Options, v ts.Visibility) bool {
	want := opts.Visibilities
	if len(want) == 0 {
		want = []ts.Visibility{ts.VisibilityPublic}
	}
	for _, w := range want {
		if w == v {
			return true
		}
	}
	return false
}

// indexSchemas builds a (namespace, struct name) → ObjectDesc lookup so
// the renderer can resolve a callable's req/final TypeDesc into its
// actual field list. Index keys use the same ClassName/Name fallback
// as the renderer uses to format Go type references.
func indexSchemas(schemas []ts.NamedObjectDesc) map[schemaKey]schema.ObjectDesc {
	out := make(map[schemaKey]schema.ObjectDesc, len(schemas))
	for _, s := range schemas {
		out[schemaKey{Namespace: s.Namespace, Name: s.Name}] = s.Object
	}
	return out
}

// schemaKey identifies a schema by namespace + struct name. We keep
// (namespace, name) as the lookup key rather than schemaId because
// callables' TypeDesc references carry Name/ClassName, not SchemaID.
type schemaKey struct {
	Namespace string
	Name      string
}

// writeHeader prepends the optional comment header to a file buffer.
// Mirrors ts-client.writeHeader exactly so all spore-gen-* CLIs lay
// out file headers identically.
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
