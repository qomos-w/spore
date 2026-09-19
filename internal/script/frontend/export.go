package frontend

// Exported type aliases for AST nodes used by the bytecode compiler.
// The frontend package keeps its internal types unexported; this file exposes
// only the minimal surface currently consumed outside the package.

type (
	Statement  = statement
	Expression = expression
	Program    = program
)

type (
	FunStmt       = funStmt
	StructStmt    = structStmt
	ClassStmt     = classStmt
	VarStmt       = varStmt
	TypeAliasStmt = typeAliasStmt
	InterfaceStmt = interfaceStmt
	EnumStmt      = enumStmt
	BlockStmt     = blockStmt
	ReturnStmt    = returnStmt
	YieldStmt     = yieldStmt
	IfStmt        = ifStmt
	WhileStmt     = whileStmt
	ForStmt       = forStmt
	WhenStmt      = whenStmt
	ExprStatement = exprStatement
	BreakStmt     = breakStmt
	ContinueStmt  = continueStmt
	TryStmt       = tryStmt
	DeferStmt     = deferStmt
)

type (
	IntLiteral        = intLiteral
	FloatLiteral      = floatLiteral
	StringLiteral     = stringLiteral
	BoolLiteral       = boolLiteral
	NullLiteral       = nullLiteral
	IdentExpr         = identExpr
	Ident             = ident
	BinaryExpr        = binaryExpr
	UnaryExpr         = unaryExpr
	CallExpr          = callExpr
	MemberExpr        = memberExpr
	IndexExpr         = indexExpr
	AssignExpr        = assignExpr
	TypeCheckExpr     = typeCheckExpr
	TypeCastExpr      = typeCastExpr
	ThisExpr          = thisExpr
	ArrayLiteral      = arrayLiteral
	MapLiteral        = mapLiteral
	NewExpr           = newExpr
	StructLiteral     = structLiteral
	SuperExpr         = superExpr
	LambdaExpr        = lambdaExpr
	NullCoalesceExpr  = nullCoalesceExpr
	OptionalChainExpr = optionalChainExpr
)

type (
	MethodSignature = methodSignature
	TypeAnnotation  = typeAnnotation
	Param           = param
)

type (
	EnumMember = enumMember
)
