// Command migratediff runs `atlas migrate diff` against the schema code generation sees, including the poly
// step, where `--to ent://` loads the schema package without it. It dumps that schema to SQL and hands it to atlas
// as the desired state, so the diff itself is atlas's own.
//
//	go run github.com/acdifran/go-tools/ent/poly/cmd/migratediff -schema ./internal/arent/schema -- \
//		--dir file://internal/arent/migrate/migrations --dev-url postgres://... name
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql/schema"
	"entgo.io/ent/entc/gen"
	"github.com/acdifran/go-tools/ent/poly/polygen"
)

func main() {
	schemaPath := flag.String("schema", "", "path to the ent schema package")
	version := flag.String("pg-version", "15", "Postgres version the dumped DDL targets")
	flag.Parse()
	if *schemaPath == "" {
		fmt.Fprintln(
			os.Stderr,
			"usage: migratediff -schema PATH [-pg-version N] -- <atlas migrate diff args>",
		)
		os.Exit(2)
	}
	if err := run(context.Background(), *schemaPath, *version, flag.Args()); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, schemaPath, version string, atlasArgs []string) error {
	graph, err := polygen.LoadGraph(schemaPath, &gen.Config{})
	if err != nil {
		return fmt.Errorf("loading schema: %w", err)
	}
	tables, err := graph.Tables()
	if err != nil {
		return fmt.Errorf("building tables: %w", err)
	}
	ddl, err := schema.Dump(ctx, dialect.Postgres, version, tables)
	if err != nil {
		return fmt.Errorf("dumping schema: %w", err)
	}
	tmp, err := os.MkdirTemp("", "migratediff")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	desired := filepath.Join(tmp, "schema.sql")
	if err := os.WriteFile(desired, []byte(ddl), 0o600); err != nil {
		return err
	}
	args := append([]string{"migrate", "diff", "--to", "file://" + desired}, atlasArgs...)
	cmd := exec.CommandContext(ctx, "atlas", args...)
	// Stdin lets atlas ask whether a dropped and added column is a rename.
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}
