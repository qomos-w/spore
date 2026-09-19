package hash

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"

	"github.com/qomos-w/spore/binding"
)

// Register registers the hash standard library module into the given ScriptBinding.
func Register(sb *binding.ScriptBinding) error {
	builder := binding.NewCapability("hash", "module")

	// md5 returns the MD5 hash of a string as a lowercase hex string.
	if err := builder.AddFreeFunction("md5", func(s string) string {
		sum := md5.Sum([]byte(s))
		return hex.EncodeToString(sum[:])
	}); err != nil {
		return err
	}

	// sha256 returns the SHA-256 hash of a string as a lowercase hex string.
	if err := builder.AddFreeFunction("sha256", func(s string) string {
		sum := sha256.Sum256([]byte(s))
		return hex.EncodeToString(sum[:])
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
	return sb.ExposeCapabilityCallables("hash")
}
