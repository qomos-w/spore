package script

import (
	"testing"

	"github.com/qomos-w/spore/schema"
)

type gcProbeGreeter struct{}

func (g *gcProbeGreeter) Label() string { return "x" }

// Repro: host interface proxy objects allocated via v.CreateObject are not
// registered as GC roots; sustained script allocation reclaims them and the
// stale handle resolves to a wrong object → "object class not found" panic.
func TestHostInterfaceProxySurvivesSustainedAllocation(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
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

export fun churn(): string {
    var keep: array<string> = []
    for (i in [0,0,0,0,0,0,0,0,0,0]) {
        keep = [Greeter.label()]
    }
    return Greeter.label()
}`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	for i := 0; i < 50000; i++ {
		if _, err := rt.Call("churn"); err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}
	}
}
