package polygen

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"entgo.io/ent/entc/gen"
	"entgo.io/ent/schema/field"
	"github.com/acdifran/go-tools/ent/poly"
	"golang.org/x/tools/imports"
)

type tpcPattern struct {
	Name    string
	IDType  string
	Members []*tpcMember
	Shared  []*gen.Field
	Methods []method
}

type tpcMember struct {
	T         *gen.Type
	Prefix    string
	Overrides map[string]bool
}

type method struct {
	Name, Params, Args, Results string
}

type pointerEdge struct {
	PolyEdge
	P       *tpcPattern
	Members []pointerMember
}

type pointerMember struct {
	T     *gen.Type
	Field *gen.Field
	Edge  *gen.Edge
}

type pointer struct {
	T      *gen.Type
	Edges  []*pointerEdge
	Policy bool
}

type edgeInterface struct {
	Name string
	P    *tpcPattern
}

type model struct {
	Package    string
	Schema     string
	GraphQL    bool
	Patterns   []*tpcPattern
	Pointers   []*pointer
	Interfaces []edgeInterface
}

func buildModel(g *gen.Graph) (*model, error) {
	m := &model{
		Package:    g.Package,
		Schema:     g.Schema,
		GraphQL:    false,
		Patterns:   nil,
		Pointers:   nil,
		Interfaces: nil,
	}
	if _, err := os.Stat(filepath.Join(g.Target, "gql_collection.go")); err == nil {
		m.GraphQL = true
	}
	src, err := handWritten(g.Target)
	if err != nil {
		return nil, err
	}
	byName := map[string]*tpcPattern{}
	for _, n := range g.Nodes {
		ma, ok := decode[poly.MemberAnnotation](n.Annotations[poly.MemberAnnotationName])
		if !ok {
			continue
		}
		p := byName[ma.Pattern]
		if p == nil {
			p = &tpcPattern{
				Name:    ma.Pattern,
				IDType:  n.ID.Type.String(),
				Members: nil,
				Shared:  nil,
				Methods: nil,
			}
			byName[ma.Pattern] = p
			m.Patterns = append(m.Patterns, p)
			for _, f := range n.Fields {
				for _, name := range ma.Fields {
					if f.Name == name {
						p.Shared = append(p.Shared, f)
					}
				}
			}
			p.Methods = src.methods[ma.Pattern+"Methods"]
		}
		if n.ID.Type.String() != p.IDType {
			return nil, fmt.Errorf(
				"pattern %s: members' ids differ (%s and %s)",
				p.Name,
				p.IDType,
				n.ID.Type,
			)
		}
		prefix, _ := decode[struct{ Prefix string }](n.Annotations["PULID"])
		p.Members = append(
			p.Members,
			&tpcMember{T: n, Prefix: prefix.Prefix, Overrides: src.methodsOf[n.Name]},
		)
	}
	sort.Slice(m.Patterns, func(i, j int) bool { return m.Patterns[i].Name < m.Patterns[j].Name })
	interfaces := map[string]*tpcPattern{}
	for _, n := range g.Nodes {
		pes, ok := decode[[]PolyEdge](n.Annotations[poly.EdgesAnnotationName])
		if !ok {
			continue
		}
		ptr := &pointer{T: n, Edges: nil, Policy: n.NumPolicy() > 0}
		for _, pe := range pes {
			e := &pointerEdge{PolyEdge: pe, P: byName[pe.Pattern], Members: nil}
			for _, pm := range pe.Members {
				var mem pointerMember
				for _, f := range n.Fields {
					if f.Name == pm.Field {
						mem.Field = f
					}
				}
				for _, ed := range n.Edges {
					if ed.Name == pm.Edge {
						mem.Edge, mem.T = ed, ed.Type
					}
				}
				e.Members = append(e.Members, mem)
			}
			ptr.Edges = append(ptr.Edges, e)
			interfaces[pe.Name] = e.P
		}
		m.Pointers = append(m.Pointers, ptr)
	}
	for name, p := range interfaces {
		m.Interfaces = append(m.Interfaces, edgeInterface{Name: name, P: p})
	}
	sort.Slice(
		m.Interfaces,
		func(i, j int) bool { return m.Interfaces[i].Name < m.Interfaces[j].Name },
	)
	return m, nil
}

type source struct {
	methods   map[string][]method        // methods declared on a <Pattern>Methods type
	methodsOf map[string]map[string]bool // methods members declare themselves, which override the pattern's
}

// Only hand-written files count: the forwarders this package generated last time would otherwise look like
// overrides and suppress themselves.
func handWritten(dir string) (*source, error) {
	out := &source{methods: map[string][]method{}, methodsOf: map[string]map[string]bool{}}
	fset := token.NewFileSet()
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return nil, err
	}
	for _, path := range files {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		f, err := parser.ParseFile(fset, path, b, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		if ast.IsGenerated(f) {
			continue
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || len(fn.Recv.List) != 1 {
				continue
			}
			recv := fn.Recv.List[0].Type
			if star, ok := recv.(*ast.StarExpr); ok {
				recv = star.X
			}
			id, ok := recv.(*ast.Ident)
			if !ok {
				continue
			}
			if strings.HasSuffix(id.Name, "Methods") {
				mt := method{
					Name: fn.Name.Name,
					Params: string(
						b[fset.Position(fn.Type.Params.Opening).Offset : fset.Position(fn.Type.Params.Closing).Offset+1],
					),
					Args:    "",
					Results: "",
				}
				var args []string
				for _, p := range fn.Type.Params.List {
					for _, n := range p.Names {
						args = append(args, n.Name)
					}
				}
				mt.Args = strings.Join(args, ", ")
				if fn.Type.Results != nil {
					mt.Results = string(
						b[fset.Position(fn.Type.Results.Pos()).Offset:fset.Position(fn.Type.Results.End()).Offset],
					)
				}
				out.methods[id.Name] = append(out.methods[id.Name], mt)
				continue
			}
			if out.methodsOf[id.Name] == nil {
				out.methodsOf[id.Name] = map[string]bool{}
			}
			out.methodsOf[id.Name][fn.Name.Name] = true
		}
	}
	return out, nil
}

var funcs = template.FuncMap{
	"pascal": gen.Funcs["pascal"],
	"base":   path.Base,
	"lower":  strings.ToLower,
	"goType": func(f *gen.Field) string {
		if f.Nillable {
			return "*" + f.Type.String()
		}
		return f.Type.String()
	},
	"isString": func(f *gen.Field) bool { return f.Type.Type == field.TypeString },
}

// writeOutput writes the poly API into the generated package. Files it owns start with poly_, so a removed
// pattern or edge leaves nothing stale behind.
func writeOutput(g *gen.Graph) error {
	m, err := buildModel(g)
	if err != nil {
		return err
	}
	_ = os.Remove(filepath.Join(g.Target, "runtime", runtimeFile))
	dirs := []string{g.Target}
	for _, n := range g.Nodes {
		dirs = append(dirs, filepath.Join(g.Target, n.PackageDir()))
	}
	for _, d := range dirs {
		stale, _ := filepath.Glob(filepath.Join(d, "poly_*.go"))
		for _, f := range stale {
			if err := os.Remove(f); err != nil {
				return err
			}
		}
	}
	write := func(path, tmpl string, data any) error {
		t, err := template.New(path).Funcs(funcs).Parse(tmpl)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		var buf bytes.Buffer
		if err := t.Execute(&buf, data); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		full := filepath.Join(g.Target, path)
		src, err := imports.Process(full, buf.Bytes(), nil)
		if err != nil {
			return fmt.Errorf("formatting %s: %w\n%s", path, err, buf.String())
		}
		return os.WriteFile(full, src, 0o644)
	}
	if err := write("poly_patterns.go", patternsTmpl, m); err != nil {
		return err
	}
	for _, ptr := range m.Pointers {
		data := map[string]any{"M": m, "T": ptr}
		if err := write(
			filepath.Join(ptr.T.PackageDir(), "poly_where.go"),
			whereTmpl,
			data,
		); err != nil {
			return err
		}
	}
	if _, err := os.Stat(filepath.Join(g.Target, "runtime")); err == nil {
		return write(filepath.Join("runtime", runtimeFile), runtimeTmpl, m)
	}
	return nil
}

const header = "// Code generated by polygen. DO NOT EDIT.\n\n"

// Sorts after ent's runtime.go, so its init runs after ent's has built each schema's policy.
const runtimeFile = "runtime_poly.go"

var patternsTmpl = header + `package {{ base .Package }}

import (
	"context"
	"strings"

	"entgo.io/ent"
	"entgo.io/ent/entql"
	"github.com/99designs/gqlgen/graphql"
)

var polyEdgeHooks = map[string][]ent.Hook{}

// PolyRegisterHooks is called from the generated runtime package, which can import the schema.
func PolyRegisterHooks(node string, hooks []ent.Hook) { polyEdgeHooks[node] = hooks }

func polyInitConfig(cfg *config) {
	{{- range .Pointers }}
	cfg.hooks.{{ .T.Name }} = append(cfg.hooks.{{ .T.Name }}, polyEdgeHooks["{{ .T.Name }}"]...)
	{{- end }}
}

{{- range $p := .Patterns }}

type {{ $p.Name }} interface {
	is{{ $p.Name }}()
	GetID() {{ $p.IDType }}
	{{- range $p.Shared }}
	Get{{ pascal .Name }}() {{ goType . }}
	{{- end }}
	{{- range $p.Methods }}
	{{ .Name }}{{ .Params }} {{ .Results }}
	{{- end }}
}

type {{ lower $p.Name }}Fields interface {
	GetID() {{ $p.IDType }}
	{{- range $p.Shared }}
	Get{{ pascal .Name }}() {{ goType . }}
	{{- end }}
}

// {{ $p.Name }}Methods holds methods written once for every {{ $p.Name }}; it sees only the shared fields.
type {{ $p.Name }}Methods struct{ {{ lower $p.Name }}Fields }

{{- range $mem := $p.Members }}{{ $n := $mem.T.Name }}

var _ {{ $p.Name }} = (*{{ $n }})(nil)

func (*{{ $n }}) is{{ $p.Name }}() {}
{{- range $p.Methods }}{{ if not (index $mem.Overrides .Name) }}

func (n *{{ $n }}) {{ .Name }}{{ .Params }} {{ .Results }} {
	return {{ $p.Name }}Methods{n}.{{ .Name }}({{ .Args }})
}
{{- end }}{{ end }}
{{- end }}
{{- end }}

{{- range .Interfaces }}{{ $E := pascal .Name }}{{ $p := .P }}

type With{{ $E }} interface {
	{{ $E }}(ctx context.Context) ({{ $p.Name }}, error)
	{{ $E }}OrErr() ({{ $p.Name }}, error)
	{{ $E }}ID() *{{ $p.IDType }}
}
{{- end }}

{{- range $ptr := .Pointers }}{{ $n := $ptr.T.Name }}
{{- range $e := $ptr.Edges }}{{ $E := pascal $e.Name }}{{ $p := $e.P }}

func (n *{{ $n }}) {{ $E }}(ctx context.Context) ({{ $p.Name }}, error) {
	switch {
	{{- range $e.Members }}
	case n.{{ .Field.StructField }} != nil:
		if v, err := n.Edges.{{ .Edge.StructField }}OrErr(); err == nil {
			return v, nil
		}
		v, err := n.Query{{ .Edge.StructField }}().Only(ctx)
		if err != nil || v == nil {
			return nil, MaskNotFound(err)
		}
		return v, nil
	{{- end }}
	}
	return nil, nil
}

func (n *{{ $n }}) {{ $E }}OrErr() ({{ $p.Name }}, error) {
	switch {
	{{- range $e.Members }}
	case n.{{ .Field.StructField }} != nil:
		v, err := n.Edges.{{ .Edge.StructField }}OrErr()
		if err != nil {
			return nil, err
		}
		return v, nil
	{{- end }}
	}
	return nil, &NotFoundError{label: "{{ $e.Name }}"}
}

func (n *{{ $n }}) {{ $E }}ID() *{{ $p.IDType }} {
	{{- range $e.Members }}
	if n.{{ .Field.StructField }} != nil {
		return n.{{ .Field.StructField }}
	}
	{{- end }}
	return nil
}
{{- range $e.Members }}

func (n *{{ $n }}) From{{ .T.Name }}() bool { return n.{{ .Field.StructField }} != nil }
{{- end }}

func (m *{{ $n }}Mutation) {{ $E }}ID() (r {{ $p.IDType }}, exists bool) {
	{{- range $e.Members }}
	if id, ok := m.{{ .Field.StructField }}(); ok {
		return id, true
	}
	{{- end }}
	return
}

// Set{{ $E }}ID picks the member from the id's prefix.
func (m *{{ $n }}Mutation) Set{{ $E }}ID(id {{ $p.IDType }}) {
	switch {
	{{- range $mem := $p.Members }}{{ range $e.Members }}{{ if eq .T.Name $mem.T.Name }}
	case strings.HasPrefix(string(id), "{{ $mem.Prefix }}"):
		m.{{ .Field.MutationSet }}(id)
	{{- end }}{{ end }}{{ end }}
	}
}

func (c *{{ $n }}Create) Set{{ $E }}ID(id {{ $p.IDType }}) *{{ $n }}Create {
	c.mutation.Set{{ $E }}ID(id)
	return c
}

func (q *{{ $n }}Query) With{{ $E }}() *{{ $n }}Query {
	return q{{ range $e.Members }}.With{{ .Edge.StructField }}(){{ end }}
}

func (f *{{ $n }}Filter) Where{{ $E }}IDEQ(id {{ $p.IDType }}) {
	f.Where(entql.Or(
	{{- range $e.Members }}
		entql.{{ if isString .Field }}StringEQ(string(id)){{ else }}IntEQ(int(id)){{ end }}.Field("{{ .Field.Name }}"),
	{{- end }}
	))
}
{{- if $.GraphQL }}

func (q *{{ $n }}Query) {{ $E }}CollectFields(
	ctx context.Context,
	field graphql.CollectedField,
	path *[]string,
	fieldSeen map[string]struct{},
	selectedFields *[]string,
	oneNode bool,
	opCtx *graphql.OperationContext,
	satisfies ...string,
) error {
	{{- range $e.Members }}
	{
		mq := (&{{ .T.Name }}Client{config: q.config}).Query()
		p := append(append([]string(nil), *path...), field.Alias)
		if err := mq.collectField(ctx, oneNode, opCtx, field, p, mayAddCondition(satisfies, {{ lower .T.Name }}Implementors)...); err != nil {
			return err
		}
		q.With{{ .Edge.StructField }}(func(x *{{ .T.Name }}Query) { *x = *mq })
		if _, ok := fieldSeen["{{ .Field.Name }}"]; !ok {
			*selectedFields = append(*selectedFields, "{{ .Field.Name }}")
			fieldSeen["{{ .Field.Name }}"] = struct{}{}
		}
	}
	{{- end }}
	return nil
}
{{- end }}
{{- end }}
{{- end }}
`

var whereTmpl = header + `package {{ .T.T.Package }}

import "{{ .M.Package }}/predicate"
{{ $n := .T.T.Name }}
{{- range $e := .T.Edges }}{{ $E := pascal $e.Name }}

func {{ $E }}ID(id {{ $e.P.IDType }}) predicate.{{ $n }} {
	return Or({{ range $i, $m := $e.Members }}{{ if $i }}, {{ end }}{{ .Field.StructField }}(id){{ end }})
}

func {{ $E }}IDNEQ(id {{ $e.P.IDType }}) predicate.{{ $n }} {
	return And({{ range $i, $m := $e.Members }}{{ if $i }}, {{ end }}{{ .Field.StructField }}NEQ(id){{ end }})
}
{{- end }}
`

var runtimeTmpl = header + `package runtime

import (
	"entgo.io/ent/privacy"
	"github.com/acdifran/go-tools/ent/poly"
	"{{ .Package }}"
	"{{ .Schema }}"
	{{- range .Pointers }}
	"{{ $.Package }}/{{ .T.PackageDir }}"
	{{- end }}
)

func init() {
	{{- range .Pointers }}{{ $n := .T.Name }}
	{{ base $.Package }}.PolyRegisterHooks("{{ $n }}", poly.EdgeHooks(schema.{{ $n }}{}.Edges()))
	{{- if .Policy }}
	{{ .T.Package }}.Policy = privacy.NewPolicies(append(append(
		poly.MixinPolicies(schema.{{ $n }}{}.Mixin()),
		poly.EdgePolicies(schema.{{ $n }}{}.Edges())...),
		schema.{{ $n }}{})...)
	{{- end }}
	{{- end }}
}
`
