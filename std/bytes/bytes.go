package bytes

import (
	"bytes"
	"fmt"

	"github.com/qomos-w/spore/binding"
)

// Register registers the bytes standard library module into the given ScriptBinding.
func Register(sb *binding.ScriptBinding) error {
	builder := binding.NewCapability("bytes", "module")

	if err := builder.AddFreeFunction("length", func(data []byte) int {
		return len(data)
	}); err != nil {
		return err
	}

	if err := builder.AddFreeFunction("slice", func(data []byte, start, end int) ([]byte, error) {
		if start < 0 || end > len(data) || start > end {
			return nil, fmt.Errorf("slice bounds out of range")
		}
		return data[start:end], nil
	}); err != nil {
		return err
	}

	if err := builder.AddFreeFunction("concat", func(a, b []byte) []byte {
		out := make([]byte, len(a)+len(b))
		copy(out, a)
		copy(out[len(a):], b)
		return out
	}); err != nil {
		return err
	}

	if err := builder.AddFreeFunction("compare", func(a, b []byte) int {
		return bytes.Compare(a, b)
	}); err != nil {
		return err
	}

	if err := builder.AddFreeFunction("contains", func(data, sub []byte) bool {
		return bytes.Contains(data, sub)
	}); err != nil {
		return err
	}

	if err := builder.AddFreeFunction("index", func(data, sub []byte) int {
		return bytes.Index(data, sub)
	}); err != nil {
		return err
	}

	if err := builder.AddFreeFunction("equal", func(a, b []byte) bool {
		return bytes.Equal(a, b)
	}); err != nil {
		return err
	}

	if err := builder.AddFreeFunction("hasPrefix", func(data, prefix []byte) bool {
		return bytes.HasPrefix(data, prefix)
	}); err != nil {
		return err
	}

	if err := builder.AddFreeFunction("hasSuffix", func(data, suffix []byte) bool {
		return bytes.HasSuffix(data, suffix)
	}); err != nil {
		return err
	}

	if err := builder.AddFreeFunction("repeat", func(data []byte, count int) []byte {
		return bytes.Repeat(data, count)
	}); err != nil {
		return err
	}

	if err := builder.AddFreeFunction("replace", func(data, old, new []byte, n int) []byte {
		return bytes.Replace(data, old, new, n)
	}); err != nil {
		return err
	}

	if err := builder.AddFreeFunction("toLower", func(data []byte) []byte {
		return bytes.ToLower(data)
	}); err != nil {
		return err
	}

	if err := builder.AddFreeFunction("toUpper", func(data []byte) []byte {
		return bytes.ToUpper(data)
	}); err != nil {
		return err
	}

	if err := builder.AddFreeFunction("trimSpace", func(data []byte) []byte {
		return bytes.TrimSpace(data)
	}); err != nil {
		return err
	}

	cap, err := builder.Build()
	if err != nil {
		return err
	}
	if err := sb.RegisterCapability(cap); err != nil {
		return err
	}
	return sb.ExposeCapabilityCallables("bytes")
}
