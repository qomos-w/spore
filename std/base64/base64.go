package base64

import (
	"encoding/base64"

	"github.com/qomos-w/spore/binding"
)

// Register registers the base64 standard library module into the given ScriptBinding.
func Register(sb *binding.ScriptBinding) error {
	builder := binding.NewCapability("base64", "module")

	// encode converts a string to its base64 representation.
	if err := builder.AddFreeFunction("encode", func(s string) string {
		return base64.StdEncoding.EncodeToString([]byte(s))
	}); err != nil {
		return err
	}

	// decode converts a base64 string back to its original form.
	if err := builder.AddFreeFunction("decode", func(s string) (string, error) {
		b, err := base64.StdEncoding.DecodeString(s)
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
	return sb.ExposeCapabilityCallables("base64")
}
