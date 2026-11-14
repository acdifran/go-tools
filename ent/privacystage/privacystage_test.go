package privacystage

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"entgo.io/ent"
	"entgo.io/ent/privacy"
)

type ranKey struct{}

type viewerKey struct{}

func ran(ctx context.Context, name string) {
	if r, ok := ctx.Value(ranKey{}).(*[]string); ok {
		*r = append(*r, name)
	}
}

func asViewer(name string) context.Context {
	ctx := context.WithValue(context.Background(), viewerKey{}, name)
	return context.WithValue(ctx, ranKey{}, &[]string{})
}

func ranIn(ctx context.Context) []string { return *ctx.Value(ranKey{}).(*[]string) }

// build is what a generated policy file amounts to: the contributors' rules, stage by stage.
func build(cs ...Contributor) ent.Policy {
	var q Query
	var m Mutation
	stages := collect(cs)
	for _, c := range stages[0] {
		q.Bypass, m.Bypass = append(q.Bypass, c.query...), append(m.Bypass, c.mutation...)
	}
	for _, c := range stages[1] {
		q.Deny, m.Deny = append(q.Deny, c.query...), append(m.Deny, c.mutation...)
	}
	for _, c := range stages[2] {
		q.Filter, m.Filter = append(q.Filter, c.query...), append(m.Filter, c.mutation...)
	}
	for _, c := range stages[3] {
		q.Allow, m.Allow = append(q.Allow, c.query...), append(m.Allow, c.mutation...)
	}
	return Build(q, m)
}

// ent wraps a schema's policy in privacy.Policies, which is what turns an Allow into a nil error.
func evalQuery(ctx context.Context, p ent.Policy) error {
	return privacy.Policies{p}.EvalQuery(ctx, nil)
}

func allowIfViewer(name string) privacy.QueryMutationRule {
	return privacy.ContextQueryMutationRule(func(ctx context.Context) error {
		ran(ctx, "allow "+name)
		if ctx.Value(viewerKey{}) == name {
			return privacy.Allow
		}
		return privacy.Skip
	})
}

func denyIfViewer(name string) privacy.QueryMutationRule {
	return privacy.ContextQueryMutationRule(func(ctx context.Context) error {
		ran(ctx, "deny "+name)
		if ctx.Value(viewerKey{}) == name {
			return privacy.Denyf("denied %s", name)
		}
		return privacy.Skip
	})
}

func filterRows() privacy.QueryMutationRule {
	return privacy.ContextQueryMutationRule(func(ctx context.Context) error {
		ran(ctx, "filter")
		return privacy.Skip
	})
}

type contributor struct{ p Policy }

func (c contributor) Privacy() Policy { return c.p }

type bypassedMixin struct {
	contributor
	Bypass
}

func queryPolicy(rules ...privacy.QueryRule) privacy.Policy {
	return privacy.Policy{Query: rules, Mutation: nil}
}

func only(stage string, rules ...privacy.QueryRule) contributor {
	p := Policy{Bypass: privacy.Policy{}, Deny: privacy.Policy{}, Filter: privacy.Policy{}, Allow: privacy.Policy{}}
	switch stage {
	case "bypass":
		p.Bypass = queryPolicy(rules...)
	case "deny":
		p.Deny = queryPolicy(rules...)
	case "filter":
		p.Filter = queryPolicy(rules...)
	case "allow":
		p.Allow = queryPolicy(rules...)
	}
	return contributor{p: p}
}

func TestBuildRunsStagesInOrderWhateverTheContributorOrder(t *testing.T) {
	policy := build(
		only("allow", allowIfViewer("member")),
		only("filter", filterRows()),
		only("deny", denyIfViewer("anonymous")),
		only("bypass", allowIfViewer("root")),
	)

	ctx := asViewer("member")
	if err := evalQuery(ctx, policy); err != nil {
		t.Fatalf("member: %v", err)
	}
	want := "allow root, deny anonymous, filter, allow member"
	if got := strings.Join(ranIn(ctx), ", "); got != want {
		t.Errorf("ran %q, want %q", got, want)
	}

	if err := evalQuery(asViewer("anonymous"), policy); err == nil {
		t.Error("anonymous was allowed, though a deny runs before any allow")
	}

	ctx = asViewer("root")
	if err := evalQuery(ctx, policy); err != nil {
		t.Fatalf("root: %v", err)
	}
	if got := strings.Join(ranIn(ctx), ", "); got != "allow root" {
		t.Errorf("a bypass ran %q after allowing, want nothing", got)
	}

	if err := evalQuery(asViewer("stranger"), policy); err == nil {
		t.Error("a viewer no rule allowed was let through")
	}
}

func TestMixinBypassSkipsOnlyThatMixinsRules(t *testing.T) {
	mixin := bypassedMixin{
		contributor: only("filter", filterRows()),
		Bypass:      Bypass{Query: allowIfViewer("system"), Mutation: nil},
	}
	policy := build(mixin, only("allow", allowIfViewer("system"), allowIfViewer("member")))

	ctx := asViewer("system")
	if err := evalQuery(ctx, policy); err != nil {
		t.Fatalf("system: %v", err)
	}
	want := "allow system, allow system"
	if got := strings.Join(ranIn(ctx), ", "); got != want {
		t.Errorf("system ran %q, want %q: the mixin's filter skipped and the policy carrying on", got, want)
	}

	ctx = asViewer("member")
	if err := evalQuery(ctx, policy); err != nil {
		t.Fatalf("member: %v", err)
	}
	want = "allow system, filter, allow system, allow member"
	if got := strings.Join(ranIn(ctx), ", "); got != want {
		t.Errorf("member ran %q, want %q", got, want)
	}
}

func TestBypassThatDeniesDenies(t *testing.T) {
	mixin := bypassedMixin{
		contributor: only("filter", filterRows()),
		Bypass:      Bypass{Query: denyIfViewer("banned"), Mutation: nil},
	}
	if err := evalQuery(asViewer("banned"), build(mixin, only("allow", privacy.AlwaysAllowRule()))); err == nil {
		t.Errorf("err = %v, want the bypass's deny", err)
	}
}

type opMutation struct {
	ent.Mutation
	op ent.Op
}

func (m opMutation) Op() ent.Op { return m.op }

func TestOnRunsOnlyForItsOperations(t *testing.T) {
	rule := On(privacy.AlwaysDenyRule(), ent.OpDelete|ent.OpDeleteOne)
	if err := rule.EvalMutation(context.Background(), opMutation{Mutation: nil, op: ent.OpUpdateOne}); !errors.Is(
		err,
		privacy.Skip,
	) {
		t.Errorf("update: err = %v, want skip", err)
	}
	if err := rule.EvalMutation(context.Background(), opMutation{Mutation: nil, op: ent.OpDeleteOne}); err == nil ||
		errors.Is(err, privacy.Skip) {
		t.Errorf("deleteOne: err = %v, want deny", err)
	}
}

func systemViewer() privacy.QueryMutationRule { return allowIfViewer("system") }

func memberDenied() privacy.QueryMutationRule { return denyIfViewer("member") }

func TestRenderRebuildsEachRuleAndNamesItsSource(t *testing.T) {
	mixin := bypassedMixin{
		contributor: only("filter", filterRows()),
		Bypass:      Bypass{Query: systemViewer(), Mutation: nil},
	}
	schema := contributor{p: Policy{
		Bypass: privacy.Policy{},
		Deny: privacy.Policy{
			Query:    nil,
			Mutation: privacy.MutationPolicy{On(memberDenied(), ent.OpDelete|ent.OpDeleteOne)},
		},
		Filter: privacy.Policy{},
		Allow:  queryPolicy(privacy.AlwaysAllowRule()),
	}}
	got, err := Render(privacystagePkg, mixin, schema)
	if err != nil {
		t.Fatal(err)
	}
	want := map[[2]int][]Rule{
		{2, 0}: {{Expr: "privacystage.QueryUnless(allowIfViewer(), filterRows())", Source: "bypassedMixin"}},
		{1, 1}: {{Expr: "privacystage.On(denyIfViewer(), ent.OpDelete|ent.OpDeleteOne)", Source: "contributor"}},
		{3, 0}: {{Expr: "privacy.AlwaysAllowRule()", Source: "contributor"}},
	}
	for s := range got.Stages {
		for k := range got.Stages[s] {
			if g, w := got.Stages[s][k], want[[2]int{s, k}]; !slices.Equal(g, w) {
				t.Errorf("%s %d = %v, want %v", stageNames[s], k, g, w)
			}
		}
	}
}

type inlineMixin struct{}

func (inlineMixin) Privacy() Policy {
	return only("allow", privacy.ContextQueryMutationRule(func(context.Context) error { return privacy.Skip })).p
}

func TestRenderRefusesARuleWrittenInline(t *testing.T) {
	if _, err := Render(privacystagePkg, inlineMixin{}); err == nil {
		t.Error("an inline rule rendered, though nothing can rebuild it")
	}
}
