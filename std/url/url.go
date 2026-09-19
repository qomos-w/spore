package url

import (
	"net/url"

	"github.com/qomos-w/spore/binding"
)

// Register registers the url standard library module into the given ScriptBinding.
func Register(sb *binding.ScriptBinding) error {
	builder := binding.NewCapability("url", "module")

	// encode escapes a string so it can be safely placed inside a URL query.
	if err := builder.AddFreeFunction("encode", func(s string) string {
		return url.QueryEscape(s)
	}); err != nil {
		return err
	}

	// decode unescapes a URL-encoded string.
	if err := builder.AddFreeFunction("decode", func(s string) (string, error) {
		return url.QueryUnescape(s)
	}); err != nil {
		return err
	}

	// parse splits a raw URL into its components.
	// Returns a map with scheme, host, path, and rawQuery.
	if err := builder.AddFreeFunction("parse", func(raw string) (map[string]string, error) {
		u, err := url.Parse(raw)
		if err != nil {
			return nil, err
		}
		port := u.Port()
		if port == "" {
			port = ""
		}
		return map[string]string{
			"scheme":   u.Scheme,
			"host":     u.Host,
			"port":     port,
			"path":     u.Path,
			"rawQuery": u.RawQuery,
			"fragment": u.Fragment,
		}, nil
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
	return sb.ExposeCapabilityCallables("url")
}
