package strconv_test

import (
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/std/strconv"
)

func TestStrconvModule_RegistersAllFunctions(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := strconv.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	for _, name := range []string{"strconv.parseInt", "strconv.parseFloat", "strconv.formatInt", "strconv.formatFloat"} {
		_, ok := sb.Executors.Lookup(name)
		if !ok {
			t.Fatalf("expected %s to be registered", name)
		}
	}
}

func TestStrconvModule_ParseInt(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := strconv.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	outcome, err := sb.Invoke(binding.InvocationRequest{Callable: "strconv.parseInt", Stage: binding.InvocationStageUnary, Args: []any{"42"}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != int64(42) {
		t.Fatalf("expected 42, got %+v", outcome.Payload)
	}
}

func TestStrconvModule_ParseIntInvalid(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := strconv.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	outcome, err := sb.Invoke(binding.InvocationRequest{Callable: "strconv.parseInt", Stage: binding.InvocationStageUnary, Args: []any{"abc"}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultError {
		t.Fatalf("expected error result, got %s", outcome.Result.Kind)
	}
}

func TestStrconvModule_ParseFloat(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := strconv.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	outcome, err := sb.Invoke(binding.InvocationRequest{Callable: "strconv.parseFloat", Stage: binding.InvocationStageUnary, Args: []any{"3.14"}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 3.14 {
		t.Fatalf("expected 3.14, got %+v", outcome.Payload)
	}
}

func TestStrconvModule_FormatInt(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := strconv.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	outcome, err := sb.Invoke(binding.InvocationRequest{Callable: "strconv.formatInt", Stage: binding.InvocationStageUnary, Args: []any{int64(42)}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != "42" {
		t.Fatalf("expected 42, got %+v", outcome.Payload)
	}
}

func TestStrconvModule_FormatFloat(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := strconv.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	outcome, err := sb.Invoke(binding.InvocationRequest{Callable: "strconv.formatFloat", Stage: binding.InvocationStageUnary, Args: []any{3.14}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != "3.14" {
		t.Fatalf("expected 3.14, got %+v", outcome.Payload)
	}
}
