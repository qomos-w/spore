package bytecode

import (
	"fmt"
	"github.com/qomos-w/spore/invoke"
	"math"
	"sort"
	"strings"

	"github.com/qomos-w/spore/diagnostics"
	"github.com/qomos-w/spore/internal/script/frontend"
	"github.com/qomos-w/spore/internal/script/vm"
)

// compiler walks the AST and emits bytecode.
type compiler struct {
	chunk              *chunk
	locals             []local
	globals            map[string]int
	errors             []error
	loopStack          []loopContext
	functions          map[string]*chunk
	funcInfo           map[string]*functionInfo
	classes            map[string]classInfo
	structs            map[string]structInfo
	enums              map[string]enumInfo
	importedEnums      map[string]string // local alias → declared enum name
	interfaces         map[string]interfaceInfo
	typeAliases        map[string]string // alias name → resolved type name
	currentClassName   string            // name of the class currently being compiled
	typeHint           string            // type annotation hint for literal encoding ("long", "ulong", "double")
	returnTypeHint     string            // resolved function/method return type for block-body returns
	currentStreamFun   bool
	scopeDepth         int
	curLine            int
	peakLocals         int // max locals ever allocated in current function scope
	nativeCapabilities map[string]map[string]struct{}
	globalTypes        map[string]string
	importedCallables  map[string]string
	importedGlobals    map[string]string
	importedNatives    map[string]string
	nativeValueSlots   map[string]int
	nativeValues       map[string]vm.Value
	lambdaCounter      int             // unique lambda name counter for this compile unit
	capturedNames      map[string]bool // names captured by lambdas in the current function scope
	capturedOrder      []string        // deterministic capture order (must match lambda chunk layout)
	// optionalChainJumps collects the pending opJumpIfNull instructions
	// emitted for `?.` links of the optional chain currently being
	// compiled; compileOptionalChainExpr patches them to the chain end.
	optionalChainJumps []int
	handlerDepth       int // open try bodies in the code currently being compiled
	inDeferBody        int // >0 while compiling a defer body (rejects return/break/continue/yield)
}

type topLevelStmt interface {
	topLevelStmtNode()
}

type funStmtAdapter struct{ stmt *frontend.FunStmt }

type structStmtAdapter struct{ stmt *frontend.StructStmt }

type classStmtAdapter struct{ stmt *frontend.ClassStmt }

type interfaceStmtAdapter struct{ stmt *frontend.InterfaceStmt }

type varStmtAdapter struct{ stmt *frontend.VarStmt }

type globalAssignStmtAdapter struct{ stmt *frontend.ExprStatement }

type enumStmtAdapter struct{ stmt *frontend.EnumStmt }

type typeAliasStmtAdapter struct {
	stmt *frontend.TypeAliasStmt
}

func (funStmtAdapter) topLevelStmtNode()          {}
func (structStmtAdapter) topLevelStmtNode()       {}
func (classStmtAdapter) topLevelStmtNode()        {}
func (interfaceStmtAdapter) topLevelStmtNode()    {}
func (varStmtAdapter) topLevelStmtNode()          {}
func (globalAssignStmtAdapter) topLevelStmtNode() {}
func (enumStmtAdapter) topLevelStmtNode()         {}
func (typeAliasStmtAdapter) topLevelStmtNode()    {}

type localStmt interface {
	localStmtNode()
}

type altStmt interface {
	altStmtNode()
}

type varStmtAdapterLocal struct{ stmt *frontend.VarStmt }

type returnStmtAdapter struct{ stmt *frontend.ReturnStmt }

type yieldStmtAdapter struct{ stmt *frontend.YieldStmt }

type ifStmtAdapter struct{ stmt *frontend.IfStmt }

type whileStmtAdapter struct{ stmt *frontend.WhileStmt }

type forStmtAdapter struct{ stmt *frontend.ForStmt }

type whenStmtAdapter struct{ stmt *frontend.WhenStmt }

type breakStmtAdapter struct{}

type continueStmtAdapter struct{}

type blockStmtAdapter struct{ stmt *frontend.BlockStmt }

type exprStmtAdapter struct{ stmt *frontend.ExprStatement }

type altIfStmtAdapter struct{ stmt *frontend.IfStmt }

type altBlockStmtAdapter struct{ stmt *frontend.BlockStmt }

type tryStmtAdapter struct{ stmt *frontend.TryStmt }

type deferStmtAdapter struct{ stmt *frontend.DeferStmt }

func (varStmtAdapterLocal) localStmtNode() {}
func (returnStmtAdapter) localStmtNode()   {}
func (yieldStmtAdapter) localStmtNode()    {}
func (ifStmtAdapter) localStmtNode()       {}
func (whileStmtAdapter) localStmtNode()    {}
func (forStmtAdapter) localStmtNode()      {}
func (whenStmtAdapter) localStmtNode()     {}
func (breakStmtAdapter) localStmtNode()    {}
func (continueStmtAdapter) localStmtNode() {}
func (blockStmtAdapter) localStmtNode()    {}
func (exprStmtAdapter) localStmtNode()     {}

func (altIfStmtAdapter) altStmtNode()    {}
func (altBlockStmtAdapter) altStmtNode() {}

func (tryStmtAdapter) localStmtNode()   {}
func (deferStmtAdapter) localStmtNode() {}

type accessModifier int

const (
	accessPublic accessModifier = iota
	accessPrivate
)

type classInfo struct {
	name       string
	parent     string // parent class name (empty if none)
	isOpen     bool
	implements []string // interface names
	fields     []fieldInfo
	methods    []methodDecl
}

type fieldInfo struct {
	name     string
	access   accessModifier
	typeName string
}

type methodDecl struct {
	name       string
	paramNames []string
	paramTypes []string
	body       blockDecl
	exprBody   frontend.Expression
	returnType string
	isOpen     bool
	isOverride bool
	access     accessModifier
}

type structFieldInfo struct {
	name     string
	typeName string
}

type structInfo struct {
	name   string
	fields []structFieldInfo
}

// enumMemberInfo is one resolved enum member (name + underlying int value).
type enumMemberInfo struct {
	name  string
	value int32
}

// enumInfo is a resolved enum declaration: a closed set of int-valued members.
type enumInfo struct {
	name    string
	members []enumMemberInfo
}

// memberValue returns the underlying value for a member name.
func (e enumInfo) memberValue(member string) (int32, bool) {
	for i := range e.members {
		if e.members[i].name == member {
			return e.members[i].value, true
		}
	}
	return 0, false
}

type interfaceInfo struct {
	name    string
	methods []interfaceMethodSig
}

type interfaceMethodSig struct {
	name       string
	paramCount int
	returnType string
	// Default method body support (optional):
	hasDefault bool
	paramNames []string
	paramTypes []string
	body       blockDecl
}

type funDecl struct {
	name       string
	paramNames []string
	paramTypes []string
	body       blockDecl
	exprBody   frontend.Expression
	returnType string
	isStream   bool
}

func methodReturnsCompatible(method, parentMethod methodDecl) bool {
	return method.returnType == parentMethod.returnType
}

func (m methodDecl) returnTypeName() string {
	return m.returnType
}

type varDecl struct {
	name   string
	value  frontend.Expression
	type_  string // type annotation name ("long", "ulong", "double", etc.) for encoding selection
	export bool
}

type blockDecl struct {
	stmts []stmtDecl
}

type returnDecl struct {
	value frontend.Expression
}

type yieldDecl struct {
	value frontend.Expression
}

type ifDecl struct {
	condition      frontend.Expression
	consequence    blockDecl
	alternativeIf  *ifDecl
	alternativeBlk *blockDecl
}

type whileDecl struct {
	condition frontend.Expression
	body      blockDecl
}

type forDecl struct {
	init      *stmtDecl
	condition frontend.Expression
	update    frontend.Expression
	body      blockDecl
	isForIn   bool
	variable  string
	iterable  frontend.Expression
}

type whenCaseDecl struct {
	values       []frontend.Expression
	variable     string
	typeName     string
	hasTypeMatch bool
	guard        frontend.Expression
	body         blockDecl
}

type whenDecl struct {
	expr        frontend.Expression
	cases       []whenCaseDecl
	defaultCase *blockDecl
}

type tryDecl struct {
	body      blockDecl
	catchVar  string
	catchBody blockDecl
}

type deferDecl struct {
	body blockDecl
}

type stmtKind int

const (
	stmtKindVar stmtKind = iota
	stmtKindReturn
	stmtKindYield
	stmtKindIf
	stmtKindWhile
	stmtKindFor
	stmtKindWhen
	stmtKindBreak
	stmtKindContinue
	stmtKindBlock
	stmtKindExpr
	stmtKindTry
	stmtKindDefer
)

type stmtDecl struct {
	kind stmtKind
	vr   varDecl
	ret  returnDecl
	yld  yieldDecl
	if_  ifDecl
	whl  whileDecl
	for_ forDecl
	when whenDecl
	blk  blockDecl
	expr frontend.Expression
	try_ tryDecl
	dfr  deferDecl
}

type topLevelDeclKind int

const (
	topLevelDeclFunction topLevelDeclKind = iota
	topLevelDeclStruct
	topLevelDeclEnum
	topLevelDeclClass
	topLevelDeclInterface
	topLevelDeclVar
	topLevelDeclGlobalAssign
	topLevelDeclTypeAlias
)

type topLevelDecl struct {
	kind         topLevelDeclKind
	fun          funDecl
	class        classInfo
	strct        structInfo
	enm          enumInfo
	enumAST      *frontend.EnumStmt
	iface        interfaceInfo
	vr           varDecl
	globalAssign *frontend.ExprStatement
	typeAlias    *frontend.TypeAliasStmt
}

type local struct {
	name     string
	depth    int
	typeName string
	isCell   bool // local slot holds a capture-cell reference (shared with closures)
}

type loopContext struct {
	start         int
	breakJumps    []int
	continueJumps []int
	continuePos   int // resolved target for continue; 0 means not yet set
	handlerDepth  int // open try handlers at loop entry; break/continue pops back to this
}

type memberLookup struct {
	owner      string
	access     accessModifier
	typeName   string
	returnType string
	methodDecl methodDecl
	fieldInfo  fieldInfo
}

type functionInfo struct {
	name       string
	paramCount int
	paramTypes []string // declared parameter types (serialized, aliases unresolved)
	localCount int
	returnType string
}

type compilerDiagnostic struct {
	code     string
	message  string
	path     string
	category diagnostics.Category
	expected string
	actual   string
}

func (e compilerDiagnostic) Error() string { return e.message }
func (e compilerDiagnostic) DiagnosticCode() string {
	return e.code
}
func (e compilerDiagnostic) DiagnosticPath() string {
	return e.path
}
func (e compilerDiagnostic) DiagnosticCategory() diagnostics.Category {
	return e.category
}
func (e compilerDiagnostic) DiagnosticExpected() string { return e.expected }
func (e compilerDiagnostic) DiagnosticActual() string   { return e.actual }

func newCompiler() *compiler {
	return &compiler{
		chunk:              newChunk(),
		locals:             make([]local, 0),
		globals:            make(map[string]int),
		errors:             make([]error, 0),
		loopStack:          make([]loopContext, 0),
		functions:          make(map[string]*chunk),
		funcInfo:           make(map[string]*functionInfo),
		classes:            make(map[string]classInfo),
		structs:            make(map[string]structInfo),
		enums:              make(map[string]enumInfo),
		importedEnums:      make(map[string]string),
		interfaces:         make(map[string]interfaceInfo),
		typeAliases:        make(map[string]string),
		nativeCapabilities: make(map[string]map[string]struct{}),
		globalTypes:        make(map[string]string),
		importedCallables:  make(map[string]string),
		importedGlobals:    make(map[string]string),
		importedNatives:    make(map[string]string),
		nativeValueSlots:   make(map[string]int),
		nativeValues:       make(map[string]vm.Value),
	}
}

// compile compiles a program AST into a main chunk and function chunks.
func (c *compiler) compile(prog *frontend.Program) (*chunk, error) {
	return c.compileProgram(prog, "")
}

func (c *compiler) compileModule(path string, prog *frontend.Program) (*chunk, error) {
	return c.compileProgram(prog, path)
}

func (c *compiler) compileProgram(prog *frontend.Program, modulePath string) (*chunk, error) {
	decls := topLevelDeclsFromProgram(prog)
	c.interfaces = collectTopLevelInterfaces(prog)
	for _, decl := range decls {
		if decl.kind == topLevelDeclTypeAlias && decl.typeAlias != nil {
			c.registerTypeAlias(decl.typeAlias)
		}
	}
	for _, decl := range decls {
		c.registerTopLevelTypeInfo(decl)
	}
	var previousGlobals map[string]string
	var previousGlobalTypes map[string]string
	var previousFunctions map[string]*chunk
	if modulePath != "" {
		previousGlobals = make(map[string]string, len(c.globals))
		for name := range c.globals {
			previousGlobals[name] = c.globalTypes[name]
		}
		previousGlobalTypes = make(map[string]string, len(c.globalTypes))
		for name, typeName := range c.globalTypes {
			previousGlobalTypes[name] = typeName
		}
		previousFunctions = make(map[string]*chunk, len(c.functions))
		for name, chunk := range c.functions {
			previousFunctions[name] = chunk
		}
		for i, decl := range decls {
			if decl.kind == topLevelDeclVar && decl.vr.export {
				originalName := decl.vr.name
				qualifiedName := qualifiedModuleGlobalName(modulePath, originalName)
				c.globalTypes[qualifiedName] = c.globalTypes[originalName]
				delete(c.globalTypes, originalName)
				decl.vr.name = qualifiedName
				decls[i] = decl
			}
		}
	}
	c.validateTypeAliases()
	for i, decl := range decls {
		if decl.kind == topLevelDeclClass {
			c.normalizeClassRelationships(decl.class.name)
			decl.class = c.classes[decl.class.name]
			decls[i] = decl
		}
	}
	// Inherit interface default method bodies into implementing classes that
	// do not provide (or inherit) the method themselves. Runs after all class
	// relationships are normalized and in topological order (parents first) so
	// subclass synthesis sees defaults already inherited by ancestors.
	for _, className := range c.topologicalClassOrder() {
		c.synthesizeInterfaceDefaultMethods(className)
	}
	for i, decl := range decls {
		if decl.kind == topLevelDeclClass {
			decl.class = c.classes[decl.class.name]
			decls[i] = decl
		}
	}
	for i, decl := range decls {
		isLast := i == len(decls)-1
		c.compileTopLevelDecl(decl, isLast)
	}
	if modulePath != "" {
		c.retainModuleGlobals(previousGlobals, previousGlobalTypes)
		c.qualifyModuleFunctions(modulePath, previousFunctions)
	}
	c.emit(opHalt, 0, 0)

	if len(c.errors) > 0 {
		if len(c.errors) == 1 {
			return nil, c.errors[0]
		}
		messages := make([]string, len(c.errors))
		for i, err := range c.errors {
			messages[i] = err.Error()
		}
		return nil, compilerDiagnostic{
			code:     "multiple_compile_diagnostics",
			message:  fmt.Sprintf("compilation errors: [%s]", strings.Join(messages, "; ")),
			path:     "bytecode/compiler",
			category: diagnostics.CategorySchema,
		}
	}
	return c.chunk, nil
}

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

func (c *compiler) validateSuperOutsideClass(fn funDecl) {
	if len(fn.body.stmts) == 0 && fn.exprBody == nil {
		return
	}
	method := methodDecl{name: fn.name, body: fn.body, exprBody: fn.exprBody}
	c.validateSuperUsage(classInfo{name: fn.name, parent: ""}, method)
}

func (c *compiler) validateClassDecl(info classInfo) {
	c.validateFieldShadowing(info)
	for _, method := range info.methods {
		if method.isOverride && info.parent == "" {
			c.addCompileError("override_without_parent", "bytecode/oop/override", fmt.Sprintf("method %q in class %q is marked override but class has no parent", method.name, info.name))
		}
		if method.isOverride && method.access == accessPrivate {
			c.addCompileError("override_visibility_narrowed", "bytecode/oop/override", fmt.Sprintf("method %q in class %q cannot override with private visibility", method.name, info.name))
		}
		if !method.isOverride {
			if ancestor, ok := c.lookupMethodOnAncestor(info.parent, method.name); ok && method.access == accessPrivate {
				c.addCompileError("method_visibility_narrowed", "bytecode/oop/override", fmt.Sprintf("method %q in class %q shadows ancestor method from class %q with private visibility", method.name, info.name, ancestor.owner))
			}
		}
		if info.parent != "" {
			parentInfo, ok := c.classes[info.parent]
			if !ok {
				if _, isIface := c.interfaces[info.parent]; !isIface {
					c.addCompileError("parent_class_not_found", "bytecode/oop/inheritance", fmt.Sprintf("parent class %q not found for %q", info.parent, info.name))
				}
			} else {
				for _, parentMethod := range parentInfo.methods {
					if parentMethod.name != method.name {
						continue
					}
					if method.isOverride {
						if len(parentMethod.paramNames) != len(method.paramNames) {
							c.addCompileErrorWithTypes("override_signature_mismatch", "bytecode/oop/override", fmt.Sprintf("cannot override method %q: parameter count %d does not match parent count %d", method.name, len(method.paramNames), len(parentMethod.paramNames)), fmt.Sprintf("%d parameter(s)", len(parentMethod.paramNames)), fmt.Sprintf("%d parameter(s)", len(method.paramNames)))
						} else if !methodReturnsCompatible(method, parentMethod) {
							c.addCompileErrorWithTypes("override_signature_mismatch", "bytecode/oop/override", fmt.Sprintf("cannot override method %q: return type %q does not match parent return type %q", method.name, method.returnTypeName(), parentMethod.returnTypeName()), parentMethod.returnTypeName(), method.returnTypeName())
						}
						if !parentMethod.isOpen {
							c.addCompileError("override_parent_method_not_open", "bytecode/oop/override", fmt.Sprintf("cannot override method %q: parent method in class %q is not open", method.name, info.parent))
						}
					}
				}
			}
		}
		if method.exprBody == nil && method.body.stmts == nil {
			continue
		}
		c.validateSuperUsage(info, method)
	}
	for _, ifaceName := range info.implements {
		iface, ok := c.interfaces[ifaceName]
		if !ok {
			c.addCompileError("unknown_interface", "bytecode/oop/interface", fmt.Sprintf("class %q implements unknown interface %q", info.name, ifaceName))
			continue
		}
		for _, ifaceMethod := range iface.methods {
			found := false
			for _, classMethod := range info.methods {
				if classMethod.name == ifaceMethod.name {
					found = true
					if len(classMethod.paramNames) != ifaceMethod.paramCount {
						c.addCompileErrorWithTypes("interface_signature_mismatch", "bytecode/oop/interface", fmt.Sprintf("class %q implements interface %q but method %q has parameter count %d, expected %d", info.name, ifaceName, ifaceMethod.name, len(classMethod.paramNames), ifaceMethod.paramCount), fmt.Sprintf("%d parameter(s)", ifaceMethod.paramCount), fmt.Sprintf("%d parameter(s)", len(classMethod.paramNames)))
					}
					if ifaceMethod.returnType != "" && classMethod.returnType != ifaceMethod.returnType {
						c.addCompileErrorWithTypes("interface_signature_mismatch", "bytecode/oop/interface", fmt.Sprintf("class %q implements interface %q but method %q returns %q, expected %q", info.name, ifaceName, ifaceMethod.name, classMethod.returnType, ifaceMethod.returnType), ifaceMethod.returnType, classMethod.returnType)
					}
					break
				}
			}
			if found {
				continue
			}
			if info.parent != "" {
				if parentInfo, ok := c.classes[info.parent]; ok {
					for _, parentMethod := range parentInfo.methods {
						if parentMethod.name != ifaceMethod.name {
							continue
						}
						found = true
						if len(parentMethod.paramNames) != ifaceMethod.paramCount {
							c.addCompileErrorWithTypes("interface_signature_mismatch", "bytecode/oop/interface", fmt.Sprintf("class %q inherits method %q from %q with parameter count %d, expected %d for interface %q", info.name, ifaceMethod.name, info.parent, len(parentMethod.paramNames), ifaceMethod.paramCount, ifaceName), fmt.Sprintf("%d parameter(s)", ifaceMethod.paramCount), fmt.Sprintf("%d parameter(s)", len(parentMethod.paramNames)))
						}
						if ifaceMethod.returnType != "" && parentMethod.returnType != ifaceMethod.returnType {
							c.addCompileErrorWithTypes("interface_signature_mismatch", "bytecode/oop/interface", fmt.Sprintf("class %q inherits method %q from %q returning %q, expected %q for interface %q", info.name, ifaceMethod.name, info.parent, parentMethod.returnType, ifaceMethod.returnType, ifaceName), ifaceMethod.returnType, parentMethod.returnType)
						}
						break
					}
				}
			}
			if !found {
				c.addCompileError("interface_method_missing", "bytecode/oop/interface", fmt.Sprintf("class %q implements interface %q but is missing method %q", info.name, ifaceName, ifaceMethod.name))
			}
		}
	}
}

func (c *compiler) validateFieldShadowing(info classInfo) {
	if info.parent == "" {
		return
	}
	for _, field := range info.fields {
		className := info.parent
		for className != "" {
			parent, ok := c.classes[className]
			if !ok {
				break
			}
			for _, parentField := range parent.fields {
				if parentField.name == field.name {
					c.addCompileError("field_shadowing_disallowed", "bytecode/oop/field", fmt.Sprintf("field %q in class %q shadows field from ancestor class %q", field.name, info.name, parent.name))
					return
				}
			}
			className = parent.parent
		}
	}
}

func (c *compiler) validateSuperUsage(info classInfo, method methodDecl) {
	walkExpression := func(expr frontend.Expression, visit func(frontend.Expression)) {}
	var walk func(frontend.Expression)
	walk = func(expr frontend.Expression) {
		if expr == nil {
			return
		}
		switch e := expr.(type) {
		case *frontend.SuperExpr:
			if info.parent == "" {
				c.addCompileError("super_without_parent", "bytecode/oop/super", fmt.Sprintf("super can only be used in class %q when a parent class exists", info.name))
				return
			}
			if e.Method == nil {
				return
			}
		case *frontend.BinaryExpr:
			walk(e.Left)
			walk(e.Right)
		case *frontend.UnaryExpr:
			walk(e.Right)
		case *frontend.CallExpr:
			walk(e.Callee)
			for _, arg := range e.Arguments {
				walk(arg)
			}
		case *frontend.MemberExpr:
			walk(e.Object)
		case *frontend.NullCoalesceExpr:
			walk(e.Left)
			walk(e.Right)
		case *frontend.OptionalChainExpr:
			walk(e.Expr)
		case *frontend.IndexExpr:
			walk(e.Left)
			walk(e.Index)
		case *frontend.AssignExpr:
			walk(e.Target)
			walk(e.Value)
		case *frontend.TypeCheckExpr:
			walk(e.Left)
		case *frontend.TypeCastExpr:
			walk(e.Left)
		case *frontend.ArrayLiteral:
			for _, elem := range e.Elements {
				walk(elem)
			}
		case *frontend.MapLiteral:
			for _, pair := range e.Pairs {
				walk(pair.Key)
				walk(pair.Value)
			}
		case *frontend.NewExpr:
			for _, arg := range e.Arguments {
				walk(arg)
			}
		case *frontend.StructLiteral:
			for _, field := range e.Fields {
				walk(field.Value)
			}
		case *frontend.ThisExpr, *frontend.IdentExpr, *frontend.IntLiteral, *frontend.FloatLiteral, *frontend.StringLiteral, *frontend.BoolLiteral, *frontend.NullLiteral:
			return
		}
	}
	var walkStmt func(stmt stmtDecl)
	walkStmt = func(stmt stmtDecl) {
		switch stmt.kind {
		case stmtKindVar:
			walk(stmt.vr.value)
		case stmtKindReturn:
			walk(stmt.ret.value)
		case stmtKindYield:
			walk(stmt.yld.value)
		case stmtKindIf:
			walk(stmt.if_.condition)
			for _, nested := range stmt.if_.consequence.stmts {
				walkStmt(nested)
			}
			if stmt.if_.alternativeIf != nil {
				for _, nested := range stmt.if_.alternativeIf.consequence.stmts {
					walkStmt(nested)
				}
			}
			if stmt.if_.alternativeBlk != nil {
				for _, nested := range stmt.if_.alternativeBlk.stmts {
					walkStmt(nested)
				}
			}
		case stmtKindWhile:
			walk(stmt.whl.condition)
			for _, nested := range stmt.whl.body.stmts {
				walkStmt(nested)
			}
		case stmtKindFor:
			if stmt.for_.init != nil {
				walkStmt(*stmt.for_.init)
			}
			walk(stmt.for_.condition)
			walk(stmt.for_.update)
			walk(stmt.for_.iterable)
			for _, nested := range stmt.for_.body.stmts {
				walkStmt(nested)
			}
		case stmtKindWhen:
			walk(stmt.when.expr)
			for _, cc := range stmt.when.cases {
				for _, value := range cc.values {
					walk(value)
				}
				for _, nested := range cc.body.stmts {
					walkStmt(nested)
				}
			}
			if stmt.when.defaultCase != nil {
				for _, nested := range stmt.when.defaultCase.stmts {
					walkStmt(nested)
				}
			}
		case stmtKindBlock:
			for _, nested := range stmt.blk.stmts {
				walkStmt(nested)
			}
		case stmtKindExpr:
			walk(stmt.expr)
		case stmtKindTry:
			for _, nested := range stmt.try_.body.stmts {
				walkStmt(nested)
			}
			for _, nested := range stmt.try_.catchBody.stmts {
				walkStmt(nested)
			}
		case stmtKindDefer:
			for _, nested := range stmt.dfr.body.stmts {
				walkStmt(nested)
			}
		}
	}
	_ = walkExpression
	walk(method.exprBody)
	for _, stmt := range method.body.stmts {
		walkStmt(stmt)
	}
}

// --- Function compilation ---

func (c *compiler) compileFunDecl(fn funDecl) {
	funcName := fn.name

	// Save current compiler state.
	savedChunk := c.chunk
	savedLocals := c.locals
	savedDepth := c.scopeDepth
	savedReturnHint := c.returnTypeHint
	savedStreamFun := c.currentStreamFun
	savedCapturedNames := c.capturedNames
	savedCapturedOrder := c.capturedOrder
	savedHandlerDepth := c.handlerDepth
	savedInDeferBody := c.inDeferBody

	// Create a new chunk for this function.
	c.chunk = newChunk()
	c.chunk.sourceName = funcName
	c.locals = make([]local, 0)
	c.peakLocals = 0
	c.scopeDepth = 1
	c.handlerDepth = 0
	c.inDeferBody = 0

	// Pre-compute which locals/params are captured by lambdas in the body.
	c.capturedNames, c.capturedOrder = computeFunctionCaptures(fn.paramNames, fn.body, false, nil)

	// Register parameters as locals at function scope.
	for i, name := range fn.paramNames {
		typeName := ""
		if i < len(fn.paramTypes) {
			typeName = c.resolveType(fn.paramTypes[i])
		}
		c.addLocalWithType(name, typeName)
	}
	// Parameters captured by lambdas must be boxed into capture cells at
	// entry so mutations stay shared between the function and its closures.
	c.boxCapturedParams(fn.paramNames)
	c.returnTypeHint = ""
	c.currentStreamFun = fn.isStream
	if resolved := c.resolveType(fn.returnType); resolved == "long" || resolved == "ulong" || resolved == "double" {
		c.returnTypeHint = resolved
	}

	// Compile the body.
	if len(fn.body.stmts) > 0 {
		c.compileBlock(fn.body)
	} else if fn.exprBody != nil {
		// Expression body: fun f(): int = expr → compile as return <expr>.
		prevHint := c.typeHint
		resolved := c.resolveType(fn.returnType)
		if resolved == "long" || resolved == "ulong" || resolved == "double" {
			c.typeHint = resolved
		} else {
			c.typeHint = ""
		}
		c.compileExpression(fn.exprBody)
		c.typeHint = prevHint
		c.emit(opReturn, 0, c.curLine)
		// Store the function chunk.
		c.functions[funcName] = c.chunk
		c.funcInfo[funcName] = &functionInfo{
			name:       funcName,
			paramCount: len(fn.paramNames),
			paramTypes: append([]string(nil), fn.paramTypes...),
			localCount: c.peakLocals,
			returnType: fn.returnType,
		}
		c.chunk.LocalCount = c.peakLocals
		c.chunk = savedChunk
		c.locals = savedLocals
		c.scopeDepth = savedDepth
		c.returnTypeHint = savedReturnHint
		c.currentStreamFun = savedStreamFun
		c.capturedNames = savedCapturedNames
		c.capturedOrder = savedCapturedOrder
		c.handlerDepth = savedHandlerDepth
		c.inDeferBody = savedInDeferBody
		return
	}

	// Ensure there's a return at the end.
	c.emit(opReturnVoid, 0, c.curLine)
	// Store the function chunk.
	c.functions[funcName] = c.chunk
	c.funcInfo[funcName] = &functionInfo{
		name:       funcName,
		paramCount: len(fn.paramNames),
		paramTypes: append([]string(nil), fn.paramTypes...),
		localCount: c.peakLocals,
		returnType: fn.returnType,
	}
	c.chunk.LocalCount = c.peakLocals

	// Restore compiler state.
	c.chunk = savedChunk
	c.locals = savedLocals
	c.scopeDepth = savedDepth
	c.returnTypeHint = savedReturnHint
	c.currentStreamFun = savedStreamFun
	c.capturedNames = savedCapturedNames
	c.capturedOrder = savedCapturedOrder
	c.handlerDepth = savedHandlerDepth
	c.inDeferBody = savedInDeferBody
}

// boxCapturedParams wraps parameters that are captured by lambdas into
// capture cells at function entry. Must run after params were registered
// and before the body is compiled.
func (c *compiler) boxCapturedParams(paramNames []string) {
	for _, name := range paramNames {
		if !c.capturedNames[name] {
			continue
		}
		idx := c.resolveLocal(name)
		if idx < 0 {
			continue
		}
		c.emit(opLoadLocal, int32(idx), c.curLine)
		c.emit(opMakeCell, 0, c.curLine)
		c.emit(opStoreLocal, int32(idx), c.curLine)
		c.locals[idx].isCell = true
	}
}

// --- Lambda / closure compilation ---

// compileLambdaExpr compiles `fun(params): T { ... }` used as an expression.
// The lambda body becomes an internal chunk (named lambda$N, never exported);
// captured enclosing locals are shared through capture cells.
func (c *compiler) compileLambdaExpr(e *frontend.LambdaExpr) {
	lambdaName := fmt.Sprintf("lambda$%d", c.lambdaCounter)
	c.lambdaCounter++

	paramNames := make([]string, len(e.Params))
	paramTypes := make([]string, len(e.Params))
	rawParamTypes := make([]string, len(e.Params))
	for i, p := range e.Params {
		paramNames[i] = p.Name.Value
		if p.Type_ != nil {
			rawParamTypes[i] = typeAnnotationName(p.Type_)
			paramTypes[i] = c.resolveType(rawParamTypes[i])
		}
	}

	var body blockDecl
	if e.Body != nil {
		body = blockDeclFromBlock(e.Body)
	}

	// Free names of the lambda body relative to the lambda's own scope. These
	// resolve either to the enclosing frame (captures) or to this lambda's own
	// params/locals (which must then be boxed for deeper closures).
	ownDeclared := make(map[string]bool)
	for _, p := range paramNames {
		ownDeclared[p] = true
	}
	collectDeclaredNamesBlock(body, ownDeclared)
	ownFree := make(map[string]bool)
	freeNamesBlock(body, ownDeclared, ownFree)
	if e.ExprBody != nil {
		freeNamesExpr(e.ExprBody, ownDeclared, ownFree)
	}

	// Names this lambda's own frame must box: free names that are declared in
	// this lambda (captured by nested lambdas). `this` is boxed at
	// closure-creation time instead, so it is excluded.
	boxSet := make(map[string]bool)
	for name := range ownFree {
		if name != "this" && ownDeclared[name] {
			boxSet[name] = true
		}
	}

	// Captures from the enclosing frame: free names resolvable as locals at
	// the closure-creation site (or `this` inside a method).
	captureOrder := make([]string, 0, len(ownFree))
	for name := range ownFree {
		if name == "this" {
			if c.currentClassName != "" && c.resolveLocal("this") >= 0 {
				captureOrder = append(captureOrder, name)
			}
			continue
		}
		if c.resolveLocal(name) >= 0 {
			captureOrder = append(captureOrder, name)
		}
	}
	sort.Strings(captureOrder)

	// Resolve capture types from the enclosing scope before switching chunks.
	captureTypes := make([]string, len(captureOrder))
	for i, name := range captureOrder {
		captureTypes[i] = ""
		if name == "this" {
			captureTypes[i] = c.currentClassName
			continue
		}
		if idx := c.resolveLocal(name); idx >= 0 {
			captureTypes[i] = c.locals[idx].typeName
		}
	}

	// Save compiler state.
	savedChunk := c.chunk
	savedLocals := c.locals
	savedDepth := c.scopeDepth
	savedPeak := c.peakLocals
	savedReturnHint := c.returnTypeHint
	savedStreamFun := c.currentStreamFun
	savedCapturedNames := c.capturedNames
	savedCapturedOrder := c.capturedOrder
	savedCurrentClass := c.currentClassName
	savedHandlerDepth := c.handlerDepth
	savedInDeferBody := c.inDeferBody

	// Compile the lambda into its own chunk.
	c.chunk = newChunk()
	c.chunk.sourceName = lambdaName
	c.locals = make([]local, 0)
	c.peakLocals = 0
	c.scopeDepth = 1
	c.handlerDepth = 0
	c.inDeferBody = 0
	c.capturedNames = boxSet
	c.capturedOrder = nil
	// Keep the enclosing class context so `this.field` expressions inside the
	// lambda resolve field types (typed arithmetic, method resolution). `this`
	// itself still arrives as a capture cell, not as a receiver local.
	c.currentClassName = savedCurrentClass
	c.currentStreamFun = false
	c.returnTypeHint = ""
	returnTypeName := ""
	if e.ReturnType != nil {
		returnTypeName = c.resolveType(typeAnnotationName(e.ReturnType))
		if returnTypeName == "long" || returnTypeName == "ulong" || returnTypeName == "double" {
			c.returnTypeHint = returnTypeName
		}
	}

	// Layout: declared params first, then capture-cell locals. The runtime
	// appends capture cells after the declared arguments on every call.
	for i, name := range paramNames {
		c.addLocalWithType(name, paramTypes[i])
	}
	for i, name := range captureOrder {
		idx := c.addLocalWithType(name, captureTypes[i])
		c.locals[idx].isCell = true
	}
	// Parameters captured by nested lambdas are boxed into cells at entry.
	c.boxCapturedParams(paramNames)

	if len(body.stmts) > 0 {
		c.compileBlock(body)
	} else if e.ExprBody != nil {
		prevHint := c.typeHint
		if returnTypeName == "long" || returnTypeName == "ulong" || returnTypeName == "double" {
			c.typeHint = returnTypeName
		} else {
			c.typeHint = ""
		}
		c.compileExpression(e.ExprBody)
		c.typeHint = prevHint
		c.emit(opReturn, 0, c.curLine)
	} else {
		c.emit(opReturnVoid, 0, c.curLine)
	}

	c.functions[lambdaName] = c.chunk
	c.funcInfo[lambdaName] = &functionInfo{
		name:       lambdaName,
		paramCount: len(paramNames) + len(captureOrder),
		paramTypes: rawParamTypes,
		localCount: c.peakLocals,
		returnType: returnTypeName,
	}
	c.chunk.LocalCount = c.peakLocals

	// Restore compiler state.
	c.chunk = savedChunk
	c.locals = savedLocals
	c.scopeDepth = savedDepth
	c.peakLocals = savedPeak
	c.returnTypeHint = savedReturnHint
	c.currentStreamFun = savedStreamFun
	c.capturedNames = savedCapturedNames
	c.capturedOrder = savedCapturedOrder
	c.currentClassName = savedCurrentClass
	c.handlerDepth = savedHandlerDepth
	c.inDeferBody = savedInDeferBody

	// Emit closure creation in the enclosing chunk: push the capture cells
	// (same order as the lambda chunk layout), then bind them to the chunk.
	funcRefID := savedChunk.addFunctionRef(lambdaName, c.functions[lambdaName])
	for _, name := range captureOrder {
		if name == "this" {
			thisIdx := c.resolveLocal("this")
			if thisIdx < 0 {
				c.addCompileError("undefined_variable", "bytecode/scope/closure", fmt.Sprintf("lambda captures 'this' outside of a method"))
				return
			}
			c.emit(opLoadLocal, int32(thisIdx), c.curLine)
			c.emit(opMakeCell, 0, c.curLine)
			continue
		}
		idx := c.resolveLocal(name)
		if idx < 0 {
			c.addCompileError("undefined_variable", "bytecode/scope/closure", fmt.Sprintf("lambda captures variable %q before its declaration", name))
			return
		}
		c.emit(opLoadLocal, int32(idx), c.curLine)
		if !c.locals[idx].isCell {
			// Capture analysis said this name is captured, but the enclosing
			// local was not boxed (e.g. `this`). Box the raw value now.
			c.emit(opMakeCell, 0, c.curLine)
		}
	}
	operand := int32(uint32(funcRefID)&0xFFFF | ((uint32(len(captureOrder)) & 0xFFFF) << 16))
	c.emit(opMakeClosure, operand, c.curLine)
}

// --- Capture analysis ---

// computeFunctionCaptures analyzes a function body and returns the set (and a
// deterministic order) of names that lambdas inside the body capture from the
// enclosing function scope. Params and body locals are candidates; names that
// only resolve to globals/functions are not captured. extraCaptures lists
// names already known to be captures (e.g. the enclosing function's captured
// set, or `this` inside methods); those are captured too when referenced.
func computeFunctionCaptures(params []string, body blockDecl, isMethod bool, extraCaptures map[string]bool) (map[string]bool, []string) {
	declared := make(map[string]bool)
	for _, p := range params {
		declared[p] = true
	}
	if isMethod {
		declared["this"] = true
	}
	collectDeclaredNamesBlock(body, declared)

	free := make(map[string]bool)
	freeNamesStmt(stmtDecl{kind: stmtKindBlock, blk: body}, declared, free)

	captured := make(map[string]bool)
	for name := range free {
		// `this` never becomes a capture cell in the enclosing scope; it is
		// boxed at closure-creation time instead.
		if name == "this" {
			continue
		}
		if declared[name] {
			captured[name] = true
		}
	}
	for name := range extraCaptures {
		if free[name] {
			captured[name] = true
		}
	}
	order := make([]string, 0, len(captured))
	for name := range captured {
		order = append(order, name)
	}
	sort.Strings(order)
	return captured, order
}

// collectDeclaredNamesBlock adds every name bound inside the block (vars,
// for-loop variables, when-case variables) to out.
func collectDeclaredNamesBlock(block blockDecl, out map[string]bool) {
	for _, s := range block.stmts {
		collectDeclaredNames(s, out)
	}
}

func collectDeclaredNames(stmt stmtDecl, out map[string]bool) {
	switch stmt.kind {
	case stmtKindVar:
		if stmt.vr.name != "" {
			out[stmt.vr.name] = true
		}
	case stmtKindFor:
		if stmt.for_.isForIn {
			if stmt.for_.variable != "" {
				out[stmt.for_.variable] = true
			}
		} else if stmt.for_.init != nil {
			collectDeclaredNames(*stmt.for_.init, out)
		}
	case stmtKindWhen:
		// when-case type-match variables are not real locals at runtime
		// (never bound by compileWhenStmt); skip them.
	case stmtKindIf:
		collectDeclaredNamesBlock(stmt.if_.consequence, out)
		if stmt.if_.alternativeBlk != nil {
			collectDeclaredNamesBlock(*stmt.if_.alternativeBlk, out)
		}
		if stmt.if_.alternativeIf != nil {
			alt := stmtDecl{kind: stmtKindIf, if_: *stmt.if_.alternativeIf}
			collectDeclaredNames(alt, out)
		}
	case stmtKindWhile:
		collectDeclaredNamesBlock(stmt.whl.body, out)
	case stmtKindBlock:
		collectDeclaredNamesBlock(stmt.blk, out)
	case stmtKindTry:
		collectDeclaredNamesBlock(stmt.try_.body, out)
		if stmt.try_.catchVar != "" {
			out[stmt.try_.catchVar] = true
		}
		collectDeclaredNamesBlock(stmt.try_.catchBody, out)
	case stmtKindDefer:
		collectDeclaredNamesBlock(stmt.dfr.body, out)
	}
}

// freeNamesStmt collects free identifier names (reads or writes) reachable
// from stmt into out, ignoring names in bound.
func freeNamesStmt(stmt stmtDecl, bound map[string]bool, out map[string]bool) {
	switch stmt.kind {
	case stmtKindVar:
		freeNamesExpr(stmt.vr.value, bound, out)
	case stmtKindReturn:
		freeNamesExpr(stmt.ret.value, bound, out)
	case stmtKindYield:
		freeNamesExpr(stmt.yld.value, bound, out)
	case stmtKindExpr:
		freeNamesExpr(stmt.expr, bound, out)
	case stmtKindIf:
		freeNamesExpr(stmt.if_.condition, bound, out)
		freeNamesBlock(stmt.if_.consequence, bound, out)
		if stmt.if_.alternativeBlk != nil {
			freeNamesBlock(*stmt.if_.alternativeBlk, bound, out)
		}
		if stmt.if_.alternativeIf != nil {
			freeNamesStmt(stmtDecl{kind: stmtKindIf, if_: *stmt.if_.alternativeIf}, bound, out)
		}
	case stmtKindWhile:
		freeNamesExpr(stmt.whl.condition, bound, out)
		freeNamesBlock(stmt.whl.body, bound, out)
	case stmtKindFor:
		if stmt.for_.init != nil {
			freeNamesStmt(*stmt.for_.init, bound, out)
		}
		freeNamesExpr(stmt.for_.condition, bound, out)
		freeNamesExpr(stmt.for_.update, bound, out)
		freeNamesExpr(stmt.for_.iterable, bound, out)
		freeNamesBlock(stmt.for_.body, bound, out)
	case stmtKindWhen:
		freeNamesExpr(stmt.when.expr, bound, out)
		for _, cc := range stmt.when.cases {
			for _, val := range cc.values {
				freeNamesExpr(val, bound, out)
			}
			freeNamesExpr(cc.guard, bound, out)
			freeNamesBlock(cc.body, bound, out)
		}
		if stmt.when.defaultCase != nil {
			freeNamesBlock(*stmt.when.defaultCase, bound, out)
		}
	case stmtKindBlock:
		freeNamesBlock(stmt.blk, bound, out)
	case stmtKindTry:
		freeNamesBlock(stmt.try_.body, bound, out)
		freeNamesBlock(stmt.try_.catchBody, bound, out)
	case stmtKindDefer:
		freeNamesBlock(stmt.dfr.body, bound, out)
	case stmtKindBreak, stmtKindContinue:
		// nothing
	}
}

func freeNamesBlock(block blockDecl, bound map[string]bool, out map[string]bool) {
	for _, s := range block.stmts {
		freeNamesStmt(s, bound, out)
	}
}

// freeNamesExpr collects free identifier names from expr into out. Lambda
// sub-expressions are analyzed against their own parameter/local scope, so
// only names truly free inside the lambda propagate to out.
func freeNamesExpr(expr frontend.Expression, bound map[string]bool, out map[string]bool) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *frontend.IdentExpr:
		if !bound[e.Value] {
			out[e.Value] = true
		}
	case *frontend.IntLiteral, *frontend.FloatLiteral, *frontend.StringLiteral,
		*frontend.BoolLiteral, *frontend.NullLiteral:
		// no free names
	case *frontend.ThisExpr:
		// `this` inside a lambda body is resolved via captures; freeNamesLambda
		// deliberately leaves "this" out of the lambda's bound set so it
		// propagates to the enclosing scope analysis.
		if !bound["this"] {
			out["this"] = true
		}
	case *frontend.BinaryExpr:
		freeNamesExpr(e.Left, bound, out)
		freeNamesExpr(e.Right, bound, out)
	case *frontend.UnaryExpr:
		freeNamesExpr(e.Right, bound, out)
	case *frontend.CallExpr:
		freeNamesExpr(e.Callee, bound, out)
		for _, a := range e.Arguments {
			freeNamesExpr(a, bound, out)
		}
	case *frontend.MemberExpr:
		freeNamesExpr(e.Object, bound, out)
	case *frontend.NullCoalesceExpr:
		freeNamesExpr(e.Left, bound, out)
		freeNamesExpr(e.Right, bound, out)
	case *frontend.OptionalChainExpr:
		freeNamesExpr(e.Expr, bound, out)
	case *frontend.IndexExpr:
		freeNamesExpr(e.Left, bound, out)
		freeNamesExpr(e.Index, bound, out)
	case *frontend.AssignExpr:
		freeNamesExpr(e.Target, bound, out)
		freeNamesExpr(e.Value, bound, out)
	case *frontend.TypeCheckExpr:
		freeNamesExpr(e.Left, bound, out)
	case *frontend.TypeCastExpr:
		freeNamesExpr(e.Left, bound, out)
	case *frontend.ArrayLiteral:
		for _, el := range e.Elements {
			freeNamesExpr(el, bound, out)
		}
	case *frontend.MapLiteral:
		for _, p := range e.Pairs {
			freeNamesExpr(p.Key, bound, out)
			freeNamesExpr(p.Value, bound, out)
		}
	case *frontend.StructLiteral:
		for _, f := range e.Fields {
			freeNamesExpr(f.Value, bound, out)
		}
	case *frontend.NewExpr:
		for _, a := range e.Arguments {
			freeNamesExpr(a, bound, out)
		}
	case *frontend.SuperExpr:
		for _, a := range e.Arguments {
			freeNamesExpr(a, bound, out)
		}
	case *frontend.LambdaExpr:
		freeNamesLambda(e, bound, out)
	}
}

// freeNamesLambda analyzes a lambda's body against its own scope only (params
// + body locals). Enclosing-scope names intentionally stay free so they
// propagate up to the enclosing function's capture analysis. `this` is never
// part of the lambda's own scope, so method-receiver capture propagates too.
func freeNamesLambda(e *frontend.LambdaExpr, bound map[string]bool, out map[string]bool) {
	lambdaBound := make(map[string]bool, len(e.Params)+4)
	for _, p := range e.Params {
		lambdaBound[p.Name.Value] = true
	}
	if e.Body != nil {
		body := blockDeclFromBlock(e.Body)
		collectDeclaredNamesBlock(body, lambdaBound)
		freeNamesBlock(body, lambdaBound, out)
	}
	if e.ExprBody != nil {
		freeNamesExpr(e.ExprBody, lambdaBound, out)
	}
}

// --- Struct compilation ---

func (c *compiler) compileStructDecl(info structInfo) {
	c.structs[info.name] = info
}

// --- Class compilation ---

func (c *compiler) compileClassDecl(info classInfo) {
	c.classes[info.name] = info

	// Compile methods as separate function chunks.
	for _, method := range info.methods {
		methodKey := info.name + "." + method.name

		savedChunk := c.chunk
		savedLocals := c.locals
		savedDepth := c.scopeDepth
		savedClass := c.currentClassName
		savedReturnHint := c.returnTypeHint
		savedStreamFun := c.currentStreamFun
		savedCapturedNames := c.capturedNames
		savedCapturedOrder := c.capturedOrder
		savedHandlerDepth := c.handlerDepth
		savedInDeferBody := c.inDeferBody

		c.chunk = newChunk()
		c.chunk.sourceName = methodKey
		c.locals = make([]local, 0)
		c.peakLocals = 0
		c.scopeDepth = 0
		c.currentClassName = info.name
		c.handlerDepth = 0
		c.inDeferBody = 0

		// Pre-compute captures for the method body (`this` included).
		c.capturedNames, c.capturedOrder = computeFunctionCaptures(method.paramNames, method.body, true, nil)

		// "this" as local 0.
		c.addLocalWithType("this", info.name)
		for i, name := range method.paramNames {
			typeName := ""
			if i < len(method.paramTypes) {
				typeName = c.resolveType(method.paramTypes[i])
			}
			c.addLocalWithType(name, typeName)
		}
		// Parameters captured by lambdas are boxed into cells at entry.
		// `this` is never boxed; it is boxed at closure-creation time.
		c.boxCapturedParams(method.paramNames)
		c.returnTypeHint = ""
		c.currentStreamFun = false
		if resolved := c.resolveType(method.returnType); resolved == "long" || resolved == "ulong" || resolved == "double" {
			c.returnTypeHint = resolved
		}

		if len(method.body.stmts) > 0 {
			c.compileBlock(method.body)
		} else if method.exprBody != nil {
			c.compileExpression(method.exprBody)
			c.emit(opReturn, 0, c.curLine)
			c.functions[methodKey] = c.chunk
			c.funcInfo[methodKey] = &functionInfo{
				name:       methodKey,
				paramCount: len(method.paramNames) + 1,
				paramTypes: append([]string(nil), method.paramTypes...),
				localCount: c.peakLocals,
				returnType: method.returnType,
			}
			c.chunk.LocalCount = c.peakLocals
			c.chunk = savedChunk
			c.locals = savedLocals
			c.scopeDepth = savedDepth
			c.currentClassName = savedClass
			c.returnTypeHint = savedReturnHint
			c.currentStreamFun = savedStreamFun
			c.capturedNames = savedCapturedNames
			c.capturedOrder = savedCapturedOrder
			c.handlerDepth = savedHandlerDepth
			c.inDeferBody = savedInDeferBody
			continue
		}
		c.emit(opReturnVoid, 0, c.curLine)

		c.functions[methodKey] = c.chunk
		c.funcInfo[methodKey] = &functionInfo{
			name:       methodKey,
			paramCount: len(method.paramNames) + 1, // +1 for this
			paramTypes: append([]string(nil), method.paramTypes...),
			localCount: c.peakLocals,
			returnType: method.returnType,
		}
		c.chunk.LocalCount = c.peakLocals

		c.chunk = savedChunk
		c.locals = savedLocals
		c.scopeDepth = savedDepth
		c.currentClassName = savedClass
		c.returnTypeHint = savedReturnHint
		c.currentStreamFun = savedStreamFun
		c.capturedNames = savedCapturedNames
		c.capturedOrder = savedCapturedOrder
		c.handlerDepth = savedHandlerDepth
		c.inDeferBody = savedInDeferBody
	}
}

// --- Interface compilation ---

func (c *compiler) compileInterfaceDecl(info interfaceInfo) {
	c.interfaces[info.name] = info
}

// synthesizeInterfaceDefaultMethods appends default method bodies declared on
// implemented interfaces to a class's own method list when neither the class
// nor any ancestor provides the method. The synthesized methods are marked
// open so subclasses may override the inherited default. This makes interface
// defaults flow through ordinary method compilation, vtable construction, and
// name-based dispatch like regular class methods.
func (c *compiler) synthesizeInterfaceDefaultMethods(className string) {
	info, ok := c.classes[className]
	if !ok {
		return
	}
	alreadySynthesized := make(map[string]bool)
	for _, ifaceName := range info.implements {
		iface, ok := c.interfaces[ifaceName]
		if !ok {
			continue // unknown_interface is reported by contract validation
		}
		for i := range iface.methods {
			m := &iface.methods[i]
			if !m.hasDefault {
				continue
			}
			if _, provided := c.lookupMethodOnAncestor(info.name, m.name); provided {
				continue // class or an ancestor already provides the method
			}
			if alreadySynthesized[m.name] {
				continue // an earlier interface already contributed this default
			}
			alreadySynthesized[m.name] = true
			info.methods = append(info.methods, methodDecl{
				name:       m.name,
				paramNames: m.paramNames,
				paramTypes: m.paramTypes,
				body:       m.body,
				returnType: m.returnType,
				isOpen:     true,
			})
		}
	}
	c.classes[className] = info
}

// --- Var compilation ---

func (c *compiler) compileVarDecl(v varDecl) {
	c.compileVarDeclWithName(v, v.name)
}

func (c *compiler) compileVarDeclWithName(v varDecl, name string) {
	// Set type hint so literal compilation picks the right encoding.
	prevHint := c.typeHint
	resolved := c.resolveType(v.type_)
	// Function-typed slots validate their initializer at compile time.
	c.checkFunTypeValueAssign(resolved, name, v.value)
	if resolved == "long" || resolved == "ulong" || resolved == "double" {
		c.typeHint = resolved
	} else if v.type_ == "long" || v.type_ == "ulong" || v.type_ == "double" {
		c.typeHint = v.type_
	} else {
		c.typeHint = ""
	}

	if c.scopeDepth == 0 {
		// Global variable.
		idx := len(c.globals)
		c.globals[name] = idx
		if v.value != nil {
			c.compileExpression(v.value)
			c.emit(opStoreGlobal, int32(idx), c.curLine)
		}
	} else {
		// Local variable.
		c.addLocalWithType(name, resolved)
		if c.capturedNames[name] {
			// Box captured locals into cells so closures share the slot.
			if v.value != nil {
				c.compileExpression(v.value)
				c.emit(opMakeCell, 0, c.curLine)
				localIdx := c.resolveLocal(name)
				c.emit(opStoreLocal, int32(localIdx), c.curLine)
			} else {
				c.emitZeroValue(resolved)
				c.emit(opMakeCell, 0, c.curLine)
				localIdx := c.resolveLocal(name)
				c.emit(opStoreLocal, int32(localIdx), c.curLine)
			}
			if localIdx := c.resolveLocal(name); localIdx >= 0 {
				c.locals[localIdx].isCell = true
			}
		} else if v.value != nil {
			c.compileExpression(v.value)
			localIdx := c.resolveLocal(name)
			c.emit(opStoreLocal, int32(localIdx), c.curLine)
		}
	}
	c.typeHint = prevHint
}

// --- Block compilation ---

func (c *compiler) compileBlock(block blockDecl) {
	c.scopeDepth++
	for _, stmt := range block.stmts {
		c.compileStatementDecl(stmt)
	}
	c.scopeDepth--
	c.removeLocals(c.scopeDepth)
}

func (c *compiler) compileStatementDecl(stmt stmtDecl) {
	switch stmt.kind {
	case stmtKindVar:
		c.compileVarDecl(stmt.vr)
	case stmtKindReturn:
		c.compileReturnStmt(stmt.ret)
	case stmtKindYield:
		c.compileYieldStmt(stmt.yld)
	case stmtKindIf:
		c.compileIfStmt(stmt.if_)
	case stmtKindWhile:
		c.compileWhileStmt(stmt.whl)
	case stmtKindFor:
		c.compileForStmt(stmt.for_)
	case stmtKindWhen:
		c.compileWhenStmt(stmt.when)
	case stmtKindBreak:
		c.compileBreakStmt()
	case stmtKindContinue:
		c.compileContinueStmt()
	case stmtKindBlock:
		c.compileBlock(stmt.blk)
	case stmtKindExpr:
		if stmt.expr != nil {
			c.compileExpression(stmt.expr)
			c.emitPopAfterExpression()
		}
	case stmtKindTry:
		c.compileTryStmt(stmt.try_)
	case stmtKindDefer:
		c.compileDeferStmt(stmt.dfr)
	}
}

// emitPopAfterExpression emits a POP unless the last instruction was a store,
// in which case it fuses the store into a store-and-pop opcode.
func (c *compiler) emitPopAfterExpression() {
	if len(c.chunk.code) > 0 {
		lastIdx := len(c.chunk.code) - 1
		last := c.chunk.code[lastIdx]
		switch last.op {
		case opStoreLocal:
			c.chunk.code[lastIdx].op = opStoreLocalPop
		case opStoreGlobal:
			c.chunk.code[lastIdx].op = opStoreGlobalPop
		case opStoreCell:
			c.chunk.code[lastIdx].op = opStoreCellPop
		case opIncLocal, opDecLocal, opAddLocalInt, opConcatLocalConstString, opArrayPushLocalConstString, opMapDelete:
			// fused local mutators and map delete don't push a value; nothing to pop
		default:
			c.emit(opPop, 0, c.curLine)
		}
	} else {
		c.emit(opPop, 0, c.curLine)
	}
}

func (c *compiler) compileReturnStmt(r returnDecl) {
	if c.inDeferBody > 0 {
		c.addCompileError("return_in_defer", "bytecode/control/defer", "return is not allowed inside a defer body")
		return
	}
	if r.value != nil {
		prevHint := c.typeHint
		if c.returnTypeHint == "long" || c.returnTypeHint == "ulong" || c.returnTypeHint == "double" {
			c.typeHint = c.returnTypeHint
		} else {
			c.typeHint = ""
		}
		c.compileExpression(r.value)
		c.typeHint = prevHint
		c.emit(opReturn, 0, c.curLine)
	} else {
		c.emit(opReturnVoid, 0, c.curLine)
	}
}

func (c *compiler) compileYieldStmt(y yieldDecl) {
	if c.inDeferBody > 0 {
		c.addCompileError("yield_in_defer", "bytecode/control/defer", "yield is not allowed inside a defer body")
		return
	}
	if !c.currentStreamFun {
		c.addCompileError("yield_requires_stream_fun", "bytecode/callable/stream", "yield is only allowed inside stream fun")
		return
	}
	if c.handlerDepth > 0 {
		c.addCompileError("yield_disallowed_in_try", "bytecode/callable/stream", "yield is not allowed inside a try block: the catch context cannot be preserved across a stream suspension; move the error-prone segment into a helper fun and yield its result")
		return
	}
	if y.value == nil {
		c.addCompileError("yield_value_required", "bytecode/callable/stream", "yield requires a value")
		return
	}
	c.compileExpression(y.value)
	c.emit(opYield, 0, c.curLine)
}

func (c *compiler) compileIfStmt(s ifDecl) {
	c.compileExpression(s.condition)
	jumpIfFalse := c.emit(opJumpIfFalse, 0, c.curLine)

	c.compileBlock(s.consequence)

	if s.alternativeIf != nil || s.alternativeBlk != nil {
		jumpEnd := c.emit(opJump, 0, c.curLine)
		c.chunk.patchJump(jumpIfFalse, c.chunk.size())
		if s.alternativeIf != nil {
			c.compileIfStmt(*s.alternativeIf)
		} else {
			c.compileBlock(*s.alternativeBlk)
		}
		c.chunk.patchJump(jumpEnd, c.chunk.size())
	} else {
		c.chunk.patchJump(jumpIfFalse, c.chunk.size())
	}
}

func (c *compiler) compileWhileStmt(s whileDecl) {
	// Optimized layout: jump over body to condition on first entry, then
	// condition at bottom with JUMP_IF_TRUE back to body. This eliminates
	// one unconditional jump per iteration.
	jumpToCheck := c.emit(opJump, 0, c.curLine)

	loopStart := c.chunk.size()
	c.loopStack = append(c.loopStack, loopContext{start: loopStart, handlerDepth: c.handlerDepth})

	c.compileBlock(s.body)

	checkPos := c.chunk.size()
	// Patch continue jumps to the condition check.
	for _, jump := range c.loopStack[len(c.loopStack)-1].continueJumps {
		c.chunk.patchJump(jump, checkPos)
	}

	if !c.tryEmitLocalLocalIntLtLoopBranch(s.condition, loopStart) {
		c.compileExpression(s.condition)
		c.emit(opJumpIfTrue, int32(loopStart), c.curLine)
	}

	endPos := c.chunk.size()
	c.chunk.patchJump(jumpToCheck, checkPos)
	for _, jump := range c.loopStack[len(c.loopStack)-1].breakJumps {
		c.chunk.patchJump(jump, endPos)
	}
	c.loopStack = c.loopStack[:len(c.loopStack)-1]
}

func (c *compiler) compileForStmt(s forDecl) {
	if s.isForIn {
		c.compileForIn(s)
		return
	}

	// C-style for: for (init; cond; update) { body }
	// Optimized layout: init; JUMP check; loopStart: body; update;
	// check: condition; JUMP_IF_TRUE loopStart; exit:
	// This eliminates one unconditional jump per iteration.
	c.scopeDepth++
	if s.init != nil {
		c.compileStatementDecl(*s.init)
	}

	jumpToCheck := c.emit(opJump, 0, c.curLine)

	loopStart := c.chunk.size()
	c.loopStack = append(c.loopStack, loopContext{start: loopStart, handlerDepth: c.handlerDepth})

	c.compileBlock(s.body)

	// Update expression (if any). Continue jumps target here.
	if s.update != nil {
		updatePos := c.chunk.size()
		for _, jump := range c.loopStack[len(c.loopStack)-1].continueJumps {
			c.chunk.patchJump(jump, updatePos)
		}
		c.compileExpression(s.update)
		c.emitPopAfterExpression()
	}

	// Condition check at bottom.
	checkPos := c.chunk.size()
	if s.condition != nil {
		if !c.tryEmitLocalLocalIntLtLoopBranch(s.condition, loopStart) {
			c.compileExpression(s.condition)
			c.emit(opJumpIfTrue, int32(loopStart), c.curLine)
		}
	} else {
		// Infinite loop.
		c.emit(opJump, int32(loopStart), c.curLine)
	}

	// Patch continue jumps that had no update target to the check.
	if s.update == nil {
		for _, jump := range c.loopStack[len(c.loopStack)-1].continueJumps {
			c.chunk.patchJump(jump, checkPos)
		}
	}

	endPos := c.chunk.size()
	c.chunk.patchJump(jumpToCheck, checkPos)
	for _, jump := range c.loopStack[len(c.loopStack)-1].breakJumps {
		c.chunk.patchJump(jump, endPos)
	}
	c.loopStack = c.loopStack[:len(c.loopStack)-1]
	c.scopeDepth--
	c.removeLocals(c.scopeDepth)
}

func (c *compiler) compileForIn(s forDecl) {
	c.scopeDepth++

	// Store iterable in a hidden local.
	collLocal := c.addLocal("__iter_coll__")
	c.compileExpression(s.iterable)
	c.emit(opStoreLocal, int32(collLocal), c.curLine)

	// Initialize index to 0.
	idxLocal := c.addLocal("__iter_idx__")
	zeroIdx := c.chunk.addConstant(int32(0))
	c.emit(opPushInt, int32(zeroIdx), c.curLine)
	c.emit(opStoreLocal, int32(idxLocal), c.curLine)

	// Jump over body to condition on first entry.
	jumpToCheck := c.emit(opJump, 0, c.curLine)

	loopStart := c.chunk.size()
	c.loopStack = append(c.loopStack, loopContext{start: loopStart, handlerDepth: c.handlerDepth})

	// Body: var item = coll[idx]; <body>.
	itemLocal := c.addLocal(s.variable)
	c.emit(opLoadLocal, int32(collLocal), c.curLine)
	c.emit(opLoadLocal, int32(idxLocal), c.curLine)
	c.emit(opIterItem, 0, c.curLine)
	if c.capturedNames[s.variable] {
		// Loop variable captured by a closure: store through a fresh cell
		// each iteration (per-iteration binding semantics).
		c.emit(opMakeCell, 0, c.curLine)
		c.locals[itemLocal].isCell = true
	}
	c.emit(opStoreLocal, int32(itemLocal), c.curLine)

	c.compileBlock(s.body)

	// Increment: idx++ (continue target).
	incrementPos := c.chunk.size()
	for _, jump := range c.loopStack[len(c.loopStack)-1].continueJumps {
		c.chunk.patchJump(jump, incrementPos)
	}
	c.emit(opLoadLocal, int32(idxLocal), c.curLine)
	oneIdx := c.chunk.addConstant(int32(1))
	c.emit(opPushInt, int32(oneIdx), c.curLine)
	c.emit(opAdd, 0, c.curLine)
	c.emit(opStoreLocal, int32(idxLocal), c.curLine)

	// Condition: idx < len(coll).
	checkPos := c.chunk.size()
	c.emit(opLoadLocal, int32(idxLocal), c.curLine)
	c.emit(opLoadLocal, int32(collLocal), c.curLine)
	c.emit(opIterLen, 0, c.curLine)
	c.emit(opLt, 0, c.curLine)
	c.emit(opJumpIfTrue, int32(loopStart), c.curLine)

	endPos := c.chunk.size()
	c.chunk.patchJump(jumpToCheck, checkPos)
	for _, jump := range c.loopStack[len(c.loopStack)-1].breakJumps {
		c.chunk.patchJump(jump, endPos)
	}
	c.loopStack = c.loopStack[:len(c.loopStack)-1]
	c.scopeDepth--
	c.removeLocals(c.scopeDepth)
}

func (c *compiler) compileWhenStmt(s whenDecl) {
	// Compile when as a chain of if-else blocks.
	c.compileExpression(s.expr)
	subjectLocal := c.addLocal("__when__")
	c.emit(opStoreLocal, int32(subjectLocal), c.curLine)

	var endJumps []int
	for _, cc := range s.cases {
		for _, val := range cc.values {
			c.emit(opLoadLocal, int32(subjectLocal), c.curLine)
			c.compileExpression(val)
			c.emit(opEq, 0, c.curLine)
			matchJump := c.emit(opJumpIfFalse, 0, c.curLine)
			var guardJump int
			haveGuard := cc.guard != nil
			if haveGuard {
				c.compileExpression(cc.guard)
				guardJump = c.emit(opJumpIfFalse, 0, c.curLine)
			}
			c.compileBlock(cc.body)
			endJumps = append(endJumps, c.emit(opJump, 0, c.curLine))
			afterBody := c.chunk.size()
			c.chunk.patchJump(matchJump, afterBody)
			if haveGuard {
				c.chunk.patchJump(guardJump, afterBody)
			}
		}
		if cc.variable != "" && cc.hasTypeMatch {
			c.emit(opLoadLocal, int32(subjectLocal), c.curLine)
			typeNameIdx := c.chunk.addConstant(cc.typeName)
			c.emit(opIs, int32(typeNameIdx), c.curLine)
			matchJump := c.emit(opJumpIfFalse, 0, c.curLine)
			var guardJump int
			haveGuard := cc.guard != nil
			if haveGuard {
				c.compileExpression(cc.guard)
				guardJump = c.emit(opJumpIfFalse, 0, c.curLine)
			}
			c.compileBlock(cc.body)
			endJumps = append(endJumps, c.emit(opJump, 0, c.curLine))
			afterBody := c.chunk.size()
			c.chunk.patchJump(matchJump, afterBody)
			if haveGuard {
				c.chunk.patchJump(guardJump, afterBody)
			}
		}
	}

	if s.defaultCase != nil {
		c.compileBlock(*s.defaultCase)
	}

	endPos := c.chunk.size()
	for _, jump := range endJumps {
		c.chunk.patchJump(jump, endPos)
	}
}

func (c *compiler) compileBreakStmt() {
	if len(c.loopStack) == 0 {
		c.addCompileError("break_outside_loop", "bytecode/control/loop", "break outside of loop")
		return
	}
	if c.inDeferBody > 0 {
		c.addCompileError("break_in_defer", "bytecode/control/defer", "break is not allowed inside a defer body")
		return
	}
	// Leaving the loop discards try handlers opened inside the loop body.
	lc := &c.loopStack[len(c.loopStack)-1]
	for i := lc.handlerDepth; i < c.handlerDepth; i++ {
		c.emit(opPopHandler, 0, c.curLine)
	}
	jump := c.emit(opJump, 0, c.curLine)
	lc.breakJumps = append(lc.breakJumps, jump)
}

func (c *compiler) compileContinueStmt() {
	if len(c.loopStack) == 0 {
		c.addCompileError("continue_outside_loop", "bytecode/control/loop", "continue outside of loop")
		return
	}
	if c.inDeferBody > 0 {
		c.addCompileError("continue_in_defer", "bytecode/control/defer", "continue is not allowed inside a defer body")
		return
	}
	lc := &c.loopStack[len(c.loopStack)-1]
	// Jumping to the next iteration discards try handlers opened inside the
	// current loop body; handlers opened before the loop stay active.
	for i := lc.handlerDepth; i < c.handlerDepth; i++ {
		c.emit(opPopHandler, 0, c.curLine)
	}
	if lc.continuePos != 0 {
		// Target already resolved (e.g., for-in loop).
		c.emit(opJump, int32(lc.continuePos), c.curLine)
	} else {
		// Target not yet known (C-style for loop); record jump to patch later.
		jump := c.emit(opJump, 0, c.curLine)
		lc.continueJumps = append(lc.continueJumps, jump)
	}
}

// compileTryStmt compiles try { body } catch (e) { catchBody } as:
//
//	PUSH_HANDLER catch          ; try start
//	<body>
//	POP_HANDLER
//	JUMP end
//	catch:  STORE_LOCAL_POP e   ; catch start (handler target); stack holds error value
//	<catchBody>
//	end:
//
// The handler records the frame state at PUSH time; the VM restores it when
// dispatching to the catch block, so partial operands from the failed body are
// discarded. The catch variable is a map<string, any> holding the error
// attributes (code/category/message/callable/line/...).
func (c *compiler) compileTryStmt(s tryDecl) {
	pushIP := c.emit(opPushHandler, 0, c.curLine)
	c.handlerDepth++
	c.compileBlock(s.body)
	c.handlerDepth--
	c.emit(opPopHandler, 0, c.curLine)
	jumpEnd := c.emit(opJump, 0, c.curLine)

	catchStart := c.chunk.size()
	c.chunk.patchJump(pushIP, catchStart)

	// Bind the error value (top of stack) to the catch variable.
	catchVar := s.catchVar
	if catchVar == "" {
		catchVar = "__err__"
	}
	c.scopeDepth++
	catchLocal := c.addLocalWithType(catchVar, "map")
	if c.capturedNames[catchVar] {
		// Captured by a closure inside the catch body: bind through a cell so
		// the closure observes the caught error value.
		c.emit(opMakeCell, 0, c.curLine)
		c.locals[catchLocal].isCell = true
	}
	c.emit(opStoreLocalPop, int32(catchLocal), c.curLine)

	c.compileBlock(s.catchBody)
	c.scopeDepth--
	c.removeLocals(c.scopeDepth)

	c.chunk.patchJump(jumpEnd, c.chunk.size())
}

// compileDeferStmt compiles defer { body } as:
//
//	PUSH_DEFER bodyStart
//	JUMP after
//	bodyStart: <body>
//	END_DEFER
//	after:
//
// At runtime PUSH_DEFER registers the body address on the VM defer stack;
// when the enclosing function returns (or unwinds on an uncaught error) the
// VM runs registered bodies in reverse registration order. The body is skipped
// during normal control flow via the jump.
func (c *compiler) compileDeferStmt(s deferDecl) {
	if c.inDeferBody > 0 {
		c.addCompileError("defer_in_defer", "bytecode/control/defer", "defer cannot be nested inside a defer body")
		return
	}
	pushIP := c.emit(opPushDefer, 0, c.curLine)
	jumpOver := c.emit(opJump, 0, c.curLine)
	bodyStart := c.chunk.size()
	c.chunk.patchJump(pushIP, bodyStart)

	c.inDeferBody++
	c.compileBlock(s.body)
	c.inDeferBody--
	c.emit(opEndDefer, 0, c.curLine)

	c.chunk.patchJump(jumpOver, c.chunk.size())
}

// --- Expression compilation ---

func (c *compiler) compileExpression(expr frontend.Expression) {
	if expr == nil {
		return
	}

	switch e := expr.(type) {
	case *frontend.IntLiteral:
		switch c.typeHint {
		case "long":
			idx := c.chunk.addConstant(e.Value) // int64
			c.emit(opPushLong, int32(idx), c.curLine)
		case "ulong":
			idx := c.chunk.addConstant(uint64(e.Value)) // uint64
			c.emit(opPushULong, int32(idx), c.curLine)
		default:
			idx := c.chunk.addConstant(int32(e.Value)) // int32 (default)
			c.emit(opPushInt, int32(idx), c.curLine)
		}
	case *frontend.FloatLiteral:
		if c.typeHint == "double" {
			idx := c.chunk.addConstant(e.Value) // float64
			c.emit(opPushDouble, int32(idx), c.curLine)
		} else {
			idx := c.chunk.addConstant(float32(e.Value)) // float32 (default)
			c.emit(opPushFloat, int32(idx), c.curLine)
		}
	case *frontend.StringLiteral:
		idx := c.chunk.addConstant(e.Value)
		c.emit(opPushString, int32(idx), c.curLine)
	case *frontend.BoolLiteral:
		if e.Value {
			c.emit(opPushTrue, 0, c.curLine)
		} else {
			c.emit(opPushFalse, 0, c.curLine)
		}
	case *frontend.NullLiteral:
		c.emit(opPushNull, 0, c.curLine)
	case *frontend.IdentExpr:
		c.compileIdentExpr(e)
	case *frontend.BinaryExpr:
		c.compileBinaryExpr(e)
	case *frontend.UnaryExpr:
		c.compileUnaryExpr(e)
	case *frontend.CallExpr:
		c.compileCallExpr(e)
	case *frontend.MemberExpr:
		c.compileMemberExpr(e)
	case *frontend.NullCoalesceExpr:
		c.compileNullCoalesceExpr(e)
	case *frontend.OptionalChainExpr:
		c.compileOptionalChainExpr(e)
	case *frontend.IndexExpr:
		c.compileIndexExpr(e)
	case *frontend.AssignExpr:
		c.compileAssignExpr(e)
	case *frontend.TypeCheckExpr:
		c.compileTypeCheckExpr(e)
	case *frontend.TypeCastExpr:
		c.compileTypeCastExpr(e)
	case *frontend.ThisExpr:
		c.compileThisExpr()
	case *frontend.ArrayLiteral:
		c.compileArrayLiteral(e)
	case *frontend.MapLiteral:
		c.compileMapLiteral(e)
	case *frontend.StructLiteral:
		c.compileStructLiteral(e)
	case *frontend.NewExpr:
		c.compileNewExpr(e)
	case *frontend.SuperExpr:
		c.compileSuperExpr(e)
	case *frontend.LambdaExpr:
		c.compileLambdaExpr(e)
	default:
		c.addError(fmt.Sprintf("unsupported expression type: %T", e))
	}
}

func qualifiedModuleCallableName(path, name string) string {
	return "__module_" + sanitizeModulePath(path) + "__" + name
}

func qualifiedModuleGlobalName(path, name string) string {
	return "__module_global_" + sanitizeModulePath(path) + "__" + name
}

func sanitizeModulePath(path string) string {
	replacer := strings.NewReplacer("/", "_", "\\", "_", ".", "_", "-", "_")
	return replacer.Replace(path)
}

func (c *compiler) compileIdentExpr(e *frontend.IdentExpr) {
	if localIdx := c.resolveLocal(e.Value); localIdx >= 0 {
		if c.locals[localIdx].isCell {
			c.emit(opLoadCell, int32(localIdx), c.curLine)
		} else {
			c.emit(opLoadLocal, int32(localIdx), c.curLine)
		}
	} else if _, ok := c.nativeValues[e.Value]; ok {
		slot, exists := c.nativeValueSlots[e.Value]
		if !exists {
			c.addCompileError("undefined_imported_variable", "bytecode/scope/import", fmt.Sprintf("undefined imported native value: %s", e.Value))
			return
		}
		c.emit(opLoadNativeValue, int32(slot), c.curLine)
	} else if target, ok := c.importedGlobals[e.Value]; ok {
		globalIdx, exists := c.globals[target]
		if !exists {
			c.addCompileError("undefined_imported_variable", "bytecode/scope/import", fmt.Sprintf("undefined imported variable: %s", e.Value))
			return
		}
		c.emit(opLoadGlobal, int32(globalIdx), c.curLine)
	} else if globalIdx, ok := c.globals[e.Value]; ok {
		c.emit(opLoadGlobal, int32(globalIdx), c.curLine)
	} else {
		c.addCompileError("undefined_variable", "bytecode/scope/identifier", fmt.Sprintf("undefined variable: %s", e.Value))
	}
}

func (c *compiler) compileBinaryExpr(e *frontend.BinaryExpr) {
	// Short-circuit evaluation for && and ||.
	if e.Operator == "&&" {
		c.compileExpression(e.Left)
		jumpIfFalse := c.emit(opJumpIfFalse, 0, c.curLine)
		c.compileExpression(e.Right)
		jumpEnd := c.emit(opJump, 0, c.curLine)
		c.chunk.patchJump(jumpIfFalse, c.chunk.size())
		c.emit(opPushFalse, 0, c.curLine)
		c.chunk.patchJump(jumpEnd, c.chunk.size())
		return
	}
	if e.Operator == "||" {
		c.compileExpression(e.Left)
		jumpIfTrue := c.emit(opJumpIfTrue, 0, c.curLine)
		c.compileExpression(e.Right)
		jumpEnd := c.emit(opJump, 0, c.curLine)
		c.chunk.patchJump(jumpIfTrue, c.chunk.size())
		c.emit(opPushTrue, 0, c.curLine)
		c.chunk.patchJump(jumpEnd, c.chunk.size())
		return
	}

	c.compileExpression(e.Left)
	c.compileExpression(e.Right)

	// Try type-specialization first: when both operands' static types
	// are known to match a registered scalar (int / double), emit the
	// typed opcode and skip the runtime tag dispatch in the generic op.
	if op, ok := c.typedBinaryOp(e); ok {
		c.emit(op, 0, c.curLine)
		return
	}

	switch e.Operator {
	case "+":
		c.emit(opAdd, 0, c.curLine)
	case "-":
		c.emit(opSub, 0, c.curLine)
	case "*":
		c.emit(opMul, 0, c.curLine)
	case "/":
		c.emit(opDiv, 0, c.curLine)
	case "%":
		c.emit(opMod, 0, c.curLine)
	case "==":
		c.emit(opEq, 0, c.curLine)
	case "!=":
		c.emit(opNe, 0, c.curLine)
	case "<":
		c.emit(opLt, 0, c.curLine)
	case "<=":
		c.emit(opLe, 0, c.curLine)
	case ">":
		c.emit(opGt, 0, c.curLine)
	case ">=":
		c.emit(opGe, 0, c.curLine)
	default:
		c.addError(fmt.Sprintf("unsupported binary operator: %s", e.Operator))
	}
}

// typedBinaryOp returns a type-specialized opcode for the given binary
// expression when both operands' inferred static types match a registered
// scalar specialization. Returns (0, false) for unknown / unsupported
// combinations so the caller falls through to the generic dispatch ops.
func (c *compiler) typedBinaryOp(e *frontend.BinaryExpr) (opcode, bool) {
	leftT := c.inferScalarType(e.Left)
	if leftT == "" {
		return 0, false
	}
	rightT := c.inferScalarType(e.Right)
	if leftT != rightT {
		return 0, false
	}
	switch leftT {
	case "int":
		switch e.Operator {
		case "+":
			return opAddInt, true
		case "-":
			return opSubInt, true
		case "*":
			return opMulInt, true
		case "/":
			return opDivInt, true
		case "%":
			return opModInt, true
		case "==":
			return opEqInt, true
		case "!=":
			return opNeInt, true
		case "<":
			return opLtInt, true
		case "<=":
			return opLeInt, true
		case ">":
			return opGtInt, true
		case ">=":
			return opGeInt, true
		}
	case "double":
		switch e.Operator {
		case "+":
			return opAddDouble, true
		case "-":
			return opSubDouble, true
		case "*":
			return opMulDouble, true
		case "/":
			return opDivDouble, true
		case "==":
			return opEqDouble, true
		case "!=":
			return opNeDouble, true
		case "<":
			return opLtDouble, true
		case "<=":
			return opLeDouble, true
		case ">":
			return opGtDouble, true
		case ">=":
			return opGeDouble, true
		}
	case "long":
		switch e.Operator {
		case "+":
			return opAddLong, true
		case "-":
			return opSubLong, true
		case "*":
			return opMulLong, true
		case "/":
			return opDivLong, true
		case "%":
			return opModLong, true
		case "==":
			return opEqLong, true
		case "!=":
			return opNeLong, true
		case "<":
			return opLtLong, true
		case "<=":
			return opLeLong, true
		case ">":
			return opGtLong, true
		case ">=":
			return opGeLong, true
		}
	case "ulong":
		switch e.Operator {
		case "+":
			return opAddULong, true
		case "-":
			return opSubULong, true
		case "*":
			return opMulULong, true
		case "/":
			return opDivULong, true
		case "%":
			return opModULong, true
		case "==":
			return opEqULong, true
		case "!=":
			return opNeULong, true
		case "<":
			return opLtULong, true
		case "<=":
			return opLeULong, true
		case ">":
			return opGtULong, true
		case ">=":
			return opGeULong, true
		}
	case "float":
		switch e.Operator {
		case "+":
			return opAddFloat, true
		case "-":
			return opSubFloat, true
		case "*":
			return opMulFloat, true
		case "/":
			return opDivFloat, true
		case "==":
			return opEqFloat, true
		case "!=":
			return opNeFloat, true
		case "<":
			return opLtFloat, true
		case "<=":
			return opLeFloat, true
		case ">":
			return opGtFloat, true
		case ">=":
			return opGeFloat, true
		}
	case "string":
		switch e.Operator {
		case "+":
			return opAddString, true
		}
	}
	return 0, false
}

// inferScalarType returns the static scalar type name of an expression
// when the compiler can prove it without running the program. Returns
// "" when the type is unknown or not a single scalar (so the caller
// must fall back to the generic runtime-tagged dispatch).
//
// The function is conservative: false negatives (missed specializations)
// are fine; false positives (wrong type) would corrupt the bytecode.
func (c *compiler) inferScalarType(expr frontend.Expression) string {
	switch e := expr.(type) {
	case *frontend.IntLiteral:
		switch c.typeHint {
		case "long", "ulong":
			return c.typeHint
		}
		return "int"
	case *frontend.FloatLiteral:
		if c.typeHint == "double" {
			return "double"
		}
		return "float"
	case *frontend.BoolLiteral:
		return "bool"
	case *frontend.StringLiteral:
		return "string"
	case *frontend.IdentExpr:
		return c.localOrGlobalTypeName(e.Value)
	case *frontend.MemberExpr:
		return c.memberExprTypeName(e)
	case *frontend.NullCoalesceExpr:
		// `a ?? b` has the type of the non-null branch when both sides
		// agree; otherwise the type is unknown.
		lt := c.inferScalarType(e.Left)
		if lt == "" {
			return ""
		}
		if rt := c.inferScalarType(e.Right); lt == rt {
			return lt
		}
		return ""
	case *frontend.OptionalChainExpr:
		// The chain yields the inner type on the non-null path.
		return c.inferScalarType(e.Expr)
	case *frontend.CallExpr:
		return c.callExpressionReturnType(e)
	case *frontend.BinaryExpr:
		switch e.Operator {
		case "==", "!=", "<", "<=", ">", ">=", "&&", "||":
			return "bool"
		case "+", "-", "*", "/", "%":
			lt := c.inferScalarType(e.Left)
			if lt == "" {
				return ""
			}
			rt := c.inferScalarType(e.Right)
			if lt == rt {
				return lt
			}
		}
		return ""
	case *frontend.UnaryExpr:
		switch e.Operator {
		case "!":
			return "bool"
		case "-":
			return c.inferScalarType(e.Right)
		}
		return ""
	}
	return ""
}

func (c *compiler) compileUnaryExpr(e *frontend.UnaryExpr) {
	c.compileExpression(e.Right)
	switch e.Operator {
	case "!":
		c.emit(opNot, 0, c.curLine)
	case "-":
		c.emit(opNeg, 0, c.curLine)
	default:
		c.addError(fmt.Sprintf("unsupported unary operator: %s", e.Operator))
	}
}

func (c *compiler) compileCallExpr(e *frontend.CallExpr) {
	if nativeName, ok := c.nativeCallableNameForCall(e); ok {
		for _, arg := range e.Arguments {
			c.compileExpression(arg)
		}
		nameIdx := c.chunk.addConstant(nativeName)
		operand := int32(len(e.Arguments))<<16 | int32(nameIdx)
		c.emit(opCall, operand, c.curLine)
		return
	}
	// Check if the callee is a simple identifier (direct function call).
	if ident, ok := e.Callee.(*frontend.IdentExpr); ok {
		if ident.Value == "len" {
			if len(e.Arguments) != 1 {
				c.addError("len expects exactly 1 argument")
				return
			}
			c.compileExpression(e.Arguments[0])
			if c.isMapExpressionInContext(e.Arguments[0]) {
				c.emit(opMapLen, 0, c.curLine)
			} else {
				c.emit(opArrayLen, 0, c.curLine)
			}
			return
		}
		if ident.Value == "push" {
			if len(e.Arguments) != 2 {
				c.addError("push expects exactly 2 arguments")
				return
			}
			// Fast path: push(localArray, "literal") -> fused opcode
			if arr, ok := e.Arguments[0].(*frontend.IdentExpr); ok {
				if lit, ok := e.Arguments[1].(*frontend.StringLiteral); ok {
					if localIdx := c.resolveLocal(arr.Value); localIdx >= 0 {
						constIdx := c.chunk.addConstant(lit.Value)
						operand, ok := packLocalPair(localIdx, constIdx)
						if ok {
							c.emit(opArrayPushLocalConstString, operand, c.curLine)
							return
						}
					}
				}
			}
			c.compileExpression(e.Arguments[0]) // array
			c.compileExpression(e.Arguments[1]) // value
			c.emit(opArrayPush, 0, c.curLine)
			return
		}
		if ident.Value == "delete" {
			if len(e.Arguments) != 2 {
				c.addError("delete expects exactly 2 arguments")
				return
			}
			c.compileExpression(e.Arguments[0]) // map
			c.compileExpression(e.Arguments[1]) // key
			c.emit(opMapDelete, 0, c.curLine)
			return
		}
		callName := ident.Value
		// Value-call path: if the callee identifier resolves to a local variable
		// or a global var (not a function), the callee is a runtime value —
		// typically a lambda/closure stored in that slot. opCallValue expects
		// args first and the callee pushed last (on top of the stack).
		if c.resolveLocal(ident.Value) >= 0 {
			// Function-typed variables validate call arguments against
			// their declared signature at compile time.
			c.checkFunTypeCallArgs(ident.Value, e.Arguments)
			for _, arg := range e.Arguments {
				c.compileExpression(arg)
			}
			c.compileExpression(e.Callee)
			c.emit(opCallValue, int32(len(e.Arguments)), c.curLine)
			return
		}
		if _, isGlobal := c.globals[ident.Value]; isGlobal {
			if _, importedCallable := c.importedCallables[ident.Value]; !importedCallable {
				if _, importedNative := c.importedNatives[ident.Value]; !importedNative {
					if _, isFn := c.functions[ident.Value]; !isFn {
						c.checkFunTypeCallArgs(ident.Value, e.Arguments)
						for _, arg := range e.Arguments {
							c.compileExpression(arg)
						}
						c.compileExpression(e.Callee)
						c.emit(opCallValue, int32(len(e.Arguments)), c.curLine)
						return
					}
				}
			}
		}
		if target, ok := c.importedCallables[ident.Value]; ok {
			callName = target
		} else if target, ok := c.importedNatives[ident.Value]; ok {
			callName = target
		}
		// Compile arguments.
		for _, arg := range e.Arguments {
			c.compileExpression(arg)
		}
		if calleeChunk, ok := c.functions[callName]; ok {
			// Lambda arguments are validated against function-typed
			// parameters of the callee (higher-order callables).
			if info, ok := c.funcInfo[callName]; ok && len(info.paramTypes) > 0 {
				c.checkDirectCallFunTypeParams(callName, info.paramTypes, e.Arguments)
			}
			funcID := c.chunk.addFunctionRef(callName, calleeChunk)
			operand := int32(len(e.Arguments))<<16 | int32(funcID)
			c.emit(opCallDirect, operand, c.curLine)
			return
		}
		// Emit call by name.
		nameIdx := c.chunk.addConstant(callName)
		operand := int32(len(e.Arguments))<<16 | int32(nameIdx)
		c.emit(opCall, operand, c.curLine)
		return
	}

	// Check if it's a method call (obj.method(args)).
	if member, ok := e.Callee.(*frontend.MemberExpr); ok {
		if object, ok := member.Object.(*frontend.IdentExpr); ok {
			if _, importedNativeValue := c.nativeValues[object.Value]; importedNativeValue {
				c.compileObjectForChain(member)
				for _, arg := range e.Arguments {
					c.compileExpression(arg)
				}
				methodNameIdx := c.chunk.addConstant(member.Member.Value)
				operand := int32(len(e.Arguments))<<16 | int32(methodNameIdx)
				c.checkMethodAccess(member.Object, member.Member.Value)
				c.emit(opCallMethod, operand, c.curLine)
				return
			}
			if _, knownNamespace := c.nativeCapabilities[object.Value]; knownNamespace {
				if c.resolveLocal(object.Value) < 0 {
					if _, ok := c.globals[object.Value]; !ok {
						if !member.Optional {
							idx := c.chunk.addConstant(object.Value)
							c.emit(opPushString, int32(idx), c.curLine)
							for _, arg := range e.Arguments {
								c.compileExpression(arg)
							}
							methodNameIdx := c.chunk.addConstant(member.Member.Value)
							operand := int32(len(e.Arguments))<<16 | int32(methodNameIdx)
							c.emit(opCallMethod, operand, c.curLine)
							return
						}
					}
				}
			}
		}
		c.compileObjectForChain(member)
		for _, arg := range e.Arguments {
			c.compileExpression(arg)
		}
		methodNameIdx := c.chunk.addConstant(member.Member.Value)
		operand := int32(len(e.Arguments))<<16 | int32(methodNameIdx)
		c.checkMethodAccess(member.Object, member.Member.Value)
		c.emit(opCallMethod, operand, c.curLine)
		return
	}

	// General case: value call. opCallValue expects args first and the
	// callee pushed last (on top of the stack). A callee spine that still
	// carries an un-wrapped optional link (e.g. `x?.m(y)(z)`) would emit a
	// null short-circuit jump to the outer chain end with the already
	// pushed arguments stranded below — reject instead of corrupting the
	// operand stack.
	if directOptionalLink(e.Callee) {
		c.addError("cannot call the result of an optional chain directly; assign it to a variable first")
		return
	}
	for _, arg := range e.Arguments {
		c.compileExpression(arg)
	}
	c.compileExpression(e.Callee)
	c.emit(opCallValue, int32(len(e.Arguments)), c.curLine)
}

// directOptionalLink reports whether compiling expr as a value can emit a
// null short-circuit jump that targets an enclosing optional-chain end
// (rather than an inner, self-balanced wrapper). Such jumps must not run
// while caller-owned values sit below on the operand stack.
func directOptionalLink(expr frontend.Expression) bool {
	switch e := expr.(type) {
	case *frontend.OptionalChainExpr:
		return false // inner wrapper isolates and patches its own jumps
	case *frontend.MemberExpr:
		return e.Optional
	case *frontend.CallExpr:
		return directOptionalLink(e.Callee)
	case *frontend.IndexExpr:
		return directOptionalLink(e.Left)
	case *frontend.NullCoalesceExpr:
		return false // balanced: both branches leave exactly one value
	}
	return false
}

func (c *compiler) isMapExpressionInContext(expr frontend.Expression) bool {
	currentClass := c.currentClassName
	if currentClass == "" && c.chunk != nil && strings.Contains(c.chunk.sourceName, ".") {
		if idx := strings.Index(c.chunk.sourceName, "."); idx > 0 {
			currentClass = c.chunk.sourceName[:idx]
		}
	}
	if currentClass == "" {
		return c.isMapExpression(expr)
	}
	shadow := *c
	shadow.currentClassName = currentClass
	return shadow.isMapExpression(expr)
}

func (c *compiler) compileMemberExpr(e *frontend.MemberExpr) {
	// Enum member access (`Color.Red`): when the object identifier names an
	// enum (declared or imported) and does not shadow a local/global/native
	// binding, the member must be one of the enum's members and compiles to
	// a tagged enum-value constant.
	if id, ok := e.Object.(*frontend.IdentExpr); ok {
		if enumName, enumOK := c.resolveEnumName(id.Value); enumOK && !c.nameShadowsEnum(id.Value) {
			c.compileEnumMember(enumName, e.Member)
			return
		}
	}
	c.compileObjectForChain(e)
	c.checkFieldAccess(e.Object, e.Member.Value, "read")
	fieldNameIdx := c.chunk.addConstant(e.Member.Value)
	c.emit(opGetField, int32(fieldNameIdx), c.curLine)
}

// compileObjectForChain compiles the receiver of a memberExpr and, when the
// member is optional (`obj?.member`), emits opJumpIfNull. The jump is
// recorded on the compiler so compileOptionalChainExpr can patch it to the
// end of the enclosing chain; until then the operand is a placeholder that
// is always patched before the chunk is finalized. On the null path the
// null value itself stays on the stack and becomes the chain's result.
func (c *compiler) compileObjectForChain(member *frontend.MemberExpr) {
	c.compileExpression(member.Object)
	if member.Optional {
		jump := c.emit(opJumpIfNull, 0, c.curLine)
		c.optionalChainJumps = append(c.optionalChainJumps, jump)
	}
}

// compileOptionalChainExpr compiles a postfix chain containing at least one
// `?.` link. Each optional link emits opJumpIfNull targeting the end of the
// whole chain, so a null receiver anywhere skips every remaining link —
// including argument evaluation of subsequent calls — and leaves null as
// the chain's value.
func (c *compiler) compileOptionalChainExpr(e *frontend.OptionalChainExpr) {
	saved := c.optionalChainJumps
	c.optionalChainJumps = nil
	c.compileExpression(e.Expr)
	end := c.chunk.size()
	for _, jump := range c.optionalChainJumps {
		c.chunk.patchJump(jump, end)
	}
	c.optionalChainJumps = saved
}

// compileNullCoalesceExpr compiles `left ?? right`: when left is not null it
// is kept on the stack as the result and right is skipped; when left is
// null it is popped and right becomes the result.
func (c *compiler) compileNullCoalesceExpr(e *frontend.NullCoalesceExpr) {
	c.compileExpression(e.Left)
	jumpEnd := c.emit(opJumpIfNotNull, 0, c.curLine)
	c.emit(opPop, 0, c.curLine)
	c.compileExpression(e.Right)
	c.chunk.patchJump(jumpEnd, c.chunk.size())
}

// resolveEnumName maps a source name to a declared enum name, following
// import aliases. The second return is false when the name is not an enum.
func (c *compiler) resolveEnumName(name string) (string, bool) {
	if _, ok := c.enums[name]; ok {
		return name, true
	}
	if target, ok := c.importedEnums[name]; ok {
		return target, true
	}
	return "", false
}

// nameShadowsEnum reports whether the given source name is bound to a
// local, global, imported global, or native value, in which case those
// bindings take precedence over enum member resolution.
func (c *compiler) nameShadowsEnum(name string) bool {
	if c.resolveLocal(name) >= 0 {
		return true
	}
	if _, ok := c.globals[name]; ok {
		return true
	}
	if _, ok := c.importedGlobals[name]; ok {
		return true
	}
	if _, ok := c.nativeValues[name]; ok {
		return true
	}
	if _, ok := c.importedNatives[name]; ok {
		return true
	}
	return false
}

// compileEnumMember emits an opEnumValue constant for `EnumName.member`.
// Unknown members are compile-time errors (closed set).
func (c *compiler) compileEnumMember(enumName string, member *frontend.Ident) {
	info, ok := c.enums[enumName]
	if !ok {
		c.addCompileErrorWithTypes("unknown_enum", "bytecode/enum", fmt.Sprintf("enum %q is not declared", enumName), enumName, "")
		return
	}
	value, ok := info.memberValue(member.Value)
	if !ok {
		c.addCompileErrorWithTypes("unknown_enum_member", "bytecode/enum", fmt.Sprintf("enum %s has no member %q", enumName, member.Value), enumName+"."+member.Value, "")
		return
	}
	constIdx := c.chunk.addConstant(enumConst{enum: enumName, value: value})
	c.emit(opEnumValue, int32(constIdx), c.curLine)
}

func (c *compiler) nativeCallableNameForCall(e *frontend.CallExpr) (string, bool) {
	member, ok := e.Callee.(*frontend.MemberExpr)
	if !ok {
		return "", false
	}
	// Optional chains must compile the receiver and emit the null check,
	// so the qualified-name fast path (which never materializes the
	// receiver) does not apply.
	if member.Optional {
		return "", false
	}
	object, ok := member.Object.(*frontend.IdentExpr)
	if !ok {
		return "", false
	}
	callables, knownNamespace := c.nativeCapabilities[object.Value]
	if c.resolveLocal(object.Value) >= 0 {
		return "", false
	}
	if _, ok := c.globals[object.Value]; ok {
		return "", false
	}
	if !knownNamespace {
		return "", false
	}
	if _, ok := callables[member.Member.Value]; !ok {
		return "", false
	}
	return object.Value + "." + member.Member.Value, true
}

func (c *compiler) registerNativeCapability(desc invoke.CapabilityDesc) {
	if desc.Name == "" {
		return
	}
	callables := make(map[string]struct{}, len(desc.Callables))
	for _, callable := range desc.Callables {
		callables[callable.Name] = struct{}{}
	}
	c.nativeCapabilities[desc.Name] = callables
}

// checkFieldAccess checks if accessing the named field from the current
// compilation context would violate private access rules.
func (c *compiler) checkFieldAccess(object frontend.Expression, fieldName string, mode string) {
	lookup, ok := c.lookupFieldOnExpr(object, fieldName)
	if !ok {
		return
	}
	if lookup.access == accessPrivate && c.currentClassName != lookup.owner {
		c.addCompileError("private_field_access_denied", "bytecode/access/field", fmt.Sprintf("cannot %s private field '%s' of class '%s'", mode, fieldName, lookup.owner))
	}
}

// checkMethodAccess checks if calling the named method from the current
// compilation context would violate private access rules.
func (c *compiler) checkMethodAccess(object frontend.Expression, methodName string) {
	lookup, ok := c.lookupMethodOnExpr(object, methodName)
	if !ok {
		return
	}
	if lookup.access == accessPrivate && c.currentClassName != lookup.owner {
		c.addCompileError("private_method_access_denied", "bytecode/access/method", fmt.Sprintf("cannot call private method '%s' of class '%s'", methodName, lookup.owner))
	}
}

func (c *compiler) lookupFieldOnExpr(object frontend.Expression, fieldName string) (memberLookup, bool) {
	return c.lookupFieldOnType(c.expressionTypeName(object), fieldName)
}

func (c *compiler) lookupMethodOnExpr(object frontend.Expression, methodName string) (memberLookup, bool) {
	return c.lookupMethodOnType(c.expressionTypeName(object), methodName)
}

func (c *compiler) lookupFieldOnType(typeName string, fieldName string) (memberLookup, bool) {
	className := c.classNameFromType(typeName)
	for className != "" {
		cls, ok := c.classes[className]
		if !ok {
			return memberLookup{}, false
		}
		for _, f := range cls.fields {
			if f.name == fieldName {
				return memberLookup{owner: cls.name, access: f.access, typeName: c.resolveType(f.typeName), fieldInfo: f}, true
			}
		}
		className = cls.parent
	}
	return memberLookup{}, false
}

func (c *compiler) lookupMethodOnType(typeName string, methodName string) (memberLookup, bool) {
	className := c.classNameFromType(typeName)
	for className != "" {
		cls, ok := c.classes[className]
		if !ok {
			return memberLookup{}, false
		}
		for _, m := range cls.methods {
			if m.name == methodName {
				return memberLookup{owner: cls.name, access: m.access, returnType: c.resolveType(m.returnType), methodDecl: m}, true
			}
		}
		className = cls.parent
	}
	return memberLookup{}, false
}

func (c *compiler) lookupMethodOnAncestor(className string, methodName string) (memberLookup, bool) {
	for className != "" {
		cls, ok := c.classes[className]
		if !ok {
			return memberLookup{}, false
		}
		for _, m := range cls.methods {
			if m.name == methodName {
				return memberLookup{owner: cls.name, access: m.access, returnType: c.resolveType(m.returnType), methodDecl: m}, true
			}
		}
		className = cls.parent
	}
	return memberLookup{}, false
}

func (c *compiler) classNameFromType(typeName string) string {
	typeName = c.resolveType(typeName)
	if strings.HasPrefix(typeName, "array<") {
		return c.resolveType(elementTypeName(typeName))
	}
	if strings.HasPrefix(typeName, "map<") {
		return c.resolveType(mapValueTypeName(typeName))
	}
	if _, ok := c.classes[typeName]; ok {
		return typeName
	}
	return ""
}

func (c *compiler) isMapExpression(expr frontend.Expression) bool {
	switch e := expr.(type) {
	case *frontend.MapLiteral:
		return true
	case *frontend.IdentExpr:
		return c.localOrGlobalIsMap(e.Value)
	case *frontend.MemberExpr:
		return c.memberExpressionIsMap(e)
	case *frontend.IndexExpr:
		return c.indexExpressionIsMap(e)
	case *frontend.CallExpr:
		return c.callExpressionReturnsMap(e)
	case *frontend.OptionalChainExpr:
		return c.isMapExpression(e.Expr)
	case *frontend.NullCoalesceExpr:
		return c.isMapExpression(e.Left) || c.isMapExpression(e.Right)
	default:
		return false
	}
}

func (c *compiler) memberExpressionIsMap(expr *frontend.MemberExpr) bool {
	if expr == nil {
		return false
	}
	typeName := c.expressionTypeName(expr)
	return c.resolveType(typeName) == "map" || strings.HasPrefix(typeName, "map<")
}

func (c *compiler) localOrGlobalIsMap(name string) bool {
	typeName := c.localOrGlobalTypeName(name)
	return c.resolveType(typeName) == "map" || strings.HasPrefix(typeName, "map<")
}

func (c *compiler) localOrGlobalTypeName(name string) string {
	if typeName := c.localTypeName(name); typeName != "" {
		return typeName
	}
	if target, ok := c.importedGlobals[name]; ok {
		return c.globalTypes[target]
	}
	return c.globalTypes[name]
}

func (c *compiler) localTypeName(name string) string {
	for i := len(c.locals) - 1; i >= 0; i-- {
		if c.locals[i].name == name {
			return c.locals[i].typeName
		}
	}
	return ""
}

// isCellLocal reports whether the innermost local named name holds a capture
// cell. Fused local opcodes must not be used on cell locals.
func (c *compiler) isCellLocal(name string) bool {
	for i := len(c.locals) - 1; i >= 0; i-- {
		if c.locals[i].name == name {
			return c.locals[i].isCell
		}
	}
	return false
}

func (c *compiler) memberExprTypeName(expr *frontend.MemberExpr) string {
	if expr == nil {
		return ""
	}
	objectType := c.expressionTypeName(expr.Object)
	return c.memberOnObjectType(objectType, expr.Member.Value)
}

func (c *compiler) indexExpressionIsMap(expr *frontend.IndexExpr) bool {
	typeName := c.indexExpressionTypeName(expr)
	return c.resolveType(typeName) == "map" || strings.HasPrefix(typeName, "map<")
}

func (c *compiler) indexExpressionTypeName(expr *frontend.IndexExpr) string {
	if expr == nil {
		return ""
	}
	leftType := c.resolveType(c.expressionTypeName(expr.Left))
	if strings.HasPrefix(leftType, "array<") {
		return c.resolveType(elementTypeName(leftType))
	}
	if strings.HasPrefix(leftType, "map<") {
		return c.resolveType(mapValueTypeName(leftType))
	}
	if leftType == "array" || leftType == "map" {
		return ""
	}
	return ""
}

func (c *compiler) callExpressionReturnsMap(expr *frontend.CallExpr) bool {
	typeName := c.callExpressionReturnType(expr)
	return c.resolveType(typeName) == "map" || strings.HasPrefix(typeName, "map<")
}

func (c *compiler) callExpressionReturnType(expr *frontend.CallExpr) string {
	if expr == nil {
		return ""
	}
	if ident, ok := expr.Callee.(*frontend.IdentExpr); ok {
		if fn, ok := c.funcInfo[ident.Value]; ok {
			return fn.returnType
		}
	}
	if member, ok := expr.Callee.(*frontend.MemberExpr); ok {
		lookup, ok := c.lookupMethodOnExpr(member.Object, member.Member.Value)
		if ok {
			return lookup.returnType
		}
	}
	return ""
}

func (c *compiler) expressionTypeName(expr frontend.Expression) string {
	switch e := expr.(type) {
	case *frontend.IdentExpr:
		if e.Value == "this" {
			return c.currentClassName
		}
		return c.localOrGlobalTypeName(e.Value)
	case *frontend.ThisExpr:
		return c.currentClassName
	case *frontend.MemberExpr:
		return c.memberExprTypeName(e)
	case *frontend.IndexExpr:
		return c.indexExpressionTypeName(e)
	case *frontend.CallExpr:
		return c.callExpressionReturnType(e)
	case *frontend.NullCoalesceExpr:
		// `a ?? b` has the type of the non-null branch when both sides
		// agree; otherwise the type is unknown.
		lt := c.expressionTypeName(e.Left)
		if lt == "" {
			return ""
		}
		if rt := c.expressionTypeName(e.Right); lt == rt {
			return lt
		}
		return ""
	case *frontend.OptionalChainExpr:
		// The chain yields the inner type on the non-null path.
		return c.expressionTypeName(e.Expr)
	case *frontend.MapLiteral:
		return "map"
	case *frontend.ArrayLiteral:
		return "array"
	case *frontend.IntLiteral:
		return "int"
	default:
		return ""
	}
}

func elementTypeName(typeName string) string {
	start := strings.Index(typeName, "<")
	end := strings.LastIndex(typeName, ">")
	if start < 0 || end <= start {
		return ""
	}
	inner := typeName[start+1 : end]
	depth := 0
	for i, r := range inner {
		switch r {
		case '<':
			depth++
		case '>':
			depth--
		case ',':
			if depth == 0 {
				return strings.TrimSpace(inner[:i])
			}
		}
	}
	return strings.TrimSpace(inner)
}

func mapValueTypeName(typeName string) string {
	start := strings.Index(typeName, "<")
	end := strings.LastIndex(typeName, ">")
	if start < 0 || end <= start {
		return ""
	}
	inner := typeName[start+1 : end]
	depth := 0
	for i, r := range inner {
		switch r {
		case '<':
			depth++
		case '>':
			depth--
		case ',':
			if depth == 0 {
				return strings.TrimSpace(inner[i+1:])
			}
		}
	}
	return ""
}

func (c *compiler) memberOnObjectType(objectTypeName, fieldName string) string {
	lookup, ok := c.lookupFieldOnType(objectTypeName, fieldName)
	if !ok {
		return ""
	}
	return lookup.typeName
}

func (c *compiler) compileIndexExpr(e *frontend.IndexExpr) {
	if c.tryCompileMapGetString(e) {
		return
	}
	if c.tryCompileArrayGetInt(e) {
		return
	}
	c.compileExpression(e.Left)
	c.compileExpression(e.Index)
	c.emit(opGetElement, 0, c.curLine)
}

func (c *compiler) tryCompileArrayGetInt(e *frontend.IndexExpr) bool {
	if !indexReceiverIsTypedArray(c.expressionTypeName(e.Left)) {
		return false
	}
	idx, ok := e.Index.(*frontend.IntLiteral)
	if !ok || idx.Value < 0 || idx.Value > 0xFFFF {
		return false
	}
	c.compileExpression(e.Left)
	c.emit(opArrayGetInt, int32(idx.Value), c.curLine)
	return true
}

func (c *compiler) tryCompileMapGetString(e *frontend.IndexExpr) bool {
	if !indexReceiverIsTypedMap(c.expressionTypeName(e.Left)) {
		return false
	}
	key, ok := e.Index.(*frontend.StringLiteral)
	if !ok {
		return false
	}
	c.compileExpression(e.Left)
	keyIdx := c.chunk.addConstant(key.Value)
	c.emit(opMapGetString, int32(keyIdx), c.curLine)
	return true
}

func (c *compiler) tryCompileMapSetString(target *frontend.IndexExpr, value frontend.Expression) bool {
	if !indexReceiverIsTypedMap(c.expressionTypeName(target.Left)) {
		return false
	}
	key, ok := target.Index.(*frontend.StringLiteral)
	if !ok {
		return false
	}
	c.compileExpression(target.Left)
	c.compileExpression(value)
	keyIdx := c.chunk.addConstant(key.Value)
	c.emit(opMapSetString, int32(keyIdx), c.curLine)
	return true
}

func (c *compiler) tryCompileArraySetInt(target *frontend.IndexExpr, value frontend.Expression) bool {
	if !indexReceiverIsTypedArray(c.expressionTypeName(target.Left)) {
		return false
	}
	idx, ok := target.Index.(*frontend.IntLiteral)
	if !ok || idx.Value < 0 || idx.Value > 0xFFFF {
		return false
	}
	c.compileExpression(target.Left)
	c.compileExpression(value)
	c.emit(opArraySetInt, int32(idx.Value), c.curLine)
	return true
}

func indexReceiverIsTypedMap(typeName string) bool {
	return strings.HasPrefix(typeName, "map<")
}

func indexReceiverIsTypedArray(typeName string) bool {
	return strings.HasPrefix(typeName, "array<")
}

// isIncDecPattern detects `x = x + 1` and `x = x - 1`.
func isIncDecPattern(target string, val frontend.Expression) (inc bool, ok bool) {
	bin, ok := val.(*frontend.BinaryExpr)
	if !ok {
		return false, false
	}
	left, ok := bin.Left.(*frontend.IdentExpr)
	if !ok || left.Value != target {
		return false, false
	}
	lit, ok := bin.Right.(*frontend.IntLiteral)
	if !ok || lit.Value != 1 {
		return false, false
	}
	switch bin.Operator {
	case "+":
		return true, true
	case "-":
		return false, true
	}
	return false, false
}

func packLocalPair(dst, src int) (int32, bool) {
	if dst < 0 || src < 0 || dst > 0xFFFF || src > 0xFFFF {
		return 0, false
	}
	return int32(uint32(dst)&0xFFFF | (uint32(src)&0xFFFF)<<16), true
}

func unpackLocalPair(operand int32) (dst, src int) {
	raw := uint32(operand)
	return int(raw & 0xFFFF), int((raw >> 16) & 0xFFFF)
}

func packLocalLocalTarget(left, right, target int) (int32, bool) {
	if left < 0 || right < 0 || target < 0 || left > 0xFF || right > 0xFF || target > 0xFFFF {
		return 0, false
	}
	return int32(uint32(left)&0xFF | (uint32(right)&0xFF)<<8 | (uint32(target)&0xFFFF)<<16), true
}

func unpackLocalLocalTarget(operand int32) (left, right, target int) {
	raw := uint32(operand)
	return int(raw & 0xFF), int((raw >> 8) & 0xFF), int((raw >> 16) & 0xFFFF)
}

func (c *compiler) tryEmitLocalLocalIntLtLoopBranch(condition frontend.Expression, target int) bool {
	bin, ok := condition.(*frontend.BinaryExpr)
	if !ok || bin.Operator != "<" {
		return false
	}
	left, ok := bin.Left.(*frontend.IdentExpr)
	if !ok || c.localTypeName(left.Value) != "int" {
		return false
	}
	right, ok := bin.Right.(*frontend.IdentExpr)
	if !ok || c.localTypeName(right.Value) != "int" {
		return false
	}
	if c.isCellLocal(left.Value) || c.isCellLocal(right.Value) {
		return false
	}
	leftIdx := c.resolveLocal(left.Value)
	rightIdx := c.resolveLocal(right.Value)
	operand, ok := packLocalLocalTarget(leftIdx, rightIdx, target)
	if !ok {
		return false
	}
	c.emit(opJumpLocalLtInt, operand, c.curLine)
	return true
}

func (c *compiler) tryCompileAddLocalIntAssign(target string, val frontend.Expression) bool {
	if c.localTypeName(target) != "int" {
		return false
	}
	if c.isCellLocal(target) {
		return false
	}
	dst := c.resolveLocal(target)
	if dst < 0 {
		return false
	}
	bin, ok := val.(*frontend.BinaryExpr)
	if !ok || bin.Operator != "+" {
		return false
	}
	left, ok := bin.Left.(*frontend.IdentExpr)
	if !ok || left.Value != target {
		return false
	}
	right, ok := bin.Right.(*frontend.IdentExpr)
	if !ok || c.localTypeName(right.Value) != "int" {
		return false
	}
	src := c.resolveLocal(right.Value)
	if src < 0 {
		return false
	}
	operand, ok := packLocalPair(dst, src)
	if !ok {
		return false
	}
	c.emit(opAddLocalInt, operand, c.curLine)
	return true
}

func (c *compiler) tryCompileConcatLocalConstStringAssign(target string, val frontend.Expression) bool {
	if c.localTypeName(target) != "string" {
		return false
	}
	if c.isCellLocal(target) {
		return false
	}
	dst := c.resolveLocal(target)
	if dst < 0 {
		return false
	}
	bin, ok := val.(*frontend.BinaryExpr)
	if !ok || bin.Operator != "+" {
		return false
	}
	// target + "literal" or "literal" + target
	var constIdx int
	found := false
	if left, ok := bin.Left.(*frontend.IdentExpr); ok && left.Value == target {
		if lit, ok := bin.Right.(*frontend.StringLiteral); ok {
			constIdx = c.chunk.addConstant(lit.Value)
			found = true
		}
	}
	if !found {
		if right, ok := bin.Right.(*frontend.IdentExpr); ok && right.Value == target {
			if lit, ok := bin.Left.(*frontend.StringLiteral); ok {
				constIdx = c.chunk.addConstant(lit.Value)
				found = true
			}
		}
	}
	if !found {
		return false
	}
	operand, ok := packLocalPair(dst, constIdx)
	if !ok {
		return false
	}
	c.emit(opConcatLocalConstString, operand, c.curLine)
	return true
}

func (c *compiler) compileAssignExpr(e *frontend.AssignExpr) {
	// Determine the target.
	switch target := e.Target.(type) {
	case *frontend.IdentExpr:
		if _, imported := c.importedGlobals[target.Value]; imported {
			c.addCompileError("assign_imported_variable", "bytecode/scope/import", fmt.Sprintf("cannot assign imported variable: %s", target.Value))
			return
		}
		if _, imported := c.nativeValues[target.Value]; imported {
			c.addCompileError("assign_imported_variable", "bytecode/scope/import", fmt.Sprintf("cannot assign imported variable: %s", target.Value))
			return
		}
		// Function-typed targets validate the assigned value at compile time.
		if targetType := c.localOrGlobalTypeName(target.Value); isFunTypeName(targetType) {
			c.checkFunTypeValueAssign(targetType, target.Value, e.Value)
		}
		// Capture-cell locals bypass all fused local mutators below: those
		// opcodes operate on raw local slots, not cell contents.
		if cellIdx := c.resolveLocal(target.Value); cellIdx >= 0 && c.locals[cellIdx].isCell {
			c.compileExpression(e.Value)
			c.emit(opStoreCell, int32(cellIdx), c.curLine)
			return
		}
		// Fast path: x = x + 1  →  INC_LOCAL
		//            x = x - 1  →  DEC_LOCAL
		if inc, ok := isIncDecPattern(target.Value, e.Value); ok {
			if localIdx := c.resolveLocal(target.Value); localIdx >= 0 {
				if inc {
					c.emit(opIncLocal, int32(localIdx), c.curLine)
				} else {
					c.emit(opDecLocal, int32(localIdx), c.curLine)
				}
				return
			}
		}
		if c.tryCompileAddLocalIntAssign(target.Value, e.Value) {
			return
		}
		if c.tryCompileConcatLocalConstStringAssign(target.Value, e.Value) {
			return
		}
		c.compileExpression(e.Value)
		if localIdx := c.resolveLocal(target.Value); localIdx >= 0 {
			c.emit(opStoreLocal, int32(localIdx), c.curLine)
		} else if globalIdx, ok := c.globals[target.Value]; ok {
			c.emit(opStoreGlobal, int32(globalIdx), c.curLine)
		} else {
			c.addCompileError("undefined_variable", "bytecode/scope/identifier", fmt.Sprintf("undefined variable: %s", target.Value))
		}
	case *frontend.MemberExpr:
		if target.Optional {
			c.addError("optional chaining cannot be used as an assignment target")
			return
		}
		c.compileExpression(target.Object)
		c.compileExpression(e.Value)
		fieldNameIdx := c.chunk.addConstant(target.Member.Value)
		c.checkFieldAccess(target.Object, target.Member.Value, "write")
		c.emit(opSetField, int32(fieldNameIdx), c.curLine)
	case *frontend.OptionalChainExpr:
		c.addError("optional chaining cannot be used as an assignment target")
	case *frontend.IndexExpr:
		if c.tryCompileMapSetString(target, e.Value) {
			return
		}
		if c.tryCompileArraySetInt(target, e.Value) {
			return
		}
		c.compileExpression(target.Left)
		c.compileExpression(target.Index)
		c.compileExpression(e.Value)
		c.emit(opSetElement, 0, c.curLine)
	default:
		c.addError("invalid assignment target")
	}
}

func (c *compiler) compileTypeCheckExpr(e *frontend.TypeCheckExpr) {
	c.compileExpression(e.Left)
	typeNameIdx := c.chunk.addConstant(e.TypeName)
	c.emit(opIs, int32(typeNameIdx), c.curLine)
}

func (c *compiler) compileTypeCastExpr(e *frontend.TypeCastExpr) {
	c.compileExpression(e.Left)
	typeNameIdx := c.chunk.addConstant(e.Target.Name)
	c.emit(opAs, int32(typeNameIdx), c.curLine)
}

func (c *compiler) compileThisExpr() {
	localIdx := c.resolveLocal("this")
	if localIdx >= 0 {
		if c.locals[localIdx].isCell {
			// `this` captured by a lambda arrives as a capture cell.
			c.emit(opLoadCell, int32(localIdx), c.curLine)
		} else {
			c.emit(opLoadLocal, int32(localIdx), c.curLine)
		}
	} else {
		c.emit(opPushNull, 0, c.curLine)
	}
}

func (c *compiler) compileArrayLiteral(e *frontend.ArrayLiteral) {
	for _, elem := range e.Elements {
		c.compileExpression(elem)
	}
	c.emit(opNewArray, int32(len(e.Elements)), c.curLine)
}

func (c *compiler) compileMapLiteral(e *frontend.MapLiteral) {
	// Create empty map on stack.
	c.emit(opNewMap, 0, c.curLine)
	for _, pair := range e.Pairs {
		// Duplicate map reference for opMapSet.
		c.emit(opDup, 0, c.curLine)
		// Stack: [map, map]
		c.compileExpression(pair.Key)
		// Stack: [map, map, key]
		c.compileExpression(pair.Value)
		// Stack: [map, map, key, value]
		c.emit(opMapSet, 0, c.curLine)
		// Stack: [map]
	}
}

func (c *compiler) compileStructLiteral(e *frontend.StructLiteral) {
	// Look up struct in registry.
	structName := e.TypeName
	structInfo, ok := c.structs[structName]
	if !ok {
		c.addCompileError("unknown_struct_type", "bytecode/struct/literal", fmt.Sprintf("unknown struct type: %s", structName))
		return
	}

	// Build field name list for the struct.
	fieldCount := len(structInfo.fields)

	// Push all field values in declaration order.
	fieldMap := make(map[string]frontend.Expression)
	for _, f := range e.Fields {
		if _, exists := fieldMap[f.Name]; exists {
			c.addCompileError("duplicate_struct_field", "bytecode/struct/literal", fmt.Sprintf("duplicate field %q for struct %q", f.Name, structName))
			return
		}
		found := false
		for _, declared := range structInfo.fields {
			if f.Name == declared.name {
				found = true
				break
			}
		}
		if !found {
			c.addCompileError("unknown_struct_field", "bytecode/struct/literal", fmt.Sprintf("unknown field %q for struct %q", f.Name, structName))
			return
		}
		fieldMap[f.Name] = f.Value
	}
	for i := 0; i < fieldCount; i++ {
		field := structInfo.fields[i]
		if valExpr, found := fieldMap[field.name]; found {
			if !structFieldValueMatchesType(valExpr, field.typeName) {
				actualType := exprTypeName(valExpr)
				c.addCompileErrorWithTypes("struct_field_type_mismatch", "bytecode/struct/literal", fmt.Sprintf("field %q for struct %q expects %q", field.name, structName, field.typeName), field.typeName, actualType)
				return
			}
			prevHint := c.typeHint
			resolved := c.resolveType(field.typeName)
			if resolved == "long" || resolved == "ulong" || resolved == "double" {
				c.typeHint = resolved
			} else {
				c.typeHint = ""
			}
			c.compileExpression(valExpr)
			c.typeHint = prevHint
		} else if e.HasZeroFill {
			c.emitZeroValue(field.typeName)
		} else {
			c.addCompileError("missing_struct_field", "bytecode/struct/literal", fmt.Sprintf("missing required field %q for struct %q", field.name, structName))
			return
		}
	}

	// Create struct instance.
	nameIdx := c.chunk.addConstant(structName)
	c.emit(opNewStructInstance, int32(nameIdx), c.curLine)
}

// emitZeroValue pushes the typed zero value for the given declared field type.
// Used by zero-fill struct literal spread (e.g. `Point{x: 1, ..}`) to fill
// fields the author chose to leave at their type's default. Container types
// produce empty containers; struct/class/null/unknown references produce a
// null handle.
func (c *compiler) emitZeroValue(typeName string) {
	resolved := c.resolveType(typeName)
	switch resolved {
	case "int":
		idx := c.chunk.addConstant(int32(0))
		c.emit(opPushInt, int32(idx), c.curLine)
	case "long":
		idx := c.chunk.addConstant(int64(0))
		c.emit(opPushLong, int32(idx), c.curLine)
	case "ulong":
		idx := c.chunk.addConstant(uint64(0))
		c.emit(opPushULong, int32(idx), c.curLine)
	case "uint":
		idx := c.chunk.addConstant(int32(0))
		c.emit(opPushInt, int32(idx), c.curLine)
	case "short", "byte", "ushort":
		idx := c.chunk.addConstant(int32(0))
		c.emit(opPushInt, int32(idx), c.curLine)
	case "float":
		idx := c.chunk.addConstant(float32(0))
		c.emit(opPushFloat, int32(idx), c.curLine)
	case "double":
		idx := c.chunk.addConstant(float64(0))
		c.emit(opPushDouble, int32(idx), c.curLine)
	case "bool":
		c.emit(opPushFalse, 0, c.curLine)
	case "string", "bytes":
		idx := c.chunk.addConstant("")
		c.emit(opPushString, int32(idx), c.curLine)
	default:
		if strings.HasPrefix(resolved, "array<") {
			c.emit(opNewArray, 0, c.curLine)
			return
		}
		if strings.HasPrefix(resolved, "map<") {
			c.emit(opNewMap, 0, c.curLine)
			return
		}
		// struct/class/unknown ref/null → null handle
		c.emit(opPushNull, 0, c.curLine)
	}
}
func exprTypeName(expr frontend.Expression) string {
	switch expr.(type) {
	case *frontend.IntLiteral:
		return "int"
	case *frontend.FloatLiteral:
		return "float"
	case *frontend.StringLiteral:
		return "string"
	case *frontend.BoolLiteral:
		return "bool"
	case *frontend.NullLiteral:
		return "null"
	case *frontend.ArrayLiteral:
		return "array"
	case *frontend.MapLiteral:
		return "map"
	case *frontend.StructLiteral:
		return "struct"
	default:
		return "unknown"
	}
}

func structFieldValueMatchesType(expr frontend.Expression, typeName string) bool {
	// Only perform strict literal type checking for literal expressions.
	// Runtime expressions (variables, calls, etc.) pass through.
	switch expr.(type) {
	case *frontend.IntLiteral, *frontend.FloatLiteral, *frontend.StringLiteral,
		*frontend.BoolLiteral, *frontend.NullLiteral, *frontend.ArrayLiteral,
		*frontend.MapLiteral, *frontend.StructLiteral:
		// literal — continue to type check
	default:
		return true
	}

	switch typeName {
	case "", "any":
		return true
	case "int", "long", "ulong", "byte", "short", "ushort", "uint":
		_, ok := expr.(*frontend.IntLiteral)
		return ok
	case "float", "double":
		switch expr.(type) {
		case *frontend.FloatLiteral, *frontend.IntLiteral:
			return true
		}
		return false
	case "string":
		_, ok := expr.(*frontend.StringLiteral)
		return ok
	case "bool":
		_, ok := expr.(*frontend.BoolLiteral)
		return ok
	}
	return true
}

func (c *compiler) compileNewExpr(e *frontend.NewExpr) {
	classNameIdx := c.chunk.addConstant(e.ClassName)
	c.emit(opNewObject, int32(classNameIdx), c.curLine)

	// Two-step: allocate + call constructor when one is declared.
	if c.classHasConstructor(e.ClassName) {
		c.emit(opDup, 0, c.curLine)
		for _, arg := range e.Arguments {
			c.compileExpression(arg)
		}
		ctorNameIdx := c.chunk.addConstant(e.ClassName)
		operand := int32(len(e.Arguments))<<16 | int32(ctorNameIdx)
		c.emit(opCallMethod, operand, c.curLine)
		c.emit(opPop, 0, c.curLine) // discard constructor return
	}
}

func (c *compiler) classHasConstructor(className string) bool {
	info, ok := c.classes[className]
	if !ok {
		return false
	}
	for _, method := range info.methods {
		if method.name == className {
			return true
		}
	}
	return false
}

func (c *compiler) compileSuperExpr(e *frontend.SuperExpr) {
	// Load "this" (local 0 in method scope).
	c.emit(opLoadLocal, 0, c.curLine)

	// Compile arguments.
	for _, arg := range e.Arguments {
		c.compileExpression(arg)
	}

	if e.Method == nil {
		// super() — call parent constructor.
		// Constructor method name equals the parent class name.
		classInfo, ok := c.classes[c.currentClassName]
		if !ok || classInfo.parent == "" {
			return
		}
		ctorNameIdx := c.chunk.addConstant(classInfo.parent)
		operand := int32(len(e.Arguments))<<16 | int32(ctorNameIdx)
		c.emit(opCallSuperMethod, operand, c.curLine)
		return
	}

	methodNameIdx := c.chunk.addConstant(e.Method.Value)
	operand := int32(len(e.Arguments))<<16 | int32(methodNameIdx)
	c.emit(opCallSuperMethod, operand, c.curLine)
}

func (c *compiler) compileGlobalAssign(stmt *frontend.ExprStatement) {
	if stmt == nil {
		return
	}
	assign, ok := stmt.Expr.(*frontend.AssignExpr)
	if !ok {
		c.addError("expected assignment at top level")
		return
	}
	c.compileAssignExpr(assign)
}

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

func init() {
	diagnostics.RegisterCode(diagnostics.CodeInfo{Code: "compile_error", Category: diagnostics.CategorySchema, Description: "Bytecode compilation failed", Hint: "检查 bytecode 编译错误列表并修正对应源代码"})
	diagnostics.RegisterCode(diagnostics.CodeInfo{Code: "type_alias_cycle", Category: diagnostics.CategorySchema, Description: "Type alias definitions formed a cycle", Hint: "打破 type alias 循环引用，使其最终指向具体类型"})
	diagnostics.RegisterCode(diagnostics.CodeInfo{Code: "unknown_type_alias_target", Category: diagnostics.CategorySchema, Description: "Type alias referenced an unknown target type", Hint: "将 type alias 指向已声明的类型"})
	diagnostics.RegisterCode(diagnostics.CodeInfo{Code: "override_without_parent", Category: diagnostics.CategorySchema, Description: "Override method was declared without a parent class", Hint: "为 override 方法声明可继承的父类，或移除 override"})
	diagnostics.RegisterCode(diagnostics.CodeInfo{Code: "parent_class_not_found", Category: diagnostics.CategorySchema, Description: "Declared parent class could not be found", Hint: "确认父类名称正确且已在当前模块或导入模块中声明"})
	diagnostics.RegisterCode(diagnostics.CodeInfo{Code: "override_signature_mismatch", Category: diagnostics.CategorySchema, Description: "Override method signature did not match the parent method", Hint: "将 override 方法的参数和返回类型改为与父类方法一致"})
	diagnostics.RegisterCode(diagnostics.CodeInfo{Code: "override_parent_method_not_open", Category: diagnostics.CategorySchema, Description: "Parent method was not open for overriding", Hint: "将父类方法标记为 open，或移除子类 override"})
	diagnostics.RegisterCode(diagnostics.CodeInfo{Code: "override_method_not_found", Category: diagnostics.CategorySchema, Description: "Override target method was not found on the parent class", Hint: "确认父类中存在同名方法，或移除 override"})
	diagnostics.RegisterCode(diagnostics.CodeInfo{Code: "unknown_interface", Category: diagnostics.CategorySchema, Description: "Implemented interface could not be found", Hint: "确认 interface 名称正确且已声明或导入"})
	diagnostics.RegisterCode(diagnostics.CodeInfo{Code: "interface_signature_mismatch", Category: diagnostics.CategorySchema, Description: "Class method signature did not satisfy an interface method", Hint: "将实现方法的参数和返回类型改为与 interface 定义一致"})
	diagnostics.RegisterCode(diagnostics.CodeInfo{Code: "interface_method_missing", Category: diagnostics.CategorySchema, Description: "Class was missing a required interface method", Hint: "为 class 补充缺失的 interface 方法实现"})
	diagnostics.RegisterCode(diagnostics.CodeInfo{Code: "super_without_parent", Category: diagnostics.CategorySchema, Description: "super was used without a parent class", Hint: "仅在存在父类的 class 中使用 super"})
	diagnostics.RegisterCode(diagnostics.CodeInfo{Code: "break_outside_loop", Category: diagnostics.CategorySchema, Description: "break was used outside a loop", Hint: "将 break 放入循环体内，或改用 return/条件分支"})
	diagnostics.RegisterCode(diagnostics.CodeInfo{Code: "continue_outside_loop", Category: diagnostics.CategorySchema, Description: "continue was used outside a loop", Hint: "将 continue 放入循环体内"})
	diagnostics.RegisterCode(diagnostics.CodeInfo{Code: "undefined_imported_variable", Category: diagnostics.CategorySchema, Description: "Referenced imported variable was not found", Hint: "确认 import 的符号名称正确且已导出"})
	diagnostics.RegisterCode(diagnostics.CodeInfo{Code: "undefined_variable", Category: diagnostics.CategorySchema, Description: "Referenced variable was not defined", Hint: "在使用前声明变量，或修正变量名"})
	diagnostics.RegisterCode(diagnostics.CodeInfo{Code: "private_field_access_denied", Category: diagnostics.CategorySchema, Description: "Attempted to access a private field from an invalid context", Hint: "仅在类内部访问 private 字段，或调整字段可见性"})
	diagnostics.RegisterCode(diagnostics.CodeInfo{Code: "private_method_access_denied", Category: diagnostics.CategorySchema, Description: "Attempted to call a private method from an invalid context", Hint: "仅在类内部调用 private 方法，或调整方法可见性"})
	diagnostics.RegisterCode(diagnostics.CodeInfo{Code: "assign_imported_variable", Category: diagnostics.CategorySchema, Description: "Attempted to assign to an imported variable", Hint: "不要给 import 得到的变量重新赋值，改为写入本地变量"})
	diagnostics.RegisterCode(diagnostics.CodeInfo{Code: "duplicate_struct_field", Category: diagnostics.CategorySchema, Description: "Struct literal provided the same field more than once", Hint: "在 struct 字面量中移除重复字段"})
	diagnostics.RegisterCode(diagnostics.CodeInfo{Code: "unknown_struct_field", Category: diagnostics.CategorySchema, Description: "Struct literal referenced an unknown field", Hint: "将字段名改为 struct 中已声明的字段"})
	diagnostics.RegisterCode(diagnostics.CodeInfo{Code: "struct_field_type_mismatch", Category: diagnostics.CategorySchema, Description: "Struct literal field type did not match the schema", Hint: "将 struct 字段值类型从 actual 改为 expected"})
	diagnostics.RegisterCode(diagnostics.CodeInfo{Code: "missing_struct_field", Category: diagnostics.CategorySchema, Description: "Struct literal omitted a required field", Hint: "为 struct 字面量补充缺失的必填字段"})
	diagnostics.RegisterCode(diagnostics.CodeInfo{Code: "unknown_struct_type", Category: diagnostics.CategorySchema, Description: "Struct literal referenced a struct type that was not declared", Hint: "将 struct 字面量类型名改为已声明的 struct，或在模块中先声明该 struct"})
	diagnostics.RegisterCode(diagnostics.CodeInfo{Code: "inherit_non_open_class", Category: diagnostics.CategorySchema, Description: "Class attempted to inherit from a non-open class", Hint: "仅继承 open class，或移除 extends"})
	diagnostics.RegisterCode(diagnostics.CodeInfo{Code: "override_visibility_narrowed", Category: diagnostics.CategorySchema, Description: "Override method narrowed parent method visibility", Hint: "不要让 override 方法以 private 收紧父类方法的可见性"})
	diagnostics.RegisterCode(diagnostics.CodeInfo{Code: "method_visibility_narrowed", Category: diagnostics.CategorySchema, Description: "Method shadowed an ancestor method with narrower visibility", Hint: "不要以更窄的可见性覆盖祖先同名方法,或重命名当前方法"})
	diagnostics.RegisterCode(diagnostics.CodeInfo{Code: "field_shadowing_disallowed", Category: diagnostics.CategorySchema, Description: "Field shadowed an ancestor field", Hint: "重命名当前类字段以避免与祖先字段同名"})
	diagnostics.RegisterCode(diagnostics.CodeInfo{Code: "yield_requires_stream_fun", Category: diagnostics.CategorySchema, Description: "yield was used outside a stream fun", Hint: "仅在 stream fun 内使用 yield，或将函数声明为 stream"})
	diagnostics.RegisterCode(diagnostics.CodeInfo{Code: "yield_disallowed_in_try", Category: diagnostics.CategorySchema, Description: "yield was used inside a try block", Hint: "将易错片段移入辅助 fun 并 yield 其结果；catch 上下文无法跨流挂起保存"})
	diagnostics.RegisterCode(diagnostics.CodeInfo{Code: "yield_value_required", Category: diagnostics.CategorySchema, Description: "yield in a stream fun did not provide a value", Hint: "为 stream fun 的 yield 提供返回值"})
	diagnostics.RegisterCode(diagnostics.CodeInfo{Code: "function_type_arity_mismatch", Category: diagnostics.CategorySchema, Description: "Function-typed value used with a mismatched parameter count", Hint: "使 lambda 参数个数与 fun 类型签名的参数个数一致"})
	diagnostics.RegisterCode(diagnostics.CodeInfo{Code: "unknown_enum", Category: diagnostics.CategorySchema, Description: "Enum referenced in code was not declared", Hint: "确认 enum 名称正确且已在当前模块或导入模块中声明"})
	diagnostics.RegisterCode(diagnostics.CodeInfo{Code: "unknown_enum_member", Category: diagnostics.CategorySchema, Description: "Enum member referenced was not part of the enum's closed set", Hint: "将成员名改为 enum 中已声明的成员"})
	diagnostics.RegisterCode(diagnostics.CodeInfo{Code: "duplicate_enum_member", Category: diagnostics.CategorySchema, Description: "Enum declared the same member name more than once", Hint: "移除重复的 enum 成员名"})
	diagnostics.RegisterCode(diagnostics.CodeInfo{Code: "duplicate_enum_member_value", Category: diagnostics.CategorySchema, Description: "Enum members shared the same underlying value", Hint: "为 enum 成员指定互不相同的底层值"})
	diagnostics.RegisterCode(diagnostics.CodeInfo{Code: "enum_value_out_of_range", Category: diagnostics.CategorySchema, Description: "Enum member explicit value exceeded the int range", Hint: "将 enum 成员的显式赋值限制在 int32 范围内"})
	diagnostics.RegisterCode(diagnostics.CodeInfo{Code: "function_type_param_mismatch", Category: diagnostics.CategorySchema, Description: "Lambda parameter type did not match the fun type signature", Hint: "将 lambda 参数类型改为与 fun 类型签名一致（或 any）"})
	diagnostics.RegisterCode(diagnostics.CodeInfo{Code: "function_type_return_mismatch", Category: diagnostics.CategorySchema, Description: "Lambda return type did not match the fun type signature", Hint: "将 lambda 返回类型改为与 fun 类型签名一致（或 any）"})
	diagnostics.RegisterCode(diagnostics.CodeInfo{Code: "function_type_arg_mismatch", Category: diagnostics.CategorySchema, Description: "Call argument did not match the fun type signature", Hint: "将调用实参类型改为与 fun 类型签名一致"})
	diagnostics.RegisterCode(diagnostics.CodeInfo{Code: "function_type_mismatch", Category: diagnostics.CategorySchema, Description: "Value assigned to a function-typed slot was not compatible", Hint: "将赋值改为兼容的 lambda 或函数类型值"})
	diagnostics.RegisterCode(diagnostics.CodeInfo{Code: "return_in_defer", Category: diagnostics.CategorySchema, Description: "return was used inside a defer body", Hint: "移除 defer 块内的 return；defer 会在函数退出时自动执行"})
	diagnostics.RegisterCode(diagnostics.CodeInfo{Code: "yield_in_defer", Category: diagnostics.CategorySchema, Description: "yield was used inside a defer body", Hint: "移除 defer 块内的 yield；defer 块不能挂起执行"})
	diagnostics.RegisterCode(diagnostics.CodeInfo{Code: "break_in_defer", Category: diagnostics.CategorySchema, Description: "break was used inside a defer body", Hint: "移除 defer 块内的 break；defer 块不属于任何循环"})
	diagnostics.RegisterCode(diagnostics.CodeInfo{Code: "continue_in_defer", Category: diagnostics.CategorySchema, Description: "continue was used inside a defer body", Hint: "移除 defer 块内的 continue；defer 块不属于任何循环"})
	diagnostics.RegisterCode(diagnostics.CodeInfo{Code: "defer_in_defer", Category: diagnostics.CategorySchema, Description: "defer was nested inside another defer body", Hint: "移除嵌套的 defer；defer 块内不支持再注册 defer"})
}

func (c *compiler) addCompileError(code, path, msg string) {
	c.errors = append(c.errors, compilerDiagnostic{code: code, message: msg, path: path, category: diagnostics.CategorySchema})
}

func (c *compiler) addCompileErrorWithTypes(code, path, msg, expected, actual string) {
	c.errors = append(c.errors, compilerDiagnostic{code: code, message: msg, path: path, category: diagnostics.CategorySchema, expected: expected, actual: actual})
}

// GetFunctions returns all compiled function chunks.
func (c *compiler) getFunctions() map[string]*chunk {
	return c.functions
}

// RegisterFunctions registers compiled functions into the VM's function registry.
func (c *compiler) registerFunctions(v *vm.VM, interp *Interpreter) {
	for name, chunk := range c.functions {
		if strings.HasPrefix(name, "lambda$") {
			// Lambda chunks are only callable through closure values;
			// they must not be invoked by name (captures would be unbound).
			continue
		}
		info := c.funcInfo[name]
		if info == nil {
			continue
		}

		capturedChunk := chunk
		capturedInterp := interp

		body := vm.NewBytecodeFunctionBody(func(v *vm.VM, args []vm.Value) vm.Value {
			result, err := capturedInterp.ExecuteFunction(capturedChunk, invoke.InvocationStageUnary, args)
			if err != nil {
				v.Panic(fmt.Sprintf("runtime error in %s: %s", name, err.Error()))
				return vm.EncodeInt(0) // unreachable
			}
			return result
		})

		params := make([]vm.ParameterDef, info.paramCount)
		for i := range params {
			params[i] = vm.NewParameterDef(fmt.Sprintf("param%d", i), vm.TypeInvalid)
		}

		v.FuncReg().RegisterFunction(vm.NewFunctionDef(name, params, vm.TypeInvalid, body))
	}
}

// RegisterClasses registers compiled classes into the VM.
func (c *compiler) registerClasses(v *vm.VM) {
	classReg := v.ClassReg()

	// Pass 1: Create classes without parent.
	classMap := make(map[string]*vm.Class)
	for className := range c.classes {
		class := vm.NewClass(0, className, nil)
		classMap[className] = class
	}

	// Pass 1.5: Resolve parent references and validate inheritance constraints.
	for className, info := range c.classes {
		if info.parent != "" {
			parentClass, ok := classMap[info.parent]
			if !ok {
				// Parent is not a class; check if it's an interface.
				if _, isIface := c.interfaces[info.parent]; isIface {
					// Move from parent to implements.
					info.implements = append(info.implements, info.parent)
					info.parent = ""
					c.classes[className] = info
				} else {
					c.addCompileError("parent_class_not_found", "bytecode/oop/inheritance", fmt.Sprintf("parent class %q not found for %q", info.parent, className))
					continue
				}
			} else {
				// Enforce: only open classes can be inherited.
				parentInfo := c.classes[info.parent]
				if !parentInfo.isOpen {
					c.addCompileError("inherit_non_open_class", "bytecode/oop/inheritance", fmt.Sprintf("class %q cannot inherit from non-open class %q", className, info.parent))
					continue
				}
				classMap[className].SetParent(parentClass)
			}
		}
	}

	// Pass 1.6: Validate override methods exist in parent class.
	for className, info := range c.classes {
		for _, method := range info.methods {
			if !method.isOverride {
				continue
			}
			if info.parent == "" {
				c.addCompileError("override_without_parent", "bytecode/oop/override", fmt.Sprintf("method %q in class %q is marked override but class has no parent", method.name, className))
				continue
			}
			parentInfo, ok := c.classes[info.parent]
			if !ok {
				continue // already reported in pass 1.5
			}
			found := false
			for _, parentMethod := range parentInfo.methods {
				if parentMethod.name == method.name {
					if len(parentMethod.paramNames) != len(method.paramNames) {
						c.addCompileErrorWithTypes("override_signature_mismatch", "bytecode/oop/override", fmt.Sprintf("cannot override method %q: parameter count %d does not match parent count %d", method.name, len(method.paramNames), len(parentMethod.paramNames)), fmt.Sprintf("%d parameter(s)", len(parentMethod.paramNames)), fmt.Sprintf("%d parameter(s)", len(method.paramNames)))
					} else if !methodReturnsCompatible(method, parentMethod) {
						c.addCompileErrorWithTypes("override_signature_mismatch", "bytecode/oop/override", fmt.Sprintf("cannot override method %q: return type %q does not match parent return type %q", method.name, method.returnTypeName(), parentMethod.returnTypeName()), parentMethod.returnTypeName(), method.returnTypeName())
					}
					if !parentMethod.isOpen {
						c.addCompileError("override_parent_method_not_open", "bytecode/oop/override", fmt.Sprintf("cannot override method %q: parent method in class %q is not open", method.name, info.parent))
					}
					found = true
					break
				}
			}
			if !found {
				c.addCompileError("override_method_not_found", "bytecode/oop/override", fmt.Sprintf("cannot override method %q: method not found in parent class %q", method.name, info.parent))
			}
		}
	}

	// Pass 1.7: Validate interface contracts for classes that implement interfaces.
	for className, info := range c.classes {
		for _, ifaceName := range info.implements {
			iface, ok := c.interfaces[ifaceName]
			if !ok {
				c.addCompileError("unknown_interface", "bytecode/oop/interface", fmt.Sprintf("class %q implements unknown interface %q", className, ifaceName))
				continue
			}
			// Check each interface method is provided by the class.
			for _, ifaceMethod := range iface.methods {
				found := false
				for _, classMethod := range info.methods {
					if classMethod.name == ifaceMethod.name {
						found = true
						if len(classMethod.paramNames) != ifaceMethod.paramCount {
							c.addCompileErrorWithTypes("interface_signature_mismatch", "bytecode/oop/interface", fmt.Sprintf("class %q implements interface %q but method %q has parameter count %d, expected %d", className, ifaceName, ifaceMethod.name, len(classMethod.paramNames), ifaceMethod.paramCount), fmt.Sprintf("%d parameter(s)", ifaceMethod.paramCount), fmt.Sprintf("%d parameter(s)", len(classMethod.paramNames)))
						}
						break
					}
				}
				if !found {
					c.addCompileError("interface_method_missing", "bytecode/oop/interface", fmt.Sprintf("class %q implements interface %q but is missing method %q", className, ifaceName, ifaceMethod.name))
				}
			}
		}
	}

	// Pass 2: Add fields.
	for className, classInfo := range c.classes {
		class := classMap[className]
		for _, f := range classInfo.fields {
			class.AddField(f.name, vm.TypeInvalid)
		}
	}

	// Compute topological order: parents before children.
	order := c.topologicalClassOrder()

	// Pass 3: Compute offsets (parents first), add methods, register.
	for _, className := range order {
		classInfo := c.classes[className]
		class := classMap[className]
		class.ComputeFieldOffsets()

		for _, method := range classInfo.methods {
			methodKey := className + "." + method.name
			funcDef := v.FuncReg().GetFunction(methodKey)
			if funcDef != nil {
				capturedFunc := funcDef
				impl := func(v *vm.VM, receiver vm.Handle, args []vm.Value) vm.Value {
					allArgs := make([]vm.Value, len(args)+1)
					allArgs[0] = vm.EncodeHandle(receiver)
					copy(allArgs[1:], args)
					return capturedFunc.ExecuteBody(v, allArgs)
				}
				if method.isOverride {
					class.OverrideMethod(method.name, impl)
				} else if method.isOpen {
					class.AddOpenMethod(method.name, impl)
				} else {
					class.AddMethod(method.name, impl)
				}
			}
		}

		class.BuildVTable()
		classReg.RegisterClass(class)
	}

	// Register interfaces into VM and record class→interface mappings.
	ifaceReg := v.IfaceReg()
	for ifaceName, ifaceInfo := range c.interfaces {
		methods := make([]vm.InterfaceMethodSig, len(ifaceInfo.methods))
		for i, m := range ifaceInfo.methods {
			methods[i] = vm.NewInterfaceMethodSig(m.name, m.paramCount)
		}
		ifaceReg.RegisterInterface(vm.NewInterfaceDef(ifaceName, methods))
	}
	for className, classInfo := range c.classes {
		for _, ifaceName := range classInfo.implements {
			ifaceReg.RegisterImplementation(className, ifaceName)
		}
	}
}

// topologicalClassOrder returns class names in dependency order (parents before children).
func (c *compiler) topologicalClassOrder() []string {
	visited := make(map[string]bool)
	var order []string

	var visit func(name string)
	visit = func(name string) {
		if visited[name] {
			return
		}
		visited[name] = true
		info, ok := c.classes[name]
		if !ok {
			return
		}
		if info.parent != "" {
			visit(info.parent)
		}
		order = append(order, name)
	}

	for name := range c.classes {
		visit(name)
	}
	return order
}

// RegisterStructs registers compiled structs into the VM.
func (c *compiler) registerStructs(v *vm.VM) {
	structReg := v.StructReg()
	// Pass 1: register all struct names so IDs are allocated.
	for structName := range c.structs {
		structReg.RegisterStruct(structName, nil)
	}
	// Pass 2: compute field TypeIDs and update definitions.
	for structName, structInfo := range c.structs {
		fields := make([]vm.FieldDef, len(structInfo.fields))
		for i, field := range structInfo.fields {
			tid := resolveTypeID(field.typeName, structReg, v.EnumReg())
			fields[i] = vm.NewFieldDef(field.name, tid, i+1)
		}
		structReg.UpdateStructFields(structName, fields)
	}
}

// registerEnums registers compiled enums into the VM's enum registry so
// opEnumValue constants, `is`/`as` checks, and enum-typed struct fields can
// resolve the enum's runtime ID. Idempotent per enum name.
func (c *compiler) registerEnums(v *vm.VM) {
	enumReg := v.EnumReg()
	for _, info := range c.enums {
		members := make([]vm.EnumMemberDef, len(info.members))
		for i, m := range info.members {
			members[i] = vm.EnumMemberDef{Name: m.name, Value: m.value}
		}
		enumReg.RegisterEnum(info.name, members)
	}
}

// resolveTypeID maps a script type name string to a VM TypeID.
// It handles basic types, registered structs, registered enums, and array<T> generics.
func resolveTypeID(typeName string, structReg *vm.StructRegistry, enumReg *vm.EnumRegistry) vm.TypeID {
	switch typeName {
	case "bool":
		return vm.TypeBool
	case "byte":
		return vm.TypeByte
	case "short":
		return vm.TypeShort
	case "ushort":
		return vm.TypeUShort
	case "int":
		return vm.TypeInt
	case "uint":
		return vm.TypeUInt
	case "long":
		return vm.TypeLong
	case "ulong":
		return vm.TypeULong
	case "float":
		return vm.TypeFloat
	case "double":
		return vm.TypeDouble
	case "string":
		return vm.TypeString
	case "bytes":
		return vm.TypeBytes
	case "object":
		return vm.TypeObject
	case "any":
		return vm.TypeAny
	case "media":
		// Media values travel as {mime, src} reference maps; the VM carries
		// them untyped and the binding boundary enforces the value contract.
		return vm.TypeAny
	}
	if eid := enumReg.GetEnumID(typeName); eid != 0 {
		return vm.MakeEnumType(eid)
	}
	if sid := structReg.GetStructID(typeName); sid != 0 {
		return vm.TypeID(uint64(3)<<60 | uint64(sid))
	}
	if isFunTypeName(typeName) {
		// Function-typed fields hold first-class closure values (any).
		return vm.TypeAny
	}
	if strings.HasPrefix(typeName, "array<") && strings.HasSuffix(typeName, ">") {
		elemTypeName := typeName[6 : len(typeName)-1]
		elemTID := resolveTypeID(elemTypeName, structReg, enumReg)
		elemCategory := uint64((elemTID >> 60) & 0xF)
		elemData := uint64(elemTID) & 0x0FFFFFFFFFFFFFFF
		return vm.TypeID(uint64(1)<<60 | elemCategory<<56 | elemData)
	}
	return vm.TypeInvalid
}

// --- Type alias ---

// registerTypeAlias records a type alias mapping.
func (c *compiler) registerTypeAlias(stmt *frontend.TypeAliasStmt) {
	if stmt == nil || stmt.Alias == nil {
		return
	}
	c.typeAliases[stmt.Name.Value] = typeAnnotationName(stmt.Alias)
}

func expandTypeAliases(typeName string, aliases map[string]string, seen map[string]bool) string {
	typeName = strings.TrimSpace(typeName)
	if typeName == "" {
		return ""
	}
	if resolved, ok := aliases[typeName]; ok {
		if seen[typeName] {
			return typeName
		}
		seen[typeName] = true
		return expandTypeAliases(resolved, aliases, seen)
	}
	if isFunTypeName(typeName) {
		if paramStr, retStr, ok := splitFunTypeName(typeName); ok {
			parts := splitTopLevelTypeArgs(paramStr)
			for i, part := range parts {
				parts[i] = expandTypeAliases(part, aliases, seen)
			}
			ret := expandTypeAliases(retStr, aliases, seen)
			return fmt.Sprintf("fun(%s):%s", strings.Join(parts, ","), ret)
		}
		return typeName
	}
	start := strings.Index(typeName, "<")
	end := strings.LastIndex(typeName, ">")
	if start < 0 || end <= start {
		return typeName
	}
	base := strings.TrimSpace(typeName[:start])
	inner := typeName[start+1 : end]
	parts := splitTopLevelTypeArgs(inner)
	for i, part := range parts {
		parts[i] = expandTypeAliases(part, aliases, seen)
	}
	if resolvedBase, ok := aliases[base]; ok {
		base = expandTypeAliases(resolvedBase, aliases, seen)
	}
	return fmt.Sprintf("%s<%s>", base, strings.Join(parts, ","))
}

func splitTopLevelTypeArgs(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := make([]string, 0, 2)
	depth := 0
	start := 0
	for i, r := range s {
		switch r {
		case '<', '(':
			depth++
		case '>', ')':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	parts = append(parts, strings.TrimSpace(s[start:]))
	return parts
}

// resolveType resolves a type name through aliases to its base type.
func (c *compiler) resolveType(typeName string) string {
	return expandTypeAliases(typeName, c.typeAliases, make(map[string]bool))
}

// --- Function types (`fun(P1, ...): R`) ---

// funTypePrefix marks serialized function type names, e.g. `fun(int):string`.
const funTypePrefix = "fun("

// isFunTypeName reports whether typeName is a serialized function type.
func isFunTypeName(typeName string) bool {
	return strings.HasPrefix(strings.TrimSpace(typeName), funTypePrefix)
}

// splitFunTypeName splits a serialized function type `fun(P1,...,Pn):R` into
// its parameter list and return type strings. Paren-depth tracking keeps
// nested function types inside the parameter list intact.
func splitFunTypeName(typeName string) (params string, ret string, ok bool) {
	typeName = strings.TrimSpace(typeName)
	if !strings.HasPrefix(typeName, funTypePrefix) {
		return "", "", false
	}
	depth := 0
	for i := len(funTypePrefix); i < len(typeName); i++ {
		switch typeName[i] {
		case '(':
			depth++
		case ')':
			if depth == 0 {
				if i+2 <= len(typeName) && typeName[i+1] == ':' {
					return typeName[len(funTypePrefix):i], typeName[i+2:], true
				}
				return "", "", false
			}
			depth--
		}
	}
	return "", "", false
}

// funTypeSig is the structural view of a serialized function type.
type funTypeSig struct {
	params []string
	ret    string
}

// parseFunTypeSig parses a serialized function type name. The second return
// value is false when typeName is not a function type.
func parseFunTypeSig(typeName string) (funTypeSig, bool) {
	paramStr, retStr, ok := splitFunTypeName(typeName)
	if !ok {
		return funTypeSig{}, false
	}
	sig := funTypeSig{ret: strings.TrimSpace(retStr)}
	if strings.TrimSpace(paramStr) != "" {
		for _, part := range splitTopLevelTypeArgs(paramStr) {
			sig.params = append(sig.params, part)
		}
	}
	return sig, true
}

// funTypeComponentCompat reports whether a declared type component (parameter
// or return) accepts the provided one. Empty and `any` components are
// wildcards, preserving the existing lenient behavior outside strict
// function-type checking.
func funTypeComponentCompat(want, got string) bool {
	want = strings.TrimSpace(want)
	got = strings.TrimSpace(got)
	if want == "" || got == "" || want == "any" || got == "any" {
		return true
	}
	return want == got
}

// funTypeAssignable reports whether a function value of type src can inhabit
// a slot of type dst. Types that are not (both) function types bypass.
func funTypeAssignable(dst, src string) bool {
	dstSig, okDst := parseFunTypeSig(dst)
	srcSig, okSrc := parseFunTypeSig(src)
	if !okDst || !okSrc {
		return true
	}
	if len(dstSig.params) != len(srcSig.params) {
		return false
	}
	for i := range dstSig.params {
		if !funTypeComponentCompat(dstSig.params[i], srcSig.params[i]) {
			return false
		}
	}
	return funTypeComponentCompat(dstSig.ret, srcSig.ret)
}

// checkLambdaAgainstSig validates a lambda literal against a function type:
// parameter count, parameter types, and declared return type must match.
// Untyped lambda components (no annotation) and `any` bypass, matching the
// compile-time-only, lenient-by-default checking model.
func (c *compiler) checkLambdaAgainstSig(e *frontend.LambdaExpr, funTypeName, name string) {
	sig, ok := parseFunTypeSig(funTypeName)
	if !ok {
		return
	}
	if len(e.Params) != len(sig.params) {
		c.addCompileErrorWithTypes("function_type_arity_mismatch", "bytecode/type/funtype",
			fmt.Sprintf("cannot assign lambda with %d parameter(s) to %q of type %s (expects %d)", len(e.Params), name, funTypeName, len(sig.params)),
			funTypeName, fmt.Sprintf("fun with %d parameter(s)", len(e.Params)))
		return
	}
	for i, p := range e.Params {
		paramType := ""
		if p.Type_ != nil {
			paramType = c.resolveType(typeAnnotationName(p.Type_))
		}
		if !funTypeComponentCompat(sig.params[i], paramType) {
			c.addCompileErrorWithTypes("function_type_param_mismatch", "bytecode/type/funtype",
				fmt.Sprintf("lambda parameter %d of %q expects %q but lambda declares %q", i+1, name, sig.params[i], paramType),
				sig.params[i], paramType)
			return
		}
	}
	retType := ""
	if e.ReturnType != nil {
		retType = c.resolveType(typeAnnotationName(e.ReturnType))
	}
	if !funTypeComponentCompat(sig.ret, retType) {
		c.addCompileErrorWithTypes("function_type_return_mismatch", "bytecode/type/funtype",
			fmt.Sprintf("lambda assigned to %q must return %q but declares %q", name, sig.ret, retType),
			sig.ret, retType)
	}
}

// isLiteralValueExpr reports whether expr is a literal with a statically
// known non-function type.
func isLiteralValueExpr(expr frontend.Expression) bool {
	switch expr.(type) {
	case *frontend.IntLiteral, *frontend.FloatLiteral, *frontend.StringLiteral,
		*frontend.BoolLiteral, *frontend.NullLiteral, *frontend.ArrayLiteral,
		*frontend.MapLiteral, *frontend.StructLiteral:
		return true
	}
	return false
}

// checkFunTypeValueAssign validates a value being stored into a slot whose
// declared type is a function type. Lambdas are checked against the
// signature; identifiers keep their own tracked function type checked for
// structural compatibility; literals of other kinds are rejected. Slots that
// are not function types bypass entirely.
func (c *compiler) checkFunTypeValueAssign(funTypeName, name string, value frontend.Expression) {
	if value == nil || !isFunTypeName(funTypeName) {
		return
	}
	switch val := value.(type) {
	case *frontend.LambdaExpr:
		c.checkLambdaAgainstSig(val, funTypeName, name)
	case *frontend.IdentExpr:
		srcType := c.localOrGlobalTypeName(val.Value)
		if srcType == "" || !isFunTypeName(srcType) {
			return
		}
		if resolved := c.resolveType(funTypeName); !funTypeAssignable(resolved, srcType) {
			c.addCompileErrorWithTypes("function_type_mismatch", "bytecode/type/funtype",
				fmt.Sprintf("cannot assign %q of type %s to %q of type %s", val.Value, srcType, name, resolved),
				resolved, srcType)
		}
	default:
		if isLiteralValueExpr(value) {
			resolved := c.resolveType(funTypeName)
			c.addCompileErrorWithTypes("function_type_mismatch", "bytecode/type/funtype",
				fmt.Sprintf("cannot assign %s value to %q of type %s", exprTypeName(value), name, resolved),
				resolved, exprTypeName(value))
		}
	}
}

// checkFunTypeCallArgs validates call arguments against a function-typed
// callee's signature: argument count plus literal argument types (non-literal
// arguments bypass type checking, matching the existing lenient behavior).
func (c *compiler) checkFunTypeCallArgs(calleeName string, args []frontend.Expression) {
	typeName := c.resolveType(c.localOrGlobalTypeName(calleeName))
	if !isFunTypeName(typeName) {
		return
	}
	sig, ok := parseFunTypeSig(typeName)
	if !ok {
		return
	}
	if len(args) != len(sig.params) {
		c.addCompileErrorWithTypes("function_type_arity_mismatch", "bytecode/type/funtype",
			fmt.Sprintf("call to %q of type %s expects %d argument(s) but got %d", calleeName, typeName, len(sig.params), len(args)),
			fmt.Sprintf("%d argument(s)", len(sig.params)), fmt.Sprintf("%d argument(s)", len(args)))
		return
	}
	for i, arg := range args {
		if lam, isLambda := arg.(*frontend.LambdaExpr); isLambda {
			if isFunTypeName(sig.params[i]) {
				c.checkLambdaAgainstSig(lam, sig.params[i], fmt.Sprintf("argument %d of %q", i+1, calleeName))
			}
			continue
		}
		if isLiteralValueExpr(arg) && !structFieldValueMatchesType(arg, sig.params[i]) {
			c.addCompileErrorWithTypes("function_type_arg_mismatch", "bytecode/type/funtype",
				fmt.Sprintf("argument %d of %q expects %q", i+1, calleeName, sig.params[i]),
				sig.params[i], exprTypeName(arg))
			return
		}
	}
}

// checkDirectCallFunTypeParams validates lambda arguments passed to a named
// callable whose corresponding parameter is declared with a function type.
// Only function-typed parameters are checked; everything else keeps the
// existing pass-through behavior.
func (c *compiler) checkDirectCallFunTypeParams(calleeName string, paramTypes []string, args []frontend.Expression) {
	for i, arg := range args {
		if i >= len(paramTypes) {
			return
		}
		lam, isLambda := arg.(*frontend.LambdaExpr)
		if !isLambda {
			continue
		}
		resolved := c.resolveType(paramTypes[i])
		if isFunTypeName(resolved) {
			c.checkLambdaAgainstSig(lam, resolved, fmt.Sprintf("argument %d of %q", i+1, calleeName))
		}
	}
}
