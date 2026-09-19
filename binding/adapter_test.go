package binding_test

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/schema"
)

// ============================================================================
// ExecutableRegistry tests
// ============================================================================

func TestExecutableRegistry_RegisterAdapterAndLookup(t *testing.T) {
	callables := binding.NewCallableRegistry()
	desc, err := callables.RegisterGoFunction("greet", greet)
	if err != nil {
		t.Fatalf("RegisterGoFunction: %v", err)
	}

	exec := binding.NewExecutableRegistry(callables)

	adapter, err := binding.NewUnaryInvocationAdapter(desc, func(args []any) (any, error) {
		return args[1], nil
	})
	if err != nil {
		t.Fatalf("NewUnaryInvocationAdapter: %v", err)
	}

	if err := exec.RegisterAdapter(adapter); err != nil {
		t.Fatalf("RegisterAdapter: %v", err)
	}

	found, ok := exec.Lookup("greet")
	if !ok {
		t.Fatal("expected to find registered adapter")
	}
	if found.Callable().Name != "greet" {
		t.Fatalf("expected adapter callable name greet, got %s", found.Callable().Name)
	}
}

func TestExecutableRegistry_RegisterAdapter_RejectsUnregisteredCallable(t *testing.T) {
	callables := binding.NewCallableRegistry()
	exec := binding.NewExecutableRegistry(callables)

	desc := schema.CallableDesc{
		Name:       "unregistered",
		Mode:       schema.CallableModeUnary,
		Parameters: []schema.ParameterDesc{{Name: "arg0", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}}},
	}

	adapter, err := binding.NewUnaryInvocationAdapter(desc, func(args []any) (any, error) {
		return nil, nil
	})
	if err != nil {
		t.Fatalf("NewUnaryInvocationAdapter: %v", err)
	}

	if err := exec.RegisterAdapter(adapter); err == nil {
		t.Fatal("expected error for unregistered callable, got nil")
	}
}

func TestExecutableRegistry_RegisterAdapter_RejectsDuplicateAdapter(t *testing.T) {
	callables := binding.NewCallableRegistry()
	desc, err := callables.RegisterGoFunction("greet", greet)
	if err != nil {
		t.Fatalf("RegisterGoFunction: %v", err)
	}

	exec := binding.NewExecutableRegistry(callables)

	adapter1, err := binding.NewUnaryInvocationAdapter(desc, func(args []any) (any, error) {
		return nil, nil
	})
	if err != nil {
		t.Fatalf("NewUnaryInvocationAdapter: %v", err)
	}
	if err := exec.RegisterAdapter(adapter1); err != nil {
		t.Fatalf("first RegisterAdapter: %v", err)
	}

	adapter2, err := binding.NewUnaryInvocationAdapter(desc, func(args []any) (any, error) {
		return nil, nil
	})
	if err != nil {
		t.Fatalf("NewUnaryInvocationAdapter (2): %v", err)
	}

	if err := exec.RegisterAdapter(adapter2); err == nil {
		t.Fatal("expected error for duplicate adapter registration, got nil")
	}
}

func TestExecutableRegistry_Invoke(t *testing.T) {
	callables := binding.NewCallableRegistry()
	_, err := callables.RegisterGoFunction("greet", greet)
	if err != nil {
		t.Fatalf("RegisterGoFunction: %v", err)
	}

	exec := binding.NewExecutableRegistry(callables)

	adapter, err := binding.NewGoFunctionAdapter("greet", greet)
	if err != nil {
		t.Fatalf("NewGoFunctionAdapter: %v", err)
	}
	if err := exec.RegisterAdapter(adapter); err != nil {
		t.Fatalf("RegisterAdapter: %v", err)
	}

	outcome, err := exec.Invoke(binding.InvocationRequest{
		Callable: "greet",
		Stage:    binding.InvocationStageUnary,
		Args:     []any{1, "Alice"},
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
}

func TestExecutableRegistry_Invoke_UnregisteredCallable(t *testing.T) {
	callables := binding.NewCallableRegistry()
	exec := binding.NewExecutableRegistry(callables)

	_, err := exec.Invoke(binding.InvocationRequest{
		Callable: "missing",
		Stage:    binding.InvocationStageUnary,
	})
	if err == nil {
		t.Fatal("expected error for unregistered callable, got nil")
	}
}

func TestExecutableRegistry_Invoke_NoAdapter(t *testing.T) {
	callables := binding.NewCallableRegistry()
	_, err := callables.RegisterGoFunction("greet", greet)
	if err != nil {
		t.Fatalf("RegisterGoFunction: %v", err)
	}

	exec := binding.NewExecutableRegistry(callables)
	// No adapter registered

	_, err = exec.Invoke(binding.InvocationRequest{
		Callable: "greet",
		Stage:    binding.InvocationStageUnary,
		Args:     []any{1, "Alice"},
	})
	if err == nil {
		t.Fatal("expected error for missing adapter, got nil")
	}
}

// ============================================================================
// GoFunctionAdapter tests
// ============================================================================

func TestGoFunctionAdapter_BindsAndInvokes(t *testing.T) {
	adapter, err := binding.NewGoFunctionAdapter("greet", greet)
	if err != nil {
		t.Fatalf("NewGoFunctionAdapter: %v", err)
	}

	if adapter.Callable().Name != "greet" {
		t.Fatalf("expected callable name greet, got %s", adapter.Callable().Name)
	}

	outcome, err := adapter.Invoke(binding.InvocationRequest{
		Callable: "greet",
		Stage:    binding.InvocationStageUnary,
		Args:     []any{1, "Alice"},
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}

	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil {
		t.Fatal("expected non-nil payload for non-void result")
	}
	if outcome.Payload.Value != "Alice" {
		t.Fatalf("expected payload Alice, got %v", outcome.Payload.Value)
	}
}

func TestGoFunctionAdapter_VoidFunction(t *testing.T) {
	adapter, err := binding.NewGoFunctionAdapter("touch", touch)
	if err != nil {
		t.Fatalf("NewGoFunctionAdapter: %v", err)
	}

	outcome, err := adapter.Invoke(binding.InvocationRequest{
		Callable: "touch",
		Stage:    binding.InvocationStageUnary,
		Args:     []any{1},
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}

	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload != nil {
		t.Fatalf("expected nil payload for void function, got %v", outcome.Payload)
	}
	if outcome.Result.Value != nil {
		t.Fatalf("expected nil Value in result for void function, got %+v", outcome.Result.Value)
	}
}

func TestGoFunctionAdapter_ErrorFunction(t *testing.T) {
	adapter, err := binding.NewGoFunctionAdapter("fail", fail)
	if err != nil {
		t.Fatalf("NewGoFunctionAdapter: %v", err)
	}

	outcome, err := adapter.Invoke(binding.InvocationRequest{
		Callable: "fail",
		Stage:    binding.InvocationStageUnary,
		Args:     []any{1},
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}

	if outcome.Result.Kind != binding.InvocationResultError {
		t.Fatalf("expected error result, got %s", outcome.Result.Kind)
	}
	if outcome.Result.Error == nil {
		t.Fatal("expected error descriptor in result")
	}
	if outcome.Result.Error.Message != "boom" {
		t.Fatalf("expected error message boom, got %s", outcome.Result.Error.Message)
	}
}

func TestGoFunctionAdapter_PanicIsRecovered(t *testing.T) {
	adapter, err := binding.NewGoFunctionAdapter("boom", func(id int) string {
		if id < 0 {
			panic("intentional negative id")
		}
		return ""
	})
	if err != nil {
		t.Fatalf("NewGoFunctionAdapter: %v", err)
	}

	outcome, err := adapter.Invoke(binding.InvocationRequest{
		Callable: "boom",
		Stage:    binding.InvocationStageUnary,
		Args:     []any{-1},
	})
	if err != nil {
		t.Fatalf("Invoke must not return Go error for host panic: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultError {
		t.Fatalf("expected error result, got %s", outcome.Result.Kind)
	}
	if outcome.Result.Error == nil {
		t.Fatal("expected error descriptor for panic")
	}
	if !strings.Contains(outcome.Result.Error.Message, "host function panic") {
		t.Fatalf("expected message to contain 'host function panic', got %q", outcome.Result.Error.Message)
	}
	if !strings.Contains(outcome.Result.Error.Message, "intentional negative id") {
		t.Fatalf("expected message to contain panic value 'intentional negative id', got %q", outcome.Result.Error.Message)
	}
}

func TestGoFunctionAdapter_NilDereferencePanicIsRecovered(t *testing.T) {
	adapter, err := binding.NewGoFunctionAdapter("deref", func(id int) string {
		var p *struct{ V string }
		return p.V
	})
	if err != nil {
		t.Fatalf("NewGoFunctionAdapter: %v", err)
	}

	outcome, err := adapter.Invoke(binding.InvocationRequest{
		Callable: "deref",
		Stage:    binding.InvocationStageUnary,
		Args:     []any{1},
	})
	if err != nil {
		t.Fatalf("Invoke must not return Go error for host panic: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultError {
		t.Fatalf("expected error result, got %s", outcome.Result.Kind)
	}
	if outcome.Result.Error == nil {
		t.Fatal("expected error descriptor for panic")
	}
	if !strings.Contains(outcome.Result.Error.Message, "host function panic") {
		t.Fatalf("expected message to contain 'host function panic', got %q", outcome.Result.Error.Message)
	}
}

func TestGoFunctionAdapter_SuccessWithTrailingError(t *testing.T) {
	adapter, err := binding.NewGoFunctionAdapter("load", load)
	if err != nil {
		t.Fatalf("NewGoFunctionAdapter: %v", err)
	}

	outcome, err := adapter.Invoke(binding.InvocationRequest{
		Callable: "load",
		Stage:    binding.InvocationStageUnary,
		Args:     []any{1},
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}

	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
}

func TestGoFunctionAdapter_WrongCallableName(t *testing.T) {
	adapter, err := binding.NewGoFunctionAdapter("greet", greet)
	if err != nil {
		t.Fatalf("NewGoFunctionAdapter: %v", err)
	}

	_, err = adapter.Invoke(binding.InvocationRequest{
		Callable: "other",
		Stage:    binding.InvocationStageUnary,
		Args:     []any{1, "Alice"},
	})
	if err == nil {
		t.Fatal("expected error for wrong callable name, got nil")
	}
}

func TestGoFunctionAdapter_WrongArgCount(t *testing.T) {
	adapter, err := binding.NewGoFunctionAdapter("greet", greet)
	if err != nil {
		t.Fatalf("NewGoFunctionAdapter: %v", err)
	}

	_, err = adapter.Invoke(binding.InvocationRequest{
		Callable: "greet",
		Stage:    binding.InvocationStageUnary,
		Args:     []any{1},
	})
	if err == nil {
		t.Fatal("expected error for wrong arg count, got nil")
	}
}

func TestGoFunctionAdapter_RejectsNonFunction(t *testing.T) {
	_, err := binding.NewGoFunctionAdapter("bad", 123)
	if err == nil {
		t.Fatal("expected error for non-function, got nil")
	}
}

// ============================================================================
// UnaryInvocationAdapter tests
// ============================================================================

func TestUnaryInvocationAdapter_RequiresHandler(t *testing.T) {
	desc := schema.CallableDesc{
		Name:       "test",
		Mode:       schema.CallableModeUnary,
		Parameters: []schema.ParameterDesc{{Name: "arg0", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}}},
	}

	_, err := binding.NewUnaryInvocationAdapter(desc, nil)
	if err == nil {
		t.Fatal("expected error for nil handler, got nil")
	}
}

func TestUnaryInvocationAdapter_RejectsStreamingDesc(t *testing.T) {
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

	_, err = binding.NewUnaryInvocationAdapter(desc, func(args []any) (any, error) {
		return nil, nil
	})
	if err == nil {
		t.Fatal("expected error for streaming desc with unary adapter, got nil")
	}
}

// ============================================================================
// StreamingInvocationAdapter tests
// ============================================================================

func TestStreamingInvocationAdapter_NextAndFinal(t *testing.T) {
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

	adapter, err := binding.NewStreamingInvocationAdapter(
		desc,
		func(args []any) (any, error) { return "partial", nil },
		func(args []any) (any, error) { return 42, nil },
	)
	if err != nil {
		t.Fatalf("NewStreamingInvocationAdapter: %v", err)
	}

	// Invoke next
	outcome, err := adapter.Invoke(binding.InvocationRequest{
		Callable: "stream",
		Stage:    binding.InvocationStageNext,
		Args:     []any{},
	})
	if err != nil {
		t.Fatalf("Invoke next: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result for next, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != "partial" {
		t.Fatalf("expected payload partial, got %v", outcome.Payload)
	}

	// Invoke final
	outcome, err = adapter.Invoke(binding.InvocationRequest{
		Callable: "stream",
		Stage:    binding.InvocationStageFinal,
		Args:     []any{},
	})
	if err != nil {
		t.Fatalf("Invoke final: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result for final, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 42 {
		t.Fatalf("expected payload 42, got %v", outcome.Payload)
	}
}

func TestStreamingInvocationAdapter_RequiresNextWhenDefined(t *testing.T) {
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

	_, err = binding.NewStreamingInvocationAdapter(desc, nil, func(args []any) (any, error) {
		return 42, nil
	})
	if err == nil {
		t.Fatal("expected error for nil next handler when next schema is defined, got nil")
	}
}

func TestStreamingInvocationAdapter_RequiresFinalWhenDefined(t *testing.T) {
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

	_, err = binding.NewStreamingInvocationAdapter(desc, func(args []any) (any, error) {
		return "partial", nil
	}, nil)
	if err == nil {
		t.Fatal("expected error for nil final handler when final schema is defined, got nil")
	}
}

func TestStreamingInvocationAdapter_RejectsUnaryDesc(t *testing.T) {
	desc := schema.CallableDesc{
		Name:       "unary",
		Mode:       schema.CallableModeUnary,
		Parameters: []schema.ParameterDesc{{Name: "arg0", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}}},
	}

	_, err := binding.NewStreamingInvocationAdapter(desc,
		func(args []any) (any, error) { return nil, nil },
		func(args []any) (any, error) { return nil, nil },
	)
	if err == nil {
		t.Fatal("expected error for unary desc with streaming adapter, got nil")
	}
}

// ============================================================================
// GoFunctionAdapter struct binding tests
// ============================================================================

func TestGoFunctionAdapter_BindsStructArgFromMap(t *testing.T) {
	adapter, err := binding.NewGoFunctionAdapter("useProfile", func(p bindProfile) string {
		return p.DisplayName
	})
	if err != nil {
		t.Fatalf("NewGoFunctionAdapter: %v", err)
	}

	outcome, err := adapter.Invoke(binding.InvocationRequest{
		Callable: "useProfile",
		Stage:    binding.InvocationStageUnary,
		Args:     []any{map[string]any{"DisplayName": "Alice", "Level": 5}},
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != "Alice" {
		t.Fatalf("expected payload Alice, got %v", outcome.Payload)
	}
}

func TestGoFunctionAdapter_BindsNestedStructArgFromMap(t *testing.T) {
	adapter, err := binding.NewGoFunctionAdapter("useEnvelope", func(e bindEnvelope) string {
		return e.Name
	})
	if err != nil {
		t.Fatalf("NewGoFunctionAdapter: %v", err)
	}

	outcome, err := adapter.Invoke(binding.InvocationRequest{
		Callable: "useEnvelope",
		Stage:    binding.InvocationStageUnary,
		Args: []any{map[string]any{
			"Name": "Bob",
			"Profile": map[string]any{
				"DisplayName": "Bobby",
				"Level":       3,
			},
		}},
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil || outcome.Payload.Value != "Bob" {
		t.Fatalf("expected payload Bob, got %v", outcome.Payload)
	}
}

func TestGoFunctionAdapter_BindsMapArg(t *testing.T) {
	adapter, err := binding.NewGoFunctionAdapter("useIntMap", useIntMap)
	if err != nil {
		t.Fatalf("NewGoFunctionAdapter: %v", err)
	}

	outcome, err := adapter.Invoke(binding.InvocationRequest{
		Callable: "useIntMap",
		Stage:    binding.InvocationStageUnary,
		Args:     []any{map[string]int{"a": 1, "b": 2}},
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
}

func TestGoFunctionAdapter_BindsBoolMapArg(t *testing.T) {
	adapter, err := binding.NewGoFunctionAdapter("useBoolMap", useBoolMap)
	if err != nil {
		t.Fatalf("NewGoFunctionAdapter: %v", err)
	}

	outcome, err := adapter.Invoke(binding.InvocationRequest{
		Callable: "useBoolMap",
		Stage:    binding.InvocationStageUnary,
		Args:     []any{map[string]bool{"active": true}},
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
}

func TestGoFunctionAdapter_IntArgRejectsString(t *testing.T) {
	adapter, err := binding.NewGoFunctionAdapter("greet", greet)
	if err != nil {
		t.Fatalf("NewGoFunctionAdapter: %v", err)
	}

	_, err = adapter.Invoke(binding.InvocationRequest{
		Callable: "greet",
		Stage:    binding.InvocationStageUnary,
		Args:     []any{"not-an-int", "Alice"},
	})
	if err == nil {
		t.Fatal("expected error for wrong arg type, got nil")
	}
}

// Verify errors import is used
var _ = errors.New("")

func TestJSONTagNameContract(t *testing.T) {
	cases := []struct {
		tag      string
		field    string
		expected string
	}{
		{"", "Name", "Name"},
		{"-", "Hidden", "-"},
		{"display_name", "DisplayName", "display_name"},
		{"msg,omitempty", "Message", "msg"},
		{",omitempty", "Fallback", "Fallback"},
	}
	for _, tc := range cases {
		field := reflect.StructField{
			Name: tc.field,
		}
		if tc.tag != "" {
			field.Tag = reflect.StructTag(fmt.Sprintf(`json:"%s"`, tc.tag))
		}
		got := binding.JSONTagName(field)
		if got != tc.expected {
			t.Errorf("JSONTagName(%q, %q) = %q, want %q", tc.tag, tc.field, got, tc.expected)
		}
	}
}
