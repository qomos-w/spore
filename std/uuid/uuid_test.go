package uuid_test

import (
	"strings"
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/std/uuid"
)

func TestUuidModule_RegistersAllFunctions(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := uuid.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	for _, name := range []string{"uuid.v4", "uuid.nil"} {
		_, ok := sb.Executors.Lookup(name)
		if !ok {
			t.Fatalf("expected %s to be registered", name)
		}
	}
}

func TestUuidModule_V4(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := uuid.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	outcome, err := sb.Invoke(binding.InvocationRequest{Callable: "uuid.v4", Stage: binding.InvocationStageUnary, Args: nil})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil {
		t.Fatal("expected payload, got nil")
	}
	s, ok := outcome.Payload.Value.(string)
	if !ok {
		t.Fatalf("expected string, got %T", outcome.Payload.Value)
	}
	// UUID v4 format: xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx
	if len(s) != 36 {
		t.Fatalf("expected length 36, got %d", len(s))
	}
	parts := strings.Split(s, "-")
	if len(parts) != 5 {
		t.Fatalf("expected 5 parts, got %d", len(parts))
	}
	if parts[2][0] != '4' {
		t.Fatalf("expected version 4, got %s", parts[2])
	}
}

func TestUuidModule_Nil(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := uuid.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	outcome, err := sb.Invoke(binding.InvocationRequest{Callable: "uuid.nil", Stage: binding.InvocationStageUnary, Args: nil})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != "00000000-0000-0000-0000-000000000000" {
		t.Fatalf("expected nil UUID, got %+v", outcome.Payload)
	}
}
