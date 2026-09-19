package binding

import (
	"context"
	"fmt"
	"time"
)

// InvocationIdentity identifies the App and caller responsible for a host call.
type InvocationIdentity struct {
	AppID       string
	Runtime     string
	UserID      string
	Role        string
	ProjectID   string
	RequestID   string
	Permissions map[string]struct{}
}

// CapabilityPolicy describes the host-side policy for a capability.
type CapabilityPolicy struct {
	Permissions []string
	Roles       []string
	ProjectID   string
	Timeout     time.Duration
}

// AuthorizedInvocation is the policy-aware input to a capability call.
type AuthorizedInvocation struct {
	Context  context.Context
	Identity InvocationIdentity
	Budget   ExecutionBudget
}

func authorizeCapability(policy CapabilityPolicy, identity InvocationIdentity) error {
	if policy.ProjectID != "" && policy.ProjectID != identity.ProjectID {
		return fmt.Errorf("capability project scope denied")
	}
	if len(policy.Roles) > 0 {
		allowed := false
		for _, role := range policy.Roles {
			if role == identity.Role {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("capability role denied")
		}
	}
	for _, permission := range policy.Permissions {
		if _, ok := identity.Permissions[permission]; !ok {
			return fmt.Errorf("capability permission denied: %s", permission)
		}
	}
	return nil
}

func authorizedContext(inv AuthorizedInvocation, policy CapabilityPolicy) (context.Context, context.CancelFunc, error) {
	if err := authorizeCapability(policy, inv.Identity); err != nil {
		return nil, nil, err
	}
	ctx := inv.Context
	if ctx == nil {
		ctx = context.Background()
	}
	if policy.Timeout > 0 {
		child, cancel := context.WithTimeout(ctx, policy.Timeout)
		return child, cancel, nil
	}
	return ctx, func() {}, nil
}
