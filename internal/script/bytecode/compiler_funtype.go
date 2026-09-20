// compiler_funtype.go implements parsing and assignability checking of function types.

package bytecode

import (
	"fmt"
	"strings"

	"github.com/qomos-w/spore/internal/script/frontend"
)

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
