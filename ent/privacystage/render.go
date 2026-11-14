package privacystage

import (
	"context"
	"errors"
	"fmt"
	"path"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"unicode"

	"entgo.io/ent"
	"entgo.io/ent/privacy"
)

// Namer is implemented by a contributor whose Go type doesn't say where its rules come from, such as an edge.
type Namer interface {
	PrivacyName() string
}

// Rule is one contributed rule as Go source, and where it came from.
type Rule struct {
	Expr   string
	Source string
}

// Rendered is a policy's contributed rules as Go source, by stage and then query and mutation, with the imports the
// source needs keyed by path.
type Rendered struct {
	Stages  [len(stageNames)][2][]Rule
	Imports map[string]string
}

const (
	privacystagePkg = "github.com/acdifran/go-tools/ent/privacystage"
	entPkg          = "entgo.io/ent"
	privacyPkg      = "entgo.io/ent/privacy"
)

// Render writes contributors' rules as the Go that builds them, for a policy file in package pkg. A rule can only be
// rebuilt from a call to the exported, argument-free function that made it, or from privacystage's own wrappers.
func Render(pkg string, cs ...Contributor) (Rendered, error) {
	r := renderer{pkg: pkg, imports: map[string]string{
		privacystagePkg: "privacystage",
		entPkg:          "ent",
		privacyPkg:      "privacy",
	}}
	var out Rendered
	for s, stage := range collect(cs) {
		for _, c := range stage {
			src := sourceName(c.source)
			for _, rule := range c.query {
				expr, err := r.rule(rule)
				if err != nil {
					return Rendered{}, fmt.Errorf("%s %s query rule: %w", src, stageNames[s], err)
				}
				out.Stages[s][0] = append(out.Stages[s][0], Rule{Expr: expr, Source: src})
			}
			for _, rule := range c.mutation {
				expr, err := r.rule(rule)
				if err != nil {
					return Rendered{}, fmt.Errorf("%s %s mutation rule: %w", src, stageNames[s], err)
				}
				out.Stages[s][1] = append(out.Stages[s][1], Rule{Expr: expr, Source: src})
			}
		}
	}
	out.Imports = r.imports
	return out, nil
}

type renderer struct {
	pkg     string
	imports map[string]string
}

func (r renderer) rule(rule any) (string, error) {
	switch rule := rule.(type) {
	case queryUnless:
		return r.wrap("QueryUnless", rule.bypass, rule.rule)
	case mutationUnless:
		return r.wrap("MutationUnless", rule.bypass, rule.rule)
	case onOp:
		inner, err := r.rule(rule.rule)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("privacystage.On(%s, %s)", inner, opExpr(rule.op)), nil
	}
	v := reflect.ValueOf(rule)
	if t := v.Type(); t.PkgPath() == privacyPkg && t.Name() == "fixedDecision" {
		// Its decision field is unexported; a fixed decision ignores the query it's given.
		if errors.Is(rule.(privacy.QueryRule).EvalQuery(context.Background(), nil), privacy.Allow) {
			return "privacy.AlwaysAllowRule()", nil
		}
		return "privacy.AlwaysDenyRule()", nil
	}
	if v.Kind() == reflect.Struct {
		for i := range v.NumField() {
			if f := v.Field(i); f.Kind() == reflect.Func && !f.IsNil() {
				return r.call(f)
			}
		}
	}
	if v.Kind() == reflect.Func {
		return r.call(v)
	}
	return "", fmt.Errorf("can't rebuild a %s", v.Type())
}

func (r renderer) wrap(fn string, bypass, rule any) (string, error) {
	b, err := r.rule(bypass)
	if err != nil {
		return "", err
	}
	inner, err := r.rule(rule)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("privacystage.%s(%s, %s)", fn, b, inner), nil
}

var closureSuffix = regexp.MustCompile(`(\.func\d+|\.\d+|-fm)+$`)

// A rule is a closure, which the runtime names after the function that made it: the rule's constructor.
func (r renderer) call(v reflect.Value) (string, error) {
	full := runtime.FuncForPC(v.Pointer()).Name()
	name := closureSuffix.ReplaceAllString(full, "")
	if name == full {
		return "", fmt.Errorf("%s is a function, not a rule its constructor made", full)
	}
	slash := strings.LastIndex(name, "/")
	dot := strings.Index(name[slash+1:], ".")
	if dot < 0 {
		return "", fmt.Errorf("can't tell %s's package", full)
	}
	pkg, fn := name[:slash+1+dot], name[slash+1+dot+1:]
	if strings.Contains(fn, ".") || !unicode.IsUpper([]rune(fn)[0]) && pkg != r.pkg {
		return "", fmt.Errorf("rule made in %s; give it an exported constructor that takes no arguments", name)
	}
	if pkg == r.pkg {
		return fn + "()", nil
	}
	return r.importName(pkg) + "." + fn + "()", nil
}

func (r renderer) importName(pkg string) string {
	if n, ok := r.imports[pkg]; ok {
		return n
	}
	name := path.Base(pkg)
	for taken(r.imports, name) {
		name = path.Base(path.Dir(pkg)) + name
	}
	r.imports[pkg] = name
	return name
}

func taken(imports map[string]string, name string) bool {
	for _, n := range imports {
		if n == name {
			return true
		}
	}
	return false
}

func sourceName(c Contributor) string {
	if n, ok := c.(Namer); ok {
		return n.PrivacyName()
	}
	t := reflect.TypeOf(c)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t.Name()
}

var opOrder = []struct {
	op   ent.Op
	name string
}{
	{ent.OpCreate, "OpCreate"},
	{ent.OpUpdate, "OpUpdate"},
	{ent.OpUpdateOne, "OpUpdateOne"},
	{ent.OpDelete, "OpDelete"},
	{ent.OpDeleteOne, "OpDeleteOne"},
}

func opExpr(op ent.Op) string {
	var names []string
	for _, o := range opOrder {
		if op.Is(o.op) {
			names = append(names, "ent."+o.name)
		}
	}
	return strings.Join(names, "|")
}
