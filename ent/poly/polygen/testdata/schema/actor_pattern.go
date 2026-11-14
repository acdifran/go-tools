package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/acdifran/go-tools/ent/poly"
)

type ActorPattern struct{ poly.TPC }

func (ActorPattern) Fields() []ent.Field {
	return []ent.Field{field.String("first_name").Optional().Nillable()}
}
