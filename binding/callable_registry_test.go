package binding_test

import (
	"errors"
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/schema"
)

// ============================================================================
// Helper functions (mirrors from schema/function_test.go, now external)
// ============================================================================

func greet(id int, name string) string        { return name }
func touch(id int)                            {}
func fail(id int) error                       { return errors.New("boom") }
func load(id int) (string, error)             { return "", nil }
func useIntMap(items map[string]int) string   { return "" }
func useBoolMap(items map[string]bool) string { return "" }

type bindProfile struct {
	DisplayName string
	Level       int
}

type bindEnvelope struct {
	Name    string
	Profile bindProfile
}

// ============================================================================
// CallableRegistry tests
// ============================================================================

func TestCallableRegistry_RegisterAndLookup(t *testing.T) {
	reg := binding.NewCallableRegistry()

	desc, err := schema.DescribeGoFunction("greet", greet)
	if err != nil {
		t.Fatalf("DescribeGoFunction: %v", err)
	}

	if err := reg.Register(desc); err != nil {
		t.Fatalf("Register: %v", err)
	}

	found, ok := reg.Lookup("greet")
	if !ok {
		t.Fatal("expected to find registered callable")
	}
	if found.Name != "greet" {
		t.Fatalf("expected name greet, got %s", found.Name)
	}
}

func TestCallableRegistry_RegisterGoFunction(t *testing.T) {
	reg := binding.NewCallableRegistry()

	desc, err := reg.RegisterGoFunction("greet", greet)
	if err != nil {
		t.Fatalf("RegisterGoFunction: %v", err)
	}
	if desc.Name != "greet" {
		t.Fatalf("expected name greet, got %s", desc.Name)
	}
	if len(desc.Parameters) != 2 {
		t.Fatalf("expected 2 parameters, got %d", len(desc.Parameters))
	}

	// Should be lookable
	found, ok := reg.Lookup("greet")
	if !ok {
		t.Fatal("expected to find registered callable")
	}
	if found.Name != "greet" {
		t.Fatalf("expected name greet, got %s", found.Name)
	}
}

func TestCallableRegistry_RejectsDuplicateName(t *testing.T) {
	reg := binding.NewCallableRegistry()

	desc, err := schema.DescribeGoFunction("greet", greet)
	if err != nil {
		t.Fatalf("DescribeGoFunction: %v", err)
	}

	if err := reg.Register(desc); err != nil {
		t.Fatalf("first Register: %v", err)
	}

	if err := reg.Register(desc); err == nil {
		t.Fatal("expected error for duplicate registration, got nil")
	}
}

func TestCallableRegistry_RejectsEmptyName(t *testing.T) {
	reg := binding.NewCallableRegistry()

	desc := schema.CallableDesc{Name: ""}
	if err := reg.Register(desc); err == nil {
		t.Fatal("expected error for empty name, got nil")
	}
}

func TestCallableRegistry_RejectsMixedUnaryAndStreamingReturns(t *testing.T) {
	reg := binding.NewCallableRegistry()

	desc := schema.CallableDesc{
		Name:    "mixed",
		Mode:    schema.CallableModeStreaming,
		Returns: []schema.TypeDesc{{Kind: schema.TypeKindScalar, Name: "string"}},
		Streaming: &schema.StreamingCallableDesc{
			Next:  &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"},
			Final: &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"},
		},
	}

	if err := reg.Register(desc); err == nil {
		t.Fatal("expected error for mixed unary+streaming returns, got nil")
	}
}

func TestCallableRegistry_RegisterStreamingCallable(t *testing.T) {
	reg := binding.NewCallableRegistry()

	desc, err := schema.NewStreamingCallableDesc(
		"stream",
		nil,
		&schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"},
		&schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"},
		false,
	)
	if err != nil {
		t.Fatalf("NewStreamingCallableDesc: %v", err)
	}

	if err := reg.Register(desc); err != nil {
		t.Fatalf("Register streaming: %v", err)
	}

	found, ok := reg.Lookup("stream")
	if !ok {
		t.Fatal("expected to find streaming callable")
	}
	if found.Mode != schema.CallableModeStreaming {
		t.Fatalf("expected streaming mode, got %s", found.Mode)
	}
}

func TestCallableRegistry_LookupMissing(t *testing.T) {
	reg := binding.NewCallableRegistry()

	_, ok := reg.Lookup("nonexistent")
	if ok {
		t.Fatal("expected Lookup to return false for unregistered callable")
	}
}

func TestCallableRegistry_ListPreservesRegistrationOrder(t *testing.T) {
	reg := binding.NewCallableRegistry()

	_, _ = reg.RegisterGoFunction("alpha", greet)
	_, _ = reg.RegisterGoFunction("beta", touch)
	_, _ = reg.RegisterGoFunction("gamma", fail)

	list := reg.List()
	if len(list) != 3 {
		t.Fatalf("expected 3 callables, got %d", len(list))
	}
	if list[0].Name != "alpha" {
		t.Fatalf("expected first entry alpha, got %s", list[0].Name)
	}
	if list[1].Name != "beta" {
		t.Fatalf("expected second entry beta, got %s", list[1].Name)
	}
	if list[2].Name != "gamma" {
		t.Fatalf("expected third entry gamma, got %s", list[2].Name)
	}
}

func TestCallableRegistry_RegisterGoFunction_RejectsInvalidFunction(t *testing.T) {
	reg := binding.NewCallableRegistry()
	_, err := reg.RegisterGoFunction("bad", 123)
	if err == nil {
		t.Fatal("expected error for non-function")
	}
}

func TestCallableRegistry_RegisterGoFunction_RejectsDuplicate(t *testing.T) {
	reg := binding.NewCallableRegistry()
	_, err := reg.RegisterGoFunction("greet", greet)
	if err != nil {
		t.Fatalf("first RegisterGoFunction: %v", err)
	}
	_, err = reg.RegisterGoFunction("greet", greet)
	if err == nil {
		t.Fatal("expected error for duplicate name")
	}
}

func TestCallableRegistry_ListClonesStreamingDescriptor(t *testing.T) {
	reg := binding.NewCallableRegistry()

	desc, err := schema.NewStreamingCallableDesc(
		"stream",
		nil,
		&schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"},
		&schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"},
		false,
	)
	if err != nil {
		t.Fatalf("NewStreamingCallableDesc: %v", err)
	}

	if err := reg.Register(desc); err != nil {
		t.Fatalf("Register: %v", err)
	}

	list := reg.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 callable, got %d", len(list))
	}

	// Mutating the returned list entry should not affect the registry
	list[0].Name = "mutated"

	found, ok := reg.Lookup("stream")
	if !ok {
		t.Fatal("expected to find original streaming callable after list mutation")
	}
	if found.Name != "stream" {
		t.Fatalf("expected name stream after list mutation, got %s", found.Name)
	}
}
