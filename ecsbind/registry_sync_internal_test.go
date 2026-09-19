package ecsbind

import (
	"strings"
	"testing"

	"github.com/qomos-w/spore/runtime"
	"github.com/qomos-w/spore/schema"
	"github.com/qomos-w/spore/script"
)

// This file lives in the internal test package so it can inject the exact
// condition the guard exists for: a descriptor sitting in the binding's map
// without a matching declaration on the World. That drift cannot be produced
// through the public surface (RegisterComponent writes both sides), which is
// the point of the fix — the test constructs it out of band and proves it is
// exposed rather than silently tolerated.

func syncDesc(name string) schema.ObjectDesc {
	return schema.ObjectDesc{Name: name}
}

// The #7 fix: registering a component on the facade declares it on the World,
// so the binding's descriptor map is derived from — never independent of — the
// World's component registry.
func TestRegistrySync_RegisterComponentDeclaresOnWorld(t *testing.T) {
	w := runtime.NewWorld()
	b := New(w).
		RegisterComponent("Health", syncDesc("Health")).
		RegisterComponent("Position", syncDesc("Position"))

	for _, name := range []string{"Health", "Position"} {
		if !w.RegisteredComponent(name) {
			t.Fatalf("binding registration of %q must declare it on the World", name)
		}
	}
	if err := b.VerifyRegistry(); err != nil {
		t.Fatalf("VerifyRegistry after symmetric registration: %v", err)
	}

	// Single source of truth in the other direction too: the World's declared
	// vocabulary is exactly what the binding exposed.
	if got, want := strings.Join(w.RegisteredComponents(), ","), "Health,Position"; got != want {
		t.Fatalf("World vocabulary = %q, want %q", got, want)
	}
}

// Drift injected out of band (a descriptor the World never heard of) must be
// exposed by VerifyRegistry and must stop Bind from sealing a facade whose
// component names the World would reject.
func TestRegistrySync_DriftExposedAtBind(t *testing.T) {
	w := runtime.NewWorld()
	b := New(w).RegisterComponent("Health", syncDesc("Health"))

	// Out-of-band drift: bypass RegisterComponent so only the binding's half
	// of the double registration exists.
	b.descs["Ghost"] = syncDesc("Ghost")

	err := b.VerifyRegistry()
	if err == nil {
		t.Fatal("VerifyRegistry must expose binding-side drift")
	}
	if !strings.Contains(err.Error(), "Ghost") {
		t.Fatalf("drift error must name the drifting component, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "drift") {
		t.Fatalf("drift error must name the condition, got %q", err.Error())
	}

	rt, rtErr := script.NewRuntime()
	if rtErr != nil {
		t.Fatalf("NewRuntime: %v", rtErr)
	}
	if bindErr := b.Bind(rt); bindErr == nil {
		t.Fatal("Bind must refuse a drifted registry")
	} else if !strings.Contains(bindErr.Error(), "Ghost") {
		t.Fatalf("Bind error must name the drifting component, got %q", bindErr.Error())
	}
	if bindErr := b.BindAs(rt, "ecs2", "World2"); bindErr == nil {
		t.Fatal("BindAs must refuse a drifted registry")
	}

	// Declaring the missing name on the World resolves the drift: the check is
	// a consistency assertion, not a lock.
	if regErr := w.RegisterComponent("Ghost"); regErr != nil {
		t.Fatalf("RegisterComponent: %v", regErr)
	}
	if err := b.VerifyRegistry(); err != nil {
		t.Fatalf("VerifyRegistry after resolving drift: %v", err)
	}
	if bindErr := b.Bind(rt); bindErr != nil {
		t.Fatalf("Bind after resolving drift: %v", bindErr)
	}
}

// The invariant is deliberately one-way: the World may carry components the
// script facade does not expose (host aggregates registered through
// AddRegistry), and that must not read as drift.
func TestRegistrySync_WorldOnlyComponentsAreNotDrift(t *testing.T) {
	w := runtime.NewWorld()
	w.RegisterComponents("host.Only")
	b := New(w).RegisterComponent("Health", syncDesc("Health"))

	if err := b.VerifyRegistry(); err != nil {
		t.Fatalf("World-only components must not read as drift: %v", err)
	}
}
