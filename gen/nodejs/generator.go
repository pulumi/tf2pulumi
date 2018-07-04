package nodejs

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/hashicorp/terraform/config"
	"github.com/hashicorp/terraform/helper/schema"
	"github.com/pkg/errors"
	"github.com/pulumi/pulumi/pkg/util/contract"
	"github.com/pulumi/pulumi-terraform/pkg/tfbridge"

	"github.com/pgavlin/firewalker/gen"
	"github.com/pgavlin/firewalker/il"
)

type Generator struct {
	ProjectName string
	module      *il.Graph

	indent string
	countIndex string
	applyArgs []il.BoundExpr
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

func (g *Generator) isRoot() bool {
	return len(g.module.Tree.Path()) == 0
}

func (g *Generator) indented(f func()) {
	g.indent += "    "
	f()
	g.indent = g.indent[:len(g.indent)-4]
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
	g.gen(buf, p)
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
	g.module = m
	if !g.isRoot() {
		fmt.Printf("const new_mod_%s = function(mod_name: string, mod_args: pulumi.Inputs) {\n", cleanName(m.Tree.Name()))
		g.indent += "    "
	}
	return nil
}

func (g *Generator) EndModule(m *il.Graph) error {
	if !g.isRoot() {
		g.indent = g.indent[:len(g.indent)-4]
		fmt.Printf("};\n")
	}
	g.module = nil
	return nil
}

func (g *Generator) GenerateVariables(vs []*il.VariableNode) error {
	// If there are no variables, we're done.
	if len(vs) == 0 {
		return nil
	}

	// Otherwise, what we do depends on whether or not we're generating the root module. If we are, then we generate
	// a config object and appropriate get/require calls; if we are not, we generate references into the module args.
	isRoot := g.isRoot()
	if isRoot {
		fmt.Printf("const config = new pulumi.Config(\"%s\")\n", g.ProjectName)
	}
	for _, v := range vs {
		name := tsName(v.Config.Name, nil, nil, false)

		fmt.Printf("%sconst %s = ", g.indent, name)
		if v.DefaultValue == nil {
			if isRoot {
				fmt.Printf("config.require(\"%s\")", name)
			} else {
				fmt.Printf("mod_args[\"%s\"]", name)
			}
		} else {
			def, _, err := g.computeProperty(v.DefaultValue, g.indent, "")
			if err != nil {
				return err
			}

			if isRoot {
				fmt.Printf("config.get(\"%s\") || %s", name, def)
			} else {
				fmt.Printf("mod_args[\"%s\"] || %s", name, def)
			}
		}
		fmt.Printf(";\n")
	}
	fmt.Printf("\n")

	return nil
}

func (g *Generator) GenerateLocal(l *il.LocalNode) error {
	value, hasOutputs, err := g.computeProperty(l.Value, g.indent, "")
	if err != nil {
		return err
	}

	fmt.Printf("%sconst %s = ", g.indent, localName(l.Config.Name))
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
	// generate a call to the module constructor
	args, _, err := g.computeProperty(m.Properties, g.indent, "")
	if err != nil {
		return err
	}

	modName := cleanName(m.Config.Name)
	fmt.Printf("%sconst mod_%s = new_mod_%s(\"%s\", %s);\n", g.indent, modName, modName, modName, args)
	return nil
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
		inputs, _, err := g.computeProperty(r.Properties, g.indent, "")
		if err != nil {
			return err
		}

		if r.Config.Mode == config.ManagedResourceMode {
			resName := ""
			if len(g.module.Tree.Path()) == 0 {
				resName = fmt.Sprintf("\"%s\"", r.Config.Name)
			} else {
				resName = fmt.Sprintf("`${mod_name}_%s`", r.Config.Name)
			}

			fmt.Printf("%sconst %s = new %s(%s, %s%s);\n", g.indent, name, qualifiedMemberName, resName, inputs, explicitDeps)
		} else {
			// TODO: explicit dependencies
			fmt.Printf("%sconst %s = pulumi.output(%s(%s));\n", g.indent, name, qualifiedMemberName, inputs)
		}
	} else {
		// Otherwise we need to Generate multiple resources in a loop.
		count, _, err := g.computeProperty(r.Count, g.indent, "")
		if err != nil {
			return err
		}
		inputs, _, err := g.computeProperty(r.Properties, g.indent + "    ", "i")
		if err != nil {
			return err
		}

		arrElementType := qualifiedMemberName
		if r.Config.Mode == config.DataResourceMode {
			arrElementType = fmt.Sprintf("Output<%s%s.%sResult>", provider, module, strings.ToUpper(memberName))
		}

		fmt.Printf("const %s: %s[] = [];\n", name, arrElementType)
		fmt.Printf("for (let i = 0; i < %s; i++) {\n", count)
		g.indented(func() {
			if r.Config.Mode == config.ManagedResourceMode {
				fmt.Printf("%s%s.push(new %s(`%s-${i}`, %s%s));\n", g.indent, name, qualifiedMemberName, r.Config.Name, inputs, explicitDeps)
			} else {
				// TODO: explicit dependencies
				fmt.Printf("%s%s.push(pulumi.output(%s(%s)));\n", g.indent, name, qualifiedMemberName, inputs)
			}
		})
		fmt.Printf("}\n")
	}

	return nil
}

func (g *Generator) GenerateOutputs(os []*il.OutputNode) error {
	if len(os) == 0 {
		return nil
	}

	isRoot := g.isRoot()

	fmt.Printf("\n")
	if !isRoot {
		fmt.Printf("%sreturn {\n", g.indent)
		g.indent += "    "
	}
	for _, o := range os {
		outputs, _, err := g.computeProperty(o.Value, g.indent, "")
		if err != nil {
			return err
		}

		if !isRoot {
			fmt.Printf("%s%s: %s,\n", g.indent, tsName(o.Config.Name, nil, nil, true), outputs)
		} else {
			fmt.Printf("export const %s = %s;\n", tsName(o.Config.Name, nil, nil, false), outputs)
		}
	}
	if !isRoot {
		g.indent = g.indent[:len(g.indent)-4]
		fmt.Printf("%s};\n", g.indent)
	}
	return nil
}

func (g *Generator) gen(w io.Writer, vs ...interface{}) {
	for _, v := range vs {
		switch v := v.(type) {
		case string:
			fmt.Fprint(w, v)
		case *il.BoundArithmetic:
			g.genArithmetic(w, v)
		case *il.BoundCall:
			g.genCall(w, v)
		case *il.BoundConditional:
			g.genConditional(w, v)
		case *il.BoundIndex:
			g.genIndex(w, v)
		case *il.BoundLiteral:
			g.genLiteral(w, v)
		case *il.BoundOutput:
			g.genOutput(w, v)
		case *il.BoundVariableAccess:
			g.genVariableAccess(w, v)
		case *il.BoundListProperty:
			g.genListProperty(w, v)
		case *il.BoundMapProperty:
			g.genMapProperty(w, v)
		default:
			contract.Failf("unexpected type in gen: %T", v)
		}
	}
}

func (g *Generator) genf(w io.Writer, format string, args ...interface{}) {
	for i := range args {
		if expr, ok := args[i].(il.BoundExpr); ok {
			args[i] = gen.FormatFunc(func(f fmt.State, c rune) { g.gen(f, expr) })
		}
	}
	fmt.Fprintf(w, format, args...)
}
