package softdelete

import (
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"
)

func TestFilterLooksForDeletedParents(t *testing.T) {
	Register("Workflow", Node{Table: "workflows", SoftDeleted: true, Parents: nil})
	Register("Run", Node{
		Table:       "runs",
		SoftDeleted: false,
		Parents:     []Parent{{Entity: "Workflow", Column: "workflow_id"}},
	})
	Register("Step", Node{
		Table:       "steps",
		SoftDeleted: false,
		Parents:     []Parent{{Entity: "Run", Column: "run_id"}},
	})
	filter, ok := Filter("Step")
	if !ok {
		t.Fatal("no filter")
	}

	builder := sql.Dialect(dialect.Postgres)
	table := builder.Table("steps")
	s := builder.Select(table.C("id")).From(table)
	filter(s)
	query, _ := s.Query()

	want := `SELECT "steps"."id" FROM "steps" WHERE NOT (EXISTS (SELECT "runs"."id" FROM "runs" ` +
		`WHERE "runs"."id" = "steps"."run_id" AND EXISTS (SELECT "workflows"."id" FROM "workflows" ` +
		`WHERE "workflows"."id" = "runs"."workflow_id" AND "workflows"."delete_time" IS NOT NULL)))`
	if strings.Join(strings.Fields(query), " ") != want {
		t.Fatalf("query:\n%s\nwant:\n%s", query, want)
	}
}
