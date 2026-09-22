package bytecode

import (
	"errors"
	"reflect"
	"testing"

	"github.com/qomos-w/spore/internal/script/vm"
)

// codecProofScalar is a hypothetical host type that exists only in this test.
// It is a named scalar (underlying int64) that is NOT one of the codec's
// built-in exact types, so it can only cross the boundary through the
// extension seam. It is taught to the codec in exactly one place — its own
// method set — without editing any production type switch.
type codecProofScalar int64

var errCodecProofDecode = errors.New("codecProofScalar: cannot decode")

// EncodeToVM implements HostValueEncoder (value receiver).
func (p codecProofScalar) EncodeToVM(vm_ *vm.VM, path string) (vm.Value, error) {
	return vm.EncodeLong(int64(p), vm_), nil
}

// DecodeFromHost implements HostValueDecoder (pointer receiver).
func (p *codecProofScalar) DecodeFromHost(value any) error {
	n, ok := value.(int64)
	if !ok {
		return errCodecProofDecode
	}
	*p = codecProofScalar(n)
	return nil
}

// codecProofHolder is a plain host struct used to exercise the struct-field
// encoding path with a codec extension field.
type codecProofHolder struct {
	M codecProofScalar
}

func TestValueCodec_ExtensionType_EveryPath(t *testing.T) {
	eval := NewVMEvaluator()

	// 1. Direct scalar encode through the public bridge.
	v, err := HostAnyToVMValue(nil, eval.vm_, codecProofScalar(7), "test/scalar")
	if err != nil {
		t.Fatalf("scalar encode: %v", err)
	}
	if !vm.IsLong(v) || eval.vm_.DecodeLong(v) != 7 {
		t.Fatalf("scalar encode: got %v, want long 7", v)
	}

	// 2. Call-argument encode (the exact path Evaluate/toVMArgs uses).
	args, err := toVMArgs(nil, eval.vm_, []any{codecProofScalar(11)})
	if err != nil {
		t.Fatalf("toVMArgs: %v", err)
	}
	if len(args) != 1 || eval.vm_.DecodeLong(args[0]) != 11 {
		t.Fatalf("toVMArgs: got %v, want long 11", args)
	}

	// 3. Nested encode inside an array.
	nested, err := anyToVMValueAtPath(nil, eval.vm_, []any{codecProofScalar(3)}, "test/nested")
	if err != nil {
		t.Fatalf("nested encode: %v", err)
	}
	items, ok := vmValueToAny(eval.vm_, nested).([]any)
	if !ok || len(items) != 1 || items[0].(int64) != 3 {
		t.Fatalf("nested encode: got %#v, want []any{int64(3)}", vmValueToAny(eval.vm_, nested))
	}

	// 4. Host struct field encode (goStructToVMStruct).
	eval.vm_.StructReg().RegisterStruct("codecProofHolder", []vm.FieldDef{
		vm.NewFieldDef("M", vm.TypeInvalid, 1),
	})
	c := acquireConvCtx(nil, eval.vm_, "test/holder")
	hv, err := c.goStruct(reflect.ValueOf(codecProofHolder{M: codecProofScalar(5)}))
	releaseConvCtx(c)
	if err != nil {
		t.Fatalf("struct field encode: %v", err)
	}
	field := vmValueToAnyWithHost(nil, eval.vm_, eval.vm_.GetStructFieldByIndex(vm.DecodeHandle(hv), 0))
	if field != int64(5) {
		t.Fatalf("struct field encode: got %#v, want int64(5)", field)
	}

	// 5. Native-result projection must pass the extension type through intact,
	// then the codec encodes it (the sequence InvokeVMNative uses).
	projected := projectNativeResult(codecProofScalar(13))
	if projected != codecProofScalar(13) {
		t.Fatalf("projectNativeResult: got %#v, want unchanged codecProofScalar", projected)
	}
	res, err := anyToVMValueAtPath(nil, eval.vm_, projected, "vm/call/native/result")
	if err != nil {
		t.Fatalf("native result encode: %v", err)
	}
	if eval.vm_.DecodeLong(res) != 13 {
		t.Fatalf("native result encode: got %d, want 13", eval.vm_.DecodeLong(res))
	}

	// 6. Host-side decode target (Result.DecodeInto -> DecodeHostValue).
	var out codecProofScalar
	if err := DecodeHostValue(&out, int64(21)); err != nil {
		t.Fatalf("decode target: %v", err)
	}
	if out != codecProofScalar(21) {
		t.Fatalf("decode target: got %d, want 21", int64(out))
	}
}

// TestValueCodec_ExtensionType_UnregisteredStillRejected guards the negative
// path: a struct that does NOT opt into the codec is still rejected, so the
// seam does not accidentally accept arbitrary host types.
func TestValueCodec_ExtensionType_UnregisteredStillRejected(t *testing.T) {
	eval := NewVMEvaluator()
	type stranger struct{ Foo int }
	_, err := anyToVMValueAtPath(nil, eval.vm_, stranger{Foo: 1}, "vm/test/stranger")
	rtErr, ok := err.(*RuntimeError)
	if !ok {
		t.Fatalf("expected *RuntimeError, got %T", err)
	}
	if rtErr.Code != "unsupported_vm_argument_type" {
		t.Fatalf("got code %q, want unsupported_vm_argument_type", rtErr.Code)
	}
}
