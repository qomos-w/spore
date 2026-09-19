package schema

import (
	"encoding/json"
	"reflect"
	"testing"
)

// fixtureLoginReq mirrors exp09 LoginReq for manifest builder tests.
// Defining fixtures inline keeps the schema package self-contained.
type fixtureLoginReq struct {
	User string `json:"user"`
}

type fixtureLoginResp struct {
	Ok    bool   `json:"ok"`
	Token string `json:"token"`
}

type fixtureProjection struct {
	Logins int `json:"logins"`
}

type fixtureLookupUserReq struct {
	UserID string `json:"userId"`
}

type fixtureLookupUserResp struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

type fixtureBusinessActor struct {
	logins int
}

func (a *fixtureBusinessActor) Login(req fixtureLoginReq) (fixtureLoginResp, error) {
	a.logins++
	return fixtureLoginResp{Ok: true, Token: "tok-" + req.User}, nil
}

func (a *fixtureBusinessActor) LookupUser(req fixtureLookupUserReq) (fixtureLookupUserResp, error) {
	return fixtureLookupUserResp{Name: req.UserID, Age: 0}, nil
}

func (a *fixtureBusinessActor) helper(_ int) int { return 0 }

func TestBuildManifest_SingleNamespaceUnaryCallable(t *testing.T) {
	m, err := BuildManifest([]NamespaceDecl{
		{
			Namespace: "auth",
			Methods: []any{
				(*fixtureBusinessActor).Login,
			},
			ExtraSchemas: []any{
				fixtureProjection{},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildManifest: %v", err)
	}

	if got, want := len(m.Schemas), 3; got != want {
		t.Fatalf("schemas len = %d, want %d", got, want)
	}
	expectSchema := []struct {
		name string
		id   uint64
	}{
		{"fixtureLoginReq", 1},
		{"fixtureLoginResp", 2},
		{"fixtureProjection", 3},
	}
	for i, want := range expectSchema {
		got := m.Schemas[i]
		if got.Namespace != "auth" {
			t.Errorf("schemas[%d].Namespace = %q, want auth", i, got.Namespace)
		}
		if got.Name != want.name {
			t.Errorf("schemas[%d].Name = %q, want %q", i, got.Name, want.name)
		}
		if got.SchemaID != want.id {
			t.Errorf("schemas[%d].SchemaID = %d, want %d", i, got.SchemaID, want.id)
		}
		if got.Visibility != "public" {
			t.Errorf("schemas[%d].Visibility = %q, want public", i, got.Visibility)
		}
	}

	if got, want := len(m.Callables), 1; got != want {
		t.Fatalf("callables len = %d, want %d", got, want)
	}
	c := m.Callables[0]
	if c.Namespace != "auth" || c.Name != "login" || c.Mode != "unary" {
		t.Errorf("callable[0] = %+v, want {auth, login, unary}", c)
	}
	if c.ReqSchemaID != 1 || c.FinalSchemaID != 2 {
		t.Errorf("callable[0] schema ids = (%d, %d), want (1, 2)", c.ReqSchemaID, c.FinalSchemaID)
	}
	if c.Req.ClassName != "fixtureLoginReq" || c.Final.ClassName != "fixtureLoginResp" {
		t.Errorf("callable[0] type refs = (%s, %s), want (fixtureLoginReq, fixtureLoginResp)", c.Req.ClassName, c.Final.ClassName)
	}
}

func TestBuildManifest_MultipleCallables(t *testing.T) {
	// Multi-callable namespace verifies (a) snake_case conversion of
	// LookupUser → lookup_user and (b) schema ID assignment in declaration
	// order across multiple methods.
	m, err := BuildManifest([]NamespaceDecl{
		{
			Namespace: "auth",
			Methods: []any{
				(*fixtureBusinessActor).Login,
				(*fixtureBusinessActor).LookupUser,
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildManifest: %v", err)
	}

	if got, want := len(m.Schemas), 4; got != want {
		t.Fatalf("schemas len = %d, want %d", got, want)
	}
	wantNames := []string{"fixtureLoginReq", "fixtureLoginResp", "fixtureLookupUserReq", "fixtureLookupUserResp"}
	for i, name := range wantNames {
		if got := m.Schemas[i].Name; got != name {
			t.Errorf("schemas[%d].Name = %q, want %q", i, got, name)
		}
		if got := m.Schemas[i].SchemaID; got != uint64(i+1) {
			t.Errorf("schemas[%d].SchemaID = %d, want %d", i, got, i+1)
		}
	}
	if got, want := len(m.Callables), 2; got != want {
		t.Fatalf("callables len = %d, want %d", got, want)
	}
	if got := m.Callables[1].Name; got != "lookup_user" {
		t.Errorf("callables[1].Name = %q, want lookup_user", got)
	}
	if got := m.Callables[1].ReqSchemaID; got != 3 {
		t.Errorf("callables[1].ReqSchemaID = %d, want 3", got)
	}
	if got := m.Callables[1].FinalSchemaID; got != 4 {
		t.Errorf("callables[1].FinalSchemaID = %d, want 4", got)
	}
}

func TestBuildManifest_RejectsEmptyNamespace(t *testing.T) {
	_, err := BuildManifest([]NamespaceDecl{{Namespace: ""}})
	if err == nil {
		t.Fatal("expected error for empty namespace")
	}
}

func TestBuildManifest_RejectsNilMethod(t *testing.T) {
	_, err := BuildManifest([]NamespaceDecl{{
		Namespace: "auth",
		Methods:   []any{nil},
	}})
	if err == nil {
		t.Fatal("expected error for nil method expression")
	}
}

func TestBuildManifest_RejectsNonFunction(t *testing.T) {
	_, err := BuildManifest([]NamespaceDecl{{
		Namespace: "auth",
		Methods:   []any{42},
	}})
	if err == nil {
		t.Fatal("expected error for non-function method entry")
	}
}

func TestBuildManifest_RejectsPlainFunction(t *testing.T) {
	// Plain function (not a method expression) has only 1 input; the
	// methods-only contract requires `func(R, Req) (Final, error)`.
	plain := func(req fixtureLoginReq) (fixtureLoginResp, error) {
		return fixtureLoginResp{}, nil
	}
	_, err := BuildManifest([]NamespaceDecl{{
		Namespace: "auth",
		Methods:   []any{plain},
	}})
	if err == nil {
		t.Fatal("expected error for plain function (not a method expression)")
	}
}

func TestBuildManifest_RejectsNonStructReq(t *testing.T) {
	bad := func(_ *fixtureBusinessActor, req string) (fixtureLoginResp, error) {
		return fixtureLoginResp{}, nil
	}
	_, err := BuildManifest([]NamespaceDecl{{
		Namespace: "auth",
		Methods:   []any{bad},
	}})
	if err == nil {
		t.Fatal("expected error for non-struct Req")
	}
}

func TestBuildManifest_RejectsMissingErrorReturn(t *testing.T) {
	bad := func(_ *fixtureBusinessActor, req fixtureLoginReq) (fixtureLoginResp, fixtureLoginResp) {
		return fixtureLoginResp{}, fixtureLoginResp{}
	}
	_, err := BuildManifest([]NamespaceDecl{{
		Namespace: "auth",
		Methods:   []any{bad},
	}})
	if err == nil {
		t.Fatal("expected error when second return is not error")
	}
}

func TestBuildManifest_DeduplicatesSharedTypes(t *testing.T) {
	// If the same struct appears as Req for two different callables
	// (or as both Req and Final via one callable), it should share an
	// ID rather than be assigned multiple IDs. We exercise this by
	// re-using fixtureLoginReq in ExtraSchemas — should not bump IDs.
	m, err := BuildManifest([]NamespaceDecl{
		{
			Namespace: "auth",
			Methods: []any{
				(*fixtureBusinessActor).Login,
			},
			ExtraSchemas: []any{
				fixtureLoginReq{}, // already an ID for this — must not duplicate
				fixtureProjection{},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildManifest: %v", err)
	}
	if got, want := len(m.Schemas), 3; got != want {
		t.Fatalf("schemas len = %d, want %d (dedup must not double-emit shared types)", got, want)
	}
	wantIDs := map[string]uint64{
		"fixtureLoginReq":   1,
		"fixtureLoginResp":  2,
		"fixtureProjection": 3,
	}
	for _, s := range m.Schemas {
		if got, want := s.SchemaID, wantIDs[s.Name]; got != want {
			t.Errorf("schema %q SchemaID = %d, want %d", s.Name, got, want)
		}
	}
}

func TestBuildManifest_JSONShapeMatchesManifestDecoder(t *testing.T) {
	// The Manifest struct's JSON shape must match what spore-gen-*
	// CLIs decode via internal/gen/manifest.Decode. We assert key field
	// names appear in the encoded JSON so future renames break this
	// test loudly instead of silently breaking codegen pipelines.
	m, err := BuildManifest([]NamespaceDecl{{
		Namespace: "auth",
		Methods:   []any{(*fixtureBusinessActor).Login},
	}})
	if err != nil {
		t.Fatalf("BuildManifest: %v", err)
	}
	encoded, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	got := string(encoded)
	for _, key := range []string{`"schemas"`, `"callables"`, `"namespace"`, `"schemaId"`, `"reqSchemaId"`, `"finalSchemaId"`, `"mode"`, `"visibility"`} {
		if !contains(got, key) {
			t.Errorf("encoded JSON missing %s; got = %s", key, got)
		}
	}
}

func contains(haystack, needle string) bool { return reflect.DeepEqual(haystack, "") == false && len(haystack) >= len(needle) && (indexOf(haystack, needle) >= 0) }

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

func TestSnakeCase(t *testing.T) {
	cases := map[string]string{
		"Login":            "login",
		"LookupUser":       "lookup_user",
		"HTTPRequest":      "http_request",
		"ABCMethod":        "abc_method",
		"Foo":              "foo",
		"FooBar":           "foo_bar",
		"FooBarBaz":        "foo_bar_baz",
	}
	for in, want := range cases {
		if got := snakeCase(in); got != want {
			t.Errorf("snakeCase(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestExtractGoMethodName(t *testing.T) {
	cases := []struct {
		runtimeName string
		want        string
		wantErr     bool
	}{
		{"github.com/qomos-w/spore/schema.(*fixtureBusinessActor).Login", "Login", false},
		{"github.com/qomos-w/spore/schema.(*fixtureBusinessActor).Login-fm", "Login", false},
		{"pkg.Recv.Method", "Method", false},
		{"github.com/qomos-w/spore/schema.fixtureBusinessActor.Login", "Login", false},
		{"pkg.PlainFunc", "", true},                  // plain function rejected
		{"github.com/x/y.lowercase.Method", "Method", false}, // unexported receiver OK; only method visibility matters
		{"github.com/x/y.Recv.lowercase", "", true},   // unexported method rejected
	}
	for _, c := range cases {
		got, err := extractGoMethodName(c.runtimeName)
		if (err != nil) != c.wantErr {
			t.Errorf("extractGoMethodName(%q) err = %v, wantErr = %v", c.runtimeName, err, c.wantErr)
		}
		if !c.wantErr && got != c.want {
			t.Errorf("extractGoMethodName(%q) = %q, want %q", c.runtimeName, got, c.want)
		}
	}
}

// fixtureMixedShapes carries one good unary-callable method plus three
// non-callable shapes. MethodsOf must keep Good and silently drop the
// rest, so that a typed actor declaring private helpers / debug dumpers
// alongside callables doesn't pollute the reflected callable list.
type fixtureMixedShapes struct{}

func (a *fixtureMixedShapes) Good(req fixtureLoginReq) (fixtureLoginResp, error) {
	return fixtureLoginResp{Ok: true}, nil
}

func (a *fixtureMixedShapes) TooManyInputs(req fixtureLoginReq, extra int) (fixtureLoginResp, error) {
	return fixtureLoginResp{}, nil
}

func (a *fixtureMixedShapes) NoErrorReturn(req fixtureLoginReq) fixtureLoginResp {
	return fixtureLoginResp{}
}

func (a *fixtureMixedShapes) NonStructReq(req string) (fixtureLoginResp, error) {
	return fixtureLoginResp{}, nil
}

func TestMethodsOf_ReflectsCallableShapesIntoBuilder(t *testing.T) {
	// MethodsOf scans fixtureBusinessActor and returns method-expression
	// values that BuildManifest consumes the same way as if the user had
	// written `(*fixtureBusinessActor).Login` literally. helper(int) int
	// is unexported — reflect.Type.NumMethod skips it implicitly.
	methods := MethodsOf((*fixtureBusinessActor)(nil))
	if got, want := len(methods), 2; got != want {
		t.Fatalf("MethodsOf returned %d methods, want %d (Login + LookupUser)", got, want)
	}
	m, err := BuildManifest([]NamespaceDecl{{
		Namespace: "auth",
		Methods:   methods,
	}})
	if err != nil {
		t.Fatalf("BuildManifest from MethodsOf: %v", err)
	}
	if got, want := len(m.Callables), 2; got != want {
		t.Fatalf("callables = %d, want %d", got, want)
	}
	// reflect.Type.Method is alphabetical by name: Login < LookupUser.
	wantNames := []string{"login", "lookup_user"}
	for i, want := range wantNames {
		if got := m.Callables[i].Name; got != want {
			t.Errorf("callables[%d].Name = %q, want %q", i, got, want)
		}
	}
}

func TestMethodsOf_NilReceiverReturnsEmpty(t *testing.T) {
	if got := MethodsOf(nil); len(got) != 0 {
		t.Errorf("MethodsOf(nil) = %v, want empty slice", got)
	}
}

func TestMethodsOf_PointerReceiverFormExposesAll(t *testing.T) {
	// Pointer-form input exposes pointer-receiver methods (the common
	// actor convention since callables typically mutate actor state).
	// Value-form input only exposes value-receiver methods, so a
	// fixture whose methods are all pointer-receiver returns 0 from
	// the value-form input.
	ptr := MethodsOf((*fixtureBusinessActor)(nil))
	val := MethodsOf(fixtureBusinessActor{})
	if got, want := len(ptr), 2; got != want {
		t.Errorf("pointer-form returned %d, want %d", got, want)
	}
	if got, want := len(val), 0; got != want {
		t.Errorf("value-form returned %d, want %d (all fixture methods are pointer-receiver)", got, want)
	}
}

func TestMethodsOf_SkipsNonCallableShapes(t *testing.T) {
	// fixtureMixedShapes has 4 exported methods but only one matches
	// the unary callable shape. MethodsOf must keep just the matching
	// one and silently drop the others — this is the contract that
	// lets actor types carry private helpers / debug dumpers next to
	// callables without disrupting reflection.
	methods := MethodsOf((*fixtureMixedShapes)(nil))
	if got, want := len(methods), 1; got != want {
		t.Fatalf("MethodsOf returned %d methods, want %d (only Good)", got, want)
	}
	m, err := BuildManifest([]NamespaceDecl{{Namespace: "auth", Methods: methods}})
	if err != nil {
		t.Fatalf("BuildManifest: %v", err)
	}
	if got, want := m.Callables[0].Name, "good"; got != want {
		t.Errorf("callable name = %q, want %q (only Good must survive)", got, want)
	}
}
