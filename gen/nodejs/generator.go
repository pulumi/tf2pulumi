package nodejs

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform/helper/schema"
	"github.com/pkg/errors"
	"github.com/pulumi/pulumi-terraform/pkg/tfbridge"

	"github.com/pgavlin/firewalker/il"
)

// TODO:
// - proper use of apply
// - assets

type Generator struct {
	ProjectName string
	graph       *il.Graph
}

func resName(typ, name string) string {
	n := fmt.Sprintf("%s_%s", typ, name)
	if strings.ContainsAny(n, " -.") {
		return strings.Map(func(r rune) rune {
			if r == ' ' || r == '-' || r == '.' {
				return '_'
			}
			return r
		}, n)
	}
	return n
}

func tsName(tfName string, tfSchema *schema.Schema, schemaInfo *tfbridge.SchemaInfo, isObjectKey bool) string {
	if schemaInfo != nil && schemaInfo.Name != "" {
		return schemaInfo.Name
	}

	if strings.ContainsAny(tfName, " -.") {
		if isObjectKey {
			return fmt.Sprintf("\"%s\"", tfName)
		}
		return strings.Map(func(r rune) rune {
			if r == ' ' || r == '-' || r == '.' {
				return '_'
			}
			return r
		}, tfName)
	}
	return tfbridge.TerraformToPulumiName(tfName, tfSchema, false)
}

func (g *Generator) computeProperty(v interface{}, indent string, sch schemas) (string, error) {
	return g.computePropertyWithCount(v, indent, sch, "")
}

func (g *Generator) computePropertyWithCount(v interface{}, indent string, sch schemas, count string) (string, error) {
	binder := &propertyBinder{hil: &hilBinder{graph: g.graph, hasCountIndex: count != ""}}
	prop, err := binder.bindProperty(v, sch)
	if err != nil {
		return "", err
	}

	prop, err = doAssetRewrite(prop)
	if err != nil {
		return "", err
	}

	prop, err = doApplyRewrite(prop)
	if err != nil {
		return "", err
	}

	buf := &bytes.Buffer{}
	generator := &propertyGenerator{w: buf, hil: &hilGenerator{w: buf, countIndex: count}, indent: indent}
	generator.gen(prop)
	return buf.String(), nil
}

func (g *Generator) GeneratePreamble(gr *il.Graph) error {
	// Stash the graph for later.
	g.graph = gr

	// Emit imports for the various providers
	fmt.Printf("import * as pulumi from \"@pulumi/pulumi\";\n")
	for _, p := range gr.Providers {
		fmt.Printf("import * as %s from \"@pulumi/%s\";\n", p.Config.Name, p.Config.Name)
	}
	fmt.Printf("import * as fs from \"fs\";")
	fmt.Printf("\n\n")

	return nil
}

func (g *Generator) GenerateVariables(vs []*il.VariableNode) error {
	// If there are no variables, we're done.
	if len(vs) == 0 {
		return nil
	}

	// Otherwise, new up a config object and declare the various vars.
	fmt.Printf("const config = new pulumi.Config(\"%s\")\n", g.ProjectName)
	for _, v := range vs {
		name := tfbridge.TerraformToPulumiName(v.Config.Name, nil, false)

		fmt.Printf("const %s = ", name)
		if v.DefaultValue == nil {
			fmt.Printf("config.require(\"%s\")", name)
		} else {
			def, err := g.computeProperty(v.DefaultValue, "", schemas{})
			if err != nil {
				return err
			}

			fmt.Printf("config.get(\"%s\") || %s", name, def)
		}
		fmt.Printf(";\n")
	}
	fmt.Printf("\n")

	return nil
}

func (*Generator) GenerateLocal(l *il.LocalNode) error {
	return errors.New("NYI: locals")
}

func (g *Generator) GenerateResource(r *il.ResourceNode) error {
	config := r.Config

	underscore := strings.IndexRune(config.Type, '_')
	if underscore == -1 {
		return errors.New("NYI: single-resource providers")
	}
	provider, resourceType := config.Type[:underscore], config.Type[underscore+1:]

	var resInfo *tfbridge.ResourceInfo
	var sch schemas
	if r.Provider.Info != nil {
		resInfo = r.Provider.Info.Resources[config.Type]
		sch.tfRes = r.Provider.Info.P.ResourcesMap[config.Type]
		sch.pulumi = &tfbridge.SchemaInfo{Fields: resInfo.Fields}
	}

	typeName := tfbridge.TerraformToPulumiName(resourceType, nil, true)

	module := ""
	if resInfo != nil {
		components := strings.Split(string(resInfo.Tok), ":")
		if len(components) != 3 {
			return errors.Errorf("unexpected resource token format %s", resInfo.Tok)
		}

		mod, typ := components[1], components[2]

		slash := strings.IndexRune(mod, '/')
		if slash == -1 {
			return errors.Errorf("unexpected resource module format %s", mod)
		}

		module, typeName = "."+mod[:slash], typ
	}

	// Build the list of explicit deps, if any.
	explicitDeps := ""
	if len(r.ExplicitDeps) != 0 {
		buf := &bytes.Buffer{}
		fmt.Fprintf(buf, ", {dependsOn: [")
		for i, n := range r.ExplicitDeps {
			if i > 0 {
				fmt.Fprintf(buf, ", ")
			}
			depRes := n.(*il.ResourceNode)
			if depRes.Count != nil {
				fmt.Fprintf(buf, "...")
			}
			fmt.Fprintf(buf, "%s", resName(depRes.Config.Type, depRes.Config.Name))
		}
		fmt.Fprintf(buf, "]}")
		explicitDeps = buf.String()
	}

	name := resName(config.Type, config.Name)
	qualifiedTypeName := fmt.Sprintf("%s%s.%s", provider, module, typeName)
	if r.Count == nil {
		// If count is nil, this is a single-instance resource.
		inputs, err := g.computeProperty(r.Properties, "", sch)
		if err != nil {
			return err
		}

		fmt.Printf("const %s = new %s(\"%s\", %s%s);\n", name, qualifiedTypeName, config.Name, inputs, explicitDeps)
	} else {
		// Otherwise we need to Generate multiple resources in a loop.
		count, err := g.computeProperty(r.Count, "", schemas{})
		if err != nil {
			return err
		}
		inputs, err := g.computePropertyWithCount(r.Properties, "    ", sch, "i")
		if err != nil {
			return err
		}

		fmt.Printf("const %s: %s[] = [];\n", name, qualifiedTypeName)
		fmt.Printf("for (let i = 0; i < %s; i++) {\n", count)
		fmt.Printf("    %s.push(new %s(`%s-${i}`, %s%s));\n", name, qualifiedTypeName, config.Name, inputs, explicitDeps)
		fmt.Printf("}\n")
	}

	return nil
}

func (g *Generator) GenerateOutputs(os []*il.OutputNode) error {
	if len(os) == 0 {
		return nil
	}

	fmt.Printf("\n")
	for _, o := range os {
		outputs, err := g.computeProperty(o.Value, "", schemas{})
		if err != nil {
			return err
		}

		fmt.Printf("export const %s = %s;\n", tsName(o.Config.Name, nil, nil, false), outputs)
	}
	return nil
}
