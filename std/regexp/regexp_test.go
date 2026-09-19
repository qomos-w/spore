package regexp_test

import (
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/std/regexp"
)

func TestRegexpModule_RegistersAllFunctions(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := regexp.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	for _, name := range []string{"regexp.match", "regexp.find", "regexp.findAll", "regexp.replace", "regexp.split"} {
		_, ok := sb.Executors.Lookup(name)
		if !ok {
			t.Fatalf("expected %s to be registered", name)
		}
	}
}

func TestRegexpModule_Match(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := regexp.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	outcome, err := sb.Invoke(binding.InvocationRequest{Callable: "regexp.match", Stage: binding.InvocationStageUnary, Args: []any{"^hello", "hello world"}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != true {
		t.Fatalf("expected true, got %+v", outcome.Payload)
	}

	outcome, err = sb.Invoke(binding.InvocationRequest{Callable: "regexp.match", Stage: binding.InvocationStageUnary, Args: []any{"^world", "hello world"}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != false {
		t.Fatalf("expected false, got %+v", outcome.Payload)
	}
}

func TestRegexpModule_Find(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := regexp.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	outcome, err := sb.Invoke(binding.InvocationRequest{Callable: "regexp.find", Stage: binding.InvocationStageUnary, Args: []any{"wo\\w+", "hello world"}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != "world" {
		t.Fatalf("expected world, got %+v", outcome.Payload)
	}
}

func TestRegexpModule_FindAll(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := regexp.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	outcome, err := sb.Invoke(binding.InvocationRequest{Callable: "regexp.findAll", Stage: binding.InvocationStageUnary, Args: []any{"\\b\\w{2}\\b", "aa bb cc dd"}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil {
		t.Fatal("expected payload, got nil")
	}
	items, ok := outcome.Payload.Value.([]string)
	if !ok {
		t.Fatalf("expected []string, got %T", outcome.Payload.Value)
	}
	if len(items) != 4 {
		t.Fatalf("expected 4 items, got %d: %v", len(items), items)
	}
}

func TestRegexpModule_Replace(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := regexp.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	outcome, err := sb.Invoke(binding.InvocationRequest{Callable: "regexp.replace", Stage: binding.InvocationStageUnary, Args: []any{"world", "hello world", "spore"}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != "hello spore" {
		t.Fatalf("expected hello spore, got %+v", outcome.Payload)
	}
}

func TestRegexpModule_Split(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := regexp.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	outcome, err := sb.Invoke(binding.InvocationRequest{Callable: "regexp.split", Stage: binding.InvocationStageUnary, Args: []any{",\\s*", "a, b, c"}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil {
		t.Fatal("expected payload, got nil")
	}
	items, ok := outcome.Payload.Value.([]string)
	if !ok {
		t.Fatalf("expected []string, got %T", outcome.Payload.Value)
	}
	if len(items) != 3 || items[0] != "a" || items[1] != "b" || items[2] != "c" {
		t.Fatalf("expected [a b c], got %v", items)
	}
}

func TestRegexpModule_InvalidPattern(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := regexp.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	outcome, err := sb.Invoke(binding.InvocationRequest{Callable: "regexp.find", Stage: binding.InvocationStageUnary, Args: []any{"[invalid", "hello"}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultError {
		t.Fatalf("expected error result, got %s", outcome.Result.Kind)
	}
}
