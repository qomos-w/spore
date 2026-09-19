# spore

Spore is a script-first embedding layer for Go. Write behavior in a lightweight
statically-typed language, register Go functions and structs as host
capabilities, and invoke script callables from Go with structured type safety.

[简体中文](README.zh-CN.md)

> Status: early development. The `script` package embedding surface is the
> stable contract; `internal/` packages may change without notice.

```go
rt, _ := script.NewRuntime()
rt.BindFunc("math", "add", func(a, b int) int { return a + b })
rt.LoadSource("demo", `export fun main(): int { return add(1, 2) }`)
result, _ := rt.Call("main")
var n int
result.DecodeInto(&n) // 3
```

## Install

```bash
go get github.com/qomos-w/spore
```

Requires Go 1.27+.

## Quick Start

```go
package main

import (
    "fmt"
    "log"

    "github.com/qomos-w/spore/script"
)

func main() {
    rt, err := script.NewRuntime()
    if err != nil {
        log.Fatal(err)
    }

    // 1. Register host functions under a namespace
    rt.BindFunc("math", "add", func(a, b int) int { return a + b })
    rt.BindValue("config", "version", "1.0.0")

    // 2. Load script source
    err = rt.LoadSource("demo", `
        import { add, version } from "math"

        export fun greet(name: string): string {
            return "hello " + name + " (v" + version + ")"
        }
    `)
    if err != nil {
        log.Fatal(err)
    }

    // 3. Invoke
    result, err := rt.Call("greet", "world")
    if err != nil {
        log.Fatal(err)
    }
    if err := result.Unwrap(); err != nil {
        log.Fatal(err)
    }

    var s string
    if err := result.DecodeInto(&s); err != nil {
        log.Fatal(err)
    }
    fmt.Println(s) // hello world (v1.0.0)
}
```

## Script Language

sporescript is a statically typed language that compiles to stack-based
bytecode. Key features:

- **Types**: `int`, `long`, `float`, `double`, `bool`, `string`, `bytes`,
  arrays, ordered maps, structs, classes, interfaces
- **Functions**: `fun`, `export fun`, `stream fun` (generator-style streaming)
- **Control flow**: `if`/`else`, `when`/`case` (value/type match with guards),
  `for in`, `while`, `try`/`catch`
- **Closures**: lambda expressions with by-reference capture and compile-time
  function-type checking
- **Modules**: `import`/`from` with aliases and named lists, `export`
  (including compile-time `export struct`/`export type`)

See [SYNTAX.md](SYNTAX.md) for the full language reference.

## Type Mapping

| Script type | Go type | Notes |
|---|---|---|
| `int` | `int32` | 32-bit signed |
| `long` | `int64` | 64-bit signed |
| `uint` | `uint32` | 32-bit unsigned |
| `ulong` | `uint64` | 64-bit unsigned |
| `float` | `float32` | 32-bit IEEE-754 |
| `double` | `float64` | 64-bit IEEE-754 |
| `bool` | `bool` | |
| `string` | `string` | |
| `bytes` | `[]byte` | |
| `T[]` | `[]T` | slice |
| `map<K,V>` | `map[K]V` | ordered map (string keys only for K) |
| `struct` | Go struct | Pass-by-value deep copy |

**Narrowing conversions are checked at boundaries.** Passing a Go `int64`
value that exceeds `int32` range to a script `int` parameter produces a
runtime error rather than silent truncation.

## Schema and Code Generation

Schemas are the source of truth for cross-language contracts. The `cmd/`
generators turn schema manifests into typed code for both sides:

| Tool | Output |
|---|---|
| `spore-gen-go-types` | Go type definitions from schema manifests |
| `spore-gen-go-server` | Go server stubs from schema manifests |
| `spore-gen-ts` | TypeScript types from schema manifests |
| `spore-gen-ts-client` | TypeScript client classes |

For in-process generation (no subprocess), the public `gen/render` package
re-exports all four generators behind one entry point each — `Generate`,
`GenerateTSClient`, `GenerateGoServer`, `RenderGoTypes`/`RenderGoTypesRegistry`.
The CLIs above are thin wrappers over that same façade, so the four generators
present one symmetric public surface to embedders.

## Project Layout

| Path | Contents |
|---|---|
| `script/` | Public host embedding API (`Runtime`, `Result`, `Bind*`) |
| `schema/` | Canonical schema descriptors (`TypeDesc`, `ObjectDesc`, `CallableDesc`) |
| `binding/` | Host binding layer (capabilities, invocation, data projection) |
| `transport/` | Schema-aware JSON codec and wire envelope |
| `std/` | Standard library modules (`math`, `strings`, `json`, `time`, ...) |
| `ts/` | TypeScript runtime mirror |
| `gen/render/` | Public code-generation façade (four generators) |
| `benchmarks/` | Comparative benchmarks against goja / lua / tengo |
| `internal/script/` | Compiler, VM, and frontend (internal; not imported by hosts) |

## Documentation

| Document | Purpose |
|---|---|
| [SYNTAX.md](SYNTAX.md) | Script language reference: types, grammar, semantics |

## Stability

The public surface (`script.Runtime` and its methods) is the stable embedding
contract. `internal/` packages may change without notice. The TypeScript
mirror follows the Go side's schema evolution.

## License

[MIT](LICENSE)
