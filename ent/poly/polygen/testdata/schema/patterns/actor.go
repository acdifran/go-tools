// Package patterns is a fixture: patterns live outside the schema package so edge helpers can import them.
package patterns

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/acdifran/go-tools/ent/poly"
)

type Actor struct{ poly.TPC }

func (Actor) Fields() []ent.Field {
	return []ent.Field{field.String("first_name").Optional().Nillable()}
}
