package binding

import (
	"context"
	"testing"
	"time"

	"github.com/qomos-w/spore/schema"
)

type policyTestCallable struct {
	desc schema.CallableDesc
	seen context.Context
}

func (c *policyTestCallable) Desc() schema.CallableDesc { return c.desc }
func (c *policyTestCallable) Invoke(ctx context.Context, input any) (any, error) {
	c.seen = ctx
	return input, nil
}

func TestRegistryInvokeCapabilityAuthorized(t *testing.T) {
	desc := schema.CallableDesc{Name: "read", Parameters: []schema.ParameterDesc{{Name: "input", Type: schema.TypeDesc{Kind: schema.TypeKindScalar}}}, Returns: []schema.TypeDesc{{Kind: schema.TypeKindScalar}}}
	callable := &policyTestCallable{desc: desc}
	registry := NewRegistry()
	if err := registry.RegisterCapability(RegisteredCapability{
		Desc:      CapabilityDesc{Name: "project", Callables: []schema.CallableDesc{desc}},
		Policy:    CapabilityPolicy{Permissions: []string{"project:read"}, Roles: []string{"agent"}, ProjectID: "p1", Timeout: time.Second},
		Callables: map[string]CapabilityCallable{"read": callable},
	}); err != nil {
		t.Fatal(err)
	}
	_, err := registry.InvokeCapabilityAuthorized(AuthorizedInvocation{Identity: InvocationIdentity{Role: "agent", ProjectID: "p1", Permissions: map[string]struct{}{"project:read": {}}}}, "project", "read", "ok")
	if err != nil {
		t.Fatalf("authorized invoke: %v", err)
	}
	if callable.seen == nil {
		t.Fatal("callable did not receive context")
	}
}

func TestRegistryInvokeCapabilityAuthorizedRejectsPermission(t *testing.T) {
	desc := schema.CallableDesc{Name: "read", Returns: []schema.TypeDesc{{Kind: schema.TypeKindScalar}}}
	registry := NewRegistry()
	if err := registry.RegisterCapability(RegisteredCapability{
		Desc:      CapabilityDesc{Name: "project", Callables: []schema.CallableDesc{desc}},
		Policy:    CapabilityPolicy{Permissions: []string{"project:read"}},
		Callables: map[string]CapabilityCallable{"read": &policyTestCallable{desc: desc}},
	}); err != nil {
		t.Fatal(err)
	}
	_, err := registry.InvokeCapabilityAuthorized(AuthorizedInvocation{Identity: InvocationIdentity{Permissions: map[string]struct{}{}}}, "project", "read", nil)
	if err == nil {
		t.Fatal("expected permission denial")
	}
}
