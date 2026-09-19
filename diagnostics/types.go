package diagnostics

// Category classifies the layer where a Spore diagnostic originates.
type Category string

const (
	CategoryLoad      Category = "load"
	CategorySchema    Category = "schema"
	CategoryContract  Category = "contract"
	CategoryRuntime   Category = "runtime"
	CategoryHost      Category = "host"
	CategoryStream    Category = "stream"
	CategoryTransport Category = "transport"
)

// Position identifies a line/column location in Spore-authored source.
type Position struct {
	Line   int
	Column int
}

// Span identifies a source range.
type Span struct {
	Start Position
	End   Position
}

// Frame identifies one logical execution/debugging frame.
type Frame struct {
	Callable string
	Stage    string
	Span     Span
}

// Descriptor is the structured host-side diagnostic envelope.
type Descriptor struct {
	Category Category
	Code     string
	Message  string
	Callable string
	Stage    string
	Span     Span
	Path     string
	Identity string
	Expected string // Expected type/value/pattern for LLM-guided repair.
	Actual   string // Actual type/value/pattern for LLM-guided repair.
	Hint     string // Suggested repair action for LLM-guided recovery.
	Stack    []Frame
	Cause    *Descriptor
}
