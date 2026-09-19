package common

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// This file holds the CLI glue shared by the manifest-driven generator
// commands (cmd/spore-gen-ts, cmd/spore-gen-ts-client,
// cmd/spore-gen-go-server). Those three commands had verbatim copies of the
// same flag block, stdin/file reader and output writer; keeping them here means
// the CLI surface stays identical across generators.

// ManifestFlags holds the flag values every manifest-driven generator CLI
// accepts. The zero value is not meaningful — use RegisterManifestFlags.
type ManifestFlags struct {
	In         string // manifest path; "-" reads stdin
	Out        string // output directory (required)
	Package    string // Go package declaration (only when withPackage)
	Visibility string // comma-separated visibilities to include
	Header     string // comment header prepended to every generated file
}

// RegisterManifestFlags binds the shared manifest-CLI flags onto fs and returns
// the value holder. withPackage additionally registers --package for the
// Go-targeted generators. Call before fs.Parse.
func RegisterManifestFlags(fs *flag.FlagSet, withPackage bool) *ManifestFlags {
	f := &ManifestFlags{}
	fs.StringVar(&f.In, "in", "-", `manifest path; "-" reads stdin`)
	fs.StringVar(&f.Out, "out", "", "output directory (required)")
	if withPackage {
		fs.StringVar(&f.Package, "package", "", "Go package declaration written into every generated file (required)")
	}
	fs.StringVar(&f.Visibility, "visibility", "public", "comma-separated visibilities to include: internal,public,admin,diagnostic")
	fs.StringVar(&f.Header, "header", "// AUTO-GENERATED — DO NOT EDIT", "comment header prepended to every generated file")
	return f
}

// ReadInput reads a manifest from path; "-" reads stdin.
func ReadInput(path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(path)
}

// WriteFiles writes every relative-path → content pair under outDir, creating
// parent directories as needed. Iteration stops at the first error and the
// partially written tree is left in place, matching the CLIs' previous
// fail-fast behaviour.
func WriteFiles(outDir string, files map[string]string) error {
	for relPath, content := range files {
		full := filepath.Join(outDir, relPath)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", full, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", full, err)
		}
	}
	return nil
}
