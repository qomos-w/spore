// compiler_decls.go registers top-level type information, derives the compiler's
// resolved declaration records from the frontend AST (classInfo/structInfo/
// enumInfo/interfaceInfo + the thin adapter helpers), and dispatches top-level
// declaration compilation. No private copy of the frontend AST is kept.

package bytecode

import (
	"fmt"
	"math"
	"strings"

	"github.com/qomos-w/spore/internal/script/frontend"
)

// registerTopLevelTypeInfo performs pass-1 registration of top-level type
// information (classes, structs, enums, function signatures and global types)
// before any body is compiled. Adding a top-level form touches only this switch
// and compileTopLevelDecl.
func (c *compiler) registerTopLevelTypeInfo(stmt frontend.Statement) {
	switch s := stmt.(type) {
	case *frontend.ClassStmt:
		c.classes[s.Name.Value] = classInfoFromStmt(s)
	case *frontend.StructStmt:
		c.structs[s.Name.Value] = structInfoFromStmt(s)
	case *frontend.EnumStmt:
		info := enumInfoFromStmt(s)
		c.enums[info.name] = info
		validateEnumMembers(s, info, func(code, path, msg string) {
			c.addCompileError(code, path, msg)
		})
	case *frontend.FunStmt:
		c.funcInfo[s.Name.Value] = &functionInfo{
			name:       s.Name.Value,
			paramCount: len(s.Params),
			paramTypes: append([]string(nil), funParamTypes(s)...),
			returnType: funReturnType(s),
		}
	case *frontend.VarStmt:
		c.globalTypes[s.Name.Value] = c.resolveType(typeAnnotationName(s.Type_))
	}
}

func (c *compiler) validateTypeAliases() {
	pendingClasses := make(map[string]bool, len(c.classes))
	for name := range c.classes {
		pendingClasses[name] = true
	}
	pendingStructs := make(map[string]bool, len(c.structs))
	for name := range c.structs {
		pendingStructs[name] = true
	}
	pendingIfaces := make(map[string]bool, len(c.interfaces))
	for name := range c.interfaces {
		pendingIfaces[name] = true
	}
	pendingEnums := make(map[string]bool, len(c.enums))
	for name := range c.enums {
		pendingEnums[name] = true
	}
	for name, target := range c.typeAliases {
		if hasAliasCycle(name, c.typeAliases, map[string]bool{}) {
			c.addCompileError("type_alias_cycle", "bytecode/typealias", fmt.Sprintf("type alias %q participates in a cycle", name))
			continue
		}
		if !typeNameKnown(target, c.typeAliases, pendingClasses, pendingStructs, pendingIfaces, pendingEnums) {
			unknown := firstUnknownInGenericTarget(target, c.typeAliases, pendingClasses, pendingStructs, pendingIfaces, pendingEnums)
			if unknown != "" && unknown != target {
				c.addCompileError("unknown_type_alias_target", "bytecode/typealias", fmt.Sprintf("type alias %q references unknown type %q in %q", name, unknown, target))
			} else {
				c.addCompileError("unknown_type_alias_target", "bytecode/typealias", fmt.Sprintf("type alias %q references unknown type %q", name, target))
			}
		}
	}
}

func firstUnknownInGenericTarget(typeName string, aliases map[string]string, classes, structs, interfaces, enums map[string]bool) string {
	for _, part := range collectAliasTypeRefs(typeName) {
		if isBuiltinTypeName(part) {
			continue
		}
		if classes[part] || structs[part] || interfaces[part] || enums[part] {
			continue
		}
		if _, ok := aliases[part]; ok {
			continue
		}
		return part
	}
	return ""
}

func isBuiltinTypeName(name string) bool {
	switch name {
	case "any", "bool", "byte", "short", "ushort", "int", "uint", "long", "ulong", "float", "double", "string", "media", "void", "array", "map":
		return true
	}
	return false
}

func hasAliasCycle(name string, aliases map[string]string, seen map[string]bool) bool {
	target, ok := aliases[name]
	if !ok {
		return false
	}
	if seen[name] {
		return true
	}
	seen[name] = true
	for _, part := range collectAliasTypeRefs(target) {
		if part == name {
			return true
		}
		if _, ok := aliases[part]; ok && hasAliasCycle(part, aliases, seen) {
			return true
		}
	}
	delete(seen, name)
	return false
}

func collectAliasTypeRefs(typeName string) []string {
	typeName = strings.TrimSpace(typeName)
	if isFunTypeName(typeName) {
		if paramStr, retStr, ok := splitFunTypeName(typeName); ok {
			refs := make([]string, 0, 2)
			for _, arg := range splitTopLevelTypeArgs(paramStr) {
				refs = append(refs, collectAliasTypeRefs(arg)...)
			}
			refs = append(refs, collectAliasTypeRefs(retStr)...)
			return refs
		}
		return []string{typeName}
	}
	start := strings.Index(typeName, "<")
	end := strings.LastIndex(typeName, ">")
	if start < 0 || end <= start {
		return []string{typeName}
	}
	parts := []string{strings.TrimSpace(typeName[:start])}
	for _, arg := range splitTopLevelTypeArgs(typeName[start+1 : end]) {
		parts = append(parts, collectAliasTypeRefs(arg)...)
	}
	return parts
}

func typeNameKnown(typeName string, aliases map[string]string, classes, structs, interfaces, enums map[string]bool) bool {
	if typeName == "" {
		return false
	}
	typeName = strings.TrimSpace(typeName)

	if isFunTypeName(typeName) {
		paramStr, retStr, ok := splitFunTypeName(typeName)
		if !ok {
			return false
		}
		for _, arg := range splitTopLevelTypeArgs(paramStr) {
			if !typeNameKnown(arg, aliases, classes, structs, interfaces, enums) {
				return false
			}
		}
		return typeNameKnown(retStr, aliases, classes, structs, interfaces, enums)
	}

	if strings.HasPrefix(typeName, "array<") && strings.HasSuffix(typeName, ">") {
		inner := typeName[len("array<") : len(typeName)-1]
		args := splitTopLevelTypeArgs(inner)
		if len(args) != 1 {
			return false
		}
		return typeNameKnown(args[0], aliases, classes, structs, interfaces, enums)
	}
	if strings.HasPrefix(typeName, "map<") && strings.HasSuffix(typeName, ">") {
		inner := typeName[len("map<") : len(typeName)-1]
		args := splitTopLevelTypeArgs(inner)
		if len(args) != 2 {
			return false
		}
		return typeNameKnown(args[0], aliases, classes, structs, interfaces, enums) &&
			typeNameKnown(args[1], aliases, classes, structs, interfaces, enums)
	}

	if isBuiltinTypeName(typeName) {
		return true
	}
	if classes[typeName] || structs[typeName] || interfaces[typeName] || enums[typeName] {
		return true
	}
	if target, ok := aliases[typeName]; ok {
		return typeNameKnown(target, aliases, classes, structs, interfaces, enums)
	}
	return false
}

func (c *compiler) normalizeClassRelationships(className string) {
	info, ok := c.classes[className]
	if !ok {
		return
	}
	if info.parent != "" {
		if _, ok := c.classes[info.parent]; !ok {
			if _, isIface := c.interfaces[info.parent]; isIface {
				info.implements = append(info.implements, info.parent)
				info.parent = ""
				c.classes[className] = info
			}
		}
	}
}

// --- Thin frontend adapters ---
//
// The compiler reads the frontend AST directly; these helpers derive the few
// normalized views it needs (parameter names, parameter types, return type)
// without storing a copy of any node. Because no private declaration mirror is
// kept, adding a statement form requires only a case in compileStatement
// (compiler_stmt.go) — not a new adapter type plus a conversion switch.

// funParamNames returns the declared parameter names of a function or method.
func funParamNames(fn *frontend.FunStmt) []string {
	names := make([]string, len(fn.Params))
	for i, p := range fn.Params {
		names[i] = p.Name.Value
	}
	return names
}

// funParamTypes returns the declared parameter type names (aliases unresolved)
// of a function or method.
func funParamTypes(fn *frontend.FunStmt) []string {
	types := make([]string, len(fn.Params))
	for i, p := range fn.Params {
		types[i] = typeAnnotationName(p.Type_)
	}
	return types
}

// funReturnType returns the declared return type name of a function or method
// ("" when absent).
func funReturnType(fn *frontend.FunStmt) string { return typeAnnotationName(fn.ReturnType) }

// methodAccess normalizes the frontend access modifier to the compiler's.
func methodAccess(m *frontend.FunStmt) accessModifier {
	if m.Access != 0 {
		return accessPrivate
	}
	return accessPublic
}

// methodReturnsCompatible reports whether an override's return type matches the
// parent method's.
func methodReturnsCompatible(method, parentMethod *frontend.FunStmt) bool {
	return funReturnType(method) == funReturnType(parentMethod)
}

// interfaceMethodReturnType returns an interface method signature's return type
// name ("" when absent).
func interfaceMethodReturnType(m *frontend.MethodSignature) string {
	return typeAnnotationName(m.ReturnType)
}

func (c *compiler) retainModuleGlobals(before map[string]string, beforeTypes map[string]string) {
	if before == nil {
		return
	}
	for name := range c.globals {
		if _, existed := before[name]; existed {
			continue
		}
		if strings.HasPrefix(name, "__module_global_") {
			continue
		}
		delete(c.globals, name)
		delete(c.globalTypes, name)
	}
	for name, typeName := range beforeTypes {
		c.globalTypes[name] = typeName
	}
}

func (c *compiler) qualifyModuleFunctions(modulePath string, before map[string]*chunk) {
	if modulePath == "" {
		return
	}
	for name, chunk := range c.functions {
		if _, existed := before[name]; existed {
			continue
		}
		if strings.HasPrefix(name, "lambda$") {
			// Lambda chunks are internal: referenced only through closure
			// function refs, never by name. Keep their identities stable.
			continue
		}
		if dot := strings.Index(name, "."); dot > 0 {
			if _, isClassMethod := c.classes[name[:dot]]; isClassMethod {
				// Class method chunks are keyed `ClassName.methodName` and
				// looked up by that exact form in registerClasses, so they
				// must stay unqualified across module boundaries.
				continue
			}
		}
		qualifiedName := qualifiedModuleCallableName(modulePath, name)
		chunk.sourceName = qualifiedName
		delete(c.functions, name)
		c.functions[qualifiedName] = chunk
		if info := c.funcInfo[name]; info != nil {
			cloned := *info
			cloned.name = qualifiedName
			delete(c.funcInfo, name)
			c.funcInfo[qualifiedName] = &cloned
		}
	}
}

// topLevelStatements returns the top-level statements the compiler lowers, in
// source order. Bare expression statements that are not global assignments have
// no top-level meaning and are skipped, matching the historical declaration
// filter.
func topLevelStatements(prog *frontend.Program) []frontend.Statement {
	if prog == nil || len(prog.Stmts) == 0 {
		return nil
	}
	stmts := make([]frontend.Statement, 0, len(prog.Stmts))
	for _, stmt := range prog.Stmts {
		switch s := stmt.(type) {
		case *frontend.FunStmt, *frontend.StructStmt, *frontend.EnumStmt,
			*frontend.ClassStmt, *frontend.InterfaceStmt, *frontend.VarStmt,
			*frontend.TypeAliasStmt:
			stmts = append(stmts, stmt)
		case *frontend.ExprStatement:
			// Top-level global assignment (ident = expr).
			if _, ok := s.Expr.(*frontend.AssignExpr); ok {
				stmts = append(stmts, stmt)
			}
		}
	}
	return stmts
}

func collectTopLevelInterfaces(prog *frontend.Program) map[string]interfaceInfo {
	interfaces := make(map[string]interfaceInfo)
	if prog == nil {
		return interfaces
	}
	for _, stmt := range prog.Stmts {
		iface, ok := stmt.(*frontend.InterfaceStmt)
		if !ok {
			continue
		}
		info := interfaceInfoFromStmt(iface)
		interfaces[info.name] = info
	}
	return interfaces
}

func typeAnnotationName(t *frontend.TypeAnnotation) string {
	if t == nil {
		return ""
	}
	if t.IsFun {
		parts := make([]string, len(t.FunParams))
		for i, param := range t.FunParams {
			parts[i] = typeAnnotationName(param)
		}
		ret := "void"
		if t.FunReturn != nil {
			ret = typeAnnotationName(t.FunReturn)
		}
		return fmt.Sprintf("fun(%s):%s", strings.Join(parts, ","), ret)
	}
	if len(t.Params) == 0 {
		return t.Name
	}
	parts := make([]string, len(t.Params))
	for i, param := range t.Params {
		parts[i] = typeAnnotationName(param)
	}
	return fmt.Sprintf("%s<%s>", t.Name, strings.Join(parts, ","))
}

func structInfoFromStmt(s *frontend.StructStmt) structInfo {
	fields := make([]structFieldInfo, len(s.Fields))
	for i, field := range s.Fields {
		var typeName string
		if field.Type_ != nil {
			typeName = typeAnnotationName(field.Type_)
		}
		fields[i] = structFieldInfo{name: field.Name.Value, typeName: typeName}
	}
	return structInfo{name: s.Name.Value, fields: fields}
}

// enumInfoFromStmt resolves an enum declaration's member values: members
// without an explicit `= N` auto-increment from the previous member's value
// (starting at 0). Validation errors are reported by the caller through
// validateEnumMembers.
func enumInfoFromStmt(s *frontend.EnumStmt) enumInfo {
	info := enumInfo{name: s.Name.Value, members: make([]enumMemberInfo, 0, len(s.Members))}
	next := int64(0)
	for _, m := range s.Members {
		if m == nil || m.Name == nil {
			continue
		}
		if m.HasValue {
			next = m.Value
		}
		v := next
		if v < math.MinInt32 || v > math.MaxInt32 {
			v = 0 // clamped; reported as a compile error by validateEnumMembers
		}
		info.members = append(info.members, enumMemberInfo{name: m.Name.Value, value: int32(v)})
		next++
	}
	return info
}

// validateEnumMembers reports duplicate member names, out-of-range explicit
// values, and duplicate member values (ambiguous closed set).
func validateEnumMembers(s *frontend.EnumStmt, info enumInfo, report func(code, path, msg string)) {
	if s == nil {
		return
	}
	seenNames := make(map[string]bool, len(info.members))
	seenValues := make(map[int32]string, len(info.members))
	for _, m := range info.members {
		if seenNames[m.name] {
			report("duplicate_enum_member", "bytecode/enum", fmt.Sprintf("duplicate member %q in enum %s", m.name, info.name))
			continue
		}
		seenNames[m.name] = true
		if other, exists := seenValues[m.value]; exists {
			report("duplicate_enum_member_value", "bytecode/enum", fmt.Sprintf("members %q and %q of enum %s share value %d", other, m.name, info.name, m.value))
		} else {
			seenValues[m.value] = m.name
		}
	}
	for _, m := range s.Members {
		if m != nil && m.HasValue && (m.Value < math.MinInt32 || m.Value > math.MaxInt32) {
			report("enum_value_out_of_range", "bytecode/enum", fmt.Sprintf("enum %s member %s value %d exceeds int32 range", info.name, m.Name.Value, m.Value))
		}
	}
}

func classInfoFromStmt(cl *frontend.ClassStmt) classInfo {
	fields := make([]fieldInfo, len(cl.Fields))
	for i, field := range cl.Fields {
		access := accessPublic
		if field.Access == 1 { // accessPrivate from frontend
			access = accessPrivate
		}
		var typeName string
		if field.Type_ != nil {
			typeName = typeAnnotationName(field.Type_)
		}
		fields[i] = fieldInfo{name: field.Name.Value, access: access, typeName: typeName}
	}
	// Methods are kept as the frontend nodes themselves (copied into a fresh
	// slice so interface-default synthesis can append without mutating the AST).
	methods := append([]*frontend.FunStmt(nil), cl.Methods...)
	var parentName string
	if cl.Parent != nil {
		parentName = cl.Parent.Value
	}
	var implNames []string
	for _, iface := range cl.Implements {
		implNames = append(implNames, iface.Value)
	}
	return classInfo{
		name:       cl.Name.Value,
		parent:     parentName,
		isOpen:     cl.IsOpen,
		implements: implNames,
		fields:     fields,
		methods:    methods,
	}
}

func interfaceInfoFromStmt(iface *frontend.InterfaceStmt) interfaceInfo {
	// Method signatures are kept as the frontend nodes themselves; parameter
	// counts, return types and default bodies are read from the signature on
	// demand (compiler_register.go / compiler_validate.go / compiler_typedecl.go).
	methods := append([]*frontend.MethodSignature(nil), iface.Methods...)
	return interfaceInfo{
		name:    iface.Name.Value,
		methods: methods,
	}
}

// compileTopLevelDecl compiles one top-level statement and emits its code into
// the main chunk. Adding a top-level form touches only this switch (plus
// topLevelStatements and registerTopLevelTypeInfo).
func (c *compiler) compileTopLevelDecl(stmt frontend.Statement) {
	switch s := stmt.(type) {
	case *frontend.FunStmt:
		c.validateSuperOutsideClass(s)
		c.compileFunDecl(s)
	case *frontend.StructStmt:
		c.compileStructDecl(structInfoFromStmt(s))
	case *frontend.EnumStmt:
		// Enums are compile-time constants: registered in c.enums during
		// pass 1 and materialized into the VM registry by registerEnums.
		// Nothing to emit at top level.
	case *frontend.ClassStmt:
		// Read the resolved record back from c.classes so normalized
		// relationships and synthesized interface defaults are honored.
		c.compileClassDecl(c.classes[s.Name.Value])
		c.validateClassDecl(c.classes[s.Name.Value])
	case *frontend.InterfaceStmt:
		c.compileInterfaceDecl(c.interfaces[s.Name.Value])
	case *frontend.VarStmt:
		c.compileVarDecl(s)
	case *frontend.ExprStatement:
		c.compileGlobalAssign(s)
	case *frontend.TypeAliasStmt:
		c.registerTypeAlias(s)
	}
}
