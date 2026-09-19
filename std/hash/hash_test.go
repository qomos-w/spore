package hash_test

import (
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/std/hash"
)

func TestHashModule_RegistersAllFunctions(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := hash.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	for _, name := range []string{"hash.md5", "hash.sha256"} {
		_, ok := sb.Executors.Lookup(name)
		if !ok {
			t.Fatalf("expected %s to be registered", name)
		}
	}
}

func TestHashModule_Md5(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := hash.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	outcome, err := sb.Invoke(binding.InvocationRequest{Callable: "hash.md5", Stage: binding.InvocationStageUnary, Args: []any{"hello"}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != "5d41402abc4b2a76b9719d911017c592" {
		t.Fatalf("expected md5 of hello, got %+v", outcome.Payload)
	}
}

func TestHashModule_Sha256(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := hash.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	outcome, err := sb.Invoke(binding.InvocationRequest{Callable: "hash.sha256", Stage: binding.InvocationStageUnary, Args: []any{"hello"}})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" {
		t.Fatalf("expected sha256 of hello, got %+v", outcome.Payload)
	}
}
