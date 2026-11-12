package polygen

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"entgo.io/contrib/entgql"
	"entgo.io/ent/entc/gen"
	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/formatter"
)

func TestSchemaHook(t *testing.T) {
	target := t.TempDir()
	methods := `package ent

import "context"

func (a ActorMethods) DisplayName() string { return "" }

func (a ActorMethods) Initials(ctx context.Context) (*string, error) { return nil, nil }

func (a ActorMethods) Greet(name string) string { return name }
`
	if err := os.WriteFile(
		filepath.Join(target, "actor.manual.go"),
		[]byte(methods),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	g, err := LoadGraph(
		"./testdata/schema",
		&gen.Config{Target: target, Package: "example.com/ent"},
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range g.Nodes {
		if n.Name == "User" || n.Name == "Agent" {
			n.Annotations[entgql.Annotation{}.Name()] = asJSON(entgql.Implements("Actor"))
		}
	}
	object := func(name string, fields ...*ast.FieldDefinition) *ast.Definition {
		id := &ast.FieldDefinition{Name: "id", Type: ast.NonNullNamedType("ID", nil)}
		return &ast.Definition{
			Kind:   ast.Object,
			Name:   name,
			Fields: append(ast.FieldList{id}, fields...),
		}
	}
	firstName := &ast.FieldDefinition{Name: "firstName", Type: ast.NamedType("String", nil)}
	s := &ast.Schema{Types: map[string]*ast.Definition{
		"User":  object("User", firstName),
		"Agent": object("Agent", firstName),
		"Asset": object("Asset"),
		"Note":  object("Note"),
	}}
	if err := SchemaHook(g, s); err != nil {
		t.Fatal(err)
	}

	field := func(def, name string) string {
		t.Helper()
		d := s.Types[def]
		if d == nil {
			t.Fatalf("no GraphQL type %s", def)
		}
		fd := d.Fields.ForName(name)
		if fd == nil {
			t.Fatalf("%s has no field %s", def, name)
		}
		return fd.Type.String()
	}
	for _, c := range []struct{ def, field, want string }{
		{"Actor", "id", "ID!"},
		{"Actor", "firstName", "String"},
		{"Actor", "displayName", "String!"},
		{"Actor", "initials", "String"},
		{"User", "displayName", "String!"},
		{"Agent", "initials", "String"},
		{"Asset", "actor", "Actor"},
		{"Asset", "actorID", "ID"},
		{"Note", "author", "Actor!"},
		{"Note", "authorID", "ID!"},
		{"WithActor", "actor", "Actor"},
	} {
		if got := field(c.def, c.field); got != c.want {
			t.Errorf("%s.%s is %s, want %s", c.def, c.field, got, c.want)
		}
	}
	if s.Types["Actor"].Fields.ForName("greet") != nil {
		t.Error("a method that takes arguments became a GraphQL field")
	}
	if got := s.Types["Actor"].Interfaces; len(got) != 1 || got[0] != "Node" {
		t.Errorf("Actor implements %v, want Node", got)
	}
	if t.Failed() {
		var b bytes.Buffer
		formatter.NewFormatter(&b).FormatSchema(s)
		t.Log(b.String())
	}
}

func TestGraphQLPatternIsANoder(t *testing.T) {
	storage, err := gen.NewStorage("sql")
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	// entgql writes gql_collection.go; polygen takes it to mean the package has Noder.
	if err := os.WriteFile(
		filepath.Join(target, "gql_collection.go"),
		[]byte("package ent\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	g, err := LoadGraph(
		"./testdata/schema",
		&gen.Config{Target: target, Package: "example.com/ent", Storage: storage},
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range g.Nodes {
		if err := os.MkdirAll(filepath.Join(target, n.PackageDir()), 0o755); err != nil {
			t.Fatal(err)
		}
		if n.Name == "User" || n.Name == "Agent" {
			n.Annotations[entgql.Annotation{}.Name()] = asJSON(entgql.Implements("Actor"))
		}
	}
	if err := writeOutput(g); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(target, "poly_patterns.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "type Actor interface {\n\tNoder\n") {
		t.Errorf("the GraphQL pattern's Go interface doesn't embed Noder:\n%s", b)
	}
}
