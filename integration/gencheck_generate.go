//go:build ignore

package main

import (
	"os"
	"path/filepath"

	gotypes "github.com/qomos-w/spore/internal/gen/go-types"
	"github.com/qomos-w/spore/script"
)

func main() {
	src := `
@component
@schema(300)
struct Position {
  x: float
}

@component
@schema(301)
struct Velocity {
  dx: float
}

@schema(302)
struct PlainMsg {
  note: string
}

@schema(303)
struct Avatar {
  photo: media
  gallery: array<media>
}
`
	objs, err := script.ParseObjects(src)
	if err != nil {
		panic(err)
	}

	dir := os.Args[1]
	out, err := gotypes.Render(objs, gotypes.Options{Package: "gencheck", EmitComponents: true})
	if err != nil {
		panic(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "components.gen.go"), out, 0o644); err != nil {
		panic(err)
	}

	var entries []gotypes.RegistryEntry
	for _, o := range objs {
		entries = append(entries, gotypes.RegistryEntry{ID: o.SchemaID, Name: o.Name, SourceFile: "game.spore", IsComponent: o.IsComponent})
	}
	reg, err := gotypes.RenderRegistry(entries, gotypes.Options{Package: "gencheck", EmitComponents: true})
	if err != nil {
		panic(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "registry.gen.go"), reg, 0o644); err != nil {
		panic(err)
	}
}
