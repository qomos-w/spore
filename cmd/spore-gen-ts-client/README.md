# spore-gen-ts-client

Reads a JSON manifest of spore callable descriptors and writes a directory
of TypeScript client classes that wrap the `@qomos/spore-ts/client`
transport seam. Designed to run alongside `spore-gen-ts` against the same
manifest — `spore-gen-ts` produces the schema interfaces and the
runtime-side `CallableRegistry`, this CLI produces the typed
`<NS>Client` classes that consumers actually call.

## Install

```bash
go install github.com/qomos-w/spore/cmd/spore-gen-ts-client@latest
```

Or build locally from the spore repo:

```bash
go build -o ./bin/spore-gen-ts-client ./cmd/spore-gen-ts-client
```

## Usage

Same flag surface as `spore-gen-ts`:

```bash
spore-gen-ts-client \
    --in   ./schemas.json \
    --out  ./client/src/generated \
    --visibility public
```

| Flag | Default | Meaning |
|---|---|---|
| `--in` | `-` (stdin) | Path to the manifest JSON file. `-` reads from stdin. |
| `--out` | _(required)_ | Output directory. Created if it does not exist. |
| `--visibility` | `public` | Comma-separated set of visibilities to emit. Valid values: `internal`, `public`, `admin`, `diagnostic`. |
| `--header` | `// AUTO-GENERATED — DO NOT EDIT` | Comment prepended to every generated file. |

The manifest format is identical to `spore-gen-ts`'s; see
[../spore-gen-ts/README.md](../spore-gen-ts/README.md) for the full
schema. Both CLIs accept the legacy bare-array form (schemas only) and the
combined `{schemas, callables}` object form. The schemas section is parsed
but ignored — only the callables list drives client generation.

The intended workflow is to run both CLIs against the same manifest and
write into the same output directory:

```bash
go run ./gen | tee schemas.json
spore-gen-ts        --in schemas.json --out ./client/src/generated
spore-gen-ts-client --in schemas.json --out ./client/src/generated
```

## Output layout

For every namespace whose callables survive the visibility filter, exactly
one file is written:

```
<out>/
  <namespace>/
    client.ts    # exports `class <NS>Client`
```

Example output for the `auth` namespace (mixed unary + streaming
callables):

```typescript
// AUTO-GENERATED — DO NOT EDIT

import type { ClientTransport, StreamCall } from "@qomos/spore-ts/client";
import type { LoginEvent, LookupUserReq, LookupUserResp, TailLoginsFinal, TailLoginsReq } from "./types.js";

export class AuthClient {
  constructor(private readonly transport: ClientTransport) {}

  lookup_user(req: LookupUserReq): Promise<LookupUserResp> {
    return this.transport.unary<LookupUserReq, LookupUserResp>({
      namespace: "auth",
      name: "lookup_user",
      reqSchemaId: 4,
      finalSchemaId: 5,
    }, req).final();
  }

  tail_logins(req: TailLoginsReq): StreamCall<LoginEvent, TailLoginsFinal> {
    return this.transport.stream<TailLoginsReq, LoginEvent, TailLoginsFinal>({
      namespace: "auth",
      name: "tail_logins",
      reqSchemaId: 1,
      chunkSchemaId: 2,
      finalSchemaId: 3,
    }, req);
  }
}
```

Convention: the generated `client.ts` imports request / response types from
`./types.js` — i.e. it assumes every callable's `req` / `chunk` /
`final` struct has a matching schema entry **in the same namespace**.
Cross-namespace type imports are out of scope for the MVP. If your manifest
already groups callables and their I/O struct schemas by namespace, this
falls out naturally.

## Behavior details

- **Method names** preserve the on-wire callable name. `tail_logins` stays
  `tail_logins`; no camelCase transformation. This keeps the TypeScript
  surface symmetric with the Go side and simplifies cross-cutting work
  like network inspection or log search.
- **Class names** uppercase the first letter of the namespace and append
  `Client`: `auth` → `AuthClient`, `signals` → `SignalsClient`.
- **`void` request types** drop the `req` parameter entirely:

  ```typescript
  next_invoice_id(): Promise<string> {
    return this.transport.unary<void, string>({...}, undefined).final();
  }
  ```

  No method signature ever ends up with the awkward `req: void` parameter.
- **`StreamCall`** is imported only when the namespace has at least one
  streaming callable, avoiding an unused-import warning for unary-only
  namespaces.
- **Visibility filtering** is identical to `spore-gen-ts`: callables
  whose `visibility` is not in `--visibility` are silently dropped. A
  namespace whose callables are all filtered out produces zero files (no
  empty `client.ts` is written).

## Calling the package directly

If you would rather skip the JSON manifest, you can import the gen package
from any program **inside** the spore module (it lives at
`internal/gen/ts-client`, so external imports are not allowed):

```go
import (
    tsclient "github.com/qomos-w/spore/internal/gen/ts-client"
    "github.com/qomos-w/spore/internal/gen/ts"
    "github.com/qomos-w/spore/schema"
)

files, err := tsclient.Generate(
    []ts.NamedCallableDesc{
        {
            Namespace:     "auth",
            Name:          "lookup_user",
            Visibility:    ts.VisibilityPublic,
            Mode:          schema.CallableModeUnary,
            ReqSchemaID:   1,
            FinalSchemaID: 2,
            Req:           reqType,
            Final:         respType,
        },
    },
    ts.Options{
        Visibilities: []ts.Visibility{ts.VisibilityPublic},
        Header:       "// AUTO-GENERATED — DO NOT EDIT",
    },
)
```

`Generate` returns `map[relPath]content` (e.g. `"auth/client.ts" -> "..."`);
the CLI is a thin wrapper that writes each entry to disk.

## Verification

```bash
go test ./internal/gen/ts-client/...   # unit + golden tests
go build ./cmd/spore-gen-ts-client   # confirm the CLI builds
```

Golden fixtures live in `internal/gen/ts-client/testdata/golden/`. Pass
`-update` to the test command to refresh them after intentional output
changes.
