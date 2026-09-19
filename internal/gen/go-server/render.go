package goserver

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/qomos-w/spore/internal/gen/common"
	"github.com/qomos-w/spore/internal/gen/ts"
	"github.com/qomos-w/spore/schema"
)

// renderDispatcher renders the body of `<ns>_dispatcher_gen.go` for one
// namespace. The caller (Generate) is responsible for sorting entries
// alphabetically by Name and applying the visibility filter.
//
// Output shape:
//
//	package <pkg>
//
//	import "fmt"
//
//	type <NS>Handler interface {
//	    Method(req Req) (Final, error)
//	    ...
//	}
//
//	func Dispatch<NS>(h <NS>Handler, callID string, payload map[string]any) (map[string]any, error) {
//	    switch callID {
//	    case "<ns>.<name>":
//	        req := Req{ Field: payload["json_name"].(GoType), ... }
//	        resp, err := h.Method(req)
//	        if err != nil { return nil, err }
//	        return map[string]any{
//	            "json_name": resp.Field,
//	            ...
//	        }, nil
//	    default:
//	        return nil, fmt.Errorf("unknown callID: %s", callID)
//	    }
//	}
func renderDispatcher(
	pkg, namespace string,
	entries []ts.NamedCallableDesc,
	schemaIndex map[schemaKey]schema.ObjectDesc,
) (string, error) {
	var b strings.Builder

	fmt.Fprintf(&b, "package %s\n\n", pkg)
	b.WriteString("import \"fmt\"\n\n")

	handlerType := handlerNameFor(namespace)
	dispatchFn := dispatchNameFor(namespace)

	// Interface declaration — one method per surviving callable.
	fmt.Fprintf(&b, "// %s is the typed handler shape that %s routes calls into.\n", handlerType, dispatchFn)
	fmt.Fprintf(&b, "// Each method matches one callable in the %q namespace; user code\n", namespace)
	fmt.Fprintf(&b, "// implements this interface and the boundary marshal between\n")
	fmt.Fprintf(&b, "// map[string]any wire payloads and the typed Go structs is generated\n")
	fmt.Fprintf(&b, "// by spore-gen-go-server.\n")
	fmt.Fprintf(&b, "type %s interface {\n", handlerType)
	for _, c := range entries {
		method, err := renderHandlerMethod(c, schemaIndex)
		if err != nil {
			return "", fmt.Errorf("callable %s.%s: %w", c.Namespace, c.Name, err)
		}
		b.WriteString("\t" + method + "\n")
	}
	b.WriteString("}\n\n")

	// Dispatch function — switch over callID, marshal in/out per callable.
	fmt.Fprintf(&b, "// %s routes a wire-form callID and payload to the matching\n", dispatchFn)
	fmt.Fprintf(&b, "// %s method, marshaling the request payload into the typed\n", handlerType)
	fmt.Fprintf(&b, "// request struct and converting the typed response back into a\n")
	fmt.Fprintf(&b, "// map[string]any wire payload.\n")
	fmt.Fprintf(&b, "func %s(h %s, callID string, payload map[string]any) (map[string]any, error) {\n", dispatchFn, handlerType)
	b.WriteString("\tswitch callID {\n")
	for _, c := range entries {
		caseBody, err := renderCallableCase(c, schemaIndex)
		if err != nil {
			return "", fmt.Errorf("callable %s.%s: %w", c.Namespace, c.Name, err)
		}
		b.WriteString(caseBody)
	}
	b.WriteString("\tdefault:\n")
	b.WriteString("\t\treturn nil, fmt.Errorf(\"unknown callID: %s\", callID)\n")
	b.WriteString("\t}\n")
	b.WriteString("}\n")

	return b.String(), nil
}

// renderHandlerMethod emits one line for the handler interface, of the
// form `Method(req Req) (Final, error)`. Both Req and Final must be
// struct types in the same Go package as the generated file (the
// renderer does not yet emit cross-package import paths).
func renderHandlerMethod(c ts.NamedCallableDesc, schemaIndex map[schemaKey]schema.ObjectDesc) (string, error) {
	reqType, err := renderStructTypeRef(c.Req)
	if err != nil {
		return "", fmt.Errorf("req type: %w", err)
	}
	finalType, err := renderStructTypeRef(c.Final)
	if err != nil {
		return "", fmt.Errorf("final type: %w", err)
	}
	if _, ok := lookupSchema(c.Namespace, c.Req, schemaIndex); !ok {
		return "", fmt.Errorf("req schema %q not found in manifest schemas[]", reqType)
	}
	if _, ok := lookupSchema(c.Namespace, c.Final, schemaIndex); !ok {
		return "", fmt.Errorf("final schema %q not found in manifest schemas[]", finalType)
	}
	methodName := goMethodName(c.Name)
	return fmt.Sprintf("%s(req %s) (%s, error)", methodName, reqType, finalType), nil
}

// renderCallableCase emits the `case "<ns>.<name>":` block for one
// callable, including the request marshal, handler invocation, and
// response demarshal. Indentation is tab-based to match the
// surrounding switch body inside renderDispatcher.
func renderCallableCase(c ts.NamedCallableDesc, schemaIndex map[schemaKey]schema.ObjectDesc) (string, error) {
	reqDesc, ok := lookupSchema(c.Namespace, c.Req, schemaIndex)
	if !ok {
		return "", fmt.Errorf("req schema not found in manifest")
	}
	finalDesc, ok := lookupSchema(c.Namespace, c.Final, schemaIndex)
	if !ok {
		return "", fmt.Errorf("final schema not found in manifest")
	}

	reqType, err := renderStructTypeRef(c.Req)
	if err != nil {
		return "", err
	}
	methodName := goMethodName(c.Name)

	var b strings.Builder
	fmt.Fprintf(&b, "\tcase %q:\n", c.Namespace+"."+c.Name)

	// Request marshal: typed struct literal whose fields read out of
	// payload via type assertions.
	fmt.Fprintf(&b, "\t\treq := %s{\n", reqType)
	for _, f := range reqDesc.Fields {
		goType, err := renderScalarGoType(f.Type)
		if err != nil {
			return "", fmt.Errorf("req field %q: %w", f.Name, err)
		}
		fmt.Fprintf(&b, "\t\t\t%s: payload[%q].(%s),\n", goFieldName(f.Name), f.Name, goType)
	}
	b.WriteString("\t\t}\n")

	// Handler invocation.
	fmt.Fprintf(&b, "\t\tresp, err := h.%s(req)\n", methodName)
	b.WriteString("\t\tif err != nil {\n")
	b.WriteString("\t\t\treturn nil, err\n")
	b.WriteString("\t\t}\n")

	// Response demarshal: typed struct → map[string]any.
	b.WriteString("\t\treturn map[string]any{\n")
	for _, f := range finalDesc.Fields {
		fmt.Fprintf(&b, "\t\t\t%q: resp.%s,\n", f.Name, goFieldName(f.Name))
	}
	b.WriteString("\t\t}, nil\n")

	return b.String(), nil
}

// renderStructTypeRef converts a schema.TypeDesc into a Go type reference
// string usable on the right-hand side of a parameter or struct literal.
// MVP scope: only struct kinds are supported; everything else surfaces an
// error so the codegen fails loudly rather than emitting bogus Go.
func renderStructTypeRef(t schema.TypeDesc) (string, error) {
	if t.Kind != schema.TypeKindStruct {
		return "", fmt.Errorf("unsupported type kind %q (only struct supported in goserver MVP)", t.Kind)
	}
	if t.ClassName != "" {
		return t.ClassName, nil
	}
	if t.Name != "" && t.Name != "struct" {
		return t.Name, nil
	}
	return "", fmt.Errorf("struct TypeDesc missing ClassName/Name")
}

// renderScalarGoType maps a spore scalar TypeDesc into the Go type the
// dispatcher uses for its `payload[...].(<type>)` cast, resolving through the
// shared scalar table in internal/gen/common. Only scalar kinds are supported
// in the MVP; the codegen errors out for nested struct / array / map fields
// rather than emitting unsafe code.
func renderScalarGoType(t schema.TypeDesc) (string, error) {
	if t.Kind != schema.TypeKindScalar {
		return "", fmt.Errorf("unsupported field kind %q (only scalar fields supported in goserver MVP)", t.Kind)
	}
	if gt, ok := common.GoServerScalar(t.Name); ok {
		return gt, nil
	}
	return "", fmt.Errorf("unsupported scalar %q", t.Name)
}

// lookupSchema resolves a callable's TypeDesc reference against the
// schema index built from manifest.schemas[]. Lookup is by Name (or
// ClassName fallback), scoped to the callable's own namespace.
func lookupSchema(
	namespace string,
	t schema.TypeDesc,
	schemaIndex map[schemaKey]schema.ObjectDesc,
) (schema.ObjectDesc, bool) {
	name := t.Name
	if t.ClassName != "" {
		name = t.ClassName
	}
	desc, ok := schemaIndex[schemaKey{Namespace: namespace, Name: name}]
	return desc, ok
}

// handlerNameFor maps a namespace into a Go interface name. Conventions
// match ts-client's `<NS>Client` pattern: `<NS>Handler` (auth →
// AuthHandler).
func handlerNameFor(namespace string) string {
	return titleCase(namespace) + "Handler"
}

// dispatchNameFor maps a namespace into the Go top-level dispatch
// function name. (auth → DispatchAuth.)
func dispatchNameFor(namespace string) string {
	return "Dispatch" + titleCase(namespace)
}

// goMethodName converts a wire-form callable name (typically snake_case
// like "auth.tail_logins") into a Go-idiomatic method name (TailLogins).
// Underscores are removed and each segment is title-cased.
func goMethodName(callableName string) string {
	parts := strings.Split(callableName, "_")
	for i, p := range parts {
		parts[i] = titleCase(p)
	}
	return strings.Join(parts, "")
}

// goFieldName upper-cases the first letter of a JSON field name to
// produce its Go counterpart. Conversion is conservative: rest of the
// string is preserved verbatim, so camelCase JSON like "loginCount"
// becomes "LoginCount" (correct), and lower_snake_case stays unchanged
// after the first letter (e.g., "login_count" → "Login_count" — fine
// for codegen output but stylistically odd; producers should prefer
// camelCase JSON tags).
func goFieldName(jsonName string) string {
	if jsonName == "" {
		return ""
	}
	runes := []rune(jsonName)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

// titleCase upper-cases the first rune of s and leaves the rest
// untouched. Returns "" for empty input.
func titleCase(s string) string {
	if s == "" {
		return ""
	}
	runes := []rune(s)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}
