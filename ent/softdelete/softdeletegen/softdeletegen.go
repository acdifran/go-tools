package softdeletegen

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"text/template"

	"github.com/acdifran/go-tools/ent/softdelete"

	"entgo.io/ent/entc/gen"
)

func FuncMap() template.FuncMap {
	return template.FuncMap{
		"softDeleted":        softDeleted,
		"softDeleteTracked":  tracked,
		"softDeleteHides":    hiddenThrough,
		"softDeleteCascades": func(t *gen.Type) []*gen.Edge { return declared(t, softdelete.ModeCascade) },
		"softDeleteEnforced": enforced,
		"softDeleteMode":     func(e *gen.Edge) softdelete.Mode { return annotation(e).Mode },
	}
}

// Hook fails generation on an undeclared child edge, so a new child can't ship without a decision.
func Hook() gen.Hook {
	return func(next gen.Generator) gen.Generator {
		return gen.GenerateFunc(func(g *gen.Graph) error {
			if err := validate(g); err != nil {
				return err
			}
			return next.Generate(g)
		})
	}
}

func annotation(e *gen.Edge) softdelete.Annotation {
	var a softdelete.Annotation
	raw, ok := e.Annotations[softdelete.AnnotationName]
	if !ok {
		return a
	}
	// The graph holds annotations as decoded JSON from the schema snapshot.
	data, err := json.Marshal(raw)
	if err != nil {
		panic(fmt.Sprintf("encoding soft delete annotation on %s: %v", e.Name, err))
	}
	if err := json.Unmarshal(data, &a); err != nil {
		panic(fmt.Sprintf("decoding soft delete annotation on %s: %v", e.Name, err))
	}
	return a
}

func softDeleted(t *gen.Type) bool {
	return slices.ContainsFunc(t.Fields, func(f *gen.Field) bool {
		return f.Name == softdelete.FieldDeleteTime
	})
}

func hiddenThrough(t *gen.Type) []*gen.Edge {
	var edges []*gen.Edge
	for _, e := range t.Edges {
		if e.OwnFK() && e.Ref != nil && annotation(e.Ref).Mode == softdelete.ModeHide {
			edges = append(edges, e)
		}
	}
	return edges
}

func tracked(t *gen.Type) bool {
	return softDeleted(t) || len(hiddenThrough(t)) > 0
}

func declared(t *gen.Type, mode softdelete.Mode) []*gen.Edge {
	var edges []*gen.Edge
	for _, e := range t.Edges {
		if annotation(e).Mode == mode {
			edges = append(edges, e)
		}
	}
	return edges
}

func enforced(t *gen.Type) []*gen.Edge {
	return append(declared(t, softdelete.ModeHide), declared(t, softdelete.ModeCascade)...)
}

// Ent generates the edge to a join table, so it has no definition to declare on; the join table's own
// foreign key declares instead.
func throughEdge(t *gen.Type, e *gen.Edge) bool {
	return slices.ContainsFunc(t.Edges, func(m2m *gen.Edge) bool {
		return m2m.Through != nil && m2m.Through.Name == e.Type.Name
	})
}

func childEdge(t *gen.Type, e *gen.Edge) bool {
	return !e.OwnFK() && !e.M2M() && !throughEdge(t, e)
}

// The parent has no edge to carry a declaration, so a Keep goes on the foreign key itself.
func oneWayToTracked(e *gen.Edge) bool {
	return e.OwnFK() && !e.M2M() && e.Ref == nil && tracked(e.Type)
}

func validate(g *gen.Graph) error {
	var problems []string
	for _, t := range g.Nodes {
		for _, e := range t.Edges {
			problems = append(problems, validateEdge(t, e)...)
		}
		if cycle := hideCycle(t, nil); cycle != nil {
			problems = append(problems, fmt.Sprintf(
				"%s: Hide cycle through %s; recursive edges must use Cascade",
				t.Name, strings.Join(cycle, " -> "),
			))
		}
	}
	if len(problems) == 0 {
		return nil
	}
	return errors.New("soft delete declarations:\n  " + strings.Join(problems, "\n  "))
}

func validateEdge(t *gen.Type, e *gen.Edge) []string {
	name := fmt.Sprintf("%s.%s -> %s", t.Name, e.Name, e.Type.Name)
	a := annotation(e)
	switch {
	case a.Mode == "":
		if tracked(t) && childEdge(t, e) {
			return []string{name + ": declare softdelete.Hide, Cascade or Keep"}
		}
		if oneWayToTracked(e) {
			return []string{
				name + ": declare softdelete.Keep, or add an inverse edge on the parent that declares Hide",
			}
		}
		return nil
	case oneWayToTracked(e) && a.Mode != softdelete.ModeKeep:
		return []string{name + ": Hide and Cascade go on an inverse edge on the parent"}
	case oneWayToTracked(e) && a.Reason == "":
		return []string{name + ": Keep needs a reason"}
	case oneWayToTracked(e):
		return nil
	case !childEdge(t, e):
		return []string{
			name + ": only an edge whose child row holds the foreign key takes a declaration",
		}
	case !tracked(t):
		return []string{name + ": parent is neither soft-deleted nor hidden"}
	case a.Mode == softdelete.ModeKeep && a.Reason == "":
		return []string{name + ": Keep needs a reason"}
	case a.Mode == softdelete.ModeHide && e.Ref == nil:
		return []string{name + ": Hide needs an inverse edge on the child"}
	case a.Mode == softdelete.ModeCascade && !softDeleted(e.Type):
		return []string{name + ": Cascade needs a child with delete_time"}
	case a.Mode == softdelete.ModeCascade && (e.Ref == nil || e.Ref.Field() == nil):
		return []string{name + ": Cascade needs an inverse edge bound to a foreign key field"}
	}
	return nil
}

func hideCycle(t *gen.Type, path []string) []string {
	if slices.Contains(path, t.Name) {
		return append(path, t.Name)
	}
	for _, e := range hiddenThrough(t) {
		if cycle := hideCycle(e.Type, append(path, t.Name)); cycle != nil {
			return cycle
		}
	}
	return nil
}
