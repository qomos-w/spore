// compiler.go holds the bytecode compiler's core state, the resolved
// declaration-info records, and the entry points that lower a parsed program
// into a chunk. The compiler consumes the frontend AST directly: body
// statements dispatch through compileStatement (compiler_stmt.go) and top-level
// statements through compileTopLevelDecl (compiler_decls.go), with no private
// copy of the AST.
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
	// moduleGlobalNames maps exported top-level var statements to their
	// module-qualified global name during module compilation (set by
	// compileProgram, read by compileVarDecl). It keeps the shared frontend
	// AST immutable across compiles.
	moduleGlobalNames map[*frontend.VarStmt]string
	// optionalChainJumps collects the pending opJumpIfNull instructions
	// emitted for `?.` links of the optional chain currently being
	// compiled; compileOptionalChainExpr patches them to the chain end.
	optionalChainJumps []int
	handlerDepth       int // open try bodies in the code currently being compiled
	inDeferBody        int // >0 while compiling a defer body (rejects return/break/continue/yield)
}

type accessModifier int

const (
	accessPublic accessModifier = iota
	accessPrivate
)

// classInfo is a resolved class declaration: hierarchy, fields and the resolved
// method set (declared methods plus interface defaults synthesized by
// synthesizeInterfaceDefaultMethods). Methods are the frontend AST nodes
// themselves — no private copy is kept.
type classInfo struct {
	name       string
	parent     string // parent class name (empty if none)
	isOpen     bool
	implements []string // interface names
	fields     []fieldInfo
	methods    []*frontend.FunStmt
}

type fieldInfo struct {
	name     string
	access   accessModifier
	typeName string
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

// interfaceInfo is a resolved interface declaration. Methods are the frontend
// method signatures themselves; parameter counts and return types are derived
// from the signature on demand (see interfaceMethodParamCount /
// interfaceMethodReturnType).
type interfaceInfo struct {
	name    string
	methods []*frontend.MethodSignature
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
	stmts := topLevelStatements(prog)
	c.interfaces = collectTopLevelInterfaces(prog)
	c.moduleGlobalNames = nil
	for _, stmt := range stmts {
		if alias, ok := stmt.(*frontend.TypeAliasStmt); ok {
			c.registerTypeAlias(alias)
		}
	}
	for _, stmt := range stmts {
		c.registerTopLevelTypeInfo(stmt)
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
		c.moduleGlobalNames = make(map[*frontend.VarStmt]string)
		for _, stmt := range stmts {
			vr, ok := stmt.(*frontend.VarStmt)
			if !ok || !vr.Exported {
				continue
			}
			originalName := vr.Name.Value
			qualifiedName := qualifiedModuleGlobalName(modulePath, originalName)
			c.globalTypes[qualifiedName] = c.globalTypes[originalName]
			delete(c.globalTypes, originalName)
			// Record the qualified name for the compile pass without mutating
			// the shared AST node (the program may be compiled again).
			c.moduleGlobalNames[vr] = qualifiedName
		}
	}
	c.validateTypeAliases()
	for _, stmt := range stmts {
		if cl, ok := stmt.(*frontend.ClassStmt); ok {
			c.normalizeClassRelationships(cl.Name.Value)
		}
	}
	// Inherit interface default method bodies into implementing classes that
	// do not provide (or inherit) the method themselves. Runs after all class
	// relationships are normalized and in topological order (parents first) so
	// subclass synthesis sees defaults already inherited by ancestors. The
	// resolved class records are read back from c.classes by
	// compileTopLevelDecl, so no re-sync of a local declaration copy is needed.
	for _, className := range c.topologicalClassOrder() {
		c.synthesizeInterfaceDefaultMethods(className)
	}
	for _, stmt := range stmts {
		c.compileTopLevelDecl(stmt)
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
