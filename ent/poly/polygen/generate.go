// Package polygen runs Ent's code generation with the poly step between loading the schema and building the
// graph, which entc.Generate has no hook for.
package polygen

import (
	"errors"
	"fmt"
	"go/token"
	"path"
	"strings"

	"entgo.io/ent/entc"
	"entgo.io/ent/entc/gen"
	"entgo.io/ent/entc/load"
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
	cfg.Templates = append(cfg.Templates, gen.MustParse(gen.NewTemplate("poly").Parse(initTmpl)))
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
	return writeOutput(graph)
}

// Ent's newConfig has no hook point for client hooks other than this one, which runs before options apply.
const initTmpl = `{{ define "config/init/fields/poly" }}
	polyInitConfig(&cfg)
{{ end }}`

// LoadGraph is entc.LoadGraph with the poly step, for anything else that reads the graph: template builders and
// migration diffs.
func LoadGraph(schemaPath string, cfg *gen.Config) (*gen.Graph, error) {
	spec, err := (&load.Config{Path: schemaPath, BuildFlags: cfg.BuildFlags}).Load()
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
