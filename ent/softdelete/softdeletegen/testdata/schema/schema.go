// Package schema is a small fixture covering each kind of soft delete declaration softdeletegen validates.
package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"github.com/acdifran/go-tools/ent/softdelete"
)

type Workflow struct{ ent.Schema }

func (Workflow) Mixin() []ent.Mixin { return []ent.Mixin{softdelete.Mixin{}} }

func (Workflow) Fields() []ent.Field {
	return []ent.Field{field.Int("event_source_id").Optional()}
}

func (Workflow) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("plans", WorkflowPlan.Type).Annotations(softdelete.Hide()),
		edge.To("runs", WorkflowRun.Type).Annotations(softdelete.Hide()),
		edge.To("event_source", Connection.Type).Field("event_source_id").Unique().
			Annotations(softdelete.Keep("a workflow outlives the connection that triggers it")),
	}
}

type WorkflowPlan struct{ ent.Schema }

func (WorkflowPlan) Fields() []ent.Field { return []ent.Field{field.Int("workflow_id")} }

func (WorkflowPlan) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("workflow", Workflow.Type).Ref("plans").Field("workflow_id").Unique().Required(),
	}
}

type WorkflowRun struct{ ent.Schema }

func (WorkflowRun) Fields() []ent.Field { return []ent.Field{field.Int("workflow_id")} }

func (WorkflowRun) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("workflow", Workflow.Type).Ref("runs").Field("workflow_id").Unique().Required(),
	}
}

type Connection struct{ ent.Schema }

func (Connection) Mixin() []ent.Mixin { return []ent.Mixin{softdelete.Mixin{}} }

type AssetFolder struct{ ent.Schema }

func (AssetFolder) Mixin() []ent.Mixin { return []ent.Mixin{softdelete.Mixin{}} }

func (AssetFolder) Fields() []ent.Field { return []ent.Field{field.Int("parent_id").Optional()} }

func (AssetFolder) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("children", AssetFolder.Type).Annotations(softdelete.Cascade()).
			From("parent").Field("parent_id").Unique(),
	}
}
