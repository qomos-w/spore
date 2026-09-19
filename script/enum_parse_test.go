package script_test

import (
	"testing"

	"github.com/qomos-w/spore/script"
)

func TestRuntimeEnumNegativeValuesThroughVM(t *testing.T) {
	rt, err := script.NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	src := `
enum Zoom { None = -1, Far, Near }

export fun noneValue(): int { return Zoom.None as int }
export fun farValue(): int { return Zoom.Far as int }
export fun asEnumRoundTrip(): bool { return ((-1 as Zoom) is Zoom) && ((-1 as Zoom) == Zoom.None) }
`
	if err := rt.LoadSource("demo", src); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	r, err := rt.Call("noneValue")
	if err != nil {
		t.Fatalf("Call noneValue: %v", err)
	}
	if r.Value != int32(-1) && r.Value != -1 {
		t.Fatalf("noneValue = %#v, want -1", r.Value)
	}

	r, err = rt.Call("farValue")
	if err != nil {
		t.Fatalf("Call farValue: %v", err)
	}
	if r.Value != int32(0) && r.Value != 0 {
		t.Fatalf("farValue = %#v, want 0 (auto-increment from -1)", r.Value)
	}

	r, err = rt.Call("asEnumRoundTrip")
	if err != nil {
		t.Fatalf("Call asEnumRoundTrip: %v", err)
	}
	if r.Value != true {
		t.Fatalf("asEnumRoundTrip = %#v, want true", r.Value)
	}
}

func TestParseEnums(t *testing.T) {
	source := `
export enum Color { Red = 1, Green, Blue }
enum Status { Pending = 10, Active, Closed = 40, Done }
fun notAnEnum(): int { return 1 }
`
	enums, err := script.ParseEnums(source)
	if err != nil {
		t.Fatalf("ParseEnums: %v", err)
	}
	if len(enums) != 2 {
		t.Fatalf("expected 2 enums, got %d", len(enums))
	}
	if enums[0].Name != "Color" || len(enums[0].Members) != 3 {
		t.Fatalf("unexpected first enum: %+v", enums[0])
	}
	wantColor := []int{1, 2, 3}
	for i, m := range enums[0].Members {
		if m.Value != wantColor[i] {
			t.Fatalf("Color member %q: expected %d, got %d", m.Name, wantColor[i], m.Value)
		}
	}
	wantStatus := []int{10, 11, 40, 41}
	for i, m := range enums[1].Members {
		if m.Value != wantStatus[i] {
			t.Fatalf("Status member %q: expected %d, got %d", m.Name, wantStatus[i], m.Value)
		}
	}
}

func TestParseEnumsNegativeValues(t *testing.T) {
	source := `enum Zoom { None = -1, Far, Near = 2, Min = -5, Below }`
	enums, err := script.ParseEnums(source)
	if err != nil {
		t.Fatalf("ParseEnums: %v", err)
	}
	if len(enums) != 1 {
		t.Fatalf("expected 1 enum, got %d", len(enums))
	}
	want := []int{-1, 0, 2, -5, -4}
	for i, m := range enums[0].Members {
		if m.Value != want[i] {
			t.Fatalf("Zoom member %q: expected %d, got %d", m.Name, want[i], m.Value)
		}
	}
}
