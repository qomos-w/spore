package frontend

import (
	"fmt"
	"sync"
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/schema"
)

func executableAdapterConformance(t *testing.T, adapter binding.ExecutableAdapter) {
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
