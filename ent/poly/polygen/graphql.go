package polygen

import (
	"fmt"
	"go/token"
	"strings"
	"unicode"

	"entgo.io/ent/entc/gen"
	"github.com/vektah/gqlparser/v2/ast"
)

// SchemaHook goes to entgql.WithSchemaHook. Only a pattern with entgql.Implements(<its own name>) gets GraphQL.
func SchemaHook(g *gen.Graph, s *ast.Schema) error {
	m, err := buildModel(g)
	if err != nil {
		return err
	}
	camel := gen.Funcs["camel"].(func(string) string)
	for _, p := range m.Patterns {
		if !p.GraphQL {
			continue
		}
		methods, err := graphqlMethods(p)
		if err != nil {
			return err
		}
		first, ok := s.Types[p.Members[0].T.Name]
		if !ok {
			return fmt.Errorf(
				"pattern %s: member %s has no GraphQL type",
				p.Name,
				p.Members[0].T.Name,
			)
		}
		iface := &ast.Definition{
			Kind: ast.Interface,
			Name: p.Name,
			// Relay only refetches fragments on Node or what implements it.
			Interfaces: []string{"Node"},
			Directives: ast.DirectiveList{goModel(g.Package + "." + p.Name)},
		}
		names := []string{"id"}
		for _, f := range p.Shared {
			names = append(names, camel(f.Name))
		}
		for _, name := range names {
			if fd := first.Fields.ForName(name); fd != nil {
				iface.Fields = append(iface.Fields, fd)
			}
		}
		iface.Fields = append(iface.Fields, methods...)
		if _, taken := s.Types[p.Name]; taken {
			return fmt.Errorf("pattern %s: GraphQL already has a type named %s", p.Name, p.Name)
		}
		s.Types[p.Name] = iface
		for _, mem := range p.Members {
			def := s.Types[mem.T.Name]
			for _, fd := range methods {
				if def.Fields.ForName(fd.Name) == nil {
					def.Fields = append(def.Fields, fd)
				}
			}
		}
	}
	edgeIfaces := map[string]*ast.Definition{}
	for _, ptr := range m.Pointers {
		def, ok := s.Types[ptr.T.Name]
		if !ok || !gqlType(ptr.T) {
			continue
		}
		for _, e := range ptr.Edges {
			if !e.P.GraphQL {
				continue
			}
			fields := edgeFields(camel(e.Name), e.P.Name, e.Optional, e.Comment)
			def.Fields = append(def.Fields, fields...)
			for _, name := range e.Implements {
				iface := edgeIfaces[name]
				if iface == nil {
					iface = &ast.Definition{
						Kind:       ast.Interface,
						Name:       name,
						Directives: ast.DirectiveList{goModel(g.Package + "." + name)},
					}
					edgeIfaces[name] = iface
					s.Types[name] = iface
				}
				for _, fd := range edgeFields(camel(e.Name), e.P.Name, true, "") {
					if iface.Fields.ForName(fd.Name) == nil {
						iface.Fields = append(iface.Fields, fd)
					}
				}
			}
		}
	}
	return nil
}

func goModel(model string) *ast.Directive {
	return &ast.Directive{
		Name: "goModel",
		Arguments: ast.ArgumentList{{
			Name:  "model",
			Value: &ast.Value{Kind: ast.StringValue, Raw: model},
		}},
	}
}

// An implementing type may narrow a nullable interface field to non-null, so an interface takes every edge as
// optional.
func edgeFields(name, pattern string, optional bool, comment string) ast.FieldList {
	named := func(t string) *ast.Type {
		if optional {
			return ast.NamedType(t, nil)
		}
		return ast.NonNullNamedType(t, nil)
	}
	return ast.FieldList{
		{Name: name, Type: named(pattern), Description: comment},
		{Name: name + "ID", Type: named("ID")},
	}
}

// Every exported method on <Pattern>Methods that takes no arguments, other than a context, is a GraphQL field: a
// method that needs arguments is Go-only.
func graphqlMethods(p *tpcPattern) (ast.FieldList, error) {
	var out ast.FieldList
	for _, mt := range p.Methods {
		if !token.IsExported(mt.Name) {
			continue
		}
		params := strings.TrimSpace(strings.Trim(mt.Params, "()"))
		if params != "" && !strings.HasSuffix(params, "context.Context") {
			continue
		}
		t, err := graphqlType(p, mt)
		if err != nil {
			return nil, err
		}
		out = append(out, &ast.FieldDefinition{Name: lowerFirst(mt.Name), Type: t})
	}
	return out, nil
}

func graphqlType(p *tpcPattern, mt method) (*ast.Type, error) {
	results := strings.Split(strings.Trim(strings.TrimSpace(mt.Results), "()"), ",")
	parts := strings.Fields(results[0])
	if len(parts) == 0 {
		return nil, fmt.Errorf(
			"%sMethods.%s returns nothing, so it can't be a GraphQL field",
			p.Name,
			mt.Name,
		)
	}
	goType := parts[len(parts)-1]
	nullable := strings.HasPrefix(goType, "*")
	goType = strings.TrimPrefix(goType, "*")
	scalars := map[string]string{
		"string":    "String",
		"bool":      "Boolean",
		"int":       "Int",
		"int64":     "Int",
		"float64":   "Float",
		"time.Time": "Time",
		p.IDType:    "ID",
	}
	name, ok := scalars[goType]
	if !ok {
		return nil, fmt.Errorf(
			"%sMethods.%s returns %s, which has no GraphQL scalar",
			p.Name,
			mt.Name,
			goType,
		)
	}
	if nullable {
		return ast.NamedType(name, nil), nil
	}
	return ast.NonNullNamedType(name, nil), nil
}

func lowerFirst(s string) string {
	r := []rune(s)
	n := 0
	for n < len(r) && unicode.IsUpper(r[n]) {
		n++
	}
	if n > 1 && n < len(r) {
		n--
	}
	for i := range n {
		r[i] = unicode.ToLower(r[i])
	}
	return string(r)
}
