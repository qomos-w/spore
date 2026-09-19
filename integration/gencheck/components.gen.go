package gencheck

import (
	"reflect"

	"github.com/qomos-w/spore/runtime"
	"github.com/qomos-w/spore/schema"
)

// Schema IDs for value types defined in this package.
const (
	AvatarSchemaID   uint64 = 303
	PlainMsgSchemaID uint64 = 302
	PositionSchemaID uint64 = 300
	VelocitySchemaID uint64 = 301
)

func init() {
	schema.RegisterStructType(AvatarSchemaID, reflect.TypeOf(Avatar{}))
	schema.RegisterStructType(PlainMsgSchemaID, reflect.TypeOf(PlainMsg{}))
	schema.RegisterStructType(PositionSchemaID, reflect.TypeOf(Position{}))
	schema.RegisterStructType(VelocitySchemaID, reflect.TypeOf(Velocity{}))
}

// ECS component descriptors (declared @component in the schema).
var (
	PositionC = runtime.NewComponent[Position]("Position")
	VelocityC = runtime.NewComponent[Velocity]("Velocity")
)

// Media is the canonical {mime, src} reference carrier for the media
// schema type. src is a data:/file:/https: reference; inline data: URLs
// are capped at 1 MiB and file:/https: resolution is host-side.
type Media struct {
	Mime string `json:"mime"`
	Src  string `json:"src"`
}

// Validate enforces the media value contract.
func (m Media) Validate() error {
	return schema.ValidateMediaValue(m)
}

type Avatar struct {
	Photo   Media   `json:"photo"`
	Gallery []Media `json:"gallery"`
}

type PlainMsg struct {
	Note string `json:"note"`
}

type Position struct {
	X float32 `json:"x"`
}

type Velocity struct {
	Dx float32 `json:"dx"`
}
