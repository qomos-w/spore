package invoke

import "time"

// ExecutionBudget limits one invocation. A zero value disables a limit;
// MaxMemory is reserved for future VM accounting.
type ExecutionBudget struct {
	MaxInstructions uint64
	MaxDuration     time.Duration
	MaxMemory       uint64
	MaxHostCalls    uint32
	MaxOutputBytes  uint64
	MaxRecursion    uint32
}
