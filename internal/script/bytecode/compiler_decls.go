// compiler_decls.go lowers frontend AST declarations into the compiler's internal declaration forms, registers top-level type information, and dispatches top-level declaration compilation.

package bytecode

import (
	"fmt"
	"math"
	"strings"

	"github.com/qomos-w/spore/internal/script/frontend"
)

func (c *compiler) registerTopLevelTypeInfo(decl topLevelDecl) {
	switch decl.kind {
	case topLevelDeclClass:
		c.classes[decl.class.name] = decl.class
	case topLevelDeclStruct:
		c.structs[decl.strct.name] = decl.strct
	case topLevelDeclEnum:
		c.enums[decl.enm.name] = decl.enm
		validateEnumMembers(decl.enumAST, decl.enm, func(code, path, msg string) {
			c.addCompileError(code, path, msg)
		})
	case topLevelDeclFunction:
		c.funcInfo[decl.fun.name] = &functionInfo{
			name:       decl.fun.name,
			paramCount: len(decl.fun.paramNames),
			paramTypes: append([]string(nil), decl.fun.paramTypes...),
			returnType: decl.fun.returnType,
		}
	case topLevelDeclVar:
		c.globalTypes[decl.vr.name] = c.resolveType(decl.vr.type_)
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

func topLevelStmtFromStatement(stmt frontend.Statement) topLevelStmt {
	switch s := stmt.(type) {
	case *frontend.FunStmt:
		return funStmtAdapter{stmt: s}
	case *frontend.StructStmt:
		return structStmtAdapter{stmt: s}
	case *frontend.EnumStmt:
		return enumStmtAdapter{stmt: s}
	case *frontend.ClassStmt:
		return classStmtAdapter{stmt: s}
	case *frontend.InterfaceStmt:
		return interfaceStmtAdapter{stmt: s}
	case *frontend.VarStmt:
		return varStmtAdapter{stmt: s}
	case *frontend.ExprStatement:
		// Top-level global assignment (ident = expr)
		if _, ok := s.Expr.(*frontend.AssignExpr); ok {
			return globalAssignStmtAdapter{stmt: s}
		}
		return nil
	case *frontend.TypeAliasStmt:
		return typeAliasStmtAdapter{stmt: s}
	default:
		return nil
	}
}

func localStmtFromStatement(stmt frontend.Statement) localStmt {
	switch s := stmt.(type) {
	case *frontend.VarStmt:
		return varStmtAdapterLocal{stmt: s}
	case *frontend.ReturnStmt:
		return returnStmtAdapter{stmt: s}
	case *frontend.YieldStmt:
		return yieldStmtAdapter{stmt: s}
	case *frontend.IfStmt:
		return ifStmtAdapter{stmt: s}
	case *frontend.WhileStmt:
		return whileStmtAdapter{stmt: s}
	case *frontend.ForStmt:
		return forStmtAdapter{stmt: s}
	case *frontend.WhenStmt:
		return whenStmtAdapter{stmt: s}
	case *frontend.BreakStmt:
		return breakStmtAdapter{}
	case *frontend.ContinueStmt:
		return continueStmtAdapter{}
	case *frontend.BlockStmt:
		return blockStmtAdapter{stmt: s}
	case *frontend.ExprStatement:
		return exprStmtAdapter{stmt: s}
	case *frontend.TryStmt:
		return tryStmtAdapter{stmt: s}
	case *frontend.DeferStmt:
		return deferStmtAdapter{stmt: s}
	default:
		return nil
	}
}

func altStmtFromStatement(stmt frontend.Statement) altStmt {
	switch s := stmt.(type) {
	case *frontend.IfStmt:
		return altIfStmtAdapter{stmt: s}
	case *frontend.BlockStmt:
		return altBlockStmtAdapter{stmt: s}
	default:
		return nil
	}
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

func topLevelDeclsFromProgram(prog *frontend.Program) []topLevelDecl {
	if prog == nil || len(prog.Stmts) == 0 {
		return nil
	}
	decls := make([]topLevelDecl, 0, len(prog.Stmts))
	for _, stmt := range prog.Stmts {
		switch s := topLevelStmtFromStatement(stmt).(type) {
		case funStmtAdapter:
			decls = append(decls, topLevelDecl{kind: topLevelDeclFunction, fun: funDeclFromStmt(s.stmt)})
		case structStmtAdapter:
			decls = append(decls, topLevelDecl{kind: topLevelDeclStruct, strct: structInfoFromStmt(s.stmt)})
		case enumStmtAdapter:
			decls = append(decls, topLevelDecl{kind: topLevelDeclEnum, enm: enumInfoFromStmt(s.stmt), enumAST: s.stmt})
		case classStmtAdapter:
			decls = append(decls, topLevelDecl{kind: topLevelDeclClass, class: classInfoFromStmt(s.stmt)})
		case interfaceStmtAdapter:
			decls = append(decls, topLevelDecl{kind: topLevelDeclInterface, iface: interfaceInfoFromStmt(s.stmt)})
		case globalAssignStmtAdapter:
			decls = append(decls, topLevelDecl{kind: topLevelDeclGlobalAssign, globalAssign: s.stmt})
		case varStmtAdapter:
			decls = append(decls, topLevelDecl{kind: topLevelDeclVar, vr: varDeclFromStmt(s.stmt)})
		case typeAliasStmtAdapter:
			decls = append(decls, topLevelDecl{kind: topLevelDeclTypeAlias, typeAlias: s.stmt})
		}
	}
	return decls
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

func funDeclFromStmt(fn *frontend.FunStmt) funDecl {
	paramNames := make([]string, len(fn.Params))
	paramTypes := make([]string, len(fn.Params))
	for i, p := range fn.Params {
		paramNames[i] = p.Name.Value
		if p.Type_ != nil {
			paramTypes[i] = typeAnnotationName(p.Type_)
		}
	}
	var returnType string
	if fn.ReturnType != nil {
		returnType = typeAnnotationName(fn.ReturnType)
	}
	return funDecl{
		name:       fn.Name.Value,
		paramNames: paramNames,
		paramTypes: paramTypes,
		body:       blockDeclFromBlock(fn.Body),
		exprBody:   fn.ExprBody,
		returnType: returnType,
		isStream:   fn.IsStream,
	}
}

func methodDeclFromStmt(method *frontend.FunStmt) methodDecl {
	paramNames := make([]string, len(method.Params))
	paramTypes := make([]string, len(method.Params))
	for i, p := range method.Params {
		paramNames[i] = p.Name.Value
		if p.Type_ != nil {
			paramTypes[i] = typeAnnotationName(p.Type_)
		}
	}
	access := accessPublic
	if method.Access != 0 {
		access = accessModifier(method.Access)
	}
	var returnType string
	if method.ReturnType != nil {
		returnType = typeAnnotationName(method.ReturnType)
	}
	return methodDecl{
		name:       method.Name.Value,
		paramNames: paramNames,
		paramTypes: paramTypes,
		body:       blockDeclFromBlock(method.Body),
		exprBody:   method.ExprBody,
		returnType: returnType,
		isOpen:     method.IsOpen,
		isOverride: method.IsOverride,
		access:     access,
	}
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

func varDeclFromStmt(v *frontend.VarStmt) varDecl {
	var typeName string
	if v.Type_ != nil {
		typeName = typeAnnotationName(v.Type_)
	}
	return varDecl{name: v.Name.Value, value: v.Value, type_: typeName, export: v.Exported}
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
	methods := make([]methodDecl, len(cl.Methods))
	for i, method := range cl.Methods {
		methods[i] = methodDeclFromStmt(method)
	}
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
	methods := make([]interfaceMethodSig, len(iface.Methods))
	for i, m := range iface.Methods {
		returnType := ""
		if m.ReturnType != nil {
			returnType = typeAnnotationName(m.ReturnType)
		}
		paramNames := make([]string, len(m.Params))
		paramTypes := make([]string, len(m.Params))
		for j, p := range m.Params {
			paramNames[j] = p.Name.Value
			if p.Type_ != nil {
				paramTypes[j] = typeAnnotationName(p.Type_)
			}
		}
		methods[i] = interfaceMethodSig{
			name:       m.Name.Value,
			paramCount: len(m.Params),
			returnType: returnType,
			hasDefault: m.Body != nil,
			paramNames: paramNames,
			paramTypes: paramTypes,
			body:       blockDeclFromBlock(m.Body),
		}
	}
	return interfaceInfo{
		name:    iface.Name.Value,
		methods: methods,
	}
}

func blockDeclFromBlock(block *frontend.BlockStmt) blockDecl {
	if block == nil || len(block.Stmts) == 0 {
		return blockDecl{}
	}
	stmts := make([]stmtDecl, len(block.Stmts))
	for i, stmt := range block.Stmts {
		stmts[i] = stmtDeclFromStatement(localStmtFromStatement(stmt))
	}
	return blockDecl{stmts: stmts}
}

func ifDeclFromStmt(s *frontend.IfStmt) ifDecl {
	decl := ifDecl{
		condition:   s.Condition,
		consequence: blockDeclFromBlock(s.Consequence),
	}
	if s.Alternative == nil {
		return decl
	}
	switch alt := altStmtFromStatement(s.Alternative).(type) {
	case altIfStmtAdapter:
		altDecl := ifDeclFromStmt(alt.stmt)
		decl.alternativeIf = &altDecl
	case altBlockStmtAdapter:
		altDecl := blockDeclFromBlock(alt.stmt)
		decl.alternativeBlk = &altDecl
	}
	return decl
}

func whileDeclFromStmt(s *frontend.WhileStmt) whileDecl {
	return whileDecl{
		condition: s.Condition,
		body:      blockDeclFromBlock(s.Body),
	}
}

func forDeclFromStmt(s *frontend.ForStmt) forDecl {
	decl := forDecl{
		condition: s.Condition,
		update:    s.Update,
		body:      blockDeclFromBlock(s.Body),
		isForIn:   s.IsForIn,
		variable:  s.Variable,
		iterable:  s.Iterable,
	}
	if s.Init != nil {
		initDecl := stmtDeclFromStatement(localStmtFromStatement(s.Init))
		decl.init = &initDecl
	}
	return decl
}

func whenDeclFromStmt(s *frontend.WhenStmt) whenDecl {
	decl := whenDecl{
		expr:  s.Expr,
		cases: make([]whenCaseDecl, len(s.Cases)),
	}
	for i, cc := range s.Cases {
		values := make([]frontend.Expression, len(cc.Values))
		copy(values, cc.Values)
		decl.cases[i] = whenCaseDecl{
			values:       values,
			variable:     cc.Variable,
			typeName:     "",
			hasTypeMatch: cc.TypeAnnotation != nil,
			guard:        cc.Guard,
			body:         blockDeclFromBlock(cc.Body),
		}
		if cc.TypeAnnotation != nil {
			decl.cases[i].typeName = cc.TypeAnnotation.Name
		}
	}
	if s.DefaultCase != nil {
		blk := blockDeclFromBlock(s.DefaultCase)
		decl.defaultCase = &blk
	}
	return decl
}

func stmtDeclFromStatement(stmt localStmt) stmtDecl {
	switch s := stmt.(type) {
	case varStmtAdapterLocal:
		return stmtDecl{kind: stmtKindVar, vr: varDeclFromStmt(s.stmt)}
	case returnStmtAdapter:
		return stmtDecl{kind: stmtKindReturn, ret: returnDecl{value: s.stmt.Value}}
	case yieldStmtAdapter:
		return stmtDecl{kind: stmtKindYield, yld: yieldDecl{value: s.stmt.Value}}
	case ifStmtAdapter:
		return stmtDecl{kind: stmtKindIf, if_: ifDeclFromStmt(s.stmt)}
	case whileStmtAdapter:
		return stmtDecl{kind: stmtKindWhile, whl: whileDeclFromStmt(s.stmt)}
	case forStmtAdapter:
		return stmtDecl{kind: stmtKindFor, for_: forDeclFromStmt(s.stmt)}
	case whenStmtAdapter:
		return stmtDecl{kind: stmtKindWhen, when: whenDeclFromStmt(s.stmt)}
	case breakStmtAdapter:
		return stmtDecl{kind: stmtKindBreak}
	case continueStmtAdapter:
		return stmtDecl{kind: stmtKindContinue}
	case blockStmtAdapter:
		return stmtDecl{kind: stmtKindBlock, blk: blockDeclFromBlock(s.stmt)}
	case exprStmtAdapter:
		return stmtDecl{kind: stmtKindExpr, expr: s.stmt.Expr}
	case tryStmtAdapter:
		return stmtDecl{kind: stmtKindTry, try_: tryDeclFromStmt(s.stmt)}
	case deferStmtAdapter:
		return stmtDecl{kind: stmtKindDefer, dfr: deferDeclFromStmt(s.stmt)}
	default:
		return stmtDecl{}
	}
}

func tryDeclFromStmt(s *frontend.TryStmt) tryDecl {
	decl := tryDecl{
		body:      blockDeclFromBlock(s.Body),
		catchBody: blockDeclFromBlock(s.CatchBody),
	}
	if s.CatchVar != nil {
		decl.catchVar = s.CatchVar.Value
	}
	return decl
}

func deferDeclFromStmt(s *frontend.DeferStmt) deferDecl {
	return deferDecl{body: blockDeclFromBlock(s.Body)}
}

func (c *compiler) compileTopLevelDecl(decl topLevelDecl, isLast bool) {
	switch decl.kind {
	case topLevelDeclFunction:
		c.validateSuperOutsideClass(decl.fun)
		c.compileFunDecl(decl.fun)
	case topLevelDeclStruct:
		c.compileStructDecl(decl.strct)
	case topLevelDeclEnum:
		// Enums are compile-time constants: registered in c.enums during
		// pass 1 and materialized into the VM registry by registerEnums.
		// Nothing to emit at top level.
	case topLevelDeclClass:
		c.compileClassDecl(decl.class)
		c.validateClassDecl(c.classes[decl.class.name])
	case topLevelDeclInterface:
		c.compileInterfaceDecl(decl.iface)
	case topLevelDeclVar:
		c.compileVarDecl(decl.vr)
	case topLevelDeclGlobalAssign:
		c.compileGlobalAssign(decl.globalAssign)
	case topLevelDeclTypeAlias:
		if decl.typeAlias != nil {
			c.registerTypeAlias(decl.typeAlias)
		}
	}
}
