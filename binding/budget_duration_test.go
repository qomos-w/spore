package binding

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/qomos-w/spore/schema"
)

// budgetProbeCallable records the deadline of the context it receives so tests
// can assert how the duration budget reached the capability boundary.
type budgetProbeCallable struct {
	desc        schema.CallableDesc
	deadline    time.Time
	hasDeadline bool
	block       bool
}

func (c *budgetProbeCallable) Desc() schema.CallableDesc { return c.desc }

func (c *budgetProbeCallable) Invoke(ctx context.Context, input any) (any, error) {
	c.deadline, c.hasDeadline = ctx.Deadline()
	if c.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return input, nil
}

func budgetProbeDesc() schema.CallableDesc {
	return schema.CallableDesc{
		Name:       "read",
		Parameters: []schema.ParameterDesc{{Name: "input", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}}},
		Returns:    []schema.TypeDesc{{Kind: schema.TypeKindScalar, Name: "string"}},
	}
}

// newBudgetProbeBinding registers a `budget` capability and exposes its `read`
// callable as the flattened binding callable "budget.read".
func newBudgetProbeBinding(t *testing.T) (*budgetProbeCallable, *ScriptBinding) {
	t.Helper()
	probe := &budgetProbeCallable{desc: budgetProbeDesc()}
	sb := NewScriptBinding()
	if err := sb.RegisterCapability(RegisteredCapability{
		Desc:      CapabilityDesc{Name: "budget", Callables: []schema.CallableDesc{probe.desc}},
		Callables: map[string]CapabilityCallable{"read": probe},
	}); err != nil {
		t.Fatalf("RegisterCapability: %v", err)
	}
	if err := sb.ExposeCapabilityCallables("budget"); err != nil {
		t.Fatalf("ExposeCapabilityCallables: %v", err)
	}
	return probe, sb
}

func newBudgetProbeRegistry(t *testing.T, probe *budgetProbeCallable, policy CapabilityPolicy) *Registry {
	t.Helper()
	registry := NewRegistry()
	if err := registry.RegisterCapability(RegisteredCapability{
		Desc:      CapabilityDesc{Name: "budget", Callables: []schema.CallableDesc{probe.desc}},
		Policy:    policy,
		Callables: map[string]CapabilityCallable{"read": probe},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	return registry
}

// TestBindingInvokeAppliesBudgetMaxDuration is the "trigger" path: a declared
// MaxDuration must reach the callable as a context deadline.
func TestBindingInvokeAppliesBudgetMaxDuration(t *testing.T) {
	probe, sb := newBudgetProbeBinding(t)
	start := time.Now()
	if _, err := sb.Invoke(InvocationRequest{
		Callable: "budget.read",
		Stage:    InvocationStageUnary,
		Args:     []any{"ok"},
		Budget:   ExecutionBudget{MaxDuration: time.Minute},
	}); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if !probe.hasDeadline {
		t.Fatal("expected the duration budget to become a context deadline")
	}
	if d := probe.deadline; d.Before(start.Add(time.Minute)) || d.After(start.Add(time.Minute+2*time.Second)) {
		t.Fatalf("deadline %v not within the expected budget window", d)
	}
}

// TestBindingInvokeWithoutBudgetLeavesContextUnbounded is the "no trigger"
// path: a zero MaxDuration must not introduce a deadline.
func TestBindingInvokeWithoutBudgetLeavesContextUnbounded(t *testing.T) {
	probe, sb := newBudgetProbeBinding(t)
	if _, err := sb.Invoke(InvocationRequest{
		Callable: "budget.read",
		Stage:    InvocationStageUnary,
		Args:     []any{"ok"},
	}); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if probe.hasDeadline {
		t.Fatalf("zero MaxDuration must not introduce a deadline, got %v", probe.deadline)
	}
}

// TestBindingInvokeKeepsEarlierCallerDeadline verifies the deadline semantics:
// an earlier caller deadline wins over a larger duration budget.
func TestBindingInvokeKeepsEarlierCallerDeadline(t *testing.T) {
	probe, sb := newBudgetProbeBinding(t)
	parent, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	parentDeadline, ok := parent.Deadline()
	if !ok {
		t.Fatal("expected parent deadline")
	}
	if _, err := sb.Invoke(InvocationRequest{
		Callable: "budget.read",
		Stage:    InvocationStageUnary,
		Args:     []any{"ok"},
		Context:  parent,
		Budget:   ExecutionBudget{MaxDuration: time.Hour},
	}); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if !probe.hasDeadline || !probe.deadline.Equal(parentDeadline) {
		t.Fatalf("expected earlier caller deadline %v, got %v (has=%v)", parentDeadline, probe.deadline, probe.hasDeadline)
	}
}

// TestBindingInvokeBudgetMaxDurationExpires confirms the budget actually fires:
// a blocking callable is cut off once the budget elapses.
func TestBindingInvokeBudgetMaxDurationExpires(t *testing.T) {
	probe, sb := newBudgetProbeBinding(t)
	probe.block = true
	outcome, err := sb.Invoke(InvocationRequest{
		Callable: "budget.read",
		Stage:    InvocationStageUnary,
		Args:     []any{"ok"},
		Budget:   ExecutionBudget{MaxDuration: 5 * time.Millisecond},
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Error == nil {
		t.Fatal("expected the elapsed duration budget to surface as an invocation error")
	}
	if !strings.Contains(outcome.Result.Error.Message, context.DeadlineExceeded.Error()) {
		t.Fatalf("expected deadline exceeded message, got %q", outcome.Result.Error.Message)
	}
}

// TestCapabilityRegistryFallsBackToBudgetMaxDuration covers capability_registry
// consuming Budget.MaxDuration when the policy declares no timeout.
func TestCapabilityRegistryFallsBackToBudgetMaxDuration(t *testing.T) {
	probe := &budgetProbeCallable{desc: budgetProbeDesc()}
	registry := newBudgetProbeRegistry(t, probe, CapabilityPolicy{})
	start := time.Now()
	if _, err := registry.InvokeCapabilityAuthorized(AuthorizedInvocation{Budget: ExecutionBudget{MaxDuration: time.Minute}}, "budget", "read", "ok"); err != nil {
		t.Fatalf("InvokeCapabilityAuthorized: %v", err)
	}
	if !probe.hasDeadline {
		t.Fatal("expected capability path to honor Budget.MaxDuration when policy has no timeout")
	}
	if d := probe.deadline; d.Before(start.Add(time.Minute)) || d.After(start.Add(time.Minute+2*time.Second)) {
		t.Fatalf("deadline %v not within the expected budget window", d)
	}
}

// TestCapabilityRegistryPolicyTimeoutWinsOverBudget confirms the fallback does
// not override an explicit capability policy timeout.
func TestCapabilityRegistryPolicyTimeoutWinsOverBudget(t *testing.T) {
	probe := &budgetProbeCallable{desc: budgetProbeDesc()}
	registry := newBudgetProbeRegistry(t, probe, CapabilityPolicy{Timeout: 30 * time.Millisecond})
	start := time.Now()
	if _, err := registry.InvokeCapabilityAuthorized(AuthorizedInvocation{Budget: ExecutionBudget{MaxDuration: time.Hour}}, "budget", "read", "ok"); err != nil {
		t.Fatalf("InvokeCapabilityAuthorized: %v", err)
	}
	if !probe.hasDeadline {
		t.Fatal("expected policy timeout deadline")
	}
	if d := probe.deadline; d.After(start.Add(time.Second)) {
		t.Fatalf("expected policy timeout (30ms) to win over the 1h budget, got %v", d)
	}
}

// TestCapabilityRegistryBudgetMaxDurationExpires confirms the fallback budget
// actually fires on the capability path.
func TestCapabilityRegistryBudgetMaxDurationExpires(t *testing.T) {
	probe := &budgetProbeCallable{desc: budgetProbeDesc(), block: true}
	registry := newBudgetProbeRegistry(t, probe, CapabilityPolicy{})
	_, err := registry.InvokeCapabilityAuthorized(AuthorizedInvocation{Budget: ExecutionBudget{MaxDuration: 5 * time.Millisecond}}, "budget", "read", "ok")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
}
