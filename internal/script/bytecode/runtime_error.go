package bytecode

import (
	"fmt"

	"github.com/qomos-w/spore/diagnostics"
)

// RuntimeError is a structured execution error from the bytecode VM.
// It carries a diagnostic code, callable context, source location, and
// type information so that callers (adapters, binding layer) can extract
// machine-readable diagnostics instead of parsing error strings.
type RuntimeError struct {
	Code     string // Stable diagnostic code (e.g., "type_cast_failed")
	Category diagnostics.Category
	Callable string // Function name where the error occurred
	Line     int    // Source line (0 if unavailable)
	Column   int
	Path     string
	Target   string // Target type name for type operations
	Message  string // Human-readable summary
	Expected string // Expected type/pattern for LLM-guided repair
	Actual   string // Actual type/pattern for LLM-guided repair
	Stack    []diagnostics.Frame
	Cause    *diagnostics.Descriptor
}

var runtimeErrorDiagnosticCodes = []diagnostics.CodeInfo{
	{Code: "type_cast_failed", Category: diagnostics.CategoryRuntime, Description: "A runtime type cast failed", Hint: "将值类型从 actual 转为 expected，或先做类型检查"},
	{Code: "stream_exhausted", Category: diagnostics.CategoryStream, Description: "Stream was exhausted before the requested stage", Hint: "检查 stream 调用顺序，并在 next/final 之前确认流未结束"},
	{Code: "stream_cancelled", Category: diagnostics.CategoryStream, Description: "Stream session was cancelled before completion", Hint: "取消后请使用全新 session 重启 stream，或在调用前先检查取消状态"},
	{Code: "division_by_zero", Category: diagnostics.CategoryRuntime, Description: "Division by zero occurred", Hint: "在除法前确保除数非零"},
	{Code: "modulo_by_zero", Category: diagnostics.CategoryRuntime, Description: "Modulo by zero occurred", Hint: "在取模前确保除数非零"},
	{Code: "stack_overflow", Category: diagnostics.CategoryRuntime, Description: "VM call stack exceeded its maximum depth", Hint: "减少递归深度，或为递归添加终止条件"},
	{Code: "native_call_failed", Category: diagnostics.CategoryHost, Description: "Native callable invocation failed", Hint: "检查 native callable 的输入、返回值和底层错误 cause"},
	{Code: "local_index_out_of_bounds", Category: diagnostics.CategoryRuntime, Description: "VM local load index was out of bounds", Hint: "检查局部变量声明和编译后的 local 索引"},
	{Code: "local_store_out_of_bounds", Category: diagnostics.CategoryRuntime, Description: "VM local store index was out of bounds", Hint: "检查赋值目标是否在当前作用域内声明"},
	{Code: "global_index_out_of_bounds", Category: diagnostics.CategoryRuntime, Description: "VM global load index was out of bounds", Hint: "检查全局变量是否已声明并成功编译"},
	{Code: "undefined_function", Category: diagnostics.CategoryRuntime, Description: "VM attempted to call an undefined function", Hint: "确认函数已声明、导入或注册到 VM"},
	{Code: "undefined_native_callable", Category: diagnostics.CategoryRuntime, Description: "VM attempted to call an undefined native callable", Hint: "确认 native callable 已在绑定层注册"},
	{Code: "class_not_found", Category: diagnostics.CategoryRuntime, Description: "VM could not find a class descriptor", Hint: "确认 class 已声明并被当前模块加载"},
	{Code: "invalid_map_key_type", Category: diagnostics.CategoryRuntime, Description: "Map key value had an invalid runtime type", Hint: "将 map key 改为 string 类型"},
	{Code: "map_key_not_found", Category: diagnostics.CategoryRuntime, Description: "Requested map key was not present", Hint: "访问前确认 map 中存在该 key，或提供默认值"},
	{Code: "invalid_array_index_type", Category: diagnostics.CategoryRuntime, Description: "Array index value had an invalid runtime type", Hint: "将 array 索引改为 int 类型"},
	{Code: "array_index_out_of_range", Category: diagnostics.CategoryRuntime, Description: "Array index was outside array bounds", Hint: "访问 array 前确认索引在 0 到 length-1 范围内"},
	{Code: "invalid_iterable_type", Category: diagnostics.CategoryRuntime, Description: "A for-in loop iterable was not an array, map, or string", Hint: "for-in 的可迭代对象必须是 array、map 或 string 类型"},
	{Code: "struct_not_found", Category: diagnostics.CategoryRuntime, Description: "VM could not find a struct descriptor", Hint: "确认 struct 已声明并被当前模块加载"},
	{Code: "unknown_opcode", Category: diagnostics.CategoryRuntime, Description: "VM encountered an unknown opcode", Hint: "检查 bytecode 编译器和解释器版本是否匹配"},
	{Code: "unsupported_vm_argument_type", Category: diagnostics.CategoryRuntime, Description: "Host value cannot be converted to a VM argument", Hint: "将参数改为 VM 支持的标量、数组、map 或结构体类型"},
	{Code: "native_value_index_out_of_bounds", Category: diagnostics.CategoryRuntime, Description: "VM native value load index was out of bounds", Hint: "检查 imported native value 槽位注册与编译结果是否一致"},
	{Code: "value_out_of_range", Category: diagnostics.CategoryRuntime, Description: "A value exceeded the range of its target type", Hint: "改用更宽的类型（如 long/double）或缩小数值范围"},
	{Code: "undefined_closure", Category: diagnostics.CategoryRuntime, Description: "VM could not resolve a closure reference", Hint: "确认闭包值未被拷贝到其解释器实例之外后调用"},
	{Code: "invalid_capture_cell", Category: diagnostics.CategoryRuntime, Description: "A local slot or capture operand did not hold a capture cell", Hint: "检查闭包捕获的变量是否已在声明后被 lambda 捕获"},
	{Code: "enum_not_registered", Category: diagnostics.CategoryRuntime, Description: "VM could not resolve an enum constant's runtime registration", Hint: "确认 enum 已声明并被当前模块加载（registerEnums）"},
	{Code: "invalid_constant", Category: diagnostics.CategoryRuntime, Description: "VM encountered an opcode operand with an unexpected constant kind", Hint: "检查 bytecode 编译器和解释器版本是否匹配"},
}

func init() {
	for _, info := range runtimeErrorDiagnosticCodes {
		diagnostics.RegisterCode(info)
	}
}

func (e *RuntimeError) Error() string {
	if e.Callable != "" {
		return fmt.Sprintf("callable %q: %s", e.Callable, e.Message)
	}
	return e.Message
}

// DiagnosticCode returns the stable diagnostic code for this error.
func (e *RuntimeError) DiagnosticCode() string { return e.Code }
func (e *RuntimeError) DiagnosticCategory() diagnostics.Category {
	if e == nil || e.Category == "" {
		return diagnostics.CategoryRuntime
	}
	return e.Category
}
func (e *RuntimeError) DiagnosticSpan() diagnostics.Span {
	if e == nil {
		return diagnostics.Span{}
	}
	return diagnostics.Span{Start: diagnostics.Position{Line: e.Line, Column: e.Column}, End: diagnostics.Position{Line: e.Line, Column: e.Column}}
}
func (e *RuntimeError) DiagnosticPath() string {
	if e == nil {
		return ""
	}
	return e.Path
}
func (e *RuntimeError) DiagnosticStack() []diagnostics.Frame {
	if e == nil {
		return nil
	}
	if len(e.Stack) > 0 {
		return append([]diagnostics.Frame(nil), e.Stack...)
	}
	if e.Callable == "" {
		return nil
	}
	return []diagnostics.Frame{{Callable: e.Callable, Span: e.DiagnosticSpan()}}
}
func (e *RuntimeError) DiagnosticCause() *diagnostics.Descriptor {
	if e == nil || e.Cause == nil {
		return nil
	}
	cloned := *e.Cause
	if len(e.Cause.Stack) > 0 {
		cloned.Stack = append([]diagnostics.Frame(nil), e.Cause.Stack...)
	}
	return &cloned
}
func (e *RuntimeError) DiagnosticExpected() string {
	if e == nil {
		return ""
	}
	return e.Expected
}
func (e *RuntimeError) DiagnosticActual() string {
	if e == nil {
		return ""
	}
	return e.Actual
}
