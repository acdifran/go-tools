// Package polygen runs Ent's code generation with the poly step between loading the schema and building the
// graph, which entc.Generate has no hook for.
package polygen

import (
	"errors"
	"fmt"
	"go/token"
	"path"
	"reflect"
	"strings"
	"text/template"

	"entgo.io/ent/entc"
	"entgo.io/ent/entc/gen"
	"github.com/acdifran/go-tools/ent/poly"
)

// Generate is entc.Generate with the poly step. cfg.Target must be set.
func Generate(schemaPath string, cfg *gen.Config, options ...entc.Option) (err error) {
	if cfg.Target == "" {
		return errors.New("polygen: cfg.Target must be set")
	}
	for _, opt := range options {
		if err := opt(cfg); err != nil {
			return err
		}
	}
	if cfg.Storage == nil {
		if cfg.Storage, err = gen.NewStorage("sql"); err != nil {
			return err
		}
	}
	cfg.Templates = append(
		cfg.Templates,
		gen.MustParse(gen.NewTemplate("poly").Parse(initTmpl)),
		gen.MustParse(
			gen.NewTemplate("poly_client").
				Funcs(template.FuncMap{"polyTPHClients": tphClients}).
				Parse(
					clientTmpl,
				),
		),
		gen.MustParse(
			gen.NewTemplate("poly_projection").
				Funcs(template.FuncMap{"polyTPHColumns": tphColumns}).
				Parse(projectionTmpl),
		),
	)
	undo, err := gen.PrepareEnv(cfg)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = undo()
		}
	}()
	graph, err := LoadGraph(schemaPath, cfg)
	if err != nil {
		// entc.Generate restores the schema snapshot when the generated package no longer builds; its own pass
		// regenerates without the poly step, so the schema loads again for this one.
		if ok, _ := cfg.FeatureEnabled(gen.FeatureSnapshot.Name); !ok {
			return err
		}
		if rerr := entc.Generate(schemaPath, cfg); rerr != nil {
			return errors.Join(err, rerr)
		}
		if graph, err = LoadGraph(schemaPath, cfg); err != nil {
			return err
		}
	}
	if err := normalizePkg(cfg); err != nil {
		return err
	}
	if err := graph.Gen(); err != nil {
		return err
	}
	if err := runtimeMixins(cfg.Target); err != nil {
		return err
	}
	return writeOutput(graph)
}

// Ent's newConfig has no hook point for client hooks other than this one, which runs before options apply.
const initTmpl = `{{ define "config/init/fields/poly" }}
	polyInitConfig(&cfg)
{{ end }}`

// LoadGraph is entc.LoadGraph with the poly step, for anything else that reads the graph: template builders and
// migration diffs.
func LoadGraph(schemaPath string, cfg *gen.Config) (*gen.Graph, error) {
	if err := writeStubs(schemaPath); err != nil {
		return nil, fmt.Errorf("polygen: writing TPH tables: %w", err)
	}
	spec, err := loadSpec(schemaPath, cfg.BuildFlags)
	if err != nil {
		return nil, err
	}
	if err := expand(spec); err != nil {
		return nil, err
	}
	cfg.Schema = spec.PkgPath
	if cfg.Package == "" {
		cfg.Package = path.Dir(spec.PkgPath)
	}
	return gen.NewGraph(cfg, spec.Schemas...)
}

func normalizePkg(c *gen.Config) error {
	base := path.Base(c.Package)
	if strings.ContainsRune(base, '-') {
		base = strings.ReplaceAll(base, "-", "_")
		c.Package = path.Join(path.Dir(c.Package), base)
	}
	if !token.IsIdentifier(base) {
		return fmt.Errorf("invalid package identifier: %q", base)
	}
	return nil
}

// The pattern and variant clients sit on Client next to ent's own; ent's client/init has no hook to fill them.
const clientTmpl = `{{ define "client/init" }}
// Client is the client that holds all ent builders.
type Client struct {
	config
	{{- if $.SupportMigrate }}
		// Schema is the client for creating, migrating and dropping schema.
		Schema *migrate.Schema
	{{- end }}
	{{- range $n := $.Nodes }}
		// {{ $n.Name }} is the client for interacting with the {{ $n.Name }} builders.
		{{ $n.Name }} *{{ $n.ClientName }}
	{{- end }}
	{{- range $c := polyTPHClients $ }}
		{{ $c.Name }} *{{ $c.Name }}Client
	{{- end }}
	{{- template "client/fields/additional" $ }}
	{{- with $tmpls := matchTemplate "client/fields/additional/*" }}
		{{- range $tmpl := $tmpls }}
			{{- xtemplate $tmpl $ }}
		{{- end }}
	{{- end }}
}

// NewClient creates a new client configured with the given options.
func NewClient(opts ...Option) *Client {
	client := &Client{config: newConfig(opts...)}
	client.init()
	return client
}

func (c *Client) init() {
	{{- if $.SupportMigrate }}
		c.Schema = migrate.NewSchema(c.driver)
	{{- end }}
	{{- range $n := $.Nodes }}
		c.{{ $n.Name }} = New{{ $n.ClientName }}(c.config)
	{{- end }}
	{{- range $c := polyTPHClients $ }}
		c.{{ $c.Name }} = &{{ $c.Name }}Client{b: New{{ $c.Base }}Client(c.config)}
	{{- end }}
}
{{- range $c := polyTPHClients $ }}

// {{ $c.Name }}Client is the {{ $c.Name }} TPH client; its methods are in poly_tph_*.go.
type {{ $c.Name }}Client struct{ b *{{ $c.Base }}Client }
{{- end }}
{{ end }}`

type tphClient struct{ Name, Base string }

func tphClients(g *gen.Graph) []tphClient {
	var out []tphClient
	for _, n := range g.Nodes {
		a, ok := decode[poly.BaseAnnotation](n.Annotations[poly.BaseAnnotationName])
		if !ok || a.Storage != poly.StorageTPH {
			continue
		}
		out = append(out, tphClient{Name: a.Pattern, Base: n.Name})
		for _, m := range a.Members {
			out = append(out, tphClient{Name: m.Type, Base: n.Name})
		}
	}
	return out
}

// entgql selects only the columns a GraphQL query asks for, and a row can't become its variant without the
// discriminator and the variant's columns. This runs where rows are loaded, not in counts.
const projectionTmpl = `{{ define "dialect/sql/query/spec/poly" }}
	{{- with $cols := polyTPHColumns $ }}
		if len(_spec.Node.Columns) > 0 {
			for _, c := range []string{ {{- range $i, $c := $cols }}{{ if $i }}, {{ end }}{{ $.Package }}.{{ $c }}{{ end }} } {
				if !slices.Contains(_spec.Node.Columns, c) {
					_spec.Node.Columns = append(_spec.Node.Columns, c)
				}
			}
		}
	{{- end }}
{{ end }}

{{ define "import/additional/poly_projection" }}
	{{- if polyTPHColumns $ }}
		"slices"
	{{- end }}
{{ end }}`

// Ent's extension templates get either a *gen.Type or a scope that embeds one.
func tphColumns(v any) []string {
	t, ok := v.(*gen.Type)
	if !ok {
		rv := reflect.Indirect(reflect.ValueOf(v))
		if rv.Kind() != reflect.Struct {
			return nil
		}
		f := rv.FieldByName("Type")
		if !f.IsValid() {
			return nil
		}
		if t, ok = f.Interface().(*gen.Type); !ok {
			return nil
		}
	}
	a, ok := decode[poly.BaseAnnotation](t.Annotations[poly.BaseAnnotationName])
	if !ok || a.Storage != poly.StorageTPH {
		return nil
	}
	var cols []string
	for _, f := range t.Fields {
		if _, variant := f.Annotations[poly.VariantFieldAnnotationName]; variant || f.Name == a.Discriminator {
			cols = append(cols, f.Constant())
		}
	}
	return cols
}
