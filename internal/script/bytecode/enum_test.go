package bytecode

import (
	"testing"

	"github.com/qomos-w/spore/internal/script/frontend"
	"github.com/qomos-w/spore/internal/script/vm"
)

func TestEnum_AutoIncrementValues(t *testing.T) {
	source := `
enum Color { Red, Green, Blue }

fun value(): int {
    return (Color.Red as int) + (Color.Green as int) * 10 + (Color.Blue as int) * 100
}`
	result, err := compileAndCall(t, source, "value", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 0+1*10+2*100 {
		t.Fatalf("expected 210, got %d", got)
	}
}

func TestEnum_ExplicitValuesAndResumeAutoIncrement(t *testing.T) {
	source := `
enum Status { Pending = 10, Active, Closed = 40, Done }

fun value(): int {
    return (Status.Pending as int) + (Status.Active as int) * 100 + (Status.Closed as int) * 10000 + (Status.Done as int) * 1000000
}`
	result, err := compileAndCall(t, source, "value", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.DecodeInt(result); got != 10+11*100+40*10000+41*1000000 {
		t.Fatalf("expected 4104011, got %d", got)
	}
}

func TestEnum_EqualityAndComparison(t *testing.T) {
	source := `
enum Color { Red, Green, Blue }

fun check(): bool {
    return Color.Red == Color.Red && Color.Red != Color.Blue && (Color.Green as int) < (Color.Blue as int)
}`
	result, err := compileAndCall(t, source, "check", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !vm.DecodeBool(result) {
		t.Fatalf("expected true, got %v", result)
	}
}

func TestEnum_IsTypeCheck(t *testing.T) {
	source := `
enum Color { Red, Green, Blue }
enum Other { Red, Green, Blue }

fun check(): bool {
    return (Color.Red is Color) && !(Color.Red is Other) && !(5 is Color) && !("x" is Color)
}`
	result, err := compileAndCall(t, source, "check", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !vm.DecodeBool(result) {
		t.Fatalf("expected true, got %v", result)
	}
}

func TestEnum_AsConversion(t *testing.T) {
	source := `
enum Status { Pending = 0, Active = 1, Closed = 2 }

fun check(): bool {
    return ((1 as Status) is Status) && ((1 as Status) == Status.Active) && ((Status.Closed as int) == 2)
}`
	result, err := compileAndCall(t, source, "check", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !vm.DecodeBool(result) {
		t.Fatalf("expected true, got %v", result)
	}
}

func TestEnum_AsInvalidMemberFails(t *testing.T) {
	source := `
enum Status { Pending = 0, Active = 1 }

fun check(): bool {
    return (99 as Status) is Status
}`
	_, err := compileAndCall(t, source, "check", nil)
	if err == nil {
		t.Fatal("expected error for 99 as Status (value not in closed set)")
	}
}

func TestEnum_UnknownMemberIsCompileError(t *testing.T) {
	prog, err := frontend.ParseModuleForTest(`
enum Color { Red, Green }

fun value(): int {
    return Color.Purple as int
}`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	c := newCompiler()
	if _, err := c.compile(prog); err == nil {
		t.Fatal("expected compile error for unknown enum member")
	}
}

func TestEnum_StructField(t *testing.T) {
	source := `
enum Color { Red, Green, Blue }

struct Pixel {
  x: int
  y: int
  color: Color
}

fun check(): bool {
    var p: Pixel = Pixel{ x: 1, y: 2, color: Color.Blue }
    return p.color == Color.Blue && p.color is Color
}`
	result, err := compileAndCall(t, source, "check", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !vm.DecodeBool(result) {
		t.Fatalf("expected true, got %v", result)
	}
}

func TestEnum_WhenMatch(t *testing.T) {
	source := `
enum Status { Pending, Active, Closed }

fun label(s: Status): string {
    when (s) {
        case Status.Pending { return "pending" }
        case Status.Active { return "active" }
        case Status.Closed { return "closed" }
    }
    return "unknown"
}

fun check(): bool {
    return label(Status.Pending) == "pending" && label(Status.Active) == "active" && label(Status.Closed) == "closed"
}`
	_, vmInstance, err := compileAndCallWithVM(t, source, "check", nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := compileAndCall(t, source, "check", nil)
	if err != nil {
		t.Fatal(err)
	}
	if vmInstance == nil {
		t.Fatal("expected VM instance")
	}
	if !vm.DecodeBool(result) {
		t.Fatalf("expected true, got %v", result)
	}
}

func TestEnum_ParameterAndReturn(t *testing.T) {
	source := `
enum Level { Low, Mid, High }

fun bump(l: Level): Level {
    if l == Level.Low {
        return Level.Mid
    }
    return Level.High
}

fun check(): bool {
    return bump(Level.Low) == Level.Mid && bump(Level.Mid) == Level.High && bump(Level.High) == Level.High
}`
	result, err := compileAndCall(t, source, "check", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !vm.DecodeBool(result) {
		t.Fatalf("expected true, got %v", result)
	}
}

func TestEnum_EnumValueStringifiesAsInt(t *testing.T) {
	source := `
enum Status { Pending = 7 }

fun check(): bool {
    var s: string = "v" + (Status.Pending as int)
    return s == "v7"
}`
	result, err := compileAndCall(t, source, "check", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !vm.DecodeBool(result) {
		t.Fatalf("expected true (\"v7\"), got %v", result)
	}
}
