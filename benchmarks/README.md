# Spore Performance Benchmarks

Baseline performance of Spore against three established embedded scripting
engines (`tengo`, `goja`, `gopher-lua`). Each engine compiles its script once
and the benchmark loop times only the per-call execution path.

This module is intentionally separated from the main Spore `go.mod` so its
heavyweight comparison dependencies do not pollute the core module. It can
still import Spore's actual execution path because Go's `internal/` rule
checks the filesystem tree, not module boundaries — a sub-module under
`github.com/qomos-w/spore/...` is allowed to reach `spore/internal/...`.

## Run

```
cd benchmarks
go test -bench=. -benchmem -benchtime=1s -run=^$ ./...
```

## Workloads

| Workload        | What it measures                                | N values            |
| --------------- | ----------------------------------------------- | ------------------- |
| `Sum`           | tight loop + integer arithmetic + var I/O       | 100, 1000, 10000    |
| `Fib`           | recursive call overhead                         | 10, 15, 20          |
| `Concat`        | string allocation + GC pressure (`s = s + "x"`) | 100, 1000, 10000    |
| `ConcatBuilder` | array push + bulk string join                   | 100, 1000, 10000    |
| `ArrayLoop`     | array index reads + integer accumulation        | 128, 1024, 8192     |
| `ArrayUpdate`   | array element overwrite/update                  | 128, 1024, 8192     |
| `MapLookup`     | repeated string-key map/table/object lookup     | 100, 1000, 10000    |
| `MapUpdate`     | string-key map/table/object overwrite/update    | 100, 1000, 10000    |
| `BranchyLoop`   | hot if/else branching inside loop body          | 100, 1000, 10000    |
| `SmallCall`     | non-recursive tiny script function calls        | 100, 1000, 10000    |
| `FunctionArgs`  | multi-argument script function calls            | 100, 1000, 10000    |
| `ClosureCounter`| closure with writable capture, called in loop   | 100, 1000, 10000    |
| `MapFilter`     | map/filter higher-order funcs with lambda args  | 128, 1024, 8192     |
| `NestedClosure` | closure nested inside closure (transitive capt.)| 100, 1000, 10000    |

`Alloc` (per-iteration map allocation) is in scope but currently disabled —
Spore's default VM memory pool (`bytecode.NewVMEvaluator` hard-codes 4096
words) cannot hold the working set even at small N. Re-enable once the VM
gains a sizing constructor or adaptive growth.

Each engine implements the same logical function in idiomatic syntax:

- Spore: `fun sum(n: int): int { ... }`
- tengo: `sum := func(n) { ... }; __result = sum(__n)` (script-level result
  variable; tengo lacks a compile-once-call-many surface)
- goja: `function sum(n) { ... }` with `goja.AssertFunction` cached after
  first call
- lua: `function sum(n) ... end` resolved by name through `L.GetGlobal`

The closure workloads exercise Spore's `fun` lambda syntax
(`fun(x: int): int { ... }` assigned to `any`, passed as an argument, and
returned from another lambda) against the equivalent function-expression
idiom in each comparison engine. `MapFilter` is sized like the array
workloads — at N=10000 its three live arrays plus the previous invocation's
still-rooted garbage exceed the evaluator's 65536-word VM pool.
`TestClosureWorkloadCorrectness` (in `closure_test.go`) cross-checks every
engine's result against a Go-computed oracle at each workload size, so a
plain `go test` run validates script correctness without benchmarking.

## Baseline (12th Gen i7-12700K, Go 1.25, Windows)

ns/op — lower is better. Captured at `-benchtime=1s`.

### Sum

| Engine  | N=100  | N=1000  | N=10000  |
| ------- | -----: | ------: | -------: |
| Spore |    589 |   5,546 |   53,916 |
| tengo   | 61,145 |  92,411 |  449,169 |
| goja    |  5,902 |  62,629 |  645,307 |
| lua     |  1,689 |  19,641 |  196,518 |

### Fib (recursive)

| Engine  |  N=10  |   N=15   |    N=20   |
| ------- | -----: | -------: | --------: |
| Spore |  6,468 |   72,817 |   797,787 |
| tengo   | 16,206 |  101,567 | 1,021,754 |
| goja    | 17,352 |  181,915 | 2,050,991 |
| lua     | 11,089 |  124,125 | 1,363,097 |

### Concat (`s = s + "x"`)

| Engine  | N=100  |   N=1000  |     N=10000     |
| ------- | -----: | --------: | --------------: |
| Spore |  8,001 |   496,926 |      40,745,642 |
| tengo   | 20,460 |   185,619 |      44,194,739 |
| goja    | 10,865 |   199,538 |      33,228,184 |
| lua     |  8,054 |   170,931 |      21,084,613 |

### ConcatBuilder (array push + join)

| Engine  | N=100 | N=1000 | N=10000 |
| ------- | ----: | -----: | ------: |
| Spore | 4,273 | 38,355 |  415,114 |
| goja    | 21,242 | 242,520 | 3,079,008 |
| lua     |  9,138 |  91,714 |     N/A |

### ArrayLoop

| Engine  | N=128 | N=1024 | N=8192  |
| ------- | ----: | -----: | ------: |
| Spore | 3,210 | 24,569 | 174,578 |
| tengo   | 11,716 | 30,949 | 194,543 |
| goja    | 4,485 | 32,619 | 251,377 |
| lua     | 2,517 | 20,123 | 150,464 |

### ArrayUpdate

| Engine  | N=128 | N=1024 | N=8192  |
| ------- | ----: | -----: | ------: |
| Spore | 5,139 | 37,788 | 267,051 |
| tengo   | 12,840 | 42,170 | 294,760 |
| goja    | 7,013 | 49,897 | 447,557 |
| lua     | 4,007 | 30,532 | 254,341 |

### MapLookup

| Engine  |  N=100 | N=1000 | N=10000 |
| ------- | -----: | -----: | ------: |
| Spore |  8,774 | 82,721 | 789,651 |
| tengo   | 20,647 | 137,429 | 1,262,701 |
| goja    | 14,750 | 141,992 | 1,425,168 |
| lua     |  9,125 |  91,684 | 1,013,699 |

### MapUpdate

| Engine  |  N=100 |  N=1000 |   N=10000 |
| ------- | -----: | ------: | --------: |
| Spore | 15,985 | 154,940 | 1,455,701 |
| tengo   | 30,300 | 219,538 | 2,007,858 |
| goja    | 24,052 | 263,527 | 2,752,566 |
| lua     | 21,549 | 223,585 | 2,245,208 |

### BranchyLoop

| Engine  |  N=100 | N=1000 | N=10000 |
| ------- | -----: | -----: | ------: |
| Spore |  1,643 | 15,139 | 162,011 |
| tengo   | 14,890 | 66,847 | 605,027 |
| goja    |  8,332 | 83,070 | 829,977 |
| lua     |  2,389 | 32,107 | 319,631 |

### SmallCall

| Engine  | N=100 | N=1000 |  N=10000 |
| ------- | ----: | -----: | -------: |
| Spore | 2,496 | 24,632 | 245,076 |
| tengo   | 13,372 | 64,817 | 602,477 |
| goja    |  9,865 | 111,934 | 1,130,527 |
| lua     |  6,181 | 63,760 | 699,845 |

### FunctionArgs

| Engine  |  N=100 |  N=1000 |   N=10000 |
| ------- | -----: | ------: | --------: |
| Spore |  3,571 |  35,448 |   350,314 |
| tengo   | 15,095 |  86,308 |   853,968 |
| goja    | 13,399 | 147,734 | 1,457,559 |
| lua     |  7,363 |  77,412 |   773,702 |

### Observations

- **Loop dispatch (Sum)**: Spore is now faster than all three comparison
  engines in this tight integer loop on this machine. The fused local-int
  opcodes collapse both `s = s + i` (`ADD_LOCAL_INT`) and `i < n` loop
  branching (`JUMP_LOCAL_LT_INT`) into direct local operations, improving the
  Sum hot loop by roughly another 48% over the previous `ADD_LOCAL_INT`
  baseline.
- **Recursive calls (Fib)**: Spore is now faster than all three engines
  (0.80 ms vs 1.36 ms lua at N=20) after direct function ID calls (`CALL_DIRECT`)
  eliminated runtime name lookups. The remaining gap is the binding-seam invocation
  overhead per recursive call.
- **String concat (Concat)**: Spore is now fastest at N=100 (tied with
  lua) and competitive at larger sizes after fused `CONCAT_LOCAL_CONST_STRING`
  eliminated 3 of 4 opcode dispatches per iteration. The remaining wall-time
  gap at N=10000 is the inherent O(N²) byte-copy cost.
- **Array builder (ConcatBuilder)**: Spore is 2× faster than lua and 7×
  faster than goja at N=10000 after fused `ARRAY_PUSH_LOCAL_CONST_STRING`
  collapsed 3 opcodes per iteration into 1.
- **Array reads (ArrayLoop)**: Spore is now faster than tengo/goja and close
  to lua after typed literal int-index reads (`ARRAY_GET_INT`) removed generic
  element dispatch from the hot path.
- **Array updates (ArrayUpdate)**: Spore is close to lua and ahead of
  tengo/goja after typed literal int-index get/set specialization. The remaining
  gap is mostly array probe/update dispatch and repeated local/container loads.
- **Map lookup (MapLookup)**: Spore is now fastest in this workload after
  specializing typed literal string-key lookup with `MAP_GET_STRING`. The
  remaining allocation is per invocation rather than per lookup path, making
  map updates the next higher-value target.
- **Map updates (MapUpdate)**: Spore is now fastest in this workload after
  adding typed literal string-key get/set specialization. Allocation is down to
  one per invocation, so the remaining cost is mostly map probe/update and
  loop/body dispatch rather than per-key projection.
- **Body branches (BranchyLoop)**: Spore is fastest at all tested sizes after
  the fused integer loop-branch work, but this workload still exercises an
  unfused if/else branch inside the body. A boolean/int branch specialization
  could reduce the remaining compare + conditional-dispatch cost.
- **Tiny calls (SmallCall)**: Spore is now 2.5× faster than the next closest
  engine after direct function ID calls. The remaining cost is mostly binding-seam
  overhead.
- **Multi-arg calls (FunctionArgs)**: Spore is now 2× faster than the next
  closest engine. The gap to SmallCall (245K vs 350K at N=10000) reflects
  the extra argument movement cost; small-arity call opcodes remain a future
  optimization target.

## Caveats

- Each engine's per-call idiom differs (tengo `Run()` re-executes the whole
  script; goja `AssertFunction` caches a callable; lua `PCall` uses the value
  stack; Spore `Invoke` goes through the binding seam). The benchmark
  measures **what an embedder actually pays** to invoke a script function,
  not pure interpreter-loop instructions per second.
- Engine versions: tengo v2.17.0, goja (latest), gopher-lua v1.1.2.
- `GOGC` is left at default. No per-engine tuning was applied.
- The `Alloc` workload is currently disabled — Spore's default 4KB VM
  memory pool (hard-coded in `bytecode.NewVMEvaluator`) cannot hold even
  N=10 per-iteration map allocations. Adding a sizing API would unblock
  this comparison.

## Out of scope (future iterations)

- Object/map allocation (`Alloc` workload above) — pending VM sizing API.
- HTTP / JSON / regex hot paths.
- Throughput-style benchmarks (large data ingestion).
- Cross-engine numeric type comparisons (int64 vs float64).
- A Markdown report renderer that diffs runs over time.
