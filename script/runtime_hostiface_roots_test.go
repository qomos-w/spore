package script

import (
	"testing"

	"github.com/qomos-w/spore/schema"
)

// The host-interface root provider must be attached at every evaluator
// wiring path — construction, the compile-time VM swap, Clone, and Reset —
// by VM-lifecycle subscription, not by call-site discipline at bind/finalize
// time (review #14: "host-bound objects stay rooted for the RT lifetime"
// as a construction-time guarantee).
func TestHostIfaceRootsFollowVMLifecycle(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if rt.hostIfaceRootProviderVM != rt.evaluator.VM() {
		t.Fatal("root provider must be attached at construction")
	}

	g := &gcProbeGreeter{}
	if err := rt.BindInterfaceObject("host", "Greeter", schema.InterfaceDesc{
		Name: "Greeter",
		Methods: []schema.MethodDesc{{
			Name:    "label",
			Returns: []schema.TypeDesc{{Kind: schema.TypeKindScalar, Name: "string"}},
		}},
	}, g); err != nil {
		t.Fatalf("BindInterfaceObject: %v", err)
	}
	if err := rt.LoadSource("demo", `import Greeter from "host"

export fun churn(): string { return Greeter.label() }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	if rt.hostIfaceRootProviderVM != rt.evaluator.VM() {
		t.Fatal("root provider must follow the post-compile VM swap")
	}

	// Behavioral check: sustained allocation must not reclaim the proxy.
	for i := 0; i < 20000; i++ {
		if _, err := rt.Call("churn"); err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}
	}

	if err := rt.Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if rt.hostIfaceRootProviderVM != rt.evaluator.VM() {
		t.Fatal("Reset must re-attach the provider on the fresh evaluator's VM")
	}

	cloned, err := rt.Clone()
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if cloned.hostIfaceRootProviderVM != cloned.evaluator.VM() {
		t.Fatal("Clone must attach the provider on the clone's own evaluator")
	}
}
