// Command spore-gen-go-server renders the callable section of a spore
// JSON manifest into Go server-side dispatcher source files. It is the
// Go-flavored sibling of `spore-gen-ts-client`: both consume the same
// `{schemas, callables}` manifest produced by `spore-gen-ts`-style
// reflection, but emit code targeting different sides of the wire.
//
// One `<namespace>_dispatcher_gen.go` file is emitted per namespace
// surviving the visibility filter. Each file declares an `<NS>Handler`
// interface (one method per callable) and a top-level `Dispatch<NS>`
// function that translates a `(callID, payload map[string]any)` pair
// into a typed handler invocation. User code implements the interface
// with typed Go structs; the marshal between map and struct is generated.
//
// Usage:
//
//	spore-gen-go-server \
//	    --in       ./manifest.json \
//	    --out      ./server \
//	    --package  exp09 \
//	    --visibility public
//
// MVP scope mirrors `internal/gen/go-server`: only struct request and
// struct final types with scalar fields are supported; streaming
// callables and nested struct fields fail loudly. exp09 is the primary
// consumer today and stays inside the supported subset.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	goserver "github.com/qomos-w/spore/internal/gen/go-server"
	"github.com/qomos-w/spore/internal/gen/manifest"
)

func main() {
	var (
		inPath        = flag.String("in", "-", `manifest path; "-" reads stdin`)
		outDir        = flag.String("out", "", "output directory (required)")
		pkgName       = flag.String("package", "", "Go package declaration written into every generated file (required)")
		visibilityCSV = flag.String("visibility", "public", "comma-separated visibilities to include: internal,public,admin,diagnostic")
		header        = flag.String("header", "// AUTO-GENERATED — DO NOT EDIT", "comment header prepended to every generated file")
	)
	flag.Parse()

	if *outDir == "" {
		fmt.Fprintln(os.Stderr, "spore-gen-go-server: --out is required")
		flag.Usage()
		os.Exit(2)
	}
	if *pkgName == "" {
		fmt.Fprintln(os.Stderr, "spore-gen-go-server: --package is required")
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

	files, err := goserver.Generate(schemas, callables, goserver.Options{
		Package:      *pkgName,
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
	fmt.Fprintf(os.Stderr, "spore-gen-go-server: wrote %d file(s) to %s\n", len(files), *outDir)
}

func readInput(path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(path)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "spore-gen-go-server:", err)
	os.Exit(1)
}
