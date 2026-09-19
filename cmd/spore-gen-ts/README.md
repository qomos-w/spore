# spore-gen-ts

Reads a JSON manifest of spore schema and callable descriptors and writes a
directory of TypeScript types, registry indexes, and callable manifests.
Intended to be run as a build step in projects that consume `@qomos/spore-ts`
from the browser or Node side.

## Install

```bash
go install github.com/qomos-w/spore/cmd/spore-gen-ts@latest
```

Or build locally from the spore repo:

```bash
go build -o ./bin/spore-gen-ts ./cmd/spore-gen-ts
```

## Usage

```bash
spore-gen-ts \
    --in   ./schemas.json \
    --out  ./client/src/generated \
    --visibility public,admin
```

### Flags

| Flag | Default | Meaning |
|---|---|---|
| `--in` | `-` (stdin) | Path to the manifest JSON file. `-` reads from stdin. |
| `--out` | _(required)_ | Output directory. Created if it does not exist. |
| `--visibility` | `public` | Comma-separated set of visibilities to emit. Valid values: `internal`, `public`, `admin`, `diagnostic`. |
| `--header` | `// AUTO-GENERATED — DO NOT EDIT` | Comment prepended to every generated file. |

Entries whose `visibility` is **not** in the `--visibility` set are silently
dropped — the same input manifest can produce different output bundles for
different audiences (e.g. one bundle for public clients, another for an
internal admin console).

## Manifest format

### Vocabulary / scope note

This generator emits two kinds of artifacts:

- **schema artifacts** — TypeScript interfaces + the `SchemaRegistry`-ready
  `schemaEntries` array, derived from `schema.ObjectDesc`.
- **callable artifacts** — runtime `callableEntries` consumed by
  `CallableRegistry`, carrying invocation metadata (mode, schema ids, request
  / chunk / final type references).

Higher-level invocation surfaces — typed client classes, host runtime
invokers, transport adapters — are produced by sibling generators
([`spore-gen-ts-client`](../spore-gen-ts-client/README.md) for the
client classes) that consume the same manifest. This CLI's job is to lay
down the registry-grade source of truth.

Key terms in this README:
- manifest entry = one schema or callable descriptor
- `schemas` array = list of `manifestSchemaEntry` (formerly the bare-array form)
- `callables` array = list of `manifestCallableEntry`
- `object` = serialized `schema.ObjectDesc`
- `kind: "struct"` = current wire/type vocabulary for export-safe data shapes
- `mode: "unary" | "streaming"` = matches `schema.CallableMode`

### Two accepted forms

**Legacy bare-array** (schemas only) — still accepted unchanged:

```json
[
  {
    "namespace":  "auth",
    "schemaId":   1,
    "name":       "LoginReq",
    "visibility": "public",
    "object":     { "kind": "struct", "name": "LoginReq", "fields": [
      { "name": "User", "type": { "kind": "scalar", "name": "string" } },
      { "name": "Pass", "type": { "kind": "scalar", "name": "string" } }
    ]}
  }
]
```

**Combined object form** (schemas + callables in one file):

```json
{
  "schemas": [
    {
      "namespace":  "auth",
      "schemaId":   1,
      "name":       "TailLoginsReq",
      "visibility": "public",
      "object":     { "kind": "struct", "name": "TailLoginsReq", "fields": [
        { "name": "Limit", "type": { "kind": "scalar", "name": "int" } }
      ]}
    },
    {
      "namespace":  "auth",
      "schemaId":   2,
      "name":       "LoginEvent",
      "visibility": "public",
      "object":     { "kind": "struct", "name": "LoginEvent", "fields": [
        { "name": "User", "type": { "kind": "scalar", "name": "string" } },
        { "name": "At",   "type": { "kind": "scalar", "name": "string" } }
      ]}
    },
    {
      "namespace":  "auth",
      "schemaId":   3,
      "name":       "TailLoginsFinal",
      "visibility": "public",
      "object":     { "kind": "struct", "name": "TailLoginsFinal", "fields": [
        { "name": "Total", "type": { "kind": "scalar", "name": "int" } }
      ]}
    }
  ],
  "callables": [
    {
      "namespace":     "auth",
      "name":          "tail_logins",
      "visibility":    "public",
      "mode":          "streaming",
      "reqSchemaId":   1,
      "chunkSchemaId": 2,
      "finalSchemaId": 3,
      "req":   { "kind": "struct", "name": "TailLoginsReq",   "className": "TailLoginsReq"   },
      "chunk": { "kind": "struct", "name": "LoginEvent",      "className": "LoginEvent"      },
      "final": { "kind": "struct", "name": "TailLoginsFinal", "className": "TailLoginsFinal" }
    }
  ]
}
```

The first non-whitespace byte selects the form: `[` → legacy, `{` → combined.

The `object` field on a schema entry is `schema.ObjectDesc` serialized as
JSON. The `req` / `chunk` / `final` fields on a callable entry are
`schema.TypeDesc` serialized as JSON. The constraint on
`(namespace, schemaId)` for schemas — every pair must be unique within the
manifest — applies as before. Callables additionally enforce
`(namespace, name)` uniqueness and require `mode: "streaming"` callables to
populate `chunkSchemaId` + `chunk`.

## Producing the manifest

You write a small Go program in your own repository that imports your schema
types and dumps the descriptors. spore does **not** ship a generic dump
tool — your program decides which types to register.

```go
// gen/main.go
package main

import (
    "encoding/json"
    "os"

    "github.com/qomos-w/spore/schema"

    "myproject/auth"
)

type entry struct {
    Namespace  string            `json:"namespace"`
    SchemaID   uint32            `json:"schemaId"`
    Name       string            `json:"name"`
    Visibility string            `json:"visibility"`
    Object     schema.ObjectDesc `json:"object"`
}

func main() {
    entries := []entry{
        {"auth", 1, "LoginReq", "public",
            mustObject(schema.DescribeGoStruct(auth.LoginReq{}))},
        {"auth", 2, "LoginResp", "public",
            mustObject(schema.DescribeGoStruct(auth.LoginResp{}))},
    }
    if err := json.NewEncoder(os.Stdout).Encode(entries); err != nil {
        panic(err)
    }
}

func mustObject(o schema.ObjectDesc, err error) schema.ObjectDesc {
    if err != nil {
        panic(err)
    }
    return o
}
```

Pipe the output straight into the CLI:

```bash
go run ./gen | spore-gen-ts --in - --out ./client/src/generated
```

Or write the manifest to disk first if you want to commit it / inspect it:

```bash
go run ./gen > schemas.json
spore-gen-ts --in schemas.json --out ./client/src/generated
```

## Output layout

For every namespace in the (filtered) manifest the tool writes one or more
of these files. `types.ts` and `registry.ts` only appear when the namespace
contributes schema entries; `callables.ts` only appears when the namespace
contributes callable entries; `index.ts` always appears for any namespace
that participates and re-exports whichever siblings were produced.

```
<out>/
  <namespace>/
    types.ts       # one `export interface X` per entry, alpha sorted by name
    registry.ts    # SchemaIDs + runtime-ready SchemaEntry exports
    callables.ts   # callableEntries + buildCallableRegistry helper
    index.ts       # barrel: `export * from "./types.js";` etc.
```

`types.ts` example:

```typescript
// AUTO-GENERATED — DO NOT EDIT
export interface LoginReq {
  Pass: string;
  User: string;
}

export interface LoginResp {
  Token: string;
}
```

`registry.ts` example:

```typescript
// AUTO-GENERATED — DO NOT EDIT
import { SchemaRegistry, type SchemaEntry } from "@qomos/spore-ts/registry";

export const SchemaIDs = {
  LoginReq: 1,
  LoginResp: 2,
} as const;

export const schemaEntries: SchemaEntry[] = [
  {
    namespace: "auth",
    schemaId: 1,
    name: "LoginReq",
    visibility: "public",
    type: { "kind": "struct", "name": "LoginReq", "className": "LoginReq" },
    object: {
      "kind": "struct",
      "name": "LoginReq",
      "fields": [
        { "name": "User", "type": { "kind": "scalar", "name": "string" } },
        { "name": "Pass", "type": { "kind": "scalar", "name": "string" } }
      ]
    }
  },
];

export function buildSchemaRegistry(): SchemaRegistry {
  const registry = new SchemaRegistry();
  for (const entry of schemaEntries) registry.register(entry);
  return registry;
}
```

`callables.ts` example:

```typescript
// AUTO-GENERATED — DO NOT EDIT
import { CallableRegistry, type CallableEntry } from "@qomos/spore-ts/callables";

export const callableEntries: CallableEntry[] = [
  {
    namespace: "auth",
    name: "tail_logins",
    visibility: "public",
    mode: "streaming",
    reqSchemaId: 1,
    chunkSchemaId: 2,
    finalSchemaId: 3,
    req:   { kind: "struct", name: "TailLoginsReq",   className: "TailLoginsReq"   },
    chunk: { kind: "struct", name: "LoginEvent",      className: "LoginEvent"      },
    final: { kind: "struct", name: "TailLoginsFinal", className: "TailLoginsFinal" }
  },
];

export function buildCallableRegistry(): CallableRegistry {
  const registry = new CallableRegistry();
  for (const entry of callableEntries) registry.register(entry);
  return registry;
}
```

These helpers can be used directly with `@qomos/spore-ts`:

```typescript
import { JSONCodec } from "@qomos/spore-ts";
import { buildSchemaRegistry }   from "./generated/auth/registry.js";
import { buildCallableRegistry } from "./generated/auth/callables.js";

const schemas   = buildSchemaRegistry();
const callables = buildCallableRegistry();
const codec     = new JSONCodec();
const objectFor = schemas.objectFor("auth");
```

## Type mapping

| Go / schema | TypeScript |
|---|---|
| `bool` | `boolean` |
| `int`, `int8`…`int64`, `uint`, `uint8`…`uint64`, `float32`, `float64` | `number` |
| `string` | `string` |
| `bytes` | `Uint8Array` |
| `[]T` | `T[]` |
| `map[K]V` | `Record<string, V>` |
| `struct` reference | the corresponding `interface` name |
| field with `private: true` | _(omitted from output)_ |

The codegen MVP focuses on struct + scalars + array + map data shapes plus
unary / streaming callable metadata. Higher-level surfaces (typed client
classes, host runtime invokers, transport adapters) are produced by sibling
generators that consume the same manifest. The companion
[`spore-gen-ts-client`](../spore-gen-ts-client/README.md) emits
`<namespace>/client.ts` files holding `<NS>Client` classes that wrap the
`@qomos/spore-ts/client` transport seam.

## Calling the package directly

If you would rather skip the JSON manifest, you can import the gen package
from any program **inside** the spore module (it lives at
`internal/gen/ts`, so external imports are not allowed):

```go
import (
    gents "github.com/qomos-w/spore/internal/gen/ts"
    "github.com/qomos-w/spore/schema"
)

files, err := gents.Generate(
    []gents.NamedObjectDesc{
        {
            Namespace:  "auth",
            SchemaID:   1,
            Name:       "LoginReq",
            Object:     loginReqObject,
            Visibility: gents.VisibilityPublic,
        },
    },
    []gents.NamedCallableDesc{
        {
            Namespace:     "auth",
            Name:          "lookup_user",
            Visibility:    gents.VisibilityPublic,
            Mode:          schema.CallableModeUnary,
            ReqSchemaID:   1,
            FinalSchemaID: 2,
            Req:           loginReqType,
            Final:         loginRespType,
        },
    },
    gents.Options{
        Visibilities: []gents.Visibility{gents.VisibilityPublic},
        Header:       "// AUTO-GENERATED — DO NOT EDIT",
    },
)
```

`Generate` returns `map[relPath]content`; the CLI is a thin wrapper that
writes each entry to disk.

## Verification

```bash
go test ./internal/gen/ts/...   # unit + golden tests
go build ./cmd/spore-gen-ts   # confirm the CLI builds
```

Golden fixtures live in `internal/gen/ts/testdata/golden/`. Pass `-update`
to the test command to refresh them after intentional output changes.
