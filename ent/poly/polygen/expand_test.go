package polygen

import (
	"slices"
	"testing"

	"entgo.io/ent/entc/gen"
)

func TestExpandTPC(t *testing.T) {
	g, err := LoadGraph("./testdata/schema", &gen.Config{})
	if err != nil {
		t.Fatal(err)
	}
	nodes := map[string]*gen.Type{}
	for _, n := range g.Nodes {
		nodes[n.Name] = n
	}
	if _, ok := nodes["Actor"]; ok {
		t.Fatal("the pattern itself must not become a table")
	}
	for _, member := range []string{"User", "Agent"} {
		if !slices.ContainsFunc(
			nodes[member].Fields,
			func(f *gen.Field) bool { return f.Name == "first_name" },
		) {
			t.Fatalf("%s is missing the pattern's field", member)
		}
	}
	tables, err := g.Tables()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		node, table, checkName, check string
		inverse                       bool
	}{
		{"Asset", "assets", "actor_set", "num_nonnulls(agent_id, user_id) <= 1", false},
		{"Note", "notes", "author_set", "num_nonnulls(agent_id, user_id) = 1", true},
	} {
		n := nodes[tc.node]
		for _, fk := range []string{"agent_id", "user_id"} {
			if !slices.ContainsFunc(
				n.Fields,
				func(f *gen.Field) bool { return f.Name == fk && f.Nillable },
			) {
				t.Fatalf("%s: missing nillable %s", tc.node, fk)
			}
		}
		for _, name := range []string{"agent", "user"} {
			i := slices.IndexFunc(n.Edges, func(e *gen.Edge) bool { return e.Name == name })
			if i < 0 || n.Edges[i].IsInverse() != tc.inverse || !n.Edges[i].Unique {
				t.Fatalf("%s: edge %s is wrong", tc.node, name)
			}
		}
		for _, tb := range tables {
			if tb.Name != tc.table {
				continue
			}
			if got := tb.Annotation.Checks[tc.checkName]; got != tc.check {
				t.Fatalf("%s: check = %q, want %q", tc.table, got, tc.check)
			}
			if len(tb.Indexes) != 2 {
				t.Fatalf("%s: %d indexes, want one per foreign key", tc.table, len(tb.Indexes))
			}
		}
	}
}
