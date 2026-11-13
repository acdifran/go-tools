package polygen

import (
	"slices"
	"testing"

	"entgo.io/ent/dialect/sql/schema"
	"entgo.io/ent/entc/gen"
)

// A TPH pattern's table and every edge into it keep the names the pattern's own type would have given them.
func TestTPHKeepsStorageNames(t *testing.T) {
	g, err := LoadGraph("./testdata/tph", &gen.Config{Package: "example.com/ent"})
	if err != nil {
		t.Fatal(err)
	}
	tables, err := g.Tables()
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]*schema.Table{}
	for _, tb := range tables {
		byName[tb.Name] = tb
	}
	columns := func(table string) []string {
		tb := byName[table]
		if tb == nil {
			t.Fatalf("no table %s; have %v", table, keys(byName))
		}
		var out []string
		for _, c := range tb.Columns {
			out = append(out, c.Name)
		}
		return out
	}
	for table, want := range map[string][]string{
		"connections":  {"provider", "org_id", "zendesk_subdomain", "zendesk_trigger_id", "hs_portal"},
		"events":       {"connection_id"},
		"flow_sources": {"flow_id", "connection_id"},
		"notes":        {"note_conn"},
	} {
		got := columns(table)
		for _, c := range want {
			if !slices.Contains(got, c) {
				t.Errorf("%s has columns %v, want %s", table, got, c)
			}
		}
	}
	var indexes []string
	for _, idx := range byName["connections"].Indexes {
		indexes = append(indexes, idx.Name)
	}
	if !slices.Contains(indexes, "connection_org_id_provider") {
		t.Errorf("connections indexes are %v, want connection_org_id_provider", indexes)
	}
	var fks []string
	for _, tb := range tables {
		for _, fk := range tb.ForeignKeys {
			fks = append(fks, fk.Symbol)
		}
	}
	for _, want := range []string{"notes_connections_conn", "flow_sources_connection_id", "connections_orgs_connections"} {
		if !slices.Contains(fks, want) {
			t.Errorf("foreign keys are %v, want %s", fks, want)
		}
	}
}

func keys[V any](m map[string]V) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
