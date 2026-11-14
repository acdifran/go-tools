// Package privacystage runs a schema's ent privacy rules in fixed stages: every bypass rule, then every deny, filter and
// allow rule, then a deny for anything no rule allowed. Mixins, poly patterns and edges contribute rules to stages
// instead of to one list, so the order a schema lists its mixins in can't change what its policy decides.
package privacystage

import (
	"context"
	"errors"

	"entgo.io/ent"
	"entgo.io/ent/privacy"
)

// Policy is one contributor's rules by stage. Each stage holds rules the way an ent privacy.Policy does: Bypass and
// Allow rules return Allow or Skip, Deny rules return Deny or Skip, and Filter rules narrow the query and return Skip.
type Policy struct {
	Bypass privacy.Policy
	Deny   privacy.Policy
	Filter privacy.Policy
	Allow  privacy.Policy
}

type Contributor interface {
	Privacy() Policy
}

// Bypass is embedded in a mixin. When its rule allows, that mixin's own rules are skipped and the rest of the
// policy still runs, unlike a rule in the Bypass stage, which ends it.
type Bypass struct {
	Query    privacy.QueryRule
	Mutation privacy.MutationRule
}

func (b Bypass) PrivacyBypass() Bypass { return b }

type bypasser interface {
	PrivacyBypass() Bypass
}

// Query is a schema's read rules by stage, as its generated privacy policy file writes them out.
type Query struct {
	Bypass privacy.QueryPolicy
	Deny   privacy.QueryPolicy
	Filter privacy.QueryPolicy
	Allow  privacy.QueryPolicy
}

// Mutation is a schema's write rules by stage, as its generated permission policy file writes them out.
type Mutation struct {
	Bypass privacy.MutationPolicy
	Deny   privacy.MutationPolicy
	Filter privacy.MutationPolicy
	Allow  privacy.MutationPolicy
}

// Build is the ent policy that runs each stage in order, then denies anything no rule allowed.
func Build(q Query, m Mutation) ent.Policy {
	var query privacy.QueryPolicy
	for _, s := range [...]privacy.QueryPolicy{q.Bypass, q.Deny, q.Filter, q.Allow} {
		query = append(query, s...)
	}
	var mutation privacy.MutationPolicy
	for _, s := range [...]privacy.MutationPolicy{m.Bypass, m.Deny, m.Filter, m.Allow} {
		mutation = append(mutation, s...)
	}
	return privacy.Policy{
		Query:    append(query, denyUnallowed),
		Mutation: append(mutation, denyUnallowed),
	}
}

var denyUnallowed = privacy.ContextQueryMutationRule(func(context.Context) error {
	return privacy.Denyf("no privacy rule allowed this operation")
})

var stageNames = [...]string{"bypass", "deny", "filter", "allow"}

type contributed struct {
	source   Contributor
	query    privacy.QueryPolicy
	mutation privacy.MutationPolicy
}

func collect(cs []Contributor) [len(stageNames)][]contributed {
	var out [len(stageNames)][]contributed
	for _, c := range cs {
		p := c.Privacy()
		var b Bypass
		if bp, ok := c.(bypasser); ok {
			b = bp.PrivacyBypass()
		}
		for i, s := range [...]privacy.Policy{p.Bypass, p.Deny, p.Filter, p.Allow} {
			out[i] = append(out[i], contributed{
				source:   c,
				query:    unlessQuery(b.Query, s.Query),
				mutation: unlessMutation(b.Mutation, s.Mutation),
			})
		}
	}
	return out
}

func unlessQuery(bypass privacy.QueryRule, rules privacy.QueryPolicy) privacy.QueryPolicy {
	if bypass == nil {
		return rules
	}
	out := make(privacy.QueryPolicy, len(rules))
	for i, r := range rules {
		out[i] = QueryUnless(bypass, r)
	}
	return out
}

func unlessMutation(bypass privacy.MutationRule, rules privacy.MutationPolicy) privacy.MutationPolicy {
	if bypass == nil {
		return rules
	}
	out := make(privacy.MutationPolicy, len(rules))
	for i, r := range rules {
		out[i] = MutationUnless(bypass, r)
	}
	return out
}

// QueryUnless runs rule unless bypass allows the query, in which case it skips rule instead of allowing.
func QueryUnless(bypass, rule privacy.QueryRule) privacy.QueryRule {
	return queryUnless{bypass: bypass, rule: rule}
}

// MutationUnless runs rule unless bypass allows the mutation, in which case it skips rule instead of allowing.
func MutationUnless(bypass, rule privacy.MutationRule) privacy.MutationRule {
	return mutationUnless{bypass: bypass, rule: rule}
}

type queryUnless struct{ bypass, rule privacy.QueryRule }

func (u queryUnless) EvalQuery(ctx context.Context, q ent.Query) error {
	if skip, err := bypassed(u.bypass.EvalQuery(ctx, q)); skip || err != nil {
		return err
	}
	return u.rule.EvalQuery(ctx, q)
}

type mutationUnless struct{ bypass, rule privacy.MutationRule }

func (u mutationUnless) EvalMutation(ctx context.Context, m ent.Mutation) error {
	if skip, err := bypassed(u.bypass.EvalMutation(ctx, m)); skip || err != nil {
		return err
	}
	return u.rule.EvalMutation(ctx, m)
}

// A bypass rule that denies, or fails, denies the operation rather than being read as "not bypassed".
func bypassed(decision error) (bool, error) {
	switch {
	case errors.Is(decision, privacy.Allow):
		return true, privacy.Skip
	case decision == nil || errors.Is(decision, privacy.Skip):
		return false, nil
	default:
		return true, decision
	}
}

// On runs rule only for the mutation operations in op. It is privacy.OnMutationOperation, except that Render can
// see the rule it wraps.
func On(rule privacy.MutationRule, op ent.Op) privacy.MutationRule {
	return onOp{rule: rule, op: op}
}

type onOp struct {
	rule privacy.MutationRule
	op   ent.Op
}

func (o onOp) EvalMutation(ctx context.Context, m ent.Mutation) error {
	if m.Op().Is(o.op) {
		return o.rule.EvalMutation(ctx, m)
	}
	return privacy.Skip
}
