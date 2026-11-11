package polygen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"entgo.io/ent/entc/gen"
)

func TestWriteOutputGeneratesSharedPredicates(t *testing.T) {
	storage, err := gen.NewStorage("sql")
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	g, err := LoadGraph("./testdata/schema", &gen.Config{Target: target, Package: "example.com/ent", Storage: storage})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range g.Nodes {
		if err := os.MkdirAll(filepath.Join(target, n.PackageDir()), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeOutput(g); err != nil {
		t.Fatal(err)
	}
	read := func(rel string) string {
		b, err := os.ReadFile(filepath.Join(target, rel))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}

	pattern := read("actor/poly_pattern.go")
	for _, want := range []string{
		"func FirstNameEQ(v string) Predicate",
		"func FirstNameIsNil() Predicate",
		"func FirstNameContainsFold(v string) Predicate",
		"func IDIn(ids ...int) Predicate",
		"func Or(ps ...Predicate) Predicate",
	} {
		if !strings.Contains(pattern, want) {
			t.Errorf("actor predicates are missing %q", want)
		}
	}
	where := read("asset/poly_where.go")
	for _, want := range []string{
		"func HasActor() predicate.Asset",
		"func HasActorWith(preds ...actor.Predicate) predicate.Asset",
		"var ActorFields = []string{",
	} {
		if !strings.Contains(where, want) {
			t.Errorf("asset's poly edge is missing %q", want)
		}
	}
	if strings.Contains(read("poly_patterns.go"), "CollectFields") {
		t.Error("a pattern without entgql.Implements got GraphQL field collection")
	}
}
