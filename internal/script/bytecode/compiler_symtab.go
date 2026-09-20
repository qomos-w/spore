// compiler_symtab.go manages the compiler's local slot table, instruction emission and diagnostic collection/registration.

package bytecode

import (
	"github.com/qomos-w/spore/diagnostics"
)

// --- Local variable management ---

func (c *compiler) addLocalWithType(name, typeName string) int {
	c.locals = append(c.locals, local{name: name, depth: c.scopeDepth, typeName: typeName})
	if len(c.locals) > c.peakLocals {
		c.peakLocals = len(c.locals)
	}
	return len(c.locals) - 1
}

func (c *compiler) addLocal(name string) int {
	return c.addLocalWithType(name, "")
}

func (c *compiler) resolveLocal(name string) int {
	for i := len(c.locals) - 1; i >= 0; i-- {
		if c.locals[i].name == name {
			return i
		}
	}
	return -1
}

func (c *compiler) removeLocals(depth int) {
	for len(c.locals) > 0 && c.locals[len(c.locals)-1].depth > depth {
		c.locals = c.locals[:len(c.locals)-1]
	}
}

// --- Helpers ---

func (c *compiler) emit(op opcode, operand int32, line int) int {
	c.curLine = line
	return c.chunk.addInstruction(op, operand, line)
}

func (c *compiler) addError(msg string) {
	c.addCompileError("compile_error", "bytecode/compiler", msg)
}

var compilerDiagnosticCodes = []diagnostics.CodeInfo{
	{Code: "compile_error", Category: diagnostics.CategorySchema, Description: "Bytecode compilation failed", Hint: "检查 bytecode 编译错误列表并修正对应源代码"},
	{Code: "type_alias_cycle", Category: diagnostics.CategorySchema, Description: "Type alias definitions formed a cycle", Hint: "打破 type alias 循环引用，使其最终指向具体类型"},
	{Code: "unknown_type_alias_target", Category: diagnostics.CategorySchema, Description: "Type alias referenced an unknown target type", Hint: "将 type alias 指向已声明的类型"},
	{Code: "override_without_parent", Category: diagnostics.CategorySchema, Description: "Override method was declared without a parent class", Hint: "为 override 方法声明可继承的父类，或移除 override"},
	{Code: "parent_class_not_found", Category: diagnostics.CategorySchema, Description: "Declared parent class could not be found", Hint: "确认父类名称正确且已在当前模块或导入模块中声明"},
	{Code: "override_signature_mismatch", Category: diagnostics.CategorySchema, Description: "Override method signature did not match the parent method", Hint: "将 override 方法的参数和返回类型改为与父类方法一致"},
	{Code: "override_parent_method_not_open", Category: diagnostics.CategorySchema, Description: "Parent method was not open for overriding", Hint: "将父类方法标记为 open，或移除子类 override"},
	{Code: "override_method_not_found", Category: diagnostics.CategorySchema, Description: "Override target method was not found on the parent class", Hint: "确认父类中存在同名方法，或移除 override"},
	{Code: "unknown_interface", Category: diagnostics.CategorySchema, Description: "Implemented interface could not be found", Hint: "确认 interface 名称正确且已声明或导入"},
	{Code: "interface_signature_mismatch", Category: diagnostics.CategorySchema, Description: "Class method signature did not satisfy an interface method", Hint: "将实现方法的参数和返回类型改为与 interface 定义一致"},
	{Code: "interface_method_missing", Category: diagnostics.CategorySchema, Description: "Class was missing a required interface method", Hint: "为 class 补充缺失的 interface 方法实现"},
	{Code: "super_without_parent", Category: diagnostics.CategorySchema, Description: "super was used without a parent class", Hint: "仅在存在父类的 class 中使用 super"},
	{Code: "break_outside_loop", Category: diagnostics.CategorySchema, Description: "break was used outside a loop", Hint: "将 break 放入循环体内，或改用 return/条件分支"},
	{Code: "continue_outside_loop", Category: diagnostics.CategorySchema, Description: "continue was used outside a loop", Hint: "将 continue 放入循环体内"},
	{Code: "undefined_imported_variable", Category: diagnostics.CategorySchema, Description: "Referenced imported variable was not found", Hint: "确认 import 的符号名称正确且已导出"},
	{Code: "undefined_variable", Category: diagnostics.CategorySchema, Description: "Referenced variable was not defined", Hint: "在使用前声明变量，或修正变量名"},
	{Code: "private_field_access_denied", Category: diagnostics.CategorySchema, Description: "Attempted to access a private field from an invalid context", Hint: "仅在类内部访问 private 字段，或调整字段可见性"},
	{Code: "private_method_access_denied", Category: diagnostics.CategorySchema, Description: "Attempted to call a private method from an invalid context", Hint: "仅在类内部调用 private 方法，或调整方法可见性"},
	{Code: "assign_imported_variable", Category: diagnostics.CategorySchema, Description: "Attempted to assign to an imported variable", Hint: "不要给 import 得到的变量重新赋值，改为写入本地变量"},
	{Code: "duplicate_struct_field", Category: diagnostics.CategorySchema, Description: "Struct literal provided the same field more than once", Hint: "在 struct 字面量中移除重复字段"},
	{Code: "unknown_struct_field", Category: diagnostics.CategorySchema, Description: "Struct literal referenced an unknown field", Hint: "将字段名改为 struct 中已声明的字段"},
	{Code: "struct_field_type_mismatch", Category: diagnostics.CategorySchema, Description: "Struct literal field type did not match the schema", Hint: "将 struct 字段值类型从 actual 改为 expected"},
	{Code: "missing_struct_field", Category: diagnostics.CategorySchema, Description: "Struct literal omitted a required field", Hint: "为 struct 字面量补充缺失的必填字段"},
	{Code: "unknown_struct_type", Category: diagnostics.CategorySchema, Description: "Struct literal referenced a struct type that was not declared", Hint: "将 struct 字面量类型名改为已声明的 struct，或在模块中先声明该 struct"},
	{Code: "inherit_non_open_class", Category: diagnostics.CategorySchema, Description: "Class attempted to inherit from a non-open class", Hint: "仅继承 open class，或移除 extends"},
	{Code: "override_visibility_narrowed", Category: diagnostics.CategorySchema, Description: "Override method narrowed parent method visibility", Hint: "不要让 override 方法以 private 收紧父类方法的可见性"},
	{Code: "method_visibility_narrowed", Category: diagnostics.CategorySchema, Description: "Method shadowed an ancestor method with narrower visibility", Hint: "不要以更窄的可见性覆盖祖先同名方法,或重命名当前方法"},
	{Code: "field_shadowing_disallowed", Category: diagnostics.CategorySchema, Description: "Field shadowed an ancestor field", Hint: "重命名当前类字段以避免与祖先字段同名"},
	{Code: "yield_requires_stream_fun", Category: diagnostics.CategorySchema, Description: "yield was used outside a stream fun", Hint: "仅在 stream fun 内使用 yield，或将函数声明为 stream"},
	{Code: "yield_disallowed_in_try", Category: diagnostics.CategorySchema, Description: "yield was used inside a try block", Hint: "将易错片段移入辅助 fun 并 yield 其结果；catch 上下文无法跨流挂起保存"},
	{Code: "yield_value_required", Category: diagnostics.CategorySchema, Description: "yield in a stream fun did not provide a value", Hint: "为 stream fun 的 yield 提供返回值"},
	{Code: "function_type_arity_mismatch", Category: diagnostics.CategorySchema, Description: "Function-typed value used with a mismatched parameter count", Hint: "使 lambda 参数个数与 fun 类型签名的参数个数一致"},
	{Code: "unknown_enum", Category: diagnostics.CategorySchema, Description: "Enum referenced in code was not declared", Hint: "确认 enum 名称正确且已在当前模块或导入模块中声明"},
	{Code: "unknown_enum_member", Category: diagnostics.CategorySchema, Description: "Enum member referenced was not part of the enum's closed set", Hint: "将成员名改为 enum 中已声明的成员"},
	{Code: "duplicate_enum_member", Category: diagnostics.CategorySchema, Description: "Enum declared the same member name more than once", Hint: "移除重复的 enum 成员名"},
	{Code: "duplicate_enum_member_value", Category: diagnostics.CategorySchema, Description: "Enum members shared the same underlying value", Hint: "为 enum 成员指定互不相同的底层值"},
	{Code: "enum_value_out_of_range", Category: diagnostics.CategorySchema, Description: "Enum member explicit value exceeded the int range", Hint: "将 enum 成员的显式赋值限制在 int32 范围内"},
	{Code: "function_type_param_mismatch", Category: diagnostics.CategorySchema, Description: "Lambda parameter type did not match the fun type signature", Hint: "将 lambda 参数类型改为与 fun 类型签名一致（或 any）"},
	{Code: "function_type_return_mismatch", Category: diagnostics.CategorySchema, Description: "Lambda return type did not match the fun type signature", Hint: "将 lambda 返回类型改为与 fun 类型签名一致（或 any）"},
	{Code: "function_type_arg_mismatch", Category: diagnostics.CategorySchema, Description: "Call argument did not match the fun type signature", Hint: "将调用实参类型改为与 fun 类型签名一致"},
	{Code: "function_type_mismatch", Category: diagnostics.CategorySchema, Description: "Value assigned to a function-typed slot was not compatible", Hint: "将赋值改为兼容的 lambda 或函数类型值"},
	{Code: "return_in_defer", Category: diagnostics.CategorySchema, Description: "return was used inside a defer body", Hint: "移除 defer 块内的 return；defer 会在函数退出时自动执行"},
	{Code: "yield_in_defer", Category: diagnostics.CategorySchema, Description: "yield was used inside a defer body", Hint: "移除 defer 块内的 yield；defer 块不能挂起执行"},
	{Code: "break_in_defer", Category: diagnostics.CategorySchema, Description: "break was used inside a defer body", Hint: "移除 defer 块内的 break；defer 块不属于任何循环"},
	{Code: "continue_in_defer", Category: diagnostics.CategorySchema, Description: "continue was used inside a defer body", Hint: "移除 defer 块内的 continue；defer 块不属于任何循环"},
	{Code: "defer_in_defer", Category: diagnostics.CategorySchema, Description: "defer was nested inside another defer body", Hint: "移除嵌套的 defer；defer 块内不支持再注册 defer"},
}

func init() {
	for _, info := range compilerDiagnosticCodes {
		diagnostics.RegisterCode(info)
	}
}

func (c *compiler) addCompileError(code, path, msg string) {
	c.errors = append(c.errors, compilerDiagnostic{code: code, message: msg, path: path, category: diagnostics.CategorySchema})
}

func (c *compiler) addCompileErrorWithTypes(code, path, msg, expected, actual string) {
	c.errors = append(c.errors, compilerDiagnostic{code: code, message: msg, path: path, category: diagnostics.CategorySchema, expected: expected, actual: actual})
}
