package bytecode

import (
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/internal/script/frontend"
	"github.com/qomos-w/spore/internal/script/vm"
)

// The interpreter owns the single execution (operand) stack; the VM's parallel
// operand stack was removed in the stack unification. These benchmarks pin the
// operand-stack hot path so future refactors can detect regressions.

// BenchmarkInterpreterStackPushPop measures the raw push/pop pair cost on the
// single execution stack.
func BenchmarkInterpreterStackPushPop(b *testing.B) {
	v := vm.NewVM(4096, 4096)
	interp := newInterpreter(v)
	val := vm.EncodeInt(7)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		interp.push(val)
		interp.pop()
	}
}

// BenchmarkInterpreterStackPushPeek measures the push/peek/pop triple.
func BenchmarkInterpreterStackPushPeek(b *testing.B) {
	v := vm.NewVM(4096, 4096)
	interp := newInterpreter(v)
	val := vm.EncodeInt(7)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		interp.push(val)
		_ = interp.peek()
		interp.pop()
	}
}

const stackHotLoopSource = `fun run(n: int): int {
  var x: int = 0
  for (var i: int = 0; i < n; i = i + 1) { x = x + i }
  return x
}`

// BenchmarkInterpreterStackHotLoop drives the operand stack through the real
// dispatch loop (opLoadLocal/opStoreLocalPop/opAddInt/opLtInt/opJump*) for a
// tight arithmetic loop, the regime the single-stack refactor must not slow.
func BenchmarkInterpreterStackHotLoop(b *testing.B) {
	prog, err := frontend.ParseModuleForTest(stackHotLoopSource)
	if err != nil {
		b.Fatalf("parse: %v", err)
	}
	eval := NewVMEvaluator()
	if err := eval.CompileProgram(prog); err != nil {
		b.Fatalf("compile: %v", err)
	}
	if _, err := eval.Evaluate("run", binding.InvocationStageUnary, []any{100}); err != nil {
		b.Fatalf("warmup: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := eval.Evaluate("run", binding.InvocationStageUnary, []any{100}); err != nil {
			b.Fatalf("evaluate: %v", err)
		}
	}
}
