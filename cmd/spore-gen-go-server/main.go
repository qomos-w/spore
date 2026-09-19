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
	"os"

	"github.com/qomos-w/spore/internal/gen/common"
	goserver "github.com/qomos-w/spore/internal/gen/go-server"
	"github.com/qomos-w/spore/internal/gen/manifest"
)

func main() {
	flags := common.RegisterManifestFlags(flag.CommandLine, true)
	flag.Parse()

	if flags.Out == "" {
		fmt.Fprintln(os.Stderr, "spore-gen-go-server: --out is required")
		flag.Usage()
		os.Exit(2)
	}
	if flags.Package == "" {
		fmt.Fprintln(os.Stderr, "spore-gen-go-server: --package is required")
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

	files, err := goserver.Generate(schemas, callables, goserver.Options{
		Package:      flags.Package,
		Visibilities: visibilities,
		Header:       flags.Header,
	})
	if err != nil {
		fail(fmt.Errorf("generate: %w", err))
	}

	if err := common.WriteFiles(flags.Out, files); err != nil {
		fail(err)
	}
	fmt.Fprintf(os.Stderr, "spore-gen-go-server: wrote %d file(s) to %s\n", len(files), flags.Out)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "spore-gen-go-server:", err)
	os.Exit(1)
}
