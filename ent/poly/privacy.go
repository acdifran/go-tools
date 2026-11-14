package poly

import (
	"entgo.io/ent"
	"github.com/acdifran/go-tools/ent/privacystage"
)

// Privacy is everything with privacy rules on a schema, in the order a schema reads: its mixins, then each pattern's
// mixins and the pattern, then its poly edges, then the schema.
func Privacy(s interface{ Mixin() []ent.Mixin }) []privacystage.Contributor {
	var out []privacystage.Contributor
	addMixins := func(ms []ent.Mixin) {
		for _, m := range ms {
			if c, ok := m.(privacystage.Contributor); ok {
				out = append(out, c)
			}
		}
	}
	addMixins(s.Mixin())
	if m, ok := s.(Member); ok {
		for _, p := range m.Patterns() {
			addMixins(p.Mixin())
			if c, ok := p.(privacystage.Contributor); ok {
				out = append(out, c)
			}
		}
	}
	if es, ok := s.(interface{ Edges() []ent.Edge }); ok {
		for _, e := range es.Edges() {
			if b, ok := e.(*EdgeBuilder); ok && b.privacy != nil {
				out = append(out, edgePrivacy{name: b.name, p: *b.privacy})
			}
		}
	}
	if c, ok := s.(privacystage.Contributor); ok {
		out = append(out, c)
	}
	return out
}

type edgePrivacy struct {
	name string
	p    privacystage.Policy
}

func (e edgePrivacy) Privacy() privacystage.Policy { return e.p }

func (e edgePrivacy) PrivacyName() string { return e.name + " edge" }
