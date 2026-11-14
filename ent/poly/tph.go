package poly

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"unicode"

	"entgo.io/contrib/entgql"
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

// TPH stores every member in one table: a discriminator column says which member a row is, and each member's own
// fields are nullable columns prefixed with its name. The pattern is its table's ent schema, so its type is named
// <Name>Pattern and lives in the schema package; TPHTable adds its variants to it.
type TPH struct{ ent.Schema }

func (TPH) polyStorage() Storage { return StorageTPH }

func (TPH) Patterns() []Pattern { return nil }

// TPHTable is the full ent schema of a TPH pattern's table: the pattern as written plus its variants' columns, hooks
// and checks. An embedded TPH can't see the pattern it's in, so polygen loads this in the pattern's place and points
// ent's generated runtime at it.
func TPHTable(p Pattern) ent.Interface {
	if p.polyStorage() != StorageTPH {
		panic(fmt.Sprintf("poly: %s is a %s pattern, not TPH", Name(p), p.polyStorage()))
	}
	if t := reflect.TypeOf(p).Name(); t == Name(p) {
		panic(fmt.Sprintf("poly: TPH pattern %s must be named %sPattern, or its table takes the interface's name", t, t))
	}
	return tphTable{Schema: ent.Schema{}, p: p}
}

type tphTable struct {
	ent.Schema
	p Pattern
}

func (t tphTable) Mixin() []ent.Mixin { return t.p.Mixin() }

func (t tphTable) Fields() []ent.Field { return TPHFields(t.p) }

func (t tphTable) Edges() []ent.Edge { return t.p.Edges() }

func (t tphTable) Indexes() []ent.Index { return t.p.Indexes() }

func (t tphTable) Hooks() []ent.Hook { return TPHHooks(t.p) }

func (t tphTable) Interceptors() []ent.Interceptor { return t.p.Interceptors() }

func (t tphTable) Policy() ent.Policy { return t.p.Policy() }

func (t tphTable) Annotations() []schema.Annotation { return TPHAnnotations(t.p) }

func (t tphTable) Patterns() []Pattern {
	if m, ok := t.p.(Member); ok {
		return m.Patterns()
	}
	return nil
}

// Variant is one kind of row in a TPH pattern's table, declared in the pattern's Variants.
type Variant struct {
	name   string
	value  string
	prefix string
	fields []ent.Field
}

// Of declares a variant: its name (the generated type is the name and the pattern's, such as ZendeskConnection)
// and the discriminator value its rows carry.
func Of(name string, value any) Variant {
	return Variant{name: name, value: fmt.Sprint(value), prefix: Snake(name), fields: nil}
}

// Fields are the variant's own fields, nullable columns prefixed with its name. A field it requires gets a CHECK
// instead of NOT NULL, since other variants' rows leave it empty.
func (v Variant) Fields(fs ...ent.Field) Variant {
	v.fields = append(append([]ent.Field{}, v.fields...), fs...)
	return v
}

// ColumnPrefix replaces the prefix of the variant's columns, which is otherwise its name in snake case.
func (v Variant) ColumnPrefix(prefix string) Variant {
	v.prefix = prefix
	return v
}

func (v Variant) member(p Pattern) BaseMember {
	return BaseMember{Type: v.name + Name(p), Value: v.value, Prefix: v.prefix}
}

func variantsOf(p Pattern) []Variant {
	vp, ok := p.(interface{ Variants() []Variant })
	if !ok {
		panic(fmt.Sprintf("poly: TPH pattern %s needs a Variants method", Name(p)))
	}
	return vp.Variants()
}

const (
	DiscriminatorAnnotationName = "PolyDiscriminator"
	BaseAnnotationName          = "PolyBase"
	VariantFieldAnnotationName  = "PolyVariantField"
)

type DiscriminatorAnnotation struct{ Field string }

func (DiscriminatorAnnotation) Name() string { return DiscriminatorAnnotationName }

// Discriminator names the TPH pattern's field that says which variant a row is. It must be an enum, and every
// value needs a variant.
func Discriminator(field string) schema.Annotation { return DiscriminatorAnnotation{Field: field} }

type BaseMember struct {
	Type   string
	Value  string
	Prefix string
}

type BaseAnnotation struct {
	Pattern       string
	Storage       Storage
	Discriminator string
	Members       []BaseMember
}

func (BaseAnnotation) Name() string { return BaseAnnotationName }

type VariantFieldAnnotation struct {
	Variant  string
	Value    string
	Field    string
	Required bool
}

func (VariantFieldAnnotation) Name() string { return VariantFieldAnnotationName }

type rawField struct{ d *field.Descriptor }

func (f rawField) Descriptor() *field.Descriptor { return f.d }

// TPHFields are the pattern's fields followed by each variant's, prefixed. A field a variant requires is optional
// in the table, since other variants' rows leave it empty; TPHAnnotations adds a CHECK for it instead.
func TPHFields(p Pattern) []ent.Field {
	out := append([]ent.Field{}, p.Fields()...)
	for _, v := range variantsOf(p) {
		bm := v.member(p)
		for _, f := range v.fields {
			d := f.Descriptor()
			a := VariantFieldAnnotation{
				Variant:  bm.Type,
				Value:    bm.Value,
				Field:    d.Name,
				Required: !d.Optional,
			}
			d.Name = bm.Prefix + "_" + d.Name
			d.Optional = true
			if d.Info.Type != field.TypeJSON && d.Info.Type != field.TypeOther {
				d.Nillable = true
			}
			d.Annotations = append(d.Annotations, a, entgql.Skip(entgql.SkipAll))
			out = append(out, rawField{d})
		}
	}
	return out
}

// TPHAnnotations are the pattern's annotations plus its table name, the CHECKs for required variant fields and
// the BaseAnnotation polygen reads.
func TPHAnnotations(p Pattern) []schema.Annotation {
	anns := append([]schema.Annotation{}, p.Annotations()...)
	disc := discriminatorOf(p)
	base := BaseAnnotation{Pattern: Name(p), Storage: StorageTPH, Discriminator: disc, Members: nil}
	checks := map[string]string{}
	for _, v := range variantsOf(p) {
		bm := v.member(p)
		base.Members = append(base.Members, bm)
		for _, f := range v.fields {
			d := f.Descriptor()
			if !d.Optional {
				col := bm.Prefix + "_" + d.Name
				checks[col+"_required"] = fmt.Sprintf(
					"%s <> '%s' OR %s IS NOT NULL",
					disc,
					bm.Value,
					col,
				)
			}
		}
	}
	sort.Slice(
		base.Members,
		func(i, j int) bool { return base.Members[i].Type < base.Members[j].Type },
	)
	return append(anns, entsql.Annotation{Table: Snake(Name(p)) + "s", Checks: checks}, base)
}

// Snake is ent's naming for a Go name in the database: FieldHub is field_hub, URLPath is url_path.
func Snake(s string) string {
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

func discriminatorOf(p Pattern) string {
	for _, a := range p.Annotations() {
		if d, ok := a.(DiscriminatorAnnotation); ok {
			return d.Field
		}
	}
	panic(fmt.Sprintf("poly: TPH pattern %s needs poly.Discriminator in its Annotations", Name(p)))
}

type requiredColumn struct{ column, label string }

// TPHHooks are the pattern's hooks plus the Go side of each variant's required fields, which the table can only make
// nullable: like ent's required fields, a create of the variant must set them and no write may clear them. The
// table's CHECKs still catch writes that skip hooks, such as an upsert's update or raw SQL.
func TPHHooks(p Pattern) []ent.Hook {
	hooks := append([]ent.Hook{}, p.Hooks()...)
	required := map[string][]requiredColumn{}
	for _, v := range variantsOf(p) {
		bm := v.member(p)
		for _, f := range v.fields {
			if d := f.Descriptor(); !d.Optional {
				required[bm.Value] = append(required[bm.Value], requiredColumn{
					column: bm.Prefix + "_" + d.Name,
					label:  bm.Type + "." + d.Name,
				})
			}
		}
	}
	if len(required) == 0 {
		return hooks
	}
	disc := discriminatorOf(p)
	return append(hooks, func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			for _, cols := range required {
				for _, c := range cols {
					if m.FieldCleared(c.column) {
						return nil, fmt.Errorf("ent: required field %q can't be cleared", c.label)
					}
				}
			}
			if m.Op().Is(ent.OpCreate) {
				v, _ := m.Field(disc)
				for _, c := range required[fmt.Sprint(v)] {
					if _, ok := m.Field(c.column); !ok {
						return nil, fmt.Errorf("ent: missing required field %q", c.label)
					}
				}
			}
			return next.Mutate(ctx, m)
		})
	})
}
