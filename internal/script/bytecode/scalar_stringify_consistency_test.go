package bytecode

import (
	"testing"
)

// TestScalarStringifyConsistency pins the invariant that the two paths a
// scalar can become text — implicit coercion in string concatenation
// (`"|" + x`) and explicit conversion (`x as string`) — render identically.
// LLM-authored scripts freely mix both forms; divergence here surfaces as
// a confusing format mismatch (e.g. "5.0" from one path, "5" from the
// other). The implicit path lives in vm.concatStrings (%g for floats);
// the explicit path in convertScalarAs (%v for floats) — identical verbs
// today, and this test holds them together.
func TestScalarStringifyConsistency(t *testing.T) {
	cases := []struct {
		name string
		lit  string
		want string // agreed rendering (shortest repr: 5.0 -> "5")
	}{
		{"int", "7", "|7"},
		{"int-neg", "-7", "|-7"},
		{"float-whole", "5.0", "|5"},
		{"float-frac", "2.5", "|2.5"},
		{"float-tiny", "0.001", "|0.001"},
		{"float-big", "123456.75", "|123456.75"},
		{"bool-true", "true", "|true"},
		{"bool-false", "false", "|false"},
	}
	for _, tc := range cases {
		src := `
fun concat(x: float): string { return "|" + x }
fun asstr(x: float): string { return "|" + (x as string) }
fun main(): string { return concat(LIT) + ";" + asstr(LIT) }
`
		src = replaceFirst(src, "LIT", tc.lit)
		src = replaceFirst(src, "LIT", tc.lit)
		result, v, err := compileAndCallWithVM(t, src, "main", nil)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		s := v.DecodeString(result)
		sep := -1
		for i := 0; i < len(s); i++ {
			if s[i] == ';' {
				sep = i
				break
			}
		}
		if sep < 0 {
			t.Fatalf("%s: malformed output %q", tc.name, s)
		}
		implicit, explicit := s[:sep], s[sep+1:]
		if implicit != explicit {
			t.Errorf("%s: implicit coercion %q != explicit `as string` %q", tc.name, implicit, explicit)
		}
		if implicit != tc.want {
			t.Errorf("%s: rendering drifted: got %q, want %q", tc.name, implicit, tc.want)
		}
	}
}

// replaceFirst replaces the first occurrence of old with new in s.
func replaceFirst(s, old, new string) string {
	for i := 0; i+len(old) <= len(s); i++ {
		if s[i:i+len(old)] == old {
			return s[:i] + new + s[i+len(old):]
		}
	}
	return s
}