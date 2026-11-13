package polygen

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"entgo.io/ent/entc/gen"
	"github.com/acdifran/go-tools/ent/poly"
)

type tphField struct {
	Snake    string // trigger_id
	Base     *gen.Field
	Name     string // the variant's own name: TriggerID for zendesk_trigger_id
	Required bool
}

func (f tphField) GoType() string {
	t := f.Base.Type.String()
	if f.Base.Nillable && !f.Required {
		return "*" + t
	}
	return t
}

type tphVariant struct {
	Name, Value, Prefix string
	Fields              []tphField
	Renames             map[string]string
}

type tphPattern struct {
	Name          string
	Base          *gen.Type
	Disc          *gen.Field
	Shared        []*gen.Field
	IDType        string
	Members       []*tphVariant
	VariantFields []string
	Imports       []string
	Entity        []method2
	Query         []method2
	Create        []method2
	UpdateOne     []method2
	Update        []method2
	Client        []method2
	// The table's other exported types (ConnectionPatternUpsert), re-exported under the pattern's name.
	Types []string
}

type tphEdge struct {
	T       *gen.Type
	Edge    *gen.Edge
	Name    string
	P       *tphPattern
	Many    bool
	Field   string
	GQL     *method2
	Setters map[string]bool
}

type method2 struct {
	Name    string
	Params  string
	Args    string
	Results string
}

type tphModel struct {
	Package  string
	Patterns []*tphPattern
	Edges    []*tphEdge
}

func tphPatterns(g *gen.Graph) ([]*tphPattern, error) {
	var out []*tphPattern
	for _, n := range g.Nodes {
		a, ok := decode[poly.BaseAnnotation](n.Annotations[poly.BaseAnnotationName])
		if !ok || a.Storage != poly.StorageTPH {
			continue
		}
		p := &tphPattern{
			Name:          a.Pattern,
			Base:          n,
			Disc:          nil,
			Shared:        nil,
			IDType:        n.ID.Type.String(),
			Members:       nil,
			VariantFields: nil,
			Imports:       nil,
			Entity:        nil,
			Query:         nil,
			Create:        nil,
			UpdateOne:     nil,
			Update:        nil,
			Client:        nil,
			Types:         nil,
		}
		byVariant := map[string]*tphVariant{}
		for _, m := range a.Members {
			v := &tphVariant{
				Name:    m.Type,
				Value:   m.Value,
				Prefix:  m.Prefix,
				Fields:  nil,
				Renames: map[string]string{},
			}
			byVariant[m.Type] = v
			p.Members = append(p.Members, v)
		}
		for _, f := range n.Fields {
			if vf, ok := decode[poly.VariantFieldAnnotation](
				f.Annotations[poly.VariantFieldAnnotationName],
			); ok {
				v := byVariant[vf.Variant]
				own := gen.Funcs["pascal"].(func(string) string)(vf.Field)
				v.Fields = append(
					v.Fields,
					tphField{Snake: vf.Field, Base: f, Name: own, Required: vf.Required},
				)
				v.Renames[f.StructField()] = own
				p.VariantFields = append(p.VariantFields, f.StructField())
				continue
			}
			if f.Name == a.Discriminator {
				p.Disc = f
				continue
			}
			p.Shared = append(p.Shared, f)
		}
		if p.Disc == nil || !p.Disc.IsEnum() {
			return nil, fmt.Errorf(
				"%s: the discriminator %q must be an enum field",
				a.Pattern,
				a.Discriminator,
			)
		}
		values := map[string]bool{}
		for _, v := range p.Members {
			if values[v.Value] {
				return nil, fmt.Errorf("%s: more than one variant has %s %q", a.Pattern, a.Discriminator, v.Value)
			}
			values[v.Value] = true
		}
		for _, ev := range p.Disc.EnumValues() {
			if !values[ev] {
				return nil, fmt.Errorf(
					"%s: %s %q has no variant; add poly.Of(name, %[3]q) to %[4]s's Variants",
					a.Pattern,
					a.Discriminator,
					ev,
					a.Pattern,
				)
			}
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (p *tphPattern) BaseName() string { return p.Base.Name }

// A conversion works for ent's own enum types and for a GoType enum, which has no constants in the base package.
func (p *tphPattern) EnumConst(value string) string {
	return fmt.Sprintf("%s(%q)", p.Disc.Type.String(), value)
}

func (p *tphPattern) mentionsVariant(s string) bool {
	for _, f := range p.VariantFields {
		if strings.Contains(s, f) {
			return true
		}
	}
	return false
}

func (p *tphPattern) own(v *tphVariant, s string) (string, bool) {
	for from, to := range v.Renames {
		if strings.Contains(s, from) {
			return strings.ReplaceAll(s, from, to), true
		}
	}
	return s, !p.mentionsVariant(s)
}

type pkgSource struct {
	fset    *token.FileSet
	files   map[string]*ast.File
	src     map[string][]byte
	imports map[string]string // name used in code -> import path
}

func parsePkg(dir string) (*pkgSource, error) {
	ps := &pkgSource{
		fset:    token.NewFileSet(),
		files:   map[string]*ast.File{},
		src:     map[string][]byte{},
		imports: map[string]string{},
	}
	paths, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return nil, err
	}
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") || strings.HasPrefix(filepath.Base(path), "poly_") {
			continue
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		f, err := parser.ParseFile(ps.fset, path, b, 0)
		if err != nil {
			return nil, err
		}
		ps.files[path], ps.src[path] = f, b
		for _, im := range f.Imports {
			p, _ := strconv.Unquote(im.Path.Value)
			name := filepath.Base(p)
			if im.Name != nil {
				name = im.Name.Name
			}
			ps.imports[name] = p
		}
	}
	return ps, nil
}

func (ps *pkgSource) node(n ast.Node) string {
	var b bytes.Buffer
	_ = printer.Fprint(&b, ps.fset, n)
	return b.String()
}

func (ps *pkgSource) methods(recv string) []method2 {
	var out []method2
	paths := make([]string, 0, len(ps.files))
	for p := range ps.files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, path := range paths {
		for _, d := range ps.files[path].Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || len(fn.Recv.List) != 1 || !fn.Name.IsExported() {
				continue
			}
			star, ok := fn.Recv.List[0].Type.(*ast.StarExpr)
			if !ok {
				continue
			}
			if id, ok := star.X.(*ast.Ident); !ok || id.Name != recv {
				continue
			}
			m := method2{Name: fn.Name.Name, Params: "", Args: "", Results: ""}
			var params, args []string
			for i, p := range fn.Type.Params.List {
				t := ps.node(p.Type)
				names := p.Names
				if len(names) == 0 {
					names = []*ast.Ident{ast.NewIdent(fmt.Sprintf("a%d", i))}
				}
				for _, n := range names {
					params = append(params, n.Name+" "+t)
					if strings.HasPrefix(t, "...") {
						args = append(args, n.Name+"...")
					} else {
						args = append(args, n.Name)
					}
				}
			}
			m.Params, m.Args = "("+strings.Join(params, ", ")+")", strings.Join(args, ", ")
			if fn.Type.Results != nil {
				var rs []string
				for _, r := range fn.Type.Results.List {
					t := ps.node(r.Type)
					for range max(1, len(r.Names)) {
						rs = append(rs, t)
					}
				}
				m.Results = strings.Join(rs, ", ")
				if len(rs) > 1 {
					m.Results = "(" + m.Results + ")"
				}
			}
			out = append(out, m)
		}
	}
	return out
}

func (ps *pkgSource) types() []string {
	var out []string
	for _, f := range ps.files {
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, s := range gd.Specs {
				if ts := s.(*ast.TypeSpec); ts.Name.IsExported() {
					out = append(out, ts.Name.Name)
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

var identRe = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

func (ps *pkgSource) usedImports(text string) []string {
	var out []string
	for _, id := range identRe.FindAllString(text, -1) {
		if p, ok := ps.imports[id]; ok {
			out = append(out, p)
		}
	}
	return out
}

func buildTPH(g *gen.Graph) (*tphModel, error) {
	patterns, err := tphPatterns(g)
	if err != nil || len(patterns) == 0 {
		return nil, err
	}
	ps, err := parsePkg(g.Target)
	if err != nil {
		return nil, err
	}
	m := &tphModel{Package: g.Package, Patterns: patterns, Edges: nil}
	byBase := map[string]*tphPattern{}
	for _, p := range patterns {
		b := p.Base.Name
		byBase[b] = p
		p.Entity = ps.methods(b)
		p.Query = ps.methods(b + "Query")
		p.Create = ps.methods(b + "Create")
		p.UpdateOne = ps.methods(b + "UpdateOne")
		p.Update = ps.methods(b + "Update")
		p.Client = ps.methods(b + "Client")
		own := map[string]bool{}
		for _, suffix := range []string{"Query", "Create", "UpdateOne", "Update", "Client", "Connection", "Edge"} {
			own[b+suffix] = true
		}
		for _, t := range ps.types() {
			if t != b && strings.HasPrefix(t, b) && !own[t] {
				p.Types = append(p.Types, t)
			}
		}
		var all strings.Builder
		for _, list := range [][]method2{p.Entity, p.Query, p.Create, p.UpdateOne, p.Update, p.Client} {
			for _, mt := range list {
				all.WriteString(mt.Params + " " + mt.Results + " ")
			}
		}
		p.Imports = ps.usedImports(all.String())
	}
	for _, n := range g.Nodes {
		for _, e := range n.Edges {
			ref, ok := decode[poly.RefAnnotation](e.Annotations[poly.RefAnnotationName])
			if !ok || ref.Storage != poly.StorageTPH {
				continue
			}
			p := byBase[e.Type.Name]
			te := &tphEdge{
				T:       n,
				Edge:    e,
				Name:    strings.TrimSuffix(e.Name, PatternSuffix),
				P:       p,
				Many:    ref.Many,
				Field:   ref.Field,
				GQL:     nil,
				Setters: map[string]bool{},
			}
			for _, recv := range []string{n.Name + "Create", n.Name + "UpdateOne", n.Name + "Update"} {
				for _, mt := range ps.methods(recv) {
					te.Setters[recv+"."+mt.Name] = true
				}
			}
			for _, mt := range ps.methods(n.Name) {
				if mt.Name == e.StructField() && strings.Contains(mt.Params, "ctx") {
					gm := mt
					te.GQL = &gm
				}
			}
			m.Edges = append(m.Edges, te)
		}
	}
	return m, nil
}
