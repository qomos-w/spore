package schema

import (
	"cmp"
	"reflect"
	"slices"
	"sort"
)

// IsOrderedMapType reports whether t is an OrderedMap, SortedOrderedMap, or
// other Entries()-bearing variant defined by this package. Boundary layers
// (binding, transport) must use this single authoritative detection instead
// of re-implementing package-path and type-name-prefix matching.
// Contract: public semantic contract — OrderedMap detection for boundary layers.
func IsOrderedMapType(t reflect.Type) bool {
	if t.PkgPath() != "github.com/qomos-w/spore/schema" {
		return false
	}
	name := t.Name()
	return (len(name) > 11 && name[:11] == "OrderedMap[") ||
		(len(name) > 17 && name[:17] == "SortedOrderedMap[")
}

type orderedKey interface {
	~string |
		~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr |
		~float32 | ~float64
}

// Entry is the canonical key-value pair in an ordered map.
// Contract: public semantic contract — shared surface consumed by script
// enumeration, snapshot, diff, and transport projection.
type Entry[K comparable, V any] struct {
	Key   K
	Value V
}

// SetResult classifies the outcome of a Set operation.
// Contract: public semantic contract — distinguishes inserted vs replaced.
type SetResult string

// DeleteResult classifies the outcome of a Delete operation.
// Contract: public semantic contract — distinguishes removed vs missing/no-op.
type DeleteResult string

const (
	SetInserted SetResult = "inserted"
	SetReplaced SetResult = "replaced"

	DeleteRemoved DeleteResult = "removed"
	DeleteMissing DeleteResult = "missing"
)

// OrderedMap is an insertion-ordered map. It provides stable enumeration
// order suitable for script-visible map surfaces, snapshot/diff, and
// transport projection.
// Contract: public semantic contract — the canonical ordered map for
// script-visible map semantics across all planes.
type OrderedMap[K comparable, V any] struct {
	index   map[K]int
	entries []Entry[K, V]
}

func NewOrderedMap[K comparable, V any]() *OrderedMap[K, V] {
	return &OrderedMap[K, V]{
		index: make(map[K]int),
	}
}

func OrderedMapFromMap[K orderedKey, V any](src map[K]V) *OrderedMap[K, V] {
	ordered := NewOrderedMap[K, V]()
	if len(src) == 0 {
		return ordered
	}

	keys := make([]K, 0, len(src))
	for key := range src {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		ordered.Set(key, src[key])
	}
	return ordered
}

func (m *OrderedMap[K, V]) Len() int {
	if m == nil {
		return 0
	}
	return len(m.entries)
}

func (m *OrderedMap[K, V]) Has(key K) bool {
	if m == nil {
		return false
	}
	_, ok := m.index[key]
	return ok
}

func (m *OrderedMap[K, V]) Get(key K) (V, bool) {
	var zero V
	if m == nil {
		return zero, false
	}
	idx, ok := m.index[key]
	if !ok {
		return zero, false
	}
	return m.entries[idx].Value, true
}

func (m *OrderedMap[K, V]) Set(key K, value V) SetResult {
	if m.index == nil {
		m.index = make(map[K]int)
	}
	if idx, ok := m.index[key]; ok {
		m.entries[idx].Value = value
		return SetReplaced
	}
	m.index[key] = len(m.entries)
	m.entries = append(m.entries, Entry[K, V]{Key: key, Value: value})
	return SetInserted
}

func (m *OrderedMap[K, V]) Delete(key K) DeleteResult {
	if m == nil || m.index == nil {
		return DeleteMissing
	}
	idx, ok := m.index[key]
	if !ok {
		return DeleteMissing
	}

	delete(m.index, key)
	m.entries = append(m.entries[:idx], m.entries[idx+1:]...)
	for i := idx; i < len(m.entries); i++ {
		m.index[m.entries[i].Key] = i
	}
	return DeleteRemoved
}

func (m *OrderedMap[K, V]) Entries() []Entry[K, V] {
	if m == nil || len(m.entries) == 0 {
		return nil
	}
	entries := make([]Entry[K, V], len(m.entries))
	copy(entries, m.entries)
	return entries
}

// Comparator is an explicit comparison function for key ordering.
// Contract: public semantic contract — provides the ordering contract
// for SortedOrderedMap and any comparator-driven variant.
// It returns negative if a < b, zero if a == b, positive if a > b.
//
// SortedOrderedMap uses a Comparator instead of relying on the natural
// ordering of orderedKey types. This makes the ordering contract explicit
// and allows custom sort orders without affecting the default insertion-
// order semantics of OrderedMap.
type Comparator[K comparable] func(a, b K) int

// NaturalOrder returns a Comparator that uses the natural ordering of
// orderedKey types. This is the comparator equivalent of slices.Sort.
//
// Contract: public semantic contract — the default comparator for orderedKey types.
func NaturalOrder[K orderedKey]() Comparator[K] {
	return func(a, b K) int {
		return cmp.Compare(a, b)
	}
}

// SortedOrderedMap is a comparator-driven variant of OrderedMap. It keeps
// entries in sorted order determined by its Comparator, rather than insertion
// order. This is a distinct type from OrderedMap to prevent accidental
// overriding of default insertion-order semantics.
//
// Contract: public semantic contract — the comparator-driven ordered map,
// a distinct type from OrderedMap to preserve default insertion-order semantics.
//
// SortedOrderedMap satisfies the same public surface as OrderedMap:
// Get, Set, Delete, Has, Len, Entries. The difference is that Set inserts
// new keys at their sorted position instead of at the end.
//
// Per TDD §9.4: "comparator-driven ordered variant 若被引入，必须作为显式
// contract 单独测试，而不覆盖默认插入顺序语义"
type SortedOrderedMap[K comparable, V any] struct {
	cmp     Comparator[K]
	index   map[K]int
	entries []Entry[K, V]
}

// NewSortedOrderedMap creates a SortedOrderedMap with the given comparator.
// The comparator determines the sorted order of keys.
func NewSortedOrderedMap[K comparable, V any](cmp Comparator[K]) *SortedOrderedMap[K, V] {
	return &SortedOrderedMap[K, V]{
		cmp:   cmp,
		index: make(map[K]int),
	}
}

func (m *SortedOrderedMap[K, V]) Len() int {
	if m == nil {
		return 0
	}
	return len(m.entries)
}

func (m *SortedOrderedMap[K, V]) Has(key K) bool {
	if m == nil {
		return false
	}
	_, ok := m.index[key]
	return ok
}

func (m *SortedOrderedMap[K, V]) Get(key K) (V, bool) {
	var zero V
	if m == nil {
		return zero, false
	}
	idx, ok := m.index[key]
	if !ok {
		return zero, false
	}
	return m.entries[idx].Value, true
}

// Set inserts the key at its sorted position if new, or updates the value
// in place if the key already exists. New keys are inserted at the position
// determined by the comparator, not at the end.
func (m *SortedOrderedMap[K, V]) Set(key K, value V) SetResult {
	if m.index == nil {
		m.index = make(map[K]int)
	}
	if idx, ok := m.index[key]; ok {
		m.entries[idx].Value = value
		return SetReplaced
	}

	entry := Entry[K, V]{Key: key, Value: value}
	pos := sort.Search(len(m.entries), func(i int) bool {
		return m.cmp(m.entries[i].Key, key) > 0
	})

	// Insert at sorted position
	m.entries = append(m.entries, Entry[K, V]{})
	copy(m.entries[pos+1:], m.entries[pos:])
	m.entries[pos] = entry

	// Rebuild index from insertion point onward
	m.index[key] = pos
	for i := pos + 1; i < len(m.entries); i++ {
		m.index[m.entries[i].Key] = i
	}

	return SetInserted
}

func (m *SortedOrderedMap[K, V]) Delete(key K) DeleteResult {
	if m == nil || m.index == nil {
		return DeleteMissing
	}
	idx, ok := m.index[key]
	if !ok {
		return DeleteMissing
	}

	delete(m.index, key)
	m.entries = append(m.entries[:idx], m.entries[idx+1:]...)
	for i := idx; i < len(m.entries); i++ {
		m.index[m.entries[i].Key] = i
	}
	return DeleteRemoved
}

func (m *SortedOrderedMap[K, V]) Entries() []Entry[K, V] {
	if m == nil || len(m.entries) == 0 {
		return nil
	}
	entries := make([]Entry[K, V], len(m.entries))
	copy(entries, m.entries)
	return entries
}
