package integration_test

import (
	"context"
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/internal/script/bytecode"
	"github.com/qomos-w/spore/internal/script/frontend"
)

type milestoneCapabilityInput struct {
	Text string
}

type milestoneCapabilityOutput struct {
	Text string
}

func milestoneCapabilityEcho(ctx context.Context, in milestoneCapabilityInput) (milestoneCapabilityOutput, error) {
	prefix, _ := ctx.Value("prefix").(string)
	return milestoneCapabilityOutput{Text: prefix + in.Text}, nil
}

func TestMilestone_CapabilityRegistrationAndInvocation(t *testing.T) {
	sb := binding.NewScriptBinding()
	builder := binding.NewCapability("tool", "service").WithMetadata("owner", "integration")
	if err := builder.AddFunction("execute", milestoneCapabilityEcho); err != nil {
		t.Fatalf("AddFunction: %v", err)
	}
	cap, err := builder.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := sb.RegisterCapability(cap); err != nil {
		t.Fatalf("RegisterCapability: %v", err)
	}
	descs := sb.DescribeCapabilities()
	if len(descs) != 1 || descs[0].Name != "tool" {
		t.Fatalf("unexpected capability descriptions: %#v", descs)
	}
	result, err := sb.InvokeCapability(context.WithValue(context.Background(), "prefix", "ok:"), "tool", "execute", map[string]any{"Text": "hello"})
	if err != nil {
		t.Fatalf("InvokeCapability: %v", err)
	}
	out, ok := result.(milestoneCapabilityOutput)
	if !ok {
		t.Fatalf("expected milestoneCapabilityOutput, got %T", result)
	}
	if out.Text != "ok:hello" {
		t.Fatalf("expected ok:hello, got %q", out.Text)
	}
}

func TestMilestone_CapabilityExposureAsBindingCallable(t *testing.T) {
	sb := binding.NewScriptBinding()
	builder := binding.NewCapability("tool", "service")
	if err := builder.AddFunction("execute", milestoneCapabilityEcho); err != nil {
		t.Fatalf("AddFunction: %v", err)
	}
	cap, err := builder.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := sb.RegisterCapability(cap); err != nil {
		t.Fatalf("RegisterCapability: %v", err)
	}
	if _, ok := sb.Callables.Lookup("tool.execute"); ok {
		t.Fatal("expected capability callable not to be exposed before explicit exposure")
	}
	if err := sb.ExposeCapabilityCallables("tool"); err != nil {
		t.Fatalf("ExposeCapabilityCallables: %v", err)
	}
	outcome, err := sb.Invoke(binding.InvocationRequest{
		Callable: "tool.execute",
		Stage:    binding.InvocationStageUnary,
		Args:     []any{map[string]any{"Text": "hello"}},
		Context:  context.WithValue(context.Background(), "prefix", "ok:"),
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	out, ok := outcome.Payload.Value.(milestoneCapabilityOutput)
	if !ok {
		t.Fatalf("expected milestoneCapabilityOutput, got %T", outcome.Payload.Value)
	}
	if out.Text != "ok:hello" {
		t.Fatalf("expected ok:hello, got %q", out.Text)
	}
}

func TestMilestone_CapabilityScriptNamespaceBridge(t *testing.T) {
	sb := binding.NewScriptBinding()
	builder := binding.NewCapability("tool", "service")
	if err := builder.AddFunction("execute", milestoneCapabilityEcho); err != nil {
		t.Fatalf("AddFunction: %v", err)
	}
	cap, err := builder.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := sb.RegisterCapability(cap); err != nil {
		t.Fatalf("RegisterCapability: %v", err)
	}
	if err := sb.ExposeCapabilityCallables("tool"); err != nil {
		t.Fatalf("ExposeCapabilityCallables: %v", err)
	}

	f, err := frontend.New(sb)
	if err != nil {
		t.Fatalf("frontend.New: %v", err)
	}
	vmEval := bytecode.NewVMEvaluator()
	vmEval.SetNativeBinding(sb)
	f.SetVMCompileHook(vmEval)
	if err := f.LoadSource(`fun call_tool(): string { return tool.execute({"Text": "hello"})["Text"] }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	outcome, err := sb.Invoke(binding.InvocationRequest{
		Callable: "call_tool",
		Stage:    binding.InvocationStageUnary,
	})
	if err != nil {
		t.Fatalf("Invoke script callable: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != "hello" {
		t.Fatalf("expected hello, got %#v", outcome.Payload)
	}
}

func TestMilestone_CapabilityScriptNamespaceShadowedByGlobalVariable(t *testing.T) {
	sb := binding.NewScriptBinding()
	builder := binding.NewCapability("tool", "service")
	if err := builder.AddFunction("execute", milestoneCapabilityEcho); err != nil {
		t.Fatalf("AddFunction: %v", err)
	}
	cap, err := builder.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := sb.RegisterCapability(cap); err != nil {
		t.Fatalf("RegisterCapability: %v", err)
	}
	if err := sb.ExposeCapabilityCallables("tool"); err != nil {
		t.Fatalf("ExposeCapabilityCallables: %v", err)
	}

	f, err := frontend.New(sb)
	if err != nil {
		t.Fatalf("frontend.New: %v", err)
	}
	vmEval := bytecode.NewVMEvaluator()
	vmEval.SetNativeBinding(sb)
	f.SetVMCompileHook(vmEval)
	if err := f.LoadSource(`var tool: int = 7 fun call_tool(): int { return tool }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	outcome, err := sb.Invoke(binding.InvocationRequest{
		Callable: "call_tool",
		Stage:    binding.InvocationStageUnary,
	})
	if err != nil {
		t.Fatalf("Invoke script callable: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 7 {
		t.Fatalf("expected 7, got %#v", outcome.Payload)
	}
}

func TestMilestone_CapabilityRegistrationAfterScriptCompile(t *testing.T) {
	sb := binding.NewScriptBinding()

	f, err := frontend.New(sb)
	if err != nil {
		t.Fatalf("frontend.New: %v", err)
	}
	vmEval := bytecode.NewVMEvaluator()
	vmEval.AllowNativeNamespace("tool")
	vmEval.SetNativeBinding(sb)
	f.SetVMCompileHook(vmEval)
	if err := f.LoadSource(`fun call_tool(): string { return tool.execute({"Text": "hello"})["Text"] }`); err != nil {
		t.Fatalf("LoadSource before registration: %v", err)
	}

	builder := binding.NewCapability("tool", "service")
	if err := builder.AddFunction("execute", func(ctx context.Context, in milestoneCapabilityInput) (milestoneCapabilityOutput, error) {
		prefix, _ := ctx.Value("prefix").(string)
		return milestoneCapabilityOutput{Text: prefix + in.Text}, nil
	}); err != nil {
		t.Fatalf("AddFunction: %v", err)
	}
	cap, err := builder.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := sb.RegisterCapability(cap); err != nil {
		t.Fatalf("RegisterCapability: %v", err)
	}
	if err := sb.ExposeCapabilityCallables("tool"); err != nil {
		t.Fatalf("ExposeCapabilityCallables: %v", err)
	}
	vmEval.SetNativeBinding(sb)

	outcome, err := sb.Invoke(binding.InvocationRequest{
		Callable: "call_tool",
		Stage:    binding.InvocationStageUnary,
		Context:  context.WithValue(context.Background(), "prefix", "ok:"),
	})
	if err != nil {
		t.Fatalf("Invoke after registration: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != "hello" {
		t.Fatalf("expected hello, got %#v", outcome.Payload)
	}
}
