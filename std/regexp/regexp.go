package regexp

import (
	"regexp"

	"github.com/qomos-w/spore/binding"
)

// Register registers the regexp standard library module into the given ScriptBinding.
func Register(sb *binding.ScriptBinding) error {
	builder := binding.NewCapability("regexp", "module")

	// match reports whether the string s contains any match of the pattern.
	if err := builder.AddFreeFunction("match", func(pattern, s string) (bool, error) {
		return regexp.MatchString(pattern, s)
	}); err != nil {
		return err
	}

	// find returns the leftmost match of the pattern in s.
	// Returns empty string if there is no match — indistinguishable from a match of "".
	if err := builder.AddFreeFunction("find", func(pattern, s string) (string, error) {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return "", err
		}
		result := re.FindString(s)
		return result, nil
	}); err != nil {
		return err
	}

	// findAll returns all successive matches of the pattern in s.
	if err := builder.AddFreeFunction("findAll", func(pattern, s string) ([]string, error) {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, err
		}
		return re.FindAllString(s, -1), nil
	}); err != nil {
		return err
	}

	// replace returns a copy of s with all matches of the pattern replaced by repl.
	if err := builder.AddFreeFunction("replace", func(pattern, s, repl string) (string, error) {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return "", err
		}
		return re.ReplaceAllString(s, repl), nil
	}); err != nil {
		return err
	}

	// split slices s into substrings separated by the pattern and returns a slice.
	if err := builder.AddFreeFunction("split", func(pattern, s string) ([]string, error) {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, err
		}
		return re.Split(s, -1), nil
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
	return sb.ExposeCapabilityCallables("regexp")
}
