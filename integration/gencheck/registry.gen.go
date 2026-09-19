// Source: schema registry (4 types)

package gencheck

import (
	"reflect"

	"github.com/qomos-w/spore/schema"
)

// SchemaIDs maps schema ID to canonical struct name.
var SchemaIDs = map[uint64]string{
	300: "Position",
	301: "Velocity",
	302: "PlainMsg",
	303: "Avatar",
}

// SchemaTypes maps schema ID to registered reflect.Type.
var SchemaTypes = map[uint64]reflect.Type{
	300: reflect.TypeOf(Position{}),
	301: reflect.TypeOf(Velocity{}),
	302: reflect.TypeOf(PlainMsg{}),
	303: reflect.TypeOf(Avatar{}),
}

// ComponentShape is one @component struct available for script
// BindStruct wiring. The consumer-side helper (ecsbind) iterates
// ComponentShapes and resolves each entry's reflect.Type from
// Registry.SchemaTypes() to obtain a zero value for
// script.Runtime.BindStruct:
//
//	for _, c := range pkg.ComponentShapes {
//	    rt.BindStruct("components", c.Name,
//	        reflect.New(Registry.SchemaTypes()[c.SchemaID]).Elem().Interface())
//	}
//
// The struct holds pure data only (no Go type references) so the
// generated file stays free of any reverse-import on script or
// runtime. Codegen never emits the BindStruct call itself.
type ComponentShape struct {
	Name     string
	SchemaID uint64
}

// ComponentShapes is the @component subset of the schema table, in
// schema ID order. The consumer-side helper (ecsbind) iterates this
// slice and calls script.Runtime.BindStruct for each entry; the
// codegen package itself stays out of the binding path so it can be
// imported by any dependency-free target.
var ComponentShapes = []ComponentShape{
	{Name: "Position", SchemaID: 300},
	{Name: "Velocity", SchemaID: 301},
}

// ComponentIDs lists the schema IDs of structs declared @component.
var componentIDs = []uint64{
	300,
	301,
}

// Registry is the codegen schema/component table for this package.
// It is a plain data object: pass it to a runtime World so the World can
// resolve @component Go types to their schema names:
//
//	w := runtime.NewWorld()
//	w.AddRegistry(Registry)
//
// The concrete type is local (stdlib method signatures only); the file
// never imports runtime.
type registryTable struct {
	schemaTypes  map[uint64]reflect.Type
	schemaIDs    map[uint64]string
	componentIDs []uint64
}

func (t registryTable) SchemaTypes() map[uint64]reflect.Type {
	return t.schemaTypes
}

func (t registryTable) SchemaIDs() map[uint64]string {
	return t.schemaIDs
}

func (t registryTable) ComponentIDs() []uint64 {
	return t.componentIDs
}

var Registry = registryTable{
	schemaTypes:  SchemaTypes,
	schemaIDs:    SchemaIDs,
	componentIDs: componentIDs,
}

func init() {
	for id, typ := range SchemaTypes {
		schema.RegisterStructType(id, typ)
	}
}
