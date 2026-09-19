package benchmarks

import "fmt"

type errFnNotFound string

func (e errFnNotFound) Error() string {
	return fmt.Sprintf("benchmarks: function %q not found in script", string(e))
}
