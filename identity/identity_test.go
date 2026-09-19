package identity_test

import (
	"testing"

	"github.com/qomos-w/spore/identity"
)

// --- constructor and segment packing ---

func TestNewCanonicalID_PacksAllSegments(t *testing.T) {
	tsMS := uint64(0x0001_0002_0003)
	slot := uint16(0xABCD)
	inc := uint16(0x0102)
	seq := uint64(0x0004_0005_0006)

	id, err := identity.NewCanonicalID(tsMS, slot, inc, seq)
	if err != nil {
		t.Fatalf("NewCanonicalID() error = %v", err)
	}
	if id.TimestampMS() != tsMS {
		t.Errorf("TimestampMS() = %d, want %d", id.TimestampMS(), tsMS)
	}
	if id.RuntimeSlot() != slot {
		t.Errorf("RuntimeSlot() = %d, want %d", id.RuntimeSlot(), slot)
	}
	if id.Incarnation() != inc {
		t.Errorf("Incarnation() = %d, want %d", id.Incarnation(), inc)
	}
	if id.Sequence() != seq {
		t.Errorf("Sequence() = %d, want %d", id.Sequence(), seq)
	}
}

func TestNewCanonicalID_SplitMatchesIndividualAccessors(t *testing.T) {
	id, _ := identity.NewCanonicalID(111, 222, 333, 444)
	ts, slot, inc, seq := id.Split()
	if ts != id.TimestampMS() || slot != id.RuntimeSlot() || inc != id.Incarnation() || seq != id.Sequence() {
		t.Errorf("Split() segments do not match individual accessors")
	}
}

// --- overflow validation ---

func TestNewCanonicalID_RejectsTimestampOverflow(t *testing.T) {
	overflow := uint64(1<<48) // one beyond the 48-bit max
	_, err := identity.NewCanonicalID(overflow, 0, 0, 0)
	if err != identity.ErrCanonicalIDTimestampOverflow {
		t.Errorf("NewCanonicalID() error = %v, want ErrCanonicalIDTimestampOverflow", err)
	}
}

func TestNewCanonicalID_AcceptsMaxTimestamp(t *testing.T) {
	max := uint64(1<<48 - 1)
	_, err := identity.NewCanonicalID(max, 0, 0, 0)
	if err != nil {
		t.Errorf("NewCanonicalID() unexpected error for max timestamp: %v", err)
	}
}

func TestNewCanonicalID_RejectsSequenceOverflow(t *testing.T) {
	overflow := uint64(1<<48) // one beyond the 48-bit max
	_, err := identity.NewCanonicalID(0, 0, 0, overflow)
	if err != identity.ErrCanonicalIDSequenceOverflow {
		t.Errorf("NewCanonicalID() error = %v, want ErrCanonicalIDSequenceOverflow", err)
	}
}

func TestNewCanonicalID_AcceptsMaxSequence(t *testing.T) {
	max := uint64(1<<48 - 1)
	_, err := identity.NewCanonicalID(0, 0, 0, max)
	if err != nil {
		t.Errorf("NewCanonicalID() unexpected error for max sequence: %v", err)
	}
}

// --- zero value ---

func TestCanonicalID_ZeroValue_IsZero(t *testing.T) {
	var id identity.CanonicalID
	if !id.IsZero() {
		t.Error("zero CanonicalID should be IsZero() == true")
	}
}

func TestCanonicalID_NonZeroValue_IsNotZero(t *testing.T) {
	id, _ := identity.NewCanonicalID(1, 0, 0, 0)
	if id.IsZero() {
		t.Error("non-zero CanonicalID should be IsZero() == false")
	}
}

// --- string / hex encoding ---

func TestCanonicalID_String_Is32HexChars(t *testing.T) {
	id, _ := identity.NewCanonicalID(1, 2, 3, 4)
	s := id.String()
	if len(s) != 32 {
		t.Errorf("String() length = %d, want 32", len(s))
	}
	for _, c := range s {
		if !isHexChar(c) {
			t.Errorf("String() contains non-hex character %q", c)
		}
	}
}

func TestCanonicalID_ZeroValue_StringIsAllZeros(t *testing.T) {
	var id identity.CanonicalID
	want := "00000000000000000000000000000000"
	if got := id.String(); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

// --- parse / round-trip ---

func TestParseCanonicalID_RoundTrips(t *testing.T) {
	original, _ := identity.NewCanonicalID(0x0001_0002_0003, 0xABCD, 0x0102, 0x0004_0005_0006)
	parsed, err := identity.ParseCanonicalID(original.String())
	if err != nil {
		t.Fatalf("ParseCanonicalID() error = %v", err)
	}
	if parsed != original {
		t.Errorf("ParseCanonicalID(id.String()) = %v, want %v", parsed, original)
	}
}

func TestParseCanonicalID_ZeroRoundTrips(t *testing.T) {
	var zero identity.CanonicalID
	parsed, err := identity.ParseCanonicalID(zero.String())
	if err != nil {
		t.Fatalf("ParseCanonicalID() error = %v", err)
	}
	if !parsed.IsZero() {
		t.Error("parsed zero string should yield IsZero() == true")
	}
}

func TestParseCanonicalID_RejectsWrongLength(t *testing.T) {
	_, err := identity.ParseCanonicalID("abc")
	if err != identity.ErrCanonicalIDInvalidFormat {
		t.Errorf("ParseCanonicalID() error = %v, want ErrCanonicalIDInvalidFormat", err)
	}
}

func TestParseCanonicalID_RejectsNonHex(t *testing.T) {
	// 32 chars but invalid hex
	_, err := identity.ParseCanonicalID("zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz")
	if err != identity.ErrCanonicalIDInvalidFormat {
		t.Errorf("ParseCanonicalID() error = %v, want ErrCanonicalIDInvalidFormat", err)
	}
}

// --- helpers ---

func isHexChar(c rune) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}
