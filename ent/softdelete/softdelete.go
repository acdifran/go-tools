// Package softdelete imports nothing from an app's generated ent package, so schemas, mixins and the
// generated code can all use it.
package softdelete

import (
	"context"
	"errors"

	"entgo.io/ent/dialect/sql"
)

const (
	FieldDeleteTime = "delete_time"
	AnnotationName  = "SoftDelete"
	idColumn        = "id"
)

var ErrDirectDeleteTimeWrite = errors.New("delete_time can only be set by deleting the row")

type Mode string

const (
	ModeHide    Mode = "hide"
	ModeCascade Mode = "cascade"
	ModeKeep    Mode = "keep"
)

// It goes on the edge.To of a relation, next to entsql.OnDelete, whichever side that is. Generation fails on a
// relation into a soft-deleted entity without one, and the filters and cascades are generated from it.
type Annotation struct {
	Mode   Mode
	Reason string
}

func (Annotation) Name() string {
	return AnnotationName
}

// Hide filters children out of reads without touching their rows, so restoring the parent brings them back.
// It costs an EXISTS per hop on every read of the child.
func Hide() Annotation {
	return Annotation{Mode: ModeHide, Reason: ""}
}

// Cascade stamps the children with the parent's delete_time, so a restore can tell this delete from
// children deleted earlier. A soft delete is an update, so ON DELETE CASCADE never fires for it.
// It is for recursive edges, which a fixed query cannot follow, and for tables where Hide's read cost matters.
func Cascade() Annotation {
	return Annotation{Mode: ModeCascade, Reason: ""}
}

// Keep is for children that still mean something once the parent is gone.
func Keep(reason string) Annotation {
	return Annotation{Mode: ModeKeep, Reason: reason}
}

// A Hide or Cascade relation. On and Name are the edge.To it is declared on, on either the parent or the child.
type Edge struct {
	On     string
	Name   string
	Parent string
	Child  string
	Mode   Mode
}

type Parent struct {
	Entity string
	Column string
}

// A row is hidden when it or anything up its Parents chain is soft-deleted.
type Node struct {
	Table       string
	SoftDeleted bool
	Parents     []Parent
}

// Filled by the generated code, so an entity's package can filter another entity without importing
// it, which would be a cycle.
var nodes = map[string]Node{}

func Register(entity string, node Node) {
	nodes[entity] = node
}

// Looks for deleted parents, not live ones: deleted rows are the small set, while "has a live
// parent" makes Postgres hash the whole parent table on every read.
func Filter(entity string) (func(*sql.Selector), bool) {
	node, ok := nodes[entity]
	if !ok {
		return nil, false
	}
	return func(s *sql.Selector) {
		if node.SoftDeleted {
			s.Where(sql.IsNull(s.C(FieldDeleteTime)))
		}
		for _, parent := range node.Parents {
			s.Where(sql.Not(sql.Exists(deletedParent(s, parent))))
		}
	}, true
}

func deletedParent(child *sql.Selector, parent Parent) *sql.Selector {
	node := nodes[parent.Entity]
	builder := sql.Dialect(child.Dialect())
	table := builder.Table(node.Table)
	selector := builder.Select(table.C(idColumn)).From(table)
	var deleted []*sql.Predicate
	if node.SoftDeleted {
		deleted = append(deleted, sql.NotNull(table.C(FieldDeleteTime)))
	}
	for _, grandparent := range node.Parents {
		deleted = append(deleted, sql.Exists(deletedParent(selector, grandparent)))
	}
	return selector.Where(sql.And(
		sql.ColumnsEQ(table.C(idColumn), child.C(parent.Column)),
		sql.Or(deleted...),
	))
}

// Exclude runs inside generated edge predicates, where interceptors do not apply.
func Exclude(s *sql.Selector, entity string) {
	if filter, ok := Filter(entity); ok && !Skipped(s.Context()) {
		filter(s)
	}
}

type skipKey struct{}

// Skip returns a ctx in which deletes are hard and queries include deleted rows.
func Skip(parent context.Context) context.Context {
	return context.WithValue(parent, skipKey{}, true)
}

func Skipped(ctx context.Context) bool {
	skip, _ := ctx.Value(skipKey{}).(bool)
	return skip
}

func NotDeleted() func(*sql.Selector) {
	return sql.FieldIsNull(FieldDeleteTime)
}
