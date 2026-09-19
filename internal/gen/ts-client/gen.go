// Package tsclient generates TypeScript client classes from spore callable
// descriptors. It is the sibling of `internal/gen/ts`: where `ts` produces
// schema interfaces and registry/callable manifests for the runtime, this
// package produces typed client classes that consume the runtime's
// `ClientTransport` to launch invocations.
//
// One file per namespace is emitted under `<ns>/client.ts`; each file
// exports a `<NS>Client` class whose methods correspond to that namespace's
// callables. Unary callables expose `Promise<Final>` methods; streaming
// callables expose `StreamCall<Chunk, Final>` methods. The generated class
// holds a single `ClientTransport` and routes every call through it,
// passing the spec object literal (namespace / name / schema IDs) the
// transport needs to encode the wire frame.
//
// Like `internal/gen/ts`, this is internal to the spore module: external
// users invoke the wrapping CLI in `cmd/spore-gen-ts-client`.
package tsclient

import (
	"fmt"
	"sort"
	"strings"

	"github.com/qomos-w/spore/internal/gen/ts"
)

// Generate renders callable descriptors into a path → file content map. One
// `<namespace>/client.ts` file is emitted per namespace whose callables
// survive Options.Visibilities filtering.
//
// The shape of input mirrors `ts.Generate` so both CLIs can share a single
// manifest decoder. We deliberately do NOT consume schema entries here —
// callable descriptors carry every TypeDesc we need for method signatures,
// and types.ts itself is produced by the schema generator. Generated
// methods import types from `./types.js` (same namespace) under the
// convention that every callable's req/chunk/final type has a matching
// schema entry in the same namespace; cross-namespace imports are out of
// scope for the MVP.
func Generate(callables []ts.NamedCallableDesc, opts ts.Options) (map[string]string, error) {
	if err := validateCallables(callables); err != nil {
		return nil, err
	}

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

		var b strings.Builder
		writeHeader(&b, opts.Header)
		b.WriteString(renderClient(ns, entries))
		files[ns+"/client.ts"] = b.String()
	}
	return files, nil
}

// validateCallables enforces the same contract as ts.validateCallables:
// non-empty Namespace / Name, mode-consistent chunk schema, and unique
// (Namespace, Name) pairs. Duplicating the logic keeps tsclient
// independent of ts's private validators while still rejecting the same
// malformed inputs at the same layer.
func validateCallables(input []ts.NamedCallableDesc) error {
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

// shouldEmit reports whether the callable passes the visibility filter.
// Empty Options.Visibilities defaults to {VisibilityPublic} — the common
// case for browser-facing client bundles.
func shouldEmit(opts ts.Options, v ts.Visibility) bool {
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
