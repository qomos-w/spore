package uuid_test

import (
	"strings"
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/internal/script/bytecode"
	"github.com/qomos-w/spore/internal/script/frontend"
	"github.com/qomos-w/spore/std/uuid"
)

func TestUuidModule_VMEndToEnd(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := uuid.Register(sb); err != nil {
		t.Fatalf("Register: %v", err)
	}

	f, err := frontend.New(sb)
	if err != nil {
		t.Fatalf("frontend.New: %v", err)
	}
	vmEval := bytecode.NewVMEvaluator()
	vmEval.SetNativeBinding(sb)
	f.SetVMCompileHook(vmEval)
	f.SetModuleResolver(frontend.MapModuleResolver{})

	if err := f.LoadSource(`import v4 from "uuid"
fun getV4(): string { return v4() }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("getV4", nil)
	if err != nil {
		t.Fatalf("Invoke getV4: %v", err)
	}
	if outcome.Payload == nil {
		t.Fatal("expected payload, got nil")
	}
	s, ok := outcome.Payload.Value.(string)
	if !ok {
		t.Fatalf("expected string, got %T", outcome.Payload.Value)
	}
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
