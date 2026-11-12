// Package poly declares polymorphic patterns in an ent schema: a pattern is an abstract type whose members are
// real ent types, and a poly edge points at whichever member a row belongs to. polygen expands them while
// loading the schema; this package is what schemas import.
package poly

import (
	"encoding/json"
	"reflect"

	"entgo.io/ent"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/mixin"
)

type Storage string

const StorageTPC Storage = "TPC"

// Pattern is an abstract type. Its methods are read like a schema's: Fields, Edges, Indexes, Hooks, Policy, Mixin
// and Annotations are given to every member.
type Pattern interface {
	ent.Mixin
	Mixin() []ent.Mixin
	polyStorage() Storage
}

// patternBase has no Type method, so a pattern is never an ent.Interface and ent's loader never takes it for a
// table, whichever package it lives in.
type patternBase struct{ mixin.Schema }

func (patternBase) Mixin() []ent.Mixin { return nil }

// TPC stores each member in its own table; an edge to the pattern is one foreign key per member.
type TPC struct{ patternBase }

func (TPC) polyStorage() Storage { return StorageTPC }

func Name(p Pattern) string { return reflect.TypeOf(p).Name() }

const (
	MemberAnnotationName = "PolyMember"
	RefAnnotationName    = "PolyRef"
	EdgesAnnotationName  = "PolyEdges"
	PlaceholderType      = "PolyPlaceholder"
)

type MemberAnnotation struct {
	Pattern string
	Storage Storage
	Fields  []string
}

func (MemberAnnotation) Name() string { return MemberAnnotationName }

type memberMixin struct {
	mixin.Schema
	p Pattern
}

// Member makes the schema a member of the pattern. It goes in Mixin, after the schema's id mixin: a TPC
// member keeps its own id, whose prefix is how an id says which member it belongs to.
func Member(p Pattern) ent.Mixin { return memberMixin{Schema: mixin.Schema{}, p: p} }

func (m memberMixin) Fields() []ent.Field {
	var fields []ent.Field
	for _, mx := range m.p.Mixin() {
		fields = append(fields, mx.Fields()...)
	}
	return append(fields, m.p.Fields()...)
}

func (m memberMixin) Edges() []ent.Edge { return m.p.Edges() }

func (m memberMixin) Indexes() []ent.Index {
	var indexes []ent.Index
	for _, mx := range m.p.Mixin() {
		indexes = append(indexes, mx.Indexes()...)
	}
	return append(indexes, m.p.Indexes()...)
}

func (m memberMixin) Hooks() []ent.Hook {
	var hooks []ent.Hook
	for _, mx := range m.p.Mixin() {
		hooks = append(hooks, mx.Hooks()...)
	}
	return append(hooks, m.p.Hooks()...)
}

func (m memberMixin) Policy() ent.Policy { return m.p.Policy() }

func (m memberMixin) Annotations() []schema.Annotation {
	var names []string
	for _, f := range m.Fields() {
		names = append(names, f.Descriptor().Name)
	}
	anns := append([]schema.Annotation{}, m.p.Annotations()...)
	return append(
		anns,
		MemberAnnotation{Pattern: Name(m.p), Storage: m.p.polyStorage(), Fields: names},
	)
}

type RefAnnotation struct {
	Pattern    string
	Storage    Storage
	Optional   bool
	Immutable  bool
	Ref        string
	Implements []string
	Comment    string
	// Annotations for every member edge, and for one member's edge, keyed by annotation name.
	All     map[string]json.RawMessage
	Members map[string]map[string]json.RawMessage
}

func (RefAnnotation) Name() string { return RefAnnotationName }

type EdgeBuilder struct {
	name       string
	p          Pattern
	optional   bool
	immutable  bool
	ref        string
	implements []string
	comment    string
	all        []schema.Annotation
	members    map[string][]schema.Annotation
	hooks      []ent.Hook
	policy     ent.Policy
}

func To(name string, p Pattern) *EdgeBuilder {
	return &EdgeBuilder{
		name:       name,
		p:          p,
		optional:   false,
		immutable:  false,
		ref:        "",
		implements: nil,
		comment:    "",
		all:        nil,
		members:    map[string][]schema.Annotation{},
		hooks:      nil,
		policy:     nil,
	}
}

func (b *EdgeBuilder) Optional() *EdgeBuilder  { b.optional = true; return b }
func (b *EdgeBuilder) Immutable() *EdgeBuilder { b.immutable = true; return b }

// Ref names the back-edge each member declares, making the member edges inverses of it.
func (b *EdgeBuilder) Ref(ref string) *EdgeBuilder { b.ref = ref; return b }

func (b *EdgeBuilder) Implements(names ...string) *EdgeBuilder {
	b.implements = append(b.implements, names...)
	return b
}

// Comment is the edge's GraphQL description.
func (b *EdgeBuilder) Comment(c string) *EdgeBuilder { b.comment = c; return b }

func (b *EdgeBuilder) Annotations(as ...schema.Annotation) *EdgeBuilder {
	b.all = append(b.all, as...)
	return b
}

// On adds annotations to one member's edge; member is its Type method, such as User.Type.
func (b *EdgeBuilder) On(member any, as ...schema.Annotation) *EdgeBuilder {
	name := typeName(member)
	b.members[name] = append(b.members[name], as...)
	return b
}

func (b *EdgeBuilder) Hooks(hooks ...ent.Hook) *EdgeBuilder {
	b.hooks = append(b.hooks, hooks...)
	return b
}

// Policy runs after the schema's mixin policies and before its own Policy, where a mixin carrying these rules
// would sit.
func (b *EdgeBuilder) Policy(p ent.Policy) *EdgeBuilder {
	b.policy = p
	return b
}

func (b *EdgeBuilder) Descriptor() *edge.Descriptor {
	ref := RefAnnotation{
		Pattern:    Name(b.p),
		Storage:    b.p.polyStorage(),
		Optional:   b.optional,
		Immutable:  b.immutable,
		Ref:        b.ref,
		Implements: b.implements,
		Comment:    b.comment,
		All:        encode(b.all),
		Members:    map[string]map[string]json.RawMessage{},
	}
	for member, as := range b.members {
		ref.Members[member] = encode(as)
	}
	return &edge.Descriptor{
		Name:        b.name,
		Type:        PlaceholderType,
		Annotations: []schema.Annotation{ref},
	}
}

func encode(as []schema.Annotation) map[string]json.RawMessage {
	merged := map[string]schema.Annotation{}
	for _, a := range as {
		if prev, ok := merged[a.Name()]; ok {
			if m, ok := prev.(schema.Merger); ok {
				a = m.Merge(a)
			}
		}
		merged[a.Name()] = a
	}
	out := make(map[string]json.RawMessage, len(merged))
	for name, a := range merged {
		b, err := json.Marshal(a)
		if err != nil {
			panic(err)
		}
		out[name] = b
	}
	return out
}

func typeName(v any) string {
	t := reflect.TypeOf(v)
	if t.Kind() == reflect.Func && t.NumIn() > 0 {
		return t.In(0).Name()
	}
	return t.Name()
}

func Edges(edges ...ent.Edge) []ent.Edge { return edges }

// EdgeHooks are registered as client hooks by the generated code, so they run before the schema's hooks.
func EdgeHooks(edges []ent.Edge) []ent.Hook {
	var hooks []ent.Hook
	for _, e := range edges {
		if b, ok := e.(*EdgeBuilder); ok {
			hooks = append(hooks, b.hooks...)
		}
	}
	return hooks
}

type policyHolder struct{ p ent.Policy }

func (h policyHolder) Policy() ent.Policy { return h.p }

// EdgePolicies are what the generated code inserts between a schema's mixin policies and its own.
func EdgePolicies(edges []ent.Edge) []interface{ Policy() ent.Policy } {
	var out []interface{ Policy() ent.Policy }
	for _, e := range edges {
		if b, ok := e.(*EdgeBuilder); ok && b.policy != nil {
			out = append(out, policyHolder{b.policy})
		}
	}
	return out
}

func MixinPolicies(mixins []ent.Mixin) []interface{ Policy() ent.Policy } {
	var out []interface{ Policy() ent.Policy }
	for _, m := range mixins {
		if m.Policy() != nil {
			out = append(out, m)
		}
	}
	return out
}
