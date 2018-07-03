package nodejs

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform/config"
	"github.com/hashicorp/terraform/helper/schema"
	"github.com/pkg/errors"
	"github.com/pulumi/pulumi-terraform/pkg/tfbridge"

	"github.com/pgavlin/firewalker/il"
)

type Generator struct {
	ProjectName string
	module      *il.Graph
}

func cleanName(name string) string {
	if !strings.ContainsAny(name, " -.") {
		return name
	}
	return strings.Map(func(r rune) rune {
		if r == ' ' || r == '-' || r == '.' {
			return '_'
		}
		return r
	}, name)
}

func localName(name string) string {
	return "local_" + cleanName(name)
}

func resName(typ, name string) string {
	return cleanName(fmt.Sprintf("%s_%s", typ, name))
}

func tsName(tfName string, tfSchema *schema.Schema, schemaInfo *tfbridge.SchemaInfo, isObjectKey bool) string {
	if schemaInfo != nil && schemaInfo.Name != "" {
		return schemaInfo.Name
	}

	if strings.ContainsAny(tfName, " -.") {
		if isObjectKey {
			return fmt.Sprintf("\"%s\"", tfName)
		}
		return cleanName(tfName)
	}
	return tfbridge.TerraformToPulumiName(tfName, tfSchema, false)
}

func (g *Generator) computeProperty(prop il.BoundNode, indent, count string) (string, bool, error) {
	containsOutputs := false
	il.VisitBoundNode(prop, il.IdentityVisitor, func(n il.BoundNode) (il.BoundNode, error) {
		if v, ok := n.(*il.BoundVariableAccess); ok {
			switch v.TFVar.(type) {
			case *config.LocalVariable, *config.ResourceVariable:
				containsOutputs = true
			}
		}
		return n, nil
	})

	p, err := il.RewriteAssets(prop)
	if err != nil {
		return "", false, err
	}

	p, err = il.RewriteApplies(p)
	if err != nil {
		return "", false, err
	}

	buf := &bytes.Buffer{}
	generator := &propertyGenerator{w: buf, hil: &hilGenerator{w: buf, countIndex: count}, indent: indent}
	generator.gen(p)
	return buf.String(), containsOutputs, nil
}

func (g *Generator) GeneratePreamble(modules []*il.Graph) error {
	// Emit imports for the various providers
	fmt.Printf("import * as pulumi from \"@pulumi/pulumi\";\n")

	providers := make(map[string]struct{})
	for _, m := range modules {
		for _, p := range m.Providers {
			providers[p.Config.Name] = struct{}{}
		}
	}

	for p := range providers {
		switch p {
		case "archive":
			// Nothing to do
		case "http":
			fmt.Printf("import rpn = require(\"request-promise-native\");\n")
		default:
			fmt.Printf("import * as %s from \"@pulumi/%s\";\n", p, p)
		}
	}
	fmt.Printf("import * as fs from \"fs\";\n")
	fmt.Printf("\n")

	return nil
}

func (g *Generator) BeginModule(m *il.Graph) error {
	if m.ModuleName != "" {
		fmt.Printf("const mod_%s = function(mod_name: string, mod_args: any) {\n")
	}
	g.module = m
	return nil
}

func (g *Generator) EndModule(m *il.Graph) error {
	if m.ModuleName != "" {
		fmt.Printf("\n}")
	}
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
		name := tsName(v.Config.Name, nil, nil, false)

		fmt.Printf("const %s = ", name)
		if v.DefaultValue == nil {
			fmt.Printf("config.require(\"%s\")", name)
		} else {
			def, _, err := g.computeProperty(v.DefaultValue, "", "")
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

func (g *Generator) GenerateLocal(l *il.LocalNode) error {
	value, hasOutputs, err := g.computeProperty(l.Value, "", "")
	if err != nil {
		return err
	}

	fmt.Printf("const %s = ", localName(l.Config.Name))
	if !hasOutputs {
		fmt.Print("pulumi.output(")
	}
	fmt.Printf("%s", value)
	if !hasOutputs {
		fmt.Printf(")")
	}
	fmt.Printf(";\n")
	return nil
}

func (g *Generator) GenerateModule(m *il.ModuleNode) error {
	return errors.New("NYI: modules")
}

func (g *Generator) GenerateResource(r *il.ResourceNode) error {
	switch r.Provider.Config.Name {
	case "archive":
		return g.generateArchive(r)
	case "http":
		return g.generateHTTP(r)
	}

	underscore := strings.IndexRune(r.Config.Type, '_')
	if underscore == -1 {
		return errors.New("NYI: single-resource providers")
	}
	provider, resourceType := r.Config.Type[:underscore], r.Config.Type[underscore+1:]

	memberName := tfbridge.TerraformToPulumiName(resourceType, nil, true)

	module := ""
	if tok, ok := r.Tok(); ok {
		components := strings.Split(tok, ":")
		if len(components) != 3 {
			return errors.Errorf("unexpected resource token format %s", tok)
		}

		mod, typ := components[1], components[2]

		slash := strings.IndexRune(mod, '/')
		if slash == -1 {
			slash = len(mod)
		}

		module, memberName = "."+mod[:slash], typ
		if module == ".index" {
			module = ""
		}
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

	name := resName(r.Config.Type, r.Config.Name)
	qualifiedMemberName := fmt.Sprintf("%s%s.%s", provider, module, memberName)
	if r.Count == nil {
		// If count is nil, this is a single-instance resource.
		inputs, _, err := g.computeProperty(r.Properties, "", "")
		if err != nil {
			return err
		}

		if r.Config.Mode == config.ManagedResourceMode {
			resName := ""
			if g.module.ModuleName == "" {
				resName = fmt.Sprintf("\"%s\"", r.Config.Name)
			} else {
				resName = fmt.Sprintf("`${mod_name}_%s`", r.Config.Name)
			}

			fmt.Printf("const %s = new %s(%s, %s%s);\n", name, qualifiedMemberName, resName, inputs, explicitDeps)
		} else {
			// TODO: explicit dependencies
			fmt.Printf("const %s = pulumi.output(%s(%s));\n", name, qualifiedMemberName, inputs)
		}
	} else {
		// Otherwise we need to Generate multiple resources in a loop.
		count, _, err := g.computeProperty(r.Count, "", "")
		if err != nil {
			return err
		}
		inputs, _, err := g.computeProperty(r.Properties, "    ", "i")
		if err != nil {
			return err
		}

		arrElementType := qualifiedMemberName
		if r.Config.Mode == config.DataResourceMode {
			arrElementType = fmt.Sprintf("Output<%s%s.%sResult>", provider, module, strings.ToUpper(memberName))
		}

		fmt.Printf("const %s: %s[] = [];\n", name, arrElementType)
		fmt.Printf("for (let i = 0; i < %s; i++) {\n", count)
		if r.Config.Mode == config.ManagedResourceMode {
			fmt.Printf("    %s.push(new %s(`%s-${i}`, %s%s));\n", name, qualifiedMemberName, r.Config.Name, inputs, explicitDeps)
		} else {
			// TODO: explicit dependencies
			fmt.Printf("    %s.push(pulumi.output(%s(%s)));\n", name, qualifiedMemberName, inputs)
		}
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
		outputs, _, err := g.computeProperty(o.Value, "", "")
		if err != nil {
			return err
		}

		fmt.Printf("export const %s = %s;\n", tsName(o.Config.Name, nil, nil, false), outputs)
	}
	return nil
}
