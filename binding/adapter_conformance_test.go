package binding_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/schema"
)

// ============================================================================
// ExecutableAdapter SPI conformance test
//
// ExecutableAdapterConformance verifies that an ExecutableAdapter
// implementation satisfies the binding.ExecutableAdapter SPI contract.
// Any alternative adapter backend must pass this conformance test to be
// considered a valid implementation.
//
// Usage:
//
//	func TestMyAdapter_Conformance(t *testing.T) {
//	    adapter, _ := binding.NewMyAdapter("myCallable", myFunc)
//	    ExecutableAdapterConformance(t, adapter)
//	}
//
// The adapter must be pre-configured for a unary callable named "conform"
// that accepts a single int32 argument and returns a string.
// ============================================================================

// ExecutableAdapterConformance tests that an ExecutableAdapter implementation
// satisfies the binding.ExecutableAdapter SPI contract. It covers callable
// descriptor validity, successful invocation, error invocation, name/stage/arg
// validation, and result structure.
//
// The adapter must be configured for a unary callable named "conform" with
// one int32 parameter and a string return type.
func ExecutableAdapterConformance(t *testing.T, adapter binding.ExecutableAdapter) {
	t.Run("CallableReturnsValidDescriptor", func(t *testing.T) {
		desc := adapter.Callable()
		if desc.Name == "" {
			t.Fatal("Callable() must return a descriptor with a non-empty name")
		}
		if desc.Mode != schema.CallableModeUnary && desc.Mode != "" {
			t.Fatalf("expected unary mode, got %q", desc.Mode)
		}
	})

	t.Run("InvokeSuccessProducesValueOutcome", func(t *testing.T) {
		desc := adapter.Callable()
		outcome, err := adapter.Invoke(binding.InvocationRequest{
			Callable: desc.Name,
			Stage:    binding.InvocationStageUnary,
			Args:     []any{int32(42)},
		})
		if err != nil {
			t.Fatalf("Invoke: %v", err)
		}
		if outcome.Result.Kind != binding.InvocationResultValue {
			t.Fatalf("expected value result, got %s", outcome.Result.Kind)
		}
		if outcome.Result.Callable != desc.Name {
			t.Fatalf("result callable mismatch: expected %q, got %q", desc.Name, outcome.Result.Callable)
		}
		if outcome.Result.Stage != binding.InvocationStageUnary {
			t.Fatalf("expected unary stage, got %s", outcome.Result.Stage)
		}
	})

	t.Run("InvokeWrongCallableNameReturnsError", func(t *testing.T) {
		desc := adapter.Callable()
		_, err := adapter.Invoke(binding.InvocationRequest{
			Callable: "wrong_" + desc.Name,
			Stage:    binding.InvocationStageUnary,
			Args:     []any{int32(1)},
		})
		if err == nil {
			t.Fatal("expected error for wrong callable name, got nil")
		}
	})

	t.Run("InvokeWrongStageReturnsError", func(t *testing.T) {
		desc := adapter.Callable()
		_, err := adapter.Invoke(binding.InvocationRequest{
			Callable: desc.Name,
			Stage:    binding.InvocationStageNext,
			Args:     []any{int32(1)},
		})
		if err == nil {
			t.Fatal("expected error for wrong invocation stage on unary callable, got nil")
		}
	})

	t.Run("InvokeWrongArgCountReturnsError", func(t *testing.T) {
		desc := adapter.Callable()
		_, err := adapter.Invoke(binding.InvocationRequest{
			Callable: desc.Name,
			Stage:    binding.InvocationStageUnary,
			Args:     []any{},
		})
		if err == nil {
			t.Fatal("expected error for wrong arg count, got nil")
		}
	})

	t.Run("SequentialInvocations_StateIndependent", func(t *testing.T) {
		desc := adapter.Callable()

		out1, err := adapter.Invoke(binding.InvocationRequest{
			Callable: desc.Name,
			Stage:    binding.InvocationStageUnary,
			Args:     []any{int32(1)},
		})
		if err != nil {
			t.Fatalf("first invoke: %v", err)
		}
		if out1.Result.Kind != binding.InvocationResultValue {
			t.Fatalf("first invoke: expected value result, got %s", out1.Result.Kind)
		}

		out2, err := adapter.Invoke(binding.InvocationRequest{
			Callable: desc.Name,
			Stage:    binding.InvocationStageUnary,
			Args:     []any{int32(99)},
		})
		if err != nil {
			t.Fatalf("second invoke: %v", err)
		}
		if out2.Result.Kind != binding.InvocationResultValue {
			t.Fatalf("second invoke: expected value result, got %s", out2.Result.Kind)
		}

		descAfter := adapter.Callable()
		if descAfter.Name != desc.Name {
			t.Fatalf("descriptor changed after invocations: expected %q, got %q", desc.Name, descAfter.Name)
		}
	})

	t.Run("ConcurrentInvoke_NoDataRace", func(t *testing.T) {
		desc := adapter.Callable()

		const goroutines = 10
		const opsPer = 20
		done := make(chan error, goroutines*opsPer)
		var wg sync.WaitGroup

		for i := 0; i < goroutines; i++ {
			wg.Add(1)
			go func(seq int) {
				defer wg.Done()
				for j := 0; j < opsPer; j++ {
					_, err := adapter.Invoke(binding.InvocationRequest{
						Callable: desc.Name,
						Stage:    binding.InvocationStageUnary,
						Args:     []any{int32(seq*100 + j)},
					})
					if err != nil {
						done <- fmt.Errorf("concurrent invoke seq=%d j=%d: %w", seq, j, err)
						continue
					}
				}
			}(i)
		}

		wg.Wait()
		close(done)
		for err := range done {
			t.Errorf("concurrent operation error: %v", err)
		}
	})
}

// TestGoFunctionAdapter_Conformance runs the ExecutableAdapter SPI
// conformance test suite against the default GoFunctionAdapter backend.
func TestGoFunctionAdapter_Conformance(t *testing.T) {
	adapter, err := binding.NewGoFunctionAdapter("conform", func(x int32) string {
		return "ok"
	})
	if err != nil {
		t.Fatalf("NewGoFunctionAdapter: %v", err)
	}
	ExecutableAdapterConformance(t, adapter)
}

// TestUnaryInvocationAdapter_Conformance runs the ExecutableAdapter SPI
// conformance test suite against the default UnaryInvocationAdapter backend.
func TestUnaryInvocationAdapter_Conformance(t *testing.T) {
	desc := schema.CallableDesc{
		Name:       "conform",
		Mode:       schema.CallableModeUnary,
		Parameters: []schema.ParameterDesc{{Name: "arg0", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}}},
		Returns:    []schema.TypeDesc{{Kind: schema.TypeKindScalar, Name: "string"}},
	}
	adapter, err := binding.NewUnaryInvocationAdapter(desc, func(args []any) (any, error) {
		return "ok", nil
	})
	if err != nil {
		t.Fatalf("NewUnaryInvocationAdapter: %v", err)
	}
	ExecutableAdapterConformance(t, adapter)
}
