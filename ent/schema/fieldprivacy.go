package schema

import (
	"context"
	"fmt"
	"go/token"
	"reflect"
	"runtime"
	"strings"
)

// entc serializes annotations to JSON, so this holds the rule's name rather than the func.
type FieldPrivacyAnnotation struct {
	RulePkg   string
	RuleAlias string
	RuleName  string
}

func (FieldPrivacyAnnotation) Name() string {
	return "FieldPrivacy"
}

// rule must be a top-level func because the generated code calls it by name.
func FieldPrivacy[N any](rule func(ctx context.Context, node N) bool) *FieldPrivacyAnnotation {
	full := runtime.FuncForPC(reflect.ValueOf(rule).Pointer()).Name()
	slash := strings.LastIndex(full, "/")
	pkgEnd := slash + 1 + strings.Index(full[slash+1:], ".")
	if pkgEnd <= slash {
		panic(fmt.Sprintf("FieldPrivacy: cannot resolve rule %q", full))
	}
	name := full[pkgEnd+1:]
	if !token.IsIdentifier(name) {
		panic(fmt.Sprintf("FieldPrivacy: rule %q must be a top-level func, not a closure or method", full))
	}
	pkg := full[:pkgEnd]
	alias := strings.Map(func(r rune) rune {
		if r == '-' || r == '.' {
			return '_'
		}
		return r
	}, pkg[slash+1:])
	return &FieldPrivacyAnnotation{RulePkg: pkg, RuleAlias: alias, RuleName: name}
}
