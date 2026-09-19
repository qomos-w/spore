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
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/qomos-w/spore/gen/render"
	"github.com/qomos-w/spore/internal/gen/manifest"
)

func main() {
	var (
		inPath        = flag.String("in", "-", `manifest path; "-" reads stdin`)
		outDir        = flag.String("out", "", "output directory (required)")
		visibilityCSV = flag.String("visibility", "public", "comma-separated visibilities to include: internal,public,admin,diagnostic")
		header        = flag.String("header", "// AUTO-GENERATED — DO NOT EDIT", "comment header prepended to every generated file")
	)
	flag.Parse()

	if *outDir == "" {
		fmt.Fprintln(os.Stderr, "spore-gen-ts: --out is required")
		flag.Usage()
		os.Exit(2)
	}

	visibilities, err := manifest.ParseVisibilities(*visibilityCSV)
	if err != nil {
		fail(err)
	}

	raw, err := readInput(*inPath)
	if err != nil {
		fail(fmt.Errorf("read manifest: %w", err))
	}

	schemas, callables, err := manifest.Decode(raw)
	if err != nil {
		fail(fmt.Errorf("decode manifest: %w", err))
	}

	files, err := render.Generate(schemas, callables, render.Options{
		Visibilities: visibilities,
		Header:       *header,
	})
	if err != nil {
		fail(fmt.Errorf("generate: %w", err))
	}

	for relPath, content := range files {
		full := filepath.Join(*outDir, relPath)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			fail(fmt.Errorf("mkdir %s: %w", full, err))
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			fail(fmt.Errorf("write %s: %w", full, err))
		}
	}
	fmt.Fprintf(os.Stderr, "spore-gen-ts: wrote %d file(s) to %s\n", len(files), *outDir)
}

func readInput(path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(path)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "spore-gen-ts:", err)
	os.Exit(1)
}
