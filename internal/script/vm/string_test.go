package vm

import (
	"strings"
	"testing"
)

func TestStringPoolSmallStringRoundTrip(t *testing.T) {
	v := newVM(4096, 256)

	cases := []string{"", "a", "ab", "abc", "abcd", "abcde", "abcdef"}
	for _, s := range cases {
		encoded := v.encodeString(s)
		decoded := v.decodeString(encoded)
		if decoded != s {
			t.Errorf("small string round-trip: %q → %q", s, decoded)
		}
	}
}

func TestStringPoolMediumStringRoundTrip(t *testing.T) {
	v := newVM(4096, 256)

	// Medium strings: 7–256 bytes.
	s7 := "abcdefg" // exactly 7 bytes
	s100 := strings.Repeat("x", 100)
	s256 := strings.Repeat("y", 256)

	for _, s := range []string{s7, s100, s256} {
		encoded := v.encodeString(s)
		decoded := v.decodeString(encoded)
		if decoded != s {
			t.Errorf("medium string round-trip failed: len=%d", len(s))
		}
	}
}

func TestStringPoolLargeStringRoundTrip(t *testing.T) {
	v := newVM(16384, 256)

	// Large string: >256 bytes.
	s := strings.Repeat("Z", 300)
	encoded := v.encodeString(s)
	decoded := v.decodeString(encoded)
	if decoded != s {
		t.Errorf("large string round-trip failed: len=%d", len(s))
	}
}

func TestStringPoolLargeStringOddLength(t *testing.T) {
	v := newVM(16384, 256)

	// 257 bytes — odd length to test byte packing edge.
	s := strings.Repeat("A", 257)
	encoded := v.encodeString(s)
	decoded := v.decodeString(encoded)
	if decoded != s {
		t.Errorf("large string odd-length round-trip failed")
	}
}

func TestStringPoolInterningDedup(t *testing.T) {
	v := newVM(4096, 256)

	// Intern the same string twice.
	s1 := v.encodeString("duplicate")
	s2 := v.encodeString("duplicate")

	// Medium strings should be interned (same value encoding).
	if s1 != s2 {
		t.Error("identical medium strings should produce the same encoded value (interned)")
	}
}

func TestStringPoolMediumStringNotInternedForDifferent(t *testing.T) {
	v := newVM(4096, 256)

	s1 := v.encodeString("hello world")
	s2 := v.encodeString("hello earth")

	if s1 == s2 {
		t.Error("different strings should produce different encoded values")
	}
}

func TestStringConcatAllTypes(t *testing.T) {
	v := newVM(4096, 256)

	// String + String
	result := v.concatStrings(v.encodeString("hello "), v.encodeString("world"))
	if v.decodeString(result) != "hello world" {
		t.Errorf("string+string concat = %q", v.decodeString(result))
	}

	// String + Int
	result = v.concatStrings(v.encodeString("n="), encodeInt(42))
	if v.decodeString(result) != "n=42" {
		t.Errorf("string+int concat = %q", v.decodeString(result))
	}

	// Int + String
	result = v.concatStrings(encodeInt(10), v.encodeString(" items"))
	if v.decodeString(result) != "10 items" {
		t.Errorf("int+string concat = %q", v.decodeString(result))
	}

	// Bool + String
	result = v.concatStrings(encodeBool(true), v.encodeString(" flag"))
	if v.decodeString(result) != "true flag" {
		t.Errorf("bool+string concat = %q", v.decodeString(result))
	}

	// Float + String
	result = v.concatStrings(encodeFloat(3.14), v.encodeString(" pi"))
	decoded := v.decodeString(result)
	if !strings.HasPrefix(decoded, "3.14") {
		t.Errorf("float+string concat = %q, want prefix 3.14", decoded)
	}
}

func TestStringConcatEmptyStrings(t *testing.T) {
	v := newVM(4096, 256)

	result := v.concatStrings(v.encodeString(""), v.encodeString(""))
	if v.decodeString(result) != "" {
		t.Errorf("empty+empty concat = %q, want empty", v.decodeString(result))
	}

	result = v.concatStrings(v.encodeString("abc"), v.encodeString(""))
	if v.decodeString(result) != "abc" {
		t.Errorf("abc+empty concat = %q, want abc", v.decodeString(result))
	}
}

func TestStringConcatLongValues(t *testing.T) {
	v := newVM(4096, 256)

	// Long value that requires heap allocation.
	lv := v.encodeLong(maxInlineLong + 1)
	result := v.concatStrings(lv, v.encodeString(" big"))
	decoded := v.decodeString(result)
	if !strings.Contains(decoded, "big") {
		t.Errorf("long+string concat = %q, should contain 'big'", decoded)
	}
}

func TestDecodeStringEmptyForNonString(t *testing.T) {
	v := newVM(4096, 256)

	// Decoding a non-string value should return empty string.
	result := v.decodeString(encodeBool(true))
	if result != "" {
		t.Errorf("decodeString(bool) = %q, want empty", result)
	}
}

func TestJoinStringArray(t *testing.T) {
	tests := []struct {
		name  string
		parts []string
		sep   string
		want  string
	}{
		{name: "empty separator", parts: []string{"a", "b", "c"}, sep: "", want: "abc"},
		{name: "non-empty separator", parts: []string{"a", "b", "c"}, sep: "-", want: "a-b-c"},
		{name: "empty array", parts: nil, sep: ",", want: ""},
		{name: "single element", parts: []string{"abc"}, sep: ",", want: "abc"},
		{name: "large result", parts: []string{strings.Repeat("x", 100), strings.Repeat("y", 100), strings.Repeat("z", 100)}, sep: ":", want: strings.Repeat("x", 100) + ":" + strings.Repeat("y", 100) + ":" + strings.Repeat("z", 100)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := newVM(4096, 256)
			arr := v.newArray(typeString, 0)
			for _, part := range tt.parts {
				v.arrayPush(arr, v.encodeString(part))
			}
			got, ok := v.joinStringArray(arr, v.encodeString(tt.sep))
			if !ok {
				t.Fatal("joinStringArray returned ok=false")
			}
			if v.decodeString(got) != tt.want {
				t.Fatalf("joinStringArray = %q, want %q", v.decodeString(got), tt.want)
			}
		})
	}
}

func TestJoinStringArrayFallbackShapes(t *testing.T) {
	v := newVM(4096, 256)
	arr := v.newArray(typeAny, 0)
	v.arrayPush(arr, v.encodeString("a"))
	v.arrayPush(arr, encodeInt(1))

	if _, ok := v.joinStringArray(invalidHandle, v.encodeString(",")); ok {
		t.Fatal("invalid array handle should not fast-path")
	}
	if _, ok := v.joinStringArray(arr, encodeInt(1)); ok {
		t.Fatal("non-string separator should not fast-path")
	}
	if _, ok := v.joinStringArray(arr, v.encodeString(",")); ok {
		t.Fatal("non-string element should not fast-path")
	}
}

func TestSmallStringMaxLength(t *testing.T) {
	// Exactly 6 bytes is the max small string.
	v := encodeSmallString([]byte("123456"))
	if !v.isSmallString() {
		t.Error("6-byte string should be small string")
	}
	if v.smallStringLength() != 6 {
		t.Errorf("smallStringLength = %d, want 6", v.smallStringLength())
	}
}

func TestSmallStringWithNullBytes(t *testing.T) {
	data := []byte{0, 1, 2, 3, 4, 5}
	v := encodeSmallString(data)
	if !v.isSmallString() {
		t.Error("small string with null bytes should be small string")
	}
	decoded := v.decodeSmallString()
	for i, b := range data {
		if decoded[i] != b {
			t.Errorf("byte %d: got %d, want %d", i, decoded[i], b)
		}
	}
}

// stringByteAt powers for-in over strings in the bytecode layer. It must
// work across all three string encodings (small, medium, large).
func TestStringByteAtAllEncodings(t *testing.T) {
	v := newVM(1<<20, 256)

	cases := []struct {
		name string
		s    string
	}{
		{"small", "abcdef"},                  // inline small string
		{"medium", "the quick brown fox"},    // medium string
		{"large", strings.Repeat("ab", 100)}, // large string
	}
	for _, tc := range cases {
		val := v.encodeString(tc.s)
		length, ok := v.stringLength(val)
		if !ok || length != len(tc.s) {
			t.Fatalf("%s: stringLength = %d,%v; want %d,true", tc.name, length, ok, len(tc.s))
		}
		for i := 0; i < len(tc.s); i++ {
			bv, ok := v.stringByteAt(val, i)
			if !ok {
				t.Fatalf("%s: stringByteAt(%d) returned !ok", tc.name, i)
			}
			if got := v.decodeString(bv); got != tc.s[i:i+1] {
				t.Errorf("%s: stringByteAt(%d) = %q, want %q", tc.name, i, got, tc.s[i:i+1])
			}
		}
		if _, ok := v.stringByteAt(val, len(tc.s)); ok {
			t.Errorf("%s: stringByteAt(len) should return !ok", tc.name)
		}
		if _, ok := v.stringByteAt(val, -1); ok {
			t.Errorf("%s: stringByteAt(-1) should return !ok", tc.name)
		}
	}
	if _, ok := v.stringByteAt(encodeInt(1), 0); ok {
		t.Error("stringByteAt on int should return !ok")
	}
}
