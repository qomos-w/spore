package runtime_test

import (
	"testing"

	"github.com/qomos-w/spore/diagnostics"
	"github.com/qomos-w/spore/runtime"
)

func TestRuntime_AllDiagnosticCodesRegistered(t *testing.T) {
	codes := []string{runtime.CodeEntityError}
	for _, code := range codes {
		info, ok := diagnostics.LookupCode(code)
		if !ok {
			t.Fatalf("expected runtime diagnostic code %q to be registered", code)
		}
		if info.Category != diagnostics.CategoryRuntime {
			t.Fatalf("code %q registered under category %q, want CategoryRuntime", code, info.Category)
		}
		if info.Hint == "" {
			t.Fatalf("code %q registered without a hint", code)
		}
		if info.Description == "" {
			t.Fatalf("code %q registered without a description", code)
		}
	}
}
