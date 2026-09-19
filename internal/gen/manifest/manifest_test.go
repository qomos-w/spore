package manifest

import (
	"testing"

	"github.com/qomos-w/spore/internal/gen/ts"
	"github.com/qomos-w/spore/schema"
)

// TestDecode_LegacyArrayForm verifies the legacy bare-array
// manifest still decodes into schema entries with no callables.
func TestDecode_LegacyArrayForm(t *testing.T) {
	raw := []byte(`[
		{
			"namespace": "auth",
			"schemaId": 1,
			"name": "LoginReq",
			"visibility": "public",
			"object": {
				"kind": "struct",
				"name": "LoginReq",
				"fields": [
					{"name": "User", "type": {"kind": "scalar", "name": "string"}}
				]
			}
		}
	]`)

	schemas, callables, err := Decode(raw)
	if err != nil {
		t.Fatalf("decode legacy form: %v", err)
	}
	if len(schemas) != 1 {
		t.Fatalf("schemas len = %d, want 1", len(schemas))
	}
	if schemas[0].Namespace != "auth" || schemas[0].Name != "LoginReq" {
		t.Fatalf("schemas[0] = %#v", schemas[0])
	}
	if len(callables) != 0 {
		t.Fatalf("callables len = %d, want 0 (legacy form has no callables)", len(callables))
	}
}

// TestDecode_CombinedObjectForm verifies the new combined form
// decodes both schemas and callables.
func TestDecode_CombinedObjectForm(t *testing.T) {
	raw := []byte(`{
		"schemas": [
			{
				"namespace": "auth",
				"schemaId": 1,
				"name": "TailLoginsReq",
				"visibility": "public",
				"object": {"kind": "struct", "name": "TailLoginsReq", "fields": []}
			}
		],
		"callables": [
			{
				"namespace": "auth",
				"name": "tail_logins",
				"visibility": "public",
				"mode": "streaming",
				"reqSchemaId": 1,
				"chunkSchemaId": 2,
				"finalSchemaId": 3,
				"req":   {"kind": "struct", "name": "TailLoginsReq", "className": "TailLoginsReq"},
				"chunk": {"kind": "struct", "name": "LoginEvent", "className": "LoginEvent"},
				"final": {"kind": "struct", "name": "TailLoginsFinal", "className": "TailLoginsFinal"}
			}
		]
	}`)

	schemas, callables, err := Decode(raw)
	if err != nil {
		t.Fatalf("decode combined form: %v", err)
	}
	if len(schemas) != 1 || schemas[0].Name != "TailLoginsReq" {
		t.Fatalf("schemas = %#v", schemas)
	}
	if len(callables) != 1 {
		t.Fatalf("callables len = %d, want 1", len(callables))
	}
	c := callables[0]
	if c.Mode != schema.CallableModeStreaming {
		t.Fatalf("mode = %q, want streaming", c.Mode)
	}
	if c.ReqSchemaID != 1 || c.ChunkSchemaID != 2 || c.FinalSchemaID != 3 {
		t.Fatalf("schema ids = %d/%d/%d, want 1/2/3", c.ReqSchemaID, c.ChunkSchemaID, c.FinalSchemaID)
	}
	if c.Chunk == nil || c.Chunk.ClassName != "LoginEvent" {
		t.Fatalf("chunk = %#v", c.Chunk)
	}
}

// TestDecode_LeadingWhitespaceDoesNotConfuseFormatProbe ensures
// callers can pretty-print their manifest with leading whitespace and have
// the form-detection still work.
func TestDecode_LeadingWhitespaceDoesNotConfuseFormatProbe(t *testing.T) {
	raw := []byte("   \n\t {\"schemas\": [], \"callables\": []}")
	schemas, callables, err := Decode(raw)
	if err != nil {
		t.Fatalf("decode whitespace-prefixed: %v", err)
	}
	if len(schemas) != 0 || len(callables) != 0 {
		t.Fatalf("expected empty schemas/callables, got %d/%d", len(schemas), len(callables))
	}
}

// TestDecode_RejectsUnknownTopLevel rejects neither '[' nor '{'.
func TestDecode_RejectsUnknownTopLevel(t *testing.T) {
	if _, _, err := Decode([]byte("\"just a string\"")); err == nil {
		t.Errorf("expected rejection for non-array/object manifest")
	}
}

// TestDecode_EmptyInputRejected guards against zero-length input.
func TestDecode_EmptyInputRejected(t *testing.T) {
	if _, _, err := Decode([]byte("")); err == nil {
		t.Errorf("expected rejection for empty manifest")
	}
}

// TestDecode_RejectsUnknownCallableMode rejects modes outside the
// schema.CallableMode vocabulary at the manifest boundary.
func TestDecode_RejectsUnknownCallableMode(t *testing.T) {
	raw := []byte(`{"schemas": [], "callables": [
		{"namespace": "x", "name": "n", "visibility": "public",
		 "mode": "telepathy",
		 "reqSchemaId": 1, "finalSchemaId": 2,
		 "req":   {"kind": "void"},
		 "final": {"kind": "void"}}
	]}`)
	if _, _, err := Decode(raw); err == nil {
		t.Errorf("expected rejection for unknown mode")
	}
}

// TestDecode_PreservesCallableDescription verifies the human-readable
// `description` from a hand-written manifest survives the Decode path
// into NamedCallableDesc, where renderers consume it for JSDoc.
func TestDecode_PreservesCallableDescription(t *testing.T) {
	raw := []byte(`{
		"schemas": [
			{"namespace": "auth", "schemaId": 1, "name": "LoginReq",
			 "visibility": "public",
			 "object": {"kind": "struct", "name": "LoginReq", "fields": []}},
			{"namespace": "auth", "schemaId": 2, "name": "LoginFinal",
			 "visibility": "public",
			 "object": {"kind": "struct", "name": "LoginFinal", "fields": []}}
		],
		"callables": [
			{"namespace": "auth", "name": "login",
			 "description": "exchange credentials for a session token",
			 "visibility": "public", "mode": "unary",
			 "reqSchemaId": 1, "finalSchemaId": 2,
			 "req":   {"kind": "struct", "name": "LoginReq",   "className": "LoginReq"},
			 "final": {"kind": "struct", "name": "LoginFinal", "className": "LoginFinal"}}
		]
	}`)

	_, callables, err := Decode(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(callables) != 1 {
		t.Fatalf("expected 1 callable, got %d", len(callables))
	}
	if got, want := callables[0].Description, "exchange credentials for a session token"; got != want {
		t.Errorf("Description = %q, want %q", got, want)
	}
}

// TestParseVisibilities_RoundtripsAllValues covers the CSV → []ts.Visibility
// mapping. Empty CSV is rejected; unknown tokens are rejected.
func TestParseVisibilities_RoundtripsAllValues(t *testing.T) {
	cases := map[string][]ts.Visibility{
		"public":                       {ts.VisibilityPublic},
		"public,admin":                 {ts.VisibilityPublic, ts.VisibilityAdmin},
		" public , admin , internal ":  {ts.VisibilityPublic, ts.VisibilityAdmin, ts.VisibilityInternal},
		"public,admin,diagnostic":      {ts.VisibilityPublic, ts.VisibilityAdmin, ts.VisibilityDiagnostic},
	}
	for csv, want := range cases {
		got, err := ParseVisibilities(csv)
		if err != nil {
			t.Errorf("ParseVisibilities(%q) error: %v", csv, err)
			continue
		}
		if len(got) != len(want) {
			t.Errorf("ParseVisibilities(%q) len = %d, want %d", csv, len(got), len(want))
			continue
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("ParseVisibilities(%q)[%d] = %v, want %v", csv, i, got[i], want[i])
			}
		}
	}
	if _, err := ParseVisibilities(""); err == nil {
		t.Errorf("expected ParseVisibilities(\"\") to reject empty CSV")
	}
	if _, err := ParseVisibilities("nonsense"); err == nil {
		t.Errorf("expected ParseVisibilities(nonsense) to reject unknown")
	}
}
