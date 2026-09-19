package binding

import (
	"testing"

	"github.com/qomos-w/spore/diagnostics"
)

func TestBinding_AllDiagnosticCodesRegistered(t *testing.T) {
	codes := []string{
		CodeInvalidArgumentCount,
		CodeNilArgument,
		CodeInvalidArgumentType,
		CodeInvalidInvocationStage,
		CodeMissingStreamNextSchema,
		CodeMissingStreamFinalSchema,
		CodeUnsupportedCallableMode,
		CodeBindingError,
	}
	for _, code := range codes {
		info, ok := diagnostics.LookupCode(code)
		if !ok {
			t.Fatalf("expected binding diagnostic code %q to be registered", code)
		}
		if info.Category != diagnostics.CategoryContract {
			t.Fatalf("code %q registered under category %q, want CategoryContract", code, info.Category)
		}
		if info.Hint == "" {
			t.Fatalf("code %q registered without a hint", code)
		}
		if info.Description == "" {
			t.Fatalf("code %q registered without a description", code)
		}
	}
}
