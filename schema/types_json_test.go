package schema

import (
	"encoding/json"
	"strings"
	"testing"
)

// JSON marshaling for descriptor types must produce lowerCamelCase wire keys
// matching cmd/spore-gen-ts/README.md and ts/src/schema.ts.

func TestTypeDesc_JSONMarshalUsesLowerCamelCase(t *testing.T) {
	desc := TypeDesc{
		Kind: TypeKindArray,
		Name: "ArrayOfString",
		Element: &TypeDesc{
			Kind: TypeKindScalar,
			Name: "string",
		},
	}

	out, err := json.Marshal(desc)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	got := string(out)

	for _, key := range []string{`"kind":"array"`, `"name":"ArrayOfString"`, `"element":`} {
		if !strings.Contains(got, key) {
			t.Errorf("expected %q in %s", key, got)
		}
	}
	for _, banned := range []string{`"Kind":`, `"Name":`, `"Element":`} {
		if strings.Contains(got, banned) {
			t.Errorf("expected lowerCamelCase, found PascalCase key %q in %s", banned, got)
		}
	}
}

func TestTypeDesc_OmitEmptyOnOptionalFields(t *testing.T) {
	desc := TypeDesc{
		Kind: TypeKindScalar,
		Name: "int",
	}

	out, err := json.Marshal(desc)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	got := string(out)

	for _, banned := range []string{`"element"`, `"key"`, `"value"`, `"className"`, `"classId"`, `"typeId"`} {
		if strings.Contains(got, banned) {
			t.Errorf("optional field %q should be omitted when zero, got %s", banned, got)
		}
	}
	if !strings.Contains(got, `"kind":"scalar"`) {
		t.Errorf("required field 'kind' must always emit, got %s", got)
	}
}

func TestObjectDesc_JSONRoundTrip(t *testing.T) {
	in := ObjectDesc{
		Kind: TypeKindStruct,
		Name: "User",
		Fields: []FieldDesc{
			{Name: "id", Type: TypeDesc{Kind: TypeKindScalar, Name: "string"}},
			{Name: "age", Type: TypeDesc{Kind: TypeKindScalar, Name: "int"}, Private: true},
		},
	}

	wire, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	got := string(wire)

	if !strings.Contains(got, `"kind":"struct"`) {
		t.Errorf("expected lowerCamelCase 'kind', got %s", got)
	}
	if !strings.Contains(got, `"fields":`) {
		t.Errorf("expected lowerCamelCase 'fields', got %s", got)
	}
	if !strings.Contains(got, `"private":true`) {
		t.Errorf("expected lowerCamelCase 'private' to be present when true, got %s", got)
	}

	// non-private FieldDesc must omit "private" because of omitempty
	if strings.Contains(got, `"name":"id","type":{"kind":"scalar","name":"string"},"private"`) {
		t.Errorf("expected 'private' to be omitted when false, got %s", got)
	}

	var out ObjectDesc
	if err := json.Unmarshal(wire, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out.Kind != in.Kind || out.Name != in.Name {
		t.Errorf("round-trip mismatch on top-level: in=%+v out=%+v", in, out)
	}
	if len(out.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(out.Fields))
	}
	if out.Fields[1].Private != true {
		t.Errorf("expected Private=true on second field, got %+v", out.Fields[1])
	}
}

func TestCallableDesc_RequiredFieldsAlwaysEmit(t *testing.T) {
	desc := CallableDesc{
		Name:       "noop",
		Parameters: []ParameterDesc{},
		Returns:    []TypeDesc{},
		HasError:   false,
		Mode:       CallableModeUnary,
	}

	out, err := json.Marshal(desc)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	got := string(out)

	// HasError, Parameters, Returns are required on the wire even when
	// zero/empty, because the TypeScript mirror declares them as required
	// fields. They must NOT be omitted.
	for _, required := range []string{`"hasError":false`, `"parameters":[]`, `"returns":[]`, `"mode":"unary"`, `"name":"noop"`} {
		if !strings.Contains(got, required) {
			t.Errorf("required wire field %q missing in %s", required, got)
		}
	}
	if strings.Contains(got, `"streaming"`) {
		t.Errorf("nil Streaming pointer should be omitted, got %s", got)
	}
}

func TestObjectDesc_DecodeAcceptsLowerCamelCase(t *testing.T) {
	wire := []byte(`{
		"kind":"struct",
		"name":"X",
		"fields":[
			{"name":"f","type":{"kind":"scalar","name":"int"}}
		]
	}`)

	var got ObjectDesc
	if err := json.Unmarshal(wire, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.Kind != TypeKindStruct || got.Name != "X" || len(got.Fields) != 1 {
		t.Fatalf("unmarshal mismatch: %+v", got)
	}
	if got.Fields[0].Name != "f" || got.Fields[0].Type.Kind != TypeKindScalar {
		t.Fatalf("nested field mismatch: %+v", got.Fields[0])
	}
}

func TestObjectDesc_DecodeRemainsCaseInsensitive(t *testing.T) {
	// Backward-compat check: existing PascalCase manifests still decode
	// because Go's json.Unmarshal is case-insensitive on field names.
	wire := []byte(`{
		"Kind":"struct",
		"Name":"X",
		"Fields":[
			{"Name":"f","Type":{"Kind":"scalar","Name":"int"}}
		]
	}`)

	var got ObjectDesc
	if err := json.Unmarshal(wire, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.Kind != TypeKindStruct || got.Name != "X" || len(got.Fields) != 1 {
		t.Fatalf("PascalCase compat broke: %+v", got)
	}
}

// TestFieldDesc_DescriptionRoundTrip locks in the wire shape of the
// Description field — emitted as "description" only when non-empty,
// and recovered on Unmarshal.
func TestFieldDesc_DescriptionRoundTrip(t *testing.T) {
	in := FieldDesc{
		Name:        "id",
		Type:        TypeDesc{Kind: TypeKindScalar, Name: "string"},
		Description: "opaque account identifier",
	}

	wire, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	got := string(wire)
	if !strings.Contains(got, `"description":"opaque account identifier"`) {
		t.Errorf("expected lowerCamelCase 'description' in %s", got)
	}

	empty, err := json.Marshal(FieldDesc{Name: "id", Type: in.Type})
	if err != nil {
		t.Fatalf("Marshal empty: %v", err)
	}
	if strings.Contains(string(empty), `"description"`) {
		t.Errorf("empty Description must be omitted, got %s", string(empty))
	}

	var out FieldDesc
	if err := json.Unmarshal(wire, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out.Description != in.Description {
		t.Errorf("round-trip Description: got %q, want %q", out.Description, in.Description)
	}
}

// TestManifestCallable_DescriptionRoundTrip locks in the wire shape of
// ManifestCallable.Description — same omitempty / lowerCamelCase contract.
func TestManifestCallable_DescriptionRoundTrip(t *testing.T) {
	in := ManifestCallable{
		Namespace:     "auth",
		Name:          "login",
		Description:   "exchange credentials for a session token",
		Visibility:    "public",
		Mode:          string(CallableModeUnary),
		ReqSchemaID:   1,
		FinalSchemaID: 2,
		Req:           TypeDesc{Kind: TypeKindStruct, Name: "LoginReq", ClassName: "LoginReq"},
		Final:         TypeDesc{Kind: TypeKindStruct, Name: "LoginFinal", ClassName: "LoginFinal"},
	}

	wire, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	got := string(wire)
	if !strings.Contains(got, `"description":"exchange credentials for a session token"`) {
		t.Errorf("expected lowerCamelCase 'description' in %s", got)
	}

	empty := in
	empty.Description = ""
	bare, err := json.Marshal(empty)
	if err != nil {
		t.Fatalf("Marshal empty: %v", err)
	}
	if strings.Contains(string(bare), `"description"`) {
		t.Errorf("empty Description must be omitted, got %s", string(bare))
	}

	var out ManifestCallable
	if err := json.Unmarshal(wire, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out.Description != in.Description {
		t.Errorf("round-trip Description: got %q, want %q", out.Description, in.Description)
	}
}
