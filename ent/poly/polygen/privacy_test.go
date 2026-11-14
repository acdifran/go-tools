package polygen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/acdifran/go-tools/ent/privacystage"
)

func TestPolicyFileKeepsManualSectionsAndRewritesTheRest(t *testing.T) {
	stub := &privacyStub{Name: "Memory", Package: "schema", Base: "memory"}
	first := privacystage.Rendered{Stages: [4][2][]privacystage.Rule{}, Imports: map[string]string{
		"github.com/acdifran/go-tools/ent/privacystage": "stages",
		"entgo.io/ent":         "ent",
		"entgo.io/ent/privacy": "privacy",
		"example.com/rules":    "rules",
	}}
	first.Stages[0][1] = []privacystage.Rule{{Expr: "rules.AllowIfRoot()", Source: "BaseMixin"}}
	src, err := renderPolicyFile("memory_permission_policy.go", stub, 1, first, nil)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(
		string(src),
		"// MANUAL ALLOW\n",
		"// MANUAL ALLOW\n\t\t\t\t// Anyone in the org writes memories.\n\t\t\t\tprivacy.AlwaysAllowRule(),\n",
		1,
	)

	second := first
	second.Stages = [4][2][]privacystage.Rule{}
	second.Stages[1][1] = []privacystage.Rule{{Expr: "rules.DenyIfAnonymous()", Source: "BaseMixin"}}
	out, err := renderPolicyFile("memory_permission_policy.go", stub, 1, second, []byte(edited))
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	for _, want := range []string{
		"// Anyone in the org writes memories.\n\t\t\tprivacy.AlwaysAllowRule(),\n\t\t\t// END ALLOW",
		"rules.DenyIfAnonymous(), // BaseMixin",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rewritten file lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "AllowIfRoot") {
		t.Errorf("rewritten file kept a rule its mixins no longer contribute:\n%s", got)
	}
}

func TestSchemaCannotWriteItsOwnPolicy(t *testing.T) {
	dir := t.TempDir()
	src := `package schema

import "entgo.io/ent"

type Memory struct{ ent.Schema }

func (Memory) Policy() ent.Policy { return nil }
`
	if err := os.WriteFile(filepath.Join(dir, "memory.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := scanPrivacy(dir); err == nil || !strings.Contains(err.Error(), "Memory declares Policy") {
		t.Errorf("err = %v, want Memory's own Policy refused", err)
	}
}

func TestRenamedPolicyFileKeepsItsManualSections(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "asset_chunk.go"), []byte("package schema\n\nimport \"entgo.io/ent\"\n\ntype AssetChunk struct{ ent.Schema }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stubs, err := scanPrivacy(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writePrivacyFiles(dir, stubs, nil); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(dir, "asset_chunk_permission_policy.go")
	src, err := os.ReadFile(old)
	if err != nil {
		t.Fatal(err)
	}
	src = []byte(strings.Replace(string(src), "// MANUAL ALLOW\n", "// MANUAL ALLOW\nprivacy.AlwaysAllowRule(),\n", 1))
	if err := os.WriteFile(old, src, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(dir, "asset_chunk.go"), filepath.Join(dir, "assetchunk.go")); err != nil {
		t.Fatal(err)
	}

	if stubs, err = scanPrivacy(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := writePrivacyFiles(dir, stubs, nil); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "assetchunk_permission_policy.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "privacy.AlwaysAllowRule(),\n\t\t\t// END ALLOW") {
		t.Errorf("the renamed file lost its manual allow:\n%s", got)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Errorf("the old file is still there: %v", err)
	}
}
