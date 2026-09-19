// Command spore-gen-ts-client renders the callable section of a spore
// JSON manifest into per-namespace TypeScript client classes that wrap the
// `@qomos/spore-ts/client` transport seam.
//
// It accepts the same manifest format as `spore-gen-ts` (the schemas-only
// legacy bare array OR the new `{schemas, callables}` object form). The
// schemas section is parsed but only used for validation — output is purely
// callable-driven, one `<namespace>/client.ts` file per namespace whose
// callables survive the visibility filter.
//
// Usage:
//
//	spore-gen-ts-client \
//	    --in   ./schemas.json \
//	    --out  ./client/src/generated \
//	    --visibility public
//
// The `--out` directory is typically the same one used for `spore-gen-ts`
// so the generated `client.ts` lands beside its corresponding `types.ts`,
// `registry.ts`, and `callables.ts`. Generated client methods import their
// argument / return types from `./types.js`; the convention is that every
// callable's req/chunk/final type also has a matching schema entry in the
// same namespace. Cross-namespace type imports are out of scope for the
// MVP — flag this when planning your manifest.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/qomos-w/spore/internal/gen/manifest"
	"github.com/qomos-w/spore/internal/gen/ts"
	tsclient "github.com/qomos-w/spore/internal/gen/ts-client"
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
		fmt.Fprintln(os.Stderr, "spore-gen-ts-client: --out is required")
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

	// We discard the schemas slice — client generation only needs the
	// callables list. Decoding the schemas anyway keeps us strict about
	// rejecting malformed manifests at the same boundary as
	// spore-gen-ts.
	_, callables, err := manifest.Decode(raw)
	if err != nil {
		fail(fmt.Errorf("decode manifest: %w", err))
	}

	files, err := tsclient.Generate(callables, ts.Options{
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
	fmt.Fprintf(os.Stderr, "spore-gen-ts-client: wrote %d file(s) to %s\n", len(files), *outDir)
}

func readInput(path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(path)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "spore-gen-ts-client:", err)
	os.Exit(1)
}
