package runtime

// Component is a typed descriptor that binds a Go component type to its
// schema name — the storage key used by the World's name-keyed component
// maps and by the schema/binding/transport layers.
//
// Identity invariant: a Component[T] is identified solely by its schema
// name. NewComponent[tPos]("Position") and the string "Position" refer to
// the same stored component; the type parameter is a compile-time safety
// facade, not a separate storage identity. Two descriptors with the same
// name but different types alias the same storage slot — callers must keep
// the type-to-name mapping consistent.
//
// Component is a comparable value and safe to use as a package-level
// variable or constant-like definition:
//
//	var Position = runtime.NewComponent[tPos]("Position")
//
// Contract: public semantic contract — typed component descriptor for
// the name-keyed runtime carrier.
type Component[T any] struct {
	name string
}

// NewComponent creates a typed descriptor for the given schema name.
func NewComponent[T any](schemaName string) Component[T] {
	return Component[T]{name: schemaName}
}

// Name returns the schema name that identifies the component in the World.
func (c Component[T]) Name() string {
	return c.name
}