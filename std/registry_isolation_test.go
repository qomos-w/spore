package std_test

import (
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/std"
)

// fakeMathModule is a distinguishable stand-in for the built-in "math" module.
// It is a pointer type so registry entries can be compared by identity.
type fakeMathModule struct{}

func (*fakeMathModule) Name() string { return "math" }

func (*fakeMathModule) Register(sb *binding.ScriptBinding) error {
	builder := binding.NewCapability("math", "module")
	if err := builder.AddFreeFunction("customAdd", func(a, b float64) float64 {
		return a + b + 999
	}); err != nil {
		return err
	}
	cap, err := builder.Build()
	if err != nil {
		return err
	}
	if err := sb.RegisterCapability(cap); err != nil {
		return err
	}
	return sb.ExposeCapabilityCallables("math")
}

// TestRegistry_ReplaceThenRestore proves the module registry is usable as a
// test-isolation seam: a test can substitute a std module, have RegisterAll
// pick up the substitution, then restore the built-in set — leaving the
// package-level registry unpolluted for every other test.
func TestRegistry_ReplaceThenRestore(t *testing.T) {
	original := std.Registry()
	// Safety net: even if an assertion below fails, never leak the swap.
	defer std.SetRegistry(original)

	// Baseline: the built-in registry contains "math", which registers math.abs.
	if moduleByName(original, "math") == nil {
		t.Fatal(`built-in registry is missing module "math"`)
	}
	baseline := binding.NewScriptBinding()
	if err := std.RegisterAll(baseline); err != nil {
		t.Fatalf("RegisterAll (baseline): %v", err)
	}
	if _, ok := baseline.Executors.Lookup("math.abs"); !ok {
		t.Fatal("expected built-in math.abs before replacement")
	}

	// Replace "math" with the stand-in.
	replacement := &fakeMathModule{}
	std.SetRegistry(replaceModule(original, "math", replacement))

	// The global registry now reports the replacement...
	if got := moduleByName(std.Registry(), "math"); got != std.Module(replacement) {
		t.Fatalf("expected registry to hold the replacement math module, got %v", got)
	}
	// ...and RegisterAll consumes it: customAdd is present, math.abs is gone.
	swapped := binding.NewScriptBinding()
	if err := std.RegisterAll(swapped); err != nil {
		t.Fatalf("RegisterAll (replaced): %v", err)
	}
	if _, ok := swapped.Executors.Lookup("math.customAdd"); !ok {
		t.Fatal("expected replacement module to expose math.customAdd")
	}
	if _, ok := swapped.Executors.Lookup("math.abs"); ok {
		t.Fatal("expected built-in math.abs to be absent after replacement")
	}

	// Restore the saved snapshot.
	std.SetRegistry(original)

	// The global registry is back to its original shape...
	restored := std.Registry()
	if len(restored) != len(original) {
		t.Fatalf("expected %d modules after restore, got %d", len(original), len(restored))
	}
	for i, m := range original {
		if restored[i].Name() != m.Name() {
			t.Fatalf("module %d: expected %q after restore, got %q", i, m.Name(), restored[i].Name())
		}
	}
	// ...and RegisterAll is back to the built-in behavior.
	final := binding.NewScriptBinding()
	if err := std.RegisterAll(final); err != nil {
		t.Fatalf("RegisterAll (restored): %v", err)
	}
	if _, ok := final.Executors.Lookup("math.abs"); !ok {
		t.Fatal("expected built-in math.abs to be restored")
	}
	if _, ok := final.Executors.Lookup("math.customAdd"); ok {
		t.Fatal("expected replacement math.customAdd to be gone after restore")
	}
}

func moduleByName(mods []std.Module, name string) std.Module {
	for _, m := range mods {
		if m.Name() == name {
			return m
		}
	}
	return nil
}

func replaceModule(mods []std.Module, name string, replacement std.Module) []std.Module {
	out := make([]std.Module, len(mods))
	for i, m := range mods {
		if m.Name() == name {
			out[i] = replacement
			continue
		}
		out[i] = m
	}
	return out
}
