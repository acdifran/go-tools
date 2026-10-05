package schema

import (
	"context"
	"testing"
)

func allowAll(context.Context, any, any) bool { return true }

type ruleHolder struct{}

func (ruleHolder) allow(context.Context, any, any) bool { return true }

func TestFieldPrivacyResolvesTopLevelFunc(t *testing.T) {
	a := FieldPrivacy(allowAll)
	want := FieldPrivacyAnnotation{
		RulePkg:   "github.com/acdifran/go-tools/ent/schema",
		RuleAlias: "schema",
		RuleName:  "allowAll",
	}
	if *a != want {
		t.Fatalf("FieldPrivacy(allowAll) = %+v, want %+v", *a, want)
	}
}

func TestFieldPrivacyRejectsClosuresAndMethods(t *testing.T) {
	for name, rule := range map[string]func(context.Context, any, any) bool{
		"closure": func(context.Context, any, any) bool { return true },
		"method":  ruleHolder{}.allow,
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("expected panic")
				}
			}()
			FieldPrivacy(rule)
		})
	}
}
