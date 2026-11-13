package polygen

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"github.com/acdifran/go-tools/ent/poly"
	"golang.org/x/tools/imports"
)

type stubPattern struct {
	Name    string
	Package string
}

// TPH patterns are found from source, not by compiling the schema package: the table's schema has to exist before
// anything that refers to it compiles.
func scanTPH(dir string) ([]*stubPattern, error) {
	fset := token.NewFileSet()
	paths, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return nil, err
	}
	var out []*stubPattern
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return nil, err
		}
		name := f.Name.Name
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range gd.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				st, ok := ts.Type.(*ast.StructType)
				if !ok {
					continue
				}
				for _, fld := range st.Fields.List {
					if len(fld.Names) == 0 && selName(fld.Type) == "TPH" {
						out = append(out, &stubPattern{Name: ts.Name.Name, Package: name})
					}
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func selName(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.SelectorExpr:
		return e.Sel.Name
	case *ast.Ident:
		return e.Name
	}
	return ""
}

func stubFile(p string) string { return "zz_poly_" + poly.Snake(p) + "_pattern.go" }

var stubTmpl = template.Must(template.New("stub").Parse(header + `package {{ .Package }}

import (
	"entgo.io/ent"
	"entgo.io/ent/schema"
	"github.com/acdifran/go-tools/ent/poly"
)

// {{ .Name }}Pattern is the table of the {{ .Name }} pattern. Use the generated {{ .Name }} interface and its variants.
type {{ .Name }}Pattern struct{ ent.Schema }

func ({{ .Name }}Pattern) Mixin() []ent.Mixin { return {{ .Name }}{}.Mixin() }

func ({{ .Name }}Pattern) Fields() []ent.Field {
	return poly.TPHFields({{ .Name }}{})
}

func ({{ .Name }}Pattern) Edges() []ent.Edge { return {{ .Name }}{}.Edges() }

func ({{ .Name }}Pattern) Indexes() []ent.Index { return {{ .Name }}{}.Indexes() }

func ({{ .Name }}Pattern) Hooks() []ent.Hook { return poly.TPHHooks({{ .Name }}{}) }

func ({{ .Name }}Pattern) Interceptors() []ent.Interceptor { return {{ .Name }}{}.Interceptors() }

func ({{ .Name }}Pattern) Policy() ent.Policy { return {{ .Name }}{}.Policy() }

func ({{ .Name }}Pattern) Annotations() []schema.Annotation {
	return poly.TPHAnnotations({{ .Name }}{})
}

func ({{ .Name }}Pattern) Patterns() []poly.Pattern { return {{ .Name }}{}.Patterns() }
`))

func writeStubs(dir string) error {
	patterns, err := scanTPH(dir)
	if err != nil {
		return err
	}
	keep := map[string]bool{}
	for _, p := range patterns {
		var buf bytes.Buffer
		if err := stubTmpl.Execute(&buf, p); err != nil {
			return err
		}
		path := filepath.Join(dir, stubFile(p.Name))
		src, err := imports.Process(path, buf.Bytes(), nil)
		if err != nil {
			return fmt.Errorf("formatting %s: %w", path, err)
		}
		keep[path] = true
		if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, src) {
			continue
		}
		if err := os.WriteFile(path, src, 0o644); err != nil {
			return err
		}
	}
	stale, _ := filepath.Glob(filepath.Join(dir, "zz_poly_*_pattern.go"))
	for _, f := range stale {
		if !keep[f] {
			if err := os.Remove(f); err != nil {
				return err
			}
		}
	}
	return nil
}
