package json

import (
	"encoding/json"

	"github.com/qomos-w/spore/binding"
)

// Register registers the json standard library module into the given ScriptBinding.
func Register(sb *binding.ScriptBinding) error {
	builder := binding.NewCapability("json", "module")

	// encode converts any value to a JSON string.
	if err := builder.AddFreeFunction("encode", func(v any) (string, error) {
		b, err := json.Marshal(v)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}); err != nil {
		return err
	}

	// decode parses a JSON string into a value.
	// Numbers decode as float64; objects decode as map[string]any;
	// arrays decode as []any; null decodes as nil.
	if err := builder.AddFreeFunction("decode", func(s string) (any, error) {
		var v any
		if err := json.Unmarshal([]byte(s), &v); err != nil {
			return nil, err
		}
		return v, nil
	}); err != nil {
		return err
	}

	// prettyEncode converts any value to an indented JSON string.
	if err := builder.AddFreeFunction("prettyEncode", func(v any) (string, error) {
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return "", err
		}
		return string(b), nil
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
	return sb.ExposeCapabilityCallables("json")
}
