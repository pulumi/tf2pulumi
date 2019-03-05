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

// Package python implements a Python back-end for tf2pulumi's intermediate representation. It is responsible for
// translating the Graph IR emit by the frontend into valid Pulumi Python code that is as semantically equivalent to
// the original Terraform as possible.
package python

import (
	"bytes"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/hashicorp/terraform/config"
	"github.com/pkg/errors"
	"github.com/pulumi/pulumi-terraform/pkg/tfbridge"
	"github.com/pulumi/pulumi/pkg/util/contract"

	"github.com/pulumi/tf2pulumi/gen"
	"github.com/pulumi/tf2pulumi/il"
)

// New creates a new Python Generator that writes to the given writer and uses the given project name.
func New(projectName string, w io.Writer) gen.Generator {
	g := &generator{projectName: projectName}
	g.Emitter = gen.NewEmitter(w, g)
	return g
}

type generator struct {
	// The emitter to use when generating code.
	*gen.Emitter

	projectName   string
	needNYIHelper bool

	// put here because of copy-pasta
	// countIndex is the name (if any) of the currently in-scope count variable.
	countIndex string
	// unknownInputs is the set of input variables that may be unknown at runtime.
	unknownInputs map[*il.VariableNode]struct{}
}

func (g *generator) GeneratePreamble(modules []*il.Graph) error {
	g.Println("import pulumi")

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
					return errors.New("NYI: Python Archive Provider")
				case "http":
					return errors.New("NYI: Python HTTP Provider")
				default:
					imports = append(imports, fmt.Sprintf("import pulumi_%[1]s as %[1]s", name))
				}
			}
		}
	}

	// TODO(swgillespie) walk the graph to find optional imports

	sort.Strings(imports)
	for _, pkg := range imports {
		g.Println(pkg)
	}
	g.Println("") // end the preamble with a newline, standard Python style.
	return nil
}

func (g *generator) BeginModule(mod *il.Graph) error {
	if len(mod.Tree.Path()) != 0 {
		return errors.New("NYI: Python Modules")
	}
	return nil
}

func (g *generator) EndModule(mod *il.Graph) error {
	g.genNYIHelper(g)
	return nil
}

func (g *generator) GenerateVariables(vs []*il.VariableNode) error {
	if len(vs) != 0 {
		return errors.New("NYI: Python Variables")
	}
	return nil
}

func (g *generator) GenerateModule(m *il.ModuleNode) error {
	return errors.New("NYI: Python Modules")
}

func (g *generator) GenerateLocal(l *il.LocalNode) error {
	return errors.New("NYI: Python Locals")
}

func (g *generator) GenerateResource(r *il.ResourceNode) error {
	pkg, subpkg, class, err := resourceTypeName(r)
	if err != nil {
		return err
	}
	if subpkg != "" {
		subpkg = "." + subpkg
	}

	// TODO(swgillespie) resource explicit dependencies
	if len(r.ExplicitDeps) != 0 {
		return errors.New("NYI: Python Explicit Dependencies")
	}
	if r.Config.Mode != config.ManagedResourceMode {
		return errors.New("NYI: Python Data Source Invocation")
	}
	if r.Count != nil {
		return errors.New("NYI: Python Resource Count")
	}

	qualifiedMemberName := fmt.Sprintf("%s%s.%s", pkg, subpkg, class)
	inputs, _, err := g.computeProperty(r.Properties, false, "")
	if err != nil {
		return err
	}

	name := g.nodeName(r)
	g.Printf("%s%s = %s(%q%s)\n", g.Indent, name, qualifiedMemberName, name, inputs)
	return nil
}

func (g *generator) GenerateOutputs(os []*il.OutputNode) error {
	if len(os) != 0 {
		return errors.New("NYI: Python Outputs")
	}
	return nil
}

// lowerToLiterals gives the generator a chance to lower certain elements into literals before code generation. It is
// unclear whether or not this is useful for Python yet.
func (g *generator) lowerToLiterals(prop il.BoundNode) (il.BoundNode, error) {
	return prop, nil
}

// nodeName returns a name suitable for the given node. It consults the IL to determine a good name for the node,
// returning the selected name.
//
// In the future, this name will need to be at least somewhat unique to avoid redefining local variables, since
// Terraform namespaces names by resource and we do not. For now, this returns the Terraform name for a particular
// resource, which may not be unique.
func (g *generator) nodeName(n il.Node) string {
	if res, ok := n.(*il.ResourceNode); ok {
		return res.Config.Name
	}

	// Obviously not great...
	return "unknown"
}

// cleanName takes a name visible in Terraform config and translates it to a form suitable for Python. This involves
// working around keywords and other things that are otherwise not legal in Python identifiers. For now, this does
// nothing and returns the name verbatim.
func cleanName(name string) string {
	return name
}

//
// Copy-pasted stuff from the node backend. Don't modify anything below this line!
//

// computeProperty generates code for the given property into a string ala fmt.Sprintf. It returns both the generated
// code and a bool value that indicates whether or not any output-typed values were nested in the property value.
func (g *generator) computeProperty(prop il.BoundNode, indent bool, count string) (string, bool, error) {
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

// resourceTypeName computes the Python package, subpackage, and class name for a given resource.
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
