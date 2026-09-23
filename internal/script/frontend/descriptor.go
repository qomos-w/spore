package frontend

import (
	"fmt"

	"github.com/qomos-w/spore/diagnostics"
	"github.com/qomos-w/spore/schema"
)

// typeContext carries declaration-name context needed to resolve
// user-defined type annotations into TypeKindStruct vs TypeKindClass.
type typeContext struct {
	structNames       map[string]bool            // names declared via structStmt
	classNames        map[string]bool            // names declared via classStmt
	enumNames         map[string]bool            // names declared via enumStmt
	typeAliases       map[string]*typeAnnotation // alias name -> underlying type annotation
	localTypes        map[string]schema.TypeDesc // local variable name -> inferred/declared type
	importedTypeDescs map[string]schema.TypeDesc // compile-time imported type name -> TypeDesc
}

// extractCallableDesc maps a funStmt AST node to a schema.CallableDesc.
func extractCallableDesc(stmt *funStmt, ctx typeContext) (schema.CallableDesc, error) {
	params := make([]schema.ParameterDesc, 0, len(stmt.Params))
	for _, p := range stmt.Params {
		params = append(params, schema.ParameterDesc{
			Name: p.Name.Value,
			Type: typeAnnotationToTypeDesc(p.Type_, ctx),
		})
	}
	if stmt.IsStream {
		if !hasYieldStmt(stmt) {
			return schema.CallableDesc{}, newLoweringError(
				"stream_fun_requires_yield",
				fmt.Sprintf("stream fun %q must contain at least one yield statement", stmt.Name.Value),
				spanFromFunStmt(stmt),
				"frontend/callable/stream",
				nil,
			)
		}
		next, final, err := extractStreamingReturnTypes(stmt, ctx)
		if err != nil {
			return schema.CallableDesc{}, err
		}
		desc, err := schema.NewStreamingCallableDesc(stmt.Name.Value, params, next, final, false)
		if err != nil {
			return schema.CallableDesc{}, newLoweringError("callable_lowering_failed", err.Error(), spanFromFunStmt(stmt), "frontend/schema/lowering", err)
		}
		return desc, nil
	}
	// Non-stream fun must not contain yield.
	if hasYieldStmt(stmt) {
		return schema.CallableDesc{}, newLoweringError(
			"yield_requires_stream_fun",
			fmt.Sprintf("yield is only allowed inside stream fun, but %q is not declared as stream fun", stmt.Name.Value),
			spanFromFunStmt(stmt),
			"frontend/callable/stream",
			nil,
		)
	}
	desc := schema.CallableDesc{
		Name:       stmt.Name.Value,
		Parameters: params,
		Mode:       schema.CallableModeUnary,
	}
	if stmt.ReturnType != nil {
		desc.Returns = []schema.TypeDesc{typeAnnotationToTypeDesc(stmt.ReturnType, ctx)}
	}
	return desc, nil
}

func hasYieldStmt(stmt *funStmt) bool {
	if stmt == nil || stmt.Body == nil {
		return false
	}
	return blockHasYield(stmt.Body)
}

func blockHasYield(block *blockStmt) bool {
	if block == nil {
		return false
	}
	for _, stmt := range block.Stmts {
		if statementHasYield(stmt) {
			return true
		}
	}
	return false
}

func statementHasYield(stmt statement) bool {
	switch s := stmt.(type) {
	case *yieldStmt:
		return true
	case *blockStmt:
		return blockHasYield(s)
	case *ifStmt:
		if blockHasYield(s.Consequence) {
			return true
		}
		return statementHasYield(s.Alternative)
	case *whileStmt:
		return blockHasYield(s.Body)
	case *forStmt:
		return blockHasYield(s.Body)
	case *whenStmt:
		for _, cc := range s.Cases {
			if blockHasYield(cc.Body) {
				return true
			}
		}
		return blockHasYield(s.DefaultCase)
	default:
		return false
	}
}

func extractStreamingReturnTypes(stmt *funStmt, ctx typeContext) (*schema.TypeDesc, *schema.TypeDesc, error) {
	if stmt == nil {
		return nil, nil, newLoweringError("callable_lowering_failed", "streaming callable cannot be nil", diagnostics.Span{}, "frontend/schema/lowering", nil)
	}
	if stmt.ExprBody != nil {
		return nil, nil, newLoweringError("stream_expr_body_unsupported", fmt.Sprintf("streaming callable %q cannot use expression body", stmt.Name.Value), spanFromFunStmt(stmt), "frontend/callable/stream", nil)
	}
	if stmt.ReturnType == nil {
		return nil, nil, newLoweringError("stream_final_type_required", fmt.Sprintf("streaming callable %q must declare final return type", stmt.Name.Value), spanFromFunStmt(stmt), "frontend/callable/stream", nil)
	}
	final := typeAnnotationToTypeDesc(stmt.ReturnType, ctx)
	next, err := inferYieldType(stmt.Body, ctx)
	if err != nil {
		return nil, nil, err
	}
	return &next, &final, nil
}

func inferYieldType(block *blockStmt, ctx typeContext) (schema.TypeDesc, error) {
	var (
		inferred schema.TypeDesc
		found    bool
	)
	var visitBlock func(*blockStmt, typeContext) error
	var visitStmt func(statement, typeContext) error
	visitStmt = func(stmt statement, ctx typeContext) error {
		switch s := stmt.(type) {
		case *varStmt:
			if ctx.localTypes == nil {
				ctx.localTypes = make(map[string]schema.TypeDesc)
			}
			if s.Type_ != nil {
				ctx.localTypes[s.Name.Value] = typeAnnotationToTypeDesc(s.Type_, ctx)
			} else if s.InitExpr != nil {
				td, err := inferExpressionType(s.InitExpr, ctx)
				if err == nil {
					ctx.localTypes[s.Name.Value] = td
				}
			}
			return nil
		case *yieldStmt:
			if s.Value == nil {
				return newLoweringError("yield_value_required", "yield requires a value", spanFromBlock(block), "frontend/callable/stream", nil)
			}
			td, err := inferExpressionType(s.Value, ctx)
			if err != nil {
				return err
			}
			if !found {
				inferred = td
				found = true
				return nil
			}
			if !sameTypeDesc(inferred, td) {
				return newSchemaValidationErrorWithTypes("yield_type_mismatch", "yield types must match",
					spanFromExpr(s.Value), "frontend/type/inference",
					inferred.String(), td.String())
			}
			return nil
		case *blockStmt:
			return visitBlock(s, ctx)
		case *ifStmt:
			if err := visitBlock(s.Consequence, ctx); err != nil {
				return err
			}
			return visitStmt(s.Alternative, ctx)
		case *whileStmt:
			return visitBlock(s.Body, ctx)
		case *forStmt:
			return visitBlock(s.Body, ctx)
		case *whenStmt:
			for _, cc := range s.Cases {
				if err := visitBlock(cc.Body, ctx); err != nil {
					return err
				}
			}
			return visitBlock(s.DefaultCase, ctx)
		default:
			return nil
		}
	}
	visitBlock = func(block *blockStmt, ctx typeContext) error {
		if block == nil {
			return nil
		}
		if ctx.localTypes == nil {
			ctx.localTypes = make(map[string]schema.TypeDesc)
		}
		for _, stmt := range block.Stmts {
			if err := visitStmt(stmt, ctx); err != nil {
				return err
			}
		}
		return nil
	}
	if err := visitBlock(block, ctx); err != nil {
		return schema.TypeDesc{}, err
	}
	if !found {
		return schema.TypeDesc{}, newLoweringError("yield_value_required", "streaming callable must yield a value", spanFromBlock(block), "frontend/callable/stream", nil)
	}
	return inferred, nil
}

func inferExpressionType(expr expression, ctx typeContext) (schema.TypeDesc, error) {
	switch e := expr.(type) {
	case *intLiteral:
		return schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}, nil
	case *floatLiteral:
		return schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "float"}, nil
	case *stringLiteral:
		return schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}, nil
	case *boolLiteral:
		return schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "bool"}, nil
	case *nullLiteral:
		return schema.TypeDesc{Kind: schema.TypeKindVoid, Name: "void"}, nil
	case *identExpr:
		if ctx.localTypes != nil {
			if td, ok := ctx.localTypes[e.Value]; ok {
				return td, nil
			}
		}
		return schema.TypeDesc{}, newLoweringError("yield_type_inference_failed", fmt.Sprintf("cannot infer yield type from identifier %q", e.Value), spanFromExpr(expr), "frontend/type/inference", nil)
	case *arrayLiteral:
		if len(e.Elements) == 0 {
			return schema.TypeDesc{Kind: schema.TypeKindArray}, nil
		}
		elem, err := inferExpressionType(e.Elements[0], ctx)
		if err != nil {
			return schema.TypeDesc{}, err
		}
		for _, candidate := range e.Elements[1:] {
			td, err := inferExpressionType(candidate, ctx)
			if err != nil {
				return schema.TypeDesc{}, err
			}
			if !sameTypeDesc(elem, td) {
				return schema.TypeDesc{}, newSchemaValidationErrorWithTypes("array_literal_type_mismatch", "array literal elements must share a type",
					spanFromExpr(candidate), "frontend/type/inference",
					elem.String(), td.String())
			}
		}
		return schema.TypeDesc{Kind: schema.TypeKindArray, Element: &elem}, nil
	case *mapLiteral:
		if len(e.Pairs) == 0 {
			return schema.TypeDesc{Kind: schema.TypeKindMap}, nil
		}
		key, err := inferExpressionType(e.Pairs[0].Key, ctx)
		if err != nil {
			return schema.TypeDesc{}, err
		}
		value, err := inferExpressionType(e.Pairs[0].Value, ctx)
		if err != nil {
			return schema.TypeDesc{}, err
		}
		for _, pair := range e.Pairs[1:] {
			candidateKey, err := inferExpressionType(pair.Key, ctx)
			if err != nil {
				return schema.TypeDesc{}, err
			}
			candidateValue, err := inferExpressionType(pair.Value, ctx)
			if err != nil {
				return schema.TypeDesc{}, err
			}
			if !sameTypeDesc(key, candidateKey) {
				return schema.TypeDesc{}, newSchemaValidationErrorWithTypes("map_literal_type_mismatch", "map literal key types must match",
					spanFromExpr(pair.Key), "frontend/type/inference",
					key.String(), candidateKey.String())
			}
			if !sameTypeDesc(value, candidateValue) {
				return schema.TypeDesc{}, newSchemaValidationErrorWithTypes("map_literal_type_mismatch", "map literal value types must match",
					spanFromExpr(pair.Value), "frontend/type/inference",
					value.String(), candidateValue.String())
			}
		}
		return schema.TypeDesc{Kind: schema.TypeKindMap, Key: &key, Value: &value}, nil
	case *structLiteral:
		return typeAnnotationToTypeDesc(&typeAnnotation{tok: e.tok, Name: e.TypeName}, ctx), nil
	default:
		return schema.TypeDesc{}, newLoweringError("yield_type_inference_failed", fmt.Sprintf("cannot infer yield type from %T", expr), spanFromExpr(expr), "frontend/type/inference", nil)
	}
}

func sameTypeDesc(left schema.TypeDesc, right schema.TypeDesc) bool {
	if left.Kind != right.Kind || left.Name != right.Name || left.ClassName != right.ClassName {
		return false
	}
	if (left.Element == nil) != (right.Element == nil) {
		return false
	}
	if left.Element != nil && !sameTypeDesc(*left.Element, *right.Element) {
		return false
	}
	if (left.Key == nil) != (right.Key == nil) {
		return false
	}
	if left.Key != nil && !sameTypeDesc(*left.Key, *right.Key) {
		return false
	}
	if (left.Value == nil) != (right.Value == nil) {
		return false
	}
	if left.Value != nil && !sameTypeDesc(*left.Value, *right.Value) {
		return false
	}
	return true
}

// extractClassDesc maps a structStmt or classStmt AST node to a schema.ObjectDesc.
func extractClassDesc(stmt statement, ctx typeContext) schema.ObjectDesc {
	switch s := stmt.(type) {
	case *structStmt:
		return classDescFromStruct(s, ctx)
	case *classStmt:
		return classDescFromClass(s, ctx)
	default:
		return schema.ObjectDesc{}
	}
}

func classDescFromStruct(s *structStmt, ctx typeContext) schema.ObjectDesc {
	fieldDescs := make([]schema.FieldDesc, 0, len(s.Fields))
	for _, f := range s.Fields {
		var ref *schema.FieldRef
		if f.Ref != nil {
			ref = &schema.FieldRef{Target: f.Ref.Target, Field: f.Ref.Field}
		}
		fieldDescs = append(fieldDescs, schema.FieldDesc{
			Name:     f.Name.Value,
			Type:     typeAnnotationToTypeDesc(f.Type_, ctx),
			Private:  f.Access == accessPrivate,
			Optional: f.Optional,
			IsKey:    f.Key,
			Ref:      ref,
		})
	}
	return schema.ObjectDesc{
		Kind:        schema.TypeKindStruct,
		Name:        s.Name.Value,
		Fields:      fieldDescs,
		SchemaID:    s.SchemaID,
		IsComponent: s.IsComponent,
		IsData:      s.IsData,
		DataVersion: s.DataVersion,
	}
}

func classDescFromClass(s *classStmt, ctx typeContext) schema.ObjectDesc {
	fieldDescs := make([]schema.FieldDesc, 0, len(s.Fields))
	for _, f := range s.Fields {
		fieldDescs = append(fieldDescs, schema.FieldDesc{
			Name:     f.Name.Value,
			Type:     typeAnnotationToTypeDesc(f.Type_, ctx),
			Private:  f.Access == accessPrivate,
			Optional: f.Optional,
		})
	}
	methodDescs := make([]schema.MethodDesc, 0, len(s.Methods))
	for _, m := range s.Methods {
		methodDescs = append(methodDescs, extractMethodDesc(m, ctx))
	}
	parent := ""
	if s.Parent != nil {
		parent = s.Parent.Value
	}
	implements := make([]string, 0, len(s.Implements))
	for _, iface := range s.Implements {
		implements = append(implements, iface.Value)
	}
	return schema.ObjectDesc{
		Kind:       schema.TypeKindClass,
		Name:       s.Name.Value,
		Fields:     fieldDescs,
		Parent:     parent,
		IsOpen:     s.IsOpen,
		Implements: implements,
		Methods:    methodDescs,
	}
}

// extractMethodDesc maps a funStmt AST node to a schema.MethodDesc.
func extractMethodDesc(stmt *funStmt, ctx typeContext) schema.MethodDesc {
	params := make([]schema.ParameterDesc, 0, len(stmt.Params))
	for _, p := range stmt.Params {
		params = append(params, schema.ParameterDesc{
			Name: p.Name.Value,
			Type: typeAnnotationToTypeDesc(p.Type_, ctx),
		})
	}
	var returns []schema.TypeDesc
	if stmt.ReturnType != nil {
		returns = []schema.TypeDesc{typeAnnotationToTypeDesc(stmt.ReturnType, ctx)}
	}
	return schema.MethodDesc{
		Name:       stmt.Name.Value,
		Parameters: params,
		Returns:    returns,
		IsOpen:     stmt.IsOpen,
		IsOverride: stmt.IsOverride,
		Private:    stmt.Access == accessPrivate,
	}
}

// extractEnumDesc maps an enumStmt AST node to a schema.EnumDesc, resolving
// member values (explicit `= N` or auto-increment from the previous member).
func extractEnumDesc(stmt *enumStmt) schema.EnumDesc {
	desc := schema.EnumDesc{Name: stmt.Name.Value, Members: make([]schema.EnumMemberDesc, 0, len(stmt.Members))}
	next := int64(0)
	for _, m := range stmt.Members {
		if m == nil || m.Name == nil {
			continue
		}
		if m.HasValue {
			next = m.Value
		}
		desc.Members = append(desc.Members, schema.EnumMemberDesc{Name: m.Name.Value, Value: int(next)})
		next++
	}
	return desc
}

// extractInterfaceDesc maps an interfaceStmt AST node to a schema.InterfaceDesc.
func extractInterfaceDesc(stmt *interfaceStmt, ctx typeContext) schema.InterfaceDesc {
	methods := make([]schema.MethodDesc, 0, len(stmt.Methods))
	for _, m := range stmt.Methods {
		params := make([]schema.ParameterDesc, 0, len(m.Params))
		for _, p := range m.Params {
			params = append(params, schema.ParameterDesc{
				Name: p.Name.Value,
				Type: typeAnnotationToTypeDesc(p.Type_, ctx),
			})
		}
		var returns []schema.TypeDesc
		if m.ReturnType != nil {
			returns = []schema.TypeDesc{typeAnnotationToTypeDesc(m.ReturnType, ctx)}
		}
		methods = append(methods, schema.MethodDesc{
			Name:       m.Name.Value,
			Parameters: params,
			Returns:    returns,
		})
	}
	return schema.InterfaceDesc{
		Name:    stmt.Name.Value,
		Methods: methods,
	}
}

// typeAnnotationToTypeDesc converts an AST typeAnnotation to a schema.TypeDesc.
// Uses token kind for type resolution — no string heuristics.
// Struct names resolve to TypeKindStruct; class names resolve to TypeKindClass.
func typeAnnotationToTypeDesc(ta *typeAnnotation, ctx typeContext) schema.TypeDesc {
	if ta == nil {
		return schema.TypeDesc{}
	}

	// Function types are a compile-time-only surface in this phase; they
	// surface as scalar `any` in schema descriptors (closures travel as
	// ordinary first-class values).
	if ta.IsFun {
		return schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "any"}
	}

	resolved := ta
	if ctx.typeAliases != nil {
		seen := make(map[string]bool)
		for {
			aliased, ok := ctx.typeAliases[resolved.Name]
			if !ok || seen[resolved.Name] || aliased == nil {
				break
			}
			seen[resolved.Name] = true
			resolved = aliased
		}
	}
	resolvedName := resolved.Name

	// Compile-time imported types take precedence over local name heuristics.
	if ctx.importedTypeDescs != nil {
		if td, ok := ctx.importedTypeDescs[resolvedName]; ok {
			return td
		}
	}

	// Name-based resolution for compound types parsed with bracket/map syntax.
	switch {
	case resolvedName == "array" && len(resolved.Params) == 1:
		elem := typeAnnotationToTypeDesc(resolved.Params[0], ctx)
		return schema.TypeDesc{Kind: schema.TypeKindArray, Element: &elem}
	case resolvedName == "map" && len(resolved.Params) == 2:
		key := typeAnnotationToTypeDesc(resolved.Params[0], ctx)
		value := typeAnnotationToTypeDesc(resolved.Params[1], ctx)
		return schema.TypeDesc{Kind: schema.TypeKindMap, Key: &key, Value: &value}
	default:
		switch {
		case resolvedName == "void":
			return schema.TypeDesc{Kind: schema.TypeKindVoid, Name: "void"}
		case resolvedName == "bool", resolvedName == "byte", resolvedName == "short", resolvedName == "ushort", resolvedName == "int", resolvedName == "uint", resolvedName == "long", resolvedName == "ulong", resolvedName == "float", resolvedName == "double", resolvedName == "string", resolvedName == "bytes", resolvedName == "any":
			return schema.TypeDesc{Kind: schema.TypeKindScalar, Name: resolvedName}
		case resolvedName == "media":
			return schema.TypeDesc{Kind: schema.TypeKindMedia, Name: "media"}
		case ctx.structNames[resolvedName]:
			return schema.TypeDesc{Kind: schema.TypeKindStruct, Name: resolvedName, ClassName: resolvedName}
		case ctx.classNames[resolvedName]:
			return schema.TypeDesc{Kind: schema.TypeKindClass, Name: resolvedName, ClassName: resolvedName}
		case ctx.enumNames[resolvedName]:
			return schema.TypeDesc{Kind: schema.TypeKindEnum, Name: resolvedName}
		}

		switch resolved.tok.typ {
		case tokVoid:
			return schema.TypeDesc{Kind: schema.TypeKindVoid, Name: "void"}
		case tokArray:
			if len(resolved.Params) == 1 {
				elem := typeAnnotationToTypeDesc(resolved.Params[0], ctx)
				return schema.TypeDesc{Kind: schema.TypeKindArray, Element: &elem}
			}
			return schema.TypeDesc{Kind: schema.TypeKindArray}
		case tokMap:
			if len(resolved.Params) == 2 {
				key := typeAnnotationToTypeDesc(resolved.Params[0], ctx)
				value := typeAnnotationToTypeDesc(resolved.Params[1], ctx)
				return schema.TypeDesc{Kind: schema.TypeKindMap, Key: &key, Value: &value}
			}
			return schema.TypeDesc{Kind: schema.TypeKindMap}
		case tokBool, tokByte, tokShort, tokUShort, tokInt, tokUint,
			tokLong, tokUlong, tokFloat, tokDouble, tokString, tokBytes, tokAny:
			return schema.TypeDesc{Kind: schema.TypeKindScalar, Name: resolvedName}
		case tokMedia:
			return schema.TypeDesc{Kind: schema.TypeKindMedia, Name: "media"}
		case tokClass:
			return schema.TypeDesc{Kind: schema.TypeKindClass, Name: resolvedName, ClassName: resolvedName}
		case tokIdent:
			if len(resolvedName) > 0 && resolvedName[0] >= 'A' && resolvedName[0] <= 'Z' {
				if ctx.structNames[resolvedName] {
					return schema.TypeDesc{Kind: schema.TypeKindStruct, Name: resolvedName, ClassName: resolvedName}
				}
				return schema.TypeDesc{Kind: schema.TypeKindClass, Name: resolvedName, ClassName: resolvedName}
			}
			return schema.TypeDesc{Kind: schema.TypeKindScalar, Name: resolvedName}
		}
	}

	return schema.TypeDesc{}
}
