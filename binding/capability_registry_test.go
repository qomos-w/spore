package binding_test

import (
	"context"
	"testing"

	"github.com/qomos-w/spore/binding"
)

func TestMemoryCapabilityRegistryRegisterDescribeInvoke(t *testing.T) {
	builder := binding.NewCapability("tool", "service")
	if err := builder.AddFunction("execute", capabilityEcho); err != nil {
		t.Fatalf("AddFunction: %v", err)
	}
	cap, err := builder.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	reg := binding.NewMemoryCapabilityRegistry()
	if err := reg.Register(cap); err != nil {
		t.Fatalf("Register: %v", err)
	}
	desc, ok := reg.Describe("tool")
	if !ok {
		t.Fatal("expected tool capability")
	}
	if desc.Name != "tool" || len(desc.Callables) != 1 {
		t.Fatalf("unexpected descriptor: %#v", desc)
	}
	result, err := reg.Invoke(context.Background(), "tool", "execute", map[string]any{"Text": "hello"})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	out := result.(capabilityOutput)
	if out.Text != "hello" {
		t.Fatalf("expected hello, got %q", out.Text)
	}
}

func TestMemoryCapabilityRegistryRejectsDuplicateCapability(t *testing.T) {
	builder := binding.NewCapability("tool", "service")
	if err := builder.AddFunction("execute", capabilityEcho); err != nil {
		t.Fatalf("AddFunction: %v", err)
	}
	cap, err := builder.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	reg := binding.NewMemoryCapabilityRegistry()
	if err := reg.Register(cap); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := reg.Register(cap); err == nil {
		t.Fatal("expected duplicate capability error")
	}
}

func TestMemoryCapabilityRegistryRejectsDescriptorMismatch(t *testing.T) {
	builder := binding.NewCapability("tool", "service")
	if err := builder.AddFunction("execute", capabilityEcho); err != nil {
		t.Fatalf("AddFunction: %v", err)
	}
	cap, err := builder.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	cap.Desc.Callables[0].Name = "other"
	reg := binding.NewMemoryCapabilityRegistry()
	if err := reg.Register(cap); err == nil {
		t.Fatal("expected descriptor mismatch error")
	}
}

func TestMemoryCapabilityRegistryFindCallableAndDescribeAll(t *testing.T) {
	toolBuilder := binding.NewCapability("tool", "service")
	if err := toolBuilder.AddFunction("execute", capabilityEcho); err != nil {
		t.Fatalf("tool AddFunction: %v", err)
	}
	toolCap, err := toolBuilder.Build()
	if err != nil {
		t.Fatalf("tool Build: %v", err)
	}
	knowledgeBuilder := binding.NewCapability("knowledge", "service")
	if err := knowledgeBuilder.AddFunction("execute", capabilityEcho); err != nil {
		t.Fatalf("knowledge AddFunction: %v", err)
	}
	knowledgeCap, err := knowledgeBuilder.Build()
	if err != nil {
		t.Fatalf("knowledge Build: %v", err)
	}
	reg := binding.NewMemoryCapabilityRegistry()
	if err := reg.Register(toolCap); err != nil {
		t.Fatalf("Register tool: %v", err)
	}
	if err := reg.Register(knowledgeCap); err != nil {
		t.Fatalf("Register knowledge: %v", err)
	}
	callable, ok := reg.FindCallable("tool", "execute")
	if !ok {
		t.Fatal("expected tool.execute callable")
	}
	if callable.Desc().Name != "execute" {
		t.Fatalf("expected local callable name execute, got %s", callable.Desc().Name)
	}
	all := reg.DescribeAll()
	if len(all) != 2 || all[0].Name != "tool" || all[1].Name != "knowledge" {
		t.Fatalf("unexpected capability order: %#v", all)
	}
	all[0].Metadata = map[string]string{"changed": "yes"}
	again, _ := reg.Describe("tool")
	if len(again.Metadata) != 0 {
		t.Fatalf("expected descriptor clone isolation, got %#v", again.Metadata)
	}
}

func TestMemoryCapabilityRegistryRejectsUnknownTargets(t *testing.T) {
	reg := binding.NewMemoryCapabilityRegistry()
	if _, err := reg.Invoke(context.Background(), "tool", "execute", capabilityInput{}); err == nil {
		t.Fatal("expected unknown capability error")
	}
	builder := binding.NewCapability("tool", "service")
	if err := builder.AddFunction("execute", capabilityEcho); err != nil {
		t.Fatalf("AddFunction: %v", err)
	}
	cap, err := builder.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := reg.Register(cap); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := reg.Invoke(context.Background(), "tool", "missing", capabilityInput{}); err == nil {
		t.Fatal("expected unknown callable error")
	}
}

func TestMemoryCapabilityRegistryRejectsEmptyCallableName(t *testing.T) {
	builder := binding.NewCapability("tool", "service")
	if err := builder.AddFunction("execute", capabilityEcho); err != nil {
		t.Fatalf("AddFunction: %v", err)
	}
	cap, err := builder.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	cap.Callables[""] = cap.Callables["execute"]
	delete(cap.Callables, "execute")
	reg := binding.NewMemoryCapabilityRegistry()
	if err := reg.Register(cap); err == nil {
		t.Fatal("expected empty callable name rejection")
	}
}
