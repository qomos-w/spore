package runtime

// This file implements the World's indexed component storage:
//
//   - per-World component ID interning (schema name → dense uint32 ID),
//   - per-entity component presence/change bitmasks,
//   - per-component sparse sets (dense entity arrays with per-entity
//     position backlinks) serving as an inverted index for queries.
//
// Queries resolve their schema-name filters to component IDs once per call,
// pick the smallest hasAll set as the iteration driver, and verify the
// remaining filters with bitmask ANDs — no string hashing and no full
// entity-table scan on the hot path.

const maskBits = 64

func maskHas(m []uint64, id uint32) bool {
	w := int(id >> 6)
	return w < len(m) && m[w]&(1<<(id&63)) != 0
}

// maskSet sets the bit for id, growing m as needed.
func maskSet(m *[]uint64, id uint32) {
	w := int(id >> 6)
	for len(*m) <= w {
		*m = append(*m, 0)
	}
	(*m)[w] |= 1 << (id & 63)
}

func maskClear(m []uint64, id uint32) {
	w := int(id >> 6)
	if w < len(m) {
		m[w] &^= 1 << (id & 63)
	}
}

func maskZero(m []uint64) {
	for i := range m {
		m[i] = 0
	}
}

// eachMaskBit invokes fn for every set bit in m (ascending component ID).
func eachMaskBit(m []uint64, fn func(uint32)) {
	for w, word := range m {
		for word != 0 {
			b := word & (-word)
			fn(uint32(w<<6) + uint32(bittz(b)))
			word ^= b
		}
	}
}

func bittz(x uint64) int {
	n := 0
	for x&1 == 0 {
		x >>= 1
		n++
	}
	return n
}

// compID returns the dense component ID for schemaName, interning it on
// first use. Component IDs are per-World and assigned in first-write order.
func (w *World) compID(name string) uint32 {
	if id, ok := w.compIDs[name]; ok {
		return id
	}
	id := uint32(len(w.compNames))
	w.compIDs[name] = id
	w.compNames = append(w.compNames, name)
	w.compSets = append(w.compSets, &compSet{})
	return id
}

// compSet is the inverted index for one component: the dense array of
// entity states carrying it. Membership positions are backlinked through
// entityState.compPos, making add/remove O(1) with no map operations.
type compSet struct {
	dense []*entityState
}

// ensureCompSlots grows the entity's ID-indexed slices (components, compPos)
// so index id is addressable. Bitmask slices are grown lazily by maskSet.
func (w *World) ensureCompSlots(st *entityState, id uint32) {
	grow := int(id) + 1 - len(st.components)
	for i := 0; i < grow; i++ {
		st.components = append(st.components, nil)
		st.compPos = append(st.compPos, -1)
	}
}

// setAdd inserts the entity into the component's sparse set.
func (w *World) setAdd(st *entityState, id uint32) {
	w.ensureCompSlots(st, id)
	if st.compPos[id] >= 0 {
		return
	}
	s := w.compSets[id]
	st.compPos[id] = int32(len(s.dense))
	s.dense = append(s.dense, st)
}

// setRemove removes the entity from the component's sparse set via
// swap-remove, repairing the swapped-in entity's backlink.
func (w *World) setRemove(st *entityState, id uint32) {
	if int(id) >= len(st.compPos) || st.compPos[id] < 0 {
		return
	}
	s := w.compSets[id]
	p := st.compPos[id]
	last := s.dense[len(s.dense)-1]
	s.dense[p] = last
	last.compPos[id] = p
	s.dense[len(s.dense)-1] = nil
	s.dense = s.dense[:len(s.dense)-1]
	st.compPos[id] = -1
}

// removeFromAllSets strips the entity from every sparse set it belongs to,
// clearing its component slots. Used by the Tick tombstone sweep and by
// CreateWithID when overwriting an existing entry.
func (w *World) removeFromAllSets(st *entityState) {
	if st.has == nil {
		return
	}
	ids := make([]uint32, 0, len(st.components))
	eachMaskBit(st.has, func(id uint32) { ids = append(ids, id) })
	for _, id := range ids {
		w.setRemove(st, id)
		st.components[id] = nil
		maskClear(st.has, id)
	}
}

// stateComponent returns the stored component data for a schema name from
// an already-resolved entity state.
func (w *World) stateComponent(st *entityState, name string) (any, bool) {
	id, ok := w.compIDs[name]
	if !ok || !maskHas(st.has, id) {
		return nil, false
	}
	return st.components[id], true
}

// compIDList is a filter list resolved to component IDs. Up to four
// filters of one kind are held inline so compiling a typical query
// allocates nothing; larger lists spill to a heap slice.
type compIDList struct {
	ids     [4]uint32
	n       int
	spilled []uint32
}

func (l *compIDList) add(id uint32) {
	if l.n < len(l.ids) {
		l.ids[l.n] = id
		l.n++
		return
	}
	l.spilled = append(l.spilled, id)
}

func (l *compIDList) empty() bool { return l.n == 0 && len(l.spilled) == 0 }

// compiledQuery is a Query with its schema-name filters resolved to
// component IDs against a specific World.
type compiledQuery struct {
	impossible   bool // references a never-interned component under a must-have filter
	hasAll       compIDList
	hasNone      compIDList
	hasEither    compIDList
	whenAdded    compIDList
	whenChanged  compIDList
	whenRemoved  compIDList
}

// compileQuery resolves the query's name filters to component IDs once,
// ahead of entity iteration. Filters naming components that were never
// interned (no entity ever carried them) are dropped from hasNone/hasEither
// (vacuously satisfied) and make must-have filters unsatisfiable.
func (w *World) compileQuery(q *Query) compiledQuery {
	var cq compiledQuery
	resolve := func(names []string, dst *compIDList, mustHave bool) {
		for _, name := range names {
			id, ok := w.compIDs[name]
			if !ok {
				if mustHave {
					cq.impossible = true
				}
				continue
			}
			dst.add(id)
		}
	}
	resolve(q.hasAll, &cq.hasAll, true)
	resolve(q.hasNone, &cq.hasNone, false)
	resolve(q.hasEither, &cq.hasEither, false)
	resolve(q.whenAdded, &cq.whenAdded, true)
	resolve(q.whenChanged, &cq.whenChanged, true)
	resolve(q.whenRemoved, &cq.whenRemoved, true)
	return cq
}

// matches checks an entity state against the compiled filters using
// bitmask lookups only.
func (cq *compiledQuery) matches(st *entityState) bool {
	for i := 0; i < cq.hasAll.n; i++ {
		if !maskHas(st.has, cq.hasAll.ids[i]) {
			return false
		}
	}
	for _, id := range cq.hasAll.spilled {
		if !maskHas(st.has, id) {
			return false
		}
	}
	for i := 0; i < cq.hasNone.n; i++ {
		if maskHas(st.has, cq.hasNone.ids[i]) {
			return false
		}
	}
	for _, id := range cq.hasNone.spilled {
		if maskHas(st.has, id) {
			return false
		}
	}
	if !cq.hasEither.empty() {
		found := false
		for i := 0; i < cq.hasEither.n && !found; i++ {
			found = maskHas(st.has, cq.hasEither.ids[i])
		}
		for _, id := range cq.hasEither.spilled {
			if maskHas(st.has, id) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	for i := 0; i < cq.whenAdded.n; i++ {
		if !maskHas(st.added, cq.whenAdded.ids[i]) {
			return false
		}
	}
	for _, id := range cq.whenAdded.spilled {
		if !maskHas(st.added, id) {
			return false
		}
	}
	for i := 0; i < cq.whenChanged.n; i++ {
		if !maskHas(st.changed, cq.whenChanged.ids[i]) {
			return false
		}
	}
	for _, id := range cq.whenChanged.spilled {
		if !maskHas(st.changed, id) {
			return false
		}
	}
	for i := 0; i < cq.whenRemoved.n; i++ {
		if !maskHas(st.removed, cq.whenRemoved.ids[i]) {
			return false
		}
	}
	for _, id := range cq.whenRemoved.spilled {
		if !maskHas(st.removed, id) {
			return false
		}
	}
	return true
}

// rangeStates iterates entities matching the compiled query, invoking fn
// per matched state. fn returning false stops iteration.
//
// When the query has hasAll filters, iteration is driven by the smallest
// matching sparse set; entities swap-removed from the driver by fn are
// compensated by revisiting the slot (no entity is skipped by its own
// removal). Queries without hasAll filters fall back to a full table walk.
func (w *World) rangeStates(cq *compiledQuery, fn func(st *entityState) bool) {
	if cq.impossible {
		return
	}
	if !cq.hasAll.empty() {
		var driver *compSet
		for i := 0; i < cq.hasAll.n; i++ {
			s := w.compSets[cq.hasAll.ids[i]]
			if driver == nil || len(s.dense) < len(driver.dense) {
				driver = s
			}
		}
		for _, id := range cq.hasAll.spilled {
			s := w.compSets[id]
			if driver == nil || len(s.dense) < len(driver.dense) {
				driver = s
			}
		}
		if len(driver.dense) == 0 {
			return
		}
		i := 0
		for i < len(driver.dense) {
			// Re-read the set each step: fn may append (entities gaining
			// the driving component) and swap-remove concurrently.
			st := driver.dense[i]
			if st == nil || st.disposed || !cq.matches(st) {
				i++
				continue
			}
			if !fn(st) {
				return
			}
			if i < len(driver.dense) && driver.dense[i] != st {
				i-- // slot was backfilled during fn; revisit the moved-in entity
			}
			i++
		}
		return
	}
	for _, st := range w.entities {
		if st.disposed || !cq.matches(st) {
			continue
		}
		if !fn(st) {
			return
		}
	}
}
