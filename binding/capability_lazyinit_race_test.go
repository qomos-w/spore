package binding_test

import (
	"context"
	"sync"
	"testing"

	"github.com/qomos-w/spore/binding"
)

// TestScriptBinding_CapabilitiesLazyInitRaceFree exercises the lazy
// initialization of ScriptBinding.Capabilities under concurrency.
// Run with -race: the check-then-act nil guard that once guarded this
// field would have flagged a write/read race on concurrent first use;
// the sync.Once path must not.
func TestScriptBinding_CapabilitiesLazyInitRaceFree(t *testing.T) {
	// Zero-value ScriptBinding: Capabilities starts nil, forcing every
	// call below through ensureCapabilities' once.Do.
	sb := &binding.ScriptBinding{}

	builder := binding.NewCapability("tool", "service")
	if err := builder.AddFunction("execute", capabilityEcho); err != nil {
		t.Fatalf("AddFunction: %v", err)
	}
	cap, err := builder.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			switch i % 4 {
			case 0:
				_ = sb.RegisterCapability(cap)
			case 1:
				_, _ = sb.DescribeCapability("tool")
			case 2:
				_, _ = sb.FindCapabilityObject("tool", "nope")
			case 3:
				_, _ = sb.InvokeCapability(context.Background(), "tool", "execute", map[string]any{"Text": "hi"})
			}
		}(i)
	}
	wg.Wait()

	if sb.Capabilities == nil {
		t.Fatal("ensureCapabilities must have initialized the registry")
	}
}
