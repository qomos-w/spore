package invoke

import (
	"fmt"

	"github.com/qomos-w/spore/diagnostics"
	"github.com/qomos-w/spore/schema"
)

type ContractError struct {
	Code     string
	Callable string
	Stage    InvocationStage
	Path     string
	Message  string
	Expected string
	Actual   string
	Cause    error
}

const (
	CodeInvalidArgumentCount     = "invalid_argument_count"
	CodeNilArgument              = "nil_argument"
	CodeInvalidArgumentType      = "invalid_argument_type"
	CodeInvalidInvocationStage   = "invalid_invocation_stage"
	CodeMissingStreamNextSchema  = "missing_stream_next_schema"
	CodeMissingStreamFinalSchema = "missing_stream_final_schema"
	CodeUnsupportedCallableMode  = "unsupported_callable_mode"
)

func init() {
	diagnostics.RegisterCode(diagnostics.CodeInfo{Code: CodeInvalidArgumentCount, Category: diagnostics.CategoryContract, Description: "Callable received the wrong number of arguments", Hint: "调整参数数量：期望值见 expected，实际值见 actual"})
	diagnostics.RegisterCode(diagnostics.CodeInfo{Code: CodeNilArgument, Category: diagnostics.CategoryContract, Description: "Callable argument cannot be nil", Hint: "为该参数提供非 nil 值"})
	diagnostics.RegisterCode(diagnostics.CodeInfo{Code: CodeInvalidArgumentType, Category: diagnostics.CategoryContract, Description: "Callable argument type does not match schema", Hint: "将参数类型从 actual 调整为 expected"})
	diagnostics.RegisterCode(diagnostics.CodeInfo{Code: CodeInvalidInvocationStage, Category: diagnostics.CategoryContract, Description: "Invocation stage is not allowed for this callable", Hint: "改用 expected 指定的调用阶段"})
	diagnostics.RegisterCode(diagnostics.CodeInfo{Code: CodeMissingStreamNextSchema, Category: diagnostics.CategoryContract, Description: "Streaming callable is missing next schema", Hint: "为 streaming callable 补充 next schema"})
	diagnostics.RegisterCode(diagnostics.CodeInfo{Code: CodeMissingStreamFinalSchema, Category: diagnostics.CategoryContract, Description: "Streaming callable is missing final schema", Hint: "为 streaming callable 补充 final schema"})
	diagnostics.RegisterCode(diagnostics.CodeInfo{Code: CodeUnsupportedCallableMode, Category: diagnostics.CategoryContract, Description: "Callable mode is unsupported", Hint: "将 callable mode 改为 unary 或 streaming"})
	diagnostics.RegisterCode(diagnostics.CodeInfo{Code: schema.CodeMediaInlineTooLarge, Category: diagnostics.CategoryContract, Description: "Inline media data: URL exceeds the size limit", Hint: "改用 file: 或 https: 引用，或缩减 data: URL 至 1 MiB 以内"})
}

func (e *ContractError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func (e *ContractError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func (e *ContractError) DiagnosticCode() string {
	if e == nil {
		return ""
	}
	return e.Code
}

func (e *ContractError) DiagnosticCategory() diagnostics.Category {
	return diagnostics.CategoryContract
}
func (e *ContractError) DiagnosticPath() string {
	if e == nil {
		return ""
	}
	return e.Path
}

func (e *ContractError) DiagnosticStack() []diagnostics.Frame {
	if e == nil || e.Callable == "" {
		return nil
	}
	return []diagnostics.Frame{{Callable: e.Callable, Stage: string(e.Stage)}}
}

func (e *ContractError) DiagnosticCause() *diagnostics.Descriptor {
	if e == nil || e.Cause == nil {
		return nil
	}
	return &diagnostics.Descriptor{Category: diagnostics.CategoryContract, Message: e.Cause.Error()}
}
func (e *ContractError) DiagnosticExpected() string {
	if e == nil {
		return ""
	}
	return e.Expected
}
func (e *ContractError) DiagnosticActual() string {
	if e == nil {
		return ""
	}
	return e.Actual
}

func newContractError(code string, callable string, stage InvocationStage, path string, format string, args ...any) *ContractError {
	return &ContractError{Code: code, Callable: callable, Stage: stage, Path: path, Message: fmt.Sprintf(format, args...)}
}

func newContractErrorWithTypes(code string, callable string, stage InvocationStage, path string, expected string, actual string, format string, args ...any) *ContractError {
	return &ContractError{Code: code, Callable: callable, Stage: stage, Path: path, Expected: expected, Actual: actual, Message: fmt.Sprintf(format, args...)}
}
