package poly

import (
	"context"
	"strings"
	"testing"

	"entgo.io/ent"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

type hookConn struct{ TPH }

func (hookConn) Fields() []ent.Field { return []ent.Field{field.String("provider")} }

func (hookConn) Variants() []Variant {
	return []Variant{
		Of("Zendesk", "zendesk").Fields(field.String("subdomain"), field.String("trigger_id").Optional()),
		Of("HubSpot", "hubspot").ColumnPrefix("hs").Fields(field.String("portal").Optional()),
	}
}

func (hookConn) Annotations() []schema.Annotation {
	return []schema.Annotation{Discriminator("provider")}
}

// fakeMutation implements only what the hook reads; the embedded nil interface panics on anything else.
type fakeMutation struct {
	ent.Mutation
	op      ent.Op
	fields  map[string]ent.Value
	cleared []string
}

func (m fakeMutation) Op() ent.Op { return m.op }

func (m fakeMutation) Field(name string) (ent.Value, bool) {
	v, ok := m.fields[name]
	return v, ok
}

func (m fakeMutation) FieldCleared(name string) bool {
	for _, c := range m.cleared {
		if c == name {
			return true
		}
	}
	return false
}

func TestTPHHooksEnforceRequiredVariantFields(t *testing.T) {
	hooks := TPHHooks(hookConn{})
	if len(hooks) != 1 {
		t.Fatalf("got %d hooks, want the required-fields hook", len(hooks))
	}
	saved := false
	mutate := hooks[0](ent.MutateFunc(func(context.Context, ent.Mutation) (ent.Value, error) {
		saved = true
		return nil, nil
	}))
	for _, tc := range []struct {
		name    string
		m       fakeMutation
		wantErr string
	}{
		{
			"create with the required field",
			fakeMutation{Mutation: nil, op: ent.OpCreate, fields: map[string]ent.Value{"provider": "zendesk", "zendesk_subdomain": "acme"}, cleared: nil},
			"",
		},
		{
			"create without it",
			fakeMutation{Mutation: nil, op: ent.OpCreate, fields: map[string]ent.Value{"provider": "zendesk"}, cleared: nil},
			`missing required field "ZendeskhookConn.subdomain"`,
		},
		{
			"another variant doesn't need it",
			fakeMutation{Mutation: nil, op: ent.OpCreate, fields: map[string]ent.Value{"provider": "hubspot"}, cleared: nil},
			"",
		},
		{
			"an update needn't set it",
			fakeMutation{Mutation: nil, op: ent.OpUpdateOne, fields: map[string]ent.Value{"provider": "zendesk"}, cleared: nil},
			"",
		},
		{
			"an update can't clear it",
			fakeMutation{Mutation: nil, op: ent.OpUpdate, fields: nil, cleared: []string{"zendesk_subdomain"}},
			`required field "ZendeskhookConn.subdomain" can't be cleared`,
		},
		{
			"an optional field can be cleared",
			fakeMutation{Mutation: nil, op: ent.OpUpdateOne, fields: nil, cleared: []string{"zendesk_trigger_id", "hs_portal"}},
			"",
		},
	} {
		saved = false
		_, err := mutate.Mutate(context.Background(), tc.m)
		switch {
		case tc.wantErr == "" && err != nil:
			t.Errorf("%s: err = %v", tc.name, err)
		case tc.wantErr == "" && !saved:
			t.Errorf("%s: the mutation didn't reach the next mutator", tc.name)
		case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
			t.Errorf("%s: err = %v, want %q", tc.name, err, tc.wantErr)
		case tc.wantErr != "" && saved:
			t.Errorf("%s: the mutation was saved despite the error", tc.name)
		}
	}
}

func TestTPHHooksAddNothingWithoutRequiredFields(t *testing.T) {
	if hooks := TPHHooks(optionalConn{}); len(hooks) != 0 {
		t.Fatalf("got %d hooks, want none", len(hooks))
	}
}

type optionalConn struct{ hookConn }

func (optionalConn) Variants() []Variant {
	return []Variant{Of("Zendesk", "zendesk").Fields(field.String("subdomain").Optional())}
}
