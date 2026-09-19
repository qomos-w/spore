package transport_test

import (
	"testing"

	"github.com/qomos-w/spore/diagnostics"
	"github.com/qomos-w/spore/transport"
)

func TestTransport_AllDiagnosticCodesRegistered(t *testing.T) {
	codes := []string{transport.CodeEncodeError, transport.CodeDecodeError}
	for _, code := range codes {
		info, ok := diagnostics.LookupCode(code)
		if !ok {
			t.Fatalf("expected transport diagnostic code %q to be registered", code)
		}
		if info.Category != diagnostics.CategoryTransport {
			t.Fatalf("code %q registered under category %q, want CategoryTransport", code, info.Category)
		}
		if info.Hint == "" {
			t.Fatalf("code %q registered without a hint", code)
		}
		if info.Description == "" {
			t.Fatalf("code %q registered without a description", code)
		}
	}
}
