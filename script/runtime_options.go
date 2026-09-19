package script

import (
	"context"

	"github.com/qomos-w/spore/binding"
)

// ExecutionBudget limits one script invocation. A zero value leaves a limit
// disabled; see invoke.ExecutionBudget for the enforcement status of each
// field.
type ExecutionBudget = binding.ExecutionBudget

// CallContext describes the host cancellation and execution limits for one call.
type CallContext struct {
	Context context.Context
	Budget  ExecutionBudget
}

func (c CallContext) context() (context.Context, context.CancelFunc) {
	ctx := c.Context
	if ctx == nil {
		ctx = context.Background()
	}
	if c.Budget.MaxDuration > 0 {
		return context.WithTimeout(ctx, c.Budget.MaxDuration)
	}
	return ctx, func() {}
}
