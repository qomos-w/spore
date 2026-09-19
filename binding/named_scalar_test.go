package binding

import (
	"testing"

	"github.com/qomos-w/spore/identity"
	"github.com/qomos-w/spore/schema"
)

func namedScalarTestID(t *testing.T, seq uint64) identity.CanonicalID {
	t.Helper()
	id, err := identity.NewCanonicalID(1, uint16(seq), 0, 1)
	if err != nil {
		t.Fatalf("NewCanonicalID: %v", err)
	}
	return id
}

type namedLevel int32

type namedEnumComp struct {
	Level namedLevel
	Scale float64
}

// TestProjectViewNormalisesNamedScalar 命名标量(枚举)字段投影必须归一化为
// 内建 Go 类型:脚本 VM 与 JSON 消费方不认识自定义类型的装箱值。
func TestProjectViewNormalisesNamedScalar(t *testing.T) {
	desc := schema.ObjectDesc{
		Name: "NamedEnumComp",
		Kind: schema.TypeKindStruct,
		Fields: []schema.FieldDesc{
			{Name: "Level", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}},
			{Name: "Scale", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "double"}},
		},
	}
	obj := namedEnumComp{Level: 3, Scale: 1.5}
	b, err := NewObjectBinding(desc, namedScalarTestID(t, 1), &obj)
	if err != nil {
		t.Fatalf("NewObjectBinding: %v", err)
	}
	view, err := ProjectView(b)
	if err != nil {
		t.Fatalf("ProjectView: %v", err)
	}
	if v, ok := view.Fields["Level"]; !ok || v != int32(3) {
		t.Fatalf("Level = %#v (type %T), want int32(3)", view.Fields["Level"], view.Fields["Level"])
	}
}

// TestApplyViewPatchIntegerToNamedScalar 整数视图值(脚本 VM 的 int64)必须能
// 写回命名整型字段(枚举),带范围守卫。
func TestApplyViewPatchIntegerToNamedScalar(t *testing.T) {
	desc := schema.ObjectDesc{
		Name: "NamedEnumComp",
		Kind: schema.TypeKindStruct,
		Fields: []schema.FieldDesc{
			{Name: "Level", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}},
		},
	}
	obj := namedEnumComp{Level: 0}
	b, err := NewObjectBinding(desc, namedScalarTestID(t, 2), &obj)
	if err != nil {
		t.Fatalf("NewObjectBinding: %v", err)
	}
	muts, err := ApplyViewPatch(b, &ViewProjection{Schema: desc, Fields: map[string]any{
		"Level": int64(3),
	}})
	if err != nil {
		t.Fatalf("ApplyViewPatch: %v", err)
	}
	if obj.Level != namedLevel(3) {
		t.Fatalf("Level = %v, want 3", obj.Level)
	}
	if len(muts) != 1 || muts[0].Kind != MutationReplaced {
		t.Fatalf("mutations = %+v, want one MutationReplaced", muts)
	}

	// 越界值必须拒绝而不是回绕。
	if _, err := ApplyViewPatch(b, &ViewProjection{Schema: desc, Fields: map[string]any{
		"Level": int64(1 << 40),
	}}); err != nil {
		t.Fatalf("out-of-range patch should be a skipped mutation, got error: %v", err)
	}
	if obj.Level != namedLevel(3) {
		t.Fatalf("Level changed by rejected patch: %v", obj.Level)
	}
}
