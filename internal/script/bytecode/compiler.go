// compiler.go holds the bytecode compiler's core state, the AST adapter types and resolved declaration-info records, and the entry points that lower a parsed program into a chunk.

package bytecode

import (
	"fmt"
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
