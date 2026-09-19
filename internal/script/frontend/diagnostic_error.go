package frontend

import (
	"fmt"

	"github.com/qomos-w/spore/diagnostics"
)

type schemaValidationError struct {
	code     string
	message  string
	category diagnostics.Category
	span     diagnostics.Span
	path     string
	expected string
	actual   string
	cause    error
}

func (e *schemaValidationError) Error() string {
	if e == nil {
		return ""
	}
	return e.message
}

func (e *schemaValidationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func (e *schemaValidationError) DiagnosticCode() string {
	if e == nil {
		return ""
	}
	return e.code
}
func (e *schemaValidationError) DiagnosticCategory() diagnostics.Category {
	if e == nil || e.category == "" {
		return diagnostics.CategorySchema
	}
	return e.category
}
func (e *schemaValidationError) DiagnosticSpan() diagnostics.Span {
	if e == nil {
		return diagnostics.Span{}
	}
	return e.span
}
func (e *schemaValidationError) DiagnosticPath() string {
	if e == nil {
		return ""
	}
	return e.path
}
func (e *schemaValidationError) DiagnosticCause() *diagnostics.Descriptor {
	if e == nil || e.cause == nil {
		return nil
	}
	return &diagnostics.Descriptor{Category: diagnostics.CategorySchema, Message: e.cause.Error()}
}
func (e *schemaValidationError) DiagnosticExpected() string {
	if e == nil {
		return ""
	}
	return e.expected
}
func (e *schemaValidationError) DiagnosticActual() string {
	if e == nil {
		return ""
	}
	return e.actual
}

func newSchemaValidationError(code string, message string, span diagnostics.Span, path string) error {
	return &schemaValidationError{code: code, message: message, category: diagnostics.CategorySchema, span: span, path: path}
}

func spanFromFunStmt(stmt *funStmt) diagnostics.Span {
	if stmt == nil {
		return diagnostics.Span{}
	}
	line, col := stmt.pos()
	return diagnostics.Span{
		Start: diagnostics.Position{Line: line, Column: col},
		End:   diagnostics.Position{Line: line, Column: col},
	}
}

func newLoweringError(code string, message string, span diagnostics.Span, path string, cause error) error {
	return &schemaValidationError{code: code, message: message, category: diagnostics.CategorySchema, span: span, path: path, cause: cause}
}

func newSchemaValidationErrorWithTypes(code string, message string, span diagnostics.Span, path string, expected string, actual string) error {
	return &schemaValidationError{code: code, message: message, category: diagnostics.CategorySchema, span: span, path: path, expected: expected, actual: actual}
}

var validationDiagnosticCodes = []diagnostics.CodeInfo{
	{Code: "export_boundary_violation", Category: diagnostics.CategorySchema, Description: "Export boundary uses a disallowed class type", Hint: "将 export fun 边界类型改为 struct，而不是 class"},
	{Code: "callable_lowering_failed", Category: diagnostics.CategorySchema, Description: "Callable lowering to schema failed", Hint: "检查 callable 声明是否完整且类型可解析"},
	{Code: "stream_fun_requires_yield", Category: diagnostics.CategorySchema, Description: "Streaming function must contain yield", Hint: "在 stream fun 中至少添加一个 yield 表达式"},
	{Code: "yield_requires_stream_fun", Category: diagnostics.CategorySchema, Description: "Yield is only valid inside streaming functions", Hint: "将函数声明为 stream fun，或移除 yield"},
	{Code: "stream_expr_body_unsupported", Category: diagnostics.CategorySchema, Description: "Streaming callable cannot use expression body", Hint: "将表达式函数体改为块体，并显式 yield/return"},
	{Code: "stream_final_type_required", Category: diagnostics.CategorySchema, Description: "Streaming callable must declare final return type", Hint: "为 stream fun 补充最终返回类型"},
	{Code: "yield_value_required", Category: diagnostics.CategorySchema, Description: "Yield statement requires a value", Hint: "为 yield 提供与流元素类型一致的值"},
	{Code: "yield_type_mismatch", Category: diagnostics.CategorySchema, Description: "Yield values must have a consistent type", Hint: "将 yield 表达式类型统一为 expected"},
	{Code: "yield_type_inference_failed", Category: diagnostics.CategorySchema, Description: "Frontend could not infer yield type", Hint: "为相关表达式添加显式类型，或改为可推断的字面量/变量"},
	{Code: "array_literal_type_mismatch", Category: diagnostics.CategorySchema, Description: "Array literal elements must share one type", Hint: "将数组元素类型统一为 expected"},
	{Code: "map_literal_type_mismatch", Category: diagnostics.CategorySchema, Description: "Map literal keys and values must have consistent types", Hint: "将 map 的键或值类型统一为 expected"},
	{Code: "module_not_found", Category: diagnostics.CategoryLoad, Description: "Module resolver could not locate the requested module", Hint: "检查 import 路径是否正确，并确认 resolver 可返回该模块源码"},
	{Code: "missing_module_export", Category: diagnostics.CategoryLoad, Description: "Requested symbol is not exported by the target module", Hint: "将 import 名称改为目标模块已导出的符号，或在目标模块补充 export"},
	{Code: "cyclic_import", Category: diagnostics.CategoryLoad, Description: "Module import cycle prevented symbol resolution", Hint: "打破模块循环依赖，或仅在循环中保留类型导入"},
	{Code: "missing_module_resolver", Category: diagnostics.CategoryLoad, Description: "Frontend attempted module linking without a configured resolver", Hint: "在加载带 import 的源码前先调用 SetModuleResolver"},
	{Code: "invalid_module_path", Category: diagnostics.CategoryLoad, Description: "Module import path violates frontend path rules", Hint: "使用相对逻辑模块名，避免空路径、绝对路径和 .. 遍历"},
	{Code: "duplicate_import_binding", Category: diagnostics.CategoryLoad, Description: "Import binding collides with another imported or local name", Hint: "为 import 使用不同 alias，避免和本地声明或其他 import 重名"},
}

func init() {
	for _, info := range validationDiagnosticCodes {
		diagnostics.RegisterCode(info)
	}
}

func spanFromExpr(expr expression) diagnostics.Span {
	if expr == nil {
		return diagnostics.Span{}
	}
	line, col := expr.pos()
	return diagnostics.Span{Start: diagnostics.Position{Line: line, Column: col}, End: diagnostics.Position{Line: line, Column: col}}
}

func spanFromBlock(block *blockStmt) diagnostics.Span {
	if block == nil {
		return diagnostics.Span{}
	}
	line, col := block.pos()
	return diagnostics.Span{Start: diagnostics.Position{Line: line, Column: col}, End: diagnostics.Position{Line: line, Column: col}}
}

func newExportBoundaryError(stmt *funStmt, role string, detail string) error {
	span := diagnostics.Span{}
	if stmt != nil {
		span = spanFromFunStmt(stmt)
	}
	return newSchemaValidationError(
		"export_boundary_violation",
		fmt.Sprintf("export fun %s uses class type %q: schema boundary requires struct types only", role, detail),
		span,
		"export."+role,
	)
}
