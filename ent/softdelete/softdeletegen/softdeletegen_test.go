package softdeletegen

import (
	"strings"
	"testing"

	"github.com/acdifran/go-tools/ent/softdelete"

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
			"Workflow.event_source -> Connection: declare softdelete.Keep",
		},
		"hide on a one-way foreign key": {
			func(t *testing.T, g *gen.Graph) { declare(edge(t, g, "Workflow", "event_source"), softdelete.Hide()) },
			"Hide and Cascade go on an inverse edge",
		},
		"keep without a reason on a one-way foreign key": {
			func(t *testing.T, g *gen.Graph) { declare(edge(t, g, "Workflow", "event_source"), softdelete.Keep("")) },
			"Keep needs a reason",
		},
		"declaration on the foreign key side": {
			func(t *testing.T, g *gen.Graph) { declare(edge(t, g, "WorkflowRun", "workflow"), softdelete.Hide()) },
			"only an edge whose child row holds the foreign key",
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
