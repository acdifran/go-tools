package polygen

import (
	"fmt"
	"strings"

	"entgo.io/ent/entc/gen"
)

func pkgName(path string) string { return path[strings.LastIndex(path, "/")+1:] }

func goTypeOf(f *gen.Field) string {
	if f.Nillable {
		return "*" + f.Type.String()
	}
	return f.Type.String()
}

func jsonName(name string, sensitive bool) string {
	if sensitive {
		return "-"
	}
	return name + ",omitempty"
}

// entitySkip leaves out entity methods the variants declare themselves, or that would hand out the table's type.
func entitySkip(p *tphPattern, mt method2) bool {
	switch mt.Name {
	case "Update", "Unwrap", "ToEdge", "String", "IsNode":
		return true
	}
	return p.mentionsVariant(mt.Name) || strings.Contains(mt.Results, "*"+p.BaseName())
}

// builder is one of the table's generated builders re-exposed once, generically, for the pattern and every
// variant: T is what reads return (the interface, or a variant), and S is the builder type chaining returns.
type builder struct {
	suffix  string // Query, Create, UpdateOne, Update
	methods []method2
	self    bool // chaining returns S; Query chains to itself, since it has no variant-only methods
}

func (p *tphPattern) builders() []builder {
	return []builder{
		{suffix: "Query", methods: p.Query, self: false},
		{suffix: "Create", methods: p.Create, self: true},
		{suffix: "UpdateOne", methods: p.UpdateOne, self: true},
		{suffix: "Update", methods: p.Update, self: true},
	}
}

func (bd builder) generic(p *tphPattern) string {
	if bd.self {
		return p.Name + bd.suffix + "Of[T, S]"
	}
	return p.Name + bd.suffix + "Of[T]"
}

func renderGeneric(b *strings.Builder, p *tphPattern, bd builder) {
	P, B := p.Name, p.BaseName()
	name, base, be := P+bd.suffix+"Of", "*"+B+bd.suffix, "*"+B
	if bd.self {
		fmt.Fprintf(
			b,
			"type %s[T any, S any] struct {\n\tb *%s%s\n\tconv func(*%s) T\n\tself S\n\tfix func()\n}\n\n",
			name,
			B,
			bd.suffix,
			B,
		)
	} else {
		fmt.Fprintf(
			b,
			"type %s[T any] struct {\n\tb *%s%s\n\tconv func(*%s) T\n}\n\n",
			name,
			B,
			bd.suffix,
			B,
		)
	}
	recv := "wrap *" + bd.generic(p)
	for _, m := range bd.methods {
		if p.mentionsVariant(m.Name) || p.mentionsVariant(m.Params) {
			continue
		}
		call := fmt.Sprintf("wrap.b.%s(%s)", m.Name, m.Args)
		pre := ""
		if bd.self &&
			(m.Name == "Save" || m.Name == "SaveX" || m.Name == "Exec" || m.Name == "ExecX") {
			pre = "\tif wrap.fix != nil {\n\t\twrap.fix()\n\t}\n"
		}
		switch m.Results {
		case base:
			switch {
			case m.Name == "Clone":
				fmt.Fprintf(
					b,
					"func (%s) Clone() *%s {\n\treturn &%s{b: %s, conv: wrap.conv}\n}\n\n",
					recv,
					bd.generic(p),
					bd.generic(p),
					call,
				)
			case bd.self:
				fmt.Fprintf(
					b,
					"func (%s) %s%s S {\n\t%s\n\treturn wrap.self\n}\n\n",
					recv,
					m.Name,
					m.Params,
					call,
				)
			default:
				fmt.Fprintf(b, "func (%s) %s%s *%s {\n\twrap.b = %s\n\treturn wrap\n}\n\n",
					recv, m.Name, m.Params, bd.generic(p), call)
			}
		case "(" + be + ", error)":
			fmt.Fprintf(
				b,
				"func (%s) %s%s (T, error) {\n%s\tv, err := %s\n\tif err != nil {\n\t\tvar zero T\n\t\treturn zero, err\n\t}\n\treturn wrap.conv(v), nil\n}\n\n",
				recv,
				m.Name,
				m.Params,
				pre,
				call,
			)
		case "([]" + be + ", error)":
			fmt.Fprintf(
				b,
				"func (%s) %s%s ([]T, error) {\n%s\tv, err := %s\n\tif err != nil {\n\t\treturn nil, err\n\t}\n\treturn polyMap(v, wrap.conv), nil\n}\n\n",
				recv,
				m.Name,
				m.Params,
				pre,
				call,
			)
		case be:
			fmt.Fprintf(
				b,
				"func (%s) %s%s T {\n%s\treturn wrap.conv(%s)\n}\n\n",
				recv,
				m.Name,
				m.Params,
				pre,
				call,
			)
		case "[]" + be:
			fmt.Fprintf(
				b,
				"func (%s) %s%s []T {\n%s\treturn polyMap(%s, wrap.conv)\n}\n\n",
				recv,
				m.Name,
				m.Params,
				pre,
				call,
			)
		case "(*" + B + "Connection, error)":
			fmt.Fprintf(
				b,
				"func (%s) %s%s (*%sConnection, error) {\n\tc, err := %s\n\tif err != nil {\n\t\treturn nil, err\n\t}\n\treturn poly%sConnectionOf(c), nil\n}\n\n",
				recv,
				m.Name,
				m.Params,
				P,
				call,
				P,
			)
		case "":
			fmt.Fprintf(b, "func (%s) %s%s {\n%s\t%s\n}\n\n", recv, m.Name, m.Params, pre, call)
		default:
			fmt.Fprintf(
				b,
				"func (%s) %s%s %s {\n%s\treturn %s\n}\n\n",
				recv,
				m.Name,
				m.Params,
				m.Results,
				pre,
				call,
			)
		}
	}
}

func renderTyped(b *strings.Builder, p *tphPattern, v *tphVariant) {
	P, B := p.Name, p.BaseName()
	name, entity, conv, disc := P, P, lowerFirst(P)+"Of", ""
	if v != nil {
		name, entity, conv = v.Name, "*"+v.Name, "new"+v.Name
		disc = p.EnumConst(v.Value)
	}
	for _, bd := range p.builders() {
		t := name + bd.suffix
		if !bd.self {
			fmt.Fprintf(b, "type %s = %s%sOf[%s]\n\n", t, P, bd.suffix, entity)
			continue
		}
		fmt.Fprintf(b, "type %s struct{ *%s%sOf[%s, *%s] }\n\n", t, P, bd.suffix, entity, t)
		fmt.Fprintf(
			b,
			"func new%s(b *%s%s) *%s {\n\tw := &%s{}\n\tw.%s%sOf = &%s%sOf[%s, *%s]{b: b, conv: %s, self: w, fix: nil}\n",
			t,
			B,
			bd.suffix,
			t,
			t,
			P,
			bd.suffix,
			P,
			bd.suffix,
			entity,
			t,
			conv,
		)
		if v != nil && hasMethod(bd.methods, "Set"+p.Disc.StructField()) {
			// The value is set again just before a save, so no setter can turn the row into another variant.
			fmt.Fprintf(b, "\tw.fix = func() { b.Set%s(%s) }\n", p.Disc.StructField(), disc)
		}
		b.WriteString("\treturn w\n}\n\n")
		if v == nil {
			continue
		}
		for _, m := range bd.methods {
			own, ok := p.own(v, m.Name)
			if !ok || own == m.Name || !strings.HasPrefix(m.Results, "*"+B) {
				continue
			}
			fmt.Fprintf(
				b,
				"func (w *%s) %s%s *%s {\n\tw.b.%s(%s)\n\treturn w\n}\n\n",
				t,
				own,
				m.Params,
				t,
				m.Name,
				m.Args,
			)
		}
	}
	scope := ""
	create := "c.b.Create()"
	if v != nil {
		scope = fmt.Sprintf(".Where(%s.%sEQ(%s))", p.Base.Package(), p.Disc.StructField(), disc)
		create = fmt.Sprintf("c.b.Create().Set%s(%s)", p.Disc.StructField(), disc)
	}
	fmt.Fprintf(
		b,
		`func (c *%[1]sClient) Query() *%[1]sQuery { return &%[1]sQuery{b: c.b.Query()%[2]s, conv: %[3]s} }

func (c *%[1]sClient) Get(ctx context.Context, id %[4]s) (%[5]s, error) {
	return c.Query().Where(%[6]s.ID(id)).Only(ctx)
}

func (c *%[1]sClient) GetX(ctx context.Context, id %[4]s) %[5]s {
	n, err := c.Get(ctx, id)
	if err != nil {
		panic(err)
	}
	return n
}

func (c *%[1]sClient) Create() *%[1]sCreate { return new%[1]sCreate(%[7]s) }

func (c *%[1]sClient) UpdateOneID(id %[4]s) *%[1]sUpdateOne {
	return new%[1]sUpdateOne(c.b.UpdateOneID(id)%[2]s)
}

func (c *%[1]sClient) UpdateOne(n %[5]s) *%[1]sUpdateOne { return c.UpdateOneID(n.GetID()) }

func (c *%[1]sClient) Update() *%[1]sUpdate { return new%[1]sUpdate(c.b.Update()%[2]s) }

func (c *%[1]sClient) DeleteOneID(id %[4]s) *%[8]sDeleteOne { return c.b.DeleteOneID(id) }

func (c *%[1]sClient) DeleteOne(n %[5]s) *%[8]sDeleteOne { return c.b.DeleteOneID(n.GetID()) }

`,
		name,
		scope,
		conv,
		p.IDType,
		entity,
		p.Base.Package(),
		create,
		B,
	)
}

func renderTPH(m *tphModel, p *tphPattern) string {
	var b strings.Builder
	P, B := p.Name, p.BaseName()
	of := lowerFirst(P) + "Of"
	fmt.Fprintf(&b, "%spackage %s\n\nimport (\n", header, pkgName(m.Package))
	fmt.Fprintf(&b, "\t%q\n\t%q\n", m.Package+"/"+p.Base.Package(), m.Package+"/predicate")
	for _, im := range p.Imports {
		fmt.Fprintf(&b, "\t%q\n", im)
	}
	b.WriteString(")\n\n")

	fmt.Fprintf(
		&b,
		"type %s interface {\n\tNoder\n\tis%s()\n\tpolyBase() *%s\n\tUpdate() *%sUpdateOne\n\tGetEdges() %sEdges\n",
		P,
		P,
		B,
		P,
		B,
	)
	for _, mt := range p.Entity {
		if !entitySkip(p, mt) {
			fmt.Fprintf(&b, "\t%s%s %s\n", mt.Name, mt.Params, mt.Results)
		}
	}
	b.WriteString("}\n\n")

	fmt.Fprintf(
		&b,
		"func %s(b *%s) %s {\n\tif b == nil {\n\t\treturn nil\n\t}\n\tswitch b.%s {\n",
		of,
		B,
		P,
		p.Disc.StructField(),
	)
	for _, v := range p.Members {
		fmt.Fprintf(&b, "\tcase %s:\n\t\treturn new%s(b)\n", p.EnumConst(v.Value), v.Name)
	}
	b.WriteString("\t}\n\treturn nil\n}\n\n")
	fmt.Fprintf(
		&b,
		"// %sOf turns a %s row into its variant, for code that builds rows itself, such as test fakes.\n",
		P,
		B,
	)
	fmt.Fprintf(&b, "func %sOf(b *%s) %s { return %s(b) }\n\n", P, B, P, of)
	impl := strings.ToLower(B) + "Implementors"
	fmt.Fprintf(
		&b,
		"// entgql collects the fields of fragments on these types when it eager-loads the table's rows.\nfunc init() {\n\t%s = append(%s, %q",
		impl,
		impl,
		P,
	)
	for _, v := range p.Members {
		fmt.Fprintf(&b, ", %q", v.Name)
	}
	b.WriteString(")\n}\n\n")

	renderFields(&b, p)
	for _, v := range p.Members {
		renderVariant(&b, p, v)
	}

	fmt.Fprintf(&b, `type %[1]sConnection struct {
	Edges      []*%[1]sEdge `+"`json:\"edges\"`"+`
	PageInfo   PageInfo `+"`json:\"pageInfo\"`"+`
	TotalCount int `+"`json:\"totalCount\"`"+`
}

type %[1]sEdge struct {
	Node   %[1]s `+"`json:\"node\"`"+`
	Cursor Cursor `+"`json:\"cursor\"`"+`
}

func poly%[1]sConnectionOf(c *%[2]sConnection) *%[1]sConnection {
	if c == nil {
		return nil
	}
	out := &%[1]sConnection{Edges: nil, PageInfo: c.PageInfo, TotalCount: c.TotalCount}
	for _, e := range c.Edges {
		out.Edges = append(out.Edges, &%[1]sEdge{Node: %[3]s(e.Node), Cursor: e.Cursor})
	}
	return out
}

`, P, B, of)

	for _, t := range p.Types {
		fmt.Fprintf(&b, "type %s = %s\n", P+strings.TrimPrefix(t, B), t)
	}
	b.WriteString("\n")
	for _, bd := range p.builders() {
		renderGeneric(&b, p, bd)
	}
	renderTyped(&b, p, nil)
	for _, mt := range p.Client {
		if strings.HasPrefix(mt.Name, "Query") && mt.Name != "Query" &&
			strings.Contains(mt.Params, "*"+B+")") {
			params := strings.ReplaceAll(mt.Params, "*"+B, P)
			args := mt.Args
			for _, a := range strings.Split(mt.Args, ", ") {
				if strings.Contains(mt.Params, a+" *"+B) {
					args = strings.Replace(args, a, a+".polyBase()", 1)
				}
			}
			fmt.Fprintf(
				&b,
				"func (c *%sClient) %s%s %s {\n\treturn c.b.%s(%s)\n}\n\n",
				P,
				mt.Name,
				params,
				mt.Results,
				mt.Name,
				args,
			)
		}
	}
	for _, v := range p.Members {
		renderTyped(&b, p, v)
	}
	return b.String()
}

func renderFields(b *strings.Builder, p *tphPattern) {
	P, B := p.Name, p.BaseName()
	F := P + "Fields"
	fmt.Fprintf(b, "type %s struct {\n\tID %s `json:\"id,omitempty\"`\n", F, p.IDType)
	fmt.Fprintf(
		b,
		"\t%s %s `json:\"%s,omitempty\"`\n",
		p.Disc.StructField(),
		goTypeOf(p.Disc),
		p.Disc.Name,
	)
	for _, f := range p.Shared {
		fmt.Fprintf(
			b,
			"\t%s %s `json:\"%s\"`\n",
			f.StructField(),
			goTypeOf(f),
			jsonName(f.Name, f.Sensitive()),
		)
	}
	fmt.Fprintf(b, "\tEdges %sEdges `json:\"edges\"`\n\tbase *%s\n}\n\n", B, B)
	fmt.Fprintf(b, "func %sFieldsOf(b *%s) %s {\n\treturn %s{\n\t\tID: b.ID,\n\t\t%s: b.%s,\n",
		lowerFirst(P), B, F, F, p.Disc.StructField(), p.Disc.StructField())
	for _, f := range p.Shared {
		fmt.Fprintf(b, "\t\t%s: b.%s,\n", f.StructField(), f.StructField())
	}
	b.WriteString("\t\tEdges: b.Edges,\n\t\tbase: b,\n\t}\n}\n\n")
	fmt.Fprintf(
		b,
		"func (*%s) IsNode() {}\n\nfunc (n *%s) polyBase() *%s { return n.base }\n\n",
		F,
		F,
		B,
	)
	fmt.Fprintf(b, "func (n *%s) GetEdges() %sEdges { return n.Edges }\n\n", F, B)
	fmt.Fprintf(
		b,
		"func (n *%s) Update() *%sUpdateOne { return (&%sClient{b: New%sClient(n.base.config)}).UpdateOneID(n.ID) }\n\n",
		F,
		P,
		P,
		B,
	)
	// Getters read the copy, so a variant built as a literal (in a test) works without a row.
	own := map[string]string{"GetID": "ID", "Get" + p.Disc.StructField(): p.Disc.StructField()}
	for _, f := range p.Shared {
		own["Get"+f.StructField()] = f.StructField()
	}
	for _, mt := range p.Entity {
		if entitySkip(p, mt) {
			continue
		}
		if f, ok := own[mt.Name]; ok && mt.Params == "()" {
			fmt.Fprintf(b, "func (n *%s) %s() %s { return n.%s }\n\n", F, mt.Name, mt.Results, f)
			continue
		}
		fmt.Fprintf(
			b,
			"func (n *%s) %s%s %s {\n\treturn n.base.%s(%s)\n}\n\n",
			F,
			mt.Name,
			mt.Params,
			mt.Results,
			mt.Name,
			mt.Args,
		)
	}
}

func renderVariant(b *strings.Builder, p *tphPattern, v *tphVariant) {
	P, B, V := p.Name, p.BaseName(), v.Name
	fmt.Fprintf(b, "type %s struct {\n\t%sFields\n", V, P)
	for _, f := range v.Fields {
		fmt.Fprintf(
			b,
			"\t%s %s `json:\"%s\"`\n",
			f.Name,
			f.GoType(),
			jsonName(f.Snake, f.Base.Sensitive()),
		)
	}
	b.WriteString("}\n\n")
	fmt.Fprintf(
		b,
		"func new%s(b *%s) *%s {\n\tn := &%s{%sFields: %sFieldsOf(b)}\n",
		V,
		B,
		V,
		V,
		P,
		lowerFirst(P),
	)
	for _, f := range v.Fields {
		if f.Required && f.Base.Nillable {
			fmt.Fprintf(
				b,
				"\tif b.%s != nil {\n\t\tn.%s = *b.%s\n\t}\n",
				f.Base.StructField(),
				f.Name,
				f.Base.StructField(),
			)
		} else {
			fmt.Fprintf(b, "\tn.%s = b.%s\n", f.Name, f.Base.StructField())
		}
	}
	b.WriteString("\treturn n\n}\n\n")
	fmt.Fprintf(b, "func (*%s) is%s() {}\n\n", V, P)
	for _, f := range v.Fields {
		fmt.Fprintf(b, "func (n *%s) Get%s() %s { return n.%s }\n\n", V, f.Name, f.GoType(), f.Name)
	}
}

func hasMethod(ms []method2, name string) bool {
	for _, m := range ms {
		if m.Name == name {
			return true
		}
	}
	return false
}
