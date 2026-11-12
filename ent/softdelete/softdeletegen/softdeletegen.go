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
		"softDeleteCascades": cascades,
		"softDeleteEnforced": enforced,
	}
}

// Hook fails generation on an undeclared relation, so a new child can't ship without a decision.
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

// A foreign key from Child to Parent. Ent reads entsql.OnDelete from the edge.To, so the soft delete
// declaration lives there too: on the parent when it defines the relation, on the child when the child does.
type Relation struct {
	On   *gen.Type
	Decl *gen.Edge
	// The child's edge holding the foreign key; nil when only the parent has an edge.
	FK     *gen.Edge
	Parent *gen.Type
	Child  *gen.Type
}

func (r Relation) Mode() softdelete.Mode { return annotation(r.Decl).Mode }

func (r Relation) Column() string { return r.Decl.Rel.Column() }

func (r Relation) String() string {
	return fmt.Sprintf("%s.%s -> %s", r.On.Name, r.Decl.Name, r.Decl.Type.Name)
}

// Ent generates the edge to a join table, so it has no definition to declare on; the join table's own
// foreign key declares instead.
func throughEdge(t *gen.Type, e *gen.Edge) bool {
	return slices.ContainsFunc(t.Edges, func(m2m *gen.Edge) bool {
		return m2m.Through != nil && m2m.Through.Name == e.Type.Name
	})
}

// Found from the child's own edges, so the edge predicate templates, which only see one type, can use it.
func childRelations(t *gen.Type) []Relation {
	var rels []Relation
	for _, e := range t.Edges {
		if !e.OwnFK() || e.M2M() {
			continue
		}
		r := Relation{On: t, Decl: e, FK: e, Parent: e.Type, Child: t}
		if e.IsInverse() {
			r.On, r.Decl = e.Type, e.Ref
		}
		rels = append(rels, r)
	}
	return rels
}

func relations(g *gen.Graph) []Relation {
	var rels []Relation
	for _, n := range g.Nodes {
		rels = append(rels, childRelations(n)...)
		for _, e := range n.Edges {
			if e.IsInverse() || e.OwnFK() || e.M2M() || e.Ref != nil || throughEdge(n, e) {
				continue
			}
			rels = append(rels, Relation{On: n, Decl: e, FK: nil, Parent: n, Child: e.Type})
		}
	}
	return rels
}

func softDeleted(t *gen.Type) bool {
	return slices.ContainsFunc(t.Fields, func(f *gen.Field) bool {
		return f.Name == softdelete.FieldDeleteTime
	})
}

func hiddenThrough(t *gen.Type) []Relation {
	return slices.DeleteFunc(childRelations(t), func(r Relation) bool {
		return r.Mode() != softdelete.ModeHide
	})
}

func tracked(t *gen.Type) bool {
	return softDeleted(t) || len(hiddenThrough(t)) > 0
}

func cascades(g *gen.Graph, parent *gen.Type) []Relation {
	return slices.DeleteFunc(relations(g), func(r Relation) bool {
		return r.Parent != parent || r.Mode() != softdelete.ModeCascade
	})
}

func enforced(g *gen.Graph) []Relation {
	return slices.DeleteFunc(relations(g), func(r Relation) bool {
		return r.Mode() != softdelete.ModeHide && r.Mode() != softdelete.ModeCascade
	})
}

func validate(g *gen.Graph) error {
	var problems []string
	for _, t := range g.Nodes {
		for _, e := range t.Edges {
			problems = append(problems, validateDeclarationSide(t, e)...)
		}
		if cycle := hideCycle(t, nil); cycle != nil {
			problems = append(problems, fmt.Sprintf(
				"%s: Hide cycle through %s; recursive edges must use Cascade",
				t.Name, strings.Join(cycle, " -> "),
			))
		}
	}
	for _, r := range relations(g) {
		problems = append(problems, validateRelation(r)...)
	}
	if len(problems) == 0 {
		return nil
	}
	return errors.New("soft delete declarations:\n  " + strings.Join(problems, "\n  "))
}

func validateDeclarationSide(t *gen.Type, e *gen.Edge) []string {
	name := fmt.Sprintf("%s.%s -> %s", t.Name, e.Name, e.Type.Name)
	// Ent silently ignores OnDelete here, which is how the two declarations drift apart.
	if e.IsInverse() && e.EntSQL() != nil && e.EntSQL().OnDelete != "" {
		return []string{fmt.Sprintf(
			"%s: ent ignores entsql.OnDelete on an inverse edge; put it on %s.%s, the edge.To",
			name, e.Type.Name, e.Ref.Name,
		)}
	}
	if annotation(e).Mode == "" {
		return nil
	}
	switch {
	case e.IsInverse():
		return []string{fmt.Sprintf(
			"%s: declare on %s.%s, the edge.To, next to entsql.OnDelete",
			name, e.Type.Name, e.Ref.Name,
		)}
	case e.M2M() || throughEdge(t, e):
		return []string{
			name + ": a many-to-many edge takes no declaration; its join table's foreign keys do",
		}
	}
	return nil
}

func validateRelation(r Relation) []string {
	mode := r.Mode()
	switch {
	case mode == "":
		if tracked(r.Parent) {
			return []string{r.String() + ": declare softdelete.Hide, Cascade or Keep"}
		}
	case !tracked(r.Parent):
		return []string{r.String() + ": parent is neither soft-deleted nor hidden"}
	case mode == softdelete.ModeKeep && annotation(r.Decl).Reason == "":
		return []string{r.String() + ": Keep needs a reason"}
	case mode == softdelete.ModeHide && r.FK == nil:
		return []string{r.String() + ": Hide needs an edge on the child"}
	case mode == softdelete.ModeCascade && !softDeleted(r.Child):
		return []string{r.String() + ": Cascade needs a child with delete_time"}
	case mode == softdelete.ModeCascade && (r.FK == nil || r.FK.Field() == nil):
		return []string{
			r.String() + ": Cascade needs the child's edge bound to a foreign key field",
		}
	}
	return nil
}

func hideCycle(t *gen.Type, path []string) []string {
	if slices.Contains(path, t.Name) {
		return append(path, t.Name)
	}
	for _, r := range hiddenThrough(t) {
		if cycle := hideCycle(r.Parent, append(path, t.Name)); cycle != nil {
			return cycle
		}
	}
	return nil
}
