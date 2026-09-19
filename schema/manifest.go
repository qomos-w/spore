package schema

import (
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"unicode"
)

// ManifestSchema is one entry in the combined-form manifest's `schemas`
// list. JSON tags match the wire format consumed by spore-gen-* CLI
// tools via internal/gen/manifest.Decode, so a Manifest produced by
// BuildManifest can be JSON-encoded directly into the file that those
// CLIs read.
type ManifestSchema struct {
	Namespace  string     `json:"namespace"`
	SchemaID   uint64     `json:"schemaId"`
	Name       string     `json:"name"`
	Visibility string     `json:"visibility"`
	Object     ObjectDesc `json:"object"`
}

// ManifestCallable is one entry in the combined-form manifest's
// `callables` list.
//
// Description carries human-readable documentation forwarded to the
// generated TypeScript surface. BuildManifest does not populate it
// — Go method values expose no doc-string source through reflection
// — so it is empty for reflect-driven manifests. Hand-written or
// annotation-driven manifest producers may set it; the codegen
// pipeline (internal/gen/manifest, internal/gen/ts) preserves it
// through to renderers.
type ManifestCallable struct {
	Namespace     string    `json:"namespace"`
	Name          string    `json:"name"`
	ActorType     string    `json:"actorType,omitempty"`
	Description   string    `json:"description,omitempty"`
	Visibility    string    `json:"visibility"`
	Mode          string    `json:"mode"`
	Effect        string    `json:"effect,omitempty"`
	Service       string    `json:"service,omitempty"`
	ToolName      string    `json:"toolName,omitempty"`
	ReqSchemaID   uint64    `json:"reqSchemaId"`
	ChunkSchemaID uint64    `json:"chunkSchemaId,omitempty"`
	FinalSchemaID uint64    `json:"finalSchemaId"`
	Req           TypeDesc  `json:"req"`
	Chunk         *TypeDesc `json:"chunk,omitempty"`
	Final         TypeDesc  `json:"final"`
	FinalDesc     string    `json:"finalDesc,omitempty"`
	ChunkDesc     string    `json:"chunkDesc,omitempty"`
}

// Manifest is the combined-form manifest as a Go value. Encode to JSON to
// emit the manifest file that spore-gen-ts / spore-gen-ts-client /
// spore-gen-go-server consume.
type Manifest struct {
	Schemas   []ManifestSchema   `json:"schemas"`
	Callables []ManifestCallable `json:"callables"`
}

// NamespaceDecl declares one namespace's contribution to a manifest. It
// is the user-facing input shape for BuildManifest: callable methods are
// described as Go method expressions (e.g. `(*BusinessActor).Login`) so
// the Go compiler verifies their existence at codegen-build time, and
// any extra struct types that should appear in the schema list without
// being on a callable signature are listed in ExtraSchemas (e.g., a
// projection state type consumed via a side channel).
type NamespaceDecl struct {
	// Namespace is the codegen namespace string (e.g. "auth"). All
	// callables and schemas produced from this declaration are tagged
	// with this namespace.
	Namespace string

	// Methods is a list of Go method expressions describing the unary
	// callables in this namespace. Each entry must have shape
	// `func(R, ReqStruct) (FinalStruct, error)` — the receiver-bound
	// method-expression form produced by `(*Receiver).MethodName`.
	// The Go method name is converted to snake_case to form the
	// callable wire name (e.g. LookupUser → lookup_user).
	Methods []any

	// ExtraSchemas lists struct values whose schemas should appear in
	// the manifest even though they don't show up on any callable
	// signature. Useful for projection / event types consumed via
	// side channels.
	ExtraSchemas []any
}

// BuildManifest constructs a combined-form manifest from one or more
// namespace declarations.
//
// Schema IDs are assigned deterministically by walking declarations in
// argument order: for each namespace, callables' Req then Final types
// are visited in declaration order, then ExtraSchemas. The first occurrence
// of any reflect.Type receives the next free ID (starting at 1); duplicates
// share that ID. This ordering matches what hand-written exp09 manifest
// generators previously produced, so generated client / server output
// stays bit-identical when migrating to BuildManifest.
//
// Each callable's Req and Final reference their corresponding schema by
// {schemaId, classname} so the spore-gen-ts-client / spore-gen-go-server
// renderers can resolve types without re-running reflection.
//
// Currently only unary callables (`func(R, ReqStruct) (FinalStruct, error)`)
// are supported. Streaming will be added when exp09/exp10 grow a streaming
// callable on the typed-handler side.
func BuildManifest(decls []NamespaceDecl) (Manifest, error) {
	out := Manifest{}
	idIndex := newSchemaIDIndex()

	for declIdx, decl := range decls {
		if decl.Namespace == "" {
			return Manifest{}, fmt.Errorf("decls[%d]: namespace cannot be empty", declIdx)
		}

		// Reflect each method expression and queue its (Req, Final)
		// types into the ID index. The order matters — IDs are assigned
		// in visit order, so existing manifest consumers stay stable.
		callables := make([]reflectedCallable, 0, len(decl.Methods))
		for methodIdx, methodExpr := range decl.Methods {
			rc, err := reflectUnaryMethod(decl.Namespace, methodExpr)
			if err != nil {
				return Manifest{}, fmt.Errorf("decls[%d].Methods[%d]: %w", declIdx, methodIdx, err)
			}
			if err := idIndex.add(rc.reqType); err != nil {
				return Manifest{}, fmt.Errorf("decls[%d].Methods[%d] (Req): %w", declIdx, methodIdx, err)
			}
			if err := idIndex.add(rc.finalType); err != nil {
				return Manifest{}, fmt.Errorf("decls[%d].Methods[%d] (Final): %w", declIdx, methodIdx, err)
			}
			callables = append(callables, rc)
		}
		// ExtraSchemas come after callable types in the same namespace.
		extraTypes := make([]reflect.Type, 0, len(decl.ExtraSchemas))
		for extraIdx, val := range decl.ExtraSchemas {
			t, err := structTypeOf(val)
			if err != nil {
				return Manifest{}, fmt.Errorf("decls[%d].ExtraSchemas[%d]: %w", declIdx, extraIdx, err)
			}
			if err := idIndex.add(t); err != nil {
				return Manifest{}, fmt.Errorf("decls[%d].ExtraSchemas[%d]: %w", declIdx, extraIdx, err)
			}
			extraTypes = append(extraTypes, t)
		}

		// Emit ManifestSchema entries for every type assigned to this
		// namespace via this decl. Order: callable Req/Final visit
		// order followed by ExtraSchemas.
		emittedTypes := make(map[reflect.Type]bool)
		emit := func(t reflect.Type) error {
			if emittedTypes[t] {
				return nil
			}
			emittedTypes[t] = true
			obj, err := DescribeGoStruct(reflect.New(t).Elem().Interface())
			if err != nil {
				return fmt.Errorf("describe %s: %w", t.Name(), err)
			}
			out.Schemas = append(out.Schemas, ManifestSchema{
				Namespace:  decl.Namespace,
				SchemaID:   idIndex.idOf(t),
				Name:       t.Name(),
				Visibility: "public",
				Object:     obj,
			})
			return nil
		}
		for _, rc := range callables {
			if err := emit(rc.reqType); err != nil {
				return Manifest{}, fmt.Errorf("decls[%d]: %w", declIdx, err)
			}
			if err := emit(rc.finalType); err != nil {
				return Manifest{}, fmt.Errorf("decls[%d]: %w", declIdx, err)
			}
		}
		for _, t := range extraTypes {
			if err := emit(t); err != nil {
				return Manifest{}, fmt.Errorf("decls[%d]: %w", declIdx, err)
			}
		}

		// Emit ManifestCallable entries. Req/Final are referenced by
		// {schemaId, classname} so the renderers don't re-reflect.
		for _, rc := range callables {
			out.Callables = append(out.Callables, ManifestCallable{
				Namespace:     decl.Namespace,
				Name:          rc.callableName,
				Visibility:    "public",
				Mode:          string(CallableModeUnary),
				ReqSchemaID:   idIndex.idOf(rc.reqType),
				FinalSchemaID: idIndex.idOf(rc.finalType),
				Req:           structRef(rc.reqType.Name()),
				Final:         structRef(rc.finalType.Name()),
			})
		}
	}
	return out, nil
}

// MethodsOf reflects all exported methods on a receiver type whose
// signature matches the unary callable shape (`func(Req) (Final, error)`
// in source form, equivalently `func(R, Req) (Final, error)` as a method
// expression). Each returned slice element is a method-expression `any`
// value suitable for use directly as an entry in `NamespaceDecl.Methods`,
// so a callable list can be derived from a typed actor type without the
// caller having to list each method by name.
//
// Pass either a typed nil pointer (`(*BusinessActor)(nil)`) or a zero
// value (`BusinessActor{}`). The receiver value's data is not used —
// only its reflect.Type. The pointer form is preferred so that
// pointer-receiver methods (the common Go convention for actor-shaped
// types that mutate state) are reachable; value-form receivers expose
// only methods declared on the value type.
//
// Methods that don't match the unary shape (extra inputs, non-struct
// Req/Final, missing error return, etc.) are silently skipped — this
// is intentional, so non-callable helper methods on the actor (private
// state setters, debug dumpers, plain accessors) coexist with callables
// without disrupting reflection. Unexported methods are skipped because
// reflect.Type.NumMethod / Method enumerates only exported methods.
//
// Method order follows reflect.Type.Method's alphabetical-by-name
// ordering, so the schema-id assignment in BuildManifest is fully
// deterministic across rebuilds even though the user never wrote down
// an order.
func MethodsOf(receiver any) []any {
	if receiver == nil {
		return nil
	}
	t := reflect.TypeOf(receiver)
	out := make([]any, 0, t.NumMethod())
	for i := 0; i < t.NumMethod(); i++ {
		m := t.Method(i)
		ft := m.Func.Type()
		// Method expression includes the receiver as In(0): NumIn must
		// be exactly 2 (receiver, req); NumOut must be exactly 2
		// (final, error).
		if ft.NumIn() != 2 || ft.NumOut() != 2 {
			continue
		}
		if ft.In(1).Kind() != reflect.Struct {
			continue
		}
		if ft.Out(0).Kind() != reflect.Struct {
			continue
		}
		if ft.Out(1) != errorType {
			continue
		}
		out = append(out, m.Func.Interface())
	}
	return out
}

// reflectedCallable is the internal post-reflection shape for one method
// expression. It's not exported because BuildManifest is the only consumer.
type reflectedCallable struct {
	namespace    string
	callableName string // wire name (snake_case)
	goMethod     string // Go method identifier (e.g. "Login")
	reqType      reflect.Type
	finalType    reflect.Type
}

// reflectUnaryMethod accepts a Go method expression value and returns
// the reflectedCallable derived from its signature. It rejects anything
// that isn't a method expression with shape
// `func(R, ReqStruct) (FinalStruct, error)`, so a plain func or a
// streaming-shaped method does not silently slip through.
func reflectUnaryMethod(namespace string, methodExpr any) (reflectedCallable, error) {
	if methodExpr == nil {
		return reflectedCallable{}, fmt.Errorf("method expression is nil")
	}
	v := reflect.ValueOf(methodExpr)
	t := v.Type()
	if t.Kind() != reflect.Func {
		return reflectedCallable{}, fmt.Errorf("expected function (method expression), got %s", t.Kind())
	}
	// Method expression has the form func(Receiver, Req) (Final, error).
	if t.NumIn() != 2 {
		return reflectedCallable{}, fmt.Errorf("expected method expression with 2 inputs (receiver, req), got %d", t.NumIn())
	}
	if t.NumOut() != 2 {
		return reflectedCallable{}, fmt.Errorf("expected method expression with 2 outputs (final, error), got %d", t.NumOut())
	}
	reqType := t.In(1)
	if reqType.Kind() != reflect.Struct {
		return reflectedCallable{}, fmt.Errorf("expected Req to be a struct, got %s", reqType.Kind())
	}
	finalType := t.Out(0)
	if finalType.Kind() != reflect.Struct {
		return reflectedCallable{}, fmt.Errorf("expected Final to be a struct, got %s", finalType.Kind())
	}
	if t.Out(1) != errorType {
		return reflectedCallable{}, fmt.Errorf("expected second output to be error, got %s", t.Out(1))
	}

	rfn := runtime.FuncForPC(v.Pointer())
	if rfn == nil {
		return reflectedCallable{}, fmt.Errorf("cannot resolve runtime func name for method expression")
	}
	goName, err := extractGoMethodName(rfn.Name())
	if err != nil {
		return reflectedCallable{}, err
	}
	return reflectedCallable{
		namespace:    namespace,
		callableName: snakeCase(goName),
		goMethod:     goName,
		reqType:      reqType,
		finalType:    finalType,
	}, nil
}

// extractGoMethodName parses Go's runtime.Func.Name() output for a method
// expression to recover the bare Go method identifier.
//
// Supported forms (`-fm` may be appended for closure forms):
//   pkgpath.(*ReceiverType).Method
//   pkgpath.ReceiverType.Method
//
// The function rejects plain non-method names like `pkgpath.Func` because
// the typed-handler-callable contract is methods-only — passing a plain
// function would silently misregister it.
func extractGoMethodName(runtimeName string) (string, error) {
	s := strings.TrimSuffix(runtimeName, "-fm")

	// Pointer-receiver method: pkgpath.(*Recv).Method
	if idx := strings.LastIndex(s, ")."); idx >= 0 {
		name := s[idx+2:]
		if name == "" {
			return "", fmt.Errorf("empty method name in runtime func name %q", runtimeName)
		}
		if !isExportedIdent(name) {
			return "", fmt.Errorf("method name %q in %q is not an exported Go identifier", name, runtimeName)
		}
		return name, nil
	}

	// Value-receiver method: pkgpath.Recv.Method (no parens).
	// Strip the package path prefix (everything up to the last '/')
	// so that dots inside the package path (e.g. github.com) don't
	// get confused with the receiver/method dots. After that, we
	// expect exactly three dot-separated segments: pkgleaf.Recv.Method.
	// A plain function from the same package would have only two
	// segments (pkgleaf.Func), so it gets rejected here as required
	// by the methods-only contract. Receiver visibility is intentionally
	// not checked — fixture types in tests legitimately use unexported
	// receivers; we only require that the wire-emitted method name be
	// an exported Go identifier.
	tail := s
	if i := strings.LastIndex(tail, "/"); i >= 0 {
		tail = tail[i+1:]
	}
	parts := strings.Split(tail, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("runtime func name %q does not match the methods-only contract", runtimeName)
	}
	method := parts[2]
	if !isExportedIdent(method) {
		return "", fmt.Errorf("method name %q in %q is not an exported Go identifier", method, runtimeName)
	}
	return method, nil
}

// isExportedIdent returns true if s is a valid exported Go identifier
// (starts with an uppercase letter, contains only letters/digits/_).
func isExportedIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 {
			if !unicode.IsUpper(r) {
				return false
			}
			continue
		}
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			return false
		}
	}
	return true
}

// snakeCase converts an exported Go identifier (e.g. "LookupUser") to its
// snake_case wire name (e.g. "lookup_user"). Acronyms with mixed case are
// not specially handled; runs of consecutive uppercase letters become a
// single lowercase segment unless interrupted by a lowercase letter, e.g.
// "HTTPRequest" → "http_request".
func snakeCase(s string) string {
	if s == "" {
		return s
	}
	runes := []rune(s)
	var out []rune
	for i, r := range runes {
		isUpper := unicode.IsUpper(r)
		if i > 0 && isUpper {
			prev := runes[i-1]
			// Insert underscore between (lower)(Upper) and between (Upper)(Upper)(lower).
			if unicode.IsLower(prev) || unicode.IsDigit(prev) {
				out = append(out, '_')
			} else if i+1 < len(runes) && unicode.IsLower(runes[i+1]) {
				out = append(out, '_')
			}
		}
		out = append(out, unicode.ToLower(r))
	}
	return string(out)
}

// structTypeOf returns the reflect.Type of value, dereferencing one
// pointer level if needed. Returns an error if the underlying type is
// not a struct.
func structTypeOf(value any) (reflect.Type, error) {
	if value == nil {
		return nil, fmt.Errorf("value is nil")
	}
	t := reflect.TypeOf(value)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("expected struct, got %s", t.Kind())
	}
	return t, nil
}

// structRef builds a TypeDesc that the renderers can resolve back to a
// schema entry. ClassName is the import name on the TS side; Name keeps
// the same identifier for symmetry with the gen tools' golden fixtures.
func structRef(name string) TypeDesc {
	return TypeDesc{
		Kind:      TypeKindStruct,
		Name:      name,
		ClassName: name,
	}
}

// schemaIDIndex maintains a deterministic mapping from reflect.Type to
// schema ID. IDs start at 1 so a zero-valued ID can serve as "unset" in
// JSON (`omitempty` on optional schema id fields).
type schemaIDIndex struct {
	ids  map[reflect.Type]uint64
	next uint64
}

func newSchemaIDIndex() *schemaIDIndex {
	return &schemaIDIndex{ids: map[reflect.Type]uint64{}, next: 1}
}

func (idx *schemaIDIndex) add(t reflect.Type) error {
	if t == nil {
		return fmt.Errorf("type is nil")
	}
	if _, ok := idx.ids[t]; ok {
		return nil
	}
	idx.ids[t] = idx.next
	idx.next++
	return nil
}

func (idx *schemaIDIndex) idOf(t reflect.Type) uint64 {
	return idx.ids[t]
}
