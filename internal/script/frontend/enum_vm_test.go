package frontend_test

import (
	"testing"

	"github.com/qomos-w/spore/internal/script/frontend"
)

func TestVMFrontend_EnumLocalExecution(t *testing.T) {
	f := newVMFrontend(t)
	if err := f.LoadSource(`
enum Color { Red, Green, Blue }
enum Status { Pending = 10, Active, Closed = 40 }

fun redIsColor(): bool { return (Color.Red is Color) && !(Color.Red is Status) }
fun greenValue(): int { return Color.Green as int }
fun statusSum(): int { return (Status.Pending as int) + (Status.Active as int) + (Status.Closed as int) }
fun castRoundTrip(): bool { var s: Status = 11 as Status return s == Status.Active }
fun badCastFails(): bool { return (99 as Status) is Status }
fun crossEnumNotEqual(): bool { return !(enum2_helper() == Status.Active) }

fun enum2_helper(): Color { return Color.Green }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	cases := []struct {
		callable string
		want     any
	}{
		{"redIsColor", true},
		{"greenValue", 1},
		{"statusSum", 10 + 11 + 40},
		{"castRoundTrip", true},
		{"crossEnumNotEqual", true},
	}
	for _, tc := range cases {
		outcome, err := f.Invoke(tc.callable, nil)
		if err != nil {
			t.Fatalf("Invoke %s: %v", tc.callable, err)
		}
		if outcome.Payload == nil || outcome.Payload.Value != tc.want {
			t.Fatalf("%s: expected %v, got %+v", tc.callable, tc.want, outcome.Payload)
		}
	}

	badOutcome, badErr := f.Invoke("badCastFails", nil)
	if badErr != nil {
		t.Fatalf("Invoke badCastFails: %v", badErr)
	}
	if badOutcome.Result.Error == nil || badOutcome.Result.Error.DiagnosticCode != "type_cast_failed" {
		t.Fatalf("expected type_cast_failed envelope for 99 as Status, got %+v", badOutcome.Result.Error)
	}
}

func TestVMFrontend_EnumImportedFromModule(t *testing.T) {
	f := newVMFrontendWithResolver(t, frontend.MapModuleResolver{
		"colors": `export enum Color { Red = 1, Green, Blue }

export fun redValue(): int { return Color.Red as int }`,
	})
	if err := f.LoadSource(`
import Color from "colors"
import redValue from "colors"

fun useImported(): bool { return (Color.Blue is Color) && (Color.Blue as int) == 3 }
fun callModuleFun(): int { return redValue() }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	outcome, err := f.Invoke("useImported", nil)
	if err != nil {
		t.Fatalf("Invoke useImported: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != true {
		t.Fatalf("expected true, got %+v", outcome.Payload)
	}
	outcome, err = f.Invoke("callModuleFun", nil)
	if err != nil {
		t.Fatalf("Invoke callModuleFun: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != 1 {
		t.Fatalf("expected 1, got %+v", outcome.Payload)
	}
}

func TestVMFrontend_EnumStructFieldAndWhen(t *testing.T) {
	f := newVMFrontend(t)
	if err := f.LoadSource(`
enum Level { Low, Mid, High }

struct Task {
  name: string
  level: Level
}

fun label(level: Level): string {
  when (level) {
    case Level.Low { return "low" }
    case Level.Mid { return "mid" }
    case Level.High { return "high" }
  }
  return "?"
}

fun run(): bool {
  var t: Task = Task{ name: "sync", level: Level.High }
  return t.level is Level && label(t.level) == "high" && label(Level.Low) == "low"
}`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	outcome, err := f.Invoke("run", nil)
	if err != nil {
		t.Fatalf("Invoke run: %v", err)
	}
	if outcome.Payload == nil || outcome.Payload.Value != true {
		t.Fatalf("expected true, got %+v", outcome.Payload)
	}
}
