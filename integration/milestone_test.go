package integration_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/qomos-w/spore/binding"
	"github.com/qomos-w/spore/ecsbind"
	"github.com/qomos-w/spore/identity"
	"github.com/qomos-w/spore/internal/script/bytecode"
	frontend "github.com/qomos-w/spore/internal/script/frontend"
	"github.com/qomos-w/spore/runtime"
	"github.com/qomos-w/spore/schema"
	"github.com/qomos-w/spore/std"
	"github.com/qomos-w/spore/transport"
)

// ============================================================================
// Cross-plane milestone tests
//
// These tests verify that data flows correctly across the four planes
// (schema, binding, transport, identity) — not just within a single
// package. Each milestone corresponds to a complete data flow path.
//
// Per TDD §9.7, these tests establish "readiness-first 的跨平面里程碑检查"
// as opposed to only module-level unit tests.
// ============================================================================

// helpers

func mustID(t *testing.T, ts uint64, slot uint16, inc uint16, seq uint64) identity.CanonicalID {
	t.Helper()
	id, err := identity.NewCanonicalID(ts, slot, inc, seq)
	if err != nil {
		t.Fatalf("NewCanonicalID: %v", err)
	}
	return id
}

// ============================================================================
// Milestone 1: Go struct → schema → binding → transport 全链路数据投影
//
// 验证一个 Go struct 从 runtime 经过 schema 描述、binding 投影、
// transport 编码后，数据完整无损，identity 全链路一致。
// ============================================================================

type milestonePlayer struct {
	Name  string
	Level int
}

type milestoneLocation struct {
	Zone string
	X    int
	Y    int
}

type milestoneEntity struct {
	Name     string
	Level    int
	Location milestoneLocation
	Tags     []string
}

func milestonePlayerClassDesc() schema.ObjectDesc {
	return schema.ObjectDesc{
		Name: "milestonePlayer",
		Fields: []schema.FieldDesc{
			{Name: "Name", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
			{Name: "Level", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}},
		},
	}
}

func milestoneEntityClassDesc() schema.ObjectDesc {
	return schema.ObjectDesc{
		Name: "milestoneEntity",
		Fields: []schema.FieldDesc{
			{Name: "Name", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
			{Name: "Level", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}},
			{Name: "Location", Type: schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "struct", ClassName: "milestoneLocation"}},
			{Name: "Tags", Type: schema.TypeDesc{Kind: schema.TypeKindArray, Name: "array", Element: &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}}},
		},
	}
}

func TestMilestone_StructProjectionRoundTrip(t *testing.T) {
	id := mustID(t, 1000, 1, 0, 1)
	codec := &transport.JSONCodec{}

	p := milestonePlayer{Name: "alice", Level: 5}
	desc := milestonePlayerClassDesc()

	// Step 1: Create binding
	b, err := binding.NewObjectBinding(desc, id, &p)
	if err != nil {
		t.Fatalf("NewObjectBinding: %v", err)
	}

	// Step 2: Project view
	view, err := binding.ProjectView(b)
	if err != nil {
		t.Fatalf("ProjectView: %v", err)
	}

	// Step 3: Encode to transport
	structTypeDesc := schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "struct", ClassName: "milestonePlayer"}
	tv, err := codec.Encode(structTypeDesc, id, view.Fields)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	// Step 4: Decode from transport
	decoded, err := codec.Decode(tv)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	decodedMap, ok := decoded.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", decoded)
	}

	// Step 5: Verify scalar field values survived the round-trip
	if decodedMap["Name"] != "alice" {
		t.Fatalf("expected Name=alice, got %v", decodedMap["Name"])
	}
	// JSON numbers decode as float64
	if level, ok := decodedMap["Level"].(float64); !ok || int(level) != 5 {
		t.Fatalf("expected Level=5, got %v", decodedMap["Level"])
	}

	// Step 6: Construct ViewProjection from decoded data and patch back
	p2 := milestonePlayer{Name: "original", Level: 0}
	b2, err := binding.NewObjectBinding(desc, id, &p2)
	if err != nil {
		t.Fatalf("NewObjectBinding p2: %v", err)
	}

	patchView := &binding.ViewProjection{
		Schema:   desc,
		Identity: id,
		Fields:   decodedMap,
	}
	mutations, err := binding.ApplyViewPatch(b2, patchView)
	if err != nil {
		t.Fatalf("ApplyViewPatch: %v", err)
	}

	if p2.Name != "alice" {
		t.Fatalf("expected p2.Name=alice after patch, got %s", p2.Name)
	}
	if p2.Level != 5 {
		t.Fatalf("expected p2.Level=5 after patch, got %d", p2.Level)
	}
	if len(mutations) == 0 {
		t.Fatal("expected at least one mutation from patch")
	}
}

func TestMilestone_StructProjectionRoundTrip_FractionalIntRejected(t *testing.T) {
	id := mustID(t, 1000, 1, 0, 11)
	desc := milestonePlayerClassDesc()

	p := milestonePlayer{Name: "original", Level: 3}
	b, err := binding.NewObjectBinding(desc, id, &p)
	if err != nil {
		t.Fatalf("NewObjectBinding: %v", err)
	}

	patchView := &binding.ViewProjection{
		Schema:   desc,
		Identity: id,
		Fields: map[string]any{
			"Level": float64(5.5),
		},
	}

	mutations, err := binding.ApplyViewPatch(b, patchView)
	if err != nil {
		t.Fatalf("ApplyViewPatch: %v", err)
	}
	if p.Level != 3 {
		t.Fatalf("expected Level to remain 3 after fractional patch, got %d", p.Level)
	}
	for _, mutation := range mutations {
		if mutation.Key == "Level" {
			if mutation.Kind != binding.MutationSkipped {
				t.Fatalf("expected Level mutation to be skipped, got %s", mutation.Kind)
			}
			if mutation.Reason == "" {
				t.Fatal("expected MutationSkipped to carry diagnostic Reason")
			}
		}
	}
}

func TestMilestone_NestedStructProjectionRoundTrip(t *testing.T) {
	id := mustID(t, 1000, 1, 0, 2)
	codec := &transport.JSONCodec{}

	e := milestoneEntity{
		Name:  "hero",
		Level: 10,
		Location: milestoneLocation{
			Zone: "forest",
			X:    100,
			Y:    200,
		},
		Tags: []string{"warrior", "ranged"},
	}
	desc := milestoneEntityClassDesc()

	b, err := binding.NewObjectBinding(desc, id, &e)
	if err != nil {
		t.Fatalf("NewObjectBinding: %v", err)
	}

	view, err := binding.ProjectView(b)
	if err != nil {
		t.Fatalf("ProjectView: %v", err)
	}

	// Verify nested struct is projected as map[string]any
	loc, ok := view.Fields["Location"].(map[string]any)
	if !ok {
		t.Fatalf("expected Location as map[string]any, got %T", view.Fields["Location"])
	}
	if loc["Zone"] != "forest" {
		t.Fatalf("expected Zone=forest, got %v", loc["Zone"])
	}

	// Verify slice field
	tags, ok := view.Fields["Tags"].([]string)
	if !ok {
		t.Fatalf("expected Tags as []string, got %T", view.Fields["Tags"])
	}
	if len(tags) != 2 || tags[0] != "warrior" || tags[1] != "ranged" {
		t.Fatalf("unexpected Tags value: %v", tags)
	}

	// Encode → Decode
	structTypeDesc := schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "struct", ClassName: "milestoneEntity"}
	tv, err := codec.Encode(structTypeDesc, id, view.Fields)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	decoded, err := codec.Decode(tv)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	decodedMap, ok := decoded.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", decoded)
	}

	// Verify nested struct survived
	decodedLoc, ok := decodedMap["Location"].(map[string]any)
	if !ok {
		t.Fatalf("expected decoded Location as map[string]any, got %T", decodedMap["Location"])
	}
	if decodedLoc["Zone"] != "forest" {
		t.Fatalf("expected Zone=forest, got %v", decodedLoc["Zone"])
	}
}

// ============================================================================
// Milestone 2: OrderedMap → binding → transport 顺序保持
//
// 验证 OrderedMap 的插入顺序在 binding 投影和 transport 编码后完整保留。
// ============================================================================

type milestoneMapHolder struct {
	Name   string
	Props  map[string]int
	Traits *schema.OrderedMap[string, string]
}

func milestoneMapHolderClassDesc() schema.ObjectDesc {
	return schema.ObjectDesc{
		Name: "milestoneMapHolder",
		Fields: []schema.FieldDesc{
			{Name: "Name", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
			{Name: "Props", Type: schema.TypeDesc{Kind: schema.TypeKindMap, Name: "map", Key: &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}, Value: &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}}},
			{Name: "Traits", Type: schema.TypeDesc{Kind: schema.TypeKindMap, Name: "map", Key: &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}, Value: &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}}},
		},
	}
}

func TestMilestone_OrderedMapProjectionChain(t *testing.T) {
	id := mustID(t, 1000, 2, 0, 1)
	codec := &transport.JSONCodec{}

	om := schema.NewOrderedMap[string, string]()
	om.Set("z_trait", "zeal")
	om.Set("a_trait", "agility")
	om.Set("m_trait", "might")

	holder := milestoneMapHolder{
		Name:   "hero",
		Traits: om,
	}
	desc := milestoneMapHolderClassDesc()

	b, err := binding.NewObjectBinding(desc, id, &holder)
	if err != nil {
		t.Fatalf("NewObjectBinding: %v", err)
	}

	// Step 1: Project to ViewProjection
	view, err := binding.ProjectView(b)
	if err != nil {
		t.Fatalf("ProjectView: %v", err)
	}

	// Step 2: Verify entry sequence with insertion order
	entries, ok := view.Fields["Traits"].([]any)
	if !ok {
		t.Fatalf("expected Traits as []any entry sequence, got %T", view.Fields["Traits"])
	}

	expectedKeys := []string{"z_trait", "a_trait", "m_trait"}
	for i, entry := range entries {
		entryMap, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("entry %d: expected map[string]any, got %T", i, entry)
		}
		key, _ := entryMap["Key"].(string)
		if key != expectedKeys[i] {
			t.Fatalf("entry %d: expected key %q (insertion order), got %q", i, expectedKeys[i], key)
		}
	}

	// Step 3: Encode → Decode through transport
	mapDesc := schema.TypeDesc{
		Kind:  schema.TypeKindMap,
		Name:  "map",
		Key:   &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"},
		Value: &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"},
	}
	tv, err := codec.Encode(mapDesc, id, entries)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	decoded, err := codec.Decode(tv)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	// Step 4: Verify decoded entry sequence preserves insertion order
	decodedEntries, ok := decoded.([]any)
	if !ok {
		t.Fatalf("expected []any, got %T", decoded)
	}
	if len(decodedEntries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(decodedEntries))
	}
	for i, entry := range decodedEntries {
		entryMap, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("decoded entry %d: expected map[string]any, got %T", i, entry)
		}
		key, _ := entryMap["Key"].(string)
		if key != expectedKeys[i] {
			t.Fatalf("decoded entry %d: expected key %q, got %q (insertion order not preserved through transport)", i, expectedKeys[i], key)
		}
	}
}

func TestMilestone_PlainGoMapProjectionChain(t *testing.T) {
	id := mustID(t, 1000, 2, 0, 2)
	codec := &transport.JSONCodec{}

	holder := milestoneMapHolder{
		Name:  "hero",
		Props: map[string]int{"zeal": 10, "agility": 5, "might": 8},
	}
	desc := milestoneMapHolderClassDesc()

	b, err := binding.NewObjectBinding(desc, id, &holder)
	if err != nil {
		t.Fatalf("NewObjectBinding: %v", err)
	}

	view, err := binding.ProjectView(b)
	if err != nil {
		t.Fatalf("ProjectView: %v", err)
	}

	// Plain Go map should be projected as sorted-key entry sequence
	entries, ok := view.Fields["Props"].([]any)
	if !ok {
		t.Fatalf("expected Props as []any entry sequence, got %T", view.Fields["Props"])
	}

	// Sorted order: agility, might, zeal
	expectedKeys := []string{"agility", "might", "zeal"}
	for i, entry := range entries {
		entryMap, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("entry %d: expected map[string]any, got %T", i, entry)
		}
		key, _ := entryMap["Key"].(string)
		if key != expectedKeys[i] {
			t.Fatalf("entry %d: expected key %q (sorted order), got %q", i, expectedKeys[i], key)
		}
	}

	// Encode → Decode
	mapDesc := schema.TypeDesc{
		Kind:  schema.TypeKindMap,
		Name:  "map",
		Key:   &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"},
		Value: &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"},
	}
	tv, err := codec.Encode(mapDesc, id, entries)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	decoded, err := codec.Decode(tv)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	decodedEntries, ok := decoded.([]any)
	if !ok {
		t.Fatalf("expected []any, got %T", decoded)
	}
	// Verify sorted order preserved through transport
	for i, entry := range decodedEntries {
		entryMap, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("decoded entry %d: expected map[string]any, got %T", i, entry)
		}
		key, _ := entryMap["Key"].(string)
		if key != expectedKeys[i] {
			t.Fatalf("decoded entry %d: expected key %q, got %q (sorted order not preserved through transport)", i, expectedKeys[i], key)
		}
	}
}

func TestMilestone_SortedOrderedMapProjectionChain(t *testing.T) {
	id := mustID(t, 1000, 2, 0, 3)
	codec := &transport.JSONCodec{}

	type sortedHolder struct {
		Name     string
		Priority *schema.SortedOrderedMap[string, int]
	}

	sm := schema.NewSortedOrderedMap[string, int](schema.NaturalOrder[string]())
	sm.Set("gamma", 3)
	sm.Set("alpha", 1)
	sm.Set("beta", 2)

	holder := sortedHolder{Name: "test", Priority: sm}

	desc := schema.ObjectDesc{
		Name: "sortedHolder",
		Fields: []schema.FieldDesc{
			{Name: "Name", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
			{Name: "Priority", Type: schema.TypeDesc{Kind: schema.TypeKindMap, Name: "map"}},
		},
	}

	b, err := binding.NewObjectBinding(desc, id, &holder)
	if err != nil {
		t.Fatalf("NewObjectBinding: %v", err)
	}

	view, err := binding.ProjectView(b)
	if err != nil {
		t.Fatalf("ProjectView: %v", err)
	}

	// Verify sorted order: alpha, beta, gamma
	entries, ok := view.Fields["Priority"].([]any)
	if !ok {
		t.Fatalf("expected Priority as []any, got %T", view.Fields["Priority"])
	}

	expectedKeys := []string{"alpha", "beta", "gamma"}
	for i, entry := range entries {
		entryMap, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("entry %d: expected map[string]any, got %T", i, entry)
		}
		key, _ := entryMap["Key"].(string)
		if key != expectedKeys[i] {
			t.Fatalf("entry %d: expected key %q (sorted order), got %q", i, expectedKeys[i], key)
		}
	}

	// Encode → Decode
	mapDesc := schema.TypeDesc{
		Kind:  schema.TypeKindMap,
		Name:  "map",
		Key:   &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"},
		Value: &schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"},
	}
	tv, err := codec.Encode(mapDesc, id, entries)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	decoded, err := codec.Decode(tv)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	decodedEntries, ok := decoded.([]any)
	if !ok {
		t.Fatalf("expected []any, got %T", decoded)
	}
	for i, entry := range decodedEntries {
		entryMap, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("decoded entry %d: expected map[string]any, got %T", i, entry)
		}
		key, _ := entryMap["Key"].(string)
		if key != expectedKeys[i] {
			t.Fatalf("decoded entry %d: expected key %q, got %q (sorted order not preserved through transport)", i, expectedKeys[i], key)
		}
	}
}

// ============================================================================
// Milestone 3: Go function → schema → callable invocation → outcome 全链路
//
// 验证 Go function 从 schema 描述到 callable 注册到 invocation 执行的完整链路。
// ============================================================================

func TestMilestone_FunctionBindingAndInvocation(t *testing.T) {
	sb := binding.NewScriptBinding()

	// Step 1: Register a Go function
	err := sb.BindFunction("greet", func(name string) string {
		return "hello " + name
	})
	if err != nil {
		t.Fatalf("BindFunction: %v", err)
	}

	// Step 2: Verify callable descriptor is registered
	desc, ok := sb.Callables.Lookup("greet")
	if !ok {
		t.Fatal("expected to find registered callable")
	}
	if desc.Name != "greet" {
		t.Fatalf("expected callable name greet, got %s", desc.Name)
	}
	if desc.Mode != schema.CallableModeUnary {
		t.Fatalf("expected unary mode, got %s", desc.Mode)
	}
	if len(desc.Parameters) != 1 {
		t.Fatalf("expected 1 parameter, got %d", len(desc.Parameters))
	}
	if desc.Parameters[0].Type.Kind != schema.TypeKindScalar || desc.Parameters[0].Type.Name != "string" {
		t.Fatalf("expected string parameter, got %v", desc.Parameters[0].Type)
	}

	// Step 3: Invoke successfully
	outcome, err := sb.Invoke(binding.InvocationRequest{
		Callable: "greet",
		Stage:    binding.InvocationStageUnary,
		Args:     []any{"world"},
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}

	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Result.Callable != "greet" {
		t.Fatalf("expected callable=greet, got %s", outcome.Result.Callable)
	}
	if outcome.Payload == nil || outcome.Payload.Value != "hello world" {
		t.Fatalf("expected payload 'hello world', got %v", outcome.Payload)
	}

	// Step 4: Verify schema-described return type
	if len(desc.Returns) != 1 {
		t.Fatalf("expected 1 return type, got %d", len(desc.Returns))
	}
	if desc.Returns[0].Kind != schema.TypeKindScalar || desc.Returns[0].Name != "string" {
		t.Fatalf("expected string return type, got %v", desc.Returns[0])
	}
}

func TestMilestone_FunctionInvocationErrorContext(t *testing.T) {
	sb := binding.NewScriptBinding()

	err := sb.BindFunction("fail", func() (string, error) {
		return "", testMilestoneErr("boom")
	})
	if err != nil {
		t.Fatalf("BindFunction: %v", err)
	}

	outcome, err := sb.Invoke(binding.InvocationRequest{
		Callable: "fail",
		Stage:    binding.InvocationStageUnary,
		Args:     []any{},
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}

	// Error invocation should produce error result with context
	if outcome.Result.Kind != binding.InvocationResultError {
		t.Fatalf("expected error result, got %s", outcome.Result.Kind)
	}
	if outcome.Result.Callable != "fail" {
		t.Fatalf("expected callable=fail, got %s", outcome.Result.Callable)
	}
	if outcome.Result.Error == nil {
		t.Fatal("expected error descriptor")
	}
	if outcome.Result.Error.Message != "boom" {
		t.Fatalf("expected error message 'boom', got %s", outcome.Result.Error.Message)
	}
	if outcome.Payload != nil {
		t.Fatalf("expected nil payload for error result, got %+v", outcome.Payload)
	}
}

type testMilestoneErr string

func (e testMilestoneErr) Error() string { return string(e) }

// ============================================================================
// Milestone 4: 跨平面 identity 一致性
//
// 验证 CanonicalID 在 binding ObjectBinding、ViewProjection、
// transport View 之间一致传递。
// ============================================================================

func TestMilestone_IdentityConsistency(t *testing.T) {
	id := mustID(t, 5000, 3, 0, 42)
	codec := &transport.JSONCodec{}

	p := milestonePlayer{Name: "hero", Level: 10}
	desc := milestonePlayerClassDesc()

	// Step 1: Create binding — identity attached
	b, err := binding.NewObjectBinding(desc, id, &p)
	if err != nil {
		t.Fatalf("NewObjectBinding: %v", err)
	}
	if b.Identity != id {
		t.Fatal("ObjectBinding identity mismatch")
	}

	// Step 2: Project view — identity carried forward
	view, err := binding.ProjectView(b)
	if err != nil {
		t.Fatalf("ProjectView: %v", err)
	}
	if view.Identity != id {
		t.Fatal("ViewProjection identity mismatch — identity lost at binding→view boundary")
	}

	// Step 3: Encode to transport — identity carried to wire
	structTypeDesc := schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "struct", ClassName: "milestonePlayer"}
	tv, err := codec.Encode(structTypeDesc, id, view.Fields)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if tv.Identity != id {
		t.Fatal("transport View identity mismatch — identity lost at view→transport boundary")
	}

	// Step 4: Decode from transport — identity still accessible on the View
	if tv.Identity.IsZero() {
		t.Fatal("transport View identity is zero after decode")
	}
	if tv.Identity != id {
		t.Fatalf("transport View identity after decode: expected %s, got %s", id, tv.Identity)
	}

	// Step 5: All three planes carry the same identity value
	if b.Identity != view.Identity {
		t.Fatal("binding→view identity drift")
	}
	if view.Identity != tv.Identity {
		t.Fatal("view→transport identity drift")
	}
}

func TestMilestone_DiffDetectsChangeAcrossProjection(t *testing.T) {
	id := mustID(t, 1000, 4, 0, 1)
	desc := milestonePlayerClassDesc()

	p := milestonePlayer{Name: "alice", Level: 5}
	b, err := binding.NewObjectBinding(desc, id, &p)
	if err != nil {
		t.Fatalf("NewObjectBinding: %v", err)
	}

	// Project before
	before, err := binding.ProjectView(b)
	if err != nil {
		t.Fatalf("ProjectView: %v", err)
	}

	// Mutate runtime
	p.Level = 99

	// Project after
	after, err := binding.ProjectView(b)
	if err != nil {
		t.Fatalf("ProjectView: %v", err)
	}

	// Diff should detect the change
	diff := binding.DiffViewProjection(before, after)
	if len(diff) != 1 {
		t.Fatalf("expected 1 change, got %d", len(diff))
	}
	if diff[0].Kind != binding.MutationReplaced {
		t.Fatalf("expected MutationReplaced, got %s", diff[0].Kind)
	}
	if diff[0].Key != "Level" {
		t.Fatalf("expected key Level, got %s", diff[0].Key)
	}
}

// ============================================================================
// Milestone 5: Runtime Carrier → Schema Binding → Transport 全链路
//
// 验证 runtime carrier 中的 entity component 可以经过 schema binding
// 投影到 transport view，并且 identity 在全链路一致。
// 这是 Phase 5 的核心跨平面闭环证据。
// ============================================================================

type milestonePosition struct {
	X float64
	Y float64
}

type milestoneVelocity struct {
	DX float64
	DY float64
}

type milestoneHealth struct {
	Value int
}

func milestonePositionClassDesc() schema.ObjectDesc {
	return schema.ObjectDesc{
		Name: "milestonePosition",
		Fields: []schema.FieldDesc{
			{Name: "X", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "double"}},
			{Name: "Y", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "double"}},
		},
	}
}

func milestoneVelocityClassDesc() schema.ObjectDesc {
	return schema.ObjectDesc{
		Name: "milestoneVelocity",
		Fields: []schema.FieldDesc{
			{Name: "DX", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "double"}},
			{Name: "DY", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "double"}},
		},
	}
}

func milestoneHealthClassDesc() schema.ObjectDesc {
	return schema.ObjectDesc{
		Name: "milestoneHealth",
		Fields: []schema.FieldDesc{
			{Name: "Value", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}},
		},
	}
}

func TestMilestone_RuntimeCarrier_EntityProjectionRoundTrip(t *testing.T) {
	w := runtime.NewWorld(runtime.WithSlot(1))
	codec := &transport.JSONCodec{}
	posDesc := milestonePositionClassDesc()

	// Step 1: Create entity with Position component
	e := w.Create()
	pos := &milestonePosition{X: 100.5, Y: 200.3}
	w.SetComponent(e, "Position", pos)

	// Step 2: Project entity component to transport view
	tv, err := ecsbind.ProjectEntityToTransport(w, e, "Position", posDesc, codec)
	if err != nil {
		t.Fatalf("ProjectEntityToTransport: %v", err)
	}

	// Step 3: Verify identity consistency
	if tv.Identity != e.ID() {
		t.Fatal("transport view identity mismatch — identity lost at runtime→transport boundary")
	}

	// Step 4: Decode from transport
	decoded, err := codec.Decode(tv)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	decodedMap, ok := decoded.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", decoded)
	}
	if decodedMap["X"] != 100.5 {
		t.Fatalf("expected X=100.5, got %v", decodedMap["X"])
	}
	if decodedMap["Y"] != 200.3 {
		t.Fatalf("expected Y=200.3, got %v", decodedMap["Y"])
	}

	// Step 5: Patch back to a different entity
	e2 := w.Create()
	pos2 := &milestonePosition{X: 0, Y: 0}
	w.SetComponent(e2, "Position", pos2)

	patchView := &binding.ViewProjection{
		Schema:   posDesc,
		Identity: e2.ID(),
		Fields:   decodedMap,
	}
	mutations, err := ecsbind.PatchEntity(w, e2, "Position", posDesc, patchView)
	if err != nil {
		t.Fatalf("PatchEntity: %v", err)
	}
	if len(mutations) == 0 {
		t.Fatal("expected at least one mutation from patch")
	}
	if pos2.X != 100.5 {
		t.Fatalf("expected pos2.X=100.5 after round-trip, got %f", pos2.X)
	}
}

func TestMilestone_RuntimeCarrier_QueryAndChangeTracking(t *testing.T) {
	w := runtime.NewWorld()

	// Create entities with different components
	e1 := w.Create()
	w.SetComponent(e1, "Position", &milestonePosition{X: 10, Y: 20})
	w.SetComponent(e1, "Velocity", &milestoneVelocity{DX: 1, DY: -1})

	e2 := w.Create()
	w.SetComponent(e2, "Position", &milestonePosition{X: 30, Y: 40})

	_ = w.Create() // entity with no components

	// Query: entities with Position AND Velocity
	q := runtime.NewQuery().Has("Position", "Velocity")
	result := w.Execute(q)
	if len(result) != 1 || result[0].ID() != e1.ID() {
		t.Fatalf("expected 1 entity with Position+Velocity (e1), got %d", len(result))
	}

	// Query: entities with Position but NOT Velocity
	q2 := runtime.NewQuery().Has("Position").HasNone("Velocity")
	result2 := w.Execute(q2)
	if len(result2) != 1 || result2[0].ID() != e2.ID() {
		t.Fatalf("expected 1 entity with Position only (e2), got %d", len(result2))
	}

	// Change tracking: mutate and observe
	w.ClearChanges(e1)
	posAny, _ := w.GetComponent(e1, "Position")
	pos := posAny.(*milestonePosition)
	pos.X = 99
	w.MarkChanged(e1, "Position")

	cs := w.ChangeSet(e1)
	if len(cs.Changed) != 1 || cs.Changed[0] != "Position" {
		t.Fatalf("expected Position in Changed, got %v", cs.Changed)
	}

	// Query changed entities
	q3 := runtime.NewQuery().WhenChanged("Position")
	result3 := w.Execute(q3)
	if len(result3) != 1 || result3[0].ID() != e1.ID() {
		t.Fatalf("expected 1 entity with Position changed, got %d", len(result3))
	}
}

func TestMilestone_RuntimeCarrier_EntityLifecycleWithProjection(t *testing.T) {
	w := runtime.NewWorld()
	codec := &transport.JSONCodec{}
	posDesc := milestonePositionClassDesc()

	// Create, project, dispose, verify projection fails
	e := w.Create()
	w.SetComponent(e, "Position", &milestonePosition{X: 50, Y: 60})

	// Should work before dispose
	_, err := ecsbind.ProjectEntityToTransport(w, e, "Position", posDesc, codec)
	if err != nil {
		t.Fatalf("ProjectEntityToTransport before dispose: %v", err)
	}

	// Dispose
	w.Dispose(e)

	// Should fail after dispose
	_, err = ecsbind.ProjectEntityToTransport(w, e, "Position", posDesc, codec)
	if err == nil {
		t.Fatal("expected error projecting disposed entity, got nil")
	}

	// Entity count should reflect the disposal
	if w.EntityCount() != 0 {
		t.Fatalf("expected 0 alive entities, got %d", w.EntityCount())
	}
}

func TestMilestone_RuntimeCarrier_IdentityConsistencyAcrossPlanes(t *testing.T) {
	w := runtime.NewWorld(runtime.WithSlot(7), runtime.WithIncarnation(2))
	codec := &transport.JSONCodec{}
	posDesc := milestonePositionClassDesc()

	e := w.Create()
	w.SetComponent(e, "Position", &milestonePosition{X: 1, Y: 2})

	// Project to binding view
	view, err := ecsbind.ProjectEntity(w, e, "Position", posDesc)
	if err != nil {
		t.Fatalf("ProjectEntity: %v", err)
	}

	// Verify identity at each plane
	if e.ID() != view.Identity {
		t.Fatal("entity→view identity drift")
	}

	// Project to transport
	tv, err := ecsbind.ProjectEntityToTransport(w, e, "Position", posDesc, codec)
	if err != nil {
		t.Fatalf("ProjectEntityToTransport: %v", err)
	}

	if view.Identity != tv.Identity {
		t.Fatal("view→transport identity drift")
	}

	// Verify the ID was generated with the correct slot and incarnation
	if e.ID().RuntimeSlot() != 7 {
		t.Fatalf("expected RuntimeSlot=7, got %d", e.ID().RuntimeSlot())
	}
	if e.ID().Incarnation() != 2 {
		t.Fatalf("expected Incarnation=2, got %d", e.ID().Incarnation())
	}
}

func TestMilestone_RuntimeCarrier_StateChangeProjectionToTransportPatch(t *testing.T) {
	w := runtime.NewWorld()
	codec := &transport.JSONCodec{}
	componentDescs := map[string]schema.ObjectDesc{
		"Position": milestonePositionClassDesc(),
		"Velocity": milestoneVelocityClassDesc(),
		"Health":   milestoneHealthClassDesc(),
	}

	e := w.Create()
	if err := w.SetComponent(e, "Position", &milestonePosition{X: 10, Y: 20}); err != nil {
		t.Fatalf("SetComponent Position: %v", err)
	}
	if err := w.SetComponent(e, "Velocity", &milestoneVelocity{DX: 1, DY: 2}); err != nil {
		t.Fatalf("SetComponent Velocity: %v", err)
	}
	w.ClearChanges(e)

	posAny, _ := w.GetComponent(e, "Position")
	pos := posAny.(*milestonePosition)
	pos.X = 99
	w.MarkChanged(e, "Position")
	w.RemoveComponent(e, "Velocity")
	if err := w.SetComponent(e, "Health", &milestoneHealth{Value: 100}); err != nil {
		t.Fatalf("SetComponent Health: %v", err)
	}

	tv, err := ecsbind.ProjectEntityChangesToTransport(w, e, componentDescs, codec)
	if err != nil {
		t.Fatalf("ProjectEntityChangesToTransport: %v", err)
	}
	if tv.Kind != transport.ViewKindPatch {
		t.Fatalf("expected ViewKindPatch, got %s", tv.Kind)
	}
	if tv.Identity != e.ID() {
		t.Fatal("transport patch view identity mismatch")
	}

	decoded, err := codec.Decode(tv)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	decodedMap, ok := decoded.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", decoded)
	}

	added, ok := decodedMap["Added"].(map[string]any)
	if !ok {
		t.Fatalf("expected Added as map[string]any, got %T", decodedMap["Added"])
	}
	health, ok := added["Health"].(map[string]any)
	if !ok {
		t.Fatalf("expected Health add payload as map[string]any, got %T", added["Health"])
	}
	if health["Value"] != 100.0 {
		t.Fatalf("expected Health.Value=100, got %v", health["Value"])
	}

	changed, ok := decodedMap["Changed"].(map[string]any)
	if !ok {
		t.Fatalf("expected Changed as map[string]any, got %T", decodedMap["Changed"])
	}
	position, ok := changed["Position"].(map[string]any)
	if !ok {
		t.Fatalf("expected Position change payload as map[string]any, got %T", changed["Position"])
	}
	if position["X"] != 99.0 {
		t.Fatalf("expected Position.X=99, got %v", position["X"])
	}
	if position["Y"] != 20.0 {
		t.Fatalf("expected Position.Y=20, got %v", position["Y"])
	}
	if _, ok := changed["Health"]; ok {
		t.Fatal("did not expect added component Health under Changed")
	}

	removed, ok := decodedMap["Removed"].([]any)
	if !ok {
		t.Fatalf("expected Removed as []any, got %T", decodedMap["Removed"])
	}
	if len(removed) != 1 || removed[0] != "Velocity" {
		t.Fatalf("expected Removed=[Velocity], got %v", removed)
	}
}

func TestMilestone_SporeClosure_RuntimeCarrierCallableAndTransport(t *testing.T) {
	w := runtime.NewWorld(runtime.WithSlot(3))
	codec := &transport.JSONCodec{}
	sb := binding.NewScriptBinding()

	boost := func(base int, bonus int) int {
		return base + bonus
	}
	if err := sb.BindFunction("boost", boost); err != nil {
		t.Fatalf("BindFunction: %v", err)
	}

	e := w.Create()
	health := &milestoneHealth{Value: 10}
	if err := w.SetComponent(e, "Health", health); err != nil {
		t.Fatalf("SetComponent Health: %v", err)
	}
	w.ClearChanges(e)

	outcome, err := sb.Invoke(binding.InvocationRequest{
		Callable: "boost",
		Stage:    binding.InvocationStageUnary,
		Args:     []any{health.Value, 5},
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil {
		t.Fatal("expected invocation payload")
	}
	result, ok := outcome.Payload.Value.(int)
	if !ok {
		t.Fatalf("expected int payload, got %T", outcome.Payload.Value)
	}
	if result != 15 {
		t.Fatalf("expected boost result 15, got %d", result)
	}

	health.Value = result
	w.MarkChanged(e, "Health")

	healthDesc := milestoneHealthClassDesc()
	fullView, err := ecsbind.ProjectEntityToTransport(w, e, "Health", healthDesc, codec)
	if err != nil {
		t.Fatalf("ProjectEntityToTransport: %v", err)
	}
	if fullView.Kind != transport.ViewKindFull {
		t.Fatalf("expected full transport view, got %s", fullView.Kind)
	}
	fullDecoded, err := codec.Decode(fullView)
	if err != nil {
		t.Fatalf("Decode full view: %v", err)
	}
	fullMap, ok := fullDecoded.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any from full view, got %T", fullDecoded)
	}
	if fullMap["Value"] != 15.0 {
		t.Fatalf("expected full view Value=15, got %v", fullMap["Value"])
	}

	patchView, err := ecsbind.ProjectEntityChangesToTransport(w, e, map[string]schema.ObjectDesc{"Health": healthDesc}, codec)
	if err != nil {
		t.Fatalf("ProjectEntityChangesToTransport: %v", err)
	}
	if patchView.Kind != transport.ViewKindPatch {
		t.Fatalf("expected patch transport view, got %s", patchView.Kind)
	}
	if patchView.Identity != e.ID() || fullView.Identity != e.ID() {
		t.Fatal("expected identity consistency across runtime and transport views")
	}
	patchDecoded, err := codec.Decode(patchView)
	if err != nil {
		t.Fatalf("Decode patch view: %v", err)
	}
	patchMap, ok := patchDecoded.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any from patch view, got %T", patchDecoded)
	}
	changed, ok := patchMap["Changed"].(map[string]any)
	if !ok {
		t.Fatalf("expected Changed bucket, got %T", patchMap["Changed"])
	}
	changedHealth, ok := changed["Health"].(map[string]any)
	if !ok {
		t.Fatalf("expected Health change payload, got %T", changed["Health"])
	}
	if changedHealth["Value"] != 15.0 {
		t.Fatalf("expected patch Value=15, got %v", changedHealth["Value"])
	}
}

func TestMilestone_ExternalConsumer_UsesOnlySporeSurfaces(t *testing.T) {
	type objectSurface struct {
		classDesc schema.ObjectDesc
		target    *milestoneHealth
	}

	type consumer struct {
		binding *binding.ScriptBinding
		codec   transport.Codec
	}

	applyCallableToObject := func(c consumer, obj objectSurface, callable string, args []any) (transport.View, transport.View, error) {
		id := mustID(t, 3000, 9, 0, 1)
		w := runtime.NewWorld()
		carrier := w.CreateWithID(id)
		if err := w.SetComponent(carrier, obj.classDesc.Name, obj.target); err != nil {
			return transport.View{}, transport.View{}, err
		}
		w.ClearChanges(carrier)

		outcome, err := c.binding.Invoke(binding.InvocationRequest{
			Callable: callable,
			Stage:    binding.InvocationStageUnary,
			Args:     args,
		})
		if err != nil {
			return transport.View{}, transport.View{}, err
		}
		value, ok := outcome.Payload.Value.(int)
		if !ok {
			return transport.View{}, transport.View{}, fmt.Errorf("expected int payload, got %T", outcome.Payload.Value)
		}
		obj.target.Value = value
		w.MarkChanged(carrier, obj.classDesc.Name)

		fullView, err := ecsbind.ProjectEntityToTransport(w, carrier, obj.classDesc.Name, obj.classDesc, c.codec)
		if err != nil {
			return transport.View{}, transport.View{}, err
		}
		patchView, err := ecsbind.ProjectEntityChangesToTransport(w, carrier, map[string]schema.ObjectDesc{obj.classDesc.Name: obj.classDesc}, c.codec)
		if err != nil {
			return transport.View{}, transport.View{}, err
		}
		return fullView, patchView, nil
	}

	sb := binding.NewScriptBinding()
	if err := sb.BindFunction("boost", func(base int, bonus int) int { return base + bonus }); err != nil {
		t.Fatalf("BindFunction: %v", err)
	}
	consumerAPI := consumer{binding: sb, codec: &transport.JSONCodec{}}
	healthObj := objectSurface{classDesc: milestoneHealthClassDesc(), target: &milestoneHealth{Value: 7}}

	fullView, patchView, err := applyCallableToObject(consumerAPI, healthObj, "boost", []any{healthObj.target.Value, 5})
	if err != nil {
		t.Fatalf("applyCallableToObject: %v", err)
	}
	if fullView.Kind != transport.ViewKindFull {
		t.Fatalf("expected full view, got %s", fullView.Kind)
	}
	if patchView.Kind != transport.ViewKindPatch {
		t.Fatalf("expected patch view, got %s", patchView.Kind)
	}
	if fullView.Identity != patchView.Identity {
		t.Fatal("expected external consumer to preserve identity across returned views")
	}

	fullDecoded, err := consumerAPI.codec.Decode(fullView)
	if err != nil {
		t.Fatalf("Decode full view: %v", err)
	}
	fullMap, ok := fullDecoded.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any from full view, got %T", fullDecoded)
	}
	if fullMap["Value"] != 12.0 {
		t.Fatalf("expected full view Value=12, got %v", fullMap["Value"])
	}

	patchDecoded, err := consumerAPI.codec.Decode(patchView)
	if err != nil {
		t.Fatalf("Decode patch view: %v", err)
	}
	patchMap, ok := patchDecoded.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any from patch view, got %T", patchDecoded)
	}
	changed, ok := patchMap["Changed"].(map[string]any)
	if !ok {
		t.Fatalf("expected Changed bucket, got %T", patchMap["Changed"])
	}
	changedHealth, ok := changed[healthObj.classDesc.Name].(map[string]any)
	if !ok {
		t.Fatalf("expected health change payload, got %T", changed[healthObj.classDesc.Name])
	}
	if changedHealth["Value"] != 12.0 {
		t.Fatalf("expected patch Value=12, got %v", changedHealth["Value"])
	}
}

func TestMilestone_ExternalConsumer_DiagnosabilityAcrossCallableAndTransport(t *testing.T) {
	type consumer struct {
		binding *binding.ScriptBinding
		codec   transport.Codec
	}

	invokeCallable := func(c consumer, callable string, args []any) (binding.InvocationOutcome, error) {
		return c.binding.Invoke(binding.InvocationRequest{
			Callable: callable,
			Stage:    binding.InvocationStageUnary,
			Args:     args,
		})
	}

	encodeObjectView := func(c consumer, id identity.CanonicalID, typeDesc schema.TypeDesc, value any) (transport.View, error) {
		return c.codec.Encode(typeDesc, id, value)
	}

	sb := binding.NewScriptBinding()
	if err := sb.BindFunction("fail", func() (int, error) { return 0, testMilestoneErr("boom") }); err != nil {
		t.Fatalf("BindFunction: %v", err)
	}
	consumerAPI := consumer{binding: sb, codec: &transport.JSONCodec{}}

	outcome, err := invokeCallable(consumerAPI, "fail", nil)
	if err != nil {
		t.Fatalf("invokeCallable: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultError {
		t.Fatalf("expected error result, got %s", outcome.Result.Kind)
	}
	if outcome.Result.Error == nil {
		t.Fatal("expected invocation error descriptor")
	}
	if outcome.Result.Error.Callable != "fail" {
		t.Fatalf("expected callable context 'fail', got %s", outcome.Result.Error.Callable)
	}
	if outcome.Result.Error.Stage != binding.InvocationStageUnary {
		t.Fatalf("expected stage context unary, got %s", outcome.Result.Error.Stage)
	}
	if outcome.Result.Error.Message != "boom" {
		t.Fatalf("expected invocation message boom, got %s", outcome.Result.Error.Message)
	}

	id := mustID(t, 4000, 9, 0, 2)
	_, err = encodeObjectView(consumerAPI, id, schema.TypeDesc{Kind: schema.TypeKindStruct, Name: "struct", ClassName: "milestoneHealth"}, 123)
	if err == nil {
		t.Fatal("expected encodeObjectView error, got nil")
	}
	encodeErr, ok := err.(*transport.EncodeError)
	if !ok {
		t.Fatalf("expected *transport.EncodeError, got %T", err)
	}
	if encodeErr.SchemaName != "struct" {
		t.Fatalf("expected schema name 'struct', got %q", encodeErr.SchemaName)
	}
	if encodeErr.Path != "" {
		t.Fatalf("expected empty top-level path, got %q", encodeErr.Path)
	}
	if encodeErr.Identity != id {
		t.Fatalf("expected encode error identity %s, got %s", id, encodeErr.Identity)
	}
}

func TestMilestone_ScriptLanguageFuture_UsesExistingBindingSeam(t *testing.T) {
	scriptCallableDesc := schema.CallableDesc{
		Name: "script_boost",
		Parameters: []schema.ParameterDesc{
			{Name: "base", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}},
			{Name: "bonus", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}},
		},
		Returns: []schema.TypeDesc{{Kind: schema.TypeKindScalar, Name: "int"}},
		Mode:    schema.CallableModeUnary,
	}

	sb := binding.NewScriptBinding()
	if err := sb.Callables.Register(scriptCallableDesc); err != nil {
		t.Fatalf("Register script callable descriptor: %v", err)
	}

	scriptAdapter, err := binding.NewUnaryInvocationAdapter(scriptCallableDesc, func(args []any) (any, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("expected 2 args, got %d", len(args))
		}
		base, ok := args[0].(int)
		if !ok {
			return nil, fmt.Errorf("expected int base, got %T", args[0])
		}
		bonus, ok := args[1].(int)
		if !ok {
			return nil, fmt.Errorf("expected int bonus, got %T", args[1])
		}
		return base + bonus, nil
	})
	if err != nil {
		t.Fatalf("NewUnaryInvocationAdapter: %v", err)
	}
	if err := sb.Executors.RegisterAdapter(scriptAdapter); err != nil {
		t.Fatalf("RegisterAdapter: %v", err)
	}

	desc, ok := sb.Callables.Lookup("script_boost")
	if !ok {
		t.Fatal("expected script-defined callable descriptor to be registered")
	}
	if desc.Name != scriptCallableDesc.Name {
		t.Fatalf("expected callable name %q, got %q", scriptCallableDesc.Name, desc.Name)
	}

	adapter, ok := sb.Executors.Lookup("script_boost")
	if !ok {
		t.Fatal("expected executable adapter for script-defined callable")
	}
	if adapter.Callable().Name != scriptCallableDesc.Name {
		t.Fatalf("expected adapter callable name %q, got %q", scriptCallableDesc.Name, adapter.Callable().Name)
	}

	outcome, err := sb.Invoke(binding.InvocationRequest{
		Callable: "script_boost",
		Stage:    binding.InvocationStageUnary,
		Args:     []any{7, 5},
	})
	if err != nil {
		t.Fatalf("Invoke script-defined callable: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Result.Callable != "script_boost" {
		t.Fatalf("expected callable context script_boost, got %s", outcome.Result.Callable)
	}
	if outcome.Payload == nil {
		t.Fatal("expected payload for script-defined callable")
	}
	result, ok := outcome.Payload.Value.(int)
	if !ok {
		t.Fatalf("expected int payload, got %T", outcome.Payload.Value)
	}
	if result != 12 {
		t.Fatalf("expected script-defined callable result 12, got %d", result)
	}
}

func TestMilestone_ScriptLanguageFuture_SourceFrontendConvergesOnCanonicalSurface(t *testing.T) {
	sb := binding.NewScriptBinding()
	f, err := frontend.New(sb)
	if err != nil {
		t.Fatalf("frontend.New: %v", err)
	}

	source := strings.Join([]string{
		"struct Player { tags: array<string>, attrs: map<string, int>, owner: Account }",
		"fun collect(ids: array<int>, attrs: map<string, int>): array<string> {}",
	}, "\n")
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	desc, ok := sb.Callables.Lookup("collect")
	if !ok {
		t.Fatal("expected source-defined callable descriptor in ScriptBinding")
	}
	if desc.Mode != schema.CallableModeUnary {
		t.Fatalf("expected unary callable mode, got %s", desc.Mode)
	}
	if len(desc.Parameters) != 2 {
		t.Fatalf("expected 2 callable parameters, got %d", len(desc.Parameters))
	}
	if desc.Parameters[0].Type.Kind != schema.TypeKindArray || desc.Parameters[0].Type.Element == nil || desc.Parameters[0].Type.Element.Name != "int" {
		t.Fatalf("expected ids parameter as []int, got %+v", desc.Parameters[0].Type)
	}
	if desc.Parameters[1].Type.Kind != schema.TypeKindMap || desc.Parameters[1].Type.Key == nil || desc.Parameters[1].Type.Key.Name != "string" || desc.Parameters[1].Type.Value == nil || desc.Parameters[1].Type.Value.Name != "int" {
		t.Fatalf("expected attrs parameter as map[string]int, got %+v", desc.Parameters[1].Type)
	}
	if len(desc.Returns) != 1 || desc.Returns[0].Kind != schema.TypeKindArray || desc.Returns[0].Element == nil || desc.Returns[0].Element.Name != "string" {
		t.Fatalf("expected return type []string, got %+v", desc.Returns)
	}

	player, ok := f.Object("Player")
	if !ok {
		t.Fatal("expected source-defined object descriptor in frontend snapshot")
	}
	if len(player.Fields) != 3 {
		t.Fatalf("expected 3 Player fields, got %d", len(player.Fields))
	}
	if player.Fields[0].Type.Kind != schema.TypeKindArray || player.Fields[0].Type.Element == nil || player.Fields[0].Type.Element.Name != "string" {
		t.Fatalf("expected tags field as []string, got %+v", player.Fields[0].Type)
	}
	if player.Fields[1].Type.Kind != schema.TypeKindMap || player.Fields[1].Type.Key == nil || player.Fields[1].Type.Key.Name != "string" || player.Fields[1].Type.Value == nil || player.Fields[1].Type.Value.Name != "int" {
		t.Fatalf("expected attrs field as map[string]int, got %+v", player.Fields[1].Type)
	}
	if player.Fields[2].Type.Kind != schema.TypeKindClass || player.Fields[2].Type.ClassName != "Account" {
		t.Fatalf("expected owner field as class Account, got %+v", player.Fields[2].Type)
	}
}

func TestMilestone_ScriptLanguageFuture_SourceFrontendExecutionUsesBindingSeam(t *testing.T) {
	sb := binding.NewScriptBinding()
	f, err := frontend.New(sb)
	if err != nil {
		t.Fatalf("frontend.New: %v", err)
	}

	if err := f.LoadSource("fun collect(ids: array<int>): array<string> {}"); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	adapter, ok := sb.Executors.Lookup("collect")
	if !ok {
		t.Fatal("expected executable adapter for source-defined callable")
	}
	if adapter.Callable().Name != "collect" {
		t.Fatalf("expected adapter callable collect, got %s", adapter.Callable().Name)
	}

	outcome, err := sb.Invoke(binding.InvocationRequest{
		Callable: "collect",
		Stage:    binding.InvocationStageUnary,
		Args:     []any{[]int{1, 2}},
	})
	if err != nil {
		t.Fatalf("Invoke source-defined callable: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultError {
		t.Fatalf("expected binding error result kind, got %s", outcome.Result.Kind)
	}
	if outcome.Result.Callable != "collect" {
		t.Fatalf("expected callable context collect, got %s", outcome.Result.Callable)
	}
	if outcome.Result.Stage != binding.InvocationStageUnary {
		t.Fatalf("expected unary stage, got %s", outcome.Result.Stage)
	}
	if outcome.Result.Error == nil || !strings.Contains(outcome.Result.Error.Message, "no evaluator") {
		t.Fatalf("expected no-evaluator binding error, got %+v", outcome.Result.Error)
	}
	if outcome.Payload != nil {
		t.Fatalf("expected nil payload for source-defined not-implemented callable, got %+v", outcome.Payload)
	}
}

func TestMilestone_ScriptLanguageFuture_SourceFrontendExecutionDiagnosticsStayObservable(t *testing.T) {
	sb := binding.NewScriptBinding()
	f, err := frontend.New(sb)
	if err != nil {
		t.Fatalf("frontend.New: %v", err)
	}

	vmEval := bytecode.NewVMEvaluator()
	f.SetVMCompileHook(vmEval)
	if err := f.LoadSource("fun bad(value: int): int = value + \"x\""); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	_, err = sb.Invoke(binding.InvocationRequest{
		Callable: "bad",
		Stage:    binding.InvocationStageUnary,
		Args:     []any{1},
	})
	// The VM executes int+string as string concatenation, producing a string result.
	// The binding layer rejects the type mismatch — the error is still observable.
	if err == nil {
		t.Fatal("expected error from type-mismatching script execution")
	}
}

func TestMilestone_ScriptLanguageFuture_UsesObjectBindingSeam(t *testing.T) {
	type scriptHealthObject struct {
		Value int
		Label string
	}

	scriptObjectDesc := schema.ObjectDesc{
		Name: "scriptHealthObject",
		Fields: []schema.FieldDesc{
			{Name: "Value", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}},
			{Name: "Label", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
		},
	}

	id := mustID(t, 5000, 10, 0, 1)
	target := &scriptHealthObject{Value: 21, Label: "script-owned"}
	sb := binding.NewScriptBinding()

	boundObject, err := sb.BindObject(scriptObjectDesc, id, target)
	if err != nil {
		t.Fatalf("BindObject: %v", err)
	}
	if boundObject.Identity != id {
		t.Fatalf("expected bound object identity %s, got %s", id, boundObject.Identity)
	}
	if boundObject.Schema.Name != scriptObjectDesc.Name {
		t.Fatalf("expected schema name %q, got %q", scriptObjectDesc.Name, boundObject.Schema.Name)
	}

	view, err := binding.ProjectView(boundObject)
	if err != nil {
		t.Fatalf("ProjectView: %v", err)
	}
	if view.Schema.Name != scriptObjectDesc.Name {
		t.Fatalf("expected projected schema name %q, got %q", scriptObjectDesc.Name, view.Schema.Name)
	}
	if view.Identity != id {
		t.Fatalf("expected projected identity %s, got %s", id, view.Identity)
	}
	if view.Fields["Value"] != 21 {
		t.Fatalf("expected Value=21, got %v", view.Fields["Value"])
	}
	if view.Fields["Label"] != "script-owned" {
		t.Fatalf("expected Label=script-owned, got %v", view.Fields["Label"])
	}

	patchView := &binding.ViewProjection{
		Schema:   scriptObjectDesc,
		Identity: id,
		Fields: map[string]any{
			"Value": float64(34),
			"Label": "patched",
		},
	}
	mutations, err := binding.ApplyViewPatch(boundObject, patchView)
	if err != nil {
		t.Fatalf("ApplyViewPatch: %v", err)
	}
	if target.Value != 34 {
		t.Fatalf("expected patched Value=34, got %d", target.Value)
	}
	if target.Label != "patched" {
		t.Fatalf("expected patched Label=patched, got %s", target.Label)
	}
	if len(mutations) != 2 {
		t.Fatalf("expected 2 mutations from patch, got %d", len(mutations))
	}
}

func TestMilestone_ScriptLanguageFuture_ExternalConsumerCallableEquivalence(t *testing.T) {
	type consumer struct {
		binding *binding.ScriptBinding
	}

	invoke := func(c consumer, callable string, args []any) (binding.InvocationOutcome, error) {
		return c.binding.Invoke(binding.InvocationRequest{
			Callable: callable,
			Stage:    binding.InvocationStageUnary,
			Args:     args,
		})
	}

	sb := binding.NewScriptBinding()
	if err := sb.BindFunction("go_boost", func(base int, bonus int) int { return base + bonus }); err != nil {
		t.Fatalf("BindFunction go_boost: %v", err)
	}

	scriptCallableDesc := schema.CallableDesc{
		Name: "script_boost_equiv",
		Parameters: []schema.ParameterDesc{
			{Name: "base", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}},
			{Name: "bonus", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}},
		},
		Returns: []schema.TypeDesc{{Kind: schema.TypeKindScalar, Name: "int"}},
		Mode:    schema.CallableModeUnary,
	}
	if err := sb.Callables.Register(scriptCallableDesc); err != nil {
		t.Fatalf("Register script callable descriptor: %v", err)
	}
	scriptAdapter, err := binding.NewUnaryInvocationAdapter(scriptCallableDesc, func(args []any) (any, error) {
		base, ok := args[0].(int)
		if !ok {
			return nil, fmt.Errorf("expected int base, got %T", args[0])
		}
		bonus, ok := args[1].(int)
		if !ok {
			return nil, fmt.Errorf("expected int bonus, got %T", args[1])
		}
		return base + bonus, nil
	})
	if err != nil {
		t.Fatalf("NewUnaryInvocationAdapter: %v", err)
	}
	if err := sb.Executors.RegisterAdapter(scriptAdapter); err != nil {
		t.Fatalf("RegisterAdapter: %v", err)
	}

	consumerAPI := consumer{binding: sb}
	goOutcome, err := invoke(consumerAPI, "go_boost", []any{8, 4})
	if err != nil {
		t.Fatalf("invoke go_boost: %v", err)
	}
	scriptOutcome, err := invoke(consumerAPI, "script_boost_equiv", []any{8, 4})
	if err != nil {
		t.Fatalf("invoke script_boost_equiv: %v", err)
	}

	if goOutcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected go callable value result, got %s", goOutcome.Result.Kind)
	}
	if scriptOutcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected script callable value result, got %s", scriptOutcome.Result.Kind)
	}
	if goOutcome.Result.Stage != scriptOutcome.Result.Stage {
		t.Fatalf("expected identical invocation stage, got %s vs %s", goOutcome.Result.Stage, scriptOutcome.Result.Stage)
	}
	if goOutcome.Payload == nil || scriptOutcome.Payload == nil {
		t.Fatal("expected payloads for both go-defined and script-defined callable")
	}
	goResult, ok := goOutcome.Payload.Value.(int)
	if !ok {
		t.Fatalf("expected int go payload, got %T", goOutcome.Payload.Value)
	}
	scriptResult, ok := scriptOutcome.Payload.Value.(int)
	if !ok {
		t.Fatalf("expected int script payload, got %T", scriptOutcome.Payload.Value)
	}
	if goResult != 12 || scriptResult != 12 {
		t.Fatalf("expected equivalent results 12/12, got %d/%d", goResult, scriptResult)
	}
	if goOutcome.Result.Value == nil || scriptOutcome.Result.Value == nil {
		t.Fatal("expected result schema context for both go-defined and script-defined callable")
	}
	if goOutcome.Result.Value.Kind != scriptOutcome.Result.Value.Kind || goOutcome.Result.Value.Name != scriptOutcome.Result.Value.Name {
		t.Fatalf("expected equivalent result schema, got %+v vs %+v", *goOutcome.Result.Value, *scriptOutcome.Result.Value)
	}
}

func TestMilestone_ScriptLanguageFuture_RuntimeBoundary_NoVMObjectsInWorld(t *testing.T) {
	type vmFrame struct {
		PC    int
		Stack []int
	}

	type scriptState struct {
		Value int
	}

	scriptStateDesc := schema.ObjectDesc{
		Name:   "scriptState",
		Fields: []schema.FieldDesc{{Name: "Value", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "int"}}},
	}

	w := runtime.NewWorld()
	codec := &transport.JSONCodec{}
	sb := binding.NewScriptBinding()

	vmExecute := func(frame *vmFrame, target *scriptState, delta int) error {
		frame.PC++
		frame.Stack = append(frame.Stack, delta)
		target.Value += delta
		return nil
	}

	e := w.Create()
	state := &scriptState{Value: 10}
	if err := w.SetComponent(e, "scriptState", state); err != nil {
		t.Fatalf("SetComponent scriptState: %v", err)
	}
	w.ClearChanges(e)

	boundObject, err := sb.BindObject(scriptStateDesc, e.ID(), state)
	if err != nil {
		t.Fatalf("BindObject: %v", err)
	}

	frame := &vmFrame{PC: 7, Stack: []int{1, 2}}
	if err := vmExecute(frame, state, 5); err != nil {
		t.Fatalf("vmExecute: %v", err)
	}
	w.MarkChanged(e, "scriptState")

	if _, ok := w.GetComponent(e, "vmFrame"); ok {
		t.Fatal("did not expect VM frame object to be stored in runtime world")
	}
	if names := w.ComponentNames(e); len(names) != 1 || names[0] != "scriptState" {
		t.Fatalf("expected runtime world to expose only scriptState component, got %v", names)
	}
	if frame.PC != 8 {
		t.Fatalf("expected VM frame PC=8 after execution, got %d", frame.PC)
	}
	if state.Value != 15 {
		t.Fatalf("expected script state Value=15 after execution, got %d", state.Value)
	}

	view, err := binding.ProjectView(boundObject)
	if err != nil {
		t.Fatalf("ProjectView: %v", err)
	}
	if _, exists := view.Fields["PC"]; exists {
		t.Fatal("did not expect VM frame fields to leak into projected script state view")
	}
	if view.Fields["Value"] != 15 {
		t.Fatalf("expected projected Value=15, got %v", view.Fields["Value"])
	}

	patchView, err := ecsbind.ProjectEntityChangesToTransport(w, e, map[string]schema.ObjectDesc{"scriptState": scriptStateDesc}, codec)
	if err != nil {
		t.Fatalf("ProjectEntityChangesToTransport: %v", err)
	}
	decoded, err := codec.Decode(patchView)
	if err != nil {
		t.Fatalf("Decode patch view: %v", err)
	}
	patchMap, ok := decoded.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any patch payload, got %T", decoded)
	}
	changed, ok := patchMap["Changed"].(map[string]any)
	if !ok {
		t.Fatalf("expected Changed bucket, got %T", patchMap["Changed"])
	}
	changedState, ok := changed["scriptState"].(map[string]any)
	if !ok {
		t.Fatalf("expected scriptState change payload, got %T", changed["scriptState"])
	}
	if _, exists := changedState["PC"]; exists {
		t.Fatal("did not expect VM frame fields in transport patch payload")
	}
	if changedState["Value"] != 15.0 {
		t.Fatalf("expected transport patch Value=15, got %v", changedState["Value"])
	}
}

// ============================================================================
// Milestone 7: Script-visible object lifecycle across planes
//
// Verifies that ScriptBinding tracks ObjectBindings by identity and that
// external lifecycle events (entity disposal) can propagate to the binding
// layer via InvalidateBindingsFor, causing projections to fail gracefully.
//
// ScriptBinding does NOT define lifecycle authority — it provides an
// observability surface. The decision of when to invalidate belongs to
// the caller (runtime carrier consumer, hosting layer, etc.).
// ============================================================================

type lifecycleTestItem struct {
	Name string
}

func lifecycleTestClassDesc() schema.ObjectDesc {
	return schema.ObjectDesc{
		Name: "lifecycleTestItem",
		Fields: []schema.FieldDesc{
			{Name: "Name", Type: schema.TypeDesc{Kind: schema.TypeKindScalar, Name: "string"}},
		},
	}
}

func TestMilestone_BindingLifecycle_EntityDisposeInvalidatesBinding(t *testing.T) {
	w := runtime.NewWorld(runtime.WithSlot(5))
	sb := binding.NewScriptBinding()
	codec := &transport.JSONCodec{}

	// Step 1: Create entity and bind
	e := w.Create()
	item := &lifecycleTestItem{Name: "potion"}
	if err := w.SetComponent(e, "lifecycleTestItem", item); err != nil {
		t.Fatalf("SetComponent: %v", err)
	}

	desc := lifecycleTestClassDesc()
	b, err := sb.BindObject(desc, e.ID(), item)
	if err != nil {
		t.Fatalf("BindObject: %v", err)
	}

	// Step 2: Verify binding is active
	if !b.Valid() {
		t.Fatal("expected valid binding after creation")
	}
	_, ok := sb.LookupBinding(e.ID())
	if !ok {
		t.Fatal("expected binding to be tracked by identity")
	}
	ids := sb.BoundIdentities()
	if len(ids) != 1 {
		t.Fatalf("expected 1 bound identity, got %d", len(ids))
	}
	foundID := false
	for _, id := range ids {
		if id == e.ID() {
			foundID = true
			break
		}
	}
	if !foundID {
		t.Fatalf("expected bound identity %s in %v", e.ID(), ids)
	}

	// Step 3: Project through binding → transport (should succeed)
	view, err := binding.ProjectView(b)
	if err != nil {
		t.Fatalf("ProjectView before dispose: %v", err)
	}
	tv, err := ecsbind.ProjectEntityToTransport(w, e, "lifecycleTestItem", desc, codec)
	if err != nil {
		t.Fatalf("ProjectEntityToTransport before dispose: %v", err)
	}
	if tv.Identity != e.ID() {
		t.Fatal("identity drift before dispose")
	}
	if _, ok := view.Fields["Name"].(string); !ok {
		t.Fatal("expected Name field in projection")
	}

	// Step 4: Dispose entity
	w.Dispose(e)

	// Step 5: External caller notifies binding of entity disposal
	// (This simulates the hosting layer calling InvalidateBindingsFor)
	sb.InvalidateBindingsFor(e.ID())

	// Step 6: Verify binding is invalidated
	if b.Valid() {
		t.Fatal("expected binding to be invalid after entity disposal notification")
	}
	_, ok = sb.LookupBinding(e.ID())
	if ok {
		t.Fatal("expected LookupBinding to return false for disposed entity")
	}

	// Step 7: Projection should fail
	_, err = binding.ProjectView(b)
	if err == nil {
		t.Fatal("expected error projecting from disposed entity binding")
	}

	// Step 8: Patch should fail
	_, err = binding.ApplyViewPatch(b, &binding.ViewProjection{
		Schema:   desc,
		Identity: e.ID(),
		Fields:   map[string]any{"Name": "disposed"},
	})
	if err == nil {
		t.Fatal("expected error patching disposed entity binding")
	}

	// Step 9: Other bindings should still work
	e2 := w.Create()
	item2 := &lifecycleTestItem{Name: "armor"}
	if err := w.SetComponent(e2, "lifecycleTestItem", item2); err != nil {
		t.Fatalf("SetComponent e2: %v", err)
	}
	b2, err := sb.BindObject(desc, e2.ID(), item2)
	if err != nil {
		t.Fatalf("BindObject e2: %v", err)
	}
	if !b2.Valid() {
		t.Fatal("expected e2 binding to remain valid")
	}
}

// ============================================================================
// Phase 13: External-consumption equivalence proof
//
// Proves that external systems consume the spore canonical surface (ScriptBinding,
// InvocationRequest/Outcome, CallableDesc), not script engine internals.
// Go-defined and script-defined callables are equivalent through the binding seam.
// ============================================================================

func setupVMBinding(t *testing.T, source string) (*binding.ScriptBinding, *frontend.Frontend) {
	t.Helper()
	sb := binding.NewScriptBinding()
	vmEval := bytecode.NewVMEvaluator()
	f, err := frontend.New(sb)
	if err != nil {
		t.Fatalf("frontend.New: %v", err)
	}
	f.SetVMCompileHook(vmEval)
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	return sb, f
}

func TestMilestone_Phase14_ExplicitRuntimeBackendPreservesExternalBindingSurface(t *testing.T) {
	sb, f := setupVMBinding(t, "fun add(a: int, b: int): int { return a + b }")

	outcome, err := f.Invoke("add", []any{10, 20})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	value, ok := outcome.Payload.Value.(int)
	if !ok {
		t.Fatalf("expected int payload, got %T", outcome.Payload.Value)
	}
	if value != 30 {
		t.Fatalf("expected 30, got %d", value)
	}

	adapter, ok := sb.Executors.Lookup("add")
	if !ok {
		t.Fatal("expected executable adapter for add")
	}
	if adapter.Callable().Name != "add" {
		t.Fatalf("expected adapter callable add, got %q", adapter.Callable().Name)
	}
}

func TestMilestone_Phase13_ScriptCallableInvocationViaBindingSeam(t *testing.T) {
	sb, _ := setupVMBinding(t, "fun add(a: int, b: int): int { return a + b }")

	outcome, err := sb.Invoke(binding.InvocationRequest{
		Callable: "add",
		Stage:    binding.InvocationStageUnary,
		Args:     []any{1, 2},
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	if outcome.Payload == nil {
		t.Fatal("expected non-nil payload")
	}
	result, ok := outcome.Payload.Value.(int)
	if !ok {
		t.Fatalf("expected int payload, got %T", outcome.Payload.Value)
	}
	if result != 3 {
		t.Fatalf("expected 3, got %d", result)
	}
}

func TestMilestone_Phase13_ScriptCallableResultSchemaEquivalence(t *testing.T) {
	sb, _ := setupVMBinding(t, "fun add(a: int, b: int): int { return a + b }")

	outcome, err := sb.Invoke(binding.InvocationRequest{
		Callable: "add",
		Stage:    binding.InvocationStageUnary,
		Args:     []any{5, 7},
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Callable != "add" {
		t.Fatalf("expected callable 'add', got %s", outcome.Result.Callable)
	}
	if outcome.Result.Stage != binding.InvocationStageUnary {
		t.Fatalf("expected unary stage, got %s", outcome.Result.Stage)
	}
	if outcome.Result.Mode != schema.CallableModeUnary {
		t.Fatalf("expected unary mode, got %s", outcome.Result.Mode)
	}
	if outcome.Result.Value == nil {
		t.Fatal("expected result Value to be populated")
	}
	if outcome.Result.Value.Kind != schema.TypeKindScalar || outcome.Result.Value.Name != "int" {
		t.Fatalf("expected int type desc, got %+v", *outcome.Result.Value)
	}
}

func TestMilestone_Phase13_ScriptCallableStringReturnType(t *testing.T) {
	sb, _ := setupVMBinding(t, "fun greet(name: string): string { return \"hello \" + name }")

	outcome, err := sb.Invoke(binding.InvocationRequest{
		Callable: "greet",
		Stage:    binding.InvocationStageUnary,
		Args:     []any{"world"},
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}
	result, ok := outcome.Payload.Value.(string)
	if !ok {
		t.Fatalf("expected string payload, got %T", outcome.Payload.Value)
	}
	if result != "hello world" {
		t.Fatalf("expected 'hello world', got %q", result)
	}
}

func TestMilestone_Phase13_ErrorOutcomeStructuralEquivalence(t *testing.T) {
	// Go-defined callable that always errors
	sb := binding.NewScriptBinding()
	if err := sb.BindFunction("go_fail", func() (int, error) { return 0, fmt.Errorf("go boom") }); err != nil {
		t.Fatalf("BindFunction: %v", err)
	}
	goOutcome, err := sb.Invoke(binding.InvocationRequest{
		Callable: "go_fail",
		Stage:    binding.InvocationStageUnary,
		Args:     nil,
	})
	if err != nil {
		t.Fatalf("Invoke go_fail: %v", err)
	}
	if goOutcome.Result.Kind != binding.InvocationResultError {
		t.Fatalf("expected error result for go_fail, got %s", goOutcome.Result.Kind)
	}
	if goOutcome.Result.Error == nil {
		t.Fatal("expected error descriptor for go_fail")
	}
	if goOutcome.Result.Error.Callable != "go_fail" {
		t.Fatalf("expected callable 'go_fail', got %s", goOutcome.Result.Error.Callable)
	}
	if goOutcome.Result.Error.Stage != binding.InvocationStageUnary {
		t.Fatalf("expected unary stage, got %s", goOutcome.Result.Error.Stage)
	}
	if goOutcome.Result.Error.Message != "go boom" {
		t.Fatalf("expected message 'go boom', got %s", goOutcome.Result.Error.Message)
	}

	// Script-defined callable that causes a VM error (division by zero)
	scriptSB, _ := setupVMBinding(t, "fun div_zero(): int { return 1 / 0 }")
	scriptOutcome, err := scriptSB.Invoke(binding.InvocationRequest{
		Callable: "div_zero",
		Stage:    binding.InvocationStageUnary,
		Args:     nil,
	})
	if err != nil {
		t.Fatalf("Invoke div_zero: %v", err)
	}
	if scriptOutcome.Result.Kind != binding.InvocationResultError {
		t.Fatalf("expected error result for script callable, got %s", scriptOutcome.Result.Kind)
	}
	if scriptOutcome.Result.Error == nil {
		t.Fatal("expected error descriptor for script callable")
	}
	if scriptOutcome.Result.Error.Callable != "div_zero" {
		t.Fatalf("expected callable 'div_zero', got %s", scriptOutcome.Result.Error.Callable)
	}
	if scriptOutcome.Result.Error.Stage != binding.InvocationStageUnary {
		t.Fatalf("expected unary stage, got %s", scriptOutcome.Result.Error.Stage)
	}
	// Both error outcomes share the same structural shape: Callable, Stage, Message
	// External consumer sees identical InvocationResultDesc shape regardless of origin.
	_ = goOutcome.Result.Callable
	_ = scriptOutcome.Result.Callable
}

func TestMilestone_Phase13_ScriptCallableDiagnosticCodePropagates(t *testing.T) {
	// Script callable with type-cast failure produces structured DiagnosticCode
	scriptSB, _ := setupVMBinding(t, `fun bad_cast(x: int): string { return x as string }`)
	outcome, err := scriptSB.Invoke(binding.InvocationRequest{
		Callable: "bad_cast",
		Stage:    binding.InvocationStageUnary,
		Args:     []any{42},
	})
	if err != nil {
		t.Fatalf("Invoke bad_cast: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultError {
		t.Fatalf("expected error result, got %s", outcome.Result.Kind)
	}
	if outcome.Result.Error == nil {
		t.Fatal("expected error descriptor")
	}
	if outcome.Result.Error.DiagnosticCode != "type_cast_failed" {
		t.Fatalf("expected diagnostic code 'type_cast_failed', got %q", outcome.Result.Error.DiagnosticCode)
	}
	if outcome.Result.Error.Callable != "bad_cast" {
		t.Fatalf("expected callable 'bad_cast', got %s", outcome.Result.Error.Callable)
	}

	// Go-defined callable error has empty DiagnosticCode (no structured error taxonomy)
	sb := binding.NewScriptBinding()
	if err := sb.BindFunction("go_err", func() (int, error) { return 0, fmt.Errorf("go fail") }); err != nil {
		t.Fatalf("BindFunction: %v", err)
	}
	goOutcome, err := sb.Invoke(binding.InvocationRequest{
		Callable: "go_err",
		Stage:    binding.InvocationStageUnary,
		Args:     nil,
	})
	if err != nil {
		t.Fatalf("Invoke go_err: %v", err)
	}
	if goOutcome.Result.Error.DiagnosticCode != "" {
		t.Fatalf("expected empty diagnostic code for Go error, got %q", goOutcome.Result.Error.DiagnosticCode)
	}
}

func TestMilestone_Phase13_DescriptorSurfaceUnified(t *testing.T) {
	sb := binding.NewScriptBinding()
	if err := sb.BindFunction("go_add", func(a int, b int) int { return a + b }); err != nil {
		t.Fatalf("BindFunction: %v", err)
	}
	f, err := frontend.New(sb)
	if err != nil {
		t.Fatalf("frontend.New: %v", err)
	}
	vmEval := bytecode.NewVMEvaluator()
	f.SetVMCompileHook(vmEval)
	if err := f.LoadSource("fun script_add(a: int, b: int): int { return a + b }"); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	goDesc, ok := sb.Callables.Lookup("go_add")
	if !ok {
		t.Fatal("expected go_add in registry")
	}
	scriptDesc, ok := sb.Callables.Lookup("script_add")
	if !ok {
		t.Fatal("expected script_add in registry")
	}

	// Both descriptors have the same structural fields populated
	if goDesc.Name != "go_add" || scriptDesc.Name != "script_add" {
		t.Fatal("descriptor names mismatch")
	}
	if len(goDesc.Parameters) != 2 || len(scriptDesc.Parameters) != 2 {
		t.Fatalf("expected 2 parameters each, got %d/%d", len(goDesc.Parameters), len(scriptDesc.Parameters))
	}
	if len(goDesc.Returns) != 1 || len(scriptDesc.Returns) != 1 {
		t.Fatalf("expected 1 return each, got %d/%d", len(goDesc.Returns), len(scriptDesc.Returns))
	}
	if goDesc.Mode != schema.CallableModeUnary || scriptDesc.Mode != schema.CallableModeUnary {
		t.Fatalf("expected unary mode, got %s/%s", goDesc.Mode, scriptDesc.Mode)
	}

	// External consumer cannot distinguish origin from the descriptor alone:
	// both have Name, Parameters, Returns, Mode — no "source" or "engine" field.
	_ = goDesc
	_ = scriptDesc
}

func TestMilestone_Phase13_ExternalConsumerCannotAccessVMInternals(t *testing.T) {
	sb, f := setupVMBinding(t, "fun compute(x: int): int { return x * 2 }")

	// Invoke through the binding seam
	outcome, err := sb.Invoke(binding.InvocationRequest{
		Callable: "compute",
		Stage:    binding.InvocationStageUnary,
		Args:     []any{5},
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}

	// ScriptBinding only exposes Callables (CallableRegistry) and Executors (ExecutableRegistry).
	// No VM, no chunks, no interpreter, no frames are accessible.
	if sb.Callables == nil {
		t.Fatal("expected Callables to be non-nil")
	}
	if sb.Executors == nil {
		t.Fatal("expected Executors to be non-nil")
	}

	// Verify the consumer only sees canonical types
	goDesc, ok := sb.Callables.Lookup("compute")
	if !ok {
		t.Fatal("expected compute in registry")
	}
	if goDesc.Name != "compute" {
		t.Fatalf("expected descriptor name 'compute', got %s", goDesc.Name)
	}

	// Verify the outcome uses only canonical types
	if outcome.Result.Callable != "compute" {
		t.Fatalf("expected callable 'compute', got %s", outcome.Result.Callable)
	}
	if outcome.Payload == nil {
		t.Fatal("expected non-nil payload")
	}
	result, ok := outcome.Payload.Value.(int)
	if !ok || result != 10 {
		t.Fatalf("expected payload 10, got %T=%v", outcome.Payload.Value, outcome.Payload.Value)
	}

	// Frontend also does not expose VM internals in its public surface
	callables := f.Callables()
	if len(callables) == 0 {
		t.Fatal("expected callables from frontend")
	}
	for name := range callables {
		if name != "compute" {
			t.Fatalf("unexpected callable %q in frontend surface", name)
		}
	}
}

func TestMilestone_Phase13_ExportedCallablesFromScriptSource(t *testing.T) {
	source := "export fun api(): int { return 42 }\nfun internal_helper(): int { return 1 }"
	sb, f := setupVMBinding(t, source)

	// Both callables are in the full callable set
	all := f.Callables()
	if len(all) != 2 {
		t.Fatalf("expected 2 callables, got %d", len(all))
	}
	if _, ok := all["api"]; !ok {
		t.Fatal("expected 'api' in callables")
	}
	if _, ok := all["internal_helper"]; !ok {
		t.Fatal("expected 'internal_helper' in callables")
	}

	// Only exported callables appear in ExportedCallables
	exported := f.ExportedCallables()
	if len(exported) != 1 {
		t.Fatalf("expected 1 exported callable, got %d", len(exported))
	}
	if exported[0].Name != "api" {
		t.Fatalf("expected exported callable 'api', got %q", exported[0].Name)
	}

	// LookupExportedCallable finds the exported one
	_, ok := f.LookupExportedCallable("api")
	if !ok {
		t.Fatal("expected to find exported callable 'api'")
	}

	// internal_helper is not in exported surface
	_, ok = f.LookupExportedCallable("internal_helper")
	if ok {
		t.Fatal("internal_helper should not be in exported callables")
	}

	// Both are invocable through the binding seam
	outcome, err := sb.Invoke(binding.InvocationRequest{
		Callable: "api",
		Stage:    binding.InvocationStageUnary,
		Args:     []any{},
	})
	if err != nil {
		t.Fatalf("invoke api: %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome.Result.Kind)
	}

	outcome2, err := sb.Invoke(binding.InvocationRequest{
		Callable: "internal_helper",
		Stage:    binding.InvocationStageUnary,
		Args:     []any{},
	})
	if err != nil {
		t.Fatalf("invoke internal_helper: %v", err)
	}
	if outcome2.Result.Kind != binding.InvocationResultValue {
		t.Fatalf("expected value result, got %s", outcome2.Result.Kind)
	}
}

func TestMilestone_Phase13_GoAndScriptCallablesCoexistInRegistry(t *testing.T) {
	sb := binding.NewScriptBinding()

	// Register a Go-defined callable first
	if err := sb.BindFunction("go_fn", func(x int) (int, error) {
		return x * 2, nil
	}); err != nil {
		t.Fatalf("BindFunction go_fn: %v", err)
	}

	// Then load a script-defined callable
	vmEval := bytecode.NewVMEvaluator()
	f, err := frontend.New(sb)
	if err != nil {
		t.Fatalf("frontend.New: %v", err)
	}
	f.SetVMCompileHook(vmEval)
	source := "fun script_fn(x: int): int { return x + 1 }"
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	// Both exist in the registry
	list := sb.Callables.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 callables in registry, got %d", len(list))
	}

	// Registration order is preserved: go_fn first, then script_fn
	names := make([]string, len(list))
	for i, desc := range list {
		names[i] = desc.Name
	}
	if names[0] != "go_fn" || names[1] != "script_fn" {
		t.Fatalf("expected order [go_fn, script_fn], got %v", names)
	}

	// Both have executable adapters
	for _, name := range []string{"go_fn", "script_fn"} {
		adapter, ok := sb.Executors.Lookup(name)
		if !ok {
			t.Fatalf("expected adapter for %q", name)
		}
		if adapter == nil {
			t.Fatalf("adapter for %q is nil", name)
		}
	}

	// Both are invocable with correct results
	goOutcome, err := sb.Invoke(binding.InvocationRequest{
		Callable: "go_fn",
		Stage:    binding.InvocationStageUnary,
		Args:     []any{5},
	})
	if err != nil {
		t.Fatalf("invoke go_fn: %v", err)
	}
	if goResult, ok := goOutcome.Payload.Value.(int); !ok || goResult != 10 {
		t.Fatalf("expected go_fn(5)=10, got %v", goOutcome.Payload.Value)
	}

	scriptOutcome, err := sb.Invoke(binding.InvocationRequest{
		Callable: "script_fn",
		Stage:    binding.InvocationStageUnary,
		Args:     []any{5},
	})
	if err != nil {
		t.Fatalf("invoke script_fn: %v", err)
	}
	if scriptResult, ok := scriptOutcome.Payload.Value.(int); !ok || scriptResult != 6 {
		t.Fatalf("expected script_fn(5)=6, got %v", scriptOutcome.Payload.Value)
	}
}

// ============================================================================
// Milestone: Standard library full-chain integration
//
// Verifies that std modules registered via NewScriptBindingWithStd are
// accessible through the script frontend -> bytecode -> VM execution chain,
// and that multiple std modules can be combined in a single script.
// ============================================================================

func TestMilestone_StdLibrary_FullChain(t *testing.T) {
	sb := std.NewScriptBindingWithStd()
	f, err := frontend.New(sb)
	if err != nil {
		t.Fatalf("frontend.New: %v", err)
	}
	vmEval := bytecode.NewVMEvaluator()
	vmEval.SetNativeBinding(sb)
	f.SetVMCompileHook(vmEval)
	f.SetModuleResolver(frontend.MapModuleResolver{})

	source := `import md5 from "hash"
import encode from "json"
import toUpper from "strings"
import formatFloat from "strconv"
import pow from "math"
fun pipeline(): string {
    var hashResult: string = md5("secret")
    var jsonResult: string = encode(hashResult)
    var upperResult: string = toUpper(jsonResult)
    var mathResult: double = pow(2.0, 3.0)
    var formatted: string = formatFloat(mathResult)
    return upperResult + ":" + formatted
}`
	if err := f.LoadSource(source); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("pipeline", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if outcome.Payload == nil {
		t.Fatal("expected payload, got nil")
	}
	s, ok := outcome.Payload.Value.(string)
	if !ok {
		t.Fatalf("expected string, got %T", outcome.Payload.Value)
	}
	if s != `"5EBE2294ECD0E0F08EAB7690D2A6EE69":8` {
		t.Fatalf("expected combined result, got %s", s)
	}
}

// ============================================================================
// Milestone: Standard library error propagation
//
// Verifies that std function errors (e.g. json.decode invalid input) propagate
// through the VM execution chain as InvocationResultError with structured
// diagnostics, not as silent nil or unrecoverable panics.
// ============================================================================

func TestMilestone_StdLibrary_ErrorPropagation(t *testing.T) {
	sb := std.NewScriptBindingWithStd()
	f, err := frontend.New(sb)
	if err != nil {
		t.Fatalf("frontend.New: %v", err)
	}
	vmEval := bytecode.NewVMEvaluator()
	vmEval.SetNativeBinding(sb)
	f.SetVMCompileHook(vmEval)
	f.SetModuleResolver(frontend.MapModuleResolver{})

	if err := f.LoadSource(`import decode from "json"
fun badDecode(): any { return decode("not json") }`); err != nil {
		t.Fatalf("LoadSource: %v", err)
	}

	outcome, err := f.Invoke("badDecode", nil)
	if err != nil {
		t.Fatalf("expected no Go error, got %v", err)
	}
	if outcome.Result.Kind != binding.InvocationResultError {
		t.Fatalf("expected InvocationResultError, got %s", outcome.Result.Kind)
	}
	if outcome.Result.Error == nil {
		t.Fatal("expected error descriptor with message")
	}
	msg := outcome.Result.Error.Message
	if !strings.Contains(msg, "invalid character") {
		t.Fatalf("expected 'invalid character' in error message, got %s", msg)
	}
	if outcome.Result.Error.DiagnosticCode == "" {
		t.Fatal("expected structured diagnostic code in error descriptor")
	}
}
