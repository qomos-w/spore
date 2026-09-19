package invoke

import "time"

// ExecutionBudget limits one invocation. A zero value leaves the
// corresponding limit disabled.
//
// Enforcement status of each field today:
//
//   - MaxInstructions: enforced. CheckExecution compares it against
//     ExecutionState.Instructions.
//   - MaxHostCalls: enforced. CheckExecution compares it against
//     ExecutionState.HostCalls.
//   - MaxDuration: enforced, but through the context rather than
//     ExecutionState. The invocation path derives the context from the duration
//     budget (binding.Registry.Invoke and script.CallContext.context
//     apply the same "earlier caller deadline wins, otherwise WithTimeout"
//     semantics); CheckExecution then surfaces the resulting context error once
//     the deadline elapses. Duration deliberately flows through ctx, so it is
//     never double-enforced.
//   - MaxOutputBytes: NOT enforced by the engine. It declares an output-size
//     ceiling that the host is responsible for enforcing after encoding the
//     result.
//
// Fields that were reserved but never enforced (MaxMemory, MaxRecursion) have
// been removed: they were inert, so their presence only over-promised the
// contract.
type ExecutionBudget struct {
	// MaxInstructions caps executed VM instructions per invocation.
	// Zero disables the limit. Enforced by CheckExecution.
	MaxInstructions uint64

	// MaxDuration caps the wall-clock duration of one invocation. Zero
	// disables the limit. The invocation path reflects it as a context
	// deadline (an earlier caller deadline is preserved); CheckExecution
	// surfaces the resulting context error.
	MaxDuration time.Duration

	// MaxHostCalls caps host/dispatch calls per invocation. Zero disables the
	// limit. Enforced by CheckExecution.
	MaxHostCalls uint32

	// MaxOutputBytes declares an output-size ceiling for one invocation. Zero
	// disables it. The engine does NOT enforce this limit; the host is
	// responsible for validating the encoded output size.
	MaxOutputBytes uint64
}
