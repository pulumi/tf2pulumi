package nodejs

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/hashicorp/terraform/config"
	"github.com/hashicorp/terraform/helper/schema"
	"github.com/pkg/errors"
	"github.com/pulumi/pulumi-terraform/pkg/tfbridge"
	"github.com/pulumi/pulumi/pkg/util/contract"

	"github.com/pulumi/tf2pulumi/gen"
	"github.com/pulumi/tf2pulumi/il"
)

// Generator generates Typescript code that targets the Pulumi libraries from a Terraform configuration.
type Generator struct {
	// ProjectName is the name of the Pulumi project.
	ProjectName string
	// module is the module currently being generated;.
	module *il.Graph
	// indent is the current indentation level for the generated source.
	indent string
	// countIndex is the name (if any) of the currently in-scope count variable.
	countIndex string
	// applyArgs is the list of currently in-scope apply arguments.
	applyArgs []il.BoundExpr
}

// cleanName replaces characters that are not allowed in Typescript identifiers with underscores. No attempt is made to
// ensure that the result is unique.
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

// localName returns the name for a local value with the given Terraform name.
func localName(name string) string {
	return "local_" + cleanName(name)
}

// resName returns the name for a resource instantiation with the given Terraform type and name.
func resName(typ, name string) string {
	return cleanName(fmt.Sprintf("%s_%s", typ, name))
}

// tsName returns the Pulumi name for the property with the given Terraform name and schemas.
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

// indented bumps the current indentation level, invokes the given function, and then resets the indentation level to
// its prior value.
func (g *Generator) indented(f func()) {
	g.indent += "    "
	f()
	g.indent = g.indent[:len(g.indent)-4]
}

// gen generates code for a list of strings and expression trees. The former are written directly to the destination;
// the latter are recursively generated using the appropriate gen* functions.
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

// genf generates code using a format string and its arguments. Any arguments that are BoundNode values are wrapped in
// a FormatFunc that calls the appropriate recursive generation function. This allows for the composition of standard
// format strings with expression/property code gen (e.g. `g.genf(w, ".apply(__arg0 => %v)", then)`, where `then` is
// an expression tree).
func (g *Generator) genf(w io.Writer, format string, args ...interface{}) {
	for i := range args {
		if node, ok := args[i].(il.BoundNode); ok {
			args[i] = gen.FormatFunc(func(f fmt.State, c rune) { g.gen(f, node) })
		}
	}
	fmt.Fprintf(w, format, args...)
}

// computeProperty generates code for the given property into a string ala fmt.Sprintf. It returns both the generated
// code and a bool value that indicates whether or not any output-typed values were nested in the property value.
func (g *Generator) computeProperty(prop il.BoundNode, indent bool, count string) (string, bool, error) {
	// First:
	// - retype any module inputs as the appropriate output types if we are generated a child module definition
	// - discover whether or not the property contains any output-typed expressions
	containsOutputs := false
	il.VisitBoundNode(prop, il.IdentityVisitor, func(n il.BoundNode) (il.BoundNode, error) {
		if v, ok := n.(*il.BoundVariableAccess); ok {
			if !g.isRoot() {
				if _, ok := v.TFVar.(*config.UserVariable); ok {
					v.ExprType = v.ExprType.OutputOf()
				}
			}
			containsOutputs = containsOutputs || v.Type().IsOutput()
		}
		return n, nil
	})

	// Next, rewrite assets, insert any necessary coercions, and run the apply transform.
	p, err := il.RewriteAssets(prop)
	if err != nil {
		return "", false, err
	}

	p, err = addCoercions(p)
	if err != nil {
		return "", false, err
	}

	p, err = il.RewriteApplies(p)
	if err != nil {
		return "", false, err
	}

	// Finally, generate code for the property.
	if indent {
		g.indent += "    "
		defer func() { g.indent = g.indent[:len(g.indent)-4] }()
	}
	buf := &bytes.Buffer{}
	g.gen(buf, p)
	return buf.String(), containsOutputs, nil
}

// isRoot returns true if we are generating code for the root module.
func (g *Generator) isRoot() bool {
	return len(g.module.Tree.Path()) == 0
}

// GeneratePreamble generates appropriate import statements based on the providers referenced by the set of modules.
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
	fmt.Printf("import sprintf = require(\"sprintf-js\");\n")
	fmt.Printf("\n")

	return nil
}

// BeginModule saves the indicated module in the Generator and emits an appropriate function declaration if the module
// is a child module.
func (g *Generator) BeginModule(m *il.Graph) error {
	g.module = m
	if !g.isRoot() {
		fmt.Printf("const new_mod_%s = function(mod_name: string, mod_args: pulumi.Inputs) {\n", cleanName(m.Tree.Name()))
		g.indent += "    "
	}
	return nil
}

// EndModule closes the current module definition if the module is a child module and clears the Generator's module
// field.
func (g *Generator) EndModule(m *il.Graph) error {
	if !g.isRoot() {
		g.indent = g.indent[:len(g.indent)-4]
		fmt.Printf("};\n")
	}
	g.module = nil
	return nil
}

// GenerateVariables generates definitions for the set of user variables in the context of the current module.
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
				fmt.Printf("pulumi.output(mod_args[\"%s\"])", name)
			}
		} else {
			def, _, err := g.computeProperty(v.DefaultValue, false, "")
			if err != nil {
				return err
			}

			if isRoot {
				fmt.Printf("config.get(\"%s\") || %s", name, def)
			} else {
				fmt.Printf("pulumi.output(mod_args[\"%s\"] || %s)", name, def)
			}
		}
		fmt.Printf(";\n")
	}
	fmt.Printf("\n")

	return nil
}

// GenerateLocal generates a single local value. These values are generated as local variable definitions. All local
// values are output-typed for the sake of consistency.
func (g *Generator) GenerateLocal(l *il.LocalNode) error {
	value, hasOutputs, err := g.computeProperty(l.Value, false, "")
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

// GenerateModule generates a single module instantiation. A module instantiation is generated as a call to the
// appropriate module factory function; the result is assigned to a local variable.
func (g *Generator) GenerateModule(m *il.ModuleNode) error {
	// generate a call to the module constructor
	args, _, err := g.computeProperty(m.Properties, false, "")
	if err != nil {
		return err
	}

	modName := cleanName(m.Config.Name)
	fmt.Printf("%sconst mod_%s = new_mod_%s(\"%s\", %s);\n", g.indent, modName, modName, modName, args)
	return nil
}

// GenerateResource generates a single resource instantiation. Each resource instantiation is generated as a call or
// sequence of calls (in the case of a counted resource) to the approriate resource constructor or data source
// function. Single-instance resources are assigned to a local variable; counted resources are stored in an array-typed
// local.
func (g *Generator) GenerateResource(r *il.ResourceNode) error {
	// If this resource's provider is one of the built-ins, perform whatever provider-specific code generation is
	// required.
	switch r.Provider.Config.Name {
	case "archive":
		return g.generateArchive(r)
	case "http":
		return g.generateHTTP(r)
	}

	// Compute the provider name and resource type from the Terraform type.
	underscore := strings.IndexRune(r.Config.Type, '_')
	if underscore == -1 {
		return errors.New("NYI: single-resource providers")
	}
	provider, resourceType := r.Config.Type[:underscore], r.Config.Type[underscore+1:]

	// Convert the TF resource type into its Pulumi name.
	memberName := tfbridge.TerraformToPulumiName(resourceType, nil, true)

	// Compute the module in which the Pulumi type definition lives.
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
		inputs, _, err := g.computeProperty(r.Properties, false, "")
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
		count, _, err := g.computeProperty(r.Count, false, "")
		if err != nil {
			return err
		}
		inputs, _, err := g.computeProperty(r.Properties, true, "i")
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

// GenerateOutputs generates the list of Terraform outputs in the context of the current module.
func (g *Generator) GenerateOutputs(os []*il.OutputNode) error {
	// If there are no outputs, we're done.
	if len(os) == 0 {
		return nil
	}

	// Otherwise, what we do depends on whether or not we're the root module: if we are, we generate a list of exports;
	// if we are not, we generate an appropriate return statement with the outputs as properties in a map.
	isRoot := g.isRoot()

	fmt.Printf("\n")
	if !isRoot {
		fmt.Printf("%sreturn {\n", g.indent)
		g.indent += "    "
	}
	for _, o := range os {
		outputs, _, err := g.computeProperty(o.Value, false, "")
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
