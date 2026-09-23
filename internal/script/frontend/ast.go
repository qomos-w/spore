package frontend

// AST node types for the sporescript frontend.
// Only fields consumed by the bytecode compiler are exported across packages;
// the rest remain internal frontend implementation details.

// node is the base interface for all AST nodes.
type node interface {
	pos() (int, int) // line, column
}

// statement is the interface for AST nodes that represent statements.
type statement interface {
	node
	stmtNode()
}

// expression is the interface for AST nodes that represent expressions.
type expression interface {
	node
	exprNode()
}

// --- Program ---

type program struct {
	pkg     *packageStmt
	imports []*importStmt
	Stmts   []statement
}

func (p *program) pos() (int, int) {
	if len(p.Stmts) == 0 {
		return 1, 1
	}
	return p.Stmts[0].pos()
}

// PackageName returns the declared package name, or an empty string if no
// package declaration is present.
func (p *program) PackageName() string {
	if p == nil || p.pkg == nil {
		return ""
	}
	return p.pkg.Name
}

// --- Top-level declarations ---

type packageStmt struct {
	tok  token
	Name string
}

func (p *packageStmt) pos() (int, int) { return p.tok.line, p.tok.col }
func (p *packageStmt) stmtNode()       {}

type importStmt struct {
	tok      token
	Name     string
	Path     string
	Alias    string
	ReExport bool
}

func (i *importStmt) pos() (int, int) { return i.tok.line, i.tok.col }
func (i *importStmt) stmtNode()       {}

type funStmt struct {
	tok        token
	Name       *ident
	Params     []*param
	ReturnType *typeAnnotation
	Body       *blockStmt
	ExprBody   expression
	Exported   bool
	IsOpen     bool
	IsStream   bool
	IsOverride bool
	Access     accessModifier
	BodyLine   int
	BodyCol    int
}

func (f *funStmt) pos() (int, int) { return f.tok.line, f.tok.col }
func (f *funStmt) stmtNode()       {}

type structStmt struct {
	tok         token
	Name        *ident
	Fields      []*astFieldDecl
	Exported    bool
	SchemaID    uint64
	IsComponent bool
	IsData      bool
	DataVersion uint64
}

func (s *structStmt) pos() (int, int) { return s.tok.line, s.tok.col }
func (s *structStmt) stmtNode()       {}

// enumMember is a single member of an enum declaration. Value is the raw
// underlying int; HasValue reports whether the source gave an explicit
// `= N` assignment (auto-increment resolution happens at compilation).
type enumMember struct {
	Name     *ident
	Value    int64
	HasValue bool
}

type enumStmt struct {
	tok      token
	Name     *ident
	Members  []*enumMember
	Exported bool
}

func (e *enumStmt) pos() (int, int) { return e.tok.line, e.tok.col }
func (e *enumStmt) stmtNode()       {}

type classStmt struct {
	tok        token
	Name       *ident
	Parent     *ident   // parent class name (nil if none)
	IsOpen     bool     // open class — allows inheritance
	Implements []*ident // interface names
	Fields     []*astFieldDecl
	Methods    []*funStmt
}

func (c *classStmt) pos() (int, int) { return c.tok.line, c.tok.col }
func (c *classStmt) stmtNode()       {}

type varStmt struct {
	tok      token
	Name     *ident
	Type_    *typeAnnotation
	Value    expression
	InitExpr expression
	Exported bool
}

func (v *varStmt) pos() (int, int) { return v.tok.line, v.tok.col }
func (v *varStmt) stmtNode()       {}

type typeAliasStmt struct {
	tok      token
	Name     *ident
	Alias    *typeAnnotation
	Exported bool
}

func (t *typeAliasStmt) pos() (int, int) { return t.tok.line, t.tok.col }
func (t *typeAliasStmt) stmtNode()       {}

// --- Access modifiers (no protected per Spore philosophy) ---

type accessModifier int

const (
	accessPublic accessModifier = iota
	accessPrivate
)

// --- Shared types ---

type ident struct {
	tok   token
	Value string
}

func (i *ident) pos() (int, int) { return i.tok.line, i.tok.col }

type param struct {
	Name  *ident
	Type_ *typeAnnotation
}

type astFieldDecl struct {
	Name     *ident
	Type_    *typeAnnotation
	Access   accessModifier
	Optional bool
	Key      bool
	Ref      *fieldRef
}

// fieldRef is the parsed @ref(T) / @ref(T.field) field decorator. Field is
// empty when the reference targets the referenced struct's key field.
type fieldRef struct {
	Target string
	Field  string
}

// typeAnnotation represents a type reference in source code.
type typeAnnotation struct {
	tok    token
	Name   string
	Params []*typeAnnotation

	// Function type surface: `fun(T1, T2): R` used in type position. IsFun
	// marks the annotation as a function type; FunParams holds the parameter
	// types (types only, no parameter names) and FunReturn the return type
	// (nil means `void`). Name stays "fun" for display purposes; Params is
	// unused for function types.
	IsFun     bool
	FunParams []*typeAnnotation
	FunReturn *typeAnnotation
}

func (t *typeAnnotation) pos() (int, int) { return t.tok.line, t.tok.col }

// --- Block and body ---

type blockStmt struct {
	tok   token
	Stmts []statement
}

func (b *blockStmt) pos() (int, int) { return b.tok.line, b.tok.col }
func (b *blockStmt) stmtNode()       {}

// --- Expression nodes ---

type intLiteral struct {
	tok   token
	Value int64
}

func (n *intLiteral) pos() (int, int) { return n.tok.line, n.tok.col }
func (n *intLiteral) exprNode()       {}

type floatLiteral struct {
	tok   token
	Value float64
}

func (n *floatLiteral) pos() (int, int) { return n.tok.line, n.tok.col }
func (n *floatLiteral) exprNode()       {}

type stringLiteral struct {
	tok   token
	Value string
}

func (n *stringLiteral) pos() (int, int) { return n.tok.line, n.tok.col }
func (n *stringLiteral) exprNode()       {}

type boolLiteral struct {
	tok   token
	Value bool
}

func (n *boolLiteral) pos() (int, int) { return n.tok.line, n.tok.col }
func (n *boolLiteral) exprNode()       {}

type nullLiteral struct {
	tok token
}

func (n *nullLiteral) pos() (int, int) { return n.tok.line, n.tok.col }
func (n *nullLiteral) exprNode()       {}

// identExpr wraps an identifier as an expression.
type identExpr struct {
	tok   token
	Value string
}

func (n *identExpr) pos() (int, int) { return n.tok.line, n.tok.col }
func (n *identExpr) exprNode()       {}

type binaryExpr struct {
	tok      token
	Left     expression
	Operator string
	Right    expression
}

func (n *binaryExpr) pos() (int, int) { return n.tok.line, n.tok.col }
func (n *binaryExpr) exprNode()       {}

type unaryExpr struct {
	tok      token
	Operator string
	Right    expression
}

func (n *unaryExpr) pos() (int, int) { return n.tok.line, n.tok.col }
func (n *unaryExpr) exprNode()       {}

type callExpr struct {
	tok       token
	Callee    expression
	Arguments []expression
}

func (n *callExpr) pos() (int, int) { return n.tok.line, n.tok.col }
func (n *callExpr) exprNode()       {}

type memberExpr struct {
	tok    token
	Object expression
	Member *ident
	// Optional marks `object?.member`: when object evaluates to null the
	// whole enclosing chain short-circuits to null instead of performing
	// the field access (or method call, when the memberExpr is the callee
	// of a callExpr).
	Optional bool
}

func (n *memberExpr) pos() (int, int) { return n.tok.line, n.tok.col }
func (n *memberExpr) exprNode()       {}

// nullCoalesceExpr is `Left ?? Right`: if Left evaluates to null the
// expression yields Right, otherwise it yields Left. Left-assoc and
// chainable (`a ?? b ?? c`).
type nullCoalesceExpr struct {
	tok   token
	Left  expression
	Right expression
}

func (n *nullCoalesceExpr) pos() (int, int) { return n.tok.line, n.tok.col }
func (n *nullCoalesceExpr) exprNode()       {}

// optionalChainExpr wraps a postfix chain that contains at least one `?.`
// link (member access, method call, index access). The wrapper delimits the
// short-circuit region: when any optional link's receiver is null, control
// jumps to the end of the whole chain with null as the chain's value,
// skipping every remaining link (and any argument evaluation they would
// have performed).
type optionalChainExpr struct {
	tok  token
	Expr expression
}

func (n *optionalChainExpr) pos() (int, int) { return n.tok.line, n.tok.col }
func (n *optionalChainExpr) exprNode()       {}

type indexExpr struct {
	tok   token
	Left  expression
	Index expression
}

func (n *indexExpr) pos() (int, int) { return n.tok.line, n.tok.col }
func (n *indexExpr) exprNode()       {}

type assignExpr struct {
	tok    token
	Target expression
	Value  expression
}

func (n *assignExpr) pos() (int, int) { return n.tok.line, n.tok.col }
func (n *assignExpr) exprNode()       {}

type typeCheckExpr struct {
	tok      token
	Left     expression
	TypeName string
}

func (n *typeCheckExpr) pos() (int, int) { return n.tok.line, n.tok.col }
func (n *typeCheckExpr) exprNode()       {}

type typeCastExpr struct {
	tok    token
	Left   expression
	Target *typeAnnotation
}

func (n *typeCastExpr) pos() (int, int) { return n.tok.line, n.tok.col }
func (n *typeCastExpr) exprNode()       {}

type thisExpr struct {
	tok token
}

func (n *thisExpr) pos() (int, int) { return n.tok.line, n.tok.col }
func (n *thisExpr) exprNode()       {}

type arrayLiteral struct {
	tok      token
	Elements []expression
}

func (n *arrayLiteral) pos() (int, int) { return n.tok.line, n.tok.col }
func (n *arrayLiteral) exprNode()       {}

type mapPair struct {
	Key   expression
	Value expression
}

type mapLiteral struct {
	tok   token
	Pairs []mapPair
}

func (n *mapLiteral) pos() (int, int) { return n.tok.line, n.tok.col }
func (n *mapLiteral) exprNode()       {}

type structFieldInit struct {
	Name  string
	Value expression
}

type structLiteral struct {
	tok         token
	TypeName    string
	Fields      []structFieldInit
	HasZeroFill bool
}

func (n *structLiteral) pos() (int, int) { return n.tok.line, n.tok.col }
func (n *structLiteral) exprNode()       {}

type newExpr struct {
	tok       token
	ClassName string
	Arguments []expression
}

func (n *newExpr) pos() (int, int) { return n.tok.line, n.tok.col }
func (n *newExpr) exprNode()       {}

// --- Statement nodes (body-level) ---

type returnStmt struct {
	tok   token
	Value expression
}

func (n *returnStmt) pos() (int, int) { return n.tok.line, n.tok.col }
func (n *returnStmt) stmtNode()       {}

type yieldStmt struct {
	tok   token
	Value expression
}

func (n *yieldStmt) pos() (int, int) { return n.tok.line, n.tok.col }
func (n *yieldStmt) stmtNode()       {}

type exprStatement struct {
	tok  token
	Expr expression
}

func (n *exprStatement) pos() (int, int) { return n.tok.line, n.tok.col }
func (n *exprStatement) stmtNode()       {}

type ifStmt struct {
	tok         token
	Condition   expression
	Consequence *blockStmt
	Alternative statement // may be *ifStmt or *blockStmt
}

func (n *ifStmt) pos() (int, int) { return n.tok.line, n.tok.col }
func (n *ifStmt) stmtNode()       {}

type whileStmt struct {
	tok       token
	Condition expression
	Body      *blockStmt
}

func (n *whileStmt) pos() (int, int) { return n.tok.line, n.tok.col }
func (n *whileStmt) stmtNode()       {}

type forStmt struct {
	tok       token
	Init      statement  // var declaration or assignment
	Condition expression // for-in: nil
	Update    expression // for-in: nil
	Body      *blockStmt
	IsForIn   bool
	Variable  string     // for-in variable name
	Iterable  expression // for-in iterable
}

func (n *forStmt) pos() (int, int) { return n.tok.line, n.tok.col }
func (n *forStmt) stmtNode()       {}

type whenStmt struct {
	tok         token
	Expr        expression
	Cases       []*caseClause
	DefaultCase *blockStmt
}

func (n *whenStmt) pos() (int, int) { return n.tok.line, n.tok.col }
func (n *whenStmt) stmtNode()       {}

type caseClause struct {
	tok            token
	Values         []expression    // value match: case 1, 2, 3
	Variable       string          // type match: case x: Type (variable name)
	TypeAnnotation *typeAnnotation // type match target type
	Guard          expression      // optional guard: case ... when expr { ... }
	Body           *blockStmt
}

func (n *caseClause) pos() (int, int) { return n.tok.line, n.tok.col }

type breakStmt struct {
	tok token
}

func (n *breakStmt) pos() (int, int) { return n.tok.line, n.tok.col }
func (n *breakStmt) stmtNode()       {}

type continueStmt struct {
	tok token
}

func (n *continueStmt) pos() (int, int) { return n.tok.line, n.tok.col }
func (n *continueStmt) stmtNode()       {}

// tryStmt is a try/catch error-handling statement. A RuntimeError raised
// while executing Body transfers control to CatchBody with the error value
// bound to CatchVar.
type tryStmt struct {
	tok       token
	Body      *blockStmt
	CatchVar  *ident
	CatchBody *blockStmt
}

func (n *tryStmt) pos() (int, int) { return n.tok.line, n.tok.col }
func (n *tryStmt) stmtNode()       {}

// deferStmt registers a deferred block that executes (in reverse registration
// order relative to other defers) when the enclosing function returns or
// unwinds due to an uncaught error.
type deferStmt struct {
	tok  token
	Body *blockStmt
}

func (n *deferStmt) pos() (int, int) { return n.tok.line, n.tok.col }
func (n *deferStmt) stmtNode()       {}

// --- Super expression ---

type superExpr struct {
	tok       token
	Method    *ident // nil for super()
	Arguments []expression
}

func (n *superExpr) pos() (int, int) { return n.tok.line, n.tok.col }
func (n *superExpr) exprNode()       {}

// lambdaExpr is an anonymous function / closure expression.
type lambdaExpr struct {
	tok        token
	Params     []*param
	ReturnType *typeAnnotation
	Body       *blockStmt
	ExprBody   expression
}

func (n *lambdaExpr) pos() (int, int) { return n.tok.line, n.tok.col }
func (n *lambdaExpr) exprNode()       {}

// --- Interface declaration ---

type interfaceStmt struct {
	tok      token
	Name     *ident
	Methods  []*methodSignature
	Exported bool
}

func (i *interfaceStmt) pos() (int, int) { return i.tok.line, i.tok.col }
func (i *interfaceStmt) stmtNode()       {}

type methodSignature struct {
	Name       *ident
	Params     []*param
	ReturnType *typeAnnotation
	Body       *blockStmt // optional default method body; nil for abstract signatures
}
