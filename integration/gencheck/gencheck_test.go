package gencheck

import (
	"errors"
	"strings"
	"testing"

	"github.com/qomos-w/spore/runtime"
	"github.com/qomos-w/spore/schema"
)

// Compile-time proof: the codegen Registry object must satisfy the runtime
// SchemaTable interface structurally — otherwise this assignment fails.
var _ runtime.SchemaTable = Registry

func TestAddRegistryEndToEnd(t *testing.T) {
	w := runtime.NewWorld()
	w.AddRegistry(Registry)

	e := w.Create()
	if err := w.SetT(e, &Position{X: 1}); err != nil {
		t.Fatalf("SetT: %v", err)
	}
	if p, ok := w.GetT[Position](e); !ok || p.X != 1 {
		t.Fatalf("GetT = %+v %v", p, ok)
	}
	if _, ok := w.LookupComponent[Velocity](); !ok {
		t.Fatal("LookupComponent[Velocity] must resolve")
	}
	// PlainMsg is a schema but not @component — must not resolve.
	if _, ok := w.LookupComponent[PlainMsg](); ok {
		t.Fatal("LookupComponent[PlainMsg] must NOT resolve (not a component)")
	}
}

// TestMediaContract proves the generated media carrier and its validation
// compile and enforce the {mime, src} contract end-to-end.
func TestMediaContract(t *testing.T) {
	ok := Media{Mime: "image/png", Src: "https://cdn.example.com/a.png"}
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid media rejected: %v", err)
	}

	oversized := Media{Mime: "image/png", Src: "data:image/png;base64," + strings.Repeat("A", schema.MediaInlineLimit+1)}
	err := oversized.Validate()
	if err == nil {
		t.Fatal("expected oversized inline media to be rejected")
	}
	var tooLarge *schema.MediaInlineTooLargeError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("expected *schema.MediaInlineTooLargeError, got %T: %v", err, err)
	}

	avatar := Avatar{Photo: ok, Gallery: []Media{ok}}
	if err := avatar.Photo.Validate(); err != nil {
		t.Fatalf("avatar photo rejected: %v", err)
	}
	if len(avatar.Gallery) != 1 {
		t.Fatalf("gallery = %v, want 1 entry", avatar.Gallery)
	}
}
