package hex

import (
	"encoding/hex"

	"github.com/qomos-w/spore/binding"
)

// Register registers the hex standard library module into the given ScriptBinding.
func Register(sb *binding.ScriptBinding) error {
	builder := binding.NewCapability("hex", "module")

	// encode converts a string to its hexadecimal representation.
	// The input is treated as UTF-8 bytes.
	if err := builder.AddFreeFunction("encode", func(s string) string {
		return hex.EncodeToString([]byte(s))
	}); err != nil {
		return err
	}
	if err := builder.AddFreeFunction("decode", func(s string) (string, error) {
		b, err := hex.DecodeString(s)
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
	return sb.ExposeCapabilityCallables("hex")
}
