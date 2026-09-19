package runtime

// ChangeSet captures the observable mutations on an entity within a tick.
// It records which components were added, changed (mutated), or removed.
//
// ChangeSet is an observability surface, NOT an authority commit record.
// It serves script-visible change detection, binding layer dirty tracking,
// and transport projection — not goflora fact/knowledge/control semantics.
//
// Contract: public semantic contract — entity change observability surface.
type ChangeSet struct {
	Added   []string
	Changed []string
	Removed []string
}

// Empty reports whether the change set has no recorded mutations.
func (cs ChangeSet) Empty() bool {
	return len(cs.Added) == 0 && len(cs.Changed) == 0 && len(cs.Removed) == 0
}
