package binding

import (
	"testing"

	"github.com/qomos-w/spore/identity"
	"github.com/qomos-w/spore/schema"
)

func tagTestID(t *testing.T, seq uint64) identity.CanonicalID {
	t.Helper()
	id, err := identity.NewCanonicalID(2000, 1, 0, seq)
	if err != nil {
		t.Fatalf("NewCanonicalID: %v", err)
	}
	return id
}

// View→apply round-trips must survive json-tagged hosts: projection and
// patch resolution follow encoding/json name semantics (exact Go field
// name first, json tag second), so script-side keys emitted as lowerCamel
// tags reach the right Go fields.

type jsonTaggedHost struct {
	BuffId int     `json:"buffId"`
	Name   string  `json:"name"`
	Speed  float64 `json:"speed"`
}

func jsonTaggedHostDesc() schema.ObjectDesc {
	return schema.ObjectDesc{
		Name: "jsonTaggedHost",
		Fields: []schema.FieldDesc{
			{Name: "BuffId", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}},
			{Name: "Name", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
			{Name: "Speed", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "float64"}},
		},
	}
}

func TestApplyViewPatch_AcceptsJsonTagKeys(t *testing.T) {
	id := tagTestID(t, 901)
	obj := jsonTaggedHost{BuffId: 1, Name: "old", Speed: 1.5}

	b, err := NewObjectBinding(jsonTaggedHostDesc(), id, &obj)
	if err != nil {
		t.Fatalf("NewObjectBinding: %v", err)
	}

	muts, err := ApplyViewPatch(b, &ViewProjection{
		Schema:   jsonTaggedHostDesc(),
		Identity: id,
		Fields: map[string]any{
			"buffId": 7,
			"name":   "renamed",
			"speed":  9.25,
		},
	})
	if err != nil {
		t.Fatalf("ApplyViewPatch: %v", err)
	}
	if len(muts) != 3 {
		t.Fatalf("expected 3 mutations, got %d: %+v", len(muts), muts)
	}
	if obj.BuffId != 7 || obj.Name != "renamed" || obj.Speed != 9.25 {
		t.Fatalf("tag-keyed patch did not land: %+v", obj)
	}
}

func TestApplyViewPatch_GoNameKeysStillApplyUntaggedAndTagged(t *testing.T) {
	id := tagTestID(t, 902)
	obj := jsonTaggedHost{BuffId: 2, Name: "keep", Speed: 2.5}

	b, err := NewObjectBinding(jsonTaggedHostDesc(), id, &obj)
	if err != nil {
		t.Fatalf("NewObjectBinding: %v", err)
	}

	muts, err := ApplyViewPatch(b, &ViewProjection{
		Schema:   jsonTaggedHostDesc(),
		Identity: id,
		Fields: map[string]any{
			"BuffId": 42,
			"Name":   "go-name",
		},
	})
	if err != nil {
		t.Fatalf("ApplyViewPatch: %v", err)
	}
	if len(muts) != 2 {
		t.Fatalf("expected 2 mutations, got %d", len(muts))
	}
	if obj.BuffId != 42 || obj.Name != "go-name" || obj.Speed != 2.5 {
		t.Fatalf("go-name patch did not land: %+v", obj)
	}

	// Untagged host: exact Go names, no tag indirection.
	type plainHost struct {
		Level int
	}
	plainDesc := schema.ObjectDesc{
		Name: "plainHost",
		Fields: []schema.FieldDesc{
			{Name: "Level", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}},
		},
	}
	plain := plainHost{Level: 1}
	pb, err := NewObjectBinding(plainDesc, id, &plain)
	if err != nil {
		t.Fatalf("NewObjectBinding(plain): %v", err)
	}
	if _, err := ApplyViewPatch(pb, &ViewProjection{
		Schema:   plainDesc,
		Identity: id,
		Fields:   map[string]any{"Level": 99},
	}); err != nil {
		t.Fatalf("ApplyViewPatch(plain): %v", err)
	}
	if plain.Level != 99 {
		t.Fatalf("plain patch did not land: %+v", plain)
	}
}

func TestApplyViewPatch_UnknownKeysSkippedSilently(t *testing.T) {
	id := tagTestID(t, 903)
	obj := jsonTaggedHost{BuffId: 3, Name: "x", Speed: 0.5}

	b, err := NewObjectBinding(jsonTaggedHostDesc(), id, &obj)
	if err != nil {
		t.Fatalf("NewObjectBinding: %v", err)
	}
	muts, err := ApplyViewPatch(b, &ViewProjection{
		Schema:   jsonTaggedHostDesc(),
		Identity: id,
		Fields: map[string]any{
			"unknownField": 1, // neither schema name nor tag → skipped
			"buffId":       8,
		},
	})
	if err != nil {
		t.Fatalf("ApplyViewPatch: %v", err)
	}
	if len(muts) != 1 || obj.BuffId != 8 {
		t.Fatalf("expected only buffId mutation, got %+v obj=%+v", muts, obj)
	}
}
