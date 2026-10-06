package std_test

import (
	"testing"

	"github.com/qomos-w/spore/script"
	"github.com/qomos-w/spore/std"
)

func TestStdModuleScriptSurface(t *testing.T) {
	sb := std.NewScriptBindingWithStd()
	rt, err := script.NewRuntimeWith(script.RuntimeOptions{ScriptBinding: sb})
	if err != nil {
		t.Fatalf("NewRuntimeWith: %v", err)
	}
	err = rt.LoadSource("stdev", `
import { join, split, contains, has_prefix, has_suffix, trim, to_upper, to_lower, parse_int, parse_float, err_text, now_ms } from "std"

fun joinit(): string {
	return join(["a", "b", "c"], "-")
}
fun splitit(): string {
	var parts: array<string> = split("a,b,c", ",")
	return parts[1]
}
fun hasit(): bool {
	return contains("hello", "ell") && has_prefix("hello", "he") && has_suffix("hello", "lo")
}
fun trimit(): string {
	return to_upper(trim("  pad  ")) + "|" + to_lower("UP")
}
fun parseit(): int {
	return parse_int("42")
}
fun parsefit(): double {
	return parse_float("2.5")
}
fun errtext(): string {
	var out: string = "none"
	try {
		var m: map<string, int> = {"a": 1}
		var x: int = m["zzz"]
		out = "got"
	} catch (e) {
		out = err_text(e)
	}
	return out
}
fun nowms(): long {
	var n: long = now_ms() / 1000
	return n
}
`)
	if err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	cases := []struct {
		fn   string
		want string
	}{
		{"joinit", "a-b-c"},
		{"splitit", "b"},
		{"hasit", "true"},
		{"trimit", "PAD|up"},
		{"parseit", "42"},
		{"parsefit", "2.5"},
		{"errtext", "map key not found: \"zzz\""},
	}
	for _, tc := range cases {
		res, err := rt.Call(tc.fn)
		if err != nil {
			t.Fatalf("Call %s: %v", tc.fn, err)
		}
		if res.Error != nil {
			t.Fatalf("%s script error: %v", tc.fn, res.Error)
		}
		if got := fmtValue(res.Value); got != tc.want {
			t.Errorf("%s = %q, want %q", tc.fn, got, tc.want)
		}
	}
	res, err := rt.Call("nowms")
	if err != nil || res.Error != nil {
		t.Fatalf("nowms: err=%v res.Error=%v", err, res.Error)
	}
}

func fmtValue(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case int:
		return itoa(int64(x))
	case int64:
		return itoa(x)
	case float64:
		return ftoa(x)
	default:
		return ""
	}
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func ftoa(f float64) string {
	return itoa(int64(f)) + "." + itoa(int64((f-float64(int64(f)))*10))
}