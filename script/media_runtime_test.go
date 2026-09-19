package script_test

import (
	"testing"

	"github.com/qomos-w/spore/schema"
	"github.com/qomos-w/spore/script"
)

func TestRuntimeNullReturnForReferenceResult(t *testing.T) {
	rt, err := script.NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}

	src := `
export fun thinkNull(snap: map<string, any>): map<string, any> {
    var x: int = snap["mode"] as int
    if x == 0 {
        return null
    }
    return {"job": x}
}
`
	if err := rt.LoadSource("demo", src); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	r, err := rt.Call("thinkNull", map[string]any{"mode": 0})
	if err != nil {
		t.Fatalf("Call thinkNull(null path): %v", err)
	}
	if r.Error != nil {
		t.Fatalf("null return surfaced as runtime error: %v", r.Error)
	}
	if r.Value != nil {
		t.Fatalf("expected nil result value for null return, got %#v", r.Value)
	}

	r, err = rt.Call("thinkNull", map[string]any{"mode": 17})
	if err != nil {
		t.Fatalf("Call thinkNull(map path): %v", err)
	}
	m, ok := r.Value.(map[string]any)
	if !ok || m["job"] != 17 {
		t.Fatalf("expected {job: 17}, got %#v", r.Value)
	}
}

func TestRuntimeMediaParamAndResult(t *testing.T) {
	rt, err := script.NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}

	src := `
struct Avatar {
  photo: media
  gallery: array<media>
}

export fun echo(m: media): media { return m }
`
	if err := rt.LoadSource("demo", src); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	in := map[string]any{"mime": "image/png", "src": "https://cdn.example.com/a.png"}
	result, err := rt.Call("echo", in)
	if err != nil {
		t.Fatalf("Call echo: %v", err)
	}
	m, ok := result.Value.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any result, got %T: %#v", result.Value, result.Value)
	}
	if m["mime"] != in["mime"] || m["src"] != in["src"] {
		t.Fatalf("round trip mismatch: %v", m)
	}
}

func TestRuntimeMediaRejectsBadShape(t *testing.T) {
	rt, err := script.NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}

	if err := rt.LoadSource("demo", `export fun echo(m: media): media { return m }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	if _, err := rt.Call("echo", map[string]any{"mime": "image/png", "src": "http://insecure.example.com/a.png"}); err == nil {
		t.Fatal("expected rejection of non-whitelisted src scheme")
	}
}

func TestRuntimeMediaFromHostFunction(t *testing.T) {
	rt, err := script.NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.BindFunc("host", "photo", func() schema.Media {
		return schema.Media{Mime: "image/png", Src: "data:image/png;base64,iVBORw0KGgo="}
	}); err != nil {
		t.Fatalf("BindFunc: %v", err)
	}

	src := `
import { photo } from "host"

export fun host_photo(): media { return photo() }
`
	if err := rt.LoadSource("demo", src); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	result, err := rt.Call("host_photo")
	if err != nil {
		t.Fatalf("Call photo: %v", err)
	}
	m, ok := result.Value.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any result, got %T: %#v", result.Value, result.Value)
	}
	if m["mime"] != "image/png" || m["src"] != "data:image/png;base64,iVBORw0KGgo=" {
		t.Fatalf("host media mismatch: %v", m)
	}
}

func TestRuntimeMediaFieldAccessInScript(t *testing.T) {
	rt, err := script.NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.BindFunc("host", "photo", func() schema.Media {
		return schema.Media{Mime: "image/png", Src: "https://cdn.example.com/a.png"}
	}); err != nil {
		t.Fatalf("BindFunc: %v", err)
	}
	if err := rt.LoadSource("demo", `
import { photo } from "host"

export fun probe(): string {
  var m: media = photo()
  return m.mime
}
`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	r, err := rt.Call("probe")
	if err != nil {
		t.Fatalf("Call probe: %v", err)
	}
	if r.Value != "image/png" {
		t.Fatalf("probe = %#v, want image/png", r.Value)
	}
}

func TestRuntimeMediaPassedFromScriptToHost(t *testing.T) {
	rt, err := script.NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.BindFunc("host", "save", func(m schema.Media) string { return m.Mime }); err != nil {
		t.Fatalf("BindFunc: %v", err)
	}
	if err := rt.LoadSource("demo", `
import { save } from "host"

export fun store(m: media): string { return save(m) }
`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	r, err := rt.Call("store", map[string]any{"mime": "image/png", "src": "data:image/png;base64,iVBORw0KGgo="})
	if err != nil {
		t.Fatalf("Call store: %v", err)
	}
	if r.Value != "image/png" {
		t.Fatalf("store = %#v, want image/png", r.Value)
	}
}
