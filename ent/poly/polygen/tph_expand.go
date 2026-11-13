package polygen

import (
	"encoding/json"
	"fmt"
	"strings"

	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/entc/gen"
	"entgo.io/ent/entc/load"
	"entgo.io/ent/schema/edge"
	"github.com/acdifran/go-tools/ent/poly"
)

// PatternSuffix ends the ent edge an edge into a TPH pattern becomes, which frees the edge's own name for the
// generated API that returns the pattern's interface.
const PatternSuffix = "_pattern"

type tphBase struct {
	s    *load.Schema
	a    poly.BaseAnnotation
	name string
}

func tphBases(spec *load.SchemaSpec) map[string]*tphBase {
	out := map[string]*tphBase{}
	for _, s := range spec.Schemas {
		a, ok := decode[poly.BaseAnnotation](s.Annotations[poly.BaseAnnotationName])
		if ok && a.Storage == poly.StorageTPH {
			out[a.Pattern] = &tphBase{s: s, a: a, name: a.Pattern}
		}
	}
	return out
}

func gfunc(name string) func(string) string { return gen.Funcs[name].(func(string) string) }

func tableOf(s *load.Schema) string {
	if a, ok := decode[entsql.Annotation](
		s.Annotations[entsql.Annotation{}.Name()],
	); ok &&
		a.Table != "" {
		return a.Table
	}
	return gfunc("snake")(gfunc("plural")(s.Name))
}

func edgeByName(s *load.Schema, name string) *load.Edge {
	for _, e := range s.Edges {
		if e.Name == name {
			return e
		}
	}
	return nil
}

// expandTPH turns edges into a TPH pattern into ent edges to its generated table, and pins every name ent derives
// from a type or edge name to what it was before the pattern existed: the table's type is <Pattern>Pattern and the
// edges are renamed, but no column, index, constraint or join table moves.
func expandTPH(spec *load.SchemaSpec) error {
	bases := tphBases(spec)
	if len(bases) == 0 {
		return nil
	}
	for _, s := range spec.Schemas {
		for i, e := range s.Edges {
			if e.Type != poly.PlaceholderType {
				continue
			}
			ref, ok := decode[poly.RefAnnotation](e.Annotations[poly.RefAnnotationName])
			if !ok || ref.Storage != poly.StorageTPH {
				continue
			}
			b := bases[ref.Pattern]
			if b == nil {
				return fmt.Errorf(
					"%s.%s: TPH pattern %s has no generated table",
					s.Name,
					e.Name,
					ref.Pattern,
				)
			}
			ne, err := tphEntEdge(s, e.Name, ref, b)
			if err != nil {
				return err
			}
			s.Edges[i] = ne
		}
	}
	for _, b := range bases {
		if err := pinIndexes(b); err != nil {
			return err
		}
		pinTableEdges(spec, b)
	}
	return nil
}

// An edge the table declares, with no field on its inverse, names its column (or join table) after the table's
// type.
func pinTableEdges(spec *load.SchemaSpec, b *tphBase) {
	label := gfunc("snake")(b.name)
	for _, e := range b.s.Edges {
		if e.Inverse || e.Field != "" || e.StorageKey != nil {
			continue
		}
		child := schemaByName(spec, e.Type)
		inv := inverseOf(child, b.s.Name, e.Name)
		if inv != nil && inv.Field != "" {
			continue
		}
		switch {
		case e.Unique || (inv != nil && inv.Unique):
			setStorage(e, "", nil, []string{label + "_" + gfunc("snake")(e.Name)})
		case inv != nil:
			setStorage(
				e,
				label+"_"+e.Name,
				nil,
				[]string{label + "_id", gfunc("snake")(child.Name) + "_id"},
			)
		default:
			setStorage(
				e,
				label+"_"+e.Name,
				nil,
				[]string{label + "_id", gfunc("singular")(e.Name) + "_id"},
			)
		}
	}
}

func tphEntEdge(
	s *load.Schema,
	name string,
	ref poly.RefAnnotation,
	b *tphBase,
) (*load.Edge, error) {
	label := gfunc("snake")(b.name)
	slabel := gfunc("snake")(s.Name)
	ne := &load.Edge{
		Name:        name + PatternSuffix,
		Type:        b.s.Name,
		Unique:      !ref.Many,
		Required:    !ref.Optional,
		Immutable:   ref.Immutable,
		Field:       ref.Field,
		Annotations: map[string]any{},
	}
	for k, raw := range ref.All {
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, err
		}
		ne.Annotations[k] = v
	}
	ne.Annotations[poly.RefAnnotationName] = asJSON(ref)
	if ref.Inverse {
		ne.Inverse, ne.RefName = true, ref.Ref
		if edgeByName(b.s, ref.Ref) == nil {
			return nil, fmt.Errorf(
				"%s.%s: pattern %s has no edge %q",
				s.Name,
				name,
				b.name,
				ref.Ref,
			)
		}
		return ne, nil
	}
	inv := inverseOf(b.s, s.Name, name)
	if inv != nil {
		inv.RefName = ne.Name
	}
	symbol := func(owner, refT string) []string { return []string{fmt.Sprintf("%s_%s_%s", owner, refT, name)} }
	column := slabel + "_" + gfunc("snake")(name)
	switch {
	case ref.Many && inv != nil && inv.Unique && inv.Field != "":
		setStorage(ne, "", symbol(tableOf(b.s), tableOf(s)), nil)
	case ref.Many && inv != nil && inv.Unique:
		setStorage(ne, "", symbol(tableOf(b.s), tableOf(s)), []string{column})
	case ref.Many && inv != nil:
		setStorage(ne, slabel+"_"+name, nil, []string{slabel + "_id", label + "_id"})
	case ref.Many:
		setStorage(
			ne,
			slabel+"_"+name,
			nil,
			[]string{slabel + "_id", gfunc("singular")(name) + "_id"},
		)
	case ref.Field == "":
		setStorage(ne, "", symbol(tableOf(s), tableOf(b.s)), []string{column})
	default:
		setStorage(ne, "", symbol(tableOf(s), tableOf(b.s)), nil)
	}
	return ne, nil
}

func setStorage(e *load.Edge, table string, symbols, columns []string) {
	if e.StorageKey == nil {
		e.StorageKey = &edge.StorageKey{Table: "", Symbols: nil, Columns: nil}
	}
	if table != "" && e.StorageKey.Table == "" {
		e.StorageKey.Table = table
	}
	if len(symbols) > 0 && len(e.StorageKey.Symbols) == 0 {
		e.StorageKey.Symbols = symbols
	}
	if len(columns) > 0 && len(e.StorageKey.Columns) == 0 {
		e.StorageKey.Columns = columns
	}
}

func schemaByName(spec *load.SchemaSpec, name string) *load.Schema {
	for _, s := range spec.Schemas {
		if s.Name == name {
			return s
		}
	}
	return nil
}

func inverseOf(s *load.Schema, owner, assoc string) *load.Edge {
	if s == nil {
		return nil
	}
	for _, e := range s.Edges {
		if e.Inverse && e.Type == owner && e.RefName == assoc {
			return e
		}
	}
	return nil
}

// Ent names an index after its type; the table keeps the names the pattern's own type would give it.
func pinIndexes(b *tphBase) error {
	for _, idx := range b.s.Indexes {
		if idx.StorageKey != "" {
			continue
		}
		cols := append([]string{}, idx.Fields...)
		for _, en := range idx.Edges {
			e := edgeByName(b.s, en)
			if e == nil || e.Field == "" {
				return fmt.Errorf(
					"%s: index on edge %q needs a StorageKey, since the edge has no field to name its column",
					b.name,
					en,
				)
			}
			cols = append(cols, e.Field)
		}
		idx.StorageKey = strings.ToLower(b.name) + "_" + strings.Join(cols, "_")
	}
	return nil
}
