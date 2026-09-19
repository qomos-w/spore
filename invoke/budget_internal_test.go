package invoke

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestCheckExecutionDurationFlowsThroughContext pins the contract that
// MaxDuration is enforced through the invocation context, not through
// ExecutionState: a live context passes, and an elapsed deadline surfaces
// unchanged as ctx.Err() (no duration-specific rewrite).
func TestCheckExecutionDurationFlowsThroughContext(t *testing.T) {
	budget := ExecutionBudget{MaxDuration: time.Minute}

	live, cancelLive := context.WithTimeout(context.Background(), time.Minute)
	defer cancelLive()
	if err := CheckExecution(live, budget, &ExecutionState{}); err != nil {
		t.Fatalf("live duration budget should pass: %v", err)
	}

	expired, cancelExpired := context.WithDeadline(context.Background(), time.Now().Add(-time.Millisecond))
	defer cancelExpired()
	if err := CheckExecution(expired, budget, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded passthrough, got %v", err)
	}
}

// TestCheckExecutionCancellationStillSurfacesCanceled guards that a caller
// cancellation with a duration budget declared is not mis-reported: the
// context error passes through untouched.
func TestCheckExecutionCancellationStillSurfacesCanceled(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := CheckExecution(cancelled, ExecutionBudget{MaxDuration: time.Minute}, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled passthrough, got %v", err)
	}
}
