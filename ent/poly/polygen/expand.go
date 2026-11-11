package polygen

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"entgo.io/contrib/entgql"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/entc/load"
	"entgo.io/ent/schema/field"
	"github.com/acdifran/go-tools/ent/poly"
)

type PolyEdge struct {
	Name     string
	Pattern  string
	Optional bool
	Members  []PolyMember
}

type PolyMember struct {
	Type  string
	Field string
	Edge  string
}

func decode[T any](v any) (T, bool) {
	var out T
	if v == nil {
		return out, false
	}
	b, err := json.Marshal(v)
	if err != nil || json.Unmarshal(b, &out) != nil {
		return out, false
	}
	return out, true
}

func expand(spec *load.SchemaSpec) error {
	members := map[string][]*load.Schema{}
	for _, s := range spec.Schemas {
		if m, ok := decode[poly.MemberAnnotation](s.Annotations[poly.MemberAnnotationName]); ok {
			members[m.Pattern] = append(members[m.Pattern], s)
		}
	}
	for p := range members {
		sort.Slice(
			members[p],
			func(i, j int) bool { return members[p][i].Name < members[p][j].Name },
		)
	}
	for _, s := range spec.Schemas {
		var kept []*load.Edge
		var polyEdges []PolyEdge
		for _, e := range s.Edges {
			if e.Type != poly.PlaceholderType {
				kept = append(kept, e)
				continue
			}
			ref, ok := decode[poly.RefAnnotation](e.Annotations[poly.RefAnnotationName])
			if !ok {
				return fmt.Errorf("%s.%s: unreadable poly edge", s.Name, e.Name)
			}
			if len(members[ref.Pattern]) == 0 {
				return fmt.Errorf(
					"%s.%s: pattern %s has no members; add poly.Member(%s{}) to a schema's Mixin",
					s.Name,
					e.Name,
					ref.Pattern,
					ref.Pattern,
				)
			}
			edges, pe, err := expandTPC(s, e.Name, ref, members[ref.Pattern])
			if err != nil {
				return err
			}
			kept = append(kept, edges...)
			polyEdges = append(polyEdges, pe)
		}
		s.Edges = kept
		if len(polyEdges) > 0 {
			if s.Annotations == nil {
				s.Annotations = map[string]any{}
			}
			s.Annotations[poly.EdgesAnnotationName] = asJSON(polyEdges)
		}
	}
	return nil
}

func expandTPC(
	s *load.Schema,
	name string,
	ref poly.RefAnnotation,
	members []*load.Schema,
) ([]*load.Edge, PolyEdge, error) {
	pe := PolyEdge{Name: name, Pattern: ref.Pattern, Optional: ref.Optional, Members: nil}
	var edges []*load.Edge
	cols := make([]string, len(members))
	for i, m := range members {
		fk := snake(m.Name) + "_id"
		cols[i] = fk
		f, err := fkField(m, fk, ref.Immutable)
		if err != nil {
			return nil, pe, fmt.Errorf("%s.%s: %w", s.Name, name, err)
		}
		if err := addField(s, f); err != nil {
			return nil, pe, err
		}
		e := &load.Edge{
			Name:        snake(m.Name),
			Type:        m.Name,
			Unique:      true,
			Field:       fk,
			Immutable:   ref.Immutable,
			Annotations: map[string]any{},
		}
		if ref.Ref != "" {
			e.Inverse, e.RefName = true, ref.Ref
		}
		for _, anns := range []map[string]json.RawMessage{ref.All, ref.Members[m.Name]} {
			for k, raw := range anns {
				var v any
				if err := json.Unmarshal(raw, &v); err != nil {
					return nil, pe, err
				}
				e.Annotations[k] = v
			}
		}
		edges = append(edges, e)
		s.Indexes = append(s.Indexes, &load.Index{Fields: []string{fk}})
		pe.Members = append(pe.Members, PolyMember{Type: m.Name, Field: fk, Edge: e.Name})
	}
	op := "= 1"
	if ref.Optional {
		op = "<= 1"
	}
	addCheck(s, name+"_set", fmt.Sprintf("num_nonnulls(%s) %s", strings.Join(cols, ", "), op))
	ant := entgql.Annotation{}
	if a, ok := decode[entgql.Annotation](s.Annotations[ant.Name()]); ok {
		ant = a
	}
	merged := ant.Merge(entgql.CustomManualCollectedFields(entgql.NewCustomManualCollectedField(
		name,
	)))
	if len(ref.Implements) > 0 {
		merged = merged.(entgql.Annotation).Merge(entgql.Implements(ref.Implements...))
	}
	if s.Annotations == nil {
		s.Annotations = map[string]any{}
	}
	s.Annotations[ant.Name()] = asJSON(merged)
	return edges, pe, nil
}

// The foreign key takes the member's id type, so a pulid member gets a pulid column.
func fkField(m *load.Schema, name string, immutable bool) (*load.Field, error) {
	for _, f := range m.Fields {
		if f.Name != "id" {
			continue
		}
		fk := &load.Field{
			Name:         name,
			Info:         f.Info,
			ValueScanner: f.ValueScanner,
			Size:         f.Size,
			SchemaType:   f.SchemaType,
			Optional:     true,
			Nillable:     true,
			Immutable:    immutable,
		}
		return fk, nil
	}
	d := field.Int(name).Optional().Nillable()
	if immutable {
		d.Immutable()
	}
	return load.NewField(d.Descriptor())
}

func addField(s *load.Schema, f *load.Field) error {
	for _, existing := range s.Fields {
		if existing.Name == f.Name {
			return fmt.Errorf(
				"%s: poly edge field %q collides with an existing field",
				s.Name,
				f.Name,
			)
		}
	}
	s.Fields = append(s.Fields, f)
	return nil
}

func addCheck(s *load.Schema, name, expr string) {
	if s.Annotations == nil {
		s.Annotations = map[string]any{}
	}
	key := entsql.Annotation{}.Name()
	ant, _ := decode[entsql.Annotation](s.Annotations[key])
	if ant.Checks == nil {
		ant.Checks = map[string]string{}
	}
	ant.Checks[name] = expr
	s.Annotations[key] = asJSON(ant)
}

// Ent's templates read annotations as decoded JSON, the form the loader hands them, not as typed structs.
func asJSON(v any) any {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		panic(err)
	}
	return out
}

func snake(s string) string {
	var b strings.Builder
	rs := []rune(s)
	for i, r := range rs {
		if unicode.IsUpper(r) {
			if i > 0 &&
				(unicode.IsLower(rs[i-1]) || (i+1 < len(rs) && unicode.IsLower(rs[i+1]) && unicode.IsUpper(rs[i-1]))) {
				b.WriteByte('_')
			}
			b.WriteRune(unicode.ToLower(r))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
