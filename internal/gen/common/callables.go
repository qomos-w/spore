package common

import (
	"fmt"

	"github.com/qomos-w/spore/schema"
)

// ValidateOptions adjusts ValidateCallables for a generator that supports only
// a subset of the callable modes.
type ValidateOptions struct {
	// RejectStreaming, when non-nil, is consulted for every streaming
	// callable and its error is returned verbatim. Generators that cannot yet
	// handle streaming (go-server) supply a function that always errors, so
	// the restriction lives with the generator instead of in this package.
	//
	// When nil, streaming callables are accepted and their chunk fields are
	// validated like any other field.
	RejectStreaming func(namespace, name string) error
}

// ValidateCallables enforces the callable descriptor contract shared by every
// generator: non-empty Namespace / Name, a known Mode, mode-consistent chunk
// fields, and unique (Namespace, Name) pairs. Duplicates would either silently
// overwrite or render conflicting output; better to fail loudly.
//
// Streaming callables are validated (or rejected via opts.RejectStreaming)
// before the duplicate check, matching the historical per-generator order.
func ValidateCallables(input []NamedCallableDesc, opts ValidateOptions) error {
	seen := map[string]map[string]struct{}{}
	for i, c := range input {
		if c.Namespace == "" {
			return fmt.Errorf("callable entry[%d]: empty Namespace", i)
		}
		if c.Name == "" {
			return fmt.Errorf("callable entry[%d]: empty Name", i)
		}
		switch c.Mode {
		case schema.CallableModeUnary:
			if c.ChunkSchemaID != 0 || c.Chunk != nil {
				return fmt.Errorf("callable %s/%s: unary mode must not carry chunk schema", c.Namespace, c.Name)
			}
		case schema.CallableModeStreaming:
			if opts.RejectStreaming != nil {
				return opts.RejectStreaming(c.Namespace, c.Name)
			}
			if c.ChunkSchemaID == 0 {
				return fmt.Errorf("callable %s/%s: streaming mode requires non-zero ChunkSchemaID", c.Namespace, c.Name)
			}
			if c.Chunk == nil {
				return fmt.Errorf("callable %s/%s: streaming mode requires Chunk TypeDesc", c.Namespace, c.Name)
			}
		default:
			return fmt.Errorf("callable %s/%s: unknown Mode %q (want unary or streaming)", c.Namespace, c.Name, c.Mode)
		}
		nsTable, ok := seen[c.Namespace]
		if !ok {
			nsTable = map[string]struct{}{}
			seen[c.Namespace] = nsTable
		}
		if _, dup := nsTable[c.Name]; dup {
			return fmt.Errorf("namespace %q: callable name %q declared twice", c.Namespace, c.Name)
		}
		nsTable[c.Name] = struct{}{}
	}
	return nil
}
