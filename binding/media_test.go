package binding_test

import (
	"strings"
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/schema"
)

func mediaFunc(photo schema.Media) string { return photo.Mime }

func TestMediaBinding_DescribeMapsParamKind(t *testing.T) {
	desc, err := schema.DescribeGoFunction("mediaFunc", mediaFunc)
	if err != nil {
		t.Fatalf("DescribeGoFunction: %v", err)
	}
	if len(desc.Parameters) != 1 {
		t.Fatalf("expected 1 parameter, got %d", len(desc.Parameters))
	}
	if desc.Parameters[0].Type.Kind != schema.TypeKindMedia {
		t.Fatalf("expected media parameter kind, got %s", desc.Parameters[0].Type.Kind)
	}
}

func TestMediaBinding_ArgValidation(t *testing.T) {
	desc, err := schema.DescribeGoFunction("mediaFunc", mediaFunc)
	if err != nil {
		t.Fatalf("DescribeGoFunction: %v", err)
	}

	err = binding.ValidateInvocationArgs(desc, []any{
		map[string]any{"mime": "image/png", "src": "https://cdn.example.com/a.png"},
	})
	if err != nil {
		t.Fatalf("ValidateInvocationArgs: %v", err)
	}

	err = binding.ValidateInvocationArgs(desc, []any{
		map[string]any{"mime": "image/png", "src": "http://insecure.example.com/a.png"},
	})
	if err == nil {
		t.Fatal("expected rejection of non-whitelisted scheme")
	}
}

func TestMediaBinding_OversizedInlineGetsStableCode(t *testing.T) {
	desc, err := schema.DescribeGoFunction("mediaFunc", mediaFunc)
	if err != nil {
		t.Fatalf("DescribeGoFunction: %v", err)
	}

	oversized := "data:image/png;base64," + strings.Repeat("A", schema.MediaInlineLimit+1)
	err = binding.ValidateInvocationArgs(desc, []any{
		map[string]any{"mime": "image/png", "src": oversized},
	})
	if err == nil {
		t.Fatal("expected rejection of oversized inline media")
	}
	var contractErr *binding.ContractError
	if !asContractError(err, &contractErr) {
		t.Fatalf("expected ContractError, got %T: %v", err, err)
	}
	if contractErr.Code != schema.CodeMediaInlineTooLarge {
		t.Fatalf("code = %q, want %q", contractErr.Code, schema.CodeMediaInlineTooLarge)
	}
}

func asContractError(err error, target **binding.ContractError) bool {
	if e, ok := err.(*binding.ContractError); ok {
		*target = e
		return true
	}
	return false
}
