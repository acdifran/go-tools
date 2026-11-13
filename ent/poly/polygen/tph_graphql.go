package polygen

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"entgo.io/ent/entc/gen"
	"entgo.io/ent/schema/field"
	"github.com/acdifran/go-tools/ent/poly"
	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/parser"
)

// SchemaHookWith is SchemaHook for a schema with TPH patterns. sources are globs of the hand-written GraphQL
// files: fields a file adds with "extend interface <Pattern>" are copied onto every variant, which GraphQL requires
// of an interface's implementations.
func SchemaHookWith(sources ...string) func(*gen.Graph, *ast.Schema) error {
	return func(g *gen.Graph, s *ast.Schema) error {
		if err := SchemaHook(g, s); err != nil {
			return err
		}
		return tphSchemaHook(g, s, sources)
	}
}

func interfaceExtensions(sources []string, name string) (ast.FieldList, error) {
	var out ast.FieldList
	for _, glob := range sources {
		paths, err := filepath.Glob(glob)
		if err != nil {
			return nil, err
		}
		for _, path := range paths {
			b, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			doc, err := parser.ParseSchema(&ast.Source{
				Name:    path,
				Input:   string(b),
				BuiltIn: false,
			})
			if err != nil {
				return nil, err
			}
			for _, ext := range doc.Extensions {
				if ext.Kind == ast.Interface && ext.Name == name {
					out = append(out, ext.Fields...)
				}
			}
		}
	}
	return out, nil
}

func tphSchemaHook(g *gen.Graph, s *ast.Schema, sources []string) error {
	patterns, err := tphPatterns(g)
	if err != nil {
		return err
	}
	camel := gen.Funcs["camel"].(func(string) string)
	for _, p := range patterns {
		B, P := p.BaseName(), p.Name
		renameTypes(s, g.Package, B, P)
		def, ok := s.Types[P]
		if !ok {
			return fmt.Errorf("%s: entgql generated no GraphQL type for its table", P)
		}
		def.Kind = ast.Interface
		def.Directives = ast.DirectiveList{goModel(g.Package + "." + P)}
		for _, suffix := range []string{"Connection", "Edge"} {
			if d, ok := s.Types[P+suffix]; ok {
				d.Directives = ast.DirectiveList{goModel(g.Package + "." + P + suffix)}
			}
		}
		ext, err := interfaceExtensions(sources, P)
		if err != nil {
			return err
		}
		for _, v := range p.Members {
			vd := &ast.Definition{
				Kind:       ast.Object,
				Name:       v.Name,
				Interfaces: append(append([]string{}, def.Interfaces...), P),
				Directives: ast.DirectiveList{goModel(g.Package + "." + v.Name)},
				Fields:     append(append(ast.FieldList{}, def.Fields...), ext...),
			}
			for _, f := range v.Fields {
				if f.Base.Sensitive() {
					continue
				}
				t := scalarOf(f.Base)
				if t == "" {
					continue
				}
				typ := ast.NamedType(t, nil)
				if f.Required {
					typ = ast.NonNullNamedType(t, nil)
				}
				vd.Fields = append(vd.Fields, &ast.FieldDefinition{Name: camel(f.Snake), Type: typ})
			}
			s.Types[v.Name] = vd
		}
	}
	for _, n := range g.Nodes {
		def, ok := s.Types[n.Name]
		if !ok {
			continue
		}
		where := s.Types[n.Name+"WhereInput"]
		for _, e := range n.Edges {
			ref, ok := decode[poly.RefAnnotation](e.Annotations[poly.RefAnnotationName])
			if !ok || ref.Storage != poly.StorageTPH {
				continue
			}
			name := strings.TrimSuffix(e.Name, PatternSuffix)
			if f := def.Fields.ForName(camel(e.Name)); f != nil {
				f.Name = camel(name)
				f.Directives = withGoField(f.Directives, pascalOf(name))
			}
			if where == nil {
				continue
			}
			for _, suffix := range []string{"", "With"} {
				if f := where.Fields.ForName("has" + pascalOf(e.Name) + suffix); f != nil {
					f.Name = "has" + pascalOf(name) + suffix
					f.Directives = withGoField(f.Directives, "Has"+pascalOf(e.Name)+suffix)
				}
			}
		}
	}
	return nil
}

func withGoField(ds ast.DirectiveList, name string) ast.DirectiveList {
	var out ast.DirectiveList
	for _, d := range ds {
		if d.Name != "goField" {
			out = append(out, d)
		}
	}
	return append(out, &ast.Directive{
		Name: "goField",
		Arguments: ast.ArgumentList{{
			Name:  "name",
			Value: &ast.Value{Kind: ast.StringValue, Raw: name},
		}},
	})
}

// A variant field of any other type stays out of GraphQL until it has a scalar here.
var scalars = map[field.Type]string{
	field.TypeString:  "String",
	field.TypeBool:    "Boolean",
	field.TypeInt:     "Int",
	field.TypeInt8:    "Int",
	field.TypeInt16:   "Int",
	field.TypeInt32:   "Int",
	field.TypeInt64:   "Int",
	field.TypeUint:    "Int",
	field.TypeUint8:   "Int",
	field.TypeUint16:  "Int",
	field.TypeUint32:  "Int",
	field.TypeFloat32: "Float",
	field.TypeFloat64: "Float",
	field.TypeTime:    "Time",
}

func scalarOf(f *gen.Field) string { return scalars[f.Type.Type] }

// GraphQL names take the pattern's name (ConnectionPatternWhereInput becomes ConnectionWhereInput); Go keeps
// entgql's, because the pattern's own name is its Go interface.
func renameTypes(s *ast.Schema, pkg, from, to string) {
	rename := func(n string) string {
		if strings.HasPrefix(n, from) {
			return to + strings.TrimPrefix(n, from)
		}
		return n
	}
	var fixType func(t *ast.Type)
	fixType = func(t *ast.Type) {
		if t == nil {
			return
		}
		t.NamedType = rename(t.NamedType)
		fixType(t.Elem)
	}
	types := map[string]*ast.Definition{}
	for name, d := range s.Types {
		if renamed := rename(d.Name); renamed != d.Name {
			if d.Directives.ForName("goModel") == nil {
				d.Directives = append(d.Directives, goModel(pkg+"."+d.Name))
			}
			d.Name = renamed
		}
		types[rename(name)] = d
		for i, iface := range d.Interfaces {
			d.Interfaces[i] = rename(iface)
		}
		for i, t := range d.Types {
			d.Types[i] = rename(t)
		}
		for _, f := range d.Fields {
			fixType(f.Type)
			for _, a := range f.Arguments {
				fixType(a.Type)
			}
		}
	}
	s.Types = types
}
