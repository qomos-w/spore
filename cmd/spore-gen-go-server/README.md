# spore-gen-go-server

Reads a JSON manifest of spore callable descriptors and writes Go server-side
dispatcher source files. It is the Go-flavored sibling of
[`spore-gen-ts-client`](../spore-gen-ts-client/README.md): both consume the same
`{schemas, callables}` manifest, but emit code targeting different sides of the
wire.

## Install

```bash
go install github.com/qomos-w/spore/cmd/spore-gen-go-server@latest
```

Or build locally from the spore repo:

```bash
go build -o ./bin/spore-gen-go-server ./cmd/spore-gen-go-server
```

## Usage

```bash
spore-gen-go-server \
    --in       ./manifest.json \
    --out      ./server \
    --package  exp09 \
    --visibility public
```

| Flag | Default | Meaning |
|---|---|---|
| `--in` | `-` (stdin) | Path to the manifest JSON file. `-` reads from stdin. |
| `--out` | _(required)_ | Output directory. Created if it does not exist. |
| `--package` | _(required)_ | Go package declaration written into every generated file. |
| `--visibility` | `public` | Comma-separated set of visibilities to emit. Valid values: `internal`, `public`, `admin`, `diagnostic`. |
| `--header` | `// AUTO-GENERATED — DO NOT EDIT` | Comment prepended to every generated file. |

The manifest format is the same one [`spore-gen-ts`](../spore-gen-ts/README.md)
accepts; see that README for the full schema.

## Output layout

One `<namespace>_dispatcher_gen.go` file is emitted per namespace surviving the
visibility filter:

```
<out>/
  <namespace>_dispatcher_gen.go
```

Each file declares an `<NS>Handler` interface (one method per callable) and a
top-level `Dispatch<NS>` function that translates a `(callID, payload
map[string]any)` pair into a typed handler invocation. User code implements the
interface with typed Go structs; the marshal between map and struct is generated.

MVP scope mirrors `internal/gen/go-server`: only struct request and struct final
types with scalar fields are supported; streaming callables and nested struct
fields fail loudly. `exp09` is the primary consumer today and stays inside the
supported subset.

## Calling the package directly

Embedders **outside** the spore module drive generation through the public
`gen/render` façade: `render.GenerateGoServer` re-exports this generator over
the same visibility/options contract, so no internal import is required.
Programs inside the spore module may still import `internal/gen/go-server`
directly.

```go
import "github.com/qomos-w/spore/gen/render"

files, err := render.GenerateGoServer(schemas, callables, render.GoServerOptions{
    Package:      "exp09",
    Visibilities: []render.Visibility{render.VisibilityPublic},
    Header:       "// AUTO-GENERATED — DO NOT EDIT",
})
```

`GenerateGoServer` returns `map[relPath]content`; the CLI is a thin wrapper that
writes each entry to disk.

## Verification

```bash
go test ./internal/gen/go-server/...   # unit + golden tests
go build ./cmd/spore-gen-go-server     # confirm the CLI builds
```

Golden fixtures live in `internal/gen/go-server/testdata/golden/`. Pass
`-update` to the test command to refresh them after intentional output changes.
