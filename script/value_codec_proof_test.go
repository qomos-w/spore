package script_test

import (
	"fmt"
	"testing"

	"github.com/qomos-w/spore/internal/script/bytecode"
	"github.com/qomos-w/spore/internal/script/vm"
	"github.com/qomos-w/spore/script"
)

// proofCodecValue is a hypothetical host type that exists only in this test. It
// is a named scalar (underlying int64) taught to the boundary codec in one
// place — its own method set — with no production switch edits. This test
// proves that the single definition is honoured end to end: as a Runtime.Call
// argument on the way into the VM, and as a Result.DecodeInto target on the way
// back out.
type proofCodecValue int64

func (p proofCodecValue) EncodeToVM(vm_ *vm.VM, path string) (vm.Value, error) {
	return vm.EncodeLong(int64(p), vm_), nil
}

func (p *proofCodecValue) DecodeFromHost(value any) error {
	n, ok := value.(int64)
	if !ok {
		return fmt.Errorf("proofCodecValue: cannot decode %T", value)
	}
	*p = proofCodecValue(n)
	return nil
}

// Compile-time assertions that the type satisfies the codec extension seams.
var (
	_ bytecode.HostValueEncoder = proofCodecValue(0)
	_ bytecode.HostValueDecoder = (*proofCodecValue)(nil)
)

func TestBoundaryCodec_ExtensionTypeEndToEnd(t *testing.T) {
	rt, err := script.NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.LoadSource("demo", `export fun echo(n: long): long { return n }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	// Outbound: the extension type crosses into the VM as a long via its
	// EncodeToVM method, with no production change.
	result, err := rt.Call("echo", proofCodecValue(123))
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !result.Ok() {
		t.Fatalf("Call failed: %v", result.Unwrap())
	}

	// Inbound: the projected host value (an int64) is decoded back into the
	// extension type via its DecodeFromHost method.
	var out proofCodecValue
	if err := result.DecodeInto(&out); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if int64(out) != 123 {
		t.Fatalf("round trip: got %d, want 123", int64(out))
	}
}
