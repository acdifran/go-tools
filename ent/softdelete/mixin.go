package softdelete

import (
	"context"
	"fmt"
	"time"

	"entgo.io/contrib/entgql"
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/dialect/sql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"entgo.io/ent/schema/mixin"
)

// Mutation is what the mixin needs from a soft-deletable entity's mutation. Ent generates the first methods;
// templates/softdelete generates the SoftDelete ones, which need the app's own client and transaction types.
type Mutation interface {
	ent.Mutation
	SetOp(ent.Op)
	SetDeleteTime(time.Time)
	DeleteTime() (time.Time, bool)
	WhereP(...func(*sql.Selector))
	SoftDeleteDispatch(ctx context.Context) (ent.Value, error)
	SoftDeleteCascade(ctx context.Context, stamp time.Time, next ent.Mutator) (ent.Value, error)
}

// Query is generated onto every query by templates/softdelete.
type Query interface {
	SoftDeleteEntity() string
	SoftDeleteWhereP(...func(*sql.Selector))
}

type Mixin struct {
	mixin.Schema
}

func (Mixin) Fields() []ent.Field {
	return []ent.Field{
		field.Time(FieldDeleteTime).
			Optional().
			Nillable().
			Annotations(entgql.Skip(entgql.SkipAll)),
	}
}

// Partial because deleted rows are the small set, and a purge only ever looks for those.
func (Mixin) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields(FieldDeleteTime).
			Annotations(entsql.IndexWhere("delete_time IS NOT NULL")),
	}
}

// Interceptor goes on a mixin every entity has, not on Mixin, because a hidden child has no soft delete field
// of its own.
func Interceptor() ent.Interceptor {
	return ent.TraverseFunc(func(ctx context.Context, q ent.Query) error {
		sq, ok := q.(Query)
		if !ok {
			return nil
		}
		if filter, ok := Filter(sq.SoftDeleteEntity()); ok && !Skipped(ctx) {
			sq.SoftDeleteWhereP(filter)
		}
		return nil
	})
}

type deletingKey struct{}

// Unexported so only this mixin can write delete_time: a direct write would skip the cascades and
// the shared stamp a restore relies on.
func deleting(ctx context.Context) context.Context {
	return context.WithValue(ctx, deletingKey{}, true)
}

func isDeleting(ctx context.Context) bool {
	return ctx.Value(deletingKey{}) != nil
}

func on(
	op ent.Op,
	fn func(ctx context.Context, m Mutation, next ent.Mutator) (ent.Value, error),
) ent.Hook {
	return func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			if !m.Op().Is(op) {
				return next.Mutate(ctx, m)
			}
			mx, ok := m.(Mutation)
			if !ok {
				return nil, fmt.Errorf(
					"softdelete: %T is missing the generated SoftDelete methods; load templates/softdelete",
					m,
				)
			}
			return fn(ctx, mx, next)
		})
	}
}

func (Mixin) Hooks() []ent.Hook {
	return []ent.Hook{
		// Re-dispatches the mutation as an update, so later delete hooks on the
		// same schema are not reached; update hooks run instead.
		on(
			ent.OpDeleteOne|ent.OpDelete,
			func(ctx context.Context, m Mutation, next ent.Mutator) (ent.Value, error) {
				if Skipped(ctx) {
					return next.Mutate(ctx, m)
				}
				m.WhereP(NotDeleted())
				m.SetOp(ent.OpUpdate)
				m.SetDeleteTime(time.Now())
				return m.SoftDeleteDispatch(deleting(ctx))
			},
		),
		on(
			ent.OpCreate|ent.OpUpdate|ent.OpUpdateOne,
			func(ctx context.Context, m Mutation, next ent.Mutator) (ent.Value, error) {
				if _, setting := m.DeleteTime(); setting && !isDeleting(ctx) {
					return nil, ErrDirectDeleteTimeWrite
				}
				return next.Mutate(ctx, m)
			},
		),
		// Interceptors only filter queries; this keeps bulk updates off deleted rows too.
		on(
			ent.OpUpdate,
			func(ctx context.Context, m Mutation, next ent.Mutator) (ent.Value, error) {
				if !Skipped(ctx) {
					m.WhereP(NotDeleted())
				}
				return next.Mutate(ctx, m)
			},
		),
		// On update because the delete hook above re-dispatches before delete hooks are reached.
		on(
			ent.OpUpdate,
			func(ctx context.Context, m Mutation, next ent.Mutator) (ent.Value, error) {
				stamp, stamping := m.DeleteTime()
				if !stamping || Skipped(ctx) {
					return next.Mutate(ctx, m)
				}
				return m.SoftDeleteCascade(ctx, stamp, next)
			},
		),
	}
}
