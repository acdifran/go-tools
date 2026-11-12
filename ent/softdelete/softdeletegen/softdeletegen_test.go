package softdeletegen

import (
	"slices"
	"strings"
	"testing"

	"github.com/acdifran/go-tools/ent/softdelete"

	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/entc"
	"entgo.io/ent/entc/gen"
)

func loadGraph(t *testing.T) *gen.Graph {
	t.Helper()
	g, err := entc.LoadGraph("./testdata/schema", &gen.Config{})
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func edge(t *testing.T, g *gen.Graph, typeName, name string) *gen.Edge {
	t.Helper()
	for _, n := range g.Nodes {
		for _, e := range n.Edges {
			if n.Name == typeName && e.Name == name {
				return e
			}
		}
	}
	t.Fatalf("no edge %s.%s", typeName, name)
	return nil
}

func declare(e *gen.Edge, a softdelete.Annotation) {
	if e.Annotations == nil {
		e.Annotations = gen.Annotations{}
	}
	e.Annotations[softdelete.AnnotationName] = map[string]any{"Mode": a.Mode, "Reason": a.Reason}
}

func TestValidate(t *testing.T) {
	if err := validate(loadGraph(t)); err != nil {
		t.Fatalf("fixture should be fully declared: %v", err)
	}
	for name, tc := range map[string]struct {
		change func(*testing.T, *gen.Graph)
		want   string
	}{
		"undeclared child": {
			func(t *testing.T, g *gen.Graph) {
				delete(edge(t, g, "Workflow", "plans").Annotations, softdelete.AnnotationName)
			},
			"Workflow.plans -> WorkflowPlan: declare",
		},
		"keep without a reason": {
			func(t *testing.T, g *gen.Graph) { declare(edge(t, g, "Workflow", "plans"), softdelete.Keep("")) },
			"Keep needs a reason",
		},
		"cascade to a child without delete_time": {
			func(t *testing.T, g *gen.Graph) { declare(edge(t, g, "Workflow", "plans"), softdelete.Cascade()) },
			"Cascade needs a child with delete_time",
		},
		"hide on a recursive edge": {
			func(t *testing.T, g *gen.Graph) { declare(edge(t, g, "AssetFolder", "children"), softdelete.Hide()) },
			"Hide cycle",
		},
		"undeclared one-way foreign key": {
			func(t *testing.T, g *gen.Graph) {
				delete(edge(t, g, "Workflow", "event_source").Annotations, softdelete.AnnotationName)
			},
			"Workflow.event_source -> Connection: declare softdelete.Hide, Cascade or Keep",
		},
		"keep without a reason on a one-way foreign key": {
			func(t *testing.T, g *gen.Graph) { declare(edge(t, g, "Workflow", "event_source"), softdelete.Keep("")) },
			"Keep needs a reason",
		},
		"undeclared relation defined by the child": {
			func(t *testing.T, g *gen.Graph) {
				delete(edge(t, g, "Note", "workflow").Annotations, softdelete.AnnotationName)
			},
			"Note.workflow -> Workflow: declare",
		},
		"declaration on the inverse edge": {
			func(t *testing.T, g *gen.Graph) { declare(edge(t, g, "WorkflowRun", "workflow"), softdelete.Hide()) },
			"declare on Workflow.runs, the edge.To",
		},
		"on delete on the inverse edge": {
			func(t *testing.T, g *gen.Graph) {
				e := edge(t, g, "WorkflowRun", "workflow")
				e.Annotations = gen.Annotations{entsql.Annotation{}.Name(): map[string]any{"on_delete": "CASCADE"}}
			},
			"ent ignores entsql.OnDelete on an inverse edge; put it on Workflow.runs",
		},
		"hide without an edge on the child": {
			func(t *testing.T, g *gen.Graph) { declare(edge(t, g, "Workflow", "tags"), softdelete.Hide()) },
			"Workflow.tags -> Tag: Hide needs an edge on the child",
		},
	} {
		t.Run(name, func(t *testing.T) {
			g := loadGraph(t)
			tc.change(t, g)
			err := validate(g)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func names(rels []Relation) []string {
	out := make([]string, len(rels))
	for i, r := range rels {
		out[i] = r.String() + " " + string(r.Mode())
	}
	slices.Sort(out)
	return out
}

func node(t *testing.T, g *gen.Graph, name string) *gen.Type {
	t.Helper()
	for _, n := range g.Nodes {
		if n.Name == name {
			return n
		}
	}
	t.Fatalf("no type %s", name)
	return nil
}

func TestDeclarationsComeFromTheEdgeTo(t *testing.T) {
	g := loadGraph(t)
	if got, want := names(hiddenThrough(node(t, g, "Note"))), []string{
		"Note.workflow -> Workflow hide",
	}; !slices.Equal(got, want) {
		t.Errorf("Note hidden through %v, want %v", got, want)
	}
	if got, want := names(hiddenThrough(node(t, g, "WorkflowRun"))), []string{
		"Workflow.runs -> WorkflowRun hide",
	}; !slices.Equal(got, want) {
		t.Errorf("WorkflowRun hidden through %v, want %v", got, want)
	}
	if got, want := names(cascades(g, node(t, g, "Connection"))), []string{
		"Note.connection -> Connection cascade",
	}; !slices.Equal(got, want) {
		t.Errorf("Connection cascades %v, want %v", got, want)
	}
	if got, want := names(enforced(g)), []string{
		"AssetFolder.children -> AssetFolder cascade",
		"Note.connection -> Connection cascade",
		"Note.workflow -> Workflow hide",
		"Workflow.plans -> WorkflowPlan hide",
		"Workflow.runs -> WorkflowRun hide",
	}; !slices.Equal(got, want) {
		t.Errorf("enforced %v, want %v", got, want)
	}
}
