package binding

import (
	"fmt"

	"github.com/qomos-w/spore/diagnostics"
	"github.com/qomos-w/spore/schema"
)

// NewCapabilityExecutableAdapter exposes a capability callable as a flattened binding callable.
func NewCapabilityExecutableAdapter(capabilityName string, callable CapabilityCallable) (ExecutableAdapter, error) {
	if capabilityName == "" {
		return nil, fmt.Errorf("capability name cannot be empty")
	}
	if callable == nil {
		return nil, fmt.Errorf("capability %q requires callable", capabilityName)
	}
	local := callable.Desc()
	if local.Name == "" {
		return nil, fmt.Errorf("capability %q callable name cannot be empty", capabilityName)
	}
	if normalizedCallableMode(local) != schema.CallableModeUnary {
		return nil, fmt.Errorf("capability %q callable %q must be unary", capabilityName, local.Name)
	}
	flattened := cloneCapabilityCallableDesc(capabilityName, local)
	return &capabilityExecutableAdapter{
		desc:     flattened,
		callable: callable,
	}, nil
}

type capabilityExecutableAdapter struct {
	desc     schema.CallableDesc
	callable CapabilityCallable
}

func (a *capabilityExecutableAdapter) Callable() schema.CallableDesc {
	return schema.CloneCallableDesc(a.desc)
}

func (a *capabilityExecutableAdapter) Invoke(req InvocationRequest) (InvocationOutcome, error) {
	if req.Callable != a.desc.Name {
		return InvocationOutcome{}, fmt.Errorf("adapter for %q cannot handle %q", a.desc.Name, req.Callable)
	}
	if err := ValidateInvocationStage(a.desc, req.Stage); err != nil {
		return InvocationOutcome{}, err
	}
	if err := ValidateInvocationArgs(a.desc, req.Args); err != nil {
		return InvocationOutcome{}, err
	}
	var input any
	if len(req.Args) == 1 {
		input = req.Args[0]
	} else {
		input = req.Args
	}
	payload, err := a.callable.Invoke(req.Context, input)
	if err != nil {
		diag := diagnostics.FromError(err, diagnostics.Descriptor{
			Category: diagnostics.CategoryHost,
			Code:     "native_call_failed",
			Path:     "binding/capability/invoke",
			Message:  err.Error(),
		})
		result, descErr := NewInvocationErrorDescWithDiagnostic(a.desc, req.Stage, diag)
		if descErr != nil {
			return InvocationOutcome{}, descErr
		}
		return NewInvocationOutcome(result, nil)
	}
	result, err := DescribeInvocationResult(a.desc, req.Stage)
	if err != nil {
		return InvocationOutcome{}, err
	}
	return NewInvocationOutcome(result, payload)
}

func cloneCapabilityCallableDesc(capabilityName string, local schema.CallableDesc) schema.CallableDesc {
	flattened := schema.CloneCallableDesc(local)
	flattened.Name = capabilityCallableName(capabilityName, local.Name)
	return flattened
}

func capabilityCallableName(capabilityName, callableName string) string {
	return capabilityName + "." + callableName
}
