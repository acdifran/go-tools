// Package schema is a fixture: a TPC pattern with two members, pointed at by a forward edge and an inverse one.
package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"github.com/acdifran/go-tools/ent/poly"
	"github.com/acdifran/go-tools/ent/poly/polygen/testdata/schema/patterns"
)

type User struct{ ent.Schema }

func (User) Patterns() []poly.Pattern { return []poly.Pattern{patterns.Actor{}} }

func (User) Edges() []ent.Edge { return []ent.Edge{edge.To("notes", Note.Type)} }

type Agent struct{ ent.Schema }

func (Agent) Patterns() []poly.Pattern { return []poly.Pattern{patterns.Actor{}} }

func (Agent) Edges() []ent.Edge { return []ent.Edge{edge.To("notes", Note.Type)} }

type Asset struct{ ent.Schema }

func (Asset) Edges() []ent.Edge {
	return []ent.Edge{poly.To("actor", patterns.Actor{}).Optional().Immutable().Implements("WithActor")}
}

type Note struct{ ent.Schema }

func (Note) Edges() []ent.Edge { return []ent.Edge{poly.To("author", patterns.Actor{}).Ref("notes")} }
