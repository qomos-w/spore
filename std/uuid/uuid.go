package uuid

import (
	"crypto/rand"
	"fmt"

	"github.com/qomos-w/spore/binding"
)

// Register registers the uuid standard library module into the given ScriptBinding.
func Register(sb *binding.ScriptBinding) error {
	builder := binding.NewCapability("uuid", "module")

	// v4 generates a random UUID version 4 string.
	if err := builder.AddFreeFunction("v4", func() (string, error) {
		var b [16]byte
		if _, err := rand.Read(b[:]); err != nil {
			return "", err
		}
		// Set version (4) and variant (RFC 4122) bits.
		b[6] = (b[6] & 0x0f) | 0x40
		b[8] = (b[8] & 0x3f) | 0x80
		return fmt.Sprintf("%x-%x-%x-%x-%x",
			b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
	}); err != nil {
		return err
	}

	// nil returns the nil UUID (all zeros).
	if err := builder.AddFreeFunction("nil", func() string {
		return "00000000-0000-0000-0000-000000000000"
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
	return sb.ExposeCapabilityCallables("uuid")
}
