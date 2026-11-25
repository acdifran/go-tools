package polygen

import (
	"regexp"
	"strings"
	"testing"

	"entgo.io/ent/entc/gen"

	"github.com/acdifran/go-tools/ent/poly"
)

// A wrapper without conv panics on the first row All, First or Only converts, so every edge query has to carry it.
func TestTPHEdgeQueriesConvertRows(t *testing.T) {
	g, err := LoadGraph("./testdata/tph", &gen.Config{Package: "example.com/ent"})
	if err != nil {
		t.Fatal(err)
	}
	patterns, err := tphPatterns(g)
	if err != nil {
		t.Fatal(err)
	}
	byBase := map[string]*tphPattern{}
	for _, p := range patterns {
		byBase[p.BaseName()] = p
	}
	var b strings.Builder
	edges := 0
	for _, n := range g.Nodes {
		for _, e := range n.Edges {
			ref, ok := decode[poly.RefAnnotation](e.Annotations[poly.RefAnnotationName])
			if !ok || ref.Storage != poly.StorageTPH {
				continue
			}
			renderEdge(&b, &tphEdge{
				T:       n,
				Edge:    e,
				Name:    strings.TrimSuffix(e.Name, PatternSuffix),
				P:       byBase[e.Type.Name],
				Many:    ref.Many,
				Field:   ref.Field,
				GQL:     nil,
				Setters: map[string]bool{},
			})
			edges++
		}
	}
	if edges == 0 {
		t.Fatal("testdata has no TPH edges")
	}
	literals := regexp.MustCompile(`&\w+Query\{b: [^}]*\}`).FindAllString(b.String(), -1)
	if len(literals) == 0 {
		t.Fatalf("no query wrappers rendered:\n%s", b.String())
	}
	for _, l := range literals {
		if !strings.Contains(l, "conv: ") {
			t.Errorf("%s has no conv", l)
		}
	}
}
