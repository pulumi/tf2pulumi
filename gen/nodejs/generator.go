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
	"unicode"

	"github.com/blang/semver"
	"github.com/hashicorp/terraform/config"
	"github.com/hashicorp/terraform/helper/schema"
	"github.com/pkg/errors"
	"github.com/pulumi/pulumi-terraform/pkg/tfbridge"
	"github.com/pulumi/pulumi/pkg/util/contract"

	"github.com/pulumi/tf2pulumi/gen"
	"github.com/pulumi/tf2pulumi/il"
)

// New creates a new NodeJS code generator.
func New(projectName string, targetSDKVersion string, w io.Writer) (gen.Generator, error) {
	v, err := semver.Parse(targetSDKVersion)
	if err != nil {
		return nil, err
	}
	g := &generator{ProjectName: projectName, supportsProxyApplies: v.GTE(semver.MustParse("0.17.0"))}
	g.Emitter = gen.NewEmitter(w, g)
	return g, nil
}

// generator generates Typescript code that targets the Pulumi libraries from a Terraform configuration.
type generator struct {
	// The emitter to use when generating code.
	*gen.Emitter

	// ProjectName is the name of the Pulumi project.
	ProjectName string
	// supportsProxyApplies is true if the target SDK version supports proxied applies on Outputs.
	supportsProxyApplies bool
	// rootPath is the path to the directory that contains the root module.
	rootPath string
	// module is the module currently being generated;.
	module *il.Graph
	// countIndex is the name (if any) of the currently in-scope count variable.
	countIndex string
	// inApplyCall is true iff we are currently generating an apply call.
	inApplyCall bool
	// applyArgs is the list of currently in-scope apply arguments.
	applyArgs []*il.BoundVariableAccess
	// applyArgNames is the list of names for the currently in-scope apply arguments.
	applyArgNames []string
	// unknownInputs is the set of input variables that may be unknown at runtime.
	unknownInputs map[*il.VariableNode]struct{}
	// nameTable is a mapping from top-level nodes to names.
	nameTable map[il.Node]string
}

// isLegalIdentifierStart returns true if it is legal for c to be the first character of a JavaScript identifier as per
// ECMA-262.
func isLegalIdentifierStart(c rune) bool {
	return c == '$' || c == '_' ||
		unicode.In(c, unicode.Lu, unicode.Ll, unicode.Lt, unicode.Lm, unicode.Lo, unicode.Nl)
}

// isLegalIdentifierPart returns true if it is legal for c to be part of a JavaScript identifier (besides the first
// character) as per ECMA-262.
func isLegalIdentifierPart(c rune) bool {
	return isLegalIdentifierStart(c) || unicode.In(c, unicode.Mn, unicode.Mc, unicode.Nd, unicode.Pc)
}

// isLegalIdentifier returns true if s is a legal JavaScript identifier as per ECMA-262.
func isLegalIdentifier(s string) bool {
	reader := strings.NewReader(s)
	c, _, _ := reader.ReadRune()
	if !isLegalIdentifierStart(c) {
		return false
	}
	for {
		c, _, err := reader.ReadRune()
		if err != nil {
			return err == io.EOF
		}
		if !isLegalIdentifierPart(c) {
			return false
		}
	}
}

// cleanName replaces characters that are not allowed in JavaScript identifiers with underscores. No attempt is made to
// ensure that the result is unique.
func cleanName(name string) string {
	var builder strings.Builder
	for i, c := range name {
		if !isLegalIdentifierPart(c) {
			builder.WriteRune('_')
		} else {
			if i == 0 && !isLegalIdentifierStart(c) {
				builder.WriteRune('_')
			}
			builder.WriteRune(c)
		}
	}
	return builder.String()
}

// tsName returns the Pulumi name for the property with the given Terraform name and schemas.
func tsName(tfName string, tfSchema *schema.Schema, schemaInfo *tfbridge.SchemaInfo, isObjectKey bool) string {
	if schemaInfo != nil && schemaInfo.Name != "" {
		return schemaInfo.Name
	}

	if !isLegalIdentifier(tfName) {
		if isObjectKey {
			return fmt.Sprintf("%q", tfName)
		}
		return cleanName(tfName)
	}
	return tfbridge.TerraformToPulumiName(tfName, tfSchema, false)
}

func (g *generator) nodeName(n il.Node) string {
	name, ok := g.nameTable[n]
	contract.Assert(ok)
	return name
}

func (g *generator) variableName(n *il.BoundVariableAccess) string {
	if n.ILNode != nil {
		return g.nodeName(n.ILNode)
	}

	switch v := n.TFVar.(type) {
	case *config.CountVariable:
		return g.countIndex
	case *config.LocalVariable:
		return "local_" + cleanName(v.Name)
	case *config.ModuleVariable:
		return "mod_" + cleanName(v.Name)
	case *config.PathVariable:
		// Path variables are not assigned names.
		return ""
	case *config.ResourceVariable:
		return cleanName(v.Type + "_" + v.Name)
	case *config.UserVariable:
		return "var_" + cleanName(v.Name)
	default:
		contract.Failf("unexpected TF var type in variableName: %T", v)
		return ""
	}
}

func (g *generator) resourceMode(n *il.BoundVariableAccess) config.ResourceMode {
	contract.Assert(n.TFVar.(*config.ResourceVariable) != nil)

	// If this access refers to a missing variable, assume that we are dealing with a managed resource.
	if n.IsMissingVariable() {
		return config.ManagedResourceMode
	}

	return n.ILNode.(*il.ResourceNode).Config.Mode
}

// genError generates code for a node that represents a binding error.
func (g *generator) GenError(w io.Writer, v *il.BoundError) {
	g.Fgen(w, "(() => {\n")
	g.Indented(func() {
		g.Fgenf(w, "%sthrow \"tf2pulumi error: %v\";\n", g.Indent, v.Error.Error())
		g.Fgenf(w, "%sreturn %v;\n", g.Indent, v.Value)
	})
	g.Fgen(w, g.Indent, "})()")
}

// computeProperty generates code for the given property into a string ala fmt.Sprintf.
func (g *generator) computeProperty(prop il.BoundNode, indent bool, count string) (string, error) {
	// First:
	// - retype any possibly-unknown module inputs as the appropriate output types
	_, err := il.VisitBoundNode(prop, il.IdentityVisitor, func(n il.BoundNode) (il.BoundNode, error) {
		if n, ok := n.(*il.BoundVariableAccess); ok {
			if v, ok := n.ILNode.(*il.VariableNode); ok {
				if _, ok = g.unknownInputs[v]; ok {
					n.ExprType = n.ExprType.OutputOf()
				}
			}
		}
		return n, nil
	})
	contract.Assert(err == nil)

	// Next, rewrite assets, lower certain constructrs to literals, insert any necessary coercions, and run the apply
	// transform.
	p, err := il.RewriteAssets(prop)
	if err != nil {
		return "", err
	}

	p, err = g.lowerToLiterals(p)
	if err != nil {
		return "", err
	}

	p, err = il.AddCoercions(p)
	if err != nil {
		return "", err
	}

	p, err = il.RewriteApplies(p)
	if err != nil {
		return "", err
	}

	if g.supportsProxyApplies {
		p, err = g.lowerProxyApplies(p)
		if err != nil {
			return "", err
		}
	}

	// Finally, generate code for the property.
	if indent {
		g.Indent += "    "
		defer func() { g.Indent = g.Indent[:len(g.Indent)-4] }()
	}
	g.countIndex = count
	buf := &bytes.Buffer{}
	g.Fgen(buf, p)
	return buf.String(), nil
}

// isRoot returns true if we are generating code for the root module.
func (g *generator) isRoot() bool {
	return len(g.module.Tree.Path()) == 0
}

// genLeadingComment generates a leading comment into the output.
func (g *generator) genLeadingComment(w io.Writer, comments *il.Comments) {
	if comments == nil {
		return
	}
	for _, l := range comments.Leading {
		g.Fgenf(w, "%s//%s\n", g.Indent, l)
	}
}

// genTrailing comment generates a trailing comment into the output.
func (g *generator) genTrailingComment(w io.Writer, comments *il.Comments) {
	if comments == nil {
		return
	}

	// If this is a single-line comment, generate it as-is. Otherwise, add a line break and generate it as a block.
	if len(comments.Trailing) == 1 {
		g.Fgenf(w, " //%s", comments.Trailing[0])
	} else {
		for _, l := range comments.Trailing {
			g.Fgenf(w, "\n%s//%s", g.Indent, l)
		}
	}
}

// GeneratePreamble generates appropriate import statements based on the providers referenced by the set of modules.
func (g *generator) GeneratePreamble(modules []*il.Graph) error {
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

	// Print the @pulumi/pulumi import at the top.
	g.Println(`import * as pulumi from "@pulumi/pulumi";`)

	// Accumulate other imports for the various providers. Don't emit them yet, as we need to sort them later on.
	var imports []string
	providers := make(map[string]bool)
	for _, m := range modules {
		for _, p := range m.Providers {
			name := p.PluginName
			if !providers[name] {
				providers[name] = true
				switch name {
				case "archive":
					// Nothing to do
				case "http":
					imports = append(imports,
						`import rpn = require("request-promise-native");`)
				default:
					imports = append(imports,
						fmt.Sprintf(`import * as %s from "@pulumi/%s";`, cleanName(name), name))
				}
			}
		}
	}

	// Look for additional optional imports, also appending them to the list so we can sort them later on.
	optionals := make(map[string]bool)
	findOptionals := func(n il.BoundNode) (il.BoundNode, error) {
		switch n := n.(type) {
		case *il.BoundCall:
			switch n.HILNode.Func {
			case "file":
				if !optionals["fs"] {
					imports = append(imports, `import * as fs from "fs";`)
					optionals["fs"] = true
				}
			case "format":
				if !optionals["sprintf"] {
					imports = append(imports, `import sprintf = require("sprintf-js");`)
					optionals["sprintf"] = true
				}
			}
		case *il.BoundVariableAccess:
			if v, ok := n.TFVar.(*config.PathVariable); ok && v.Type == config.PathValueCwd && !optionals["process"] {
				imports = append(imports, `import * as process from "process";`)
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

	// Now sort the imports, so we emit them deterministically, and emit them.
	sort.Strings(imports)
	for _, line := range imports {
		g.Println(line)
	}
	g.Printf("\n")

	return nil
}

// BeginModule saves the indicated module in the generator and emits an appropriate function declaration if the module
// is a child module.
func (g *generator) BeginModule(m *il.Graph) error {
	g.module = m
	if !g.isRoot() {
		g.Printf("const new_mod_%s = function(mod_name: string, mod_args: pulumi.Inputs) {\n",
			cleanName(m.Tree.Name()))
		g.Indent += "    "

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

	// Compute unambiguous names for this module's top-level nodes.
	g.nameTable = assignNames(m, g.isRoot())
	return nil
}

// EndModule closes the current module definition if the module is a child module and clears the generator's module
// field.
func (g *generator) EndModule(m *il.Graph) error {
	if !g.isRoot() {
		g.Indent = g.Indent[:len(g.Indent)-4]
		g.Printf("};\n")
	}
	g.module = nil
	return nil
}

// GenerateVariables generates definitions for the set of user variables in the context of the current module.
func (g *generator) GenerateVariables(vs []*il.VariableNode) error {
	// If there are no variables, we're done.
	if len(vs) == 0 {
		return nil
	}

	// Otherwise, what we do depends on whether or not we're generating the root module. If we are, then we generate
	// a config object and appropriate get/require calls; if we are not, we generate references into the module args.
	isRoot := g.isRoot()
	if isRoot {
		g.Printf("const config = new pulumi.Config();\n")
	}
	for _, v := range vs {
		configName := tsName(v.Config.Name, nil, nil, false)
		_, isUnknown := g.unknownInputs[v]

		g.genLeadingComment(g, v.Comments)

		g.Printf("%sconst %s = ", g.Indent, g.nodeName(v))
		if v.DefaultValue == nil {
			if isRoot {
				g.Printf("config.require(\"%s\")", configName)
			} else {
				f := "mod_args[\"%s\"]"
				if isUnknown {
					f = "pulumi.output(" + f + ")"
				}
				g.Printf(f, configName)
			}
		} else {
			def, err := g.computeProperty(v.DefaultValue, false, "")
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
				g.Printf("config.%v(\"%s\") || %s", get, configName, def)
			} else {
				f := "mod_args[\"%s\"] || %s"
				if isUnknown {
					f = "pulumi.output(" + f + ")"
				}
				g.Printf(f, configName, def)
			}
		}
		g.Printf(";")

		g.genTrailingComment(g, v.Comments)
		g.Printf("\n")
	}
	g.Printf("\n")

	return nil
}

// GenerateLocal generates a single local value. These values are generated as local variable definitions.
func (g *generator) GenerateLocal(l *il.LocalNode) error {
	value, err := g.computeProperty(l.Value, false, "")
	if err != nil {
		return err
	}

	g.genLeadingComment(g, l.Comments)
	g.Printf("%sconst %s = %s;", g.Indent, g.nodeName(l), value)
	g.genTrailingComment(g, l.Comments)
	g.Print("\n")

	return nil
}

// GenerateModule generates a single module instantiation. A module instantiation is generated as a call to the
// appropriate module factory function; the result is assigned to a local variable.
func (g *generator) GenerateModule(m *il.ModuleNode) error {
	// generate a call to the module constructor
	args, err := g.computeProperty(m.Properties, false, "")
	if err != nil {
		return err
	}

	instanceName, modName := g.nodeName(m), cleanName(m.Config.Name)
	g.genLeadingComment(g, m.Comments)
	g.Printf("%sconst %s = new_mod_%s(\"%s\", %s);", g.Indent, instanceName, modName, instanceName, args)
	g.genTrailingComment(g, m.Comments)
	g.Print("\n")

	return nil
}

// resourceTypeName computes the NodeJS package, module, and type name for the given resource.
func resourceTypeName(r *il.ResourceNode) (string, string, string, error) {
	// Compute the resource type from the Terraform type.
	underscore := strings.IndexRune(r.Config.Type, '_')
	if underscore == -1 {
		return "", "", "", errors.New("NYI: single-resource providers")
	}
	provider, resourceType := cleanName(r.Provider.PluginName), r.Config.Type[underscore+1:]

	// Convert the TF resource type into its Pulumi name.
	memberName := tfbridge.TerraformToPulumiName(resourceType, nil, true)

	// Compute the module in which the Pulumi type definition lives.
	module := ""
	if tok, ok := r.Tok(); ok {
		components := strings.Split(tok, ":")
		if len(components) != 3 {
			return "", "", "", errors.Errorf("unexpected resource token format %s", tok)
		}

		mod, typ := components[1], components[2]

		slash := strings.IndexRune(mod, '/')
		if slash == -1 {
			slash = len(mod)
		}

		module, memberName = mod[:slash], typ
		if module == "index" {
			module = ""
		}
	}

	return provider, module, memberName, nil
}

// generateResource handles the generation of instantiations of non-builtin resources.
func (g *generator) generateResource(r *il.ResourceNode) error {
	provider, module, memberName, err := resourceTypeName(r)
	if err != nil {
		return err
	}
	if module != "" {
		module = "." + module
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
			fmt.Fprintf(buf, "%s", g.nodeName(depRes))
		}
		fmt.Fprintf(buf, "]}")
		explicitDeps = buf.String()
	}

	name := g.nodeName(r)
	qualifiedMemberName := fmt.Sprintf("%s%s.%s", provider, module, memberName)

	// Because data sources are treated as normal function calls, we treat them a little bit differently by first
	// rewriting them into calls to the `__dataSource` intrinsic.
	properties := il.BoundNode(r.Properties)
	if r.Config.Mode != config.ManagedResourceMode {
		properties = newDataSourceCall(qualifiedMemberName, properties)
	}

	if r.Count == nil {
		// If count is nil, this is a single-instance resource.
		inputs, err := g.computeProperty(properties, false, "")
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

			g.Printf("%sconst %s = new %s(%s, %s%s);", g.Indent, name, qualifiedMemberName, resName, inputs, explicitDeps)
		} else {
			// TODO: explicit dependencies
			g.Printf("%sconst %s = %s;", g.Indent, name, inputs)
		}
	} else {
		// Otherwise we need to Generate multiple resources in a loop.
		count, err := g.computeProperty(r.Count, false, "")
		if err != nil {
			return err
		}
		inputs, err := g.computeProperty(properties, true, "i")
		if err != nil {
			return err
		}

		arrElementType := qualifiedMemberName
		if r.Config.Mode == config.DataResourceMode {
			arrElementType = fmt.Sprintf("Output<%s%s.%sResult>", provider, module, strings.ToUpper(memberName))
		}

		g.Printf("%sconst %s: %s[] = [];\n", g.Indent, name, arrElementType)
		g.Printf("%sfor (let i = 0; i < %s; i++) {\n", g.Indent, count)
		g.Indented(func() {
			if r.Config.Mode == config.ManagedResourceMode {
				g.Printf("%s%s.push(new %s(`%s-${i}`, %s%s));\n", g.Indent, name, qualifiedMemberName, r.Config.Name,
					inputs, explicitDeps)
			} else {
				// TODO: explicit dependencies
				g.Printf("%s%s.push(%s);\n", g.Indent, name, qualifiedMemberName)
			}
		})
		g.Printf("%s}", g.Indent)
	}

	return nil
}

// GenerateResource generates a single resource instantiation. Each resource instantiation is generated as a call or
// sequence of calls (in the case of a counted resource) to the approriate resource constructor or data source
// function. Single-instance resources are assigned to a local variable; counted resources are stored in an array-typed
// local.
func (g *generator) GenerateResource(r *il.ResourceNode) error {
	g.genLeadingComment(g, r.Comments)

	// If this resource's provider is one of the built-ins, perform whatever provider-specific code generation is
	// required.
	var err error
	switch r.Provider.Config.Name {
	case "archive":
		err = g.generateArchive(r)
	case "http":
		err = g.generateHTTP(r)
	default:
		err = g.generateResource(r)
	}
	if err != nil {
		return err
	}

	g.genTrailingComment(g, r.Comments)
	g.Print("\n")
	return nil
}

// GenerateOutputs generates the list of Terraform outputs in the context of the current module.
func (g *generator) GenerateOutputs(os []*il.OutputNode) error {
	// If there are no outputs, we're done.
	if len(os) == 0 {
		return nil
	}

	// Otherwise, what we do depends on whether or not we're the root module: if we are, we generate a list of exports;
	// if we are not, we generate an appropriate return statement with the outputs as properties in a map.
	isRoot := g.isRoot()

	g.Printf("\n")
	if !isRoot {
		g.Printf("%sreturn {\n", g.Indent)
		g.Indent += "    "
	}
	for _, o := range os {
		outputs, err := g.computeProperty(o.Value, false, "")
		if err != nil {
			return err
		}

		// We combine the leading and trailing comments for the output itself and its value.

		comments := &il.Comments{}
		if o.Comments != nil {
			comments.Leading, comments.Trailing = o.Comments.Leading, o.Comments.Trailing
		}
		if vc := o.Value.Comments(); vc != nil {
			comments.Leading = append(comments.Leading, vc.Leading...)
			comments.Trailing = append(comments.Trailing, vc.Trailing...)
		}

		g.genLeadingComment(g, comments)

		if !isRoot {
			g.Printf("%s%s: %s,", g.Indent, g.nodeName(o), outputs)
		} else {
			g.Printf("export const %s = %s;", g.nodeName(o), outputs)
		}

		g.genTrailingComment(g, comments)
		g.Print("\n")
	}
	if !isRoot {
		g.Indent = g.Indent[:len(g.Indent)-4]
		g.Printf("%s};\n", g.Indent)
	}
	return nil
}
