// Command spore-gen-ts renders a JSON manifest of schema descriptors and
// callable descriptors into a directory of TypeScript files.
//
// Manifest format — legacy bare-array (schemas only):
//
//	[
//	  {
//	    "namespace":  "auth",
//	    "schemaId":   1,
//	    "name":       "LoginReq",
//	    "visibility": "public",
//	    "object":     { ... schema.ObjectDesc as JSON ... }
//	  },
//	  ...
//	]
//
// Manifest format — combined object form (schemas + callables):
//
//	{
//	  "schemas":   [ ...same shape as legacy entries... ],
//	  "callables": [
//	    {
//	      "namespace":     "auth",
//	      "name":          "tail_logins",
//	      "visibility":    "public",
//	      "mode":          "streaming",
//	      "reqSchemaId":   1,
//	      "chunkSchemaId": 2,
//	      "finalSchemaId": 3,
//	      "req":           { ... schema.TypeDesc as JSON ... },
//	      "chunk":         { ... },
//	      "final":         { ... }
//	    }
//	  ]
//	}
//
// Both forms are accepted; combined form lets a single manifest carry both
// data shapes (rendered to <ns>/types.ts + <ns>/registry.ts) and callable
// invocation metadata (rendered to <ns>/callables.ts).
//
// Users dump this manifest from a small Go program in their own repository
// using schema.DescribeGoStruct and any callable metadata they maintain —
// see README.md for an example.
//
// Generation runs through the public gen/render façade — the same entry point
// external embedders use — so the four spore-gen-* CLIs present one symmetric
// public code-generation surface. Only CLI plumbing (flag registration,
// manifest decoding, file IO) comes from internal packages.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/qomos-w/spore/gen/render"
	"github.com/qomos-w/spore/internal/gen/common"
	"github.com/qomos-w/spore/internal/gen/manifest"
)

func main() {
	flags := common.RegisterManifestFlags(flag.CommandLine, false)
	flag.Parse()

	if flags.Out == "" {
		fmt.Fprintln(os.Stderr, "spore-gen-ts: --out is required")
		flag.Usage()
		os.Exit(2)
	}

	visibilities, err := manifest.ParseVisibilities(flags.Visibility)
	if err != nil {
		fail(err)
	}

	raw, err := common.ReadInput(flags.In)
	if err != nil {
		fail(fmt.Errorf("read manifest: %w", err))
	}

	schemas, callables, err := manifest.Decode(raw)
	if err != nil {
		fail(fmt.Errorf("decode manifest: %w", err))
	}

	files, err := render.Generate(schemas, callables, render.Options{
		Visibilities: visibilities,
		Header:       flags.Header,
	})
	if err != nil {
		fail(fmt.Errorf("generate: %w", err))
	}

	if err := common.WriteFiles(flags.Out, files); err != nil {
		fail(err)
	}
	fmt.Fprintf(os.Stderr, "spore-gen-ts: wrote %d file(s) to %s\n", len(files), flags.Out)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "spore-gen-ts:", err)
	os.Exit(1)
}
