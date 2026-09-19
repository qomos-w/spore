package bytecode

import "testing"

// TestValueCodec_ErrorTextGolden pins the negative-path error wording of the
// value codec. The messages (and diagnostic codes/paths) are part of the
// observable contract: host embedders and golden fixtures depend on them, so a
// refactor of the conversion logic must not drift them.
func TestValueCodec_ErrorTextGolden(t *testing.T) {
	eval := NewVMEvaluator()

	t.Run("unsupported host type", func(t *testing.T) {
		_, err := anyToVMValueAtPath(nil, eval.vm_, complex128(1), "vm/test/unsupported")
		rtErr, ok := err.(*RuntimeError)
		if !ok {
			t.Fatalf("expected *RuntimeError, got %T", err)
		}
		if rtErr.Code != "unsupported_vm_argument_type" {
			t.Errorf("code = %q, want unsupported_vm_argument_type", rtErr.Code)
		}
		if rtErr.Message != "unsupported VM argument type complex128" {
			t.Errorf("message = %q, want %q", rtErr.Message, "unsupported VM argument type complex128")
		}
		if rtErr.Path != "vm/test/unsupported" {
			t.Errorf("path = %q, want vm/test/unsupported", rtErr.Path)
		}
	})

	t.Run("unregistered struct appends struct-not-registered", func(t *testing.T) {
		_, err := anyToVMValueAtPath(nil, eval.vm_, struct{ Foo int }{Foo: 1}, "vm/test/struct")
		rtErr, ok := err.(*RuntimeError)
		if !ok {
			t.Fatalf("expected *RuntimeError, got %T", err)
		}
		if rtErr.Code != "unsupported_vm_argument_type" {
			t.Errorf("code = %q, want unsupported_vm_argument_type", rtErr.Code)
		}
		if rtErr.Message != "unsupported VM argument type struct { Foo int }" {
			t.Errorf("message = %q, want %q", rtErr.Message, "unsupported VM argument type struct { Foo int }")
		}
		if rtErr.Path != "vm/test/struct/struct-not-registered:" {
			t.Errorf("path = %q, want vm/test/struct/struct-not-registered:", rtErr.Path)
		}
	})

	t.Run("int exceeds int32 range", func(t *testing.T) {
		_, err := anyToVMValueAtPath(nil, eval.vm_, int(1)<<40, "vm/test/range")
		rtErr, ok := err.(*RuntimeError)
		if !ok {
			t.Fatalf("expected *RuntimeError, got %T", err)
		}
		if rtErr.Code != "value_out_of_range" {
			t.Errorf("code = %q, want value_out_of_range", rtErr.Code)
		}
		if rtErr.Message != "Go int value 1099511627776 exceeds script int32 range" {
			t.Errorf("message = %q", rtErr.Message)
		}
		if rtErr.Path != "vm/test/range" {
			t.Errorf("path = %q, want vm/test/range", rtErr.Path)
		}
	})

	t.Run("host decode type mismatch", func(t *testing.T) {
		var s string
		if err := DecodeHostValue(&s, int64(1)); err == nil || err.Error() != "DecodeInto: cannot decode int64 into string target" {
			t.Errorf("err = %v", err)
		}
		var i int64
		if err := DecodeHostValue(&i, nil); err == nil || err.Error() != "DecodeInto: cannot decode nil into integer target" {
			t.Errorf("err = %v", err)
		}
		var u uint64
		if err := DecodeHostValue(&u, "x"); err == nil || err.Error() != "DecodeInto: cannot decode string into unsigned integer target" {
			t.Errorf("err = %v", err)
		}
		var f float64
		if err := DecodeHostValue(&f, true); err == nil || err.Error() != "DecodeInto: cannot decode bool into number target" {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("host decode unsupported target", func(t *testing.T) {
		if err := DecodeHostValue(struct{}{}, int64(1)); err == nil || err.Error() != "DecodeInto does not support target type struct {}" {
			t.Errorf("err = %v", err)
		}
	})
}
