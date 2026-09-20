// Package bytecode compiles Spore source into bytecode chunks and executes
// them on the internal/script/vm VM.
//
// # Error model: two tracks, one boundary
//
// The engine has exactly two failure channels, and which channel a condition
// belongs to is a contract, not an accident:
//
//   - Track 1 — invariants (panic). A VM internal invariant is violated:
//     the heap budget is exhausted, an object handle no longer resolves, a
//     class/struct/field descriptor is missing, a compiled body is invoked
//     with a mismatched arity. These are engine bugs or resource limits, not
//     script bugs; the vm package signals them with panic (vm.Panic,
//     vmPanic), and callers inside the vm package are allowed to assume a
//     consistent VM state afterwards.
//
//   - Track 2 — recoverable conditions (error). Everything a script author
//     can trigger and a host can act on: division by zero, a failed type
//     cast, an out-of-range index, a host callable that returned an error, a
//     conversion the codec does not support. These are *RuntimeError values
//     carrying a stable diagnostic code, and they are returned — never
//     panicked — so that script try/catch, the invocation envelope, and the
//     host's Result.Error all see the same structured object.
//
// The two tracks meet at exactly one place: recoverBytecodePanic, installed
// by every top-level entry point of this package (VMEvaluator.EvaluateContext,
// EvaluateUnaryInt, InvokeVMNative, executeNext, executeFinal,
// ResolveImportedNativeValue, and Interpreter.Execute). A panic that reaches
// that boundary — i.e. any Track 1 condition, and any Track 2 condition
// raised by a seam that has no error return — is converted into a structured
// *RuntimeError before it leaves the package:
//
//   - a plain panic value (string, vmPanic's *runtimeError, anything else)
//     becomes Code "vm_internal_panic", the stable code that tells a host
//     "the engine hit an invariant, do not blame the script";
//   - a *RuntimeError panic value is passed through unchanged, so the
//     original code/path/stack survive the round trip.
//
// The second case is the documented exception for the one seam that cannot
// carry an error: the vm package invokes registered function and class-method
// bodies through value-only signatures (vm.FunctionBody, vm.MethodImpl), so a
// *RuntimeError panic is the only way such a body can report failure without
// widening the vm API. See compiler_register.go and
// script/runtime_hostiface.go for the two producers, and note that this
// exception still bypasses script-level try/catch (a pre-existing property of
// the value-only seam, unrelated to the boundary conversion).
//
// The upshot for hosts: no bytecode entry point may be allowed to panic. A
// host embedding script.Runtime never needs a recover() around Call; the
// most it can see is a structured RuntimeError with code vm_internal_panic.
package bytecode
