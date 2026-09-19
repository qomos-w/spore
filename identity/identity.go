// Package identity defines the canonical identity discipline for spore.
//
// CanonicalID is a 128-bit identifier whose binary layout aligns with
// gospore.ActorID, enabling lossless mapping between the two when spore
// is hosted by gospore in a future phase.
//
// Layout (big-endian, 16 bytes):
//
//	bytes  0..5  — 48-bit millisecond timestamp
//	bytes  6..7  — 16-bit origin runtime slot
//	bytes  8..9  — 16-bit runtime incarnation
//	bytes 10..15 — 48-bit local sequence
package identity

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
)

var (
	// ErrCanonicalIDTimestampOverflow is returned when the timestamp does not
	// fit into 48 bits.
	// Contract: public semantic contract — sentinel error for timestamp overflow.
	ErrCanonicalIDTimestampOverflow = errors.New("spore/identity: timestamp exceeds 48 bits")

	// ErrCanonicalIDSequenceOverflow is returned when the sequence does not
	// fit into 48 bits.
	// Contract: public semantic contract — sentinel error for sequence overflow.
	ErrCanonicalIDSequenceOverflow = errors.New("spore/identity: sequence exceeds 48 bits")

	// ErrCanonicalIDInvalidFormat is returned when a string cannot be parsed
	// as a canonical identity.
	// Contract: public semantic contract — sentinel error for invalid identity format.
	ErrCanonicalIDInvalidFormat = errors.New("spore/identity: invalid canonical id format")
)

const (
	timestampMax uint64 = 1<<48 - 1
	sequenceMax  uint64 = 1<<48 - 1
)

// CanonicalID is the spore canonical 128-bit identity value.
// It is comparable and can be used as a map key.
// The zero value represents the null identity; use IsZero to test for it.
// Contract: public semantic contract — the canonical identity discipline
// for cross-plane entity identification, aligned with gospore.ActorID layout.
type CanonicalID [16]byte

// NewCanonicalID packs the four logical segments into a CanonicalID.
// It returns ErrCanonicalIDTimestampOverflow if timestampMS exceeds 2^48-1,
// and ErrCanonicalIDSequenceOverflow if sequence exceeds 2^48-1.
// Contract: public semantic contract — canonical identity construction.
// It returns ErrCanonicalIDTimestampOverflow if timestampMS exceeds 2^48-1,
// and ErrCanonicalIDSequenceOverflow if sequence exceeds 2^48-1.
func NewCanonicalID(timestampMS uint64, runtimeSlot uint16, incarnation uint16, sequence uint64) (CanonicalID, error) {
	var id CanonicalID
	if timestampMS > timestampMax {
		return id, ErrCanonicalIDTimestampOverflow
	}
	if sequence > sequenceMax {
		return id, ErrCanonicalIDSequenceOverflow
	}

	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], timestampMS)
	copy(id[0:6], buf[2:8])
	binary.BigEndian.PutUint16(id[6:8], runtimeSlot)
	binary.BigEndian.PutUint16(id[8:10], incarnation)
	binary.BigEndian.PutUint64(buf[:], sequence)
	copy(id[10:16], buf[2:8])
	return id, nil
}

// TimestampMS extracts the 48-bit millisecond timestamp segment.
func (id CanonicalID) TimestampMS() uint64 {
	var buf [8]byte
	copy(buf[2:8], id[0:6])
	return binary.BigEndian.Uint64(buf[:])
}

// RuntimeSlot extracts the 16-bit origin runtime slot segment.
func (id CanonicalID) RuntimeSlot() uint16 {
	return binary.BigEndian.Uint16(id[6:8])
}

// Incarnation extracts the 16-bit runtime incarnation segment.
func (id CanonicalID) Incarnation() uint16 {
	return binary.BigEndian.Uint16(id[8:10])
}

// Sequence extracts the 48-bit local sequence segment.
func (id CanonicalID) Sequence() uint64 {
	var buf [8]byte
	copy(buf[2:8], id[10:16])
	return binary.BigEndian.Uint64(buf[:])
}

// Split returns all four segments in layout order.
func (id CanonicalID) Split() (timestampMS uint64, runtimeSlot uint16, incarnation uint16, sequence uint64) {
	return id.TimestampMS(), id.RuntimeSlot(), id.Incarnation(), id.Sequence()
}

// IsZero reports whether the identity is the zero value.
func (id CanonicalID) IsZero() bool {
	return id == CanonicalID{}
}

// String encodes the identity as a 32-character lowercase hex string.
func (id CanonicalID) String() string {
	return hex.EncodeToString(id[:])
}

// ParseCanonicalID decodes a 32-character lowercase hex string produced by
// String. It returns ErrCanonicalIDInvalidFormat for any other input.
// Contract: public semantic contract — canonical identity decoding.
func ParseCanonicalID(s string) (CanonicalID, error) {
	var id CanonicalID
	if len(s) != 32 {
		return id, ErrCanonicalIDInvalidFormat
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return id, ErrCanonicalIDInvalidFormat
	}
	copy(id[:], b)
	return id, nil
}
