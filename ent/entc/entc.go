package entc

import (
	"embed"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"unicode"

	"entgo.io/ent"
	"entgo.io/ent/entc/gen"
	"github.com/acdifran/go-tools/ent/schema"
)

type NodeWithActions struct {
	Node     *gen.Type
	Actions  []*schema.ActionDef
	NumHooks int
}

func getActions(node *gen.Type, registry map[string]ent.Interface) ([]*schema.ActionDef, error) {
	entNode, ok := registry[node.Name]
	if !ok {
		return nil, fmt.Errorf("type %s not found in registry", node.Name)
	}

	actions := []*schema.ActionDef{}
	mixins := entNode.Mixin()

	for _, mixin := range mixins {
		mixinWithActions, ok := mixin.(schema.EntWithActions)
		if ok {
			actionsFromMixin := mixinWithActions.Actions()
			actions = append(actions, actionsFromMixin...)
		}
	}

	entWithActions, ok := entNode.(schema.EntWithActions)
	if ok {
		actionsFromEnt := entWithActions.Actions()
		actions = append(actions, actionsFromEnt...)
	}

	return actions, nil
}

func BuildEntTemplates(
	entTemplates []*gen.Template,
	graph *gen.Graph,
	registry map[string]ent.Interface,
) {
	for _, template := range entTemplates {
		for _, node := range graph.Nodes {
			actions, err := getActions(node, registry)
			if err != nil {
				log.Fatalf(fmt.Errorf("getting actions for %s: %w", node.Name, err).Error())
			}
			nodeWithActions := &NodeWithActions{Node: node, Actions: actions}

			filename := filepath.Join(
				"./internal/ent",
				strings.ToLower(node.Name)+"_"+template.Name()+".go",
			)
			file, err := os.Create(filename)
			if err != nil {
				log.Fatalf(fmt.Errorf("creating file %s: %w", filename, err).Error())
			}
			defer file.Close()

			err = template.ExecuteTemplate(file, template.Name(), nodeWithActions)
			if err != nil {
				log.Fatalf(fmt.Errorf("executing template for %s: %w", node.Name, err).Error())
			}
		}
	}
}

func LoadTemplates(
	internalPath string,
	dir string,
	embeddedFS embed.FS,
	funcMap template.FuncMap,
) ([]*gen.Template, error) {
	var templates []*gen.Template
	path := internalPath + "/" + dir

	if _, err := os.Stat(path); err == nil {
		files, err := os.ReadDir(path)
		if err != nil {
			return nil, err
		}

		for _, file := range files {
			if filepath.Ext(file.Name()) == ".tmpl" {
				template, err := gen.NewTemplate(strings.TrimSuffix(file.Name(), ".go.tmpl")).
					Funcs(funcMap).
					ParseFiles(filepath.Join(path, file.Name()))
				if err != nil {
					log.Printf("error parsing template %s: %v", file.Name(), err)
					continue
				}
				templates = append(templates, template)
			}
		}
	}

	files, err := fs.ReadDir(embeddedFS, dir)
	if err != nil {
		log.Printf("error reading embedded templates: %v", err)
	} else {
		for _, file := range files {
			if filepath.Ext(file.Name()) == ".tmpl" {
				data, err := fs.ReadFile(embeddedFS, dir+"/"+file.Name())
				if err != nil {
					log.Printf("error reading embedded template %s: %v", file.Name(), err)
					continue
				}
				template, err := gen.NewTemplate(strings.TrimSuffix(file.Name(), ".go.tmpl")).
					Funcs(funcMap).
					Parse(string(data))
				if err != nil {
					log.Printf("error parsing embedded template %s: %v", file.Name(), err)
					continue
				}
				templates = append(templates, template)
			}
		}
	}

	return templates, nil
}

func NodeImplementorsVar(n *gen.Type) string {
	return strings.ToLower(n.Name) + "Implementors"
}

func FirstWordPascalCase(s string) string {
	if s == "" {
		return ""
	}
	for i := 1; i < len(s); i++ {
		if unicode.IsUpper(rune(s[i])) && i > 0 && unicode.IsLower(rune(s[i-1])) {
			return s[:i]
		}
	}
	return s
}
