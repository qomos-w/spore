// Package manifest decodes the shared spore codegen manifest format used
// by both `spore-gen-ts` and `spore-gen-ts-client`. Hosting this in one
// place lets both CLIs accept exactly the same input shape — schemas alone
// (legacy bare-array form) or schemas + callables (combined object form) —
// without divergence between the two decoders.
//
// The parsed manifest is converted into the `internal/gen/ts` package's
// strongly-typed descriptors (NamedObjectDesc / NamedCallableDesc). Either
// CLI can then forward those slices to its own renderer.
package manifest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/qomos-w/spore/internal/gen/ts"
	"github.com/qomos-w/spore/schema"
)

// SchemaEntry mirrors ts.NamedObjectDesc with lowerCamelCase JSON keys so
// the manifest reads naturally for hand-edited fixtures.
type SchemaEntry struct {
	Namespace  string            `json:"namespace"`
	SchemaID   uint64            `json:"schemaId"`
	Name       string            `json:"name"`
	Visibility string            `json:"visibility"`
	Object     schema.ObjectDesc `json:"object"`
}

// CallableEntry mirrors ts.NamedCallableDesc.
type CallableEntry struct {
	Namespace     string           `json:"namespace"`
	Name          string           `json:"name"`
	Description   string           `json:"description,omitempty"`
	Visibility    string           `json:"visibility"`
	Mode          string           `json:"mode"`
	ReqSchemaID   uint64           `json:"reqSchemaId"`
	ChunkSchemaID uint64           `json:"chunkSchemaId,omitempty"`
	FinalSchemaID uint64           `json:"finalSchemaId"`
	Req           schema.TypeDesc  `json:"req"`
	Chunk         *schema.TypeDesc `json:"chunk,omitempty"`
	Final         schema.TypeDesc  `json:"final"`
}

// combined is the new manifest shape; either field may be absent / empty.
type combined struct {
	Schemas   []SchemaEntry   `json:"schemas"`
	Callables []CallableEntry `json:"callables"`
}

// Decode accepts either the legacy bare-array form (schemas only) or the
// combined object form ({schemas, callables}). Discrimination is by the
// first non-whitespace byte: '[' → legacy; '{' → combined.
//
// Both forms produce ts.NamedObjectDesc / ts.NamedCallableDesc slices ready
// for ts.Generate (or any sibling generator). The slices are returned in
// declaration order.
func Decode(raw []byte) ([]ts.NamedObjectDesc, []ts.NamedCallableDesc, error) {
	trimmed := bytes.TrimLeftFunc(raw, isJSONSpace)
	if len(trimmed) == 0 {
		return nil, nil, fmt.Errorf("empty manifest")
	}

	var schemas []SchemaEntry
	var callables []CallableEntry
	switch trimmed[0] {
	case '[':
		if err := json.Unmarshal(raw, &schemas); err != nil {
			return nil, nil, err
		}
	case '{':
		var c combined
		if err := json.Unmarshal(raw, &c); err != nil {
			return nil, nil, err
		}
		schemas = c.Schemas
		callables = c.Callables
	default:
		return nil, nil, fmt.Errorf("manifest must start with '[' (legacy array) or '{' (combined object)")
	}

	outSchemas, err := convertSchemas(schemas)
	if err != nil {
		return nil, nil, err
	}
	outCallables, err := convertCallables(callables)
	if err != nil {
		return nil, nil, err
	}
	return outSchemas, outCallables, nil
}

func convertSchemas(entries []SchemaEntry) ([]ts.NamedObjectDesc, error) {
	out := make([]ts.NamedObjectDesc, 0, len(entries))
	for i, e := range entries {
		v, ok := ts.ParseVisibility(e.Visibility)
		if !ok {
			return nil, fmt.Errorf("schema entry[%d] %s/%s: unknown visibility %q", i, e.Namespace, e.Name, e.Visibility)
		}
		out = append(out, ts.NamedObjectDesc{
			Namespace:  e.Namespace,
			SchemaID:   e.SchemaID,
			Name:       e.Name,
			Object:     e.Object,
			Visibility: v,
		})
	}
	return out, nil
}

func convertCallables(entries []CallableEntry) ([]ts.NamedCallableDesc, error) {
	out := make([]ts.NamedCallableDesc, 0, len(entries))
	for i, e := range entries {
		v, ok := ts.ParseVisibility(e.Visibility)
		if !ok {
			return nil, fmt.Errorf("callable entry[%d] %s/%s: unknown visibility %q", i, e.Namespace, e.Name, e.Visibility)
		}
		mode := schema.CallableMode(e.Mode)
		switch mode {
		case schema.CallableModeUnary, schema.CallableModeStreaming:
		default:
			return nil, fmt.Errorf("callable entry[%d] %s/%s: unknown mode %q (want unary or streaming)", i, e.Namespace, e.Name, e.Mode)
		}
		out = append(out, ts.NamedCallableDesc{
			Namespace:     e.Namespace,
			Name:          e.Name,
			Description:   e.Description,
			Visibility:    v,
			Mode:          mode,
			ReqSchemaID:   e.ReqSchemaID,
			ChunkSchemaID: e.ChunkSchemaID,
			FinalSchemaID: e.FinalSchemaID,
			Req:           e.Req,
			Chunk:         e.Chunk,
			Final:         e.Final,
		})
	}
	return out, nil
}

// ParseVisibilities turns a comma-separated visibility CSV from a CLI flag
// into a slice of ts.Visibility values. Empty CSV is rejected — callers
// must spell out which audiences the output should target.
func ParseVisibilities(csv string) ([]ts.Visibility, error) {
	var out []ts.Visibility
	for _, raw := range strings.Split(csv, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		v, ok := ts.ParseVisibility(raw)
		if !ok {
			return nil, fmt.Errorf("unknown visibility %q (want one of internal/public/admin/diagnostic)", raw)
		}
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("--visibility cannot be empty")
	}
	return out, nil
}

// isJSONSpace returns true for the four JSON-defined whitespace runes.
// We deliberately avoid unicode.IsSpace because RFC 8259 only treats
// space, tab, LF, and CR as whitespace.
func isJSONSpace(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\r':
		return true
	}
	return false
}
