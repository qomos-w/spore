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
// Like `internal/gen/ts`, this is internal to the spore module. The public
// code-generation entry points are the `spore-gen-ts-client` CLI and the
// re-export `GenerateTSClient` in gen/render; the CLI itself calls that
// re-export rather than this package directly.
package tsclient

import (
	"sort"
	"strings"

	"github.com/qomos-w/spore/internal/gen/common"
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
	if err := common.ValidateCallables(callables, common.ValidateOptions{}); err != nil {
		return nil, err
	}

	byNS := map[string][]ts.NamedCallableDesc{}
	for _, c := range callables {
		if !opts.ShouldEmit(c.Visibility) {
			continue
		}
		byNS[c.Namespace] = append(byNS[c.Namespace], c)
	}

	files := map[string]string{}
	for ns, entries := range byNS {
		sort.SliceStable(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })

		var b strings.Builder
		common.WriteHeader(&b, opts.Header)
		b.WriteString(renderClient(ns, entries))
		files[ns+"/client.ts"] = b.String()
	}
	return files, nil
}
