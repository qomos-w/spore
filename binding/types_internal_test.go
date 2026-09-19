package binding

import "testing"

func TestErrNil_Error(t *testing.T) {
	e := errNil("binding is nil")
	if e.Error() != "binding is nil" {
		t.Fatalf("expected 'binding is nil', got %q", e.Error())
	}
}
