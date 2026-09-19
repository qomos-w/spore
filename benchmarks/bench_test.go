package benchmarks

import (
	"fmt"
	"testing"
)

// runner is the cross-engine handle used by the benchmark loop.
type runner interface {
	Run(name string, n int) (int64, error)
}

type workload struct {
	name  string            // pretty name used in benchmark labels
	fn    string            // function name inside each script
	sizes []int             // input N values to benchmark
	src   map[string]string // engine -> script source
}

const (
	engSpore = "spore"
	engTengo   = "tengo"
	engGoja    = "goja"
	engLua     = "lua"
)

var workloads = []workload{
	{
		name:  "Sum",
		fn:    "sum",
		sizes: []int{100, 1000, 10000},
		src: map[string]string{
			engSpore: `fun sum(n: int): int {
  var s: int = 0
  for (var i: int = 0; i < n; i = i + 1) { s = s + i }
  return s
}`,
			engTengo: `sum := func(n) {
  s := 0
  for i := 0; i < n; i++ { s += i }
  return s
}
__result = sum(__n)`,
			engGoja: `function sum(n) {
  var s = 0;
  for (var i = 0; i < n; i++) { s += i; }
  return s;
}`,
			engLua: `function sum(n)
  local s = 0
  for i = 0, n - 1 do s = s + i end
  return s
end`,
		},
	},
	{
		name:  "Fib",
		fn:    "fib",
		sizes: []int{10, 15, 20},
		src: map[string]string{
			engSpore: `fun fib(n: int): int {
  if (n < 2) { return n }
  return fib(n - 1) + fib(n - 2)
}`,
			engTengo: `fib := func(n) {
  if n < 2 { return n }
  return fib(n - 1) + fib(n - 2)
}
__result = fib(__n)`,
			engGoja: `function fib(n) {
  if (n < 2) return n;
  return fib(n - 1) + fib(n - 2);
}`,
			engLua: `function fib(n)
  if n < 2 then return n end
  return fib(n - 1) + fib(n - 2)
end`,
		},
	},
	{
		name:  "Concat",
		fn:    "concat",
		sizes: []int{100, 1000, 10000},
		src: map[string]string{
			engSpore: `fun concat(n: int): int {
  var s: string = ""
  for (var i: int = 0; i < n; i = i + 1) { s = s + "x" }
  return len(s)
}`,
			engTengo: `concat := func(n) {
  s := ""
  for i := 0; i < n; i++ { s += "x" }
  return len(s)
}
__result = concat(__n)`,
			engGoja: `function concat(n) {
  var s = "";
  for (var i = 0; i < n; i++) { s += "x"; }
  return s.length;
}`,
			engLua: `function concat(n)
  local s = ""
  for i = 0, n - 1 do s = s .. "x" end
  return #s
end`,
		},
	},
	{
		name:  "ConcatBuilder",
		fn:    "concatBuilder",
		sizes: []int{100, 1000, 10000},
		src: map[string]string{
			engSpore: `import join from "strings"
fun concatBuilder(n: int): int {
  var parts: array<string> = []
  for (var i: int = 0; i < n; i = i + 1) { push(parts, "x") }
  return len(join(parts, ""))
}`,
			engTengo: `concatBuilder := func(n) {
  parts := []
  for i := 0; i < n; i++ { parts = append(parts, "x") }
  return len("".join(parts))
}
__result = concatBuilder(__n)`,
			engGoja: `function concatBuilder(n) {
  var parts = [];
  for (var i = 0; i < n; i++) { parts.push("x"); }
  return parts.join("").length;
}`,
			engLua: `function concatBuilder(n)
  local parts = {}
  for i = 0, n - 1 do parts[#parts + 1] = "x" end
  return #table.concat(parts, "")
end`,
		},
	},
	{
		name:  "ArrayLoop",
		fn:    "arrayLoop",
		sizes: []int{128, 1024, 8192},
		src: map[string]string{
			engSpore: `fun arrayLoop(n: int): int {
  var xs: array<int> = [1, 2, 3, 4, 5, 6, 7, 8]
  var s: int = 0
  for (var i: int = 0; i < n; i = i + 8) {
    s = s + xs[0] + xs[1] + xs[2] + xs[3] + xs[4] + xs[5] + xs[6] + xs[7]
  }
  return s
}`,
			engTengo: `arrayLoop := func(n) {
  xs := [1, 2, 3, 4, 5, 6, 7, 8]
  s := 0
  for i := 0; i < n; i += 8 {
    s += xs[0] + xs[1] + xs[2] + xs[3] + xs[4] + xs[5] + xs[6] + xs[7]
  }
  return s
}
__result = arrayLoop(__n)`,
			engGoja: `function arrayLoop(n) {
  var xs = [1, 2, 3, 4, 5, 6, 7, 8];
  var s = 0;
  for (var i = 0; i < n; i += 8) {
    s += xs[0] + xs[1] + xs[2] + xs[3] + xs[4] + xs[5] + xs[6] + xs[7];
  }
  return s;
}`,
			engLua: `function arrayLoop(n)
  local xs = {1, 2, 3, 4, 5, 6, 7, 8}
  local s = 0
  for i = 0, n - 1, 8 do
    s = s + xs[1] + xs[2] + xs[3] + xs[4] + xs[5] + xs[6] + xs[7] + xs[8]
  end
  return s
end`,
		},
	},
	{
		name:  "MapLookup",
		fn:    "mapLookup",
		sizes: []int{100, 1000, 10000},
		src: map[string]string{
			engSpore: `fun mapLookup(n: int): int {
  var m: map<string, int> = {"a": 1, "b": 2, "c": 3, "d": 4}
  var s: int = 0
  for (var i: int = 0; i < n; i = i + 1) {
    s = s + m["a"] + m["b"] + m["c"] + m["d"]
  }
  return s
}`,
			engTengo: `mapLookup := func(n) {
  m := {"a": 1, "b": 2, "c": 3, "d": 4}
  s := 0
  for i := 0; i < n; i++ {
    s += m["a"] + m["b"] + m["c"] + m["d"]
  }
  return s
}
__result = mapLookup(__n)`,
			engGoja: `function mapLookup(n) {
  var m = {"a": 1, "b": 2, "c": 3, "d": 4};
  var s = 0;
  for (var i = 0; i < n; i++) {
    s += m["a"] + m["b"] + m["c"] + m["d"];
  }
  return s;
}`,
			engLua: `function mapLookup(n)
  local m = {a = 1, b = 2, c = 3, d = 4}
  local s = 0
  for i = 0, n - 1 do
    s = s + m["a"] + m["b"] + m["c"] + m["d"]
  end
  return s
end`,
		},
	},
	{
		name:  "SmallCall",
		fn:    "smallCall",
		sizes: []int{100, 1000, 10000},
		src: map[string]string{
			engSpore: `fun inc(x: int): int { return x + 1 }
fun smallCall(n: int): int {
  var s: int = 0
  for (var i: int = 0; i < n; i = i + 1) { s = inc(s) }
  return s
}`,
			engTengo: `inc := func(x) { return x + 1 }
smallCall := func(n) {
  s := 0
  for i := 0; i < n; i++ { s = inc(s) }
  return s
}
__result = smallCall(__n)`,
			engGoja: `function inc(x) { return x + 1; }
function smallCall(n) {
  var s = 0;
  for (var i = 0; i < n; i++) { s = inc(s); }
  return s;
}`,
			engLua: `function inc(x)
  return x + 1
end
function smallCall(n)
  local s = 0
  for i = 0, n - 1 do s = inc(s) end
  return s
end`,
		},
	},
	{
		name:  "ArrayUpdate",
		fn:    "arrayUpdate",
		sizes: []int{128, 1024, 8192},
		src: map[string]string{
			engSpore: `fun arrayUpdate(n: int): int {
  var xs: array<int> = [0, 0, 0, 0, 0, 0, 0, 0]
  for (var i: int = 0; i < n; i = i + 8) {
    xs[0] = xs[0] + 1
    xs[1] = xs[1] + 1
    xs[2] = xs[2] + 1
    xs[3] = xs[3] + 1
    xs[4] = xs[4] + 1
    xs[5] = xs[5] + 1
    xs[6] = xs[6] + 1
    xs[7] = xs[7] + 1
  }
  return xs[0] + xs[1] + xs[2] + xs[3] + xs[4] + xs[5] + xs[6] + xs[7]
}`,
			engTengo: `arrayUpdate := func(n) {
  xs := [0, 0, 0, 0, 0, 0, 0, 0]
  for i := 0; i < n; i += 8 {
    xs[0] = xs[0] + 1
    xs[1] = xs[1] + 1
    xs[2] = xs[2] + 1
    xs[3] = xs[3] + 1
    xs[4] = xs[4] + 1
    xs[5] = xs[5] + 1
    xs[6] = xs[6] + 1
    xs[7] = xs[7] + 1
  }
  return xs[0] + xs[1] + xs[2] + xs[3] + xs[4] + xs[5] + xs[6] + xs[7]
}
__result = arrayUpdate(__n)`,
			engGoja: `function arrayUpdate(n) {
  var xs = [0, 0, 0, 0, 0, 0, 0, 0];
  for (var i = 0; i < n; i += 8) {
    xs[0] = xs[0] + 1;
    xs[1] = xs[1] + 1;
    xs[2] = xs[2] + 1;
    xs[3] = xs[3] + 1;
    xs[4] = xs[4] + 1;
    xs[5] = xs[5] + 1;
    xs[6] = xs[6] + 1;
    xs[7] = xs[7] + 1;
  }
  return xs[0] + xs[1] + xs[2] + xs[3] + xs[4] + xs[5] + xs[6] + xs[7];
}`,
			engLua: `function arrayUpdate(n)
  local xs = {0, 0, 0, 0, 0, 0, 0, 0}
  for i = 0, n - 1, 8 do
    xs[1] = xs[1] + 1
    xs[2] = xs[2] + 1
    xs[3] = xs[3] + 1
    xs[4] = xs[4] + 1
    xs[5] = xs[5] + 1
    xs[6] = xs[6] + 1
    xs[7] = xs[7] + 1
    xs[8] = xs[8] + 1
  end
  return xs[1] + xs[2] + xs[3] + xs[4] + xs[5] + xs[6] + xs[7] + xs[8]
end`,
		},
	},
	{
		name:  "MapUpdate",
		fn:    "mapUpdate",
		sizes: []int{100, 1000, 10000},
		src: map[string]string{
			engSpore: `fun mapUpdate(n: int): int {
  var m: map<string, int> = {"a": 0, "b": 0, "c": 0, "d": 0}
  for (var i: int = 0; i < n; i = i + 1) {
    m["a"] = m["a"] + 1
    m["b"] = m["b"] + 1
    m["c"] = m["c"] + 1
    m["d"] = m["d"] + 1
  }
  return m["a"] + m["b"] + m["c"] + m["d"]
}`,
			engTengo: `mapUpdate := func(n) {
  m := {"a": 0, "b": 0, "c": 0, "d": 0}
  for i := 0; i < n; i++ {
    m["a"] = m["a"] + 1
    m["b"] = m["b"] + 1
    m["c"] = m["c"] + 1
    m["d"] = m["d"] + 1
  }
  return m["a"] + m["b"] + m["c"] + m["d"]
}
__result = mapUpdate(__n)`,
			engGoja: `function mapUpdate(n) {
  var m = {"a": 0, "b": 0, "c": 0, "d": 0};
  for (var i = 0; i < n; i++) {
    m["a"] = m["a"] + 1;
    m["b"] = m["b"] + 1;
    m["c"] = m["c"] + 1;
    m["d"] = m["d"] + 1;
  }
  return m["a"] + m["b"] + m["c"] + m["d"];
}`,
			engLua: `function mapUpdate(n)
  local m = {a = 0, b = 0, c = 0, d = 0}
  for i = 0, n - 1 do
    m["a"] = m["a"] + 1
    m["b"] = m["b"] + 1
    m["c"] = m["c"] + 1
    m["d"] = m["d"] + 1
  end
  return m["a"] + m["b"] + m["c"] + m["d"]
end`,
		},
	},
	{
		name:  "BranchyLoop",
		fn:    "branchyLoop",
		sizes: []int{100, 1000, 10000},
		src: map[string]string{
			engSpore: `fun branchyLoop(n: int): int {
  var s: int = 0
  var half: int = n / 2
  for (var i: int = 0; i < n; i = i + 1) {
    if (i < half) { s = s + i } else { s = s - i }
  }
  return s
}`,
			engTengo: `branchyLoop := func(n) {
  s := 0
  half := n / 2
  for i := 0; i < n; i++ {
    if i < half { s += i } else { s -= i }
  }
  return s
}
__result = branchyLoop(__n)`,
			engGoja: `function branchyLoop(n) {
  var s = 0;
  var half = Math.floor(n / 2);
  for (var i = 0; i < n; i++) {
    if (i < half) { s += i; } else { s -= i; }
  }
  return s;
}`,
			engLua: `function branchyLoop(n)
  local s = 0
  local half = math.floor(n / 2)
  for i = 0, n - 1 do
    if i < half then s = s + i else s = s - i end
  end
  return s
end`,
		},
	},
	{
		name:  "FunctionArgs",
		fn:    "functionArgs",
		sizes: []int{100, 1000, 10000},
		src: map[string]string{
			engSpore: `fun add3(a: int, b: int, c: int): int { return a + b + c }
fun functionArgs(n: int): int {
  var s: int = 0
  for (var i: int = 0; i < n; i = i + 1) { s = add3(s, i, 1) }
  return s
}`,
			engTengo: `add3 := func(a, b, c) { return a + b + c }
functionArgs := func(n) {
  s := 0
  for i := 0; i < n; i++ { s = add3(s, i, 1) }
  return s
}
__result = functionArgs(__n)`,
			engGoja: `function add3(a, b, c) { return a + b + c; }
function functionArgs(n) {
  var s = 0;
  for (var i = 0; i < n; i++) { s = add3(s, i, 1); }
  return s;
}`,
			engLua: `function add3(a, b, c)
  return a + b + c
end
function functionArgs(n)
  local s = 0
  for i = 0, n - 1 do s = add3(s, i, 1) end
  return s
end`,
		},
	},
	{
		name:  "ClosureCounter",
		fn:    "closureCounter",
		sizes: []int{100, 1000, 10000},
		src: map[string]string{
			engSpore: `fun closureCounter(n: int): int {
  var count: int = 0
  var inc: any = fun(by: int): int { count = count + by; return count }
  for (var i: int = 0; i < n; i = i + 1) { inc(1) }
  return count
}`,
			engTengo: `closureCounter := func(n) {
  count := 0
  inc := func(by) { count += by; return count }
  for i := 0; i < n; i++ { inc(1) }
  return count
}
__result = closureCounter(__n)`,
			engGoja: `function closureCounter(n) {
  var count = 0;
  var inc = function(by) { count += by; return count; };
  for (var i = 0; i < n; i++) { inc(1); }
  return count;
}`,
			engLua: `function closureCounter(n)
  local count = 0
  local function inc(by) count = count + by; return count end
  for i = 0, n - 1 do inc(1) end
  return count
end`,
		},
	},
	{
		name:  "MapFilter",
		fn:    "mapFilter",
		// Sized like ArrayLoop/ArrayUpdate: at N=10000 the three live arrays
		// (with array-growth doubling slack) plus the previous invocation's
		// still-rooted garbage exceed the evaluator's 65536-word VM pool.
		sizes: []int{128, 1024, 8192},
		src: map[string]string{
			engSpore: `fun mapInts(xs: array<int>, f: any): array<int> {
  var out: array<int> = []
  for (x in xs) { push(out, f(x)) }
  return out
}
fun filterInts(xs: array<int>, keep: any): array<int> {
  var out: array<int> = []
  for (x in xs) { if (keep(x)) { push(out, x) } }
  return out
}
fun mapFilter(n: int): int {
  var xs: array<int> = []
  for (var i: int = 0; i < n; i = i + 1) { push(xs, i) }
  var doubled: array<int> = mapInts(xs, fun(x: int): int { return x * 2 })
  var evens: array<int> = filterInts(doubled, fun(x: int): int { return x % 4 == 0 })
  var s: int = 0
  for (v in evens) { s = s + v }
  return s
}`,
			engTengo: `mapInts := func(xs, f) {
  out := []
  for x in xs { out = append(out, f(x)) }
  return out
}
filterInts := func(xs, keep) {
  out := []
  for x in xs { if keep(x) { out = append(out, x) } }
  return out
}
mapFilter := func(n) {
  xs := []
  for i := 0; i < n; i++ { xs = append(xs, i) }
  doubled := mapInts(xs, func(x) { return x * 2 })
  evens := filterInts(doubled, func(x) { return x % 4 == 0 })
  s := 0
  for v in evens { s += v }
  return s
}
__result = mapFilter(__n)`,
			engGoja: `function mapFilter(n) {
  var xs = [];
  for (var i = 0; i < n; i++) { xs.push(i); }
  var doubled = xs.map(function(x) { return x * 2; });
  var evens = doubled.filter(function(x) { return x % 4 === 0; });
  var s = 0;
  for (var i = 0; i < evens.length; i++) { s += evens[i]; }
  return s;
}`,
			engLua: `local function mapInts(xs, f)
  local out = {}
  for _, x in ipairs(xs) do out[#out + 1] = f(x) end
  return out
end
local function filterInts(xs, keep)
  local out = {}
  for _, x in ipairs(xs) do if keep(x) then out[#out + 1] = x end end
  return out
end
function mapFilter(n)
  local xs = {}
  for i = 0, n - 1 do xs[#xs + 1] = i end
  local doubled = mapInts(xs, function(x) return x * 2 end)
  local evens = filterInts(doubled, function(x) return x % 4 == 0 end)
  local s = 0
  for _, v in ipairs(evens) do s = s + v end
  return s
end`,
		},
	},
	{
		name:  "NestedClosure",
		fn:    "nestedClosure",
		sizes: []int{100, 1000, 10000},
		src: map[string]string{
			engSpore: `fun nestedClosure(n: int): int {
  var base: int = 1000
  var outer: any = fun(y: int): any {
    var inner: any = fun(z: int): int { return base + y + z }
    return inner
  }
  var s: int = 0
  for (var i: int = 0; i < n; i = i + 1) {
    var fn: any = outer(i % 10)
    s = s + fn(i % 100)
  }
  return s
}`,
			engTengo: `nestedClosure := func(n) {
  base := 1000
  outer := func(y) {
    inner := func(z) { return base + y + z }
    return inner
  }
  s := 0
  for i := 0; i < n; i++ {
    fn := outer(i % 10)
    s += fn(i % 100)
  }
  return s
}
__result = nestedClosure(__n)`,
			engGoja: `function nestedClosure(n) {
  var base = 1000;
  var outer = function(y) {
    var inner = function(z) { return base + y + z; };
    return inner;
  };
  var s = 0;
  for (var i = 0; i < n; i++) {
    var fn = outer(i % 10);
    s += fn(i % 100);
  }
  return s;
}`,
			engLua: `function nestedClosure(n)
  local base = 1000
  local function outer(y)
    local function inner(z) return base + y + z end
    return inner
  end
  local s = 0
  for i = 0, n - 1 do
    local fn = outer(i % 10)
    s = s + fn(i % 100)
  end
  return s
end`,
		},
	},
	// Alloc workload deferred: Spore's default VM memory pool (4096 words,
	// hard-coded inside bytecode.NewVMEvaluator) cannot hold the per-iteration
	// map allocations for any meaningful N. Re-enable once the VM gains a
	// sizing API or adaptive growth.
}

func mkRunner(eng, src string) (runner, error) {
	switch eng {
	case engSpore:
		return NewSporeRunner(src)
	case engTengo:
		return NewTengoRunner(src)
	case engGoja:
		return NewGojaRunner(src)
	case engLua:
		return NewLuaRunner(src)
	}
	return nil, fmt.Errorf("unknown engine %q", eng)
}

func runEngine(b *testing.B, eng string) {
	b.Helper()
	for _, w := range workloads {
		src, ok := w.src[eng]
		if !ok {
			b.Fatalf("workload %q has no source for engine %q", w.name, eng)
		}
		for _, n := range w.sizes {
			b.Run(fmt.Sprintf("%s/N=%d", w.name, n), func(b *testing.B) {
				r, err := mkRunner(eng, src)
				if err != nil {
					b.Fatalf("setup %s: %v", eng, err)
				}
				if closer, ok := r.(interface{ Close() }); ok {
					b.Cleanup(closer.Close)
				}
				// One warm-up call to amortize lazy bind paths (e.g., goja
				// AssertFunction) and any first-touch JIT on the engine side.
				if _, err := r.Run(w.fn, n); err != nil {
					b.Fatalf("warmup %s/%s: %v", eng, w.name, err)
				}
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if _, err := r.Run(w.fn, n); err != nil {
						b.Fatalf("run %s/%s: %v", eng, w.name, err)
					}
				}
			})
		}
	}
}

func BenchmarkSpore(b *testing.B) { runEngine(b, engSpore) }
func BenchmarkTengo(b *testing.B)   { runEngine(b, engTengo) }
func BenchmarkGoja(b *testing.B)    { runEngine(b, engGoja) }
func BenchmarkLua(b *testing.B)     { runEngine(b, engLua) }
