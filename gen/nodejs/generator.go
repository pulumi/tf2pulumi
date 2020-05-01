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
	"github.com/hashicorp/terraform-plugin-sdk/helper/schema"
	"github.com/pkg/errors"
	"github.com/pulumi/pulumi-terraform-bridge/v2/pkg/tfbridge"
	"github.com/pulumi/pulumi/sdk/v2/go/common/util/contract"

	"github.com/pulumi/tf2pulumi/gen"
	"github.com/pulumi/tf2pulumi/il"
	"github.com/pulumi/tf2pulumi/internal/config"
)

// Options defines parameters that are specific to the NodeJS code generator.
type Options struct {
	// UsePromptDataSources is true if the target provider supports prompt invocation of data sources.
	UsePromptDataSources bool
}

// New creates a new NodeJS code generator.
func New(projectName string, targetSDKVersion string, usePromptDataSources bool, w io.Writer) (gen.Generator, error) {
	supportsProxyApplies := true
	if targetSDKVersion != "" {
		v, err := semver.Parse(targetSDKVersion)
		if err != nil {
			return nil, err
		}
		supportsProxyApplies = v.GTE(semver.MustParse("0.17.0"))
	}
	g := &generator{
		ProjectName:          projectName,
		supportsProxyApplies: supportsProxyApplies,
		usePromptDataSources: usePromptDataSources,
		importNames:          make(map[string]bool),
	}
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
	// usePromptDataSources is true if the target provider supports prompt invocation of data sources.
	usePromptDataSources bool
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
	// promptDataSources is a table of datasources that do not contain output-typed inputs.
	promptDataSources map[*il.ResourceNode]bool
	// importNames is the set of names used by package imports.
	importNames map[string]bool
	// conditionalResources is a table of resources that are instantiated at most once.
	conditionalResources map[*il.ResourceNode]bool
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
	return tfbridge.TerraformToPulumiName(tfName, tfSchema, nil, false)
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

func (g *generator) isDataSourceAccess(n *il.BoundVariableAccess) bool {
	contract.Assert(n.TFVar.(*config.ResourceVariable) != nil)

	// If this access refers to a missing variable, assume that we are dealing with a managed resource.
	if n.IsMissingVariable() {
		return false
	}

	return n.ILNode.(*il.ResourceNode).IsDataSource
}

// isConditionalResource returns true if the given resource is conditionally-instantiated (i.e. the count is a boolean
// value).
func (g *generator) isConditionalResource(r *il.ResourceNode) bool {
	return g.conditionalResources[r]
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

// computeProperty generates code for the given property into a string ala fmt.Sprintf. It returns both the generated
// code and a bool value that indicates whether or not any output-typed values were nested in the property value.
func (g *generator) computeProperty(prop il.BoundNode, indent bool, count string) (string, bool, error) {
	// First:
	// - retype any possibly-unknown module inputs as the appropriate output types
	// - discover whether or not the property contains any output-typed expressions
	containsOutputs := false
	_, err := il.VisitBoundNode(prop, il.IdentityVisitor, func(n il.BoundNode) (il.BoundNode, error) {
		if n, ok := n.(*il.BoundVariableAccess); ok {
			containsOutputs = containsOutputs || n.Type().IsOutput()
		}
		return n, nil
	})
	contract.Assert(err == nil)

	// Next, rewrite assets, lower certain constructrs to literals, insert any necessary coercions, and run the apply
	// transform.
	p, err := il.RewriteAssets(prop)
	if err != nil {
		return "", false, err
	}

	p, err = g.lowerToLiterals(p)
	if err != nil {
		return "", false, err
	}

	p, err = il.AddCoercions(p)
	if err != nil {
		return "", false, err
	}

	p, err = il.RewriteApplies(p)
	if err != nil {
		return "", false, err
	}

	if g.supportsProxyApplies {
		p, err = g.lowerProxyApplies(p)
		if err != nil {
			return "", false, err
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
	return buf.String(), containsOutputs, nil
}

// isRoot returns true if we are generating code for the root module.
func (g *generator) isRoot() bool {
	return g.module.IsRoot
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
		if m.IsRoot {
			g.rootPath = m.Path
			break
		}
	}
	if g.rootPath == "" {
		g.rootPath = "."
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
					g.importNames["rpn"] = true
				default:
					importName := cleanName(name)
					imports = append(imports,
						fmt.Sprintf(`import * as %s from "@pulumi/%s";`, importName, name))
					g.importNames[importName] = true
				}
			}
		}
	}

	// Look for additional optional imports, also appending them to the list so we can sort them later on.
	findOptionals := func(n il.BoundNode) (il.BoundNode, error) {
		switch n := n.(type) {
		case *il.BoundCall:
			switch n.Func {
			case "file":
				if !g.importNames["fs"] {
					imports = append(imports, `import * as fs from "fs";`)
					g.importNames["fs"] = true
				}
			case "format":
				if !g.importNames["sprintf"] {
					imports = append(imports, `import sprintf = require("sprintf-js");`)
					g.importNames["sprintf"] = true
				}
			}
		case *il.BoundVariableAccess:
			if v, ok := n.TFVar.(*config.PathVariable); ok && v.Type == config.PathValueCwd && !g.importNames["process"] {
				imports = append(imports, `import * as process from "process";`)
				g.importNames["process"] = true
			}
		}
		return n, nil
	}
	for _, m := range modules {
		err := il.VisitAllProperties(m, findOptionals, il.IdentityVisitor)
		contract.Assert(err == nil)
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
			cleanName(m.Name))
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

		// Retype any possibly-unknown module inputs as the appropriate output type.
		err := il.VisitAllProperties(m, il.IdentityVisitor, func(n il.BoundNode) (il.BoundNode, error) {
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
	}

	// Find all prompt datasources if possible.
	if g.usePromptDataSources {
		g.promptDataSources = il.MarkPromptDataSources(m)
	}

	// Find all conditional resources.
	g.conditionalResources = il.MarkConditionalResources(m)

	// Compute unambiguous names for this module's top-level nodes.
	g.nameTable = assignNames(m, g.importNames, g.isRoot())
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
		configName := tsName(v.Name, nil, nil, false)
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
	value, _, err := g.computeProperty(l.Value, false, "")
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
	args, _, err := g.computeProperty(m.Properties, false, "")
	if err != nil {
		return err
	}

	instanceName, modName := g.nodeName(m), cleanName(m.Name)
	g.genLeadingComment(g, m.Comments)
	g.Printf("%sconst %s = new_mod_%s(\"%s\", %s);", g.Indent, instanceName, modName, instanceName, args)
	g.genTrailingComment(g, m.Comments)
	g.Print("\n")

	return nil
}

// GenerateProvider generates a single provider instantiation. Each provider instantiation is generated as a call to
// the appropriate provider constructor that is assigned to a local variable.
func (g *generator) GenerateProvider(p *il.ProviderNode) error {
	// If this provider has no alias, ignore it.
	if p.Alias == "" {
		return nil
	}

	g.genLeadingComment(g, p.Comments)

	name := g.nodeName(p)
	qualifiedMemberName := p.PluginName + ".Provider"

	inputs, _, err := g.computeProperty(il.BoundNode(p.Properties), false, "")
	if err != nil {
		return err
	}

	var resName string
	if g.isRoot() {
		resName = fmt.Sprintf("\"%s\"", p.Alias)
	} else {
		resName = fmt.Sprintf("`${mod_name}_%s`", p.Alias)
	}

	g.Printf("%sconst %s = new %s(%s, %s);", g.Indent, name, qualifiedMemberName, resName, inputs)
	g.genTrailingComment(g, p.Comments)
	g.Print("\n")
	return nil
}

// resourceTypeName computes the NodeJS package, module, and type name for the given resource.
func resourceTypeName(r *il.ResourceNode) (string, string, string, error) {
	// Compute the resource type from the Terraform type.
	underscore := strings.IndexRune(r.Type, '_')
	if underscore == -1 {
		return "", "", "", errors.New("NYI: single-resource providers")
	}
	provider, resourceType := cleanName(r.Provider.PluginName), r.Type[underscore+1:]

	// Convert the TF resource type into its Pulumi name.
	memberName := tfbridge.TerraformToPulumiName(resourceType, nil, nil, true)

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

// makeResourceName returns the expression that should be emitted for a resource's "name" parameter given its base name
// and the count variable name, if any.
func (g *generator) makeResourceName(baseName, count string) string {
	if g.isRoot() {
		if count == "" {
			return fmt.Sprintf(`"%s"`, baseName)
		}
		return fmt.Sprintf("`%s-${%s}`", baseName, count)
	}
	baseName = fmt.Sprintf("${mod_name}_%s", baseName)
	if count == "" {
		return fmt.Sprintf("`%s`", baseName)
	}
	return fmt.Sprintf("`%s-${%s}`", baseName, count)
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

	var resourceOptions []string
	if r.Provider.Alias != "" {
		resourceOptions = append(resourceOptions, "provider: "+g.nodeName(r.Provider))
	}

	// Build the list of explicit deps, if any.
	if len(r.ExplicitDeps) != 0 && !r.IsDataSource {
		buf := &bytes.Buffer{}
		fmt.Fprintf(buf, "dependsOn: [")
		for i, n := range r.ExplicitDeps {
			if i > 0 {
				fmt.Fprintf(buf, ", ")
			}
			depRes := n.(*il.ResourceNode)
			if depRes.Count != nil {
				if g.isConditionalResource(depRes) {
					fmt.Fprintf(buf, "!")
				} else {
					fmt.Fprintf(buf, "...")
				}
			}
			fmt.Fprintf(buf, "%s", g.nodeName(depRes))
		}
		fmt.Fprintf(buf, "]")
		resourceOptions = append(resourceOptions, buf.String())
	}

	if r.Timeouts != nil {
		buf := &bytes.Buffer{}
		g.Fgenf(buf, "timeouts: %v", r.Timeouts)
		resourceOptions = append(resourceOptions, buf.String())
	}

	if len(r.IgnoreChanges) != 0 {
		buf := &bytes.Buffer{}
		fmt.Fprintf(buf, "ignoreChanges: [")
		for i, ic := range r.IgnoreChanges {
			if i > 0 {
				fmt.Fprintf(buf, ", ")
			}
			fmt.Fprintf(buf, "\"%s\"", ic)
		}
		fmt.Fprintf(buf, "]")
		resourceOptions = append(resourceOptions, buf.String())
	}

	if r.IsDataSource && !g.promptDataSources[r] {
		resourceOptions = append(resourceOptions, "async: true")
	}

	optionsBag := ""
	if len(resourceOptions) != 0 {
		optionsBag = fmt.Sprintf("{ %s }", strings.Join(resourceOptions, ", "))
	}

	name := g.nodeName(r)
	qualifiedMemberName := fmt.Sprintf("%s%s.%s", provider, module, memberName)

	// Because data sources are treated as normal function calls, we treat them a little bit differently by first
	// rewriting them into calls to the `__dataSource` intrinsic.
	properties := il.BoundNode(r.Properties)
	if r.IsDataSource {
		properties = newDataSourceCall(qualifiedMemberName, properties, optionsBag)
	}

	if optionsBag != "" {
		optionsBag = ", " + optionsBag
	}

	if r.Count == nil {
		// If count is nil, this is a single-instance resource.
		inputs, transformed, err := g.computeProperty(properties, false, "")
		if err != nil {
			return err
		}

		if !r.IsDataSource {
			resName := g.makeResourceName(r.Name, "")
			g.Printf("%sconst %s = new %s(%s, %s%s);", g.Indent, name, qualifiedMemberName, resName, inputs, optionsBag)
		} else {
			// TODO: explicit dependencies

			// If the input properties did not contain any outputs, then we need to wrap the result in a call to pulumi.output.
			// Otherwise, we are okay as-is: the apply rewrite perfomed by computeProperty will have ensured that the result
			// is output-typed.
			fmtstr := "%sconst %s = pulumi.output(%s);"
			if g.promptDataSources[r] || transformed {
				fmtstr = "%sconst %s = %s;"
			}

			g.Printf(fmtstr, g.Indent, name, inputs)
		}
	} else if g.isConditionalResource(r) {
		// If this is a confitional resource, we need to generate a resource that is instantiated inside an if statement.

		// If this resource's properties reference its count, we need to generate its code slightly differently:
		// a) We need to assign the value of the count to a local s.t. the properties have something to reference
		// b) We want to avoid changing the type of the count if it is not a boolean so that downstream code does not
		//    require changes.
		hasCountReference, countVariableName := false, ""
		_, err = il.VisitBoundNode(properties, il.IdentityVisitor, func(n il.BoundNode) (il.BoundNode, error) {
			if n, ok := n.(*il.BoundVariableAccess); ok {
				_, isCountVar := n.TFVar.(*config.CountVariable)
				hasCountReference = hasCountReference || isCountVar
			}
			return n, nil
		})
		contract.Assert(err == nil)

		// If the resource's properties do not reference the count, we can simplify the condition expression for
		// cleaner-looking code. We don't do this if the count is referenced because it can change the type of the
		// expression (e.g. from a number to a boolean, if the number is statically coerceable to a boolean).
		count := r.Count
		if !hasCountReference {
			count = il.SimplifyBooleanExpressions(count.(il.BoundExpr))
		}
		condition, _, err := g.computeProperty(count, false, "")
		if err != nil {
			return err
		}

		// If the resoure's properties reference the count, assign its value to a local s.t. the properties have
		// something to refer to.
		if hasCountReference {
			countVariableName = fmt.Sprintf("create%s", title(name))
			g.Printf("%sconst %s = %s;\n", g.Indent, countVariableName, condition)
			condition = countVariableName
		}

		inputs, transformed, err := g.computeProperty(properties, true, countVariableName)
		if err != nil {
			return err
		}

		g.Printf("%slet %s: %s | undefined;\n", g.Indent, name, qualifiedMemberName)
		ifFmt := "%sif (%s) {\n"
		if count.Type() != il.TypeBool {
			ifFmt = "%sif (!!(%s)) {\n"
		}
		g.Printf(ifFmt, g.Indent, condition)
		g.Indented(func() {
			if !r.IsDataSource {
				resName := g.makeResourceName(r.Name, "")
				g.Printf("%s%s = new %s(%s, %s%s);\n", g.Indent, name, qualifiedMemberName, resName, inputs, optionsBag)
			} else {
				// TODO: explicit dependencies

				// If the input properties did not contain any outputs, then we need to wrap the result in a call to pulumi.output.
				// Otherwise, we are okay as-is: the apply rewrite perfomed by computeProperty will have ensured that the result
				// is output-typed.
				fmtstr := "%s%s = pulumi.output(%s);\n"
				if g.promptDataSources[r] || transformed {
					fmtstr = "%s%s = %s;\n"
				}

				g.Printf(fmtstr, g.Indent, name, inputs)
			}
		})
		g.Printf("%s}", g.Indent)
	} else {
		// Otherwise we need to Generate multiple resources in a loop.
		count, _, err := g.computeProperty(r.Count, false, "")
		if err != nil {
			return err
		}
		inputs, transformed, err := g.computeProperty(properties, true, "i")
		if err != nil {
			return err
		}

		arrElementType := qualifiedMemberName
		if r.IsDataSource {
			fmtStr := "pulumi.Output<%s%s.%sResult>"
			if g.promptDataSources[r] {
				fmtStr = "%s%s.%sResult"
			}
			arrElementType = fmt.Sprintf(fmtStr, provider, module, strings.Title(memberName))
		}

		g.Printf("%sconst %s: %s[] = [];\n", g.Indent, name, arrElementType)
		g.Printf("%sfor (let i = 0; i < %s; i++) {\n", g.Indent, count)
		g.Indented(func() {
			if !r.IsDataSource {
				resName := g.makeResourceName(r.Name, "i")
				g.Printf("%s%s.push(new %s(%s, %s%s));\n", g.Indent, name, qualifiedMemberName, resName, inputs,
					optionsBag)
			} else {
				// TODO: explicit dependencies

				// If the input properties did not contain any outputs, then we need to wrap the result in a call to
				// pulumi.output. Otherwise, we are okay as-is: the apply rewrite perfomed by computeProperty will hav
				// ensured that the result is output-typed.
				fmtstr := "%s%s.push(pulumi.output(%s));\n"
				if g.promptDataSources[r] || transformed {
					fmtstr = "%s%s.push(%s);\n"
				}

				g.Printf(fmtstr, g.Indent, name, inputs)
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
	switch r.Provider.Name {
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
		outputs, _, err := g.computeProperty(o.Value, false, "")
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
