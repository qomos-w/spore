package bytecode

import (
	"testing"

	"github.com/qomos-w/spore/internal/script/frontend"
	"github.com/qomos-w/spore/internal/script/vm"
)

// OnVMReplaced must fire at subscription time with the current VM and again
// on every CompileLoweredProgram VM swap — the construction-time guarantee
// that external GC-root owners rely on (review #14).
func TestVMEvaluator_OnVMReplacedFiresOnSubscribeAndSwap(t *testing.T) {
	eval := NewVMEvaluator()

	var seen []*vm.VM
	eval.OnVMReplaced(func(v *vm.VM) { seen = append(seen, v) })

	if len(seen) != 1 || seen[0] == nil || seen[0] != eval.VM() {
		t.Fatalf("subscription must fire immediately with the current VM (got %d notifications)", len(seen))
	}

	prog, err := frontend.ParseModuleForTest(`fun f(): int { return 1 }`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if err := eval.CompileProgram(prog); err != nil {
		t.Fatalf("CompileProgram: %v", err)
	}

	if len(seen) != 2 {
		t.Fatalf("compile swap must re-fire subscribers, got %d notifications", len(seen))
	}
	if seen[1] == seen[0] {
		t.Fatal("CompileLoweredProgram must produce a fresh VM instance")
	}
	if seen[1] != eval.VM() {
		t.Fatal("swap notification must carry the new VM")
	}
}
