// Package tph is a fixture: a TPH pattern with two variants, and every shape of edge into it.
package tph

import (
	"entgo.io/ent"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/acdifran/go-tools/ent/poly"
)

type Org struct{ ent.Schema }

func (Org) Edges() []ent.Edge {
	return []ent.Edge{poly.To("connections", ConnectionPattern{}).Many().Optional()}
}

type ConnectionPattern struct{ poly.TPH }

func (ConnectionPattern) Fields() []ent.Field {
	return []ent.Field{
		field.String("name"),
		field.Enum("provider").Values("zendesk", "hubspot"),
		field.Int("org_id"),
	}
}

func (ConnectionPattern) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("org", Org.Type).Ref("connections").Field("org_id").Unique().Required(),
		edge.To("events", Event.Type),
		edge.From("flows", Flow.Type).Ref("sources"),
	}
}

func (ConnectionPattern) Indexes() []ent.Index {
	return []ent.Index{index.Fields("org_id", "provider")}
}

func (ConnectionPattern) Variants() []poly.Variant {
	return []poly.Variant{
		poly.Of("Zendesk", "zendesk").Fields(field.String("subdomain"), field.String("trigger_id").Optional()),
		poly.Of("HubSpot", "hubspot").ColumnPrefix("hs").Fields(field.String("portal")),
	}
}

func (ConnectionPattern) Annotations() []schema.Annotation {
	return []schema.Annotation{poly.Discriminator("provider")}
}

type Event struct{ ent.Schema }

func (Event) Fields() []ent.Field { return []ent.Field{field.Int("connection_id")} }

func (Event) Edges() []ent.Edge {
	return []ent.Edge{poly.From("connection", ConnectionPattern{}).Ref("events").Field("connection_id")}
}

type Flow struct{ ent.Schema }

func (Flow) Edges() []ent.Edge {
	return []ent.Edge{poly.To("sources", ConnectionPattern{}).Many().Optional()}
}

type Note struct{ ent.Schema }

func (Note) Edges() []ent.Edge { return []ent.Edge{poly.To("conn", ConnectionPattern{}).Optional()} }
