// Copyright 2016-2018, Pulumi Corporation.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package nodejs

import (
	"bytes"
	"fmt"
	"io"
	"sort"
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
	// rootPath is the path to the directory that contains the root module.
	rootPath string
	// module is the module currently being generated;.
	module *il.Graph
	// indent is the current indentation level for the generated source.
	indent string
	// countIndex is the name (if any) of the currently in-scope count variable.
	countIndex string
	// applyArgs is the list of currently in-scope apply arguments.
	applyArgs []il.BoundExpr
	// unknownInputs is the set of input variables that may be unknown at runtime.
	unknownInputs map[*il.VariableNode]struct{}
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
			_, err := fmt.Fprint(w, v)
			contract.IgnoreError(err)
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
		case *il.BoundError:
			g.genError(w, v)
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

// genError generates code for a node that represents a binding error.
func (g *Generator) genError(w io.Writer, v *il.BoundError) {
	g.gen(w, "(() => {\n")
	g.indented(func() {
		g.genf(w, "%sthrow \"tf2pulumi error: %v\";\n", g.indent, v.Error.Error())
		g.genf(w, "%sreturn %v;\n", g.indent, v.Value)
	})
	g.gen(w, g.indent, "})()")
}

// computeProperty generates code for the given property into a string ala fmt.Sprintf. It returns both the generated
// code and a bool value that indicates whether or not any output-typed values were nested in the property value.
func (g *Generator) computeProperty(prop il.BoundNode, indent bool, count string) (string, bool, error) {
	// First:
	// - retype any possibly-unknown module inputs as the appropriate output types
	// - discover whether or not the property contains any output-typed expressions
	containsOutputs := false
	_, err := il.VisitBoundNode(prop, il.IdentityVisitor, func(n il.BoundNode) (il.BoundNode, error) {
		if n, ok := n.(*il.BoundVariableAccess); ok {
			if v, ok := n.ILNode.(*il.VariableNode); ok {
				if _, ok = g.unknownInputs[v]; ok {
					n.ExprType = n.ExprType.OutputOf()
				}
			}
			containsOutputs = containsOutputs || n.Type().IsOutput()
		}
		return n, nil
	})
	contract.Assert(err == nil)

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
	g.countIndex = count
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
	// Find the root module and stash its path.
	for _, m := range modules {
		if len(m.Tree.Path()) == 0 {
			g.rootPath = m.Tree.Config().Dir
			break
		}
	}
	if g.rootPath == "" {
		return errors.New("could not determine root module path")
	}

	// Emit imports for the various providers
	fmt.Printf("import * as pulumi from \"@pulumi/pulumi\";\n")

	providers := make(map[string]struct{})
	for _, m := range modules {
		for _, p := range m.Providers {
			providers[p.PluginName] = struct{}{}
		}
	}
	for p := range providers {
		switch p {
		case "archive":
			// Nothing to do
		case "http":
			fmt.Printf("import rpn = require(\"request-promise-native\");\n")
		default:
			fmt.Printf("import * as %s from \"@pulumi/%s\";\n", cleanName(p), p)
		}
	}

	// Look for optional imports
	optionals := make(map[string]bool)
	imports := []string{}
	findOptionals := func(n il.BoundNode) (il.BoundNode, error) {
		switch n := n.(type) {
		case *il.BoundCall:
			switch n.HILNode.Func {
			case "file":
				if !optionals["fs"] {
					imports = append(imports, "import * as fs from \"fs\";\n")
					optionals["fs"] = true
				}
			case "format":
				if !optionals["sprintf"] {
					imports = append(imports, "import sprintf = require(\"sprintf-js\");\n")
					optionals["sprintf"] = true
				}
			}
		case *il.BoundVariableAccess:
			if v, ok := n.TFVar.(*config.PathVariable); ok && v.Type == config.PathValueCwd && !optionals["process"] {
				imports = append(imports, "import * as process from \"process\";\n")
				optionals["process"] = true
			}
		}
		return n, nil
	}
	for _, m := range modules {
		for _, n := range m.Modules {
			_, err := il.VisitBoundNode(n.Properties, findOptionals, il.IdentityVisitor)
			contract.Assert(err == nil)
		}
		for _, n := range m.Providers {
			_, err := il.VisitBoundNode(n.Properties, findOptionals, il.IdentityVisitor)
			contract.Assert(err == nil)
		}
		for _, n := range m.Resources {
			_, err := il.VisitBoundNode(n.Properties, findOptionals, il.IdentityVisitor)
			contract.Assert(err == nil)
		}
		for _, n := range m.Outputs {
			_, err := il.VisitBoundNode(n.Value, findOptionals, il.IdentityVisitor)
			contract.Assert(err == nil)
		}
		for _, n := range m.Locals {
			_, err := il.VisitBoundNode(n.Value, findOptionals, il.IdentityVisitor)
			contract.Assert(err == nil)
		}
		for _, n := range m.Variables {
			_, err := il.VisitBoundNode(n.DefaultValue, findOptionals, il.IdentityVisitor)
			contract.Assert(err == nil)
		}
	}

	sort.Strings(imports)
	for _, line := range imports {
		fmt.Print(line)
	}
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

		// Discover the set of input variables that may have unknown values. This is the complete set of inputs minus
		// the set of variables used in count interpolations, as Terraform requires that the latter are known at graph
		// generation time (and thus at Pulumi run time).
		knownInputs := make(map[*il.VariableNode]struct{})
		for _, n := range m.Resources {
			if n.Count != nil {
				_, err := il.VisitBoundNode(n.Count, il.IdentityVisitor, func(n il.BoundNode) (il.BoundNode, error) {
					if n, ok := n.(*il.BoundVariableAccess); ok {
						if v, ok := n.ILNode.(*il.VariableNode); ok {
							knownInputs[v] = struct{}{}
						}
					}
					return n, nil
				})
				contract.Assert(err == nil)
			}
		}
		g.unknownInputs = make(map[*il.VariableNode]struct{})
		for _, v := range m.Variables {
			if _, ok := knownInputs[v]; !ok {
				g.unknownInputs[v] = struct{}{}
			}
		}
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
		fmt.Printf("const config = new pulumi.Config();\n")
	}
	for _, v := range vs {
		name := tsName(v.Config.Name, nil, nil, false)
		_, isUnknown := g.unknownInputs[v]

		fmt.Printf("%sconst var_%s = ", g.indent, cleanName(v.Config.Name))
		if v.DefaultValue == nil {
			if isRoot {
				fmt.Printf("config.require(\"%s\")", name)
			} else {
				f := "mod_args[\"%s\"]"
				if isUnknown {
					f = "pulumi.output(" + f + ")"
				}
				fmt.Printf(f, name)
			}
		} else {
			def, _, err := g.computeProperty(v.DefaultValue, false, "")
			if err != nil {
				return err
			}

			if isRoot {
				get := "get"
				switch v.DefaultValue.Type() {
				case il.TypeBool:
					get = "getBoolean"
				case il.TypeNumber:
					get = "getNumber"
				}
				fmt.Printf("config.%v(\"%s\") || %s", get, name, def)
			} else {
				f := "mod_args[\"%s\"] || %s"
				if isUnknown {
					f = "pulumi.output(" + f + ")"
				}
				fmt.Printf(f, name, def)
			}
		}
		fmt.Printf(";\n")
	}
	fmt.Printf("\n")

	return nil
}

// GenerateLocal generates a single local value. These values are generated as local variable definitions.
func (g *Generator) GenerateLocal(l *il.LocalNode) error {
	value, _, err := g.computeProperty(l.Value, false, "")
	if err != nil {
		return err
	}

	fmt.Printf("%sconst %s = %s;\n", g.indent, localName(l.Config.Name), value)
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

	// Compute the resource type from the Terraform type.
	underscore := strings.IndexRune(r.Config.Type, '_')
	if underscore == -1 {
		return errors.New("NYI: single-resource providers")
	}
	provider, resourceType := cleanName(r.Provider.PluginName), r.Config.Type[underscore+1:]

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
			var resName string
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

		fmt.Printf("%sconst %s: %s[] = [];\n", g.indent, name, arrElementType)
		fmt.Printf("%sfor (let i = 0; i < %s; i++) {\n", g.indent, count)
		g.indented(func() {
			if r.Config.Mode == config.ManagedResourceMode {
				fmt.Printf("%s%s.push(new %s(`%s-${i}`, %s%s));\n", g.indent, name, qualifiedMemberName, r.Config.Name,
					inputs, explicitDeps)
			} else {
				// TODO: explicit dependencies
				fmt.Printf("%s%s.push(pulumi.output(%s(%s)));\n", g.indent, name, qualifiedMemberName, inputs)
			}
		})
		fmt.Printf("%s}\n", g.indent)
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
