package schema

import (
	"entgo.io/contrib/entgql"
	entschema "entgo.io/ent/schema"
	"github.com/vektah/gqlparser/v2/ast"
)

type customGetter struct {
	WithContext bool
}

type CustomGetterOption func(*customGetter)

func WithContext() CustomGetterOption {
	return func(cg *customGetter) {
		cg.WithContext = true
	}
}

type FieldAnnotation struct {
	CustomGetter *customGetter
}

func (f FieldAnnotation) Name() string {
	return "FieldAnnotation"
}

// CustomGetter returns the FieldAnnotation and the entgql directive for forcing a resolver.
func CustomGetter(opts ...CustomGetterOption) []entschema.Annotation {
	cg := &customGetter{}
	for _, opt := range opts {
		opt(cg)
	}
	return []entschema.Annotation{
		&FieldAnnotation{CustomGetter: cg},
		entgql.Directives(entgql.NewDirective("goField", &ast.Argument{
			Name: "forceResolver",
			Value: &ast.Value{
				Kind: ast.BooleanValue,
				Raw:  "true",
			},
		})),
	}
}
