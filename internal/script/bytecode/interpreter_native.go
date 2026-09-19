package bytecode

import (
	"context"
	"fmt"
	"github.com/qomos-w/spore/invoke"

	"github.com/qomos-w/spore/diagnostics"
	"github.com/qomos-w/spore/internal/script/vm"
)

func (interp *Interpreter) invokeNative(callable string, args []vm.Value) (vm.Value, bool, error) {
	if interp.execution != nil {
		interp.execution.HostCalls++
		if err := invoke.CheckExecution(interp.ctx, interp.budget, interp.execution); err != nil {
			return vm.EncodeInt(0), false, err
		}
	}
	if result, ok, err := interp.invokeNativeFastPath(callable, args); ok || err != nil {
		return result, ok, err
	}
	if interp.nativeInvoker == nil {
		return vm.EncodeInt(0), false, nil
	}
	releaseArgRoots := interp.vm_.AddTemporaryRoot(args...)
	defer releaseArgRoots()
	result, ok, err := interp.nativeInvoker.InvokeVMNative(context.Background(), callable, args)
	if err != nil {
		return vm.EncodeInt(0), ok, wrapNativeInvocationError(callable, interp.function, interp.lineForIP(), err)
	}
	return result, ok, nil
}

func (interp *Interpreter) invokeNativeFastPath(callable string, args []vm.Value) (vm.Value, bool, error) {
	if callable != "strings.join" || len(args) != 2 {
		return vm.EncodeInt(0), false, nil
	}
	result, ok := interp.vm_.JoinStringArray(args[0], args[1])
	if !ok {
		return vm.EncodeInt(0), false, nil
	}
	return result, true, nil
}

func (interp *Interpreter) invokeNativeMember(receiver vm.Value, methodName string, args []vm.Value) (vm.Value, bool, error) {
	if !interp.vm_.IsStringValue(receiver) {
		return vm.EncodeInt(0), false, nil
	}
	prefix := interp.vm_.DecodeString(receiver)
	if prefix == "" {
		return vm.EncodeInt(0), false, nil
	}
	return interp.invokeNative(prefix+"."+methodName, args)
}

func wrapNativeInvocationError(callable, current string, line int, err error) error {
	if rtErr, ok := err.(*RuntimeError); ok {
		if rtErr.Callable == "" {
			rtErr.Callable = current
		}
		if rtErr.Line == 0 {
			rtErr.Line = line
		}
		return rtErr
	}
	diag := diagnostics.FromError(err, diagnostics.Descriptor{
		Category: diagnostics.CategoryHost,
		Code:     "native_call_failed",
		Path:     "vm/call/native",
		Message:  fmt.Sprintf("native callable %s failed: %v", callable, err),
	})
	return &RuntimeError{
		Code:     diag.Code,
		Category: diag.Category,
		Callable: current,
		Line:     line,
		Path:     diag.Path,
		Message:  diag.Message,
		Stack:    diag.Stack,
		Cause:    diag.Cause,
	}
}
