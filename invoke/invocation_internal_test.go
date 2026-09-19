package invoke

import (
	"testing"

	"github.com/qomos-w/spore/schema"
)

func TestInvocationValueForStageBranches(t *testing.T) {
	unary, err := schema.DescribeGoFunction("v", func() {})
	if err != nil {
		t.Fatalf("DescribeGoFunction: %v", err)
	}
	if v, err := invocationValueForStage(unary, InvocationStageUnary); err != nil || v != nil {
		t.Fatalf("void unary should produce nil value, got %v err=%v", v, err)
	}
	stream, err := schema.NewStreamingCallableDesc("s", nil,
		&schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"},
		&schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}, false)
	if err != nil {
		t.Fatalf("NewStreamingCallableDesc: %v", err)
	}
	if v, err := invocationValueForStage(stream, InvocationStageNext); err != nil || v == nil || v.Name != "string" {
		t.Fatalf("next value: %v err=%v", v, err)
	}
	if v, err := invocationValueForStage(stream, InvocationStageFinal); err != nil || v == nil || v.Name != "int" {
		t.Fatalf("final value: %v err=%v", v, err)
	}
	if _, err := invocationValueForStage(stream, InvocationStageUnary); err == nil {
		t.Fatal("expected stage validation error")
	}
}
